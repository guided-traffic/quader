# Data and secret flow

Status: implemented in M0 for the credentials of every connection, the stored-secret key ring and its envelope, and the
redaction of logs, spans and error texts, and in M2 for the upstream credentials of remote repositories: their sealed rows, the
secret references and the clients that send them, and for the names an npm audit forwards to an advisory provider; the release
pipeline's secrets as the workflows hold them. Package bytes are blobs in the object store; who can read them there besides Quader is
[H-11](../spec/storage.md#h-11--object-store-principals-bypass-quaders-authorization-and-integrity-checks) on
[file-transfer.md](file-transfer.md).

This page says where each credential lives inside the running installation, when a process reads it, what a process holds in
memory, and what is written where it could be read by someone else, and which code the release pipeline's credentials reach.
The rules are the code's; the requirement pages are linked.

## Where credentials live

Credentials are files. The chart mounts each one from an existing Secret and names only the path in the configuration.
No Secret becomes an environment variable, not even the non-secret bucket, region and endpoint that an object-store provisioner writes
beside the keys: they are files under `/var/run/secrets/quader/s3-<id>-location/` that the connection's `bucket_file`, `region_file` and
`endpoint_file` name ([_storage.tpl](../../deploy/helm/quader/templates/_storage.tpl), [ha-and-helm.md](../spec/ha-and-helm.md)
R-HA-072). The chart never creates a Secret ([values.yaml](../../deploy/helm/quader/values.yaml), `secrets`).

| Credential | Where it lives | When a process reads it | Where it is not |
|---|---|---|---|
| PostgreSQL user and password | files under `/var/run/secrets/quader/database/` | on every new connection ([postgres/connect.go](../../internal/meta/postgres/connect.go), `beforeConnect`) | the connection string carries neither ([connString](../../internal/meta/postgres/connect.go)) |
| Valkey user and password, Sentinel user and password | files under `/var/run/secrets/quader/valkey/` and `valkey-sentinel/` | on every connection attempt, through `AuthCredentialsFn` ([cluster/valkey_auth.go](../../internal/cluster/valkey_auth.go)) | the process holds the values only while it authenticates |
| Object-store access key and secret, per connection | files under `/var/run/secrets/quader/s3-<id>/` (`credentials_source: static`, the entry of `secrets.storageConnections` with that id), or the SDK's default chain, or a web-identity token file | the static files at most once per second ([blob/s3/credentials.go](../../internal/blob/s3/credentials.go), `fileReread`) | the default chain: see [H-74](privilege-footprint.md#h-74--the-default-object-store-credential-chain-can-reach-node-metadata) |
| Metrics bearer token | file `metrics.bearer_token_file` | on every request to `/metrics` with `auth: static` ([observability/metrics.go](../../internal/observability/metrics.go), `authorized`) | not logged |
| OTLP headers | file `tracing.headers_file`, `Name: value` per line | once, when the exporter is built ([observability/otlp.go](../../internal/observability/otlp.go)) | not in the configuration |
| Stored-secret key ring | files under `/var/run/secrets/quader/encryption/`, one per key | once, at start ([wire_keys.go](../../internal/app/wire_keys.go), `secrets.Load`) | see [H-55](../spec/architecture.md#h-55--key-files-of-the-stored-secret-key-ring-are-read-once-at-start-up) |
| Break-glass password | one file, `/var/run/secrets/quader/break-glass/password`, from `secrets.breakGlass`; plaintext (at least 20 characters) or an Argon2id hash | at start and again whenever its modification time changes, looked at every `auth.break_glass.check_interval` ([oidc/breakglass.go](../../internal/auth/oidc/breakglass.go)); read by `api` replicas only, and by `web` in the bridge of the upgrade across the cut (R-AUTH-042) | not in PostgreSQL, not in `packages`, `worker`, the migration Job or the backup, restore and post-restore pods; not logged |
| Upstream password or token of a remote repository | sealed in `repository_secrets.sealed`, or a reference to a file under `config_as_code.secrets_dir` or to a `QUADER_CAC_` variable | a sealed value when the repository's client is built; a reference at most every 10 s ([upstream/secret.go](../../internal/proxy/upstream/secret.go)); both only by the replica that sends a request to that upstream | not in `repositories.config` (it holds `{"set": true}` or the reference), not in an audit event, a log line or an error text, not in any API answer |
| Account of a well-known repository from the configuration (`repositories.upstream_accounts`, [R-REPO-077](../spec/repositories-and-proxy.md)) | files under `/var/run/secrets/quader/upstream-accounts/<key>/` (`username`, `password`), one projected volume of the Secrets `secrets.upstreamAccounts` names, optional, in the pods of `api`, `packages` and `worker` ([R-HA-073](../spec/ha-and-helm.md)) | when the repository's client is built and again at most every 10 s, 1 s after a failure ([upstream/secret.go](../../internal/proxy/upstream/secret.go), `newFileSecret`), by the replica that sends a request to that upstream; the admin page's answer reads them for their state and drops them ([upstream_accounts.go](../../internal/repository/upstream_accounts.go), `FilesState`) | not in PostgreSQL (the document holds `authMode: none`, the row the time of the declaration), not in the configuration file (it holds the paths and the Secret's name), the values, an API answer, an audit event, a log line (the path only) or an error text; not in a scanner, the updater or any Job |
| Signing key of a Debian repository | sealed in `deb_signing_keys.private_key`, bound to the row's id; generated by Quader or imported (the passphrase of an import is used once and not kept) | opened by the index build for one signing call and by the self-test at generation and import, on the replica that does it ([formatdata/deb/keys_store.go](../../internal/formatdata/deb/keys_store.go), `SigningKeys`) | not in an API answer, an audit event, a journal entry, a log line or an error text; see [deb-signing.md](deb-signing.md) |
| Signing key of the installation ([signatures.md](../spec/signatures.md) R-SIG-045 to R-SIG-047) | either files under `/var/run/secrets/quader/signing/` from the Secret of `signing.identity` (existing, or issued by cert-manager), mounted into the `worker` pods only ([ha-and-helm.md](../spec/ha-and-helm.md) R-HA-071); or sealed in `signing_identities.sealed_key`, bound to the row's id, generated by Quader for a certificate request or imported (an import's passphrase is used once and not kept) | the files at start and whenever their content changes, by the worker; the sealed key by the job `signing` of the worker for its signing calls; `api` holds a generated or imported key only until it is sealed | not in `api` or `packages` after that, and not in an API answer, an audit event, a journal entry, a log line or an error text; see [artifact-signatures.md](artifact-signatures.md#quaders-own-signature-stage-2) and [H-1188](artifact-signatures.md#h-1188--an-uploaded-identity-can-be-opened-by-every-role-that-holds-the-key-ring-and-the-database) |
| OIDC client secrets | one file per provider, `/var/run/secrets/quader/oidc-<providerId>/client-secret`, from `secrets.oidc[]` | when the file's modification time changes ([oidc/provider.go](../../internal/auth/oidc/provider.go)); `api` replicas only | not in PostgreSQL, not in `packages`, `worker` or any Job |
| Scan token of the image scanning ([registrytoken/scan.go](../../internal/auth/registrytoken/scan.go)) | nowhere at rest: the worker signs it with the registry key for one call and holds it in memory in a type that renders nothing; it travels in the call's body to the scanner, which holds it for the call and sends it as `Authorization: Bearer` to the scan listener ([R-OCI-037](../spec/formats/oci.md#r-oci-037--scan-tokens-the-registry-token-of-the-image-scannings-pull)) | by the scan listener of `packages`, on each pull request of the call | not in a job's arguments or result, a scan row, an error text, an audit event or a log line; the scan ID `sid` is what the logs carry |
| Blob token of the malware scanning ([registrytoken/blob.go](../../internal/auth/registrytoken/blob.go)) | nowhere at rest: the worker signs it with the registry key for one call and holds it in memory in a type that renders nothing; it travels in the call's body to the malware scanner, which holds it for the call and sends it as `Authorization: Bearer` to the scan listener ([R-MAL-006](../spec/malware-scanning.md#r-mal-006--the-scanners-read-of-a-blob-the-route-on-the-scan-listener-and-its-token)) | by the scan listener of `packages`, on the read of the blob | not in a job's arguments or result, a row of `malware_scan`, an error text, an audit event or a log line; the scan ID is what the logs carry |

The chart mounts each Secret into the pods that need it and nowhere else ([_secrets.tpl](../../deploy/helm/quader/templates/_secrets.tpl)):

- the key ring (`encryption`) in the pods of every role but `scanner` and in the restore Job, which checks that it opens every key ID of
  the backup; never in the migration Job or the backup pods, which never seal or open a value;
- the TLS pair of each kind of pod (`api`, `packages`, `worker`, `scanner`; Secret `<fullname>-tls-<kind>`, which the chart's Certificate
  makes, or `tls.existingSecrets.<kind>`) in the pods of that kind only, and its `ca.crt` beside it where a link verifies with it
  ([R-HA-063](../spec/ha-and-helm.md#r-ha-063--chart-tls-wiring)); a pod holds no other kind's pair;
- in the `vuln-scanner-trivy` pods, the scanner's own pair and the internal CA, and nothing else: no key ring, no database, Valkey or object-store file, no shared `extraEnvFrom` or
  `extraVolumes` ([R-HA-065](../spec/ha-and-helm.md#r-ha-065--the-scanner-deployment), [vuln-scanner-trivy.yaml](../../deploy/helm/quader/templates/vuln-scanner-trivy.yaml));
  the same pair in their container `database` too, and nothing of the installation in their init container `trivy`, which mounts the volume
  `trivy-bin` alone ([R-HA-070](../spec/ha-and-helm.md#r-ha-070--the-analyser-from-its-own-image-and-the-database-in-the-scanner-pod)); the same in the container `quader` of the `malware-scanner-clamav` pods, and nothing
  of the installation in its container `clamd` or in the container `freshclam` of the updater `malware-updater-freshclam` (whose
  `quader` container holds its own pair and the CA and nothing else, [malware-updater-freshclam.yaml](../../deploy/helm/quader/templates/malware-updater-freshclam.yaml)), which hold no credential at all
  ([R-HA-068](../spec/ha-and-helm.md#r-ha-068--the-malware-scanner-deployment), [R-HA-069](../spec/ha-and-helm.md#r-ha-069--the-updater-deployment-malware-updater-freshclam));
- in the `worker` pods while the image scanning is on, the worker's pair as the identity they present to the scanner, and the internal
  CA; in the main `packages` pods, the internal CA for the scan listener ([R-HA-066](../spec/ha-and-helm.md#r-ha-066--the-links-of-the-image-scanning-inside-the-chart));
- the object-store credentials only with `credentials_source: static`, per connection;
- the metrics token only with `metrics.auth: static`;
- the accounts of the well-known repositories (`secrets.upstreamAccounts`) in the pods of `api`, `packages` and `worker`, their pools and
  endpoint pools, which build the clients of the upstreams; never in a scanner, the updater, the migration Job (which applies the
  declaration from the configuration alone) or a backup, restore or post-restore Job ([R-HA-073](../spec/ha-and-helm.md));
- the break-glass password and the OIDC client secrets only in the pods of the role `api` (the main Deployment, its path pools and its
  endpoint pools), which serve the login; `packages`, `worker` and the Jobs never get them. The Secret that holds the break-glass password is an administrator credential: whoever can
  read it through the Kubernetes API can sign in as the break-glass account, so its RBAC is the access control of that account;
- the listener certificate (`tls.crt` and `tls.key`) only in the pods whose process serves a listener, and `ca.crt` only in the
  `worker`, `packages`, `vuln-scanner-trivy` and `malware-scanner-clamav` pods while a scanning is on, where the links of R-HA-066 and
  R-HA-068 verify with it.

The Valkey and PostgreSQL files are mounted with `defaultMode: 0440`, read-only, without `subPath`, so a rotated Secret reaches
the process through the kubelet's swap and the next read (see [transport-encryption.md](transport-encryption.md)).

## Upstream credentials of remote repositories

Specified on 2026-11-23, not built: the upstream catalogue of a remote asks with the same credential, to the same hosts, so its answers are
that account's view ([remote-curation.md](remote-curation.md),
[H-1722](remote-curation.md#h-1722--the-catalogue-shows-what-the-upstream-credential-sees)); a catalogue host of a preset receives no
credential.

A remote repository's password (the token with `authMode: bearer`) is a secret field ([R-REPO-033](../spec/repositories-and-proxy.md),
[remote.go](../../internal/repository/remote.go), `SecretField`). Three forms exist, and each has one place:

| Form | In the request | In `repositories.config` and in every read | In `repository_secrets` |
|---|---|---|---|
| inline value | `{"value": "…"}`, through the API only | `{"set": true}` | `sealed`: AES-256-GCM under the active key, associated data `(table, column, repo_id, field)` |
| reference | `{"secretRef": {"file": "<name>"}}` or `{"env": "QUADER_CAC_<NAME>"}` | the reference | `secret_ref` |
| keep | `{"set": true}` | `{"set": true}` | unchanged; 422 `invalid_secret` when there is no sealed value |

The plaintext of a request exists in `Settings.pending` between the validation and the write, in an `observability.Secret`, and is
sealed in the transaction that writes the row ([secrets_store.go](../../internal/repository/secrets_store.go), `writeSecrets`). No JSON form
of the field can carry it (`SecretField.MarshalJSON`), the audit event of the update lists the member `remote` and nothing of its content,
and the idempotency layer stores a hash of the request body, not the body. A value that is wrong (a line break, over 8 KiB, empty) is
refused before anything is written; an error text never repeats a value.

The associated data binds a sealed value to its repository and field: copying the `sealed` text of one repository's row into another's
makes that repository's client fail with `502 upstream_secret_unavailable` and leaves every other repository working
([upstream/clients.go](../../internal/proxy/upstream/clients.go), `secretOf`). A reference is checked twice, when the document is written
and when the client reads it (`SecretRef.Valid`): a file name has no path separator, no `..` and no leading dot, a variable carries the
prefix `QUADER_CAC_`, so `QUADER_DATABASE_PASSWORD` and the files of the key ring cannot be routed to an upstream whatever a row says.

An Owner of a project without `repositories:write-config` (R-AUTHZ-026) gives a secret as a value only: its writes and its unsaved
upstream test refuse a reference other than the one the repository holds (422 `secret_value_required`), and a write of it that changes
`url`, adds a `sendCredentials` host or turns `forwardAuthOnRedirect` on keeps no stored credential, sealed or referenced: it sends a new
value or none
([Q-REPO-127](../spec/repositories-and-proxy.md#q-repo-127--a-project-administrator-gives-an-upstream-secret-as-a-value-and-a-stored-one-stays-with-its-hosts),
[delegated-administration.md](delegated-administration.md#remote-repositories-of-a-project)).

The plaintext lives in the replica that sends a request: in the client's credential object for as long as the client is cached, and in
the `Authorization` header of the request in flight. The credential policy of the client removes every `Authorization` header and sets it
again only for the hosts of [egress.md](egress.md#which-hosts-receive-credentials), and over `https` only while the upstream itself is
`https`; a URL in the upstream's metadata cannot move a fetch to `http` (Q-ARCH-224). A rotated reference reaches a replica within 10 s
without a new client; a rotated inline value moves the repository's `config_version`, which replaces the client.

### An account on a well-known repository

Built ([repositories-and-proxy.md](../spec/repositories-and-proxy.md) R-REPO-069, R-REPO-073; `setUpstreamRepositoryAccount`). An administrator may store
an account on a repository of the built-in project `upstream` (owner, 2026-11-20: "um zum Beispiel Rate-Limits zu umgehen oder besser
umzugehen"). It is stored and sealed as every remote's credential, in the rows above; what differs is who uses it.

- **Said plainly: whoever reads the repository pulls with the account.** A well-known repository at `internal` is read by every
  signed-in subject that is not a user flagged external or that user's token, people, CI tokens and service-account tokens alike,
  directly and through every team's virtual repository that lists it ([authorization.md](../spec/authorization.md) R-AUTHZ-030,
  Q-AUTHZ-302); at `public`, by anyone on the internet. Every one of their misses is fetched with the account. An account
  that can read more than the public registry's public content (a Docker Hub account with private repositories, a GitHub token with
  access to an organization's packages, a GitLab token of a project) hands that content to every one of those callers, under the
  installation's name ([H-1025](#h-1025--an-account-stored-on-a-well-known-repository-pulls-for-everyone-who-reads-it)). The
  installation's own private registries are not presets for that reason (T-21b).
- **The account stays with its hosts, for every caller.** The rule that an Owner's write of a project's remote must enter a stored
  credential again when its hosts change binds every caller here, holders of `repositories:write-config` included: an address override
  that moves the hosts that receive the account carries a new account or removes it (422 `secret_value_required` otherwise). A release
  that moves the product's hosts removes a stored account in the reconcile of `quader migrate` and records `accountRemoved: true`
  (Q-REPO-377), so a credential never follows the product to a host nobody entered it for.
- **Every account write** needs recent authentication and is recorded with the secret as a fingerprint (R-API-007, R-OPS-016).
- **The account takes a value only** ([Q-REPO-487](../spec/repositories-and-proxy.md#q-repo-487--the-account-operation-of-a-well-known-repository-takes-a-value-only)):
  a `secretRef` is refused (422 `secret_value_required`), so a file under `config_as_code.secrets_dir` reaches a well-known repository
  through no write of the API (H-281's scope narrows to the project's remotes).
- **An account from the installation's configuration** ([R-REPO-077](../spec/repositories-and-proxy.md), R-HA-073). The operator names an
  existing Secret in `secrets.upstreamAccounts.<key>`; the value never leaves the Secret's files: the migration Job sees the configuration
  (paths and the Secret's name) only, removes an account an administrator had stored, and records the change with the actor
  `system:configuration` and `account: {source, authMode}`, without a value or a fingerprint. The replica reads the files at use, as a
  reference; a username with a colon or a file that cannot be used fails that repository with `502 upstream_secret_unavailable` and is logged
  with its path only. The paths are outside `secrets_dir`, so no `secretRef` names them. The account's writes and an address that moves the
  hosts receiving it answer 409 `managed_by_config` ([Q-REPO-486](../spec/repositories-and-proxy.md#q-repo-486--a-declared-account-follows-its-registry-an-address-that-moves-its-hosts-is-refused)):
  only the operator decides where the token goes. Whoever can read the Secret through the Kubernetes API holds the account; whoever reads the
  repository pulls with it, as above. A rotation reaches every replica within the kubelet's sync and 10 s
  ([H-1940](#h-1940--replicas-send-the-old-and-the-new-token-of-a-declared-account-side-by-side-for-up-to-two-minutes)) and is not in Quader's audit trail ([H-1941](#h-1941--quaders-audit-trail-does-not-show-a-rotation-of-a-declared-accounts-secret)).

### The development profiles' Docker Hub account

Built for the two development profiles only ([architecture.md](../spec/architecture.md) Q-ARCH-1066 to Q-ARCH-1069,
[local-profile.md](../developer/local-profile.md#the-docker-hub-account)); nothing of it is in the product, the chart or an image. The owner
keeps his Docker Hub user and a read-only access token in `<main checkout>/.dev/credentials/upstream-registries.env` (mode `0600` in a
directory of `0700`, ignored by git and by the image build), and [upstream-accounts.sh](../../deploy/dev/upstream-accounts.sh) hands it to
Quader the way an operator declares an account above: the UI profile through `repositories.upstream_accounts` of its configuration, the kind
profile through the Secret `quader-upstream-docker-hub` that `secrets.upstreamAccounts` names.

- **Where the value goes.** `sed` reads it from the file into a file (`umask 077`, renamed into place) under the profile's state:
  `.dev/ui-dev/upstream-accounts/docker-hub/` or `.dev/upstream-accounts/docker-hub/`. From there `kubectl create secret --from-file` and
  `base64` read it; the Secret's manifest and the nodes' Docker configuration go through pipes, into `kubectl apply --server-side` (no
  `last-applied-configuration` annotation holds the data) and into each node's `/var/lib/kubelet/config.json` (mode `0600`). The source
  file is never sourced. No value becomes a shell word, so a `bash -x` trace, the process list, the profiles' logs and the pipeline's
  failure capture hold key names and paths only; `TestTheAccountFileReachesTheProfilesAndNoTraceShowsAValue`
  ([internal/tools/devprofile](../../internal/tools/devprofile/upstream_accounts_test.go)) runs the script with `bash -x` and made-up values
  and fails on a value, or its base64, in the trace, the output or an argument of `kubectl`.
- **Who uses it.** The main checkout's instance 1 of the UI profile and its kind cluster; a git worktree and the instances 2 to 8 read no
  account unless `UPSTREAM_ACCOUNTS_FILE` names a file (Q-ARCH-1067), so a builder's run never pulls with it unasked. A placeholder or an
  empty value is no account; every CI leg has no file.
- **What stays exposed** on the machine and in the kind cluster is below:
  [H-1942](#h-1942--the-development-profiles-keep-the-owners-upstream-token-in-plaintext-files-of-the-machine),
  [H-1943](#h-1943--the-kind-profiles-cluster-holds-the-owners-upstream-token-where-its-kubeconfig-and-its-nodes-reach), and the shared
  allowance, [H-1944](#h-1944--the-profiles-and-the-owners-installation-draw-on-one-docker-hub-accounts-allowance). Whoever can read
  `upstream/docker-hub` of a profile pulls with the account (H-1025); the UI profile listens on the loopback address only.

## The stored-secret key ring

A key ring holds the keys with which the database's sealed columns are encrypted (R-ARCH-021). Keys are loaded from
`secrets.encryption_key_files`; the active key is `secrets.active_key_id` ([config/secrets.go](../../internal/config/secrets.go)).

- **File form.** A key file holds 32 random bytes. A file of exactly 32 bytes is the key itself. Any other file is base64 text
  that decodes to 32 bytes, standard or unpadded. The check of the exact length comes before any trimming, because a random
  key may start or end with a white-space byte ([secrets/keyring.go](../../internal/secrets/keyring.go), `readKeyFile`).
- **Key ID.** The file name without `.key`. It must match `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`, and no two files may share one
  ([keyring.go](../../internal/secrets/keyring.go), `Load`).
- **Ring.** Every listed key opens values; only the active key seals. The active ID must be one of the listed keys
  ([config/secrets.go](../../internal/config/secrets.go), `Validate`). The chart takes the first key of `secrets.encryption.keys`
  as active ([_helpers.tpl](../../deploy/helm/quader/templates/_helpers.tpl)).
- **Fingerprint.** `Fingerprint` is an HMAC-SHA-256 under a key derived with HKDF-SHA-256 from the active key, info
  `quader fingerprint v1`. It has no call site.
- **Canary.** `SealCanary` and `CheckCanary` seal and open a known text in the `secret_key_canary` row of a key. Every role checks
  them at start, before it serves ([wire_api.go](../../internal/app/wire_api.go), `wireKeyRing`, `verifyKeyRingCanaries`): a replica
  refuses to start when sealed rows reference a key ID its ring lacks (`RequireKeys`, which names the missing IDs), and when its ring
  cannot open the canary of a key ID it holds (the same ID with other key material); a key ID without a canary gets one. The restore
  runs the same checks against the backup's key IDs ([restore/checks.go](../../internal/backup/restore/checks.go)). A reference file of
  a remote repository is not part of the check ([H-280](#h-280--a-secret-that-cannot-be-resolved-is-found-at-its-first-use)).
- **Errors.** Errors name the file or the key ID, never key material. `MismatchError` lists missing key IDs only
  ([keyring.go](../../internal/secrets/keyring.go), `MismatchError`).
- **Logging.** `KeyRing` implements `String` and `LogValue` with the key IDs only ([keyring.go](../../internal/secrets/keyring.go)).

## The envelope and its additional data

A sealed value is stored as `v1:<key ID>:<base64(nonce ‖ ciphertext ‖ tag)>`:

- AES-256-GCM with a random 96-bit nonce per value, from `crypto/rand`;
- the key ID is in clear, so a value under an old key is found and re-sealed by its ID, and an unknown ID names only the ID
  (`ErrUnknownKey`);
- the additional data binds the value to its row and column. It is the count of primary-key parts, then the strings
  `quader/sealed/v1`, the table name, the column name and each primary-key part, every string prefixed with its length as
  an unsigned varint ([keyring.go](../../internal/secrets/keyring.go), `aad`). A value moved to another row or column fails
  authentication (`ErrAuthentication`), and two different row keys do not encode alike (`("a:b")` and `("a", "b")` differ);
- a row key with the wrong number of parts is refused (`ErrRowKey`), and a column that is not registered cannot be sealed or
  opened (`ErrUnregistered`).

Rotation: a value under a key other than the active one is re-sealed (`Reseal`, `NeedsReseal`), a value under the active key is
left as it is, so the re-encryption job is idempotent. The procedure in [R-ARCH-021](../spec/architecture.md#r-arch-021--encryption-of-stored-secrets-and-the-stored-secret-key)
adds the new key first (decrypt-only on every replica), makes it active, re-seals, and removes the old key last.

## Redaction

Secrets are kept out of every log record, span attribute, error text and ring-buffer record. The mechanism is in
[observability/redact.go](../../internal/observability/redact.go):

- a `Secret` type whose string forms are the marker `[REDACTED]`; only `Reveal` returns the value, and only the code that needs to
  send it calls that. `config.Secret` does the same for configuration values ([config/secret.go](../../internal/config/secret.go));
- attribute keys containing `password`, `secret`, `token`, `authorization`, `cookie`, `api_key`, `apikey`, `private_key` or
  `credential` have their value replaced (`token_id` and `token_type` are exempt);
- free text: `qdr_<prefix>_<value>` tokens keep the prefix and lose the value, `Bearer` and `Basic` values are replaced, URL user
  information is replaced up to the last `@` before the authority ends, the poll secret of a `/auth/cli/` path is replaced, and
  the values of the configured query parameters (`log.request.redact_query_params`) are replaced.

The scrubber runs before a field is cut to its length, so a secret is never split into a fragment that no pattern recognises
(`clipField`, [requestlog.go](../../internal/observability/requestlog.go)). The span exporter scrubs its attributes through a
wrapper ([tracing.go](../../internal/observability/tracing.go), `scrubExporter`).

The status of a mirror (`GET /api/v1/projects/{projectKey}/repositories/{repoKey}/mirror`, R-REPO-056) shows `errors.lastMessage`, the failure text the engine
stored in `mirror_state.last_error`; the status adds no scrubbing of its own, so the rule that the text names hosts and never a URL or
a credential is the engine's ([H-530](#h-530--the-mirror-status-shows-the-engines-failure-text-as-stored)).

The in-memory ring buffer holds the same lines the log handler writes, bounded by record count and bytes, and a record above
16 KiB is kept as a stub ([ring.go](../../internal/observability/ring.go)). Standard output is not bounded, which is
[H-70](#h-70--the-standard-output-log-carries-every-record-in-full).

## A failed answer in the browser

Every failed call of the web UI shows what the browser knows of its answer and copies it as one text for a ticket
([R-UI-216](../spec/api-and-ui.md#r-ui-216--a-failure-shows-every-fact-of-its-answer),
[Q-UI-1186](../spec/api-and-ui.md#q-ui-1186--a-failures-facts-leave-out-every-credential-and-show-a-foreign-body-as-text)). The facts
come from one function, `apiErrorOf` in [failure.ts](../../web/src/app/core/http/failure.ts), for the interceptors and the upload's
own client alike, and leave out what carries a credential:

- **Nothing of the request but its method and its path.** No request header (the session cookie, the CSRF token, the
  `Idempotency-Key`, an `Authorization`), no request body (a password, a token's secret), no cookie.
- **A query parameter that carries a credential is named without its value** (`token=[redacted]`): a name that contains `token`,
  `signature`, `secret`, `password`, `passwd` or `credential`, `sig`, `api_key`/`apikey`, and the `X-Amz-` and `X-Goog-` parameters
  of a signed address. No call of the UI carries one today (verified 2026-11-25: the sign-in sends its password in a body, a signed
  download link is a navigation of the browser, not a call); the rule is for the one that will.
- **The answer's headers that a script may read**, less `Set-Cookie`, `Set-Cookie2`, `Authorization`, `Proxy-Authorization` and
  `Cookie`; the address of a `Location`, `Content-Location`, `Link` or `Refresh` header loses its credential parameters by the same
  rule.
- **The body of an answer that is no problem document**, a gateway's error page, as text at most 4,000 characters, and never as
  markup: Angular's interpolation writes it as a text node. **Never the body of a successful answer that could not be read**: it may
  be a token's secret that a proxy broke (R-UI-043); the notice says only that it could not be read, with the parser's message.

The viewer sees nothing the browser did not already hold: the same answer was readable in the browser's developer tools. What
changes is that **Copy report** puts it into a text that travels, which [H-1960](#h-1960--a-failures-report-carries-what-the-answer-named)
names.

## What a process holds, and what Valkey may hold

A replica holds in memory the key ring and the credentials it has read, for as long as it uses them. The plaintext of a sealed
value exists only while a request needs it. Nothing Quader writes to disk holds a credential: the spool holds bodies of running
uploads, with mode `0600` and UUID names ([spool.go](../../internal/blob/spool.go)), and its contents are not encrypted by Quader
([H-2](../spec/architecture.md#h-2--the-upload-spool-is-not-encrypted-by-quader)).

Valkey holds coordination state and the negative cache of remote repositories. Keys are built from registered families with a
mandatory expiry ([valkey_keys.go](../../internal/cluster/valkey_keys.go)); their parts match `[A-Za-z0-9_-]`. Nothing Quader
serves is read from Valkey and no positive authentication or authorization result is ([spec Q-ARCH-16](../spec/architecture.md#q-arch-16--what-valkey-may-hold)).
What a Valkey writer can do is on [roles-and-trust-boundaries.md](roles-and-trust-boundaries.md).

## The audit forward

`npm audit`, `pnpm audit` (pnpm 11 and later) and `yarn npm audit` post a project's dependency names and versions to the
registry. A remote repository forwards them to its advisory provider ([R-NPM-038](../spec/formats/npm.md#r-npm-038--security-audit-endpoints),
[handlers_audit.go](../../internal/formats/npm/handlers_audit.go)); a virtual repository forwards through its first forwarding
remote member. This is the one place where Quader sends data a client posted to a third party.

| What | Leaves the installation | Is withheld |
|---|---|---|
| A name the forwarding remote holds a stored upstream document for (`npm_remote_packages`, also one marked gone) | yes, with the versions the client listed | — |
| A name that a package row or a tombstone of any npm local repository holds | — | `local` |
| A name a namespace claim of any scope of an npm local repository covers | — | `claimed` |
| A name a pin rule of the requesting virtual repository selects | — | `pinned` |
| A name the remote never fetched, one its or the virtual repository's patterns refuse, one the caller may not read on the forwarding remote (its grants, role, token scope and endpoint, decided through the addressed repository, Q-NPM-81), and what is no npm name | — | `not_proxied` |

One statement decides all of it before the provider is asked (`NPMAuditNames` in
[npm_remote.sql](../../internal/meta/postgres/queries/npm_remote.sql)), on committed rows, so a claim made a moment ago on another
replica withholds already. The withheld names are counted in `quader_npm_audit_names_withheld_total{repo,reason}` and named, as a
count, in the `npm-notice` header of the answer. Nothing of the request is stored, logged beyond the access record, or kept in Valkey.

Where the request goes and what travels with it:

- **The provider** is `npm.audit.providerURL` of the forwarding remote, by default its upstream URL. The URL is `https` and carries no
  user info, query or fragment (the schema and `settingsOf` refuse them), because the repository document is shown in clear to every
  holder of `repositories:read-config` (H-303 of the settings form's work). A credential never travels in it.
- **The connection** is the remote's upstream client ([egress.md](egress.md#which-connections-are-checked)): the address policy, the
  trust store and the redirect rules apply, and a refusal is `502 egress_denied` with the security event `proxy.egress_denied`.
- **The credential** is the remote's sealed upstream credential, added by the client's credential policy only on a request to the
  upstream origin or to an `extraHosts` entry with `sendCredentials: true`
  ([egress.md](egress.md#which-hosts-receive-credentials)). A `providerURL` on another host gets no credential.
- **The answer** is the provider's body, passed to the client unchanged and bounded to 128 MiB; Quader does not check what it says
  ([H-300](#h-300--the-advisory-providers-answer-is-trusted-as-it-is)).

What the audit does not send: the client's own credential (the request to the provider is Quader's), the `npm-command`, `npm-session`
and other client headers, the project's name, and any name of the table's lower rows. What it does send is still the combination of
public names a project uses: [H-34](../spec/formats/npm.md#h-34--audit-forwarding-discloses-the-dependency-set).

## The release pipeline's credentials

The workflows that test, release and publish Quader hold credentials that the running installation never sees, but that
decide what every installation pulls: the image and the chart. They are secrets of the organisation
([releasing.md](../developer/releasing.md#secrets-and-settings)), and each job that names one receives it. Every action is
referenced by a tag, not by a commit (the owner's decision of 2026-10-31,
[ADR-130](../decisions.md#adr-130--the-organisations-ci-and-release-standard)), so the third-party actions of a job run with
what that job holds:

| Credential | What it can do | Jobs that hold it | Third-party actions in those jobs |
|---|---|---|---|
| `DOCKERHUB_PAT`, the access token of the Docker Hub user `guidedtraffic` | push `docker.io/guidedtraffic/quader` under any tag | `Build Docker Image` ([build.yml](../../.github/workflows/build.yml)); in [release.yml](../../.github/workflows/release.yml) `image`, `container-scan`, `integration`, `s3-conformance`, `e2e`, `e2e-npm`, `e2e-nuget`, `e2e-ci`, `e2e-bastion`, `e2e-vuln` and `e2e-oci`, which log in for base-image pulls, on pull requests from this repository's branches too ([H-80](../spec/ha-and-helm.md#h-80--self-hosted-runners-build-the-code-of-every-pull-request)); the five before `e2e-oci` run on GitHub-hosted runners (`ubuntu-24.04`), `e2e-oci` on a large runner; in [performance.yml](../../.github/workflows/performance.yml) the fifteen jobs `latency`, `vuln-perf`, `npm-ha-soak`, `e2e`, `e2e-npm`, `e2e-nuget`, `e2e-oci`, `e2e-ci`, `e2e-bastion`, `e2e-vuln`, `e2e-backup`, `e2e-edge`, `e2e-alerts` and `e2e-upgrade`, which log in for image pulls, every night and on a dispatch of any ref the dispatcher can push to | `Build Docker Image`: the `docker/*` actions, `anchore/sbom-action@v0`, `softprops/action-gh-release@v3`, `docker/scout-action@v1` (the token as an input), and the throwaway registry container (the image `SBOM_GENERATOR_REGISTRY_IMAGE`, by digest) that holds no credential and shares Docker's default bridge network with the builder and the `RUN` steps of the image build (the generator is referenced by digest, so none of them can replace what the builder runs) (the job `sbom-generator` of release.yml pulls anonymously and holds no secret); `container-scan`: `aquasecurity/trivy-action@v0.36.0` and the binary `make trivy` took out of `TRIVY_IMAGE` (pulled with that login), after `docker/login-action` has stored the login on the runner; the other jobs of release.yml and performance.yml: `docker/*`, `azure/setup-helm@v5`, `azure/setup-kubectl@v5` |
| `APP_CLIENT_ID`, `APP_PRIVATE_KEY`, the organisation's GitHub App | an installation token for this repository: tags, releases (a release starts build.yml), commits and pushes to any branch | `Semantic Release` (release.yml, token with `contents: write` and nothing else: the tag, the release, and since [Q-ARCH-811](../spec/architecture.md#q-arch-811--the-pipeline-writes-the-evidence-record-and-the-coverage-badge-onto-the-branch-coverage-main-holds-neither) the push of the branch `coverage` by [publish-coverage-record.sh](../../tools/publish-coverage-record.sh), which never pushes `main` and never forces; a token cannot be limited to one branch, so the branch adds no reach the job did not have. The token has no `workflows` permission, so it cannot create or change a workflow on any branch: GitHub refuses such a push, and a commit on `coverage` holds the record and the badge alone ([Q-ARCH-1040](../spec/architecture.md#q-arch-1040--a-commit-on-the-branch-coverage-holds-the-record-and-the-badge-alone))), `renovate` ([renovate.yml](../../.github/workflows/renovate.yml), token with every permission of the App) | `Semantic Release`: none, only `actions/*`; `renovate`: `renovatebot/github-action@v46.3.10` |
| `GITHUB_TOKEN` of release.yml (the workflow default is `contents: read`) | `coverage-evidence` and `Semantic Release`: `actions: read` beside `contents: read`, to list the runs of `main` and download their report, record and badge artifacts. The artifacts taken come only from runs of this repository started by a push, a schedule or a dispatch, never from a pull request's or a fork's run, and only from runs of the commit itself or an ancestor | `coverage-evidence`, `Semantic Release` | as above; the token goes to `curl` on standard input, never on its command line |
| `GITHUB_TOKEN` of build.yml (the workflow default is `contents: read`) | `Build Docker Image`: `contents: write`, for the release's assets; `Release Helm Chart to GitHub Pages`: `contents: write`, for the release's assets and the push of the `gh-pages` branch that serves the chart repository, and `pages: write`, which no step of the job uses (GitHub Pages builds the site from the push). No job holds `id-token`, `attestations` or `actions` | both jobs of build.yml | as for `DOCKERHUB_PAT`, and `azure/setup-helm@v5` and `softprops/action-gh-release@v3` in the chart job |
| `GITHUB_TOKEN` of performance.yml (the workflow default is `contents: read`) | `vuln-perf`: `issues: write`, to open a regression issue after a miss or a failed run; it can open, edit and comment on every issue of the repository. The other jobs: `contents: read` | `vuln-perf` | `docker/login-action@v4` (with `DOCKERHUB_PAT` in the same job) |

**The job token in release downloads.** The jobs that download a GitHub release asset send their `GITHUB_TOKEN` to github.com, in the request for the release URL (not to the API), and to no other host. The request is the one `actions/setup-go` makes with its default token; github.com does not validate the header on that route (measured during the incident of 2026-11-08), so the token is not what makes the download work, and it is sent anyway. [tools/github-download.sh](../../tools/github-download.sh) sends it on stdin in a request that does not follow redirects, and fetches the redirect target (the storage host) in a second request without it. The steps that hold the token while they run are the buildx installation of a job that needs the pinned plugin, `make dev-charts` (the haproxy-ingress and cloudnative-pg archives) and `make promtool`; each runs only that download (`make trivy` downloads nothing from GitHub since 2026-11-21: it takes the analyser out of `TRIVY_IMAGE` with Docker's login and holds no `GITHUB_TOKEN`), and the steps of `make dev-up` and `make chart-test` do not hold it. The token's scope is `contents: read` in every job except `build`, where it is `contents: write` ([H-589](#h-589--release-downloads-send-the-job-token-to-githubcom-two-residuals)). The token is never printed or written to a file. Without a token (a local run) the same download is unauthenticated. The digest of the archive, not the request, decides what is used.

No job runs in a GitHub environment with protection rules, so the secrets are bound to nothing but the job that names them.

## What this does not cover

The gaps of the spec pages whose mechanism this page describes:

- [H-2](../spec/architecture.md#h-2--the-upload-spool-is-not-encrypted-by-quader): the upload spool is not encrypted by Quader.
- [H-8](../spec/repositories-and-proxy.md#h-8--the-stored-secret-key-sits-on-every-replica): the stored-secret key sits on every replica.
- [H-55](../spec/architecture.md#h-55--key-files-of-the-stored-secret-key-ring-are-read-once-at-start-up): key files of the stored-secret key ring are read once at start-up.
- [H-34](../spec/formats/npm.md#h-34--audit-forwarding-discloses-the-dependency-set): the audit forward discloses the dependency set.
- [H-79](../spec/ha-and-helm.md#h-79--artifacts-are-unsigned-and-nothing-checks-their-origin): the image and the chart are unsigned, and nothing checks their origin.
- [H-80](../spec/ha-and-helm.md#h-80--self-hosted-runners-build-the-code-of-every-pull-request): self-hosted runners build the code of every pull request, and its jobs hold `DOCKERHUB_PAT`.

### H-1025 · An account stored on a well-known repository pulls for everyone who reads it

Stage: the account of a repository of the project `upstream` ([repositories-and-proxy.md](../spec/repositories-and-proxy.md) R-REPO-069,
built). Subject: whatever the account can read at its registry beyond the public content, and the account's pull
allowance.

- **Live by design.** Quader does not know what an account can read; a fetch through the repository
  sends it for every miss of every caller the repository's level and the global policies on it admit. An account with access to private content at
  the registry turns that content into content of the installation, readable by those callers without any grant that names it. The
  admin page says so beside the account's fields (R-UI-188); nothing checks it.
- What makes it real: an administrator who stores a personal or organization account instead of an account that reads only public
  content. Use an account made for pulling public content, or keep the repository `private` and grant it through global policies.

### H-768 · The dev stack archives PostgreSQL with the blob bucket's login

Stage: the WAL archive and the base backups of `QUADER_PITR=1` ([pitr.yaml](../../deploy/dev/pitr.yaml)), which R-HA-023 asks to keep in a bucket
other than the blob bucket and under different credentials.

Live in the dev profile only: the `ObjectStore quader-pg-archive` reads the Secret `quader-s3`, the one SeaweedFS identity that also writes and
deletes the blob bucket and the backup target, so a compromised Quader replica that holds that login could delete the archive. The bucket is
separate, the link is TLS against the dev CA. A production installation gives the archive its own login; the dev SeaweedFS identity file
(one identity) has no second one. Subject: the archive of the dev cluster, which holds nothing but test data.

### H-70 · The standard-output log carries every record in full

Stage: every log record written to standard output, which the platform's log stack collects
([observability/ring.go](../../internal/observability/ring.go) bounds only the in-memory copy).

Dormant: the in-memory copy is bounded, but the line on standard output is the whole record. A record above 16 KiB is kept in
the ring as a stub with its head and its original size, while standard output keeps all of it. The request log clips the
user agent, path and host before they reach a record ([requestlog.go](../../internal/observability/requestlog.go)), so the
gap becomes live only with a new attribute that carries client data unclipped, or with a value that a scrubber pattern does not
match. Subject: the log storage of the platform, which keeps what standard output carries.

### H-280 · A secret that cannot be resolved is found at its first use

Stage: the repository's upstream client, built at the first request after a start or after a change of the repository
([upstream/clients.go](../../internal/proxy/upstream/clients.go), `buildSaved`).

Live: the specification says such a secret is reported at start-up ([R-REPO-033](../spec/repositories-and-proxy.md)). A replica does not
read every repository at start-up; it logs the failure when the client is built and fails that repository's requests with
`502 upstream_secret_unavailable`. Subject: a repository whose reference file is missing on one replica, or whose sealed value was
sealed under a key that replica's ring lacks, until a request or an upstream test reaches it. Other repositories are unaffected and
readiness does not depend on it.

What makes it real: a rolled-out secret file that misses a replica, or a key ring that is rolled out in the wrong order (the canary
check at start-up catches a key that the database references, not a reference file). The upstream test of the repository reports it on
demand. An account the configuration declares (R-REPO-077) is found the same way, on purpose
([Q-REPO-485](../spec/repositories-and-proxy.md#q-repo-485--a-missing-empty-or-wrong-file-fails-that-registrys-fetches-and-stops-nothing-else)):
its Secret may be created after the install; the registry's page in the admin area shows the state of its files on the replica that
answered.

### H-281 · A holder of `repositories:write-config` can send any file of `secrets_dir` to an upstream of their choice

Stage: the resolution of a `secretRef` ([upstream/secret.go](../../internal/proxy/upstream/secret.go), `resolver`) together with the
choice of the upstream URL.

Live: whoever may create or change a remote repository chooses both the URL and the reference, so a reference to a file that belongs to
another repository's upstream, with this repository's URL pointing at a host the holder controls, sends that file's content there in
the `Authorization` header. The same holds for every variable with the prefix `QUADER_CAC_`. The rules of R-OPS-029 confine the reference
to the directory and the prefix, so Quader's own secrets are out of reach, but they do not separate the secrets inside them. Subject: the
upstream tokens that the operator mounts under `config_as_code.secrets_dir`, one per partner.

What makes it real: one `secrets_dir` that holds the tokens of several upstreams and a repository administrator who is not meant to
know all of them. The mitigation is operational: mount only the references that the holders of the capability may use.

Not reached by an Owner of a project without the capability: its writes and its unsaved upstream test take no reference but the one
the repository holds, and a stored credential does not follow a change of `url`, of a credential host or of `forwardAuthOnRedirect`
([Q-REPO-127](../spec/repositories-and-proxy.md#q-repo-127--a-project-administrator-gives-an-upstream-secret-as-a-value-and-a-stored-one-stays-with-its-hosts)).
What an Owner still reaches of an upstream an administrator configured is
[H-403](delegated-administration.md#h-403--what-a-project-administrator-still-reaches-of-an-upstream-an-administrator-configured).

Not reached through a well-known repository of the project `upstream` since 2026-11-25: its account operation takes a value only
([Q-REPO-487](../spec/repositories-and-proxy.md#q-repo-487--the-account-operation-of-a-well-known-repository-takes-a-value-only)), and an
account the configuration declares is read from paths outside `secrets_dir` that no reference can name and that only the operator points
at a registry (R-REPO-077). The gap stays for the remotes of the projects.

### H-300 · The advisory provider's answer is trusted as it is

Stage: the answer of an audit forward ([handlers_audit.go](../../internal/formats/npm/handlers_audit.go), `handleAudit`,
[R-NPM-038](../spec/formats/npm.md#r-npm-038--security-audit-endpoints)).

Live wherever a remote forwards (the default). Quader passes the provider's 200 body to the client unchanged; it checks neither its
shape nor a signature. Subject: what `npm audit`, `pnpm audit` and `yarn npm audit` report to developers and CI gates. A provider, or
whoever controls the path to it, can leave out advisories (a vulnerable package reported clean) or add false ones (a build blocked).
The TLS verification of the upstream client protects the path; the provider itself is the operator's choice, by default the upstream.

What makes it real: a `providerURL` that points at a service the operator does not control, or an upstream that is itself a mirror of
unknown provenance. Mitigation: leave `providerURL` empty or point it at a provider the organization trusts; treat audit results as
one signal next to the vulnerability scanning of the installation.

### H-301 · An audit body is held in memory

Stage: the reading of an audit body ([handlers_audit.go](../../internal/formats/npm/handlers_audit.go), `readAuditBody`).

Live: a body is read and decompressed in memory before the forwarded set is decided, up to ten times `npm.audit.maxBodySize`
(10 MiB with the default of 1 MiB, 100 MiB with the largest setting) plus the decoded names, at most 100,000
([Q-NPM-64](../spec/formats/npm.md#q-npm-64--the-bounds-of-an-audit-body)). No budget per replica bounds the audits that run at once.
Subject: the memory of the `packages` replicas. A caller with `read` on a forwarding repository (an anonymous one where anonymous read is
on) can send many large gzip bodies at once.

What makes it real: a raised `maxBodySize` together with a burst of large audits; the request rate limits of the edge are the only
bound beyond the per-request one. Mitigation: keep the default, and keep anonymous read off on repositories that forward audits.

### H-302 · pnpm and Yarn do not show that an audit was partial

Stage: the `npm-notice` header of an audit answer ([R-NPM-038](../spec/formats/npm.md#r-npm-038--security-audit-endpoints)).

Live: a withheld name is absent from the answer, which every client reads as "no advisories". npm prints the notice that says how many
names were withheld; pnpm 11 and 12 and Yarn 4 do not print it on a successful answer (measured with the pinned clients on 2026-10-31).
Subject: the developers and CI jobs that audit with pnpm or Yarn. A package installed from elsewhere (a lockfile resolved against
another registry, a tarball dependency) that a remote never fetched is reported clean, whatever advisories exist for it.

What makes it real: lockfiles that were not resolved through Quader, and audit gates in CI that run `pnpm audit` or `yarn npm audit`.
Mitigation: install through Quader before auditing (an install fetches every public package of the tree), and watch
`quader_npm_audit_names_withheld_total{reason="not_proxied"}`, whose sustained growth is the revisit condition of
[Q-NPM-3](../spec/formats/npm.md#q-npm-3--audit-forwarding-on-by-default).

### H-530 · The mirror status shows the engine's failure text as stored

Stage: the status of a remote repository's mirror ([mirror_api.go](../../internal/api/v1/mirror_api.go), `buildMirrorStatus`) and the
failure text the engine stores for it (`mirror_state.last_error`, at most 1024 characters). Subject: the upstream URLs and credentials
that a failure's text could carry, and who can read the status: a holder of `repositories:read-config`, a holder of `manage` on the
repository (a Maintainer or an Owner of its project among them) and an Owner of its project
([Q-REPO-166](../spec/repositories-and-proxy.md#q-repo-166--errorslastmessage-is-shown-as-the-engine-stored-it)).

- **Dormant until the engine's text includes more than hosts.** R-REPO-056 and R-REPO-024's rule for the upstream test say the message
  names hosts and never a URL or a credential; the engine builds it from the outcome of a request, not from the upstream's answer.
  The status shows it unchanged, so a message that broke the rule would reach every reader of the status, and a `manage` holder or an
  Owner is not meant to see an upstream's credential or a path that carries one.
- What makes it real: an engine message built from a part of an upstream's answer, from a request URL, or from an error of the HTTP
  client that quotes the URL. The engine's tests of its messages are the guard.
- Not covered by the status: the log and the failure event of the engine carry the same text.

### H-1960 · A failure's report carries what the answer named

Stage: the shared error notice of the web UI and its **Copy report**
([error-notice.ts](../../web/src/app/shared/error-notice.ts), R-UI-216). Subject: what a failed answer names besides a credential: the
server's own sentence of a problem (`detail`, which may name a repository, a path or a host of the installation), the headers of a
gateway (its software and version, `server: nginx/1.27.3`) and its error page, and the path of the call with its query.

- **Live by design.** The notice shows them to the person whose browser received the answer, who could read them in the developer
  tools before; the report copies them as they are, and a ticket or a chat that receives it gives them to whoever reads there. A
  gateway's error page that names an internal address, or a server's sentence that names a project the ticket's readers may not see,
  goes with it.
- What makes it real: an edge whose error pages name internal hosts, and a report pasted where people read who may not see the
  installation. The report holds no credential of the rules of the section above; whatever else the answer carried, it carries.

### H-1961 · A credential in a query parameter of another name is shown

Stage: `redactUrl` of [failure.ts](../../web/src/app/core/http/failure.ts), which reads a parameter's name (R-UI-216). Subject: a
credential that a future call of the UI sends in its query under a name the rule does not know.

- **Dormant.** No call of the UI sends a credential in its query (verified 2026-11-25 against the clients of `web/src/app/api`); a
  cursor (`cursor=`) is an opaque position, not a credential. The rule reads names, so a call that put a token into `q=` or into
  `cursor=` would show and copy it.
- What makes it real: a new call that authenticates by its query. Its parameter's name joins the rule in the same change.

### H-325 · A moved action tag runs in a job that holds release secrets

Stage: the actions of [release.yml](../../.github/workflows/release.yml), [build.yml](../../.github/workflows/build.yml),
[performance.yml](../../.github/workflows/performance.yml) and [renovate.yml](../../.github/workflows/renovate.yml), each referenced by a tag
([the release pipeline's credentials](#the-release-pipelines-credentials)).

Live wherever the workflows run: Test and Release on every push and on every pull request from a branch of this repository,
Renovate every day, the performance workflow every night and on every dispatch of any ref the dispatcher can push to (its twelve
jobs that pull images run that ref's workflow file with `DOCKERHUB_PAT`, and `vuln-perf` with `issues: write` as well), Release
Docker & Helm on every published release and on every dispatch of any ref the dispatcher can push to (the check job runs that ref's
workflow file with `DOCKERHUB_PAT` and a read-only `GITHUB_TOKEN`; the publish jobs do not run on a dispatch). Release Docker & Helm
has published releases: run 42436292019 published `v0.25.2` with all three jobs green. A tag is a movable reference: whoever controls an action's repository, or a maintainer's account, can point it at another
commit, and the next run of a job that names the tag runs that commit with the job's secrets. Renovate does not notice: it
raises a tag when a new one appears, and a moved tag keeps its name. In the Trivy incident (GHSA-69fq-xp46-6x23, March 2026)
76 of 77 tags of `aquasecurity/trivy-action` were moved to malicious commits. The owner decided on 2026-10-31 to keep tag pins,
as the organisation's standard has them, after the risk was named
([ADR-130](../decisions.md#adr-130--the-organisations-ci-and-release-standard)).

Subject, per credential:

- `DOCKERHUB_PAT`: a moved `aquasecurity/trivy-action` tag runs in `container-scan` after the Docker Hub login, on every push
  and pull request; a moved `softprops/action-gh-release` or `docker/scout-action` tag runs in the job
  that pushes the image, at the next release; the `docker/*` and `azure/*` tags of the other jobs that log in, in release.yml and
  in performance.yml, run with the login as well. With the token, the image of an existing tag can be replaced
  ([H-79](../spec/ha-and-helm.md#h-79--artifacts-are-unsigned-and-nothing-checks-their-origin)), so every installation that
  pulls by tag runs it.
- The GitHub App: a moved `renovatebot/github-action` tag runs in Renovate's daily job with an installation token that holds
  every permission of the App: it can push commits and tags and publish a release, which starts Release Docker & Helm.
- The `GITHUB_TOKEN` of build.yml: a moved tag in the two publish jobs, which hold `contents: write`, can change the release's assets and the chart
  repository on `gh-pages`.
- The `GITHUB_TOKEN` of performance.yml: a moved `docker/login-action` tag in `vuln-perf` can open and edit issues of the repository.

What makes it real: a compromised repository or maintainer account of one of the third-party actions above, before the moved
tag is noticed and restored.

Hardening that would mitigate it (none is in place):

- commit-SHA pins, with the tag as a comment, for the third-party actions of the jobs that hold these secrets:
  `aquasecurity/trivy-action`, `softprops/action-gh-release`, `docker/scout-action` and the `docker/*`
  actions of build.yml, and `renovatebot/github-action`; a moved tag then changes nothing until a reviewed update;
- a GitHub environment with protection rules (deployment from `main` only, required reviewers) for the jobs that publish,
  holding `DOCKERHUB_PAT` and the App's credentials as environment secrets, so that the test jobs and pull requests cannot
  read them, with a separate read-only Docker Hub token for the base-image pulls of the test jobs;
- Renovate's digest pinning for GitHub Actions (`helpers:pinGitHubActionDigests`), which keeps SHA pins current through
  reviewed pull requests;
- an allow list of actions at the organisation level (the allowed actions of the organisation's Actions settings), so that a
  workflow change cannot bring in an action nobody approved.

### H-578 · Renovate merges digest updates of digest-pinned images on green CI

Stage: [renovate.json](../../renovate.json): the preset `:automergeDigest`, and the package rule that automerges `minor`, `patch` and `digest` updates of the Docker images in
`tools/versions.env` (the runtime base image `RUNTIME_BASE_IMAGE`, `SYFT_IMAGE`, `SBOM_GENERATOR_SHELL_IMAGE`, `SBOM_GENERATOR_REGISTRY_IMAGE`, the integration and dev-profile images).

Live for every digest-pinned image: when the registry's tag is moved to another image, Renovate raises a digest-only update, and it merges when CI is green, without a review. A moved
upstream tag (a compromised publisher, a rebuilt tag) reaches the pin that way, and from there the release image's base (`RUNTIME_BASE_IMAGE`), the syft that writes both SBOMs (`SYFT_IMAGE`), or the
containers of the test and release jobs. The pin makes the change visible in the history; it does not stop it. What makes it real is a moved tag of one of these images that passes the tests.

What would close it: excluding digest-only updates from automerge for the images that enter the release (`RUNTIME_BASE_IMAGE`, `SYFT_IMAGE`) and keeping it for the test images. Whether to do that is
the owner's policy; the automerge policy is unchanged.

### H-589 · Release downloads send the job token to github.com: two residuals

Stage: the steps that download a GitHub release asset in [build.yml](../../.github/workflows/build.yml), [release.yml](../../.github/workflows/release.yml) and
[performance.yml](../../.github/workflows/performance.yml): the buildx installation, `make dev-charts` and `make promtool`.

Residual 1, local: the token is in the environment of the whole step that downloads, so every process of that step can read it (the step is a shell with the job's
workflow commands, and a third-party action of the same step would see it). The sending itself is not the risk: the token goes to github.com, which issued it. In the
job `build`, the token has `contents: write` for the release assets, so a process of that step that reads the environment can write to the repository; the buildx
installation of `build` downloads only when the runner has not the pinned file. Narrowing it needs a read-only token for the download, which no step of a job with
`contents: write` has. What makes it real is a process of one of these steps that reads or logs its environment.

Residual 2, provenance: `CNPG_CHART_SHA256` and `DEV_HAPROXY_INGRESS_CHART_SHA256` are the digests the chart indexes list and GitHub reports for the release assets. The
cloudnative-pg release publishes a helm provenance file next to its archive (`cloudnative-pg-0.29.1.tgz.prov`); the haproxy-ingress 0.16.3 release has only the archive. The provenance file is not verified: no publisher key is pinned. The digest
check catches a replaced archive unless the index that lists the digest is replaced with it; the provenance file is what would show a signature by another key, and it is
not checked. What makes it real is a compromise of the chart repository's index together with the release. What would close it: pinning the publisher's key and running
`helm verify` on the archive (the key is the owner's to choose and to verify with the project).

### H-1940 · Replicas send the old and the new token of a declared account side by side for up to two minutes

Component: the files of `secrets.upstreamAccounts` in the pods of `api`, `packages` and `worker` (R-HA-073, R-REPO-077). Subject: an
operator who rotates an upstream registry's token. Live with every declared account. The kubelet swaps a Secret's files within its sync
(R-HA-030's 2 minutes, per node), and each replica reads them again after 10 s, so for that time some replicas send the old token and
some the new one. Revoking the old token at once fails the fetches of the replicas that still send it (`upstream_auth_failed`). What
makes it real: a rotation that revokes before every pod has the new files; the manual's steps (create the new token, update the Secret,
wait three minutes, revoke the old one) avoid it.

### H-1942 · The development profiles keep the owner's upstream token in plaintext files of the machine

Component: `<main checkout>/.dev/credentials/upstream-registries.env` and the copies [upstream-accounts.sh](../../deploy/dev/upstream-accounts.sh)
writes, `.dev/ui-dev/upstream-accounts/docker-hub/` and `.dev/upstream-accounts/docker-hub/` (Q-ARCH-1066). Subject: every process that
runs as the owner's user on his development machine: builds, tests, the install scripts of the web workspace's packages, editor
extensions, and a backup of the home directory that does not exclude the checkout. Live while the file holds an account; dormant with the
placeholder. The modes `0600` and `0700` keep other users of the machine out, not the owner's own processes, and the copies sit beside the
source until `make ui-dev-down PURGE=1` or `make dev-down`. What makes it real: code that runs as that user and reads files, such as a
compromised dependency. What it reaches: a token of the scope "Public Repo Read-only" pulls public content under the owner's name and from
his allowance, nothing more; revoking it at Docker Hub ends it.

### H-1943 · The kind profile's cluster holds the owner's upstream token where its kubeconfig and its nodes reach

Component: the Secret `quader-upstream-docker-hub` and each node's `/var/lib/kubelet/config.json` of the kind profile (Q-ARCH-1066,
Q-ARCH-1068). Subject: whoever holds the cluster's kubeconfig (an administrator of the cluster, which kind's own is), the profile's
operators that read Secrets of every namespace (the internal-secrets-operator caches every Secret of the cluster,
[local-profile.md](../developer/local-profile.md)), and whoever can talk to the machine's Docker daemon (`docker exec` into a node). Live
while the main checkout's cluster exists and the file holds an account; never in CI. kind keeps the Secret in etcd without encryption at
rest, and every pod of the cluster whose image is on Docker Hub is pulled with the account. `make dev-down` deletes the cluster and the
copies; `up.sh` with a placeholder file deletes the Secret and the nodes' entry. What makes it real: a kubeconfig or a Docker socket in
other hands than the owner's.

### H-1944 · The profiles and the owner's installation draw on one Docker Hub account's allowance

Component: the account of the owner's file (Q-ARCH-1066) and the account his installation declares in `secrets.upstreamAccounts`
(R-HA-073). Subject: the owner's installation, when both are the same Docker Hub user. Live in that case. Docker Hub counts its limit per
user, so a bulk download through a profile's `upstream/docker-hub` (on 2026-11-25 one of 315 tags on a machine of the site used up the
address's anonymous allowance) or the kind nodes' pulls spend what the installation needs, and its fetches then wait on `429`. What closes
it: a second Docker Hub user for the development machine, the owner's operational choice.

### H-1941 · Quader's audit trail does not show a rotation of a declared account's Secret

Component: the accounts of `repositories.upstream_accounts` (R-REPO-077). Subject: an auditor who asks when an upstream registry's token
changed. Live with every declared account. `quader migrate` records a declaration and its removal; a change of the Secret's content
moves no configuration, no row and no event, and the replicas read the new files without saying so. The Kubernetes API's audit log
holds the Secret's update. What makes it real: an investigation of which token sent a request at a given time, which Quader's records
cannot answer.
