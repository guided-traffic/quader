package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The refusals of the Bastion as R-BAS-095 and Q-BAS-337 specify them (not built yet): the stub answers the entry points a client
// needs before its first content request (the registry's /v2/, a NuGet service index, a Galaxy API root) and refuses everything
// else with one status, code and detail in the error shape the format declares. Each request is logged as one JSON line, so a
// client's retries can be counted.

// The texts of Q-BAS-321 and, for 429, of R-LIM-020's form.
var stubDetails = map[string]string{
	"busy":         "Too many requests are waiting; try again in a moment.",
	"read_only":    "This address is read-only: it serves downloads and accepts no uploads.",
	"rate_limited": "The limit of 50 requests a second is reached. Try again in 2 seconds.",
}

// shapeOf is the errorShape of the format a path belongs to (R-BAS-095's table).
func shapeOf(path string) string {
	switch {
	case strings.HasPrefix(path, "/v2/"):
		return "distribution"
	case strings.HasPrefix(path, "/api/npm/"):
		return "npm"
	case strings.HasPrefix(path, "/api/nuget/"):
		return "text"
	}
	return "problem"
}

// distributionCode is R-BAS-029's mapping of a code to the distribution specification's codes.
func distributionCode(status int) string {
	switch status {
	case http.StatusForbidden:
		return "DENIED"
	case http.StatusTooManyRequests:
		return "TOOMANYREQUESTS"
	}
	return "UNAVAILABLE"
}

func writeRefusal(w http.ResponseWriter, path string, status int, code, detail, retryAfter string) {
	if retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	w.Header().Set("Cache-Control", "no-store")
	var body []byte
	switch shapeOf(path) {
	case "npm":
		w.Header().Set("Content-Type", "application/json")
		body, _ = json.Marshal(map[string]string{"error": detail})
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		body = []byte(code + ": " + detail + "\n")
	case "distribution":
		w.Header().Set("Content-Type", "application/json")
		body, _ = json.Marshal(map[string]any{"errors": []map[string]any{{"code": distributionCode(status), "message": detail, "detail": map[string]string{"code": code}}}})
	default:
		w.Header().Set("Content-Type", "application/problem+json")
		body, _ = json.Marshal(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail, "code": code})
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func stub(args []string) error {
	fs := flag.NewFlagSet("stub", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:19288", "address")
	cert := fs.String("cert", "", "certificate file")
	key := fs.String("key", "", "key file")
	status := fs.Int("status", 503, "the status of every refusal")
	code := fs.String("code", "busy", "busy, rate_limited or read_only")
	retry := fs.String("retry-after", "2", "Retry-After in seconds, empty for none")
	writesOnly := fs.Bool("writes-only", false, "refuse only methods other than GET and HEAD; reads answer 404")
	if err := fs.Parse(args); err != nil {
		return err
	}
	detail := stubDetails[*code]
	var mu sync.Mutex
	enc := json.NewEncoder(os.Stdout)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		mu.Lock()
		_ = enc.Encode(map[string]string{"time": time.Now().UTC().Format(time.RFC3339Nano), "method": r.Method, "path": r.URL.RequestURI(), "ua": r.UserAgent()})
		mu.Unlock()
		base := "https://" + r.Host
		switch {
		case r.Method == http.MethodGet && (p == "/v2/" || p == "/v2"):
			w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
			return
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/api/nuget/") && strings.HasSuffix(p, "/v3/index.json"):
			root := base + strings.TrimSuffix(p, "index.json")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "3.0.0", "resources": []map[string]string{
				{"@id": root + "flatcontainer/", "@type": "PackageBaseAddress/3.0.0"},
				{"@id": root + "package", "@type": "PackagePublish/2.0.0"},
				{"@id": root + "registration-semver2/", "@type": "RegistrationsBaseUrl/3.6.0"},
			}})
			return
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/api/ansible/") && strings.Count(strings.TrimPrefix(p, "/api/ansible/"), "/") == 2 && strings.HasSuffix(p, "/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"available_versions":{"v3":"v3/"},"current_version":"v3"}`))
			return
		case *writesOnly && (r.Method == http.MethodGet || r.Method == http.MethodHead):
			http.NotFound(w, r)
			return
		}
		writeRefusal(w, p, *status, *code, detail, *retry)
	})
	srv := &http.Server{Addr: *listen, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintln(os.Stderr, "stub: "+*listen+" answers "+strconv.Itoa(*status)+" "+*code)
	return srv.ListenAndServeTLS(*cert, *key)
}
