package oci

import (
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// TestR_OCI_031_TheMetadataSizeIsTheStoredImageSize: the size of the tag's metadata is the image's (the config and layers of an image
// manifest, the sum over the children of an index), and the platforms keep each child's own.
func TestR_OCI_031_TheMetadataSizeIsTheStoredImageSize(t *testing.T) {
	image := rows.TagView{Size: 400, ContentSize: 30 << 20}
	if got := tagMetadata(image)["size"]; got != int64(30<<20) {
		t.Fatalf("image size = %v", got)
	}
	index := rows.TagView{Size: 300, ContentSize: 30, Children: []rows.Child{
		{Platform: "linux/amd64", Stored: true, ContentSize: 10}, {Platform: "linux/arm64", Stored: true, ContentSize: 20},
	}}
	md := tagMetadata(index)
	if md["size"] != int64(30) || md["sizePartial"] != nil {
		t.Fatalf("index size = %v, partial %v", md["size"], md["sizePartial"])
	}
	if ps := md["platforms"].([]map[string]any); len(ps) != 2 || ps[0]["size"] != int64(10) || ps[1]["size"] != int64(20) {
		t.Fatalf("platforms = %v", md["platforms"])
	}
}

// TestQ_OCI_221_AnIndexCountsThePlatformsTheRepositoryHolds: the size of an index is the sum over the images of its platforms that the
// repository holds; while some are missing it is partial and says so, while none is it is not known and is no number (R-API-080). An
// attestation manifest is no platform and is left out of the count; the platform that is not held lists no size.
func TestQ_OCI_221_AnIndexCountsThePlatformsTheRepositoryHolds(t *testing.T) {
	amd := rows.Child{Platform: "linux/amd64", Stored: true, ContentSize: 10}
	arm := rows.Child{Platform: "linux/arm64", ContentSize: 0}
	att := rows.Child{Platform: rows.AttestationPlatform}
	cases := []struct {
		name     string
		children []rows.Child
		want     formats.ViewSize
	}{
		{"every platform held", []rows.Child{amd, {Platform: "linux/arm64", Stored: true, ContentSize: 20}}, formats.ViewSize{Bytes: 30}},
		{"one of two platforms held", []rows.Child{amd, arm}, formats.ViewSize{Bytes: 10, Partial: true}},
		{"no platform held", []rows.Child{{Platform: "linux/amd64"}, arm}, formats.ViewSize{Unknown: true}},
		{"the attestations are not fetched by a pull", []rows.Child{amd, att, att}, formats.ViewSize{Bytes: 10}},
		{"an index of attestations alone counts them", []rows.Child{att, {Platform: rows.AttestationPlatform, Stored: true, ContentSize: 4}}, formats.ViewSize{Bytes: 4, Partial: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tv := rows.TagView{Size: 300, ContentSize: 999, Children: c.children}
			if got := imageSize(rows.SizeOf(tv)); got != c.want {
				t.Fatalf("size = %+v, want %+v", got, c.want)
			}
			md := tagMetadata(tv)
			switch {
			case c.want.Unknown && md["size"] != nil:
				t.Errorf("the metadata of a size that is not known says %v", md["size"])
			case !c.want.Unknown && md["size"] != c.want.Bytes:
				t.Errorf("the metadata says %v, want %d", md["size"], c.want.Bytes)
			case c.want.Partial != (md["sizePartial"] == true):
				t.Errorf("the metadata says sizePartial %v, want %v", md["sizePartial"], c.want.Partial)
			}
		})
	}
	md := tagMetadata(rows.TagView{Children: []rows.Child{amd, arm}})
	ps := md["platforms"].([]map[string]any)
	if ps[1]["stored"] != false || ps[1]["size"] != nil || ps[1]["mediaType"] != nil || ps[0]["stored"] != true {
		t.Fatalf("the platforms = %v, want the one not held without a size", ps)
	}
	// An image manifest states its config and layers: its size is known whether the layers are held or not.
	if got := imageSize(rows.TagSize{ContentSize: 77}); got != (formats.ViewSize{Bytes: 77}) {
		t.Fatalf("image = %+v", got)
	}
}
