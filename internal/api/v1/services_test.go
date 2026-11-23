package v1

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

func TestIdempotencyKeyAndBodyHash(t *testing.T) {
	key := func(vals ...string) (string, bool, error) {
		r := httptest.NewRequest("POST", "/x", nil)
		for _, v := range vals {
			r.Header.Add(HeaderIdempotencyKey, v)
		}
		return idemKey(r)
	}
	t.Run("R-API-014 the key is 1 to 255 printable ASCII characters and optional", func(t *testing.T) {
		if _, has, err := key(); has || err != nil {
			t.Fatalf("absent: %v %v", has, err)
		}
		if k, has, err := key("0f8fad5b-d9cb-469f-a165-70867728950e"); !has || err != nil || k == "" {
			t.Fatalf("uuid: %v %v", has, err)
		}
		for _, bad := range []string{"", strings.Repeat("a", 256), "tab\there", "ümlaut", "new\nline"} {
			if _, _, err := key(bad); err == nil {
				t.Errorf("%q accepted", bad)
			}
		}
		if _, _, err := key("a", "b"); err == nil {
			t.Error("two keys accepted")
		}
		if _, _, err := key(strings.Repeat("a", 255)); err != nil {
			t.Errorf("255 characters refused: %v", err)
		}
	})
	t.Run("R-API-014 the same JSON in another layout is the same request", func(t *testing.T) {
		a := canonicalSHA256([]byte(`{"name":"a","labels":{"x":"1","y":"2"},"n":1.50}`))
		b := canonicalSHA256([]byte("{ \"labels\": {\"y\":\"2\", \"x\":\"1\"},\n \"n\": 1.50, \"name\": \"a\" }"))
		c := canonicalSHA256([]byte(`{"name":"b"}`))
		if string(a) != string(b) || string(a) == string(c) {
			t.Fatal("canonical hash")
		}
		if len(canonicalSHA256(nil)) != 32 {
			t.Fatal("empty body")
		}
	})
}

func TestItemPaths(t *testing.T) {
	cases := []struct {
		path string
		opts PathOptions
		ok   bool
	}{
		{"a/b/c.txt", PathOptions{}, true},
		{"@team-a/ui/-/ui-1.4.0.tgz", PathOptions{}, true},
		{"a/../b", PathOptions{}, false},
		{"a//b", PathOptions{}, false},
		{"./a", PathOptions{}, false},
		{"a/.", PathOptions{}, false},
		{"/a", PathOptions{}, false},
		{"", PathOptions{}, false},
		{"", PathOptions{AllowRoot: true}, true},
		{"a/b/", PathOptions{}, false},
		{"a/b/", PathOptions{AllowTrailingSlash: true}, true},
		{"/", PathOptions{AllowTrailingSlash: true}, false},
		{"a\x00b", PathOptions{}, false},
		{"a\nb", PathOptions{}, false},
		{"a\xffb", PathOptions{}, false},
		{"ä/ö", PathOptions{}, true},
	}
	for _, c := range cases {
		t.Run("R-API-004 "+strings.ReplaceAll(c.path, "\x00", "NUL"), func(t *testing.T) {
			_, err := CheckItemPath(c.path, c.opts)
			if (err == nil) != c.ok {
				t.Fatalf("CheckItemPath(%q, %+v) = %v", c.path, c.opts, err)
			}
			if err != nil {
				if st, code, _ := problemOf(t, err); st != 400 || code != "invalid_path" {
					t.Fatalf("%d %s", st, code)
				}
			}
		})
	}
	t.Run("R-API-004 a create with a reserved name answers 422 reserved_name", func(t *testing.T) {
		if err := CheckReservedName("/key", "npm-local", "batch-create"); err != nil {
			t.Fatal(err)
		}
		st, code, f := problemOf(t, CheckReservedName("/key", "batch-create", "batch-create"))
		if st != 422 || code != "reserved_name" || f[0].Pointer != "/key" {
			t.Fatalf("%d %s %+v", st, code, f)
		}
	})
}

func TestTaskParameters(t *testing.T) {
	type gcArgs struct {
		DryRun bool `json:"dryRun"`
	}
	do := func(body string, proto func() any) error {
		r := httptest.NewRequest("POST", "/x", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		_, err := decodeTaskParams([]byte(body), r, proto)
		return err
	}
	gc := func() any { return &gcArgs{} }
	t.Run("R-OPS-020 dryRun is a boolean and nothing else is accepted", func(t *testing.T) {
		if err := do(`{"dryRun":true}`, gc); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{`{"dryrun":true}`, `{"dryRun":"yes"}`, `[1]`, `{"dryRun":true,"dryRun":false}`} {
			if err := do(bad, gc); err == nil {
				t.Errorf("%s accepted", bad)
			}
		}
	})
	t.Run("R-OPS-019 a task without parameters takes an empty object only", func(t *testing.T) {
		if err := do(`{}`, nil); err != nil {
			t.Fatal(err)
		}
		if err := do(`{"x":1}`, nil); err == nil {
			t.Fatal("a parameter of a task that takes none was accepted")
		}
	})
	t.Run("R-STOR-026 a parameter the task's own check refuses answers 422 with its field", func(t *testing.T) {
		err := do(`{"minAge":"1h"}`, func() any { return &boundedArgs{} })
		var e *errs.Error
		if !errors.As(err, &e) || e.Status() != http.StatusUnprocessableEntity || len(e.Fields) != 1 || e.Fields[0].Pointer != "/minAge" {
			t.Fatalf("got %v", err)
		}
		if err := do(`{"minAge":"3h"}`, func() any { return &boundedArgs{} }); err != nil {
			t.Fatal(err)
		}
	})
}

// boundedArgs has a check of its own, as the orphan sweep's parameters do (blob.SweepJobArgs).
type boundedArgs struct {
	MinAge string `json:"minAge"`
}

func (a *boundedArgs) Validate() error {
	if a.MinAge == "1h" {
		return errs.E(errs.Invalid, errs.CodeValidation, "invalid", errs.WithFields(errs.FieldError{Pointer: "/minAge", Code: "too_short"}))
	}
	return nil
}

func TestEveryOperationCanBeRateLimited(t *testing.T) {
	doc := specDocument(t)
	t.Run("R-API-016 every operation under /api/v1/ documents the 429 the rate-limit stage can answer", func(t *testing.T) {
		for path, item := range doc.Paths.Map() {
			for method, op := range item.Operations() {
				if op.Responses.Status(429) == nil {
					t.Errorf("%s %s (%s) does not document 429", method, path, op.OperationID)
				}
			}
		}
	})
}
