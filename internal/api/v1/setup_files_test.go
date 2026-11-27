package v1

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
)

// hostileNames are file names a person can upload to a generic repository and that a shell or PowerShell would read as more than
// one argument, an expansion, an option or standard output, if a snippet did not quote them (R-GENERIC-017).
var hostileNames = []string{
	"a b.txt", "it's.txt", `say "hi".txt`, "$(id).txt", "`id`.txt", "${HOME}", "x;rm -rf ~;y", "a|b&c>d", "!history",
	"-rf", "-", "--output=x", "*.bin", "ä ö.bin", "%41.bin", "1kb", "@splat", "tab\u00a0name", "‘curly’.txt",
}

// fileSetup asks the client setup of the generic repository it/files for a path.
func fileSetup(t *testing.T, client, path, sha string) map[string]gen.ClientSnippet {
	t.Helper()
	q := url.Values{"client": {client}, "credentials": {"none"}, "path": {path}}
	if sha != "" {
		q.Set("sha256", sha)
	}
	w := get(setupServer(t, "https://quader.example.com", true), "/api/v1/projects/it/repositories/files/client-setup?"+q.Encode())
	var out gen.ClientSetup
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("%s %q: %d %s", client, path, w.Code, w.Body.String())
	}
	got := map[string]gen.ClientSnippet{}
	for _, sn := range out.Snippets {
		got[sn.Id] = sn
	}
	return got
}

// runShell runs a snippet in a POSIX shell in which the client functions and sha256sum print their arguments and standard input,
// and returns what each printed: the arguments, one per element, and the input as the last.
func runShell(t *testing.T, snippet string) []string {
	t.Helper()
	const mocks = `quader_curl() { printf '%s\0' "$@"; }
quader_wget() { printf '%s\0' "$@"; }
sha256sum() { printf '%s\0' "$@"; cat; }
`
	cmd := exec.Command("sh", "-c", mocks+snippet)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("sh: %v %s\n%s", err, stderr.String(), snippet)
	}
	return strings.Split(out.String(), "\x00")
}

// TestR_GENERIC_017_AHostileNameIsOneArgumentOfEveryClient: a file's name with spaces, quotes, an expansion, a separator, a
// leading dash, a glob or a non-ASCII character comes out of the download and the check of curl and wget as one argument, run by
// a real POSIX shell, and out of PowerShell's as one single-quoted literal; the address carries the path percent-encoded with
// unreserved characters only, and names the same file.
func TestR_GENERIC_017_AHostileNameIsOneArgumentOfEveryClient(t *testing.T) {
	const sha = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	for _, name := range hostileNames {
		path := "dir with space/" + name
		saved := name
		if strings.HasPrefix(name, "-") {
			saved = "./" + name
		}
		addr := "https://quader.example.com/files/it/files/" + escapeFilePath(path)
		if strings.Trim(strings.TrimPrefix(addr, "https://"), "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~/%") != "" {
			t.Errorf("%q: the address %s holds a character a shell could read", name, addr)
		}
		if dec, err := url.PathUnescape(strings.TrimPrefix(addr, "https://quader.example.com/files/it/files/")); err != nil || dec != path {
			t.Errorf("%q: the address decodes to %q, %v", name, dec, err)
		}

		curl := fileSetup(t, "curl", path, sha)
		if got := runShell(t, curl["curl.install"].Content); !slices.Equal(got, []string{"--fail", "--location", "--output", saved, addr, ""}) {
			t.Errorf("curl %q: %q", name, got)
		}
		wget := fileSetup(t, "wget", path, sha)
		if got := runShell(t, wget["wget.install"].Content); !slices.Equal(got, []string{"--output-document", saved, addr, ""}) {
			t.Errorf("wget %q: %q", name, got)
		}
		for _, id := range []string{"curl.install.checksum", "wget.install.checksum"} {
			sn := curl[id]
			if id == "wget.install.checksum" {
				sn = wget[id]
			}
			if got := runShell(t, sn.Content); !slices.Equal(got, []string{"-c", "-", sha + "  " + saved + "\n"}) {
				t.Errorf("%s %q: %q", id, name, got)
			}
		}

		ps := fileSetup(t, "powershell", path, sha)
		literal := "'" + strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’").Replace(saved) + "'"
		if got, want := ps["powershell.install"].Content, `Invoke-WebRequest -Uri "`+addr+`" -OutFile `+literal+" -Headers $QuaderHeaders\n"; got != want {
			t.Errorf("powershell %q:\n%s\nwant\n%s", name, got, want)
		}
		if got, want := ps["powershell.install.checksum"].Content, "(Get-FileHash -Algorithm SHA256 -LiteralPath "+literal+").Hash -eq '"+sha+"'\n"; got != want {
			t.Errorf("powershell check %q:\n%s", name, got)
		}
		for _, sn := range []gen.ClientSnippet{curl["curl.install"], wget["wget.install"], ps["powershell.install"]} {
			if len(sn.Marks) != 0 {
				t.Errorf("%s %q: a filled command marks %+v", sn.Id, name, sn.Marks)
			}
		}
	}
}

// TestR_GENERIC_017_AFoldersUploadCommands: a folder's path answers the publish snippets of every client, uploading the reader's
// <file> into the folder, and no download; the root's address is the repository's.
func TestR_GENERIC_017_AFoldersUploadCommands(t *testing.T) {
	for path, folder := range map[string]string{"releases/2.4 rc/": "releases/2.4%20rc/", "/": ""} {
		want := map[string]string{
			"curl.publish":       `quader_curl --fail --upload-file <file> "https://quader.example.com/files/it/files/` + folder + `<file>"` + "\n",
			"wget.publish":       `quader_wget --method PUT --body-file <file> --output-document - "https://quader.example.com/files/it/files/` + folder + `<file>"` + "\n",
			"powershell.publish": `Invoke-WebRequest -Method Put -InFile <file> -Uri "https://quader.example.com/files/it/files/` + folder + `<file>" -Headers $QuaderHeaders` + "\n",
		}
		for _, client := range []string{"curl", "wget", "powershell"} {
			got := fileSetup(t, client, path, "")
			for id, sn := range got {
				if sn.Tab == gen.ClientSnippetTabInstall {
					t.Errorf("%s %q: a folder has the download %s", client, path, id)
				}
			}
			id := client + ".publish"
			if sn, ok := got[id]; !ok || sn.Content != want[id] || !slices.ContainsFunc(sn.Marks, func(m gen.SnippetMark) bool { return m.Text == "<file>" }) {
				t.Errorf("%s %q: %+v", client, path, sn)
			}
		}
	}
}

// TestR_GENERIC_017_WgetAndPowerShellKeepTheTokenOffTheCommandLine (Q-GENERIC-10, Q-GENERIC-13): the configure snippets of wget and
// PowerShell hold the token's reference in the one line that is dropped for credentials=none, written in the client's syntax for env
// and as the placeholder for the browser; wget reads it from a configuration file of its own, PowerShell keeps it in a variable of the
// session, and neither puts it on a command line.
func TestR_GENERIC_017_WgetAndPowerShellKeepTheTokenOffTheCommandLine(t *testing.T) {
	wget := wgetFunction
	ps := func(line string) string { return "$QuaderHeaders = @{}\n" + line }
	for credentials, want := range map[string]map[string]string{
		"env": {
			"wget.configure.function":      wget("header = Authorization: Bearer ${MY_TOKEN}\n"),
			"powershell.configure.headers": ps("$QuaderHeaders.Authorization = \"Bearer $env:MY_TOKEN\"\n"),
		},
		"placeholder": {
			"wget.configure.function":      wget("header = Authorization: Bearer {{token}}\n"),
			"powershell.configure.headers": ps("$QuaderHeaders.Authorization = \"Bearer {{token}}\"\n"),
		},
		"none": {"wget.configure.function": wget(""), "powershell.configure.headers": ps("")},
	} {
		for _, client := range []string{"wget", "powershell"} {
			w := get(setupServer(t, "https://quader.example.com", true), "/api/v1/projects/it/repositories/files/client-setup?client="+client+"&credentials="+credentials)
			var out gen.ClientSetup
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
				t.Fatalf("%s %s: %d %s", client, credentials, w.Code, w.Body.String())
			}
			var ids []string
			for _, sn := range out.Snippets {
				ids = append(ids, sn.Id)
				if sn.Tab == gen.ClientSnippetTabConfigure && sn.Content != want[sn.Id] {
					t.Errorf("%s %s:\n%s\nwant\n%s", sn.Id, credentials, sn.Content, want[sn.Id])
				}
			}
			if wantIDs := []string{client + ".configure." + map[string]string{"wget": "function", "powershell": "headers"}[client], client + ".install", client + ".install.checksum", client + ".publish"}; !slices.Equal(ids, wantIDs) {
				t.Errorf("%s: %v", client, ids)
			}
		}
	}
}

// wgetFunction is wget's configure snippet with the header line given ("" for credentials=none).
func wgetFunction(header string) string {
	return "quader_wget() (\n  config=$(mktemp) || exit\n  trap 'rm -f \"$config\"' EXIT\n  cat > \"$config\" <<EOF\n" + header +
		"EOF\n  wget --no-verbose --config=\"$config\" \"$@\"\n)\n"
}

// wgetMock stands for wget in TestR_GENERIC_017_WgetReadsTheHeaderFromAFileOfItsOwnThatTheCallRemoves: it prints the kind and the mode
// of the file --config names (never a device such as /dev/stdin, whose here-document wget refuses), its content, writes its name to
// $WHERE, and fails as wget does for the address "fail".
const wgetMock = `wget() {
  for a; do case $a in --config=*) c=${a#--config=} ;; esac; done
  case $c in /dev/* | /proc/*) echo "a device: $c" ;; esac
  if [ -f "$c" ] && [ ! -L "$c" ]; then echo "regular $(ls -l "$c" | cut -c1-10)"; else echo "not a file: $c"; fi
  cat "$c"
  echo "$c" > "$WHERE"
  case $* in *fail*) return 8 ;; esac
}
`

// TestR_GENERIC_017_WgetReadsTheHeaderFromAFileOfItsOwnThatTheCallRemoves (Q-GENERIC-13): run by a real POSIX shell with wget replaced
// by a function, the configure snippet hands wget a regular file of mode 0600, not standard input, that holds the header with the
// token's value; the file is gone when the call returns, also when wget fails, and wget's status is the function's; a shell trace
// (set -x) of the call never shows the token's value.
func TestR_GENERIC_017_WgetReadsTheHeaderFromAFileOfItsOwnThatTheCallRemoves(t *testing.T) {
	w := get(setupServer(t, "https://quader.example.com", true), "/api/v1/projects/it/repositories/files/client-setup?client=wget&credentials=env")
	var out gen.ClientSetup
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var function string
	for _, sn := range out.Snippets {
		if sn.Id == "wget.configure.function" {
			function = sn.Content
		}
	}
	for address, status := range map[string]string{"https://quader.example.com/files/it/files/a.bin": "0", "fail": "8"} {
		dir := t.TempDir()
		script := wgetMock + function + "set -x\nquader_wget --output-document - " + address + "\necho \"status $?\"\nset +x\n" +
			`[ -e "$(cat "$WHERE")" ] && echo "left: $(cat "$WHERE")"` + "\nexit 0\n"
		cmd := exec.Command("sh", "-c", script)
		cmd.Env = append(cmd.Environ(), "MY_TOKEN=tok-value-1", "WHERE="+dir+"/where") // the server's variable
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("sh: %v %s", err, stderr.String())
		}
		if got, want := stdout.String(), "regular -rw-------\nheader = Authorization: Bearer tok-value-1\nstatus "+status+"\n"; got != want {
			t.Errorf("%s:\n%s\nwant\n%s", address, got, want)
		}
		if strings.Contains(stderr.String(), "tok-value-1") {
			t.Errorf("%s: the shell trace shows the token:\n%s", address, stderr.String())
		}
	}
}

// TestR_GENERIC_017_AFilesPathIsHeldToThePathRules: a path the /files/ rules refuse, a path of a format named by packages, a path
// beside a package, and a SHA-256 that is none or names a folder answer 400.
func TestR_GENERIC_017_AFilesPathIsHeldToThePathRules(t *testing.T) {
	h := setupServer(t, "https://quader.example.com", true)
	sha := strings.Repeat("a", 64)
	for q, repo := range map[string]string{
		"path=a/../b": "files", "path=a//b": "files", "path=" + url.QueryEscape("a\nb"): "files", "path=" + url.QueryEscape(`a\b`): "files",
		"path=": "files", "path=" + strings.Repeat("x", 300): "files", "path=a/qdr_pat_abcdefgh": "files",
		"path=a&package=x": "files", "sha256=" + sha: "files", "path=a/&sha256=" + sha: "files", "path=a&sha256=xyz": "files",
	} {
		if w := get(h, "/api/v1/projects/it/repositories/"+repo+"/client-setup?client=curl&"+q); w.Code != 400 {
			t.Errorf("%s: %d %s", q, w.Code, w.Body.String())
		}
	}
	if w := get(h, "/api/v1/projects/it/repositories/npm-local/client-setup?client=npm&path=a"); w.Code != 400 || !strings.Contains(w.Body.String(), `\"path\"`) {
		t.Errorf("npm with a path: %d %s", w.Code, w.Body.String())
	}
	// A literal "%" is a character of the name, not an escape.
	if got := fileSetup(t, "curl", "a%2Fb", "")["curl.install"].Content; !strings.Contains(got, `/files/it/files/a%252Fb"`) || !strings.Contains(got, "--output 'a%2Fb' ") {
		t.Errorf("%q", got)
	}
}
