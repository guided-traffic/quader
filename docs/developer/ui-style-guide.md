# UI style guide

How to build and style a screen of Quader. [design-language.md](design-language.md) says what the look is made of and which
values carry it; this page says what to do with them. Read it before you add a screen or change one, and check a screen
against the [checklist](#checklist) before it is committed.

## The rule behind every other rule

The reference is the owner's GitLab ([Q-UI-128](../spec/api-and-ui.md#q-ui-128--the-references-type-colours-and-measures)).
A screen looks like the reference's screen for the same job, and nothing is styled that the reference does not style.

When two rules meet, the higher one wins:

1. Accessibility: focus, contrast, labels, keyboard ([R-UI-044](../spec/api-and-ui.md)).
2. The owner's explicit decisions, listed under [Deliberate differences](#deliberate-differences).
3. The reference's screen for the same job.
4. An existing screen of Quader that follows this page.

Where a screen needs data Quader does not have, leave the element out. A column that would stay empty, or a value made up to
fill it, is worse than a list that is shorter than the reference's ([Q-UI-214](../spec/api-and-ui.md#q-ui-214--the-members-screen-takes-the-references-member-list)).

## Simple by default, details on demand

The owner's product rules of 2026-11-12 ([CLAUDE.md](../../CLAUDE.md#product-simple-by-default-details-on-demand), T-21)
come before every other rule of this page that is about what a
screen shows. Quader is for people who keep and fetch their artifacts, not for people who configure a repository manager; the
three kinds of user they serve are [R-UI-067](../spec/api-and-ui.md#r-ui-067--three-kinds-of-user-the-plain-user-the-power-user-and-the-administrator).

- **The plain path first.** The standard flow of a screen asks and shows only what the thing needs to work, for every format and
  every class. Access in a standard flow is the members and the visibility level, nothing else.
- **Details on demand.** A setting or a fact that only an expert needs (include and exclude patterns, disabling, hiding existence,
  administrator notes, mirror and protocol tuning, statistics, internal names) stays, in a detail view or behind a closed
  **Details** or advanced-settings section. It is never a step of a creation flow and never part of the main form.
- **A remote's connection is tested where its upstream is entered**, before the save, and the format's well-known public
  upstreams are one-click choices (for OCI: Docker Hub, Quay, GitHub `ghcr.io`, GitLab).
- **A default is a value in the field**, editable, never a hint beside an empty field.
- **A field is as wide as its expected input**: a number of seconds is a short field, a URL a long one.
- **A title names the thing**: **New OCI repository**, **New Ansible repository**, not **New repository**.
- **A field shows an error only after the user had a chance**: once it was touched and left, or on submit. An empty field the
  user has not touched is never red.
- **A step or section label names one topic.** "Access, upstream, OCI (Docker) and mirror settings" is four steps or four sections.

## How to style an element

Take the first of these that fits, in this order:

1. A shared control: `<quader-text-field>`, `<quader-select-field>`, `<quader-checkbox>`, `<quader-switch>`, `<quader-tab-group>`,
   `<quader-data-table>`, `<quader-avatar>`, `QuaderDialog`, the `[quaderTooltip]` directive for a sentence of plain text, the
   `[quaderDetailPopover]` directive on a `<button>` for details on demand in lines (hover, focus and tap, R-UI-256),
   `<quader-help>` for the sentence that explains a setting, a heading or a column behind an info sign, and `<quader-setting>` for
   a row of a settings card, its name beside its control (R-UI-267) ([web/src/app/shared/](../../web/src/app/shared/)).
2. A class of [_controls.scss](../../web/src/styles/_controls.scss): `btn`, `text-input`, `select-input`, `badge`, `tab-nav`,
   `menu`. The table in [design-language.md](design-language.md#controls) lists each with its markup.
3. A component style that uses only the `--quader-*` tokens of [_tokens.scss](../../web/src/styles/_tokens.scss).

Never write a colour, a font size, a shadow or a radius as a literal in a component. Spacing comes from `--quader-space-1` to
`-8` (4, 8, 12, 16, 20, 24 and 32 px). A value the tokens do not have is a new token, added with its light and dark value, and a
new text and background pair goes into [palette.spec.ts](../../web/src/styles/palette.spec.ts). No new font, shadow or gradient. A text in
the monospace face is a `code`, `kbd`, `pre` or `samp`, which take `--quader-font-mono` from `main.scss`; any other element names the
token, never a face of its own ([Q-UI-1211](../spec/api-and-ui.md#q-ui-1211--every-monospace-text-takes-the-tokens-face-set-once-on-code-kbd-pre-and-samp)).

## Anatomy of a page

Every page inside the shell has this order:

| Part | How |
|---|---|
| Breadcrumb | Set by the route; a page does not draw its own |
| Header | `<quader-page-header heading description>` ([page-header.ts](../../web/src/app/shared/ui/page-header.ts), [Q-UI-307](../spec/api-and-ui.md#q-ui-307--a-pages-own-actions-are-buttons-at-the-right-end-of-its-header-views-of-the-same-thing-are-tabs)): the `<h1 tabindex="-1">` names the object on its page (`[name]="true"` for a key or a package name) or the list's noun, with the list's number of entries as a badge after it where the page is one list (`[count]`, **Members 2**, [Q-UI-550](../spec/api-and-ui.md#q-ui-550--the-members-list-takes-the-head-and-the-table-of-every-list-and-its-count-moves-to-the-heading)); one muted sentence under it; the facts that name the object projected with `meta`; its client address where it has one (`[addresses]`). Below 36 rem of content a header with a client row (the address and **Set up client**) is one flow, and the page's actions share the line of **Set up client**, so no button stands alone on a line ([Q-UI-554](../spec/api-and-ui.md#q-ui-554--where-the-header-is-narrow-the-pages-actions-share-the-line-of-set-up-client)). A detail page names its object in the `<h1>` and its kind as a badge projected with `meta` (the user `alice` with the badge **User**), and has no link up to its list: that is the breadcrumb. The vulnerability pages still have `<header class="page-header">` |
| Primary action | At the right end of the header (projected with `actions`), one `btn btn-confirm`. The button that makes a new thing says **New** and the thing (**New repository**, **New user**), its dialog is titled the same and confirms with **Create** and the thing; a button that adds an existing thing to another says **Add** (**Add member**) ([Q-UI-573](../spec/api-and-ui.md#q-ui-573--the-button-that-makes-a-new-thing-says-new-thing-and-a-new-user-is-made-not-added)). **Create token** of the profile, **Create policy** and **Create service account** of a project follow when they are next changed. A second action of the page is a `btn btn-default` to its left; more go into a `more_vert` menu |
| Tabs | Directly under the header, `tab-nav`, the count of a list as a neutral `badge` in the tab. Another view of the same object (its files, its vulnerabilities) is a tab or an entry of the header's menu, never a quiet link in the header. A job has one way to it: the client setup is the header's **Set up client**, which opens the setup page for the object, not also a tab ([Q-UI-321](../spec/api-and-ui.md#q-ui-321--a-repositorys-client-setup-is-reached-one-way-the-headers-set-up-client-opens-the-setup-for-the-repository)) |
| Head of a list | Under the tabs, one row, `<quader-list-head>` ([R-UI-100](../spec/api-and-ui.md#r-ui-100--the-head-of-every-list-the-search-by-name-filters-and-the-filters-that-are-set)): the search by name (**Filter repositories**, as wide as a name), **Filters**, the filters that are set as chips, and the list's own controls at the row's end. **Filters** opens the list's drawer ([Q-UI-420](../spec/api-and-ui.md#q-ui-420--the-filters-of-a-list-open-in-a-drawer-from-the-right-over-the-page)); the list keeps its width |
| Content | A table, a form or sections, as below |
| Details | What only a power user or an administrator needs, last, in one closed `<quader-disclosure>` **Details** (see [Opening and closing](#opening-and-closing)) |

**One scrollbar per page** ([R-UI-215](../spec/api-and-ui.md#r-ui-215--one-scrollbar-per-page)): the content panel (`main`) scrolls,
the window does not, and nothing in the page scrolls up and down by itself. A list of any length grows the page and its head scrolls
with it ([Q-UI-1110](../spec/api-and-ui.md#q-ui-1110--the-page-is-the-one-thing-that-scrolls-a-lists-head-scrolls-with-it)). What may
keep a scroll of its own: a framed box of at most 24 rem that stands in for a chart or is a list box control; a navigation column that
sticks beside the page (`--quader-page-height` less the panel's padding, the manual's contents, a documentation tree); an overlay, one
scroll each; a text area. A side column that is no navigation sticks only while it fits (`quaderStickyWhenFits` of
[sticky-when-fits.ts](../../web/src/app/shared/ui/sticky-when-fits.ts)) and never scrolls by itself. A box inside a dialog or a drawer
is as tall as its content; a code block that keeps its lines may scroll sideways. The render check `inner-scroll` holds it.

One `<h1>` per page. Page titles are nouns (**Members**, **Settings**), in sentence case, and the same as the page's entry in the
side bar (**Tokens**). The description is one sentence, the header's `description`, never a paragraph of its own after the header
([Q-UI-569](../spec/api-and-ui.md#q-ui-569--the-sentence-under-a-title-is-the-headers-description)); what would need a second one belongs in the
[manual](manual.md).

## Lists

[`project-members.ts`](../../web/src/app/features/projects/project-members.ts) is the worked example.

- A table without a frame, hairlines between rows, the head at weight 600. `<quader-data-table>` where the list is paged.
- **The first column names the thing.** It is a `<th scope="row">`. A person or group is an avatar of 48 px, the name at weight
  600 on the first line and the `@username` or its kind muted on the second.
- **A status is a badge** with an icon and a word, never colour alone: `<span class="ok|warn|bad|badge"><span class="material-icons">…</span>Word</span>`,
  one element that never breaks, its word with a capital (**Succeeded**, **Denied**). `ok` is green and means a positive status only.
  A status whose words are too long for a pill in a narrow cell puts its verdict on the badge and the rest after it as muted text
  that wraps between its words, in a list's cell as the cell's second line (`.secondary`, as a refused file's reason under
  **Refused here**): the signatures' **Signed** with "signer not trusted" after it
  ([Q-UI-1076](../spec/api-and-ui.md#q-ui-1076--a-status-too-long-for-a-pill-puts-its-verdict-on-the-badge-and-the-rest-after-it)).
- **A list of facts** is `<dl class="facts">`: the terms in a column as wide as the longest, each value beside its term. Below 36 rem
  of content (a phone) each term stands above its value, so a value has the whole line and breaks inside itself only where it is
  longer than that line (`overflow-wrap: anywhere`, the last resort for an address or a path;
  [Q-UI-1077](../spec/api-and-ui.md#q-ui-1077--below-36-rem-of-content-a-facts-term-stands-above-its-value)). A page does not give
  its facts a grid of its own.
- **A heading** wraps between its words, after a hyphen, or at a soft hyphen; never between two letters. A long word is not
  answered by a smaller heading or by the browser's hyphenation: a German word of more than 20 letters carries a soft hyphen
  (`&#xAD;`) at a joint of its compound in the German file, which `make web-i18n-check` holds
  ([GLOSSARY.md](../../web/src/locale/GLOSSARY.md)), and the renders' check `heading-broken` refuses a heading broken inside a word
  ([Q-UI-1091](../spec/api-and-ui.md#q-ui-1091--a-long-german-word-carries-soft-hyphens-at-the-joints-of-its-compound-and-a-heading-never-breaks-inside-a-word)).
  A label of the sidebar that wraps keeps its icon on its first line.
  An icon never stands directly in a table cell: a status is the badge, an icon before a text that may wrap is `.icon-name`
  (`status-rule.mjs` fails the lint otherwise; [Q-UI-560](../spec/api-and-ui.md#q-ui-560--an-icon-in-a-table-cell-is-one-element-with-its-word),
  [Q-UI-564](../spec/api-and-ui.md#q-ui-564--a-status-word-starts-with-a-capital)).
- **A cell without a value** is `<quader-no-value />`: the en dash, heard as "None", never an empty cell and never a bare "–"
  ([Q-UI-561](../spec/api-and-ui.md#q-ui-561--a-cell-without-a-value-is-the-en-dash-heard-as-none)).
- **A count or a size** stands at its column's end: `numeric: true` in a data table, `num` on a raw table's header and cells
  ([Q-UI-574](../spec/api-and-ui.md#q-ui-574--a-count-or-a-size-stands-at-its-columns-end)).
- **A package format is its icon and its name**, never the icon alone: `<quader-format-icon [format]>` beside the display name,
  sized by the place's `font-size` (20 px in a list, 16 px in a badge, 32 px on a tile). The icon is the format owner's logo
  where the owner's terms allow it, else the neutral `inventory_2`
  ([Q-UI-225](../spec/api-and-ui.md#q-ui-225--a-format-shows-its-owners-logo-where-the-owners-terms-allow-it-else-the-neutral-glyph)).
  A logo is never redrawn, recoloured or cropped; on the dark scheme it shows the owner's light variant or stands on the plate
  ([Q-UI-226](../spec/api-and-ui.md#q-ui-226--a-logo-is-an-image-of-the-owners-file-and-the-dark-scheme-shows-the-owners-light-variant-or-a-plate)).
  A new logo is the owner's file unchanged under `web/public/formats/<format>/`, a section in
  [NOTICE.txt](../../web/public/formats/NOTICE.txt) and a case in [format-icon.ts](../../web/src/app/shared/format-icon.ts),
  after its owner's terms were read.
- **A row with one action names it, a row with several has a menu** (the owner's rule,
  [R-UI-163](../spec/api-and-ui.md#r-ui-163--a-row-with-one-action-names-it-a-row-with-several-has-a-menu)):
  - one action is its word as a quiet button, `btn btn-tertiary`, the row's name after the word in a `visually-hidden` span
    (**Revoke**, heard as "Revoke ci-deploy"); a destructive word (**Revoke**, **Remove**, **Unbind**) has the same quiet form and
    its confirmation dialog says what it destroys; an action that differs by permission is one word either way;
  - two or more actions are the menu: `btn btn-tertiary btn-icon` showing `more_vert`, `[cdkMenuTriggerFor]`, an `aria-label`
    **Actions for &lt;name&gt;** (**Actions for user:bob**), the entries `.menu-item`s with their words. A menu never has one entry;
  - beside the menu at most one frequent action stands on its own: the copy button of a value (its icon with name and tooltip, also
    where it is a row's one action, [Q-UI-642](../spec/api-and-ui.md#q-ui-642--a-copy-button-stays-the-copy-icon-also-where-it-is-a-rows-one-action)),
    or **Set up client** in a project's repository list (never for a system repository of `upstream`, which answers no client:
    `setupCommands` is `null` for it, [Q-UI-896](../spec/api-and-ui.md#q-ui-896--no-screen-offers-a-client-action-for-a-system-repository-one-function-decides-the-link)). The menu may offer it again;
  - opening the thing is its name, the link in the first column; a row whose only action would open it has no action cell;
  - a credential's **Revoke** is never in a menu: a token row keeps **Rotate** and **Revoke** as two words
    ([Q-UI-641](../spec/api-and-ui.md#q-ui-641--a-credentials-revoke-is-never-in-a-menu-a-token-row-keeps-its-two-words)).

  `row-actions-rule.mjs` fails the lint where a cell or a list item shows two words at once. The repository list is the example of
  a thing with a description: the key, the description as the muted second line, the actions in the menu
  ([Q-UI-302](../spec/api-and-ui.md#q-ui-302--the-repository-list-names-a-repository-by-key-and-description-in-its-first-column-and-puts-the-rows-actions-in-a-menu)).
- **The columns come from the job of the list's kind of user** (R-UI-067, [Q-UI-330](../spec/api-and-ui.md#q-ui-330--a-list-takes-its-columns-from-the-job-of-its-kind-of-user-the-important-properties-are-columns-and-the-address-takes-no-more-than-its-share)). Ask first what the list is for: the
  properties its user decides on get columns of their own, the column that names the thing gets the most room, and nothing is
  packed under the name but its description. The repository lists are the example: in a project (find and set up) repository,
  format, class, and **Set up client** at the row's end; in the admin area (manage) repository, project, format, class,
  visibility, last activity, and the address as a copy button. No list has an address column: an address means nothing without
  its client ([Q-UI-426](../spec/api-and-ui.md#q-ui-426--a-projects-repository-list-has-no-client-address-its-row-offers-set-up-client)). Judge a render by that first, and only then by widths and scrolling.
- **A column is as wide as its content** ([Q-UI-563](../spec/api-and-ui.md#q-ui-563--a-column-is-as-wide-as-its-content-what-never-breaks-declares-no-width-and-the-columns-that-wrap-share-the-room)):
  a column whose values never break (a format, a class, a level, a time, a status, an identifier, the actions) declares no `width`;
  a time with its badge is one such value, and so is a word in a time's place (`.no-time`: **Never**, **Not used yet**,
  [Q-UI-1064](../spec/api-and-ui.md#q-ui-1064--an-age-and-its-badge-are-one-value-that-never-breaks));
  a column whose values wrap (a name, a description, a project, a scope) declares its least, and those share the rest by their
  content. Only a list with one wrapping column lets it `prefer`. A row's header that is an identifier carries `translate="no"`
  and stays on one line; a value longer than any column (a UUID) is cut on one line with its whole value as its `title`, a hash
  is its first twelve characters in `<code>` with the whole as its title. The rest of this item holds where it does not
  contradict this.
- **The list's budget is its columns' widths** ([Q-UI-308](../spec/api-and-ui.md#q-ui-308--every-list-takes-its-layout-from-the-data-table-a-column-budget-a-least-width-for-its-name-one-alignment-stacked-rows)). A column of `<quader-data-table>` declares its least width
  (`width`, which must hold its header and its longest value on one line: a format with its icon 8 rem, a class 5 rem, a project
  with its mark 11 rem); the name column takes 12 to 16 rem and what the others leave. A column that `prefer`s takes every pixel
  the others leave, and every other column keeps its width (the name of a repository list, the scope of a token list). The least
  widths are where the list stacks its rows (below). A column of lower rank is `narrow`: below 64 rem of the list's
  width it is hidden and the others take its room (the list marks its cells `narrow-drop`; the admin area's Last activity). The
  list's width is the table's own (the container `table`); the table keeps no room for a scrollbar its rows do not need, so it
  ends where the lines above it end ([Q-UI-427](../spec/api-and-ui.md#q-ui-427--a-table-keeps-no-room-for-a-scrollbar-its-rows-do-not-need)).
  It never scrolls by itself: the page does (R-UI-215).
- **A list opened in a row is bounded.** An item opened in a repository's content shows its ten newest versions under a head of their
  own and, where it holds more, **All N tags** or **All N versions** to the package's page, which lists them all
  ([Q-UI-1111](../spec/api-and-ui.md#q-ui-1111--an-opened-item-shows-its-ten-newest-versions-and-links-all-of-them)); a list opened in a
  row whose items have no page of their own (an upstream package's versions) grows the page with **Load more**.
- **One alignment.** Every cell starts at the top of its row; a control in a cell (a button, a select, the address field) is
  centred on the first line of text by [the line rule](#a-control-beside-a-text). A list does not set its own vertical alignment.
- **Where the columns do not fit, the rows stack.** A data table measures its columns at their least widths (a wrapping column's
  `width`, a column that never breaks at its longest value) against its room and shows each row as a block where they do not fit,
  and only there ([Q-UI-680](../spec/api-and-ui.md#q-ui-680--a-list-stacks-where-its-columns-at-their-least-widths-do-not-fit-measured-by-the-table-itself)):
  the name and its actions on the first line, then one line per field: its column's name in a column as wide as the list's
  longest name (at most two fifths of the list, never narrower than its longest word, so a name wraps between its words and never
  inside one, [Q-UI-1068](../spec/api-and-ui.md#q-ui-1068--a-stacked-rows-column-name-wraps-between-its-words-never-inside-one)) and
  its value beside it, so a field stands at the same place in every block
  (the table sets `data-label` and measures the names, also those of a list in a block of a stacked list; `stackLabel: false` where
  the value says it, which takes the line alone),
  a cell without a value ("–") left out
  ([Q-UI-1050](../spec/api-and-ui.md#q-ui-1050--a-stacked-row-is-a-grid-of-names-and-values-one-field-a-line)); a row that only carries an expansion of
  the row above (an opened image's tags, an opened malware find) takes the block's whole width, whatever its content. So a column's least width decides
  where its list stacks: a wrapping column without one is as narrow as its longest word (the start page's description, one word
  a line). A column whose cells start with a relative time (`<quader-time-ago absolute="title">`) is at least as wide as the
  widest relative time of the language, which the table measures, so "now" and "59 minutes ago" give one layout
  ([Q-UI-1069](../spec/api-and-ui.md#q-ui-1069--a-column-of-relative-times-holds-the-widest-relative-time-of-its-language)).
  The head of a stacked list is one row of its sort buttons above the blocks; a header that sorts nothing is for assistive
  technology only ([Q-UI-391](../spec/api-and-ui.md#q-ui-391--a-stacked-list-keeps-its-sort-buttons-in-a-row-above-its-blocks)).
  `[stack]="false"` keeps the table for a list that cannot be read as blocks.
- **An empty list** is `<quader-empty-state text>`: "There is no <thing> yet." while no search and no filter is set, "No <thing>
  matches." when one is; the action that fills the list is the header's **New <thing>**, once, never repeated under the sentence
  ([Q-UI-565](../spec/api-and-ui.md#q-ui-565--an-empty-list-says-there-is-none-yet-a-filtered-one-that-nothing-matches-and-its-action-stands-once-in-the-header),
  revising [Q-UI-309](../spec/api-and-ui.md#q-ui-309--an-empty-list-or-region-is-a-sentence-and-the-action-that-fills-it-without-a-frame)).
  The data table shows it with its `emptyText`.
- **A line of facts** (a source line; never the properties of a list's row, which are columns) separates them with one middle dot, the same space
  on both sides, in the muted colour: `.dot-separated` around its parts, `.dot-before` on one part ([_controls.scss](../../web/src/styles/_controls.scss)).
  Where such a line wraps, no line starts with a dot.
- **Names stay on one line**, except a name of a list's first column that is longer than its column: `th.wrap-name` lets it
  break after a hyphen ([Q-UI-303](../spec/api-and-ui.md#q-ui-303--the-repository-lists-key-breaks-after-a-hyphen-where-it-is-longer-than-its-column)).
  So does a date, a time, a version and a short phrase ("None yet"): never broken (Q-UI-428).
- **An icon before a name** is the cell `.icon-name`: the icon in a column of its own, the name and the description under it in
  the other, so the icon and the start of the name always share the first line, and a long name breaks inside itself beside the
  icon ([Q-UI-425](../spec/api-and-ui.md#q-ui-425--an-icon-before-a-name-has-a-column-of-its-own-in-the-cell-and-a-long-name-breaks-inside-itself-beside-it)):

  ```html
  <th scope="row"><span class="icon-name"><quader-visibility-icon [level]="…" /><span class="icon-name-text"><a …>{{ key }}</a><span class="secondary">…</span></span></span></th>
  ```

- **A project is its mark and its name** ([Q-UI-424](../spec/api-and-ui.md#q-ui-424--a-projects-mark-is-one-component-before-its-name-wherever-a-project-is-listed)):
  in a list of projects `<quader-project-mark size="medium">` before the display name with the key under it; in a row that names
  its project `<quader-project-link [key]>`, the small mark and the display name, linked. Never the key alone. The built-in project
  `upstream` carries an arrow pointing up where every other project carries its initial, has the badge **Built-in** after its name,
  and stands first in every list of projects the app builds, whatever the sort (the owner's
  [Q-UI-837](../spec/api-and-ui.md#q-ui-837--the-project-upstream-stands-first-in-a-list-of-projects-and-its-mark-is-an-arrow-pointing-up)):
  `upstreamFirst` of [upstream-project.ts](../../web/src/app/core/upstream-project.ts), the one place that knows the key. A project's
  level in a list is `<quader-project-level [key] [level]>`: its icon and its name, and for `upstream` the en dash heard as "Each
  registry has its own visibility." ([Q-UI-844](../spec/api-and-ui.md#q-ui-844--the-project-upstream-shows-no-level-of-its-own-each-registry-has-one)).
- **A time in a list is relative**, its date and time the title: `<quader-time-ago [at] absolute="title" />` ("4 minutes ago",
  "in 4 weeks"). A log (the audit log, the issued CI tokens) writes the date and the time; a card or a page's facts the relative
  time with the absolute after a dot, "3 hours ago · 26.11.2026 21:22:18 (Thu.)", never in brackets, which the reader's form has of its own
  ([Q-UI-431](../spec/api-and-ui.md#q-ui-431--a-time-in-a-list-is-relative-with-the-absolute-time-as-its-title-a-log-writes-the-moment),
  [Q-UI-1525](../spec/api-and-ui.md#q-ui-1525--a-relative-time-and-its-absolute-time-are-joined-by-a-dot-not-brackets)).
  Every `<time>` is one value that never breaks inside itself (`main.scss`): a line breaks before or after "Nov 24, 2026", in a
  fact or a sentence as in a list's cell, whatever the date's length.
- An inline control in a row (the role select, a level, a switch) has no visible label and an `aria-label` that names the row. It
  shows the current value, and a change is saved at once, with the status message of the `Notifier`: a toast at the window's bottom
  right, one at a time, so a row of such changes shows the last one ([Q-UI-1063](../spec/api-and-ui.md#q-ui-1063--feedback-outside-forms-is-the-references-toast-at-the-bottom-right-one-confirmation-at-a-time)).
  The message is one sentence that names the thing ("Docker Hub is on."); a warning of the same write goes into the same message.
- Filter and order work on what is loaded, or are sent to the server where the list is paged. A filter that finds nothing says
  so in a row of the table; an empty list says so in place of the table and offers the action that fills it.
- **The head of a list** is `<quader-list-head>` ([list-head.ts](../../web/src/app/shared/ui/list-head.ts), [R-UI-100](../spec/api-and-ui.md#r-ui-100--the-head-of-every-list-the-search-by-name-filters-and-the-filters-that-are-set)): give
  it `searchLabel` where the API searches by name, the chips of the filters that are set (`ListChip`: the filter's name and its
  value; never the search text), and the drawer's `panelId`; put the list's own controls into it with `headEnd`. The filters are
  the content of `<quader-filter-panel>`: a labelled field each, or a `<quader-filter-category label>` around a group of checkboxes
  or a control named only by `aria-label`. A list keeps the applied values apart from the drawer's staged ones and puts the staged
  ones back on `dismissed`. A list whose only filter is the name has no drawer (`[filters]="false"`).

## Settings pages

The reference's settings page is one column of fields, not a card of key and value. New settings pages follow it, and the
existing ones are moved over ([What does not follow this page yet](#what-does-not-follow-this-page-yet)).

- **One column**, at most `--quader-settings-measure` (56 rem) wide, on the page and without a card around it.
- **The most used settings are open at the top**, without a section title: the name, the description, the avatar.
- **Further groups are collapsible sections** (`<quader-disclosure>`, the one collapsible section of every screen), closed by
  default: a chevron on the left, the title at the text's size, weight 600, one muted sentence under it, a hairline between
  sections. The title names the group (**Permissions**, **Advanced**).
- **Everything is edited in place.** A settings page has no read-only view with an **Edit** button that opens a dialog, and no
  key-value list of values that could be fields.
- **A value that cannot change is a read-only field** with the control's disabled fill and no border (the key of a project, an
  ID), so that it can be selected and copied.
- **Each section has its own save button**, `btn btn-confirm` with **Save changes**, at the end of the section and disabled until
  something changed.
- **Destructive actions come last**, in a section **Advanced**, with a `btn btn-danger` and a sentence that says what it destroys.
  A destructive action that cannot act stays, unavailable, and says why in its tooltip (`[quaderUnavailable]="reason"`,
  [R-UI-270](../spec/api-and-ui.md#r-ui-270--a-control-fixed-by-the-helm-configuration-is-offered-unavailable-its-reason-its-tooltip)),
  never in a sentence next to the button.

## Opening and closing

**Everything that folds, folds with an animation** ([Q-UI-215](../spec/api-and-ui.md#q-ui-215--settings-are-fields-in-one-column-and-everything-that-folds-is-animated)), with one duration and easing (`--quader-motion-duration`, 200 ms, and `--quader-motion-easing`). Never write another one, and never hide a region with `[hidden]` or `@if` alone. Pick by the kind of element; the classes are in [_controls.scss](../../web/src/styles/_controls.scss#L1):

| Element | How |
|---|---|
| A region that stays in the page | `<div class="collapse-region" [class.open]="open()"><div>…</div></div>`; the second element is required |
| A list that drops below its field: the search's suggestions ([search-field.html](../../web/src/app/features/search/search-field.html)) and its sheet below 36 rem | `animate.enter="dropdown-in"` on the list; it closes at once, because the focus stays in the field and the next key must not wait for an animation |
| A collapsible section of any screen: the details of a content page, the groups of a settings page or an editor | `<quader-disclosure heading hint note noteTone count>` (`note`: a short state after the heading, such as **Unsaved changes** of a closed section that holds one, with `noteTone="warn"` a line that needs action in the warning's colour with its icon (R-UI-267); `count`: the number of entries of a section that is a list, the neutral badge of a count after the heading, **System repositories 2**, [R-UI-192](../spec/api-and-ui.md#r-ui-192--the-system-repositories-of-the-project-upstream-stand-apart-on-its-page)) ([disclosure.ts](../../web/src/app/shared/ui/disclosure.ts), [Q-UI-322](../spec/api-and-ui.md#q-ui-322--a-closed-section-of-details-is-one-quiet-disclosure-a-small-heading-with-a-chevron-and-one-hint-line)): the heading at the text's size, weight 600, a chevron before it, one muted hint line, closed at first; `[(expanded)]` binds its state. Never a settings section below a list or a README |
| A fold inside a section or a table cell: a pattern syntax, a certificate, the messages of a row, a section's further details | `<quader-disclosure [inline]="true" heading>`: the same chevron and weight, without the heading element, the rule and the room around it ([Q-UI-552](../spec/api-and-ui.md#q-ui-552--every-fold-is-the-shared-disclosure-a-fold-inside-a-section-is-its-inline-form)). Its toggle is a button, so its label stays on one line at 390 px (R-UI-171); only a section's heading toggle wraps. A label of Set up client has at most 32 characters in every locale, which `clients.spec.ts` holds in English and German ([Q-UI-810](../spec/api-and-ui.md#q-ui-810--a-folds-label-is-a-buttons-label-at-most-32-characters-in-every-locale)): name the way, not the reason (**Remote without upstream keys**). The app has no bare `<details>`: `check-rules.mjs` refuses one in a template |
| Something added and removed with `@if` | A wrapper with `class="collapse-animated" animate.enter="collapse-in" animate.leave="collapse-out"` around one child |
| A side panel, a drop-down panel, the sidebar | `panel-in`, `panel-out`; `dropdown-in`, `dropdown-out`; `sidebar-in`, `sidebar-out` |
| Content over the page: a list's filters | `<quader-drawer [(open)] heading>` ([drawer.ts](../../web/src/app/shared/ui/drawer.ts), [ADR-200](../decisions.md#adr-200--a-lists-filters-open-in-a-drawer-the-apps-one-pattern-for-content-over-the-page)): the CDK dialog at the window's right edge; it slides in and out by itself (`quader-drawer-in`, `-out`), and focus, Escape and the return of focus come with it |
| A native `<details>` (only in rendered Markdown, the manual and a README) | Nothing: it animates by itself where the browser can |
| The chevron of the toggle | `class="expand-chevron"` on `expand_more` inside the button with `aria-expanded`; it turns with the same timing |

The button has `aria-expanded` and `aria-controls`. A closed region is `visibility: hidden`, so nothing in it can be focused. `prefers-reduced-motion` sets the duration to 0 in one place. Dialogs and CDK menus are not folding elements.

## Forms

- The label is above the field, at weight 600, always visible. A placeholder is an example, never the label.
- An optional field says **(optional)** in its label. Required fields are not marked, because they are the normal case.
- A field's hint is one muted line for its format (a list separated by commas, a name's characters); what the setting does is its
  help, behind the info sign beside its label (`help`). An error replaces nothing: it appears below the field, in the error colour,
  tied to it by `aria-describedby`, and the field has `aria-invalid`.
- An error appears only after the field was touched and left, or after a submit of its form was attempted (the form's `submit`,
  or a click on its submit or confirm button). `<quader-text-field>` and `<quader-select-field>` hold this rule
  (`revealRule` in [fields.ts](../../web/src/app/shared/access/fields.ts)), so a form passes its errors in and keeps no state of
  its own; a field outside a form shows its error at once. An error says in words what to type, never the pattern (UI-A-42).
- A field is as wide as its kind of input, by one of six widths, so the fields of one column line up whatever their content says
  ([Q-UI-334](../spec/api-and-ui.md#q-ui-334--a-picker-is-one-control-from-the-start-held-while-its-list-loads-and-a-field-is-as-wide-as-its-kind-of-input)).
  The tokens are in [_tokens.scss](../../web/src/styles/_tokens.scss), the classes in [_controls.scss](../../web/src/styles/_controls.scss).
  `<quader-text-field>` must name its `width` ([Q-UI-335](../spec/api-and-ui.md#q-ui-335--every-text-field-names-the-width-of-its-expected-input-and-a-number-field-is-never-the-column)); `<quader-select-field>` takes `width`
  (`name` unless its place fills a line); a raw input of a form carries the class:

  | Class | Token | Width | For |
  |---|---|---|---|
  | `field-w-number` | `--quader-field-width-number` | 6 rem | a count of up to six digits: a priority, a port, a limit |
  | `field-w-duration` | `--quader-field-width-duration` | 8 rem | a period, up to eight digits: seconds, a TTL, days |
  | `field-w-bytes` | `--quader-field-width-bytes` | 13 rem | a byte count or an ID of up to nineteen digits, a date |
  | `field-w-version` | `--quader-field-width-version` | 16 rem | a version with the list of the versions it offers (a bound of a range: a canary version such as `2.10.0-canary.bfb9accdc` beside the list's arrow) |
  | `field-w-name` | `--quader-field-width-name` | 28 rem, half the column | a key, a name, a login, a host, a description (one sentence, also as a text area, [Q-UI-551](../spec/api-and-ui.md#q-ui-551--a-description-is-as-wide-as-a-name)), every select and every picker |
  | `field-w-full` | — | the column | a URL, a note, a path, a pattern, a list, a key block; every other text area |

  A field is never wider than its column; a narrower column shrinks it. A filter drawer's fields and a search row's fields take
  the drawer's or the row's width; the search field of a list's head is as wide as a name. A number field never takes the column: `check-rules.mjs` fails the lint for a template's number
  field (`type="number"`, `inputmode="numeric"` or `"decimal"`, a pattern of digits) without the width number, duration or bytes.
  A URL is one line however long it may be. A field and the button that acts on it (**Bind role**, **Move repository**) stand in
  `.field-action-row`: the button level with the control, under it where the line is too narrow.
- A control that offers a list read from the server is the same control from the start. While the list loads it is disabled,
  `aria-busy`, and says so ("Loading policies…" as a select's one option or a field's placeholder); it never starts as one kind of
  control and turns into another. A list that can be long (policies, groups) is a text field with suggestions (`datalist`) and
  **Add**; a short one (a project's issuers) is a select that starts at "Choose …", never at its first entry.
- The selects of a panel (the install panel's client and repository) stack and take the panel's width, so they line up whatever
  their options say ([Q-UI-320](../spec/api-and-ui.md#q-ui-320--every-value-to-copy-of-one-line-is-a-field-with-its-joined-copy-button-a-page-shows-one-client-address-and-a-panels-selects-share-one-width)).
- A choice among the values of a list (the version of a package) is a select, so it looks like the choice it is.
- A check box is `<quader-checkbox>`, its box a direct child of `.check-row`, which keeps it at 16 px beside a label of several
  lines ([Q-UI-575](../spec/api-and-ui.md#q-ui-575--a-check-box-keeps-its-size-beside-a-long-label)); so does a radio of
  `<quader-visibility-level>` beside a sentence of two lines (its box shrank to 13 px and pulled its option 3 px left, 2026-11-25).
  What the box does is its `help` (with `about`, the label's words), behind the sign after the label; the line under it is only one
  that stays visible: why the box is off or does not act now, what it gives away ([Q-UI-1628](../spec/api-and-ui.md#q-ui-1628--the-help-sign-on-every-settings-page-and-no-info-icon-beside-a-status)).
- A thing that is on or off and changes at once, without a save (a public registry of **Upstream registries**), is a switch,
  `<quader-switch [checked] [label] (checkedChange)>` ([switch.ts](../../web/src/app/shared/ui/switch.ts),
  [Q-UI-830](../spec/api-and-ui.md#q-ui-830--upstream-registries-is-one-list-grouped-by-format-with-a-switch-and-a-level-in-each-row)).
  A choice that a form saves stays a check box.

  | Part | How |
  |---|---|
  | Element | A native `<button role="switch">` with `aria-checked`: Space and Enter toggle it, a screen reader says its name and "on" or "off" |
  | Name | `label`, what it switches: "Switch on Docker Hub"; the visible word after it, **On** or **Off**, repeats the state and is `aria-hidden` (`showWord` false where the place says the state otherwise) |
  | Track | 40 by 24 px (the target of R-UI-044), round; `--quader-accent-fill` when on, `--quader-control-border` when off, their hover tokens under the pointer; never green, which is a positive status |
  | Knob | 16 px, `--quader-on-accent`, 4 px from the track's edge, with `check` or `close` of the icon font at 12 px in the track's colour; it slides over the motion duration (none under reduced motion) |
  | Focus | The focus ring of every control, 2 px off the track |
  | Line | Centred on its text's line by the line rule (`--quader-line`), so it makes a row's line no taller (R-UI-175) |
  | State | The screen sets `checked` from the server's answer; a click only asks (`checkedChange`). `busy` holds it (`aria-busy`) while the answer is open; `disabled` where it cannot change. A reader without the right to change sees no switch: in a table's cell under its column **State** the word alone; elsewhere the sentence of the settings pages, "**&lt;the switch's name&gt;** is on." ([Q-UI-960](../spec/api-and-ui.md#q-ui-960--who-may-only-read-a-cards-switch-reads-it-as-the-settings-pages-say-a-switch-to-an-auditor)) |
- A choice among three or fewer exclusive things, where the reader should see them all, is a radio group, each option with a
  muted sentence under its label. Four or more is a select. A disabled option stays visible, muted, and says why.
- A visibility level is chosen with `<quader-visibility-level>` ([visibility-level.ts](../../web/src/app/shared/ui/visibility-level.ts), [Q-UI-228](../spec/api-and-ui.md#q-ui-228--a-level-is-chosen-in-a-radio-group-of-three-each-with-its-icon-and-a-sentence)): a native radio group of Private, Internal and Public, each with its icon (`lock`, `shield`, `public`, as the owner's GitLab has them) and one muted sentence that names what the level admits for the `subject` (new projects, a project, artifacts, wiki). An option that cannot be chosen is disabled, stays visible and shows its reason under the sentence (`reasons`), which is also part of the option's description. A restricted level an administrator may still choose, or the level an object keeps, is enabled with a muted note under it (`notes`); a reason wins over a note, and the reasons come in the order of R-AUTHZ-038 (`firstReasons`). The group is named **Visibility level** where one level of a thing is chosen (a project, a repository). What the server answered for the level when it refused the save stands under the options (`error`: the field problem at the level's member), and a group inside a fold whose toggle names it keeps its legend for assistive technology only (`labelHidden`). A second level of the same thing that only an expert sets (the wiki's own level of a repository) is such a fold, closed while it has the first level and opened by the form while it differs ([Q-UI-770](../spec/api-and-ui.md#q-ui-770--the-wikis-own-level-is-a-fold-of-the-section-visibility-closed-while-the-wiki-has-the-repositorys-level)). The levels an administrator restricts are chosen with `<quader-restricted-levels>`, checkboxes laid out as the same options ([Q-UI-541](../spec/api-and-ui.md#q-ui-541--a-restricted-level-is-a-checkbox-laid-out-as-a-level-option-and-the-default-and-the-restricted-levels-exclude-each-other-in-the-form)). The same level shown before a name in a list or a header is `<quader-visibility-icon>`: the icon with the level's name as its accessible label and tooltip ([Q-UI-230](../spec/api-and-ui.md#q-ui-230--a-level-is-shown-as-an-icon-before-a-name-as-gitlab-shows-it)). The icon is not a status colour.
- Only valid combinations can be chosen: a control that can reach a state that means nothing is the wrong control
  ([Q-UI-213](../spec/api-and-ui.md#q-ui-213--the-token-form-names-what-a-token-can-be-in-three-choices-and-only-an-administrator-is-offered-the-first)).

## Buttons and dialogs

| Kind | Class | Use |
|---|---|---|
| Primary | `btn btn-confirm` | The one main action of a view. At most one per page header and one per form or dialog |
| Secondary | `btn btn-default` | Other actions with weight |
| Quiet | `btn btn-tertiary` | **Cancel**, row actions, anything repeated down a list |
| Destructive | `btn btn-danger` | Only for the action that destroys, after a confirmation that names the object |
| Icon | `btn btn-icon` with `aria-label` | Toolbar and row actions |

Labels are a verb and its object in sentence case (**Add member**, **Save changes**). All controls are 32 px high; `btn-sm` is not
for general use. In a dialog, the buttons are at the end, **Cancel** first and the action last.

A bar that sticks to the foot of a long form (the policy editor's) closes the page down to the panel's edge: it takes the
content's bottom padding into itself, so nothing shows below it
([Q-UI-576](../spec/api-and-ui.md#q-ui-576--the-policy-editors-save-bar-closes-the-page-down-to-the-panels-edge)).

Use a dialog for a short, self-contained step that leaves the page behind it in place (adding a member, confirming a deletion). A
long or repeated form is a page. Open it with `QuaderDialog`: focus, Escape and the return of focus come with it.

## Loads

Every load of remote content shows the spinner the owner picked, the mark's three rows of stones lit one after another in sandstone
([R-UI-217](../spec/api-and-ui.md#r-ui-217--every-load-shows-the-spinner-the-owner-picked)); the pieces and how a screen uses them are
in [web-ui.md](web-ui.md#loads-and-the-spinner-r-ui-217).

| Place | How |
|---|---|
| A page, a tab or a large area that loads | `<quader-loading size="page">` with the sentence of what loads ("Loading the repository."): the spinner of 48 px above it, centred, where the page will stand |
| A section, an opened row, a dialog's body | `<quader-loading>`: the spinner of 16 px on the sentence's first line, before it ("Reading the package from the upstream.") |
| A list | the data table's `loadingText` ("Loading the members."): shown in the table's place whenever it loads and holds no row; **Load more** spins where the rows arrive |
| A select or a field whose choices load | the field held, its one option or placeholder saying what loads, the spinner inside its box |
| A button whose act waits for the server | `[quaderBusy]="flag()"` beside `[disabled]="flag()"`, or the bare `quaderBusy` for an act without a flag: the label stays, the spinner stands before it or in place of its icon |
| A switch whose answer is open | `[busy]`: the spinner in place of **On** or **Off** |

- **The sentence says what loads.** "Loading." only where nothing more is known; a list's sentence names the list ("Loading the
  tokens."), a search names the text ("Searching the upstream for “common”.").
- **Nothing shows for a fast answer**: the delay of 300 ms is the stylesheet's, and the load takes its place from the start, so nothing
  moves when it appears.
- **A live view's read again shows nothing** (R-UI-213); a new question (another search, filter, level or page) shows its load and never
  the answer or the empty sentence of the question before.
- **Never another spinner**: no turning icon, no spin of a component's own. Sandstone moves only here; it is no state colour.

## Address field

`<quader-address-field>` (`web/src/app/shared/address-field.ts`, [R-UI-065](../spec/api-and-ui.md#r-ui-065--one-read-only-address-field-with-a-joined-copy-button-for-every-client-address)) shows
one line to copy the way the reference shows **Clone with SSH**: a read-only input group. It is the one pattern for a value to
copy of one line, an address or a command
([Q-UI-320](../spec/api-and-ui.md#q-ui-320--every-value-to-copy-of-one-line-is-a-field-with-its-joined-copy-button-a-page-shows-one-client-address-and-a-panels-selects-share-one-width)).

```html
<quader-address-field [value]="address" label="Client address" copyLabel="Copy client address" />
```

| Input | Meaning |
|---|---|
| `value` (required) | The address, one line. An empty value renders nothing |
| `label` | The accessible name of the field; in a list it names the row (**Client address of npm-internal**) |
| `copyLabel` | The accessible name and title of the copy button (**Copy client address of npm-internal**) |

No list shows an address field ([Q-UI-426](../spec/api-and-ui.md#q-ui-426--a-projects-repository-list-has-no-client-address-its-row-offers-set-up-client)): a list row offers **Set up client** or, in the admin area, a quiet copy button.

- **When.** The address a client puts into its configuration: a repository's base URL or registry host with a path, a Bastion's
  repository address, an exposure rule's address; and a command of one line (the install command of a package, labelled
  **Install command**). A configuration file or a snippet of several lines is a [code window](#code-windows)
  ([Q-UI-272](../spec/api-and-ui.md#q-ui-272--an-address-field-holds-one-address-multi-line-snippets-stay-code-blocks-and-the-bastion-address-becomes-a-field), Q-UI-320, Q-UI-801).
  A hash (digest, fingerprint) is not an address either: `<code>` with its short form and the full text as `title`.
- **A repository's address on Set up client only** ([R-UI-169](../spec/api-and-ui.md#r-ui-169--a-repositorys-address-stands-in-a-field-on-set-up-client-only),
  the owner's rule of W-20). The header of the page Set up client shows the client address, beside the snippets that use it. The
  repository's own page leads there with **Set up client**, first of its header's actions, and has no address field in its header;
  the start page, the side bar and the repository's settings show no address. The one further address, **Address for downloads by
  path (curl, wget)**, stays on the repository page's tab **Details** with its visible label, because Set up client does not show it;
  two fields without a label make the user guess (Q-UI-320).
- **Measures.** Height `--quader-control-height` (32 px); text `--quader-font-mono` at 0.875rem; padding `--quader-space-1`
  `--quader-space-3`; border 1 px `--quader-control-border` (hover `--quader-control-border-hover`, focus the ring of the other
  controls); fill `--quader-control-bg`, not the grey of `.text-input[readonly]`
  ([Q-UI-274](../spec/api-and-ui.md#q-ui-274--the-field-keeps-the-fill-and-border-of-an-editable-control-not-the-grey-of-a-read-only-value)).
  The button is the 32 px `btn btn-icon` copy button, attached to the input's right edge without a gap, its left border shared
  with the input's, inner corners square, outer corners `--quader-control-radius`.
- **Width: one per place, never per address, never more than its share.** The field fills its line and never takes its
  address's width.
  - A list has no field and no copy of an address
    ([Q-UI-1042](../spec/api-and-ui.md#q-ui-1042--the-repositories-of-every-project-are-the-project-lists-columns-with-the-project-and-no-new-repository)).
    A system repository of `upstream` offers no address anywhere: its address answers every client 404, and no screen offers an address, a download
    or a command of it ([Q-UI-898](../spec/api-and-ui.md#q-ui-898--nothing-offers-what-a-system-repository-refuses-no-address-no-download-no-command)).
    Where it is listed among other repositories (the list of every project's repositories, the search's suggestions and results) it carries the neutral badge
    **System** after its key ([Q-UI-902](../spec/api-and-ui.md#q-ui-902--where-a-system-repository-is-listed-among-others-it-carries-the-badge-system)).
    A project's list, the list of every project's repositories and the repositories behind a format's tile offer **Set up client** in the row, as the quiet row action
    `btn btn-tertiary row-setup` whose accessible name ends with the key; on a page and in a header it is a default button, never
    blue ([Q-UI-487](../spec/api-and-ui.md#q-ui-487--set-up-client-is-a-default-button-on-a-page-and-a-quiet-row-action-in-a-table-row)). The search results offer neither ([Q-UI-426](../spec/api-and-ui.md#q-ui-426--a-projects-repository-list-has-no-client-address-its-row-offers-set-up-client)).
  - A field alone on its line, or a stack of the addresses of one thing, fills the line up to `--quader-address-measure`
    (40 rem, 70 characters), so a stack is one width ([Q-UI-301](../spec/api-and-ui.md#q-ui-301--a-field-alone-on-its-line-fills-the-line-up-to-a-measure-of-70-characters));
    a longer address widens its field up to the line, so it is never cut beside empty room
    ([Q-UI-683](../spec/api-and-ui.md#q-ui-683--an-address-field-is-as-wide-as-its-address-at-least-the-measure-at-most-its-line)).
  - Where the field is shorter than its address, the address scrolls inside it (the input at least 8 rem), the button stays
    attached. Never an ellipsis.
- **Behaviour.** Focus or click selects the whole address; copy uses `quader-copy-button` (clipboard, polite status, alert on failure).
  The address is the input's value, so a test reads `.value`, not the text content.

## Code windows

A snippet a reader copies into a client is a window, never a grey box ([Q-UI-801](../spec/api-and-ui.md#q-ui-801--a-snippets-window-is-an-editor-for-a-file-and-a-terminal-for-a-command-coloured-by-its-language),
the owner's choice of [Q-UI-800](../spec/api-and-ui.md#q-ui-800--set-up-client-takes-variant-a-snippets-are-windows-the-page-has-pictures-and-less-text)):
`<quader-code-window>` ([code-window.ts](../../web/src/app/shared/code/code-window.ts)).

```html
<quader-code-window [content]="sn.content" [language]="sn.language" [fileName]="sn.fileName" [marks]="sn.marks" [snippetId]="sn.id" />
```

| Input | Meaning |
|---|---|
| `content` (required) | The text, exactly as it is copied |
| `fileName` | The file it belongs in: an editor window, its tab the name, its lines numbered, **Download** in its bar. `null`: a command, a terminal window with a prompt per command |
| `language` | The syntax (`ini`, `yaml`, `json`, `xml`, `text`, `shell`, `powershell`, `cmd`), the API's `language`: it picks the colours and the prompt, never the format |
| `marks` | The parts that are the reader's, the API's `marks`: `token` a chip, `value` tinted, dashed and italic, each with a title |
| `label` | A terminal's title in place of its command line's name (`bash, zsh` for the command that reads a token) |
| `copyLabel`, `downloadable`, `shownLines`, `snippetId` | The copy button's name (**Copy snippet**), whether a file offers **Download**, a fold to a number of lines (the install panel), the `data-snippet` of the text |

- **When.** A snippet of several lines, a configuration file, a command a reader runs: the snippets of **Set up client**, the command
  of several lines of the install panel, the command that reads a created token. A value of one line to copy stays the address
  field below (Q-UI-320). A JSON document, a request body, a public key or a log line is no snippet and keeps `pre.code-block`.
- **The look.** The window's colours are the picture tokens `--quader-code-*` ([design-language.md](design-language.md#pictures));
  the line over the tab is the format's accent where the page sets `--quader-art-accent`, the neutral one elsewhere. No window
  dots, no name or logo of an editor or a terminal product.
- **Wrapping.** A long line wraps with an indent of two characters under itself and never inside a flag, a mark or a token's
  reference; an address breaks after its slashes. Nothing in a window scrolls sideways, at 390 px either. The tab and the buttons
  stand centred in the bar, so a button's middle is the middle of the tab's line (R-UI-175).
- **Words.** At most one sentence over a window, only what the window does not say; a file name, a setting or a command in it is
  code (in a sentence of `clients.ts`, between backticks).

## Vulnerability grade

A version's vulnerabilities are shown as its grade, the way the owner's cogpit grades an image
([R-VULN-038](../spec/vulnerability-scanning.md#r-vuln-038--the-grade-of-a-version),
[R-UI-090 to R-UI-094](../spec/api-and-ui.md#r-ui-090--the-grade-shield)): a shield with the letter S, A, B, C, D, E or N/A, the
count that decides it ("13 Critical" beside the shield E; the letter is the shield's and is not written again), the rule it stands for ("More than 5 Critical"), the counts as a severity bar and a
line of facts. Four shared pieces in [web/src/app/shared/vulnerability/](../../web/src/app/shared/vulnerability/) draw it; no
screen draws its own, and no screen shows a version's highest severity instead.

```html
<quader-grade [grade]="item.vulnerabilityGrade" />                       <!-- a list cell, a header, a tab -->
<quader-grade-card [grade]="g" [packageName]="name" [findingVersions]="[version]" [image]="false" />
<quader-severity-bar [counts]="summary.bySeverity" [queries]="filterPerSeverity" />
<quader-grade-shield grade="E" size="card" label="Grade E" />           <!-- alone only; beside its text it is decorative -->
```

| Piece | What it is | Inputs |
|---|---|---|
| `<quader-grade>` | The shield of 20 px (a format icon's size in a list), centred on its line, which carries the letter, and the one statement of a grade beside it: "1,145 findings · 112 fixable", "no findings", "not scanned", and "OS end of life" for an image whose OS is ([R-UI-143](../spec/api-and-ui.md#r-ui-143--a-grade-says-the-same-thing-in-every-list)); a screen reader hears "Grade E · 1,145 findings · 112 fixable", and the rule with every count as its description. It is the button of the grade's details (`<quader-grade-details>`: the rule, the counts, and when, by what and against which data it was checked, R-VULN-051), on hover, focus and tap (`quaderDetailPopover`). A repository's list, its opened rows, a package's versions and a version's facts use it | `grade` (`VulnerabilityGrade` of the API); `image`, `format`; `findingsLink` ends the details with a link (a version's facts) |
| `<quader-grade-card>` | The shield of 32 px, the text, the rule, the bar and the facts: an image's OS, its end of life (warning colour) and "Scanned 1 h ago (…)", a package's "Checked 3 min ago (…)", and **Findings in** the repository, a link to the report filtered by repository, package and version; its text opens the same details as `<quader-grade>` | `grade`, `packageName`, `findingVersions` (the version, or an image's manifest digests), `image`; `barQueries` and `activeSeverity` make the bar the filter of the page's findings (the package page) |
| `<quader-severity-bar>` | Critical, High, Medium, Low (with None) and Unknown as five segments, each as wide as its share (at least 7 %) plus the room of its count; Unknown hatched; a severity without findings faint, its 0 readable | `counts`; `queries` turns the segments into filter links (the report's header replaces the query with them, `merge` keeps the page's other parameters); `active` marks the segment of the filter in force |
| `<quader-grade-shield>` | The crest as inline SVG with its letter | `grade`, `size` (`list` 20 px, `card` 32 px), `label` |
| `<quader-time-ago>` ([time-ago.ts](../../web/src/app/shared/ui/time-ago.ts)) | One relative time and the absolute time after a dot: "3 hours ago · 26.11.2026 21:22:18 (Thu.)", a card's or a page's fact ([Q-UI-378](../spec/api-and-ui.md#q-ui-378--a-time-on-the-grade-card-is-one-relative-time-with-the-absolute-time-in-brackets-from-one-component), [Q-UI-1525](../spec/api-and-ui.md#q-ui-1525--a-relative-time-and-its-absolute-time-are-joined-by-a-dot-not-brackets)); with `absolute="title"` the relative time alone, the absolute one its title, a list's cell ([Q-UI-431](../spec/api-and-ui.md#q-ui-431--a-time-in-a-list-is-relative-with-the-absolute-time-as-its-title-a-log-writes-the-moment)). No screen formats a time with the date pipe or a helper of its own | `at` (RFC 3339), `absolute` |

- **When.** A list shows `<quader-grade>`; the deciding count, the rule, the bar and the facts are the card's, on the detail page (simple by default, details
  on demand, [Q-UI-374](../spec/api-and-ui.md#q-ui-374--a-list-shows-the-shield-and-the-text-the-bar-and-the-facts-are-the-cards)).
  A finding keeps its severity badge (`<quader-severity>`): a grade belongs to a version, a severity to a finding.
- **The grade is the server's.** The API answers `VulnerabilityGrade` ([R-VULN-039](../spec/vulnerability-scanning.md#r-vuln-039--the-grade-in-the-api));
  [grade-words.ts](../../web/src/app/shared/vulnerability/grade-words.ts) words it (text, rule, tooltip) and orders it
  (`gradeRank`, `worstGrade`) and never decides it again. A caller who gets no grade sees no grade column and no card, not N/A
  ([Q-UI-377](../spec/api-and-ui.md#q-ui-377--a-list-without-grades-has-no-grade-column)).
- **Colours.** `--quader-grade-s` to `--quader-grade-e` and `--quader-grade-na`, the inks of the letters, `--quader-sev-*` for the
  bar ([design-language.md](design-language.md#vulnerability-grades)). S, A and B are the only greens beside the positive
  status ([Q-UI-370](../spec/api-and-ui.md#q-ui-370--the-grades-take-cogpits-colours-and-s-a-and-b-are-the-greens-of-a-good-grade)).
  The letter and the text carry the grade; the colour never does alone.
- **A severity has one colour** wherever it is shown ([Q-UI-860](../spec/api-and-ui.md#q-ui-860--one-severity-scale-for-the-bar-the-trend-and-the-badge-the-bars-fills-with-a-mark-tone-of-the-same-hue),
  [the severity scale](design-language.md#the-severity-scale)): its fill `--quader-sev-<severity>` in the bar and as a band of the
  trend, its mark `--quader-sev-<severity>-mark` as the band's edge and the icon of `<quader-severity>`, whose word stays in the text
  colour. A severity never takes a status colour (`--quader-status-bad` is an error, not Critical), and a chart of something
  that is no severity takes a token of its own, as the usage screens take `--quader-chart-usage`.
- **Sorting.** A list column of grades sorts by `gradeRank` (the grade, then the count that decides it, N/A lowest); a paged list
  loads up to 1,000 entries first ([Q-UI-373](../spec/api-and-ui.md#q-ui-373--the-list-sorts-by-grade-in-the-browser-over-the-whole-list-up-to-1000-entries)).

## A control beside a text

Wherever a control or an icon stands beside a text it belongs to (a button, a copy button, a menu's trigger, a check box, a badge,
a status, a chip, an avatar, a project's mark, a format's or a visibility level's icon, a glyph), its middle is the middle of the
text's first line within 1 px, and it makes the line no taller
([R-UI-175](../spec/api-and-ui.md#r-ui-175--a-control-or-an-icon-beside-a-text-stands-on-the-middle-of-that-texts-first-line)).
The row's items stand at its top (`align-items: flex-start`, a cell's `vertical-align: top`), and the line rule of
[_controls.scss](../../web/src/styles/_controls.scss) centres the control on the line it stands in:

```scss
margin-block: calc((var(--quader-line) - <the control's height>) / 2);  // --quader-line: the line of the box it stands in
vertical-align: top;                                                     // in running text
```

- The icons of running text (`.material-icons.inline`, a paragraph's icon), every badge and status, the project's mark and every
  check box carry it already. A 32 px button or copy button beside a text gets the class `on-line`
  (`<button class="btn btn-icon on-line">`, `<quader-copy-button class="on-line">`); in a table's cell the rule of the data table
  does it for the cell's direct children.
- A box of controls beside a heading (a page's actions, a file's actions) is as high as the heading's line, with its controls
  centred in it.
- Never `vertical-align: middle` (the middle of the lower-case letters, 1.5 to 1.7 px under the line's) and never
  `align-items: last baseline` for a mark: `make web-lint` refuses both (R-UI-175, except a table cell's own alignment).
- The exceptions are layouts, not places
  ([Q-UI-724](../spec/api-and-ui.md#q-ui-724--the-exceptions-are-layouts-not-places-above-or-below-a-field-under-its-label-a-picture-a-mark-after-its-text-a-framed-box-a-word-that-wraps)):
  a control above or below its text (a tile, an empty state), a field under its label, a picture (an avatar, the medium mark, a
  grade's shield, `.picture`) beside a name and its second line or beside a card's text, centred on them, a mark after its text on
  the text's last line, a control beside a framed box (a code block) on the box.

`make ui-layout-test` proves the rule on the shared patterns in seconds; `make ui-renders` measures every screen
([the checks](web-ui.md#renders-of-every-screen)).

## Text

- A link to another site carries the mark `open_in_new` and a visually hidden "(opens another site)", all three in
  `<span class="external-link">`, so the mark stays on the link's last line where a long address wraps.

- Sentence case everywhere; upper-case is not used for labels.
- 14 px on 21 px for text and controls, 12 px for small text. Headings use `--quader-heading-1` to `-3`.
- A setting's name or value of the configuration in running text is `<code translate="no">`
  ([Q-UI-571](../spec/api-and-ui.md#q-ui-571--a-settings-name-in-running-text-is-code)). Such a value with a punctuation mark after it
  stands with the mark in `<span class="with-stop">`, in the template and in the German unit, so the mark never begins a line at 390 px
  ([Q-UI-1615](../spec/api-and-ui.md#q-ui-1615--a-value-and-the-punctuation-after-it-stay-on-one-line)).
- Say what happens, in plain words: **Filter members**, not **Search criteria**. Do not name internal terms where the reader
  needs none: **All repository permissions of the owner, without administration**, not `inherit`.
- A label promises exactly what the thing does. If it does less than it says, the label is a bug.
- Every string has an `i18n` ID and a German translation in `messages.de.xlf`. Keys, names and paths carry `translate="no"`.

## Accessibility

Each of these fails the screen, whatever it looks like: a control without a name, an icon that carries meaning without text, a
state told by colour alone, a focus that is not visible, an element that the keyboard cannot reach. Both schemes must meet the
contrast of WCAG AA. The unit tier can run axe on a rendered screen (`seriousViolations` in [axe.ts](../../web/src/app/testing/axe.ts)); a new screen's spec calls it.

## Checklist

Before a screen is committed:

- [ ] Follows [Simple by default, details on demand](#simple-by-default-details-on-demand): the standard flow holds only what the
      thing needs to work, expert settings and facts are behind a disclosure, defaults are field values, fields fit their input,
      the title names the thing, no field is red before the user touched it, every step or section names one topic.
- [ ] Rendered with `make ui-renders` ([web-ui.md](web-ui.md#renders-of-every-screen)) and every changed screen looked at in
      both schemes and at 390 px, as each kind of user that reaches it; the contact sheet lists no failure, no page that scrolls
      sideways at any width and no region that does at 1024 px and wider, that the change brought; no layout finding (an icon apart
      from its label, a short thing broken, a heading broken inside a word, a row off its line or with other actions, a cut text with room, a stacked field at another place than in the other blocks, a cell that shows nothing, a table stacked though
      it fits, a page that scrolls into nothing, a control or an icon off its text's line, a box that scrolls by itself in the page
      (`inner-scroll`, R-UI-215): [the checks](web-ui.md#renders-of-every-screen), R-UI-170 to R-UI-176); and in
      each picture what the checks do not see: a table that ends before the lines above it, a field far wider than its input, two
      words or two styles for one thing, an element that takes the room without being the screen's job.
- [ ] Compared side by side with the reference's screen for the same job, in a real browser, not only in a unit test.
- [ ] Seen in the dark and the light scheme, and at a narrow window.
- [ ] Only shared controls, classes of `_controls.scss` and `--quader-*` tokens; no colour literal.
- [ ] One `<h1>`, one primary action per view, one way to each job (no button and tab for the same thing).
- [ ] A row's one action is its quiet word, several are its menu **Actions for &lt;name&gt;**; no menu of one entry. A row that sets a state by a switch has the switch in that state's cell beside its badge and an act of one cell as that cell's one word, and no menu ([Q-CUR-201](../spec/remote-curation.md#q-cur-201--a-versions-acts-stand-where-their-state-stands)).
- [ ] No paragraph explains a setting: its help is behind the info sign; what becomes public, a refusal and a state that needs action
      stay visible, one line each ([R-UI-267](../spec/api-and-ui.md#r-ui-267--help-behind-an-info-sign)).
- [ ] A control the Helm configuration fixes, or that cannot act for a reason of its own, is offered unavailable with the reason as
      its tooltip (`quaderUnavailable`), never hidden and never explained by a paragraph
      ([R-UI-270](../spec/api-and-ui.md#r-ui-270--a-control-fixed-by-the-helm-configuration-is-offered-unavailable-its-reason-its-tooltip)).
- [ ] No `info` icon outside `<quader-help>`: a line that needs action carries `warning`, any other none
      ([Q-UI-1628](../spec/api-and-ui.md#q-ui-1628--the-help-sign-on-every-settings-page-and-no-info-icon-beside-a-status)).
- [ ] A column's help and a description above a table say what holds for every row; what holds for one row, one format or one special
      case is said in that row, in words seen without hover ([Q-UI-1500](../spec/api-and-ui.md#q-ui-1500--a-columns-help-says-what-the-column-means-for-every-row-what-holds-for-one-row-is-said-at-that-row)).
- [ ] Every value to copy of one line is an address field; a page shows one client address, further ones labelled in Details.
- [ ] Every control named; the keyboard reaches everything; a spec of the screen calls `seriousViolations` and it passes.
- [ ] Every string has an `i18n` ID and a German translation; a German word of more than 20 letters has its soft hyphen.
- [ ] Every load shows the spinner with the sentence of what loads, every act that waits for the server its busy button (R-UI-217).
- [ ] The spec names the decision, and the manual describes the screen where it did.

## Deliberate differences

These differ from the reference on the owner's decision. Everything else that differs is a defect.

| What | Quader | Why |
|---|---|---|
| Green | A positive status only, so the badge **It's you** is blue | [Q-UI-96](../spec/api-and-ui.md#q-ui-96--cogpit-blue-as-the-primary-colour-green-only-for-positive-status) |
| Content width | The panel's full width, the reference centres 1280 px | Owner, 2026-11-06 ([design-language.md](design-language.md#what-this-does-not-cover)) |
| Links | Underlined in running text; in tables only under the pointer | [Q-UI-128](../spec/api-and-ui.md#q-ui-128--the-references-type-colours-and-measures), R-UI-044 |
| Icons | Material Icons, outlined; a package format's icon is its owner's logo where allowed ([Lists](#lists)) | Owner, 2026-11-06 and 2026-11-11 |

## What does not follow this page yet

The guide was written on 2026-11-10 from the owner's screenshots of the reference's member list and general settings. Screens
written earlier were not reviewed against it, so this list is what is known, not all there is.

- Moved to one column ([Q-UI-222](../spec/api-and-ui.md#q-ui-222--which-settings-pages-are-one-column-now-and-what-a-sections-save-button-is-called)): the project's settings ([project-settings.ts](../../web/src/app/features/projects/project-settings.ts), the worked example), the general and the authentication settings, the service account and the group. Where a page has more than one save button, each is **Save changes** with the group in a visually hidden suffix, and sends only its group.
- Moved with [Q-UI-223](../spec/api-and-ui.md#q-ui-223--the-user-the-endpoint-and-the-service-account-and-group-lists-follow-the-guide) and [Q-UI-224](../spec/api-and-ui.md#q-ui-224--the-policy-editor-and-the-issuer-page-keep-their-structure-and-lose-the-cards-and-the-delete-in-the-header): the user, the endpoint, the policy editor and the issuer pages, and the lists of the group and service-account pages. The policy editor keeps its table of contents, its preview and its review before the save; the issuer page keeps its tabs of different objects.
- Moved with [Q-UI-333](../spec/api-and-ui.md#q-ui-333--a-pages-sections-are-quiet-disclosures-tokens-too-open-while-there-is-a-token-the-profile-follows): the profile (one column, the identity as read-only fields, quiet sections), and the section **Tokens** of the user and service-account pages, a disclosure the page opens when there is a token and never closes itself (`TokenManager.heldTokens`).
- Moved with [Q-UI-351](../spec/api-and-ui.md#q-ui-351--the-repository-settings-page-is-one-column-of-sections): the repository's settings ([repository-detail.html](../../web/src/app/features/repositories/repository-detail.html)), one column whose standard sections are open and whose details are closed sections, each with its own save, and Disable, Move and Delete in **Advanced**. The exposure rule editor, the mapping editor and the other editors of one object are not settings pages and are not part of this list.

## What this does not cover

- The settings rules and the 56 rem column are read from one screenshot of the reference, not measured from its rendered page; the
  values of Q-UI-128 come from its admin dashboard and user list.
- Nothing enforces this page except the lint rules of R-UI-046, R-UI-175 and R-UI-217, the layout checks of the renders and their fixtures (R-UI-170 to R-UI-176) and the
  review before a commit. Whether a screen matches the reference
  cannot be tested automatically.
