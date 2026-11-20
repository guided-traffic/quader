package imagescan

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The scanner's answer is untrusted input: an analyser that the image it read took over may have written it (Q-VULN-164). The
// worker decodes it under limits into the members the detection reads and writes a new document from them (R-VULN-043), so that
// what is stored, and what the worker's analyser parses in the detection, is a document Quader wrote.
const (
	// maxBOMDepth bounds the nesting of the document, JSON objects and arrays together.
	maxBOMDepth = 32
	// maxBOMComponents bounds the components, nested ones included.
	maxBOMComponents = 1_000_000
	// maxBOMString bounds every string of a member that is kept.
	maxBOMString = 4 << 10
)

// The members of a CycloneDX document the detection reads (R-VULN-043). Every other member is dropped.
type (
	bomDocument struct {
		BOMFormat    string          `json:"bomFormat"`
		SpecVersion  string          `json:"specVersion"`
		SerialNumber string          `json:"serialNumber,omitempty"`
		Metadata     *bomMetadata    `json:"metadata,omitempty"`
		Components   []bomComponent  `json:"components,omitempty"`
		Dependencies []bomDependency `json:"dependencies,omitempty"`
	}
	bomMetadata struct {
		Component *bomComponent `json:"component,omitempty"`
	}
	bomComponent struct {
		BOMRef  string `json:"bom-ref,omitempty"`
		Type    string `json:"type"`
		Group   string `json:"group,omitempty"`
		Name    string `json:"name"`
		Version string `json:"version,omitempty"`
		PURL    string `json:"purl,omitempty"`
		// Licenses are read by the detection into each package of its report and into the package's identity (Q-VULN-183).
		Licenses   []bomLicenseChoice `json:"licenses,omitempty"`
		Properties []bomProperty      `json:"properties,omitempty"`
		Components []bomComponent     `json:"components,omitempty"`
	}
	bomLicenseChoice struct {
		License    *bomLicense `json:"license,omitempty"`
		Expression string      `json:"expression,omitempty"`
	}
	bomLicense struct {
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	}
	bomProperty struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	bomDependency struct {
		Ref       string   `json:"ref"`
		DependsOn []string `json:"dependsOn,omitempty"`
	}
)

// Rewrite decodes the scanner's document under the limits of R-VULN-043 and returns a new document of the members the detection
// reads. A document that does not decode within the limits, or whose bomFormat is not CycloneDX, is analyser_failed.
func Rewrite(doc []byte) ([]byte, error) {
	if int64(len(doc)) > MaxAnalysisBytes {
		return nil, &ScanError{Code: CodeReportTooLarge, Detail: fmt.Sprintf("the analysis exceeds %d bytes", MaxAnalysisBytes)}
	}
	if err := bomDepth(doc); err != nil {
		return nil, err
	}
	var d bomDocument
	dec := json.NewDecoder(bytes.NewReader(doc))
	if err := dec.Decode(&d); err != nil {
		return nil, refusedBOM("the analysis is not a CycloneDX document: " + err.Error())
	}
	if dec.More() {
		return nil, refusedBOM("the analysis holds data after its document")
	}
	if d.BOMFormat != "CycloneDX" {
		return nil, refusedBOM(fmt.Sprintf("the analysis is no CycloneDX document (bomFormat %q)", cut(d.BOMFormat, 64)))
	}
	if err := d.check(); err != nil {
		return nil, err
	}
	out, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func refusedBOM(detail string) error { return &ScanError{Code: CodeAnalyserFailed, Detail: detail} }

// bomDepth scans the raw document for its nesting depth before a decoder builds anything from it.
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

// check counts the components and bounds every string that is kept.
func (d *bomDocument) check() error {
	n := 0
	strs := []string{d.SpecVersion, d.SerialNumber}
	var walk func(cs []bomComponent) error
	visit := func(c *bomComponent) error {
		n++
		if n > maxBOMComponents {
			return refusedBOM(fmt.Sprintf("the analysis holds more than %d components", maxBOMComponents))
		}
		strs = append(strs, c.BOMRef, c.Type, c.Group, c.Name, c.Version, c.PURL)
		for _, l := range c.Licenses {
			strs = append(strs, l.Expression)
			if l.License != nil {
				strs = append(strs, l.License.ID, l.License.Name)
			}
		}
		for _, p := range c.Properties {
			strs = append(strs, p.Name, p.Value)
		}
		if err := tooLong(strs); err != nil {
			return err
		}
		strs = strs[:0]
		return walk(c.Components)
	}
	walk = func(cs []bomComponent) error {
		for i := range cs {
			if err := visit(&cs[i]); err != nil {
				return err
			}
		}
		return nil
	}
	if d.Metadata != nil && d.Metadata.Component != nil {
		if err := visit(d.Metadata.Component); err != nil {
			return err
		}
	}
	if err := walk(d.Components); err != nil {
		return err
	}
	for _, dep := range d.Dependencies {
		strs = append(strs, dep.Ref)
		strs = append(strs, dep.DependsOn...)
		if err := tooLong(strs); err != nil {
			return err
		}
		strs = strs[:0]
	}
	return tooLong(strs)
}

func tooLong(strs []string) error {
	for _, s := range strs {
		if len(s) > maxBOMString {
			return refusedBOM(fmt.Sprintf("the analysis holds a string of %d bytes; the limit is %d", len(s), maxBOMString))
		}
	}
	return nil
}
