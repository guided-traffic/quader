package shipped

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"time"

	"github.com/guided-traffic/quader/internal/formats/deb/archivekeys"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
)

// data is what the release embeds (R-SIG-017):
//   - snapshot.json: the TUF repository the snapshot was read from, the TUF snapshot's version and when `make shipped-anchors` read it;
//   - targets/: the two TUF targets, trusted_root.json and registry.npmjs.org/keys.json, as the repository served them;
//   - tuf/: the TUF metadata of that read, as the repository served it (the root versions after the one sigstore-go embeds, the timestamp,
//     the snapshot and the targets), so that a test verifies the targets against the embedded TUF root without the network;
//   - nuget-org.pem: nuget.org's repository signing certificates, as its RepositorySignatures/5.0.0 resource listed them on 2026-11-21;
//   - nuget-org-timestamping.pem: the roots of the timestamp authorities whose tokens nuget.org's repository signatures carry (Q-SIG-29).
//
//go:embed data
var data embed.FS

// The paths in data.
const (
	dataSnapshot     = "data/snapshot.json"
	dataTargets      = "data/targets"
	dataTUF          = "data/tuf"
	dataNuGet        = "data/nuget-org.pem"
	dataNuGetStamped = "data/nuget-org-timestamping.pem"
)

// snapshotMeta is data/snapshot.json.
type snapshotMeta struct {
	URL        string    `json:"url"`
	TUFVersion int64     `json:"tufVersion"`
	BuiltAt    time.Time `json:"builtAt"`
}

// readData is a file of data; a file the release lacks reads as empty, and the snapshot test fails on it.
func readData(name string) []byte {
	b, err := fs.ReadFile(data, name)
	if err != nil {
		return nil
	}
	return b
}

func release() Snapshot {
	var meta snapshotMeta
	_ = json.Unmarshal(readData(dataSnapshot), &meta) // the snapshot test holds the file to its shape
	return Snapshot{
		TrustedRoot: readData(path.Join(dataTargets, TargetTrustedRoot)),
		NPMKeys:     readData(path.Join(dataTargets, TargetNPMKeys)),
		TUFVersion:  meta.TUFVersion,
		BuiltAt:     meta.BuiltAt,
	}
}

// The presets of the well-known repositories whose anchors these are (R-SIG-017, R-REPO-062): the IDs the formats give their upstream
// presets.
const (
	presetNPMJS    = "npmjs"
	presetDebian   = "debian"
	presetUbuntu   = "ubuntu"
	presetNuGetOrg = "nuget-org"
)

func anchorsOf() []Anchor {
	snap := release()
	npm, err := npmKeys(snap.NPMKeys)
	if err != nil {
		npm = &anchors.PublicKeyMaterial{} // does not parse; the trust step skips it, and the snapshot test fails on it
	}
	return []Anchor{
		{Key: KeySigstorePublic, Title: "Public Sigstore", Kind: anchors.KindSigstore, Material: anchors.Material{
			Sigstore: &anchors.SigstoreMaterial{IdentityPattern: ".*", TrustRoot: anchors.TrustRootPublic},
		}},
		{
			Key: KeyNPMJS, Title: "npm registry signing keys", Kind: anchors.KindPublicKey, Preset: presetNPMJS,
			Material: anchors.Material{PublicKey: npm},
		},
		{
			Key: KeyDebianArchive, Title: "Debian archive keys", Kind: anchors.KindOpenPGP, Preset: presetDebian,
			Material: anchors.Material{OpenPGP: &anchors.OpenPGPMaterial{Keys: string(archivekeys.Debian())}},
		},
		{
			Key: KeyUbuntuArchive, Title: "Ubuntu archive keys", Kind: anchors.KindOpenPGP, Preset: presetUbuntu,
			Material: anchors.Material{OpenPGP: &anchors.OpenPGPMaterial{Keys: string(archivekeys.Ubuntu())}},
		},
		{
			Key: KeyNuGetOrg, Title: "nuget.org repository signing certificates", Kind: anchors.KindCA, Preset: presetNuGetOrg,
			Material: anchors.Material{CA: &anchors.CAMaterial{Certificates: string(readData(dataNuGet)), Use: anchors.UseCodeSigning}},
		},
		{
			Key: KeyNuGetOrgTimestamping, Title: "Timestamp authorities of nuget.org's repository signatures", Kind: anchors.KindCA,
			Preset: presetNuGetOrg, Material: anchors.Material{CA: &anchors.CAMaterial{
				Certificates: string(readData(dataNuGetStamped)), Use: anchors.UseTimestamping,
			}},
		},
	}
}
