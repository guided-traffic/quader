package generic

import "github.com/guided-traffic/quader/internal/formats"

// The clients of the format (Q-GENERIC-8, Q-GENERIC-10): the three a person downloads and uploads a file with on a
// POSIX shell and on Windows.
const (
	ClientCurl       = "curl"
	ClientWget       = "wget"
	ClientPowerShell = "powershell"
)

// CurlFunction is the name of the shell function the configure snippet of curl defines and
// its other snippets and the guide call.
const CurlFunction = "quader_curl"

// WgetFunction is the name of the shell function the configure snippet of wget defines.
const WgetFunction = "quader_wget"

// PowerShellHeaders is the variable the configure snippet of PowerShell fills with the header that signs a request in.
const PowerShellHeaders = "$QuaderHeaders"

// checksum is the line of a POSIX shell that checks a download against its SHA-256 (R-GENERIC-017): printf writes the line
// sha256sum reads, each value its own quoted word. The "-" names standard input: GNU's and BusyBox's sha256sum read it without, the
// one of macOS only prints its usage (Q-GENERIC-13).
const checksum = `printf '%s  %s\n' ` + formats.SetupSHA256Ref + ` ` + formats.SetupFileNameRef + ` | sha256sum -c -` + "\n"

// address is a file's address in a template: the repository's registry URL and the path, double-quoted. The API puts the
// path in percent-encoded with nothing but unreserved characters, so nothing in the quotes is read by a shell (R-GENERIC-017).
const address = `"{{.RegistryURL}}` + formats.SetupPathRef + `"`

// SetupTemplates are the client setup snippets of R-GENERIC-016 and R-GENERIC-017 for curl and wget in a POSIX shell and for
// PowerShell's Invoke-WebRequest. Each keeps the token off the command line (Q-GENERIC-9, Q-GENERIC-10): curl through a shell
// function that hands it the header as configuration on standard input; wget through a shell function that writes the header into
// a configuration file of its own, readable by its owner alone, which it removes when wget ends (Q-GENERIC-13: wget refuses or
// skips a configuration read from standard input), and which it does not create without a credential (Q-GENERIC-14); PowerShell
// through a variable of the session that holds the header, which no other process sees. In neither shell function is the token an
// argument, visible in the process list or in a shell trace. The header is the one line that holds the token's reference, so
// credentials=none drops it and leaves a configuration that sends no credential; wget's function takes another shape then
// (SetupData.Anonymous). Install has two snippets per client: the download of a file under its name, and the line that checks it
// against its SHA-256. Deleting has no tab of its own (Q-API-160); the guide names it.
func (Format) SetupTemplates() []formats.SetupTemplate {
	const curlFunction = CurlFunction + `() {
  curl --silent --show-error --config - "$@" <<EOF
header = "Authorization: Bearer ${QUADER_TOKEN}"
EOF
}
`
	// The body is a subshell, so the trap and the variable end with the call; mktemp creates the file with mode 0600, and the
	// here-document, which a shell trace does not print, fills it. Without a credential there is nothing to write: the function
	// runs wget with no configuration file at all, which skips the reader's .wgetrc as --config does (Q-GENERIC-14).
	const wgetFunction = WgetFunction + `() {{if .Anonymous}}{
  wget --no-verbose --no-config "$@"
}{{else}}(
  config=$(mktemp) || exit
  trap 'rm -f "$config"' EXIT
  cat > "$config" <<EOF
header = Authorization: Bearer ${QUADER_TOKEN}
EOF
  wget --no-verbose --config="$config" "$@"
){{end}}
`
	const headers = PowerShellHeaders + ` = @{}
` + PowerShellHeaders + `.Authorization = "Bearer $env:QUADER_TOKEN"
`
	name, path, file, sha := formats.SetupFileNameRef, formats.SetupPathRef, formats.SetupFileRef, formats.SetupSHA256Ref
	return []formats.SetupTemplate{
		{ID: "curl.function.configure", Client: ClientCurl, Section: "configure", Lang: "shell", Template: curlFunction},
		{ID: "curl.install", Client: ClientCurl, Section: "install", Lang: "shell", Template: CurlFunction + ` --fail --location --output ` + name + ` ` + address + "\n", Examples: []string{name, path}},
		{ID: "curl.checksum.install", Client: ClientCurl, Section: "install", Lang: "shell", Template: checksum, Examples: []string{sha, name}},
		{ID: "curl.publish", Client: ClientCurl, Section: "publish", Lang: "shell", Template: CurlFunction + ` --fail --upload-file ` + file + ` ` + address + "\n", Examples: []string{file, path}},

		{ID: "wget.function.configure", Client: ClientWget, Section: "configure", Lang: "shell", Template: wgetFunction},
		{ID: "wget.install", Client: ClientWget, Section: "install", Lang: "shell", Template: WgetFunction + ` --output-document ` + name + ` ` + address + "\n", Examples: []string{name, path}},
		{ID: "wget.checksum.install", Client: ClientWget, Section: "install", Lang: "shell", Template: checksum, Examples: []string{sha, name}},
		{ID: "wget.publish", Client: ClientWget, Section: "publish", Lang: "shell", Template: WgetFunction + ` --method PUT --body-file ` + file + ` --output-document - ` + address + "\n", Examples: []string{file, path}},

		{ID: "powershell.headers.configure", Client: ClientPowerShell, Section: "configure", Lang: "powershell", Template: headers},
		{ID: "powershell.install", Client: ClientPowerShell, Section: "install", Lang: "powershell", Template: `Invoke-WebRequest -Uri ` + address + ` -OutFile ` + name + ` -Headers ` + PowerShellHeaders + "\n", Examples: []string{path, name}},
		{ID: "powershell.checksum.install", Client: ClientPowerShell, Section: "install", Lang: "powershell", Template: `(Get-FileHash -Algorithm SHA256 -LiteralPath ` + name + `).Hash -eq '` + sha + `'` + "\n", Examples: []string{name, sha}},
		{ID: "powershell.publish", Client: ClientPowerShell, Section: "publish", Lang: "powershell", Template: `Invoke-WebRequest -Method Put -InFile ` + file + ` -Uri ` + address + ` -Headers ` + PowerShellHeaders + "\n", Examples: []string{file, path}},
	}
}
