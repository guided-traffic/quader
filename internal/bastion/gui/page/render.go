package page

import (
	"bytes"
	"encoding/json"
	"html/template"
	"strings"
	"unicode/utf8"
)

// Page is what the head and the text of a page say: the handler fills it from the catalogue's documents, every member plain text but
// Readme, the README the main installation rendered and sanitized (R-UI-008), which the catalogue keeps as it was rendered.
type Page struct {
	Locale string
	// Title is the page's own title, without the product's name; Description the meta description, cut to [MaxDescription].
	Title       string
	Description string
	// Canonical is the page's absolute address on the Bastion's base URL, Alternates the same page in every language and the
	// address without a language (x-default); both empty for a page that is not found.
	Canonical  string
	Alternates []Alternate
	// Noindex is the robots rule noindex: a page that is not found, a list of results, an address that opens the findings.
	Noindex bool
	// LD is the page's JSON-LD (schema.org), nil for none.
	LD map[string]any

	// Crumbs lead from the start to the page; Heading is its heading, Lead the paragraph below it.
	Crumbs  []Link
	Heading string
	Lead    string
	Facts   []Fact
	Readme  template.HTML
	// Sections are lists of links: the projects, the packages, the versions, the documentation's entries.
	Sections []Section
	// Legal are the links of the footer: the imprint and the privacy notice that have a text.
	Legal []Link
}

// Alternate is one language's address of a page (hreflang).
type Alternate struct {
	Lang string
	Href string
}

// Link is a link of the text; Rel is its rel, Note a text after it.
type Link struct {
	Text string
	Href string
	Rel  string
	Note string
}

// Fact is one term of the page's facts: a text, a link or a time.
type Fact struct {
	Term     string
	Text     string
	Href     string
	Datetime string
}

// Section is a heading and its list of links.
type Section struct {
	Heading string
	Links   []Link
}

// MaxDescription bounds the meta description: a search engine shows about 160 characters, and the head stays small.
const MaxDescription = 300

// Cut shortens a text to at most n runes at a space, with an ellipsis.
func Cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)[:n-1]
	if i := strings.LastIndexByte(string(r), ' '); i > n/2 {
		return string(r)[:i] + "…"
	}
	return string(r) + "…"
}

const headTemplate = (`<title>{{.Title}}</title>` +
	`{{if .Description}}<meta name="description" content="{{.Description}}">{{end}}` +
	`{{if .Noindex}}<meta name="robots" content="noindex">{{end}}` +
	`{{if .Canonical}}<link rel="canonical" href="{{.Canonical}}">{{end}}` +
	`{{range .Alternates}}<link rel="alternate" hreflang="{{.Lang}}" href="{{.Href}}">{{end}}` +
	`{{if .LD}}<script type="application/ld+json">{{.LD}}</script>{{end}}` +
	// The text is for a reader that runs no script; a browser that runs the application shows its spinner until the application
	// replaces both (R-UI-217).
	`<style nonce="{{.Nonce}}">.quader-page{display:none}</style>` +
	`<noscript><style nonce="{{.Nonce}}">.quader-boot{display:none}` +
	`.quader-page{display:block;box-sizing:border-box;max-inline-size:60rem;margin:0 auto;padding:24px 16px}` +
	`.quader-page dl{display:grid;grid-template-columns:max-content 1fr;gap:4px 16px}.quader-page dd{margin:0}` +
	`.quader-page nav ol,.quader-page footer ul{display:flex;flex-wrap:wrap;gap:8px 20px;padding:0;list-style:none}` +
	`.quader-page img{max-inline-size:100%}.quader-page pre{overflow-x:auto}</style></noscript>`)

// Renderer writes the head, the text and the page of a refusal: its templates are parsed once. It is safe for concurrent use.
type Renderer struct {
	head, body, refusal *template.Template
}

// NewRenderer parses the templates.
func NewRenderer() *Renderer {
	return &Renderer{
		head:    template.Must(template.New("head").Parse(headTemplate)),
		body:    template.Must(template.New("body").Parse(bodyTemplate)),
		refusal: template.Must(template.New("refusal").Parse(refusalTemplate)),
	}
}

// Head writes the head's fill of a page: title, description, robots, canonical, alternates, JSON-LD and the rule that hides the text
// from a browser that runs the application. nonce is the page's (the slot is filled after the nonce went in).
func (r *Renderer) Head(p Page, nonce string) []byte {
	var ld template.JS
	if p.LD != nil {
		ld = template.JS(jsonLD(p.LD)) //nolint:gosec // JSON whose encoder escapes <, > and &, a data block (R-BAS-121)
	}
	var b bytes.Buffer
	err := r.head.Execute(&b, struct {
		Title, Description, Canonical, Nonce string
		Noindex                              bool
		Alternates                           []Alternate
		LD                                   template.JS
	}{FullTitle(p.Title), Cut(p.Description, MaxDescription), p.Canonical, nonce, p.Noindex, p.Alternates, ld})
	if err != nil {
		panic(err) // the template and its data are this package's own
	}
	return b.Bytes()
}

// jsonLD writes JSON-LD as a data block may hold it: the encoder escapes <, > and &, so no member can close the script element
// (W-35's B.11).
func jsonLD(v map[string]any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(v); err != nil {
		return []byte("{}")
	}
	return bytes.TrimSpace(b.Bytes())
}

const bodyTemplate = (`<div class="quader-page" lang="{{.Locale}}"><main>` +
	`{{if .Crumbs}}<nav><ol>{{range .Crumbs}}<li><a href="{{.Href}}">{{.Text}}</a></li>{{end}}</ol></nav>{{end}}` +
	`<h1>{{.Heading}}</h1>` +
	`{{if .Lead}}<p>{{.Lead}}</p>{{end}}` +
	`{{if .Facts}}<dl>{{range .Facts}}<dt>{{.Term}}</dt><dd>` +
	`{{if .Href}}<a href="{{.Href}}">{{.Text}}</a>{{else if .Datetime}}<time datetime="{{.Datetime}}">{{.Text}}</time>{{else}}{{.Text}}{{end}}` +
	`</dd>{{end}}</dl>{{end}}` +
	`{{if .Readme}}<article>{{.Readme}}</article>{{end}}` +
	`{{range .Sections}}<section><h2>{{.Heading}}</h2><ul>{{range .Links}}<li>` +
	`<a href="{{.Href}}"{{if .Rel}} rel="{{.Rel}}"{{end}}>{{.Text}}</a>{{if .Note}} – {{.Note}}{{end}}</li>{{end}}</ul></section>{{end}}` +
	`</main>` +
	`{{if .Legal}}<footer><ul>{{range .Legal}}<li><a href="{{.Href}}">{{.Text}}</a></li>{{end}}</ul></footer>{{end}}` +
	`</div>`)

// Body writes the text of a page into the application's root element.
func (r *Renderer) Body(p Page) []byte {
	var b bytes.Buffer
	if err := r.body.Execute(&b, p); err != nil {
		panic(err) // the template and its data are this package's own
	}
	return b.Bytes()
}
