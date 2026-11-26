package config

import (
	"strings"
	"testing"
)

// TestR_BAS_127_TheDeclaredRegistrationsAreCheckedInTheirFormOnly: an entry is decoded from the file and from the environment; the
// name, the description's length, a network's form, the count against max_registrations and a clean absolute token file are refused
// with the entry named. No file is read at load: an entry whose token file does not exist loads. Milliseconds, no data.
func TestR_BAS_127_TheDeclaredRegistrationsAreCheckedInTheirFormOnly(t *testing.T) {
	load := func(file string, env ...string) (*Loaded, error) {
		t.Helper()
		return Load(Options{File: "/c.yaml", ReadFile: fakeFS(map[string]string{"/c.yaml": file}), Environ: minimalEnv(env...), Command: CommandMigrate})
	}
	l, err := load("bastions:\n  declared:\n    partners:\n      description: Partner access\n      egress: [10.252.0.0/16, 198.51.100.20]\n" +
		"      token_file: /does/not/exist/token\n      secret_name: quader-bastion-token\n")
	if err != nil {
		t.Fatal(err)
	}
	d := l.Config.Bastions.Declared["partners"]
	if d.Description != "Partner access" || len(d.Egress) != 2 || d.TokenFile != "/does/not/exist/token" || d.SecretName != "quader-bastion-token" {
		t.Errorf("decoded %+v", d)
	}
	l, err = load("", `QUADER_BASTIONS_DECLARED={"edge":{"token_file":"/run/edge/token"}}`)
	if err != nil {
		t.Fatalf("from the environment: %v", err)
	}
	if d := l.Config.Bastions.Declared["edge"]; d.TokenFile != "/run/edge/token" || d.Description != "" || len(d.Egress) != 0 {
		t.Errorf("from the environment: %+v", d)
	}
	if l, err = load(""); err != nil || len(l.Config.Bastions.Declared) != 0 {
		t.Errorf("default: %v %+v", err, l)
	}

	const file = "token_file: /t"
	for _, tc := range []struct{ name, section, want string }{
		{"a name with capitals", "declared:\n    Partners: {" + file + "}", "bastions.declared.Partners the name must match"},
		{"a name of 21 characters", "declared:\n    " + strings.Repeat("a", 21) + ": {" + file + "}", "the name must match"},
		{"a description of 1,025 characters", "declared:\n    partners: {" + file + ", description: " + strings.Repeat("d", 1025) + "}", "bastions.declared.partners.description has more than 1024 characters"},
		{"a network that is none", "declared:\n    partners: {" + file + ", egress: [10.0.0.0/8, nonsense]}", `bastions.declared.partners.egress[1] "nonsense" is neither`},
		{"33 networks", "declared:\n    partners: {" + file + ", egress: [" + strings.TrimSuffix(strings.Repeat("10.0.0.1, ", 33), ", ") + "]}", "lists 33 networks"},
		{"no token file", "declared:\n    partners: {description: x}", "bastions.declared.partners.token_file is required"},
		{"a relative token file", "declared:\n    partners: {token_file: t}", `bastions.declared.partners.token_file "t" must be a clean absolute path`},
		{"a token file that is not clean", "declared:\n    partners: {token_file: /a/../t}", "must be a clean absolute path"},
		{"an unknown member", "declared:\n    partners: {" + file + ", token: x}", "token"},
		{"more than max_registrations", "max_registrations: 1\n  declared:\n    a: {" + file + "}\n    b: {" + file + "}", "bastions.declared declares 2 registrations, more than max_registrations (1)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load("bastions:\n  " + tc.section + "\n")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v, want %q", err, tc.want)
			}
		})
	}
}
