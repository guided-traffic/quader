//go:build chart

package chart_test

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The accounts of the registries of the built-in project upstream in the chart (R-HA-073, repositories-and-proxy.md R-REPO-077,
// Q-HA-493, Q-HA-494): secrets.upstreamAccounts projects existing Secrets, optional, into the pods of api, packages and worker and
// renders config.repositories.upstream_accounts with the files' paths and the Secrets' names; nothing else mounts them and no value is
// rendered.

// accountsVolume is the projected volume of the accounts in a pod spec, nil without one.
func accountsVolume(pod any) any {
	for _, v := range list(pod, "volumes") {
		if str(v, "name") == "upstream-accounts" {
			return v
		}
	}
	return nil
}

// podOf is the pod spec of a Deployment, a Job or a CronJob.
func podOf(d doc) any {
	if str(d, "kind") == "CronJob" {
		return at(d, "spec", "jobTemplate", "spec", "template", "spec")
	}
	return at(d, "spec", "template", "spec")
}

func TestR_HA_073_TheUpstreamAccountsReachTheFetchingPodsAlone(t *testing.T) {
	accounts := []string{
		"secrets.upstreamAccounts.docker-hub.existingSecret=quader-upstream-docker-hub", "secrets.upstreamAccounts.docker-hub.passwordKey=token",
		"secrets.upstreamAccounts.npmjs.existingSecret=quader-upstream-npmjs", "secrets.upstreamAccounts.npmjs.authMode=bearer",
	}
	pools := []string{filepath.Join(chartDir, "ci", "roles-pools.yaml")}

	t.Run("every Deployment of api, packages and worker mounts one projected volume, optional, with items, 0440, read-only and without subPath", func(t *testing.T) {
		out, docs := render(t, pools, accounts...)
		var mounted []string
		for _, d := range append(ofKind(docs, "Deployment"), append(ofKind(docs, "Job"), ofKind(docs, "CronJob")...)...) {
			name, pod := str(d, "metadata", "name"), podOf(d)
			args := containerArgs(pod)
			fetches := str(d, "kind") == "Deployment" && len(args) == 3 && slices.Contains([]string{"api", "packages", "worker"}, args[2])
			vol := accountsVolume(pod)
			if !fetches {
				if vol != nil || slices.Contains(mounts(container(pod, "quader")), "upstream-accounts:/var/run/secrets/quader/upstream-accounts") {
					t.Errorf("%s %s mounts the upstream accounts", str(d, "kind"), name)
				}
				continue
			}
			mounted = append(mounted, name)
			if vol == nil {
				t.Fatalf("%s does not mount the upstream accounts", name)
			}
			if at(vol, "projected", "defaultMode") != 288 {
				t.Errorf("%s: defaultMode %v", name, at(vol, "projected", "defaultMode"))
			}
			var sources []string
			for _, s := range list(vol, "projected", "sources") {
				items := []string{}
				for _, it := range list(s, "secret", "items") {
					items = append(items, str(it, "key")+">"+str(it, "path"))
				}
				if at(s, "secret", "optional") != true {
					t.Errorf("%s: the source %v is not optional", name, s)
				}
				sources = append(sources, str(s, "secret", "name")+"["+strings.Join(items, ",")+"]")
			}
			want := []string{"quader-upstream-docker-hub[username>docker-hub/username,token>docker-hub/password]", "quader-upstream-npmjs[password>npmjs/password]"}
			if !reflect.DeepEqual(sources, want) {
				t.Errorf("%s: sources %v, want %v", name, sources, want)
			}
			for _, m := range list(container(pod, "quader"), "volumeMounts") {
				if str(m, "name") == "upstream-accounts" &&
					(str(m, "mountPath") != "/var/run/secrets/quader/upstream-accounts" || at(m, "readOnly") != true || str(m, "subPath") != "") {
					t.Errorf("%s: the mount %v", name, m)
				}
			}
			for _, e := range list(container(pod, "quader"), "env") {
				if strings.Contains(str(e, "valueFrom", "secretKeyRef", "name"), "quader-upstream") {
					t.Errorf("%s: the Secret becomes the variable %s", name, str(e, "name"))
				}
			}
		}
		slices.Sort(mounted)
		wantMounted := []string{
			"quader-api", "quader-api-admin", "quader-api-ep-customers", "quader-packages", "quader-packages-ep-customers", "quader-packages-npm",
			"quader-worker", "quader-worker-bulk", "quader-worker-vuln",
		}
		if !reflect.DeepEqual(mounted, wantMounted) {
			t.Errorf("mounted in %v, want %v", mounted, wantMounted)
		}
		if strings.Contains(out, "stringData") || strings.Contains(out, "kind: Secret\n") {
			t.Error("the render holds a Secret: the chart never creates one")
		}
	})

	t.Run("the configuration file and the migration Job's copy name the paths and the Secrets, never a value", func(t *testing.T) {
		_, docs := render(t, required(), accounts...)
		want := map[string]any{
			"docker-hub": map[string]any{
				"auth_mode": "basic", "secret_name": "quader-upstream-docker-hub",
				"username_file": "/var/run/secrets/quader/upstream-accounts/docker-hub/username",
				"password_file": "/var/run/secrets/quader/upstream-accounts/docker-hub/password",
			},
			"npmjs": map[string]any{
				"auth_mode": "bearer", "secret_name": "quader-upstream-npmjs",
				"password_file": "/var/run/secrets/quader/upstream-accounts/npmjs/password",
			},
		}
		if got := at(renderedConfig(t, docs), "repositories", "upstream_accounts"); !reflect.DeepEqual(got, want) {
			t.Errorf("config.repositories.upstream_accounts = %v", got)
		}
		var migrate map[string]any
		if err := yaml.Unmarshal([]byte(str(find(docs, "ConfigMap", "quader-migrate"), "data", "quader.yaml")), &migrate); err != nil {
			t.Fatal(err)
		}
		if got := at(migrate, "repositories", "upstream_accounts"); !reflect.DeepEqual(got, want) {
			t.Errorf("the migration Job's config.repositories.upstream_accounts = %v", got)
		}
		if accountsVolume(podOf(find(docs, "Job", "quader-migrate"))) != nil {
			t.Error("the migration Job mounts the accounts: it reads the configuration only")
		}
	})

	t.Run("without an entry nothing of it renders", func(t *testing.T) {
		_, docs := render(t, required())
		for _, d := range ofKind(docs, "Deployment") {
			if accountsVolume(podOf(d)) != nil {
				t.Errorf("%s mounts the accounts", str(d, "metadata", "name"))
			}
		}
		if at(renderedConfig(t, docs), "repositories", "upstream_accounts") != nil {
			t.Error("the configuration holds upstream_accounts")
		}
	})

	t.Run("the three refusals: the key under config, a username key with bearer, a key that is no repository key", func(t *testing.T) {
		renderFails(t, required(), []string{"config.repositories.upstream_accounts.docker-hub.password_file=/x"},
			"config.repositories.upstream_accounts is rendered by the chart")
		renderFails(t, required(), []string{
			"secrets.upstreamAccounts.npmjs.existingSecret=s", "secrets.upstreamAccounts.npmjs.authMode=bearer",
			"secrets.upstreamAccounts.npmjs.usernameKey=user",
		}, "secrets.upstreamAccounts.npmjs.usernameKey is set with authMode bearer")
		renderFails(t, required(), []string{"secrets.upstreamAccounts.Docker_Hub.existingSecret=s"}, "'Docker_Hub' does not match pattern")
		renderFails(t, required(), []string{"secrets.upstreamAccounts.docker-hub.passwordKey=token"}, "existingSecret")
	})
}
