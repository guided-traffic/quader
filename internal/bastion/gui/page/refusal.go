package page

import (
	"bytes"
	"html/template"
	"strconv"
)

// Refusal is the page a visitor reads when the class gui refuses a page's address (R-BAS-123, P9b's open point 3): the Bastion's
// shape (the bar with the Bastion's mark and name, a heading, the sentence, the footer), drawn by the document alone, since every file of the
// application would be refused as well: its styles carry the page's nonce, it runs no script, and it loads itself again after the
// wait (a refresh, which no policy of the page forbids and which needs no script).
type Refusal struct {
	Locale string
	// Seconds is the wait of Retry-After, whole seconds, at least one.
	Seconds int64
	// Blocked is a refusal of an address on the block list (R-BAS-037) rather than one over the class's rate.
	Blocked bool
	// Legal are the footer's links: the imprint and the privacy notice that have a text.
	Legal []Link
}

// The stones of the Bastion's mark, the tower, on its 64 grid (Q-UI-1617): the plain ones and the coloured merlon, as
// web/scripts/brand-logo.mjs writes them (MARKS.bastion); a unit test holds them equal to its web/src/bastion/public/favicon.svg.
func markStones() [][4]int {
	return [][4]int{{4, 4, 12, 16}, {48, 4, 12, 16}, {4, 24, 24, 16}, {32, 24, 28, 16}, {12, 44, 24, 16}, {40, 44, 12, 16}}
}

func markColored() [4]int { return [4]int{24, 4, 16, 16} }

// refusalCSS is the shell's look in a few rules (web/src/styles/_tokens.scss and _shell.scss): the bar's and the surface's colours in
// both schemes, the system's font, the name's "Bastion" in Regular after the Bold "Quader", the heading of the not-ready page, the
// muted footer. The mark's two colours are the generator's (the same unit test holds them equal).
const refusalCSS = `:root{color-scheme:light dark;--bg:#fff;--fg:#3a383f;--nav:#ececef;--navfg:#3a383f;--muted:#626168;` +
	`--heading:#18171d;--stone:#28272d;--accent:#b4690e}` +
	`@media (prefers-color-scheme:dark){:root{--bg:#18171d;--fg:#ececef;--nav:#28272d;--navfg:#ececef;--muted:#bfbfc3;` +
	`--heading:#fff;--stone:#ececef;--accent:#e9a13b}}` +
	`html,body{block-size:100%}` +
	`body{margin:0;display:flex;flex-direction:column;background:var(--bg);color:var(--fg);` +
	`font:400 .875rem/1.5 system-ui,-apple-system,'Segoe UI',Roboto,'Noto Sans',Ubuntu,sans-serif}` +
	`header{flex:none;display:flex;align-items:center;block-size:3rem;padding-inline:12px 8px;background:var(--nav);color:var(--navfg)}` +
	`.product{display:inline-flex;align-items:center;gap:8px;padding:4px 8px;color:inherit;font-size:1.125rem;font-weight:700;` +
	`text-decoration:none}.product .q{font-weight:400}` +
	`.product svg{block-size:2rem;inline-size:2rem}.stone{fill:var(--stone)}.accent{fill:var(--accent)}` +
	`main{flex:1 0 auto;display:flex;flex-direction:column;padding:24px 32px}` +
	`h1{margin:0 0 16px;color:var(--heading);font-size:1.375rem;font-weight:400;line-height:1.75rem}p{margin:0 0 16px}` +
	`.muted{color:var(--muted)}` +
	`footer{margin-block-start:auto}footer ul{display:flex;flex-wrap:wrap;justify-content:center;gap:4px 20px;margin:0;padding:32px 0 0;list-style:none;` +
	`font-size:.75rem;line-height:1rem}` +
	`footer a{color:var(--muted);text-decoration:none}footer a:hover{text-decoration:underline}` +
	`@media (max-width:37.49rem){main{padding:16px}}`

const refusalTemplate = `<!doctype html>` +
	`<html lang="{{.Locale}}"><head><meta charset="utf-8">` +
	`<meta name="viewport" content="width=device-width, initial-scale=1">` +
	`<meta name="robots" content="noindex">` +
	`<meta http-equiv="refresh" content="{{.Seconds}}">` +
	`<title>{{.Title}}</title>` +
	`<style nonce="{{.Nonce}}">{{.CSS}}</style></head>` +
	`<body><header><a class="product" href="/ui/{{.Locale}}/">` +
	`<svg viewBox="0 0 64 64" aria-hidden="true" focusable="false">` +
	`{{range .Stones}}<rect class="stone" x="{{index . 0}}" y="{{index . 1}}" width="{{index . 2}}" height="{{index . 3}}" rx="1.5"></rect>{{end}}` +
	`<rect class="accent" x="{{index .Colored 0}}" y="{{index .Colored 1}}" width="{{index .Colored 2}}" height="{{index .Colored 3}}" rx="1.5"></rect>` +
	`</svg><span translate="no">Quader <span class="q">Bastion</span></span></a></header>` +
	`<main><h1>{{.Heading}}</h1><p>{{.Text}}</p><p class="muted">{{.Again}}</p>` +
	`{{if .Legal}}<footer><ul>{{range .Legal}}<li><a href="{{.Href}}">{{.Text}}</a></li>{{end}}</ul></footer>{{end}}` +
	`</main></body></html>`

// RefusalPage writes the page of a refusal with the nonce of its policy.
func (rd *Renderer) RefusalPage(r Refusal, nonce string) []byte {
	t := TextsOf(r.Locale)
	secs := max(r.Seconds, 1)
	text := t.TooManyText(secs)
	if r.Blocked {
		text = t.BlockedText(secs)
	}
	var b bytes.Buffer
	err := rd.refusal.Execute(&b, struct {
		Locale, Seconds, Title, Nonce, Heading, Text, Again string
		CSS                                                 template.CSS
		Stones                                              [][4]int
		Colored                                             [4]int
		Legal                                               []Link
	}{
		r.Locale, strconv.FormatInt(secs, 10), FullTitle(t.TooManyHeading), nonce, t.TooManyHeading, text, t.Again,
		template.CSS(refusalCSS), markStones(), markColored(), r.Legal, //nolint:gosec // the package's own constant rules
	})
	if err != nil {
		panic(err) // the template and its data are this package's own
	}
	return b.Bytes()
}
