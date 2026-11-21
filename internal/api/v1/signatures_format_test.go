package v1

import (
	"encoding/json"
	"testing"

	_ "github.com/guided-traffic/quader/internal/formats/all" // registers the formats whose descriptors are served
)

// TestR_SIG_009_FormatDescriptorNamesTheSignatureKinds: the descriptor of GET /api/v1/formats lists the kinds of the format's signature
// declaration by name and role (R-SIG-002, R-SIG-022), which a version without a signature names in its details (R-SIG-009); a format
// that declares none (the ClamAV definitions) lists none. The answer's shape against the document is TestR_UI_021's, which validates
// the same answer: loading the document's validator a second time cost about 7 s under the race detector.
func TestR_SIG_009_FormatDescriptorNamesTheSignatureKinds(t *testing.T) {
	s := newSetup(t, func(o *Options) { o.Access = allowAll{} }, authed)
	w := s.get("/api/v1/formats")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var body struct {
		Formats []struct {
			Name           string `json:"name"`
			SignatureKinds []struct {
				Name string `json:"name"`
				Role string `json:"role"`
			} `json:"signatureKinds"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Formats) == 0 {
		t.Fatalf("%v %s", err, w.Body)
	}
	kinds := map[string][]string{}
	for _, f := range body.Formats {
		if f.SignatureKinds == nil {
			t.Errorf("%s: no member signatureKinds", f.Name)
		}
		for _, k := range f.SignatureKinds {
			kinds[f.Name] = append(kinds[f.Name], k.Name+"/"+k.Role)
		}
	}
	if got := kinds["oci"]; len(got) == 0 || got[0] != "Notation/publisher" {
		t.Errorf("oci: %v, want Notation as the first publisher kind", got)
	}
	if got, ok := kinds["clamav"]; ok {
		t.Errorf("clamav: %v, want none", got)
	}
}
