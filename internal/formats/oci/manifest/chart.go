package manifest

import (
	"slices"

	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// The reader of a Helm chart's configuration (formats/oci.md R-OCI-042): `helm push` stores the chart's Chart.yaml as JSON as the
// config blob of the media type MediaTypeHelmConfig. It is read like an image's configuration (config.go): the strict reader, a key
// matched as Go's decoder in Helm matches it, the bounds of a string and of the kept JSON, and lists bounded here.

// The media types of a Helm chart in a registry (Helm's registry client).
const (
	MediaTypeHelmConfig  = "application/vnd.cncf.helm.config.v1+json"
	MediaTypeHelmContent = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	MediaTypeHelmProv    = "application/vnd.cncf.helm.chart.provenance.v1.prov"
)

// The bounds of a chart's lists.
const (
	maxChartKeywords     = 64
	maxChartSources      = 32
	maxChartMaintainers  = 64
	maxChartDependencies = 128
	maxChartAnnotations  = 128
	chartBudget          = 64 << 10
)

func (r *configReader) chart(root node) (*rows.ChartConfig, bool) {
	if root.kind != kindObject {
		return nil, false
	}
	c := &rows.ChartConfig{}
	obj := root.obj
	for _, s := range []struct {
		dst  *string
		name string
	}{
		{&c.APIVersion, "apiVersion"},
		{&c.Name, "name"},
		{&c.Version, "version"},
		{&c.AppVersion, "appVersion"},
		{&c.Description, "description"},
		{&c.Type, "type"},
		{&c.KubeVersion, "kubeVersion"},
		{&c.Home, "home"},
		{&c.Icon, "icon"},
	} {
		v, ok := r.str(obj, s.name)
		if !ok {
			return nil, false
		}
		*s.dst = v
	}
	if n, found, ok := field(obj, "deprecated"); !ok || (found && n.kind != kindBool) {
		return nil, false
	} else if found {
		c.Deprecated = n.b
	}
	var ok bool
	if c.Keywords, ok = r.strList(obj, "keywords", maxChartKeywords); !ok {
		return nil, false
	}
	if c.Sources, ok = r.strList(obj, "sources", maxChartSources); !ok {
		return nil, false
	}
	if c.Maintainers, ok = r.maintainers(obj); !ok {
		return nil, false
	}
	if c.Dependencies, ok = r.dependencies(obj); !ok {
		return nil, false
	}
	if c.Annotations, ok = r.chartAnnotations(obj); !ok {
		return nil, false
	}
	return c, true
}

// objects reads a list of objects of a chart, at most limit, each through fn; ok false for a member of another shape.
func (r *configReader) objects(obj map[string]node, name string, limit int, fn func(map[string]node) bool) bool {
	n, found, ok := field(obj, name)
	if !ok || (found && n.kind != kindArray) {
		return false
	}
	for i, e := range n.arr {
		if e.kind != kindObject {
			return false
		}
		if i >= limit {
			r.omitted.Other += len(n.arr) - i
			break
		}
		if !fn(e.obj) {
			return false
		}
	}
	return true
}

func (r *configReader) maintainers(obj map[string]node) ([]rows.ChartMaintainer, bool) {
	var out []rows.ChartMaintainer
	ok := r.objects(obj, "maintainers", maxChartMaintainers, func(m map[string]node) bool {
		var x rows.ChartMaintainer
		for _, s := range []struct {
			dst  *string
			name string
		}{{&x.Name, "name"}, {&x.Email, "email"}, {&x.URL, "url"}} {
			v, fine := r.str(m, s.name)
			if !fine {
				return false
			}
			*s.dst = v
		}
		out = append(out, x)
		return true
	})
	return out, ok
}

func (r *configReader) dependencies(obj map[string]node) ([]rows.ChartDependency, bool) {
	var out []rows.ChartDependency
	ok := r.objects(obj, "dependencies", maxChartDependencies, func(m map[string]node) bool {
		var x rows.ChartDependency
		for _, s := range []struct {
			dst  *string
			name string
		}{{&x.Name, "name"}, {&x.Version, "version"}, {&x.Repository, "repository"}, {&x.Condition, "condition"}, {&x.Alias, "alias"}} {
			v, fine := r.str(m, s.name)
			if !fine {
				return false
			}
			*s.dst = v
		}
		out = append(out, x)
		return true
	})
	return out, ok
}

func (r *configReader) chartAnnotations(obj map[string]node) ([]rows.Var, bool) {
	n, found, ok := field(obj, "annotations")
	if !ok || (found && n.kind != kindObject) {
		return nil, false
	}
	if !found {
		return nil, true
	}
	ks := make([]string, 0, len(n.obj))
	for k, v := range n.obj {
		if v.kind != kindString && v.kind != kindNull {
			return nil, false
		}
		ks = append(ks, k)
	}
	slices.Sort(ks)
	var out []rows.Var
	for i, k := range ks {
		name, _ := r.clip(k)
		value, cut := r.clip(n.obj[k].s)
		if i >= maxChartAnnotations || !r.take(name, value) {
			r.omitted.Labels += len(ks) - i
			break
		}
		out = append(out, rows.Var{Name: name, Value: value, Cut: cut})
	}
	return out, true
}
