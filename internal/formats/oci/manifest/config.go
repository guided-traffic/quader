package manifest

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/formats/oci/rows"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// The reader of an image's configuration (formats/oci.md R-OCI-040, Q-OCI-272). The config blob is untrusted content a pusher or an
// upstream wrote, read for the version page and for nothing the registry decides. It is read by the strict reader of the manifests
// (tree.go: UTF-8, every key once, one value), within bounds of its own, and what it keeps is bounded again so that the row stays small:
//
//   - the blob: at most MaxConfigBytes, nested at most configDepth levels, at most configValues JSON values;
//   - a string: at most MaxConfigString bytes, cut at a character boundary and marked;
//   - the lists: MaxConfigEnv environment entries, MaxConfigLabels labels, MaxConfigHistory history lines, MaxConfigArgs arguments of a
//     command, MaxConfigKeys ports or volumes, each counting what it left out;
//   - the whole: what is kept is at most ConfigBudget bytes of JSON, read in the order scalars, commands, ports, labels, environment,
//     history; once the budget is spent, the rest is counted as left out.
//
// A key the runtime reads is matched as Docker's decoder matches it, exactly or, when the document has no such key, without regard to
// case; two keys that only differ in case are ambiguous and the whole configuration is not read (two readers would read two images).
// A value of a type the runtime would refuse makes it unreadable too. Nothing of it is ever logged.

// The media types of an image's configuration.
const (
	MediaTypeImageConfig  = "application/vnd.oci.image.config.v1+json"
	MediaTypeDockerConfig = "application/vnd.docker.container.image.v1+json"
)

// The bounds of the reader.
const (
	MaxConfigBytes   = 1 << 20
	MaxConfigString  = 4 << 10
	MaxConfigEnv     = 256
	MaxConfigLabels  = 256
	MaxConfigHistory = 512
	MaxConfigArgs    = 64
	MaxConfigKeys    = 256
	// ConfigBudget bounds the JSON of what is kept; the column's check (00132) allows 256 KiB, and jsonb prints a separator a byte
	// longer than the reader counts it.
	ConfigBudget = 160 << 10

	configDepth  = 16
	configValues = 200_000
)

// ConfigKind is the kind of configuration a config of the media type is (rows.ConfigImage), "" for one no reader reads: a Helm chart's,
// a signature's, an artifact's own.
func ConfigKind(mediaType string) string {
	switch mediaType {
	case MediaTypeImageConfig, MediaTypeDockerConfig:
		return rows.ConfigImage
	case MediaTypeHelmConfig:
		return rows.ConfigChart
	}
	return ""
}

// ReadConfig reads a configuration blob of the kind ConfigKind named. It never fails: what it cannot read is a ConfigFacts that says
// why.
func ReadConfig(kind string, data []byte) rows.ConfigFacts {
	out := rows.ConfigFacts{Kind: kind}
	if kind != rows.ConfigImage && kind != rows.ConfigChart {
		out.Unreadable = rows.UnreadableInvalid
		return out
	}
	if len(data) > MaxConfigBytes {
		out.Unreadable = rows.UnreadableTooLarge
		return out
	}
	if err := untrusted.CheckJSON(data, untrusted.Limits{JSONDepth: configDepth, JSONMembers: configValues}); err != nil {
		out.Unreadable = unreadable(err)
		return out
	}
	root, err := readTree(data, configDepth, configValues)
	if err != nil {
		out.Unreadable = unreadable(err)
		return out
	}
	if kind == rows.ConfigChart {
		r := &configReader{left: chartBudget}
		c, ok := r.chart(root)
		if !ok {
			out.Unreadable = rows.UnreadableInvalid
			return out
		}
		if r.omitted != (rows.Omitted{}) {
			om := r.omitted
			c.Omitted = &om
		}
		out.Chart = c
		return out
	}
	r := &configReader{left: ConfigBudget}
	img, ok := r.image(root)
	if !ok {
		out.Unreadable = rows.UnreadableInvalid
		return out
	}
	if r.omitted != (rows.Omitted{}) {
		om := r.omitted
		img.Omitted = &om
	}
	out.Image = img
	return out
}

func unreadable(err error) string {
	if untrusted.IsTooLarge(err) {
		return rows.UnreadableTooLarge
	}
	return rows.UnreadableInvalid
}

type configReader struct {
	left    int
	omitted rows.Omitted
}

// field finds the member of an object the runtime reads for name: the exact key, else the one key that equals it without regard to
// case. ok false is an ambiguous object (a key that differs from another one only in case); found false a member that is absent or
// null.
func field(obj map[string]node, name string) (n node, found, ok bool) {
	var match []string
	for k := range obj {
		if strings.EqualFold(k, name) {
			match = append(match, k)
		}
	}
	switch len(match) {
	case 0:
		return node{}, false, true
	case 1:
		n = obj[match[0]]
		return n, n.kind != kindNull, true
	}
	return node{}, false, false
}

// cost is what a string adds to the kept JSON: its quoted form and a separator.
func cost(s string) int { return len(quoteJSON(s)) + 4 }

// take spends the budget for strings; false when it is spent, and nothing is taken.
func (r *configReader) take(ss ...string) bool {
	c := 16
	for _, s := range ss {
		c += cost(s)
	}
	if c > r.left {
		return false
	}
	r.left -= c
	return true
}

// clip bounds a string the configuration holds: at most MaxConfigString bytes, cut at a character boundary, and a NUL (which jsonb
// cannot hold) replaced. cut reports that the end is missing.
func (r *configReader) clip(s string) (out string, cut bool) {
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "�")
	}
	if len(s) <= MaxConfigString {
		return s, false
	}
	end := MaxConfigString
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	r.omitted.Cut = true
	return s[:end], true
}

// str reads a string member; ok false for a member that is no string.
func (r *configReader) str(obj map[string]node, name string) (string, bool) {
	n, found, ok := field(obj, name)
	if !ok {
		return "", false
	}
	if !found {
		return "", true
	}
	if n.kind != kindString {
		return "", false
	}
	s, _ := r.clip(n.s)
	if !r.take(s) {
		r.omitted.Other++
		return "", true
	}
	return s, true
}

// strList reads a list of strings, or a single string as a list of one, as Docker reads a command.
func (r *configReader) strList(obj map[string]node, name string, limit int) ([]string, bool) {
	n, found, ok := field(obj, name)
	if !ok {
		return nil, false
	}
	if !found {
		return nil, true
	}
	items := []node{n}
	switch n.kind {
	case kindString:
	case kindArray:
		items = n.arr
	default:
		return nil, false
	}
	var out []string
	for i, it := range items {
		if it.kind != kindString {
			return nil, false
		}
		s, _ := r.clip(it.s)
		if i >= limit || !r.take(s) {
			r.omitted.Other += len(items) - i
			break
		}
		out = append(out, s)
	}
	return out, true
}

// keys reads the keys of an object of empty objects (ExposedPorts, Volumes), sorted.
func (r *configReader) keys(obj map[string]node, name string) ([]string, bool) {
	n, found, ok := field(obj, name)
	if !ok {
		return nil, false
	}
	if !found {
		return nil, true
	}
	if n.kind != kindObject {
		return nil, false
	}
	ks := make([]string, 0, len(n.obj))
	for k := range n.obj {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	var out []string
	for i, k := range ks {
		s, _ := r.clip(k)
		if i >= MaxConfigKeys || !r.take(s) {
			r.omitted.Other += len(ks) - i
			break
		}
		out = append(out, s)
	}
	return out, true
}

func integerOf(n node) (int64, bool) {
	if n.kind != kindNumber {
		return 0, false
	}
	v, err := strconv.ParseInt(n.s, 10, 64)
	return v, err == nil
}

func (r *configReader) image(root node) (*rows.ImageConfig, bool) {
	if root.kind != kindObject {
		return nil, false
	}
	img := &rows.ImageConfig{}
	ok := true
	scalar := func(dst *string, obj map[string]node, name string) {
		if !ok {
			return
		}
		*dst, ok = r.str(obj, name)
	}
	top := root.obj
	scalar(&img.Created, top, "created")
	scalar(&img.Author, top, "author")
	scalar(&img.OS, top, "os")
	scalar(&img.OSVersion, top, "os.version")
	scalar(&img.Architecture, top, "architecture")
	scalar(&img.Variant, top, "variant")
	if !ok {
		return nil, false
	}
	if cfg, found, fine := field(top, "config"); !fine {
		return nil, false
	} else if found {
		if cfg.kind != kindObject || !r.runConfig(cfg.obj, img) {
			return nil, false
		}
	}
	if h, found, fine := field(top, "history"); !fine {
		return nil, false
	} else if found {
		if h.kind != kindArray || !r.history(h.arr, img) {
			return nil, false
		}
	}
	return img, true
}

func (r *configReader) runConfig(c map[string]node, img *rows.ImageConfig) bool {
	var ok bool
	for _, s := range []struct {
		dst  *string
		name string
	}{{&img.User, "User"}, {&img.WorkingDir, "WorkingDir"}, {&img.StopSignal, "StopSignal"}} {
		if *s.dst, ok = r.str(c, s.name); !ok {
			return false
		}
	}
	for _, l := range []struct {
		dst  *[]string
		name string
	}{{&img.Entrypoint, "Entrypoint"}, {&img.Cmd, "Cmd"}, {&img.Shell, "Shell"}, {&img.OnBuild, "OnBuild"}} {
		if *l.dst, ok = r.strList(c, l.name, MaxConfigArgs); !ok {
			return false
		}
	}
	if img.ExposedPorts, ok = r.keys(c, "ExposedPorts"); !ok {
		return false
	}
	if img.Volumes, ok = r.keys(c, "Volumes"); !ok {
		return false
	}
	if img.Healthcheck, ok = r.healthcheck(c); !ok {
		return false
	}
	if img.Labels, ok = r.labels(c); !ok {
		return false
	}
	img.Env, ok = r.env(c)
	return ok
}

func (r *configReader) healthcheck(c map[string]node) (*rows.Healthcheck, bool) {
	n, found, ok := field(c, "Healthcheck")
	if !ok || (found && n.kind != kindObject) {
		return nil, false
	}
	if !found {
		return nil, true
	}
	h := &rows.Healthcheck{}
	if h.Test, ok = r.strList(n.obj, "Test", MaxConfigArgs); !ok {
		return nil, false
	}
	for _, d := range []struct {
		dst  *int64
		name string
	}{{&h.Interval, "Interval"}, {&h.Timeout, "Timeout"}, {&h.StartPeriod, "StartPeriod"}, {&h.Retries, "Retries"}} {
		v, found, fine := field(n.obj, d.name)
		if !fine {
			return nil, false
		}
		if !found {
			continue
		}
		if *d.dst, fine = integerOf(v); !fine {
			return nil, false
		}
	}
	return h, true
}

func (r *configReader) labels(c map[string]node) ([]rows.Var, bool) {
	n, found, ok := field(c, "Labels")
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
		if i >= MaxConfigLabels || !r.take(name, value) {
			r.omitted.Labels += len(ks) - i
			break
		}
		out = append(out, rows.Var{Name: name, Value: value, Cut: cut})
	}
	return out, true
}

func (r *configReader) env(c map[string]node) ([]rows.Var, bool) {
	n, found, ok := field(c, "Env")
	if !ok || (found && n.kind != kindArray) {
		return nil, false
	}
	if !found {
		return nil, true
	}
	var out []rows.Var
	for i, e := range n.arr {
		if e.kind != kindString {
			return nil, false
		}
		k, v, _ := strings.Cut(e.s, "=")
		name, _ := r.clip(k)
		value, cut := r.clip(v)
		if i >= MaxConfigEnv || !r.take(name, value) {
			r.omitted.Env += len(n.arr) - i
			break
		}
		out = append(out, rows.Var{Name: name, Value: value, Cut: cut})
	}
	return out, true
}

func (r *configReader) history(arr []node, img *rows.ImageConfig) bool {
	for i, h := range arr {
		if h.kind != kindObject {
			return false
		}
		var e rows.HistoryEntry
		for _, s := range []struct {
			dst  *string
			name string
		}{{&e.Created, "created"}, {&e.Comment, "comment"}, {&e.Author, "author"}} {
			n, found, ok := field(h.obj, s.name)
			if !ok || (found && n.kind != kindString) {
				return false
			}
			*s.dst, _ = r.clip(n.s)
		}
		n, found, ok := field(h.obj, "created_by")
		if !ok || (found && n.kind != kindString) {
			return false
		}
		e.CreatedBy, e.Cut = r.clip(n.s)
		if n, found, ok := field(h.obj, "empty_layer"); !ok || (found && n.kind != kindBool) {
			return false
		} else if found {
			e.EmptyLayer = n.b
		}
		if i >= MaxConfigHistory || !r.take(e.Created, e.CreatedBy, e.Comment, e.Author) {
			r.omitted.History += len(arr) - i
			break
		}
		img.History = append(img.History, e)
	}
	return true
}
