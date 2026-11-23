# Errors and troubleshooting

What Quader answers when a request fails and what to do about it. The page explains the shape of an error answer and its stable error code, how to pass the request ID to an administrator, what each HTTP status means in Quader, the typical problems of npm, pnpm and Yarn through Quader, what happens when your session ends, and closes with a reference of the error codes you can meet.

## How Quader reports an error

Every error answer carries a **status**, a stable **code** in snake_case and a sentence that explains it. The code is the part to search for and to quote: it never changes its meaning within a major version.

### The management API and file transfer

The management API (`/api/v1/…`, which the UI uses) and file transfer (`/files/…`) answer with problem details of [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) (`Content-Type: application/problem+json`). An example:

```json
{
  "type": "/api/v1/problems/not_found",
  "title": "Not found",
  "status": 404,
  "detail": "The resource does not exist.",
  "instance": "/files/web/generic-local/tools/app-1.2.0.tar.gz",
  "code": "not_found",
  "requestId": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

A refused field of a request comes with `errors`, each with a JSON pointer to the field. `GET /api/v1/problems/<code>` describes any code; it needs no sign-in.

### Package clients

npm repositories answer in npm's own shape, `{"error": "…", "message": "…", "code": "…"}`. npm prints the status as `E401`, `E404` and so on, followed by the text; pnpm and Yarn print the message. The code is in the body; the client may not show it.

## Give the request ID to an administrator

Every answer carries the header `X-Request-Id`, 32 hexadecimal digits; a problem body repeats it as `requestId`. An administrator finds the request in the logs with it.

- In the UI, an error whose code the UI has no sentence for shows **Request ID** with a **Copy** button and the hint "Give this ID to your administrator."
- With curl, `--include` prints the headers of the answer, `X-Request-Id` among them.
- A package client does not show the header. Give the administrator the time, the address the client called, the command and your user name instead; the request log records each request with its path, status and caller.

## Status codes

| Status | Meaning in Quader | What to do |
|---|---|---|
| `401` | No credential where one is needed (`credentials_required`), a token that is malformed, unknown, expired or revoked (`invalid_token`, the answer does not say which), a disabled owner (`principal_disabled`), or an ended session | Set the token, create a new one, or sign in again |
| `403` | The credential is valid but lacks the permission for a write or an operation (`forbidden`), or a rule refuses it: package owners, the shadowing check, a read-only endpoint | Ask for the permission; the code names the rule |
| `404` | The resource does not exist **or is hidden from you**. A refused read answers like a missing resource, so that nobody learns what exists, unless an administrator turned this off. On an endpoint with exposure rules, everything the rules do not expose is missing too | Check the address, with the project key and the repository key, and the name; ask an Owner of the repository's project whether you may read it |
| `405` | The repository class takes no writes: a remote repository (`npm_class_not_writable`) or a virtual repository without a deployment target (`no_deploy_target`) | Publish to a local repository or through a virtual one with a target |
| `409` | A conflict with the current state: a version that exists or was unpublished, a stale revision, a path that is a folder, the token limit | Read the code; often publish a new version or retry |
| `412` | A condition of the request does not hold: a resource changed since you loaded it (`If-Match`), or an upload with `If-None-Match: *` to a path that exists | In the UI, resolve the conflict dialog; elsewhere read again and decide |
| `421` | The host name of the request is not one the endpoint answers for (`misdirected_request`) | Use the address from the client setup or the guide, not an IP address or another host name |
| `429` | Too many requests (`rate_limited`): the rate limit of the API or the endpoint, or too many rejected credentials from your address | Wait the seconds of `Retry-After`; fix a wrong token first, because every rejected attempt counts |
| `503` | Temporarily unavailable: the replica is busy or stopping, the database or an upstream is not reachable, a check could not run. Always with `Retry-After` | Retry after that period; another replica may answer |

Further statuses you may meet: `400` and `422` for a malformed or invalid request (the answer names the field), `413` for an upload above a limit, `501` for an operation of a protocol that Quader does not implement, and `502` and `504` when a remote repository's upstream fails for content that is not cached.

The UI repeats a read once by itself after `429`, waiting for `Retry-After` up to 30 seconds. npm repeats a failed `GET` and `PUT` itself.

## Typical npm problems

These apply to npm, pnpm, Yarn Berry and Yarn Classic alike. The guide's **Troubleshooting** section lists the codes of the npm protocol for your client.

### E401 although a token is configured

- `QUADER_TOKEN` is not set in the environment the client runs in, or is empty.
- The credential line does not name this repository's address exactly, with its trailing slash.
- Yarn Classic needs `always-auth=true` and Yarn Berry `npmAlwaysAuth: true` for a repository that is not public.
- The token is expired, revoked, unknown or mistyped: `invalid_token`. [Create a new token](access-tokens.md#create-a-token).
- The token works on the main address but not on another endpoint, such as one for customers: further endpoints accept only service-account tokens bound to them unless configured otherwise.
- The owner of the token is disabled: `principal_disabled`.

Quader never asks a client for a one-time password.

### E404 for a package that exists

A `404` with `not_found` has several causes, and Quader does not say which, by design:

- The version was unpublished, or the upstream registry withdrew it; this also fails an install from a lockfile.
- A **namespace claim** reserves the name for a local repository: a virtual repository then takes the name from that local repository only and never asks its remote members, so the answer is `404` while the local repository lacks the version. A claim for the whole installation also makes every remote repository refuse the name.
- The package lives in a member of the virtual repository that you cannot read. A token limited to the virtual repository reads none of its members.
- An exposure rule of the endpoint you use hides the package or version, or the repository's name patterns refuse the name.
- A remote repository is offline and the package is not cached (`not_cached_offline`).
- The virtual repository lists a public registry of the project `upstream` that is not in use: an administrator of Quader has not
  switched it on yet, has switched it off, or Quader no longer offers it. Here the error says so, in one sentence such as
  "upstream/npmjs is a member of this virtual repository, but an administrator of Quader has not switched it on." The name resolves
  through the registry as soon as an administrator switches it on, with no change to your configuration; until then, ask an
  administrator or publish the package to a local member. You are not told this for a registry you could not read once it is on.
- The registry address lacks the project key (`/api/npm/<project>/<key>/`), or the repository was moved to another project and has a new address.
- The command is not implemented: `npm deprecate`, npm's token and profile commands.

### E403 when publishing

| Code | Cause | Fix |
|---|---|---|
| `forbidden` | Publishing and dist-tags need deploy, unpublishing delete | Ask for the permission, or use a token whose scope includes it |
| `package_owned_by_other_owner` | The package has owners and you are none of them | An owner or a holder of manage adds you |
| `shadows_remote_package` | The first publish of this name would replace, in every virtual repository that contains the local repository, a package that a remote member serves | A holder of manage on the package publishes it, or claims the name first; later versions are never checked |
| `endpoint_read_only` | A write on an endpoint with exposure rules | Publish through the default endpoint |
| `populate_not_permitted` | You may read the remote repository but not bring new packages into its cache | Ask for that permission, or have the package fetched once by an account that holds it |

When Quader cannot ask the remote member whether it serves the name, the first publish answers `503` `shadow_check_unavailable` and stores nothing; retry later.

### A download is blocked (E403 on install or restore)

When Quader refuses to hand out a version it answers `403` and says why. npm prints the text after `E403`; `dotnet restore` shows the status of the `.nupkg` address, and `curl` on that address prints the one-line reason. The text names the package and version, the repository you used, the reason and a link to the page that explains it, for example `Quader blocked quader-test-mal@1.0.0 in web/npm-all: malicious package (MAL-2026-1234). Details: https://quader.example.com/ui/vulnerabilities/findings/…`.

| Code | Cause | Fix |
|---|---|---|
| `vulnerability_blocked` | A finding of this version in the repository that stores it blocks it: it is a known malicious package, or a triager of your installation blocked it | Use another version (the advisory page names the fixed one, if there is one). Only a triager can lift the block, by dismissing the finding or setting it back to confirmed; the next request then succeeds. An answer is never cached |
| `vulnerability_data_missing` | The repository refuses what it cannot check yet, and Quader has not imported the advisory data of this ecosystem | Retry after the first import has finished; ask an administrator when it does not |

A request through a virtual repository ends at the member that stores the blocked version: Quader does not serve the same version from another member. The same version in another repository has a finding of its own and may download.

### E405 and E409 when publishing

`E405` means the address is a remote repository or a virtual one without a deployment target. `E409` with `npm_version_exists` or `npm_version_unpublished` means the version number is taken for good: publish a new version. `npm_stale_rev` means another change came between your client's read and its write; run the command again, and with pnpm 11 or 12 unpublish at the deployment target instead of through a virtual repository.

### NuGet: push, unlist and delete

A NuGet push (`dotnet nuget push`, `nuget push`) goes to a local repository, or to a virtual repository with a deployment target. These answers have a code in the body of the response, which `curl` shows; the NuGet clients print the status and its reason phrase only.

| Answer | Code | What it means | What to do |
|---|---|---|---|
| `400` | `nuget_invalid_package` | The file is no package Quader accepts: not a ZIP archive, no or several `.nuspec` at the root, a name the archive rules refuse, a limit exceeded, or a symbol package sent to the package push | Build the package again; push `.snupkg` files to the symbol push after their package |
| `400` | `nuget_invalid_id`, `nuget_invalid_version`, `nuget_invalid_version_range` | The ID (ASCII letters, digits, `_`, single `.` or `-`), the version or a dependency range does not follow the rules | Correct the `.nuspec` |
| `403` | `forbidden`, `package_owned_by_other_owner`, `shadows_remote_package` | You may not deploy this ID, you are none of its owners, the first push of the ID would replace a package a remote repository serves, or the virtual repository you pushed to has no deployment target (its service index lists no publish address) | Ask for deploy or ownership, or a holder of manage; push to a local repository or to a virtual one with a deployment target |
| `409` | `nuget_version_exists` | The version is stored with other bytes. A version never changes | Push the changed package under a new version; development feeds use a pre-release counter. `--skip-duplicate` treats the answer as success |
| `404` | `not_found` | A symbol package (`.snupkg`) was pushed for a version the repository does not hold | Push the `.nupkg` first (`dotnet nuget push` does this in order), then the symbols |
| `409` | `nuget_symbol_package_exists` | The version has a symbol package with other bytes; symbols must match the binaries of the immutable package | Push the package and its symbols under a new version |
| `409` | `nuget_version_deleted` | The version was deleted; its number is never accepted again, whatever the bytes | Use a new version number |
| `409` | `nuget_version_reserved` | After a restore the number is kept for the package that was published after the backup, byte for byte | Push exactly that package again, or use a new version |
| `503` | `shadow_check_unavailable` | Whether a remote repository serves the ID could not be found out; nothing was stored | Retry after the `Retry-After` period (at most 30 seconds) |

`dotnet nuget delete` unlists the version by default: it disappears from search and stays downloadable and restorable, and the command can be repeated. In a repository whose delete mode is `delete` it removes the version for good, and repeating the command still succeeds. Deleting a version that never existed is `404`.

### NuGet: restores through a remote repository

A remote NuGet repository reads the service index of its upstream (`remote.url`, by default `https://api.nuget.org/v3/index.json`) and caches what a restore asks for. These answers have a code in the body, which `curl` shows; the NuGet clients print the status and its reason phrase only.

| Answer | Code | What it means | What to do |
|---|---|---|---|
| `502` | `upstream_error` naming `PackageBaseAddress/3.0.0` or `RegistrationsBaseUrl` | The address is no NuGet v3 feed: its service index lacks a resource Quader needs (a NuGet v2 feed is not supported) | Correct `remote.url`; it must end in the `index.json` of a v3 feed |
| `502` | `foreign_host_denied` | The service index names a resource on a host that is neither the upstream's nor in the remote's extra hosts (nuget.org spreads its resources over hosts of `nuget.org`) | Add the host to the remote's extra hosts; for nuget.org the proposed entry is `*.nuget.org` |
| `502` | `upstream_package_mismatch` | The upstream answered a request for one package with a package that declares another ID or version, or with an archive Quader's reader refuses so that its ID and version cannot be checked (the detail names the reason); nothing was stored | Report it to the owner of the upstream; the restore works as soon as the upstream serves the right package |
| `502` | `nuget_invalid_upstream_document` | A version list, registration index or page of the upstream is not what its address promises; the copy Quader had stays | Retry later; if it persists the upstream is broken |
| `404` | `not_found` | The upstream does not have the ID or the version, or the ID matches an exclude pattern of the remote, or the ID was withdrawn by the upstream (its cached packages stay stored and are served by the cache view) | Check the spelling and the patterns |

Cached packages are served while the upstream is down. Content that is no NuGet package at all (no ZIP archive, no `.nuspec` in it, a `.nuspec` that is not XML) is cached and served as the upstream sent it; it has no entry in the package view and no search hit, and is counted in `quader_nuget_remote_unparseable_packages_total`. The search of a remote is passed on to the upstream and answered from the packages it has cached when the upstream cannot be asked, when the remote's **Forward search** is off, or when the query names a package Quader keeps away from the remote.

### EINTEGRITY, audits and upstream failures

- `EINTEGRITY` usually means the lockfile was written against a registry that served other bytes for that version.
- An audit that answers `501` `npm_audit_unavailable` went to a repository whose audit mode is `disabled`, to the cache view of a remote repository (`<key>~cache`) or to an endpoint with exposure rules; audit through the repository itself. An audit that answers `503` `vulnerability_data_unavailable` came before Quader had imported its npm advisory data; `npm install` completes and only logs it, and the audit works once the import is done.
- `npm audit` through Quader lists the advisories of the public advisory database for the versions in your lockfile. A package whose name your organisation has claimed in a local repository is not matched against public advisories, and npm says how many names were left out.
- `502`, `503` and `504` with an `upstream_` code concern a package that is not cached while the remote repository's upstream fails; what is cached is still served. Retry later, and tell an administrator if it persists.

### Debian: apt-get update, upload and install

APT prints the status line of a failed request and no body; a signature that cannot be verified, a client clock that trails Quader's, a `Hash Sum mismatch` and the answers of an upload are explained in [Debian packages with APT](apt-packages.md#when-something-goes-wrong). The upload's answers carry a code in the body, which `curl -fsS` hides: drop `-f` to read it.

## A download is refused for malware

Quader compares the bytes of every file a client can download with ClamAV's signatures of known malware. When the scan stands in the
way of a download, every client gets an answer of its own kind with the same text: npm prints it after `E403`, Docker after
`error from registry:` (Kubernetes after `denied:`), `dotnet restore` as a `warning :` line before the status, `ansible-galaxy` after
`Message:` in its error about the collection version's metadata, APT shows the status, and `curl` on the address prints the text.

| Code | Status | What happened | What to do |
|---|---|---|---|
| `malware_found` | 403 | The scan found a signature in the file: the text names the file, the repository, the signature and the date of the database, for example `Quader found malware in pkg/-/pkg-2.3.0.tgz of web/npm-internal: Js.Downloader.Agent-10045123-0 (ClamAV database of 2026-11-20)` | Use another version, or another file. If you are sure it is a false positive, ask an instance auditor: an auditor can release these bytes in the security dashboard, and the next download succeeds |
| `malware_not_checked` | 403 | Only checked artifacts can be downloaded here, and the scan could not check the file: it is too large, a scan limit was reached inside it, it is encrypted, or every scan failed | Ask an instance auditor, who can release the bytes after looking at them, or use a file the scan can read |
| `malware_check_pending` | 503 | Only checked artifacts can be downloaded here, and the file is being scanned for the first time; the answer carries `Retry-After` | Run the command again in a minute. npm, NuGet and APT retry by themselves; Docker and `ansible-galaxy` do not, so their first pull of a large new file can fail once and work when you repeat it |

The page of the file and the **Files** tab of a version show the state of the scan
([the malware scan's states](files.md#the-malware-scans-states)). The same bytes have the same state in every repository, and through a
virtual repository the member that holds the file decides: Quader does not serve the same file from another member. An answer of the
scan is never cached.

## A download is refused for its signature

A repository can be set to serve only artifacts with a verified signature (the section **Signatures** of its settings). Such a repository
refuses a package, an image or a file that has no signature of its publisher or its build that a trust anchor of the repository knows.
Every client gets an answer of its own kind with the same text, and the clients show it differently: npm prints it after `E403`, Docker
after `error from registry:`, Podman and Kubernetes (in the pod's event) after `denied:`, `dotnet restore` as a `warning :` line before
each attempt's `403 (Forbidden)`, `ansible-galaxy` in `Error when getting collection version metadata for … (HTTP Code: 403, Message: <text>
Code: signature_not_verified)`, and `curl` on the address prints a problem document whose `reason` says why. Two cases show only the
status: an image that is still being checked (Docker, Podman and Kubernetes print `503 Service Unavailable`), and the first install of a
collection through a remote repository that held no copy of it before (`ansible-galaxy` prints `HTTP Error 403: Forbidden`); to read the
reason, open the version's page or request the file's address with curl.

| Code | Status | What happened | What to do |
|---|---|---|---|
| `signature_not_verified` | 403 | The repository serves only artifacts with a verified signature, and this one has none it accepts. The text names the artifact as your client does and says why: it has no signature of its publisher or its build (`missing`), no trust anchor the repository accepts knows its signer (`untrusted`), its signature is verified by an anchor the repository does not accept (`not_chosen`), the signature does not match the content (`invalid`), an administrator blocked its signer (`blocked`), or the signature could not be checked (`not_checked`); it ends with the address of the file's page | Sign the artifact with a key or identity the repository's anchors know and upload the signature (for an image, push it with `cosign` or `notation`; for a file, upload `<file>.asc` or `<file>.sigstore.json` beside it), or download it from a repository without the setting. An Owner cannot switch the setting off; an administrator can, and an administrator, a security administrator or an auditor can release this one artifact for this repository |
| `signature_check_pending` | 503 | The repository serves only artifacts with a verified signature, and the artifact's signatures are being checked, or, for a remote repository, fetched from its upstream; the answer carries `Retry-After` | Run the command again in a minute. npm retries by itself and then prints the text after `E503`; `dotnet restore` prints the text as a warning, retries at once and ends with a message that the feed is invalid or the package was removed, which here is not the case; `ansible-galaxy` waits about 25 s, prints the text and does not retry; Docker waits about 50 s and Podman about 100 s, and both print only `503 Service Unavailable` |

An image's signatures, attestations and SBOMs are always served, so that `cosign verify` and `notation verify` work against a repository
with the setting; a platform image of a multi-platform image passes on the signature of its index. Through a virtual repository both the
virtual repository's setting and the setting of the member that holds the artifact apply. An answer of the setting is never cached, so an
artifact signed later is served at the next request.

## Session ended and re-authentication

A browser session ends after a period without activity and after a maximum length (one and twelve hours unless an administrator set other values), or when it is ended under **Profile › Sessions**.

- **Your session has ended.** The UI shows this dialog when a request finds the session gone. **Sign in again** returns you to the same screen after the sign-in; **Not now** keeps the page open. An editor with unsaved changes also offers **Copy draft as JSON**.
- **Your session has ended** on the sign-in screen. When you open Quader and the browser still holds a session that has ended, Quader shows the sign-in screen with this notice, also where some content is public, and the browser forgets the old session. Sign in again, or select **Continue without signing in** to use the public content without errors.
- **Confirm it is you.** Some actions need a sign-in within the last hour (unless configured otherwise): creating or rotating a token, saving a permission policy, binding a role, changing an exposure rule, deleting a repository and others. The UI then shows this dialog; after **Sign in again** the sign-in page says that your entries are kept, and you repeat the action.

Tokens are not affected: a client with a valid token keeps working whatever happens to your browser session.

## Error codes

Each code with its status and meaning. `GET /api/v1/problems/<code>` gives the description of any code, including those an administrator meets.

<details>
<summary>Error codes you can meet as a user</summary>

| Code | Status | Meaning |
|---|---|---|
| `credentials_required` | 401 | No credential, and the request is not public |
| `invalid_token` | 401 | The token is malformed, unknown, expired, revoked or not accepted on this endpoint |
| `principal_disabled` | 401 | The owner of the credential is disabled |
| `session_expired` | 401 | The session cookie names a session that has ended |
| `reauthentication_required` | 401 | The action needs a recent sign-in |
| `unauthenticated` | 401 | The request carries no valid credential |
| `forbidden` | 403 | The credential is valid; the operation is not permitted |
| `csrf_failed` | 403 | A browser request without the session's anti-forgery header; reload the page |
| `endpoint_read_only` | 403 | A write on an endpoint with exposure rules |
| `not_admitted` | 403 | The repository's include and exclude patterns refuse the path or name |
| `package_owned_by_other_owner` | 403 | The package has owners and you are none of them |
| `shadows_remote_package` | 403 | A first publish would replace a package a remote repository serves |
| `populate_not_permitted` | 403 | Not cached, and you may not fetch it from the upstream |
| `vulnerability_blocked` | 403 | A finding of this version blocks it in this repository (a known malicious package, or a block set in triage) |
| `vulnerability_data_missing` | 403 | The repository blocks what it cannot check, and the advisory data was never imported |
| `malware_found` | 403 | The malware scan found a signature in the file and no auditor released it |
| `malware_not_checked` | 403 | Only checked artifacts can be downloaded here, and the malware scan could not check the file |
| `malware_weakening_refused` | 403 | Switching a repository's malware setting off needs an administrator or someone who triages vulnerabilities |
| `signature_not_verified` | 403 | The repository serves only artifacts with a verified signature, and this one has none it accepts; `reason` says why |
| `signature_weakening_refused` | 403 | Switching a repository's signature setting off, or accepting more trust anchors, needs an administrator or someone who triages vulnerabilities |
| `pat_not_allowed` | 403 | Personal tokens are restricted to members of certain groups |
| `derivation_exceeds_parent` | 403 | A token created with a token would outlive or outrank it |
| `not_found` | 404 | Does not exist, or is hidden from you |
| `repository_not_found` | 404 | The project has no repository of this key, or you may not see it |
| `repository_disabled` | 404 | The repository is disabled |
| `not_cached_offline` | 404 | The remote repository is offline and the item is not cached |
| `npm_class_not_writable` | 405 | A write to a remote npm repository |
| `no_deploy_target` | 405 | A write to a virtual repository without a deployment target |
| `conflict` | 409 | Conflicts with the current state, for example an upload whose digest does not match |
| `path_conflict` | 409 | A file would sit where a folder is, or the other way round |
| `already_exists` | 409 | An object with this key or name exists |
| `npm_version_exists` | 409 | The version was published before |
| `npm_version_unpublished` | 409 | The version was unpublished and is never accepted again |
| `npm_stale_rev` | 409 | Another change reached the package between read and write |
| `nuget_version_exists` | 409 | The NuGet version is stored with other bytes |
| `nuget_version_deleted` | 409 | The NuGet version was deleted and is never accepted again |
| `nuget_version_reserved` | 409 | After a restore, the number is kept for one exact package |
| `deb_version_exists`, `deb_filename_conflict`, `deb_withheld` | 409 | A Debian package file is stored with other bytes, another version has its file name, or a restore withheld these bytes |
| `generated_item` | 409 | A write or delete of a file that Quader generates, such as a Debian index below `dists/` |
| `token_limit_reached` | 409 | You hold the maximum number of active tokens |
| `idempotency_in_progress` | 409 | The same request is still running; retry after `Retry-After` |
| `precondition_failed` | 412 | A condition header did not match |
| `authz_epoch_unknown` | 412 | A permission change is not visible yet; read again |
| `payload_too_large`, `npm_too_large` | 413 | Above a size limit |
| `range_not_satisfiable` | 416 | The byte range lies beyond the end of the file |
| `misdirected_request` | 421 | The host is not one this endpoint answers for |
| `malformed_request`, `invalid_path`, `invalid_header` | 400 | The request, the path or a header is not valid |
| `npm_invalid_name`, `npm_invalid_tag`, `npm_bad_publish`, `npm_manifest_mismatch` | 400 | A name, a dist-tag or a publish request that npm's rules or Quader's checks refuse |
| `nuget_invalid_package`, `nuget_invalid_id`, `nuget_invalid_version`, `nuget_invalid_version_range` | 400 | A NuGet package, ID, version or dependency range that the rules refuse |
| `deb_distribution_required`, `deb_invalid_coordinate`, `deb_invalid_file_name` | 400 | A Debian upload without a suite, with a suite or component outside the rules, or with a file name that does not end in `.deb` |
| `deb_invalid_archive`, `deb_invalid_control`, `deb_unsupported_compression` | 422 | A Debian package that is no `ar` archive, whose control data breaks a rule (the answer names the field), or whose control archive Quader cannot read |
| `invalid_scope`, `invalid_ttl`, `ttl_exceeds_maximum`, `description_required` | 400 | A token request with an invalid scope, lifetime or description |
| `validation_failed` | 422 | A field of the request is invalid; the answer names it |
| `rate_limited` | 429 | Too many requests; wait for `Retry-After` |
| `npm_not_implemented`, `npm_audit_unavailable` | 501 | An npm operation, or an audit, that Quader does not offer here |
| `upstream_unreachable`, `upstream_error`, `upstream_auth_failed`, `upstream_integrity_mismatch`, `npm_invalid_upstream_document`, `nuget_invalid_upstream_document`, `upstream_package_mismatch`, `egress_denied`, `foreign_host_denied` | 502 | The remote repository's upstream failed, refused or sent something Quader does not accept, and nothing usable is cached |
| `server_busy`, `shutting_down` | 503 | The replica is at its limit or stopping; retry |
| `database_unavailable`, `authz_stale`, `unavailable` | 503 | A dependency is temporarily unavailable; retry |
| `malware_check_pending` | 503 | Only checked artifacts can be downloaded here, and the file is being scanned; retry after `Retry-After` |
| `signature_check_pending` | 503 | Only artifacts with a verified signature can be downloaded here, and the artifact's signatures are being checked; retry after `Retry-After` |
| `upstream_assumed_offline`, `shadow_check_unavailable`, `fetch_queue_timeout` | 503 | The upstream is assumed down, or busy; retry after `Retry-After` |
| `content_unavailable` | 503 | The stored object of the file is missing from the object store. A remote repository fetches it again from its upstream and answers this only while it cannot (offline, the upstream down, the cache view); a local repository answers it until the object is back (an administrator restores it; Quader looks again at most every 30 seconds) or the same file is uploaded again. Retry after `Retry-After`; tell an administrator when it persists. The status is 500 when the object failed Quader's verification instead |
| `upstream_timeout` | 504 | The upstream did not answer in time |
| `internal` | 500 | A defect; give the request ID to an administrator |

</details>
