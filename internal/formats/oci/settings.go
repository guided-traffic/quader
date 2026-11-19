package oci

import (
	_ "embed" // the settings schema
	"encoding/json"
)

// settingsSchema is the JSON Schema 2020-12 of the oci member of a repository document (R-ARCH-008, R-OCI-026). The repository
// service validates the member against it when a document is written, and GET /api/v1/formats serves it to the create form.
//
//go:embed settings.schema.json
var settingsSchema []byte

// DefaultPlatforms returns the value of oci.platforms while a repository sets none (Q-OCI-223): the owner's decision of 2026-11-19
// (W-25), the platforms of a multi-platform image a remote repository downloads ahead.
func DefaultPlatforms() []string { return []string{"linux/amd64", "linux/arm64"} }

// MirrorSettings are what a remote repository of the format downloads ahead: the members of oci.mirror, which only a mirror's
// passes read, and oci.platforms, which a pass and the background download of a probed tag read (R-OCI-026, R-REPO-065).
type MirrorSettings struct {
	// Tags are the tag patterns a tag must match to be mirrored.
	Tags []string
	// ExcludeTags are the tag patterns that are never mirrored.
	ExcludeTags []string
	// Platforms are oci.platforms: the platforms of an index whose manifests are downloaded ahead; empty is every platform.
	Platforms []string
}

// mirrorSettingsOf reads oci.mirror and oci.platforms from a stored repository document, with the defaults for what the document
// leaves out. The service refuses a value the schema does not allow, so only a row written around the service can fail to parse; it
// yields the defaults.
func mirrorSettingsOf(config []byte) MirrorSettings {
	var doc struct {
		OCI json.RawMessage `json:"oci"`
	}
	if json.Unmarshal(config, &doc) != nil {
		return defaultMirrorSettings()
	}
	return mirrorMemberOf(doc.OCI)
}

func defaultMirrorSettings() MirrorSettings {
	return MirrorSettings{Tags: []string{"latest"}, ExcludeTags: []string{}, Platforms: DefaultPlatforms()}
}

// mirrorMemberOf reads oci.mirror and oci.platforms from the format's member of the repository document, which is what
// discover.Upstream.Settings returns, with the defaults for what the member leaves out.
func mirrorMemberOf(member json.RawMessage) MirrorSettings {
	out := defaultMirrorSettings()
	var doc struct {
		Mirror struct {
			Tags        []string `json:"tags"`
			ExcludeTags []string `json:"excludeTags"`
		} `json:"mirror"`
		Platforms []string `json:"platforms"`
	}
	if len(member) == 0 || json.Unmarshal(member, &doc) != nil {
		return out
	}
	if m := doc.Mirror; m.Tags != nil {
		out.Tags = m.Tags
	}
	if m := doc.Mirror; m.ExcludeTags != nil {
		out.ExcludeTags = m.ExcludeTags
	}
	if doc.Platforms != nil {
		out.Platforms = doc.Platforms
	}
	return out
}
