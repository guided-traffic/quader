package v1

import (
	"regexp"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible"
	"github.com/guided-traffic/quader/internal/formats/clamav"
	"github.com/guided-traffic/quader/internal/formats/deb"
	"github.com/guided-traffic/quader/internal/formats/generic"
	"github.com/guided-traffic/quader/internal/formats/npm"
	"github.com/guided-traffic/quader/internal/formats/nuget"
	"github.com/guided-traffic/quader/internal/formats/oci"
)

// credentialLine is a line that holds the token's reference and keeps it off every program's arguments (Q-UI-1336): a variable the
// shell sets (export, set, a prefix of the command, a PowerShell variable), a builtin printf whose output is a client's standard input
// or a root-only file, a line of a configuration the client reads itself, or the header line of curl's or wget's configuration.
var credentialLine = regexp.MustCompile(`^\s*(export [A-Z_]+=|set [A-Z_]+=|[A-Z_]+="[^"]*" \\$|\$env:[A-Za-z_]+ = |\$[A-Za-z]+\.Authorization = |printf '[^']*' .*(\| |>>)|header = |PrivateMirror |.*_authToken=|\s*npmAuthToken: |\s*<add key="ClearTextPassword")`)

// argumentOnly are the commands whose client takes the credential only as an argument, which the page says beside them: dotnet's and
// nuget.exe's source password and API key (Q-UI-1336).
var argumentOnly = regexp.MustCompile(`(--password|-Password|--api-key|-ApiKey) `)

// TestR_API_030_NoSnippetPutsTheTokenIntoAnArgument: every client of every format, in the mode env, writes the token's reference only
// where no process list shows it, except the NuGet commands whose client has no other way, and every such command is NuGet's.
func TestR_API_030_NoSnippetPutsTheTokenIntoAnArgument(t *testing.T) {
	ref := "${QUADER_TOKEN}"
	for _, f := range []interface {
		SetupTemplates() []formats.SetupTemplate
	}{ansible.New(), clamav.Format{}, deb.New(), generic.Format{}, &npm.Format{}, &nuget.Format{}, &oci.Format{}} {
		tpls := f.SetupTemplates()
		seen := map[string]bool{}
		for _, tpl := range tpls {
			if seen[tpl.Client] {
				continue
			}
			seen[tpl.Client] = true
			ss, err := renderSnippets(setupInput{
				Templates: tpls, Client: tpl.Client, Credentials: credEnv, TokenEnvVar: "QUADER_TOKEN", MayPublish: true,
				Data: formats.SetupData{
					RegistryURL: "https://quader.example.net/x/", ImagePrefix: "quader.example.net/web/x", Repository: "x", Endpoint: "default",
					RequiresAuth: true, Username: "alice",
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range ss {
				for _, line := range strings.Split(s.Content, "\n") {
					if !strings.Contains(line, ref) && !strings.Contains(line, "%QUADER_TOKEN%") && !strings.Contains(line, "$env:QUADER_TOKEN") {
						continue
					}
					switch {
					case s.FileName != "" || s.Language == "ini" || s.Language == "xml" || s.Language == "yaml":
					case credentialLine.MatchString(line):
					case argumentOnly.MatchString(line) && strings.HasPrefix(s.ID, "dotnet.") || argumentOnly.MatchString(line) && strings.HasPrefix(s.ID, "nuget."):
					default:
						t.Errorf("%s puts the token into an argument: %q", s.ID, line)
					}
				}
			}
		}
	}
}
