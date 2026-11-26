package generic

import (
	"embed"

	"github.com/guided-traffic/quader/internal/formats"
)

// guideFiles are the Markdown templates of the guide, one file per section.
//
//go:embed guide/*.md
var guideFiles embed.FS

// Guide is the generic format's guide (R-GENERIC-016, R-GENERIC-017, api-and-ui.md
// R-UI-049): what a generic local repository is, and how curl, wget and PowerShell
// sign in, download and check files, resume downloads, list folders, upload,
// replace and delete files through /files/ (curl also with a digest, conditionally
// and only once), what an endpoint with exposure rules changes, and what each answer
// of /files/ means. The sections of the topics a client does differently are its
// own; the others are every client's. Every statement describes quader as built;
// the tests in guide_test.go pin the facts the text names to the code.
func (Format) Guide() []formats.GuideSection {
	local := []formats.Class{formats.Local}
	curl, wget, ps := []string{ClientCurl}, []string{ClientWget}, []string{ClientPowerShell}
	return []formats.GuideSection{
		{ID: "generic.local.what", Topic: formats.GuideWhat, Title: "What a generic local repository is", Classes: local, Markdown: guideText("what-local")},
		{ID: "generic.configure", Topic: formats.GuideConfigure, Title: "Point curl at the repository", Clients: curl, Markdown: guideText("configure")},
		{ID: "generic.wget.configure", Topic: formats.GuideConfigure, Title: "Point wget at the repository", Clients: wget, Markdown: guideText("wget-configure")},
		{ID: "generic.powershell.configure", Topic: formats.GuideConfigure, Title: "Point PowerShell at the repository", Clients: ps, Markdown: guideText("powershell-configure")},
		{ID: "generic.authenticate", Topic: formats.GuideAuthenticate, Title: "Sign curl in", Clients: curl, Markdown: guideText("authenticate")},
		{ID: "generic.wget.authenticate", Topic: formats.GuideAuthenticate, Title: "Sign wget in", Clients: wget, Markdown: guideText("wget-authenticate")},
		{ID: "generic.powershell.authenticate", Topic: formats.GuideAuthenticate, Title: "Sign PowerShell in", Clients: ps, Markdown: guideText("powershell-authenticate")},
		{ID: "generic.install", Topic: formats.GuideInstall, Title: "Download files", Clients: curl, Markdown: guideText("install")},
		{ID: "generic.wget.install", Topic: formats.GuideInstall, Title: "Download files", Clients: wget, Markdown: guideText("wget-install")},
		{ID: "generic.powershell.install", Topic: formats.GuideInstall, Title: "Download files", Clients: ps, Markdown: guideText("powershell-install")},
		{ID: "generic.publish", Topic: formats.GuidePublish, Title: "Upload files", Classes: local, Clients: curl, Markdown: guideText("publish")},
		{ID: "generic.wget.publish", Topic: formats.GuidePublish, Title: "Upload files", Classes: local, Clients: wget, Markdown: guideText("wget-publish")},
		{ID: "generic.powershell.publish", Topic: formats.GuidePublish, Title: "Upload files", Classes: local, Clients: ps, Markdown: guideText("powershell-publish")},
		{ID: "generic.maintain", Topic: formats.GuideMaintain, Title: "Replace and delete files", Classes: local, Clients: curl, Markdown: guideText("maintain")},
		{ID: "generic.wget.maintain", Topic: formats.GuideMaintain, Title: "Replace and delete files", Classes: local, Clients: wget, Markdown: guideText("wget-maintain")},
		{ID: "generic.powershell.maintain", Topic: formats.GuideMaintain, Title: "Replace and delete files", Classes: local, Clients: ps, Markdown: guideText("powershell-maintain")},
		{ID: "generic.endpoint", Topic: formats.GuideEndpoint, Title: "On this endpoint", Markdown: guideText("endpoint")},
		{ID: "generic.troubleshooting", Topic: formats.GuideTroubleshooting, Title: "Troubleshooting", Markdown: guideText("troubleshooting")},
	}
}

// guideText is the template of one section; a missing file is a defect of the
// build, which the conformance kit reports as a required topic that says nothing.
func guideText(name string) string {
	b, err := guideFiles.ReadFile("guide/" + name + ".md")
	if err != nil {
		return ""
	}
	return string(b)
}
