//go:build chart

package chart_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The cut into the roles api, packages and worker (R-HA-002, R-HA-028, R-HA-032, R-HA-037, R-HA-042, R-HA-062,
// R-EP-008, ADR-154, ADR-155): every rendered object per role, kept apart by name, label and selector, the edge's
// path rules and the pools.

func rolesSet() []string { return []string{filepath.Join(chartDir, "ci", "roles-pools.yaml")} }

// matches reports whether the labels satisfy a matchLabels selector.
func matches(selector, labels any) bool {
	sel, _ := selector.(map[string]any)
	lab, _ := labels.(map[string]any)
	if len(sel) == 0 {
		return false
	}
	for k, v := range sel {
		if lab[k] != v {
			return false
		}
	}
	return true
}

// podsSelectedBy names the Deployments whose pods carry the labels of the selector.
func podsSelectedBy(docs []doc, selector any) []string {
	var out []string
	for _, d := range ofKind(docs, "Deployment") {
		if matches(selector, at(d, "spec", "template", "metadata", "labels")) {
			out = append(out, str(d, "metadata", "name"))
		}
	}
	sort.Strings(out)
	return out
}

func deploymentNames(docs []doc) []string { return names(docs, "Deployment") }

// names lists the names of the rendered objects of a kind, sorted.
func names(docs []doc, kind string) []string {
	var out []string
	for _, d := range ofKind(docs, kind) {
		out = append(out, str(d, "metadata", "name"))
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRolesDeployments(t *testing.T) {
	t.Run("R-HA-002 R-HA-062 a Deployment, PodDisruptionBudget and NetworkPolicy per role, pool and endpoint pool, named alike", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		want := []string{
			"quader-api", "quader-api-admin", "quader-api-ep-customers", "quader-packages", "quader-packages-ep-customers",
			"quader-packages-npm", "quader-scanner", "quader-worker", "quader-worker-bulk", "quader-worker-vuln",
		}
		if got := deploymentNames(docs); !equalStrings(got, want) {
			t.Fatalf("Deployments = %v, want %v", got, want)
		}
		for _, kind := range []string{"PodDisruptionBudget", "NetworkPolicy"} {
			for _, n := range want {
				if find(docs, kind, n) == nil {
					t.Errorf("%s %s not rendered", kind, n)
				}
			}
		}
		// the autoscaler of the packages Deployment (autoscaling.enabled) is inherited by its pools
		for _, n := range []string{"quader-packages", "quader-packages-npm", "quader-packages-ep-customers"} {
			if find(docs, "HorizontalPodAutoscaler", n) == nil {
				t.Errorf("HorizontalPodAutoscaler %s not rendered", n)
			}
			if at(find(docs, "Deployment", n), "spec", "replicas") != nil {
				t.Errorf("Deployment %s keeps spec.replicas next to its autoscaler", n)
			}
		}
		for _, n := range []string{"quader-api", "quader-worker"} {
			if find(docs, "HorizontalPodAutoscaler", n) != nil {
				t.Errorf("HorizontalPodAutoscaler %s rendered without autoscaling.enabled", n)
			}
			if at(find(docs, "Deployment", n), "spec", "replicas") == nil {
				t.Errorf("Deployment %s has no replicas without an autoscaler", n)
			}
		}
	})

	t.Run("R-HA-028 the component label of every pod names its role and the pool label its pool, selectors never overlap", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		wantPool := map[string]string{
			"quader-api": "", "quader-packages": "", "quader-worker": "",
			"quader-api-admin": "admin", "quader-packages-npm": "npm", "quader-worker-vuln": "vuln", "quader-worker-bulk": "bulk",
			"quader-api-ep-customers": "ep-customers", "quader-packages-ep-customers": "ep-customers",
		}
		for _, d := range ofKind(docs, "Deployment") {
			name := str(d, "metadata", "name")
			role, _, _ := strings.Cut(strings.TrimPrefix(name, "quader-"), "-")
			labels := at(d, "spec", "template", "metadata", "labels")
			if got := str(labels, "app.kubernetes.io/component"); got != role {
				t.Errorf("%s component = %q, want %q", name, got, role)
			}
			if role == "scanner" {
				// The scanner has no pools; quader/pool belongs to the serving roles only (R-HA-028, R-HA-065).
				if at(labels, "quader/pool") != nil {
					t.Errorf("%s carries quader/pool = %v, which only the pods of api, packages and worker do", name, at(labels, "quader/pool"))
				}
			} else if v, ok := at(labels, "quader/pool").(string); !ok || v != wantPool[name] {
				t.Errorf("%s quader/pool = %v, want %q", name, at(labels, "quader/pool"), wantPool[name])
			}
			if str(labels, "quader/serve") != "true" {
				t.Errorf("%s pods are not labelled quader/serve", name)
			}
			// the selector selects the pods of this Deployment and of no other
			if got := podsSelectedBy(docs, at(d, "spec", "selector", "matchLabels")); !equalStrings(got, []string{name}) {
				t.Errorf("the selector of %s selects the pods of %v", name, got)
			}
			if pdb := find(docs, "PodDisruptionBudget", name); !equalMaps(at(pdb, "spec", "selector", "matchLabels"), at(d, "spec", "selector", "matchLabels")) {
				t.Errorf("the PodDisruptionBudget of %s selects other pods than the Deployment", name)
			}
			if np := find(docs, "NetworkPolicy", name); !equalMaps(at(np, "spec", "podSelector", "matchLabels"), at(d, "spec", "selector", "matchLabels")) {
				t.Errorf("the NetworkPolicy of %s selects other pods than the Deployment", name)
			}
		}
	})

	t.Run("R-HA-002 R-HA-062 each Deployment runs the --role of its name; a worker claims queues by environment", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		for _, d := range ofKind(docs, "Deployment") {
			name := str(d, "metadata", "name")
			role, _, _ := strings.Cut(strings.TrimPrefix(name, "quader-"), "-")
			pod := at(d, "spec", "template", "spec")
			if got := strings.Join(containerArgs(pod), " "); got != "serve --role "+role {
				t.Errorf("%s args = %q, want serve --role %s", name, got, role)
			}
			env := envByName(pod)
			want := map[string]string{
				"quader-worker":      "QUADER_JOBS_EXCLUDE_QUEUES=" + `["index","mirror","vuln"]`,
				"quader-worker-vuln": "QUADER_JOBS_QUEUES=" + `["vuln"]`,
				"quader-worker-bulk": "QUADER_JOBS_QUEUES=" + `["index","mirror"]`,
			}[name]
			for _, v := range []string{"QUADER_JOBS_QUEUES", "QUADER_JOBS_EXCLUDE_QUEUES"} {
				got := ""
				if env[v] != nil {
					got = v + "=" + str(env[v], "value")
				}
				if strings.HasPrefix(want, v+"=") {
					if got != want {
						t.Errorf("%s: %s, want %s", name, got, want)
					}
				} else if got != "" {
					t.Errorf("%s sets %s, want it unset", name, got)
				}
			}
		}
	})

	t.Run("R-HA-062 the queue lists travel as JSON lists, which the loader reads as one key", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		for name, want := range map[string]string{"quader-worker-vuln": "QUADER_JOBS_QUEUES", "quader-worker": "QUADER_JOBS_EXCLUDE_QUEUES"} {
			var l []string
			if err := json.Unmarshal([]byte(str(envByName(at(find(docs, "Deployment", name), "spec", "template", "spec"))[want], "value")), &l); err != nil || len(l) == 0 {
				t.Errorf("%s: %s is not a JSON list: %v", name, want, err)
			}
		}
		// without a worker pool the main worker excludes nothing and claims every queue
		_, docs = render(t, required())
		env := envByName(at(find(docs, "Deployment", "quader-worker"), "spec", "template", "spec"))
		if env["QUADER_JOBS_QUEUES"] != nil || env["QUADER_JOBS_EXCLUDE_QUEUES"] != nil {
			t.Error("the main worker restricts its queues although no worker pool exists")
		}
	})

	t.Run("R-HA-002 a pool inherits the role's values and overrides them key by key", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		if at(find(docs, "Deployment", "quader-worker-vuln"), "spec", "replicas") != 2 {
			t.Errorf("the vuln pool replicas = %v, want its own 2", at(find(docs, "Deployment", "quader-worker-vuln"), "spec", "replicas"))
		}
		if at(find(docs, "Deployment", "quader-worker-bulk"), "spec", "replicas") != 2 {
			t.Errorf("the bulk pool replicas = %v, want the worker value 2", at(find(docs, "Deployment", "quader-worker-bulk"), "spec", "replicas"))
		}
		res := at(find(docs, "Deployment", "quader-api-admin"), "spec", "template", "spec", "containers", "0", "resources")
		if str(res, "limits", "memory") != "1Gi" || str(res, "requests", "cpu") != "500m" {
			t.Errorf("the api pool resources = %v, want the api values", res)
		}
	})

	t.Run("R-HA-016 R-HA-032 the migration Job counts the replicas at their maximum with the surge", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		env := envByName(at(find(docs, "Job", "quader-migrate"), "spec", "template", "spec"))
		// api 2+1, api pool admin 2+1, packages 8+1 (autoscaler), pool npm 8+1 and the endpoint pools packages 8+1 and api 2+1
		if got := str(env["QUADER_DATABASE_PREFLIGHT_EXPECTED_SERVING_REPLICAS"], "value"); got != "36" {
			t.Errorf("serving replicas = %q, want 36", got)
		}
		// worker 2+1, vuln pool 2+1, bulk pool 2+1
		if got := str(env["QUADER_DATABASE_PREFLIGHT_EXPECTED_WORKER_REPLICAS"], "value"); got != "9" {
			t.Errorf("worker replicas = %q, want 9", got)
		}
	})

	t.Run("R-HA-016 the notes print the connection budget of the defaults (231)", func(t *testing.T) {
		got := notesOf(t, required())
		if !strings.Contains(got, "max_connections of at least 231") {
			t.Errorf("notes lack the budget 231:\n%s", got)
		}
	})
}

func TestRolesServices(t *testing.T) {
	t.Run("R-HA-002 R-HA-062 each Service selects the pods of one Deployment; the ops Service every serving pod", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		want := map[string][]string{
			"quader-api":              {"quader-api"},
			"quader-packages":         {"quader-packages"},
			"quader-api-admin":        {"quader-api-admin"},
			"quader-packages-npm":     {"quader-packages-npm"},
			"quader-ep-customers":     {"quader-packages-ep-customers"},
			"quader-api-ep-customers": {"quader-api-ep-customers"},
			"quader-scanner":          {"quader-scanner"},
			"quader-packages-scan":    {"quader-packages"}, // the main packages pods only, which serve the scan listener (R-HA-066)
			"quader-ops":              deploymentNames(docs),
		}
		for _, s := range ofKind(docs, "Service") {
			name := str(s, "metadata", "name")
			exp, ok := want[name]
			if !ok {
				t.Errorf("unexpected Service %s", name)
				continue
			}
			if got := podsSelectedBy(docs, at(s, "spec", "selector")); !equalStrings(got, exp) {
				t.Errorf("Service %s selects the pods of %v, want %v", name, got, exp)
			}
			if str(s, "spec", "sessionAffinity") != "None" {
				t.Errorf("Service %s sessionAffinity = %q", name, str(s, "spec", "sessionAffinity"))
			}
			delete(want, name)
		}
		for name := range want {
			t.Errorf("Service %s not rendered", name)
		}
	})

	t.Run("R-HA-002 the role Services and the pool Services open port http 8080, the endpoint Services their port", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		for _, n := range []string{"quader-api", "quader-packages", "quader-api-admin", "quader-packages-npm"} {
			s := find(docs, "Service", n)
			if str(s, "spec", "ports", "0", "name") != "http" || at(s, "spec", "ports", "0", "port") != 8080 || str(s, "spec", "ports", "0", "targetPort") != "http" {
				t.Errorf("Service %s ports = %v", n, at(s, "spec", "ports"))
			}
		}
		for _, n := range []string{"quader-ep-customers", "quader-api-ep-customers"} {
			s := find(docs, "Service", n)
			if at(s, "spec", "ports", "0", "port") != 8443 || str(s, "spec", "ports", "0", "name") != "ep-customers" {
				t.Errorf("Service %s ports = %v", n, at(s, "spec", "ports"))
			}
		}
	})

	t.Run("R-HA-028 containers open exactly the ports of their role, pool and endpoints", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		want := map[string][]int{
			"quader-api": {8080, 8081}, "quader-packages": {8080, 8082, 8081}, "quader-api-admin": {8080, 8081}, "quader-packages-npm": {8080, 8081},
			"quader-worker": {8081}, "quader-worker-vuln": {8081}, "quader-worker-bulk": {8081}, "quader-scanner": {8080, 8081},
			"quader-api-ep-customers": {8443, 8081}, "quader-packages-ep-customers": {8443, 8081},
		}
		for n, ports := range want {
			got := containerPorts(at(find(docs, "Deployment", n), "spec", "template", "spec"))
			if len(got) != len(ports) {
				t.Errorf("%s ports = %v, want %v", n, got, ports)
				continue
			}
			for _, p := range ports {
				if !hasInt(got, p) {
					t.Errorf("%s ports = %v, want %v", n, got, ports)
				}
			}
		}
	})
}

// edgeRules lists path -> Service of the first rule of every host of an Ingress.
func edgeRules(ing any) map[string]string {
	out := map[string]string{}
	for _, p := range list(ing, "spec", "rules", "0", "http", "paths") {
		if str(p, "pathType") != "Prefix" {
			out["!pathType:"+str(p, "path")] = str(p, "pathType")
		}
		out[str(p, "path")] = str(p, "backend", "service", "name")
	}
	return out
}

func TestRolesEdge(t *testing.T) {
	t.Run("R-HA-062 Q-HA-90 a pool with routed false renders without its route; the route follows when the key goes", func(t *testing.T) {
		sets := []string{"ingress.enabled=true", "ingress.backendTLS.caSecret=quader-tls", "packages.pools[0].name=npm", "packages.pools[0].paths={/api/npm}"}
		_, docs := render(t, required(), append(sets, "packages.pools[0].routed=false")...)
		if find(docs, "Deployment", "quader-packages-npm") == nil {
			t.Error("the pool Deployment is not rendered with routed: false")
		}
		if got := edgeRules(find(docs, "Ingress", "quader")); got["/api/npm"] != "" || len(got) != 6 {
			t.Errorf("rules with routed: false = %v, want the six fixed rules", got)
		}
		_, docs = render(t, required(), sets...)
		if got := edgeRules(find(docs, "Ingress", "quader")); got["/api/npm"] != "quader-packages-npm" {
			t.Errorf("rules without routed = %v, want /api/npm to quader-packages-npm", got)
		}
	})

	t.Run("R-EP-008 an endpoint named cust over both roles and an endpoint named cust-api name different Services", func(t *testing.T) {
		_, docs := render(t, required(),
			"endpoints.cust.port=8443", "endpoints.cust.baseUrl=https://a.example.com", "endpoints.cust.surface={packages,api}",
			"endpoints.cust-api.port=8444", "endpoints.cust-api.baseUrl=https://b.example.com", "endpoints.cust-api.surface={packages}")
		seen := map[string]bool{}
		for _, s := range ofKind(docs, "Service") {
			n := str(s, "metadata", "name")
			if seen[n] {
				t.Errorf("two Services are named %s", n)
			}
			seen[n] = true
		}
		for n, want := range map[string][]string{
			"quader-ep-cust": {"quader-packages"}, "quader-api-ep-cust": {"quader-api"}, "quader-ep-cust-api": {"quader-packages"},
		} {
			s := find(docs, "Service", n)
			if s == nil {
				t.Errorf("Service %s not rendered", n)
				continue
			}
			if got := podsSelectedBy(docs, at(s, "spec", "selector")); !equalStrings(got, want) {
				t.Errorf("Service %s selects %v, want %v", n, got, want)
			}
		}
	})

	t.Run("R-HA-037 each host is split by the six fixed Prefix rules between the role Services", func(t *testing.T) {
		_, docs := render(t, required(), "ingress.enabled=true", "ingress.backendTLS.caSecret=quader-tls", "ingress.hosts={quader.example.com,quader.internal.example.com}")
		ing := find(docs, "Ingress", "quader")
		want := map[string]string{
			"/": "quader-api", "/api/v1": "quader-api",
			"/api": "quader-packages", "/files": "quader-packages", "/v2": "quader-packages", "/.well-known": "quader-packages",
		}
		if len(list(ing, "spec", "rules")) != 2 {
			t.Fatalf("%d host rules, want 2", len(list(ing, "spec", "rules")))
		}
		for i := range list(ing, "spec", "rules") {
			got := map[string]string{}
			for _, p := range list(ing, "spec", "rules", string(rune('0'+i)), "http", "paths") {
				got[str(p, "path")] = str(p, "backend", "service", "name")
				if str(p, "pathType") != "Prefix" || str(p, "backend", "service", "port", "name") != "http" {
					t.Errorf("rule %v is not a Prefix rule to port http", p)
				}
			}
			if len(got) != len(want) {
				t.Errorf("host %d rules = %v, want %v", i, got, want)
			}
			for p, s := range want {
				if got[p] != s {
					t.Errorf("host %d: %s -> %q, want %q", i, p, got[p], s)
				}
			}
		}
	})

	t.Run("R-HA-062 the paths of the path pools route to their own Services, before the role's prefixes", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		got := edgeRules(find(docs, "Ingress", "quader"))
		for p, s := range map[string]string{
			"/api/v1/repositories": "quader-api-admin", "/api/npm": "quader-packages-npm",
			"/": "quader-api", "/api/v1": "quader-api", "/api": "quader-packages", "/files": "quader-packages", "/v2": "quader-packages", "/.well-known": "quader-packages",
		} {
			if got[p] != s {
				t.Errorf("%s -> %q, want %q", p, got[p], s)
			}
		}
		if len(got) != 8 {
			t.Errorf("rules = %v, want the six fixed rows and two pool paths", got)
		}
		paths := list(find(docs, "Ingress", "quader"), "spec", "rules", "0", "http", "paths")
		if str(paths[0], "path") != "/api/v1/repositories" || str(paths[1], "path") != "/api/npm" {
			t.Errorf("the pool paths do not come first: %v", paths)
		}
	})

	t.Run("R-EP-008 a further endpoint within one role is routed whole, one over both roles by the rows of its families", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		got := edgeRules(find(docs, "Ingress", "quader-ep-customers"))
		want := map[string]string{
			"/api/v1": "quader-api-ep-customers", "/": "quader-api-ep-customers",
			"/api": "quader-ep-customers", "/files": "quader-ep-customers", "/v2": "quader-ep-customers", "/.well-known": "quader-ep-customers",
		}
		if len(got) != len(want) {
			t.Errorf("customers rules = %v, want %v", got, want)
		}
		for p, s := range want {
			if got[p] != s {
				t.Errorf("customers %s -> %q, want %q", p, got[p], s)
			}
		}
		for _, p := range list(find(docs, "Ingress", "quader-ep-customers"), "spec", "rules", "0", "http", "paths") {
			if at(p, "backend", "service", "port", "number") != 8443 {
				t.Errorf("customers rule %v does not name the endpoint's port", p)
			}
		}
		// packages only: the whole host to the one Service
		_, docs = render(t, poolSet(), "endpoints.customers.ingress.backendTLS.caSecret=x")
		if got := edgeRules(find(docs, "Ingress", "quader-ep-customers")); len(got) != 1 || got["/"] != "quader-ep-customers" {
			t.Errorf("a packages-only endpoint rules = %v, want / to quader-ep-customers", got)
		}
		// an api-only surface is served by api pods
		_, docs = render(t, required(), "endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com", "endpoints.c.surface={api}")
		if got := podsSelectedBy(docs, at(find(docs, "Service", "quader-ep-c"), "spec", "selector")); !equalStrings(got, []string{"quader-api"}) {
			t.Errorf("an api-only endpoint is served by %v, want quader-api", got)
		}
		if find(docs, "Service", "quader-api-ep-c") != nil {
			t.Error("a second Service is rendered for a surface within one role")
		}
	})

	t.Run("R-EP-008 a surface over both roles opens its port on the main Deployment of each", func(t *testing.T) {
		_, docs := render(t, required(), "endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com", "endpoints.c.surface={packages,files,api}")
		for _, n := range []string{"quader-api", "quader-packages"} {
			if got := containerPorts(at(find(docs, "Deployment", n), "spec", "template", "spec")); !hasInt(got, 8443) {
				t.Errorf("%s ports = %v, want 8443", n, got)
			}
			found := false
			for _, r := range list(find(docs, "NetworkPolicy", n), "spec", "ingress") {
				if p, _, _ := portsOf(r); hasInt(p, 8443) {
					found = true
				}
			}
			if !found {
				t.Errorf("the policy of %s has no rule for 8443", n)
			}
		}
	})
}

func TestRolesNetworkPolicy(t *testing.T) {
	t.Run("R-HA-042 every policy of every value set is ingress only, every rule names one port and never an egress key", func(t *testing.T) {
		eachSet(t, func(t *testing.T, _ string, out string, docs []doc) {
			for _, np := range ofKind(docs, "NetworkPolicy") {
				name := str(np, "metadata", "name")
				if pt := list(np, "spec", "policyTypes"); len(pt) != 1 || pt[0] != "Ingress" {
					t.Errorf("%s policyTypes = %v", name, pt)
				}
				if at(np, "spec", "egress") != nil {
					t.Errorf("%s has an egress key", name)
				}
				for i, r := range list(np, "spec", "ingress") {
					if p, _, _ := portsOf(r); len(p) != 1 {
						t.Errorf("%s rule %d names ports %v, want exactly one", name, i, p)
					}
					if l, ok := at(r, "from").([]any); ok && len(l) == 0 {
						t.Errorf("%s rule %d has an empty from, which admits every peer", name, i)
					}
				}
				// the policies of the chart select its own pods only
				if sel, _ := at(np, "spec", "podSelector", "matchLabels").(map[string]any); sel["app.kubernetes.io/instance"] == nil || sel["app.kubernetes.io/component"] == nil {
					t.Errorf("%s selects pods by %v", name, sel)
				}
			}
			if strings.Contains(out, "Egress") || strings.Contains(out, "egress:") {
				t.Error("the rendered output mentions egress")
			}
		})
	})

	t.Run("R-HA-042 each policy admits the ports its pods open and no other, a pool's own 8080 and an endpoint pool's none", func(t *testing.T) {
		_, docs := render(t, rolesSet())
		want := map[string][]int{
			// The main packages pods open the scan listener's 8082, admitted from the scanner's pods only; the scanner's own policy
			// admits its 8080 from one sender too: TestR_HA_042_TheLinksAdmitTheirOneSender.
			"quader-api": {8080, 8081}, "quader-packages": {8080, 8082, 8081}, "quader-api-admin": {8080, 8081}, "quader-packages-npm": {8080, 8081},
			"quader-worker": {8081}, "quader-worker-vuln": {8081}, "quader-worker-bulk": {8081},
			"quader-api-ep-customers": {8443, 8081}, "quader-packages-ep-customers": {8443, 8081},
			"quader-migrate": nil,
		}
		for n, ports := range want {
			var got []int
			for _, r := range list(find(docs, "NetworkPolicy", n), "spec", "ingress") {
				p, from, has := portsOf(r)
				got = append(got, p...)
				switch {
				case len(p) == 1 && p[0] == 8081:
					if !has || len(from) == 0 {
						t.Errorf("%s: 8081 without named peers", n)
					}
				case len(p) == 1 && p[0] == 8443:
					if !has || str(from[0], "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name") != "ingress" {
						t.Errorf("%s: 8443 from %v, want the endpoint's networkPolicy.from", n, from)
					}
				case len(p) == 1 && p[0] == 8080:
					if has {
						t.Errorf("%s: 8080 has a from, want from anywhere by default", n)
					}
				case len(p) == 1 && p[0] == 8082:
					if !has || len(from) != 1 || str(from[0], "podSelector", "matchLabels", "app.kubernetes.io/component") != "scanner" {
						t.Errorf("%s: 8082 from %v, want the scanner's pods only", n, from)
					}
				}
			}
			if len(got) != len(ports) {
				t.Errorf("%s admits %v, want %v", n, got, ports)
				continue
			}
			for _, p := range ports {
				if !hasInt(got, p) {
					t.Errorf("%s admits %v, want %v", n, got, ports)
				}
			}
		}
	})

	t.Run("R-HA-042 the policies of the default render are api, packages, worker, scanner and migrate, and backup with the backup on", func(t *testing.T) {
		_, docs := render(t, required())
		var got []string
		for _, np := range ofKind(docs, "NetworkPolicy") {
			got = append(got, str(np, "metadata", "name"))
		}
		sort.Strings(got)
		if want := []string{"quader-api", "quader-migrate", "quader-packages", "quader-scanner", "quader-worker"}; !equalStrings(got, want) {
			t.Errorf("policies = %v, want %v", got, want)
		}
		if _, d := render(t, required(), backupSets()...); find(d, "NetworkPolicy", "quader-backup") == nil {
			t.Error("no policy quader-backup with backup.enabled: true")
		}
		if r := list(find(docs, "NetworkPolicy", "quader-migrate"), "spec", "ingress"); len(r) != 0 || at(find(docs, "NetworkPolicy", "quader-migrate"), "spec", "ingress") == nil {
			t.Errorf("quader-migrate ingress = %v, want an explicit empty list", r)
		}
	})

	t.Run("R-HA-042 networkPolicy.egress is not a value", func(t *testing.T) {
		renderFails(t, required(), []string{"networkPolicy.egress.enabled=true"}, "egress")
	})
}

func TestRolesValidation(t *testing.T) {
	pools := func(extra ...string) []string { return extra }
	for _, tc := range []struct {
		name string
		sets []string
		want []string
	}{
		{"two pools with the same name", pools("packages.pools[0].name=a", "packages.pools[0].paths={/api/npm}", "packages.pools[1].name=a", "packages.pools[1].paths={/api/nuget}"), []string{"packages.pools", "two pools named a"}},
		{"a pool name that starts with ep-", pools("packages.pools[0].name=ep-x", "packages.pools[0].paths={/api/npm}"), []string{"packages.pools[ep-x].name", "ep-"}},
		{"two pools with the same path prefix", pools("packages.pools[0].name=a", "packages.pools[0].paths={/api/npm}", "packages.pools[1].name=b", "packages.pools[1].paths={/api/npm}"), []string{"packages.pools[b].paths", "/api/npm", "packages.pools[a]"}},
		{"a packages pool path in the api families", pools("packages.pools[0].name=a", "packages.pools[0].paths={/api/v1/system}"), []string{"packages.pools[a].paths", "/api/v1/system"}},
		{"a packages pool path equal to the role's prefix", pools("packages.pools[0].name=a", "packages.pools[0].paths={/api}"), []string{"packages.pools[a].paths", "/api"}},
		{"a packages pool path outside every family", pools("packages.pools[0].name=a", "packages.pools[0].paths={/ui/x}"), []string{"packages.pools[a].paths", "/ui/x"}},
		{"an api pool path in the packages families", pools("api.pools[0].name=a", "api.pools[0].paths={/api/npm}"), []string{"api.pools[a].paths", "/api/npm"}},
		{"an api pool path equal to the role's prefix", pools("api.pools[0].name=a", "api.pools[0].paths={/api/v1}"), []string{"api.pools[a].paths", "/api/v1"}},
		{"a worker pool queue that does not exist", pools("worker.pools[0].name=a", "worker.pools[0].queues={nope}"), []string{"worker.pools[a].queues", "nope"}},
		{"two worker pools with the same queue", pools("worker.pools[0].name=a", "worker.pools[0].queues={vuln}", "worker.pools[1].name=b", "worker.pools[1].queues={vuln}"), []string{"worker.pools[b].queues", "vuln", "pool a"}},
		{"a pool name too long for a Deployment name", pools("fullnameOverride="+strings.Repeat("a", 45), "packages.pools[0].name=abcdefghijklmnopqrst", "packages.pools[0].paths={/api/npm}"), []string{"packages.pools[abcdefghijklmnopqrst].name", "63"}},
		{"a pool below two replicas", pools("worker.pools[0].name=a", "worker.pools[0].queues={vuln}", "worker.pools[0].replicaCount=1"), []string{"worker.pools[a].replicaCount", "quader-worker-a"}},
		{"a pool without a disruption budget", pools("api.pools[0].name=a", "api.pools[0].paths={/ui/x}", "api.pools[0].podDisruptionBudget.enabled=false"), []string{"api.pools[a].podDisruptionBudget.enabled"}},
		{"a pool without a memory limit", pools("packages.pools[0].name=a", "packages.pools[0].paths={/api/npm}", "packages.pools[0].resources.limits.memory="), []string{"packages.pools[a].resources.limits.memory is required"}},
		{"a worker pool of the vuln queue whose spool cannot hold the archive", pools("worker.pools[0].name=v", "worker.pools[0].queues={vuln}", "worker.pools[0].spool.sizeLimit=3Gi", "config.vulnerabilities.sources.osv.max_archive_size=2GiB", "config.vulnerabilities.images.enabled=false"), []string{"max_archive_size", "quader-worker-v", "queue vuln"}},
		{"an autoscaler without a metric", pools("api.autoscaling.enabled=true", "api.autoscaling.targetCPUUtilizationPercentage=0"), []string{"api.autoscaling"}},
		{"an autoscaler whose maximum is below its minimum", pools("packages.autoscaling.enabled=true", "packages.autoscaling.maxReplicas=2"), []string{"packages.autoscaling.maxReplicas", "minReplicas"}},
		{"an autoscaler below two replicas with highAvailability.enforce", pools("api.autoscaling.enabled=true", "api.autoscaling.minReplicas=1"), []string{"api.autoscaling.minReplicas"}},
		{"the upload of the UI larger than the api spool", pools("config.ui.deploy.max_upload_bytes=9663676416"), []string{"config.ui.deploy.max_upload_bytes", "api.spool.sizeLimit"}},
		{"a UI upload limit of 0 falls back to the upload limit, larger than the api spool", pools("config.ui.deploy.max_upload_bytes=0", "spool.sizeLimit=20Gi", "api.spool.sizeLimit=5Gi"), []string{"config.storage.upload.max_size", "api.spool.sizeLimit"}},
		{"networkPolicy egress", pools("networkPolicy.egress.enabled=true"), []string{"egress"}},
		{"worker pools that claim every queue", pools("worker.pools[0].name=a", "worker.pools[0].queues={default,maintenance,index}", "worker.pools[1].name=b", "worker.pools[1].queues={delivery,mirror,vuln}"), []string{"worker.pools", "every queue", "main worker"}},
		{"a worker pool with no replica", pools("highAvailability.enforce=false", "worker.pools[0].name=a", "worker.pools[0].queues={vuln}", "worker.pools[0].replicaCount=0"), []string{"worker.pools[a].replicaCount", "claimed by nobody"}},
		{"a default surface without a family of the role packages and no endpoint served by it", pools("endpoints.default.surface={api,ui}"), []string{"endpoints.default.surface", "role packages"}},
		{"a default surface without a family of the role api and no endpoint served by it", pools("endpoints.default.surface={packages}"), []string{"endpoints.default.surface", "role api"}},
	} {
		t.Run("R-HA-029 R-HA-062 "+tc.name, func(t *testing.T) { renderFails(t, required(), tc.sets, tc.want...) })
	}

	t.Run("R-HA-029 a worker pool below two replicas renders without highAvailability.enforce, and an autoscaler counts by its minimum", func(t *testing.T) {
		render(t, required(), "highAvailability.enforce=false", "worker.pools[0].name=a", "worker.pools[0].queues={vuln}", "worker.pools[0].replicaCount=1")
		render(t, required(), "api.autoscaling.enabled=true", "api.autoscaling.minReplicas=2", "api.replicaCount=1")
	})

	t.Run("R-HA-062 a default surface that leaves a role to a further endpoint, and a worker pool kept by its autoscaler, render", func(t *testing.T) {
		render(t, required(), "endpoints.default.surface={api,ui}", "endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com", "endpoints.c.surface={packages}")
		render(t, required(), "worker.pools[0].name=a", "worker.pools[0].queues={vuln}", "worker.pools[0].replicaCount=0",
			"worker.pools[0].autoscaling.enabled=true", "worker.pools[0].autoscaling.minReplicas=2", "worker.pools[0].autoscaling.targetCPUUtilizationPercentage=70")
	})

	t.Run("R-HA-029 the UI upload limit is checked against the api spool, the package upload limit against the packages spool", func(t *testing.T) {
		render(t, required(), "config.ui.deploy.max_upload_bytes=1073741824")
		render(t, required(), "config.storage.upload.max_size=1GiB", "packages.spool.sizeLimit=4Gi", "api.spool.sizeLimit=4Gi")
		renderFails(t, required(), []string{"packages.spool.sizeLimit=5Gi"}, "config.storage.upload.max_size", "Deployment quader-packages", "packages.spool.sizeLimit")
	})
}

// notesOf renders NOTES.txt through the template quader.notes in a copy of the chart.
func notesOf(t *testing.T, files []string, sets ...string) string {
	t.Helper()
	dir := t.TempDir()
	copyDir(t, chartDir, filepath.Join(dir, "quader"))
	probe := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: notes\ndata:\n  notes: |\n{{ include \"quader.notes\" . | indent 4 }}\n"
	if err := os.WriteFile(filepath.Join(dir, "quader", "templates", "notes-probe.yaml"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"template", "quader", filepath.Join(dir, "quader"), "--show-only", "templates/notes-probe.yaml", "--namespace", "default"}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, stderr, err := helm(t, args...)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	return out
}
