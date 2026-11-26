package errs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"strings"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// Shaped is what a client reads of an error answer in one of the error shapes of a Bastion (bastion.md R-BAS-095, Q-BAS-337):
// the answer's code, empty where the shape carries none (the distribution's list as a format renders it), and its detail.
type Shaped struct {
	Code   string
	Detail string
}

// ReadShape reads an error answer as a client of the shape reads it: the media type of contentType and the members of the body
// the shape's clients print (R-BAS-095's table). It returns an error that names what is missing when the answer is not of the
// shape. The Bastion's own renderer and every format's WriteError are held to it (the engine's tests and kit check 19), so that
// the refusals of a Bastion and of the main installation's backstop read alike.
func ReadShape[S ~string](declared S, contentType string, body []byte) (Shaped, error) {
	shape := iface.ErrorShape(declared)
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return Shaped{}, fmt.Errorf("the Content-Type %q is no media type", contentType)
	}
	want := "application/json"
	switch shape {
	case iface.ErrorShapeProblem:
		want = ProblemContentType
	case iface.ErrorShapeText:
		want = "text/plain"
	case iface.ErrorShapeNPM, iface.ErrorShapeDistribution, iface.ErrorShapeGalaxy:
	default:
		return Shaped{}, fmt.Errorf("%q is none of the five error shapes", shape)
	}
	if media != want {
		return Shaped{}, fmt.Errorf("the Content-Type is %s, the shape %s is %s", media, shape, want)
	}
	if shape == iface.ErrorShapeText {
		return readText(body)
	}
	switch shape {
	case iface.ErrorShapeProblem:
		var p struct {
			Status *int   `json:"status"`
			Code   string `json:"code"`
			Detail string `json:"detail"`
			Title  string `json:"title"`
		}
		if err := json.Unmarshal(body, &p); err != nil || p.Status == nil || p.Code == "" || p.Title == "" {
			return Shaped{}, errors.New("the body is no problem details with the members status, title and code")
		}
		return Shaped{Code: p.Code, Detail: p.Detail}, nil
	case iface.ErrorShapeNPM:
		var n struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if err := json.Unmarshal(body, &n); err != nil || n.Error == "" {
			return Shaped{}, errors.New(`the body has no member "error", which npm prints`)
		}
		return Shaped{Code: n.Code, Detail: n.Error}, nil
	case iface.ErrorShapeDistribution:
		var d struct {
			Errors []struct {
				Code    string          `json:"code"`
				Message string          `json:"message"`
				Detail  json.RawMessage `json:"detail"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(body, &d); err != nil || len(d.Errors) == 0 || d.Errors[0].Code == "" || d.Errors[0].Message == "" {
			return Shaped{}, errors.New(`the body has no list "errors" whose first entry has a code and a message, which docker and podman print`)
		}
		var own struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(d.Errors[0].Detail, &own)
		return Shaped{Code: own.Code, Detail: d.Errors[0].Message}, nil
	}
	var g struct {
		Errors []struct {
			Status string `json:"status"`
			Code   string `json:"code"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &g); err != nil || len(g.Errors) == 0 || g.Errors[0].Status == "" || g.Errors[0].Code == "" || g.Errors[0].Detail == "" {
		return Shaped{}, errors.New(`the body has no list "errors" whose first entry has a status, a code and a detail, which ansible-galaxy prints`)
	}
	return Shaped{Code: g.Errors[0].Code, Detail: g.Errors[0].Detail}, nil
}

// readText reads the text shape: one line "<code>: <detail>".
func readText(body []byte) (Shaped, error) {
	if bytes.ContainsAny(body, "\r\n") {
		return Shaped{}, errors.New("the body is more than one line")
	}
	code, detail, ok := strings.Cut(string(body), ": ")
	if !ok || code == "" || strings.ContainsAny(code, " \t") || detail == "" {
		return Shaped{}, errors.New(`the body is no line "<code>: <detail>"`)
	}
	return Shaped{Code: code, Detail: detail}, nil
}
