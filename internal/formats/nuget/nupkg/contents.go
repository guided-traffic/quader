package nupkg

import (
	"regexp"
	"slices"
	"strings"

	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
)

// What the file list of a package says (R-NUGET-038, R-NUGET-039): the target frameworks its folders name, and the files that run code
// when a project uses it. Only the names of the central directory are read, the ones nupkg.Open checked (no path that climbs, no
// control character, a bounded count and length); no entry is inflated and nothing is unpacked.

// The bounds of what is kept of a file list: the names of each list, and the length of one name. A package past a bound keeps the
// first names in archive order and the count of the rest.
const (
	maxContentNames  = 20
	maxContentName   = 256
	maxFrameworkName = 64
)

// isFrameworkFolder says whether a lower-cased top-level folder names a target framework in its second segment (`lib/net8.0/…`): the
// folders NuGet selects by framework. contentFiles names its framework in the third segment (`contentFiles/cs/net8.0/…`).
func isFrameworkFolder(top string) bool {
	switch top {
	case "lib", "ref", "build", "buildtransitive", "tools":
		return true
	}
	return false
}

// tfmShape is the shape of a target framework folder: a known family followed by a version or a profile (`net8.0`, `net462`,
// `netstandard2.0`, `netcoreapp3.1`, `portable-net45+win8`, `uap10.0`, `monoandroid10`, `net8.0-windows10.0.19041`). A folder of
// another name (`any`, `native`, `x86`, a file at the folder's root) names no framework.
var tfmShape = regexp.MustCompile(`^(net|netstandard|netcoreapp|netcore|netmf|netnano|uap|win|wp|wpa|sl|silverlight|monoandroid|monotouch|monomac|xamarin|tizen|portable|dnx|dnxcore|aspnet|aspnetcore)[a-z0-9.+\-]*$`)

// ContentsOf reads the file list of a package with the ID lowerID: the frameworks of its folders, and the files that run code when a
// project uses it (R-NUGET-039):
//
//   - build logic: `<id>.props` and `<id>.targets` in `build/`, `buildTransitive/` and `buildMultiTargeting/` and in a framework
//     folder of the first two, the files MSBuild imports into every build of a project that references the package;
//   - analyzers: every `.dll` below `analyzers/`, the analyzers and source generators the compiler loads;
//   - PowerShell scripts: `tools/init.ps1`, and `install.ps1` and `uninstall.ps1` in `tools/` or a framework folder of it, which
//     Visual Studio runs for a project that uses packages.config.
func ContentsOf(entries []Entry, lowerID string) *rows.Contents {
	c := &rows.Contents{Frameworks: []string{}, Build: []string{}, Analyzers: []string{}, Scripts: []string{}}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Dir {
			continue
		}
		segs := strings.Split(e.Path, "/")
		lower := strings.ToLower(e.Path)
		if fw := frameworkOf(segs); fw != "" && !seen[fw] {
			seen[fw] = true
			c.Frameworks = append(c.Frameworks, fw)
		}
		switch {
		case isBuildLogic(strings.Split(lower, "/"), lowerID):
			c.Build, c.More = keep(c.Build, e.Path, c.More)
		case len(segs) > 1 && strings.EqualFold(segs[0], "analyzers") && strings.HasSuffix(lower, ".dll"):
			c.Analyzers, c.More = keep(c.Analyzers, e.Path, c.More)
		case isScript(strings.Split(lower, "/")):
			c.Scripts, c.More = keep(c.Scripts, e.Path, c.More)
		}
	}
	if len(c.Frameworks) > maxContentNames {
		c.More += len(c.Frameworks) - maxContentNames
		c.Frameworks = c.Frameworks[:maxContentNames]
	}
	slices.SortStableFunc(c.Frameworks, compareFrameworks)
	return c
}

// keep appends a name to a list below its bound, and counts it in more past the bound or past the length of a name.
func keep(list []string, name string, more int) ([]string, int) {
	if len(list) >= maxContentNames || len(name) > maxContentName {
		return list, more + 1
	}
	return append(list, name), more
}

// frameworkOf is the target framework a file's folder names, lower-cased; "" for none.
func frameworkOf(segs []string) string {
	var fw string
	switch top := strings.ToLower(segs[0]); {
	case top == "contentfiles" && len(segs) > 3:
		fw = segs[2]
	case isFrameworkFolder(top) && len(segs) > 2:
		fw = segs[1]
	default:
		return ""
	}
	fw = strings.ToLower(fw)
	if len(fw) > maxFrameworkName || !tfmShape.MatchString(fw) {
		return ""
	}
	return fw
}

// isBuildLogic says whether a lower-cased path is a file MSBuild imports into the build of a project that references the package.
func isBuildLogic(segs []string, lowerID string) bool {
	name := segs[len(segs)-1]
	if name != lowerID+".props" && name != lowerID+".targets" {
		return false
	}
	switch {
	case len(segs) == 2:
		return segs[0] == "build" || segs[0] == "buildtransitive" || segs[0] == "buildmultitargeting"
	case len(segs) == 3:
		return (segs[0] == "build" || segs[0] == "buildtransitive") && tfmShape.MatchString(segs[1])
	}
	return false
}

// isScript says whether a lower-cased path is a PowerShell script Visual Studio runs for a packages.config project.
func isScript(segs []string) bool {
	if segs[0] != "tools" {
		return false
	}
	switch name := segs[len(segs)-1]; {
	case len(segs) == 2:
		return name == "init.ps1" || name == "install.ps1" || name == "uninstall.ps1"
	case len(segs) == 3:
		return (name == "install.ps1" || name == "uninstall.ps1") && tfmShape.MatchString(segs[1])
	}
	return false
}

// compareFrameworks orders frameworks by family and then by their spelling, so that net462 stands before net8.0 within .NET
// Framework and .NET, and .NET Standard after both.
func compareFrameworks(a, b string) int {
	if c := frameworkRank(a) - frameworkRank(b); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func frameworkRank(fw string) int {
	switch {
	case strings.HasPrefix(fw, "netstandard"):
		return 3
	case strings.HasPrefix(fw, "netcoreapp"):
		return 1
	case strings.HasPrefix(fw, "net") && len(fw) > 3 && fw[3] >= '0' && fw[3] <= '9' && strings.Contains(fw, "."):
		return 2 // .NET 5 and later: net5.0, net8.0-windows
	case strings.HasPrefix(fw, "net") && len(fw) > 3 && fw[3] >= '0' && fw[3] <= '9':
		return 0 // .NET Framework: net45, net462
	}
	return 4
}

// ShortFramework turns the target framework of a dependency group into the folder name NuGet gives it (`.NETStandard2.0` is
// netstandard2.0, `.NETFramework4.6.2` is net462, `net8.0` stays), so that the frameworks of the groups and those of the folders compare.
// A spelling it does not know is returned lower-cased.
func ShortFramework(tfm string) string {
	t := strings.ToLower(strings.TrimSpace(tfm))
	for long, short := range map[string]string{".netstandard": "netstandard", ".netcoreapp": "netcoreapp"} {
		if rest, ok := strings.CutPrefix(t, long); ok {
			return short + strings.TrimPrefix(rest, ",version=v")
		}
	}
	if rest, ok := strings.CutPrefix(t, ".netframework"); ok {
		rest = strings.TrimPrefix(rest, ",version=v")
		return "net" + strings.ReplaceAll(rest, ".", "")
	}
	return t
}
