package v1

import (
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci"
)

// TestR_API_030_TheSnippetsNameTheReadersParts: every snippet names the parts that are the reader's (ADR-208), by the
// credential mode and the template's examples, and the OCI pull without a package names the image its push names (Q-UI-802).
func TestR_API_030_TheSnippetsNameTheReadersParts(t *testing.T) {
	data := formats.SetupData{
		RegistryURL: "https://quader.example.com/v2/web/images/", ImagePrefix: "quader.example.com/web/images", Repository: "web/images",
		Project: "web", Key: "images", RequiresAuth: true,
	}
	tpls := (&oci.Format{}).SetupTemplates()
	render := func(creds, pkg string) map[string]snippet {
		t.Helper()
		ss, err := renderSnippets(setupInput{Templates: tpls, Client: oci.ClientDocker, Credentials: creds, TokenEnvVar: "QUADER_TOKEN", Data: data, Package: pkg, MayPublish: true})
		if err != nil {
			t.Fatal(err)
		}
		return byID(ss)
	}
	image := snippetMark{Text: "myapp:1.0", Kind: markValue}
	for _, c := range []struct {
		creds string
		id    string
		want  []snippetMark
	}{
		{credEnv, "docker.configure.shell", []snippetMark{{Text: "${QUADER_TOKEN}", Kind: markToken}}},
		{credPlaceholder, "docker.configure.shell", []snippetMark{{Text: "{{token}}", Kind: markToken}}},
		{credNone, "docker.configure.shell", []snippetMark{}},
		{credEnv, "docker.install", []snippetMark{image}},
		{credEnv, "docker.publish", []snippetMark{image}},
		{credEnv, "docker.publish.buildx", []snippetMark{image}},
	} {
		sn := render(c.creds, "")[c.id]
		got := append([]snippetMark{}, sn.Marks...)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %s: marks %+v, want %+v (content %q)", c.creds, c.id, got, c.want, sn.Content)
		}
	}
	if got := render(credEnv, "")["docker.install"].Content; got != "docker pull quader.example.com/web/images/myapp:1.0\n" {
		t.Errorf("pull without a package: %q", got)
	}
	// A package asked for (the install panel) replaces the example, and nothing of it is the reader's any more.
	pull := render(credNone, "team/app")["docker.install"]
	if pull.Content != "docker pull quader.example.com/web/images/team/app\n" || len(pull.Marks) != 0 {
		t.Errorf("pull of team/app: %q, marks %+v", pull.Content, pull.Marks)
	}
}

// TestR_API_030_AMarkIsNamedOnceInTheOrderItStands: the placeholders of the core are marked where they stand, once each, in
// the order of their first place.
func TestR_API_030_AMarkIsNamedOnceInTheOrderItStands(t *testing.T) {
	tpl := formats.SetupTemplate{
		ID: "x.publish", Client: "x", Section: "publish", Lang: "shell",
		Template: "x push ./a.tgz --to <project>/<repository> --token " + formats.SetupTokenRef + " ./a.tgz\n", Examples: []string{"./a.tgz"},
	}
	ss, err := renderSnippets(setupInput{Templates: []formats.SetupTemplate{tpl}, Client: "x", Credentials: credEnv, TokenEnvVar: "T", MayPublish: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []snippetMark{{"./a.tgz", markValue}, {"<project>", markValue}, {"<repository>", markValue}, {"${T}", markToken}}
	if !reflect.DeepEqual(ss[0].Marks, want) {
		t.Errorf("marks %+v, want %+v", ss[0].Marks, want)
	}
}
