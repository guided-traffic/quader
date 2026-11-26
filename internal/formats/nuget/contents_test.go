package nuget

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/nuget/nupkg"
	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
)

func entries(paths ...string) []nupkg.Entry {
	out := make([]nupkg.Entry, len(paths))
	for i, p := range paths {
		out[i] = nupkg.Entry{Name: p, Path: p, Dir: strings.HasSuffix(p, "/")}
	}
	return out
}

// TestR_NUGET_038_TheFrameworksOfThePackagesFolders: the target frameworks are the framework folders of lib/, ref/, build/,
// buildTransitive/, tools/ and contentFiles/, each once, lower-cased and ordered .NET Framework, .NET Core, .NET, .NET Standard; a
// file at a folder's root, a folder of another name and a folder that is no framework name none.
func TestR_NUGET_038_TheFrameworksOfThePackagesFolders(t *testing.T) {
	c := nupkg.ContentsOf(entries(
		"Acme.nuspec", "lib/netstandard2.0/Acme.dll", "lib/net8.0/Acme.dll", "lib/NET462/Acme.dll", "lib/net462/_._", "ref/net8.0/Acme.dll",
		"build/Acme.targets", "buildTransitive/netcoreapp3.1/x.props", "tools/net8.0/any/acme.dll", "contentFiles/cs/net6.0/a.cs",
		"contentFiles/any/any/b.txt", "runtimes/win-x64/native/a.dll", "lib/x86/a.dll", "lib/", "analyzers/dotnet/cs/A.dll",
	), "acme")
	want := []string{"net462", "netcoreapp3.1", "net6.0", "net8.0", "netstandard2.0"}
	if !slices.Equal(c.Frameworks, want) {
		t.Errorf("frameworks %v, want %v", c.Frameworks, want)
	}
}

// TestR_NUGET_039_WhatRunsWhenAProjectUsesThePackage: the build logic is the <id>.props and <id>.targets MSBuild imports from
// build/, buildTransitive/ and buildMultiTargeting/ (and a framework folder of the first two), not another .targets beside them; the
// analyzers are the assemblies below analyzers/; the scripts are tools/init.ps1 and the install and uninstall scripts of tools/.
// Each list keeps 20 names and counts the rest.
func TestR_NUGET_039_WhatRunsWhenAProjectUsesThePackage(t *testing.T) {
	c := nupkg.ContentsOf(entries(
		"build/Acme.Widgets.props", "build/net8.0/acme.widgets.targets", "build/Other.targets", "buildTransitive/Acme.Widgets.targets",
		"buildMultiTargeting/Acme.Widgets.props", "buildMultiTargeting/net8.0/Acme.Widgets.props", "lib/net8.0/Acme.Widgets.targets",
		"analyzers/dotnet/cs/Acme.Analyzers.dll", "analyzers/dotnet/cs/de/Acme.Analyzers.resources.dll", "analyzers/readme.txt",
		"tools/init.ps1", "tools/net45/install.ps1", "tools/Uninstall.ps1", "tools/net45/init.ps1", "tools/helper.ps1", "content/install.ps1",
	), "acme.widgets")
	for name, got := range map[string][]string{"build": c.Build, "analyzers": c.Analyzers, "scripts": c.Scripts} {
		want := map[string][]string{
			"build":     {"build/Acme.Widgets.props", "build/net8.0/acme.widgets.targets", "buildTransitive/Acme.Widgets.targets", "buildMultiTargeting/Acme.Widgets.props"},
			"analyzers": {"analyzers/dotnet/cs/Acme.Analyzers.dll", "analyzers/dotnet/cs/de/Acme.Analyzers.resources.dll"},
			"scripts":   {"tools/init.ps1", "tools/net45/install.ps1", "tools/Uninstall.ps1"},
		}[name]
		if !slices.Equal(got, want) {
			t.Errorf("%s %v, want %v", name, got, want)
		}
	}
	var many []string
	for i := range 23 {
		many = append(many, "analyzers/dotnet/cs/A"+string(rune('a'+i))+".dll")
	}
	if c := nupkg.ContentsOf(entries(many...), "acme"); len(c.Analyzers) != 20 || c.More != 3 {
		t.Errorf("bounded: %d kept, %d more", len(c.Analyzers), c.More)
	}
}

// TestR_NUGET_038_TheIngestionStoresWhatTheFileListSays: a package read like a push stores its file list's facts in its metadata,
// and the view sends them with the licence file, the icon, the repository's branch and commit, the kind of the package and the
// frameworks of its dependency groups that its folders do not name.
func TestR_NUGET_038_TheIngestionStoresWhatTheFileListSays(t *testing.T) {
	b := buildNupkg(t, "Acme.Tool", "1.0.0", withType("DotnetTool"), withDep("Acme.Core", "1.0.0"),
		withMetadata(`<icon>icon.png</icon><license type="file">LICENSE.txt</license>`+
			`<repository type="git" url="https://git.example/acme/tool" branch="main" commit="0123abc"/>`),
		withEntry("tools/net8.0/any/Acme.Tool.dll", []byte("MZ")), withEntry("build/Acme.Tool.targets", []byte("<Project/>")),
		withEntry("icon.png", []byte("\x89PNG\r\n\x1a\n")), withEntry("LICENSE.txt", []byte("MIT")))
	res, err := nupkg.Inspect(context.Background(), bytes.NewReader(b), int64(len(b)), nupkg.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	in := &incoming{result: res, meta: res.Metadata, declaredID: "Acme.Tool", lowerID: "acme.tool"}
	raw, err := in.metadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	var stored rows.Metadata
	if err := json.Unmarshal(raw, &stored); err != nil || stored.Contents == nil {
		t.Fatalf("contents %s: %v", raw, err)
	}
	if !slices.Equal(stored.Contents.Frameworks, []string{"net8.0"}) || !slices.Equal(stored.Contents.Build, []string{"build/Acme.Tool.targets"}) {
		t.Errorf("contents %+v", stored.Contents)
	}

	st := seeded(t, string(raw), "1.0.0")
	vs := []formats.ViewVersion{{ID: 100, Package: "acme.widgets", Version: "1.0.0"}}
	det, err := New().View().Describe(context.Background(), ports{st}, vs, formats.ViewSummary|formats.ViewDetails, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := det[100].Metadata
	for k, want := range map[string]any{
		"licenseFile": "LICENSE.txt", "hasIcon": true, "repositoryBranch": "main", "repositoryCommit": "0123abc", "repositoryType": "git",
		"kind": KindTool,
	} {
		if md[k] != want {
			t.Errorf("metadata[%q] = %v, want %v", k, md[k], want)
		}
	}
	if fw, _ := md["frameworks"].([]string); !slices.Equal(fw, []string{"net8.0"}) {
		t.Errorf("frameworks %v", md["frameworks"])
	}
	// The flat dependency list is a group of every framework: no framework of its own to name beside the folders.
	if _, ok := md["dependencyFrameworks"]; ok {
		t.Errorf("dependencyFrameworks %v", md["dependencyFrameworks"])
	}
	if b, _ := md["buildFiles"].([]string); !slices.Equal(b, []string{"build/Acme.Tool.targets"}) {
		t.Errorf("buildFiles %v", md["buildFiles"])
	}
	if len(det[100].Licenses) != 0 {
		t.Errorf("a licence file is no licence expression: %v", det[100].Licenses)
	}
}

// TestR_NUGET_038_DependencyFrameworksBesideTheFolders: a dependency group's framework that no folder names is sent beside the
// folders' (a meta-package has no folders at all); one a folder names in its short form is not; a version stored before the file list
// was read sends no framework at all.
func TestR_NUGET_038_DependencyFrameworksBesideTheFolders(t *testing.T) {
	meta := `{"contents": {"frameworks": ["net462", "netstandard2.0"], "build": [], "analyzers": [], "scripts": []},
	  "dependencyGroups": [{"targetFramework": ".NETFramework4.6.2"}, {"targetFramework": ".NETStandard2.0"}, {"targetFramework": "net8.0"}]}`
	d, err := New().View().Describe(context.Background(), ports{seeded(t, meta, "1.0.0")}, []formats.ViewVersion{{ID: 100}}, formats.ViewDetails, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := d[100].Metadata["dependencyFrameworks"].([]string); !slices.Equal(got, []string{"net8.0"}) {
		t.Errorf("dependencyFrameworks %v", d[100].Metadata["dependencyFrameworks"])
	}
	old, _ := New().View().Describe(context.Background(), ports{seeded(t, `{"description": "x"}`, "1.0.0")}, []formats.ViewVersion{{ID: 100}}, formats.ViewDetails, nil)
	if _, ok := old[100].Metadata["frameworks"]; ok {
		t.Error("a version whose file list was never read names frameworks")
	}
}
