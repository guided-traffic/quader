package ansible

import (
	"context"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/ansibletest"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
)

// Q-ANS-91, R-ANS-010, R-ANS-034: a collection that holds a contained link publishes, and its documentation's index lists what the
// archive holds as regular files: the module behind a link is not documented under the link's name, because nothing is read through a
// link.
func TestR_ANS_010_APublishOfACollectionWithALinkDocumentsTheRegularFilesOnly(t *testing.T) {
	h := newHost(t)
	s := collectionSpec("1.0.0")
	s.Files["plugins/modules/ping.py"] = "DOCUMENTATION = '''\nmodule: ping\nshort_description: Pings\n'''\n"
	s.Links = map[string]string{"plugins/modules/ping_alias.py": "ping.py"}
	r, err := publish(h, collectiontest.Build(s), true, "")
	if err != nil {
		t.Fatal(err)
	}
	if task := taskOf(t, h, r); task["state"] != "completed" {
		t.Fatalf("task = %v", task)
	}
	st := h.FormatData(ID).(*ansibletest.Store)
	ctx, repo := context.Background(), h.Ref().ID
	if e, ok, _ := st.DocEntry(ctx, repo, "acme.tools", "1.0.0", "module", "ping"); !ok || !e.HasDocs {
		t.Errorf("the regular module: %+v %v", e, ok)
	}
	if e, ok, _ := st.DocEntry(ctx, repo, "acme.tools", "1.0.0", "module", "ping_alias"); ok {
		t.Errorf("the link is documented: %+v", e)
	}
	if v := get(t, h, "v3/collections/acme/tools/versions/1.0.0/"); v.Code != http.StatusOK {
		t.Errorf("version: %d", v.Code)
	}
}
