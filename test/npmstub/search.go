package npmstub

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// The registry's search, `GET /-/v1/search?text=&size=&from=` (remote-curation.md R-CUR-008, formats/npm.md R-NPM-049): the packages
// whose name holds the text, in name order, in the shape the public registry answers (objects with a package and flags, a total). The
// upstream catalogue's integration cases count its requests (KindSearch). A text shorter than two characters answers 400, as the public
// registry does.

// KindSearch is the request kind of a search.
const KindSearch = "search"

const searchPath = "/-/v1/search"

func (s *Server) serveSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	text := strings.ToLower(strings.TrimSpace(q.Get("text")))
	if len(text) < 2 {
		writeError(w, http.StatusBadRequest, "text must be at least two characters")
		return
	}
	size, err := strconv.Atoi(q.Get("size"))
	if err != nil || size < 1 || size > 250 {
		size = 20
	}
	from, err := strconv.Atoi(q.Get("from"))
	if err != nil || from < 0 {
		from = 0
	}
	s.mu.Lock()
	var hits []string
	for _, n := range s.namesLocked() {
		if strings.Contains(strings.ToLower(n), text) {
			hits = append(hits, n)
		}
	}
	s.mu.Unlock()
	type pkg struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description"`
		Date        string `json:"date"`
	}
	type object struct {
		Package pkg            `json:"package"`
		Flags   map[string]int `json:"flags"`
	}
	objects := []object{}
	for i := from; i < len(hits) && i < from+size; i++ {
		objects = append(objects, object{
			Package: pkg{Name: hits[i], Version: "0.0.0", Description: "A package of the test registry.", Date: "2026-11-24T00:00:00.000Z"},
			Flags:   map[string]int{"insecure": 0},
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=300")
	_ = json.NewEncoder(w).Encode(map[string]any{"objects": objects, "total": len(hits), "time": "Mon Nov 24 2026 00:00:00 GMT+0000"})
}
