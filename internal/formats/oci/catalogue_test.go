package oci

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// hubAnswer is an upstream that answers every Get with one canned status and body.
type hubAnswer struct {
	status int
	body   string
}

func (hubAnswer) Base() *url.URL            { u, _ := url.Parse("https://registry-1.docker.io/"); return u }
func (hubAnswer) Settings() json.RawMessage { return nil }
func (h hubAnswer) Get(context.Context, *url.URL, string, int64) (*discover.Response, error) {
	return &discover.Response{Status: h.status, Body: []byte(h.body)}, nil
}

func (hubAnswer) Document(context.Context, string, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (hubAnswer) Artifact(context.Context, string, int64) ([]byte, bool, error) {
	return nil, false, nil
}

// TestR_OCI_038_DockerHubsSearchFallsBackWhenItsAnswerIsNotAPage: Docker Hub's repository search is undocumented, so an answer that is not
// a page of the expected shape (a 404, a body that does not parse, a body without results) is a *discover.UnavailableError of shape, which
// the engine turns into "no search" for its cache period (R-CUR-008, Q-CUR-15). Milliseconds, answers of a few bytes.
func TestR_OCI_038_DockerHubsSearchFallsBackWhenItsAnswerIsNotAPage(t *testing.T) {
	for name, h := range map[string]hubAnswer{
		"a 404":                  {404, `{"message":"not found"}`},
		"a body that is no JSON": {200, `<html>maintenance</html>`},
		"a page without results": {200, `{"count":3}`},
		"a server error":         {502, ``},
	} {
		t.Run("R-OCI-038 "+name, func(t *testing.T) {
			_, err := remoteAdapter{}.Search(context.Background(), h, discover.SearchQuery{Text: "nginx", Limit: 5})
			var ue *discover.UnavailableError
			if !errors.As(err, &ue) || !ue.Shape || ue.Reason != hubShape {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
