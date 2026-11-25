package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// R-OCI-040: the reader of an image's configuration. The fixture is a configuration Docker Buildx 0.x of Docker 28 wrote for a
// two-line image FROM scratch (testdata/buildx-config.json, 2.6 KB), so the reader is proven against what the real builder writes.
func TestR_OCI_040_ReadsTheConfigurationBuildxWrote(t *testing.T) {
	data, err := os.ReadFile("testdata/buildx-config.json")
	if err != nil {
		t.Fatal(err)
	}
	got := ReadConfig(ConfigKind(MediaTypeImageConfig), data)
	if got.Unreadable != "" || got.Image == nil {
		t.Fatalf("unreadable: %+v", got)
	}
	img := got.Image
	if img.OS != "linux" || img.Architecture != "amd64" || img.User != "1000:1000" || img.WorkingDir != "/app" || img.StopSignal != "SIGTERM" {
		t.Errorf("scalars: %+v", img)
	}
	if fmt.Sprint(img.Entrypoint) != "[/app/run]" || fmt.Sprint(img.Cmd) != "[--port 8080]" {
		t.Errorf("commands: %v %v", img.Entrypoint, img.Cmd)
	}
	if fmt.Sprint(img.ExposedPorts) != "[8080/tcp 9090/tcp]" || fmt.Sprint(img.Volumes) != "[/data]" {
		t.Errorf("ports and volumes: %v %v", img.ExposedPorts, img.Volumes)
	}
	if len(img.Env) != 3 || img.Env[2] != (rows.Var{Name: "GREETING", Value: "hello world"}) {
		t.Errorf("env: %+v", img.Env)
	}
	if len(img.Labels) != 3 || img.Labels[0].Name != "org.opencontainers.image.description" {
		t.Errorf("labels are sorted by key: %+v", img.Labels)
	}
	if h := img.Healthcheck; h == nil || h.Interval != 30_000_000_000 || h.Retries != 2 || fmt.Sprint(h.Test) != "[CMD /app/check]" {
		t.Errorf("healthcheck: %+v", h)
	}
	if len(img.History) != 11 || img.History[2].CreatedBy != "COPY hello.txt /app/hello.txt # buildkit" || img.History[2].EmptyLayer ||
		!img.History[0].EmptyLayer {
		t.Errorf("history: %+v", img.History)
	}
	if img.Omitted != nil {
		t.Errorf("nothing is left out of a small configuration: %+v", img.Omitted)
	}
}

// R-OCI-040, Q-OCI-272: what two readers could read differently, a value of a type the runtime refuses, and a blob over the bound are
// read as nothing, with the reason.
func TestR_OCI_040_RefusesWhatTwoReadersCouldReadDifferently(t *testing.T) {
	for _, tc := range []struct{ name, doc, want string }{
		{"a key twice", `{"os":"linux","os":"windows"}`, rows.UnreadableInvalid},
		{"two keys that only differ in case", `{"config":{"Env":["A=1"],"env":["A=2"]}}`, rows.UnreadableInvalid},
		{"a list that is no list", `{"config":{"Env":"A=1"}}`, rows.UnreadableInvalid},
		{"a label that is no string", `{"config":{"Labels":{"a":1}}}`, rows.UnreadableInvalid},
		{"not UTF-8", "{\"os\":\"\xff\"}", rows.UnreadableInvalid},
		{"no object", `[]`, rows.UnreadableInvalid},
		{"data after the value", `{} {}`, rows.UnreadableInvalid},
		{"nested too deep", strings.Repeat(`{"a":`, 20) + `1` + strings.Repeat(`}`, 20), rows.UnreadableTooLarge},
		{"too large", `{"author":"` + strings.Repeat("a", MaxConfigBytes) + `"}`, rows.UnreadableTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReadConfig(rows.ConfigImage, []byte(tc.doc)); got.Unreadable != tc.want || got.Image != nil {
				t.Errorf("got %+v, want unreadable %q", got, tc.want)
			}
		})
	}
	if got := ReadConfig("", []byte(`{}`)); got.Unreadable != rows.UnreadableInvalid {
		t.Errorf("a kind no reader reads is read as nothing: %+v", got)
	}
}

// R-OCI-040, Q-OCI-272: a key in another case is read as the runtime reads it; a single string is a command of one argument; an
// unknown key is ignored.
func TestR_OCI_040_ReadsKeysAsTheRuntimeDoes(t *testing.T) {
	got := ReadConfig(rows.ConfigImage, []byte(`{"OS":"linux","config":{"cmd":"run","Unknown":{"x":[1]}}}`))
	if got.Image == nil || got.Image.OS != "linux" || fmt.Sprint(got.Image.Cmd) != "[run]" {
		t.Errorf("got %+v", got)
	}
}

// R-OCI-040: the bounds. Long strings are cut at a character boundary and marked, long lists are cut and counted, a NUL is replaced,
// and the whole stays below the budget whatever the configuration holds.
func TestR_OCI_040_BoundsWhatItKeeps(t *testing.T) {
	long := strings.Repeat("é", MaxConfigString) // two bytes each: the cut lands inside one and moves back
	env := make([]string, MaxConfigEnv+5)
	for i := range env {
		env[i] = fmt.Sprintf("V%d=x", i)
	}
	env[0] = "TOKEN=" + long
	env[1] = "NUL=a\x00b"
	history := make([]map[string]any, 600)
	for i := range history {
		history[i] = map[string]any{"created_by": "RUN " + strings.Repeat("x", 600)}
	}
	doc, _ := json.Marshal(map[string]any{"config": map[string]any{"Env": env}, "history": history})
	got := ReadConfig(rows.ConfigImage, doc)
	img := got.Image
	if img == nil {
		t.Fatalf("unreadable: %+v", got)
	}
	if v := img.Env[0]; !v.Cut || len(v.Value) > MaxConfigString || !strings.HasPrefix(long, v.Value) {
		t.Errorf("a long value is cut at a character and marked: cut=%v len=%d", v.Cut, len(v.Value))
	}
	if img.Env[1].Value != "a�b" {
		t.Errorf("a NUL is replaced: %q", img.Env[1].Value)
	}
	if len(img.Env) != MaxConfigEnv || img.Omitted == nil || img.Omitted.Env != 5 || !img.Omitted.Cut {
		t.Errorf("env %d, omitted %+v", len(img.Env), img.Omitted)
	}
	if len(img.History)+img.Omitted.History != 600 || img.Omitted.History == 0 {
		t.Errorf("history %d kept, %d left out", len(img.History), img.Omitted.History)
	}
	raw, _ := json.Marshal(got)
	if len(raw) > ConfigBudget+8<<10 {
		t.Errorf("the kept JSON is %d bytes, the budget %d", len(raw), ConfigBudget)
	}
}
