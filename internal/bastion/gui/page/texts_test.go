package page

import (
	"encoding/xml"
	"html"
	"os"
	"regexp"
	"testing"
)

// xliffUnits reads the application's German translation file: each unit's English source and German target by its id, a
// placeholder written as {its name} ("Packages in {project}").
func xliffUnits(t *testing.T) map[string][2]string {
	t.Helper()
	raw, err := os.ReadFile("../../../../web/src/locale/messages.de.xlf")
	if err != nil {
		t.Fatal(err)
	}
	type inner struct {
		XML string `xml:",innerxml"`
	}
	var doc struct {
		Units []struct {
			ID     string `xml:"id,attr"`
			Source inner  `xml:"segment>source"`
			Target inner  `xml:"segment>target"`
		} `xml:"file>unit"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ph := regexp.MustCompile(`<ph [^>]*equiv="([^"]+)"[^>]*/>`)
	text := func(s string) string { return html.UnescapeString(ph.ReplaceAllString(s, "{$1}")) }
	out := make(map[string][2]string, len(doc.Units))
	for _, u := range doc.Units {
		out[u.ID] = [2]string{text(u.Source.XML), text(u.Target.XML)}
	}
	return out
}

// TestR_UI_272_TheCrawlerTextsMatchTheApplicationsGermanWords holds the words the crawler text writes to the application's: the
// project upstream's description and the names of the repositories the product declares, which both say by key (R-UI-272), and the
// titles a page shares with the application (W-35's B.11). A word changed in one place and not in the other fails here.
func TestR_UI_272_TheCrawlerTextsMatchTheApplicationsGermanWords(t *testing.T) {
	units := xliffUnits(t)
	en, de := TextsOf("en"), TextsOf("de")
	want := map[string][2]string{
		"upstream.project.description": {en.UpstreamDescription, de.UpstreamDescription},
		"bastion.start.title":          {en.Projects, de.Projects},
		"nav.packages":                 {en.Packages, de.Packages},
		"setup.heading":                {en.SetupClient, de.SetupClient},
		"bastion.title.packages":       {en.PackagesIn("{project}"), de.PackagesIn("{project}")},
		"bastion.title.versions":       {en.VersionsOf("{name}"), de.VersionsOf("{name}")},
		// The project view's title and the way's step (api-and-ui.md R-UI-273), which the application's pages say too.
		"bastion.title.repositories": {en.RepositoriesIn("{project}"), de.RepositoriesIn("{project}")},
		"route.repositories.title":   {en.Repositories, de.Repositories},
	}
	for key, w := range en.WellKnown {
		if properNames()[key] == w {
			// A proper name is the same word in both languages and has no unit.
			if de.WellKnown[key] != w {
				t.Errorf("the proper name %q of %s is %q in German", w, key, de.WellKnown[key])
			}
			continue
		}
		want["wellknown.repository."+key] = [2]string{w, de.WellKnown[key]}
	}
	for id, w := range want {
		got, ok := units[id]
		if !ok {
			t.Errorf("messages.de.xlf has no unit %s (the crawler text says %q | %q)", id, w[0], w[1])
			continue
		}
		if got != w {
			t.Errorf("unit %s: the application says %q | %q, the crawler text %q | %q", id, got[0], got[1], w[0], w[1])
		}
	}
}

// TestR_UI_272_ADescriptionIsWordedByKeyOnlyInTheProjectUpstream: the project upstream's description and a declared repository's
// name are the reader's words, a repository of the same key in another project and a key the product does not declare keep their
// stored text.
func TestR_UI_272_ADescriptionIsWordedByKeyOnlyInTheProjectUpstream(t *testing.T) {
	de := TextsOf("de")
	cases := []struct{ got, want string }{
		{de.ProjectDescription("upstream", "The public registries this installation offers to every project."), de.UpstreamDescription},
		{de.ProjectDescription("web", "Our web team"), "Our web team"},
		{de.RepositoryDescription("upstream", "npmjs", "npm public registry"), "Öffentliche npm-Registry"},
		{de.RepositoryDescription("upstream", "docker-hub", "Docker Hub"), "Docker Hub"},
		{de.RepositoryDescription("web", "npmjs", "Our npm mirror"), "Our npm mirror"},
		{de.RepositoryDescription("upstream", "retired-one", "A retired preset"), "A retired preset"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: %q, want %q", i, c.got, c.want)
		}
	}
}
