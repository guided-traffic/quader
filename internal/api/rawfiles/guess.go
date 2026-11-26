package rawfiles

import (
	"path"
	"strings"
)

// formEncoded is the type of an HTML form's body, which wget and PowerShell's Invoke-WebRequest put on a request body they were not
// given a type for. A file uploaded with them is no form, so a deploy that carries it stores no type (formats/generic.md
// R-GENERIC-017, Q-GENERIC-12).
const formEncoded = "application/x-www-form-urlencoded"

// guessMediaType is the media type a file deployed without one gets from the extension of its name, compared without case
// (R-GENERIC-017, Q-GENERIC-11), or "" for any other name: the item then stores none and is served as application/octet-stream
// (R-GENERIC-005). The list is closed and holds text a browser shows as text or downloads, never renders as a document and never
// runs. HTML, SVG, XML, JavaScript, CSS, PDF, images and every other type a browser renders or runs are not here, and never will
// be: a file of such a name keeps application/octet-stream unless its uploader sends a type. Whatever the type, a file of /files/
// is served as an attachment, with nosniff and the sandboxing policy (R-ARCH-007).
func guessMediaType(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt", ".log":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".toml":
		return "application/toml"
	case ".csv":
		return "text/csv"
	}
	return ""
}

// guessedExtensions are the extensions guessMediaType knows, for its test.
func guessedExtensions() []string {
	return []string{".md", ".markdown", ".txt", ".log", ".json", ".yaml", ".yml", ".toml", ".csv"}
}
