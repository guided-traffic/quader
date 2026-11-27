package catalogue

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// RenderedOn implements [Reader].
func (c *Catalogue) RenderedOn() string {
	s, err := c.served()
	if err != nil {
		return ""
	}
	return s.origin
}

// originOf is the base URL the documents of a snapshot were rendered on: the member baseUrl of its state document (R-BAS-135),
// without a trailing slash. It reads the state document only when it is another than the one of cur; a state document that cannot be
// read leaves it empty, and the next round reads it again.
func (c *Catalogue) originOf(ctx context.Context, next, cur *snapshot) string {
	path := iface.CataloguePath{Kind: iface.CatalogueState}.String()
	r, ok := next.lookup(path)
	if !ok {
		return ""
	}
	if cur != nil {
		if was, ok := cur.lookup(path); ok && was.sha == r.sha {
			return cur.origin
		}
	}
	gz, err := c.o.Store.GetCatalogueDocument(ctx, r.sha, maxDocGzip)
	var raw []byte
	if err == nil {
		raw, err = open(gz, r.sha, r.size)
	}
	var st iface.CatalogueStateDocument
	if err == nil {
		err = json.Unmarshal(raw, &st)
	}
	if err != nil {
		c.log.Warn("the state document of the public catalogue cannot be read; its pages keep the addresses they were rendered on", "error", err)
		c.lost(r.name)
		return ""
	}
	return strings.TrimSuffix(st.BaseURL, "/")
}
