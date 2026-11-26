package catalogue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Answer is the management API's answer to one read.
type Answer struct {
	Status      int
	ContentType string
	Body        []byte
}

// Renderer reads the management API as the anonymous caller of the default endpoint, through its own router and handlers with
// the declarations of R-API-058 but outside the edge chain (no limit, no request log, no usage record), with base as the response
// base URL (R-BAS-104). The composition root implements it.
type Renderer interface {
	Get(ctx context.Context, base, target string) (Answer, error)
}

// errRender is a read that did not answer what the builder needs: a status other than 200 and 404, or a body that does not read.
type errRender struct {
	target string
	status int
	detail string
}

func (e errRender) Error() string {
	return fmt.Sprintf("catalogue: GET %s answered %d %s", e.target, e.status, e.detail)
}

// reader is one registration's view of the renderer.
type reader struct {
	r    Renderer
	base string
}

// get reads target and answers its body; found is false for a 404 (and the 401 of a part the anonymous caller does not read, the
// wiki of a repository whose wiki is not public).
func (rd reader) get(ctx context.Context, target string) (Answer, bool, error) {
	a, err := rd.r.Get(ctx, rd.base, target)
	if err != nil {
		return Answer{}, false, err
	}
	switch a.Status {
	case http.StatusOK:
		return a, true, nil
	case http.StatusNotFound, http.StatusUnauthorized, http.StatusForbidden:
		return a, false, nil
	}
	return a, false, errRender{target: target, status: a.Status, detail: strings.TrimSpace(string(firstBytes(a.Body, 300)))}
}

// json reads target into v; found as get.
func (rd reader) json(ctx context.Context, target string, v any) (Answer, bool, error) {
	a, ok, err := rd.get(ctx, target)
	if err != nil || !ok {
		return a, ok, err
	}
	dec := json.NewDecoder(bytes.NewReader(a.Body))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return a, false, errRender{target: target, status: a.Status, detail: "a body that does not read: " + err.Error()}
	}
	return a, true, nil
}

// page is one page of a list of the management API.
type page struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

// list reads every page of a list (limit per page) and answers the pages' items.
func (rd reader) list(ctx context.Context, target string, limit int) ([]json.RawMessage, bool, error) {
	var items []json.RawMessage
	cursor := ""
	for {
		t := withQuery(target, "limit", strconv.Itoa(limit))
		if cursor != "" {
			t = withQuery(t, "cursor", cursor)
		}
		var p page
		_, ok, err := rd.json(ctx, t, &p)
		if err != nil || !ok {
			return nil, ok, err
		}
		items = append(items, p.Items...)
		if p.NextCursor == nil || *p.NextCursor == "" {
			return items, true, nil
		}
		cursor = *p.NextCursor
	}
}

// withQuery adds one query parameter to a target.
func withQuery(target, key, value string) string {
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return target + sep + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}

// esc escapes one path segment of a target.
func esc(s string) string { return url.PathEscape(s) }

func firstBytes(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}
