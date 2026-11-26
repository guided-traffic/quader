# A Bastion's limits, configuration and throughput: the build plan

Status: specified on 2026-11-26 (W-34; the owner's answers to the working paper of W-34 and W-35, stored at 11:51). Packages E1, E2, E3
and E5 are **built** (the cache bounded at every commit and the bytes counted, R-BAS-089, R-BAS-090; the format's cap of the wait and the
figures that let one normal command through, R-BAS-091, R-BAS-092, interface version 7); E6 is this page and the specification. Every other
package is **not built**. The requirements are [bastion.md](../spec/bastion.md#limits-configuration-and-throughput-from-the-main-installation)
R-BAS-093 to R-BAS-099 and [its wire](../spec/bastion.md#the-interface-version-8-the-shared-wire),
[usage-and-limits.md](../spec/usage-and-limits.md#a-bastion-as-a-place) R-LIM-043 to R-LIM-046 and
[api-and-ui.md](../spec/api-and-ui.md#r-ui-240--a-bastions-card-its-limits-its-throughput-and-its-state) R-UI-240 to R-UI-243; the record is
[ADR-300](../decisions.md#adr-300--a-bastion-is-a-place-of-the-download-limits-configured-from-the-main-installation-and-names-nothing-of-it);
the security perspective is [security/bastion.md](../security/bastion.md#what-this-does-not-cover) and
[security/usage-and-limits.md](../security/usage-and-limits.md#what-this-does-not-cover). This page says what each package builds, what it
must follow, the identifiers it takes, and what its tests cost.

## What has to be in your head first

- **The Bastion is configured by the main installation now.** The switches, the limits, the flood figures, the slots, the throughput, the
  anonymous ceiling and the egress addresses are members of the registration (R-BAS-093). They reach a Bastion as one member `configuration`
  of the handshake with the registration's revision; a change is the audit event `bastion.configuration.changed`, a `settings` entry of the
  feed, and a handshake of every replica within about 3 s. The chart keeps the cache, the periods, the timeouts, TLS, Valkey and the edge.
- **The Bastion applies a configuration whole**, stores it in its cache backend, and without one starts with a built-in configuration that
  fails closed (Q-BAS-327). No package of the Bastion's side reads a figure from its own configuration file any more.
- **Two budgets, two places that count.** What a caller takes through a Bastion counts at the Bastion's place, in the Bastion's memory or
  Valkey; what the Bastion forwards still counts at the main installation (Q-LIM-306). The main installation names the subject key and the
  applying rule of a signed-in caller; the Bastion resolves address and kind rules (Q-LIM-302).
- **Nothing a Bastion answers names the main installation** (R-BAS-099): no host, base URL, request ID, rule, place or version, in any body,
  header or page. Every package that touches an answer of the Bastion keeps the integration scan of R-BAS-099 green.
- **One raise of the interface version, 7 to 8, by E7 together with W-35's P5** (Q-BAS-322). No other package changes the wire; a member a
  package needs and the wire lacks is a change of E7's work and of the specification first.
- **Today's code** is at interface version 7: the handshake has a top-level `handover`; the Bastion's figures are chart values
  (`config.bastion.limits`, `failed_auth`, `blocking`, `main.max_concurrent`, `anonymous_share`, `queue_timeout`); a write is probed at the
  main installation (`internal/bastion/engine/probe.go`, `BastionRouteAuth` in `internal/api/v1/bastion_services.go`); the main
  installation's egress list is `bastions.trusted_proxies` (`internal/config/bastions.go`); the limits know three places
  (`limits.PlaceInstance`, `PlaceProject`, `PlaceRepository` in `internal/limits/model.go`).

## Work packages, in waves

At most four builders at once (rule 10 of the project plan). The waves are the order; within a wave packages run as places come free.

| Wave | Package | Builds | Follows | State |
|---|---|---|---|---|
| 1 | E1 + E5 | the cache below its size, the bytes counted (R-BAS-089, R-BAS-090) | — | built |
| 1 | E2 + E3 | the format's cap of the wait, interface version 7; the figures of one normal command (R-BAS-091, R-BAS-092) | — | built |
| 1 | E4 | the measurement, run once, not a test | — | not built |
| 1 | E6 | this specification | — | written |
| 2 | E7 | the shared wire, interface version 8, with W-35's P5 | E6, E2, W-35's wire | not built |
| 2 | E16 | nothing of the main installation in a Bastion's answers | E2 + E3 | not built |
| 2 | E14a | anonymous requests answered without the main installation | E6 | not built |
| 2 | E13 | egress addresses on the registration | E6 | not built |
| 3 | E8 | the main installation's side | E7, W32, W36, E4 | not built |
| 3 | E9a | the configuration at the Bastion, flood protection, the anonymous ceiling | E7, E4 | not built |
| 3 | E10 | the throughput | E7 | not built |
| 3 | E15 | writes refused at the Bastion | E7 | not built |
| 4 | E9b | budgets on what the Bastion serves | E9a, E8 | not built |
| 4 | E11 | the cache protects what was served twice | E1 (may take any free place from wave 2 on) | not built |
| 4 | E17 | the registry token's payload version 2 | E7 | not built |
| 4 | E12 | the screens | E8, E10, E13 | not built |

### E4 · The measurement, run once

- For each format the bytes, transfers and misses of one normal command through a cold and through a warm Bastion: `npm ci` (475 packages),
  `dotnet restore`, `docker` and `podman` pulls (a small image, an image of 1 GB, an image of 40 layers), `apt-get`, `ansible-galaxy`,
  `freshclam`, `curl`. The overflow attack, scaled to a cache of 2 GiB. The main installation's latency per check and per conditional
  request. What each client prints for a refusal in each of the four error shapes and for `503 busy`.
- The numbers go to [bastion.md](bastion.md); they set the figures of "Bastion: anonymous" and "Bastion: signed-in" (E8, R-LIM-046) and the
  anonymous ceiling's default (E9a, R-BAS-094). Scripts under `hack/`, never in the product. Minutes, once.

### E7 · The shared wire, interface version 8

- **Builds** the wire of [bastion.md](../spec/bastion.md#the-interface-version-8-the-shared-wire): `configuration` in the handshake (the
  top-level `handover` goes into it), `subject.limits` in the check answer and `Quader-Bastion-Limits` in the marking, `usage` and `status`
  in the downloads batch, `errorShape` and `readWithBody` in the format table, the renamed codes and `read_only`
  ([Q-BAS-321](../spec/bastion.md#q-bas-321--the-bastions-own-codes-are-named-by-their-status-its-texts-name-no-party-and-its-problem-type-is-aboutblank)),
  the removal of `Quader-Bastion-Probe` and `Quader-Bastion-Route`, the contact header `Quader-Bastion-Edge-Limit`, the readers of the
  registry token's payload version 2, and W-35's catalogue routes and types (P5, built in the same change).
- **Follows** the wire section exactly; a member it lacks goes into the specification first. Until E8 the main installation answers every
  registration with the built-in configuration of Q-BAS-327, and the Bastion ignores what no package of its side reads.
- **Files**: `internal/bastion/iface/*` and `testdata/interface.txt`; `internal/errs` (the code registry); the OpenAPI tag `bastion`;
  `internal/formats/catalog/bastion.go` (`ErrorShape`, `ReadWithBody` as facts, without the kit's duties, which are E15's).
- **Tests**: unit tests and the interface guard file, under 0.5 s.
- **Identifiers**: Q-BAS-320 to Q-BAS-322, H-2280, H-2281.

### E8 · The main installation's side

- **Builds** migration 00148: the configuration's columns on `bastion` and the revision trigger (also on `limit_assignment` rows of a
  Bastion place and on a profile such a row names, Q-BAS-320); the place `bastion` with `bastion_id` in `limit_assignment`; the two Bastion
  profiles and, for every registration, the Internet kind rules (R-LIM-046); the transient table `bastion_status`; the change area
  `bastion_configuration`. The place `PlaceBastion` and its resolution in `internal/limits/index.go` and `model.go`. `PATCH
  /api/v1/bastions/{name}` with `limits:write` for `limits` and `endpoints:write` for the rest (Q-BAS-325);
  `/api/v1/bastions/{name}/limits/assignments`; the usage API's place filter. A configuration snapshot after the pattern of
  `HandoverSwitches` ([handover.go](../../internal/bastionmain/handover.go)); the handshake member; the subject's limits in the check
  answer and the marking; the intake of `usage` into `usage.Buffer.Add` and of `status` into its table; `bastion.configuration.changed` in
  `feedActions` ([feed.go](../../internal/bastionmain/feed.go)).
- **Follows** E7, W32, W36 (migration 00143) and E4 (the profiles' figures).
- **Files**: `internal/bastionmain/registrations.go`, `feed.go`, `downloads.go`; `internal/api/v1/bastion_registrations_api.go`,
  `bastion_handshake_api.go`, `bastion_check_api.go`, `limits_api.go`, `usage_api.go`; `internal/limits`.
- **Tests**: unit about 1.5 s; integration area `bastionreg` about 3 s; area `limits` about 4 s.
- **Identifiers**: Q-BAS-323 to Q-BAS-325, Q-LIM-300 to Q-LIM-304, H-2282 to H-2284.

### E9a · The configuration at the Bastion, flood protection and the anonymous ceiling

- **Builds** the link that applies a configuration whole; the guard reads its figures through a getter; `SlotBudget` gains a resize
  ([budget.go](../../internal/bastion/mainclient/budget.go)); the ceiling's limiter, which also counts the background refresh; the stored
  configuration object and the built-in configuration (Q-BAS-327); the class `gui` of the flood protection (Q-LIM-307); the chart loses the
  keys that moved, `_validate.tpl`'s edge check goes, `edgeLimit.exemptCIDRs` is added and the edge figure is rendered into the configuration
  file and sent in the contact.
- **Follows** E7 and E4 (the ceiling's default).
- **Files**: `internal/bastion/link/link.go`, `guard/instance.go`, `mainclient/budget.go`, `server/server.go`; the cache (the stored
  object); `internal/config/bastion_role.go` and its schema; the chart's `values.yaml`, `_validate.tpl` and `ingress.yaml`.
- **Tests**: unit about 1 s; the chart test; integration area `bastion` about 3 s (a change made through the API is applied within the
  feed's time).
- **Identifiers**: Q-BAS-326, Q-BAS-327, H-2285.

### E9b · Budgets on what the Bastion serves

- **Builds** the request windows per subject after step 4 of R-BAS-018; before a download starts the hour and day figures and a lease of
  `parallelTransfers` (10 s wait, a quarter of `max_in_flight`, Q-BAS-329); hits paced with a floor of 64 KiB a second (Q-BAS-330); misses
  and fills charged per subject; Audit (Q-LIM-309); the codes of R-LIM-020 in the format's error shape; the usage buffer (Q-LIM-308); the
  subjects of R-BAS-096 (Q-BAS-328).
- **Follows** E9a and E8. If it reuses the arithmetic of `internal/limits`, the import rule of the role
  ([internal/archtest/bastion.go](../../internal/archtest/bastion.go)) names it with its reason.
- **Files**: `internal/bastion/engine/serve.go`, `request.go`, `readflow.go`; `internal/bastion/shared/` (byte windows and leases); a new
  `internal/bastion/usage/`.
- **Tests**: unit about 2 s; integration area `bastionshared` about 2 s; area `bastion` about 5 s.
- **Identifiers**: Q-BAS-328 to Q-BAS-330, Q-LIM-305 to Q-LIM-309, H-2286 to H-2288.

### E10 · The throughput

- **Builds** one pacing reader on every body the Bastion reads (fills, pass-through, the handover's direct read, forwarded documents, W-35's
  catalogue pull) with two buckets per registration (Q-BAS-332), a shared byte bucket in Valkey and the replica's share without; the answers
  at the cap of R-BAS-097 and its metrics.
- **Files**: `internal/bastion/engine/fill.go`, `forward.go`, `handover.go`; `internal/bastion/shared/`.
- **Tests**: unit with an injected clock, under 1 s; integration, two replicas sharing one cap through Valkey, about 3 s.
- **Identifiers**: Q-BAS-331, Q-BAS-332, H-2289.

### E11 · The cache protects what was served twice

- **Builds** the protected marker and the sweep's order of R-BAS-098, for the volume, the bucket and the encrypted layout.
- **Files**: `internal/bastion/cache/core.go`, `store.go`, `layout.go`, `sweep.go`.
- **Tests**: unit, under 0.5 s.
- **Identifiers**: Q-BAS-333, H-2290.

### E12 · The screens

- **Builds** R-UI-240 (the card's section **Limits**, its **Details**, the live state, the edge warning), R-UI-241
  (`/admin/bastions/{name}/limits` with `limit-rules.ts` and `rule-dialog.ts`, and **Blocks**), R-UI-242 (the overview's **Place**) and R-UI-243
  (the egress addresses). W-35's switches go into the same section.
- **Follows** E8, E10, E13; the product rules of [CLAUDE.md](../../CLAUDE.md) (simple by default, a default is a value in the field, no view
  needs a reload, one scrollbar per page, every load shows the spinner) and rule 11 (renders in both schemes, wide and narrow).
- **Files**: `web/src/app/features/bastions/`, `limits/`, `usage/`, `api/`.
- **Tests**: Vitest about 4 s; the renders.
- **Identifiers**: R-UI-240 to R-UI-243, Q-UI-1480 to Q-UI-1489 (Q-UI-1480 to Q-UI-1482 decided in the specification).

### E13 · Egress addresses on the registration

- **Builds** migration 00145 (`bastion.egress`); recognition per registration in `internal/bastionmain/recognize.go` and in
  `bastionForwarded` ([wire_bastion.go](../../internal/app/wire_bastion.go)); the API member; `bastions.trusted_proxies` removed from
  `internal/config/bastions.go`.
- **Follows** E6. No interface change.
- **Tests**: unit; integration area `bastionreg` about 3 s.
- **Identifiers**: Q-BAS-334, H-2291.

### E14a · Anonymous requests answered without the main installation

- **Builds** the Bastion's anonymous token answer in `internal/bastion/engine/registry.go` (Q-BAS-335) and the anonymous documents kept for
  the metadata period in `readflow.go` (R-BAS-094). No interface change: the Bastion writes its token in payload version 1 with only `a` and
  `exp`, which `iface.RegistryTokenAnonymous` reads today, so E14a does not wait for E7.
- **Tests**: unit under 1 s; integration area `bastion`, one case of about 3 s.
- **Identifiers**: Q-BAS-335, H-2292.

### E15 · Writes refused at the Bastion

- **Builds** the refusal of R-BAS-095 in the four shapes; removes `internal/bastion/engine/probe.go` and the main installation's admission of
  probes (`BastionRouteAuth` in [bastion_services.go](../../internal/api/v1/bastion_services.go)); the kit's two duties in
  [checks_bastion.go](../../internal/formats/formattest/checks_bastion.go) with a `testformat` defect each.
- **Follows** E7.
- **Tests**: unit under 1 s; kit about 20 ms per format; integration `write_test` adjusted, so the area's time falls.
- **Identifiers**: Q-BAS-336, Q-BAS-337, H-2293.

### E16 · Nothing of the main installation in a Bastion's answers

- **Builds** rows 1 (the text of the main installation's backstop), 2 (the texts), 3, 4, 5, 6, 7, 9, 10 and 15 of R-BAS-099's table.
- **Follows** E2 + E3 (which touched `request.go`). It shares its subject with W-35's P8, which inventories the problem texts of a remote's
  fill the main installation relays and scans the web interface's answers; the integration scan is one recorder for both.
- **Files**: `internal/bastion/engine/respond.go`, `request.go`, `forward.go`; `internal/errs/problem.go`;
  `internal/api/bastionmark/mark.go`; `internal/limits/errors.go`.
- **Tests**: unit about 1 s; the assertion "no main host in any byte" in the existing replays of `test/integration/bastion`, about 0 s.
- **Identifiers**: Q-BAS-338, H-2294 to H-2296.

### E17 · The registry token's payload version 2

- **Builds** version 2 of R-OCI-005's payload (Q-BAS-339) in `internal/auth/registrytoken/token.go`; the verifier reads 1 and 2.
- **Follows** E7.
- **Tests**: unit; the existing docker and containerd replays pass unchanged.
- **Identifiers**: Q-BAS-339, H-2297.

H-2298 and H-2299 are spare.

## What every package follows

- The specification first: a deviation is decided and written back into the requirement and its decision in the same change (rule 4).
- The revised requirements carry a sentence *Today, until E… lands:*; the package that lands takes it out and adds its *Built* line, as the
  pages do elsewhere.
- Tests stay runnable ([CLAUDE.md](../../CLAUDE.md)): each behaviour once, at the cheapest tier; nothing per format beyond the kit's duties;
  no end-to-end leg. The cost in total is about 30 s on the integration shards.
- The Bastion's code imports no subsystem of the main installation (R-BAS-001, the archtest).

## What a builder reads first

- **E7**: [bastion.md](../spec/bastion.md#the-interface-version-8-the-shared-wire) (the wire), R-BAS-009 and R-BAS-057, then
  [wire.go](../../internal/bastion/iface/wire.go), [problems.go](../../internal/bastion/iface/problems.go),
  [headers.go](../../internal/bastion/iface/headers.go) and `testdata/interface.txt`; W-35's P5 (its catalogue routes and types).
- **E8**: R-BAS-093, R-BAS-096, R-LIM-043 to R-LIM-046 and Q-BAS-320, Q-BAS-325, Q-LIM-302, Q-LIM-304; then
  [registrations.go](../../internal/bastionmain/registrations.go), [handover.go](../../internal/bastionmain/handover.go) (the snapshot to
  follow), [feed.go](../../internal/bastionmain/feed.go), `internal/limits/index.go` and `model.go`, and migration 00110 (the revision
  trigger of a registration).

## Tests and what they cost

| Package | Tier | Data | Time |
|---|---|---|---|
| E7 | unit, interface guard | none | under 0.5 s |
| E8 | unit; integration `bastionreg`, `limits` | rows | 1.5 s; 3 s; 4 s |
| E9a | unit; chart test; integration `bastion` | none | 1 s; chart; 3 s |
| E9b | unit; integration `bastionshared`, `bastion` | bytes | 2 s; 2 s; 5 s |
| E10 | unit (injected clock); integration with Valkey | bytes | under 1 s; 3 s |
| E11 | unit | none | under 0.5 s |
| E12 | Vitest; renders | fixtures | about 4 s; renders |
| E13 | unit; integration `bastionreg` | rows | 3 s |
| E14a | unit; integration `bastion` | bytes | under 1 s; 3 s |
| E15 | unit; kit; integration `write_test` | bytes | under 1 s; 20 ms per format; falls |
| E16 | unit; the replays' scan | existing | 1 s; about 0 s |
| E17 | unit; existing replays | none | milliseconds |

With 25 formats the kit's two duties of E15 cost about 0.5 s in all; nothing else grows with the number of formats. The times are the
planner's estimates and are written down measured by each package.

## What still needs a real deployment

The volume running full and the eviction of an `emptyDir`; how HAProxy Ingress's `limit-rps` behaves over kept-alive connections; the main
installation's latency under an anonymous flood; a real crawl of W-35's pages; the capacity of the transfer network and the egress of the
object store.

## Not verified

- Whether a route of the main installation answers a forward with an absolute `Location` on its own host (R-BAS-099 row 6).
- Every attack figure on the specification's pages is derived from the defaults, not run.
- How docker, containerd, dotnet, apt and ansible-galaxy react to each of the four error shapes and to `503 busy`: E4 records it.
