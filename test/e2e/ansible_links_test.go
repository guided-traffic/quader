//go:build e2e

package e2e

import (
	"net/url"
	"strings"
	"testing"
)

// TestAnsible_R_STOR_071_ClientsCheckALink: with the switch on and minSize 0, ansible-galaxy of every image of the leg installs a
// fresh collection whose archive comes through a 307 to a link of the object store; after the archive's object was overwritten in
// the bucket with other bytes of the same length, it refuses it (artifact.sha256, R-STOR-071's rule of evidence for
// ansible.artifact.get). The links issued count each attempt, so the refusal is the client's.
func TestAnsible_R_STOR_071_ClientsCheckALink(t *testing.T) {
	fx := ansibleFx(t)
	repo := fx.localRepo(t, "links")
	cfg := ansibleCfg(ansibleRepoURL(t, repo))
	admin := adminToken(t)
	env := tokenEnvVar + "=" + admin
	coll := collection{ns: "e2e" + fx.run, name: "links", version: "1.0.0"}
	fx.client(t, ansibleImages[0]).must(withCfg(cfg, coll.publishScript()), tokenEnvVar+"="+fx.token)

	r := fx.registry(t).get(ansibleRepoPath(repo)+"/v3/collections/"+coll.ns+"/"+coll.name+"/versions/1.0.0/").want(t, "the version document", 200)
	var v struct {
		DownloadURL string `json:"download_url"`
	}
	r.JSON(t, &v)
	u, err := url.Parse(v.DownloadURL)
	if err != nil || u.Path == "" {
		t.Fatalf("the version document names no download URL: %s", r)
	}
	sha := blobOf(t, repo, coll.archive())
	markClean(t, sha)
	linksOn(t)
	if link := waitForLink(t, fx.adm.withToken(admin), u.Path); !strings.Contains(link, "X-Amz-Signature=") {
		t.Fatalf("the 307 names no signed link: %q", link)
	}

	install := withCfg(cfg, "ansible-galaxy collection install "+coll.full()+" -p coll --force")
	for _, img := range ansibleImages {
		t.Run(img.name+" installs the collection through a link", func(t *testing.T) {
			before := issuedLinks(t, "ansible")
			if out, err := fx.client(t, img).sh(install, env); err != nil {
				t.Fatalf("%s install: %v\n%s", img.name, err, tailLines(out, 30))
			}
			if after := issuedLinks(t, "ansible"); after <= before {
				t.Fatalf("%s installed without a link (issued %v, before %v)", img.name, after, before)
			}
		})
	}
	driver(t, "overwrite", sha)
	for _, img := range ansibleImages {
		t.Run(img.name+" refuses the archive's altered bytes", func(t *testing.T) {
			before := issuedLinks(t, "ansible")
			out, err := fx.client(t, img).sh(install, env)
			if after := issuedLinks(t, "ansible"); after <= before {
				t.Fatalf("%s asked for the archive without getting a link (issued %v, before %v):\n%s", img.name, after, before, tailLines(out, 30))
			}
			if err == nil {
				t.Fatalf("%s installed the altered archive: R-STOR-071's declaration of ansible.artifact.get does not hold for it\n%s", img.name, tailLines(out, 30))
			}
			if !strings.Contains(strings.ToLower(out), "hash") {
				t.Errorf("%s failed, but not on the digest:\n%s", img.name, tailLines(out, 30))
			}
		})
	}
}
