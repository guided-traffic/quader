// Package tagorder is the order of image tags (Q-OCI-17): the one order the package screens, the mirror's tag selection and any
// other "newest tag" question of the format share. It is deterministic and reads nothing but the tags, because the only other
// source of recency is the image config, one manifest request per tag.
//
// A tag is a version when it is "v"? followed by dot-separated decimal numbers and an optional "-" suffix. Versions are newer than
// every other tag and are ordered by their numbers (a missing number counts as zero), then by the suffix: no suffix is newest, then
// a suffix that is no pre-release marker (a variant such as "alpine"), then a pre-release marker, and within one rank by the bytes of
// the suffix. A suffix starts a pre-release when it begins with alpha, beta, rc, pre or dev in any case and continues with digits
// and dots only. Every other tag is ordered by its bytes. Two different tags never compare equal. The package screens use this order for
// the versions alone: their view adapter calls a tag that is no version no version of the format, and the lists order such tags by their
// time (formats/oci.md Q-OCI-269); the mirror's tag selection uses it whole.
package tagorder

import (
	"strings"

	"github.com/guided-traffic/quader/internal/formats/versionkey"
)

// Version is a tag read as a version.
type Version struct {
	// Numbers are the decimal components without leading zeros.
	Numbers []string
	// Suffix is what follows the first "-", "" for none.
	Suffix string
}

// Parse reads a tag as a version; ok is false for a tag that is none.
func Parse(tag string) (Version, bool) {
	rest := strings.TrimPrefix(tag, "v")
	num, suffix, hasSuffix := strings.Cut(rest, "-")
	if num == "" || (hasSuffix && suffix == "") {
		return Version{}, false
	}
	var v Version
	for part := range strings.SplitSeq(num, ".") {
		if part == "" {
			return Version{}, false
		}
		for i := range len(part) {
			if part[i] < '0' || part[i] > '9' {
				return Version{}, false
			}
		}
		v.Numbers = append(v.Numbers, strings.TrimLeft(part, "0"))
	}
	v.Suffix = suffix
	return v, true
}

// Prerelease reports whether the tag is a version whose suffix marks a pre-release.
func Prerelease(tag string) bool {
	v, ok := Parse(tag)
	return ok && v.prerelease()
}

func (v Version) prerelease() bool {
	s := strings.ToLower(v.Suffix)
	for _, marker := range []string{"alpha", "beta", "rc", "pre", "dev"} {
		if rest, ok := strings.CutPrefix(s, marker); ok {
			return strings.Trim(rest, "0123456789.") == ""
		}
	}
	return false
}

// rank orders the suffixes of one number: a pre-release is older than a variant, which is older than the release itself.
func (v Version) rank() int {
	switch {
	case v.Suffix == "":
		return 2
	case v.prerelease():
		return 0
	}
	return 1
}

// Compare orders two tags: negative when a is older than b, positive when it is newer, zero only for equal strings.
func Compare(a, b string) int {
	va, oka := Parse(a)
	vb, okb := Parse(b)
	switch {
	case !oka && !okb:
		return strings.Compare(a, b)
	case !oka:
		return -1
	case !okb:
		return 1
	}
	if c := compareNumbers(va.Numbers, vb.Numbers); c != 0 {
		return c
	}
	if c := va.rank() - vb.rank(); c != 0 {
		return c
	}
	if c := strings.Compare(va.Suffix, vb.Suffix); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// Key is the encoding of a tag's place in the order of Compare, for the stored sort key of internal/formats/versionkey (Q-API-429): 0x00
// for a tag that is no version; for a version 0x01, each number up to the last that is not zero as 0x01 and its value, 0x00, the rank
// of the suffix (0x00 a pre-release, 0x01 a variant, 0x02 none) and the suffix's bytes. A number that is missing counts as zero, as in
// Compare, so the zeros at the end are left out. The caller appends the tag itself, which breaks the ties Compare breaks by the bytes.
// PostgreSQL derives the same bytes for a remote repository's tags (oci_tag_sort_key, migration 00116); a change here changes it there.
func Key(tag string) []byte {
	v, ok := Parse(tag)
	if !ok {
		return []byte{0x00}
	}
	nums := v.Numbers
	for len(nums) > 0 && nums[len(nums)-1] == "" {
		nums = nums[:len(nums)-1]
	}
	k := make([]byte, 0, 8+len(tag))
	k = append(k, 0x01)
	for _, n := range nums {
		k = versionkey.Number(append(k, 0x01), n)
	}
	k = append(k, 0x00, byte(v.rank())) //nolint:gosec // 0, 1 or 2
	return versionkey.Text(k, v.Suffix)
}

func compareNumbers(a, b []string) int {
	for i := range max(len(a), len(b)) {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if len(x) != len(y) {
			return len(x) - len(y)
		}
		if c := strings.Compare(x, y); c != 0 {
			return c
		}
	}
	return 0
}
