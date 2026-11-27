package page

import "strconv"

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
	// PackagesIn is a project's list ("Packages in web"), VersionsOf a package's versions ("Versions of ui").
	PackagesIn, VersionsOf func(string) string
	// The page of a refusal of the class gui (R-BAS-123).
	TooManyText, BlockedText func(seconds int64) string
	TooManyHeading, Again    string
}

func TextsOf(locale string) Texts {
	if locale == "de" {
		return Texts{
			Projects: "Projekte", Packages: "Pakete", Versions: "Versionen", Imprint: "Impressum", Privacy: "Datenschutzhinweise",
			NotFound: "Seite nicht gefunden", NotFoundText: "Unter dieser Adresse wird nichts angeboten.",
			LatestVersion: "Neueste Version", Published: "Veröffentlicht", Licenses: "Lizenzen", Keywords: "Schlagwörter",
			Documentation: "Dokumentation", Package: "Paket", SetupClient: "Client einrichten",
			Files: "Dateien", FilesSentence: "Die Dateien, die in diesem Repository veröffentlicht sind.",
			PackagesIn: func(p string) string { return "Pakete in " + p },
			VersionsOf: func(n string) string { return "Versionen von " + n },
			TooManyText: func(s int64) string {
				return "Von Ihrer Adresse kamen zu viele Anfragen. Versuchen Sie es in " + seconds(s, "einer Sekunde", "Sekunden") + " erneut."
			},
			BlockedText: func(s int64) string {
				return "Anfragen von Ihrer Adresse werden nach zu vielen abgewiesenen Anfragen eine Weile abgewiesen. Versuchen Sie es in " +
					seconds(s, "einer Sekunde", "Sekunden") + " erneut."
			},
			TooManyHeading: "Zu viele Anfragen",
			Again:          "Die Seite versucht es von selbst erneut.",
		}
	}
	return Texts{
		Projects: "Projects", Packages: "Packages", Versions: "Versions", Imprint: "Imprint", Privacy: "Privacy notice",
		NotFound: "Page not found", NotFoundText: "Nothing is offered at this address.",
		LatestVersion: "Latest version", Published: "Published", Licenses: "Licenses", Keywords: "Keywords",
		Documentation: "Documentation", Package: "Package", SetupClient: "Set up client",
		Files: "Files", FilesSentence: "The files published in this repository.",
		PackagesIn: func(p string) string { return "Packages in " + p },
		VersionsOf: func(n string) string { return "Versions of " + n },
		TooManyText: func(s int64) string {
			return "Too many requests came from your address. Try again in " + seconds(s, "one second", "seconds") + "."
		},
		BlockedText: func(s int64) string {
			return "Requests from your address are refused for a while after too many refused requests. Try again in " +
				seconds(s, "one second", "seconds") + "."
		},
		TooManyHeading: "Too many requests",
		Again:          "The page tries again by itself.",
	}
}

// seconds writes a wait: "one second" for one, "<n> seconds" otherwise, the number and its unit on one line (a no-break space).
func seconds(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return strconv.FormatInt(n, 10) + " " + many
}

// TitleSuffix ends every title, as the application's title strategy writes it (web/src/app/core/title-strategy.ts).
const TitleSuffix = "Quader"

// FullTitle is a page's title as the application writes it: "<own> – Quader", or "Quader" alone.
func FullTitle(own string) string {
	if own == "" {
		return TitleSuffix
	}
	return own + " – " + TitleSuffix
}
