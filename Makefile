# Task runner for quader. GNU Make 3.81 or later (the version macOS ships).
#
# Every tool and image pin lives in tools/versions.env, which this file includes
# and CI appends to $GITHUB_ENV (E0.2). Go itself is pinned by the `go` line of go.mod;
# sqlc and oapi-codegen by the `tool` directives of go.mod; Node by web/.nvmrc.

SHELL := /bin/bash

include tools/versions.env
# Hand the pins to scripts (deploy/dev/*.sh, test/integration/*.sh).
export $(shell sed -n 's/^\([A-Z0-9_]*\)=.*/\1/p' tools/versions.env)

MODULE      := github.com/guided-traffic/quader
comma       := ,
space       := $(subst ,, )
GOVERSION   := $(shell go env GOVERSION)
GO_MOD_GO   := $(shell awk '$$1=="go"{print $$2}' go.mod)
NODE_VERSION := $(shell tr -d '[:space:]' < web/.nvmrc)
# The binary accepts only major.minor.patch or "dev" as its version: a commit hash would
# never become ready. A v0.* tag on HEAD gives its number; every other checkout is "dev".
VERSION     ?= $(or $(shell git describe --tags --exact-match 2>/dev/null | sed -n 's/^v\(0\..*\)$$/\1/p'),dev)
COMMIT      ?= $(shell git rev-parse --verify -q HEAD 2>/dev/null || echo unknown)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X $(MODULE)/internal/buildinfo.version=$(VERSION) \
	-X $(MODULE)/internal/buildinfo.commit=$(COMMIT) \
	-X $(MODULE)/internal/buildinfo.date=$(DATE)

# LOCAL_CORES caps the cores of a local gate: every Go command of a target runs with GOMAXPROCS and -p of that number, and the
# pinned lint with --concurrency of it. Four builders' gates beside each other took the development machine to a load above 100 on
# 2026-11-23, and tests failed by it. LOCAL_CORES= (empty) lifts the cap, for a single run on an idle machine; the pipeline's
# workflows set it empty, so a job keeps its own settings (docs/developer/build-and-test.md, "The cap of the cores").
# test/integration/run.sh honours the same variable. A GOMAXPROCS or a -p in GOFLAGS that the caller set stays.
LOCAL_CORES ?= 6
ifneq ($(strip $(LOCAL_CORES)),)
GOMAXPROCS ?= $(LOCAL_CORES)
export GOMAXPROCS
ifeq ($(filter -p=%,$(GOFLAGS)),)
export GOFLAGS := $(strip $(GOFLAGS) -p=$(LOCAL_CORES))
endif
LINT_CONCURRENCY := --concurrency $(LOCAL_CORES)
endif

# The tools' binary names carry their pin and the Go toolchain that built them, so a change
# of either one rebuilds them and a stale binary is never reused.
LINT_BIN       := .bin/golangci-lint-$(GOLANGCI_LINT_VERSION)-$(GOVERSION)
GOVULNCHECK_BIN := .bin/govulncheck-$(GOVULNCHECK_VERSION)-$(GOVERSION)
GOCYCLO_BIN    := .bin/gocyclo-$(GOCYCLO_VERSION)-$(GOVERSION)
OASDIFF_BIN    := .bin/oasdiff-$(OASDIFF_VERSION)-$(GOVERSION)
OSV_SCANNER_BIN := .bin/osv-scanner-$(OSV_SCANNER_VERSION)-$(GOVERSION)
# promtool is a release binary, not built by Go, so its name carries the pin only (tools/versions.env).
PROMTOOL_BIN   := .bin/promtool-$(PROMTOOL_VERSION)
PROMTOOL_OS    := $(shell uname -s | tr '[:upper:]' '[:lower:]')
PROMTOOL_ARCH  := $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
# Trivy is the binary of the analyser's image TRIVY_IMAGE (tools/versions.env, R-HA-070): `make trivy` takes it out of that image, so
# the container scan runs the scanner's own analyser. Its directory carries the image's tag and the start of its index digest, so a new
# digest of the same version is taken out again; the container scan job puts the directory on PATH (make trivy-dir).
TRIVY_TAG      := $(lastword $(subst :, ,$(firstword $(subst @, ,$(TRIVY_IMAGE)))))
TRIVY_DIGEST   := $(patsubst sha256:%,%,$(word 2,$(subst @, ,$(TRIVY_IMAGE))))
TRIVY_DIR      := .bin/trivy-$(TRIVY_TAG)-$(shell printf '%.12s' '$(TRIVY_DIGEST)')
TRIVY_BIN      := $(TRIVY_DIR)/trivy

# The tag of every image this checkout builds for the profiles: `dev` in the main checkout, `dev-<worktree>` in a git worktree
# (deploy/dev/dev-tag.sh, QUADER_DEV_TAG overrides), so two checkouts never run each other's image (F-059). Read once, when a
# recipe or a default needs it: a plain `make` of another target runs no git.
DEV_TAG = $(eval DEV_TAG := $(shell deploy/dev/dev-tag.sh))$(DEV_TAG)
IMAGE       ?= guidedtraffic/quader:$(DEV_TAG)
# The image of the working tree with the second UI bundle (make image-marked): the upgrade test across two bundles
# installs it and upgrades to IMAGE (R-HA-020, Q-HA-124).
IMAGE_MARKED ?= guidedtraffic/quader:$(DEV_TAG)-marked
UI_BUNDLE_MARK ?=
IMAGE_ARCH  ?= $(shell go env GOARCH)
# Runs one fetch with a wall-clock bound per attempt and three attempts, and names it when the last fails
# (.github/scripts/fetch.sh). The test upstreams' images build on RUNTIME_BASE_IMAGE alone, so a bound of their whole build is a
# bound of that pull: one stalled registry costs minutes, not a leg's limit (2026-11-23).
FETCH := .github/scripts/fetch.sh
# Every image of the end-to-end sets that this repository builds (the e2e driver, the test upstreams, the clients images) carries this
# label, as make image's carry theirs: a build that takes the tag of an earlier one leaves that one untagged, and the prune after
# each build removes the untagged images with the label and nothing else (the Docker VM of the development machine was full on
# 2026-11-12). deploy/dev/e2e.sh and test/e2e/*/build.sh use the same label.
E2E_IMAGE_BUILD_LABEL := quader.build=e2e-image

# The integration stack belongs to the checkout that started it; a worktree uses the main checkout's (the parent of git's common
# directory), as test/integration/locked.sh does. INSTANCE picks one of the instances that run side by side (1 to 9,
# test/integration/instance.sh): instance 1 is .dev/integration, the pipeline's, instance n .dev/integration-<n>. It defaults to
# QUADER_TEST_INSTANCE, which every env file carries and locked.sh and run.sh export for the instance they took, and then to 1.
INSTANCE ?= $(or $(QUADER_TEST_INSTANCE),1)
INTEGRATION_ENV := $(abspath $(dir $(abspath $(shell git rev-parse --git-common-dir 2>/dev/null || echo .git))))/.dev/integration$(if $(filter-out 1,$(INSTANCE)),-$(INSTANCE))/env
COVERAGE_DIR := .out/coverage

# Cyclomatic complexity: no function of the module's own code above CYCLO_THRESHOLD. Tests and
# the generated code (sqlc in internal/meta/postgres/q, oapi-codegen in internal/api/v1/gen) are
# not the team's to simplify. The threshold is a ratchet, as in cogpit: it is the complexity of the
# most complex function today (config's validateEndpoints, 33), and a change that simplifies that
# function lowers it to the new maximum in the same commit. The 15 of valkey-operator is the goal:
# 40 functions are above it today (docs/developer/releasing.md, Cyclomatic complexity).
CYCLO_THRESHOLD ?= 33
CYCLO_IGNORE := _test\.go|/q/|/gen/|node_modules

# Go packages of the module. npm packages in web/node_modules and root node_modules can ship Go
# files; the `ignore` directive in go.mod keeps them out of `./...`.
PKGS        = ./...

.DEFAULT_GOAL := help

.PHONY: help
help: ## List the targets
	@awk 'BEGIN{FS=":.*## "} /^[a-zA-Z0-9_.-]+:.*## /{printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

##@ Build

.PHONY: build
build: ## Build the static binary into bin/quader (CGO_ENABLED=0, -trimpath, version via ldflags)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/quader ./cmd/quader

.PHONY: web-ci
web-ci: ## npm ci in web/ (Node from web/.nvmrc; fails when package.json and the lockfile disagree)
	cd web && npm ci

.PHONY: web-lint
web-lint: ## ESLint and the R-UI-046 rules in web/ (after web-ci)
	cd web && npm run lint

.PHONY: web-test
web-test: ## Angular unit tests and the R-UI-046 lint-rule tests in web/ (after web-ci); .out/test-web.xml and .out/test-web-lint.xml (JUnit) for the coverage generator
	cd web && npm test && npm run test:lint

.PHONY: web-i18n
web-i18n: ## Extract the translation source into web/src/locale/messages.xlf and bring messages.de.xlf to its units and location notes (after web-ci; the last step of a UI change)
	cd web && node scripts/i18n-check.mjs --write

.PHONY: web-i18n-check
web-i18n-check: ## Fail when the translation source differs from a fresh extraction or the German file has a missing, stale or moved unit (after web-ci; CI: web)
	cd web && node scripts/i18n-check.mjs

.PHONY: web-embed
web-embed: ## Build the two Angular applications and embed them into internal/webui/dist and internal/bastion/gui/dist (after web-ci)
	cd web && npm run build:embed

.PHONY: web
web: web-ci web-embed ## web-ci, then web-embed (what a local build of the UI needs)

# UI development profile (docs/developer/web-ui.md#local-ui-development): its own containers, volumes and
# ports, so it runs next to the integration stack and the kind cluster without touching either. UI_INSTANCE=2..8 picks another
# instance beside instance 1, one per builder (deploy/ui-dev/instance.sh); a worktree claims a free one when it names none.
.PHONY: ui-dev-up
ui-dev-up: ## PostgreSQL, Valkey and SeaweedFS (TLS) for UI work; writes .dev/ui-dev (deploy/ui-dev/up.sh)
	deploy/ui-dev/up.sh

.PHONY: ui-dev-backend
ui-dev-backend: ## Write .dev/ui-dev/quader.yaml again, build quader, migrate, run one replica of role all on https://127.0.0.1:19080 in the foreground (deploy/ui-dev/serve.sh)
	deploy/ui-dev/serve.sh

.PHONY: ui-dev-web
ui-dev-web: ## Angular dev server with hot reload on https://localhost:4200/ui/, everything else proxied to ui-dev-backend (after web-ci; deploy/ui-dev/web.sh)
	deploy/ui-dev/web.sh

.PHONY: ui-dev-bastion
ui-dev-bastion: ## The profile's Bastion beside ui-dev-backend: registered as ui-dev with its web interface and docs on, on https://localhost:19091/ui/ in instance 1, in the foreground (UI_BASTION_BUILD=0 keeps the last build; deploy/ui-dev/bastion.sh)
	deploy/ui-dev/bastion.sh

.PHONY: ui-dev-down
ui-dev-down: ## Stop the UI development services; PURGE=1 also removes their data and .dev/ui-dev (deploy/ui-dev/down.sh)
	deploy/ui-dev/down.sh $(if $(PURGE),--purge)

# Rule 11 of docs/projektplan/promt.md: a UI change is rendered and looked at before it lands. Test and Release runs the same
# targets on every push (Q-ARCH-510): ui-layout-test and ui-states in the job ui-layout-fixtures, ui-renders SHARD=i/n in each leg of
# the job ui-layout (Q-ARCH-1071).
.PHONY: ui-layout-test
ui-layout-test: ## The layout checks of the renders on fixture pages in Chromium, Firefox and WebKit: no stack, seconds (after web-ci and `npx playwright install chromium firefox webkit`; web/e2e/playwright.layout.config.ts)
	cd web && npx playwright test -c e2e/playwright.layout.config.ts

.PHONY: ui-states
ui-states: ## Every dialog, its failure and its validation, the confirm shapes, the token's secret and tooltips at the edge, measured by the layout checks in headless Chromium at 390 and 1440 px: no stack, seconds (after web-ci and `npx playwright install chromium`; UI_STATES_PICTURES=1 writes .out/ui-states; docs/developer/web-ui.md#dialog-states)
	cd web && npx ng run web:states --no-watch

.PHONY: ui-renders
ui-renders: ## Render every screen as every kind of user, light and dark, at 1440, 1024 and 390 px into .out/ui-renders (after web-ci; GREP=<regex> limits the screens; SHARD=i/n renders the i-th of n parts, as a leg of CI does; deploy/ui-dev/renders.sh)
	$(if $(SHARD),UI_RENDERS_SHARD='$(SHARD)') deploy/ui-dev/renders.sh $(if $(GREP),--grep '$(GREP)')

.PHONY: ui-renders-parts-check
ui-renders-parts-check: ## The parts of ui-renders SHARD=i/n hold every screen of the whole pass exactly once, from Playwright's lists: seconds, nothing rendered, no stack (after web-ci; PARTS=n, default 5; deploy/ui-dev/renders-parts-check.sh)
	deploy/ui-dev/renders-parts-check.sh $(PARTS)

# The release image builds the Angular bundle itself (Containerfile, stage web); no Node.js is
# needed on the host. The build arguments read the same pins as the rest of the repository.
# Every image make image builds carries this label (the release pipeline, build.yml, builds without it). A build that takes the
# tag of an earlier one leaves that one untagged; the prune after the build removes the untagged images that carry the label and
# no other image of the machine, and none a container still uses. A prune that fails is reported and does not fail the build.
IMAGE_BUILD_LABEL := quader.build=make-image

.PHONY: image
image: ## Build the release image (IMAGE, VERSION, IMAGE_ARCH) for linux/IMAGE_ARCH with the Containerfile, loaded into the local Docker; removes the untagged images earlier builds of this target left
	docker buildx build --load --platform linux/$(IMAGE_ARCH) \
		--build-arg NODE_VERSION=$(NODE_VERSION) \
		--build-arg GO_VERSION=$(GO_MOD_GO) \
		--build-arg RUNTIME_BASE_IMAGE=$(RUNTIME_BASE_IMAGE) \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		--build-arg UI_BUNDLE_MARK=$(UI_BUNDLE_MARK) \
		--label $(IMAGE_BUILD_LABEL) \
		-f Containerfile -t $(IMAGE) .
	-docker image prune --force --filter dangling=true --filter label=$(IMAGE_BUILD_LABEL)

.PHONY: image-marked
image-marked: ## Build IMAGE_MARKED: make image with the second UI bundle (UI_BUNDLE_MARK=1), the same code under other hashed names, for the upgrade test across two bundles
	$(MAKE) image IMAGE=$(IMAGE_MARKED) UI_BUNDLE_MARK=1

##@ Code generation

.PHONY: generate
generate: ## Run every go:generate directive (config schemas and the Bastion chart files derived from them, sqlc, oapi-codegen)
	go generate $(PKGS)

.PHONY: shipped-anchors
shipped-anchors: ## Refresh the release's snapshot of the public Sigstore TUF repository (trusted_root.json, npm's keys, the TUF metadata) into internal/signatures/shipped/data (R-SIG-017; needs the network; commit the result)
	go run ./internal/tools/shippedanchors -out internal/signatures/shipped/data

.PHONY: generate-check
generate-check: generate ## Generate, then fail when the tree differs from the committed files (CI: E0.1)
	git diff --exit-code
	@untracked="$$(git ls-files --others --exclude-standard | grep -E '(^|/)(gen|q)/|schema\.json$$|chart-managed-keys\.yaml$$' || true)"; \
	  if [ -n "$$untracked" ]; then echo "generated files are not committed:"; echo "$$untracked"; exit 1; fi

##@ Quality

$(LINT_BIN):
	@mkdir -p .bin
	@tmp="$$(mktemp -d)"; \
	  GOBIN="$$tmp" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) && \
	  mv "$$tmp/golangci-lint" $@ && rmdir "$$tmp"

$(GOVULNCHECK_BIN):
	@mkdir -p .bin
	@tmp="$$(mktemp -d)"; \
	  GOBIN="$$tmp" go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) && \
	  mv "$$tmp/govulncheck" $@ && rmdir "$$tmp"

$(GOCYCLO_BIN):
	@mkdir -p .bin
	@tmp="$$(mktemp -d)"; \
	  GOBIN="$$tmp" go install github.com/fzipp/gocyclo/cmd/gocyclo@$(GOCYCLO_VERSION) && \
	  mv "$$tmp/gocyclo" $@ && rmdir "$$tmp"

# The one time limit of golangci-lint (make lint, make gosec); .golangci.yml sets none, and golangci-lint v2 has none by default.
# On the CI runner make lint took 2.2 to 2.6 minutes (Test and Release 45113997417 to 45133371730); on a development machine at
# a load of 70 it passed the former limit of ten minutes. 20 minutes stays below the 25 of the lint job, so a run that hangs there
# ends with golangci-lint's own message and not the runner's.
LINT_TIMEOUT ?= 20m
# golangci-lint holds a lock in the system's temporary directory. Without --allow-serial-runners a second run on the machine (another
# worktree, make gosec beside make lint) ended at once with "parallel golangci-lint is running"; with it, the second waits for the
# first, so the machine runs one lint at a time (the lock stays: four builders' gates at once took it above a load of 100). LINT_WAIT
# says that it waits.
# LINT_CACHE is the checkout's own cache of golangci-lint, keyed by its path and gone with it: a run in one worktree reported a finding
# in a file of another worktree (W36-P8, 2026-11-27) and a rerun with a cache of its own did not; golangci-lint's default cache is one
# directory for every checkout of the machine (docs/developer/build-and-test.md, "The lint's cache").
LINT_CACHE ?= $(CURDIR)/.out/golangci-lint-cache
LINT_ENV = GOLANGCI_LINT_CACHE=$(LINT_CACHE)
LINT_FLAGS = --timeout $(LINT_TIMEOUT) --allow-serial-runners $(LINT_CONCURRENCY)
LINT_WAIT = if pgrep -f '[g]olangci-lint[^ ]* run( |$$)' >/dev/null 2>&1; then \
  echo "make: another golangci-lint runs on this machine; this one waits for the lock while that run holds it"; fi

.PHONY: lint
lint: $(LINT_BIN) ## golangci-lint (pinned, rebuilt when the pin or the Go toolchain changes; gosec among its linters; LINT_TIMEOUT, waits for another run) and go vet
	@$(LINT_WAIT)
	$(LINT_ENV) $(LINT_BIN) run $(LINT_FLAGS) ./...
	go vet $(PKGS)

.PHONY: cores
cores: ## Print the cap of the cores a local gate runs with (LOCAL_CORES; LOCAL_CORES= lifts it): GOMAXPROCS, GOFLAGS and the lint's concurrency
	@echo "GOMAXPROCS=$${GOMAXPROCS:-} GOFLAGS=$${GOFLAGS:-} lint=$(strip $(LINT_CONCURRENCY))"

.PHONY: fmt
fmt: $(LINT_BIN) ## Format Go code with the formatters of .golangci.yml (gofumpt, goimports)
	$(LINT_ENV) $(LINT_BIN) fmt ./...

# gosec runs through golangci-lint, at the pinned golangci-lint version, with the gosec settings of
# .golangci.yml: the accepted findings carry their reason there or at the line (//nolint:gosec). One
# gosec version, the one golangci-lint bundles, so the repository has no second copy to disagree.
.PHONY: gosec
gosec: $(LINT_BIN) ## gosec over the module through the pinned golangci-lint (CI: GoSec Security Scan; LINT_TIMEOUT, waits for another run)
	@$(LINT_WAIT)
	$(LINT_ENV) $(LINT_BIN) run $(LINT_FLAGS) --enable-only=gosec ./...

.PHONY: vuln
vuln: $(GOVULNCHECK_BIN) ## govulncheck (pinned) over the module (CI: govulncheck)
	GOFLAGS="-buildvcs=false" $(GOVULNCHECK_BIN) ./...

.PHONY: cyclo
cyclo: $(GOCYCLO_BIN) ## gocyclo (pinned): fails on any function above CYCLO_THRESHOLD (CI: cyclomatic-complexity)
	@over="$$($(GOCYCLO_BIN) -over $(CYCLO_THRESHOLD) -ignore '$(CYCLO_IGNORE)' .)"; \
	if [ -n "$$over" ]; then \
	  echo "$$over"; \
	  echo "functions above the complexity threshold $(CYCLO_THRESHOLD) (CYCLO_THRESHOLD in the Makefile)"; \
	  exit 1; \
	fi; \
	echo "every function is at or below the complexity threshold $(CYCLO_THRESHOLD)"

.PHONY: cyclo-report
cyclo-report: $(GOCYCLO_BIN) ## The 20 most complex functions, tests included
	@$(GOCYCLO_BIN) -top 20 .

# ClamAV over the source tree with the product's ClamAV (R-MAL-031): freshclam and clamscan of CLAMAV_IMAGE, one version of the tool
# for the product and this scan. freshclam keeps the database in .out/clamav-db, so a second run fetches the day's differences only
# (CI keeps that directory in a cache whose key changes once a day; ClamAV's network limits whole downloads per address). The tree
# is mounted read-only; the build outputs (.out, .bin, .dev, bin) and node_modules are not source. The report is kept in .out.
CLAMAV_DB_DIR := .out/clamav-db

.PHONY: malware-scan-db
malware-scan-db: ## freshclam of CLAMAV_IMAGE into .out/clamav-db: the whole database once, then the day's differences (CI: malware-scan; needs docker)
	@mkdir -p $(CLAMAV_DB_DIR)
	docker run --rm --user "$$(id -u):$$(id -g)" --entrypoint /bin/sh -v "$(CURDIR)/$(CLAMAV_DB_DIR):/var/lib/clamav" $(CLAMAV_IMAGE) -c \
	  'printf "DatabaseDirectory /var/lib/clamav\nDatabaseMirror database.clamav.net\nScriptedUpdates yes\n" > /tmp/freshclam.conf && freshclam --config-file=/tmp/freshclam.conf --stdout'

.PHONY: malware-scan
malware-scan: malware-scan-db ## clamscan of CLAMAV_IMAGE over the source tree; fails on any infected file or when the scan does not finish (CI: malware-scan; needs docker)
	@mkdir -p .out
	@set -o pipefail; docker run --rm --user "$$(id -u):$$(id -g)" --entrypoint clamscan -v "$(CURDIR):/src:ro" \
	  -v "$(CURDIR)/$(CLAMAV_DB_DIR):/var/lib/clamav:ro" -w /src $(CLAMAV_IMAGE) --database=/var/lib/clamav --recursive --infected \
	  --exclude-dir='(^|/)(\.git|node_modules|\.out|\.bin|\.dev|bin)$$' /src | tee .out/clamav-report.txt
	@grep -q 'Infected files: 0' .out/clamav-report.txt || { echo "malware scan found files or did not finish: see .out/clamav-report.txt"; exit 1; }
	@echo "no malware in the source tree"

.PHONY: test-release-tooling
test-release-tooling: ## Release tooling: npm ci, npm audit signatures, and the release notes of .releaserc.json rendered (CI: release-tooling; needs node and npm)
	npm ci --no-fund
	npm audit signatures
	node hack/verify-release-tooling.mjs

# R-ARCH-025 and R-ARCH-026 against the latest v0.* release, and R-ARCH-026 against the history of main (the compat
# job of release.yml). The checker is built
# at its pin into .bin, and the name carries the pin and the Go toolchain, as the linters' names do. The tests need
# the release tags in the clone: fetch-depth 0 and fetch-tags.
$(OASDIFF_BIN):
	@mkdir -p .bin
	@tmp="$$(mktemp -d)"; \
	  GOBIN="$$tmp" go install github.com/oasdiff/oasdiff@$(OASDIFF_VERSION) && \
	  mv "$$tmp/oasdiff" $@ && rmdir "$$tmp"

.PHONY: compat-check
compat-check: $(OASDIFF_BIN) ## R-ARCH-025 (OpenAPI breaking changes), R-ARCH-026 (migrations only added since the latest v0.* release; no migration of main changed) (CI: compat; needs the tags and the history)
	QUADER_OASDIFF=$(abspath $(OASDIFF_BIN)) go test -tags compat -count=1 -v ./test/compat/...

# One version per tool (E0.2): every pin of a tool that is installed on PATH is compared with the
# pin, so a copy of another version can never be the one a check ran with.
.PHONY: versions-check
versions-check: ## One version per tool (E0.2): go, Node, sqlc, oapi-codegen, the linters, the CI tools and the npm and NuGet client pins against their pins
	@set -e; fail=0; \
	have="$$(go env GOVERSION | sed 's/^go//')"; \
	if [ "$$have" != "$(GO_MOD_GO)" ]; then echo "go: running $$have, go.mod pins $(GO_MOD_GO)"; fail=1; fi; \
	have="$$(node --version | sed 's/^v//')"; \
	if [ "$$have" != "$(NODE_VERSION)" ]; then echo "node: running $$have, web/.nvmrc pins $(NODE_VERSION)"; fail=1; fi; \
	want="$$(go list -m -f '{{.Version}}' github.com/sqlc-dev/sqlc)"; have="$$(go tool sqlc version)"; \
	if [ "$$have" != "$$want" ]; then echo "sqlc: running $$have, go.mod pins $$want"; fail=1; fi; \
	want="$$(go list -m -f '{{.Version}}' github.com/oapi-codegen/oapi-codegen/v2)"; have="$$(go tool oapi-codegen --version | tail -n 1)"; \
	if [ "$$have" != "$$want" ]; then echo "oapi-codegen: running $$have, go.mod pins $$want"; fail=1; fi; \
	if [ -x "$(LINT_BIN)" ]; then \
	  have="$$($(LINT_BIN) --version | sed -n 's/.*version \([^ ]*\) built with \(go[^ ]*\).*/\1 \2/p')"; \
	  if [ "$$have" != "$(GOLANGCI_LINT_VERSION:v%=%) $(GOVERSION)" ]; then echo "golangci-lint: running '$$have', pins are $(GOLANGCI_LINT_VERSION) built with $(GOVERSION)"; fail=1; fi; \
	fi; \
	if [ -x "$(OASDIFF_BIN)" ]; then have="$$(go version -m $(OASDIFF_BIN) | awk '$$1=="mod" && $$2=="github.com/oasdiff/oasdiff" {print $$3}')"; \
	  if [ "$$have" != "$(OASDIFF_VERSION)" ]; then echo "oasdiff: built at '$$have', pin is $(OASDIFF_VERSION)"; fail=1; fi; fi; \
	if command -v kind >/dev/null; then have="$$(kind version | awk '{print $$2}')"; \
	  if [ "$$have" != "$(KIND_VERSION)" ]; then echo "kind: installed $$have, pin is $(KIND_VERSION)"; fail=1; fi; fi; \
	if command -v kubectl >/dev/null; then have="$$(kubectl version --client -o json | sed -n 's/.*"gitVersion": "\([^"]*\)".*/\1/p' | head -n 1)"; \
	  if [ "$$have" != "$(KUBECTL_VERSION)" ]; then echo "kubectl: installed $$have, pin is $(KUBECTL_VERSION)"; fail=1; fi; fi; \
	if command -v helm >/dev/null; then have="$$(helm version --template '{{.Version}}')"; \
	  if [ "$$have" != "$(HELM_VERSION)" ]; then echo "helm: installed $$have, pin is $(HELM_VERSION)"; fail=1; fi; fi; \
	if [ -x "$(PROMTOOL_BIN)" ]; then have="$$($(PROMTOOL_BIN) --version | sed -n 's/^promtool, version \([^ ]*\) .*/v\1/p')"; \
	  if [ "$$have" != "$(PROMTOOL_VERSION)" ]; then echo "promtool: $(PROMTOOL_BIN) is $$have, pin is $(PROMTOOL_VERSION)"; fail=1; fi; fi; \
	if command -v promtool >/dev/null; then have="$$(promtool --version | sed -n 's/^promtool, version \([^ ]*\) .*/v\1/p')"; \
	  if [ "$$have" != "$(PROMTOOL_VERSION)" ]; then echo "promtool: installed $$have, pin is $(PROMTOOL_VERSION)"; fail=1; fi; fi; \
	if ! printf '%s' '$(TRIVY_IMAGE)' | grep -Eq '^[a-z0-9][a-z0-9./_-]*:[A-Za-z0-9._-]+@sha256:[a-f0-9]{64}$$'; then \
	  echo "trivy: TRIVY_IMAGE '$(TRIVY_IMAGE)' is not <repository>:<tag>@sha256:<digest>"; fail=1; fi; \
	if grep -Eq '^TRIVY_(VERSION|SHA256_[A-Z0-9_]*)=' tools/versions.env; then echo "trivy: tools/versions.env pins the release archive beside TRIVY_IMAGE"; fail=1; fi; \
	if grep -Eiq '^FROM[[:space:]].*[[:space:]]AS[[:space:]]+trivy([[:space:]]|$$)|^ARG[[:space:]]+TRIVY_' Containerfile; then echo "trivy: the Containerfile builds the analyser or takes a pin of it, which only TRIVY_IMAGE carries"; fail=1; fi; \
	if [ "$(PROMTOOL_OS)" = linux ] && [ -x "$(TRIVY_BIN)" ]; then have="$$($(TRIVY_BIN) --version | sed -n 's/^Version: \(.*\)/\1/p')"; \
	  if [ "$$have" != "$(TRIVY_TAG)" ]; then echo "trivy: $(TRIVY_BIN) is $$have, TRIVY_IMAGE is $(TRIVY_TAG)"; fail=1; fi; fi; \
	if command -v trivy >/dev/null; then have="$$(trivy --version | sed -n 's/^Version: \(.*\)/\1/p')"; \
	  if [ "$$have" != "$(TRIVY_TAG)" ]; then echo "trivy: $$(command -v trivy) is $$have, TRIVY_IMAGE is $(TRIVY_TAG)"; fail=1; fi; fi; \
	if command -v k6 >/dev/null; then have="$$(k6 version | sed -n 's/^k6 v\([^ ]*\).*/v\1/p')"; \
	  if [ "$$have" != "$(K6_VERSION)" ]; then echo "k6: installed $$have, pin is $(K6_VERSION)"; fail=1; fi; fi; \
	if docker buildx version >/dev/null 2>&1; then have="$$(docker buildx version | awk '{print $$2}')"; \
	  if [ "$$have" != "$(BUILDX_VERSION)" ]; then echo "docker buildx: installed $$have, pin is $(BUILDX_VERSION)"; fail=1; fi; fi; \
	tools/sbom-generator/check-pins.sh || fail=1; \
	node test/e2e/clients/check-pins.mjs "$(NODE_VERSION)" "$(E2E_CLIENTS_NODE_IMAGE)" "$(E2E_CLIENTS_CURL_VERSION)" "$(E2E_CLIENTS_JQ_VERSION)" || fail=1; \
	node test/e2e/nugetclients/check-pins.mjs "$(E2E_NUGETCLIENTS_SDK_IMAGE)" "$(NUGETVECTORS_SDK_IMAGE)" "$(E2E_NUGETCLIENTS_MONO_VERSION)" "$(E2E_NUGETCLIENTS_NUGET_EXE_VERSION)" "$(E2E_NUGETCLIENTS_NUGET_EXE_SHA256)" "$(E2E_NUGETCLIENTS_JQ_VERSION)" "$(E2E_NUGETCLIENTS_SDK8_IMAGE)" "$(E2E_NUGETCLIENTS_SDK9_IMAGE)" || fail=1; \
	node test/e2e/ociclients/check-pins.mjs "$(E2E_OCICLIENTS_DOCKER_IMAGE)" "$(E2E_OCICLIENTS_PODMAN_IMAGE)" "$(E2E_OCICLIENTS_COSIGN_IMAGE)" || fail=1; \
	node test/e2e/aptclients/check-pins.mjs "$(E2E_APTCLIENTS_DEBIAN13_IMAGE)" "$(E2E_APTCLIENTS_DEBIAN12_IMAGE)" "$(E2E_APTCLIENTS_UBUNTU2404_IMAGE)" "$(E2E_APTCLIENTS_UBUNTU2204_IMAGE)" || fail=1; \
	node test/e2e/ansibleclients/check-pins.mjs "$(E2E_ANSIBLECLIENTS_CORE217_IMAGE)" "$(E2E_ANSIBLECLIENTS_CORE217_VERSION)" "$(E2E_ANSIBLECLIENTS_CORE221_IMAGE)" "$(E2E_ANSIBLECLIENTS_CORE221_VERSION)" || fail=1; \
	if [ $$fail -ne 0 ]; then exit 1; fi; \
	echo "versions ok: go $$(go env GOVERSION), node $(NODE_VERSION), sqlc, oapi-codegen match their pins"

##@ Tests

# The unit tests run under the race detector and write two things for the run's reports:
# .out/test-unit.json (go test -json, the requirement IDs of the test names) and the coverage
# profile of the unit tier (merged by `make coverage-report`).
# The unit tier runs without the host's AWS configuration, as on the CI runner: a region or a credential from
# ~/.aws or the shell made TestBackupCommands pass locally and fail in Test and Release run 42387599073.
UNIT_ENV := env -u AWS_REGION -u AWS_DEFAULT_REGION -u AWS_PROFILE -u AWS_ACCESS_KEY_ID -u AWS_SECRET_ACCESS_KEY \
  -u AWS_SESSION_TOKEN AWS_CONFIG_FILE=/nonexistent AWS_SHARED_CREDENTIALS_FILE=/nonexistent AWS_EC2_METADATA_DISABLED=true
# The time one unit test binary may take under the race detector and coverage. Go's default of 10 minutes ended
# internal/api/v1, the slowest package by far: 476 to 487 s in the unit job in the morning of 2026-11-21, 585 s after the
# signatures' second package, over 600 s with the image analyser's move (Test and Release 44930739304); the next slowest
# (formattest, archtest, app) take 100 to 160 s. 20 minutes is twice what that package took in the morning and stays below
# the job's own limit of 30, whose whole run took 11.9 to 14.1 minutes that day: setup and the build take about 3 minutes
# before the first package ends and the kit's plain run and the uploads about 1 minute after the last, so a package that
# hangs is ended by this bound with its goroutines printed, not by the runner without them.
UNIT_TIMEOUT ?= 20m

.PHONY: test
test: ## Unit tests with the race detector, without the host's AWS configuration; .out/test-unit.json for the coverage generator, .out/coverage/unit.out for the report
	@mkdir -p .out $(COVERAGE_DIR)
	@set -o pipefail; $(UNIT_ENV) go test -race -count=1 -timeout $(UNIT_TIMEOUT) -covermode=atomic -coverpkg=./... \
	  -coverprofile=$(COVERAGE_DIR)/unit.out -json $(PKGS) | tee .out/test-unit.json | $(TESTLOG)
	@# The time bounds of the conformance kit (R-ARCH-028: a crafted input is rejected within 5 s) hold for the code as it
	@# ships: the race run above stretches them, this plain run of the kit holds them as specified.
	$(UNIT_ENV) go test -count=1 -run 'TestR_ARCH_010' ./internal/formats/...

# The unit run before a push: the packages of `make test` with the environment a runner has (UNIT_ENV), without the race
# detector and the coverage, so it takes minutes, not the unit job's quarter of an hour. A plain `go test` reads the host's
# AWS configuration and can pass where CI fails (TestBackupCommands, run 42387599073; the offline tool's tests, 7dd5646b3).
.PHONY: unit
unit: ## The unit tests as a runner runs them, without the race detector and the coverage (before a push): PKGS, UNIT_ENV, UNIT_TIMEOUT
	$(UNIT_ENV) go test -count=1 -timeout $(UNIT_TIMEOUT) $(PKGS)

.PHONY: integration-up
integration-up: ## Start PostgreSQL, PgBouncer, Valkey with Sentinel (TLS) and SeaweedFS; write .dev/integration/env (INSTANCE=2..9: another instance beside it, .dev/integration-<n>/env)
	test/integration/up.sh $(INSTANCE)

.PHONY: integration-down
integration-down: ## Stop the integration stack and remove its volumes (INSTANCE=2..9: that instance)
	test/integration/down.sh $(INSTANCE)

# One run of integration tests on whichever instance of the stack is free (test/integration/run.sh): it holds that instance's lock
# for the run, from the main checkout or a worktree. QUADER_TEST_INSTANCE=<n> pins an instance.
.PHONY: integration-run
integration-run: ## go test -count=1 -p 1 -tags integration -timeout 30m ARGS on a free instance of the stack, under its lock (test/integration/run.sh), e.g. ARGS=./test/integration/smoke/
	@test -n "$(ARGS)" || { echo "ARGS is empty: name the packages and flags, e.g. make integration-run ARGS=./test/integration/smoke/"; exit 2; }
	test/integration/run.sh $(ARGS)

# SHARD=a|b|c|d splits the packages over the four CI jobs, each with its own stack, by a rule on the area's name that places a new
# package without anyone editing it (Q-ARCH-544, Q-ARCH-565). A format's areas are <format>local, <format>remote and
# <format>virtual (as Ansible's, Debian's and OCI's), and the suffix decides first: every area that ends in local goes to a, in
# remote to c, in virtual to d (the platform area virtual among them), so a new format spreads over three shards instead of filling
# one. The other areas by name: a takes coordination, mirror, proxy and the single packages of npm and NuGet; c the area api, the
# stores and their recovery (backup*, restore*, storage*, postgres, blob, valkey) and the Bastion's areas (bastion*); d the security
# checks (ci, the exchange of CI tokens, and vuln*, malware*, sign*); b every other area, every new platform area among them. From
# the package times of Test and Release 44948103795 (docs/developer/test-cost.md, section 8) the jobs take about 32, 31, 33 and 31
# minutes of their 45 (a, b, c, d; c runs the OCI conformance suite as well); with three shards they took 38, 43 and 42 (a, b, c),
# and b, which every new area joined, stood six seconds under the 43.5 minutes at which a shard is cut. internal/tools/ciworkflows
# checks that every package runs in exactly one shard of the workflow's matrix. Unset runs all.
# The list is read when a recipe needs it, once (the eval replaces the variable by its value): a `make` of any other target runs
# no `go list`, which in a job without the module cache downloaded every module of the graph (the gating jobs image,
# container-scan, malware-scan and release-tooling did, seen by the builder of W6 on 2026-11-23). The shards are recursive for the
# same reason.
INTEGRATION_ALL = $(eval INTEGRATION_ALL := $(shell go list -tags integration ./test/integration/...))$(INTEGRATION_ALL)
INTEGRATION_DIR := $(MODULE)/test/integration/
INTEGRATION_PLATFORM = $(filter-out $(addprefix $(INTEGRATION_DIR),%local %remote %virtual),$(INTEGRATION_ALL))
INTEGRATION_SHARD_A = $(filter $(INTEGRATION_DIR)%local,$(INTEGRATION_ALL)) \
  $(filter $(addprefix $(INTEGRATION_DIR),coordination mirror npm nuget proxy),$(INTEGRATION_PLATFORM))
INTEGRATION_SHARD_C = $(filter $(INTEGRATION_DIR)%remote,$(INTEGRATION_ALL)) \
  $(filter-out $(INTEGRATION_SHARD_A),$(filter $(addprefix $(INTEGRATION_DIR),api backup% restore% storage% postgres blob valkey bastion%),$(INTEGRATION_PLATFORM)))
INTEGRATION_SHARD_D = $(filter $(INTEGRATION_DIR)%virtual,$(INTEGRATION_ALL)) \
  $(filter-out $(INTEGRATION_SHARD_A) $(INTEGRATION_SHARD_C),$(filter $(addprefix $(INTEGRATION_DIR),ci vuln% malware% sign%),$(INTEGRATION_PLATFORM)))
INTEGRATION_SHARD_B = $(filter-out $(INTEGRATION_SHARD_A) $(INTEGRATION_SHARD_C) $(INTEGRATION_SHARD_D),$(INTEGRATION_ALL))
# The time one integration package may take. The default of 10 minutes is the sum of the area `backup` alone: about 235 tests that run one
# after the other in one process (8 minutes on an idle host, none of them longer than 45 s and none waiting on a fixed sleep a load could
# stretch), and coverage instrumentation and a loaded host add to it. A package that hangs is still ended, by this bound.
INTEGRATION_TIMEOUT ?= 30m

ifeq ($(SHARD),)
INTEGRATION_PKGS = ./test/integration/...
else ifeq ($(SHARD),a)
INTEGRATION_PKGS = $(INTEGRATION_SHARD_A)
else ifeq ($(SHARD),c)
INTEGRATION_PKGS = $(INTEGRATION_SHARD_C)
else ifeq ($(SHARD),b)
INTEGRATION_PKGS = $(INTEGRATION_SHARD_B)
else ifeq ($(SHARD),d)
INTEGRATION_PKGS = $(INTEGRATION_SHARD_D)
else ifneq ($(findstring /,$(SHARD)),)
# SHARD=i/n is a part of the screens of make ui-renders: a recipe that reads the packages with it stops there.
INTEGRATION_PKGS = $(error SHARD=$(SHARD) is a part of the screens of make ui-renders; the integration packages take a, b, c or d)
else
$(error SHARD must be a, b, c, d, a part i/n of make ui-renders, or unset, not $(SHARD))
endif

# The coverage profile of the line is named by the Valkey line it ran against (and the shard), so the
# profiles of the matrix merge into one report. -p 1 serialises the build too, so the test binaries are
# compiled in parallel first and the run takes them from the build cache.
.PHONY: test-integration
test-integration: ## Integration tests against the running stack (make integration-up; INSTANCE=<n> another instance, without its lock: locked.sh make test-integration takes a free one), SHARD=a|b|c|d for one quarter; .out/test-integration[-<shard>].json and .out/coverage/integration-<line>[-<shard>].out
	@test -f $(INTEGRATION_ENV) || { echo "$(INTEGRATION_ENV) is missing: run 'make integration-up INSTANCE=$(INSTANCE)' first"; exit 1; }
	@mkdir -p .out $(COVERAGE_DIR)
	go test -c -o .out/integration-bin/ -tags integration -covermode=atomic -coverpkg=./... $(INTEGRATION_PKGS) >/dev/null
	@set -o pipefail; set -a; . $(INTEGRATION_ENV); set +a; \
	  go test -count=1 -p 1 -timeout $(INTEGRATION_TIMEOUT) -tags integration -covermode=atomic -coverpkg=./... \
	    -coverprofile=$(COVERAGE_DIR)/integration-$${QUADER_TEST_VALKEY_LINE:-9.1}$(if $(SHARD),-$(SHARD)).out \
	    -json $(INTEGRATION_PKGS) | tee .out/test-integration$(if $(SHARD),-$(SHARD)).json | $(TESTLOG)

# The latency bounds of P-13 and P-14 with Valkey, tail included (Q-ARCH-220), and the p99 of one decision with 1,000
# exposure rules (R-EP-013, Q-ARCH-250): the per-change tier asserts delivery, the p50 and the allocations; the p99
# bound is enforced here, by the nightly Performance workflow.
.PHONY: test-performance
test-performance: ## Latency targets of the per-change tier enforced (P-13, P-14, R-NPM-010, R-DEB-010, R-API-014, R-API-055, R-EP-013, R-ARCH-033) (make integration-up); .out/test-performance.json
	@test -f $(INTEGRATION_ENV) || { echo "$(INTEGRATION_ENV) is missing: run 'make integration-up' first"; exit 1; }
	@mkdir -p .out
	@set -o pipefail; set -a; . $(INTEGRATION_ENV); set +a; \
	  rc=0; \
	  QUADER_PERF_GATE=1 go test -count=1 -p 1 -tags integration -run 'TestChangePropagation|TestJobPickup|TestR_NPM_010_ThousandVersions|TestIdempotentCreate|TestR_DEB_010_DelayBound' \
	    -json ./test/integration/coordination/... ./test/integration/npm/... ./test/integration/api/... ./test/integration/deblocal/... | tee .out/test-performance.json | $(TESTLOG) || rc=$$?; \
	  QUADER_PERF_GATE=1 go test -count=1 -p 1 -run 'TestR_EP_013_CeilingPerformance' \
	    -json ./internal/authz/ | tee -a .out/test-performance.json | $(TESTLOG) || rc=$$?; \
	  QUADER_PERF_GATE=1 go test -count=1 -p 1 -run 'TestFlightLockWait' \
	    -json ./internal/cluster/ | tee -a .out/test-performance.json | $(TESTLOG) || rc=$$?; \
	  QUADER_PERF_GATE=1 go test -count=1 -p 1 -run 'TestR_BAS_026_AFeedPageOfAThousandEntries' \
	    -json ./internal/bastion/engine/ | tee -a .out/test-performance.json | $(TESTLOG) || rc=$$?; \
	  QUADER_PERF_GATE=1 go test -count=1 -p 1 -run 'TestR_API_055_TheWorstCaseInputsAreAccepted|TestR_API_055_TheSlowestAcceptedImagesDecodeInsideTheDeadline' \
	    -json ./internal/avatar/ | tee -a .out/test-performance.json | $(TESTLOG) || rc=$$?; \
	  exit $$rc

# S3 conformance against Silo (SILO_IMAGE), served over TLS only (INV-20). The tests are the same as against any store.
.PHONY: silo-up
silo-up: ## Start Silo at SILO_IMAGE, serve it over TLS and create the buckets; writes .dev/silo/env
	test/integration/silo.sh up

.PHONY: silo-down
silo-down: ## Stop the Silo started by silo-up
	test/integration/silo.sh down

.PHONY: s3-conformance
s3-conformance: ## S3 conformance tests (R-STOR-035) against the Silo of silo-up (CI: s3-conformance)
	@test -f .dev/silo/env || { echo ".dev/silo/env is missing: run 'make silo-up' first"; exit 1; }
	@set -a; . .dev/silo/env; set +a; \
	  .github/scripts/s3-conformance.sh "Silo"

.PHONY: coverage
coverage: ## Regenerate the requirement coverage checklist from the committed evidence record, and the index of decided questions in docs/decisions.md
	go run ./internal/tools/coverage

# Merges the profiles of one run (unit and each integration line) into the combined coverage of
# the repository: coverage.txt, summary.md for the job summary, and coverage.json, the badge.
.PHONY: coverage-report
coverage-report: ## Merge .out/coverage/unit.out and integration-*.out: coverage.txt, summary.md, coverage.json (the badge)
	test/coverage/report.sh $(COVERAGE_DIR)

# TESTLOG prints the human-readable text of a `go test -json` stream: package
# results, failures and their output. Subtest chatter is dropped.
TESTLOG := sed -n -E -e 's/.*"Output":"(([^"\\]|\\.)*)".*/\1/p' | sed -e 's/\\n/\n/g' -e 's/\\t/\t/g' -e 's/\\"/"/g' -e 's/\\\\/\\/g' | { grep -v -E '^(=== |--- (PASS|RUN)|\s+--- PASS|PASS$$|^$$)' || true; }

##@ Chart and dev cluster (scripts are provided by the chart and dev-profile work packages)

.PHONY: chart-lint
chart-lint: ## helm lint and helm template over every value set (test/chart/lint.sh)
	test/chart/lint.sh

# The release archive of promtool at its pin, verified against the SHA-256 of the platform in tools/versions.env.
$(PROMTOOL_BIN):
	@mkdir -p .bin
	@set -euo pipefail; \
	  want="$$(printenv "PROMTOOL_SHA256_$$(echo '$(PROMTOOL_OS)_$(PROMTOOL_ARCH)' | tr '[:lower:]' '[:upper:]')" || true)"; \
	  if [ -z "$$want" ]; then echo "promtool: tools/versions.env has no digest for $(PROMTOOL_OS)/$(PROMTOOL_ARCH)"; exit 1; fi; \
	  tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
	  name="prometheus-$(PROMTOOL_VERSION:v%=%).$(PROMTOOL_OS)-$(PROMTOOL_ARCH)"; \
	  tools/github-download.sh "https://github.com/prometheus/prometheus/releases/download/$(PROMTOOL_VERSION)/$$name.tar.gz" "$$tmp/promtool.tar.gz"; \
	  if command -v sha256sum >/dev/null; then got="$$(sha256sum "$$tmp/promtool.tar.gz" | awk '{print $$1}')"; \
	  else got="$$(shasum -a 256 "$$tmp/promtool.tar.gz" | awk '{print $$1}')"; fi; \
	  if [ "$$got" != "$$want" ]; then echo "promtool: $$name.tar.gz has SHA-256 $$got, tools/versions.env pins $$want"; exit 1; fi; \
	  tar -xzf "$$tmp/promtool.tar.gz" -C "$$tmp" --strip-components=1 "$$name/promtool"; \
	  mv "$$tmp/promtool" $@

.PHONY: promtool
promtool: $(PROMTOOL_BIN) ## Fetch promtool at its pin into .bin (the rule tests of the charts use it)

# The analyser's binary out of TRIVY_IMAGE, by its index digest, for the host's architecture (R-HA-070): the binary every scanner pod
# runs, so the container scan of the release image runs the scanner's own analyser, one version in the repository. Linux only: the
# image's binary is a Linux one; on another system the analyser runs as the image itself. Docker pulls the image (CI: logged in to
# Docker Hub); `docker create` starts nothing, `docker cp` copies the file out, and the copy must name the tag of the pin.
$(TRIVY_BIN):
	@set -euo pipefail; \
	  if [ "$(PROMTOOL_OS)" != "linux" ]; then \
	    echo "trivy: make trivy takes the Linux binary out of $(TRIVY_IMAGE) and runs on Linux only, not on $(PROMTOOL_OS); run the image instead: docker run --rm $(TRIVY_IMAGE) --version"; exit 1; fi; \
	  case "$(PROMTOOL_ARCH)" in amd64|arm64) ;; *) echo "trivy: $(TRIVY_IMAGE) is not taken out for $(PROMTOOL_ARCH)"; exit 1 ;; esac; \
	  mkdir -p $(TRIVY_DIR); id=""; \
	  trap '[ -z "$$id" ] || docker rm -f "$$id" >/dev/null 2>&1 || true; rm -f "$@.part"' EXIT; \
	  id="$$(docker create --platform linux/$(PROMTOOL_ARCH) $(TRIVY_IMAGE))"; \
	  docker cp "$$id:/usr/local/bin/trivy" "$@.part"; \
	  have="$$("./$@.part" --version | sed -n 's/^Version: \(.*\)/\1/p')"; \
	  if [ "$$have" != "$(TRIVY_TAG)" ]; then echo "trivy: the binary of $(TRIVY_IMAGE) says $$have, the tag is $(TRIVY_TAG)"; exit 1; fi; \
	  mv "$@.part" $@

.PHONY: trivy
trivy: $(TRIVY_BIN) ## Take the analyser's binary out of TRIVY_IMAGE into .bin/trivy-<tag>-<digest> (Linux only; CI: container-scan and upstream-image-scan put the directory on PATH)

.PHONY: trivy-dir
trivy-dir: ## Print the directory of make trivy's binary, relative to the repository (CI: container-scan and upstream-image-scan append it to GITHUB_PATH)
	@echo $(TRIVY_DIR)

.PHONY: chart-test
chart-test: $(PROMTOOL_BIN) ## Chart render assertions of E0.7 and the promtool rule tests of the PrometheusRule (test/chart/test.sh); .out/test-chart.json
	QUADER_PROMTOOL=$(abspath $(PROMTOOL_BIN)) test/chart/test.sh

.PHONY: dev-charts
dev-charts: ## Fetch the dev charts that are GitHub release assets into .dev/charts with GITHUB_TOKEN when set (deploy/dev/charts.sh; CI runs it before dev-up, Q-ARCH-356)
	deploy/dev/charts.sh

.PHONY: dev-up
dev-up: ## Create the kind cluster and install the dev profile (deploy/dev/up.sh)
	deploy/dev/up.sh

.PHONY: sbom-generator-check
sbom-generator-check: ## Build the SBOM generator of the release image at the syft pin and prove it on a two-platform test image written to a file (Q-HA-166; needs Docker with buildx and jq; CI: sbom-generator)
	tools/sbom-generator/check.sh

.PHONY: dev-down
dev-down: ## Delete the dev cluster (deploy/dev/down.sh)
	deploy/dev/down.sh

.PHONY: dev-failover
dev-failover: ## PostgreSQL switchover under load (deploy/dev/failover.sh)
	deploy/dev/failover.sh

.PHONY: dev-valkey-failover
dev-valkey-failover: ## Valkey Sentinel failover under load (deploy/dev/valkey-failover.sh)
	deploy/dev/valkey-failover.sh

.PHONY: dev-valkey-kill
dev-valkey-kill: ## Kill the Valkey primary under load (deploy/dev/valkey-kill.sh)
	deploy/dev/valkey-kill.sh

.PHONY: dev-s3-outage
dev-s3-outage: ## Stop the object store for DURATION (default 60s) under load (deploy/dev/s3-outage.sh)
	deploy/dev/s3-outage.sh $(or $(DURATION),60s)

.PHONY: dev-valkey-outage
dev-valkey-outage: ## Stop Valkey for DURATION (default 60s) under load (deploy/dev/valkey-outage.sh)
	deploy/dev/valkey-outage.sh $(or $(DURATION),60s)

.PHONY: dev-valkey-stop
dev-valkey-stop: ## Stop Valkey and the Sentinels until dev-valkey-start (deploy/dev/valkey-stop.sh)
	deploy/dev/valkey-stop.sh

.PHONY: dev-valkey-start
dev-valkey-start: ## End the outage of dev-valkey-stop (deploy/dev/valkey-start.sh)
	deploy/dev/valkey-start.sh

.PHONY: dev-pg-kill
dev-pg-kill: ## Force-delete the PostgreSQL primary pod, wait for the recovery (deploy/dev/pg-kill.sh)
	deploy/dev/pg-kill.sh

.PHONY: dev-pg-stop
dev-pg-stop: ## Stop PostgreSQL for DURATION (default 30s) by fencing the CloudNativePG cluster (deploy/dev/pg-stop.sh)
	deploy/dev/pg-stop.sh $(or $(DURATION),30s)

.PHONY: dev-drain
dev-drain: ## Drain a node under k6 load with cursor-persisting jobs; fails on a failed request (deploy/dev/drain.sh, E0.14)
	deploy/dev/drain.sh

.PHONY: nightly-load-guard
nightly-load-guard: ## Before a dispatch that measures: exit 1 while load legs of the nightly run, 2 when gh cannot tell (tools/nightly-load-guard.sh; docs/developer/releasing.md#a-code-push-during-a-nightly)
	tools/nightly-load-guard.sh

.PHONY: e2e
e2e: ## End-to-end tests on the dev cluster (deploy/dev/e2e.sh); SET=push|failover|access-long|access|full|drain|drain-roles|npm|npm-closing|npm-ha|npm-ha-crash|npm-ha-soak|npm-ha-closing|ha-suite|ha-faults|ha-pooler|nuget-clients|nuget|nuget-closing|nuget-ha|drain-load|drain-load-b|drain-load-c|drain-load-e|drain-load-pooler|cold-crash|oci-clients|oci|oci-closing|oci-long|apt-clients|deb|deb-closing|ansible-clients|ansible|ansible-closing|ci|ci-ha|bastion|bastion-long|vuln|vuln-long|vuln-pooler|upgrade|backup|backup-pitr|<go test -run regexp>, default: all but the profile switch
	deploy/dev/e2e.sh '$(SET)'

# >>> M2 npm-clients
.PHONY: npm-clients-image
npm-clients-image: ## Build the image of the npm client matrix (R-NPM-045: npm 10-12, pnpm 10-12, Yarn 4, Yarn 1) into the local Docker; NPM_CLIENTS_IMAGE, default quader-e2e-clients:<DEV_TAG>
	test/e2e/clients/build.sh $(or $(NPM_CLIENTS_IMAGE),quader-e2e-clients:$(DEV_TAG))
# <<< M2 npm-clients

# >>> M3 nuget-clients
# The NuGet clients image (R-NUGET-027: the .NET 10 SDK and nuget.exe on Mono, test/e2e/nugetclients, docs/developer/e2e.md#the-nuget-clients)
# and its smoke test. Edit only between the markers.
.PHONY: nuget-clients-image
nuget-clients-image: ## Build the NuGet clients image (dotnet of the pinned SDK, nuget.exe on Mono) into the local Docker; NUGET_CLIENTS_IMAGE, default quader-e2e-nugetclients:<DEV_TAG>
	test/e2e/nugetclients/build.sh $(or $(NUGET_CLIENTS_IMAGE),quader-e2e-nugetclients:$(DEV_TAG))

.PHONY: nuget-clients-smoke
nuget-clients-smoke: nuget-clients-image nuget-stub-image ## Run the NuGet clients image like a pod against the counting NuGet test upstream: both runtimes, the CA bundle, TLS verified and refused (no cluster)
	test/e2e/nugetclients/smoke.sh $(or $(NUGET_CLIENTS_IMAGE),quader-e2e-nugetclients:$(DEV_TAG))
# <<< M3 nuget-clients

# >>> M2 npm-fuzz
# The fuzz run of the closing phase (roadmap M2, E2.6): npm's Match and Inspect with the R-ARCH-028 limits checked on
# every input. The workflow runs three legs (FUZZ=FuzzKitMatch, FuzzInspect, FuzzMatch) for 30 minutes
# each when it is dispatched with closing=true (Q-NPM-46). Locally: make fuzz-npm FUZZ=FuzzMatch FUZZTIME=1m.
FUZZ ?= FuzzKitMatch
FUZZTIME ?= 30m
# The time go test spends shrinking each new input a fuzz target finds, for every fuzz target of the Makefile (fuzz-npm,
# fuzz-nuget, fuzz-stored-format). Go's default is a minute per input, which stalls a target whose seeds are tens of kilobytes:
# 175 executions in two minutes against 565,507 with five seconds (plan entry of 2026-11-22). A crash is still written and
# fails the run; only its input may stay larger.
FUZZMINIMIZETIME ?= 5s

.PHONY: fuzz-npm
fuzz-npm: ## Fuzz one npm target (FUZZ, default FuzzKitMatch) for FUZZTIME (default 30m); a failing input stays in internal/formats/npm/testdata/fuzz
	go test ./internal/formats/npm -run '^$$' -fuzz '^$(FUZZ)$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME)
# <<< M2 npm-fuzz

# >>> M2 npm-stub
# The counting test upstream of the npm tests (test/npmstub, docs/developer/e2e.md) and the reference
# lockfile resolved against it (perf/npm, perf/README.md). Edit only between the markers.
NPM_STUB_IMAGE ?= quader-e2e-npmstub:$(DEV_TAG)

.PHONY: npm-stub-image
npm-stub-image: ## Build the counting npm test upstream image (NPM_STUB_IMAGE, IMAGE_ARCH) on the release base image
	@mkdir -p .out/npmstub
	CGO_ENABLED=0 GOOS=linux GOARCH=$(IMAGE_ARCH) go build -trimpath -ldflags '-s -w' -o .out/npmstub/npmstub-linux-$(IMAGE_ARCH) ./test/npmstub/cmd/npmstub
	$(FETCH) "the build of $(NPM_STUB_IMAGE) on $(RUNTIME_BASE_IMAGE)" 120 3 docker buildx build --load --platform linux/$(IMAGE_ARCH) --build-arg RUNTIME_BASE_IMAGE=$(RUNTIME_BASE_IMAGE) --label $(E2E_IMAGE_BUILD_LABEL) \
	  -f test/npmstub/Containerfile -t $(NPM_STUB_IMAGE) .out/npmstub
	-docker image prune --force --filter dangling=true --filter label=$(E2E_IMAGE_BUILD_LABEL) >/dev/null

.PHONY: npm-reference
npm-reference: npm-stub-image ## Regenerate perf/npm/reference (package.json, package-lock.json) with the pinned Node image's npm against the stub
	NPM_STUB_IMAGE=$(NPM_STUB_IMAGE) perf/npm/reference.sh

.PHONY: npm-reference-check
npm-reference-check: npm-stub-image ## Fail when perf/npm/reference differs from a regeneration (the corpus is deterministic)
	NPM_STUB_IMAGE=$(NPM_STUB_IMAGE) perf/npm/reference.sh --check
# <<< M2 npm-stub

# >>> M4 oidc-stub
# The GitLab-shaped OIDC issuer double of the CI tests (test/oidcstub, docs/developer/e2e.md). Edit only between
# the markers.
OIDC_STUB_IMAGE ?= quader-e2e-oidcstub:$(DEV_TAG)

.PHONY: oidc-stub-image
oidc-stub-image: ## Build the GitLab-shaped OIDC issuer double image (OIDC_STUB_IMAGE, IMAGE_ARCH) on the release base image
	@mkdir -p .out/oidcstub
	CGO_ENABLED=0 GOOS=linux GOARCH=$(IMAGE_ARCH) go build -trimpath -ldflags '-s -w' -o .out/oidcstub/oidcstub-linux-$(IMAGE_ARCH) ./test/oidcstub/cmd/oidcstub
	$(FETCH) "the build of $(OIDC_STUB_IMAGE) on $(RUNTIME_BASE_IMAGE)" 120 3 docker buildx build --load --platform linux/$(IMAGE_ARCH) --build-arg RUNTIME_BASE_IMAGE=$(RUNTIME_BASE_IMAGE) --label $(E2E_IMAGE_BUILD_LABEL) \
	  -f test/oidcstub/Containerfile -t $(OIDC_STUB_IMAGE) .out/oidcstub
	-docker image prune --force --filter dangling=true --filter label=$(E2E_IMAGE_BUILD_LABEL) >/dev/null

# Re-records test/oidcstub/testdata from a real GitLab (Q-CI-25, docs/developer/e2e.md "Validating the double against a
# real GitLab"). Never part of CI. QUADER_GITLAB_URL and QUADER_GITLAB_TOKEN (an administrator's token, scope api) come
# from the environment; QUADER_GITLAB_CA_FILE names a PEM bundle for an instance behind an internal CA.
.PHONY: oidc-fixtures
oidc-fixtures: ## Re-record the GitLab fixtures of the OIDC issuer double from a real instance (QUADER_GITLAB_URL, QUADER_GITLAB_TOKEN, optional QUADER_GITLAB_CA_FILE), then test the double against them
	@test -n "$$QUADER_GITLAB_URL" && test -n "$$QUADER_GITLAB_TOKEN" || { echo "set QUADER_GITLAB_URL and QUADER_GITLAB_TOKEN"; exit 2; }
	go run ./test/oidcstub/cmd/record-gitlab -job-image $(INTEGRATION_TOOLS_IMAGE) $${QUADER_GITLAB_CA_FILE:+-ca-file "$$QUADER_GITLAB_CA_FILE"}
	go test -count=1 -run 'Recorded' ./test/oidcstub/
# <<< M4 oidc-stub

# >>> M3 nuget
# The NuGet format's fuzz targets (id, version, nupkg, nuspec; R-NUGET-003, R-NUGET-005, R-ARCH-028) and those of the reader of a
# NuGet signature (the CMS of a .signature.p7s and the BER bounds; signatures.md R-SIG-013, Q-SIG-216), the golden vectors
# of the version and ID rules generated with NuGet.Versioning (tools/nugetvectors, R-NUGET-003) and the counting NuGet
# test upstream (test/nugetstub, docs/developer/e2e.md). A target is <package below internal/>:<Target>. Edit only between the markers.
NUGET_FUZZ_TARGETS ?= formats/nuget/nupkg:FuzzInspect formats/nuget/nuspec:FuzzNuspec \
  formats/nuget/version:FuzzNormalize formats/nuget/version:FuzzCompare formats/nuget/version:FuzzRange formats/nuget/id:FuzzID \
  signatures/scheme/nugetcms:FuzzSignatureP7s signatures/scheme/rawkey/pki:FuzzDefinite
NUGET_FUZZTIME ?= 5m
NUGET_STUB_IMAGE ?= quader-e2e-nugetstub:$(DEV_TAG)
NUGET_VECTORS := internal/formats/nuget/version/testdata/vectors.json

.PHONY: fuzz-nuget
fuzz-nuget: ## Fuzz every NuGet target (NUGET_FUZZ_TARGETS, package:Target pairs, the package below internal/) for NUGET_FUZZTIME each (default 5m); a failing input stays in the package's testdata/fuzz
	@set -e; for t in $(NUGET_FUZZ_TARGETS); do pkg=$${t%%:*}; fz=$${t##*:}; \
	  echo "fuzz $$pkg $$fz for $(NUGET_FUZZTIME)"; \
	  go test ./internal/$$pkg -run '^$$' -fuzz "^$$fz\$$" -fuzztime $(NUGET_FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME); \
	done

.PHONY: nuget-vectors
nuget-vectors: ## Regenerate the golden vectors of the version and ID rules with NuGet.Versioning in the pinned .NET SDK image (needs Docker; DOTNET=local uses a local dotnet)
	tools/nugetvectors/generate.sh

.PHONY: nuget-vectors-check
nuget-vectors-check: ## Fail when the committed golden vectors differ from a regeneration (needs Docker)
	@mkdir -p .out
	tools/nugetvectors/generate.sh .out/nuget-vectors.json
	cmp .out/nuget-vectors.json $(NUGET_VECTORS) || { echo "$(NUGET_VECTORS) differs from a regeneration: run make nuget-vectors"; exit 1; }

.PHONY: nuget-stub-image
nuget-stub-image: ## Build the counting NuGet test upstream image (NUGET_STUB_IMAGE, IMAGE_ARCH) on the release base image
	@mkdir -p .out/nugetstub
	CGO_ENABLED=0 GOOS=linux GOARCH=$(IMAGE_ARCH) go build -trimpath -ldflags '-s -w' -o .out/nugetstub/nugetstub-linux-$(IMAGE_ARCH) ./test/nugetstub/cmd/nugetstub
	$(FETCH) "the build of $(NUGET_STUB_IMAGE) on $(RUNTIME_BASE_IMAGE)" 120 3 docker buildx build --load --platform linux/$(IMAGE_ARCH) --build-arg RUNTIME_BASE_IMAGE=$(RUNTIME_BASE_IMAGE) --label $(E2E_IMAGE_BUILD_LABEL) \
	  -f test/nugetstub/Containerfile -t $(NUGET_STUB_IMAGE) .out/nugetstub
	-docker image prune --force --filter dangling=true --filter label=$(E2E_IMAGE_BUILD_LABEL) >/dev/null

# The reference solution of the NuGet tests (perf/nuget/reference) is generated from the counting upstream's package set,
# so a change of the set changes the files. CI runs this in the generate job (releasing.md#ci, Q-ARCH-279).
.PHONY: nuget-reference-check
nuget-reference-check: ## Fail when perf/nuget/reference differs from a regeneration by the counting NuGet test upstream (the corpus is deterministic; CI: generate)
	go run ./test/nugetstub/cmd/nugetstub reference -check perf/nuget/reference
# <<< M3 nuget

# >>> M4b osv-stub
# The test advisory source of the vulnerability-scanning tests (test/osvstub, R-VULN-029, docs/developer/e2e.md) and the
# refresh of the real records frozen for it. Edit only between the markers.
OSV_STUB_IMAGE ?= quader-e2e-osvstub:$(DEV_TAG)

.PHONY: osv-stub-image
osv-stub-image: ## Build the test advisory source image (OSV_STUB_IMAGE, IMAGE_ARCH) on the release base image
	@mkdir -p .out/osvstub
	CGO_ENABLED=0 GOOS=linux GOARCH=$(IMAGE_ARCH) go build -trimpath -ldflags '-s -w' -o .out/osvstub/osvstub-linux-$(IMAGE_ARCH) ./test/osvstub/cmd/osvstub
	$(FETCH) "the build of $(OSV_STUB_IMAGE) on $(RUNTIME_BASE_IMAGE)" 120 3 docker buildx build --load --platform linux/$(IMAGE_ARCH) --build-arg RUNTIME_BASE_IMAGE=$(RUNTIME_BASE_IMAGE) --label $(E2E_IMAGE_BUILD_LABEL) \
	  -f test/osvstub/Containerfile -t $(OSV_STUB_IMAGE) .out/osvstub
	-docker image prune --force --filter dangling=true --filter label=$(E2E_IMAGE_BUILD_LABEL) >/dev/null

$(OSV_SCANNER_BIN):
	@mkdir -p .bin
	@tmp="$$(mktemp -d)"; \
	  GOBIN="$$tmp" go install github.com/google/osv-scanner/v2/cmd/osv-scanner@$(OSV_SCANNER_VERSION) && \
	  mv "$$tmp/osv-scanner" $@ && rmdir "$$tmp"

.PHONY: osv-expected
osv-expected: $(OSV_SCANNER_BIN) ## Freeze the results of osv-scanner's offline mode (pinned) over the real OSV records in internal/vuln/match/testdata (R-VULN-009; needs the module proxy once)
	go run ./internal/tools/osvexpect -osv-scanner $(OSV_SCANNER_BIN) -real test/osvstub/testdata/real -out internal/vuln/match/testdata/osv-scanner-expected.json

.PHONY: osv-real-refresh
osv-real-refresh: ## Fetch the real OSV records of test/osvstub/testdata/real/ids.txt again from api.osv.dev (needs network); review the diff, the tests name fields of them
	go run ./test/osvstub/cmd/osvfreeze -dir test/osvstub/testdata/real
.PHONY: osv-reference-refresh
osv-reference-refresh: ## Record the public npm bulk answer for perf/npm/reference/package-lock.json and the OSV records behind it again (needs network); the reference test of R-VULN-017 compares against them
	go run ./test/osvstub/cmd/osvreference -lock perf/npm/reference/package-lock.json -dir test/osvstub/testdata/reference
# <<< M4b osv-stub

# >>> T16 oci-stub
# The counting OCI test upstream (test/ocistub, R-OCI-035, docs/developer/oci.md): a registry, a token realm and a blob CDN
# with a generated image set. Edit only between the markers.
OCI_STUB_IMAGE ?= quader-e2e-ocistub:$(DEV_TAG)

.PHONY: oci-stub-image
oci-stub-image: ## Build the counting OCI test upstream image (OCI_STUB_IMAGE, IMAGE_ARCH) on the release base image
	@mkdir -p .out/ocistub
	CGO_ENABLED=0 GOOS=linux GOARCH=$(IMAGE_ARCH) go build -trimpath -ldflags '-s -w' -o .out/ocistub/ocistub-linux-$(IMAGE_ARCH) ./test/ocistub/cmd/ocistub
	$(FETCH) "the build of $(OCI_STUB_IMAGE) on $(RUNTIME_BASE_IMAGE)" 120 3 docker buildx build --load --platform linux/$(IMAGE_ARCH) --build-arg RUNTIME_BASE_IMAGE=$(RUNTIME_BASE_IMAGE) --label $(E2E_IMAGE_BUILD_LABEL) \
	  -f test/ocistub/Containerfile -t $(OCI_STUB_IMAGE) .out/ocistub
	-docker image prune --force --filter dangling=true --filter label=$(E2E_IMAGE_BUILD_LABEL) >/dev/null
# <<< T16 oci-stub

# >>> T17 deb-stub
# The counting Debian archive test upstream (test/debstub, R-DEB-035, docs/developer/deb.md): suites, components, architectures,
# a Sources index, by-hash, pdiffs, flat repositories and a large index, signed by test keys. Edit only between the markers.
DEB_STUB_IMAGE ?= quader-e2e-debstub:$(DEV_TAG)

.PHONY: deb-stub-image
deb-stub-image: ## Build the counting Debian archive test upstream image (DEB_STUB_IMAGE, IMAGE_ARCH) on the release base image
	@mkdir -p .out/debstub
	CGO_ENABLED=0 GOOS=linux GOARCH=$(IMAGE_ARCH) go build -trimpath -ldflags '-s -w' -o .out/debstub/debstub-linux-$(IMAGE_ARCH) ./test/debstub/cmd/debstub
	$(FETCH) "the build of $(DEB_STUB_IMAGE) on $(RUNTIME_BASE_IMAGE)" 120 3 docker buildx build --load --platform linux/$(IMAGE_ARCH) --build-arg RUNTIME_BASE_IMAGE=$(RUNTIME_BASE_IMAGE) --label $(E2E_IMAGE_BUILD_LABEL) \
	  -f test/debstub/Containerfile -t $(DEB_STUB_IMAGE) .out/debstub
	-docker image prune --force --filter dangling=true --filter label=$(E2E_IMAGE_BUILD_LABEL) >/dev/null
# <<< T17 deb-stub

# >>> B2 ansible-stub
# The counting Ansible Galaxy test upstream (test/ansiblestub, R-ANS-032, docs/developer/ansible-remote.md): the Galaxy v3 and v1
# APIs, the GitHub stand-in for role archives and the single-sign-on realm of the hub, on five host names. Edit only between the markers.
ANSIBLE_STUB_IMAGE ?= quader-e2e-ansiblestub:$(DEV_TAG)

.PHONY: ansible-stub-image
ansible-stub-image: ## Build the counting Ansible Galaxy test upstream image (ANSIBLE_STUB_IMAGE, IMAGE_ARCH) on the release base image
	@mkdir -p .out/ansiblestub
	CGO_ENABLED=0 GOOS=linux GOARCH=$(IMAGE_ARCH) go build -trimpath -ldflags '-s -w' -o .out/ansiblestub/ansiblestub-linux-$(IMAGE_ARCH) ./test/ansiblestub/cmd/ansiblestub
	$(FETCH) "the build of $(ANSIBLE_STUB_IMAGE) on $(RUNTIME_BASE_IMAGE)" 120 3 docker buildx build --load --platform linux/$(IMAGE_ARCH) --build-arg RUNTIME_BASE_IMAGE=$(RUNTIME_BASE_IMAGE) --label $(E2E_IMAGE_BUILD_LABEL) \
	  -f test/ansiblestub/Containerfile -t $(ANSIBLE_STUB_IMAGE) .out/ansiblestub
	-docker image prune --force --filter dangling=true --filter label=$(E2E_IMAGE_BUILD_LABEL) >/dev/null
# <<< B2 ansible-stub

# >>> T16 oci-conformance
# The OCI distribution conformance suite (R-OCI-035, docs/developer/oci.md): the module at the pinned tag is downloaded into
# .out/oci-conformance/mod, its test package is compiled with the repository's Go toolchain (GOTOOLCHAIN=local, so no second Go is
# fetched) and run by test/integration/oci against one replica of the integration stack, once through a local repository and once
# through a virtual one. The conformance module is a sub-module of distribution-spec without version tags of its own
# (conformance@v1.1.1 is "unknown revision conformance/v1.1.1"), so the pin is the tag of the repository and the module is
# fetched at the commit that tag points at (Q-OCI-210). Edit only between the markers.
OCI_CONFORMANCE_TAG := v1.1.1
OCI_CONFORMANCE_REPO := https://github.com/opencontainers/distribution-spec.git
OCI_CONFORMANCE_DIR := .out/oci-conformance
OCI_CONFORMANCE_GO := GOMODCACHE="$(CURDIR)/$(OCI_CONFORMANCE_DIR)/mod" GOTOOLCHAIN=local GOFLAGS=-mod=mod

.PHONY: oci-conformance-build
oci-conformance-build: ## Download github.com/opencontainers/distribution-spec/conformance at the commit of OCI_CONFORMANCE_TAG into .out/oci-conformance and compile its test binary with the repository's Go
	@mkdir -p $(OCI_CONFORMANCE_DIR)/build
	@commit=$$(git ls-remote --tags $(OCI_CONFORMANCE_REPO) 'refs/tags/$(OCI_CONFORMANCE_TAG)^{}' | cut -f1); \
	  test -n "$$commit" || { echo "the tag $(OCI_CONFORMANCE_TAG) of $(OCI_CONFORMANCE_REPO) was not found"; exit 1; }; \
	  echo "conformance suite $(OCI_CONFORMANCE_TAG) = $$commit"; \
	  cd $(OCI_CONFORMANCE_DIR)/build && { test -f go.mod || GOTOOLCHAIN=local go mod init conformancebuild; } \
	  && $(OCI_CONFORMANCE_GO) go mod download github.com/opencontainers/distribution-spec/conformance@$$commit \
	  && $(OCI_CONFORMANCE_GO) go get github.com/opencontainers/distribution-spec/conformance@$$commit \
	  && $(OCI_CONFORMANCE_GO) go test -c -o ../conformance.test github.com/opencontainers/distribution-spec/conformance

.PHONY: oci-conformance
oci-conformance: oci-conformance-build ## OCI conformance suite (pull, push, content discovery, content management) against a replica, through a local and a virtual repository (make integration-up); JUnit in .out/oci-conformance/{local,virtual}/junit.xml
	@test -f $(INTEGRATION_ENV) || { echo "$(INTEGRATION_ENV) is missing: run 'make integration-up' first"; exit 1; }
	@set -o pipefail; set -a; . $(INTEGRATION_ENV); set +a; \
	  OCI_CONFORMANCE_BIN="$(CURDIR)/$(OCI_CONFORMANCE_DIR)/conformance.test" OCI_CONFORMANCE_OUT="$(CURDIR)/$(OCI_CONFORMANCE_DIR)" \
	  go test -count=1 -tags 'integration ociconformance' -timeout 45m -run 'TestOCIConformance$$' -v ./test/integration/oci/
# <<< T16 oci-conformance

# >>> T16 oci-clients
# The OCI clients images (R-OCI-035, Q-OCI-21: dockerd with Buildx and Podman in a privileged pod, test/e2e/ociclients,
# docs/developer/e2e.md#the-oci-clients). Edit only between the markers.
.PHONY: oci-clients-image
oci-clients-image: ## Build the OCI clients images (the pinned dind image with the forwarder, the pinned Podman image, the pinned cosign binary) into the local Docker; OCI_CLIENTS_DOCKER_IMAGE, OCI_CLIENTS_PODMAN_IMAGE and OCI_CLIENTS_COSIGN_IMAGE, default quader-e2e-ociclients-{docker,podman,cosign}:<DEV_TAG>
	test/e2e/ociclients/build.sh $(or $(OCI_CLIENTS_DOCKER_IMAGE),quader-e2e-ociclients-docker:$(DEV_TAG)) $(or $(OCI_CLIENTS_PODMAN_IMAGE),quader-e2e-ociclients-podman:$(DEV_TAG)) $(or $(OCI_CLIENTS_COSIGN_IMAGE),quader-e2e-ociclients-cosign:$(DEV_TAG))
# <<< T16 oci-clients

# >>> T17 apt-clients
# The APT clients images of the Debian e2e set (R-DEB-035, Q-DEB-27: Debian 13 and 12, Ubuntu 24.04 and 22.04 at their pinned digests
# with curl and the forwarder, test/e2e/aptclients, docs/developer/e2e.md#the-apt-clients). Edit only between the markers.
.PHONY: apt-clients-image
apt-clients-image: ## Build the four APT clients images (the pinned Debian and Ubuntu images with the forwarder) into the local Docker; APT_CLIENTS_IMAGE_PREFIX, default quader-e2e-aptclients-
	test/e2e/aptclients/build.sh $(or $(APT_CLIENTS_IMAGE_PREFIX),quader-e2e-aptclients-)
# <<< T17 apt-clients

# >>> B4 ansible-clients-image
# The two Ansible client images of the e2e set `ansible` (R-ANS-032: the pinned Python images with ansible-core 2.17 and 2.21 and the
# forwarder, test/e2e/ansibleclients, docs/developer/e2e.md#the-ansible-clients). Edit only between the markers.
.PHONY: ansible-clients-image
ansible-clients-image: ## Build the two Ansible client images (ansible-core 2.17 and 2.21 on their pinned Python images with the forwarder) into the local Docker; ANSIBLE_CLIENTS_IMAGE_PREFIX, default quader-e2e-ansibleclients-
	test/e2e/ansibleclients/build.sh $(or $(ANSIBLE_CLIENTS_IMAGE_PREFIX),quader-e2e-ansibleclients-)
# <<< B4 ansible-clients-image

# >>> fuzz-stored-format
# The fuzz targets of the readers of the stored format of an encrypted connection (storage.md R-STOR-079, R-STOR-091,
# docs/developer/stored-content-encryption.md): the inverse of the stored size, the record, a whole object, a range and a staging
# tail. Their seeds run in every go test; the nightly job fuzz-stored-format of performance.yml runs each for five minutes, one
# after the other. Edit only between the markers.
STORED_FORMAT_FUZZ_TARGETS ?= FuzzPlainSize FuzzOpenRecord FuzzReadObject FuzzReadRange FuzzOpenTail
STORED_FORMAT_FUZZTIME ?= 5m

.PHONY: fuzz-stored-format
fuzz-stored-format: ## Fuzz every reader of the stored format (STORED_FORMAT_FUZZ_TARGETS) for STORED_FORMAT_FUZZTIME each (default 5m); a failing input stays in internal/blob/blobcrypt/testdata/fuzz
	@set -e; for fz in $(STORED_FORMAT_FUZZ_TARGETS); do \
	  echo "fuzz $$fz for $(STORED_FORMAT_FUZZTIME)"; \
	  go test ./internal/blob/blobcrypt -run '^$$' -fuzz "^$$fz\$$" -fuzztime $(STORED_FORMAT_FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME); \
	done
# <<< fuzz-stored-format
