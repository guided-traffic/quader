package clamav

import (
	"embed"

	"github.com/guided-traffic/quader/internal/formats"
)

// SetupTemplates are the snippets of the client freshclam (R-CLAMAV-006): one line of freshclam.conf and the first run. The address
// is written out with its scheme and without a trailing slash, because freshclam joins it and the file name with a slash of its own and
// reads a PrivateMirror without a scheme as http. Where the repository is not anonymously readable, freshclam sends the credentials
// of its URL as Basic, so the token stands as the password of the URL (Q-REPO-415: the way rests on libcurl and is not documented by
// ClamAV); credentials=none drops that line.
func (Format) SetupTemplates() []formats.SetupTemplate {
	const configure = `{{if .RequiresAuth}}PrivateMirror https://clamav:${QUADER_TOKEN}@{{slice (schemeless (trimslash .RegistryURL)) 2}}
{{else}}PrivateMirror {{trimslash .RegistryURL}}
{{end}}`
	return []formats.SetupTemplate{
		{ID: "freshclam.conf.configure", Client: ClientFreshclam, Section: "configure", Lang: "ini", File: "/etc/clamav/freshclam.conf", Template: configure},
		{ID: "freshclam.install", Client: ClientFreshclam, Section: "install", Lang: "shell", Template: "freshclam\n"},
	}
}

// guideFiles are the Markdown templates of the guide, one file per section.
//
//go:embed guide/*.md
var guideFiles embed.FS

// Guide is the guide of freshclam (R-CLAMAV-006, api-and-ui.md R-UI-049): what the repository is and that Quader's own scanning uses
// the same set, the one line of freshclam.conf and the certificate, the token of a level below Public, the first run, and the answers
// a ClamAV installation may meet.
func (Format) Guide() []formats.GuideSection {
	return []formats.GuideSection{
		{ID: "clamav.what", Topic: formats.GuideWhat, Title: "What this repository is", Markdown: guideText("what")},
		{ID: "clamav.configure", Topic: formats.GuideConfigure, Title: "Point freshclam at Quader", Markdown: guideText("configure")},
		{ID: "clamav.authenticate", Topic: formats.GuideAuthenticate, Title: "Sign freshclam in", Markdown: guideText("authenticate")},
		{ID: "clamav.install", Topic: formats.GuideInstall, Title: "Run freshclam", Markdown: guideText("install")},
		{ID: "clamav.troubleshooting", Topic: formats.GuideTroubleshooting, Title: "Troubleshooting", Markdown: guideText("troubleshooting")},
	}
}

// guideText is the template of one section; a missing file is a defect of the build, which the kit reports as a required topic
// that says nothing.
func guideText(name string) string {
	b, err := guideFiles.ReadFile("guide/" + name + ".md")
	if err != nil {
		return ""
	}
	return string(b)
}
