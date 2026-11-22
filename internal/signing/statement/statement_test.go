package statement_test

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/signing/statement"
)

var update = flag.Bool("update", false, "rewrite the golden files of the statements")

var (
	received = time.Date(2026, 11, 21, 10, 2, 3, 456_000_000, time.UTC)
	issued   = time.Date(2026, 11, 21, 10, 2, 6, 1_000_000, time.UTC)
	scanned  = time.Date(2026, 11, 21, 10, 2, 5, 120_000_000, time.UTC)
	built    = time.Date(2026, 11, 21, 6, 10, 0, 0, time.UTC)
	dataAt   = time.Date(2026, 11, 21, 4, 0, 0, 0, time.UTC)
)

func base(format, class, path string, content []byte) statement.Facts {
	return statement.Facts{
		InstallationID: "6c3e0d2a-1f2e-4b7a-9c1d-5e8f7a6b4c3d", BaseURL: "https://quader.example.com", Host: "quader.example.com",
		Project: "web", Repository: format + "-" + class, Format: format, Class: class, Path: path, Size: int64(len(content)),
		SHA256: sha256.Sum256(content), ReceivedAt: received, IssuedAt: issued,
	}
}

func cases() map[string]statement.Facts {
	tarball := []byte("a tarball of a few bytes")
	upload := base("npm", "local", "@acme/ui/-/ui-1.2.0.tgz", tarball)
	sum512 := sha512.Sum512(tarball)
	upload.Name, upload.Version, upload.SHA512 = "@acme/ui", "1.2.0", sum512[:]
	upload.Origin = statement.Origin{Kind: statement.OriginUpload, PrincipalKind: "ci_mapping", PrincipalName: "gitlab-platform", CI: map[string]any{
		"issuer": "gitlab", "project_path": "platform/ui", "pipeline_id": "48211", "job_id": "991204", "ref": "main", "ref_type": "branch",
		"ref_protected": true, "pipeline_source": "push", "sha": "3e9d1c", "user_email": "ada@example.com",
	}}
	upload.Signatures = []statement.Signature{{
		Role: "provenance", Scheme: "sigstore-bundle", Integrity: "valid", Identity: "https://gitlab.example.com/platform/ui//.gitlab-ci.yml@refs/heads/main",
		Issuer: "https://gitlab.example.com", KeyIDs: []string{"9a41"},
	}}
	upload.Malware = statement.Malware{State: "clean", Database: "main-62.daily-27450.bytecode-339", DatabaseBuiltAt: built, ScannedAt: scanned}
	upload.Vulnerabilities = statement.Vulnerabilities{State: "evaluated", Grade: "A", Counts: &statement.Counts{Low: 2}, DataAt: dataAt}

	fill := base("npm", "remote", "lodash/-/lodash-4.17.21.tgz", []byte("lodash"))
	fill.Name, fill.Version = "lodash", "4.17.21"
	fill.Origin = statement.Origin{Kind: statement.OriginFill, Upstream: "https://registry.npmjs.org/", FetchedAt: received}
	fill.Malware = statement.Malware{State: "clean", Database: "main-62", DatabaseBuiltAt: built, ScannedAt: scanned}

	index := base("oci", "local", "app/_manifests/sha256:5f1c", []byte(`{"schemaVersion":2}`))
	index.Name, index.MediaType = "app", "application/vnd.oci.image.index.v1+json"
	index.Origin = statement.Origin{Kind: statement.OriginUpload, PrincipalKind: "user", PrincipalName: "ada"}
	index.Malware = statement.Malware{State: "clean", Database: "main-62", DatabaseBuiltAt: built, ScannedAt: scanned}
	index.Vulnerabilities = statement.Vulnerabilities{State: "evaluated", Grade: "B", Counts: &statement.Counts{High: 1, Medium: 3}, DataAt: dataAt}

	infected := base("generic", "local", "tools/eicar.com", []byte("X5O!"))
	infected.Name = "tools/eicar.com"
	infected.Origin = statement.Origin{Kind: statement.OriginUpload, PrincipalKind: "service_account", PrincipalName: "deployer"}
	infected.Malware = statement.Malware{State: "infected", Database: "main-62", DatabaseBuiltAt: built, ScannedAt: scanned}

	pending := base("nuget", "local", "acme.widgets/1.2.3/acme.widgets.1.2.3.nupkg", []byte("nupkg"))
	pending.Name, pending.Version = "acme.widgets", "1.2.3"
	pending.Origin = statement.Origin{Kind: statement.OriginUpload, PrincipalKind: "user", PrincipalName: "ada"}
	pending.Signatures = []statement.Signature{{Role: "publisher", Scheme: "nuget-cms", Integrity: "pending"}}
	pending.Malware = statement.Malware{State: statement.MalwarePending}
	pending.Vulnerabilities = statement.Vulnerabilities{State: "pending_data"}

	return map[string]statement.Facts{
		"upload-with-ci-claims": upload, "remote-fill": fill, "oci-index": index, "infected": infected, "checks-not-answered": pending,
	}
}

// TestR_SIG_051_TheStatementsOfTheReceiptEqualTheirGoldenFiles: the statements of an upload with CI claims, a remote fill, an OCI index,
// an item with an infected verdict and one whose checks have not answered equal their golden files; each is an in-toto statement v1 with
// the predicate type receipt v1 and the subject's digests, and none carries a trust state or an e-mail address. Unit, milliseconds, bytes.
func TestR_SIG_051_TheStatementsOfTheReceiptEqualTheirGoldenFiles(t *testing.T) {
	for name, f := range cases() {
		t.Run(name, func(t *testing.T) {
			got, err := statement.Build(f)
			if err != nil {
				t.Fatal(err)
			}
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, got, "", "  "); err != nil {
				t.Fatal(err)
			}
			pretty.WriteByte('\n')
			golden := filepath.Join("testdata", name+".json")
			if *update {
				if err := os.WriteFile(golden, pretty.Bytes(), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (go test ./internal/signing/statement -update writes it)", err)
			}
			if !bytes.Equal(pretty.Bytes(), want) {
				t.Errorf("the statement differs from %s:\n%s", golden, pretty.Bytes())
			}
			for _, banned := range []string{"trust", "@example.com", "user_email", "internal\"", "PRIVATE"} {
				if strings.Contains(string(got), banned) {
					t.Errorf("the statement carries %q: %s", banned, got)
				}
			}
			var st struct {
				Type          string `json:"_type"`
				PredicateType string `json:"predicateType"`
				Subject       []struct {
					Digest map[string]string `json:"digest"`
				} `json:"subject"`
			}
			if err := json.Unmarshal(got, &st); err != nil {
				t.Fatal(err)
			}
			if st.Type != statement.Type || st.PredicateType != statement.PredicateType || len(st.Subject) != 1 || st.Subject[0].Digest["sha256"] == "" {
				t.Errorf("not an in-toto statement of receipt v1: %s", got)
			}
		})
	}
}

// TestR_SIG_051_AStatementKeepsToItsBounds: more than 32 signatures are cut to 32, and a statement above 16 KiB leaves out signatures
// until it fits, counting them in signaturesOmitted. Unit, milliseconds.
func TestR_SIG_051_AStatementKeepsToItsBounds(t *testing.T) {
	f := base("generic", "local", "a.bin", []byte("a"))
	for range 40 {
		f.Signatures = append(f.Signatures, statement.Signature{Role: "publisher", Scheme: "openpgp", Integrity: "valid", Identity: strings.Repeat("x", 600)})
	}
	got, err := statement.Build(f)
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Predicate struct {
			Signatures []json.RawMessage `json:"signatures"`
			Omitted    int               `json:"signaturesOmitted"`
		} `json:"predicate"`
	}
	if err := json.Unmarshal(got, &st); err != nil {
		t.Fatal(err)
	}
	if len(got) > statement.MaxBytes || len(st.Predicate.Signatures)+st.Predicate.Omitted != 40 || len(st.Predicate.Signatures) > statement.MaxSignatures {
		t.Fatalf("%d bytes, %d signatures, %d omitted", len(got), len(st.Predicate.Signatures), st.Predicate.Omitted)
	}
	back, err := statement.Read(got)
	if err != nil || len(back.Signatures) != len(st.Predicate.Signatures) || back.SignaturesOmitted != st.Predicate.Omitted {
		t.Fatalf("read back %v: %d signatures, %d omitted", err, len(back.Signatures), back.SignaturesOmitted)
	}
}

// TestR_SIG_055_AStatementReadsBackAsItsFacts: Read is Build's inverse for what a statement carries (the facts the screens show at a
// version and a file: the time of receipt, the origin with a pipeline's claims, the integrity states, the malware and the vulnerability
// state with their dates), with the defaults Build writes and without the e-mail claim Build drops; what is no receipt v1 or is beyond
// the bounds is refused (a statement cut at its bounds reads back in the test above). Unit, milliseconds, the five golden cases.
func TestR_SIG_055_AStatementReadsBackAsItsFacts(t *testing.T) {
	for name, f := range cases() {
		t.Run(name, func(t *testing.T) {
			b, err := statement.Build(f)
			if err != nil {
				t.Fatal(err)
			}
			got, err := statement.Read(b)
			if err != nil {
				t.Fatal(err)
			}
			want := f
			want.Host = ""
			if want.Vulnerabilities.State == "" {
				want.Vulnerabilities.State = "not_covered"
			}
			if want.Origin.CI != nil {
				ci := map[string]any{}
				for k, v := range want.Origin.CI {
					if k != "user_email" {
						ci[k] = v
					}
				}
				want.Origin.CI = ci
			}
			if !reflect.DeepEqual(got.Facts, want) || got.SignaturesOmitted != 0 {
				t.Errorf("read back:\n%+v\nwant:\n%+v", got.Facts, want)
			}
		})
	}
	good, err := statement.Build(cases()["infected"])
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{
		"another predicate": bytes.Replace(good, []byte("#receipt-attestation-v1"), []byte("#receipt-attestation-v2"), 1),
		"no statement":      []byte(`{"_type":"https://example.com/other"}`),
		"not JSON":          []byte("{"),
		"above the bound":   append(bytes.Repeat([]byte(" "), statement.MaxBytes), good...),
	} {
		if _, err := statement.Read(b); !errors.Is(err, statement.ErrNotReceipt) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
