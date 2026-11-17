# Rename: trove becomes Quader

Working checklist of the rename the owner ordered on 2026-11-17. A box is ticked when the step is done **and** checked the
way the step names. The file is deleted in the change that ticks its last box; what stays is the decision record in
[decisions.md](../decisions.md).

## Decided

| Topic | Value |
|---|---|
| Product name | Quader (prose), `quader` (identifiers) |
| The internet-facing cache | keeps the name Bastion: "Quader Bastion", `quader-bastion` |
| Go module, repository | `github.com/guided-traffic/quader`, `guided-traffic/quader` |
| Binary, config file | `quader`, `/etc/quader/quader.yaml` |
| Environment variables, metrics | `QUADER_*`, `quader_*` |
| Token and cookie prefix | `qdr_` (`qdr_pat_`, `qdr_sat_`, `qdr_cit_`, `qdr_bst_`, `qdr_rgt_`, `qdr_session`, `qdr_csrf`, `qdr_oidc_binding`); same length as before, a token stays 57 characters. Chosen by the agent, open to the owner's objection |
| Image, charts | `guidedtraffic/quader`, charts `quader` and `quader-bastion` |
| Mark | Quader: a wall of three courses, one stone coloured. Bastion: a tower of three courses (battlements, overhanging crown, shaft), the middle merlon coloured. 64 grid, margin 4, stone height 16, joint 4, corner radius 1.5 |
| Colours of the mark | stones `#28272d` on light, `#ececef` on dark; coloured stone `#b4690e` on light, `#e9a13b` on dark |

No compatibility: no alias, no old prefix accepted, no migration path (pre-alpha rule of [CLAUDE.md](../../CLAUDE.md)).

## 1 Bulk rename (one pass over the whole tree, main agent)

- [x] 1.1 Content: `trove` → `quader`, `Trove` → `Quader`, `TROVE` → `QUADER`, `trv_` → `qdr_` in every tracked text file;
      `go.sum`, `package-lock.json` integrity values and binary fixtures are left out
- [x] 1.2 Paths: every tracked path that carries the name (`cmd/trove`, `deploy/helm/trove`, `deploy/helm/trove-bastion`,
      `web/public/brand/trove-*`, `web/src/app/shared/brand/trove-mark.ts`, the rest of the 120) moved with `git mv`
- [x] 1.3 `gofmt` over all Go files (the name is one character longer: alignment and import order move)
- [x] 1.4 Check: `git grep -i trove` and `git grep trv_` return only what section 5 lists as intended

## 2 Go (subagent)

- [x] 2.1 `go build ./...` and `go vet ./...`
- [x] 2.2 Generated code is current: `make generate-check`
- [x] 2.3 Test vectors and golden files that carry the prefix or the name are recomputed, not hand-edited
      (token vector of R-AUTH-022, scanner pattern, recorded fixtures)
- [x] 2.4 `make lint`, `make test`
- [x] 2.5 `make test-integration` (needs the local stack) — locally 22 packages and the six Bastion packages green, the other
      44 not run here; all three integration shards are green in the pull request's pipeline (run 44150760311)

## 3 Web and mark (subagent)

- [x] 3.1 `web/scripts/brand-logo.mjs` draws the new marks (wall, tower) and the wordmark "Quader"; the drafts directory and
      the development preview on the sign-in page are gone
- [x] 3.2 `web/public/brand/*`, `favicon.svg`, `favicon.ico` regenerated; the mark component and the colour tokens of the
      mark follow; the old brand colours are removed where nothing else uses them
- [x] 3.3 Every place that shows the name or the mark: shell head, sign-in, signed-out, page titles, manual, translations
- [x] 3.4 `make web-ci` (lint, tests, build)
- [x] 3.5 Rendered and looked at: sign-in and shell head, light and dark, wide and narrow (never committed)

## 4 Deployment, CI, release (subagent)

- [x] 4.1 Charts `quader` and `quader-bastion`: `make chart-lint`, `make chart-test`, `make promtool`
- [x] 4.2 Workflows, `Makefile`, `Containerfile`, `deploy/dev`, `hack`, `test/e2e` scripts: names, image references, job names
- [ ] 4.3 `make test-release-tooling`, `make versions-check`, `make compat-check` — release tooling green; `versions-check` red on
      the machine's Docker Desktop buildx (older than the pin, as before the rename); `compat-check` red by intent (the
      rename breaks the API against the last release) and on the migrations rule, which was red before
- [x] 4.4 What a release publishes and where is listed for the owner: image repository, chart location, release assets
- [x] 4.5 `make image` builds; `make e2e` for at least the default set (needs kind) — image green; default set 9 of 10
      (`TestE13Failover`: a worker pod OOM-killed under its 512Mi limit); bastion set 14 of 16, `RoutingByPath` red twice with
      the signature of the open finding F-116; in the pull request's pipeline every e2e set is green, `e2e (bastion)` included
      (run 44150760311), so the local failures are the machine's (arm64, memory), not the rename's

## 5 Documentation (subagent)

- [x] 5.1 Prose reads correctly after the replacement ("a Quader", plural forms, sentence starts, wrapped tables)
- [x] 5.2 [decisions.md](../decisions.md): one entry for the rename, the prefix `qdr_` and the mark; the entry on the earlier
      rename keeps its historic names
- [x] 5.3 README badges, links and anchors resolve under the new repository name; relative links still resolve after the
      path moves
- [x] 5.4 The manual (`docs/manual`) and the developer page on the mark describe the new mark
- [x] 5.5 [CLAUDE.md](../../CLAUDE.md) names the project Quader

## 6 Together (main agent)

- [x] 6.1 The checks of sections 2 to 4 pass on the combined tree
- [x] 6.2 `git grep -i trove` and `git grep trv_`: every remaining hit is intended and listed here

## 7 Outward steps (main agent, in this order)

By the owner's instruction of 2026-11-17 the rename lands as one pull request, not as a commit on `main`, and the agent
follows its pipeline until it is green.

- [x] 7.1 Branch `rename-quader`, one commit, pushed; one pull request against `main`
- [x] 7.2 The pull request's pipeline is watched job by job until it is green; a job the change broke is fixed on the branch
- [x] 7.3 What the merge publishes (section 4.4) is named to the owner in the pull request
- [ ] 7.4 This file is deleted on the branch once every box above is ticked
- [ ] 7.5 With the merge: GitHub `guided-traffic/trove` renamed to `guided-traffic/quader`, local `origin` follows (the owner's
      step, or the agent's on his word)

## 8 Outside the repository (named to the owner, not done unasked)

- [ ] 8.1 The local checkout directory `/Users/hfi/repos/trove` (the agent memory is keyed on that path)
- [ ] 8.2 The owner's installation on `wds18-k8s-main`: a new release name, new chart, new token prefix mean a fresh install
- [ ] 8.3 Docker Hub repository `guidedtraffic/quader`; the old `guidedtraffic/trove` and its tags
- [ ] 8.4 Trademark register and domains for "Quader" were not checked
