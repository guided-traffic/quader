package formats_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/observability"
)

// R-OPS-009: the line a format writes for a request that failed goes to the logger the request carries, the replica's, with its fields,
// and not to the process's default logger.
func TestR_OPS_009_AFailedRequestIsLoggedThroughTheRequestsLogger(t *testing.T) {
	var buf bytes.Buffer
	replica := slog.New(slog.NewJSONHandler(&buf, nil)).With("replica", "web-1", "component", "http")
	r := httptest.NewRequest(http.MethodGet, "/api/deb/p/r/x", nil)
	r = r.WithContext(observability.ContextWithLogger(r.Context(), replica))
	w := httptest.NewRecorder()
	w.Header().Set("X-Request-Id", "4bf92f3577b34da6a3ce929d0e0e4736")
	formats.Base{}.WriteError(w, r, errs.E(errs.Internal, errs.CodeInternal, "", errs.WithCause(errors.New("the pool ran dry"))))
	line := buf.String()
	for _, want := range []string{`"replica":"web-1"`, `"msg":"request failed"`, `"request_id":"4bf92f3577b34da6a3ce929d0e0e4736"`, "the pool ran dry"} {
		if !strings.Contains(line, want) {
			t.Errorf("the replica's log lacks %s: %s", want, line)
		}
	}
}
