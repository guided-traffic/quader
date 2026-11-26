# Imprint and privacy notice

Status: implemented on 2026-11-26 (R-UI-225, R-UI-226 of [api-and-ui.md](../spec/api-and-ui.md#r-ui-225--the-installation-serves-its-own-imprint-and-privacy-notice-from-the-charts-markdown),
R-BAS-100 of [bastion.md](../spec/bastion.md#r-bas-100--a-bastions-own-imprint-and-privacy-notice-from-its-chart),
[ADR-302](../decisions.md#adr-302--imprint-and-privacy-notice-are-the-installations-own-pages-from-the-charts-markdown)). The main installation
and a Bastion each serve an imprint and a privacy notice that their operator writes as Markdown into the chart's values. This page says what
such a text can do to the server and to a person who reads it, and who can change it.

It describes [internal/legaltext](../../internal/legaltext/legaltext.go), the composition root's
[wire_legal.go](../../internal/app/wire_legal.go), [legal_api.go](../../internal/api/v1/legal_api.go), the always-open route of the
authentication stage ([stage.go](../../internal/auth/stage.go), `alwaysOpen` and `sessionAllowed`),
[internal/bastion/legal](../../internal/bastion/legal/legal.go) and the README renderer it shares,
[internal/packages/readme](../../internal/packages/readme/render.go).

## Who writes the texts

The texts are `config.ui.legal.{imprint,privacy}.{en,de}` of the chart `quader` and of the chart `quader-bastion`. They reach the process
through the configuration file of the ConfigMap, as every other key of `config` does; whoever may change the Helm release may change them,
and nobody else: no operation of the management API, no screen and no feed of the Bastion interface writes them. They are public by intent,
so they are never a Secret. A change is a rollout (the configuration's checksum annotation), never a reload.

A Bastion's texts are its own chart's. The Bastion reads nothing of them from the main installation, and the main installation's texts
never reach a Bastion (R-BAS-100, the owner's rule that a Bastion tells nothing of the main installation).

## What the renderer keeps and drops

Each text is rendered once at start by an instance of the README renderer of its own (R-UI-008): goldmark with GitHub's extensions, then
bluemonday's allowlist, then the finisher. An operator's Markdown is not trusted more than a package author's README.

- **Size.** At most 64 KiB per page and language: a longer text stops the start (the binary counts bytes) and the chart's schema refuses
  it (it counts characters). The renderer's own limits (1 MiB of HTML, 2 s) hold as for a README.
- **Dropped with their content:** `script`, `style`, `iframe`, `object`, `embed`, `svg`, `math`, `noscript`, `template`, `form`,
  `select`, `textarea`, `title`. Every attribute outside the allowlist goes, `style` and every event handler with it.
- **Links** keep `http`, `https` and `mailto` targets and get `rel="nofollow noopener noreferrer"`; a `javascript:` or any other scheme
  and a relative target lose the link and keep the text; `target` is removed.
- **Images** never load. The instance admits no external image origin, whatever `ui.readme.external_image_origins` says: an image of
  another site becomes the link "External image: …", and an image of one of the installation's own origins (the main installation's base
  URLs and endpoints, or the Bastion's `server.base_url`) is dropped, so a text cannot make a reader's browser send a request to Quader
  with the reader's session. A reader's browser therefore sends no request to a third party for reading a legal page, which is also what
  a privacy notice would have to declare otherwise.
- The browser binds the HTML through the README view, so Angular's sanitizer and the document's Trusted Types policy run on it again
  (R-UI-008's second and third layer).

Tests: [legal_test.go](../../internal/bastion/legal/legal_test.go) (the whole list on a realistic imprint),
[wire_legal_test.go](../../internal/app/wire_legal_test.go) (the main installation's instance).

## Who can read them

`GET /api/v1/legal` is always open: on every endpoint that admits a request without a credential, whatever the visibility levels say,
on the replicas that serve the management API (`api`, `web`, `all`). On an endpoint with `auth.anonymous: false` it is `401` like every
request without a credential (R-EP-010), as the login options are. It answers the texts and nothing else: no principal, no setting, no
name of a repository. Only `GET` and `HEAD` are open; a write to the address stays under the hard limit of `/api/v1` (`401`). The session
cookie is ignored on it, as on the login options (Q-AUTH-177), so an ended session neither hides the pages nor is told by them. The
general rate limits of the management API apply.

The answer carries `Cache-Control: no-cache` and a strong ETag instead of the management API's `no-store`: the content is the same for
every caller and holds nothing personal, so a browser may keep it and ask again with the tag.

## What this does not cover

- [H-2304](../spec/bastion.md#h-2304--a-bastion-serves-its-imprint-and-privacy-notice-only-with-its-web-interface) A Bastion serves its
  pages only with its web interface (closed by W-35's P9: the web interface serves and links them, also while it has no catalogue to
  show; with the switch `gui` off a Bastion serves no page).

### H-2303 · An operator's text is not checked: its links lead wherever it says

Stage: the chart's values, rendered at start. Subject: a person who reads a legal page, signed in or not.

Live wherever an operator sets a text. The renderer takes away what could run, style or load, but not what the text says: a link "Contact"
can lead to any `https` address, and the text can claim anything, also on the pages the sign-in screen links to. Quader neither checks nor
completes what an imprint or a privacy notice has to contain. What makes it real: a changed values file of the Helm release, which means
someone who could change the deployment as a whole; the operator answers for the texts (the owner's words of R-UI-225).
