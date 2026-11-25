package config

import "testing"

func TestAPISection(t *testing.T) {
	load := func(extra ...string) (*Loaded, error) {
		return Load(Options{Environ: servableEnv(extra...), Command: CommandServe, ReadFile: fakeFS(nil)})
	}
	t.Run("R-API-010 R-API-014 R-API-016 R-API-017 the defaults of the specification", func(t *testing.T) {
		l, err := load()
		if err != nil {
			t.Fatal(err)
		}
		a := l.Config.API
		if a.PageSizeDefault != 50 || a.PageSizeMax != 500 || a.IdempotencyRetention.Hours() != 24 ||
			a.MaxRequestBodyBytes != MiB || a.MaxConfigurationBodyBytes != 4*MiB || len(a.CORS.AllowedOrigins) != 0 {
			t.Fatalf("%+v", a)
		}
		rl := a.RateLimits
		if !rl.Enabled || rl.ExemptAdmins ||
			rl.Read.RequestsPerSecond != 50 || rl.Read.Burst != 200 ||
			rl.Write.RequestsPerSecond != 10 || rl.Write.Burst != 50 ||
			rl.Anonymous.RequestsPerSecond != 5 || rl.Anonymous.Burst != 20 ||
			rl.PackageWrite.RequestsPerSecond != 10 || rl.PackageWrite.Burst != 1600 {
			t.Fatalf("%+v", rl)
		}
	})
	t.Run("R-API-016 the default burst of package_write holds a push of the largest image on four platforms", func(t *testing.T) {
		l, err := load()
		if err != nil {
			t.Fatal(err)
		}
		// Q-LIM-292: docker push sends three unsafe requests per blob (POST, PATCH, PUT), an image docker builds has at most 127
		// layers and a configuration per platform, and the push ends with a manifest per platform, the index and the token's POST.
		const platforms, layers = 4, 127
		push := platforms*(layers+1)*3 + platforms + 1 + 1
		if b := l.Config.API.RateLimits.PackageWrite.Burst; b < push {
			t.Fatalf("the burst %d does not hold the %d unsafe requests of a push of %d platforms of %d layers", b, push, platforms, layers)
		}
	})
	t.Run("R-API-018 allowed_origins must be exact origins", func(t *testing.T) {
		_, err := load(`QUADER_API_CORS_ALLOWED_ORIGINS=["https://ok.example","*","https://x.example/path","ftp://x"]`)
		msgs := issuesOf(t, err)
		if len(msgs) != 3 {
			t.Fatalf("%d issues: %v", len(msgs), msgs)
		}
		containsAll(t, msgs, "api.cors.allowed_origins")
	})
	t.Run("R-API-010 page_size_default must not exceed page_size_max", func(t *testing.T) {
		_, err := load("QUADER_API_PAGE_SIZE_DEFAULT=600")
		containsAll(t, issuesOf(t, err), "api.page_size_default")
	})
}
