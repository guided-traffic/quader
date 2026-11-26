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

// The Bastion registrations declared in the values (docs/spec/bastion.md R-BAS-127): bastions.declared renders
// config.bastions.declared with the token file's path and the Secret's name, and projects the Secrets, optional, into the migration
// Job alone; no other pod mounts them and no value is rendered.

// bastionTokensVolume is the projected volume of the declared tokens in a pod spec, nil without one.
func bastionTokensVolume(pod any) any {
	for _, v := range list(pod, "volumes") {
		if str(v, "name") == "bastion-tokens" {
			return v
		}
	}
	return nil
}

func TestR_BAS_127_TheDeclaredBastionsReachTheMigrationJobAlone(t *testing.T) {
	declared := []string{
		"bastions.declared[0].name=partners", "bastions.declared[0].description=Partner access",
		"bastions.declared[0].egress[0]=10.252.0.0/16", "bastions.declared[0].token.existingSecret=quader-bastion-token",
		"bastions.declared[1].name=edge", "bastions.declared[1].token.existingSecret=quader-edge", "bastions.declared[1].token.key=value",
	}
	pools := []string{filepath.Join(chartDir, "ci", "roles-pools.yaml")}

	t.Run("only the migration Job mounts one projected volume, optional, with items, 0440, read-only and without subPath", func(t *testing.T) {
		out, docs := render(t, pools, declared...)
		for _, d := range append(ofKind(docs, "Deployment"), append(ofKind(docs, "Job"), ofKind(docs, "CronJob")...)...) {
			name, pod := str(d, "metadata", "name"), podOf(d)
			vol := bastionTokensVolume(pod)
			if name != "quader-migrate" {
				if vol != nil {
					t.Errorf("%s %s mounts the Bastion tokens", str(d, "kind"), name)
				}
				continue
			}
			if vol == nil {
				t.Fatal("the migration Job does not mount the Bastion tokens")
			}
			if at(vol, "projected", "defaultMode") != 288 {
				t.Errorf("defaultMode %v", at(vol, "projected", "defaultMode"))
			}
			var sources []string
			for _, s := range list(vol, "projected", "sources") {
				if at(s, "secret", "optional") != true {
					t.Errorf("the source %v is not optional", s)
				}
				for _, it := range list(s, "secret", "items") {
					sources = append(sources, str(s, "secret", "name")+"["+str(it, "key")+">"+str(it, "path")+"]")
				}
			}
			if want := []string{"quader-bastion-token[token>partners/token]", "quader-edge[value>edge/token]"}; !reflect.DeepEqual(sources, want) {
				t.Errorf("sources %v, want %v", sources, want)
			}
			if !slices.Contains(mounts(container(pod, "migrate")), "bastion-tokens:/var/run/secrets/quader/bastion-tokens") {
				t.Errorf("mounts = %v", mounts(container(pod, "migrate")))
			}
			for _, m := range list(container(pod, "migrate"), "volumeMounts") {
				if str(m, "name") == "bastion-tokens" && (at(m, "readOnly") != true || str(m, "subPath") != "") {
					t.Errorf("the mount %v", m)
				}
			}
		}
		if strings.Contains(out, "stringData") || strings.Contains(out, "kind: Secret\n") {
			t.Error("the render holds a Secret: the chart never creates one")
		}
	})

	t.Run("the configuration file and the migration Job's copy name the paths and the Secrets, never a value", func(t *testing.T) {
		_, docs := render(t, required(), declared...)
		want := map[string]any{
			"partners": map[string]any{
				"description": "Partner access", "egress": []any{"10.252.0.0/16"}, "secret_name": "quader-bastion-token",
				"token_file": "/var/run/secrets/quader/bastion-tokens/partners/token",
			},
			"edge": map[string]any{"secret_name": "quader-edge", "token_file": "/var/run/secrets/quader/bastion-tokens/edge/token"},
		}
		if got := at(renderedConfig(t, docs), "bastions", "declared"); !reflect.DeepEqual(got, want) {
			t.Errorf("config.bastions.declared = %v", got)
		}
		var migrate map[string]any
		if err := yaml.Unmarshal([]byte(str(find(docs, "ConfigMap", "quader-migrate"), "data", "quader.yaml")), &migrate); err != nil {
			t.Fatal(err)
		}
		if got := at(migrate, "bastions", "declared"); !reflect.DeepEqual(got, want) {
			t.Errorf("the migration Job's config.bastions.declared = %v", got)
		}
	})

	t.Run("without an entry nothing of it renders", func(t *testing.T) {
		_, docs := render(t, required())
		if bastionTokensVolume(podOf(find(docs, "Job", "quader-migrate"))) != nil {
			t.Error("the migration Job mounts the Bastion tokens")
		}
		if at(renderedConfig(t, docs), "bastions", "declared") != nil {
			t.Error("the configuration holds bastions.declared")
		}
	})

	t.Run("the refusals: the key under config, a name twice, a name off the rule, an entry without its Secret", func(t *testing.T) {
		renderFails(t, required(), []string{"config.bastions.declared.partners.token_file=/x"},
			"config.bastions.declared is rendered by the chart")
		renderFails(t, required(), []string{
			"bastions.declared[0].name=partners", "bastions.declared[0].token.existingSecret=s",
			"bastions.declared[1].name=partners", "bastions.declared[1].token.existingSecret=s",
		}, `bastions.declared[1].name "partners" is also the name of bastions.declared[0]`)
		renderFails(t, required(), []string{"bastions.declared[0].name=Partners", "bastions.declared[0].token.existingSecret=s"}, "does not match pattern")
		renderFails(t, required(), []string{"bastions.declared[0].name=partners"}, "token")
	})
}
