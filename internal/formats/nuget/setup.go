package nuget

import "github.com/guided-traffic/quader/internal/formats"

// clients are the native clients of the acceptance matrix (R-NUGET-026, R-NUGET-027): the
// .NET SDK, nuget.exe and Visual Studio. Visual Studio reads the same nuget.config as the
// other two and has no command line, so its templates are the configuration file and the
// PackageReference of a project.
func clients() []formats.Client {
	return []formats.Client{
		{ID: ClientDotnet, DisplayName: ".NET CLI (dotnet)", Versions: []string{"8.0", "9.0", "10.0"}},
		{ID: ClientNuGet, DisplayName: "nuget.exe", Versions: []string{"6.8"}},
		{ID: ClientVisualStudio, DisplayName: "Visual Studio 2022", Versions: []string{"17.8"}},
	}
}

// The client IDs.
const (
	ClientDotnet       = "dotnet"
	ClientNuGet        = "nuget"
	ClientVisualStudio = "visual-studio"
)

// sourceURL is the expression for the address of the service index of the repository the snippet is rendered
// for: RegistryURL has its trailing slash (R-NUGET-026: the instructions always use the
// index.json URL, never a v2 address).
const sourceURL = `(printf "%sv3/index.json" .RegistryURL)`

// SetupTemplates are the client setup snippets of R-NUGET-026 and R-UI-018 (Q-NUGET-49):
// how to point a client at the repository (configure), how to install a package through it
// (install) and how to push to it (publish; the API renders it for a caller who holds deploy on
// the publish target). A publish snippet names the repository the caller chose, not the
// deployment target of a virtual one: the virtual repository forwards the push. The dialog that
// shows them and the token it mints are the API's (R-API-033); a template sees
// [formats.SetupData] and never a token, so the credential is always a reference to the
// environment variable that the client or the shell expands itself, in the spelling of the
// syntax it is written in: %QUADER_TOKEN% in a nuget.config and in cmd, ${QUADER_TOKEN} in a
// POSIX shell, $env:QUADER_TOKEN in PowerShell.
//
// The credential of a configure snippet is written only for a repository that is not
// anonymously readable (SetupData.RequiresAuth): a client with a credential for a source that
// needs none would send it to the first request of every restore for nothing, and the API
// clears RequiresAuth for a snippet without credentials, so the whole credential part goes and
// what is left is a command or a file that works. A push carries its token as the API key,
// which is the credential of the publish resource (R-NUGET-012).
func (f *Format) SetupTemplates() []formats.SetupTemplate {
	// The server never sees the user name (R-NUGET-012: any name is accepted); the instructions
	// use a fixed one.
	const user = "quader"
	nugetConfig := `<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
    <add key="quader" value="{{xml ` + sourceURL + `}}" protocolVersion="3" />
  </packageSources>
{{if .RequiresAuth}}  <packageSourceCredentials>
    <quader>
      <add key="Username" value="` + user + `" />
      <add key="ClearTextPassword" value="` + formats.SetupTokenRefWin + `" />
    </quader>
  </packageSourceCredentials>
{{end}}</configuration>
`
	dotnetShell := `dotnet nuget add source {{shell ` + sourceURL + `}} --name quader{{if .RequiresAuth}} \
  --username ` + user + ` --password "` + formats.SetupTokenRef + `" --store-password-in-clear-text{{end}}
`
	dotnetPS := "dotnet nuget add source {{powershell " + sourceURL + "}} --name quader{{if .RequiresAuth}} `\n" +
		"  --username " + user + ` --password "` + formats.SetupTokenRefPS + `" --store-password-in-clear-text{{end}}` + "\n"
	nugetCmd := `nuget sources add -Name quader -Source {{cmd ` + sourceURL + `}}{{if .RequiresAuth}} ^
  -Username ` + user + ` -Password ` + formats.SetupTokenRefWin + `{{end}}
`
	nugetPS := "nuget sources add -Name quader -Source {{powershell " + sourceURL + "}}{{if .RequiresAuth}} `\n" +
		"  -Username " + user + ` -Password "` + formats.SetupTokenRefPS + `"{{end}}` + "\n"
	// The package file of a push is the reader's (R-API-030).
	const dotnetPackage = "bin/Release/Acme.Widgets.1.2.3.nupkg"
	const nugetPackage = "Acme.Widgets.1.2.3.nupkg"
	const dotnetPush = "dotnet nuget push " + dotnetPackage + " --source "
	const nugetPush = "nuget push " + nugetPackage + " -Source "
	return []formats.SetupTemplate{
		{ID: "dotnet.shell.configure", Client: ClientDotnet, Section: "configure", Lang: "shell", Template: dotnetShell},
		{ID: "dotnet.powershell.configure", Client: ClientDotnet, Section: "configure", Lang: "powershell", Template: dotnetPS},
		{ID: "dotnet.nugetconfig.configure", Client: ClientDotnet, Section: "configure", Lang: "xml", File: "nuget.config", Template: nugetConfig},
		{ID: "nuget.cmd.configure", Client: ClientNuGet, Section: "configure", Lang: "cmd", Template: nugetCmd},
		{ID: "nuget.powershell.configure", Client: ClientNuGet, Section: "configure", Lang: "powershell", Template: nugetPS},
		{ID: "nuget.nugetconfig.configure", Client: ClientNuGet, Section: "configure", Lang: "xml", File: "nuget.config", Template: nugetConfig},
		{ID: "visual-studio.nugetconfig.configure", Client: ClientVisualStudio, Section: "configure", Lang: "xml", File: "nuget.config", Template: nugetConfig},

		// The install forms of R-NUGET-037, nuget.org's, by the client that runs them, the first of a client and a kind its default.
		// A library (no kind): the .NET CLI, then Paket, a script or a notebook, a file-based app and a Cake script, all run beside the
		// .NET SDK (`dotnet paket`, `dotnet fsi`, `dotnet run app.cs`, `dotnet cake`); Visual Studio's project file, its Package
		// Manager console and Central Package Management; nuget.exe's download.
		{ID: "dotnet.install", Client: ClientDotnet, Section: "install", Lang: "shell", Template: "dotnet add package <package>\n", VersionSpec: " --version " + formats.SetupVersionRef},
		{ID: "dotnet.paket.install", Client: ClientDotnet, Section: "install", Lang: "shell", Template: "paket add <package>\n", VersionSpec: " --version " + formats.SetupVersionRef},
		{ID: "dotnet.script.install", Client: ClientDotnet, Section: "install", Lang: "text", Template: "#r \"nuget: <package>, <version>\"\n"},
		{ID: "dotnet.file-based-app.install", Client: ClientDotnet, Section: "install", Lang: "text", Template: "#:package <package>@<version>\n"},
		{ID: "dotnet.cake-addin.install", Client: ClientDotnet, Section: "install", Lang: "text", Template: "#addin nuget:?package=<package>&version=<version>\n"},
		{ID: "dotnet.cake-tool.install", Client: ClientDotnet, Section: "install", Lang: "text", Template: "#tool nuget:?package=<package>&version=<version>\n"},
		{ID: "nuget.install", Client: ClientNuGet, Section: "install", Lang: "cmd", Template: "nuget install <package> -Source quader -OutputDirectory packages\n", VersionSpec: " -Version " + formats.SetupVersionRef},
		// The version attribute closes the Include attribute: the API replaces <package> by the
		// quoted package followed by the spec, so the spec starts with the closing quote.
		{ID: "visual-studio.packagereference.install", Client: ClientVisualStudio, Section: "install", Lang: "xml", Template: "<PackageReference Include=\"<package>\" />\n", VersionSpec: "\" Version=\"" + formats.SetupVersionRef},
		{ID: "visual-studio.pmc.install", Client: ClientVisualStudio, Section: "install", Lang: "powershell", Template: "NuGet\\Install-Package <package>\n", VersionSpec: " -Version " + formats.SetupVersionRef},
		// Central Package Management: the version in Directory.Packages.props, the reference without one in the project file, which the
		// second line names as a comment (one snippet is one file's).
		{
			ID: "visual-studio.cpm.install", Client: ClientVisualStudio, Section: "install", Lang: "xml", File: "Directory.Packages.props",
			Template: "<PackageVersion Include=\"<package>\" Version=\"<version>\" />\n" +
				"<!-- In the project file: <PackageReference Include=\"<package>\" /> -->\n",
		},
		// A .NET tool: installed for the user or into the repository's tool manifest, or named in a Cake or a NUKE build. A
		// PackageReference to it is refused by the SDK (NU1212), so Visual Studio and nuget.exe offer no form of it.
		{ID: "dotnet.tool-global.install", Client: ClientDotnet, Section: "install", Lang: "shell", Kind: KindTool, Template: "dotnet tool install --global <package>\n", VersionSpec: " --version " + formats.SetupVersionRef},
		{ID: "dotnet.tool-local.install", Client: ClientDotnet, Section: "install", Lang: "shell", Kind: KindTool, Template: "dotnet tool install --local <package>\n", VersionSpec: " --version " + formats.SetupVersionRef},
		{ID: "dotnet.cake-dotnet-tool.install", Client: ClientDotnet, Section: "install", Lang: "text", Kind: KindTool, Template: "#tool dotnet:?package=<package>&version=<version>\n"},
		{ID: "dotnet.nuke.install", Client: ClientDotnet, Section: "install", Lang: "shell", Kind: KindTool, Template: "nuke :add-package <package>\n", VersionSpec: " --version " + formats.SetupVersionRef},
		// A template package: dotnet new installs it.
		{ID: "dotnet.template.install", Client: ClientDotnet, Section: "install", Lang: "shell", Kind: KindTemplate, Template: "dotnet new install <package>\n"},
		// An MSBuild project SDK: named in the project file, or in a file-based app.
		{ID: "dotnet.sdk-file-based-app.install", Client: ClientDotnet, Section: "install", Lang: "text", Kind: KindSDK, Template: "#:sdk <package>@<version>\n"},
		{ID: "visual-studio.sdk.install", Client: ClientVisualStudio, Section: "install", Lang: "xml", Kind: KindSDK, Template: "<Sdk Name=\"<package>\" Version=\"<version>\" />\n"},

		{
			ID: "dotnet.shell.publish", Client: ClientDotnet, Section: "publish", Lang: "shell", Examples: []string{dotnetPackage},
			Template: dotnetPush + `{{shell ` + sourceURL + `}} --api-key "` + formats.SetupTokenRef + `"` + "\n",
		},
		{
			ID: "dotnet.powershell.publish", Client: ClientDotnet, Section: "publish", Lang: "powershell", Examples: []string{dotnetPackage},
			Template: dotnetPush + `{{powershell ` + sourceURL + `}} --api-key "` + formats.SetupTokenRefPS + `"` + "\n",
		},
		{
			ID: "nuget.cmd.publish", Client: ClientNuGet, Section: "publish", Lang: "cmd", Examples: []string{nugetPackage},
			Template: nugetPush + `{{cmd ` + sourceURL + `}} -ApiKey ` + formats.SetupTokenRefWin + "\n",
		},
		{
			ID: "nuget.powershell.publish", Client: ClientNuGet, Section: "publish", Lang: "powershell", Examples: []string{nugetPackage},
			Template: nugetPush + `{{powershell ` + sourceURL + `}} -ApiKey "` + formats.SetupTokenRefPS + `"` + "\n",
		},
	}
}
