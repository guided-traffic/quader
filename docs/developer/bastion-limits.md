# A Bastion's limits, configuration and throughput: the build plan

Status: specified on 2026-11-26 (W-34; the owner's answers to the working paper of W-34 and W-35, stored at 11:51). Packages E1, E2, E3
and E5 are **built** (the cache bounded at every commit and the bytes counted, R-BAS-089, R-BAS-090; the format's cap of the wait and the
figures that let one normal command through, R-BAS-091, R-BAS-092, interface version 7), and so are E16, E14a (the anonymous token and
the anonymous documents of R-BAS-094), E13 (the addresses a Bastion connects from), E7 (the shared wire, interface version 8, with
W-35's P5), E15 (writes refused at the Bastion in the five error shapes, R-BAS-095), E9a (the configuration at the Bastion, the flood protection's figures from it, the anonymous ceiling), E10 (the throughput, R-BAS-097), E11 (the cache protects what was served twice, R-BAS-098), E8 (the main installation's side), E9b (the budgets on what a Bastion serves,
its usage and its state, R-BAS-096) and E17 (the registry token's payload version 2, with W-35's P8, R-BAS-115); E4 is measured ([bastion.md](bastion.md#what-a-clients-command-costs-through-a-bastion-measured-2026-11-26)); E6 is this
page and the specification. Every other package is **not built**. The requirements are [bastion.md](../spec/bastion.md#limits-configuration-and-throughput-from-the-main-installation)
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
- **Today's code** is at interface version 8 (E7): the handshake carries `configuration`, since E8 the registration's own, which the Bastion applies
  whole since E9a (`settings.Holder`): the flood figures, the slots and the anonymous ceiling are no chart values any more; a write is refused at
  the Bastion itself since E15 ([write.go](../../internal/bastion/engine/write.go)), every answer of its own in the format's error shape
  ([shape.go](../../internal/bastion/engine/shape.go)); the main
  installation's egress list was `bastions.trusted_proxies` (`internal/config/bastions.go`) until E13 made it each registration's `egress`;
  the limits know three places
  (`limits.PlaceInstance`, `PlaceProject`, `PlaceRepository` in `internal/limits/model.go`).

## Work packages, in waves

At most four builders at once (rule 10 of the project plan). The waves are the order; within a wave packages run as places come free.

| Wave | Package | Builds | Follows | State |
|---|---|---|---|---|
| 1 | E1 + E5 | the cache below its size, the bytes counted (R-BAS-089, R-BAS-090) | — | built |
| 1 | E2 + E3 | the format's cap of the wait, interface version 7; the figures of one normal command (R-BAS-091, R-BAS-092) | — | built |
| 1 | E4 | the measurement, run once, not a test | — | measured ([bastion.md](bastion.md#what-a-clients-command-costs-through-a-bastion-measured-2026-11-26)) |
| 1 | E6 | this specification | — | written |
| 2 | E7 | the shared wire, interface version 8, with W-35's P5 | E6, E2, W-35's wire | built |
| 2 | E16 | nothing of the main installation in a Bastion's answers | E2 + E3 | built |
| 2 | E14a | anonymous requests answered without the main installation | E6 | built |
| 2 | E13 | egress addresses on the registration | E6 | built |
| 3 | E8 | the main installation's side | E7, W32, W36, E4 | built |
| 3 | E9a | the configuration at the Bastion, flood protection, the anonymous ceiling | E7, E4 | built |
| 3 | E10 | the throughput | E7 | built |
| 3 | E15 | writes refused at the Bastion | E7 | built |
| 4 | E9b | budgets on what the Bastion serves | E9a, E8 | built |
| 4 | E11 | the cache protects what was served twice | E1 (may take any free place from wave 2 on) | built |
| 4 | E17 | the registry token's payload version 2 | E7 | built (with W-35's P8) |
| 4 | E12 | the screens | E8, E10, E13 | built |

### E4 · The measurement, run once

- For each format the bytes, transfers and misses of one normal command through a cold and through a warm Bastion: `npm ci` (475 packages),
  `dotnet restore`, `docker` and `podman` pulls (a small image, an image of 1 GB, an image of 40 layers), `apt-get`, `ansible-galaxy`,
  `freshclam`, `curl`. The overflow attack, scaled to a cache of 2 GiB. The main installation's latency per check and per conditional
  request. What each client prints for a refusal in each of the four error shapes and for `503 busy`.
- The numbers are in [bastion.md](bastion.md#what-a-clients-command-costs-through-a-bastion-measured-2026-11-26), with the proposal for the
  figures of "Bastion: anonymous" and "Bastion: signed-in" (E8, R-LIM-046), the anonymous ceiling's default (E9a, R-BAS-094) and the
  throughput cap's (E10, R-BAS-097). The scripts are [hack/bastion-measure](../../hack/bastion-measure/README.md), never in the product.
  Measured on 2026-11-26.

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
- *Built on 2026-11-26, with W-35's P5.* Interface version 8 in [internal/bastion/iface](../../internal/bastion/iface): `configuration.go`
  (`Configuration`, `Validate`), `usage.go` (`UsageReport`, `ReplicaStatus`), `contact.go` (the two contact headers), `catalogue.go`
  (W-35's catalogue), the format table's `ErrorShape` (five shapes, `galaxy` from the measurement, Q-BAS-337) and `ReadWithBody`, the
  codes of Q-BAS-321, `SubjectLimits` and `HeaderLimits`, the readers of payload version 2. Each side's minimum: the main installation
  sends `bastionmain.InterimConfiguration` (Q-BAS-352, the registration's revision and handover, today's figures), reads the contact headers
  into `bastion.contact` (Q-BAS-354), refuses a batch whose usage or status breaks a bound, answers the catalogue routes by the switches
  (`bastion_catalogue_api.go`) and recognizes a probe by its form (`bastionmain.IsProbe`, Q-BAS-351); the Bastion's link validates the
  configuration and applies `switches.handover`, and its engine forwards a declared read route with a body at once. The formats declare
  `ErrorShape` and `ReadWithBody` (`catalog.Facts.Bastion`), and kit check 19 holds the declaration to the routes (Q-BAS-353). Read nowhere
  yet: `limits`, `switches.gui` and `switches.docs` at the Bastion, `subject.limits`, `usage`, `status`, `errorShape`. Tests: the
  interface guard and the wire's unit tests (`internal/bastion/iface`, 0.13 s), the routes (`internal/api/v1`, 0.2 s), the kit's two
  defects (`testformat`). Decisions Q-BAS-345 to Q-BAS-354, gaps H-2305 to H-2307, [ADR-303](../decisions.md#adr-303--the-public-catalogue-travels-on-the-bastion-interface-as-compressed-documents-under-a-change-list).
  *What E15 does with what E7 left:* remove `probe.go`, `bastionmain.IsProbe` and the admission of probes in `BastionRouteAuth`, refuse a
  write at the Bastion in the format's `errorShape`, and add kit check 19's duty that `WriteError` renders the declared shape. For Ansible
  Galaxy that is the `galaxy` list, also for a `429` on a collection's version list, which made ansible-galaxy 2.17.14 exit 250 against
  the Bastion of 2026-11-26: E15 renders it with `Retry-After` and the list's `code` and `detail`, and runs that client against the
  rendered `429` once to see it exit 1 and not 250.

### E8 · The main installation's side

- **Builds** migration 00147 (renumbered at landing: W-35's P6 took 00146): the configuration's columns on `bastion` and the revision trigger (also on `limit_assignment` rows of a
  Bastion place and on a profile such a row names, Q-BAS-320); the place `bastion` with `bastion_id` in `limit_assignment`; the two Bastion
  profiles and, for every registration, the Internet kind rules (R-LIM-046); the transient table `bastion_status`; the change area
  `bastion_configuration`. The place `PlaceBastion` and its resolution in `internal/limits/index.go` and `model.go`. `PATCH
  /api/v1/bastions/{name}` with `limits:write` for `limits` and `endpoints:write` for the rest (Q-BAS-325);
  `/api/v1/bastions/{name}/limits/assignments`; the usage API's place filter. A configuration snapshot after the pattern of
  `RegistrationSnapshot` ([snapshot.go](../../internal/bastionmain/snapshot.go)), which E13 made hold the networks too; the handshake member; the subject's limits in the check
  answer and the marking; the intake of `usage` into `usage.Buffer.Add` and of `status` into its table; `bastion.configuration.changed` in
  `feedActions` ([feed.go](../../internal/bastionmain/feed.go)).
- **Follows** E7, W32, W36 (migration 00143) and E4 (the profiles' figures).
- **Files**: `internal/bastionmain/registrations.go`, `feed.go`, `downloads.go`; `internal/api/v1/bastion_registrations_api.go`,
  `bastion_handshake_api.go`, `bastion_check_api.go`, `limits_api.go`, `usage_api.go`; `internal/limits`.
- **Tests**: unit about 1.5 s; integration area `bastionreg` about 3 s; area `limits` about 4 s.
- **Identifiers**: Q-BAS-323 to Q-BAS-325, Q-LIM-300 to Q-LIM-304, H-2282 to H-2284.
- *Built on 2026-11-26.* Migration 00147 ([database.md](database.md)): the registration's `gui`, `docs`, `limits_*` members with a new
  registration's values, the revision triggers on the registration, on a rule of its place and on a profile such a rule names, the place
  kind `bastion` with `bastion_id`, the profiles `bastion-anonymous` and `bastion-signed-in` (the measurement's proposal with the anonymous
  parallel transfers 270, [Q-LIM-310](../spec/usage-and-limits.md#q-lim-310--the-bastion-profiles-take-the-measurements-proposal-with-room-for-one-anonymous-restore-of-200-packages-at-once)),
  the Internet kind rules of every registration, `bastion_contact.edge_limit` and `catalogue_kept`, the transient `bastion_status`, the
  change area `bastion_configuration`.
  - *The configuration:* [configuration.go](../../internal/bastionmain/configuration.go) (`Limits`, `PlaceRule`, `Presets`,
    `DefaultLimits`, `EffectivePreset`, `Configuration`, `ValidateLimits`, `SubjectLimits`); the registration service writes the switches,
    the limits and a preset's kind rules in `Update` and records one `bastion.configuration.changed` ([registrations.go](../../internal/bastionmain/registrations.go));
    the snapshot of the registrations holds each registration's configuration from one statement (`BastionSnapshot`, `Configuration`,
    `RuleFigures`, `ReadConfiguration` in [snapshot.go](../../internal/bastionmain/snapshot.go), Q-BAS-440), reloaded by the areas
    `bastion_handover`, `bastion_egress`, `bastion_configuration` and `limits`; `InterimConfiguration` is gone, and the handshake answers
    `503` on a replica without the configuration's service.
  - *The place:* `limits.PlaceBastion`, `BastionPlace`, `Index.ResolveBastion` ([index.go](../../internal/limits/index.go)); the store reads
    and writes the place and records `bastion.configuration.changed` for a rule of a Bastion's place and for each registration a profile's
    change reaches (`bastionChanged` in [store.go](../../internal/limits/store/store.go), Q-BAS-441).
  - *The API:* `PATCH /api/v1/bastions/{name}` with `switches` and `limits`, declared `authenticated` and decided per member
    (`bastionPatchAllowed`, Q-BAS-325, Q-BAS-444); `GET` with `switches`, `limits`, `status` and the contacts' `edgeLimit` and
    `catalogueKept`; `/api/v1/bastions/{name}/limits/assignments` over the handlers of the other places ([limits_api.go](../../internal/api/v1/limits_api.go)
    `placeAt`); the usage API's `place` (`installation` or `bastion:<name>`, Q-LIM-313).
  - *The wire's content:* the check answer's `subject.limits` and the marking's `Quader-Bastion-Limits` from
    `App.bastionSubjectLimits` ([wire_bastion.go](../../internal/app/wire_bastion.go)), which reads no database; the intake stores the
    usage through the replica's usage buffer and the state in `bastion_status` ([report.go](../../internal/bastionmain/report.go)), after
    every refusal of the batch was decided, and its body bound is `max_batch` × 10 KiB + 100,000 × 640 bytes + 256 KiB, about 71 MiB
    (Q-BAS-442, H-2354); `bastion.configuration.changed` in `feedActions` maps to `settings`.
  - *The web client* follows the API's members (`switches.handover`, the place kind `bastion`) without a new screen: E12 builds those.
  - *Joined with E9a and W-35's P6:* a new registration's configuration is the Bastion's built-in one (`settings.Builtin`, Q-BAS-448:
    the slots' wait of 5 s, the ceiling) with the anonymous parallel transfers of Q-LIM-310 (270) on both sides;
    `TestR_BAS_093_ANewRegistrationIsTheBuiltInConfiguration` ([builtin_test.go](../../internal/app/builtin_test.go), the composition
    root, the one package that may import both sides) holds them to one another; `maintest.Configuration` is a copy of `settings.Builtin`
    with the revision 1 of a new registration (the infrastructure layer may not import `settings`), held to it by
    `TestR_BAS_093_TheTestDoubleSendsTheBuiltInConfiguration`, so the Bastion's tests run against what a main installation sends. The API refuses a
    failed-authentication window above 12 hours (`MaxFailedAuthWindowSeconds`), which a Bastion would clamp. The catalogue's builder and
    the handshake read a registration's stored `switches` (`App.bastionSwitches`; `app.Options.BastionSwitches` overrides `gui` and
    `docs` in tests only). [shapes.sh](../../hack/bastion-measure/shapes.sh) sets the anonymous flood figure through the registration's
    `PATCH`, since the chart no longer has it.
  - Decisions Q-BAS-440 to Q-BAS-444, Q-LIM-310 to Q-LIM-314; gaps H-2354 and H-2355 of [security/bastion.md](../security/bastion.md);
    H-2283 narrowed (Q-BAS-443). Tests: unit 0.4 s in `internal/bastionmain`, milliseconds in `internal/limits`, 0.1 s in
    `internal/api/v1` and `internal/api/bastionmark`; integration `bastionreg` (two cases, about 3 s and under a second) and `limits` (one
    case, about 4 s with the stack), rows only.

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
- *Built on 2026-11-26.* The package [internal/bastion/settings](../../internal/bastion/settings): `Holder.Apply` applies the handshake's
  member whole and atomically (one immutable `Applied`, Q-BAS-446), a request reads it once (`settings.With`, `Holder.For`), `LoadStored`
  and `Builtin` (Q-BAS-327), the metrics `quader_bastion_configuration_revision` and `_source`; the link hands every handshake's member to
  it. The guard reads its figures through `Options.Figures` and has the class `gui` (`Page`, Q-LIM-307); `SlotBudget.Resize`; the ceiling
  `guard.Ceiling`, taken in the slot budget after the slot by every request of the anonymous class, the background refresh included
  (Q-BAS-445, `mainclient.CeilingError`, `reason="anonymous_ceiling"`); the stored object `configuration/current.json`
  (`cache.Cache.StoreConfiguration`, signed in the domain `configuration`, sealed with a cache key) applied by `Server.Start` before the client
  listener binds; a Valkey limiter whose figures change is replaced under its name (`cluster.LimiterSet.SetLimiter`, Q-BAS-449). The chart
  lost `config.bastion.limits`, `failed_auth`, `blocking` and `main.max_concurrent`, `anonymous_share`, `queue_timeout` (no alias),
  `_validate.tpl`'s edge check, and gained `edgeLimit.exemptCIDRs` and the chart-managed `bastion.edge_limit`, which every handshake reports
  (Q-BAS-447). The defaults (Q-BAS-448): ceiling 100 a second, burst 2,000, 36,000 an hour; slots 64, 0.25 and a wait of 5 s; the reasoning
  with what each client's cold command gets is in R-BAS-094 and R-BAS-096. Decisions Q-BAS-445 to Q-BAS-449, gaps H-2356 and H-2357.
- *What E9a left:* the status of the reporting batch (`ReplicaStatus`, which the wire's text gave to "E8 and E9a"): no package sends it yet;
  E12's live state needs it, and E9b's flush is where it is cheapest to add (E8's intake takes it). Since E8 a registration's configuration
  holds the ceiling and the 5 s wait (H-2357 closed), and a change reaches a Bastion through the feed's `settings` entry; the integration
  case still takes the 5 s handshake of the harness. `mode`, `preset` and `place` are read by nothing yet (E9b); `throughput` since E10.

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
- *Built on 2026-11-26.* The package [internal/bastion/budget](../../internal/bastion/budget/budget.go): `SubjectOf` (the answer's key and
  rule, else the users' kind rule under the principal, Q-BAS-470; an anonymous caller by its address prefix and the place's address or kind
  rule, `settings.Place.Anonymous`), `Charge` with `Request` (a block, the request rate per rule, the hour), `Upstream`, `Transfer` (the hour
  of its bytes, the day, the lease that waits 10 s while the waiting leases hold at most a quarter of `server.max_in_flight`), `Pace` (slices
  of 100 ms of the subject's figure, never below 64 KiB a second per transfer, [pace.go](../../internal/bastion/budget/pace.go)), `Served`,
  `Refused` (the block of Q-BAS-472) and `End`; Audit decides in `decide`. The usage buffer is [internal/bastion/usage](../../internal/bastion/usage/usage.go)
  (Q-LIM-308). The engine's glue is [budget.go](../../internal/bastion/engine/budget.go) (`stepBudget` after `stepCredentials`, `learnSubject`
  in `recordAnswers` and `applyCheck`, `chargeUpstream` in `forward` and `checkShape`, `admitTransfer` in `serveSource`, `serveFill` and
  `passThrough`, the bytes and the pace in `countingWriter`, `endCharge` in `rq.end`, `refuseBudget` through the format's shape). The
  counters are `shared.State.Budget`, the throughput's counter type in a Valkey key family of its own, `budget` (`cluster.CountersBudget`,
  up to 50 hours); the local counters now keep the expiry they were made with, as Valkey's `EXPIRE NX` does. The settings gained
  `Applied.Enforce` and `Applied.Place`; the server builds the budgets and `reporting` ([status.go](../../internal/bastion/server/status.go)),
  which the download reporter calls with every flush (`ReporterOptions.Report`, `Restore`): the usage and the replica's state with the
  throughput's live state (`throughput.Pacer.Status`), the blocks the guard and the budgets set (`guard.Instance.Blocks`), each with a
  reason of the closed list (`refusals`, `failed_auth`, `refused_requests`), and per figure whose it is (Q-BAS-474). No interface change:
  the codes of R-LIM-020 are the budget package's, the wire's members were E7's. Metrics `quader_bastion_limits_mode`,
  `quader_bastion_budget_events_total{figure,mode}`, `quader_bastion_refused_total{reason="budget"}`, `quader_bastion_blocks_total` with
  `failed_auth` and `refused_requests`. Decisions Q-BAS-470 to Q-BAS-474; gaps H-2368 and H-2369 of
  [security/usage-and-limits.md](../security/usage-and-limits.md#h-2368--at-the-turn-of-an-hour-a-subject-can-hold-up-to-twice-its-parallel-transfers-at-a-bastion).
  Tests: `internal/bastion/budget` (eight cases, 0.3 s), `internal/bastion/usage` (two, milliseconds), `internal/bastion/engine` (three,
  0.05 s), `internal/bastion/downloads` (two, 2 s with the race detector, most of it the flush loop), `internal/bastion/server` (one,
  milliseconds); integration `bastionshared` `TestR_BAS_096_WithValkeyTheReplicasShareTheBudgets` (0.2 s) and `bastion`
  `TestR_BAS_096_ASubjectOverItsBudgetIsRefusedInItsFormatsShapeAndItsUsageAndStateReachTheMainInstallation` (3.7 s with its world).
- *Measured on 2026-11-26 for E9a's open point*: a warm anonymous `docker pull` through a Bastion still sends the tag's `HEAD` to the main
  installation, once per pull, whatever the address; E18 answers it from the cache since
  ([developer/bastion.md](bastion.md#a-head-of-a-document-answered-from-the-cache)).

### E10 · The throughput

- **Builds** one pacing reader on every body the Bastion reads (fills, pass-through, the handover's direct read, forwarded documents, W-35's
  catalogue pull) with two buckets per registration (Q-BAS-332), a shared byte bucket in Valkey and the replica's share without; the answers
  at the cap of R-BAS-097 and its metrics.
- **Files**: `internal/bastion/engine/fill.go`, `forward.go`, `handover.go`; `internal/bastion/shared/`.
- **Tests**: unit with an injected clock, under 1 s; integration, two replicas sharing one cap through Valkey, about 3 s.
- **Identifiers**: Q-BAS-331, Q-BAS-332, H-2289.
- *Built on 2026-11-26.* The package [internal/bastion/throughput](../../internal/bastion/throughput/throughput.go) (`Pacer`, the paced
  reader) over the byte counters `shared.Bytes` ([bytes.go](../../internal/bastion/shared/bytes.go)) and, with Valkey,
  `cluster.BastionStore.ClaimBytes`, `ReleaseBytes` and `AddBytes` ([valkey_bastion_bytes.go](../../internal/cluster/valkey_bastion_bytes.go),
  the key family `throughput`); the files the plan named changed less than planned: the client of the main installation admits and paces
  every forward in one place (`mainclient.Throughput`, `HTTPClient.Do`), so `fill.go` and `forward.go` only keep the cap's wait out of the
  stall bound and the floor, and `handover.go` paces the direct read. The default of 200 MB/s with an anonymous share of 25 % is the
  built-in configuration's and, since E8, a new registration's (Q-BAS-455, a revision of R-BAS-097's "unlimited").
  A miss at the cap is refused at once with the wait until its first bytes instead of after the queue time (Q-BAS-457).
  W-35's pull has `Pacer.CatalogueReader` to call; nothing calls it yet. The replica's status in the reporting batch
  (`iface.ThroughputStatus`) is still sent by no package (E9b's flush, E12's live state). Decisions Q-BAS-455 to Q-BAS-459, gaps H-2360 and
  H-2361 of [security/bastion.md](../security/bastion.md#h-2360--without-valkey-the-throughputs-replicas-hold-their-shares-as-bastionreplicas-says).
  Tests: [throughput_test.go](../../internal/bastion/throughput/throughput_test.go) (seven cases, 0.3 s, a clock the test drives),
  the client's admission (0.02 s), the engine's refusals and a paced direct read (0.03 s), and `bastionshared`'s
  `TestR_BAS_097_WithValkeyTheReplicasShareOneThroughputCap` (2.3 s).

### E11 · The cache protects what was served twice

- **Built** on 2026-11-26: the sweep's order of R-BAS-098 for the volume, the bucket and the encrypted layout. The access marker is the
  protection, because a fill writes none (Q-BAS-460); the 80 % are a rank in each sweep (Q-BAS-461); served twice on a bucket is a fill and
  a serve from the bucket by any replicas (Q-BAS-462); `quader_bastion_cache_protected_bytes{area}` (Q-BAS-463); a replica's commit lets the
  next serve write the marker again (Q-BAS-464). No new marker, family or listing on the stores; `layout.go` is unchanged.
- **Files**: `internal/bastion/cache/core.go` (`Sweep`, `rankProtected`), `cache.go`, `store.go`, `fill.go`, `sweep.go`;
  `internal/bastion/metrics/metrics.go`.
- **Tests**: unit, `protect_test.go`, five tests, 0.9 s on a loaded machine (the encrypted layout half of it).
- **Measured**: the flood of E4 again, before and after, with a warm `npm ci` beside it
  ([bastion.md](bastion.md#the-flood-again-with-the-protection-of-what-was-served-twice-measured-2026-11-26)).
- **Identifiers**: Q-BAS-333, H-2290; Q-BAS-460 to Q-BAS-464, H-2362.

### E12 · The screens

- **Builds** R-UI-240 (the card's section **Limits**, its **Details**, the live state, the edge warning), R-UI-241
  (`/admin/bastions/{name}/limits` with `limit-rules.ts` and `rule-dialog.ts`, and **Blocks**), R-UI-242 (the overview's **Place**) and R-UI-243
  (the egress addresses). W-35's switches go into the same section.
- **Follows** E8, E10, E13; the product rules of [CLAUDE.md](../../CLAUDE.md) (simple by default, a default is a value in the field, no view
  needs a reload, one scrollbar per page, every load shows the spinner) and rule 11 (renders in both schemes, wide and narrow).
- **Files**: `web/src/app/features/bastions/`, `limits/`, `usage/`, `api/`.
- **Tests**: Vitest about 4 s; the renders.
- **Identifiers**: R-UI-240 to R-UI-243, Q-UI-1480 to Q-UI-1489 (Q-UI-1480 to Q-UI-1482 decided in the specification, Q-UI-1483 and
  Q-UI-1484 by E13, which built R-UI-243's section on the card).
- **Built** on 2026-11-26. The card's section `<quader-bastion-limits>` ([bastion-limits.ts](../../web/src/app/features/bastions/bastion-limits.ts))
  with the state of [bastion-state.ts](../../web/src/app/features/bastions/bastion-state.ts), the presets' words and their comparison in
  [presets.ts](../../web/src/app/features/bastions/presets.ts) (the table itself is the server's, `GET /api/v1/bastions/presets` since
  the day's findings of 2026-11-26, Q-UI-1485), the preset's dialog, the details' form
  ([bastion-limit-details.ts](../../web/src/app/features/bastions/bastion-limit-details.ts)), the switches
  ([bastion-switches.ts](../../web/src/app/features/bastions/bastion-switches.ts); since W35-P9f with the line of the catalogue's last
  build, and the warning of a stop's reason, both from `catalogueLine` of `bastion-state.ts` over the registration's
  `catalogue`, Q-BAS-497; since B-1127h not in **Details** but a section of the card's own between the direct reads and **Limits**,
  `bastion-card.ts` placing it, with the stop's reason in place of the line and the warning of a catalogue a restart loses,
  [Q-UI-1616](../spec/api-and-ui.md#q-ui-1616--a-bastions-web-interface-is-a-section-of-the-cards-plain-view-not-a-detail-of-its-limits)) and `<quader-bastion-egress>` in two parts (warnings on the card, the list in **Details**); `BastionsApi.configure`, a merge patch of `switches` and `limits`; the mode's dialog of
  [limit-mode.ts](../../web/src/app/features/limits/limit-mode.ts) for a Bastion; the page `/admin/bastions/{name}/limits`
  ([bastion-limits-page.ts](../../web/src/app/features/bastions/bastion-limits-page.ts)) with `orderBastionRules` of `limit-rules.ts`; the
  overview's **Place** and **By place** of a caller's page and of the profile's own usage ([usage-places.ts](../../web/src/app/features/usage/usage-places.ts)). Decisions
  Q-UI-1485 to Q-UI-1489 and Q-UI-1593; gaps H-2364, H-2365 (both in api-and-ui.md). With E9b's report the state line takes the cache's fill,
  capacity and protected bytes from one replica (the bucket backend reports the whole Bastion's cache in each; *since E19* it adds up the
  replicas whose `cache.shared` is false and takes the shared one once) and names the block reasons
  of its closed list (`refusals`, `failed_auth`, `refused_requests`) in words, on the card and on **Blocks** (`blockReasonText`). A
  declared registration (R-BAS-127) shows the section as every other card; its addresses are text there as well. Tests: Vitest, 19 cases in `bastions.spec.ts`,
  `bastion-limits-page.spec.ts` and `usage.spec.ts`, about 2.5 s together, fixtures only.

### E13 · Egress addresses on the registration

- **Builds** migration 00145 (`bastion.egress`); recognition per registration in `internal/bastionmain/recognize.go` and in
  `bastionForwarded` ([wire_bastion.go](../../internal/app/wire_bastion.go)); the API member; `bastions.trusted_proxies` removed from
  `internal/config/bastions.go`.
- **Follows** E6. No interface change.
- **Tests**: unit; integration area `bastionreg` about 3 s.
- **Identifiers**: Q-BAS-334, H-2291.
- **Built on 2026-11-26.** Migration 00145; `ParseEgress` ([egress.go](../../internal/bastionmain/egress.go)); the snapshot of the
  registrations holds the networks ([snapshot.go](../../internal/bastionmain/snapshot.go), the change area `bastion_egress`);
  `Recognition.FromEgress` and `RecognizeOptions.Egress`; `bastionForwarded` over the request's own registration
  (`Resolver.AlsoTrusting`); the check's `clientAddress` only from the networks; the member `egress` of the API; the card's section
  `<quader-bastion-egress>` (R-UI-243, which E12 moves into **Details**); `bastions.trusted_proxies` removed, the e2e registrations set
  the pod range. Decisions Q-BAS-430 to Q-BAS-433, Q-UI-1483 and Q-UI-1484 (taken from E12's range); gaps H-2291 (narrowed), H-2348 and
  H-2349.

### E14a · Anonymous requests answered without the main installation

- **Builds** the Bastion's anonymous token answer in `internal/bastion/engine/registry.go` (Q-BAS-335) and the anonymous documents kept for
  the metadata period in `readflow.go` (R-BAS-094). No interface change: the Bastion writes its token in payload version 1 with only `a` and
  `exp`, which `iface.RegistryTokenAnonymous` reads today, so E14a does not wait for E7.
- **Tests**: unit under 1 s; integration area `bastion`, one case of about 3 s.
- **Identifiers**: Q-BAS-335, H-2292.
- *Built on 2026-11-26.* As planned, with no interface change: `answerAnonymousToken` in
  [registry.go](../../internal/bastion/engine/registry.go) and the dispatch of an anonymous token request in `request.go` (`core`); the
  anonymous copy in [readflow.go](../../internal/bastion/engine/readflow.go) (`fromAnonymousCopy`), judged by `freshFor` in `entries.go`,
  which a cache entry's `fresh` uses as well; `onPrivate` in `forward.go` keeps the anonymous copy also without an `ETag`. No metric was
  added: the Bastion's own token answers count as `quader_bastion_requests_total{class="pass",result="hit"}` and a kept anonymous document as
  `{class="private",result="hit"}`, neither of which is a `quader_bastion_main_requests_total`. Decisions Q-BAS-435 to Q-BAS-437 of
  [spec/bastion.md](../spec/bastion.md#q-bas-435--the-anonymous-token-is-the-anonymous-caller-at-every-replica-and-every-bastion-and-no-key-signs-it);
  gaps H-2350 and H-2351 of [security/bastion.md](../security/bastion.md#h-2350--anyone-can-mint-an-anonymous-registry-token-of-a-bastion).
  Tests: the engine's `TestR_BAS_094_TheBastionAnswersAnAnonymousTokenRequestItself` and
  `TestR_BAS_094_AnAnonymousDocumentIsKeptForTheMetadataPeriod` (about 0.05 s together, documents of bytes) and the area `bastion`'s
  `TestR_BAS_021_R_BAS_094_AnAnonymousPullAndAnAnonymousDocumentSpareTheMainInstallation` (about 7 s with its world, an image of two blobs of 1 KiB
  and one npm document).

### E15 · Writes refused at the Bastion

- **Builds** the refusal of R-BAS-095 in the five shapes; removes `internal/bastion/engine/probe.go` and the main installation's admission of
  probes (`BastionRouteAuth` in [bastion_services.go](../../internal/api/v1/bastion_services.go)); the kit's two duties in
  [checks_bastion.go](../../internal/formats/formattest/checks_bastion.go) with a `testformat` defect each.
- **Follows** E7.
- **Tests**: unit under 1 s; kit about 20 ms per format; integration `write_test` adjusted, so the area's time falls.
- **Identifiers**: Q-BAS-336, Q-BAS-337, H-2293.
- *Built on 2026-11-26.* As planned: `write` in [write.go](../../internal/bastion/engine/write.go) refuses a write `403 read_only` before
  its body or its credential is read and asks the main installation nothing (the anonymous `POST /v2/_token` is answered before it);
  `writeShaped` in [shape.go](../../internal/bastion/engine/shape.go) renders every answer of the engine's own in the declared shape;
  `errs.ReadShape` ([shape.go](../../internal/errs/shape.go)) reads each shape as its clients do, for the engine's tests and the kit; kit
  check 19's duty that `WriteError` renders the declared shape on the backstop's refusal, with the `testformat` defect
  `DeclaresAnotherErrorShape`; `probe.go`, `mainclient.Forward.Probe`, the kind and the class `probe`, `bastionmain.IsProbe`,
  `Recognition.Probe` and the probes' admission in `BastionRouteAuth` are gone. Beyond the plan: the run of ansible-galaxy 2.17.14
  against the `galaxy` shape still exited 250 on a `429` of the version list, because of the client's response cache, not the body
  ([bastion.md](bastion.md#the-galaxy-shape-against-ansible-galaxy-217-measured-2026-11-26)); Ansible Galaxy therefore answers a rate
  limit `503` (`RateLimitedStatus`, its `WriteError`), with which it exits 1 and prints the detail and the code, and kit check 16 takes
  503 as a rate limit's status; npm's shape carries the code too. Tests: the engine's `TestR_BAS_095_AWriteIsRefusedAtTheBastionInTheFormatsShape`
  (0.03 s, one body of 1 MiB that nobody reads), `TestR_BAS_095_EachShapeRenders` (under 0.01 s) and
  `TestR_BAS_095_ARouteThatTakesABodyIsForwardedWithTheCallersCredential`; the kit's duty under a millisecond per format; the area
  `bastion`'s `TestR_BAS_095_WritesAreRefusedAtTheBastion` (one Bastion instead of two, no loop of 1,000 writes). Decisions Q-BAS-450 to
  Q-BAS-453 of [spec/bastion.md](../spec/bastion.md#q-bas-450--every-answer-of-the-bastions-own-goes-through-one-renderer-by-the-declared-shape-and-each-shape-carries-the-code);
  gaps H-2358 and H-2359 of [security/bastion.md](../security/bastion.md#h-2358--a-502-on-a-document-ansible-galaxy-caches-ends-the-client-with-exit-250).

### E16 · Nothing of the main installation in a Bastion's answers

- **Builds** rows 1 (the text of the main installation's backstop), 2 (the texts), 3, 4, 5, 6, 7, 9, 10 and 15 of R-BAS-099's table.
- **Follows** E2 + E3 (which touched `request.go`). It shares its subject with W-35's P8, which inventories the problem texts of a remote's
  fill the main installation relays and scans the web interface's answers; the integration scan is one recorder for both.
- **Files**: `internal/bastion/engine/respond.go`, `request.go`, `forward.go`; `internal/errs/problem.go`;
  `internal/api/bastionmark/mark.go`; `internal/limits/errors.go`.
- **Tests**: unit about 1 s; the assertion "no main host in any byte" in the existing replays of `test/integration/bastion`, about 0 s.
- **Identifiers**: Q-BAS-338, H-2294 to H-2296.
- *Built on 2026-11-26.* As planned, and beyond the list of files: the reader of a request's problems is a mark on its context
  (`errs.WithBastionAnswer`, `errs.WithBastionForward`, read by `errs.ProblemFor`), set by the Bastion's client chain and engine and by
  the Recognize stage ([recognize.go](../../internal/bastionmain/recognize.go)); the format error shapes of npm, NuGet, OCI and Ansible
  Galaxy render through `errs.ProblemFor`, and kit check 19 holds every format to it; npm declares the replay header `Npm-Notice`; row 15
  is closed at rendering by the code of the remote engine's failures, not in `classify.go` (Q-BAS-426); the metric reason
  `foreign_location`. Decisions Q-BAS-425 to Q-BAS-428 of [spec/bastion.md](../spec/bastion.md#q-bas-425--the-headers-of-a-forwarded-answer-pass-an-allowlist-and-a-formats-need-is-a-replay-header).

### E17 · The registry token's payload version 2

- **Builds** version 2 of R-OCI-005's payload (Q-BAS-339) in `internal/auth/registrytoken/token.go`; the verifier reads 1 and 2.
- **Follows** E7.
- **Tests**: unit; the existing docker and containerd replays pass unchanged.
- **Identifiers**: Q-BAS-339, H-2297.
- *Built on 2026-11-26, as one package with W-35's P8* (R-BAS-114, R-BAS-115). As planned, and beyond it: the sealed part is AES-256-GCM
  under a key derived with the info `quader registry token seal v2`, the opaque key ID an HMAC of the ring key's ID under its MAC key
  (Q-BAS-388); a scan or blob token keeps `s` readable and the verifier reads version 1 until its tokens expire (Q-BAS-389); the
  replays' scan of `test/integration/bastion` decodes every registry token a Bastion relays. Tests:
  `TestR_BAS_115_ARegistryTokenReadsOnlyItsAnonymityExpiryAndGrant` (unit, milliseconds) and the scan (about 0 s more). The identifiers
  of the joint package are W-35's (R-BAS-114, R-BAS-115, Q-BAS-385 to Q-BAS-389, H-2322, H-2323, ADR-304); H-2297 is updated.

### E18 · A HEAD of a document answered from what the Bastion keeps

- **Builds** E9b's open point: a `HEAD` of a document is answered from the kept entry or copy, a `HEAD` miss marked `shared` or `private`
  within the memory bound is followed by the `GET` that keeps it, and the conditional request of a `HEAD` of a kept document is a `GET`;
  for every format, without an interface change (R-BAS-021, R-BAS-023).
- **Files**: [headfill.go](../../internal/bastion/engine/headfill.go) (new), `forward.go` (`ask` split from `send`, `sentMethod`),
  `readflow.go` (`conditional`), `request.go` (`rq.asGet`).
- *Built on 2026-11-26.* Tests: six unit tests in `internal/bastion/engine` (under 0.1 s together, documents of a few bytes) and Docker's
  sequence from two addresses in `TestR_BAS_021_R_BAS_094_AnAnonymousPullAndAnAnonymousDocumentSpareTheMainInstallation` (area `bastion`,
  about 0.15 s more, one more tag of the same manifest). What it costs and what was measured:
  [developer/bastion.md](bastion.md#a-head-of-a-document-answered-from-the-cache). Decisions Q-BAS-475 to Q-BAS-478 of
  [spec/bastion.md](../spec/bastion.md#q-bas-475--a-head-of-a-document-is-answered-from-what-the-bastion-keeps); gaps H-2374 and H-2375 of
  [security/bastion.md](../security/bastion.md#h-2374--a-head-makes-the-bastion-read-a-document-its-caller-never-receives).

### E19 · Interface version 9: a fill for a HEAD is no download, and a replica says whether its cache is shared

- **Builds** the two gaps of E18 and E12 that needed a member of the interface, in one raise from 8 to 9
  ([the wire](../spec/bastion.md#the-interface-version-9-a-head-is-no-download-and-a-replica-says-whose-cache-it-reports)): the request header `Quader-Bastion-For-Head` of every `GET` a Bastion sends for a `HEAD`, which the
  main installation answers as the `GET` but records and charges as the `HEAD` on a document's route (H-2375 closed), and the status's
  `cache.shared`, with which the card adds up the caches of their own and takes the shared bucket once (H-2364 closed).
- **Files**: `iface` (`headers.go`, `usage.go`, `version.go`, the surface file), `mainclient` (`Forward.ForHead`), `engine/forward.go`,
  `server/status.go` and `server.go`; at the main installation `bastionmain` (`Recognition.ForHead`, `ForHead`, `StatesForHead`,
  `Absence.NoteRoute`), `api/bastionmark/mark.go`, `items` (`Options.NoDownload`), `limits` (`Charge.NoDownload`), `api/v1/ratelimit.go`,
  `app/wire_files.go`; the OpenAPI document's status members and interface annotations; `web/.../bastion-state.ts`.
- *Built on 2026-11-26.* Tests: unit, seven cases in `internal/bastion/engine`, `internal/bastion/mainclient`, `internal/bastion/server`,
  `internal/bastionmain`, `internal/items` and `internal/limits` (milliseconds each, no data beyond a few bytes); integration
  `TestR_BAS_059_DockersHeadOfATagIsNoDownloadAtTheMainInstallation` (area `bastion`, about 3.5 s, an image of two blobs of 1 KiB); Vitest,
  the card's volume case beside the bucket case (one case more, a fraction of a second, fixtures only). Decisions Q-BAS-479 to Q-BAS-482 of
  [spec/bastion.md](../spec/bastion.md#q-bas-479--a-get-a-bastion-sends-for-a-head-says-so-in-a-header-of-the-request); gap H-2370 of
  [security/bastion.md](../security/bastion.md#h-2370--a-bastion-token-takes-a-counted-documents-download-out-of-the-count).

H-2298 and H-2299 are spare and stay unused.

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
| E9a | unit; chart test; integration `bastion`, `bastionshared` | none to bytes | 1 s; chart; 9 s (two cases); 0.2 s (measured: see E9a's entry) |
| E9b | unit; integration `bastionshared`, `bastion` | bytes | 2.5 s (measured, the reporter's 2 s of it); 0.2 s; 3.7 s (measured) |
| E10 | unit (injected clock); integration with Valkey | bytes to 300 KB | 0.35 s; 2.3 s (measured: see E10's entry) |
| E11 | unit | none | 0.9 s measured (planned under 0.5 s) |
| E12 | Vitest; renders | fixtures | about 4 s; renders |
| E13 | unit; integration `bastionreg` | rows | 3 s |
| E14a | unit; integration `bastion` | bytes | 0.05 s; 7 s with its world (measured) |
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
- How docker, containerd, dotnet, apt and ansible-galaxy react to each of the four error shapes and to `503 busy`: E4 records it;
  ansible-galaxy 2.17 against the fifth, `galaxy`, E15 (measured).
