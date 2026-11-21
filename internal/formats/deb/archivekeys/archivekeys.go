// Package archivekeys holds the archive keyrings of the well-known Debian and Ubuntu repositories (repositories-and-proxy.md Q-REPO-379):
// the keyring files apt verifies the archives with, armored, unchanged in their keys. The Debian format gives them to its presets as
// deb.upstreamKeys, and the signatures ship them as the anchors debian-archive and ubuntu-archive (signatures.md R-SIG-017). It embeds the
// two files and nothing else, so that both a format (the edge) and the signatures (the domain) can import it.
//
//   - debian-archive-keyring.asc is /usr/share/keyrings/debian-archive-keyring.gpg of the package debian-archive-keyring 2025.1 (Debian 13
//     trixie): the archive, security and stable release keys of Debian 11, 12 and 13. It signs deb.debian.org/debian and
//     deb.debian.org/debian-security.
//   - ubuntu-archive-keyring.asc is /usr/share/keyrings/ubuntu-archive-keyring.gpg of the package ubuntu-keyring 2023.11.28.1 (Ubuntu
//     24.04 noble): the archive keys of 2012 and 2018 and the CD image key of 2012. The 2018 key signs the archive, its security updates
//     and its ports.
//
// How they were obtained and checked is in docs/developer/deb.md (The archive keys of the presets); TestQ_REPO_379_TheArchiveKeys of
// internal/formats/deb pins every fingerprint and fails 90 days before a key expires. A key change of either keyring is a release of
// Quader.
package archivekeys

import _ "embed" // the two keyrings

var (
	//go:embed debian-archive-keyring.asc
	debian string
	//go:embed ubuntu-archive-keyring.asc
	ubuntu string
)

// Debian returns the armored archive keyring of Debian, a copy.
func Debian() []byte { return []byte(debian) }

// Ubuntu returns the armored archive keyring of Ubuntu, a copy.
func Ubuntu() []byte { return []byte(ubuntu) }
