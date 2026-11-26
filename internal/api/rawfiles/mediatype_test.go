package rawfiles

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// TestR_GENERIC_017_AFormsTypeIsNoFilesType: wget and PowerShell put the type of a form on a body they were not given a type for;
// such a deploy stores none.
func TestR_GENERIC_017_AFormsTypeIsNoFilesType(t *testing.T) {
	for _, v := range []string{"application/x-www-form-urlencoded", "Application/X-WWW-Form-Urlencoded; charset=utf-8"} {
		r := httptest.NewRequest("PUT", "/files/it/r/a", nil)
		r.Header.Set("Content-Type", v)
		if got, err := mediaType(r); err != nil || got != "" {
			t.Errorf("mediaType(%q) = %q, %v", v, got, err)
		}
	}
}

// TestR_GENERIC_017_TheGuessIsAClosedListOfText: a file deployed without a type gets one from the extension of its name for the
// common text types, compared without case; nothing a browser renders or runs is guessed, and an unknown name gets none.
func TestR_GENERIC_017_TheGuessIsAClosedListOfText(t *testing.T) {
	for in, want := range map[string]string{
		"README.md": "text/markdown", "docs/guide.MARKDOWN": "text/markdown", "notes.txt": "text/plain", "build/output.log": "text/plain",
		"a.json": "application/json", "values.yaml": "application/yaml", "ci.YML": "application/yaml", "Cargo.toml": "application/toml",
		"table.csv": "text/csv",
		// Never guessed: a browser renders or runs them.
		"index.html": "", "page.htm": "", "evil.svg": "", "feed.xml": "", "app.js": "", "app.mjs": "", "style.css": "", "doc.pdf": "",
		"photo.png": "", "page.xhtml": "",
		// No extension, the extension of a folder, an unknown one.
		"README": "", "dir.md/file": "", "installer.msi": "", "archive.tar.gz": "",
	} {
		if got := guessMediaType(in); got != want {
			t.Errorf("guessMediaType(%q) = %q, want %q", in, got, want)
		}
	}
	plain := []string{"application/json", "application/yaml", "application/toml"}
	for _, ext := range guessedExtensions() {
		mt := guessMediaType("x" + ext)
		if mt == "" || !strings.HasPrefix(mt, "text/") && !slices.Contains(plain, mt) {
			t.Errorf("%s guesses %s, which is no plain text type", ext, mt)
		}
		if strings.Contains(mt, "html") || strings.Contains(mt, "xml") || strings.Contains(mt, "script") {
			t.Errorf("%s guesses %s, which a browser renders or runs", ext, mt)
		}
	}
}

func TestR_STOR_007_TheStoredMediaTypeIsParsedAndNormalised(t *testing.T) {
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"", "", true},
		{"application/octet-stream", "application/octet-stream", true},
		{"Text/HTML; Charset=UTF-8", "text/html; charset=UTF-8", true},
		{`text/plain; name="a b"`, `text/plain; name="a b"`, true},
		{"not a media type", "", false},
		{"text/html; <script>", "", false},
		{"text/html;;", "", false},
	} {
		t.Run("R-STOR-007 "+c.in, func(t *testing.T) {
			r := httptest.NewRequest("PUT", "/files/it/r/a", nil)
			r.Header.Set("Content-Type", c.in)
			got, err := mediaType(r)
			if c.ok != (err == nil) || got != c.want {
				t.Fatalf("mediaType(%q) = %q, %v", c.in, got, err)
			}
		})
	}
}
