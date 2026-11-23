package testenv

import (
	"context"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// recorder captures Fatalf of the accessor under test.
type recorder struct {
	testing.TB
	msg string
}

func (r *recorder) Helper() {}
func (r *recorder) Fatalf(format string, args ...any) {
	r.msg = strings.TrimSpace(format)
	panic(r)
}

func TestGetFailsWithAClearMessage(t *testing.T) {
	t.Run("a missing variable fails and names the variable and the fix", func(t *testing.T) {
		t.Setenv("QUADER_TEST_UNSET_FOR_THIS_TEST", "")
		r := &recorder{TB: t}
		func() {
			defer func() { _ = recover() }()
			Get(r, "QUADER_TEST_UNSET_FOR_THIS_TEST")
		}()
		if !strings.Contains(r.msg, "is not set") || !strings.Contains(r.msg, "integration") {
			t.Fatalf("message %q is not clear", r.msg)
		}
	})
	t.Run("a set variable is returned", func(t *testing.T) {
		t.Setenv("QUADER_TEST_SET_FOR_THIS_TEST", "x")
		if got := Get(t, "QUADER_TEST_SET_FOR_THIS_TEST"); got != "x" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("lists are split and trimmed", func(t *testing.T) {
		got := split(" a:1, b:2 ,,c:3")
		if len(got) != 3 || got[1] != "b:2" {
			t.Fatalf("got %v", got)
		}
	})
}

func TestCheckTLSRefusesPlaintextEndpoints(t *testing.T) {
	ca := t.TempDir() + "/ca.crt"
	if err := os.WriteFile(ca, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := map[string]string{
		"QUADER_TEST_CA_FILE":          ca,
		"QUADER_TEST_PG_DSN":           "postgres://u:p@127.0.0.1:15432/quader?sslmode=verify-full&sslrootcert=" + ca,
		"QUADER_TEST_PG_PGBOUNCER_DSN": "postgres://u:p@127.0.0.1:16432/quader?sslmode=verify-full&sslrootcert=" + ca,
		"QUADER_TEST_S3_ENDPOINT":      "https://127.0.0.1:18333",
		"QUADER_TEST_VALKEY_CA_FILE":   ca,
	}
	with := func(k, v string) func(string) string {
		return func(name string) string {
			if name == k {
				return v
			}
			return good[name]
		}
	}
	if err := checkTLS(with("", "")); err != nil {
		t.Fatalf("a TLS environment is refused: %v", err)
	}
	for name, tc := range map[string]struct{ key, val, want string }{
		"plaintext S3 endpoint":    {"QUADER_TEST_S3_ENDPOINT", "http://127.0.0.1:18333", "plaintext"},
		"sslmode disable":          {"QUADER_TEST_PG_DSN", "postgres://u:p@127.0.0.1:15432/quader?sslmode=disable", "plaintext or unverified"},
		"sslmode require":          {"QUADER_TEST_PG_PGBOUNCER_DSN", "postgres://u:p@127.0.0.1:16432/quader?sslmode=require", "plaintext or unverified"},
		"verify-full without root": {"QUADER_TEST_PG_DSN", "postgres://u:p@127.0.0.1:15432/quader?sslmode=verify-full", "plaintext or unverified"},
		"no DSN":                   {"QUADER_TEST_PG_DSN", "", "is not set"},
		"no CA":                    {"QUADER_TEST_CA_FILE", "", "is not set"},
		"CA file missing":          {"QUADER_TEST_CA_FILE", "/nonexistent/ca.crt", "not a readable file"},
		"unknown scope":            {"QUADER_TEST_SCOPE", "everything", "is not stack or s3"},
	} {
		t.Run(name, func(t *testing.T) {
			err := checkTLS(with(tc.key, tc.val))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// The template's name follows the migration files and the day, and the harness drops only what carries such a name (Q-ARCH-581).
func TestTemplateName(t *testing.T) {
	steps := fstest.MapFS{
		"00001_first.sql":  {Data: []byte("CREATE TABLE a (id int);\n")},
		"00002_second.sql": {Data: []byte("CREATE TABLE b (id int);\n")},
	}
	name := func(fsys fstest.MapFS, day string) string {
		t.Helper()
		n, err := templateName(fsys, day)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	base := name(steps, "20261123")
	t.Run("Q-ARCH-581 the same files on the same day name the same template, and the name is the harness's", func(t *testing.T) {
		if again := name(steps, "20261123"); again != base {
			t.Fatalf("%s, then %s", base, again)
		}
		if !templateNames.MatchString(base) || !templateNames.MatchString(base+"_build") {
			t.Fatalf("%s or its build does not match the harness's pattern", base)
		}
	})
	t.Run("Q-ARCH-581 a changed migration, a further step and another day each name another template", func(t *testing.T) {
		changed := fstest.MapFS{"00001_first.sql": steps["00001_first.sql"], "00002_second.sql": {Data: []byte("CREATE TABLE b (id bigint);\n")}}
		more := fstest.MapFS{"00001_first.sql": steps["00001_first.sql"], "00002_second.sql": steps["00002_second.sql"],
			"00003_third.sql": {Data: []byte("CREATE TABLE c (id int);\n")}}
		for what, n := range map[string]string{"changed": name(changed, "20261123"), "more": name(more, "20261123"), "day": name(steps, "20261124")} {
			if n == base {
				t.Errorf("%s: still %s", what, n)
			}
		}
	})
	t.Run("Q-ARCH-581 no other database of the stacks' PostgreSQL has a name of the harness's templates, and none is dropped", func(t *testing.T) {
		for _, other := range []string{"postgres", "quader", "template0", "template1", "quader_it_0123456789", "quader_ittpl_0123456789",
			"quader_uploads_tmpl_abc", "quader_ittpl_20261123_0123456789abcdef_copy", "x" + base} {
			if templateNames.MatchString(other) {
				t.Errorf("%s matches the harness's pattern", other)
			}
			// The refusal comes before the server is asked: the connection is not used.
			if err := dropTemplate(context.Background(), nil, other); err == nil || !strings.Contains(err.Error(), "not a template of the harness") {
				t.Errorf("drop %s: %v", other, err)
			}
		}
	})
}

func TestCheckTLSScopeS3(t *testing.T) {
	only := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	t.Run("R-STOR-035 the conformance scope needs only an https S3 endpoint", func(t *testing.T) {
		if err := checkTLS(only(map[string]string{"QUADER_TEST_SCOPE": "s3", "QUADER_TEST_S3_ENDPOINT": "https://s3.example.test"})); err != nil {
			t.Fatalf("an S3-only environment is refused: %v", err)
		}
	})
	t.Run("R-STOR-035 the conformance scope refuses a plaintext S3 endpoint and an unreadable CA file", func(t *testing.T) {
		if err := checkTLS(only(map[string]string{"QUADER_TEST_SCOPE": "s3", "QUADER_TEST_S3_ENDPOINT": "http://127.0.0.1:9000"})); err == nil || !strings.Contains(err.Error(), "plaintext") {
			t.Fatalf("err = %v, want plaintext", err)
		}
		if err := checkTLS(only(map[string]string{"QUADER_TEST_SCOPE": "s3", "QUADER_TEST_S3_ENDPOINT": "https://s3.example.test", "QUADER_TEST_CA_FILE": "/nonexistent/ca.crt"})); err == nil || !strings.Contains(err.Error(), "not a readable file") {
			t.Fatalf("err = %v, want not a readable file", err)
		}
	})
	t.Run("R-HA-051 without a scope the full stack is still required", func(t *testing.T) {
		if err := checkTLS(only(map[string]string{"QUADER_TEST_S3_ENDPOINT": "https://s3.example.test"})); err == nil || !strings.Contains(err.Error(), "is not set") {
			t.Fatalf("err = %v, want is not set", err)
		}
	})
}
