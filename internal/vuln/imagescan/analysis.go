package imagescan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// The scanner's answer to an analysis is untrusted input: an analyser that the image it read took over may have written it. The worker
// checks it in Go and keeps it as answered (R-VULN-043, Q-VULN-198): the detection parses it in a scanner pod, the kind of pod whose
// analyser parsed the whole image, so no rewrite protects a parser the worker runs (it runs none). The check protects what the worker does
// with the document: store it, count its packages, and send it back to a scanner.
const (
	// maxBOMDepth bounds the nesting of the document, JSON objects and arrays together.
	maxBOMDepth = 32
	// maxBOMComponents bounds the components, nested ones included, counted while the document is walked.
	maxBOMComponents = 1_000_000
)

// analyserVersion is the grammar of an analyser version on the wire (Quader-Analyser-Version, the query of a detection) and in a row:
// at most 32 letters, digits and . + _ ~ - (R-VULN-043).
var analyserVersion = regexp.MustCompile(`^[A-Za-z0-9.+_~-]{1,32}$`)

// ValidAnalyserVersion reports whether v is an analyser version as a call may name it.
func ValidAnalyserVersion(v string) bool { return analyserVersion.MatchString(v) }

// CheckAnalysis checks the scanner's document within the bounds of R-VULN-043 and counts the image's packages (its library and
// application components, at any depth). A document above MaxAnalysisBytes is report_too_large; one that is not one JSON document,
// nests deeper than 32 levels, is not CycloneDX, names no specVersion, or holds more than 1,000,000 components is analyser_failed. The
// document itself is not changed: what is stored is what the scanner answered.
func CheckAnalysis(doc []byte) (packages int, err error) { return checkAnalysis(doc, MaxAnalysisBytes) }

// checkAnalysis is CheckAnalysis with the size bound given, so that a unit test shows the bound with kilobytes.
func checkAnalysis(doc []byte, limit int64) (packages int, err error) {
	if int64(len(doc)) > limit {
		return 0, &ScanError{Code: CodeReportTooLarge, Detail: fmt.Sprintf("the analysis exceeds %d bytes", limit), Terminal: true}
	}
	// The depth is bounded on the raw bytes before the walk below recurses into them.
	if err := bomDepth(doc); err != nil {
		return 0, err
	}
	w := &bomWalker{dec: json.NewDecoder(bytes.NewReader(doc))}
	tok, err := w.dec.Token()
	if err != nil {
		return 0, refusedBOM("the analysis is not a JSON document: " + err.Error())
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return 0, refusedBOM("the analysis is not a JSON object")
	}
	if err := w.object(bomDoc); err != nil {
		var se *ScanError
		if errors.As(err, &se) {
			return 0, err
		}
		return 0, refusedBOM("the analysis is not a JSON document: " + err.Error())
	}
	if _, err := w.dec.Token(); !errors.Is(err, io.EOF) {
		return 0, refusedBOM("the analysis holds data after its document")
	}
	switch {
	case w.format != "CycloneDX":
		return 0, refusedBOM(fmt.Sprintf("the analysis is no CycloneDX document (bomFormat %q)", cut(w.format, 64)))
	case w.spec == "":
		return 0, refusedBOM("the analysis names no specVersion")
	}
	return w.packages, nil
}

func refusedBOM(detail string) error { return &ScanError{Code: CodeAnalyserFailed, Detail: detail} }

// bomDepth scans the raw document for its nesting depth before a decoder reads anything from it.
func bomDepth(doc []byte) error {
	depth, inString := 0, false
	for i := 0; i < len(doc); i++ {
		c := doc[i]
		if inString {
			switch c {
			case '\\':
				i++
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxBOMDepth {
				return refusedBOM(fmt.Sprintf("the analysis is nested deeper than %d levels", maxBOMDepth))
			}
		case '}', ']':
			depth--
		}
	}
	return nil
}

// bomKind is what a value of the walk is: the document, the array of components of the document or of a component, one component, or
// anything else, which is walked and not read.
type bomKind int

const (
	bomOther bomKind = iota
	bomDoc
	bomComponents
	bomComponent
)

// bomWalker walks a document token by token, so that a document of many tiny components costs no memory per component.
type bomWalker struct {
	dec                  *json.Decoder
	format, spec         string
	components, packages int
}

// object walks the members of an object whose opening brace was read.
func (w *bomWalker) object(kind bomKind) error {
	for w.dec.More() {
		tok, err := w.dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		switch {
		case kind == bomDoc && key == "bomFormat":
			w.format, err = w.text()
		case kind == bomDoc && key == "specVersion":
			w.spec, err = w.text()
		case (kind == bomDoc || kind == bomComponent) && key == "components":
			err = w.value(bomComponents)
		case kind == bomComponent && key == "type":
			var typ string
			if typ, err = w.text(); typ == "library" || typ == "application" {
				w.packages++
			}
		default:
			err = w.value(bomOther)
		}
		if err != nil {
			return err
		}
	}
	_, err := w.dec.Token() // the closing brace
	return err
}

// array walks the elements of an array whose opening bracket was read; an element of an array of components is a component.
func (w *bomWalker) array(kind bomKind) error {
	for w.dec.More() {
		elem := bomOther
		if kind == bomComponents {
			w.components++
			if w.components > maxBOMComponents {
				return refusedBOM(fmt.Sprintf("the analysis holds more than %d components", maxBOMComponents))
			}
			elem = bomComponent
		}
		if err := w.value(elem); err != nil {
			return err
		}
	}
	_, err := w.dec.Token() // the closing bracket
	return err
}

// value walks one value.
func (w *bomWalker) value(kind bomKind) error {
	tok, err := w.dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok {
		if d == '{' {
			return w.object(kind)
		}
		return w.array(kind)
	}
	return nil
}

// text reads a value that should be a string; any other value is walked and reads as "".
func (w *bomWalker) text() (string, error) {
	tok, err := w.dec.Token()
	if err != nil {
		return "", err
	}
	switch t := tok.(type) {
	case string:
		return t, nil
	case json.Delim:
		if t == '{' {
			return "", w.object(bomOther)
		}
		return "", w.array(bomOther)
	}
	return "", nil
}
