package rows

import (
	"encoding/json"
	"net/url"
	"strings"
)

// Embedded is what the .nuspec of a stored version names of the files it embeds (R-NUGET-024): the package path of each, "" for
// a file it names none of. License is the path of a license of type file; a license expression embeds nothing.
type Embedded struct {
	Icon, License, Readme string
}

// Path is the package path of an embedded file by its name in the flat container ("icon", "license", "readme"), "" for none.
func (e Embedded) Path(file string) string {
	switch file {
	case "icon":
		return e.Icon
	case "license":
		return e.License
	case "readme":
		return e.Readme
	}
	return ""
}

// EmbeddedOf reads the embedded files a metadata object of R-NUGET-005 names, as the statement NuGetEmbeddedNames reads them: the
// members icon and readme, and the value of a license of type file. A metadata object an upstream's document gave (a remote's
// registration) names none, since its catalog entry has no such member.
func EmbeddedOf(metadata []byte) Embedded {
	var m struct {
		Icon    string `json:"icon"`
		Readme  string `json:"readme"`
		License *struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"license"`
	}
	if json.Unmarshal(metadata, &m) != nil {
		return Embedded{}
	}
	out := Embedded{Icon: strings.TrimSpace(m.Icon), Readme: strings.TrimSpace(m.Readme)}
	if m.License != nil && m.License.Type == "file" {
		out.License = strings.TrimSpace(m.License.Value)
	}
	return out
}

// EmbeddedURL is the address of an embedded file of a version in the flat container of the feed (the v3 root with its trailing
// slash): "<feed>flatcontainer/{lower_id}/{lower_version}/{file}" (R-NUGET-024).
func EmbeddedURL(feed, lowerID, lowerVersion, file string) string {
	return feed + "flatcontainer/" + url.PathEscape(lowerID) + "/" + url.PathEscape(lowerVersion) + "/" + file
}
