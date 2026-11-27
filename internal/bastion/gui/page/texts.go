package page

import (
	"maps"
	"strconv"
)

// Texts are the few fixed words of what this package writes, in both languages of the application (W-35's B.11): the titles are the
// application's own (web/src/bastion/app.routes.ts and messages.de.xlf), so the head a crawler reads and the title the application
// sets after its start say the same.
type Texts struct {
	Projects, Packages, Versions, Imprint, Privacy, NotFound, NotFoundText string
	LatestVersion, Published, Licenses, Keywords, Documentation, Package   string
	// SetupClient is the title of a repository's page Set up client (R-BAS-129).
	SetupClient string
	// Files heads a folder's entries; FilesSentence is the description of a generic repository's folders and files (R-UI-229).
	Files         string
	FilesSentence string
	// Repositories heads a project's repositories and is the way's step to them, Images what an image repository holds (R-BAS-132).
	Repositories, Images string
	// PackagesIn is a project's list ("Packages in web"), RepositoriesIn its repositories ("Repositories in web"), VersionsOf a
	// package's versions ("Versions of ui").
	PackagesIn, RepositoriesIn, VersionsOf func(string) string
	// The page of a refusal of the class gui (R-BAS-123).
	TooManyText, BlockedText func(seconds int64) string
	TooManyHeading, Again    string
	// The page of a disabled Bastion (R-BAS-133): it names no administrator's act, as an outage would read.
	UnavailableHeading, UnavailableText string
	// UpstreamDescription is the project upstream's description and WellKnown the names of the repositories the product declares in
	// it, by key, in the application's words (R-UI-272): the stored texts are the product's English, as the main installation stores
	// them (R-REPO-066), and the application words them by key (web/src/app/core/upstream-project.ts).
	UpstreamDescription string
	WellKnown           map[string]string
}

// UpstreamProject is the key of the built-in project of the product's well-known repositories (R-REPO-066), the same in every
// installation; a page recognises it by this key, as the application does.
const UpstreamProject = "upstream"

// ProjectDescription is a project's description as a page shows it (R-UI-272): the project upstream's in the reader's words, every
// other project's as its author wrote it.
func (t Texts) ProjectDescription(project, stored string) string {
	if project == UpstreamProject {
		return t.UpstreamDescription
	}
	return stored
}

// RepositoryDescription is a repository's description as a page shows it (R-UI-272): a repository the product declares in the
// project upstream by its key in the reader's words, every other one as its author wrote it.
func (t Texts) RepositoryDescription(project, key, stored string) string {
	if w, ok := t.WellKnown[key]; ok && project == UpstreamProject {
		return w
	}
	return stored
}

// properNames are the names of the repositories the product declares that are proper names: the same word in both languages. A
// function, not a package variable (R-ARCH-032): every caller gets its own table.
func properNames() map[string]string {
	return map[string]string{
		"docker-hub": "Docker Hub", "quay": "Quay", "ghcr": "GitHub (ghcr.io)", "gitlab": "GitLab", "nuget-org": "nuget.org",
		"galaxy": "Ansible Galaxy", "debian": "Debian", "ubuntu": "Ubuntu",
	}
}

// wellKnown is the table of one language: its descriptive names and the proper names.
func wellKnown(descriptive map[string]string) map[string]string {
	out := properNames()
	maps.Copy(out, descriptive)
	return out
}

func wellKnownEN() map[string]string {
	return wellKnown(map[string]string{
		"debian-security": "Debian security updates", "ubuntu-security": "Ubuntu security updates", "ubuntu-ports": "Ubuntu ports",
		"npmjs": "npm public registry", "scanner-trivy": "Image scanning database", "clamav": "ClamAV definitions",
	})
}

func wellKnownDE() map[string]string {
	return wellKnown(map[string]string{
		"debian-security": "Debian-Sicherheitsupdates", "ubuntu-security": "Ubuntu-Sicherheitsupdates", "ubuntu-ports": "Ubuntu-Ports",
		"npmjs": "Öffentliche npm-Registry", "scanner-trivy": "Datenbank der Image-Analyse", "clamav": "ClamAV-Definitionen",
	})
}

func TextsOf(locale string) Texts {
	if locale == "de" {
		return Texts{
			Projects: "Projekte", Packages: "Pakete", Versions: "Versionen", Imprint: "Impressum", Privacy: "Datenschutzhinweise",
			NotFound: "Seite nicht gefunden", NotFoundText: "Unter dieser Adresse wird nichts angeboten.",
			LatestVersion: "Neueste Version", Published: "Veröffentlicht", Licenses: "Lizenzen", Keywords: "Schlagwörter",
			Documentation: "Dokumentation", Package: "Paket", SetupClient: "Client einrichten",
			Files: "Dateien", FilesSentence: "Die Dateien, die in diesem Repository veröffentlicht sind.",
			Repositories: "Repositories", Images: "Images",
			PackagesIn:     func(p string) string { return "Pakete in " + p },
			RepositoriesIn: func(p string) string { return "Repositories in " + p },
			VersionsOf:     func(n string) string { return "Versionen von " + n },
			TooManyText: func(s int64) string {
				return "Von Ihrer Adresse kamen zu viele Anfragen. Versuchen Sie es in " + seconds(s, "einer Sekunde", "Sekunden") + " erneut."
			},
			BlockedText: func(s int64) string {
				return "Anfragen von Ihrer Adresse werden nach zu vielen abgewiesenen Anfragen eine Weile abgewiesen. Versuchen Sie es in " +
					seconds(s, "einer Sekunde", "Sekunden") + " erneut."
			},
			TooManyHeading:      "Zu viele Anfragen",
			Again:               "Die Seite versucht es von selbst erneut.",
			UnavailableHeading:  "Nicht verfügbar",
			UnavailableText:     "Zurzeit nicht verfügbar. Versuchen Sie es später noch einmal.",
			UpstreamDescription: "Die öffentlichen Registrys, die diese Installation jedem Projekt anbietet.",
			WellKnown:           wellKnownDE(),
		}
	}
	return Texts{
		Projects: "Projects", Packages: "Packages", Versions: "Versions", Imprint: "Imprint", Privacy: "Privacy notice",
		NotFound: "Page not found", NotFoundText: "Nothing is offered at this address.",
		LatestVersion: "Latest version", Published: "Published", Licenses: "Licenses", Keywords: "Keywords",
		Documentation: "Documentation", Package: "Package", SetupClient: "Set up client",
		Files: "Files", FilesSentence: "The files published in this repository.",
		Repositories: "Repositories", Images: "Images",
		PackagesIn:     func(p string) string { return "Packages in " + p },
		RepositoriesIn: func(p string) string { return "Repositories in " + p },
		VersionsOf:     func(n string) string { return "Versions of " + n },
		TooManyText: func(s int64) string {
			return "Too many requests came from your address. Try again in " + seconds(s, "one second", "seconds") + "."
		},
		BlockedText: func(s int64) string {
			return "Requests from your address are refused for a while after too many refused requests. Try again in " +
				seconds(s, "one second", "seconds") + "."
		},
		TooManyHeading:      "Too many requests",
		Again:               "The page tries again by itself.",
		UnavailableHeading:  "Not available",
		UnavailableText:     "Not available at the moment. Try again later.",
		UpstreamDescription: "The public registries this installation offers to every project.",
		WellKnown:           wellKnownEN(),
	}
}

// seconds writes a wait: "one second" for one, "<n> seconds" otherwise, the number and its unit on one line (a no-break space).
func seconds(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return strconv.FormatInt(n, 10) + " " + many
}

// TitleSuffix ends every title with the Bastion's name (Q-UI-1617), as the application's title strategy writes it
// (web/src/app/core/title-strategy.ts with the Bastion's TITLE_SUFFIX of web/src/bastion/app.config.ts).
const TitleSuffix = "Quader Bastion"

// FullTitle is a page's title as the application writes it: "<own> – Quader Bastion", or "Quader Bastion" alone.
func FullTitle(own string) string {
	if own == "" {
		return TitleSuffix
	}
	return own + " – " + TitleSuffix
}
