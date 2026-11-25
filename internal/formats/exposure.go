package formats

import (
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/formats/versionkey"
)

// ExposureDimensions say what exposure rules may narrow for the format
// (R-EP-012, R-EP-018).
type ExposureDimensions struct{ Packages, Versions, Tags bool }

// ExposureView answers what an endpoint exposes of the addressed repository. It is
// the shared half of a view: the package and the version. A format whose rules also
// narrow tags (Dimensions.Tags) reads them from the same value through an interface
// of its own, because what a tag is belongs to the format (Q-ARCH-257).
type ExposureView interface {
	Package(name string) bool
	Version(name, version string) bool
}

// ExposureAdapter is a format's part of endpoint exposure (R-EP-018): the range
// syntax the decision point compiles rules with and the dimensions the rules may
// narrow. Which version is a pre-release is not asked here: it is the format's
// [ViewAdapter.Prerelease], beside its order (api-and-ui.md Q-API-433), and a format
// whose rules narrow versions has a view adapter (Register checks). Register puts
// both halves into the catalog as one [catalog.Versioning], and authz reads them
// there, so no bridge between the two exists (C-13). What an endpoint hides from a
// document is the format's own filter over [Request.Endpoint]: no core code calls a
// filter, so the contract has none (Q-ARCH-257).
type ExposureAdapter interface {
	// ValidateRange checks one range in the format's native syntax.
	ValidateRange(expr string) error
	// InRange reports whether a normalized version lies in a validated range.
	InRange(version, expr string) bool
	Dimensions() ExposureDimensions
}

// versioning is the catalog's [catalog.Versioning] of a format whose exposure rules narrow versions: the range syntax of its exposure
// adapter and the pre-release answer of its view adapter, so that the decision point and the screens cannot disagree on a version
// (Q-API-433). A string the view adapter does not rank is no version of the format.
type versioning struct {
	x ExposureAdapter
	v ViewAdapter
}

var _ catalog.Versioning = versioning{}

func (s versioning) ValidateRange(expr string) error { return s.x.ValidateRange(expr) }

func (s versioning) InRange(version, expr string) bool { return s.x.InRange(version, expr) }

func (s versioning) Prerelease(version string) (pre, ok bool) {
	if k := s.v.SortKey(version); len(k) == 0 || k[0] != versionkey.Version {
		return false, false
	}
	return s.v.Prerelease(version), true
}
