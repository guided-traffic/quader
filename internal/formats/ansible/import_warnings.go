package ansible

import (
	"encoding/json"
	"strings"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
)

// The messages of level WARNING of R-ANS-062: what Galaxy's importer warns of, or refuses, without running a program over the archive.
// Each is advice to the publisher; none refuses a publish.
const (
	warnNoChangelog = "No changelog found: the collection has none of changelogs/changelog.yaml, CHANGELOG.md or CHANGELOG.rst, " +
		"at its root or in docs/."
	warnNoReadme          = "No README found: collection_info.readme names no file of the archive."
	warnNoRuntime         = "meta/runtime.yml is missing, so requires_ansible is not set: no client can tell which versions of Ansible the collection supports."
	warnNoRequiresAnsible = "meta/runtime.yml sets no requires_ansible: no client can tell which versions of Ansible the collection supports."
	warnNoLicence         = "No licence named: collection_info has neither license nor license_file."
)

// importWarnings are the warnings of the import task of a publish beside the archive reader's and the documentation generator's
// (R-ANS-062): a missing changelog (the places of R-ANS-053, the generator's finder), README, requires_ansible and licence. A
// meta/runtime.yml that could not be read has the reader's own warning and none of these.
func importWarnings(a *collection.Archive, noChangelog bool) []string {
	var out []string
	if noChangelog {
		out = append(out, warnNoChangelog)
	}
	if a.ReadmeName == "" {
		out = append(out, warnNoReadme)
	}
	switch {
	case !a.HasRuntime:
		out = append(out, warnNoRuntime)
	case !a.RuntimeIgnored && a.RequiresAnsible == "":
		out = append(out, warnNoRequiresAnsible)
	}
	if !namesLicence(a.Info) {
		out = append(out, warnNoLicence)
	}
	return out
}

// namesLicence says that collection_info names a licence: a license list with an entry that is not blank, or a license_file.
func namesLicence(info json.RawMessage) bool {
	var doc struct {
		License     json.RawMessage `json:"license"`
		LicenseFile json.RawMessage `json:"license_file"`
	}
	if json.Unmarshal(info, &doc) != nil {
		return false
	}
	var list []string
	if json.Unmarshal(doc.License, &list) == nil {
		for _, l := range list {
			if strings.TrimSpace(l) != "" {
				return true
			}
		}
	}
	var file string
	return json.Unmarshal(doc.LicenseFile, &file) == nil && strings.TrimSpace(file) != ""
}
