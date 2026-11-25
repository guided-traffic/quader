# Design language

The look of the UI: what it is made of, which values carry it in the light and the dark scheme, and where those
values live. Decision [Q-UI-88](../spec/api-and-ui.md#q-ui-88--the-design-language-and-its-two-colour-schemes);
since [Q-UI-128](../spec/api-and-ui.md#q-ui-128--the-references-type-colours-and-measures) the colours, the type and the
measures are the reference's, the owner's GitLab, where [Q-UI-96](../spec/api-and-ui.md#q-ui-96--cogpit-blue-as-the-primary-colour-green-only-for-positive-status)
had cogpit's blue; the shell and list layout it dresses is [Q-UI-68](../spec/api-and-ui.md#q-ui-68--layout-of-the-application-shell-and-the-list-screens)
and [Q-UI-127](../spec/api-and-ui.md#q-ui-127--the-admin-area-has-a-sidebar-of-its-own-behind-an-admin-button-and-the-shell-takes-the-references-frame).
The tokens and how components use them are in [web-ui.md](web-ui.md#design-tokens). How to build a screen with them is in [ui-style-guide.md](ui-style-guide.md).

## Where it comes from

On 2026-11-01 the owner sent two example screens, a dashboard in a dark scheme and a list screen in a light one, and asked
that Quader take their design language and colours, with both schemes. The screens themselves are not in the repository.
Every colour below was measured on them (the most frequent pixel value of a region), not estimated; where a measured
value misses WCAG AA, the table says so and names the value Quader uses instead.

On 2026-11-02 the owner replaced the screens' green accent with the blue of his other product, cogpit (the dark and
light palettes of `internal/ui/fyneui/theme.go` in guided-traffic/cogpit), and kept green for a positive status alone
(Q-UI-96). The surfaces, borders, chrome and type of the screens stay as measured.

On 2026-11-06 the owner made his GitLab the reference for colours, type and measures (Q-UI-128): "Ich will dass du das
Design (auch Schriftgröße) von hier übernimmst", in its admin area. The values were read from the rendered page of his
instance (GitLab 19.0.1, neutral navigation theme) in the dark scheme: the computed styles of the frame, the sidebar, the
breadcrumb bar, the cards, buttons, headings and lists, and the instance's 1068 `--gl-*` custom properties. The light
scheme's semantic colours come from the MIT-licensed token set of `@gitlab/ui` 134.1.3, whose dark tokens agree with the
instance's for every colour (1044 of 1064 properties; the 20 others are font stacks and empty letter spacings); the light
frame's values (`--body-background-color`, the sidebar's current-entry tint) are the instance stylesheet's own rules for
the light scheme, which it ships beside the dark ones. Nothing of GitLab's code, name or logo is in Quader: the values are
written into Quader's own tokens.

## What the language is made of

- **A frame around the work, the work as a panel.** The top bar and the full-height side bar share one fill; the content
  is a panel set into them with 16 px corners and an 8 px gap to the window's right and bottom edge, without a border
  (Q-UI-127, Q-UI-128). The frame is `#ececef` around white panels in the light scheme and `#28272d` around `#18171d`
  panels in the dark one.
- **One accent, blue.** The reference's blue marks what is active or primary: the confirm button (`#1f75cb`, `#63a6e9`),
  links (`#2f5ca0`, `#9dc7f1`), the focus ring, the current navigation entry (a blue tint, its text in a deep or pale step
  of the blue), the selected tab's underline, and as a pale or deep fill a selected row. The accent is not a status.
- **Green is a positive status, and nothing else.** "Active", "ok", "success", "allowed", the added lines of a diff:
  `--quader-status-ok` beside its icon. No other token is green (the unit tier checks it), so green always means "fine".
- **Flat surfaces.** A card is a strong fill (`#ececef`, `#3a383f`) 12 px round with 4 px inside, its title on that fill
  and its content on the page's fill 8 px round; tables and lists are separated by hairlines (`#dcdcde`, `#4c4b51`), not
  by shadows. Controls and menus are 8 px round, dialogs and panels 16 px.
- **Dense, quiet type.** Inter with the reference's lower-case l (`cv05`), 14 px on 21 px at weight 400 for text and
  controls, 12 px for small text (breadcrumbs, counts, dates); sidebar entries at weight 425, the current one at 500;
  headings at 600 with a hair less tracking, page titles 24 to 30 px and section titles 21 to 25 px by the window's width.
  Muted grey (`#626168`, `#bfbfc3`) for secondary text. Upper-case is not used for labels.
- **Data colours beside the accent.** The screens' charts use green, a muted teal and a violet. Green marks a positive
  status only (Q-UI-96), so a chart in Quader does not take it for a series. Quader's usage charts take the teal; the
  vulnerability trend takes the severity scale of the bar ([Q-UI-860](../spec/api-and-ui.md#q-ui-860--one-severity-scale-for-the-bar-the-trend-and-the-badge-the-bars-fills-with-a-mark-tone-of-the-same-hue)); neither marks state.
- **Status as a badge.** A status is an icon and a word ("Active" with a check in a circle) on a pill in the status's
  colour, the reference's badge (Q-UI-128); a line of text that reports a state (a note, a hint) keeps the status colour
  as text.
- **Links.** The link colour, underlined in running text so a link is not told apart by colour alone; in tables and
  key-value lists underlined only under the pointer and on focus (Q-UI-128). There a link is an inline-block box
  (`.content :is(table, dl) a` in `_shell.scss`), so axe's `link-in-text-block` does not count a cell with a link and
  a badge or a second line as running text (Q-UI-148); text that is real running text inside a cell needs the underline.

## The two schemes

| Role | Reference light | Reference dark | Quader light | Quader dark | Notes |
|---|---|---|---|---|---|
| Page, panel | `#ffffff` | `#18171d` | `#ffffff` | `#18171d` | `--quader-surface` |
| Frame (top bar, sidebar) | `#ececef` | `#28272d` | `#ececef` | `#28272d` | `--quader-nav-bg`; the reference's `--body-background-color` of the neutral theme |
| Card, licence bar | `#ececef` | `#3a383f` | `#ececef` | `#3a383f` | `--quader-card-bg`; the content inside on the page's fill |
| Table head | `#fbfafd` | `#28272d` | `#fbfafd` | `#28272d` | `--quader-table-head-bg`, the subtle fill |
| Row under the pointer | `#e9f3fc` | `#1d283e` | `#e9f3fc` | `#1d283e` | `--quader-row-hover` |
| Menu, dialog | `#ffffff` | `#28272d` | `#ffffff` | `#28272d` | `--quader-menu-bg` (menus), `--quader-dialog-bg` (dialogs); menus with a `#bfbfc3` / `#4c4b51` hairline |
| Hairline | `#dcdcde` | `#4c4b51` | `#dcdcde` | `#4c4b51` | `--quader-border`; decoration, no contrast rule |
| Field outline | `#89888d` | `#737278` | `#89888d` | `#737278` | `--quader-control-border`: 3.5:1 and 3.7:1 against the page |
| Text | `#3a383f` | `#ececef` | `#3a383f` | `#ececef` | 11.6:1 and 15.1:1 |
| Heading | `#18171d` | `#ffffff` | `#18171d` | `#ffffff` | `--quader-heading` |
| Muted text | `#626168` | `#bfbfc3` | `#626168` | `#bfbfc3` | 6.1:1 and 9.7:1 |
| Confirm button | `#1f75cb`, white text | `#63a6e9`, `#18171d` text | `#1f75cb` | `#63a6e9` | `--quader-accent-fill`; 4.7:1 and 6.9:1; hover `#2f68b4` / `#9dc7f1` |
| Accent as text, focus ring | `#1f75cb` | `#63a6e9` | `#2f68b4` | `#66a8ea` | `--quader-accent`, moved in its hue: `#1f75cb` is 4.0:1 on a filled field (light), `#63a6e9` 4.49:1 (dark); now 4.7:1 and 4.6:1 |
| Link | `#2f5ca0` | `#9dc7f1` | `#2f5ca0` | `#9dc7f1` | `--quader-link`; 6.6:1 and 10.1:1 |
| Default button | white, `#bfbfc3` hairline | `#89888d` at 40 %, no hairline | as measured | as measured | `--quader-btn-default-*`; the Admin button on the frame: 11.6:1 and 7.0:1 |
| Selection fills | `#e9f3fc` | `#1d283e` | `#e9f3fc` | `#1d283e` | `--quader-accent-container`: the reference's info fill |
| Current navigation entry | `#1f75cb` at 16 %, text `#0a335c` | `#68a8e8` at 25 %, text `#cae0f7` | as measured | as measured | `--quader-nav-active`, `--quader-nav-active-fg` (also the area's title); label at weight 500; 8.9:1 and 7.0:1 |
| Entry under the pointer | `#050506` at 6 % | `#ffffff` at 8 % | as measured | as measured | `--quader-nav-hover`, also the top bar's quiet buttons |
| Search field in the top bar | white, hairline `#050506` at 24 % | `#ffffff` at 8 %, hairline `#ffffff` at 16 % | white, outline `#858489` | as measured, outline `#737278` | `--quader-search-*`, 12 px round; the reference's hairlines are 1.7:1 against the frame, Quader's outline 3.1:1 and 3.1:1 (WCAG 1.4.11) |
| Positive, warning, error status | `#2f7549`, `#995715`, `#c02f12` | `#52b87a`, `#d99530`, `#f6806d` | as measured | as measured | `--quader-status-ok`, `-warn`, `-bad`: the reference's text colours for success, warning and danger |
| Data: teal | `#347da0` | `#347da0` | `#347da0` | `#5cb3d6` | `--quader-chart-usage`, the series of the usage screens (Q-UI-88's teal); the violet of the reference is no token since the trend takes the severity scale (Q-UI-860) |

Since Q-UI-127 the top bar and the side bar share one fill; the panel's fill and rounded corners set the content off from
the frame instead of a step between the two bars.

No component library supplies a palette: every colour a style reads is a token of
[_tokens.scss](../../web/src/styles/_tokens.scss) (Q-UI-126). The error and the banner colours that Angular Material's
generated palette used to provide are literal `light-dark()` pairs there, with the values the screens had; the toast and the
tooltip share the reference's strong feedback grey, one value in both schemes (Q-UI-1063, Q-UI-1065).

## Vulnerability grades

The grade of a version (R-UI-090 to R-UI-093, [Q-UI-370](../spec/api-and-ui.md#q-ui-370--the-grades-take-cogpits-colours-and-s-a-and-b-are-the-greens-of-a-good-grade))
takes the owner's cogpit scale, the same value in both schemes from S to E; N/A is the scheme's quiet grey. The crest's fill runs
from 25 % lighter (`--quader-grade-highlight`) at the top to 25 % darker (`--quader-grade-shadow`) at the bottom; S has a gold rim.
How to use the pieces is the style guide's [Vulnerability grade](ui-style-guide.md#vulnerability-grade).

| Token | Light | Dark | Its letter or count | Contrast (computed by `palette.spec.ts`) |
|---|---|---|---|---|
| `--quader-grade-s` | `#1f5f55` | `#1f5f55` | `--quader-grade-s-ink` `#a8f0d4`, rim `--quader-grade-rim` `#d4b25a` | 5.7:1 |
| `--quader-grade-a` | `#3e8e72` | `#3e8e72` | `--quader-grade-ink` `#0b1220` | 4.7:1 |
| `--quader-grade-b` | `#8fd3b6` | `#8fd3b6` | `--quader-grade-ink` | 10.8:1 |
| `--quader-grade-c` | `#e9ac4b` | `#e9ac4b` | `--quader-grade-ink` | 9.3:1 |
| `--quader-grade-d` | `#c8691c` | `#c8691c` | `--quader-grade-ink` | 4.9:1 |
| `--quader-grade-e` | `#c23b33` | `#c23b33` | `--quader-grade-ink-light` `#ffffff` | 5.3:1 |
| `--quader-grade-na` | `#ececef` | `#3a383f` | `--quader-grade-na-ink` `#626168` / `#bfbfc3` | 5.2:1 and 6.3:1 |
| `--quader-sev-critical` | `#7f1d1d` | `#7f1d1d` | `--quader-sev-ink-light` `#ffffff` | 10.0:1 |
| `--quader-sev-high` | `#c9302a` | `#c9302a` | `--quader-sev-ink-light` | 5.3:1; cogpit's `#d9403a` is 4.4:1 with white, so it is one step deeper |
| `--quader-sev-medium` | `#e8a33d` | `#e8a33d` | `--quader-sev-ink` `#1c1204` | 8.6:1 |
| `--quader-sev-low` | `#f5d68e` | `#f5d68e` | `--quader-sev-ink` | 13.1:1 |
| `--quader-sev-unknown`, `-unknown-light` | `#b9bdc4`, `#e0e3e6` | `#45505c`, `#26303c` | `--quader-on-surface`, on both stripes | 6.1:1 and 9.0:1 light, 7.0:1 and 11.3:1 dark |

A severity without findings is its colour at 28 % over the surface, with its 0 in `--quader-on-surface` (at least 4.5:1, a test of
its own). S, A and B are green on purpose: the good end of the scale is a positive status in Q-UI-96's sense, and the palette test
admits these tokens (and S's mint letter) beside `--quader-status-ok`; C to E and N/A are never green.

### The severity scale

One severity has one colour in the bar, the trend and the badge, the owner's choice of 2026-11-20 ([Q-UI-860](../spec/api-and-ui.md#q-ui-860--one-severity-scale-for-the-bar-the-trend-and-the-badge-the-bars-fills-with-a-mark-tone-of-the-same-hue)). The fills
above are the bar's segments, the trend's bands and the legend's swatches. The bar's Medium and Low fills are not marks against the
page (2.2:1 and 1.4:1 on white), which the bar does not need, because it carries its counts as text; so each severity has a
**mark tone** of its fill's hue that is: the edge of 1.5 px on top of a band of the trend, a band's swatch's top in the legend,
and the icon of the severity's badge (`<quader-severity>`), whose word stays in the text colour. None and Unknown are no step of
the scale: None is a neutral grey, Unknown the bar's hatch everywhere (a pattern in the chart) with the muted grey as its mark.

| Token | Light | Dark | Page (= raised) | Card | Table head | Row hover |
|---|---|---|---|---|---|---|
| `--quader-sev-critical-mark` | `#6c0b17` | `#d95853` | 12.4:1 / 4.6:1 | 10.5:1 / 3.0:1 | 11.9:1 / 3.9:1 | 11.0:1 / 3.8:1 |
| `--quader-sev-high-mark` | `#9c1e18` | `#f6815b` | 8.0:1 / 7.0:1 | 6.8:1 / 4.5:1 | 7.7:1 / 5.8:1 | 7.2:1 / 5.8:1 |
| `--quader-sev-medium-mark` | `#a15e04` | `#ffb755` | 5.1:1 / 10.3:1 | 4.3:1 / 6.7:1 | 4.9:1 / 8.6:1 | 4.5:1 / 8.5:1 |
| `--quader-sev-low-mark` | `#a78314` | `#ffe8b0` | 3.6:1 / 14.8:1 | 3.0:1 / 9.6:1 | 3.4:1 / 12.3:1 | 3.2:1 / 12.2:1 |
| `--quader-sev-none` | `#737278` | `#89888d` | 4.8:1 / 5.1:1 | 4.0:1 / 3.3:1 | 4.6:1 / 4.2:1 | 4.2:1 / 4.2:1 |
| `--quader-sev-unknown-mark` | `#626168` | `#a4a3a8` | 6.1:1 / 7.1:1 | 5.2:1 / 4.6:1 | 5.9:1 / 5.9:1 | 5.5:1 / 5.9:1 |
| `--quader-chart-usage` | `#347da0` | `#5cb3d6` | 4.6:1 / 7.5:1 | 3.9:1 / 4.9:1 | 4.4:1 / 6.3:1 (not a pair of the test) | 4.1:1 / 6.2:1 (not a pair of the test) |

Each cell is light / dark, against 3:1 (WCAG 1.4.11); the tightest are the light Low mark on the card (3.02:1) and the dark Critical
mark on the card (3.01:1). `palette.spec.ts` computes every pair, and two more rules of the scale:

- **Neighbours stay apart.** Critical, High, Medium, Low and None, the fills and the marks each, differ by at least 9 in CIEDE2000
  between neighbours under the deuteranopia and the protanopia matrices of Machado et al. (2009) at full severity, in linear RGB.
  The closest: the light marks of Medium and Low under deuteranopia (9.1) and the dark marks of High and Medium under deuteranopia
  (9.5); the fills, the same in both schemes, at least 11.3 (Medium and Low, deuteranopia). A later change of one tone fails there
  before two steps look alike to a reader with a red-green deficiency.
- **A mark is a tone of its fill.** Its hue in CIE LCh is within 12 degrees of its fill's (the widest today: the dark High mark,
  9.5 degrees), so that a change of one cannot split them.

In the dark scheme the deep Critical band is lit only by its edge: at 390 px a band of two Critical findings is one pixel row of
`#d95853` between High's fill (about 1.4:1) and the page (4.6:1), visible and faint ([Q-UI-861](../spec/api-and-ui.md#q-ui-861--a-bands-edge-is-drawn-on-every-day-its-severity-has-a-finding-on-the-bands-true-top)).

## The two-pane browser

The Documentation tab of an Ansible collection ([Q-UI-241](../spec/api-and-ui.md#q-ui-241--the-documentation-tab-is-the-file-browsers-two-panes-a-tree-with-a-search-field-and-the-page))
is the one screen built from two panes; a screen that browses a hierarchy beside its details takes the same pattern. The file browser
that introduced it ([Q-UI-98](../spec/api-and-ui.md#q-ui-98--the-file-browser-is-two-panes-with-a-toolbar-and-a-details-header)) went with
[ADR-203](../decisions.md#adr-203--browse-goes-a-repositorys-files-live-on-its-page): a repository's files are pages of the repository.
Its styles are the global partial [_browse.scss](../../web/src/styles/_browse.scss).

- Two flat bordered surfaces (`--quader-surface-raised`, the hairline border, the 6 px radius) side by side. The tree pane is
  `clamp(18rem, 30%, 26rem)`; the details take the rest. The details pane is as long as its page and scrolls with the page; the tree
  pane is a navigation column that sticks beside it and scrolls its own rows, at most as high as the panel's view
  (`--quader-page-height` less the panel's padding above and below), and a short page's pane is as high as the tree
  ([R-UI-215](../spec/api-and-ui.md#r-ui-215--one-scrollbar-per-page), [Q-UI-1112](../spec/api-and-ui.md#q-ui-1112--a-navigation-column-sticks-beside-the-page-with-a-scroll-of-its-own-any-other-side-column-sticks-only-while-it-fits)).
  A new page brings the details pane's top into view.
- A pane head on the table-head fill: in the tree pane the filter field, then the shared filter toggle, a sort icon button and
  a count; in the details pane the path as a breadcrumb and the actions as a toolbar. Applied filters are the shared chips under
  the head. The shared filter panel opens inside the tree pane in place of the tree, without its own border or shadow.
- A tree row is 32 px high: a chevron, an icon in the muted tone, the name, then small outlined badges (11 px) for the format
  and the labels "cache", "resolves from" and "disabled". The selected row is `--quader-accent-container`; the focus ring is the
  solid 2 px ring inside the row.
- A table in a pane is the shared data table, which grows the pane and the page; its head scrolls with them.
- Under 48 rem of content width (the `content` container query) one pane shows at a time, each as long as it is, and the details get
  a back button, which brings the entry that was read into view in the tree.

## Controls

Since 2026-11-06 the controls are Quader's own, not Angular Material's
([Q-UI-126](../spec/api-and-ui.md#q-ui-126--quaders-own-controls-in-place-of-angular-material)): the owner asked for the
design language of a sign-in page he showed, flat controls with the labels above the fields. They are plain HTML elements
with the classes of [_controls.scss](../../web/src/styles/_controls.scss), and the composite widgets take their behaviour
from the CDK and `@angular/aria`. The move is finished: `@angular/material` is no dependency of the web UI any more, and
no screen carries a Material control or a `--mat-sys-*` token. Their measures and colours are the reference's (Q-UI-128):
the confirm button, the default button with its grey fill, the quiet one in the text colour, all 32 px and 8 px round
with the label at weight 400; a tab's label is in the text colour at 14 px, the current one at weight 600 over a 2 px line
in `--quader-tab-indicator` (`#1f75cb`, `#428fdc`), the strip on a hairline. The shared data table has the reference's shape: no frame, on the
page's fill, 16 px in every cell, the head in the heading colour at weight 600 between two hairlines, a hairline under
every row.

| Control | Markup | Look |
|---|---|---|
| Primary button | `<button class="btn btn-confirm">` | the accent fill (`--quader-accent-fill`), `--quader-on-accent` text, a deeper or lighter step of the hue on hover |
| Secondary button | `class="btn btn-default"` | white with a `#bfbfc3` hairline, `#ececef` and a `#89888d` hairline on hover (light); `#89888d` at 40 % without a hairline, 64 % on hover (dark) |
| Quiet button | `class="btn btn-tertiary"` | text only, a neutral veil on hover (`#a4a3a8` at 16 %, `#89888d` at 40 %) |
| Destructive button | `class="btn btn-danger"` | `--quader-danger-fill` (the bad status's hue) |
| Icon button | `class="btn btn-icon"` with an `aria-label` | 32 × 32, text only |
| Modifiers | `btn-block`, `btn-sm` | full width; 24 px high at the same text size |
| Text field, select, area | `<label class="field-label" for>` above `<input class="text-input">`, `select-input`, `area-input` | `--quader-control-bg` (white; `#050506` at 40 % over the page in the dark scheme), a 1 px `--quader-control-border` (3.5:1 light, 3.7:1 dark against the page, WCAG 1.4.11) that darkens or lightens under the pointer, the semibold label above |
| Tabs that are links | `<nav class="tab-nav">` with `<a class="tab-link" aria-current="page">` | the reference's tab strip: labels in the text colour, 16 px above and below; the current one at weight 600 over a 2 px `--quader-tab-indicator` line; a neutral veil and a `--quader-border-strong` line under the pointer; the strip on a hairline (the package views). Wider than its place, the strip scrolls without a scrollbar, and an edge beyond which tabs lie carries a 2rem fade from `--quader-surface` with a 16 px chevron that pages the strip by a click; the current and the focused tab are scrolled clear of it (`<nav quaderTabStrip class="tab-nav">`, `TabStrip` of [tab-strip.ts](../../web/src/app/shared/ui/tab-strip.ts); the web lint refuses a strip without it; GitLab's scrolling tabs; [Q-UI-1074](../spec/api-and-ui.md#q-ui-1074--a-tab-strip-wider-than-its-place-scrolls-without-a-scrollbar-with-a-fade-and-a-chevron-at-the-edge)). Before it scrolls, a strip whose tabs do not fit at 12 px at each side of a label takes them to 8 px (`.tab-nav.tight`, [Q-UI-1090](../spec/api-and-ui.md#q-ui-1090--a-tab-strip-takes-its-tabs-closer-before-it-scrolls)) |
| Badge | `<span class="badge">` (neutral), with `ok`, `warn`, `bad` or `info`; every `<span class="ok\|warn\|bad">` that holds an icon and a word | the reference's badge: a pill 20 px high, 12 px text at weight 400, a 12 px icon 4 px before it; `--quader-badge-*-bg`, `-fg`, `-icon`: a pale fill with deep text (light), a full fill with near-black text (dark) |
| Count on an icon button | `<span class="downloads-count" aria-hidden="true">` inside the top bar's `btn btn-icon` **Downloads** ([upstream-downloads-button.ts](../../web/src/app/shared/upstream-downloads/upstream-downloads-button.ts)); the button's accessible name carries the number | the reference's badge on an icon: a pill 16 px high and at least 16 px wide on the icon's upper end corner, 11 px digits at weight 600, `99+` above 99; the information badge's fill and text (`--quader-badge-info-bg`, `-fg`), so it reads as a state in both schemes and not as a choice (a list's filter count keeps the accent); no motion ([Q-UI-1070](../spec/api-and-ui.md#q-ui-1070--the-downloads-button-stands-between-documentation-and-the-theme-and-counts-what-runs)) |
| Checkbox, radio | native `<input type="checkbox">`; `<quader-checkbox>` ([checkbox.ts](../../web/src/app/shared/ui/checkbox.ts)) for a checkbox with its label as content, an optional `describedBy` note and an `ariaLabel` for a row's box | 16 px, `accent-color` the accent; the input sits inside its `label.check-row`, so the whole label is the click target |
| Tabs that switch a view | `<quader-tab-group>` with `<quader-tab label>` ([tabs.ts](../../web/src/app/shared/ui/tabs.ts)); `<ng-template quaderTabContent>` for lazy content | the strip of the row above (`.tab-nav`, `.tab-link`, selected by `aria-selected`); the roles, arrow keys, Home and End and the single tab stop are `@angular/aria`'s tabs, the selected tab's panel is a `tabpanel` named by its tab; only the selected tab's content is in the page |
| Dialog | `QuaderDialog.open(Component, { data, width, maxWidth, disableClose })` with `<h2 quaderDialogTitle>`, `<div class="dialog-body">`, `<div class="dialog-actions">` and `quaderDialogClose` on a closing button ([dialog.ts](../../web/src/app/shared/ui/dialog.ts), [_dialog.scss](../../web/src/styles/_dialog.scss)) | the CDK dialog: `role="dialog"`, `aria-modal`, named by its title, the focus trapped inside and returned to the opener, Escape and the backdrop close it unless `disableClose`; the panel on `--quader-dialog-bg`, 16 px round, the title in the heading-3 size, the body scrolling between title and actions, the actions right-aligned |
| Menu | `cdkMenuTriggerFor` and `<div class="menu" cdkMenu>` with `class="menu-item" cdkMenuItem` (or `cdkMenuItemRadio`) | the CDK menu, in `_shell.scss`; the user menu, the sort menu of the browser and the mirror's run menu |
| Panel hung from a button | the button `btn btn-default btn-sm` with its word and `keyboard_arrow_down`, `aria-haspopup="dialog"` and `aria-expanded`; the panel a `cdkConnectedOverlay` with `class="menu"` and `role="dialog"` ([client-command-menu.ts](../../web/src/app/features/packages/client-command-menu.ts), Q-UI-1066) | GitLab's **Code** menu: the menu's fill, hairline, radius and shadow, below the button with its end on the button's end, as wide as its longest command, 24 to 48 rem and at most the window less 32 px; sections divided by a hairline, each a semibold 14 px heading (`menu-heading`) over a read-only field with its joined copy button; the focus goes into the panel and stays there, Escape, a click outside and the button close it and give the focus back to the button |
| Tooltip | `[quaderTooltip]="text"` ([tooltip.ts](../../web/src/app/shared/ui/tooltip.ts)) | the reference's tooltip ([Q-UI-1065](../spec/api-and-ui.md#q-ui-1065--the-tooltip-is-the-references-the-toasts-grey-above-the-element-with-an-arrow)): `--quader-feedback-strong-bg` with `--quader-feedback-strong-fg` in both schemes, 4 px round (`--quader-radius-s`), `--quader-elevation-2`, 8 by 12 px of padding, 12 px text on a 16 px line (`--quader-type-body-small`), centred, at most 200 px wide; above the element, under it where the window has no room above, with an arrow of 8 by 4 px in the 4 px between them that points at the element's middle also where the window's edge pushes the bubble aside; a scroll moves it with the element. On pointer and keyboard focus, closed by Escape, blur or leaving it (WCAG 1.4.13); the element is always described by the text (the CDK's hidden description), the bubble itself is `aria-hidden` |
| Toast | `Notifier.status(text, action?)`, `Notifier.alert(text, action?)` ([notifier.ts](../../web/src/app/core/notifier.ts)), drawn once by `<quader-notices>` ([notices.ts](../../web/src/app/shared/notices.ts)) | the reference's toast ([Q-UI-1063](../spec/api-and-ui.md#q-ui-1063--feedback-outside-forms-is-the-references-toast-at-the-bottom-right-one-confirmation-at-a-time)): 24 px from the window's bottom right corner, at most 586 px wide (below 576 px across the window with 8 px each side); `--quader-feedback-strong-bg` with `--quader-feedback-strong-fg` in both schemes, 8 px round, `--quader-elevation-2`, 16 by 24 px of padding, 14 px text at weight 300 on a 16 px line; the action a bold link-like button in `--quader-feedback-strong-link`, the close button a 24 px icon button **Close**. One confirmation at a time; a notice with an action and an error each keep their toast; an error has the error notice's colours and its icon |
| Text field, select with label and error | `<quader-text-field>`, `<quader-select-field>` ([fields.ts](../../web/src/app/shared/access/fields.ts)) | the label above, `text-input` / `area-input` / `select-input`, the hint and the error as text below, joined by `aria-describedby`, with `aria-invalid` on an error |
| Focus | every control | `--quader-focus-ring` (2 px solid accent), offset 2 px for buttons and 1 px for fields |

One height (`--quader-control-height`, 32 px) and one radius (`--quader-control-radius`, 8 px) for every control, the label at
14 px and weight 400 with a 16 px icon 4 px before it, as the reference sets its buttons. A disabled
control is `--quader-control-disabled-bg` with `--quader-control-disabled-fg` (WCAG exempts disabled controls from the
contrast rule). The screens without the shell (sign-in, signed out) are one column of at most 25rem in the middle of the
page, under the lockup (`.bare`, `.bare-panel`, `.bare-logo` in `_shell.scss`, Q-UI-125).
[palette.spec.ts](../../web/src/styles/palette.spec.ts) computes every label on its button fill (at rest and under the
pointer), the field's text and placeholder on the field (4.5:1), and the field outline and the primary fill against the
page (3:1), in both schemes.

## Logo

The mark is the owner's (2026-11-17, [ADR-199](../decisions.md#adr-199--the-product-is-renamed-from-trove-to-quader-with-the-token-prefix-qdr_-and-a-new-mark)).
Quader's mark is a wall of three courses, the right stone of the middle course coloured. The mark of the Quader Bastion is a
tower of three courses: the battlements with the middle merlon coloured, the overhanging crown and the shaft. Both stand on
a grid of 64 with a margin of 4; every stone is 16 high with corners rounded by 1.5, and the joints are 4. At 16 px a stone
is 4 px and a joint 1 px, so at every multiple of 16 px each edge falls on a whole pixel. The coordinates are fixed:
`MARKS` in [web/scripts/brand-logo.mjs](../../web/scripts/brand-logo.mjs) holds them, and nothing tunes them.

| Part | On a light ground | On a dark ground |
|---|---|---|
| The stones and the wordmark | `#28272d` | `#ececef` |
| The coloured stone (sandstone) | `#b4690e` | `#e9a13b` |
| The tile of the square forms and of `favicon.ico` | `#18171d` | `#18171d` |

The script writes into [web/public/brand/](../../web/public/brand/) the mark, the horizontal lockup and the square app icon
of each: `quader-mark.svg`, `quader-horizontal.svg`, `quader-square.svg`, `quader-bastion-mark.svg`,
`quader-bastion-horizontal.svg` and `quader-bastion-square.svg`, and removes every other file of that folder. The mark and
the lockup switch their colours through `prefers-color-scheme` inside the SVG; the square is the mark above its words on the
dark tile, the same in both schemes. The wordmark is outlined paths of Inter, not `<text>`, so it renders the same on every
platform: "Quader" in Bold, and in the Bastion's lockup "Bastion" after it in Regular (in the square on a line of its own).
The script reads `inter-latin-700-normal.woff` and `inter-latin-400-normal.woff` of `@fontsource/inter` with `opentype.js`
(both dev dependencies, pinned exactly, used only by the script; the static files are the same Inter release as the UI's
variable font, because opentype.js reads no `.woff2`). The words are set with the font's advances and no tracking; the pair
kerning comes from `Font.getKerningValue`, which returns 0 for every pair of both words. In the lockup the word has a cap
height of 40, centred on the mark's box (the caps from 12 to the baseline at 52), and its ink starts one stone height (16)
after the mark. The lockup's width is rounded up to a multiple of 8 units, so at 16 px of height or a multiple of it the
lockup is a whole, even number of pixels wide and stays on whole pixels when it is centred.

The favicon is the wall. [web/public/favicon.svg](../../web/public/favicon.svg) is the mark without a tile, its colours
switching with `prefers-color-scheme`. [web/public/favicon.ico](../../web/public/favicon.ico) holds PNG frames of 16, 32
and 48 px: the wall at its full size in the dark ground's colours on the dark tile, whose corner radius of 12 (of 64) stays
clear of the corner stones (a radius above 15 would cut them); at 16 px the tile shows as a border of 1 px. The script
rasterises the frames itself (8 × 8 samples per pixel), so no image tool is needed and the bytes are deterministic;
[index.html](../../web/src/index.html) links the SVG first and the ICO as the fallback. The wordmark as paths and the
generated ICO are what [Q-UI-145](../spec/api-and-ui.md#q-ui-145--the-logos-wordmark-as-paths-from-inter-bold-and-a-reduced-mark-as-the-favicon)
chose; its reduced lane mark is gone with ADR-199.

In the app, `<quader-mark>` ([shared/brand/quader-mark.ts](../../web/src/app/shared/brand/quader-mark.ts)) draws the mark
inline from `shared/brand/mark-geometry.ts`, which the same run of the script writes (formatted with the repository's
Prettier settings). Its colours are the tokens `--quader-mark-stone` and `--quader-mark-accent`, so it follows the theme the
viewer chose, which an `<img>` of the files above cannot: an image only knows the system's colour scheme. The top bar shows
the mark at 32 px, as high as the bar's controls, and the name beside it as text, Inter at weight 700; below 360 px the name
is visually hidden and stays the link's accessible name. The sign-in and signed-out screens show the lockup
(`[wordmark]="true"`, an image named Quader) 48 px high (`.bare-logo` in `_shell.scss`). [palette.spec.ts](../../web/src/styles/palette.spec.ts)
holds both colours at 3:1 against the top bar and the page in both schemes: the stones reach 12.6:1 to 15.1:1, the
coloured stone 3.6:1 on the light top bar, 4.2:1 on the light page, 6.8:1 and 8.2:1 on the dark ones. To change the
geometry or a colour, change the script (a colour also in `_tokens.scss`) and run `node scripts/brand-logo.mjs` in `web/`.

The mark is also the spinner of every load, the owner's pick of 2026-11-25
([R-UI-217](../spec/api-and-ui.md#r-ui-217--every-load-shows-the-spinner-the-owner-picked),
[Q-UI-1229](../spec/api-and-ui.md#q-ui-1229--the-spinner-is-the-mark-three-rows-of-six-stones-lit-clockwise-in-sandstone)):
`<quader-spinner>` ([shared/ui/spinner.ts](../../web/src/app/shared/ui/spinner.ts)) draws the same six stones from `mark-geometry.ts`,
at rest in `--quader-spinner-stone` (`#c9c8cd` light, `#5c5b62` dark: a grey of no hue between the hairline and the muted text), one
after another lit in the coloured stone's sandstone, clockwise from the top left, a round in 1.8 s; 16 px on a line or in a button, 48 px
for a page (16 and 48 keep every edge on a whole pixel). Under reduced motion the first stone stays lit and the mark breathes. The look is
[_spinner.scss](../../web/src/styles/_spinner.scss), global, because [index.html](../../web/src/index.html) draws the spinner before the
app starts. The spinner is the one place the sandstone moves; it is no state colour.

## Format logos

A package format is shown by its owner's logo where the owner's terms allow it, else by the neutral `inventory_2` glyph
(owner, 2026-11-11, [Q-UI-225](../spec/api-and-ui.md#q-ui-225--a-format-shows-its-owners-logo-where-the-owners-terms-allow-it-else-the-neutral-glyph)):
Debian's Open Use Logo, the NuGet logo and the Open Container Initiative's icon; npm, Ansible Galaxy and the generic format
keep the glyph, because npm's and Ansible's owners allow their logos only with permission and plain files have no brand.
The files are the owners' own, unchanged, under [web/public/formats/](../../web/public/formats/NOTICE.txt), with their
owners, sources, licences and SHA-256 in `NOTICE.txt`. [format-icon.ts](../../web/src/app/shared/format-icon.ts) decides
(`formatIcon`) and draws (`<quader-format-icon>`): a square of one em, an `<img>` with an empty `alt`, because the format's
name is always beside it.

No logo is redrawn or recoloured. The dark scheme shows the owner's own variant for a dark ground where there is one (the
white OCI icon, NuGet's white mono PNG), switched by `.logo-on-light` and `.logo-on-dark` in
[_icons.scss](../../web/src/styles/_icons.scss) under Quader's theme; Debian's red swirl has none and stands on the plate
`--quader-logo-plate` (`#ececef` in the dark scheme, transparent in the light one), because the dark surfaces leave it at 1.9:1
to 2.3:1. [Q-UI-226](../spec/api-and-ui.md#q-ui-226--a-logo-is-an-image-of-the-owners-file-and-the-dark-scheme-shows-the-owners-light-variant-or-a-plate)
has the measurements and the options.

## Pictures

The owner on 2026-11-20: "Bilder haben keine Einschränkungen der Palette" ([Q-UI-800](../spec/api-and-ui.md#q-ui-800--set-up-client-takes-variant-a-snippets-are-windows-the-page-has-pictures-and-less-text)).
A picture may carry colours of its own; the page's controls and text keep the palette above. Two kinds exist, both tokens of
`_tokens.scss` with a light and a dark value:

- **A snippet's window** (`--quader-code-*`, [Q-UI-801](../spec/api-and-ui.md#q-ui-801--a-snippets-window-is-an-editor-for-a-file-and-a-terminal-for-a-command-coloured-by-its-language)):
  light paper (`#fbfafd`) in the light scheme, dark ink (`#0f1015`) in the dark one, a bar a step darker or lighter, and one syntax
  colour per kind of piece: command (violet, bold), flag (pink), string (brown or yellow), key and variable (blue), address (teal,
  past the hue of the positive green), punctuation (grey), comment (the muted tone, italic). Where the token goes is an orange chip,
  what the reader replaces a yellow tint with a dashed line and italics. Every text colour keeps 4.5:1 on the window and the marks
  on their tints; `palette.spec.ts` computes them.
- **The flow of Set up client** (`--quader-art-*`): the machine in the window's colours, the lanes, the token's disc and key in
  the colour of the mark's coloured stone, the repository as a piece of Quader's wall (`--quader-mark-stone`,
  `--quader-mark-accent`), and the parcels in the format's accent (`--quader-format-accent-<format>`,
  [Q-UI-803](../spec/api-and-ui.md#q-ui-803--a-format-adds-one-accent-pair-for-its-pictures-a-format-without-one-takes-the-neutral-pair),
  `formatAccent` of [format-accent.ts](../../web/src/app/shared/format-accent.ts)), with a shade for their tape. The accent is
  decoration, never text and never a control.

## Where the values live

| File | What it holds |
|---|---|
| [web/src/styles/main.scss](../../web/src/styles/main.scss) | The partials in order, the CDK's overlay structure (`cdk.overlay()`, which dialogs, menus and tooltips open in), the body type and the page title |
| [web/angular.json](../../web/angular.json) | `@fontsource-variable/inter/wght.css` ahead of `main.scss` in the build's styles: Inter as one variable font (weights 100 to 900) in unicode-range subsets, so a page in Latin script loads the 48 KB Latin file; OFL-1.1, its licence in the build's `3rdpartylicenses.txt` |
| [web/src/styles/_tokens.scss](../../web/src/styles/_tokens.scss) | Quader's own properties: the page palette (`--quader-surface`, `--quader-on-surface`, `--quader-border`, `--quader-text-muted`, `--quader-accent`, `--quader-dialog-bg`, `--quader-error*`, `--quader-inverse-*`, `--quader-banner-*`), the type (`--quader-font-*`, `--quader-text-*`, `--quader-heading-*`, `--quader-type-*` as font shorthands), the radii (`--quader-radius-s` to `-full`), the link, the heading colour, the focus ring, the raised surface, the card, the table head, the frame (`--quader-nav-*`, the panel's radius and gap, the breadcrumb bar's height), the search field, the menus, the status colours, the plate behind a format logo (`--quader-logo-plate`), the mark's colours (`--quader-mark-stone`, `--quader-mark-accent`), the grades and the severity scale (`--quader-grade-*`,
`--quader-sev-*`, [Vulnerability grades](#vulnerability-grades)), the usage charts' series (`--quader-chart-usage`), the pictures (`--quader-code-*`, `--quader-art-*`, `--quader-format-accent*`, [Pictures](#pictures)) |
| [web/src/styles/_controls.scss](../../web/src/styles/_controls.scss) | The controls of the table above, from the `--quader-control-*`, `--quader-accent-fill`, `--quader-btn-*` and `--quader-danger-*` tokens |
| [web/src/styles/_dialog.scss](../../web/src/styles/_dialog.scss) | The dialog panel: backdrop, container, title, body, actions |
| [web/src/styles/_shell.scss](../../web/src/styles/_shell.scss) | How the frame uses them: the sidebar's blocks under their context titles (R-UI-168), its entries (28 px, 8 px round, a 16 px icon in a 24 px box), the current entry, the entries of **Recently opened** (the project's small mark and the key, R-UI-167), the project switcher on top of a project's sidebar (a 40 px button with the project's mark, `<quader-project-mark size="medium">`: its initial in a 32 px rounded square on `--quader-accent-fill`, drawn by `_list-screens.scss` as in every list of projects (R-UI-102), a panel on the menu's fill whose projects carry the small mark, [Q-UI-129](../spec/api-and-ui.md#q-ui-129--the-project-is-the-context-of-the-content-chosen-from-your-projects)), the panel, the breadcrumb bar (40 px), the top bar's actions and the avatar (32 px round on `--quader-surface-variant`, holding the user's generated Bottts robot of `<quader-avatar>` instead of a letter, owner 2026-11-07, Q-UI-168; the SVG is transparent, so the fill follows the theme), the menus |
| [web/scripts/build-embed.mjs](../../web/scripts/build-embed.mjs) | The notices of the bundled software: Angular's `3rdpartylicenses.txt` (the Inter font's OFL-1.1 notice among the libraries') plus the icon font's Apache-2.0 licence, which Angular does not collect, and the format logos' `formats/NOTICE.txt` (Q-UI-227); written as `<locale>/3rdpartylicenses.txt` into the embedded build, and the build fails when a licence or a logo's notice is missing (Q-UI-144) |
| [web/src/styles/_icons.scss](../../web/src/styles/_icons.scss) | The icon font and the scheme switch of the format logos (`.logo-on-light`, `.logo-on-dark`) |
| [web/src/styles/palette.spec.ts](../../web/src/styles/palette.spec.ts) | The contrast of the pairs below, computed from the two stylesheets, and the rule that only `--quader-status-ok` is green |

A scheme is chosen by `color-scheme` on the root element: without a choice the browser's setting decides, the theme button
of the top bar sets `data-theme="light"` or `"dark"` (R-API-021). Every colour is a `light-dark()` pair, so a component
never asks which scheme is on.

## Changing a colour

1. Change the pair in `_tokens.scss`, never in a component.
2. Recompute the contrast of every pair it takes part in (text 4.5:1, large text and marks 3:1) and update the numbers in
   the comment above the token and in the table here. A text tone is not only on the page: a muted fill is
   `--quader-surface-variant` (`#ececef` in the light scheme, `#3a383f` in the dark one), a dialog sits on
   `--quader-dialog-bg`, and a selected row on `--quader-accent-container`. The accent has to pass on those too (found by axe when the accent was `#1d7a43`: 4.3:1 on the field; and
   the reason cogpit's `#0a6fd6` is not the light accent: 4.0:1 there). `palette.spec.ts` recomputes the pairs in
   `make web-test` and fails below the minimum; a new surface under the accent or the link gets a pair there.
3. Run the browser journeys ([e2e.md](e2e.md#browser-journeys)): they check `color-contrast` with axe in the light scheme
   (English) and the dark one (German). The jsdom tier does not compute colours.

## Contrast of the text tones on the surfaces they sit on

Computed on 2026-11-06 from the values in `main.scss` and `_tokens.scss` (WCAG 2 relative luminance; a translucent fill
is composited over the surface beneath it in sRGB, as a browser paints it), for the same pairs `palette.spec.ts` holds.
The browser tier's axe `color-contrast` confirmed the green palette of Q-UI-88 on 2026-11-01 (a local Quader process, the
break-glass administrator, 108 checks: 28 routes with the manual's pages, the new-group dialog, the new-token form
unfocused, focused and invalid, a form field touched while empty, the help opened by "Documentation", and the list, detail and
audit screens with data, light in English and dark in German; no violation and no "incomplete" result). The palettes of
Q-UI-96 and Q-UI-128 are computed only: the browser tier has not run against them yet.

| Pair | Light | Dark |
|---|---|---|
| Accent as text on the page, the filled field, a dialog | 5.6:1, 4.7:1, 5.6:1 | 7.1:1, 4.6:1, 5.9:1 |
| Text on the confirm button (`--quader-on-accent` on `--quader-accent-fill`) | 4.7:1 | 6.9:1 |
| Link on the page, on the current audit row | 6.6:1, 5.9:1 | 10.1:1, 8.3:1 |
| Focus ring on the page, the filled field | 5.6:1, 4.7:1 | 7.1:1, 4.6:1 |
| Current navigation entry: its text on its tint, the area title on the frame | 8.9:1, 10.9:1 | 7.0:1, 10.9:1 |
| Admin button: its label on the default fill over the frame | 11.6:1 | 7.0:1 |
| Positive status on the page, on the current audit row | 5.6:1, 5.0:1 | 7.2:1, 6.0:1 |

<details>
<summary>Every pair, computed</summary>

| Foreground | On | Needs | Light | Dark | Where |
|---|---|---|---|---|---|
| `primary` | page | 4.5:1 | `#2f68b4` on `#ffffff`: 5.6:1 | `#66a8ea` on `#18171d`: 7.1:1 | accent text: focused label, outlined and text button, tab label, access tabs |
| `primary` | raised | 4.5:1 | `#2f68b4` on `#ffffff`: 5.6:1 | `#66a8ea` on `#18171d`: 7.1:1 | current entry of the help contents |
| `primary` | table head | 4.5:1 | `#2f68b4` on `#fbfafd`: 5.4:1 | `#66a8ea` on `#28272d`: 5.9:1 | button label in a table head |
| `primary` | row hover | 4.5:1 | `#2f68b4` on `#e9f3fc`: 5.0:1 | `#66a8ea` on `#1d283e`: 5.9:1 | button label in a hovered row |
| `primary` | menu | 4.5:1 | `#2f68b4` on `#ffffff`: 5.6:1 | `#66a8ea` on `#28272d`: 5.9:1 | menu and select panel |
| `primary` | dialog | 4.5:1 | `#2f68b4` on `#ffffff`: 5.6:1 | `#66a8ea` on `#28272d`: 5.9:1 | focused label and button label in a dialog |
| `primary` | filled field | 4.5:1 | `#2f68b4` on `#ececef`: 4.7:1 | `#66a8ea` on `#3a383f`: 4.6:1 | focused label of a filled form field |
| `on-primary` | primary | 4.5:1 | `#ffffff` on `#2f68b4`: 5.6:1 | `#18171d` on `#66a8ea`: 7.1:1 | filled button, step number, filter count |
| `--quader-link` | page | 4.5:1 | `#2f5ca0` on `#ffffff`: 6.6:1 | `#9dc7f1` on `#18171d`: 10.1:1 | link in the content, breadcrumb, link button |
| `--quader-link` | raised | 4.5:1 | `#2f5ca0` on `#ffffff`: 6.6:1 | `#9dc7f1` on `#18171d`: 10.1:1 | help card title |
| `--quader-link` | table head | 4.5:1 | `#2f5ca0` on `#fbfafd`: 6.4:1 | `#9dc7f1` on `#28272d`: 8.4:1 | help page contents ("On this page") |
| `--quader-link` | row hover | 4.5:1 | `#2f5ca0` on `#e9f3fc`: 5.9:1 | `#9dc7f1` on `#1d283e`: 8.3:1 | link in a hovered row |
| `--quader-link` | dialog | 4.5:1 | `#2f5ca0` on `#ffffff`: 6.6:1 | `#9dc7f1` on `#28272d`: 8.4:1 | link in a dialog |
| `--quader-link` | selected row | 4.5:1 | `#2f5ca0` on `#e9f3fc`: 5.9:1 | `#9dc7f1` on `#1d283e`: 8.3:1 | link in the current audit row |
| `on-secondary-container` | selected row | 4.5:1 | `#18171d` on `#e9f3fc`: 15.9:1 | `#ececef` on `#1d283e`: 12.5:1 | selected tree item, active access tab |
| `on-surface` | selected row | 4.5:1 | `#3a383f` on `#e9f3fc`: 10.3:1 | `#ececef` on `#1d283e`: 12.5:1 | cell text in the current audit row |
| `on-surface-variant` | selected row | 4.5:1 | `#626168` on `#e9f3fc`: 5.5:1 | `#bfbfc3` on `#1d283e`: 8.0:1 | muted text in the current audit row |
| `on-primary-container` | primary-container | 4.5:1 | `#18171d` on `#e9f3fc`: 15.9:1 | `#ececef` on `#1d283e`: 12.5:1 | text on primary-container |
| `--quader-feedback-strong-fg` | strong feedback | 4.5:1 | `#ffffff` on `#3a383f`: 11.6:1 | `#ffffff` on `#3a383f`: 11.6:1 | text of a toast and of a tooltip |
| `--quader-feedback-strong-link` | strong feedback | 4.5:1 | `#66a8ea` on `#3a383f`: 4.6:1 | `#66a8ea` on `#3a383f`: 4.6:1 | action of a toast, and its focus ring (3:1) |
| `--quader-feedback-strong-fg` | strong feedback hover | 4.5:1 | `#ffffff` on `#4b4950`: 8.9:1 | `#ffffff` on `#5a585e`: 7.0:1 | close button of a toast under the pointer |
| `on-error-container` | error container | 4.5:1 | `#93000a` on `#ffdad6`: 7.2:1 | `#ffdad6` on `#93000a`: 7.2:1 | text of an error toast, and its focus ring (3:1) |
| `on-surface` | page | 4.5:1 | `#3a383f` on `#ffffff`: 11.6:1 | `#ececef` on `#18171d`: 15.1:1 | text |
| `on-surface` | raised | 4.5:1 | `#3a383f` on `#ffffff`: 11.6:1 | `#ececef` on `#18171d`: 15.1:1 | text on a card |
| `on-surface-variant` | page | 4.5:1 | `#626168` on `#ffffff`: 6.1:1 | `#bfbfc3` on `#18171d`: 9.7:1 | muted text, field label |
| `on-surface-variant` | filled field | 4.5:1 | `#626168` on `#ececef`: 5.2:1 | `#bfbfc3` on `#3a383f`: 6.3:1 | field label |
| `on-surface-variant` | dialog | 4.5:1 | `#626168` on `#ffffff`: 6.1:1 | `#bfbfc3` on `#28272d`: 8.1:1 | muted text in a dialog |
| `--quader-status-bad` | page | 4.5:1 | `#c02f12` on `#ffffff`: 5.7:1 | `#f6806d` on `#18171d`: 7.0:1 | error text |
| `--quader-status-bad` | filled field | 4.5:1 | `#c02f12` on `#ececef`: 4.9:1 | `#f6806d` on `#3a383f`: 4.5:1 | error text under a field |
| `--quader-status-warn` | page | 4.5:1 | `#995715` on `#ffffff`: 5.6:1 | `#d99530` on `#18171d`: 7.0:1 | warning text |
| `--quader-status-warn` | filled field | 4.5:1 | `#995715` on `#ececef`: 4.8:1 | `#d99530` on `#3a383f`: 4.6:1 | warning text on a field |
| `--quader-status-ok` | page | 4.5:1 | `#2f7549` on `#ffffff`: 5.6:1 | `#52b87a` on `#18171d`: 7.2:1 | positive status |
| `--quader-status-ok` | raised | 4.5:1 | `#2f7549` on `#ffffff`: 5.6:1 | `#52b87a` on `#18171d`: 7.2:1 | positive status on a card |
| `--quader-status-ok` | dialog | 4.5:1 | `#2f7549` on `#ffffff`: 5.6:1 | `#52b87a` on `#28272d`: 6.0:1 | added lines of the policy diff |
| `--quader-status-ok` | selected row | 4.5:1 | `#2f7549` on `#e9f3fc`: 5.0:1 | `#52b87a` on `#1d283e`: 6.0:1 | success in the current audit row |
| `--quader-nav-active-fg` | active entry | 4.5:1 | `#0a335c` on `#cbd9e9`: 8.9:1 | `#cae0f7` on `#38475c`: 7.0:1 | current navigation entry |
| `--quader-nav-active-fg` | active entry in an open group | 4.5:1 | `#0a335c` on `#cbd9e9`: 8.9:1 | `#cae0f7` on `#38475c`: 7.0:1 | current entry in an open group |
| `primary` | page | 3:1 | `#2f68b4` on `#ffffff`: 5.6:1 | `#66a8ea` on `#18171d`: 7.1:1 | focus ring, tab underline, checkbox, filter dot (non-text) |
| `primary` | raised | 3:1 | `#2f68b4` on `#ffffff`: 5.6:1 | `#66a8ea` on `#18171d`: 7.1:1 | focus ring, help card border on hover (non-text) |
| `primary` | dialog | 3:1 | `#2f68b4` on `#ffffff`: 5.6:1 | `#66a8ea` on `#28272d`: 5.9:1 | focus ring, drop zone border, checkbox in a dialog (non-text) |
| `primary` | filled field | 3:1 | `#2f68b4` on `#ececef`: 4.7:1 | `#66a8ea` on `#3a383f`: 4.6:1 | active indicator of a filled field (non-text) |
| `--quader-nav-accent` | side bar | 3:1 | `#0a335c` on `#ececef`: 10.9:1 | `#cae0f7` on `#28272d`: 10.9:1 | the context titles of the sidebar's blocks (Your work, Security audit, Admin area), focus outline (text and non-text) |
| `--quader-nav-accent` | open group | 3:1 | `#0a335c` on `#ececef`: 10.9:1 | `#cae0f7` on `#28272d`: 10.9:1 | icon of the current entry in an open group (non-text) |
| `--quader-nav-accent` | top bar | 3:1 | `#0a335c` on `#ececef`: 10.9:1 | `#cae0f7` on `#28272d`: 10.9:1 | focus outline in the top bar (non-text) |
| `--quader-nav-accent` | active entry | 3:1 | `#0a335c` on `#cbd9e9`: 8.9:1 | `#cae0f7` on `#38475c`: 7.0:1 | icon of the current entry (non-text) |
| `--quader-nav-accent` | active entry in an open group | 3:1 | `#0a335c` on `#cbd9e9`: 8.9:1 | `#cae0f7` on `#38475c`: 7.0:1 | icon of the current entry in an open group (non-text) |
| `primary` | primary-container | 4.5:1 | `#2f68b4` on `#e9f3fc`: 5.0:1 | `#66a8ea` on `#1d283e`: 5.9:1 | not used today: accent text on primary-container |
| `on-surface-variant` | primary-container | 4.5:1 | `#626168` on `#e9f3fc`: 5.5:1 | `#bfbfc3` on `#1d283e`: 8.0:1 | not used today: muted text on primary-container |
| `--quader-control-fg` | default button on the frame | 4.5:1 | `#3a383f` on `#ffffff`: 11.6:1 | `#ececef` on `#4f4e53`: 7.0:1 | the Admin button in the top bar |
| `--quader-control-fg` | default button hover on the frame | 4.5:1 | `#3a383f` on `#ececef`: 9.8:1 | `#ececef` on `#66656a`: 4.9:1 | the Admin button under the pointer |
| `--quader-nav-fg` | side bar | 4.5:1 | `#3a383f` on `#ececef`: 9.8:1 | `#ececef` on `#28272d`: 12.6:1 | sidebar entries, the top bar's quiet buttons |
| `--quader-nav-fg` | top bar hover | 4.5:1 | `#3a383f` on `#dedee1`: 8.6:1 | `#ececef` on `#39383e`: 9.8:1 | a sidebar entry or a top bar button under the pointer |
| `--quader-control-fg` | menu item hover | 4.5:1 | `#3a383f` on `#f0f0f1`: 10.2:1 | `#ececef` on `#4f4e53`: 7.0:1 | a menu item under the pointer |
| `--quader-control-fg` | switcher panel | 4.5:1 | `#3a383f` on `#ffffff`: 11.6:1 | `#ececef` on `#28272d`: 12.6:1 | the projects in the switcher of the project sidebar |
| `--quader-nav-fg-muted` | switcher panel | 4.5:1 | `#626168` on `#ffffff`: 6.1:1 | `#bfbfc3` on `#28272d`: 8.1:1 | the headings and keys in the switcher |
| `--quader-search-fg` | search | 4.5:1 | `#3a383f` on `#ffffff`: 11.6:1 | `#ececef` on `#39383e`: 9.8:1 | the text typed into the top bar's search |
| `--quader-search-muted` | search | 4.5:1 | `#626168` on `#ffffff`: 6.1:1 | `#bfbfc3` on `#39383e`: 6.3:1 | the search field's placeholder and icon |
| `--quader-heading` | card | 4.5:1 | `#18171d` on `#ececef`: 15.1:1 | `#ffffff` on `#3a383f`: 11.6:1 | a card's title on its strong fill |
| `on-surface-variant` | card | 4.5:1 | `#626168` on `#ececef`: 5.2:1 | `#bfbfc3` on `#3a383f`: 6.3:1 | a count or muted text in a card's head |
| `--quader-search-border` | top bar | 3:1 | `#858489` on `#ececef`: 3.1:1 | `#737278` on `#28272d`: 3.1:1 | outline of the top bar's search field (non-text) |
| `--quader-control-border` | page | 3:1 | `#89888d` on `#ffffff`: 3.5:1 | `#737278` on `#18171d`: 3.7:1 | outline of a field (non-text) |
| `--quader-accent-fill` | page | 3:1 | `#1f75cb` on `#ffffff`: 4.7:1 | `#63a6e9` on `#18171d`: 6.9:1 | the confirm button against the page (non-text) |
| `--quader-tab-indicator` | page | 3:1 | `#1f75cb` on `#ffffff`: 4.7:1 | `#428fdc` on `#18171d`: 5.3:1 | the current tab's line (non-text) |
| `--quader-badge-neutral-fg` | neutral badge | 4.5:1 | `#4c4b51` on `#dcdcde`: 6.3:1 | `#18171d` on `#a4a3a8`: 7.1:1 | text of a neutral badge |
| `--quader-badge-neutral-icon` | neutral badge | 3:1 | `#737278` on `#dcdcde`: 3.5:1 | `#18171d` on `#a4a3a8`: 7.1:1 | icon of a neutral badge (non-text) |
| `--quader-badge-ok-fg` | positive badge | 4.5:1 | `#306440` on `#c3e6cd`: 5.1:1 | `#17291c` on `#52b87a`: 6.2:1 | text of a positive badge |
| `--quader-badge-ok-icon` | positive badge | 3:1 | `#108548` on `#c3e6cd`: 3.5:1 | `#17291c` on `#52b87a`: 6.2:1 | icon of a positive badge (non-text) |
| `--quader-badge-warn-fg` | warning badge | 4.5:1 | `#894b16` on `#f5d9a8`: 5.0:1 | `#382315` on `#d99530`: 5.8:1 | text of a warning badge |
| `--quader-badge-warn-icon` | warning badge | 3:1 | `#ab6100` on `#f5d9a8`: 3.5:1 | `#382315` on `#d99530`: 5.8:1 | icon of a warning badge (non-text) |
| `--quader-badge-bad-fg` | error badge | 4.5:1 | `#a32c12` on `#fdd4cd`: 5.3:1 | `#3e1a14` on `#f6806d`: 6.0:1 | text of an error badge |
| `--quader-badge-bad-icon` | error badge | 3:1 | `#a32c12` on `#fdd4cd`: 5.3:1 | `#3e1a14` on `#f6806d`: 6.0:1 | icon of an error badge (non-text) |
| `--quader-badge-info-fg` | info badge | 4.5:1 | `#2f5ca0` on `#cbe2f9`: 5.0:1 | `#1d283e` on `#63a6e9`: 5.7:1 | text of an info badge |
| `--quader-badge-info-icon` | info badge | 3:1 | `#1f75cb` on `#cbe2f9`: 3.5:1 | `#1d283e` on `#63a6e9`: 5.7:1 | icon of an info badge (non-text) |
</details>

<details>
<summary>Candidates measured and not taken</summary>

| Candidate, not taken | On | Needs | Scheme | Ratio | What |
|---|---|---|---|---|---|
| `#1f75cb` | filled field `#ececef` | 4.5:1 | light | 3.99:1 fails | the reference's confirm blue as the focused label |
| `#1f75cb` | row hover `#e9f3fc` | 4.5:1 | light | 4.19:1 fails | the reference's confirm blue in a hovered row |
| `#63a6e9` | filled field `#3a383f` | 4.5:1 | dark | 4.49:1 fails | the reference's dark confirm blue as the focused label |
| `#63a6e9` | strong feedback `#3a383f` | 4.5:1 | both | 4.49:1 fails | the reference's blue 300 as a toast's action |
| `#050506 @ 24 %` | frame `#ececef` | 3:1 | light | 1.74:1 fails | the reference's search hairline |
| `#ffffff @ 16 %` | frame `#28272d` | 3:1 | dark | 1.67:1 fails | the reference's search hairline |
| `#89888d` | frame `#ececef` | 3:1 | light | 2.98:1 fails | the field outline as the search outline on the frame |
| `#0a6fd6` | page `#ffffff` | 4.5:1 | light | 4.9:1 passes | cogpit's light primary as text |
| `#ffffff` | `#0a6fd6` | 4.5:1 | light | 4.9:1 passes | white text on cogpit's light primary |
| `#0a6fd6` | filled field `#e4e7ec` | 4.5:1 | light | 4.0:1 fails | cogpit's light primary as the focused label |
| `#0a6fd6` | dialog `#eaedf2` | 4.5:1 | light | 4.2:1 fails | cogpit's light primary in a dialog |
| `#0a6fd6` | row hover `#f1f4f8` | 4.5:1 | light | 4.47:1 fails | cogpit's light primary in a hovered row |
| `#0a6fd6 @ 0x99` | page `#ffffff` | 3:1 | light | 2.5:1 fails | cogpit's light focus (alpha 0x99) as a ring |
| `#4aa8ff @ 0xaa` | page `#040c18` | 3:1 | dark | 4.0:1 passes | cogpit's dark focus (alpha 0xaa) as a ring |
| `#4aa8ff @ 0xaa` | raised `#121822` | 3:1 | dark | 3.8:1 passes | cogpit's dark focus on a card |
| `#4aa8ff @ 0xaa` | filled field `#222a36` | 3:1 | dark | 3.4:1 passes | cogpit's dark focus on a filled field |
</details>

Under the palette of Q-UI-96, `primary` and the muted text missed AA on the dark `primary-container` (4.1:1 and 4.0:1);
with the reference's info fill (`#1d283e`) both pass. No component of Quader sets text other than `on-primary-container`
on it today.

## What this does not cover

- The mark's files were checked on rendered frames in headless Chrome (the SVGs from 16 to 64 px, the ICO's frames at
  their own sizes, on light and dark grounds), not in the browser tabs of Firefox or Safari, and not whether a browser that
  ignores `prefers-color-scheme` inside a favicon SVG shows dark stones on a dark tab bar. The files of `public/brand`
  follow the system's colour scheme, not the theme button; the app draws its mark inline and is not affected. Nothing in
  the app shows the Bastion's mark yet. Not checked: the pair kerning of the static font files reads as 0.
- The reference's type is Inter with its lower-case l; Quader ships Inter 4 of `@fontsource-variable/inter`, the
  reference's "GitLab Sans" is built on an earlier Inter. Glyph differences between the two versions were not compared.
- The values of Q-UI-128 come from the reference's admin dashboard and its user list; its forms and dialogs were not
  measured. The reference limits its content to a centred column of 1280 px; Quader's content runs the panel's full width,
  by the owner's choice of 2026-11-06 for now. Links differ on purpose: the reference underlines none, Quader underlines
  links in running text and, in tables and key-value lists, only under the pointer and on focus (Q-UI-128, R-UI-044).
- The reference's icons are its own line icons; Quader's are Material Icons in the outlined variant (owner, 2026-11-06),
  close to them but not the same drawings. The format logos (Q-UI-225) were checked in Chrome on a static page with the
  built stylesheet and the component's styles, at 16, 20 and 32 px, in both themes and with the system's scheme opposite
  to the theme; not in the running app, not in Firefox or Safari. npm and Ansible Galaxy show the glyph until their owners
  allow their logos.
- The vulnerability trend and the usage screens' history are the charts. The trend takes the severity scale (its fills as bands, its
  marks as edges, [The severity scale](#the-severity-scale)); the usage takes `--quader-chart-usage`. Another chart takes a token of
  its own, never a severity's colour for something that is no severity, and adds its pairs to `palette.spec.ts`.
- The dialogs, tabs, checkboxes, menus and tooltips are Quader's own since Q-UI-126 was finished; their measures follow the
  reference's modal and tab strip as far as the development agent measured them, and no screen of the owner has confirmed
  their look yet (the walkthrough does).
- The blue palette of Q-UI-96 has not been through the browser tier's axe run. axe checks text against the background it
  is rendered on; the computed pairs cover every surface the accent, the link and the containers sit on in the screens
  named above, but a component's own fill that is not in the list is covered only once the browser journeys run again.
