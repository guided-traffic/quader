package rows

import "encoding/json"

// The facts of an image (formats/oci.md R-OCI-039, R-OCI-040): what an image manifest names, kept in its order, and what the bounded
// reader of the format read of its configuration. Both are stored with the manifest's row (oci_manifests.blobs and config_facts) and
// read by the version page; they are derived from the bytes of the manifest and of the config blob and change with nothing else.

// BlobDescriptor is a blob an image manifest names: its config or a layer.
type BlobDescriptor struct {
	Digest    string `json:"digest"`
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	// Foreign says the layer is not distributed through a registry (urls, or a non-distributable media type, Q-OCI-10): the
	// repository is not asked to hold it.
	Foreign bool `json:"foreign,omitempty"`
}

// MaxLayers bounds the layers a row keeps; an image with more says how many are left out. A runtime stacks at most a few hundred
// layers, so the bound is never met by an image that runs.
const MaxLayers = 1000

// ImageBlobs are the config and the layers of an image manifest in the manifest's order.
type ImageBlobs struct {
	Config *BlobDescriptor  `json:"config,omitempty"`
	Layers []BlobDescriptor `json:"layers"`
	// More counts the layers beyond MaxLayers that are not kept.
	More int `json:"more,omitempty"`
}

// The kinds of a configuration a reader can read.
const (
	// ConfigImage is an image's configuration (the OCI image configuration and Docker's container image).
	ConfigImage = "image"
	// ConfigChart is a Helm chart's configuration: its Chart.yaml as JSON (formats/oci.md R-OCI-042).
	ConfigChart = "chart"
)

// The reasons a configuration was not read.
const (
	// UnreadableTooLarge: the blob is larger than the bound of the reader.
	UnreadableTooLarge = "too-large"
	// UnreadableInvalid: the blob is no JSON object of the configuration's shape, or one that two readers could read differently (a key
	// twice, two keys that only differ in case, a byte that is not UTF-8).
	UnreadableInvalid = "invalid"
)

// ConfigFacts is what the reader read of a manifest's configuration. Kind says which reader; Unreadable why it read nothing. Image is
// set for an image's configuration, Chart for a Helm chart's (formats/oci.md R-OCI-042).
type ConfigFacts struct {
	Kind       string       `json:"kind"`
	Unreadable string       `json:"unreadable,omitempty"`
	Image      *ImageConfig `json:"image,omitempty"`
	Chart      *ChartConfig `json:"chart,omitempty"`
}

// ChartConfig is what a Helm chart's Chart.yaml says, as `helm show chart` shows it. Every string is the chart's text, bounded.
type ChartConfig struct {
	APIVersion   string            `json:"apiVersion,omitempty"`
	Name         string            `json:"name,omitempty"`
	Version      string            `json:"version,omitempty"`
	AppVersion   string            `json:"appVersion,omitempty"`
	Description  string            `json:"description,omitempty"`
	Type         string            `json:"type,omitempty"`
	KubeVersion  string            `json:"kubeVersion,omitempty"`
	Home         string            `json:"home,omitempty"`
	Icon         string            `json:"icon,omitempty"`
	Deprecated   bool              `json:"deprecated,omitempty"`
	Keywords     []string          `json:"keywords,omitempty"`
	Sources      []string          `json:"sources,omitempty"`
	Maintainers  []ChartMaintainer `json:"maintainers,omitempty"`
	Dependencies []ChartDependency `json:"dependencies,omitempty"`
	// Annotations are sorted by key.
	Annotations []Var    `json:"annotations,omitempty"`
	Omitted     *Omitted `json:"omitted,omitempty"`
}

// ChartMaintainer is a maintainer of a chart.
type ChartMaintainer struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// ChartDependency is a chart a chart depends on: its name, its version range and the repository it comes from.
type ChartDependency struct {
	Name       string `json:"name,omitempty"`
	Version    string `json:"version,omitempty"`
	Repository string `json:"repository,omitempty"`
	Condition  string `json:"condition,omitempty"`
	Alias      string `json:"alias,omitempty"`
}

// ChartFiles are the documentation a chart's content layer carries at its root (formats/oci.md R-OCI-042): its README and its
// values.yaml, each bounded; Cut says the file was longer than the bound. Unreadable says why the archive was not read.
type ChartFiles struct {
	Readme     string
	ReadmeName string
	ReadmeCut  bool
	Values     string
	ValuesCut  bool
	HasValues  bool
	Unreadable string
}

// ImageConfig is what an image's configuration tells about how it runs and how it was built, as `docker inspect` and `docker history`
// show it. Every string is the configuration's text, bounded; nothing is interpreted beyond its type.
type ImageConfig struct {
	Created      string `json:"created,omitempty"`
	Author       string `json:"author,omitempty"`
	OS           string `json:"os,omitempty"`
	OSVersion    string `json:"osVersion,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	Variant      string `json:"variant,omitempty"`

	User       string   `json:"user,omitempty"`
	WorkingDir string   `json:"workingDir,omitempty"`
	StopSignal string   `json:"stopSignal,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
	Shell      []string `json:"shell,omitempty"`
	OnBuild    []string `json:"onBuild,omitempty"`
	// ExposedPorts and Volumes are the keys of the configuration's objects, sorted.
	ExposedPorts []string     `json:"exposedPorts,omitempty"`
	Volumes      []string     `json:"volumes,omitempty"`
	Healthcheck  *Healthcheck `json:"healthcheck,omitempty"`
	// Env is the environment in the configuration's order, each entry split at its first "=".
	Env []Var `json:"env,omitempty"`
	// Labels are sorted by key.
	Labels []Var `json:"labels,omitempty"`
	// History is the build history, oldest first, as the configuration lists it.
	History []HistoryEntry `json:"history,omitempty"`

	// Omitted counts what a bound left out.
	Omitted *Omitted `json:"omitted,omitempty"`
}

// Var is an environment entry or a label. Cut says the value was longer than the reader keeps and ends where the bound did.
type Var struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Cut   bool   `json:"cut,omitempty"`
}

// Healthcheck is the configuration's check of a running container; the durations are nanoseconds, as the configuration states them.
type Healthcheck struct {
	Test        []string `json:"test,omitempty"`
	Interval    int64    `json:"interval,omitempty"`
	Timeout     int64    `json:"timeout,omitempty"`
	StartPeriod int64    `json:"startPeriod,omitempty"`
	Retries     int64    `json:"retries,omitempty"`
}

// HistoryEntry is one step of the build. EmptyLayer says the step added no layer (ENV, CMD, LABEL).
type HistoryEntry struct {
	Created    string `json:"created,omitempty"`
	CreatedBy  string `json:"createdBy,omitempty"`
	Comment    string `json:"comment,omitempty"`
	Author     string `json:"author,omitempty"`
	EmptyLayer bool   `json:"emptyLayer,omitempty"`
	Cut        bool   `json:"cut,omitempty"`
}

// Omitted counts the entries a bound of the reader left out of each list, and whether a single string was cut.
type Omitted struct {
	Env     int  `json:"env,omitempty"`
	Labels  int  `json:"labels,omitempty"`
	History int  `json:"history,omitempty"`
	Other   int  `json:"other,omitempty"`
	Cut     bool `json:"cut,omitempty"`
}

// TagFacts is a tag's manifest with what the version page shows of it beyond TagView (R-OCI-039 to R-OCI-041): its annotations and,
// for an image manifest, its blobs and its configuration; an index adds the same of every child the repository holds, in platform
// order.
type TagFacts struct {
	VersionID int64
	// RepoID is the repository that holds the tag.
	RepoID int64
	Image     ManifestFacts
	Children  []ManifestFacts
}

// ManifestFacts are the facts of one manifest row. Blobs and ConfigFacts are nil when they were not asked for or are not known.
type ManifestFacts struct {
	// Platform is the platform the index names the child with; "" for the tag's own manifest.
	Platform     string
	Digest       [32]byte
	Annotations  json.RawMessage
	ConfigDigest *[32]byte
	Blobs        json.RawMessage
	ConfigFacts  json.RawMessage
}
