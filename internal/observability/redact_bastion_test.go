package observability

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// logged returns the log line of a record with one attribute, after the handler's redaction.
func logged(t *testing.T, key string, val any) string {
	t.Helper()
	var buf bytes.Buffer
	NewLogger(testLogCfg(), &buf).Info("event", key, val)
	return buf.String()
}

// TestR_BAS_008_TheBastionTokenHeaderIsRedactedLikeAuthorization: Quader-Bastion-Authorization is a sensitive
// header of R-OPS-013 in every log record, span attribute and support bundle (which take the same handler), as a
// header and as text; the other Quader-Bastion-* headers carry no secret and stay readable.
func TestR_BAS_008_TheBastionTokenHeaderIsRedactedLikeAuthorization(t *testing.T) {
	const secret = "qdr_bst_Zy9xKq3LmNpQrStUvWxYz0123456789abcd"
	h := http.Header{
		"Quader-Bastion-Authorization": {"Bearer " + secret},
		"Quader-Bastion-Interface":     {"1"},
		"Quader-Bastion-Replica":       {"quader-bastion-7d9c-x2k4q"},
		"Authorization":                {"Bearer callertoken123"},
	}
	out := logged(t, "headers", h)
	for _, leak := range []string{secret, "Zy9xKq3L", "callertoken123"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked: %s", leak, out)
		}
	}
	if !strings.Contains(out, "quader-bastion-7d9c-x2k4q") || !strings.Contains(out, "Quader-Bastion-Interface") {
		t.Errorf("a header without a secret lost its value: %s", out)
	}
	if !SensitiveKey("Quader_Bastion_Authorization") || !SensitiveKey("quader-bastion-authorization") {
		t.Error("SensitiveKey does not name the header")
	}
	for name, val := range map[string]any{
		"a map of headers": map[string]string{"quader-bastion-authorization": "Bearer " + secret},
		"text":             "forwarded with Quader-Bastion-Authorization: Bearer " + secret,
		"the bare token":   secret,
	} {
		if out := logged(t, "value", val); strings.Contains(out, "Zy9xKq3L") {
			t.Errorf("%s: %s", name, out)
		}
	}
}

// TestR_BAS_073_TheHandoverHeaderIsRedactedLikeAuthorization: Quader-Bastion-Handover, whose value on an answer is a signed
// link and, for an encrypted copy, a data key, is a sensitive header of R-OPS-013 as a header, as a key and as text, and a
// bare value is recognised by its start; the Bastion's offer and a field that only contains the word stay readable.
func TestR_BAS_073_TheHandoverHeaderIsRedactedLikeAuthorization(t *testing.T) {
	raw := make([]byte, iface.HandoverKeyBytes)
	for i := range raw {
		raw[i] = byte(0x50 + i)
	}
	key, err := iface.NewHandoverKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	value, err := iface.EncodeHandover(iface.Handover{
		Link:      "https://objects.example.com/b/sha256/9f/86/x?X-Amz-Signature=c0ffee",
		ExpiresAt: time.Date(2026, 11, 22, 10, 1, 0, 0, time.UTC), Connection: "default",
		SHA256: strings.Repeat("ab", 32), Size: 1024, StoredSize: 1024 + 28 + 68, Encrypted: true, DataKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	leaks := []string{value[:24], value[len(value)-24:], base64.RawURLEncoding.EncodeToString(raw)}
	check := func(name, out string) {
		t.Helper()
		for _, l := range leaks {
			if strings.Contains(out, l) {
				t.Errorf("%s: %q leaked: %s", name, l, out)
			}
		}
	}
	h := http.Header{iface.HeaderHandover: {value}, iface.HeaderClass: {"handover"}}
	out := logged(t, "headers", h)
	check("a header", out)
	if !strings.Contains(out, "handover") || !strings.Contains(out, iface.HeaderClass) {
		t.Errorf("the class lost its value: %s", out)
	}
	for _, k := range []string{iface.HeaderHandover, "quader-bastion-handover", "Quader_Bastion_Handover"} {
		if !SensitiveKey(k) {
			t.Errorf("SensitiveKey(%q) is false", k)
		}
	}
	for _, k := range []string{"bastion_handover", "handover", "delivery"} {
		if SensitiveKey(k) {
			t.Errorf("SensitiveKey(%q) is true: a field that holds an outcome would be hidden", k)
		}
	}
	for name, val := range map[string]any{
		"a map of headers": map[string]string{"quader-bastion-handover": value},
		"text":             "answered with Quader-Bastion-Handover: " + value + " and Content-Length: 0",
		"lower-case text":  "quader-bastion-handover=" + value,
		"the bare value":   value,
	} {
		check(name, logged(t, "value", val))
	}
	if out := logged(t, "bastion_handover", "fallback"); !strings.Contains(out, "fallback") {
		t.Errorf("an outcome was hidden: %s", out)
	}
	if out := Scrub("forwarded with Quader-Bastion-Handover: accept; max-size=1048576"); !strings.Contains(out, "accept; max-size=1048576") {
		t.Errorf("the offer was hidden: %s", out)
	}
}
