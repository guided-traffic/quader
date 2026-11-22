package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
)

// The member storage of a document (storage.md R-STOR-014, Q-STOR-283): a closed object with maxUploadSize (R-STOR-004) and
// downloadLinks, the repository's opt-out of the delivery by signed links of the object store. Any other member is
// invalid_value at its pointer, so no setting is accepted and ignored. downloadLinks is taken on every class of a format with a
// route whose clients check a digest (catalog.Entry.ClientDigest, R-STOR-071) and is not_applicable on any other; an absent
// member is the default true, and false only takes away (Q-STOR-282).

// The members of storage.
const (
	storageMaxUploadSize = "maxUploadSize"
	storageDownloadLinks = "downloadLinks"
)

// validateStorage checks the member storage of a document.
func validateStorage(v *validation, info catalog.Entry, d *Document) {
	if len(d.Storage) == 0 {
		return
	}
	trimmed := bytes.TrimSpace(d.Storage)
	var members map[string]json.RawMessage
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &members) != nil {
		v.add("/storage", codeInvalidValue, "must be a JSON object")
		return
	}
	names := make([]string, 0, len(members))
	for k := range members {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		raw := bytes.TrimSpace(members[k])
		switch k {
		case storageMaxUploadSize:
			if !validByteLimit(raw) {
				v.add("/storage/maxUploadSize", codeInvalidValue, "must be a non-negative integer number of bytes")
			}
		case storageDownloadLinks:
			switch {
			case !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")):
				v.add("/storage/downloadLinks", codeInvalidValue, "must be true or false")
			case !info.ClientDigest:
				v.add("/storage/downloadLinks", errs.CodeNotApplicable, fmt.Sprintf(
					"the %s format has no route whose clients check a digest, so none of its downloads is ever handed to a signed link (R-STOR-071)", info.ID))
			}
		case "presignedDownloads":
			v.add("/storage/presignedDownloads", codeInvalidValue,
				"is no member of storage: the delivery by signed links is the installation's switch, and a repository opts out with storage.downloadLinks: false (R-STOR-014)")
		default:
			v.add("/storage/"+k, codeInvalidValue, "is no member of storage, which holds maxUploadSize and downloadLinks (R-STOR-014)")
		}
	}
}

// downloadLinksOf is the effective value of storage.downloadLinks in a stored member: true unless it says false. A member that
// does not read is the default; validation refused it before it was stored.
func downloadLinksOf(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var s struct {
		DownloadLinks *bool `json:"downloadLinks"`
	}
	if json.Unmarshal(raw, &s) != nil || s.DownloadLinks == nil {
		return true
	}
	return *s.DownloadLinks
}

// withDownloadLinksOff returns the member storage with downloadLinks false and every other member kept: the replay of the part
// downloadLinks of repository.narrowed.
func withDownloadLinksOff(raw json.RawMessage) json.RawMessage {
	members := map[string]json.RawMessage{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &members)
	}
	members[storageDownloadLinks] = json.RawMessage("false")
	out, _ := json.Marshal(members) // raw messages that read and a literal always marshal
	return out
}
