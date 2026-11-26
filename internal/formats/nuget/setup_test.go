package nuget

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
	"text/template"

	"github.com/guided-traffic/quader/internal/formats"
)

func renderSetup(t *testing.T, tpl formats.SetupTemplate, d formats.SetupData) string {
	t.Helper()
	id := func(v string) string { return v }
	tm, err := template.New(tpl.ID).Option("missingkey=error").Funcs(template.FuncMap{
		"shell": id, "powershell": id, "cmd": id, "xml": id, "schemeless": id,
	}).Parse(tpl.Template)
	if err != nil {
		t.Fatalf("%s: %v", tpl.ID, err)
	}
	var b bytes.Buffer
	if err := tm.Execute(&b, d); err != nil {
		t.Fatalf("%s: %v", tpl.ID, err)
	}
	return b.String()
}

func setupData(auth bool) formats.SetupData {
	return formats.SetupData{RegistryURL: "https://quader.example.com/api/nuget/nuget-all/", Repository: "nuget-all", Endpoint: "default", RequiresAuth: auth}
}

// refFor is the spelling of the token reference that the language of a template allows.
var refFor = map[string]string{
	"shell": formats.SetupTokenRef, "powershell": formats.SetupTokenRefPS, "cmd": formats.SetupTokenRefWin, "xml": formats.SetupTokenRefWin,
}

// TestR_NUGET_026_TheTemplatesSpellTheCredentialInTheirOwnSyntax: a template spells the token reference of
// its own language (a nuget.config and cmd %V%, a POSIX shell ${V}, PowerShell $env:V) and no other, only
// for a repository that is not anonymously readable, and a snippet without credential parts is a working
// command or file.
func TestR_NUGET_026_TheTemplatesSpellTheCredentialInTheirOwnSyntax(t *testing.T) {
	tpls := New().SetupTemplates()
	if len(tpls) == 0 {
		t.Fatal("no templates")
	}
	for _, tpl := range tpls {
		withAuth, without := renderSetup(t, tpl, setupData(true)), renderSetup(t, tpl, setupData(false))
		want := refFor[tpl.Lang]
		for _, ref := range formats.SetupTokenRefs() {
			if strings.Contains(withAuth, ref) != (ref == want && spellsCredential(tpl)) {
				t.Errorf("R-NUGET-026 %s (%s): the reference %s in\n%s", tpl.ID, tpl.Lang, ref, withAuth)
			}
		}
		if tpl.Section == "configure" {
			for _, ref := range formats.SetupTokenRefs() {
				if strings.Contains(without, ref) {
					t.Errorf("R-NUGET-026 %s: a repository open to anonymous readers still gets a credential:\n%s", tpl.ID, without)
				}
			}
		}
		for _, out := range []string{withAuth, without} {
			if strings.Contains(out, "<no value>") || strings.Contains(out, "{{") || strings.Contains(out, "v2") {
				t.Errorf("R-NUGET-026 %s: %q", tpl.ID, out)
			}
			if tpl.Section != "install" && !strings.Contains(out, "https://quader.example.com/api/nuget/nuget-all/v3/index.json") {
				t.Errorf("R-NUGET-026 %s: not the index.json address:\n%s", tpl.ID, out)
			}
		}
	}
}

func spellsCredential(tpl formats.SetupTemplate) bool { return tpl.Section != "install" }

// TestR_NUGET_026_TheNuGetConfigIsWellFormed: the nuget.config of the three clients is the one of the
// specification: <clear />, one source with protocolVersion 3, and the credentials of that source only
// for a repository that needs them, with the environment reference in ClearTextPassword.
func TestR_NUGET_026_TheNuGetConfigIsWellFormed(t *testing.T) {
	var files int
	for _, tpl := range New().SetupTemplates() {
		if tpl.File != "nuget.config" {
			continue
		}
		files++
		for _, auth := range []bool{true, false} {
			out := renderSetup(t, tpl, setupData(auth))
			if !strings.HasPrefix(out, "<?xml") {
				t.Errorf("%s: %q", tpl.ID, out)
			}
			var names []string
			dec := xml.NewDecoder(strings.NewReader(out))
			for {
				tok, err := dec.Token()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("%s auth=%v is not well-formed XML: %v\n%s", tpl.ID, auth, err, out)
				}
				if se, ok := tok.(xml.StartElement); ok {
					names = append(names, se.Name.Local)
				}
			}
			want := []string{"configuration", "packageSources", "clear", "add"}
			if auth {
				want = append(want, "packageSourceCredentials", "quader", "add", "add")
			}
			if strings.Join(names, " ") != strings.Join(want, " ") {
				t.Errorf("%s auth=%v: elements %v, want %v", tpl.ID, auth, names, want)
			}
			if strings.Contains(out, "ClearTextPassword") != auth {
				t.Errorf("%s auth=%v: ClearTextPassword present is %v", tpl.ID, auth, !auth)
			}
			if auth && !strings.Contains(out, `<add key="ClearTextPassword" value="%QUADER_TOKEN%" />`) {
				t.Errorf("%s: the password is not the environment reference:\n%s", tpl.ID, out)
			}
			if !strings.Contains(out, `protocolVersion="3"`) {
				t.Errorf("%s: no protocolVersion", tpl.ID)
			}
		}
	}
	if files != 3 {
		t.Errorf("%d nuget.config templates, want one per client", files)
	}
}

// TestR_NUGET_026_TheCommandsAreTheOnesOfTheSpecification: the dotnet and nuget.exe commands add the
// source with the credential and the clear-text switch the platform needs, and push with the token as the
// API key.
func TestR_NUGET_026_TheCommandsAreTheOnesOfTheSpecification(t *testing.T) {
	byID := map[string]formats.SetupTemplate{}
	for _, tpl := range New().SetupTemplates() {
		byID[tpl.ID] = tpl
	}
	const url = "https://quader.example.com/api/nuget/nuget-all/v3/index.json"
	for id, want := range map[string]string{
		"dotnet.shell.configure":      "dotnet nuget add source " + url + " --name quader \\\n  --username quader --password \"${QUADER_TOKEN}\" --store-password-in-clear-text\n",
		"dotnet.powershell.configure": "dotnet nuget add source " + url + " --name quader `\n  --username quader --password \"$env:QUADER_TOKEN\" --store-password-in-clear-text\n",
		"nuget.cmd.configure":         "nuget sources add -Name quader -Source " + url + " ^\n  -Username quader -Password %QUADER_TOKEN%\n",
		"nuget.powershell.configure":  "nuget sources add -Name quader -Source " + url + " `\n  -Username quader -Password \"$env:QUADER_TOKEN\"\n",
		"dotnet.shell.publish":        "dotnet nuget push bin/Release/Acme.Widgets.1.2.3.nupkg --source " + url + " --api-key \"${QUADER_TOKEN}\"\n",
		"dotnet.powershell.publish":   "dotnet nuget push bin/Release/Acme.Widgets.1.2.3.nupkg --source " + url + " --api-key \"$env:QUADER_TOKEN\"\n",
		"nuget.cmd.publish":           "nuget push Acme.Widgets.1.2.3.nupkg -Source " + url + " -ApiKey %QUADER_TOKEN%\n",
		"nuget.powershell.publish":    "nuget push Acme.Widgets.1.2.3.nupkg -Source " + url + " -ApiKey \"$env:QUADER_TOKEN\"\n",
	} {
		tpl, ok := byID[id]
		if !ok {
			t.Errorf("no template %s", id)
			continue
		}
		if got := renderSetup(t, tpl, setupData(true)); got != want {
			t.Errorf("R-NUGET-026 %s:\n%s\nwant\n%s", id, got, want)
		}
	}
	// Without a credential the command is complete on one line.
	if got := renderSetup(t, byID["dotnet.shell.configure"], setupData(false)); got != "dotnet nuget add source "+url+" --name quader\n" {
		t.Errorf("anonymous: %q", got)
	}
	if got := renderSetup(t, byID["nuget.cmd.configure"], setupData(false)); got != "nuget sources add -Name quader -Source "+url+"\n" {
		t.Errorf("anonymous nuget.exe: %q", got)
	}
}

// TestR_NUGET_037_TheInstallFormsOfEveryKind: the install forms of nuget.org by package type, each by its client in the order
// the panel offers them, written for Acme.Widgets 1.2.3 the way the API substitutes a template (a VersionSpec after the package,
// or the version a template places itself); the XML forms parse, and a kind's forms are the only forms of that kind.
func TestR_NUGET_037_TheInstallFormsOfEveryKind(t *testing.T) {
	want := map[string][]string{
		"": {
			"dotnet.install: dotnet add package Acme.Widgets --version 1.2.3",
			"dotnet.paket.install: paket add Acme.Widgets --version 1.2.3",
			`dotnet.script.install: #r "nuget: Acme.Widgets, 1.2.3"`,
			"dotnet.file-based-app.install: #:package Acme.Widgets@1.2.3",
			"dotnet.cake-addin.install: #addin nuget:?package=Acme.Widgets&version=1.2.3",
			"dotnet.cake-tool.install: #tool nuget:?package=Acme.Widgets&version=1.2.3",
			"nuget.install: nuget install Acme.Widgets -Version 1.2.3 -Source quader -OutputDirectory packages",
			`visual-studio.packagereference.install: <PackageReference Include="Acme.Widgets" Version="1.2.3" />`,
			`visual-studio.pmc.install: NuGet\Install-Package Acme.Widgets -Version 1.2.3`,
			`visual-studio.cpm.install: <PackageVersion Include="Acme.Widgets" Version="1.2.3" /> ` +
				`<!-- In the project file: <PackageReference Include="Acme.Widgets" /> -->`,
		},
		KindTool: {
			"dotnet.tool-global.install: dotnet tool install --global Acme.Widgets --version 1.2.3",
			"dotnet.tool-local.install: dotnet tool install --local Acme.Widgets --version 1.2.3",
			"dotnet.cake-dotnet-tool.install: #tool dotnet:?package=Acme.Widgets&version=1.2.3",
			"dotnet.nuke.install: nuke :add-package Acme.Widgets --version 1.2.3",
		},
		KindTemplate: {"dotnet.template.install: dotnet new install Acme.Widgets@1.2.3"},
		KindSDK: {
			"dotnet.sdk-file-based-app.install: #:sdk Acme.Widgets@1.2.3",
			`visual-studio.sdk.install: <Sdk Name="Acme.Widgets" Version="1.2.3" />`,
		},
	}
	got := map[string][]string{}
	for _, tpl := range New().SetupTemplates() {
		if tpl.Section != "install" {
			if tpl.VersionSpec != "" || tpl.Kind != "" {
				t.Errorf("%s: a VersionSpec or a kind on a %s template", tpl.ID, tpl.Section)
			}
			continue
		}
		spec := tpl.VersionSpec
		if spec == "" && !strings.Contains(tpl.Template, formats.SetupVersionRef) {
			spec = "@" + formats.SetupVersionRef // the API's default
		}
		out := strings.ReplaceAll(tpl.Template, formats.SetupPackageRef, "Acme.Widgets"+strings.ReplaceAll(spec, formats.SetupVersionRef, "1.2.3"))
		out = strings.ReplaceAll(out, formats.SetupVersionRef, "1.2.3")
		if tpl.Lang == "xml" {
			if err := xml.Unmarshal([]byte("<x>"+out+"</x>"), new(struct{})); err != nil {
				t.Errorf("%s: %q is no XML: %v", tpl.ID, out, err)
			}
		}
		got[tpl.Kind] = append(got[tpl.Kind], tpl.ID+": "+strings.Join(strings.Fields(out), " "))
	}
	for kind, forms := range want {
		if strings.Join(got[kind], "\n") != strings.Join(forms, "\n") {
			t.Errorf("R-NUGET-037 kind %q:\n%s\nwant\n%s", kind, strings.Join(got[kind], "\n"), strings.Join(forms, "\n"))
		}
	}
	if len(got) != len(want) {
		t.Errorf("kinds %v", got)
	}
}

// TestR_NUGET_026_EveryClientHasConfigureAndInstall: the clients are the ones of the specification, each
// with the sections the kit requires, and only the two command line clients push.
func TestR_NUGET_026_EveryClientHasConfigureAndInstall(t *testing.T) {
	f := New()
	sections := map[string]map[string]bool{}
	for _, tpl := range f.SetupTemplates() {
		if sections[tpl.Client] == nil {
			sections[tpl.Client] = map[string]bool{}
		}
		sections[tpl.Client][tpl.Section] = true
	}
	var ids []string
	for _, c := range f.Descriptor().Clients {
		ids = append(ids, c.ID)
		s := sections[c.ID]
		if !s["configure"] || !s["install"] || s["publish"] != (c.ID != ClientVisualStudio) {
			t.Errorf("client %s has the sections %v", c.ID, s)
		}
	}
	if strings.Join(ids, " ") != "dotnet nuget visual-studio" {
		t.Errorf("clients %v", ids)
	}
}
