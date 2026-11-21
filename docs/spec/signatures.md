# Signatures on artifacts

Status: stage 1 is in work. Built (packages 1, 2 and 3 of 4, [developer/signatures.md](../developer/signatures.md)): the format
contract's declaration and detector of every format, kit check 18, the store of signature objects, signatures, attachments and origins
with the commit hook and the fill hook that record them, and OCI's attachment tags without a version row (package 1); the read and the
integrity check of every scheme, time, the trust step with its scopes, the four kinds of anchor with the shipped ones, those of the Helm
values and the projects' with the switch, blocks, the jobs `signature-verify` and `signature-trust` with discovery, the task
`signature-trust-root`, the API of R-SIG-034, the audit events, metrics, alerts and the configuration (package 2); the formats' wire
behaviour of R-SIG-023 to R-SIG-033: an OCI attachment follows its image on delete and eviction and stays out of the image scanning and
of a mirror's versions; the background fetch of a remote's signatures (job `signature-fetch`) and the mirror's; npm's keys and
attestations endpoints of every class; the signatures a client posts for a local Ansible Galaxy version; Debian's coverage port; the
kit's case that a signature follows its subject (package 3). Not built: the screens. Stage 1 of three: Quader detects, reads and verifies the signatures of artifacts of every format, keeps
each original signature with its verdict, decides trust against trust anchors of the installation and, where the administrator allows it,
of a project, shows the result at the artifact, alarms on an invalid signature, and serves signatures to every client the way its
protocol expects. It blocks nothing. Stage 2, Quader signs, is specified in the section
[Quader signs (stage 2)](#quader-signs-stage-2) (R-SIG-044 to R-SIG-060, specified, not built): one signing identity per installation,
npm's registry signatures of local packages and a signed receipt attestation of every artifact. Stage 3, the gate per repository, is
specified in the section [The gate per repository (stage 3)](#the-gate-per-repository-stage-3) (R-SIG-061 to R-SIG-078, specified, not
built): a detail setting of a repository that lets only artifacts with a verified signature, optionally of chosen anchors, be downloaded.

The owner asked for it on 2026-11-21 ("Quader muss in Zukunft auch das feststellen, auslesen und verifizieren von Signaturen auf (wenn
möglich) allen Artefaktarten unterstützen. Dabei muss es zwischen öffentlich gültigen oder intern gültigen Signaturen unterscheiden
können. Ein Admin muss also gültige unternehmens CAs einstellen können, wie zum Beispiel eine CA der internen Gitlab-CI. Er muss
einstellen können, ob Projekt-Owner weitere CAs für ihr Projekt konfigurieren können (default nein). … Das muss bei zukünftigen
Registries automatisch mit berücksichtigt werden. … von der Darstellung her, soll eine Signatur aber nicht alleine stehen sondern klar
einem Image zugeordnet werden. … signierte Artefakte aller Art werden von uns erkannt, im Frontend mit einem Icon besonders gekennzeichnet
und auch die Metadaten wie Zeitpunkt und Aussteller (alle anderen Infos ebenso) werden durch uns dargestellt. Kommt eine Software über
die API vorbei, erhält sie die signierungen wie ihr Interface es erwartet.") and decided its design in his working paper the same day:
eight readings ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request)), the way to a signature his CI has
([Q-SIG-15](#q-sig-15--which-way-to-a-signature-the-ci-has)) and fourteen decisions
([Q-SIG-1](#q-sig-1--what-the-icon-stands-for) to [Q-SIG-14](#q-sig-14--the-order-of-the-work);
[ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact) to
[ADR-228](../decisions.md#adr-228--the-format-contract-declares-every-formats-signatures-and-a-signature-is-never-a-version)).

This page covers:

- the model: what a signature, its subject, its role, its scheme and its original are; the store of the signatures of the whole
  installation, once per content; where a signature reaches a subject in a repository; the read, the integrity check and the trust step,
  each with its states; time; the state of an artifact; the origin of an upload;
- the work: the worker's job, the recomputation of trust without reading an artifact, the bounds on hostile signature containers, the
  libraries;
- trust anchors: the four kinds, their checks, their scopes (shipped, installation, project), the anchors of the Helm values, the
  administrator's switch for project Owners, blocked signers, the permissions;
- the formats: the obligation in the format contract and the kit's case, and what OCI, npm, NuGet, Debian, Ansible Galaxy and generic
  repositories detect, verify, show and serve; the fetch of a remote's signatures in the background; virtual repositories and the Bastion;
- the API, the badge and the details, the admin page, a project's anchors, the security dashboard's view, audit events, metrics, alerts,
  the configuration, backups and replicas, and the tests with their cost;
- stage 2: what Quader signs per format; the signing identity, its two ways (the Helm values, an upload in the admin area), its states,
  its rotation and where it is used; npm's registry signatures of local packages and the keys endpoint; the receipt attestation, its
  statement, its envelope, when it is made and where it is kept and served; its API, screens, events, metrics, alerts, configuration,
  backups and tests;
- stage 3: the repository's setting and who may change it; what passes state by state; what the gate judges per format and where it acts;
  the scope and virtual repositories; narrowings at the gate; the pending case and its bounded wait; each client's answer; the order with the
  malware gate and the vulnerability block; releases; the preview and the impact before a change; its API, screens, events, metrics, alert,
  configuration, backups and tests, and its build plan.

**What a signature says, and what it does not.** A valid signature says who signed which bytes, and the trust step says whether an anchor
the installation chose knows that signer. It does not say that the content is safe: a trusted signer can sign malicious content, and a
signed package can have vulnerabilities and malware. The malware scan ([malware-scanning.md](malware-scanning.md)) and the vulnerability
scanning ([vulnerability-scanning.md](vulnerability-scanning.md)) are separate checks, and no screen calls a signature a security
guarantee ([H-1171](../security/artifact-signatures.md#h-1171--a-verified-signature-says-who-signed-not-that-the-content-is-safe)).

| Topic | Owner |
|---|---|
| The format contract, its kit and the readers of untrusted content | [architecture.md](architecture.md) R-ARCH-008, R-ARCH-010, R-ARCH-028; [developer/formats.md](../developer/formats.md) |
| The item store's commit hook, blobs and the collection | [storage.md](storage.md) R-STOR-002, R-STOR-005, R-STOR-024 |
| The pull-through engine, background requests, the mirror | [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-025, R-REPO-050 to R-REPO-059, R-REPO-065 |
| The well-known repositories of `upstream` and the presets of a format | [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-062, R-REPO-066 to R-REPO-071 |
| OCI manifests, referrers, deletion, the remote and the virtual class | [formats/oci.md](formats/oci.md) R-OCI-007, R-OCI-015, R-OCI-016, R-OCI-018, R-OCI-024, R-OCI-026, R-OCI-027 |
| npm's documents, the publish and the remote class | [formats/npm.md](formats/npm.md) R-NPM-006, R-NPM-007, R-NPM-027, R-NPM-030 |
| NuGet's package, its storage and the service index | [formats/nuget.md](formats/nuget.md) R-NUGET-005 |
| Debian's releases, their verification and the indexes | [formats/debian.md](formats/debian.md) R-DEB-016, R-DEB-019, R-DEB-020, R-DEB-023 |
| Ansible Galaxy's version document | [formats/ansible.md](formats/ansible.md) R-ANS-013 |
| Generic files | [formats/generic.md](formats/generic.md) |
| CI tokens and their issuance records | [gitlab-ci-oidc.md](gitlab-ci-oidc.md) R-CI-013, R-CI-020 |
| System roles and capabilities, the restricted visibility levels the switch is modelled on | [authorization.md](authorization.md) R-AUTHZ-010, R-AUTHZ-037 |
| Usage and limits of system work | [usage-and-limits.md](usage-and-limits.md) R-LIM-001, R-LIM-015 |
| The admin area, the security dashboard | [api-and-ui.md](api-and-ui.md) R-UI-002, R-UI-158 |
| The client setup snippets, which gain the tab `verify` (stage 2) | [api-and-ui.md](api-and-ui.md) R-API-030 |
| The key ring that seals an uploaded signing key (stage 2) | [architecture.md](architecture.md) R-ARCH-021; [developer/secrets.md](../developer/secrets.md) |
| The chart's Secret or cert-manager Certificate of the signing identity (stage 2) | [ha-and-helm.md](ha-and-helm.md) R-HA-071 |
| npm's document revisions and publish times, which signing moves and npm's client compares (stage 2) | [formats/npm.md](formats/npm.md) R-NPM-009, R-NPM-012 |
| The malware gate at the serve points, its wait, its answers and its release, the pattern of stage 3 | [malware-scanning.md](malware-scanning.md) R-MAL-013 to R-MAL-021 |
| The vulnerability block and its refusals, an image's block at the pull (stage 3's order and shapes) | [vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-015, R-VULN-016, R-VULN-037 |
| The repository document, its narrowings and the revocation journal (stage 3's setting) | [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-004, R-REPO-061 |
| The Bastion's feed (stage 3's narrowings) | [bastion.md](bastion.md) R-BAS-015 |
| Audit storage, metrics, alerts | [operations-and-observability.md](operations-and-observability.md) R-OPS-008, R-OPS-016 |
| The security perspective | [security/artifact-signatures.md](../security/artifact-signatures.md) |

Conventions on this page:

- `https://quader.example.com` is the public base URL *(example)*. Repositories of the project `web` *(example)*: `docker-local`,
  `docker-hub` (remote of Docker Hub), `docker-all` (virtual: `docker-local`, then `docker-hub`), `npm-local`, `npm-remote` (upstream
  `https://registry.npmjs.org/`), `npm-all` (virtual: `npm-local`, then `npm-remote`), `nuget-remote` (upstream nuget.org), `debian`
  (remote of `https://deb.debian.org/debian/`), `ansible-local`, `galaxy` (remote), `files-internal` (generic, local).
- The *company CA*, the *company's Sigstore* and the *release key* are the three ways of the owner's CI to a signature
  ([Q-SIG-15](#q-sig-15--which-way-to-a-signature-the-ci-has)): a certificate chain of a company CA (Notation, NuGet, cosign with its own
  PKI), a Sigstore installation of the company whose certificate authority issues a certificate per CI job, and an OpenPGP key.
- *The signing identity* (stage 2) is the installation's own key and certificate *(example)*: an ECDSA P-256 key, a leaf with the subject
  `CN=Quader signing, O=Example GmbH, C=DE` and the alternative name `https://quader.example.com`, issued by `Example Code Signing CA`
  under `Example Root CA`, the company CA's chain.

## Model

### R-SIG-001 · The three steps and the words of this page
Priority: **P1** · Covers: quader-only

The owner's reading, confirmed ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r1): three steps, stored at the signed content.

- **Detect:** find a signature at every place its format defines (a referrer, a tag, a field of a document, an entry of a package, a file
  beside a file, an index). Detection runs where the content arrives (a commit, a remote fill, a mirror pass) and never on a download.
- **Read:** take from the signature what it carries: the scheme, the signer (a certificate's subject and alternative names, the claims of
  a CI job a Sigstore certificate holds, an OpenPGP key's fingerprint and user ID, a public key's ID), the issuer and the chain, the
  signer's claimed time and every proof of time, the transparency-log entry, the predicate of an attestation, and the algorithms.
- **Verify:** first, the signature matches the content (the *integrity check*); second, a trust anchor knows the signer (the *trust
  step*). The first is a fact of the bytes and holds everywhere; the second depends on the anchors of a scope and is recomputed when they
  change, without reading an artifact again (R-SIG-012).
- **The original stays.** The signature object as it arrived is kept beside its verdict (R-SIG-003), so that a later anchor, block or
  correction of Quader re-judges it without the upstream.

The words of this page:

| Word | Meaning |
|---|---|
| Signature object | The bytes of one signature as its format carries it: a Sigstore bundle, a Notation envelope, the layer and annotations of an older cosign signature, a CMS `SignedData`, an OpenPGP signature packet, an npm registry signature `{keyid, sig}` |
| Subject | What a signature covers, by its digest: a blob (an OCI manifest, an npm tarball, a NuGet package, an Ansible collection archive, a generic file, a Debian release document), or, for npm's registry signature, the string `<name>@<version>:<integrity>` together with the tarball it names |
| Role | `publisher` (the publisher or its build: a signature that proves the origin of the content), `provenance` (a signed attestation of how the content was built), `registry` (a registry's own signature: npm's registry signature and publish attestation, nuget.org's repository countersignature, a Debian archive's signed index, a Galaxy signing service), `attestation` (any other attached statement: an SBOM, a scan result, an unsigned build attestation) |
| Scheme | `sigstore-bundle`, `cosign-legacy`, `notation-jws`, `notation-cose`, `nuget-cms`, `openpgp`, `npm-registry`, `public-key` (a raw signature with a key and no certificate), or `unknown` |
| Attachment | Where a signature reaches a subject in one repository: the item or the place in a document that carries it (R-SIG-004) |
| Trust anchor | What the installation trusts a signer by: a CA certificate, an OpenPGP key, a public key or a Sigstore identity (R-SIG-015) |
| Scope | The set of anchors that judges a signature: the installation's (shipped, from the Helm values, the administrator's), or a project's (the installation's plus the project's own while the switch allows them, R-SIG-019) |

Acceptance criteria: none of its own; R-SIG-003 to R-SIG-009 test the steps.

### R-SIG-002 · What each format knows as a signature
Priority: **P1** · Covers: quader-only

The owner's reading ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r2): per format what its ecosystem knows as a signature;
Quader invents no scheme for a foreign client; a format without a scheme says so; where only the index is signed, the screen says exactly
that. The formats declare (R-SIG-022; sources read 2026-11-21, marked where a fact rests on a secondary source):

| Format | Signatures | Subject | Role | Where the client gets them | Requirement |
|---|---|---|---|---|---|
| OCI | Notation (`notary.x509`, JWS or COSE); a Sigstore bundle (cosign 3, keyless or with a key or its own PKI); the older cosign layout (tag `sha256-<hex>.sig`, layers `application/vnd.dev.cosign.simplesigning.v1+json`); attestations (`.att`, DSSE in-toto, Sigstore bundles of attestations); SBOMs (`.sbom`, referrers of an SBOM type); BuildKit's attestation entries of an index | the manifest or index digest | `publisher`, `provenance`, `attestation` | the referrers API; the referrers tag `sha256-<hex>`; the tags `sha256-<hex>.sig`, `.att`, `.sbom` | R-SIG-023 to R-SIG-027 |
| npm | the registry signature over `<name>@<version>:<integrity>` (ECDSA P-256, `dist.signatures`); the provenance attestation (Sigstore, SLSA provenance); the registry's publish attestation | the version and its tarball | `registry`, `provenance` | `dist.signatures` of the document; `-/npm/v1/keys`; `dist.attestations` and `-/npm/v1/attestations/<name>@<version>` | R-SIG-028 |
| NuGet | the author signature or the repository signature as the primary signature, a repository countersignature; CMS `SignedData` with an RFC 3161 timestamp | the package (a hash of its unsigned bytes) | `publisher` (author), `registry` (repository) | `.signature.p7s` inside the `.nupkg` | R-SIG-029 |
| Debian | OpenPGP signatures of the release document (`InRelease`, `Release.gpg`); apt checks no signature of a package | the release document, which names the index files, which name each package's SHA-256 | `registry` | `dists/<suite>/InRelease`, `Release`, `Release.gpg` | R-SIG-030 |
| Ansible Galaxy | detached OpenPGP signatures of `MANIFEST.json`, which names `FILES.json`, which names every file | the collection archive | `registry` (a signing service of the server), `publisher` (an uploaded signature) | `signatures[]` of the version document | R-SIG-031 |
| generic | a file beside the file: `<file>.asc` and `<file>.sig` (OpenPGP, or a raw signature of `cosign sign-blob`), `<file>.sigstore.json` (Sigstore bundle), `<file>.jws.sig` and `<file>.cose.sig` (Notation blob signatures) | the file of the same directory | `publisher` | the sibling file, downloaded as a file | R-SIG-032 |
| ClamAV definitions | none in the store: a container's RSA signature in its header and its detached `<db>-<version>.cvd.sign` are ClamAV's file format; the import checks the header's signature before it stores a container, and the version shows it as its verification ([formats/clamav.md](formats/clamav.md) R-CLAMAV-003, [malware-scanning.md](malware-scanning.md) R-MAL-033) | — | none | the container and its `.cvd.sign` through the definitions' endpoint; `freshclam` and `clamd` check them | [Q-SIG-96](#q-sig-96--the-clamav-definitions-declare-no-signature-their-containers-signature-is-the-imports) |

- A scheme Quader does not read (Docker Content Trust, whose signatures live on a separate server; Red Hat's simple signing on a lookaside
  server; minisign; an `.sbom` of a format Quader does not parse) is detected where it lives in Quader and shown as **Scheme not
  supported**, with the scheme's name where Quader can tell it.
- A future format declares its signatures or none in the same table (R-SIG-022): "This format has no signatures" stands in its details.

Acceptance criteria:

- Unit (the format contract's kit, `internal/formats/formattest`, milliseconds per format): the declaration of each registered format
  equals the table's row (check 18, R-SIG-022).

### R-SIG-003 · The store: signature objects and signatures, once per content
Priority: **P1** · Covers: quader-only

The unit of the integrity check is the pair of a signature object and its subject, for the whole installation
([Q-SIG-21](#q-sig-21--a-signature-is-stored-once-per-object-and-subject-reached-per-repository-and-trusted-per-scope)): a signature
pushed to thirty repositories is read and checked once. Two tables, without a foreign key to `blobs` (a foreign key there would hold a
blob against the collection, [storage.md](storage.md) R-STOR-024):

```sql
CREATE TABLE signature_objects (                     -- the original of a signature, once per its own SHA-256
  sha256        bytea  PRIMARY KEY CHECK (octet_length(sha256) = 32),  -- an OCI signature's: the manifest that carries it (Q-SIG-48)
  scheme        text   NOT NULL CHECK (scheme IN ('sigstore-bundle', 'cosign-legacy', 'notation-jws', 'notation-cose', 'nuget-cms',
                                                    'openpgp', 'npm-registry', 'public-key', 'unknown')),
  media_type    text   NULL CHECK (length(media_type) <= 255),
  raw           bytea  NULL CHECK (octet_length(raw) <= 262144),  -- the bytes, when no item of their own keeps them (Q-SIG-22)
  item_blob     bytea  NULL CHECK (octet_length(item_blob) = 32), -- the blob of the item that is the original, when one is
  locator       text   NULL CHECK (length(locator) <= 1024),      -- inside that blob: a zip entry, a layer index, a JSON pointer
  size          bigint NOT NULL CHECK (size >= 1),                  -- a stored file above max_object_size is recorded, born malformed (Q-SIG-49)
  first_seen_at timestamptz NOT NULL DEFAULT now(),               -- Quader's receipt of the object (R-SIG-007)
  CHECK ((raw IS NULL) <> (item_blob IS NULL))
);

CREATE TABLE signatures (                            -- one per (object, subject): the read and the integrity check
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  object_sha256   bytea  NOT NULL REFERENCES signature_objects (sha256) ON DELETE CASCADE,
  part            smallint NOT NULL DEFAULT 0,       -- the signature within the object: an OpenPGP packet, a countersignature, a layer
  subject_sha256  bytea  NOT NULL CHECK (octet_length(subject_sha256) = 32),
  subject_name    text   NULL CHECK (length(subject_name) <= 512),  -- npm: <name>@<version>, part of the signed message
  role            text   NOT NULL CHECK (role IN ('publisher', 'provenance', 'registry', 'attestation')),
  predicate_type  text   NULL CHECK (length(predicate_type) <= 512),
  content_sha256  bytea  NULL,                       -- what a keyless scheme or NuGet hashes when it is not the subject's bytes (Q-SIG-48)
  content_locator text   NULL,                       -- inside it: "unsigned", "cleartext", "entry:<name>"
  state           text   NOT NULL DEFAULT 'pending'
                  CHECK (state IN ('pending', 'valid', 'invalid', 'no_key', 'unsupported', 'malformed', 'failed')),
  reason          text   NULL,                       -- a closed code (R-SIG-006)
  signer          jsonb  NULL CHECK (octet_length(signer::text) <= 65536),   -- what the read took (R-SIG-005)
  key_ids         text[] NOT NULL DEFAULT '{}',      -- every identifier an anchor or a block can match: fingerprints, key IDs, serials
  signed_digest   bytea  NULL CHECK (octet_length(signed_digest) <= 64),  -- keyless schemes: the digest the signature commits to (Q-SIG-23)
  digest_alg      text   NULL,
  claimed_at      timestamptz NULL,                  -- the signer's own claim of time
  proven_at       timestamptz NULL,                  -- a verified timestamp or log time (R-SIG-007)
  time_proof      text   NULL CHECK (time_proof IN ('timestamp', 'log')),
  received_at     timestamptz NOT NULL DEFAULT now(),-- the earlier of the object's receipt and the subject's
  checked_at      timestamptz NULL,
  due             boolean NOT NULL DEFAULT true,
  priority        smallint NOT NULL DEFAULT 1 CHECK (priority BETWEEN 0 AND 3),
  attempts        integer NOT NULL DEFAULT 0,
  not_before      timestamptz NOT NULL DEFAULT now(),
  UNIQUE NULLS NOT DISTINCT (object_sha256, part, subject_sha256, subject_name)  -- a signature without a subject name is one row too
);
CREATE INDEX signatures_subject ON signatures (subject_sha256);
CREATE INDEX signatures_due     ON signatures (priority, not_before) WHERE due;
CREATE INDEX signatures_keys    ON signatures USING gin (key_ids);
CREATE INDEX signatures_invalid ON signatures (checked_at) WHERE state = 'invalid';
```

- The store `internal/signatures/store` is the one door to the tables; the commit hook's insert, the job's write and the trust step go
  through it, with the events of R-SIG-039. Built: the commit hook's and the fill hook's record, and the reads and writes the job and the
  API will use ([developer/signatures.md](../developer/signatures.md)).
- **States of the integrity check:**

  | State | Meaning | Shown as (R-SIG-009) |
  |---|---|---|
  | `pending` | detected, not checked yet, or its subject is not stored yet (`reason` `subject_missing`) | no icon; "Not checked yet" |
  | `valid` | the signature matches its subject; the trust step decides the rest | by the trust step |
  | `no_key` | the signature carries no key (OpenPGP, `npm-registry`, `public-key`, and a Sigstore bundle or an older cosign signature made with a key and no certificate) and no anchor or announced key with the signer's ID is known; the signature cannot be matched with the content yet | "Signed · signer not trusted" |
  | `invalid` | the signature does not match its subject, or a part the scheme requires is broken (a digest mismatch, a payload that names another digest, a certificate chain that does not sign the signature, a log entry that does not verify) | "Invalid signature" |
  | `unsupported` | a scheme, an algorithm or a version Quader does not read, or a weak algorithm (`weak_algorithm`: SHA-1, RSA below 2048 bits) | no icon; "Scheme not supported" |
  | `malformed` | the object does not parse within the bounds of R-SIG-013 | no icon; "Signature could not be read" |
  | `failed` | the check could not run `max_attempts` times (the object store, a subject that went) | no icon; "Not checked yet" |

- **The verdict follows the bytes.** Every repository that holds the subject and an attachment of the signature (R-SIG-004) shows the same
  integrity verdict; a re-check changes it for all of them at once.
- **Orphans.** The job removes a signature whose last attachment went, and a signature object whose last signature went (R-SIG-011).
- **The evidence** of a check (`signatures.evidence`, at most 512 KiB) holds what the trust step judges the signature by without reading
  content ([Q-SIG-109](#q-sig-109--the-trust-step-judges-the-evidence-the-check-stored)); mixed integrity verdicts of anchor keys, the
  epoch and narrowing writes follow [Q-SIG-110](#q-sig-110--what-the-store-decides-of-mixed-verdicts-epochs-and-narrowing-writes).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_003_…`, about 2 s, bytes): one Notation signature pushed with the same `subject` to
  `docker-local` and to a second repository makes one row of `signature_objects`, one row of `signatures`, two attachments and one
  check; deleting the image in both removes the three.

### R-SIG-004 · Attachments: where a signature reaches a subject in a repository
Priority: **P1** · Covers: quader-only

A signature is shown and served only where it is attached
([Q-SIG-21](#q-sig-21--a-signature-is-stored-once-per-object-and-subject-reached-per-repository-and-trusted-per-scope)): a signature
pushed to repository A says nothing about the same bytes in repository B, whose readers may not read A and whose clients cannot fetch
it there.

```sql
CREATE TABLE signature_attachments (
  repo_id       bigint NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
  signature_id  bigint NOT NULL REFERENCES signatures (id) ON DELETE CASCADE,
  location      text   NOT NULL CHECK (length(location) BETWEEN 1 AND 2048),  -- the item path of the original, or path#pointer in a document
  item_id       bigint NULL REFERENCES items (id) ON DELETE CASCADE,         -- the item that carries the original, when one does
  source        text   NOT NULL CHECK (source IN ('referrer', 'referrers_tag', 'cosign_tag', 'index_entry', 'document', 'embedded',
                                                    'sibling', 'release', 'upload')),
  name          text   NULL CHECK (length(name) <= 255),                     -- the package or image name the attachment is under
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (repo_id, signature_id, location)
);
CREATE INDEX signature_attachments_item ON signature_attachments (item_id) WHERE item_id IS NOT NULL;
CREATE INDEX signature_attachments_repo_name ON signature_attachments (repo_id, name);
```

- **An attachment follows its carrier.** When the item that carries the original goes (a delete, the trash, an eviction of a remote's
  cache), its attachment goes with it (`ON DELETE CASCADE` of `item_id`; the trash keeps the item row and so the attachment, as R-OCI-018
  keeps the format's rows); a format's cascade removes the carriers of a subject it removes (R-SIG-025).
- **Embedded signatures** (NuGet's `.signature.p7s`) are part of the subject's bytes: every repository that commits the blob gets an
  attachment with `source` `embedded` at the commit, without a new read when the signature row exists.
- **A document's signatures** (npm's `dist.signatures`, Ansible's `signatures[]` of a remote) are attached to the repository whose stored
  document carries them; a fill that replaces the document replaces its attachments in the fill's transaction.
- What a reader sees of a subject is the attachments of the repositories it may read: the addressed repository and, for a virtual
  repository, the members that serve the subject to the caller (R-SIG-027 and the format sections).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_004_…`, about 2 s, bytes): the same image digest in `docker-local` and in a second local
  repository, signed only in `docker-local`: the version of the second repository shows no signature and its referrers answer is empty;
  the signed one shows it. The same `.nupkg` bytes pushed to two NuGet repositories show the embedded signature in both after one check.

### R-SIG-005 · The read: what is taken from a signature
Priority: **P1** · Covers: quader-only

The read fills `signer`, `key_ids`, the times and, for a keyless scheme, `signed_digest`. Every value is the signature's claim until the
integrity check and the trust step confirm it; the screens say which (R-SIG-035).

| Scheme | Read |
|---|---|
| `sigstore-bundle` | the bundle's media type and version; the leaf certificate's subject alternative names (the identity: a URI or an e-mail), the OIDC issuer and every Fulcio extension it carries (for a CI job: the build signer URI, the source repository and its digest and ref, the build trigger, the run invocation URI; [Fulcio's OID registry](https://github.com/sigstore/fulcio/blob/main/docs/oid-info.md)); the chain to its root; the transparency-log entries (log ID, index, integrated time, the inclusion promise or proof); the RFC 3161 timestamps; for a DSSE envelope the payload type, the statement's predicate type and its subjects; a bundle signed with a public key and no certificate: the key's hint |
| `cosign-legacy` | per layer of a `sha256-<hex>.sig` manifest: the simple-signing payload (`critical.image.docker-manifest-digest`, `critical.type`, `optional`), the annotations `dev.cosignproject.cosign/signature` (base64), `dev.sigstore.cosign/certificate`, `dev.sigstore.cosign/chain` (PEM), `dev.sigstore.cosign/bundle` (the log entry with its signed entry timestamp), `dev.sigstore.cosign/rfc3161timestamp`; for an `.att` layer the DSSE envelope as for a bundle (names verified 2026-11-21 in cosign's `pkg/oci/static/signature.go`, `pkg/types/media.go` and `specs/SIGNATURE_SPEC.md`) |
| `notation-jws`, `notation-cose` | the signing scheme (`notary.x509`, `notary.x509.signingAuthority`), the signed attributes (signing time, expiry, the signing agent), the chain, the timestamp countersignature, the payload's target descriptor |
| `nuget-cms` | the commitment type (author: proof of origin; repository: proof of receipt), the signer's chain, the signing time, the hash algorithm of the signature content, for a repository signature its service index URL and package owners, the RFC 3161 timestamp token, each countersignature as a signature of its own (`part` 1, 2, …) |
| `openpgp` | per signature packet: the version, the issuer fingerprint or key ID, the creation time, the hash and public-key algorithms, the hashed subpackets; the user ID and the key's validity once a key is known |
| `npm-registry` | the `keyid` and the signature; the message `<name>@<version>:<integrity>` the version names |
| `public-key` | the signature and the algorithm; the key's hint where the format gives one |

- `key_ids` holds every identifier a trust anchor or a block can match: the SHA-256 fingerprint of each certificate of the chain, the issuer
  and serial of the leaf (`<issuer DN>|<serial>`), an OpenPGP fingerprint and key ID, an npm `keyid`, the SHA-256 of a raw public key.
- The read keeps at most the bounds of R-SIG-013; what lies beyond them is cut and the cut is recorded in `signer.cut`.
- cosign's older layout is read by converting each layer and its annotations into a Sigstore bundle of version 0.1, which allows an
  inclusion promise without a proof, so that one verifier checks both layouts and an identity anchor judges both
  ([Q-SIG-113](#q-sig-113--cosigns-older-layout-is-converted-to-a-bundle-of-version-01)); a log entry of a kind sigstore-go does not parse
  inside a cosign annotation (the `intoto` 0.0.1 entries of old attestations) is dropped with a note and proves no time.
- `signer` is the JSON of `signatures.Signer` ([signer.go](../../internal/signatures/signer.go)); its CI member holds every Fulcio extension
  of a CI job (1.1 to 1.22; the deprecated 1.2, 1.3, 1.5 and 1.6 fill their successors' members when those are absent).

Acceptance criteria:

- Unit (`internal/signatures/scheme/…`, under a second each, signatures made by the test, R-SIG-042): for each scheme the fields of the
  table are read from a signature of each of the three ways of the owner's CI that the scheme supports, and a certificate's Fulcio
  extensions of a GitLab job (issuer, project path, ref, pipeline) are read into `signer`.

### R-SIG-006 · The integrity check of each scheme
Priority: **P1** · Covers: quader-only

The integrity check runs in the worker with libraries in process (R-SIG-014), once per signature row, and writes `state` and `reason`:

| Scheme | The signature matches its subject when | Content read |
|---|---|---|
| `sigstore-bundle` | sigstore-go verifies the bundle's signature with its leaf certificate (or its public key) over the subject's digest or the DSSE payload, the payload's subject digest equals the subject, and every log entry and timestamp the bundle carries verifies against the trust root of an anchor's scope that knows its log or authority (a bundle whose log or authority no trust root knows is `valid` with `time_proof` unset) | none: the digest is the blob's SHA-256 or SHA-512 ([storage.md](storage.md) R-STOR-002) |
| `cosign-legacy` | the payload names the subject's digest, the signature verifies over the payload with the annotated certificate (or the anchor's public key), the annotated chain signs the certificate, and the annotated log entry's signed timestamp verifies against a trust root of the scope | the payload layer (kilobytes) |
| `notation-jws`, `notation-cose` | notation-core-go verifies the envelope with its leaf certificate, the chain signs the leaf, the payload's descriptor names the subject's digest and size | none |
| `nuget-cms` | the CMS signature verifies with its signer's certificate over the signature content, the signed attributes the commitment type requires are there ([NuGet's technical details](https://github.com/NuGet/Home/wiki/Package-Signatures-Technical-Details)), and the content's hash equals the hash of the package's unsigned bytes: the package without the `.signature.p7s` entry's local header, data and central directory record, with the offsets of the later entries and of the central directory as they were before the signature was added; a countersignature verifies over the primary signature's value | the package once, streamed, under the format's size limits |
| `openpgp` | the signature verifies with a known key (R-SIG-008) over the content followed by the packet's trailer | the content once (a Debian release document, an Ansible `MANIFEST.json` with the chain to every file of the archive, a generic file) |
| `npm-registry` | the ECDSA signature verifies with a known key over `<name>@<version>:<integrity>` (`crypto.createVerify('SHA256')` in npm's pacote, read 2026-11-21) | none |
| `public-key` | the signature verifies with a known key over the subject's digest | none |

- **Known keys of a keyless scheme** (`openpgp`, `npm-registry`, `public-key`): the keys of the anchors of any scope, and, for the
  integrity check alone, a key the upstream itself announces (npm's `-/npm/v1/keys` of a remote, a Galaxy signing service's key, Debian's
  `deb.upstreamKeys`, R-SIG-030). A key that only the upstream announces makes the signature `valid` and leaves it untrusted
  ([Q-SIG-24](#q-sig-24--a-signature-without-a-key-or-with-a-blocked-signer-reads-signed--signer-not-trusted)).
- **The committed digest is kept** ([Q-SIG-23](#q-sig-23--a-keyless-scheme-keeps-the-digest-its-signature-commits-to)). For a keyless
  scheme the read computes the digest the signature commits to (for OpenPGP the hash of the content and the packet's trailer, for npm the
  hash of the message) and stores it in `signed_digest`; an anchor added later checks the signature against it without reading the
  content again. A `no_key` signature becomes `valid` or `invalid` by the trust job (R-SIG-012).
- **Closed reasons:** `digest_mismatch`, `subject_mismatch` (a payload, a target descriptor or an attestation's subject names other
  content), `signature_mismatch`, `chain_broken`, `log_entry_invalid`, `timestamp_invalid`, `attribute_missing`, `weak_algorithm`,
  `scheme_unknown`, `version_unsupported`, `limits_exceeded`, `subject_missing`, `content_unreadable`.
- An `invalid` verdict records `signature.invalid` once per signature and counts in `quader_signatures_invalid` (R-SIG-039).
- **The first step that fails decides** ([Q-SIG-112](#q-sig-112--the-first-step-of-the-check-that-fails-decides)): the object is found and
  parsed within the bounds; the read; a weak algorithm; the subject or the digest; the chain; the signature; the log entries and
  timestamps. An OpenPGP signature whose committed digest does not begin with the packet's hash tag is `invalid` (`digest_mismatch`)
  without any key; a `public-key` signature names no key, so a file replaced under it stays `no_key` (no known key verifies it) rather
  than `invalid` ([Q-SIG-115](#q-sig-115--what-a-keyless-check-can-tell-without-a-key)); an Ed25519 raw signature signs the message, not a
  digest, so a `no_key` one is not decided later from the stored digest.
- **Every NuGet part checks the primary signature and the package**
  ([Q-SIG-116](#q-sig-116--every-nuget-part-checks-the-primary-signature-and-the-package)): a countersignature vouches only for a primary
  signature that covers the bytes stored.

Acceptance criteria:

- Unit (`internal/signatures/scheme/…`, milliseconds each, bytes made by the test): each scheme answers `valid` for its signature, and
  `invalid` with the reason of the table for a flipped byte of the content (`public-key`: `no_key`, Q-SIG-115), of the signature and of the
  payload's digest; NuGet's
  unsigned-package hash of a signed package built by the test equals the hash of the package before it was signed, with the signature
  entry first, in the middle and last; an OpenPGP signature checked with a key added later against the stored digest equals the check
  over the content.

### R-SIG-007 · Time: what proves when a signature was made
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-7](#q-sig-7--time-expiry-and-revocation)): a signature is valid if it was valid when it was made; a timestamp
or a log entry proves that time; otherwise the moment Quader received the signature counts; no CRL and no OCSP; a certificate expired
since is information in the details.

- **The time of a signature** is `proven_at` when a proof verified, else `received_at`. `claimed_at` (a signing-time attribute, an
  OpenPGP creation time) is shown and never proves anything.
- **Proofs per scheme:**

  | Scheme | Proof |
  |---|---|
  | `sigstore-bundle`, `cosign-legacy` | a log entry's integrated time with a verified inclusion promise or proof against a log key of the scope's trust root; an RFC 3161 timestamp by an authority of that trust root |
  | `notation-jws`, `notation-cose`, `nuget-cms` | an RFC 3161 timestamp whose authority's chain ends in a CA anchor with the use `timestamping` (R-SIG-015) |
  | `openpgp`, `npm-registry`, `public-key` | none: Quader's receipt |

  A proof that does not verify, or whose authority no anchor of the scope knows, is no proof
  ([Q-SIG-41](#q-sig-41--a-time-proof-counts-only-from-a-log-or-an-authority-the-scope-trusts)): the time falls back to the receipt and
  the details say why ("timestamp by an authority no trust anchor knows").
- **`received_at`** is the earlier of the signature object's first receipt (`signature_objects.first_seen_at`) and the subject's first
  commit in the installation; a remote's signature is received when Quader first fetched it.
- **Valid at its time:** a certificate's validity window holds the time; an OpenPGP key was created before and had not expired at the
  time; an npm key's `expires` lies after it (pacote compares it with the version's publish time; Quader with the time of this
  requirement). A certificate or key that expired since is a fact of the details ("certificate expired on …"), not a verdict.
- **What a trust root knows** ([Q-SIG-114](#q-sig-114--a-trust-root-knows-a-log-by-its-key-id-and-an-authority-by-its-key)): a log by its
  key ID, a timestamp authority by its key (a token whose authority's leaf key, or a certificate its chain issued, verifies it). A token
  that only names an authority proves nothing and invalidates nothing. `proven_at` is the earliest time a root verified; `claimed_at` also
  holds the earliest integrated time or timestamp no root verified.
- **The integrity check judges the proofs against every trust root** (the public one and every Sigstore identity anchor's own) and every
  CA anchor of the use `timestamping` of any scope, and stores the time it found; the trust step decides a scope's verdict at the time a
  proof of that scope verifies ([Q-SIG-118](#q-sig-118--the-time-of-a-signature-in-a-scope)), and the rows keep no time of their own.
- **No revocation lists** ([Q-SIG-7](#q-sig-7--time-expiry-and-revocation)): Quader asks no CRL and no OCSP responder; an administrator
  blocks a signer instead (R-SIG-020, [H-1168](../security/artifact-signatures.md#h-1168--a-revoked-certificate-stays-verified-until-an-administrator-blocks-its-signer)).

Acceptance criteria:

- Unit (`internal/signatures/trust`, milliseconds): a Sigstore bundle whose ten-minute certificate expired long ago is valid at its log
  time; a Notation signature with a timestamp by an authority under a `timestamping` anchor is valid at the stamped time after its
  certificate expired, and the same without the anchor falls back to the receipt and is not valid; an OpenPGP signature received after its
  key expired is not valid.

### R-SIG-008 · The trust step and its scopes
Priority: **P1** · Covers: quader-only

A `valid` signature (and a `no_key` one, which the trust step may make `valid` with an anchor's key) is judged against each scope that
reaches it ([Q-SIG-26](#q-sig-26--the-scope-is-the-project-of-the-addressed-repository)):

```sql
CREATE TABLE signature_trust (
  signature_id  bigint  NOT NULL REFERENCES signatures (id) ON DELETE CASCADE,
  project_id    uuid    NULL REFERENCES project (id) ON DELETE CASCADE,    -- NULL: the installation's scope
  state         text    NOT NULL CHECK (state IN ('public', 'internal', 'untrusted', 'blocked')),
  anchor_ids    bigint[] NOT NULL DEFAULT '{}',   -- every anchor that knows the signer (a gate of stage 3 asks for a set, R-SIG-043)
  block_id      bigint  NULL,
  epoch         bigint  NOT NULL,                 -- the anchor epoch it was computed under (R-SIG-012)
  computed_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE NULLS NOT DISTINCT (signature_id, project_id)
);
```

- **Scopes.** The installation's scope holds the shipped anchors that are switched on, the anchors of the Helm values and the
  administrator's (R-SIG-016). A project's scope holds those and the project's own anchors, while the switch of R-SIG-019 is on. A
  project's row exists only for a project that has anchors of its own and reaches the signature: an attachment in one of its repositories
  or in a repository of `upstream` that one of its virtual repositories lists; every other reader is served the installation's row.
- **Which scope a request uses:** the project of the addressed repository; through a virtual repository its project's scope judges what
  every member serves (Q-SIG-26). A repository of `upstream` addressed directly is judged in the installation's scope.
- **An anchor knows a signer** by its kind (R-SIG-015): a CA anchor when the signature's chain builds to one of its certificates at the
  signature's time with the extended key use `codeSigning` (Go's `crypto/x509` with the anchor's certificates as roots and the stored
  chain as intermediates), and the subject restriction, when set, matches the leaf; an OpenPGP or public-key anchor when its key verifies
  the signature against the stored digest; a Sigstore identity when the bundle verified against the anchor's trust root and the leaf's
  issuer and identity match its issuer and pattern.
- **States:**

  | State | When | Shown as |
  |---|---|---|
  | `public` | a shipped anchor knows the signer (R-SIG-017) | **Verified · public** |
  | `internal` | an anchor of the Helm values, of the administrator or of the project knows it | **Verified · internal** |
  | `untrusted` | no anchor of the scope knows it, or the signature is `no_key` | **Signed · signer not trusted** |
  | `blocked` | a block of R-SIG-020 matches one of its `key_ids` or its identity, whatever anchors know it | **Signed · signer not trusted**, the details say "blocked by an administrator" (Q-SIG-24) |

  When shipped and own anchors both know a signer, `internal` wins: the company's own statement is the more specific one (Q-SIG-25).
- **A CA anchor's leaf carries `codeSigning` explicitly** ([Q-SIG-117](#q-sig-117--a-ca-anchors-leaf-carries-codesigning-explicitly)): Go's
  `crypto/x509` alone would accept a leaf without extended key use. A subject attribute of the restriction matches when it equals any one
  of the leaf's values.
- **A block** matches the key IDs of the signature and the keys of the anchors that verified it; a fingerprint or key ID in hexadecimal
  matches whatever its case, npm's keyid exactly; a `no_key` signature can be blocked too
  ([Q-SIG-119](#q-sig-119--what-a-block-matches)).
- **Measured:** a judgement of a CA anchor with a chain of three certificates takes about 0.19 ms, of an OpenPGP anchor with its
  certificate's parse about 0.10 ms (one run on the development machine).
- **Anchors never repair the bytes.** An `invalid`, `unsupported`, `malformed`, `pending` or `failed` signature has no trust row and is
  shown by its integrity state.

Acceptance criteria:

- Unit (`internal/signatures/trust`, milliseconds, the three ways of R-SIG-042): a Notation signature of the company CA is `internal`
  under a CA anchor of its root and `untrusted` without it; with a subject restriction `O=Example GmbH` a leaf of another organisation of
  the same CA is `untrusted`; a certificate without `codeSigning` under the anchor is `untrusted`; a bundle of the company's Sigstore is
  `internal` under an identity anchor of its issuer and pattern, `untrusted` for another pattern and for the public trust root; an
  OpenPGP signature of the release key is `internal` under an OpenPGP anchor of the key; a bundle of the public Sigstore is `public`
  under the shipped anchor.
- Integration (area `signatures`, `TestR_SIG_008_…`, about 3 s): with the switch on, a project anchor of the Owner of `web` makes an image
  of `docker-local` `internal` for `web` and `untrusted` through a repository of another project that holds the same bytes and
  signature; an image of `upstream/docker-hub` read through `web/docker-all` is judged in `web`'s scope and read directly in the
  installation's.

### R-SIG-009 · The state of an artifact: the badge and the three states without one
Priority: **P1** · Covers: quader-only

The owner's readings and decision ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r3, [Q-SIG-1](#q-sig-1--what-the-icon-stands-for)):
four visible states, and the icon stands for what proves the publisher's origin.

- **What counts for the badge:** the signatures of the roles `publisher` and `provenance` attached where the reader reads the artifact
  (R-SIG-004). A `registry` signature is shown in the details as **Origin confirmed by registry** and never carries the icon; an
  `attestation` is listed in the details and never carries it either.
- **The rule** ([Q-SIG-25](#q-sig-25--an-artifacts-state-is-the-best-of-its-signatures-and-an-invalid-one-alone-shows-invalid)), in this
  order, over the counting signatures in the reader's scope:
  1. one `internal`: **Verified · internal**;
  2. one `public`: **Verified · public**;
  3. one `untrusted` or `blocked`: **Signed · signer not trusted**;
  4. one `invalid`: **Invalid signature**;
  5. otherwise no icon, and the details say which: **Not checked yet** (`pending`, `failed`), **Scheme not supported** (`unsupported`,
     `malformed`), or **No signature** with the schemes the format knows ("Signatures this format knows: Notation, cosign.") or "This
     format has no signatures."
- An invalid signature beside a better one does not change the badge; it is listed in the details with its reason, records its event and
  stands in the security dashboard (R-SIG-038): it alarms and blocks nothing.
- **Debian.** A package carries no badge; its details say "apt checks the signature of this repository's index, not of the package" and
  which release signed the index that names it (R-SIG-030).

Acceptance criteria:

- Unit (`internal/signatures`, milliseconds): the rule as a table of signature sets (empty; one pending; one unsupported; one invalid; one
  invalid and one public; one public and one internal; one registry only; one blocked) to the badge and the details' words.

### R-SIG-010 · The origin of an upload
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-12](#q-sig-12--quader-signs-in-a-publishers-name)): Quader does not sign in a publisher's name; it records who
uploaded, for an upload from CI the verified claims of the pipeline, and shows it as origin.

- **What is kept today, verified on 2026-11-21:** `items.created_by` and `package_versions.created_by` name the principal; a CI token's
  principal is its identity mapping (kind `ci_mapping`, [gitlab-ci-oidc.md](gitlab-ci-oidc.md) "No user subject"), not the job. The job's
  verified claims stay only in `ci_token_issuances`, keyed by the token and deleted `ci.issuances.retention` (one year by default) after
  the token expired, and in the audit events, which carry the token's ID and have their own retention. Nothing at a version names the
  pipeline ([Q-SIG-40](#q-sig-40--the-origin-of-an-upload-is-a-snapshot-at-the-item)).
- **What stage 1 stores:** a row of `artifact_origins` per item a client upload commits (the lead and its companions), written in the
  commit's transaction:

  ```sql
  CREATE TABLE artifact_origins (
    item_id         bigint PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    principal_kind  text   NOT NULL,      -- the actor's kind as the audit records it: user, service_account, ci, anonymous, ...
    principal_name  text   NOT NULL CHECK (length(principal_name) <= 255),
    token_id        uuid   NULL,          -- the credential's ID, never a secret
    ci              jsonb  NULL CHECK (octet_length(ci::text) <= 8192),  -- a CI upload: the issuance's claim snapshot
    uploaded_at     timestamptz NOT NULL DEFAULT now()
  );
  ```

  `ci` holds the issuer's name and the allowlisted claims of the issuance record ([gitlab-ci-oidc.md](gitlab-ci-oidc.md) R-CI-020):
  `project_path`, `project_id`, `namespace_path`, `pipeline_id`, `job_id`, `ref`, `ref_type`, `ref_protected`, `pipeline_source`,
  `environment`, `sha`, `user_login`; never `user_email`. A remote's fill writes no row: its origin is the remote's upstream and the
  fill's time, which the screens read from the item (R-SIG-035).
- Every file a client sent records it; a file Quader derives from the upload (NuGet's generated `.nuspec`) records none
  ([Q-SIG-52](#q-sig-52--the-origin-is-recorded-at-every-file-a-client-sent)).
- It lives as long as the item and travels with backups; it is the input of stage 2's attestation of origin (R-SIG-043).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_010_…`, about 2 s): an npm publish with a CI token of a mapping stores the job's project path,
  pipeline, ref and `ref_protected` at the tarball and its provenance bundle; a publish with a personal access token stores the user and
  no `ci`; the claims are there after the issuance record was removed.

## The work

### R-SIG-011 · The job `signature-verify`, its queue and its order
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-5](#q-sig-5--where-and-when-verification-runs)): a background job of the worker at ingest, with libraries in
process, no tool binary and no new Deployment. The shape is the malware scanning's ([malware-scanning.md](malware-scanning.md) R-MAL-004).

- **At commit.** The item store's commit hook ([storage.md](storage.md) R-STOR-005) asks the format's signature adapter (R-SIG-022) what
  the committed item or document carries or points at, and inserts what it names (`ON CONFLICT DO NOTHING` on the object and the
  signature, an attachment per place), in the commit's transaction, then wakes the job (`signature-verify`, queue `signatures`,
  `coalesce`). An upload, a remote fill, a mirror pass, a background fetch (R-SIG-026) and a link of every format pass through it. A
  commit of a subject whose signature waits with `subject_missing` makes that signature due.
- **Discovery.** The job's first step and the task `signature-verify` (default `*/5 * * * *`) ask the formats for the signatures of items,
  in batches of 10,000 in item order from a cursor (`signature_settings.discovery_after`): what was stored before stage 1, what a restore
  loaded and a row removed since. A pass that reached the last item starts again from the first once a day
  ([Q-SIG-111](#q-sig-111--discovery-walks-the-items-by-a-cursor-and-starts-a-new-pass-once-a-day)). The format host asks each file's format
  as its commit did and records at priority 3; a file Quader derived is not asked.
- **Order.** A row is due while `due` is set; the job takes due rows whose `not_before` has passed by `priority` (0: a client's upload,
  whose publisher looks for the badge; 1: a fill, a pass or a background fetch; 2: a subject that arrived after its signature; 3: discovery
  and the daily retry of `failed`), then `not_before`, with `signatures.concurrency` (default 4) checks in flight.
- **Attempts.** A check that could not run (the object store, a timeout) spends one of `signatures.max_attempts` (default 5) and is due
  again after a doubling back-off from one minute; after the last it is `failed`, and the task's discovery makes it due once a day.
- **Orphans.** The job removes a signature without attachments and an object without signatures.
- **Queue.** `signatures` is a queue of its own (`jobs.workers.signatures`, default 1), so that neither the malware scanning nor the
  advisory sync waits behind a backlog of signatures.

Acceptance criteria:

- Unit (`internal/signatures/job`, under a second): the order by priority and time, the back-off, the attempts.
- Integration (area `signatures`, `TestR_SIG_011_…`, about 3 s, bytes): a Notation signature pushed to `docker-local` is `valid` after the
  job; a row removed is found again by discovery; a signature whose image is pushed after it waits `subject_missing` and is checked when
  the image arrives.

### R-SIG-012 · Trust recomputed on a change of anchors, blocks or the switch
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-5](#q-sig-5--where-and-when-verification-runs)): when anchors change, only the trust step is recomputed; no
artifact is read again.

- **The epoch.** Every write of an anchor, of a block and of the switch raises `signature_settings.anchor_epoch` in its transaction,
  notifies the change area `signature_anchors` and wakes the job `signature-trust` (queue `signatures`, `coalesce`).
- **The run** works the signatures whose trust rows were computed under an older epoch, in batches of 10,000 in ID order, one transaction
  each: it builds the scopes once per run, judges each signature from its stored fields (the chain, `key_ids`, `signed_digest`, the
  times) and writes its rows; a `no_key` signature an added key verifies becomes `valid` or `invalid` here. It reads no blob, no item and
  no upstream.
- **Until a run reaches a row** the screens show the row computed under the older epoch, and an answer of the API says
  `"stale": true` for it ([H-1175](../security/artifact-signatures.md#h-1175--the-shown-trust-lags-a-change-of-anchors-by-the-recomputation)).
- **Cost:** measured once on the development machine, a CA anchor's judgement with a chain of three certificates takes about 0.19 ms, an
  OpenPGP anchor's with the certificate's parse about 0.10 ms; a million signatures take a few minutes of one worker's CPU.
- **What reaches a project** is computed when the trust job runs: a project that starts to reach a signature it did not reach (a new
  attachment in one of its repositories) gets its row at the next change of the epoch or the next check of the signature; until then its
  readers are served the installation's row
  ([H-1202](../security/artifact-signatures.md#h-1202--a-project-that-starts-to-reach-a-signature-is-served-the-installations-trust-until-the-next-recomputation)).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_012_…`, about 3 s): with ten signatures stored, adding a CA anchor makes the company CA's
  signatures `internal` within one run while the object store is stopped (no read happens); removing it makes them `untrusted`; a block
  of the release key's fingerprint makes its signatures `blocked` within one run.

### R-SIG-013 · Bounds on hostile signature containers
Priority: **P1** · Covers: quader-only

A signature object is input of whoever could push or whoever runs an upstream; the worker reads it, a role that holds the installation's
secrets ([Q-SIG-5](#q-sig-5--where-and-when-verification-runs),
[H-1166](../security/artifact-signatures.md#h-1166--the-worker-reads-hostile-signature-containers-in-a-process-that-holds-secrets)). The
bounds sit outside the libraries ([Q-SIG-45](#q-sig-45--the-bounds-and-their-defaults)):

| Bound | Default | What exceeding it means |
|---|---|---|
| `signatures.max_object_size`: one signature object | 256 KiB | not read: `malformed`, `limits_exceeded` |
| `signatures.max_per_subject`: signatures read per subject and repository | 32 | the rest is not read; the details say "32 of 40 signatures read" |
| `signatures.max_chain`: certificates in a chain, and the size of each | 8, 16 KiB | `malformed`, `limits_exceeded` |
| JSON documents (bundles, JWS, payloads, statements) | `untrusted.CheckJSON`: 32 levels, 100,000 values, before any decoder | `malformed` |
| ASN.1 and CBOR (CMS, COSE, timestamps) | the object's size; no nesting deeper than 32 levels, of definite or indefinite length | `malformed` |
| OpenPGP packets in one object, NuGet countersignatures, timestamps per signer | 32, 16, 4 | `malformed`, `limits_exceeded` |
| RSA keys | 2048 to 8192 bits | `unsupported` (`weak_algorithm`) or `malformed` |
| `signatures.check_timeout`: one check | 5 s | `failed` after the attempts |
| `signatures.max_content_size`: content a keyless scheme or NuGet reads | 8 GiB | `unsupported`, `limits_exceeded` |

- The readers are `internal/formats/untrusted`'s where a format reads (R-ARCH-028) and the scheme packages' own bounded parsers; a panic in a
  library is recovered per check and recorded as `malformed`, `content_unreadable`.
- A check writes nothing to disk; the content of a keyless scheme is streamed from the blob store and hashed.

Acceptance criteria:

- Unit (`internal/signatures/scheme/…`, under a second, crafted inputs built by the test): an object of 257 KiB, a chain of nine
  certificates, a JSON bundle 100,000 levels deep, a COSE envelope with a 2 GiB declared length and a CMS with an indefinitely nested
  attribute are refused within the bounds without a panic and with the heap within 64 MiB of its baseline. Built: the refusals; the heap
  bound is not asserted by a test (H-1166).

### R-SIG-014 · Libraries in process, one version each
Priority: **P1** · Covers: quader-only

The verifiers are Go libraries in the worker's binary ([Q-SIG-28](#q-sig-28--the-verifiers-are-four-libraries-in-process)); nothing runs a
tool. Measured on 2026-11-21 by building the binary of this commit (`linux/amd64`, `-trimpath -ldflags "-s -w"`, 71.6 MiB) with a probe
that calls each verifier, outside the repository:

| Scheme | Library (version measured) | Licence | Adds to the binary |
|---|---|---|---|
| Sigstore bundles, the older cosign layout through its `verify.SignedEntity` interface, the trust root and its TUF client | `github.com/sigstore/sigstore-go` v1.4.0 (2026-10-29) | Apache-2.0 | 4.08 MiB |
| Notation (JWS, COSE) | `github.com/notaryproject/notation-core-go` v1.3.0 (2025-04-16) with `github.com/veraison/go-cose` v1.3.0 | Apache-2.0; go-cose MPL-2.0 | 0.61 MiB |
| RFC 3161 timestamps of Notation | `github.com/notaryproject/tspclient-go` v1.0.0 (2024-12-30), a dependency of notation-core-go | Apache-2.0 | in the row above |
| NuGet's CMS `SignedData` and its RFC 3161 timestamp | `github.com/digitorus/pkcs7` (MIT) and `github.com/digitorus/timestamp` (BSD-2-Clause), which sigstore-go already pulls in for its own timestamps (both maintained, untagged: pseudo-versions of 2026-08-21 and 2023-12-17 selected by sigstore-go v1.4.0); the unsigned-package hash, the countersignature and the attribute checks are code of this repository | MIT, BSD-2-Clause | no module more; the NuGet probe added 0.11 MiB less than one on `smallstep/pkcs7` v0.2.3, the other candidate |
| OpenPGP | `github.com/ProtonMail/go-crypto` v1.5.2, already in the build (Debian) | BSD-3-Clause | 0.04 MiB |
| npm's ECDSA and a raw public key | the standard library | BSD-3-Clause | none |

- Together they add **4.76 MiB (6.6 %)** and 52 modules, all under Apache-2.0, MIT, BSD-2-Clause or BSD-3-Clause, and one under MPL-2.0
  (`go-cose`, file-level copyleft: its unmodified source stays available upstream). The repository has no list of third-party licences to
  update. No module of the build was upgraded by them.
- They are pinned once in `go.mod`, as every dependency is; Renovate raises them. Built with exactly these versions; the TUF refresh of
  R-SIG-017 uses `github.com/theupdateframework/go-tuf/v2` v2.4.2, which sigstore-go pulls in.
- **Measured with the code of package 2** (2026-11-21, `linux/amd64`, `-trimpath -ldflags "-s -w"`): the binary grows from 71.90 MiB to
  78.21 MiB, by 6.30 MiB (8.8 %) and 52 modules: the libraries and the verifiers, the trust step, the jobs, the store and the API.
- notation-core-go and tspclient-go had no release for 19 and 23 months on 2026-11-21 (the Notary Project is a project of the CNCF). The
  package that builds R-SIG-023 reads their state again; a known flaw without a release is a finding of the plan's section 8, not a reason
  to write the verifier anew.

Acceptance criteria: the scheme tests of R-SIG-006; and unit (`internal/archtest`, milliseconds): only `internal/signatures/...` imports
these libraries, so no format and no other package reaches them.

## Trust anchors

### R-SIG-015 · The four kinds of anchor and the checks on save
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-2](#q-sig-2--the-kinds-of-trust-anchor)): four kinds under one setting **Trust anchors**; a CA anchor
requires code signing and may restrict the subject ([Q-SIG-30](#q-sig-30--a-ca-anchor-requires-code-signing-and-may-restrict-the-subject)).

```sql
CREATE TABLE signature_anchors (
  id            bigserial PRIMARY KEY,
  scope         text   NOT NULL CHECK (scope IN ('shipped', 'configuration', 'installation', 'project')),
  project_id    uuid   NULL REFERENCES project (id) ON DELETE CASCADE,
  key           text   NOT NULL CHECK (key ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(key) <= 64),  -- unique per scope and project
  title         text   NOT NULL CHECK (length(title) BETWEEN 1 AND 128),   -- the name shown at a signature
  kind          text   NOT NULL CHECK (kind IN ('ca', 'openpgp', 'public-key', 'sigstore')),
  material      jsonb  NOT NULL CHECK (octet_length(material::text) <= 1048576),
  enabled       boolean NOT NULL DEFAULT true,
  note          text   NULL CHECK (length(note) <= 1000),
  version       bigint NOT NULL DEFAULT 1,
  created_at    timestamptz NOT NULL DEFAULT now(),
  created_by    uuid   NULL REFERENCES principal (id) ON DELETE SET NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CHECK ((scope = 'project') = (project_id IS NOT NULL)),
  UNIQUE NULLS NOT DISTINCT (scope, project_id, key)
);
```

| Kind | Material | Checked on save (422 `invalid_value` at the member, with the reason) |
|---|---|---|
| **CA certificate** (`ca`) | 1 to 16 PEM certificates; a detail `use`: `codeSigning` (default) or `timestamping`; an optional subject restriction: attributes of the leaf's subject that must match exactly (`O`, `OU`, `CN`, `C`) and patterns of its alternative names | each parses, is a CA certificate or a pinned end-entity certificate (a certificate that is the signer itself), uses RSA of 2048 bits or more, ECDSA P-256 to P-521 or Ed25519, and is not expired at save; a `timestamping` anchor's leaf or CA allows `timeStamping`; at most 64 KiB |
| **OpenPGP key** (`openpgp`) | 1 to 32 armored public certificates | each parses, holds no secret key material, has a signing key or subkey, uses RSA of 2048 bits or more, ECDSA, EdDSA or X25519/Ed25519, and is neither revoked nor expired at save (the rules of `deb.upstreamKeys`, [formats/debian.md](formats/debian.md) R-DEB-020); at most 256 KiB |
| **Public key** (`public-key`) | 1 to 16 PEM public keys, each with an optional `keyid` (npm's `SHA256:<base64>`) and `expires` | each parses as ECDSA P-256 to P-521, RSA of 2048 bits or more, or Ed25519 |
| **Sigstore identity** (`sigstore`) | the OIDC issuer (exact, `https://…`), the identity as an exact value or a pattern (RE2, anchored, at most 512 characters), and the trust root: `public` (the shipped public root, R-SIG-017) or a `trusted_root.json` of the company's Sigstore ([Q-SIG-27](#q-sig-27--a-sigstore-identity-carries-its-trust-root)) | the issuer is an HTTPS URL; the pattern compiles; an own trust root parses as Sigstore's trusted root (sigstore-go `root.NewTrustedRootFromJSON`), holds at least one certificate authority and one log or timestamp authority, and is at most 1 MiB |

- **What every anchor shows:** its title, its kind, its scope, the fingerprints of its material, the expiry of its certificates and keys,
  whether it is switched on, and the number of signatures it knows in the installation's scope (read from `signature_trust.anchor_ids`).
- An anchor's material is public data: no secret is accepted (a PEM block of a private key, an OpenPGP secret key answer 422
  `secret_material`).
- **What the table leaves open, decided** ([Q-SIG-121](#q-sig-121--the-bounds-and-checks-of-anchor-material-the-table-leaves-open)): a CA
  certificate with key usage set allows `certSign` (a CA) or `digitalSignature` (a pinned signer); a certificate with extended key use names
  the anchor's use (`codeSigning` or `timeStamping`) or any; at most 16 alternative-name patterns of at most 512 characters, wrapped
  `^(?:…)$`; subject values of 1 to 256 characters; a keyid's format only (`SHA256:` and the base64 of 32 bytes); an expired public key is
  kept (npm keeps its expired key to judge old signatures); OpenPGP signing keys RSA, ECDSA or EdDSA, X25519 only on an encryption subkey;
  a Sigstore anchor's issuer is required on save (only the shipped `sigstore-public` matches any issuer, and it is never saved through the
  checks).

Acceptance criteria:

- Unit (`internal/signatures/anchors`, milliseconds): each check of the table, with a valid and an invalid sample per row; a private key
  in any field answers `secret_material`.

### R-SIG-016 · Scopes and their precedence
Priority: **P1** · Covers: quader-only

| Scope | Who writes it | Holds for | Switched off by |
|---|---|---|---|
| `shipped` | the release (R-SIG-017) | every repository | an administrator (`enabled: false`); it stays listed |
| `configuration` | the Helm values, applied by `quader migrate` (R-SIG-018) | every repository | removing it from the values |
| `installation` | an administrator through the UI or the API | every repository | an administrator |
| `project` | the project's Owner while the switch is on (R-SIG-019) | the repositories of its project, and what its virtual repositories serve | the Owner; an administrator removes it; the switch makes all of them stop applying |

- Anchors only add: no scope can lower a judgement another scope made, and a project cannot switch off an anchor of the installation
  ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r4).
- A shipped or a configuration anchor cannot be edited or removed through the API (409 `anchor_managed`); a shipped one can be switched
  off and on.
- **What a removed or switched-off anchor does to stored verdicts:** nothing to the integrity verdicts; the trust job recomputes every row
  the anchor was in (R-SIG-012): such a signature becomes `untrusted`, or stays trusted when another anchor knows it.

Acceptance criteria: those of R-SIG-012 and R-SIG-019; this one adds the unit test of the precedence table (`internal/signatures/trust`,
milliseconds).

### R-SIG-017 · Shipped anchors: what, how they are packaged and how they stay current
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-4](#q-sig-4--what-quader-ships-as-public)): the public Sigstore root and the keys of the well-known upstreams
together with their preset; no general list of public code-signing CAs; every shipped anchor can be switched off.

| Key | Kind | What | Kept current by |
|---|---|---|---|
| `sigstore-public` | `sigstore` (identity pattern `.*` of any issuer, trust root `public`) | the public Sigstore's trusted root: its certificate authorities, transparency logs and timestamp authorities | the task `signature-trust-root` (below), else the release's snapshot |
| `npmjs` | `public-key` | the registry signing keys of `registry.npmjs.org`, with their `keyid` and `expires` | the same task: npm's CLI reads them first from the public Sigstore TUF repository as the target `registry.npmjs.org/keys.json` (verified 2026-11-21 in npm's `lib/utils/verify-signatures.js`), else the release's snapshot |
| `debian-archive`, `ubuntu-archive` | `openpgp` | the archive keyrings Quader embeds today for the well-known Debian and Ubuntu repositories (`internal/formats/deb/archivekeys`, [repositories-and-proxy.md](repositories-and-proxy.md#q-repo-379--the-debian-and-ubuntu-well-known-repositories-carry-their-archive-keys) Q-REPO-379) | the release |
| `nuget-org` | `ca` (pinned certificates) | nuget.org's repository signing certificates as its `RepositorySignatures` resource lists them (three on 2026-11-21, 2018 to 2027) | the release |
| `nuget-org-timestamping` | `ca`, use `timestamping` | the roots of the timestamp authorities whose tokens nuget.org's repository countersignatures carry, so that a countersignature made with a certificate that expired since is judged at its stamped time ([Q-SIG-29](#q-sig-29--nugetorgs-preset-ships-the-timestamping-roots-its-countersignatures-need)): VeriSign Universal Root CA, DigiCert Assured ID Root CA and DigiCert Trusted Root G4, read on 2026-11-21 from twelve packages | the release |

- Each is a shipped anchor of the preset that names it ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-062), present whether
  or not the well-known repository is switched on; `sigstore-public` belongs to no preset.
- **Packaging.** The release embeds each in the binary (`internal/signatures/shipped`): Sigstore's trusted root and the npm keys as the TUF
  targets of the release's build, read by `make shipped-anchors` from the TUF repository through sigstore-go's embedded TUF root, and
  committed; the Debian, Ubuntu and nuget.org material as files.
- **Kept current online.** The task `signature-trust-root` (queue `signatures`, `exclusive`, default `17 4 * * *`) fetches the TUF
  metadata from `signatures.trust_root.url` (default `https://tuf-repo-cdn.sigstore.dev`) through the outbound client and its egress rules,
  verifies the chain from the TUF root sigstore-go embeds with go-tuf's updater (the code sigstore-go's client wraps, driven directly so
  that the reference time is the task's), and stores the targets `trusted_root.json` and `registry.npmjs.org/keys.json` in
  `signature_trust_roots` when the TUF snapshot's version is newer than the stored one and the release's (the snapshot's, not the targets'
  version: npm's keys sit in the delegated role `registry.npmjs.org`, whose change leaves `targets.json` alone,
  [Q-SIG-120](#q-sig-120--the-shipped-anchors-titles-presets-and-the-version-a-refresh-compares)); a stored document raises the anchor
  epoch (R-SIG-012) and records `signature.trust_root.updated`. A failure keeps the previous document and records
  `signature.trust_root.failed` (at most once a day).
- **Without internet** (`signatures.trust_root.refresh: false`): the release's snapshot holds, and it ages with the release
  ([H-1170](../security/artifact-signatures.md#h-1170--without-internet-the-public-sigstore-root-is-the-releases-snapshot)); an operator
  may set `signatures.trust_root.url` to a TUF mirror of the company.
- **Switching off.** An administrator switches a shipped anchor off (`PATCH …/anchors/{id}` with `{"enabled": false}`, R-SIG-034); the
  anchor stays listed, read-only and greyed, and the trust job recomputes.

Acceptance criteria:

- Unit (`internal/signatures/shipped`, milliseconds): every shipped document parses, and the snapshot's TUF targets verify against the
  embedded TUF root. No test depends on the date: the snapshot's age is shown on the admin page and alerted
  (`QuaderSignatureTrustRootStale`, R-SIG-039), and `make shipped-anchors` refreshes it.
- Integration (area `signatures`, `TestR_SIG_017_…`, about 2 s, a TUF repository built by the test in a stub): the task stores a newer
  trusted root and records the event; a stub that serves a target not signed by the chain is refused and the previous root stays;
  switching `sigstore-public` off makes a public bundle `untrusted`.

### R-SIG-018 · Anchors declared in the Helm values
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-3](#q-sig-3--where-anchors-are-maintained)): anchors in the UI and the API, and anchors declared in the Helm
values beside them, read-only; the pattern of the system role bindings ([authorization.md](authorization.md) R-AUTHZ-010, Q-AUTHZ-170).

- The chart renders `signatures.anchors` of its values into the configuration file; each entry is an anchor of R-SIG-015 with its `key`,
  `title`, `kind` and material, the material inline or from a `<member>From.configMapKeyRef` (`certificatesFrom`, `keysFrom`, a key's
  `pemFrom`, `trustedRootFrom`) that the chart mounts read-only into the `quader migrate` Job only (public material, no Secret); the
  configuration file carries the mounted files as `certificates_file`, `keys_file`, `pem_file` and `trusted_root_file`
  ([Q-SIG-122](#q-sig-122--anchors-of-the-configuration-are-a-map-and-their-material-a-mounted-file)). `trustRoot` may be left out: a trusted
  root given means `own`, none `public`:

  ```yaml
  signatures:
    anchors:
      - key: gitlab-ci-signing           # example
        title: GitLab CI signing CA      # example
        kind: ca
        ca:
          certificates: |                # example: the company CA's root
            -----BEGIN CERTIFICATE-----
            …
          use: codeSigning
          subject: { O: "Example GmbH" } # example
      - key: company-sigstore            # example
        title: Company Sigstore
        kind: sigstore
        sigstore:
          issuer: https://gitlab.example.com
          identity: "^https://gitlab.example.com/platform/.+//\\.gitlab-ci\\.yml@refs/heads/main$"
          trustedRootFrom: { configMapKeyRef: { name: company-sigstore-root, key: trusted_root.json } }
  ```

- In the configuration file `signatures.anchors` is a map keyed by the anchor's key (the configuration's lists hold no nested lists,
  R-ARCH-020). `quader migrate` applies them, after the release's shipped anchors (R-SIG-017), in one transaction with the scope
  `configuration`: a declared anchor exists afterwards with its material,
  one no longer declared is removed, both recorded (`signature.anchor.created`, `signature.anchor.removed`, actor `system:configuration`);
  an invalid declaration stops `quader migrate` with the anchor's key and the reason of R-SIG-015.
- The API lists them with `scope: "configuration"`; a write answers 409 `anchor_managed`; the admin page shows them with "From the Helm
  values" and no controls.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_018_…`, about 2 s): a declared CA anchor is applied by the migration, listed read-only and
  refuses `DELETE` with 409; removing it from the configuration removes it at the next migration; an invalid certificate stops the
  migration with the key named.

### R-SIG-019 · The switch for project Owners, and project anchors
Priority: **P1** · Covers: quader-only

The owner's reading ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r4), modelled on the restricted visibility levels
([authorization.md](authorization.md) R-AUTHZ-037): one switch of the installation, off by default.

- **The switch** is `project_anchors` of the one row of `signature_settings` (`project_anchors boolean NOT NULL DEFAULT false`,
  `anchor_epoch`, `version`, `updated_at`, `updated_by`, and the job's own members: `discovery_after` and `discovery_pass_at` of discovery,
  `failed_retried_at` of the daily retry, `trust_root_error`, `trust_root_error_at` and `trust_root_reported_at` of the task
  `signature-trust-root`), read and changed through `GET` and `PATCH /api/v1/signatures/settings`
  (`settings:read`; `security-settings:write` with a recent authentication, R-AUTHZ-037's rule), with `If-Match`. Its label: **Project
  owners may add trust anchors for their project**.
- **On:** the Owner of a project maintains anchors of the scope `project` through `/api/v1/projects/{projectKey}/signatures/anchors`;
  they judge only signatures in the project's scope (R-SIG-008). They only add (R-SIG-016).
- **The administrator** (a holder of `security-settings:write`) lists every project's anchors (`GET /api/v1/signatures/anchors?scope=project`)
  and removes any of them; an auditor lists them.
- **Off again:** the project anchors stay stored and stop applying: the trust job drops the project rows, and the anchors show "Not applied:
  project anchors are switched off" to their Owners and to the administrator. A write of a project anchor while the switch is off answers
  403 `project_anchors_disabled`.
- Switching off and removing a project anchor narrow trust and carry the journal flag (R-SIG-039).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_019_…`, about 3 s): with the switch off the Owner of `web` gets 403 `project_anchors_disabled`;
  an administrator switches it on (an auditor gets 403); the Owner adds an OpenPGP anchor, which makes the release key's signature of
  `files-internal` `internal` in `web` only; the administrator lists and removes it; switched off again, a stored project anchor stays
  listed with `applied: false` and the signature reads `untrusted`.

### R-SIG-020 · Blocking a signer
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-7](#q-sig-7--time-expiry-and-revocation)): the administrator can block a signer (fingerprint, serial,
identity), which acts at once on all its signatures.

- **A block** is a row of `signature_blocks` (`id`, `kind` `fingerprint` (a certificate's or an OpenPGP key's), `serial` (an issuer and a
  serial), `keyid` (npm or a raw key) or `identity` (a Sigstore issuer and an identity pattern), `value`, `reason` of 1 to 1000
  characters, `created_at`, `created_by`), through `POST`, `GET` and `DELETE /api/v1/signatures/blocks` (`security-settings:write` to
  write, `settings:read` to read).
- It raises the anchor epoch; the trust job sets `blocked` on every trust row of a matching signature in every scope, whatever anchors know
  it (R-SIG-012); the badge falls to **Signed · signer not trusted** and the details say "Blocked by an administrator on <date>: <reason>".
- A block holds in every scope: no project anchor lifts it. Lifting a block recomputes.
- Blocked signatures stand in the security dashboard's view (R-SIG-038).

Acceptance criteria: those of R-SIG-012; and integration (area `signatures`, about 1 s): an Owner's block answers 403.

### R-SIG-021 · Permissions
Priority: **P1** · Covers: quader-only

([Q-SIG-39](#q-sig-39--anchors-blocks-and-the-switch-are-security-settings))

| Action | Who |
|---|---|
| Read the signatures and the origin of an artifact | whoever may read the artifact's metadata in the repository: `read` on it, `content:metadata-read` |
| List the anchors of the installation and the shipped ones, the blocks, the switch | `settings:read`: `admin`, `security-admin`, `auditor` |
| Write an anchor of the installation, switch a shipped one, block a signer, change the switch | `security-settings:write`: `admin`, `security-admin`; the switch with a recent authentication |
| List every project's anchors and remove one | read: `settings:read`; remove: `security-settings:write` |
| A project's anchors | read: the project's members (their names and fingerprints are what a member sees at a signature); write: the project's Owner while the switch is on, and a holder of `security-settings:write` |
| The security dashboard's view | `content:metadata-read` (the dashboard's rule, [api-and-ui.md](api-and-ui.md) R-UI-158) |
| Attach a signature to a local Ansible version (R-SIG-031) | `deploy` on the version's path and its ownership, as a publish |

- A token exercises these only within its scope (R-AUTHZ-013); a CI token holds none of the management capabilities (R-CI-013).

Acceptance criteria: integration (area `signatures`, `TestR_SIG_021_…`, about 2 s): the operations of R-SIG-034 for an administrator, a
`security-admin`, an auditor, an Owner of `web` with the switch on and off, a member of `web` and an anonymous caller.

## Formats

### R-SIG-022 · The format contract's declaration and the kit's case
Priority: **P1** · Covers: quader-only

The owner's reading ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r5): "automatically for future registries" is an obligation
in the format contract; store, verification, anchors, icon and API are format-neutral and built once. The contract gains a method that
every format implements ([ADR-228](../decisions.md#adr-228--the-format-contract-declares-every-formats-signatures-and-a-signature-is-never-a-version),
[Q-SIG-43](#q-sig-43--the-contract-gains-a-signature-method-every-format-implements-at-version-8)); the block below is written into
[architecture.md](architecture.md) R-ARCH-008 by the change that adds it to the code:

```go
// Signatures declares where a format's signatures live and finds them in what a commit stores.
// Every format implements it; Base returns nil, and Register refuses nil.
Signatures() SignatureAdapter

type SignatureAdapter interface {
    // Declaration is static: the kinds the format knows, or none with the reason.
    Declaration() SignatureDeclaration
    // Detect names the signatures a committed item or a stored document carries or points at.
    // It reads only the bytes it is handed and runs no SQL; the core reads and checks.
    Detect(ctx context.Context, ev SignatureEvent) ([]SignatureCandidate, error)
}

type SignatureDeclaration struct {
    None   string           // non-empty: the format has no signatures, and why
    Kinds  []SignatureKind
}

type SignatureKind struct {
    Name     string   // "Notation", "Sigstore bundle", "registry signature", …
    Schemes  []string // R-SIG-001's scheme names
    Role     string   // publisher, provenance, registry, attestation
    Location string   // where it lives, in the format's words
    Covers   string   // what it covers
    Client   string   // how a client gets it
}

type SignatureCandidate struct {
    Object        SignatureLocator // the original: an item's blob with a locator, or bytes of at most 256 KiB
    Part          int              // the signature within the object (Q-SIG-47)
    Subject       [32]byte         // the SHA-256 of the covered blob
    SubjectPath   string           // or a stored file of the event's repository whose blob is the subject (Q-SIG-47)
    SubjectName   string           // npm: <name>@<version>
    Role          string
    SchemeHint    string
    Source        string           // referrer, referrers_tag, cosign_tag, index_entry, document, embedded, sibling, release, upload
    Location      string           // the attachment's location (R-SIG-004)
    SignedContent SignatureLocator // what a keyless scheme or NuGet hashes: the subject, an entry of it, its unsigned view
}
```

As built (the full block with `SignatureLocator`, `SignatureEvent` and `SignatureContent` is in [architecture.md](architecture.md) R-ARCH-008):
a candidate names its object, its subject and its signed content by a path of the event's repository, by a blob's digest, or as bytes;
the core resolves a path in the event's repository and nowhere else, and drops a candidate whose path holds nothing, which is named
again from the other side when that path is stored ([Q-SIG-47](#q-sig-47--a-detector-names-paths-the-core-resolves-them-in-the-events-repository)).
`SignatureEvent` is a committed item or a document a remote's fill stored, with its content readable bounded from the spool or the
object store; a detector's error never fails the commit. A format without signatures returns `formats.NoSignatures(reason)`.

- `Register` panics for a format whose `Signatures()` is nil, so a format that does not declare does not start, and the test of
  `internal/formats/all` fails ([Q-SIG-43](#q-sig-43--the-contract-gains-a-signature-method-every-format-implements-at-version-8)).
- `ContractVersion` rises to 8, and every format implements the method in the same change.
- **A format adds no list entry for a signature** ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r6): an item that carries a
  signature, a tag that only names a subject's attachments and a document field never become a package or a version of their own; the
  kit holds it.
- **Check 18 of the kit** (`R-ARCH-010 check 18 signatures`, [checks_signatures.go](../../internal/formats/formattest/checks_signatures.go)):
  the declaration has `None` with a reason, or kinds with every field (`Register`'s check, run as check 1), and its roles equal the
  format's row of R-SIG-002 (`TestR_SIG_002_EveryFormatDeclaresItsRowOfTheSpecification` in `internal/formats/all`, which reads the
  table); every fixture case of `Fixture.Signatures` (one per kind at least: a signed sample the fixture builds, committed through the
  in-memory host, or a remote's stored document) makes `Detect` name the expected candidate with its subject and locator; committing the
  case raises no version row whose lead item carries the signature (a signature embedded in its subject's own file, NuGet's, is carried by
  the version's file); a format with `None` detects nothing over every content case. The check cannot be skipped. The test format's
  defects `SignatureVersion` (a signature becomes a version) and `NoSignatureDeclaration` fail it.

Acceptance criteria:

- Unit (the kit, milliseconds per format): check 18 for every registered format and the test format; the two defects fail it.

### R-SIG-023 · OCI: what is detected and read
Priority: **P1** · Covers: BEYOND-034

- **Referrers** (a manifest with `subject`, R-OCI-016): by its `artifactType`, else its config's media type, and its layers' media types:

  | Recognised by | Scheme, role |
  |---|---|
  | `application/vnd.cncf.notary.signature`, a layer `application/jose+json` or `application/cose` | `notation-jws`, `notation-cose`; `publisher` |
  | a layer or artifact type of a Sigstore bundle (`application/vnd.dev.sigstore.bundle.v0.3+json`, `…+json;version=0.x`) | `sigstore-bundle`; `publisher`, or `provenance` for a DSSE envelope whose predicate is SLSA provenance, else `attestation` |
  | `application/vnd.dev.cosign.artifact.sig.v1+json` with simple-signing layers | `cosign-legacy`; `publisher` |
  | a DSSE envelope `application/vnd.dsse.envelope.v1+json` | `cosign-legacy` attestation; `provenance` or `attestation` by its predicate |
  | an SBOM type (`application/spdx+json`, `text/spdx`, `application/vnd.cyclonedx+json`, `+xml`, `application/vnd.syft+json`) and every other artifact type | an attachment of the role `attestation` without a signature (scheme `unknown`) |

- **The referrers tag** `sha256-<64 hex>` (the OCI distribution specification's fallback, written by Notation 1.x by default): an attachment
  of its digest's subject (`source` `referrers_tag`); the index it holds lists referrers that are recognised as above.
- **The older cosign tags** `sha256-<64 hex>.sig`, `.att`, `.sbom` (the tag form `<algorithm>-<hex>.<suffix>`, verified in cosign's
  `normalize` of `pkg/oci/remote/remote.go`): each layer of a `.sig` manifest is one signature (`cosign-legacy`, `part` the layer's index)
  whose payload must name the tag's digest; an `.att` layer is a DSSE attestation; an `.sbom` is an attachment.
- **BuildKit's attestation entries** of an index (platform `unknown/unknown`, annotation `vnd.docker.reference.type: attestation-manifest`,
  R-OCI-026): attachments of the role `attestation` of the manifest `vnd.docker.reference.digest` names; they are unsigned and never carry
  the icon.
- The subject of a tag-borne attachment is the digest its name carries, whether or not that manifest is stored; the attachment waits
  (`subject_missing`) until it is.
- **The object of an OCI signature is the manifest that carries it**, and its part is the index of the layer that holds the signature
  (cosign's older layout holds one per layer; Notation, a bundle and a DSSE envelope one, in layer 0): a referrers tag's index names a
  referrer by the manifest's digest without reading it, so the referrer and the tag reach the same signature row
  ([Q-SIG-48](#q-sig-48--an-oci-signatures-object-is-its-manifest-and-signed-content-has-a-locator)). The detector reads the committed
  manifest only, never a layer, and names at most 32 candidates per event.
- A signature's identity names an image reference only in the older layout's payload (`critical.identity.docker-reference`), which cosign's
  specification says is ignored; Quader shows it and judges nothing by it.

Acceptance criteria:

- Unit (`internal/formats/oci`, milliseconds, manifests built by the test): `Detect` names a Notation referrer, a cosign 3 bundle referrer,
  an in-toto attestation referrer, an SPDX referrer, a referrers tag, a `.sig` with two layers (two candidates), an `.att` and BuildKit's
  attestation entry with their roles, subjects and sources.

### R-SIG-024 · OCI: attachments in no list and no count, shown at the image
Priority: **P1** · Covers: quader-only

The owner's reading ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r6): on the wire everything stays as clients expect; in the
UI they appear in no tag or version list and in no count, but at the image.

- **On the wire, unchanged:** the referrers API (R-OCI-016), the tags list (R-OCI-015, which lists `_tags/` items and so the attachment tags
  as every tag), the manifests by tag and digest, `HEAD`. A client pushing the referrers tag or a `.sig` tag stores it and pulls it back.
- **Not a version** (built: the commit hook of a local push, [hooks.go](../../internal/formats/oci/hooks.go); a remote's tags write no
  version rows) ([Q-SIG-32](#q-sig-32--an-attachment-tag-is-recognised-by-its-name-and-gets-no-version)): a tag whose name is
  `sha256-<64 hex>` or `sha256-<64 hex>.sig`, `.att` or `.sbom` gets no row of `package_versions` (its item is a `_tags/` item without a
  version, like a referrer's manifest). Today such a tag is a version (R-OCI-016's last rule is superseded). So it is in no place that reads
  versions, each checked on 2026-11-21:

  | Place | Reads | Effect |
  |---|---|---|
  | The package's version list, its count and its newest version ([api-and-ui.md](api-and-ui.md) R-API-029) | `package_versions` | not listed, not counted |
  | The search and its suggestions ([security/search.md](../security/search.md)) | packages and versions | not found as a version |
  | The download count and the last download of a version ([storage.md](storage.md) R-STOR-015) | the version's items | not counted for any version |
  | The image scanning ([vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-033) | every image manifest by its media types | a manifest that is a signature or an attestation by R-SIG-023 is no image and is `not_covered`, as R-VULN-033 says of a signature; an older cosign signature's manifest carries an image configuration, so R-SIG-023's recognition is what tells it apart |
  | Retention by version (R-STOR-039, not built) | versions | counted with the image it attaches to (R-SIG-025) |
  | Download limits ([usage-and-limits.md](usage-and-limits.md) R-LIM-007) | requests by route kind | unchanged: a client that fetches a signature spends its budget as for any request |

- **At the image:** the version page of a tag shows the section **Signatures** with the badge of R-SIG-009 and every attachment of the
  manifest the tag points at (and, for an index, of each platform's manifest under its platform), and a section **Attestations and SBOMs**.
  BuildKit's attestation entries leave the version's list of platforms (today `unknown/unknown` rows) and stand there. Built of this: the
  version's data lists them under `attestations` and no longer under `platforms` ([view.go](../../internal/formats/oci/view.go)); the image
  scanning's selection leaves out a referrer and a manifest that is the object of a signature attached in its repository
  ([Q-SIG-104](#q-sig-104--the-image-scanning-leaves-out-what-is-a-signature-in-its-repository)). The screens are package 4's.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_024_…`, about 3 s, bytes): an image `app:1.0` with a Notation referrer, a referrers tag and a
  `.sig` tag pushed by the test's client: the package lists one version and counts one; `tags/list` answers `1.0`, `sha256-<hex>` and
  `sha256-<hex>.sig`; the version's signatures answer lists three attachments; the image scanning's selection names one manifest; a
  BuildKit index's attestation entry is in the attachments and not in the platforms.

### R-SIG-025 · OCI: an attachment follows its image
Priority: **P1** · Covers: BEYOND-016

The owner's reading ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r6): the signature follows its image on delete, cleanup,
copy and mirror.

- **Delete** (R-OCI-018, built: [hooks_remove.go](../../internal/formats/oci/hooks_remove.go) `attachmentsOf`): removing a manifest removes,
  besides its referrers (as today), the attachment tags of its digest of the same name (`sha256-<hex>`, `.sig`, `.att`, `.sbom`) and the
  manifests those tags point at unless another tag or manifest of the name references them, in the hook's transaction and with the same
  refusals; deleting only a tag of the image leaves the image and its attachments.
- **Trash:** an image that goes to the trash takes its attachment tags with it (the cascade's reason, Q-OCI-24); a restore from the trash
  brings them back. Not built: the trash can does not exist yet (R-STOR-022), and the hook keeps every row and cascades nothing on the
  reason `trash`, as it does for an image's own tags.
- **The sweep** of unreferenced blobs and **untagged manifests:** a referrer stays while its subject stays (unchanged); attachment tags are
  tags and keep their manifests.
- **Eviction of a remote's cache** ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-045): evicting a manifest evicts its
  attachments. Built: the removal of a remote's manifest item takes the items of its stored referrers and of the manifests its attachment
  tag documents hold; the tag documents are cache entries and expire by their period
  ([Q-SIG-101](#q-sig-101--evicting-a-remotes-manifest-takes-the-items-of-its-attachments)).
- **Retention and copy** (R-STOR-039 and R-STOR-019, not built): the unit of an image is the image with its attachments; a retention run
  never removes an attachment of an image it keeps and removes the attachments with the image; a copy or a promotion of an image takes its
  attachments. The page that builds them adds the cases of this rule.
- **Mirror** ([formats/oci.md](formats/oci.md) R-OCI-026, built: [mirror_plan.go](../../internal/formats/oci/mirror_plan.go)): the plan of each
  kept top-level manifest asks for its signatures as the background fetch does (R-SIG-026) and stores what it finds; "Referrers are not
  mirrored" is superseded. An attachment tag of the upstream's list is no version of the plan
  ([Q-SIG-99](#q-sig-99--a-mirror-asks-for-the-signatures-of-every-kept-tag-in-every-pass)).

Acceptance criteria:

- Integration (area `ocilocal`, `TestR_SIG_025_…`, about 4 s): deleting `app@<digest>` removes its referrer, its `.sig` tag and their
  manifests and their attachments; a `.sig` manifest that a second tag also names stays. The trash's case waits for the trash can.
  Unit (`internal/formats/oci`, milliseconds): the mirror plan of a tag names its signature requests; the kit holds that every OCI
  signature case follows its image (`SignatureCase.RemoveSubject`).

### R-SIG-026 · Remote repositories: Quader fetches the signatures itself
Priority: **P1** · Covers: BEYOND-015

The owner's decision ([Q-SIG-6](#q-sig-6--signatures-of-remote-content)): for content that came through a remote, Quader fetches in the
background what the format defines as its signatures, so that the badge is right even if no client ever asked; mirror passes take
signatures along.

- **What is fetched** ([Q-SIG-33](#q-sig-33--what-the-background-fetch-asks-for-when-and-how-much)), per format:

  | Format | When | Requests |
  |---|---|---|
  | OCI | a top-level manifest (one a tag names, or one a client pulled by digest that no stored index lists) was stored by a fill or a pass | `GET referrers/<digest>`; `HEAD` of `sha256-<hex>.sig` and `sha256-<hex>.att`; `HEAD` of `sha256-<hex>` only when the referrers API answered 404; then `GET` of each signature or attestation manifest found (at most `signatures.remote.max_manifests_per_subject`, default 4, signature types first) and of its layers up to `max_object_size`; never an SBOM's layers |
  | npm | a version's tarball was stored and the version names `dist.attestations` | one `GET` of the upstream's attestations for the version; the keys document once per metadata period |
  | NuGet, Debian, Ansible | — | none: the signature comes with what is fetched anyway (the package, the release, the version document) |
  | generic | — | none (the generic format has no remote class) |

- **Refresh:** a top-level manifest's signatures are asked for again when a client's revalidation of its tag finds the tag unchanged and the
  last fetch is older than `signatures.remote.refresh_after` (default 24 h), so a signature added upstream after the image was cached
  arrives within a day of use.
- **Built** ([signatures_fetch.go](../../internal/formats/oci/signatures_fetch.go), [npm](../../internal/formats/npm/handlers_signatures.go),
  [internal/signatures/fetch](../../internal/signatures/fetch/fetch.go)): the format's data port queues the job `signature-fetch` in the
  transaction that stored the manifest or the tarball; the worker runs it with the upstream of the repository for one run
  ([Q-SIG-97](#q-sig-97--the-background-fetch-is-a-job-of-the-worker-with-a-state-row-per-target)).
- **Through the engine** as background requests (`proxy.PriorityBackground`): the token handshake, the circuit, the fetch slots, the egress
  rules apply; the fetched manifests and documents are stored like any (R-OCI-022), and the commit hook detects them (R-SIG-011). It is
  system work: recorded per repository under `system` and charged to no caller's budget and no ceiling
  ([usage-and-limits.md](usage-and-limits.md) R-LIM-001, R-LIM-015); `signatures.remote.requests_per_hour` (default 600 per remote and
  replica) bounds it ([H-1173](../security/artifact-signatures.md#h-1173--the-background-fetch-asks-an-upstream-for-what-no-client-asked)).
- **A client's referrers query** through a remote passes through as today (R-OCI-024); its answer and the manifests the client then fetches
  are stored and detected without a request more.
- **A repository's detail setting** `signatures.fetch` (remote repositories, default `true`) switches the background fetch off for an
  upstream whose limits are tight; it is in the closed section **Signatures** of the repository's settings, never in the creation form.
  The job reads it from the repository document; the member `signatures` of the document accepts it on a remote repository and answers
  `not_applicable` on a local or a virtual one (R-SIG-040, built with package 2,
  [Q-SIG-108](#q-sig-108--the-fetch-switch-waits-for-the-member-of-r-sig-040-and-the-manual-promises-none)).
- **The configuration** bounds what one run fetches: the job reads `signatures.remote.refresh_after` and `.requests_per_hour`, and hands
  `signatures.remote.max_manifests_per_subject` and `signatures.max_object_size` to the format's fetch in the context of the run
  ([ADR-233](../decisions.md#adr-233--the-background-fetch-hands-its-bounds-to-a-formats-fetch-in-the-context-of-a-run)); a mirror's plan
  runs under the defaults. Every request of a run counts in `quader_signatures_remote_requests_total` (R-SIG-039).
- **What it costs against Docker Hub** (read 2026-11-21 on [Docker's page on pulls](https://docs.docker.com/docker-hub/usage/pulls/)):
  "Using GET emulates a real pull and counts towards the limit. Using HEAD won't"; the limits are 100 pulls per six hours per IPv4 address or
  IPv6 /64 without an account and 200 with a personal account. The `HEAD` requests count nothing; a `GET` of a signature manifest counts
  as a pull; whether the referrers request counts is not stated (it is no manifest URL), and whether Docker Hub serves the referrers API
  was not verified (a 404 sends Quader to the tags). So an unsigned image costs no pull more, an image with an older cosign signature one,
  and at most four with the bound. A mirror of Docker Hub without an account should set `signatures.fetch: false` or an account.

Acceptance criteria:

- Integration (area `ociremote`, `TestR_SIG_026_…`, about 9 s with a worker replica, the OCI stub of R-OCI-035 with a referrers API and
  one without): a pull of a signed image through a remote stores the image's referrers and its `.sig` signature without a client asking;
  against the stub without the API the referrers tag is asked by `HEAD` and the `.sig` found; the stub records `HEAD` requests for absent
  tags and no `GET` for them; with `signatures.fetch: false` nothing is asked. The badge's state after the job is package 2's. Unit
  (`internal/formats/oci`, `internal/formats/npm`, `internal/signatures/fetch`, milliseconds): the requests of a fetch, the bound of four
  manifests, the queueing of a fill, an npm version's attestations only when its entry names them, the budget per hour.

### R-SIG-027 · OCI virtual repositories
Priority: **P1** · Covers: BEYOND-015

- The referrers of a virtual repository stay the union of its members' (R-OCI-027, Q-OCI-161); the tags list stays the union and holds
  attachment tags.
- The version page of an image through a virtual repository shows the attachments of the members that serve the image to the caller, in
  the scope of the virtual repository's project (R-SIG-008).
- The background fetch runs at the remote member that stored the manifest.

Acceptance criteria: integration (area `ocivirtual`, about 2 s): through `docker-all`, an image of `docker-hub` whose signature `docker-local`
holds as a referrer of the same digest lists both members' attachments in its signatures answer. Built of this: nothing new on the wire
(the union of R-OCI-027 holds, and a remote member's fill queues its own fetch); the signatures answer is package 2's API, and the case is
written with it.

### R-SIG-028 · npm: registry signatures, the keys endpoint, provenance and the attestations endpoint
Priority: **P1** · Covers: BEYOND-021, PKG-NPM-024

The owner's readings ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r7): nothing Quader does breaks an upstream signature, and
Quader answers the keys and the attestations endpoints. What npm's client does, read on 2026-11-21 in `lib/utils/verify-signatures.js`
of npm and `lib/registry.js` of pacote: it reads a registry's keys first from the public Sigstore TUF repository (target
`<host><path>/keys.json`), else from `-/npm/v1/keys` of the registry URL (404 means no keys); a package from a registry with keys and
without `dist.signatures` is reported missing and the run exits 1; the registry signature is ECDSA over `<_id>:<integrity>`; the
attestations are fetched at the path of `dist.attestations.url` resolved against the registry URL, a keyed publish attestation needs its
key in the keys answer (`EMISSINGSIGNATUREKEY` aborts the run otherwise), and a provenance statement's subject must name the package and
carry the tarball's SHA-512.

- **Detected:** at a remote's fill of a package document, each version's `dist.signatures` entry (`npm-registry`, `registry`, subject the
  version and its tarball by `dist.integrity`, `document` at `<name>#/versions/<v>/dist/signatures/<i>`); the subject row is the SHA-256 of
  the signed message `<name>@<version>:<integrity>` with `subject_name` `<name>@<version>`, which is also the digest the key checks
  ([Q-SIG-50](#q-sig-50--npms-registry-signature-has-its-message-as-its-subject)); a version's attestations
  (`sigstore-bundle`, the provenance as `provenance`, npm's publish attestation as `registry`) when the attestations document is stored at
  `-/npm/v1/attestations/<name>@<version>` ([Q-SIG-51](#q-sig-51--a-remotes-npm-attestations-are-stored-at-the-path-of-their-endpoint)),
  each covering the version's tarball; at a local publish, the provenance bundle `<name>/-/<basename>-<version>.sigstore` (R-NPM-006,
  Q-NPM-58) as `provenance` of the tarball.
- **Local publish** keeps refusing trust evidence from clients: `dist.signatures` and `dist.attestations` of the request are removed
  (R-NPM-007, unchanged), because a client must not set what a registry vouches for.
- **The keys endpoint** `GET <registry URL>-/npm/v1/keys` ([Q-SIG-34](#q-sig-34--the-keys-of-a-local-a-remote-and-a-virtual-npm-repository)),
  a `Metadata` route:
  - a **remote** answers its upstream's keys document, fetched from the upstream's `-/npm/v1/keys` and kept for the metadata period, an
    upstream 404 as `{"keys": []}`;
  - a **local** repository answers `{"keys": []}` in stage 1: Quader signs no npm package before stage 2 (d10, R-SIG-043); with stage 2
    it lists the keys of the signing identity while one is current, and `{"keys": []}` while none is (R-SIG-050);
  - a **virtual** repository answers the union of its members' keys by `keyid`, whatever members the caller may read (public keys, the same
    document for every caller).
- **The attestations endpoint** `GET <registry URL>-/npm/v1/attestations/<name>@<version>` (`Metadata`, decided as a read of the version's
  document; [Q-SIG-35](#q-sig-35--npm-attestations-are-served-below-the-registry-url-and-a-local-bundle-only-when-npm-can-verify-it)):
  - a **remote** answers the upstream's attestations for the version, fetched as a document (on a client's request or by the background
    fetch) and kept;
  - a **local** repository answers `{"attestations": [{"predicateType": <the statement's>, "bundle": <the stored bundle>}]}` for a version
    whose provenance bundle verified against the public Sigstore (`valid` under `sigstore-public`), and 404 otherwise: npm verifies
    provenance only against the public Sigstore, so a bundle of the company's Sigstore is stored, checked and shown by Quader and not offered
    to npm, which would report it invalid;
  - a **virtual** repository answers those of the member whose version entry wins (R-NPM-030).
- **`dist.attestations.url`** of every document Quader serves (remote, virtual, and a local version with a servable bundle) is
  `<serving registry URL>-/npm/v1/attestations/<name>@<version>`, so its path reaches the serving repository (R-NPM-027's plan, now
  specified); `dist.attestations.provenance` passes unchanged, and is set for a local version only when the bundle is served.
  `dist.signatures` passes through byte for byte: the signed string holds no URL.
- **Shown:** the registry signature as **Origin confirmed by registry** with the key's ID and owner (`registry.npmjs.org` under the
  shipped `npmjs` anchor); the provenance as the badge's evidence with its builder, source repository, ref and workflow from the
  certificate.
- **Built** ([handlers_signatures.go](../../internal/formats/npm/handlers_signatures.go)): the two routes (`npm.keys.get`,
  `npm.attestations.get`, `Metadata`, every class) with the answers above; the remote stores both documents through the engine and the
  fill of a package document queues the fetch of the keys once a day; the rewrite of `dist.attestations.url`
  ([rewrite.go](../../internal/formats/npm/remote/rewrite.go)). A local version's document carries no `dist.attestations` in stage 1
  ([Q-SIG-103](#q-sig-103--an-npm-local-document-carries-no-distattestations-in-stage-1)).
- **Through a virtual repository with a remote member, a local package reads as missing a registry signature** to `npm audit signatures`
  until stage 2 signs local packages, and in an installation without a signing identity after it
  ([H-1176](#h-1176--through-a-virtual-npm-repository-npm-audit-signatures-reports-local-packages-as-missing-a-registry-signature)).

Acceptance criteria:

- Unit (`internal/formats/npm`, milliseconds): `Detect` over a stored document with `dist.signatures` and over a publish with a bundle; the
  keys answers of the three classes; the rewritten `dist.attestations.url`.
- Integration (area `npm`, `TestR_SIG_028_…`, about 3 s, the npm stub serving keys and a version's attestations): through `npm-remote` the
  keys endpoint answers the stub's keys and the attestations endpoint the stub's, each stored once for two requests; through `npm-all`
  the keys are the union and a local member adds none. Not built: a stub that signs its documents and a local bundle's case under the
  public root, which need the trust step of package 2.
- End to end (the existing set `e2e (npm)`, one case, about 5 s, bytes): `npm audit signatures` passes for a package of the npm test upstream
  installed through `npm-remote`, with the test upstream's key served at `-/npm/v1/keys`. Not built: the npm test upstream signs no
  document yet; the case is added with it.

### R-SIG-029 · NuGet: the signature inside the package
Priority: **P1** · Covers: quader-only

- **Detected** at the commit of every `.nupkg` (a push, a fill): a `.signature.p7s` entry is the primary signature (`nuget-cms`, the author's
  `publisher`, a repository's `registry`) and each repository countersignature a `registry` signature of its own (`part` 1, …), attached
  `embedded` to every repository that holds the blob (R-SIG-004). The package stays byte for byte as received (R-NUGET-005).
- **Read and checked** by R-SIG-005 and R-SIG-006: the unsigned-package hash reads the package once.
- **Trust:** nuget.org's countersignature under the shipped `nuget-org` anchor at the time its timestamp proves under
  `nuget-org-timestamping` (R-SIG-017); an author's signature under a public code-signing CA is **Signed · signer not trusted** until an
  administrator adds the CA (d4); one of the company CA under its anchor.
- **No `RepositorySignatures` resource** (verified: the service index lists none, `TestR_SIG_029_…` of `internal/formats/nuget`;
  [Q-SIG-36](#q-sig-36--nuget-lists-no-repository-signatures-resource)): the resource announces the
  certificates a source signs with itself ([NuGet's API reference](https://learn.microsoft.com/en-us/nuget/api/repository-signatures-resource));
  Quader signs no NuGet package (d10), and a pass-through of nuget.org's resource would bind packages of a virtual repository to one member's
  certificates. A client checks author and nuget.org signatures through its own trust and `trustedSigners`, as against Quader today.
- **Shown:** the author's signature as the badge's evidence; nuget.org's countersignature as **Origin confirmed by registry**.

Acceptance criteria:

- Unit (`internal/signatures/scheme/nugetcms`, milliseconds, packages signed by the test with digitorus/pkcs7 and a test timestamp authority):
  an author signature with a timestamp, a repository countersignature, a package whose bytes changed after signing (`digest_mismatch`), a
  countersignature over another signature value (`signature_mismatch`).
- Integration (area `signatures`, `TestR_SIG_029_…`, about 2 s): a package signed by the company CA pushed to a local NuGet repository reads
  `internal` under the company CA's anchor and its bytes are served unchanged.

### R-SIG-030 · Debian: the index is signed, the package is not
Priority: **P1** · Covers: quader-only

- **Detected** when a remote stores a release document: each OpenPGP signature of `InRelease` and of `Release.gpg` (`openpgp`, `registry`,
  `source` `release`, subject the release document) ([formats/debian.md](formats/debian.md) R-DEB-019).
- **Checked** with the keys of anchors and, for the integrity check alone, with `deb.upstreamKeys` (R-SIG-006). `deb.upstreamKeys` stays what
  it is, the remote's own condition for storing a release (R-DEB-020), and is no anchor
  ([Q-SIG-31](#q-sig-31--debians-upstream-keys-stay-the-remotes-condition-and-are-no-anchor)): a writer of the repository's settings does not
  decide what reads as trusted elsewhere.
- **Coverage:** a package is covered by a release when the release names an index file that names the package's SHA-256; the format answers
  it through its data port (`Covering`, the rows of R-DEB-023). Built: the optional `formats.SignatureCoverage` of the format answers it for
  a remote from the parsed rows by the file's path and SHA-256 ([covering.go](../../internal/formats/deb/covering.go),
  [Q-SIG-105](#q-sig-105--debians-coverage-is-an-optional-interface-of-the-format-over-the-remotes-parsed-rows)). The details of a `.deb` say "apt checks the signature of the index, not of the
  package", and list each covering release with its suite, its `Date`, its signer and its trust (Debian's archive keys under the shipped
  anchors read **Verified · public**). No badge (R-SIG-009).
- **`verified_by`** of `deb_remote_releases` (today in no answer) is the release's signer in the signatures answer (R-SIG-034).
- **Local and virtual repositories** sign their indexes with their own keys (R-DEB-016, R-DEB-025): the details of a package of such a
  repository say "Index signed by this repository's key <fingerprint>"; Quader checks no signature it made itself
  ([Q-SIG-31](#q-sig-31--debians-upstream-keys-stay-the-remotes-condition-and-are-no-anchor)).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_030_…`, about 3 s, the Debian stub of R-DEB-035 signing with key A): with A as an OpenPGP anchor a
  `.deb` fetched through `debian` lists its covering release as `internal`; without the anchor and with A in `deb.upstreamKeys` as `untrusted`
  and still served.

### R-SIG-031 · Ansible Galaxy: `signatures[]` of a remote and of a local version
Priority: **P1** · Covers: quader-only

- **Detected** at a remote's fill of a version document: each entry of `signatures[]` (`openpgp`, the armored signature as the object,
  subject the collection archive by `artifact.sha256`, `document`); `registry` when the entry names a `signing_service`, else `publisher`
  ([Q-SIG-37](#q-sig-37--ansible-signatures-of-a-local-version-arrive-through-the-management-api)).
- **Checked** when the archive is stored: the signature over `MANIFEST.json`, then `MANIFEST.json`'s SHA-256 of `FILES.json` and
  `FILES.json`'s SHA-256 of every file against the archive's entries, as `ansible-galaxy collection verify` does; a mismatch anywhere is
  `invalid` (`digest_mismatch`). Before the archive is stored the signature waits `subject_missing`.
- **A local version** accepts detached signatures through the management API (R-SIG-034, built:
  [ansible_signatures.go](../../internal/api/v1/ansible_signatures.go), [signatures_store.go](../../internal/formatdata/ansible/signatures_store.go)):
  `POST /api/v1/projects/{projectKey}/repositories/{repoKey}/ansible/collections/{collection}/versions/{version}/signatures` with
  `{"signature": "<armored OpenPGP signature of MANIFEST.json>"}` (`collection` is `<namespace>.<name>`, as the tombstones' operation names
  it), at most 16 per version (409 `signature_limit_reached`), decided as a `deploy` on the collection; it is checked before it is stored as
  far as no key is needed: one signature packet within the bounds, an issuer fingerprint, and the prefix of the hash it signs against the
  version's `MANIFEST.json` (an invalid one answers 422 `signature_invalid`); the check with an anchor's key is the job's, and one whose key no
  anchor has reads `no_key` ([Q-SIG-102](#q-sig-102--a-posted-ansible-signature-is-checked-without-a-key-and-recorded-for-the-job)). It is served
  in `signatures[]` as `{"signature", "pubkey_fingerprint", "signing_service": null}`, a second signature of the same key replaces the first,
  and it is removed with `DELETE …/signatures/{fingerprint}` (`delete`). Each writes an audit event in its transaction:
  `ansible.signature.added` (`name`, `version`, `fingerprint`, `replaced`) and `ansible.signature.removed` (`name`, `version`,
  `fingerprint`), never the signature itself ([Q-SIG-107](#q-sig-107--posting-and-removing-an-ansible-signature-are-audit-events-of-the-format)).
  `ansible-galaxy` has no command to upload a signature; a CI job uses its token on this operation. Q-ANS-14 is superseded in part.
- A virtual version document carries the signatures of the member whose document it serves (R-ANS-013, unchanged).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_031_…`, about 3 s, the Galaxy stub serving a collection signed with key A): through `galaxy` the
  version's signature reads `internal` with A as an anchor after the archive is stored, and `invalid` for an archive whose `FILES.json`
  names another digest (package 2's check).
- Integration (area `ansiblelocal`, `TestR_SIG_031_ASignaturePostedForALocalVersionIsServed`, about 2 s, a release key made by the test): a
  signature posted to `ansible-local` is served in the version document with its fingerprint and recorded with the source `upload`; one of
  other content answers 422; a reader may not post; the removal empties `signatures[]`; the post and the removal each wrote their audit
  event with the fingerprint. Unit (`internal/formats/untrusted`, milliseconds):
  the facts of a real signature read without a key and its hash prefix against other content.

### R-SIG-032 · Generic repositories: a file beside the file
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-8](#q-sig-8--generic-files)): a sibling file is a signature by its name of the file of the same directory.

- **Names:** `<file>.asc`, `<file>.sig`, `<file>.sigstore.json`, `<file>.jws.sig`, `<file>.cose.sig`; the longest suffix wins (`a.jws.sig` is
  Notation's of `a`, not a `.sig` of `a.jws`).
- **The scheme is read from the content** ([Q-SIG-38](#q-sig-38--a-generic-siblings-scheme-is-read-from-its-content)): an armored or binary
  OpenPGP signature packet; a Sigstore bundle; a Notation JWS or COSE envelope; for `.sig` also the base64 raw signature of `cosign sign-blob`
  with a key (`public-key`, checked against the public-key anchors); anything else is **Scheme not supported** with "detected, scheme
  unknown".
- **Either order:** the signature file committed before its file waits `subject_missing`; a file replaced by other bytes makes its signature
  `invalid`, and its new bytes are checked again; a signature file deleted removes its attachment.
- **Shown:** the signed file carries the badge; the signature file stays a downloadable file, listed with "Signature of `<file>`".

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_032_…`, about 3 s, files of a kilobyte): `tool.tar.gz` with `.asc` (release key), `.sigstore.json`
  (company Sigstore) and `.jws.sig` (company CA) beside it shows three signatures; `.sig` holding minisign's text reads "Scheme not supported";
  replacing `tool.tar.gz` makes all three `invalid`.

### R-SIG-033 · The Bastion
Priority: **P1** · Covers: quader-only

- The Bastion serves the signatures a client asks for through the mirrored URL space ([bastion.md](bastion.md)) like any document: the
  referrers answer and the attachment manifests of OCI, npm's keys and attestations, a NuGet package, a Debian release, an Ansible version
  document, a generic sibling file. The new npm routes are `Metadata` routes and the same for every caller, so it caches them as `shared`.
- It verifies nothing and shows nothing: it has no UI. A block or an anchor change does not reach it, because it serves bytes, not verdicts.

Built of this: the two npm routes are `Metadata` routes of every class, so the main installation marks their answers `shared` for a full
reader (R-BAS-013); the kit's check 12 holds that they are the same for every such caller.

Acceptance criteria: integration (area `bastion`, about 2 s): `-/npm/v1/keys` and an attestation document of `npm-remote` are served by the
Bastion and cached once for two callers. Not built: the case needs the npm stub's signature documents in the Bastion's area.

## API and screens

### R-SIG-034 · The API
Priority: **P1** · Covers: quader-only

The operations, in `api/openapi.yaml` under the tag `signatures` ([Q-SIG-44](#q-sig-44--the-api-and-the-screens)):

| Method | Path | What | Permission |
|---|---|---|---|
| GET | `/api/v1/packages/{packageId}/versions/{version}/signatures` (`?repository=`) | Per readable repository holding the version: `state` (the badge), `stale`, and every signature of its files and, for OCI, of its image and platforms: `role`, `scheme`, `state`, `reason`, `trust` (`public`, `internal`, `untrusted`, `blocked`) with the anchors' titles, `signer`, `issuer`, `identity`, `ci` (a CI job's claims from the certificate), `keyIds`, `claimedAt`, `provenAt`, `timeProof`, `receivedAt`, `certificateNotAfter`, `log` (index, integrated time), `predicateType`, `source`, `location`, `original` (a link to download the object); `attestations` (OCI); `covering` (Debian); `origin` (R-SIG-010) | `read` on a repository that holds it, or `content:metadata-read` |
| GET | `/api/v1/projects/{projectKey}/repositories/{repoKey}/signatures?path={path}` | The same for one stored file | as the item (R-STOR-016) |
| GET | `/api/v1/signatures/objects/{sha256}` | The original bytes of a signature object, with its media type | `read` on a repository that holds an attachment of it, or `content:metadata-read`; `404` otherwise |
| GET, PATCH | `/api/v1/signatures/settings` | `{"projectAnchors", "anchorEpoch", "version", "updatedAt", "updatedBy", "trustRoot": {"source", "fetchedAt", "tufVersion", "lastError"}, "backlog": {"pending", "stale"}}`; `PATCH {"projectAnchors": true}` with `If-Match` | read `settings:read`; write `security-settings:write` and a recent authentication |
| GET, POST | `/api/v1/signatures/anchors` (`?scope=shipped\|configuration\|installation\|project`) | List and create anchors of the installation; a create names `key`, `title`, `kind` and the kind's member (R-SIG-015) | read `settings:read`; create `security-settings:write` |
| GET, PATCH, DELETE | `/api/v1/signatures/anchors/{id}` | One anchor; `PATCH` changes `title`, `note`, the material and `enabled` (only `enabled` for a shipped one); `409 anchor_managed` for a shipped or configuration anchor's other members | as above |
| GET, POST | `/api/v1/projects/{projectKey}/signatures/anchors` | A project's anchors, each with `applied`; a create while the switch is off answers `403 project_anchors_disabled` | read: the project's members; write: its Owner, `security-settings:write` |
| PATCH, DELETE | `/api/v1/projects/{projectKey}/signatures/anchors/{id}` | | as above |
| GET, POST | `/api/v1/signatures/blocks` | Blocks of R-SIG-020 | read `settings:read`; write `security-settings:write` |
| DELETE | `/api/v1/signatures/blocks/{id}` | Lifts a block | `security-settings:write` |
| GET | `/api/v1/signatures/findings` | Invalid signatures and the signatures of blocked signers, with the artifacts and repositories that hold them; filters `kind` (`invalid`, `blocked`), `repository`, `q`; cursor paging (R-API-010) | `content:metadata-read`; a caller with `read` on some repositories sees those |
| POST, DELETE | `/api/v1/projects/{projectKey}/repositories/{repoKey}/ansible/collections/{namespace}/{name}/versions/{version}/signatures[/{fingerprint}]` | R-SIG-031 | `deploy` / `delete` on the version |

- **Built** ([http-api.md](../developer/http-api.md)): every operation of the table, the Ansible operation of R-SIG-031 with the formats'
  package. The version list carries the member per row from one statement for the page's files and one count per project of the rows'
  repositories, none per row. `covering` lists the releases Debian's coverage port names (`formats.SignatureCoverage`) with the
  signatures attached to each in the repository, its state read as a publisher's would be. An anchor's and a block's `{id}` in a path is digit text. The settings carry `enabled` (signatures.enabled) besides the table's
  members; a project's list carries the switch (`projectAnchors`) and, under `installation`, the installation's applied anchors without
  their material, because a member holds no `settings:read`
  ([Q-SIG-123](#q-sig-123--the-api-the-project-list-the-virtual-members-and-the-original)). An Owner's removal of a project anchor while the
  switch is off answers `403 project_anchors_disabled`; an administrator removes one whatever the switch says.
- **The member `signatures` of a stored file and of a version** (the items API, a version's files, a version's summary in the package
  views and the rows of the version list): `{"state", "stale"}` with `state` one of `verified_public`, `verified_internal`, `untrusted`, `invalid`, `not_checked`,
  `unsupported`, `none`, absent for a folder and while `signatures.enabled` is false; the version list carries it per version for the badge.
  Stage 3 adds `refused` and `refusedReason` while a gate applies (R-SIG-073).
- Every text a signature carries (a subject, a user ID, an identity, a predicate) is shown as text and never as markup.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_034_…`, about 2 s): the operations of the table answer as stated for the callers of R-SIG-021;
  `GET …/items?path=` of a signed file carries `signatures.state`.

### R-SIG-035 · The badge and the details at a version, a file and an image
Priority: **P1** · Covers: quader-only

The owner's request: signed artifacts carry an icon, and time, issuer and every other piece of metadata are shown. Following "simple by
default, details on demand" ([CLAUDE.md](../../CLAUDE.md)): the plain user sees one badge and one sentence; the details are one click away.

- **The badge** stands beside the version in the version list, in the version page's header and beside a file on its page and in the Files
  tab: a shield mark with the words **Verified · public**, **Verified · internal**, **Signed · signer not trusted** or **Invalid signature**
  ([Q-SIG-44](#q-sig-44--the-api-and-the-screens)). Green is used only for the two verified states (the owner's rule: green only for a
  positive status); "Signed · signer not trusted" is neutral; "Invalid signature" is red. Without a badge nothing is drawn.
- **The details** (a section **Signatures** of the version page, of the file page, and at an OCI image) list each signature: the kind in
  plain words ("Publisher signature · Sigstore, keyless", "Origin confirmed by registry", "Provenance attestation"), the signer, the issuer,
  when it was signed and what proves it ("proven by the transparency log", "proven by a timestamp", "received by Quader"), the trust anchor
  and its scope, the certificate's validity ("valid for 10 minutes at signing; covered by the log entry", "expired on 30 Apr 2027"), for a CI
  job its project, ref and pipeline, and **Download** of the original; a state's sentence ("The signature matches the content. No trust
  anchor knows this signer.", "The signature does not match the content.", "No signature found. Signatures this format knows: Notation,
  cosign."). A block shows its date and reason.
- **Origin:** the file's and version's details carry **Uploaded by** (a user, a service account, or "CI job <project path> · <ref> ·
  pipeline <id>" with `ref_protected`) or **Fetched from** the remote's upstream and time (R-SIG-010).
- **OCI:** the image's section lists signatures and, apart, **Attestations and SBOMs** with their types and sizes (R-SIG-024).
- The screens are rendered and looked at before they land (rule 11 of the plan): light and dark, wide and narrow, a version of each of the
  four states, one with seven signatures and long identities, a Debian package with its covering release, a generic file with three
  siblings, and a version with no signature.

Acceptance criteria:

- Web unit (Vitest, under a second): each state renders its words and its mark; the details render the sentences and the time proof; a
  signer's text is escaped.

### R-SIG-036 · Admin area › Settings › Signatures
Priority: **P1** · Covers: quader-only

- **Where.** `/admin/settings/signatures`, the fourth page of the settings group after **Malware scanning**
  ([api-and-ui.md](api-and-ui.md) R-UI-002); its title **Signatures**; for the holder of `settings:read`, its controls for the holder of
  `security-settings:write`.
- **What it shows**, one column, sections as R-UI-087's:
  1. **Trust anchors**, open: one list of every anchor of the installation with its title, kind, scope ("Shipped with Quader", "From the
     Helm values", "Added by an administrator"), fingerprints, expiry, the signatures it knows, and on/off; **Add trust anchor** opens a form
     whose first field is the kind and whose fields follow the kind (a CA certificate's subject restriction and its use behind **More
     options**); a shipped anchor has only its switch, a Helm one nothing.
  2. **Project trust anchors**, open: the checkbox **Project owners may add trust anchors for their project** (off by default) with the
     sentence "Project anchors apply only to the repositories of their project. A project cannot switch off an anchor of the installation.",
     and below it every project's anchors with their project and **Remove**.
  3. **Blocked signers**, closed: the blocks with their reason; **Block signer** asks for the kind, the value and a reason.
  4. **Public Sigstore root**, closed: where the root comes from (refreshed, or the release's snapshot), when it was fetched, and the last
     failure.
- Under the title stands: "A signature tells who signed an artifact and whether a trust anchor knows the signer. It does not tell that the
  content is safe; the malware scan and the vulnerability scanning check that."
- Stage 2 adds the section **Signing identity** before the four (R-SIG-056).
- The page is rendered and looked at before it lands: both schemes, wide and narrow, with the shipped anchors only, with twelve anchors of
  all kinds and long titles, with the switch off and on, and with a block.

Acceptance criteria:

- Web unit (Vitest, under a second): the form shows the fields of the kind chosen; the switch is a checkbox for the holder of
  `security-settings:write` and text for an auditor; a shipped anchor offers only its switch.

### R-SIG-037 · Trust anchors in a project
Priority: **P1** · Covers: quader-only

- While the switch is on, the project's settings page shows a closed section **Trust anchors** with the project's anchors and, for its
  Owner, **Add trust anchor** (the form of R-SIG-036); members see the list. While it is off the section is not shown, and a project that
  holds anchors shows them to its Owner with "Not applied: project anchors are switched off by an administrator."
- The installation's anchors are listed read-only below the project's ("Also trusted in this project"), so an Owner sees the whole scope.

Acceptance criteria: web unit (Vitest, under a second): the section is hidden with the switch off and no anchors; read-only for a member.

### R-SIG-038 · The security dashboard's view of signatures
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-9](#q-sig-9--display-or-gate)): stage 1 shows and alarms; "Invalid signature" is an entry of the security
dashboard.

- **Where.** A third view **Signatures** (`/security/signatures`) beside **Vulnerabilities** and **Malware**
  ([api-and-ui.md](api-and-ui.md) R-UI-158, [malware-scanning.md](malware-scanning.md) R-MAL-025), for the holder of `content:metadata-read`.
- **The list** (`GET /api/v1/signatures/findings`): one row per signature that is `invalid` or `blocked`, with the artifact (repository,
  path, package and version), the kind, the signer as it claims, the reason, when it was found, and the number of repositories that hold it;
  filters kind and repository; newest first. An empty list says "No invalid signatures."
- No action in stage 1: nothing is released, because nothing is refused; a holder of `security-settings:write` finds **Block signer** on the
  admin page. Stage 3 adds the refused downloads of gated repositories and their releases (R-SIG-070, R-SIG-074).

Acceptance criteria: web unit (Vitest, under a second): the empty list's text; a row of each kind.

## Operations

### R-SIG-039 · Audit events, metrics and alerts
Priority: **P1** · Covers: quader-only

**Audit events** ([operations-and-observability.md](operations-and-observability.md) R-OPS-016; the journal flags are
[Q-SIG-42](#q-sig-42--what-the-journal-carries-of-signatures)'s):

| Event | Mode | Journal | Recorded when | Key fields in `details` |
|---|---|---|---|---|
| `signature.invalid` | T | — | a check made a signature `invalid` | `object`, `subject`, `scheme`, `role`, `reason`, `signer`, `repositories` (up to 100) and their count |
| `signature.anchor.created` | T | — | an anchor of any scope was created | `id`, `scope`, `project`, `kind`, `title`, `fingerprints` |
| `signature.anchor.updated` | T | when it narrows (material removed, switched off) | an anchor changed | `id`, `before`, `after` |
| `signature.anchor.removed` | T | yes | an anchor was removed | `id`, `scope`, `project`, `fingerprints` |
| `signature.settings.changed` | T | when switched off | the switch changed | `before`, `after` |
| `signature.signer.blocked` | T | yes | a block was added | `id`, `kind`, `value`, `reason` |
| `signature.signer.unblocked` | T | — | a block was lifted | `id`, `kind`, `value` |
| `signature.trust_root.updated` | T | — | the task stored a newer public root | `tufVersion`, `targets` |
| `signature.trust_root.failed` | A | — | the task failed, at most once a day | `code`, `detail` |
| `ansible.signature.added` | T | — | a signature was posted for a local Ansible version (R-SIG-031, built) | `name`, `version`, `fingerprint`, `replaced` |
| `ansible.signature.removed` | T | — | the signature of a key was removed from a local Ansible version (R-SIG-031, built) | `name`, `version`, `fingerprint` |

The actor of the job's and the task's events is `system:signatures`; of the Helm anchors `system:configuration`; of the two Ansible events
the caller of the operation. The two Ansible events are the format's (`internal/audit/events_ansible.go`) and built with stage 1's package 3
([Q-SIG-107](#q-sig-107--posting-and-removing-an-ansible-signature-are-audit-events-of-the-format)); the others are package 2's.

**Metrics** ([operations-and-observability.md](operations-and-observability.md) R-OPS-008):

| Metric | Type | Labels | Where |
|---|---|---|---|
| `quader_signatures_checks_total` | counter | `scheme`, `result` (a state of R-SIG-003) | worker |
| `quader_signatures_check_duration_seconds` | histogram | `scheme` | worker |
| `quader_signatures_backlog` | gauge | `priority` | worker |
| `quader_signatures_oldest_pending_seconds` | gauge | — | worker |
| `quader_signatures_invalid` | gauge | — | worker: signatures `invalid` with an attachment |
| `quader_signatures_trust_rows_stale` | gauge | — | worker: trust rows below the anchor epoch |
| `quader_signatures_remote_requests_total` | counter | `format`, `result` | worker |
| `quader_signatures_trust_root_age_seconds` | gauge | — | worker: now minus the public root's fetch or the release's build |

**Alerts** (rendered with the chart's rules, [ha-and-helm.md](ha-and-helm.md) R-HA-045):

| Alert | Expression | For | Severity |
|---|---|---|---|
| `QuaderSignatureInvalid` | `increase(quader_signatures_checks_total{result="invalid"}[1h]) > 0` | 0m | warning |
| `QuaderSignatureBacklog` | `quader_signatures_oldest_pending_seconds > 3600` | 15m | warning |
| `QuaderSignatureTrustRootStale` | `quader_signatures_trust_root_age_seconds > 1209600` (14 days) | 1h | warning |

Acceptance criteria:

- Unit (`internal/audit`, under a second): the events are in the registry with their schemas and journal flags.
- Chart test (`test/chart`, about 1 s): the three alerts render and `promtool` checks them.

### R-SIG-040 · Configuration, defaults and what it costs
Priority: **P1** · Covers: quader-only

```yaml
signatures:
  enabled: true                     # default; false: no detection, no check, no badge; stored rows stay
  concurrency: 4                    # default; checks one job has in flight
  run_slice: 10m                    # default
  max_attempts: 5                   # default
  check_timeout: 5s                 # default; one check
  max_object_size: 256KiB           # default; one signature object
  max_per_subject: 32               # default; signatures read per subject and repository
  max_chain: 8                      # default; certificates in a chain
  max_content_size: 8GiB            # default; content a keyless scheme or NuGet reads
  remote:
    max_manifests_per_subject: 4    # default; OCI signature manifests fetched per image
    refresh_after: 24h              # default
    requests_per_hour: 600          # default; per remote and replica
  trust_root:
    refresh: true                   # default; false: the release's snapshot of the public Sigstore root and npm's keys
    url: https://tuf-repo-cdn.sigstore.dev   # default; or a TUF mirror of the company
  anchors: {}                       # default; anchors declared by the Helm values, keyed by the anchor's key (R-SIG-018)
jobs:
  workers:
    signatures: 1                   # default
```

- **The repository member** `signatures` (R-REPO-004): `{"fetch": true}` on a remote repository (default), switched in the closed section
  **Signatures** of its settings page. Stage 3 adds `onlyVerified` and `anchors` on every class (R-SIG-061) and its own keys under
  `signatures.gate` (R-SIG-076).
- **What it costs:** no new Deployment, image or container; the binary grows by 4.76 MiB (R-SIG-014); PostgreSQL holds a signature object of
  a few hundred bytes to some kilobytes (an npm registry signature about 150 bytes, a Sigstore bundle about 4 KiB, a NuGet `.p7s` with its
  chain and timestamp up to about 20 KiB; estimates) and a few hundred bytes of rows per signature; an npm remote whose documents name a
  million versions holds about a million registry signatures, some 300 MB with their rows (an estimate); the worker's CPU for checks of
  milliseconds each and the reads of R-SIG-006.

Acceptance criteria: unit (`internal/config`, under a second): the defaults; a `max_object_size` above 1 MiB, a `concurrency` of 0 and a
`trust_root.url` without `https://` are refused with the key named.

### R-SIG-041 · Backups, restores and replicas
Priority: **P1** · Covers: quader-only

- **Backups** ([backup-and-restore.md](backup-and-restore.md) R-BAK-004): `signature_objects`, `signatures`, `signature_attachments`,
  `signature_anchors`, `signature_settings`, `signature_blocks`, `signature_trust_roots` and `artifact_origins` are `data`;
  `signature_trust` is `transient`: after a restore the trust job recomputes every row from the restored fields, reading no artifact.
- **Restores.** The journal re-applies what narrowed after the restore point: a removed or switched-off anchor, a block, the switch turned
  off ([authorization.md](authorization.md) R-AUTHZ-031); an anchor added after it is lost, which narrows.
- **Replicas.** The jobs run once in the installation (`coalesce`, `exclusive`); every replica reads anchors, blocks and the switch from a
  snapshot that the change area `signature_anchors` refreshes within R-ARCH-018's bound; a screen reads the stored rows, so every replica
  shows the same.

Acceptance criteria: integration (area `signatures`, about 3 s): a backup with an anchor, a block and the switch on, restored into a fresh
installation, keeps the three and recomputes the trust rows; a block recorded after the backup is re-applied by the journal.

### R-SIG-042 · The tests and what they cost
Priority: **P1** · Covers: quader-only

Each behaviour is tested once, at the cheapest tier that shows it ("Tests stay runnable" in [CLAUDE.md](../../CLAUDE.md)), with payloads of
bytes to kilobytes. Every one of the owner's three ways is exercised by a real signature made in the test, in process
([Q-SIG-46](#q-sig-46--the-three-ways-are-tested-with-real-signatures-made-in-process)):

- **The company's Sigstore:** sigstore-go's virtual Sigstore (`pkg/testing/ca`: a certificate authority, a log and a timestamp authority of
  the test's own, verified in v1.3.0's source) issues a certificate for an identity and an issuer of a GitLab job and signs; its trusted root
  is the anchor's own trust root. The public trust root's case uses a second virtual Sigstore standing for the public one.
- **The company CA:** a root, an intermediate and a code-signing leaf made by the test with `crypto/x509`; Notation envelopes signed with
  notation-core-go, NuGet packages with digitorus/pkcs7 and a timestamp token of a test authority, cosign's older layout with the leaf.
- **The release key:** an OpenPGP key made by go-crypto signs detached.

| Tier | What | Run time | Data | Where |
|---|---|---|---|---|
| Unit | every scheme's read and check; the trust step, the scopes, the time proofs, the badge rule; the anchors' checks; the bounds; the job's order; the configuration | under 5 s together (an estimate) | bytes, built by the tests | `internal/signatures/…`, `internal/config` |
| Format contract kit | check 18 for every format | milliseconds per format | one signed sample per kind | `internal/formats/formattest` |
| Integration | the flows of R-SIG-003 to R-SIG-041 against the stack, with the format stubs that exist, which gain signed samples | about 50 s for the new area `signatures` (about twenty tests of 1 to 4 s), about 2 s more in `ocivirtual` and `bastion` (estimates); the formats' wire behaviour of package 3 in each format's own area: `ocilocal` about 4 s, `ociremote` about 9 s, `ansiblelocal` about 2 s, `npm` about 3 s (measured on 2026-11-21) | bytes to kilobytes | `test/integration/signatures` and the format's area |
| Chart | the three alerts | about 1 s | rendered YAML | `test/chart` |
| End to end | one case in `e2e (npm)`: `npm audit signatures` through `npm-remote` | about 5 s, no new leg, no client image | a package of the test upstream | `test/e2e` |

What every leg gains: the unit run under 5 s; the kit milliseconds per format; the integration shard that runs `signatures` about 50 s; the
chart leg about 1 s; `e2e (npm)` about 5 s. No other leg gains anything. With 25 formats the cost is the same plus 25 kit cases of
milliseconds and one small case per format that serves signatures in its own integration test; no leg loads another format's client.

**Measured for packages 1 and 2** (2026-11-21, the development machine): the unit tests of `internal/signatures/...` take about 1 s a
package and about 10 s together, the API's new tests about 5 s; the area `signatures` runs in about 15 s (package 1's two tests and
package 2's three of about 4 s each, files of a kilobyte); the chart's signature tests about 2 s. R-SIG-017's refresh is tested at the
unit tier against a TUF repository the test builds (`shipped/tuftest`), not in the area. R-SIG-041's replay of what narrowed trust is
tested in the area `backup` against a restored database (`TestR_SIG_041_WhatNarrowedTrustIsReplayedIntoARestore`, about 2 s, rows); a
whole backup and restore of the signature tables is not, nor R-SIG-012's variant with the object store stopped.

What only a real client shows and is not tested: cosign, Notation, `dotnet nuget verify` and `ansible-galaxy --keyring` against Quader. Each
is measured once by hand when its format's part is built, and the result written into this requirement.

Acceptance criteria: the coverage generator lists every requirement of this page with a test of the tier this table names.

### R-SIG-043 · What stage 1 keeps for stage 2 and stage 3
Priority: **P1** · Covers: quader-only

Stage 2 is specified in [Quader signs (stage 2)](#quader-signs-stage-2) and stage 3 in
[The gate per repository (stage 3)](#the-gate-per-repository-stage-3); both use what stage 1's model carries:

- **Stage 2, Quader signs** ([Q-SIG-10](#q-sig-10--quader-signs-as-a-registry), [Q-SIG-11](#q-sig-11--quader-issues-attestations),
  [Q-SIG-13](#q-sig-13--the-signing-identity)): the keys endpoint of R-SIG-028 is where Quader's npm key joins: a local repository's
  `{"keys": []}` becomes the keys of the signing identity, and the virtual union with them closes H-1176 for an installation that has one
  (R-SIG-050); `artifact_origins` (R-SIG-010), the integrity verdicts, the malware verdict and the vulnerability state are the facts of the
  receipt attestation, the trust verdicts are left out of it because they are recomputed (R-SIG-051); Quader's npm signature and its
  attestation are signatures of the roles `registry` and `attestation` in this page's store, attached with the new source `issued` and
  marked with the identity that made them (R-SIG-054); the signing identity of the Helm values or of the admin area, sealed by the key ring
  when uploaded and with the Helm values winning, is the table `signing_identities` (R-SIG-045 to R-SIG-048).
- **Stage 3, the gate per repository** ([Q-SIG-9](#q-sig-9--display-or-gate)): `signature_trust` holds per signature and scope the state and
  every anchor that knows the signer (`anchor_ids`), so a gate "only artifacts with a verified signature, optionally of chosen anchors" is a
  join of the item's blob to its signatures, their attachments in the repository and the trust row of the addressed repository's scope, at
  the place where the malware gate sits ([malware-scanning.md](malware-scanning.md) R-MAL-013), for local, remote and virtual repositories
  alike; the remote's background fetch (R-SIG-026) is what lets a remote's first pull be judged. Stage 3 adds the setting (R-SIG-061), what
  passes (R-SIG-062), what the format names as an artifact (R-SIG-063), the answer in each client's shape (R-SIG-068), the wait for a pending
  check with the fetch's state (R-SIG-067), and the subtraction that keeps the stale rows of H-1175 from serving what a narrowing refuses
  (R-SIG-066). It changes stage 1's tables by two columns (`signature_anchors.narrowed_epoch`, `signature_blocks.epoch`) and one index
  (`signature_trust_anchors`).

Acceptance criteria: none; the stages bring their own.

## Quader signs (stage 2)

Status: specified, not built. The owner asked for it in his request of 2026-11-21 ("Quader ist ebenso in der Lage Artefakte zu signieren,
dafür muss aber erst ein Zertifikat in Quader über Helm oder das Frontend gepflegt werden. Hier müssen wir uns aber noch über den Umfang und
Ziele unterhalten, ich erwarte, dass du mich hier proaktiv berätst.") and decided its frame the same morning: Quader signs as a registry
where a client checks it with one command, which is Debian (built) and npm ([Q-SIG-10](#q-sig-10--quader-signs-as-a-registry)); it issues
signed attestations of what it received and checked, for an image as a referrer ([Q-SIG-11](#q-sig-11--quader-issues-attestations)); it
never signs in a publisher's name ([Q-SIG-12](#q-sig-12--quader-signs-in-a-publishers-name)); it holds one signing identity per
installation from the company's CA, and OpenPGP keys per repository where a format needs them
([Q-SIG-13](#q-sig-13--the-signing-identity)); the certificate comes through Helm (a Secret that cert-manager may issue) or an upload
sealed by the key ring, and Helm wins (the reading r8 of [Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request)). Before he left on
2026-11-21 he was told that the second stage is specified and built after the first without asking him, its open points decided by the
development agent and documented (the plan's standing rules); every decision below that is not his is the development agent's ([ADR-230](../decisions.md#adr-230--quader-signs-with-one-identity-per-installation-npms-registry-signatures-and-a-receipt-attestation-of-every-artifact-signed-in-the-worker)).

**What a signature of Quader says, and what it does not.** Quader's npm signature says that this installation's local repository serves
this version with this integrity. Its receipt attestation says that this installation received the artifact with this digest, when, from
which uploader or upstream, and what its checks said at the time. Neither says who made the content: that is the publisher's signature
and the provenance of stage 1, which Quader never makes
([H-1187](../security/artifact-signatures.md#h-1187--a-holder-of-the-signing-key-can-make-clients-believe-anything-was-received-here)).

### R-SIG-044 · What stage 2 signs, per format
Priority: **P1** · Covers: quader-only

Two kinds of signature, both made with the installation's one identity (R-SIG-045); Debian's indexes keep their own keys:

- **A registry signature** where a client checks it with one command (Q-SIG-10).
- **A receipt attestation** of every artifact a user's repository receives (R-SIG-051).
- **Never a publisher's signature** (Q-SIG-12): Quader's signatures have the roles `registry` and `attestation` and never carry the badge
  of R-SIG-009; who uploaded is shown as the origin (R-SIG-010).

| Format | Registry signature | Receipt attestation of | How a client gets the attestation |
|---|---|---|---|
| OCI | none: no client checks a registry's own signature of an image with one command against that registry; the attestation is the evidence | every top-level manifest (one a tag names, or one pulled by digest that no stored index lists) that a push or a remote fill stores | a referrer of the manifest, where it hides no older signature (R-SIG-054, [Q-SIG-66](#q-sig-66--quaders-referrer-never-stands-where-cosign-would-stop-reading-an-older-signature)); `cosign verify-attestation` (R-SIG-052) |
| npm | every version of a local repository: ECDSA P-256 over `<name>@<version>:<integrity>` in `dist.signatures`, the keys at `-/npm/v1/keys`; `npm audit signatures` (R-SIG-050) | every tarball | the API and a download at the version (R-SIG-054); not through npm's attestations endpoint ([Q-SIG-60](#q-sig-60--npm-quaders-attestations-are-not-served-through-npms-attestations-endpoint)) |
| NuGet | none: a repository countersignature changes the package's bytes (Q-SIG-10); no `RepositorySignatures` resource ([Q-SIG-36](#q-sig-36--nuget-lists-no-repository-signatures-resource) stands) | every `.nupkg` and `.snupkg` | the API and a download |
| Debian | the indexes of local and virtual repositories, signed with each repository's OpenPGP key as built ([formats/debian.md](formats/debian.md) R-DEB-016, R-DEB-025); `apt`; nothing changes ([Q-SIG-69](#q-sig-69--debian-the-repository-keys-get-no-certification-by-the-identity)) | every `.deb`; never an index | the API and a download |
| Ansible Galaxy | none in stage 2 ([Q-SIG-68](#q-sig-68--ansible-galaxy-quader-does-not-sign-local-collections-in-stage-2)) | every collection archive | the API and a download |
| generic | none: no scheme of a registry exists for a file | every file but one that R-SIG-032 reads as a signature of its sibling | the API and a download |
| `clamav` (a system format) | none | none | — |

- **System repositories** of `upstream` (`scanner-trivy`, `clamav`; [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-070,
  R-REPO-075) get no attestation: they hold what the product fetches for its own tools, `clamd` checks the publisher's signature of each
  ClamAV container itself, and the company's ClamAV installations do the same through the definitions' endpoint; a receipt of Quader's own
  tool data would be read by no one.
- **The format names what is attested** ([Q-SIG-67](#q-sig-67--the-format-names-what-quader-attests-and-the-contract-rises-to-version-9)):
  stage 2 adds a method to the adapter of R-SIG-022, and `ContractVersion` rises to 9 in the change that adds it:

  ```go
  // AttestationSubjects names the items of a commit that Quader attests: the artifacts a client fetches,
  // never an item that carries a signature or an attestation. A format with no subject returns nil.
  AttestationSubjects(ev SignatureEvent) []AttestationSubject

  type AttestationSubject struct {
      Item    int64  // the attested item
      Name    string // the artifact's name in the format's words: an image, a package, a path
      Version string // empty where the format has none
  }
  ```

  The declaration (R-SIG-022) gains `Attests string`, what is attested in the format's words, or empty with the reason in `AttestsNone`.
  A future format declares both and, where its clients check a registry's signature with one command, says so; that scheme comes back to
  the owner as a decision of its own (the owner's rule of 2026-11-19).
- **Without a signing identity Quader signs nothing:** a local npm repository's keys answer `{"keys": []}` as in stage 1, no version
  carries `dist.signatures`, no attestation is made, and the admin page says so (R-SIG-056). Debian's index signing is not affected.

Acceptance criteria:

- Unit (the format contract's kit, check 18 amended, milliseconds per format): each format's `AttestationSubjects` over its fixture
  cases equals the table: OCI names the top-level manifest and not its platform manifests, layers, referrers or attachment tags; npm the
  tarball and not the provenance bundle; NuGet the `.nupkg` and the `.snupkg`; Debian the `.deb`; Ansible the archive; generic a file and
  not its `.asc` sibling; `clamav` nothing, with `AttestsNone` set. The test format's defect `AttestsSignature` (a signature carrier named
  as a subject) fails it.

### R-SIG-045 · The signing identity: what it must be and its states
Priority: **P1** · Covers: quader-only

One per installation (Q-SIG-13): a private key and an X.509 certificate of the company's CA with its chain. The material is checked
whenever it is loaded from the Helm values or uploaded ([Q-SIG-53](#q-sig-53--the-signing-key-is-ecdsa-p-256-with-a-code-signing-certificate)):

| Part | Requirement | Why |
|---|---|---|
| Key | ECDSA on P-256 | npm's client accepts only `ecdsa-sha2-nistp256` registry keys (npm's documentation, read 2026-11-21); cosign, sigstore-go and the Notary Project's verifiers take it too, so one key serves every use |
| Leaf | not a CA certificate; key usage `digitalSignature`; extended key usage `codeSigning` | sigstore-go's check of a certificate authority requires `codeSigning` on the leaf (`pkg/root/certificate_authority.go`, read 2026-11-21); a CA anchor of stage 1 requires it too (Q-SIG-30), so an installation whose company CA is an anchor knows Quader's own attestations |
| Alternative name | at least one URI or e-mail address; the first URI is what the client setup names as the identity | cosign matches `--certificate-identity` against the alternative names, not the subject (cosign's source, read 2026-11-21) |
| Validity | valid at the check, with at least 24 hours left; shown amber from 14 days before its end (R-SIG-057) | an attestation verifies only while its certificate is valid (R-SIG-052) |
| Chain | the leaf, its intermediates and a self-signed root (the last certificate, or the separate `ca.crt` or `ca` field); the chain builds with Go's `crypto/x509` for `codeSigning`; at most 8 certificates of 16 KiB each (R-SIG-013's bound) | Quader serves the chain and a trusted root made of it (R-SIG-052) |
| Pair | the key is the leaf's | |

```sql
CREATE TABLE signing_identities (
  id              bigserial PRIMARY KEY,
  source          text    NOT NULL CHECK (source IN ('configuration', 'upload')),
  state           text    NOT NULL CHECK (state IN ('pending', 'announced', 'current', 'previous', 'retired')),
  public_key      bytea   NOT NULL,                        -- DER SubjectPublicKeyInfo
  key_sha256      bytea   NOT NULL CHECK (octet_length(key_sha256) = 32),
  npm_keyid       text    NOT NULL,                        -- R-SIG-050
  sealed_key      text    NULL,                            -- upload: the private key under the key ring (R-SIG-047); configuration: always NULL
  csr             text    NULL CHECK (length(csr) <= 8192), -- pending: the PEM request
  certificate     bytea   NULL,                            -- DER leaf; NULL while pending
  cert_sha256     bytea   NULL CHECK (octet_length(cert_sha256) = 32),
  chain           bytea[] NOT NULL DEFAULT '{}',           -- DER intermediates, then the root
  subject         text    NULL CHECK (length(subject) <= 1024),
  issuer          text    NULL CHECK (length(issuer) <= 1024),
  names           text[]  NOT NULL DEFAULT '{}',           -- the alternative names
  not_before      timestamptz NULL,
  not_after       timestamptz NULL,
  announced_at    timestamptz NULL,
  current_at      timestamptz NULL,
  expires_at      timestamptz NULL,                        -- previous: the moment it stopped signing (R-SIG-048)
  retired_at      timestamptz NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid    NULL REFERENCES principal (id) ON DELETE SET NULL,
  CHECK ((source = 'upload') = (sealed_key IS NOT NULL)),
  CHECK ((state = 'pending') = (certificate IS NULL)),
  UNIQUE (cert_sha256)
);
CREATE UNIQUE INDEX signing_identities_pending   ON signing_identities ((true)) WHERE state = 'pending';
CREATE UNIQUE INDEX signing_identities_announced ON signing_identities ((true)) WHERE state = 'announced';
CREATE UNIQUE INDEX signing_identities_current   ON signing_identities ((true)) WHERE state = 'current';
```

| State | Meaning | Signs | Listed at `-/npm/v1/keys` | Its signatures served |
|---|---|---|---|---|
| `pending` | an upload's key and request exist; no certificate yet (R-SIG-047) | no | no | — |
| `announced` | the certificate is there; listed so that caches learn the key before it signs (R-SIG-048) | no | yes, `expires: null` | — |
| `current` | the identity that signs | yes | yes, `expires: null` | yes |
| `previous` | signed until `expires_at`; keeps its signatures valid | no | yes, `expires` = `expires_at` | yes |
| `retired` | an administrator retired it (R-SIG-048) | no | no | no; the current identity makes them again |

- **While no identity is current, nothing of Quader's is on npm's wire:** the keys endpoint answers `{"keys": []}` and no document carries
  Quader's `dist.signatures`, whatever previous identities exist ([Q-SIG-57](#q-sig-57--a-new-key-is-announced-before-it-signs-old-npm-signatures-stay-attestations-are-made-again)):
  npm reports every unsigned package of a registry that offers keys as missing.
- **A renewal with the same key** (cert-manager's `rotationPolicy: Never`, or a company CA that reissues for the key) is a new row with
  the same `npm_keyid`; the keys endpoint lists a key once, with the expiry of its newest row.
- **What the database never holds:** the private key of the Helm values. Its row holds the public key, the chain and the state.
- An identity whose certificate has expired signs nothing, also while `current`; it stays listed (R-SIG-057 alerts before).

Acceptance criteria:

- Unit (`internal/signing`, milliseconds, material made by the test with `crypto/x509`): each row of the table with a passing and a
  failing sample (an RSA key, a P-384 key, a leaf without `codeSigning`, one without an alternative name, an expired one, one with 23 hours
  left, a CA certificate as the leaf, a chain without its root, a chain that does not build, a key that is not the leaf's); the state
  table as allowed and refused transitions.

### R-SIG-046 · The identity from the Helm values
Priority: **P1** · Covers: quader-only

The owner's reading r8: through Helm, a Secret that cert-manager may issue; Helm wins
([Q-SIG-54](#q-sig-54--where-the-key-lives-in-each-way-and-the-helm-values-win)).

- **The files.** The chart mounts a Secret with `tls.crt` (the leaf and its intermediates), `tls.key` (the key) and `ca.crt` (the root),
  the keys of a cert-manager TLS Secret, into the `worker` pods only, and sets `signing.identity.cert_file`, `key_file` and `ca_file`
  ([ha-and-helm.md](ha-and-helm.md) R-HA-071). The Secret is an existing one, or the one of a cert-manager `Certificate` the chart renders.
- **Loading without a restart.** A worker reads the files at start and compares their content every `signing.identity.reload_interval`
  (default 1 minute), as the TLS reloader does (R-ARCH-035); the kubelet swaps the volume, nothing rolls. A pair that passes R-SIG-045 is
  registered: a row of the source `configuration` is inserted when no row has its certificate, `announced` while another identity is
  current, `current` when none is (R-SIG-048), and `signing.identity.installed` is recorded. A pair that fails is not used, the identity in
  use keeps signing, `signing.identity.refused` is recorded once per content with the reason, and the admin page shows it.
- **The key stays in the worker's memory**, in a type that renders nothing (`observability.Secret`), and in the file; it is written
  nowhere else.
- **Helm wins.** While `signing.identity.cert_file` is set, the identity of the files is the installation's: the admin page shows it with
  "From the Helm values" and no controls, an upload or a key request answers `409 signing_identity_managed`, and an uploaded identity that
  is stored stays unused and marked "Not in use: the Helm values name the signing identity". When the values stop naming one, the identity
  of the files becomes `previous`, and a stored uploaded identity that passes R-SIG-045 is announced; without one Quader signs nothing from
  then on.
- `api` and `packages` read the public part from the database (the keys endpoint, the chain, the trusted root) and never the files.

Acceptance criteria:

- Integration (area `signing`, `TestR_SIG_046_…`, about 4 s, files written by the test into the stack worker's directory, announce period
  1 s): a pair becomes current and signs an npm publish; replacing the files with a pair of a new key announces it, then makes it current and
  the old one previous with its `expires_at`, without a restart; a pair with an RSA key is refused with `signing.identity.refused` and the
  old pair keeps signing; a key request answers 409 `signing_identity_managed`.

### R-SIG-047 · The identity uploaded in the admin area
Priority: **P1** · Covers: quader-only

The plain path is a certificate request: Quader makes the key, and it never leaves Quader
([Q-SIG-55](#q-sig-55--the-upload-is-a-certificate-request-for-a-key-quader-makes-an-import-is-a-detail)).

1. **Create signing key** (`POST /api/v1/signing/identity/key`): the `api` replica that answers generates an ECDSA P-256 key, seals it
   under the key ring's active key into `signing_identities.sealed_key` (a registered sealed column, row key the identity's `id`,
   [architecture.md](architecture.md) R-ARCH-021) in the transaction that writes the row `pending`, forgets it, and answers a PKCS#10
   request (PEM) for the subject and alternative names the form names, asking for `digitalSignature` and `codeSigning`. The form's fields
   hold the defaults: common name `Quader signing`, the URI `server.base_url`.
2. **Upload certificate** (`PUT /api/v1/signing/identity/certificate`): the certificate the company's CA issued, with its chain (PEM, the
   leaf first; the root last or in `ca`). It is checked by R-SIG-045 and against the pending key (`422 signing_key_mismatch`), and the
   identity is announced, or current when none is (R-SIG-048).

- **Import** (`POST /api/v1/signing/identity/import`), behind **More options**: a PEM private key (PKCS#8 or SEC 1, unencrypted, or an
  encrypted PKCS#8 whose passphrase is used once and not kept, as Debian's import, [formats/debian.md](formats/debian.md) R-DEB-015) and
  the chain, for a CA that hands out key and certificate together or a key moved from another installation. PKCS#12 is not taken; the
  manual names `openssl pkcs12 -in <file> -nodes` to convert.
- **Discard** (`DELETE /api/v1/signing/identity/pending`) removes a pending key and its request.
- The plaintext key is held in a type that renders nothing, never logged, and in no answer, audit event, journal entry or error text. The
  request, the certificate and the chain are public.
- **Who opens it:** the job `signing` in the worker, for its signing calls (R-SIG-049). `api` never opens it after the generation or the
  import: the check of a certificate against the pending key compares the stored public key.
- **Re-sealing:** the task `secrets-reencrypt` walks the column like every registered column.

Acceptance criteria:

- Integration (area `signing`, `TestR_SIG_047_…`, about 3 s, a CA of the test): a key request answers a request that parses, holds a P-256
  key, the base URL as its URI and the code-signing use; the CA's certificate for it is accepted, the identity becomes current and signs;
  a certificate for another key answers 422 `signing_key_mismatch`; an import of a key and a chain becomes current; `sealed_key` does not
  open under another row's key; no answer, audit event or captured log line of the test holds `PRIVATE KEY`.

### R-SIG-048 · Rotation, removal, retirement, and what the old signatures do
Priority: **P1** · Covers: quader-only

- **Announce, then switch** ([Q-SIG-57](#q-sig-57--a-new-key-is-announced-before-it-signs-old-npm-signatures-stay-attestations-are-made-again)).
  A new identity beside a current one is `announced`: listed at the keys endpoint for `signing.identity.announce_period` (default
  10 minutes), so that a cache of the keys document (the Bastion, a remote of another registry in front of this one) holds the new key
  before a signature made with it reaches that cache; the current one keeps signing meanwhile. The job `signing` then switches in one
  transaction: the announced identity becomes `current`, the current one `previous` with `expires_at = clock_timestamp()`. An
  installation's first identity, with none current, is current at once. With the Helm way the old key stays in the memory of the workers
  that loaded it, after cert-manager has replaced the files; a worker that starts during the announce period holds only the new key and
  signs nothing until the switch.
- **No signature with a key after its switch.** A signing transaction holds a shared lock on the current identity's row and checks its
  state before it writes; the switch locks the row exclusively; so every npm signature of an identity is on a version published before its
  `expires_at`. That is what npm's client compares, `time[<version>]` against the key's `expires`, and an expired key aborts the whole run
  with `EEXPIREDSIGNATUREKEY` (pacote 22.0.1 `lib/registry.js`, npm 12.2.3 `lib/utils/verify-signatures.js`, read 2026-11-21), so a
  late signature would break every client's run, not one package. A worker whose loaded key is not the current identity's signs nothing and
  leaves the work due.
- **npm signatures of a previous identity stay valid and are not made again**: its key stays listed with its `expires`, and every version
  it signed was published before.
- **Attestations are made again** by the new current identity (R-SIG-053): a bundle carries the leaf certificate, and without a log or a
  timestamp ([Q-SIG-63](#q-sig-63--no-transparency-log-and-no-timestamp-authority-in-stage-2)) a verifier checks it at the current time, so
  an attestation stops verifying when its certificate expires. Cost: one ECDSA signature (tens of microseconds) and about 3 KiB written per
  attestation; a million attestations take minutes of one worker and write about 3 GB (estimates); with cert-manager's 90-day certificate
  renewed after 60 days, six times a year.
- **Remove** (`DELETE /api/v1/signing/identity` for the current uploaded identity; an identity of the Helm values is removed by taking it
  out of the values): it becomes `previous`. With no identity current, no key and no npm signature of Quader is on the wire (R-SIG-045); attestations already made
  stay stored and served and verify until their certificate expires. A later identity brings the previous keys and their signatures back
  and makes the attestations again.
- **Retire** (`POST /api/v1/signing/identities/{id}/retire`, a previous identity; for a key believed taken): it leaves the keys list at
  once, none of its signatures is served, and the job makes each of its npm signatures and attestations again with the current identity
  (priority 2); until then those npm versions read as missing a signature. Retiring narrows trust and carries the journal flag
  (R-SIG-057).
- **Which signatures a document serves changes** with a removal, a retirement and the first current identity after none: the job raises
  the revision of every npm package whose document changes (R-NPM-009), in batches of 1,000 packages.

Acceptance criteria:

- Unit (`internal/signing`, milliseconds): a signing transaction that began before a switch and commits after it is refused; the keys
  document for each combination of states.
- Integration (area `signing`, `TestR_SIG_048_…`, about 4 s, announce period 1 s): after a rotation, a version signed before carries the old
  keyid and its `time` lies before the old key's `expires`; a version published after carries the new keyid; the old identity's
  attestations are made again by the new one; retiring the old identity removes its key from the keys answer and its signatures from the
  documents, and the job signs those versions again; removing the current identity makes the keys answer `{"keys": []}`.

### R-SIG-049 · Where signing runs: the job `signing` in the worker
Priority: **P1** · Covers: quader-only

Only the worker holds and opens the key ([Q-SIG-56](#q-sig-56--only-the-worker-holds-the-key-and-a-job-signs)).

- **The work** is a row per item and kind, written in the commit's transaction by the item store's commit hook (an npm version's tarball in
  a local repository: `npm`; an attestation subject of R-SIG-044: `attestation`) and by the job's own steps:

  ```sql
  CREATE TABLE signing_work (
    kind        text     NOT NULL CHECK (kind IN ('npm', 'attestation')),
    item_id     bigint   NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    reason      text     NOT NULL CHECK (reason IN ('upload', 'fill', 'checks', 'malware', 'rotation', 'retired', 'discovery')),
    priority    smallint NOT NULL CHECK (priority BETWEEN 0 AND 3),
    not_before  timestamptz NOT NULL DEFAULT now(),
    attempts    integer  NOT NULL DEFAULT 0,
    failed      boolean  NOT NULL DEFAULT false,
    PRIMARY KEY (kind, item_id)
  );
  CREATE INDEX signing_work_due ON signing_work (priority, not_before) WHERE NOT failed;
  ```

- **The job** `signing` (queue `signing`, `coalesce`; `jobs.workers.signing`, default 1; `signing.concurrency`, default 4, operations in
  flight) takes due rows by priority: 0 an npm signature of a publish (a pipeline may run `npm audit signatures` next), 1 the attestation of
  an upload, 2 the attestation of a fill and what a retirement left, 3 discovery and the attestations a rotation left. It is woken by the
  commit, by the verdicts R-SIG-053 waits for, and by an identity's change.
- **Discovery** (the job's first step and the task `signing`, default `*/10 * * * *`) finds, in batches of 10,000 in item order, the npm
  versions of local repositories without a served signature and the attestation subjects without an attestation of the current identity:
  what was stored before an identity existed, what a restore loaded, what a rotation or a retirement left.
- **Attempts:** an operation that could not run (the object store for an OCI referrer, the database) spends one of `signing.max_attempts`
  (default 5) and is due again after a doubling back-off from one minute; after the last it is `failed`, recorded once an hour per kind and
  code (`signing.failed`), and discovery makes it due once a day.
- **Replicas.** Every worker replica holds the same key; the job runs once in the installation (`coalesce`), so no two replicas sign the same
  item. Should a lost lease ever let two sign, ECDSA's two signatures differ and both verify, npm checks every entry of `dist.signatures`
  (verified in pacote) and both pass, and a second attestation replaces the first: no coordination is needed beyond the switch's lock of
  R-SIG-048.
- **No other role opens the key.** `api` and `packages` read public parts; the composition root wires the signer for the roles `worker`
  and `all` only.

Acceptance criteria:

- Unit (`internal/signing/job`, under a second): the order by priority and time, the back-off, the attempts; `internal/archtest`
  (milliseconds): only `internal/signing` opens `signing_identities.sealed_key` and reads `signing.identity.key_file`, and the wiring of
  `api` and `packages` builds no signer.
- Integration (area `signing`, `TestR_SIG_049_…`, about 2 s): a publish to `npm-local` carries `dist.signatures` within 2 s of its answer;
  a version stored before the identity existed is signed by discovery.

### R-SIG-050 · npm: registry signatures of local packages, and the keys endpoint
Priority: **P1** · Covers: PKG-NPM-037

What npm's client does, read on 2026-11-21 in npm 12.2.3's `lib/utils/verify-signatures.js` and pacote 22.0.1's `lib/registry.js`, and
checked against npmjs's live key with `ssh-keygen` and `openssl`:

- The signed message is `${_id}:${_integrity}`: `<name>@<version>:<dist.integrity>`; the check is `crypto.createVerify('SHA256')` with a
  base64 DER ECDSA signature, and every entry of `dist.signatures` must verify.
- The keys come first from the public Sigstore TUF repository (the target `<host><path>/keys.json`); for a registry that is not there,
  npm warns ("Fetching verification keys using TUF failed. Fetching directly from …") and reads `<registry URL>-/npm/v1/keys`, once per
  registry URL; 404 or 400 there means no keys. Any other failure of the TUF step aborts the run
  ([H-1184](#h-1184--npm-audit-signatures-needs-the-public-sigstore-tuf-repository-before-it-asks-quader)).
- `keyid` is looked up as a string and never checked against the key; `keytype` and `scheme` are not read, and the documentation names
  only `ecdsa-sha2-nistp256`.
- `expires` is compared with `time[<version>]` of the full document (2015-01-01 when `time` is absent); a signature of an expired key
  aborts the run with `EEXPIREDSIGNATUREKEY`.
- The outcomes: every package verified, exit 0, "N packages have verified registry signatures"; a registry with keys and a version without
  a signature, "N packages have missing registry signatures but the registry is providing signing keys", exit 1; an invalid signature,
  exit 1; a registry without keys, its packages not counted; a signature whose `keyid` the keys answer lacks, the run aborts.

What Quader does ([Q-SIG-58](#q-sig-58--npm-quader-signs-the-versions-of-local-repositories-only),
[Q-SIG-59](#q-sig-59--npm-signed-right-after-the-publish-and-what-was-stored-before-by-a-background-pass)):

- **Signed:** every version of a local npm repository, by the current identity, as `{"keyid": "<npm_keyid>", "sig": "<base64 DER>"}`
  over `<name>@<version>:<dist.integrity>`, stored as a signature of the role `registry` and the scheme `npm-registry` with `identity_id`
  set, attached `issued` to the version's tarball (R-SIG-054). The full and the abbreviated documents render `dist.signatures` from the
  stored signatures whose identity is listed (R-SIG-045); a client's own `dist.signatures` stays refused at the publish (R-NPM-007).
- **Not signed:** a remote's versions keep their upstream's `dist.signatures` byte for byte (R-SIG-028, unchanged); a virtual repository's
  version entry is its winning member's (R-NPM-030), with that member's signatures.
- **When:** at priority 0, woken by the commit; within 2 s of the publish's answer on an idle installation (a target the package that
  builds it measures). Until then the version has no signature and reads as missing
  ([H-1183](#h-1183--npm-audit-signatures-reports-a-local-version-as-missing-until-quader-has-signed-it)). Signing raises the package's
  revision and `modified_at` like a dist-tag change (R-NPM-009, R-NPM-012), once per package and job run, so every replica's rendition and
  every virtual repository's merge carries it.
- **What was published before the identity existed** is signed by discovery with the first current identity: a million versions are
  about a minute of one worker's CPU and a revision per package (an estimate).
- **The keys endpoint** of a local repository (R-SIG-028's `{"keys": []}` replaced while an identity is current) lists the announced and the
  current identity with `expires: null` and each previous one with its `expires_at`, each keyid once:

  ```json
  {
    "keys": [
      {"keyid": "SHA256:Zq2b…", "keytype": "ecdsa-sha2-nistp256", "scheme": "ecdsa-sha2-nistp256", "key": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE…", "expires": null},
      {"keyid": "SHA256:Kt1x…", "keytype": "ecdsa-sha2-nistp256", "scheme": "ecdsa-sha2-nistp256", "key": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE…", "expires": "2027-02-01T10:00:00.000Z"}
    ]
  }
  ```

  `key` is the base64 DER SubjectPublicKeyInfo on one line. `keyid` is `SHA256:` followed by the standard base64, without padding, of the
  SHA-256 of the key's SSH wire form (`ecdsa-sha2-nistp256`, `nistp256`, the uncompressed point): the form npmjs publishes, verified on
  2026-11-21 (`ssh-keygen -lf` of npmjs's current key gives its keyid `SHA256:DhQ8wR5APBvFHLF/+Tc+AYvPOdTpcIDqOhxsBHRwC7U`; the SHA-256 of
  the DER key does not). Go computes it with `ssh.FingerprintSHA256` of `golang.org/x/crypto/ssh`, a module already in the build.
  `expires` is ISO 8601 UTC with milliseconds. Every local npm repository of the installation lists the same keys.
- **A virtual repository** answers the union of its members' keys by keyid (R-SIG-028, unchanged), which now holds Quader's keys for a
  local member: [H-1176](#h-1176--through-a-virtual-npm-repository-npm-audit-signatures-reports-local-packages-as-missing-a-registry-signature)
  closes for an installation with a signing identity. A remote in front of another Quader's local repository receives that installation's
  keys and signatures like any upstream's.
- **What `npm audit signatures` then reports** for a project that installs `@acme/ui` of `npm-local` and `lodash` of npmjs through
  `npm-all`: "2 packages have verified registry signatures", exit 0.

Acceptance criteria:

- Unit (`internal/signing`, `internal/formats/npm`, milliseconds): the keyid of npmjs's published key computed from its `key` equals
  `SHA256:DhQ8wR5APBvFHLF/+Tc+AYvPOdTpcIDqOhxsBHRwC7U` (the vector read on 2026-11-21); a signature of the signer verifies with pacote's
  check written in Go (SHA-256, DER); the keys document for each combination of states; `dist.signatures` rendered only for listed
  identities, in the full and the abbreviated document.
- Integration (area `signing`, `TestR_SIG_050_…`, about 3 s, bytes): a publish to `npm-local` carries the current keyid in the documents of
  `npm-local` and `npm-all`; the keys of `npm-all` hold the npm stub's key and Quader's; the keys of `npm-remote` the stub's alone.
- End to end (the set `e2e (npm)`, one case, about 6 s, a package of a few hundred bytes): `npm publish` to `npm-local`, a wait until its
  document carries the signature, `npm install` through `npm-all` of it and of a package of the npm test upstream, and `npm audit
  signatures` exits 0 and reports both verified. The case needs the client pod to reach the public Sigstore TUF repository, or the npm
  clients image to carry a warm TUF cache; which one works without the network is not verified (H-1184), and the package that builds it
  reads it.

### R-SIG-051 · The receipt attestation: an in-toto statement with a predicate of Quader's own
Priority: **P1** · Covers: quader-only

The owner's decision Q-SIG-11 (a): "a signed statement per artifact: digest, origin, the signature check's result, the malware verdict
with its database, the vulnerability scan's state". Each fact is weighed by whether it stays true once signed
([Q-SIG-61](#q-sig-61--the-statement-in-toto-v1-with-a-receipt-predicate-of-quaders-own-without-trust-states)).

#### Receipt attestation v1

```json
{
  "_type": "https://in-toto.io/Statement/v1",
  "subject": [
    {"name": "quader.example.com/web/npm-local/@acme/ui/-/ui-1.2.0.tgz",
     "digest": {"sha256": "5f1c…", "sha512": "9b0e…"}}
  ],
  "predicateType": "https://github.com/guided-traffic/quader/blob/main/docs/spec/signatures.md#receipt-attestation-v1",
  "predicate": {
    "installation": {"id": "6c3e…", "url": "https://quader.example.com"},
    "repository": {"project": "web", "key": "npm-local", "format": "npm", "class": "local"},
    "artifact": {"path": "@acme/ui/-/ui-1.2.0.tgz", "name": "@acme/ui", "version": "1.2.0", "size": 18342},
    "receivedAt": "2026-11-21T10:02:03.456Z",
    "origin": {
      "kind": "upload",
      "principal": {"kind": "ci_mapping", "name": "gitlab-platform"},
      "ci": {"issuer": "gitlab", "project_path": "platform/ui", "pipeline_id": "48211", "job_id": "991204",
             "ref": "main", "ref_type": "branch", "ref_protected": true, "pipeline_source": "push", "sha": "3e9d…"}
    },
    "signatures": [
      {"role": "provenance", "scheme": "sigstore-bundle", "integrity": "valid",
       "identity": "https://gitlab.example.com/platform/ui//.gitlab-ci.yml@refs/heads/main",
       "issuer": "https://gitlab.example.com", "keyIds": ["9a41…"]}
    ],
    "malware": {"state": "clean", "database": "main-62.daily-27450.bytecode-339",
                "databaseBuiltAt": "2026-11-21T06:10:00Z", "scannedAt": "2026-11-21T10:02:05.120Z"},
    "vulnerabilities": {"state": "evaluated", "grade": "A",
                        "counts": {"critical": 0, "high": 0, "medium": 0, "low": 2, "unknown": 0},
                        "dataAt": "2026-11-21T04:00:00Z"},
    "issuedAt": "2026-11-21T10:02:06.001Z"
  }
}
```

A remote's fill has `"origin": {"kind": "fill", "upstream": "https://registry.npmjs.org/", "fetchedAt": "…"}`; a repository without the
malware scanning `"malware": {"state": "not_scanned"}`; an OCI subject is the manifest's digest under the name
`<host>/<project>/<repository>/<image>`, and its `artifact` names the image, the tag that brought it and the media type.

- **The statement** follows the in-toto attestation framework v1 (`spec/v1/statement.md`, read 2026-11-21): `_type`
  `https://in-toto.io/Statement/v1`, subjects matched by digest, `name` informative. The subject carries the item's SHA-256, and for an npm
  tarball also its SHA-512, npm's integrity.
- **The predicate type** is a fixed URI of this repository with its version, resolving to this section (a type URI should resolve and
  should carry a version, the framework's `field_types.md`). A change that removes or renames a field is `v2`; an added field keeps `v1`.
- **What is in, and why each stays true:**

  | Field | Source | Stays true because |
  |---|---|---|
  | `receivedAt`, `origin` | the item's commit; `artifact_origins` (R-SIG-010); for a fill the remote's upstream and the fill's time | facts of the receipt, which never change |
  | `signatures` | stage 1's signatures attached to the subject in this repository at the issue: role, scheme, the integrity state, identity, issuer, key IDs | the integrity state is a fact of the bytes; the **trust state is left out**, because it depends on the anchors of a scope and is recomputed when they change (R-SIG-012), so a signed copy would contradict the screens after the next anchor change |
  | `malware` | the blob's verdict (R-MAL-002): state, database version and date, scan time | a snapshot with its date; a change of the state makes the attestation again (R-SIG-053), a rescan that keeps the state does not |
  | `vulnerabilities` | the version's evaluation state (R-VULN-001), its grade (R-VULN-038), its open findings per severity, the advisory data's date | a snapshot as of `dataAt`, named so; it is not made again when advisories change, which would sign most versions daily; whoever needs today's state asks the API |
  | `issuedAt` | the signing time | Quader's claim; no log and no timestamp proves it (Q-SIG-63) |

- **Left out:** the trust state, a project's anchors, the uploader's e-mail address (R-CI-020 keeps no `user_email`), any secret.
- **One statement per attested item:** a NuGet version's `.nupkg` and `.snupkg` get one each; an OCI index gets one for the index.
- **Bounds:** a statement of at most 16 KiB; the CI claims as R-SIG-010 bounds them; at most 32 entries in `signatures` (R-SIG-013).

Acceptance criteria:

- Unit (`internal/signing/statement`, milliseconds): the statements of an upload with CI claims, a remote fill, an OCI index, an item with
  an `infected` verdict and one whose checks have not answered equal their golden files; no statement carries a trust state or an e-mail
  address.

### R-SIG-052 · The envelope, the trusted root, and how a client verifies
Priority: **P1** · Covers: quader-only

[Q-SIG-62](#q-sig-62--the-envelope-a-sigstore-bundle-with-a-dsse-envelope-and-the-leaf-certificate).

- **The envelope.** The statement is signed as a DSSE envelope (payload type `application/vnd.in-toto+json`, its pre-authentication
  encoding, one ECDSA P-256 signature over SHA-256) inside a Sigstore bundle of the media type `application/vnd.dev.sigstore.bundle.v0.3+json`
  whose verification material is the identity's leaf certificate, with no transparency-log entry and no timestamp (Q-SIG-63). It is made
  with sigstore-go v1.4.0's `sign.Bundle` over `sign.DSSEData`, a `Keypair` of the identity's key and a `CertificateProvider` that returns
  the leaf (`pkg/sign`, read 2026-11-21); the library is in the build already (R-SIG-014), so stage 2 adds no module.
- **The leaf alone**, because sigstore-go refuses a chain in a v0.3 bundle unless its verifier opts in, and cosign takes one only with
  `--allow-certificate-chain` (cosign 3.1.3 and later); with the leaf alone, the intermediates and the root come from the verifier's
  trusted root (the bundle's protobuf specification, sigstore-go's `pkg/bundle`, cosign's `options/verify.go`, read 2026-11-21).
- **The trusted root Quader serves**, `GET /api/v1/signing/trusted-root.json`: a Sigstore trusted root
  (`application/vnd.dev.sigstore.trustedroot+json;version=0.1`) with one certificate authority per distinct chain of the current and the
  previous identities (its intermediates first, its root last, `validFor` from the earliest `not_before` to the latest `not_after` of the
  identities under it) and no transparency logs, CT logs or timestamp authorities. `GET /api/v1/signing/chain.pem` answers the current
  chain. Both are public material ([Q-SIG-72](#q-sig-72--the-trusted-root-and-the-chain-are-public)).
- **How a client verifies an image's attestation** (cosign 3.1.4, read in its source on 2026-11-21, not run against Quader):

  ```shell
  curl -fsSo quader-trusted-root.json https://quader.example.com/api/v1/signing/trusted-root.json
  cosign verify-attestation \
    --trusted-root quader-trusted-root.json \
    --certificate-identity https://quader.example.com \
    --certificate-oidc-issuer-regexp '.*' \
    --insecure-ignore-tlog --insecure-ignore-sct \
    --type https://github.com/guided-traffic/quader/blob/main/docs/spec/signatures.md#receipt-attestation-v1 \
    quader.example.com/web/docker-local/app@sha256:<digest>
  ```

  `--certificate-oidc-issuer-regexp '.*'` because a certificate of the company's CA carries no issuer extension of Sigstore's and cosign
  then reads its issuer as empty; the two `--insecure-ignore-…` flags because the bundle has no log entry and no certificate-transparency
  timestamp. A file's attestation: `cosign verify-blob-attestation --bundle <file>.quader.sigstore.json` with the same trust and identity
  flags and `--type`, and the file (or `--digest` with `--digestAlg`), offline. What the predicate must say (`malware.state` `clean`, an
  upload from a protected ref) is the consumer's policy: cosign's `--policy` (CUE or Rego) or a cluster's policy engine.
- **Policy engines:** Kyverno's image validating policy documents a certificate chain with `insecureIgnoreTlog` and `insecureIgnoreSCT`
  (its documentation, read 2026-11-21, not run against Quader); Sigstore's policy-controller in its bundle mode needs a timestamp
  authority or a log and cannot verify Quader's attestations (its validator's source, read 2026-11-21;
  [H-1186](#h-1186--a-policy-engine-that-demands-a-log-or-a-timestamp-cannot-verify-quaders-attestations)).
- **Not used:** a Notary Project signature carries no predicate (its payload is a descriptor only, the Notary Project's signature
  specification); the older cosign layout (an `.att` tag, the chain in annotations) is verified only through flags cosign has deprecated
  and removed on its main branch.
- **Size:** about 2.5 KiB a bundle: the statement about 1 KiB in base64, the leaf about 1.1 KiB, the signature 96 bytes (component sizes
  from Sigstore's conformance examples, the total an estimate).

Acceptance criteria:

- Unit (`internal/signing/bundle`, milliseconds, a CA of the test): a bundle of the signer verifies with sigstore-go's verifier against the
  trusted root Quader builds (`verify.WithCurrentTime`, no log, no timestamp), and does not with a flipped byte of the statement, under a
  trusted root of another CA, or at a time outside the leaf's validity; the trusted root holds one authority per distinct chain.
- End to end (the set `e2e (oci)`, one case, about 4 s, an image of a few hundred bytes pushed through the registry API): `cosign
  verify-attestation` with the command above exits 0 for the image in the set's local repository, and non-zero with
  `--certificate-identity https://other.example.com`. The cosign binary joins the OCI clients image, pinned once; what it adds to the image
  and to the leg's load time is not measured, and the package that builds it writes the measured numbers here.

### R-SIG-053 · When an attestation is made, made again and removed
Priority: **P1** · Covers: quader-only

[Q-SIG-64](#q-sig-64--an-attestation-is-made-after-the-first-verdicts-and-made-again-on-a-change-of-malware-state-or-identity).

- **Made** once the item's checks have their first verdict: the malware scan where it applies (a state other than `pending`, R-MAL-002),
  the image scanning or the version's evaluation where it applies (not `pending_scan` or `pending_data`, R-VULN-001), and stage 1's checks
  of the signatures attached at the commit (none `pending`); or after `signing.attestations.wait_for_checks` (default 1 h) with what there
  is, a `pending` state signed as such.
- **Made again**, replacing the item's attestation in its repository: when the malware state changes (`clean` to `infected`, a release of
  R-MAL-021), when the identity changes (R-SIG-048), and when the identity that made it is retired. Not when a rescan keeps the state, not
  when advisories change the vulnerability state, not when anchors change (no trust state is in it).
- **One attestation per item and repository:** a replaced one is removed in the transaction that stores its successor; for OCI the new
  referrer is stored first and the old one removed after.
- **Removed with the item:** a delete, an unpublish, the eviction of a remote's cache; the trash keeps it while it keeps the item, and a
  restore from the trash brings it back. Quader attests no deletion.
- **Off:** `signing.attestations.enabled: false` makes no attestation (npm signing goes on); `signing.attestations.fills: false` attests
  uploads only, for an installation whose remotes cache more than it wants to sign.

Acceptance criteria:

- Integration (area `signing`, `TestR_SIG_053_…`, about 4 s, files of a kilobyte, the malware stub of the stack): a generic file's
  attestation waits for the verdict and carries `clean`; when the stub's rescan flags the file, it is made again with `infected`; a second
  rescan with the same verdict changes nothing; deleting the file removes the attestation; in a repository without the scanning it is made
  at once with `not_scanned`.

### R-SIG-054 · Where attestations are kept and served
Priority: **P1** · Covers: quader-only

[Q-SIG-65](#q-sig-65--attestations-live-in-the-store-of-stage-1-oci-as-a-referrer-every-other-format-through-the-api).

- **The store.** Quader's npm signature and its attestation are signatures in the store of R-SIG-003: the roles `registry` and
  `attestation`, the schemes `npm-registry` and `sigstore-bundle`, the predicate type of R-SIG-051, with `identity_id` set; attached with
  the new source `issued` in their repository, `item_id` the signed or attested item, so they go with it (R-SIG-004). An attestation's bundle
  is `signature_objects.raw` (about 2.5 KiB, within the bound of 256 KiB), except for OCI, where it is the referrer's layer blob
  (`item_blob`). The job `signature-verify` does not check them and the trust job writes no row for them: Quader checks no signature it made
  itself, as for Debian's indexes (Q-SIG-31).
- **Schema changes of stage 1's tables:** `signature_attachments.source` gains `issued`; `signatures` gains `identity_id bigint NULL
  REFERENCES signing_identities (id)` with the index `signatures_issued ON signatures (identity_id) WHERE identity_id IS NOT NULL`.
- **OCI.** The job stores, through the item store as system work, a manifest `application/vnd.oci.image.manifest.v1+json` with
  `artifactType` `application/vnd.dev.sigstore.bundle.v0.3+json`, the empty config `application/vnd.oci.empty.v1+json`, one layer of the
  bundle's media type, `subject` the attested manifest, and the annotations `dev.sigstore.bundle.content: dsse-envelope`,
  `dev.sigstore.bundle.predicateType: <type>` and `org.opencontainers.image.created`: the form cosign 3 writes and reads (cosign keeps a
  referrer whose single layer's media type starts with `application/vnd.dev.sigstore.bundle`; read 2026-11-21). The writer inserts its rows
  before the commit hook runs, so stage 1's detection finds them and adds nothing. It is a referrer without a version (R-SIG-024), in no
  list and no count, and follows its image (R-SIG-025). A deleted attestation referrer is made again by discovery: it is Quader's
  statement, not the user's.
  - **Only where it hides nothing**
    ([Q-SIG-66](#q-sig-66--quaders-referrer-never-stands-where-cosign-would-stop-reading-an-older-signature)). cosign 3.1.4 reads the
    older tags `sha256-<hex>.sig` and `.att` only when it finds no bundle among the referrers, counted before verification and before
    `--type`, so any bundle referrer makes a publisher's older signature unverifiable for stock cosign; and go-containerregistry falls
    back to the referrers tag only when the referrers API answers 404, 400, 406 or a type that is no index (both read in their source on
    2026-11-21). Quader's referrer is therefore written only where the repository's referrers answer for the digest is a real answer of
    the API (a local repository; a remote whose upstream answers the API) and no `.sig` or `.att` tag stands for the digest in the
    repository or, for a remote, upstream as the background fetch found it (R-SIG-026). When such a tag arrives later (a push, a fill, a
    fetch), the job removes Quader's referrer. Such an image's attestation is served through the API alone, and its page says why
    ([H-1185](#h-1185--an-image-with-an-older-cosign-signature-gets-quaders-attestation-only-through-the-api)). Notation is not affected:
    it asks only for referrers of its own artifact type (notation-go's `ListSignatures`, read 2026-11-21).
  - **local:** the referrers API lists it like any referrer (R-OCI-016).
  - **remote:** it is stored in the remote's cache under the image's name, and the referrers answer is the upstream's with Quader's
    referrers of the digest appended; an answer that is 404 stays 404 ([formats/oci.md](formats/oci.md) R-OCI-024), and the attestation
    is then reached through the API.
  - **virtual:** the union of its members' (R-SIG-027), leaving Quader's referrers out when a member that serves the digest holds a `.sig`
    or `.att` tag for it, so that the union hides no older signature either.
- **Every other format, and an image whose referrer would hide a signature:** the API, not the format's wire. `GET
  /api/v1/projects/{projectKey}/repositories/{repoKey}/attestation?path=<item path>` answers the bundle
  (`application/vnd.dev.sigstore.bundle.v0.3+json`, `Content-Disposition: attachment; filename="<file>.quader.sigstore.json"`), with the
  permission to read the item (R-STOR-016), and the version's and the file's pages offer **Download attestation**. Not a sibling file: it
  would stand in a generic repository's tree, could take a user's name and would be read as a publisher's signature by R-SIG-032.
- **npm:** not through npm's attestations endpoint and not in `dist.attestations` (Q-SIG-60); stage 1's serving of a local version's
  provenance (R-SIG-028) is unchanged.
- **The Bastion** serves an attestation referrer like any manifest (R-SIG-033); the API route is not in its URL space.

Acceptance criteria:

- Integration (area `signing`, `TestR_SIG_054_…`, about 4 s, bytes): an image pushed to `docker-local` lists Quader's referrer in its
  referrers answer with the artifact type and the annotations above, not in `tags/list` and not as a version, and through `docker-all` in
  the union; after a `.sig` tag of the image is pushed, Quader's referrer is gone and the API serves the bundle; through `docker-hub` (the
  OCI stub with the referrers API) the answer holds the stub's referrer and Quader's, and against the stub without the API it stays 404
  with no referrer of Quader's; a generic file's attestation answers the bundle with its file name; a local npm version's document carries
  no `dist.attestations` of Quader's.

### R-SIG-055 · The API of stage 2
Priority: **P1** · Covers: quader-only

In `api/openapi.yaml` under the tag `signatures`:

| Method | Path | What | Permission |
|---|---|---|---|
| GET | `/api/v1/signing/identity` | `state` (`none`, `configuration`, `upload`), the current, announced, pending and previous identities, each with `id`, `source`, `state`, `subject`, `issuer`, `names`, `notBefore`, `notAfter`, `fingerprint` (the certificate's SHA-256), `npmKeyId` and its times; the pending one's `csr`; `refused` (the last refusal of the files with its reason and time); `backlog` per kind | `settings:read` |
| POST | `/api/v1/signing/identity/key` | creates a pending key: `{"subject": {"CN", "O", "OU", "L", "ST", "C"}, "uris": [], "emails": []}`; answers the identity with its `csr`; `409 signing_identity_managed` while the Helm values name one, `409 signing_identity_pending` while one is pending | `security-settings:write` and a recent authentication |
| PUT | `/api/v1/signing/identity/certificate` | `{"chain": "<PEM>", "ca": "<PEM>"}` for the pending key; `422 signing_key_mismatch`, `422 invalid_value` naming the check of R-SIG-045 | as above |
| POST | `/api/v1/signing/identity/import` | `{"key", "passphrase", "chain", "ca"}` | as above |
| DELETE | `/api/v1/signing/identity/pending` | discards the pending key | as above |
| DELETE | `/api/v1/signing/identity` | removes the current uploaded identity (R-SIG-048); `409 signing_identity_managed` for one of the Helm values | as above |
| POST | `/api/v1/signing/identities/{id}/retire` | retires a previous identity | as above |
| GET | `/api/v1/signing/trusted-root.json`, `/api/v1/signing/chain.pem` | public material (R-SIG-052) | none: anonymous (Q-SIG-72) |
| GET | `/api/v1/projects/{projectKey}/repositories/{repoKey}/attestation?path=` | the bundle of the item's attestation; 404 when it has none | as reading the item |

- The signatures answers of R-SIG-034 list Quader's signatures with `source: "issued"`, `issuedBy` (`identity`, `subject`, `fingerprint`)
  and, for an attestation, the decoded predicate in `attestation` and `servedAs` (`referrer` or `api`).
- The client setup (R-API-030) gains the tab `verify` while an identity is current ([Q-SIG-71](#q-sig-71--the-client-setup-gains-a-tab-verify-while-an-identity-is-current)):
  `npm.verify.signatures` (`npm audit signatures`), and for every client of every format but `clamav` `<client>.verify.attestation` (the
  download of the trusted root and the cosign command of R-SIG-052, with the identity's first URI, the predicate type and, for a file, its
  bundle's download).

Acceptance criteria:

- Integration (area `signing`, `TestR_SIG_055_…`, about 2 s): the operations for an administrator, a `security-admin`, an auditor (reads,
  403 on writes) and an anonymous caller (the trusted root and the chain, 401 on the identity); the client setup of `npm-local` holds the
  tab `verify` while an identity is current and not without one.

### R-SIG-056 · The screens of stage 2
Priority: **P1** · Covers: quader-only

Simple by default ([CLAUDE.md](../../CLAUDE.md#product-simple-by-default-details-on-demand)): nothing of signing is in a creation flow.

- **Admin area › Settings › Signatures** (R-SIG-036) gains a first section **Signing identity**, open
  ([Q-SIG-70](#q-sig-70--the-signing-identity-is-a-section-of-the-signatures-settings-page)):
  - **None:** "No signing identity: Quader signs nothing. Packages of local npm repositories carry no registry signature, and no artifact
    gets a receipt attestation." For the holder of `security-settings:write`: **Create signing key**, a form whose fields hold the defaults
    (common name `Quader signing`, organization, country, the URI of the base URL; organizational unit, locality, state and an e-mail
    address behind **More options**); then the request with **Copy** and **Download**, the sentence "Have your company's CA issue a
    code-signing certificate for this request, then upload it with its chain.", and **Upload certificate**. **Import key and certificate**
    behind the section's **More options**.
  - **From the Helm values:** "From the Helm values" and the facts, no controls; a refused pair of the files as a line in red with its
    reason.
  - **Uploaded:** the facts, **Replace** (a new key request; the current identity signs until the new one is current) and **Remove**.
  - **The facts:** subject, issuer, alternative names, valid from and until ("expires in 12 days" amber from 14 days, "expired" red), the
    certificate's fingerprint, the npm key ID, the state; **Download chain** and **Download trusted root**.
  - **Previous identities**, closed: each with its dates and **Retire**, which asks "Retire this identity? Its key leaves npm's keys at
    once; Quader signs its packages and attestations again with the current identity."
- **At a version and a file** (R-SIG-035): the details list Quader's npm signature as **Origin confirmed by registry · this installation**
  with the key ID, and under **Attestations** **Receipt attestation by this installation** with its issue time, the malware state it names,
  "served as a referrer" or "served through the API" (with the reason of R-SIG-054 for an image), and **Download attestation**. Neither
  carries the badge (Q-SIG-1).
- **The client setup dialog** shows the tab **Verify** while an identity is current.
- The screens are rendered and looked at before they land (rule 11 of the plan): both schemes, wide and narrow; the section without an
  identity, from the Helm values, uploaded with two previous identities and one that expires in 12 days, with a key request shown and with
  a refused pair; a version with Quader's signature and attestation beside a publisher's provenance; an image whose attestation is served
  through the API.

Acceptance criteria:

- Web unit (Vitest, under a second): the section in each state; controls only for the holder of `security-settings:write`; the amber and
  red validity; the tab **Verify** hidden without an identity.

### R-SIG-057 · Audit events, metrics and alerts of stage 2
Priority: **P1** · Covers: quader-only

**Audit events** ([operations-and-observability.md](operations-and-observability.md) R-OPS-016):

| Event | Mode | Journal | Recorded when | Key fields in `details` |
|---|---|---|---|---|
| `signing.identity.key_created` | T | — | a pending key was made | `id`, `subject`, `names`, `keySha256` |
| `signing.identity.installed` | T | — | an identity was announced or became current from an upload, an import or the files | `id`, `source`, `state`, `subject`, `issuer`, `notAfter`, `fingerprint`, `npmKeyId` |
| `signing.identity.switched` | T | — | an announced identity became current | `id`, `previous` |
| `signing.identity.removed` | T | yes | the current identity was removed | `id`, `fingerprint` |
| `signing.identity.retired` | T | yes | a previous identity was retired | `id`, `fingerprint`, `signatures` (their count) |
| `signing.identity.refused` | A | — | a pair of the files failed R-SIG-045, once per content | `reason`, `fingerprint` |
| `signing.failed` | A | — | work reached its last attempt, at most once an hour per kind and code | `kind`, `code`, `count` |

The actor of the job's and the files' events is `system:signing`. No event carries key material.

**Metrics** ([operations-and-observability.md](operations-and-observability.md) R-OPS-008):

| Metric | Type | Labels | Where |
|---|---|---|---|
| `quader_signing_identity_not_after_seconds` | gauge | `state` (`current`, `announced`) | worker: the Unix time of the certificate's end |
| `quader_signing_identity_loaded` | gauge | — | worker: 1 when this worker holds the current identity's key |
| `quader_signing_operations_total` | counter | `kind` (`npm`, `attestation`), `result` (`signed`, `failed`) | worker |
| `quader_signing_backlog` | gauge | `kind`, `priority` | worker |
| `quader_signing_oldest_due_seconds` | gauge | `kind` | worker |

**Alerts** (the chart's rules, [ha-and-helm.md](ha-and-helm.md) R-HA-045):

| Alert | Expression | For | Severity |
|---|---|---|---|
| `QuaderSigningIdentityExpiring` | `min(quader_signing_identity_not_after_seconds{state="current"}) - time() < 14 * 86400 unless on() count(quader_signing_identity_not_after_seconds{state="announced"}) > 0` | 1h | warning |
| `QuaderSigningIdentityExpired` | `min(quader_signing_identity_not_after_seconds{state="current"}) - time() < 0` | 0m | critical |
| `QuaderSigningIdentityNotLoaded` | `max(quader_signing_identity_loaded) == 0 and on() count(quader_signing_identity_not_after_seconds{state="current"}) > 0` | 15m | warning |
| `QuaderSigningBacklog` | `max(quader_signing_oldest_due_seconds{kind="npm"}) > 300` | 15m | warning |

Acceptance criteria:

- Unit (`internal/audit`, under a second): the events are in the registry with their schemas and journal flags.
- Chart test (`test/chart`, about 1 s): the four alerts render and `promtool` checks them.

### R-SIG-058 · Configuration, defaults and what stage 2 costs
Priority: **P1** · Covers: quader-only

```yaml
signing:
  enabled: true                     # default; false: Quader signs nothing, also with an identity; what is stored stays and is served
  identity:
    cert_file: ""                   # default; the chart sets /var/run/secrets/quader/signing/tls.crt (R-HA-071)
    key_file: ""                    # default; the chart sets /var/run/secrets/quader/signing/tls.key
    ca_file: ""                     # default; the chart sets /var/run/secrets/quader/signing/ca.crt
    reload_interval: 1m             # default
    announce_period: 10m            # default; 0s: a new identity is current at once
  attestations:
    enabled: true                   # default; false: no receipt attestation; npm signing goes on
    fills: true                     # default; false: only uploads are attested, not a remote's fills
    wait_for_checks: 1h             # default
  concurrency: 4                    # default; operations one job has in flight
  max_attempts: 5                   # default
jobs:
  workers:
    signing: 1                      # default
```

- **Validation** at start, with the key named: `cert_file` and `key_file` both or neither, `ca_file` only with them; `reload_interval`
  from 10 s to 1 h; `announce_period` from 0 s to 24 h; `wait_for_checks` from 0 s to 7 days; `concurrency` from 1 to 64; `max_attempts`
  from 1 to 20.
- **The chart's values** are R-HA-071's (`signing.identity.existingSecret`, `signing.identity.certManager`).
- **What it costs:** no Deployment, image or module more (sigstore-go and `golang.org/x/crypto` are in the build); in PostgreSQL about 150
  bytes and a row per npm signature and about 2.5 KiB and a row per attestation, about 3 GB for a million attested artifacts (an
  estimate), with as much written again per rotation (R-SIG-048); for OCI two small objects per attested image in the object store; tens of
  microseconds of the worker's CPU per signature; a revision per npm package and signing run.

Acceptance criteria: unit (`internal/config`, under a second): the defaults; each refusal of the validation, with the key named.

### R-SIG-059 · Backups, restores and replicas of stage 2
Priority: **P1** · Covers: quader-only

- **Backups** ([backup-and-restore.md](backup-and-restore.md) R-BAK-004): `signing_identities` and `signing_work` are `data`. An uploaded
  key travels sealed, so a restore needs the key ring, which the restore Job checks (R-BAK-020); the key of the Helm values is in no
  backup, it is in its Secret.
- **Restores:** the journal re-applies a removal and a retirement made after the restore point, so a withdrawn identity does not come back
  ([authorization.md](authorization.md) R-AUTHZ-031's rule). An identity installed after the restore point is lost with its rows; a pair of
  the files is registered again by the workers at their next look; discovery signs and attests what the restored rows lack.
- **Into another installation** ([Q-SIG-74](#q-sig-74--a-restore-keeps-the-uploaded-identity-also-in-another-installation)): the restored
  database keeps the installation's ID ([backup-and-restore.md](backup-and-restore.md) R-BAK-022) and the sealed uploaded identity, so a
  restore with the same key ring signs as the installation it came from: the case of a recovery. A copy restored for a test signs as
  production until an administrator removes the identity
  ([H-1191](../security/artifact-signatures.md#h-1191--a-restore-carries-the-uploaded-identity-into-whichever-installation-restores-it)).
- **Replicas:** every replica reads the identities from a snapshot that the change area `signing_identities` refreshes within R-ARCH-018's
  bound, so every replica answers the same keys after that bound; the switch's lock is the one coordination (R-SIG-048, R-SIG-049).

Acceptance criteria: integration (area `signing`, `TestR_SIG_059_…`, about 3 s): a backup with an uploaded identity, restored into a fresh
installation with the same key ring, keeps signing with it; a retirement recorded after the backup is re-applied by the journal.

### R-SIG-060 · The tests of stage 2 and what they cost
Priority: **P1** · Covers: quader-only

Each behaviour once, at the cheapest tier that shows it ("Tests stay runnable" in [CLAUDE.md](../../CLAUDE.md)). The tests make the
installation's identity in process: a root, an intermediate and a code-signing leaf for a P-256 key with `crypto/x509`, as R-SIG-042 makes
the company CA.

| Tier | What | Run time | Data | Where |
|---|---|---|---|---|
| Unit | the identity's checks and states, the keyid, the npm signature against pacote's check, the statement, the bundle against sigstore-go's verifier and the trusted root, the switch's lock, the job's order, the configuration, the events | under 3 s together (an estimate) | bytes, built by the tests | `internal/signing/…`, `internal/config`, `internal/audit`, `internal/archtest` |
| Format contract kit | check 18's attestation subjects for every format | milliseconds per format | the fixtures' cases | `internal/formats/formattest` |
| Integration | the flows of R-SIG-046 to R-SIG-059 against the stack | about 30 s for the new area `signing` (nine tests of 2 to 4 s; estimates) | bytes to a kilobyte | `test/integration/signing` |
| Chart | the Secret only in the worker pods, the rendered Certificate, the four alerts | about 1 s | rendered YAML | `test/chart` |
| End to end | `npm audit signatures` in `e2e (npm)`; `cosign verify-attestation` in `e2e (oci)` | about 6 s and about 4 s, no new leg | a package and an image of a few hundred bytes | `test/e2e` |

What every leg gains: the unit run under 3 s; the kit milliseconds per format; the integration shard that runs `signing` about 30 s; the
chart leg about 1 s; `e2e (npm)` about 6 s; `e2e (oci)` about 4 s and the load of the cosign binary in its clients image (not measured).
No other leg gains anything. With 25 formats: 25 kit cases of milliseconds more, nothing else; no leg loads another format's client.

What only a real client shows and is not tested: `cosign verify-blob-attestation` against a file's bundle (the same verifier library as
the unit test), Kyverno's and other policy engines' verification in a cluster. Each is measured once by hand when the package that builds
it lands, and the result written into this requirement.

## The gate per repository (stage 3)

Status: specified, not built. The owner decided it on 2026-11-21 in his working paper, against the development agent's recommendation of no
gate for now ([Q-SIG-9](#q-sig-9--display-or-gate), option (b), in the paper's words translated: besides display and alarm, per repository
a detail setting "only artifacts with a verified signature", optionally of chosen anchors); with his choice of three stages
([Q-SIG-14](#q-sig-14--the-order-of-the-work)) it is the third. Before he left on 2026-11-21 he was told that the second and the third
stage are specified and built after the first without asking him, their open points decided by the development agent and documented (the
plan's standing rules); every decision below that is not his is the development agent's
([ADR-231](../decisions.md#adr-231--a-repository-can-serve-only-artifacts-with-a-verified-signature-a-gate-at-download-for-every-format-judged-from-stored-verdicts-and-failing-closed)).

**What the gate keeps out, and what it does not.** A repository whose gate is on serves an artifact only when a signature of its publisher
or its build is attached where the repository sees it, matches the content, and is known to a trust anchor the repository accepts. That keeps
out what no signer the repository trusts has signed: an upload of an account that was taken over but holds no signing key, content an
upstream changed or never signed, a package of a public registry that takes the name of a private one. It does not keep out what a trusted
signer signed ([H-1171](../security/artifact-signatures.md#h-1171--a-verified-signature-says-who-signed-not-that-the-content-is-safe)), a
signature made with a stolen key until an administrator blocks the signer
([H-1168](../security/artifact-signatures.md#h-1168--a-revoked-certificate-stays-verified-until-an-administrator-blocks-its-signer)), and,
while the repository accepts any anchor, a signature that any identity of the public Sigstore made
([H-1194](../security/artifact-signatures.md#h-1194--a-gate-that-accepts-any-anchor-passes-what-any-identity-of-the-public-sigstore-signed)).

**The build plan**, three packages, each landing after its own gates (rule 10 of the plan):

| Package | Builds | Waits for | Its tests |
|---|---|---|---|
| 1 · The gate | the member and its order (R-SIG-061); the decision (R-SIG-062); the contract's `Gate`, every format's classification, `oci_manifests.attachment`, kit check 18's and check 17's cases (R-SIG-063, R-SIG-068); the serve points and OCI's handlers (R-SIG-064); the view and the scope (R-SIG-065); the subtraction and its two columns (R-SIG-066); the reading of stage 1's fetch state, the priority work, the wait and the held fill (R-SIG-067); one decision, one wait (R-SIG-069); the event of a refusal, the table of refusals, the metrics, the alert, the configuration (R-SIG-073, R-SIG-075, R-SIG-076) | stage 1's packages 1 (on `main`), 2 (verification and trust: the trust rows, the anchors and their snapshot, the job and its priorities) and 3 (the background fetch, which writes and works `signature_fetches`); not stage 2: the gate leaves out a signature with `identity_id` set once that column exists | unit under 2 s, the kit milliseconds per format, integration about 14 s (R-SIG-061, R-SIG-064 to R-SIG-067) |
| 2 · Releases, previews and the API | `signature_releases` and its operations (R-SIG-070); the impact and its index (R-SIG-071); the preview (R-SIG-072); the refusals' operation and the members `refused` and `gate` (R-SIG-073); the events of a release and the Bastion's feed (R-SIG-075); backups and replicas (R-SIG-076) | package 1 | integration about 8 s (R-SIG-070 to R-SIG-073) |
| 3 · The screens | R-SIG-074 | package 2, and stage 1's package 4 (the screens of the signatures) | web unit under a second; the renders of R-SIG-074 |

### R-SIG-061 · The setting: only artifacts with a verified signature, optionally of chosen anchors
Priority: **P1** · Covers: BEYOND-019

The owner's decision ([Q-SIG-9](#q-sig-9--display-or-gate)): a detail setting per repository. The repository alone sets it
([Q-SIG-75](#q-sig-75--the-gate-is-a-setting-of-the-repository-alone)).

- **The member** `signatures` of the repository document ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-004, R-SIG-040)
  gains two members:

  ```yaml
  signatures:
    fetch: true          # default; remote repositories only (R-SIG-026)
    onlyVerified: false  # default; true: only artifacts with a verified signature are served from this repository
    anchors: []          # default; empty: any trust anchor of the repository's scope; else the anchors it accepts, as "<scope>/<key>"
  ```

  With chosen anchors, for example *(example)*: `{"signatures": {"onlyVerified": true, "anchors": ["configuration/gitlab-ci-signing",
  "installation/release-key"]}}`.
- **Where it applies:** every class (local, remote, virtual) of every format but Debian, whose packages carry no signature of their publisher
  that apt or Quader knows ([Q-SIG-80](#q-sig-80--debian-the-gate-is-not-offered)): on a Debian repository `onlyVerified` and `anchors`
  answer 422 `not_applicable`. A system repository of `upstream` (`scanner-trivy`, `clamav`) keeps the defaults (422 `managed_by_product`);
  a well-known repository of `upstream` takes the setting from an administrator, as its other narrowing settings
  ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-069). `fetch` stays `not_applicable` on local and virtual repositories.
- **`anchors`** names anchors by scope and key ([Q-SIG-77](#q-sig-77--chosen-anchors-are-named-by-scope-and-key-and-the-anchors-own-restrictions-narrow)):
  `shipped/<key>`, `configuration/<key>`, `installation/<key>`, and `project/<key>` for an anchor of the repository's own project. On a
  write each reference must name an anchor that exists in the repository's scope (one of another project, or one that does not exist,
  answers 422 `invalid_value` at its index); at most 32 references, none twice. `anchors` while `onlyVerified` is false is stored and does
  nothing. An anchor removed later leaves its reference standing: it matches nothing, so what only that anchor knew is refused (fail
  closed), and the settings page shows "Removed trust anchor" in its place. A switched-off anchor, and a project anchor while the
  administrator's switch is off (R-SIG-019), match nothing while they are off.
- **Any anchor** (`anchors: []`) means every anchor of the repository's scope: the shipped ones that are switched on, those of the Helm
  values, the administrator's and, while project anchors are allowed, the project's own
  ([Q-SIG-76](#q-sig-76--verified-is-the-badges-verified-states-and-any-anchor-includes-the-shipped-public-ones),
  [Q-SIG-93](#q-sig-93--a-project-anchor-counts-for-the-gate-of-its-projects-repositories)).
- **Who** (the rule of the malware setting, [malware-scanning.md](malware-scanning.md) Q-MAL-31): every writer of the repository's settings
  may narrow the gate, an Owner of the project included; a weakening needs `repositories:write-config` or `vulnerabilities:triage`, and an
  Owner gets 403 `signature_weakening_refused`. The order:

  | Change | Is |
  |---|---|
  | `onlyVerified` from false to true | a narrowing |
  | `onlyVerified` from true to false | a weakening |
  | `anchors` from a list to empty (any anchor) | a weakening |
  | `anchors` from empty to a list | a narrowing |
  | a reference added to a list | a weakening |
  | a reference removed from a list that stays non-empty | a narrowing |

  A change that does both (one reference swapped for another) is a weakening. A create is compared with the defaults, so a create with the
  gate on narrows. A weakening records `repository.updated` with `block: lifted` and `signatures` in `weakened`
  ([vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-002's form); a narrowing records `repository.narrowed` with the part
  `signatures`, the member before and after, journaled ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-061).
- **The replay** of a journaled `signatures` part sets `onlyVerified` to true and `anchors` to the narrower of the restored and the
  journaled value: a list before any anchor, and of two lists the references in both; two lists without a common reference would mean any
  anchor, which widens, so the repository is disabled instead (R-REPO-061's rule for a part that cannot be applied narrowing).
- **Not in a creation flow** (CLAUDE.md, "simple by default, details on demand"): the setting is in the closed section **Signatures** of the
  repository's settings page (R-SIG-074) and in no step of the creation form, its **Advanced settings** included; the API takes it at a
  create.
- **No installation switch, no project default** ([Q-SIG-75](#q-sig-75--the-gate-is-a-setting-of-the-repository-alone)), and **no report
  mode** ([Q-SIG-91](#q-sig-91--no-report-mode-the-preview-shows-what-the-gate-would-refuse)): the preview of R-SIG-072 shows what the gate
  would refuse before it is switched on.

Acceptance criteria:

- Unit (`internal/signatures/gate`, milliseconds): the table as a function of the stored and the new member; the replay's narrowing of two
  lists, and the disable for two lists without a common reference.
- Integration (area `signatures`, `TestR_SIG_061_…`, about 2 s): the Owner of `web` switches the gate on for `files-internal` (200,
  `repository.narrowed` with `signatures`), adds a reference to the list (403 `signature_weakening_refused`), an administrator adds it (200,
  `block: lifted`); a reference to a project anchor of another project answers 422 `invalid_value`; on `debian` the member answers 422
  `not_applicable`.

### R-SIG-062 · What passes and what is refused, state by state
Priority: **P1** · Covers: BEYOND-019

- **What counts** ([Q-SIG-78](#q-sig-78--only-a-publishers-signature-or-a-provenance-passes)): the signatures of the roles `publisher` and
  `provenance`, the ones that carry the badge (R-SIG-009, the owner's decision Q-SIG-1). A `registry` signature alone (npm's registry
  signature, nuget.org's countersignature, a Galaxy signing service's, a Debian release's) does not pass, and an `attestation` never does.
  Quader's own npm signature and receipt attestation (stage 2, `identity_id` set) never pass, whatever their role or the anchor their chain
  ends in: Quader signs everything it receives, so its own statement would pass everything.
- **The decision of one gate**, over the counting signatures in its view and scope (R-SIG-065), each with its integrity state (R-SIG-003)
  and its trust row as R-SIG-066 narrows it:

  | A counting signature is | With any anchor | With chosen anchors | The reason when nothing passes |
  |---|---|---|---|
  | trust `internal` (**Verified · internal**) | passes | passes when an anchor of its row is chosen | `not_chosen` |
  | trust `public` (**Verified · public**) | passes | passes when an anchor of its row is chosen | `not_chosen` |
  | trust `untrusted`, or integrity `no_key` (**Signed · signer not trusted**) | does not pass | does not pass | `untrusted` |
  | trust `blocked` (**Signed · signer not trusted**, blocked) | does not pass | does not pass | `blocked` |
  | integrity `invalid` (**Invalid signature**) | does not pass | does not pass | `invalid` |
  | integrity `unsupported`, `malformed` (**Scheme not supported**), or `failed` | does not pass | does not pass | `not_checked` |
  | integrity `pending`, or `valid` without a trust row of the scope yet (**Not checked yet**) | pending | pending | — (R-SIG-067) |
  | none at all (**No signature**) | — | — | `missing`; pending while a fetch of the remote is due (R-SIG-067) |

- **One passing signature is enough**, as one decides the badge
  ([Q-SIG-25](#q-sig-25--an-artifacts-state-is-the-best-of-its-signatures-and-an-invalid-one-alone-shows-invalid)): an invalid or a
  blocked signature beside a passing one changes nothing. A release (R-SIG-070) passes the artifact in its repository whatever its signatures
  say.
- **Nothing passes and something is pending:** the request waits (R-SIG-067). **Nothing passes and nothing is pending:** the artifact is
  refused with the first reason in this order that one of its counting signatures gives, else `missing`: `blocked`, `invalid`, `not_chosen`,
  `untrusted`, `not_checked`.
- The gate does not tell public from internal by itself; chosen anchors do: a repository that accepts only the company's own signers chooses
  the company's anchors.

Acceptance criteria:

- Unit (`internal/signatures/gate`, milliseconds): the table as signature sets to a decision and a reason, with any and with chosen anchors:
  empty; one pending; one valid without a trust row; one internal; one public whose anchor is not chosen; one untrusted and one invalid; one
  blocked beside one internal; one registry signature only; one of Quader's own attestations whose chain ends in an anchor; a release.

### R-SIG-063 · What the gate judges: the format names it
Priority: **P1** · Covers: quader-only

The gate judges artifacts, never what carries their signatures; which stored item is which is the format's knowledge
([Q-SIG-81](#q-sig-81--the-format-names-what-the-gate-judges-and-the-contract-rises-by-one)). The adapter of R-SIG-022 gains a method, and
`ContractVersion` rises by one in the change that adds it (to 10 after stage 2's 9); the block is written into
[architecture.md](architecture.md) R-ARCH-008 by that change:

```go
// Gate names, for each stored item about to be served or previewed, what the signature gate judges (R-SIG-063).
// It may read the format's own data port; it never reads the signature tables. Base answers GateNone for every item.
Gate(ctx context.Context, h Host, repo RepoRef, items []GateItem) ([]GateSubject, error)

type GateItem struct {
    Path string
    Blob [32]byte // the item's SHA-256
}

type GateSubject struct {
    Kind     GateKind   // GateArtifact, GateAttachment, GateSignatureFile, GateNone
    Covering [][32]byte // GateArtifact: artifacts whose passing signature or release passes this item too
}
```

| Kind | Meaning | What the core does |
|---|---|---|
| `GateArtifact` | an artifact a client fetches | judges the item's blob and each `Covering` digest (R-SIG-062); the item passes when one of them passes |
| `GateAttachment` | a carrier of signatures, attestations or SBOMs that the format recognised by its content | passes it: a client must reach the signatures of an artifact to check them |
| `GateSignatureFile` | a file named like a signature of the file beside it | passes it while it is the object of a signature whose scheme was read (`signature_objects.scheme` other than `unknown`, within `max_object_size`, R-SIG-032); else judges it as an artifact |
| `GateNone` | no artifact of its own: a layer or a config blob of an image | passes it |

An item Quader generated (`items.generated`) is never asked about and passes, as at the malware gate (R-MAL-013).

| Format | `GateArtifact` (`Covering`) | `GateAttachment` | `GateSignatureFile` | `GateNone` |
|---|---|---|---|---|
| OCI | a manifest or an index that is no attachment (each index of the name in the repository that lists it, R-OCI-007's references) | a manifest R-SIG-023 recognises as a signature, an attestation or an SBOM, with no image layer; an index whose every descriptor carries such an `artifactType` (a referrers tag's index) | — | layers, configs, every other blob |
| npm | a tarball | — | the provenance bundle `<name>/-/<basename>-<version>.sigstore` | — |
| NuGet | a `.nupkg`; a `.snupkg` (the `.nupkg` of the same id and version in the repository) | — | — | — |
| Debian | — (the setting is not applicable, R-SIG-061) | — | — | every item |
| Ansible Galaxy | a collection archive; a role archive | — | — | — |
| generic | every file | — | `<file>.asc`, `.sig`, `.sigstore.json`, `.jws.sig`, `.cose.sig` beside `<file>` | — |
| `clamav` (a system format) | — | — | — | every item |

- **OCI recognises an attachment by its content, never by a tag's name**
  ([Q-SIG-82](#q-sig-82--oci-the-gate-is-on-manifests-an-index-covers-its-children-and-an-attachment-is-known-by-its-content)): a tag
  `sha256-<hex>` or `sha256-<hex>.sig` that holds a runnable image is an artifact. The format records the recognition at the commit, in
  `oci_manifests.attachment boolean NOT NULL DEFAULT false` ([formats/oci.md](formats/oci.md) R-OCI-007), by the detector's rules of
  R-SIG-023 (the artifact type, the config's media type, the layers' media types) and only when no layer has an image layer's media type
  (`application/vnd.oci.image.layer.v1.tar` with its compressed and non-distributable forms, `application/vnd.docker.image.rootfs.diff.tar.gzip`,
  `application/vnd.docker.image.rootfs.foreign.diff.tar.gzip`). The row "every other artifact type" of R-SIG-023 is no attachment here: a
  Helm chart pushed with a `subject` is an artifact.
- **An index covers its platform manifests.** A signature over an index commits to the digests of its children, and `cosign sign` of a tag
  signs the digest the tag names, the index of a multi-platform image, and its children only with `--recursive` (cosign's
  `cmd/cosign/cli/sign/sign.go`, read 2026-11-21: the walk skips the children unless `--recursive` is set). A client pulls the index by tag
  and then its platform's manifest by digest; the second request passes on the index's signature.
- **NuGet:** a `.snupkg` has the id and the version of its `.nupkg` ([NuGet's documentation on symbol
  packages](https://learn.microsoft.com/en-us/nuget/create-packages/symbol-packages-snupkg), read 2026-11-21) and passes on the `.nupkg`'s
  signature or its own; whether authors or nuget.org sign symbol packages was not verified (nuget.org admits a `.p7s` in them, the same page).
- **Kit check 18** (R-SIG-022) gains the cases of the table: `Fixture.GateCases` names a stored item per row the format fills, with the kind
  and the covering digests it expects; a format without signatures answers `GateNone` for every content case. The test format's defects
  `GatesAttachment` (an attachment named an artifact) and `PassesArtifact` (an artifact named `GateNone`) fail it.

Acceptance criteria:

- Unit (the format contract's kit, milliseconds per format): check 18's gate cases for every registered format and the test format; the two
  defects fail it.
- Unit (`internal/formats/oci`, milliseconds, manifests built by the test): `attachment` is set for a Notation referrer, a cosign 3 bundle,
  an older `.sig` manifest with an image config, an SPDX referrer and a referrers tag's index; it is not set for an image with a `subject`, a
  Helm chart with a `subject` and an index under the tag `sha256-<hex>` that lists images.

### R-SIG-064 · Where the gate acts
Priority: **P1** · Covers: BEYOND-019

- **At download, for every format** ([Q-SIG-79](#q-sig-79--the-gate-acts-at-download-for-every-format-not-at-upload)): an upload is stored
  whatever it carries, and its version page says that it is refused here until it is signed. OCI's signatures arrive after the push, a
  generic file's and an Ansible version's in a request of their own; only npm's provenance and NuGet's signature come with the upload.
- **The serve points.** Every stored item a format serves passes `itemStore.Serve` of the format host
  ([ingest.go](../../internal/formathost/ingest.go)): a format's routes, a remote repository's hit and cache view, every tier of a virtual
  repository's resolution; `/files/` passes `files.Service.Stat` ([download.go](../../internal/files/download.go)). The gate is one check in
  each, after the authorization and the format's serve-time check (a version the caller may not read stays the format's 404), for `GET`,
  `HEAD`, conditional and `Range` requests alike, with no `304` for a refused item. Unlike the malware gate it is asked whatever the route's
  kind: the format's `Gate` decides what is an artifact, so an OCI manifest by tag (a route of the kind `Metadata`) is judged.
- **A stored document a format serves without the item store** (a remote OCI repository's tag, which is a metadata document, R-OCI-022) is
  judged by the format's handler through the host (`Signatures().Refuse`), where the handler asks the image block of the vulnerability
  scanning today (`refuseBlockedImage` in [handlers_manifest.go](../../internal/formats/oci/handlers_manifest.go),
  [remote_handlers.go](../../internal/formats/oci/remote_handlers.go) and [virtual_read.go](../../internal/formats/oci/virtual_read.go)).
  Each request is judged once.
- **Not asked:** while no gate that applies to the request is on (R-SIG-065), which the replica reads from its snapshot of the repositories'
  settings without a statement, so the download is unchanged; for the reads of system subjects (the image scanner's pull, the malware
  scanner's read, the worker's reads); for an item of a system repository of `upstream`
  ([malware-scanning.md](malware-scanning.md) Q-MAL-100's rule); while `signatures.enabled` is false (R-SIG-040), which switches every gate off
  as `malware.enabled: false` switches off the malware gate
  ([H-1198](../security/artifact-signatures.md#h-1198--switching-the-signature-checks-off-lifts-every-repositorys-gate)); the repository's
  section says so.
- **The cost.** In a repository whose gate applies, the counting signatures of the item's blob and their trust rows come with the item
  lookup, joined into its statement as the malware verdict is ([architecture.md](architecture.md) R-ARCH-027's one statement); the format's
  data for OCI and a covering artifact (an index for a platform manifest, a `.nupkg` for its `.snupkg`) cost one statement more, only where
  the item's own signatures do not pass.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_064_…`, about 3 s, files of a kilobyte, the release key of R-SIG-042 as an anchor of the
  installation): with the gate on `files-internal`, an unsigned file answers 403 through `/files/` (`GET`, `HEAD`, and `If-None-Match` with
  its `ETag`); a file with a valid `.asc` of the release key is served, and its `.asc` too; with the gate off both are served, and a
  download's statements are as many as without stage 3.

### R-SIG-065 · The gate's view and scope, and virtual repositories
Priority: **P1** · Covers: BEYOND-019

([Q-SIG-83](#q-sig-83--a-virtual-repository-its-own-gate-and-the-holding-members-each-in-its-own-scope))

- **Which gates apply:** the addressed repository's and, through a virtual repository, also the one of the member that holds the item. An
  artifact is served when it passes every gate that applies: a gated member stays gated through every virtual repository, and a virtual
  repository's gate holds everything it serves (the malware setting's rule, Q-MAL-32).
- **Each gate judges the artifact as its own repository sees it:** the attachments of its repository (R-SIG-004) and, for a virtual
  repository's own gate, those of its members that the caller may read (the badge's rule, R-SIG-027); the trust rows of its repository's
  project's scope (R-SIG-008), the installation's for a repository of `upstream`; and the anchors of that scope as its `anchors` names them.
  A member's gate is judged in the member's scope wherever the member is read.
- **A refusal and a pending answer end the walk** of every virtual repository of every format: the next member is not asked, as for the
  malware gate and the vulnerability block ([malware-scanning.md](malware-scanning.md) R-MAL-018); the generic engine and the OCI walk return
  the two answers of R-SIG-068 as they return the malware gate's.
- **The remote miss:** an artifact a gated remote does not hold yet is fetched and stored as any (the fill held, R-SIG-067), then judged.
  Nothing is refused before the fetch: only the stored content shows whether a signature matches it.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_065_…`, about 3 s, the company's Sigstore of R-SIG-042 as an identity anchor): with the gate on
  `npm-all`, a tarball of `npm-local` with a provenance bundle of the company's Sigstore is served through `npm-all`; one without is refused
  with a text that names `npm-all`, and `npm-remote` is not asked for the same version; with the gate on `npm-local` alone, its unsigned
  tarball is refused through `npm-all` and through `npm-local`, and a tarball of `npm-remote` is served through `npm-all`.

### R-SIG-066 · A trust row older than a narrowing: the gate subtracts
Priority: **P1** · Covers: quader-only

([Q-SIG-84](#q-sig-84--a-narrowing-reaches-the-gate-at-once-by-subtraction-a-widening-after-the-recomputation)) Trust rows lag a change of
anchors by the recomputation (R-SIG-012,
[H-1175](../security/artifact-signatures.md#h-1175--the-shown-trust-lags-a-change-of-anchors-by-the-recomputation)). A widening that lags
refuses for a while what will pass, which is safe; a narrowing that lagged would serve what no longer passes. A narrowing only takes an anchor
away or adds a block, which needs no cryptography, so the gate applies it itself from the replica's snapshot of anchors and blocks
(R-SIG-041), within the propagation bound of the change area `signature_anchors` ([architecture.md](architecture.md) R-ARCH-018):

- **Two columns join stage 1's tables:** `signature_anchors.narrowed_epoch bigint NOT NULL DEFAULT 0`, set to the raised `anchor_epoch` by a
  write that narrows the anchor (it is switched off; material is removed or replaced; a CA anchor's subject restriction or use is added or
  changed; a Sigstore identity's issuer, pattern or trust root changes); and `signature_blocks.epoch bigint NOT NULL`, the epoch the block's
  creation raised.
- **An anchor of a row's `anchor_ids` counts** only while it is in force in the snapshot (it exists, it is switched on, and a project anchor
  only while the switch is on) and its `narrowed_epoch` is not above the row's `epoch`. A block whose `epoch` is above the row's and that
  matches one of the signature's `key_ids`, or its issuer and identity, makes the row `blocked`. What remains decides the state: no anchor,
  `untrusted`; shipped anchors only, `public`; else `internal`.
- An anchor that a write narrowed counts for no older row, also where it still knows the signer: until the recomputation reaches those rows
  their artifacts are refused (fail closed). The impact of R-SIG-071 says so before the write.
- **A widening waits for the recomputation:** an anchor added or switched on, a block lifted, the switch turned on reach the gate when the
  trust job has written the rows (R-SIG-012).
- The gate's statement reads of each trust row `state`, `anchor_ids` and `epoch`, and of its signature `key_ids`, the issuer and the identity
  (R-SIG-005); nothing else.

Acceptance criteria:

- Unit (`internal/signatures/gate`, milliseconds, a snapshot built by the test): a row whose only anchor was removed reads `untrusted`; one
  with the removed anchor and another reads by the other; a block created after the row makes it `blocked`, one created before it changes
  nothing (the row holds it); an anchor whose material was replaced after the row counts for nothing; an anchor added after the row is not in
  it.
- Integration (area `signatures`, `TestR_SIG_066_…`, about 2 s, the trust job held by the test): with the gate on `files-internal`, a block of
  the release key's fingerprint through replica A refuses its signed file on the next request through replica B, before a trust row changed.

### R-SIG-067 · Pending: the priority work and the bounded wait
Priority: **P1** · Covers: BEYOND-019

([Q-SIG-85](#q-sig-85--pending-priority-work-and-a-bounded-wait-then-503)) The malware gate's pattern
([malware-scanning.md](malware-scanning.md) R-MAL-014): a gated download of an artifact whose signatures are not judged yet puts them first in
line and waits a bounded time.

- **Pending** is a decision of R-SIG-062 where nothing passes and one of these holds: a counting signature of the view has the integrity state
  `pending`; a `valid` one has no trust row of the scope yet; a remote of the view has not completed its fetch of the subject's signatures
  (R-SIG-026) since it stored the item.
- **The state of the fetch** is stage 1's table `signature_fetches` (migration 00096, built with R-SIG-026,
  [Q-SIG-106](#q-sig-106--one-table-of-fetch-state-for-both-stages-keyed-by-target-with-the-subject-beside-it)); the gate reads it and adds
  no table of its own:

  ```sql
  CREATE TABLE signature_fetches (
    repo_id        bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    target         text        COLLATE "C" NOT NULL,  -- OCI "<name>@sha256:<hex>", npm "<name>@<version>" or "keys"
    format         text        NOT NULL,
    subject_sha256 bytea       NULL,                  -- the item whose signatures the target fetches; NULL for npm's keys
    queued_at      timestamptz NULL,                  -- set while a run is due or running
    fetched_at     timestamptz NULL,                  -- the end of the last run that asked the upstream to the end, found or not
    attempts       integer     NOT NULL DEFAULT 0,    -- the runs that failed since the last fetch
    failed_at      timestamptz NULL,                  -- the job's last attempt failed
    PRIMARY KEY (repo_id, target)
  );
  CREATE INDEX signature_fetches_subject ON signature_fetches (repo_id, subject_sha256) WHERE subject_sha256 IS NOT NULL;
  ```

  The format's data port writes a row for what R-SIG-026's table fetches (an OCI top-level manifest that a fill stored, also the fill of a
  mirror pass, whose own fetch of the kept tag stores what it finds but does not write the row; an npm tarball whose version names
  `dist.attestations`) in the transaction that stored it, and the job `signature-fetch` keeps it. The gate reads the rows of the
  remote and the subject's digest (one subject can have rows under several image names): **fetched** when a row has `fetched_at`, also while
  a refresh has queued it again; **due or running** when no row has `fetched_at` and one has `queued_at`; **failed** when no row has either
  and one has `failed_at`, which reads `not_checked` until the next fill or refresh queues it again; **not fetched** when there is no row or
  only a row with none of the three (the remote's `signatures.fetch` was switched off before the run), so its gate judges what clients
  brought: an image whose signatures no client pulled reads `missing`, and the section of R-SIG-074 says so. "Fetched, nothing found" is a
  row with `fetched_at` and no attachment of the subject's signatures. An OCI row goes with its manifest's row (the data port's
  `RemoveManifest`), so a manifest stored again is fetched again; an npm row stays when the cache loses the tarball
  ([H-1201](../security/artifact-signatures.md#h-1201--an-npm-versions-fetch-state-outlives-its-tarball)).
  The retries and their pauses are the job queue's (5 attempts, its backoff), so the table carries no `not_before`.
- **The priority work.** The gate makes the subject's pending signatures due at priority 0 with `not_before` now (R-SIG-011), raises the
  priority of the subject's waiting `signature-fetch` job or queues one at priority 0 (its builder adds the statement that raises a waiting
  coalesced job's priority: an enqueue of a key that waits returns the waiting job unchanged), and wakes the job `signature-verify` and the
  background fetch; the builder also raises `signature_verdict` after a run's commit. An asked fetch counts towards
  `signatures.remote.requests_per_hour`; while that bound is spent it waits like any. Many waiters of one subject share one check and one
  fetch.
- **The wait** ends at `signatures.gate.download_wait` (default 25 s) after the request arrived, the malware gate's bound and its reason
  (inside apt's 30 s, [malware-scanning.md](malware-scanning.md) Q-MAL-29), and is shared with a pending malware scan of the same request
  (R-SIG-069). The waiter learns of a decision from the signal area `signature_verdict` (the subject's digest, raised after the commit of a
  check, a trust row or a fetch) and, without Valkey, by reading again once a second ([architecture.md](architecture.md) R-ARCH-018). The
  waits hold no database connection and no fetch slot; a replica bounds them by `signatures.gate.max_waiting` (default 512) and answers more
  at once; a draining replica answers its waits at once.
- **The decision arrives:** the gate decides again and serves or refuses. **It does not:** `503` `signature_check_pending` with
  `Retry-After: 30` (R-SIG-068); the work goes on, and the next request finds the decision.
- **A remote's fill is held** where a signature gate applies to the addressed or the storing repository, as where the malware gate is strict
  ([malware-scanning.md](malware-scanning.md) R-MAL-015, `HoldFill`): no byte of an artifact reaches a client before its stored item is
  judged.
- **Fail-safe.** A check that cannot run (the worker is down, the job is behind, an upstream does not answer the fetch) leaves the artifact
  pending, answered `503` after the wait and never served. PostgreSQL down fails the download as it does today. The object store takes no
  part in the decision: the verdicts are rows.

Acceptance criteria:

- Unit (`internal/signatures/gate`, under a second, a fake store and the signal called by the test): a waiting download is served when its
  signal arrives, and by reading once a second without it; the wait ends at `download_wait` from the arrival, so no test sleeps 25 s; the
  waits beyond `max_waiting` and every wait at a drain are answered at once.
- Integration (area `signatures`, `TestR_SIG_067_…`, about 4 s, `download_wait: 2s`, the OCI stub of R-OCI-035 with a referrers API serving
  an image signed with the company CA's Notation key, the CA as an anchor): with the gate on `docker-hub` the first `GET` of `app:1.0` waits,
  the fetch is asked and the manifest is served within the wait; with the job held, the same request answers `503 signature_check_pending`
  with `Retry-After: 30` after about 2 s, and the next request after the job ran is served; an unsigned image of the stub is refused 403
  once its fetch found nothing.

### R-SIG-068 · The answer each client gets
Priority: **P1** · Covers: quader-only

([Q-SIG-86](#q-sig-86--one-code-for-a-refusal-one-for-pending-in-the-shapes-that-exist)) The gate's answers are errors of the kinds the
formats already render: `Forbidden` with the code `signature_not_verified` and a member `reason`, and `Unavailable` with
`signature_check_pending` and `Retry-After: 30`. Each format's `WriteError` writes them in its client's shape with `Cache-Control: no-store`,
as it writes the malware gate's and the vulnerability block's:

| Format | `signature_not_verified` | `signature_check_pending` |
|---|---|---|
| npm | `403`, `{"error": <text>, "message": <text>, "code": "signature_not_verified"}`; npm prints `E403` and the text | `503` with `Retry-After`; npm retries |
| NuGet | `403`, one line `signature_not_verified: <text>` | `503` with `Retry-After`; the client retries and reads it |
| OCI | `403`, `{"errors": [{"code": "DENIED", "message": <text>, "detail": null}]}`; Docker prints `denied: <text>` | `503`, `{"errors": [{"code": "UNAVAILABLE", …}]}` with `Retry-After`; Docker does not retry |
| Ansible Galaxy | `403`, `{"errors": [{"status": "403", "code": "signature_not_verified", "title": …, "detail": <text>}]}` | `503` with `Retry-After`; `ansible-galaxy` does not retry |
| generic (`/files/`) | `403` problem with `code`, `reason` and `sha256` | `503` problem with `Retry-After` |

What each client prints and whether it retries is the malware gate's record of the same shapes ([malware-scanning.md](malware-scanning.md)
R-MAL-016, Q-MAL-3, read in the clients' sources on 2026-11-20); this stage adds no shape and no client behaviour to verify. Debian answers
none of them: its repositories have no gate (R-SIG-061).

- **The texts** name the addressed repository by its qualified name, never another repository, and no signer (a signer's name is text an
  uploader chose; the screens show it as text):

  | `reason` | Text |
  |---|---|
  | `missing` | "Quader refused `<path>` of `<project>/<repo>`: it has no signature, and this repository serves only artifacts with a verified signature." |
  | `untrusted` | "Quader refused `<path>` of `<project>/<repo>`: no trust anchor this repository accepts knows its signer." |
  | `not_chosen` | "Quader refused `<path>` of `<project>/<repo>`: its signature is verified by a trust anchor this repository does not accept." |
  | `invalid` | "Quader refused `<path>` of `<project>/<repo>`: its signature does not match its content." |
  | `blocked` | "Quader refused `<path>` of `<project>/<repo>`: an administrator blocked its signer." |
  | `not_checked` | "Quader refused `<path>` of `<project>/<repo>`: its signature could not be checked." |
  | (pending) | "Quader is checking the signatures of `<path>`. Try again in a minute." |

  Every refusal adds "Details: <the address of the version's or the file's page>".
- **Kit check 17** ([malware-scanning.md](malware-scanning.md) R-MAL-029) holds the two answers for every format as it holds the malware
  gate's: the status, `Cache-Control: no-store`, `Retry-After` for the pending one, no byte of the item.

Acceptance criteria:

- Unit (the format contract's kit, milliseconds per format): check 17 renders `403` for `signature_not_verified` and `503` with
  `Retry-After` for `signature_check_pending`, both with `Cache-Control: no-store`, for every registered format and the test format.
- Integration (area `signatures`, in the tests of R-SIG-064, R-SIG-065 and R-SIG-067): the `/files/` and the npm answers carry the code and
  the reason; the OCI answer is `DENIED` with the text.

### R-SIG-069 · One decision, one wait: the malware gate, the vulnerability block and the signature gate
Priority: **P1** · Covers: quader-only

([Q-SIG-87](#q-sig-87--one-decision-one-wait-in-the-order-of-what-was-found-before-what-is-missing))

- **A serve point decides all three checks before it waits:** the malware gate ([malware-scanning.md](malware-scanning.md) R-MAL-013), the
  vulnerability block ([vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-015) and the signature gate, each from what the item
  lookup brought. When one refuses, the answer is the first refusal in this order: `malware_found`, `vulnerability_blocked`,
  `signature_not_verified`, `malware_not_checked`, `vulnerability_data_missing`; what a check found in the content comes before what is
  missing for it. No request waits while a check refuses.
- **When none refuses and one is pending,** the request waits once, for every pending check together (the malware scan, the signatures),
  until each has decided or the earliest bound of the pending checks, counted from the arrival, is reached; it then decides again, and a check
  still pending answers its `503`, the malware gate's first.
- At OCI's handlers the image block of the vulnerability scanning is asked before the serve point, as built (R-VULN-037); the signature
  gate follows it there.
- The malware gate's `Check` is split for this into its decision and its wait; its answers, events and metrics stay as they are.

Acceptance criteria:

- Unit (`internal/formathost`, milliseconds, fakes of the three checks): an infected and unsigned item answers `malware_found`; a pending
  malware scan and an unsigned item answers `signature_not_verified` at once, without a wait; a pending scan and a pending signature wait
  together and are served when both decide.

### R-SIG-070 · Releasing an artifact for one repository
Priority: **P1** · Covers: quader-only

([Q-SIG-88](#q-sig-88--a-release-per-repository-and-artifact-by-whoever-triages-in-the-security-dashboard)) The malware gate's release
([malware-scanning.md](malware-scanning.md) R-MAL-021), per repository: the gate is a repository's decision, so its exception is one too.

```sql
CREATE TABLE signature_releases (
  repo_id         bigint NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
  subject_sha256  bytea  NOT NULL CHECK (octet_length(subject_sha256) = 32),  -- the artifact's blob, an image's manifest or index
  reason          text   NOT NULL CHECK (length(reason) BETWEEN 1 AND 1000),
  refused_reason  text   NOT NULL,                                            -- the gate's reason at the release (R-SIG-068)
  released_by     uuid   NULL REFERENCES principal (id) ON DELETE SET NULL,
  released_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (repo_id, subject_sha256)
);
```

- **What it does:** it passes the artifact through that repository's gate until it is withdrawn, whatever its signatures say; a released index
  passes its platform manifests (R-SIG-063). No other repository's gate is affected: a virtual repository's gate and its member's need a
  release each. It survives the removal of the artifact's items: the same bytes stored again in the repository are released.
- **For what:** an artifact the gate refuses, whatever the reason, `blocked` and `invalid` included (a block names a signer, not an artifact
  signed before the key was taken); one that passes cannot be released (409 `nothing_to_release`).
- **Who:** the holders of `vulnerabilities:triage` (`admin`, `security-admin`, `auditor`) together with `read` on the repository or
  `content:metadata-read`, the triage rule of [vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-013. A project's Owner does not:
  a release weakens the gate (R-SIG-061).
- **Where:** the security dashboard's view of signatures, at a refused download, and the banner of a refused version for the holder of
  these rights (R-SIG-074): a `security-admin` has no dashboard and triages on the projects' screens
  ([api-and-ui.md](api-and-ui.md) R-UI-158).
- **Recorded:** `signature.release.granted`, not journaled (it widens); `signature.release.withdrawn`, journaled, which reaches the Bastions
  as a feed entry (R-SIG-075).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_070_…`, about 2 s): an auditor releases an unsigned file of `files-internal` (201); it is served
  there and refused in a second gated repository that holds the same bytes; an Owner's release answers 403; the withdrawal refuses it again
  and records the journaled event; a release of a passing file answers 409.

### R-SIG-071 · A change of anchors, blocks or the switch, and what an administrator sees before saving it
Priority: **P1** · Covers: quader-only

- **What a change does in a gated repository:** a narrowing (an anchor removed, switched off or its material narrowed, a block, project
  anchors switched off) refuses at once what only it let pass (R-SIG-066); a widening (an anchor added or switched on, a block lifted, the
  switch turned on) passes after the trust job (R-SIG-012); a signature that arrives later passes its artifact from its check on, since no
  refusal is cached (`no-store`).
- **The impact before saving** ([Q-SIG-90](#q-sig-90--a-preview-before-switching-on-and-an-impact-before-a-narrowing-both-bounded)).
  `POST /api/v1/signatures/impact` with the change, one of `{"anchor": <id>, "change": "remove" | "disable" | "narrow"}`,
  `{"block": {"kind", "value"}}` and `{"projectAnchors": false}`, answers how many artifacts of repositories whose gate is on would stop
  passing: `{"artifacts", "repositories", "complete", "byRepository": [{"repository", "artifacts"}]}`, at most 20 repositories, the most
  first. It reads the trust rows the change subtracts from (R-SIG-066), through the new index `signature_trust_anchors` on `anchor_ids` and
  stage 1's `signatures_keys`, their signatures' attachments in gated repositories, and whether another counting signature or a release still
  passes each artifact; at most `signatures.gate.preview_limit` rows (default 100,000), else `complete: false`, and the counts are then lower
  bounds. A change of material counts as a removal, an upper bound, said so in the answer (`"bound": "upper"`). Permission: the change's own
  (`security-settings:write` for the installation's anchors, blocks and the switch; the project's Owner for its own anchors).
- **The screens** ask with it before such a change is saved (R-SIG-074): "This change makes 132 artifacts in 3 repositories that serve only
  verified artifacts refused: web/docker-local (120), …". Nothing is asked when the answer is 0.
- Stage 1's table gains `CREATE INDEX signature_trust_anchors ON signature_trust USING gin (anchor_ids);`.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_071_…`, about 2 s): with the gate on `files-internal` and on a second generic repository, the
  impact of removing the release key's anchor counts its two signed files; a file also signed by the company CA and a released file are not
  counted; a block of the key's fingerprint counts the same two.

### R-SIG-072 · The repository's preview before the gate is switched on
Priority: **P1** · Covers: quader-only

([Q-SIG-90](#q-sig-90--a-preview-before-switching-on-and-an-impact-before-a-narrowing-both-bounded)) What keeps a writer of the settings
from locking a team out.

- `GET /api/v1/projects/{projectKey}/repositories/{repoKey}/signatures/preview?onlyVerified=true&anchors=<ref>,<ref>` applies the gate of
  the given setting (the stored one without parameters) to the repository's stored items: the format's `Gate` in batches and the decision of
  R-SIG-062, without the wait. It answers `{"examined", "complete", "artifacts", "pass", "pending", "refused": {"missing", "untrusted",
  "notChosen", "invalid", "blocked", "notChecked"}, "examples": [{"path", "package", "version", "reason"}]}`, with at most 20 examples, the
  newest first. It reads at most `signatures.gate.preview_limit` items or for 10 s, else `complete: false`.
- A virtual repository previews its own gate over the items of its members that the caller may read; a remote its cache (what it has not
  stored, it does not know).
- Permission: whoever may change the member: `repositories:read-config`, or the project's Owner.
- The section of R-SIG-074 asks before it saves a narrowing whose preview refuses anything, and shows the examples.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_072_…`, about 2 s): the preview of `files-internal` with one file signed by the release key,
  one by the company CA and one unsigned answers 2 passing and 1 `missing` with any anchor, and 1 passing, 1 `notChosen` and 1 `missing`
  with only the release key's anchor chosen.

### R-SIG-073 · The API of stage 3
Priority: **P1** · Covers: quader-only

In `api/openapi.yaml` under the tag `signatures`:

| Method | Path | What | Permission |
|---|---|---|---|
| GET | `/api/v1/projects/{projectKey}/repositories/{repoKey}/signatures/preview` | R-SIG-072 | `repositories:read-config`, or the project's Owner |
| POST | `/api/v1/signatures/impact` | R-SIG-071 | the change's own |
| GET | `/api/v1/signatures/refusals` | The downloads a gate refused in the last `signatures.gate.refusals_retention`, one row per repository and artifact: the repository, the path, the package and version, the digest, the reason, the first and the last refusal, the count, and the release if one was granted since; filters `repository`, `reason`; cursor paging (R-API-010) | `content:metadata-read`; a caller with `read` on some repositories sees those |
| GET, POST | `/api/v1/projects/{projectKey}/repositories/{repoKey}/signatures/releases` | The releases of the repository; a release `{"sha256", "reason"}`, `201`, `409 nothing_to_release`, `409 already_released` | read: as the refusals; write: R-SIG-070 |
| DELETE | `/api/v1/projects/{projectKey}/repositories/{repoKey}/signatures/releases/{sha256}` | Withdraws a release; `204` | R-SIG-070 |

- **The refused downloads** are rows of a table the gate writes, with the deduplication of its audit event (R-SIG-075), so the dashboard reads
  no audit log:

  ```sql
  CREATE TABLE signature_refusals (
    repo_id        bigint NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    subject_sha256 bytea  NOT NULL CHECK (octet_length(subject_sha256) = 32),
    path           text   NOT NULL CHECK (length(path) <= 2048),
    reason         text   NOT NULL,
    first_at       timestamptz NOT NULL DEFAULT now(),
    last_at        timestamptz NOT NULL DEFAULT now(),
    count          bigint NOT NULL DEFAULT 1,
    PRIMARY KEY (repo_id, subject_sha256)
  );
  ```

  A pending answer writes none. The discovery step of the job `signature-verify` removes the rows whose `last_at` is older than the
  retention.
- **The member `signatures`** of a stored file and of a version (R-SIG-034) gains `refused` (the gate that applies refuses it now in the
  addressed repository, with the settings and anchors as they are) and `refusedReason` (a reason of R-SIG-068, or `pending`), absent while no
  gate applies; the version list carries them per version.
- **The signatures answer** of a version and of a file (R-SIG-034) gains per repository `gate`: `{"onlyVerified", "anchors": [{"ref",
  "title", "kind", "scope", "removed"}], "decision": "pass" | "refused" | "pending", "reason", "passedBy": {"signature"} | {"release"}}`.
- **The repository document** carries the member of R-SIG-061, validated as the other members (R-REPO-004).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_073_…`, about 2 s): the operations of the table for an administrator, an auditor, a
  `security-admin`, the Owner of `web` and an anonymous caller; `GET …/items?path=` of a refused file carries `signatures.refused: true` and
  its reason; a refused download makes one row of `signature_refusals` and a second one within the minute raises nothing but `last_at` and
  `count`.

### R-SIG-074 · The screens of stage 3
Priority: **P1** · Covers: quader-only

Simple by default ([CLAUDE.md](../../CLAUDE.md#product-simple-by-default-details-on-demand)): nothing of the gate is in a creation flow or
in the main view of a repository; a plain user sees why a download is refused where the file is.

- **The repository's settings page** ([api-and-ui.md](api-and-ui.md) R-UI-087): the closed section **Signatures** (R-SIG-040) holds, before
  `fetch`, the checkbox **Only artifacts with a verified signature can be downloaded from this repository**, unchecked by default, with the
  sentence "Artifacts without a signature that a trust anchor of this repository knows are refused. A signature says who signed an artifact,
  not that it is safe." Checked, it shows **Accepted trust anchors** with two choices: **Any trust anchor of this repository** (the default),
  with "This includes the public Sigstore, where anyone can sign. Choose anchors to accept only your own signers.", and **These trust
  anchors**, a list of the scope's anchors with title, kind, scope and fingerprint to tick, a removed one shown as **Removed trust anchor**
  in red. Under them the stored setting's state when the section opens: "<n> of <m> artifacts pass. <k> are refused." (R-SIG-072), with a
  link to the refused downloads in the security dashboard for its readers.
  - On a virtual repository it adds "Applies to everything this repository serves from its members."; on a remote with `fetch` off, "Signatures
    of images are not fetched: an image whose signature no client pulled is refused."
  - On a Debian repository the section says "apt checks the signature of this repository's index, not of a package. This setting is not
    available for Debian repositories." and offers nothing.
  - For a writer who may not weaken it (an Owner), a gate that is on is shown checked and read-only with its anchors, and "Only an
    administrator or someone who triages security findings can loosen this."; a narrowing stays open to it.
  - While `signatures.enabled` is false: "Signature checks are switched off in this installation. This setting does not apply."
  - Saving a narrowing whose preview refuses anything asks first: "<n> of <m> artifacts in this repository have no verified signature that
    this setting accepts and will be refused. <k> are still being checked." with up to 20 examples.
- **At a version, a file and an image** (R-SIG-035): the badge stays as it is; the version's page shows a banner when an artifact of it is
  refused by a gate in a repository the reader may read: "Downloads are refused in `<project>/<repo>`: it serves only artifacts with a
  verified signature, and `<file>` <the reason's sentence>.", with **Release for this repository** for the holder of R-SIG-070's rights,
  the dialog of the dashboard. The fact **Signature** of the Files tab and of the file's page says "Refused
  here" with the reason, and a refused file offers no **Download**, as a file the malware gate refuses
  ([api-and-ui.md](api-and-ui.md) Q-UI-882's form). An image lists its platforms with their decision; a platform that passes on its index
  says "Signed through its image index". A pending artifact shows nothing more than **Not checked yet**.
- **Security dashboard › Signatures** (R-SIG-038) gains the list **Refused downloads** (`GET /api/v1/signatures/refusals`): repository,
  artifact, reason, last refusal, count; **Release for this repository** for the holder of R-SIG-070's rights, a dialog that names the one
  repository and the reason it refuses and asks for a reason; "No refused downloads." when empty. A closed section **Releases** lists them
  with **Withdraw release**, which asks first.
- **Admin area › Settings › Signatures** (R-SIG-036) and a project's **Trust anchors** (R-SIG-037): removing or switching off an anchor,
  narrowing its material, blocking a signer and switching project anchors off ask with the impact of R-SIG-071 when it is not 0.
- **The client setup dialog** of a gated repository ([api-and-ui.md](api-and-ui.md) R-API-030) says above the snippets "This repository serves
  only artifacts with a verified signature." and names the accepted anchors' titles, read from the repository document.
- The screens are rendered and looked at before they land (rule 11 of the plan): both schemes, wide and narrow; the section off, on with any
  anchor, on with three chosen anchors and a removed one, read-only for an Owner, on a Debian repository; the confirmation with a preview of
  thousands; a refused version, an image whose index passes and a platform that passes through it; the dashboard's lists empty and with long
  paths; an anchor's removal with its impact.

Acceptance criteria:

- Web unit (Vitest, under a second): the section's states (off, any, chosen with a removed anchor, read-only for an Owner, Debian, checks
  switched off); the confirmation shows the preview's numbers; the banner for a refused file and none for a pending one; **Release for this
  repository** only for its holders.

### R-SIG-075 · Audit events, metrics, alerts and the Bastion's feed of stage 3
Priority: **P1** · Covers: quader-only

**Audit events** ([operations-and-observability.md](operations-and-observability.md) R-OPS-016):

| Event | Mode | Journal | Recorded when | Key fields in `details` |
|---|---|---|---|---|
| `signature.download_refused` | A | — | a gate refused a download, at most once a minute per repository, path and client address on each replica | `repository`, `path`, `sha256`, `reason` |
| `signature.release.granted` | T | — | R-SIG-070 | `repository`, `sha256`, `refusedReason`, `reason` |
| `signature.release.withdrawn` | T | yes | R-SIG-070 | `repository`, `sha256`, `reason` |

The setting is recorded as every repository setting: `repository.updated`, with `block: lifted` for a weakening, and `repository.narrowed`
(R-SIG-061).

**Metrics** ([operations-and-observability.md](operations-and-observability.md) R-OPS-008), of the roles that serve downloads:

| Metric | Type | Labels |
|---|---|---|
| `quader_signatures_downloads_refused_total` | counter | `reason` (R-SIG-068) |
| `quader_signatures_download_wait_seconds` | histogram | `outcome`: `served`, `refused`, `pending` |

**Alerts** (rendered with the chart's rules, [ha-and-helm.md](ha-and-helm.md) R-HA-045), the one the gate needs beside stage 1's
(R-SIG-039), whose `QuaderSignatureInvalid` stays the alarm on an invalid signature. A refusal is the gate's work and alerts nothing; what
must alarm is that clients get `503` because the checks do not keep up:

| Alert | Expression | For | Severity |
|---|---|---|---|
| `QuaderSignatureGatePending` | `sum(increase(quader_signatures_download_wait_seconds_count{outcome="pending"}[15m])) > 0` | 15m | warning |

**The Bastion's feed** ([bastion.md](bastion.md) R-BAS-015;
[Q-SIG-94](#q-sig-94--the-bastion-learns-a-narrowing-as-content-entries-for-the-gated-repositories)): `signature.release.withdrawn` yields
`content` for its repository and its virtual repositories; stage 1's `signature.anchor.removed`, `signature.anchor.updated` and
`signature.settings.changed` with the journal flag, and `signature.signer.blocked`, yield `content` for every repository whose gate is on when
the feed is read, and their virtual repositories; a gate switched on is a `repository.narrowed`, which yields `content` already.

Acceptance criteria:

- Unit (`internal/audit`, under a second): the three events are in the registry with their schemas and journal flags; the Bastion's
  classification test (`TestR_BAS_015_EveryJournaledEventIsClassified`) classifies the journaled ones.
- Chart test (`test/chart`, about 0.1 s within `TestR_HA_045_PrometheusRule`): the alert renders and `promtool` checks it.

### R-SIG-076 · Configuration, backups, restores and replicas of stage 3
Priority: **P1** · Covers: quader-only

```yaml
signatures:
  gate:
    download_wait: 25s              # default; 0s to 60s, from the request's arrival (R-SIG-067)
    max_waiting: 512                # default; waits per replica
    preview_limit: 100000           # default; rows or items one preview or impact reads (R-SIG-071, R-SIG-072)
    refusals_retention: 720h        # default; how long a refused download stays listed (R-SIG-073)
```

- **Validation** at start, with the key named: `download_wait` from 0 s to 60 s; `max_waiting` from 1 to 65,536; `preview_limit` from 1,000
  to 10,000,000; `refusals_retention` from 1 h to 8,760 h. No chart value is needed: the keys take their defaults from the configuration, and
  the chart's rules render the alert.
- **Backups** ([backup-and-restore.md](backup-and-restore.md) R-BAK-004): `signature_releases` is `data`; `signature_fetches` (registered by stage 1)
  and `signature_refusals` are `transient` (a fetch is asked again, a refusal is a recent fact); the repository member travels with the
  document; the two columns of R-SIG-066 travel with their tables.
- **Restores:** the journal re-applies a withdrawn release and a narrowing of the member (R-SIG-061); a release granted after the restore
  point is lost, which narrows.
- **Replicas:** the setting reaches every replica through the change area of the repositories' settings, anchors and blocks through
  `signature_anchors`, within R-ARCH-018's bound; a waiting download on any replica learns a decision through the signal area
  `signature_verdict`, or by reading again once a second without Valkey. The jobs and the fetch run once in the installation, as in stage 1.
- **What it costs:** no Deployment, image, module or chart value more; per gated download a join of the item's signatures and their trust
  rows into its statement and, for OCI and a covered item, one statement more; a row of `signature_refusals` per refused artifact and
  repository for 30 days; stage 1's row of `signature_fetches` per queued remote target (some 150 bytes) is not this stage's.

Acceptance criteria:

- Unit (`internal/config`, under a second): the defaults; each refusal of the validation, with the key named.

### R-SIG-077 · Mirrors, the Bastion, stage 2, metadata documents, and what stage 3 does not do
Priority: **P1** · Covers: quader-only

- **Mirrors:** a pass fetches the signatures of each kept top-level manifest (R-SIG-025) and writes the fetch's state completed, so the first
  pull of mirrored content through a gated repository is decided without a wait. The gate does not refuse a pass: the pass stores, the gate
  judges downloads.
- **The Bastion** has no gate of its own, as for the malware gate (R-MAL-018): it caches only what the main installation served it, which
  passed the gate there, and learns a narrowing from its feed (R-SIG-075) at its next read. It serves neither OCI nor Debian (R-BAS-015).
- **Stage 2:** the receipt attestation is made for a refused artifact as well, because it states what was received (R-SIG-053), and it
  carries no decision of the gate: the decision follows the anchors and changes with them, so a signed copy would contradict the screens
  after the next change, as the trust state would (Q-SIG-61). Quader issues no verification summary
  ([Q-SIG-92](#q-sig-92--no-verification-summary-of-the-gates-decision)). Quader's own signatures never pass (R-SIG-062); its attestation
  referrer is an attachment, which passes as every attachment does (R-SIG-063).
- **Metadata documents list what the gate refuses** ([Q-SIG-89](#q-sig-89--metadata-documents-list-what-the-gate-refuses)): npm's package
  document, NuGet's registration and version lists, Ansible's version lists and OCI's tags list name the versions a gate refuses, as they name
  the versions the vulnerability block refuses ([vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-015): a client that resolves a
  range takes the newest version and gets the refusal, not an older signed one
  ([H-1199](#h-1199--a-client-that-resolves-a-range-picks-a-version-the-gate-refuses)).
- **Not in stage 3:** an installation switch and a project default (Q-SIG-75); a gate at the upload (Q-SIG-79); a report mode (Q-SIG-91);
  Debian (Q-SIG-80); a gate per package or path pattern; demands on a signature beyond its anchors, such as a transparency-log entry or a
  provenance's builder and source (the catalog's BEYOND-020).

Acceptance criteria: none of its own; the requirements it names test their parts.

### R-SIG-078 · The tests of stage 3 and what they cost
Priority: **P1** · Covers: quader-only

Each behaviour once, at the cheapest tier that shows it ("Tests stay runnable" in [CLAUDE.md](../../CLAUDE.md)). The gate is the same for
every format: it is tested without a format's client, in the gate's package and against the stack with the clients of the tests; a format adds
its kit cases only. Signatures are made in process with the three ways of R-SIG-042.

| Tier | What | Run time | Data | Where |
|---|---|---|---|---|
| Unit | the setting's order and replay; the decision table; the subtraction of R-SIG-066; the wait (signal, reading again, bound from the arrival, slots, drain); the answers; one decision, one wait; the configuration; the events | under 2 s together (an estimate) | none: rows and snapshots built by the tests | `internal/signatures/gate`, `internal/formathost`, `internal/config`, `internal/audit` |
| Format contract kit | check 18's gate cases and check 17's two answers for every format | milliseconds per format | the fixtures' cases | `internal/formats/formattest` |
| Unit, OCI | the recognition of an attachment | milliseconds | manifests built by the test | `internal/formats/oci` |
| Integration | R-SIG-061, R-SIG-064 to R-SIG-067, R-SIG-070 to R-SIG-073 against the stack | about 22 s for nine tests of 2 to 4 s in the area `signatures` (estimates) | files of a kilobyte, npm tarballs and OCI images of a few hundred bytes | `test/integration/signatures` |
| Chart | the alert | about 0.1 s | rendered YAML | `test/chart` |
| Web unit | the section, the confirmation, the banner, the dashboard's actions | under a second | none | `web/` |
| End to end | none ([Q-SIG-95](#q-sig-95--no-end-to-end-case-one-measurement-by-hand)) | — | — | — |

What every leg gains: the unit run under 2 s; the kit milliseconds per format; the integration shard that runs `signatures` about 22 s; the
chart leg about 0.1 s; the web unit run under a second. No other leg gains anything, and no leg is new. With 25 formats: 25 times two kit
cases of milliseconds more, nothing else; no leg loads another format's client.

What only a real client shows and is not tested: `docker pull` of a multi-platform image signed at its index through a gated repository, a
refused image's `denied:` line in Docker and Kubernetes, and `cosign verify` of a refused image, whose signatures stay fetchable. Each is
measured once by hand when the package that builds the gate lands, and the result written into this requirement.

Acceptance criteria: the coverage generator lists every requirement of stage 3 with a test of the tier this table names.

## Decisions

The owner decided Q-SIG-1 to Q-SIG-16 on 2026-11-21 in his working paper, from the options the development agent put to him; the blocks
keep those options as they were put, translated from the paper's German. Q-SIG-20 onward are the development agent's: each question below
was decided by the development agent from the listed options, and the owner reviews the choices at the walkthrough. Q-SIG-75 to Q-SIG-95
are stage 3's.

### Q-SIG-1 · What the icon stands for

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) What proves the publisher's origin: a publisher signature and a provenance attestation; a registry's signature in the details (chosen) | | The icon tells something apart: most artifacts do not carry it; the registry's signature stays visible as "Origin confirmed by registry" | No Debian package carries the icon, because only the index is signed |
| (b) Every valid signature, of whatever kind | | Simplest to explain | Every package of npmjs and nuget.org and every archive `.deb` carries it; it says nothing |
| (c) Two icons, one for the publisher and one for the registry | | Both facts at a glance | Two marks a plain user must tell apart |

**Applied in the spec:** R-SIG-001, R-SIG-009, R-SIG-028 to R-SIG-030.

### Q-SIG-2 · The kinds of trust anchor

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Four kinds under one setting "Trust anchors": CA certificate (with an optional restriction on the subject as a detail), OpenPGP key, public key, Sigstore identity (issuer and identity pattern, against the public or an own Sigstore root) (chosen) | | Covers every family and so every format that has a scheme; one list, one name per anchor at the artifact | Four forms; each kind needs its verifier |
| (b) CA certificates only, as said | | His words; one form | Debian, Ansible and later RPM, Maven, Helm and Terraform know no CA; cosign with a key pair neither |
| (c) CA certificates and OpenPGP keys now, the other two later | | The two oldest families first | cosign keyless, common on public images, has no anchor |

**Applied in the spec:** R-SIG-015, R-SIG-008.

### Q-SIG-3 · Where anchors are maintained

Status: **Decided by the owner on 2026-11-21** — option (b), as recommended.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Only in the UI and the API | | One place; changes act at once | Not declarative |
| (b) Both: anchors declared in the Helm values appear read-only beside those the administrator maintains (chosen) | The pattern of the system role bindings | Fits an installation from Git; anchors are public material | Two sources the UI tells apart |
| (c) Only through Helm | | Declarative | Project Owners could never maintain anchors |

**Applied in the spec:** R-SIG-016, R-SIG-018.

### Q-SIG-4 · What Quader ships as public

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The public Sigstore root and the keys of the well-known upstreams with their preset (npmjs, Debian and Ubuntu, nuget.org's repository certificates); no general list of public code-signing CAs; each can be switched off (chosen) | | Covers cosign keyless, npm, Debian and nuget.org's countersignature | An author signature under a public code-signing CA reads "signer not trusted" until an administrator adds the CA |
| (b) Also a list of public code-signing CAs | | NuGet author signatures public at once | Who keeps the list current is open; Quader's image has no system store |
| (c) Nothing | | No upkeep | A fresh installation shows "public" nowhere |

**Applied in the spec:** R-SIG-017; Q-SIG-29 adds the timestamping roots nuget.org's countersignatures need.

### Q-SIG-5 · Where and when verification runs

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A background job in the worker at ingest, libraries in process; an anchor change recomputes only the trust step (chosen) | | No Deployment, no tool binary; signatures are kilobytes; an anchor change reads no artifact | The worker holds secrets and reads foreign signature containers, as it does for Debian upstreams; new libraries in the binary |
| (b) A unit without secrets per tool, after the scanners' pattern | | Foreign input opened where no secret is | Three tools, units and images for kilobytes of input |
| (c) At display or download, every time | | Always the current anchors | Request time; no stored verdict for a gate |

**Applied in the spec:** R-SIG-011 to R-SIG-014; [H-1166](../security/artifact-signatures.md#h-1166--the-worker-reads-hostile-signature-containers-in-a-process-that-holds-secrets).

### Q-SIG-6 · Signatures of remote content

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Quader fetches in the background what the format defines, once an artifact is stored through a remote (chosen) | | The badge is right even if no client asked; mirror passes take signatures | For OCI up to three small requests more per image; how Docker Hub counts them was not checked then |
| (b) Pass through only | | No request more | A signed image reads unsigned until someone runs `cosign verify` |
| (c) In mirror passes only | | Planned load | The commonest path, a first pull through a remote, stays without |

**Applied in the spec:** R-SIG-025, R-SIG-026; Q-SIG-33 sets what and how much.

### Q-SIG-7 · Time, expiry and revocation

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Valid if valid when made; a timestamp or a log entry proves the time, else Quader's receipt; no CRL or OCSP; the administrator can block a signer (chosen) | | Old signatures do not tip over because a certificate expired since; works without internet; a block acts at once | Quader can show "verified" where a client without a timestamp refuses after expiry; the details name the expiry |
| (b) Strict as the clients | | Quader and client agree | Holdings tip over with time |
| (c) (a) plus revocation lists | | Revoked certificates found | Outbound requests per check; "unknown" without internet |

**Applied in the spec:** R-SIG-007, R-SIG-020; [H-1168](../security/artifact-signatures.md#h-1168--a-revoked-certificate-stays-verified-until-an-administrator-blocks-its-signer).

### Q-SIG-8 · Generic files

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) By name: `<file>.asc`, `.sig`, `.sigstore.json`, `.jws.sig`, `.cose.sig` are signatures of `<file>` in the same directory; the signature stays a downloadable file marked "signature of …" (chosen) | | What the tools write | A `.sig` can mean several schemes; "detected, scheme unknown" where none can be told |
| (b) Not at all | | Nothing to build | Breaks "every artifact" where teams keep their own builds |
| (c) Explicitly at upload | | Unambiguous | No tool does it |

**Applied in the spec:** R-SIG-032; Q-SIG-38.

### Q-SIG-9 · Display or gate

Status: **Decided by the owner on 2026-11-21** — option (b), **against the development agent's recommendation (a)**; with Q-SIG-14 the gate
is stage 3.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Not in stage 1: an invalid signature raises an audit event, a metric and an entry of the security dashboard; the verdict is stored so that a gate is later a query at the malware gate's place (recommended) | | Experience with the verdicts first | An artifact with an invalid signature is served, marked |
| (b) Besides, per repository a detail setting "only artifacts with a verified signature", optionally of chosen anchors (chosen) | | A curated repository takes only what its own CI signed | More scope: each client's answer, the pending case, releases |
| (c) A switch of the installation | | One place | Most public packages are not publisher-signed |
| (d) Only "Invalid signature" blocks, always | | Tampering stopped | "Invalid" can mean a variant the verifier does not read |

**Applied in the spec:** R-SIG-038 (stage 1's display and alarm), R-SIG-043 (what the gate of stage 3 finds), R-SIG-061 to R-SIG-078 (the
gate, stage 3, with Q-SIG-75 to Q-SIG-95).

### Q-SIG-10 · Quader signs as a registry

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended; stage 2.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Where a client checks it with one command: Debian (built), npm for locally published packages with Quader's key at `-/npm/v1/keys`; not NuGet's countersignature (chosen) | | `npm audit signatures` passes for own packages | A key of the installation to hold and rotate |
| (b) Only where nothing works without | | No new key | Local npm packages report a missing signature |
| (c) Everywhere a scheme exists | | Complete | NuGet's countersignature changes the package's bytes |

**Applied in the spec:** R-SIG-043; R-SIG-028 (the keys endpoint's local answer in stage 1).

### Q-SIG-11 · Quader issues attestations

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended; stage 2.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A signed statement per artifact: digest, origin, the signature check's result, the malware verdict with its database, the vulnerability scan's state; for OCI as a referrer (chosen) | | A cluster can demand images that went through Quader | A statement ages |
| (b) Only for OCI | | Tools exist | Other formats get no evidence |
| (c) Park it | | Smaller | Quader signs only as a registry |

**Applied in the spec:** R-SIG-043.

### Q-SIG-12 · Quader signs in a publisher's name

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) No; Quader records who uploaded, for CI the verified claims of the pipeline, and shows it as origin (chosen) | | A signature says only what its signer knows | A real publisher signature must come from CI |
| (b) Yes, every upload | | Everything own is signed | Write access becomes signing authority |
| (c) Yes, per repository opt-in | | Limited damage | The same weakness, smaller |

**Applied in the spec:** R-SIG-010.

### Q-SIG-13 · The signing identity

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended; stage 2.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One per installation (certificate and key of the company's CA) for attestations, OCI and the npm registry key; OpenPGP stays per repository where the format needs it; the key only in worker pods (chosen) | | Consumers check against a CA they trust; cert-manager can issue it | Debian needs OpenPGP; re-signing on a certificate change is not designed |
| (b) Also per project | | A statement names the project | Private keys with project Owners |
| (c) In a KMS or HSM | | The key never leaves hardware | One integration per product |

**Applied in the spec:** R-SIG-043.

### Q-SIG-14 · The order of the work

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Three stages: detect, read, verify, show, anchors with the Owner switch, OCI's presentation and lifecycle, the pass-through gaps, for all six formats; then Quader signs; then the gate per repository (chosen) | | Each stage useful on its own, each with its specification | Quader signs only in stage 2 |
| (b) All at once | | One design | Stage 2's open points hold stage 1 |
| (c) OCI first | | Fast where it looks most wrong | The format-neutral core grows at one format |

**Applied in the spec:** this page.

### Q-SIG-15 · Which way to a signature the CI has

Status: **Decided by the owner on 2026-11-21** — option (a), with his note: "Alle 3 Wege sollen von dir gebaut und betrieben werden." What
"betrieben" covers beyond Quader is asked back to him by the main session.

| Option | What it means |
|---|---|
| (a) The company's own Sigstore: its certificate authority issues a certificate per job (chosen) | the Sigstore identity anchor against an own trust root |
| (b) A code-signing certificate of the company's PKI in CI | the CA anchor |
| (c) An OpenPGP key in CI | the OpenPGP anchor |
| (d) Nothing yet | — |

**Choice:** all three ways are first class in stage 1: each has its anchor kind, its schemes and a real signature in the tests (R-SIG-042).

**Applied in the spec:** R-SIG-015, R-SIG-042.

### Q-SIG-16 · The eight readings of the request

Status: **Decided by the owner on 2026-11-21** — the development agent's eight readings, confirmed.

- **r1** three steps, stored at the signed content, the original kept (R-SIG-001, R-SIG-003);
- **r2** per format what its ecosystem knows; none invented; a format without a scheme says so; Debian's index says so (R-SIG-002);
- **r3** four visible states; no icon for no signature, not checked yet, scheme not supported (R-SIG-009);
- **r4** one switch of the installation, off by default; project anchors only add; the administrator sees and removes them; off again,
  they stay stored and stop applying (R-SIG-019);
- **r5** an obligation in the format contract and one case of the kit; everything else format-neutral (R-SIG-022);
- **r6** on the wire as clients expect; in the UI in no list and no count but at the image; the same for attestations and SBOMs; the
  signature follows its image; for every format a signature is never an entry of a list (R-SIG-024, R-SIG-025, R-SIG-022);
- **r7** nothing breaks an upstream signature; Quader answers every place a client asks (R-SIG-026 to R-SIG-032);
- **r8** (stage 2) the signing certificate through Helm (a Secret, cert-manager may issue it) or an upload sealed by the key ring; Helm wins
  (R-SIG-043).

### Q-SIG-20 · Signatures have a page of their own

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A page `signatures.md` with the prefix `R-SIG` (chosen) | | One subject, every format; test names `TestR_SIG_…` stay apart | One more page |
| (b) Sections of each format page | | Close to the protocol | The format-neutral core would be written six times |
| (c) Sections of `malware-scanning.md` | | Both are checks of stored content | Different units, states and screens |

**Choice:** (a). **Rationale:** a page per subject keeps each one finishable; the format pages link the sections of their format. **Applied
in the spec:** this page; [README.md](README.md) (the page index).

### Q-SIG-21 · A signature is stored once per object and subject, reached per repository, and trusted per scope

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The integrity verdict per (object, subject) for the installation; attachments per repository; trust per scope (chosen) | `signatures`, `signature_attachments`, `signature_trust` | A signature is read once however many repositories hold it; a reader sees only what its repositories carry; an anchor change touches only trust rows | Three tables |
| (b) Everything per item | | Simple joins | The same signature read once per copy; trust rows per item multiply |
| (c) Per blob only, like the malware verdict | | One row | Shows repository A's signature to readers of repository B, who may not read A and whose clients cannot fetch it |

**Choice:** (a). **Rationale:** the bytes decide integrity everywhere, the repository decides what is attached, and the scope decides trust;
each fact belongs to the row that can know it. The development agent would revisit it if the attachment rows grow beyond the items they
describe. **Applied in the spec:** R-SIG-003, R-SIG-004, R-SIG-008.

### Q-SIG-22 · The original of a signature that is no item is kept in PostgreSQL, bounded

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It refines INV-4
([README.md](README.md#design-invariants)).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A signature object that an item keeps (an OCI manifest, a sibling file, an npm bundle, a Debian release, a NuGet package's entry) is referenced; one that lives inside a document (npm's `dist.signatures`, Ansible's `signatures[]`) is kept as bytes in `signature_objects.raw`, at most 256 KiB (chosen) | | The original survives a refreshed document; no tiny objects in the store; a document's signature of 150 bytes is metadata like an OCI annotation | Binary bytes in PostgreSQL, against INV-4's wording |
| (b) Every object as a blob in the object store | | INV-4 to the letter | A million npm registry signatures become a million objects of 150 bytes |
| (c) Only references | | No copy | A remote document refreshed upstream loses the evidence a verdict was made on |

**Choice:** (a). **Rationale:** INV-4 keeps artifact content out of PostgreSQL; a signature taken out of a metadata document is metadata of
the same kind as OCI's annotations (64 KiB, R-OCI-007), with a bound. **Applied in the spec:** R-SIG-003; cross-page: INV-4 in
[README.md](README.md) names the exception.

### Q-SIG-23 · A keyless scheme keeps the digest its signature commits to

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: an OpenPGP signature, npm's registry signature and a raw key signature carry no key; the signature cannot be matched with the
content until a key is known, and the owner's decision says an anchor change reads no artifact (Q-SIG-5).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The read computes the digest over the content and the signature's trailer and stores it; a key added later verifies the signature against it (chosen) | | No read on an anchor change; a 4 GB file is hashed once | A stored digest to trust: it is Quader's own computation |
| (b) Re-read the content when a key arrives | | Simple | Contradicts Q-SIG-5; a key added makes the worker read every generic file again |
| (c) Check only when a key is known at ingest | | Simple | A key added later never verifies an earlier signature |

**Choice:** (a). **Applied in the spec:** R-SIG-003, R-SIG-006, R-SIG-012.

### Q-SIG-24 · A signature without a key, or with a blocked signer, reads "Signed · signer not trusted"

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The owner's four states stay; `no_key` and `blocked` show as "Signed · signer not trusted", the details say which (chosen) | | Four states, as decided; the details are honest ("could not be matched with the content: no key of this signer is known"; "blocked by an administrator") | One state for three facts |
| (b) A fifth state each | | Exact | Against r3 |
| (c) `no_key` as "Invalid signature" | | Cautious | An alarm for every OpenPGP signature of an unknown key |

**Choice:** (a). The integrity check uses an upstream's announced key (npm's keys, Debian's upstream keys) to establish the match without
making it trusted. **Applied in the spec:** R-SIG-003, R-SIG-006, R-SIG-008, R-SIG-020;
[H-1174](../security/artifact-signatures.md#h-1174--signed--signer-not-trusted-can-stand-for-a-signature-whose-match-was-not-checked).

### Q-SIG-25 · An artifact's state is the best of its signatures, and an invalid one alone shows "Invalid"

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The best counting signature decides; `internal` over `public`; an invalid one beside a better one is listed, alarmed and does not change the badge (chosen) | | A valid signature binds the digest, so an invalid one beside it is extra, not tampering of the content; nobody who may push can lower another publisher's badge | An invalid signature is less visible on the version |
| (b) Any invalid one shows "Invalid" | | Maximal caution | Whoever may push a referrer marks any image invalid |
| (c) The newest decides | | Simple | Order of arrival is no evidence |

**Choice:** (a). **Rationale:** signatures cover digests: content changed under a signature makes that signature invalid and no other valid.
`internal` wins because the company's own statement is the more specific. **Applied in the spec:** R-SIG-008, R-SIG-009.

### Q-SIG-26 · The scope is the project of the addressed repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The addressed repository's project; through a virtual repository its project's anchors judge every member's content, `upstream`'s included (chosen) | | A team that reads public images through its virtual repository sees them judged by its anchors; the gate of stage 3 decides at the addressed repository, as the malware gate does | A project's anchors reach content of `upstream` |
| (b) The project of the repository that holds the attachment | | The holder decides | A project's anchors would never apply to what it reads through `upstream` |
| (c) Always the installation's | | One scope | r4's project anchors have no effect |

**Choice:** (a). **Applied in the spec:** R-SIG-008, R-SIG-027;
[H-1167](../security/artifact-signatures.md#h-1167--a-project-owners-anchor-is-shown-to-every-reader-of-the-projects-repositories).

### Q-SIG-27 · A Sigstore identity carries its trust root

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The anchor names `public` or holds a `trusted_root.json` (chosen) | | One form, as the four kinds were chosen | Several identities against one own root repeat it |
| (b) A fifth object "Sigstore trust root" that identities reference | | No repetition | A fifth kind against Q-SIG-2 |

**Choice:** (a). An identity pattern covers many jobs, so an installation needs few. The development agent would revisit it when an
installation holds more than a handful of identities against one own root. **Applied in the spec:** R-SIG-015.

### Q-SIG-28 · The verifiers are four libraries in process

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) sigstore-go, notation-core-go with tspclient-go, digitorus/pkcs7 and digitorus/timestamp (which sigstore-go pulls in) with own NuGet code, go-crypto (chosen) | | Measured 4.76 MiB; one PKCS#7 library in the build, not two; the libraries of the ecosystems' own clients where they exist | 52 modules; notation-core-go and tspclient-go release rarely |
| (b) cosign's and notation-go's libraries | | Policy engines included | cosign pulls registry clients and much more; Quader has its own anchors and policy |
| (c) Own code for every scheme | | No dependency | Sigstore's and Notation's verification is security code of others' expertise |

**Choice:** (a). **Applied in the spec:** R-SIG-014.

### Q-SIG-29 · nuget.org's preset ships the timestamping roots its countersignatures need

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It completes Q-SIG-4
without widening it.

Context: nuget.org's repository certificates are of 2018 to 2021, 2021 to 2024 and 2024 to 2027; a package countersigned with an older one
is valid at its stamped time only if its timestamp's authority is trusted, else Quader's receipt is the time and the certificate expired
before it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The preset ships the roots of the authorities its countersignatures' tokens chain to, as anchors of the use `timestamping` only (chosen) | | nuget.org's packages read as they are; a timestamping anchor proves a time and never trusts a signer | A few roots more to keep with the release |
| (b) Nothing more | | Q-SIG-4 to the letter | Most of nuget.org's packages would read "signer not trusted" |

**Choice:** (a). The builder reads which roots the countersignatures carry from a sample of nuget.org packages and records them. **Read on
2026-11-21** from the `.signature.p7s` of twelve packages (Newtonsoft.Json 9.0.1, 11.0.2 and 13.0.3; Serilog 2.9.0, 2.10.0, 2.12.0, 4.0.0,
4.3.0, 4.4.0 and 4.4.1-dev-02453; Serilog.Sinks.Console 4.0.0; Microsoft.Extensions.Logging 11.0.0): VeriSign Universal Root CA (the
Symantec timestamping signers of the packages countersigned with the certificates of 2018 and 2021), DigiCert Assured ID Root CA (the
DigiCert timestamp responders of 2022 to 2026) and DigiCert Trusted Root G4 (whose cross-certificate those tokens carry until 2031), shipped
in [nuget-org-timestamping.pem](../../internal/signatures/shipped/data/nuget-org-timestamping.pem). **Applied
in the spec:** R-SIG-007, R-SIG-017, R-SIG-029.

### Q-SIG-30 · A CA anchor requires code signing and may restrict the subject

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A chain to the anchor at the signature's time with `codeSigning`; the subject restriction as a detail (chosen) | | A TLS certificate of the same CA signs nothing; Notation, NuGet and Fulcio certificates carry `codeSigning` | A company CA that issues signing certificates without the use needs it added |
| (b) Any certificate of the CA | | Fewer surprises | A web server's certificate becomes a signer |

**Choice:** (a). **Applied in the spec:** R-SIG-008, R-SIG-015.

### Q-SIG-31 · Debian's upstream keys stay the remote's condition and are no anchor

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `deb.upstreamKeys` decides what the remote stores (as built); for display the anchors judge; Quader's own index signatures are facts, not checked (chosen) | | A writer of a repository's settings does not make content read as trusted without the switch of r4 | A remote with a key no anchor has shows "signer not trusted" while it serves |
| (b) `deb.upstreamKeys` count as anchors of the repository | | Consistent at once | Bypasses the administrator's switch for project anchors |

**Choice:** (a). **Applied in the spec:** R-SIG-030.

### Q-SIG-32 · An attachment tag is recognised by its name and gets no version

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `sha256-<64 hex>` and its `.sig`, `.att`, `.sbom` forms are attachment tags; their items have no version row (chosen) | | Every list reading versions leaves them out without a filter each; the wire is unchanged | A release tag of exactly that form would be hidden |
| (b) Versions with a flag every list filters | | No change of the rows | Every reader of versions must remember the filter |
| (c) Recognise by content only | | Exact | A tag pushed before its content is parsed is a version for a moment |

**Choice:** (a). The OCI distribution specification reserves the form for the referrers tag, and cosign uses it for its own. **Applied in the
spec:** R-SIG-024; [formats/oci.md](formats/oci.md) R-OCI-016.

### Q-SIG-33 · What the background fetch asks for, when and how much

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Top-level manifests only; referrers first, the tag fallback by `HEAD`; at most four signature manifests; a refresh after a day of use; a per-remote switch (chosen) | | No pull more for an unsigned image on Docker Hub; a signature added later arrives | Platform manifests' own signatures only when a client asks |
| (b) Every manifest, platforms included | | Complete | A pull per platform more |
| (c) Once, at the first store | | Fewest requests | A signature pushed an hour after the image never arrives |

**Choice:** (a). **Applied in the spec:** R-SIG-026, R-SIG-040.

### Q-SIG-34 · The keys of a local, a remote and a virtual npm repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Put to the owner
as a question, because a client's result changes ([H-1176](#h-1176--through-a-virtual-npm-repository-npm-audit-signatures-reports-local-packages-as-missing-a-registry-signature)).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Remote: the upstream's keys; local: none; virtual: the union (chosen) | | `npm audit signatures` verifies upstream packages through every class; a keyed publish attestation finds its key | Through a virtual repository with a remote, a local package is reported missing a signature until stage 2 |
| (b) Virtual: no keys | | No "missing" report | Upstream signatures through a virtual repository are not checked, and a keyed attestation aborts npm's run |
| (c) Pull npm's registry signing of local packages into stage 1 | | No gap | Needs stage 2's signing identity |

**Choice:** (a). **Applied in the spec:** R-SIG-028.

### Q-SIG-35 · npm attestations are served below the registry URL, and a local bundle only when npm can verify it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `dist.attestations.url` rewritten to the serving registry URL; a local bundle served only when it verifies against the public Sigstore (chosen) | | npm resolves the URL's path against the registry URL (pacote, verified); npm checks provenance against the public root only, so a company bundle would fail the client | A company bundle is shown in Quader and not to npm |
| (b) Serve every local bundle | | Complete | `npm audit signatures` reports the company's own provenance invalid |
| (c) A host-root route | | The upstream URL unchanged | Cannot know the repository (Q-NPM-1) |

**Choice:** (a). **Applied in the spec:** R-SIG-028; [formats/npm.md](formats/npm.md) R-NPM-027.

### Q-SIG-36 · NuGet lists no repository signatures resource

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Not listed (chosen) | | The resource names the certificates a source signs with itself, and Quader signs none | — |
| (b) A remote passes its upstream's resource through | | A client sees nuget.org's certificates | Binds a virtual repository's packages to one member's certificates; `contentUrl` points at the upstream |

**Choice:** (a). The development agent would revisit it if Quader ever countersigns (excluded by Q-SIG-10). **Applied in the spec:** R-SIG-029.

### Q-SIG-37 · Ansible signatures of a local version arrive through the management API

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Supersedes in part
[Q-ANS-14](formats/ansible.md#q-ans-14--signatures-pass-through-quader-signs-nothing-and-verifies-nothing).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) An operation of the management API takes a detached signature for a local version, checked before it is stored (chosen) | | `ansible-galaxy` uploads none; a CI job posts its signature with its token | An operation of Quader's own |
| (b) Local versions carry none | | Nothing to build | The paper's gap stays |
| (c) A signature file in the archive | | No API | No client reads it |

**Choice:** (a). A remote's entry with `signing_service` is a registry signature, one without a publisher's. **Applied in the spec:**
R-SIG-031.

### Q-SIG-38 · A generic sibling's scheme is read from its content

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The name finds the file; the content tells the scheme (chosen) | | `.sig` holds OpenPGP or cosign's raw signature; the right verifier runs | Sniffing |
| (b) The name tells the scheme | | Simple | `.sig` would be wrong half the time |

**Choice:** (a). **Applied in the spec:** R-SIG-032.

### Q-SIG-39 · Anchors, blocks and the switch are security settings

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `security-settings:write` writes, `settings:read` reads; Owners write project anchors while the switch is on (chosen) | | Trust is an authorization-like decision; the holders are `admin` and `security-admin`, as for the restricted visibility levels; the auditor reads | — |
| (b) `settings:write` | | The malware switch's capability | `security-admin` could not maintain trust |
| (c) A capability of its own | | Separable | A capability and a scope item for the same holders |

**Choice:** (a). **Applied in the spec:** R-SIG-019 to R-SIG-021.

### Q-SIG-40 · The origin of an upload is a snapshot at the item

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context, verified: a CI token's principal is its mapping; the job's claims live in the issuance record, deleted a year after the token
expired.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A row per uploaded item with the principal and, for CI, the issuance's allowlisted claims, written in the commit (chosen) | | Kept as long as the item; the input of stage 2's attestation | A copy of claims |
| (b) Join the issuance record by the audit event's token | | No copy | Gone after the retention; audit retention differs |
| (c) Per version | | Fewer rows | Generic files have no version; companions differ |

**Choice:** (a). **Applied in the spec:** R-SIG-010.

### Q-SIG-41 · A time proof counts only from a log or an authority the scope trusts

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A log entry against a log key of the trust root; a timestamp of an authority of the trust root or under a `timestamping` anchor (chosen) | | A forged timestamp moves no expiry | An administrator adds timestamping roots for company authorities |
| (b) Any timestamp that parses | | Simpler | Backdating by a timestamp of one's own authority |

**Choice:** (a). **Applied in the spec:** R-SIG-007.

### Q-SIG-42 · What the journal carries of signatures

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) What narrows trust: an anchor removed or switched off, material removed, a block, the switch turned off (chosen) | | A restore never brings back trust an administrator withdrew | An anchor added after the backup is lost |
| (b) Nothing | | Simple | A restore revives a withdrawn anchor |

**Choice:** (a), the rule of [authorization.md](authorization.md) R-AUTHZ-031. **Applied in the spec:** R-SIG-039, R-SIG-041.

### Q-SIG-43 · The contract gains a signature method every format implements, at version 8

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `Format.Signatures()` with `Base` returning nil and `Register` refusing nil; `ContractVersion` 8 (chosen) | | "A format without the declaration does not build" (r5) holds at start and in the tests | Every format changes in one change |
| (b) An optional interface | | No version change | A format that forgets it passes silently |

**Choice:** (a). **Applied in the spec:** R-SIG-022; [ADR-228](../decisions.md#adr-228--the-format-contract-declares-every-formats-signatures-and-a-signature-is-never-a-version).

### Q-SIG-44 · The API and the screens

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Signatures as a resource of a version and of a file; a member `signatures` with the badge's state; a fourth settings page; a third dashboard view; a section in a project's settings (chosen) | | The places users already read; plain path one badge, details one click | Three new screens |
| (b) Signatures only in the details of a file | | Fewer screens | No badge in lists |

**Choice:** (a). **Applied in the spec:** R-SIG-034 to R-SIG-038; [api-and-ui.md](api-and-ui.md) R-UI-002, R-UI-158.

### Q-SIG-45 · The bounds and their defaults

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) 256 KiB an object, 32 per subject, chains of 8, 5 s a check, 8 GiB of content (chosen) | | Above every real signature seen (a NuGet `.p7s` with its chain about 20 KiB), below what hurts a worker | A larger real object needs the setting raised |
| (b) No bounds beyond the libraries' | | Nothing to tune | A crafted object reaches the worker's memory |

**Choice:** (a). **Applied in the spec:** R-SIG-013, R-SIG-040.

### Q-SIG-46 · The three ways are tested with real signatures made in process

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The tests make each signature with the ecosystems' libraries and a virtual Sigstore of their own (chosen) | | Real signatures at the unit tier, milliseconds, no service | The real clients are not run |
| (b) A Sigstore stack and a CA in the test cluster | | Real services | Minutes per leg; against "Tests stay runnable" |
| (c) Recorded fixtures | | Fast | Certificates expire; nothing tests the company's ways |

**Choice:** (a). **Applied in the spec:** R-SIG-042.

### Q-SIG-47 · A detector names paths, the core resolves them in the event's repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: R-SIG-022
names a `SignatureEvent` and a candidate with a 32-byte subject, but a generic file's signature can be committed before the file, a
Debian `Release.gpg` before its `Release`, and an npm bundle names its tarball by path; a detector runs no SQL.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A candidate names its object, subject and signed content by a path of the event's repository, a blob's digest or bytes; the core resolves paths in that repository only and drops one whose path holds nothing, which the other side's commit names again; the event carries the content, bounded, from the spool or the object store; a detector's error is logged and never fails a commit; a file Quader derived is not asked (chosen) | `SignatureCandidate.SubjectPath` and `Part`, `SignatureEvent.Content` | A detector stays pure; "never follow a reference out of the request's repository" holds in one place; either order works without a placeholder row; an upload never fails because a signature does not parse | A signature whose subject is not stored has no row until it is |
| (b) A row with a placeholder subject that a later commit fills | | A row from the first commit | A zero digest in a key; every subject's commit must search for placeholders |
| (c) Give the detector a read-only item lookup | | Simple to write | A format reaches storage, against R-ARCH-008 |

**Choice:** (a). A batch replaces the attachments its carrier item (or stored document) and its named locations had, so a replaced file,
a moved tag and a refilled document leave no stale attachment (R-SIG-004). **Applied in the spec:** R-SIG-022, R-SIG-032;
[architecture.md](architecture.md) R-ARCH-008.

### Q-SIG-48 · An OCI signature's object is its manifest, and signed content has a locator

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: the table
of R-SIG-003 keys an object by its own SHA-256 and allows a locator "a layer index"; a referrers tag's index names a referrer by its
manifest's digest, not by its layer's; the candidate's `SignedContent` has no column.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The object of an OCI signature is the manifest that carries it, the part is the layer index; a NuGet signature is the entry `entry:.signature.p7s` of the package with the entry's own SHA-256; the signed content is stored as `content_sha256` and `content_locator` (`unsigned`, `cleartext`, `entry:<name>`), and a message (npm) as `signed_digest` (chosen) | | A referrer and the referrers tag that lists it reach one signature row, without reading a layer at commit; the read finds what to hash without guessing from the format | The read follows the manifest to its layer |
| (b) The object is the layer blob | | The envelope's own digest | The referrers tag cannot name it without reading the manifest; one signature becomes two rows |
| (c) No column for signed content; the read derives it from the scheme and the format | | Fewer columns | Ansible's MANIFEST.json, Debian's cleartext and NuGet's unsigned view would be rules of the read per format |

**Choice:** (a). **Applied in the spec:** R-SIG-003, R-SIG-023.

### Q-SIG-49 · An object above the bound is recorded and born malformed

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: R-SIG-013
says an object above `signatures.max_object_size` is `malformed` (`limits_exceeded`), while the table of R-SIG-003 bounded `size` to
256 KiB, so the row could not exist.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `size` has no upper bound; a stored file above the bound gets its row with a signature born `malformed`, `limits_exceeded`, not due; `raw` keeps its bound, and a document entry above it is not kept (chosen) | | The details can say "Signature could not be read" as R-SIG-013 says; nothing is read | A row for something never read |
| (b) Drop it | | No row | A signature that is there is shown as none |

**Choice:** (a). **Applied in the spec:** R-SIG-003.

### Q-SIG-50 · npm's registry signature has its message as its subject

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: npm's
registry signature signs `<name>@<version>:<integrity>`; a remote's document is stored before its tarball, whose SHA-256 is then not
known (the document names the SHA-512).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The subject row is the SHA-256 of the message, `subject_name` is `<name>@<version>`, and `signed_digest` is that digest (chosen) | | Known at the fill; the key checks the stored digest without the tarball; the message binds the tarball by its integrity | A version's page finds the row by its name, not by its tarball's blob |
| (b) Wait for the tarball and use its SHA-256 | | One join by blob | No row for the versions nobody downloaded; the document's signatures are not where its versions are |

**Choice:** (a). **Applied in the spec:** R-SIG-028.

### Q-SIG-51 · A remote's npm attestations are stored at the path of their endpoint

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The canonical path of a version's attestations document is `-/npm/v1/attestations/<name>@<version>`, the endpoint's path (chosen) | | npm's canonical paths are its protocol paths; the detector and the endpoint name one path | The document is under `-/`, not under the package's path |
| (b) Below the package, `<name>/-/<basename>-<version>.attestations` | | Beside the tarball | A path no client requests |

**Choice:** (a). The package that builds the endpoint stores the document there. **Applied in the spec:** R-SIG-028.

### Q-SIG-52 · The origin is recorded at every file a client sent

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every file of an upload that the client sent records the origin; a file Quader derives from it (`Generated`) records none (chosen) | | The origin says who sent the bytes; a derived file's bytes are Quader's | A derived file's origin is read at its lead |
| (b) Every committed file of an upload, derived ones included | | One row per item | Claims a client wrote bytes it did not |

**Choice:** (a). **Applied in the spec:** R-SIG-010.

### Q-SIG-53 · The signing key is ECDSA P-256 with a code-signing certificate

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The owner is asked
whether his company CA issues ECDSA certificates (the plan's section "With the owner").

Context, verified on 2026-11-21: npm's client accepts registry keys of the type `ecdsa-sha2-nistp256` only (npm's documentation); sigstore-go
accepts a certificate authority's leaf only with the extended key use `codeSigning` (`pkg/root/certificate_authority.go`); cosign matches the
identity against the alternative names; cert-manager issues ECDSA keys of 256 bits and the usage `code signing` (its API reference). The
owner's Q-SIG-13 (a) names one identity "for attestations, OCI and the npm registry key".

| Option | What it means | For | Against |
|---|---|---|---|
| (a) An ECDSA P-256 key; a leaf with `codeSigning` and an alternative name (chosen) | One key for npm and for the attestations | Every verifier involved takes it; cert-manager issues it with two values | A company CA that issues RSA only must add an ECDSA profile, or the installation signs nothing |
| (b) P-256, P-384 or RSA; npm signs only with P-256 | | Any profile of a company CA fits | With another key the local npm packages stay unsigned, against the purpose of Q-SIG-10; two behaviours to explain |
| (c) The certificate for the attestations and a second key that Quader makes for npm | | Any algorithm for the certificate | Two identities, against Q-SIG-13 |

**Choice:** (a). **Rationale:** one identity that every consumer can verify is the owner's frame; P-256 is the one algorithm npm leaves. The
development agent would revisit it if the owner's CA issues RSA only. **Applied in the spec:** R-SIG-045, R-HA-071.

### Q-SIG-54 · Where the key lives in each way, and the Helm values win

Status: **Decided by the development agent** — selected from the options below, within the owner's reading r8 ("Helm wins"); the owner
reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The Helm way: a Secret, existing or issued by cert-manager, mounted into the worker pods only, the key never in the database; the upload: sealed in the database by the key ring; while the values name an identity it is the installation's, an upload is refused and a stored one unused (chosen) | | The Secret way keeps the key out of the database, the backups, `api` and `packages`; cert-manager renews it; declarative | An uploaded key can be opened by every role that holds the key ring and the database ([H-1188](../security/artifact-signatures.md#h-1188--an-uploaded-identity-can-be-opened-by-every-role-that-holds-the-key-ring-and-the-database)) |
| (b) Both ways stored in the database: the chart's pair copied in, sealed | | One code path | The Secret's key lands in the database and in every backup |
| (c) The upload wins over the values | | The administrator's latest word counts | Against r8; a value of the chart overridden without a trace in Git |

**Choice:** (a). **Applied in the spec:** R-SIG-046, R-SIG-047, R-HA-071.

### Q-SIG-55 · The upload is a certificate request for a key Quader makes; an import is a detail

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Quader makes the key and a request; the administrator uploads the certificate the CA issued; an import of a key and its certificate behind **More options** (chosen) | | The plain path moves no secret through a browser or onto an administrator's disk; every CA takes a request | Two steps with the CA between them |
| (b) Only an upload of key and certificate (PEM or PKCS#12) | | One step | The private key passes the browser and the `api` role; a key file lies somewhere |
| (c) Only the request | | Simplest and safest | A CA that hands out key and certificate together, or a key from another installation, cannot be used |

**Choice:** (a). **Rationale:** simple by default, details on demand ([CLAUDE.md](../../CLAUDE.md#product-simple-by-default-details-on-demand));
Debian's keys are generated by Quader the same way, with an import beside it. **Applied in the spec:** R-SIG-047, R-SIG-056.

### Q-SIG-56 · Only the worker holds the key, and a job signs

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It refines Q-SIG-13
(a), "the key only in worker pods".

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The worker alone, by the job `signing` right after the commit (chosen) | | `packages`, which reads clients' uploads, and `api`, which serves the UI, never load the key; one place signs, as Debian's index build does | A just-published npm version is without its signature for about a second ([H-1183](#h-1183--npm-audit-signatures-reports-a-local-version-as-missing-until-quader-has-signed-it)) |
| (b) The role that answers the upload signs at the publish | | No window | The key in every `packages` and `api` pod, the processes that read hostile uploads |
| (c) Both | | | Both exposures |

**Choice:** (a). **Applied in the spec:** R-SIG-049.

### Q-SIG-57 · A new key is announced before it signs, old npm signatures stay, attestations are made again

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context, verified on 2026-11-21 in pacote: npm compares a version's publish time with its key's `expires`, an expired key aborts the run, and a
signature whose key the keys answer lacks aborts it too; a registry that offers keys makes every unsigned package "missing".

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A new identity is listed for `announce_period` while the old one signs; the switch gives the old one its expiry; old npm signatures stay valid and are not made again; attestations are made again; no keys while none is current (chosen) | | No cache of the keys document meets a signature it cannot check; nothing of npm is re-signed; an installation that stops signing reads as one without keys, not as one with missing signatures | Ten minutes before a new key signs; attestations rewritten per rotation |
| (b) The switch at once | | Simpler | A cache that holds the old keys document meets a new signature, and npm aborts the whole run |
| (c) Re-sign every npm version and attestation on every rotation, one key listed | | One key in the answer | A revision of every package per rotation, for nothing: npm accepts the old key with its expiry |

**Choice:** (a). The first identity of an installation is current at once: there is no other to sign meanwhile. **Applied in the spec:**
R-SIG-045, R-SIG-048.

### Q-SIG-58 · npm: Quader signs the versions of local repositories only

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Local versions; a remote's keep their upstream's signatures; a virtual's are its winning member's (chosen) | Stage 1's R-SIG-028 stands | Upstream signatures stay verifiable against the upstream's key; Quader vouches only for what it serves as its own | — |
| (b) Re-sign a remote's versions with Quader's key too | | One key for everything | Hides the upstream's signature and binds npmjs's packages to Quader's key; Quader would vouch as a registry for what it fronts |

**Choice:** (a). **Applied in the spec:** R-SIG-050.

### Q-SIG-59 · npm: signed right after the publish, and what was stored before by a background pass

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A job right after the commit; discovery signs what was published before the identity existed (chosen) | | Every local version ends signed; a publish is visible at once as R-NPM-010 requires | A second or so without a signature after a publish; minutes of backlog after the first identity |
| (b) Hold a version out of the documents until it is signed | | No version reads missing | A pipeline that publishes and installs at once does not find its version |
| (c) Sign new publishes only | | No backlog | Older local versions read missing for ever, through every virtual repository with a remote |

**Choice:** (a). **Applied in the spec:** R-SIG-049, R-SIG-050.

### Q-SIG-60 · npm: Quader's attestations are not served through npm's attestations endpoint

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context, verified on 2026-11-21 in pacote 22.0.1 and sigstore-js: npm verifies attestations with sigstore-js's defaults, one transparency-log
entry and, for a certificate, one certificate-transparency entry, against the public Sigstore trust root fetched from its own TUF mirror only;
a keyed publish attestation's key expiry is compared with its first log entry's time, and a bundle without one fails outside npm's error
handling.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Not in `dist.attestations` and not at npm's endpoint; through the API and a download, verified with cosign (chosen) | | No npm client fails on Quader's bundle | npm users verify the receipt with cosign, not with npm |
| (b) As npm's publish attestation signed with the registry key | | `npm audit signatures` would read it | Needs an entry in the public log; npm's run fails without one |
| (c) As a provenance attestation | | | npm verifies provenance only against the public Sigstore |

**Choice:** (a). **Applied in the spec:** R-SIG-044, R-SIG-054.

### Q-SIG-61 · The statement: in-toto v1 with a receipt predicate of Quader's own, without trust states

Status: **Decided by the development agent** — selected from the options below, within the owner's list of Q-SIG-11 (a); the owner reviews it
at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) An in-toto v1 statement with the predicate `receipt v1`: the receipt, the origin, the integrity states, the malware and the vulnerability snapshots with their dates (chosen) | | Every field stays true or carries its date; the owner's list | A predicate no tool knows; consumers write their policy against it |
| (b) SLSA's Verification Summary Attestation (`https://slsa.dev/verification_summary/v1`, read 2026-11-21) | A known predicate for "a verifier checked this artifact against a policy": verifier, policy, `PASSED` or `FAILED`, verified levels | Tools know it | Its result needs a policy, which stage 3 brings; its levels are SLSA's; receipt and origin have no field |
| (c) Also the trust states | | Complete | Recomputed on every anchor change; a signed copy contradicts the screens |

**Choice:** (a). The development agent would revisit (b) at stage 3: a gate that passes an artifact may issue a verification summary beside
the receipt. Revisited at stage 3 and kept: no verification summary
([Q-SIG-92](#q-sig-92--no-verification-summary-of-the-gates-decision)). **Applied in the spec:** R-SIG-051.

### Q-SIG-62 · The envelope: a Sigstore bundle with a DSSE envelope and the leaf certificate

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A Sigstore bundle v0.3 with a DSSE envelope and the leaf; the chain in the trusted root Quader serves (chosen) | cosign 3's own form | `cosign verify-attestation --trusted-root` verifies it offline (cosign 3.1.4's source); made by a library already in the build | A verifier needs Quader's trusted root file |
| (b) The bundle with the chain | | Only the root to hand out | sigstore-go refuses it unless its verifier opts in; cosign takes it only with `--allow-certificate-chain` (3.1.3 and later) |
| (c) The older cosign layout, an `.att` tag with the chain in annotations | | Older cosign reads it | Verified only through flags cosign deprecated and removed on its main branch |
| (d) A Notary Project signature over an attestation artifact | | Notation users verify | Notation carries no predicate; two signatures per attestation |

**Choice:** (a). **Applied in the spec:** R-SIG-052.

### Q-SIG-63 · No transparency log and no timestamp authority in stage 2

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Put to the owner as a
question, because his own Sigstore (Q-SIG-15) could provide both.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Neither; attestations are made again when the identity changes (chosen) | | No outbound request per attestation; works without internet and without a further service | The time is Quader's claim ([H-1189](../security/artifact-signatures.md#h-1189--the-time-and-the-facts-of-an-attestation-are-quaders-own-claim)); Sigstore's policy-controller in bundle mode cannot verify ([H-1186](#h-1186--a-policy-engine-that-demands-a-log-or-a-timestamp-cannot-verify-quaders-attestations)) |
| (b) An optional RFC 3161 timestamp authority of the company | | A proven time; policy-controller verifies; no rewrite on a renewal | A request per attestation to the authority, a million at the first identity |
| (c) A log entry in the company's own Sigstore | | Evidence of the log | Needs the company's Sigstore in operation, which is asked back to the owner |

**Choice:** (a). The development agent would revisit it when the owner's own Sigstore runs. **Applied in the spec:** R-SIG-048, R-SIG-052.

### Q-SIG-64 · An attestation is made after the first verdicts, and made again on a change of malware state or identity

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) After the checks' first verdicts or a bound of an hour; again on a change of the malware state, of the identity, on a retirement; not on vulnerability or anchor changes (chosen) | | Most artifacts are signed once, with real verdicts | The vulnerability snapshot ages, named so |
| (b) At once at the commit | | Fast | Almost every attestation says "pending" |
| (c) Again on every change, vulnerabilities included | | Always current | Signs most versions every day |

**Choice:** (a). **Applied in the spec:** R-SIG-053.

### Q-SIG-65 · Attestations live in the store of stage 1; OCI as a referrer, every other format through the API

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It widens the
exception of [Q-SIG-22](#q-sig-22--the-original-of-a-signature-that-is-no-item-is-kept-in-postgresql-bounded) to INV-4.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The store of stage 1, the bundle bounded in PostgreSQL; OCI as a referrer; every other format through the API and a download (chosen) | | One model; attachments go with their items; nothing in a user's tree | About 2.5 KiB per artifact in PostgreSQL, 3 GB per million |
| (b) A sibling file in the repository | | The format's client downloads it | Clutters a generic tree, may take a user's name, is read as a publisher's signature by R-SIG-032; npm and NuGet have no place for a file beside a version |
| (c) Each bundle as a blob in the object store | | INV-4 to the letter | A million objects of 2.5 KiB, the reason Q-SIG-22 rejected it |

**Choice:** (a). **Applied in the spec:** R-SIG-054; [README.md](README.md) INV-4.

### Q-SIG-66 · Quader's referrer never stands where cosign would stop reading an older signature

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Put to the owner as a
question, because a client's limit decides it (his rule of 2026-11-19).

Context, verified on 2026-11-21 in the source of cosign 3.1.4 and go-containerregistry 0.22.1: cosign lists every referrer, keeps each
whose single layer is a Sigstore bundle whatever its predicate, and reads the older tags `sha256-<hex>.sig` and `.att` only when it found no
bundle at all, counted before verification and before `--type`; go-containerregistry reads the referrers tag only when the referrers API
answers 404, 400, 406 or something that is no index. A bundle of Quader's beside an older cosign signature therefore makes `cosign verify`
of the publisher fail with "no matching attestations". Notation asks only for its own artifact type and is not affected.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Always a referrer | | cosign and cluster engines find every image's attestation | A publisher's older cosign signature or attestation becomes unverifiable for stock cosign, through every repository that holds the image, against r7 ("nothing breaks an upstream signature") |
| (b) A referrer only where the referrers answer is the API's own and no `.sig` or `.att` tag stands for the digest; otherwise the API; removed when such a tag arrives (chosen) | | Nothing of a publisher's breaks; most images carry the referrer | An image with an older cosign signature gets the attestation through the API only, and a cluster policy that demands both cannot be met with stock cosign ([H-1185](#h-1185--an-image-with-an-older-cosign-signature-gets-quaders-attestation-only-through-the-api)) |
| (c) Never a referrer; the API only | | Nothing can break | No cluster engine finds an attestation, against "for an image as a referrer" (Q-SIG-11) |
| (d) A referrer of a media type of Quader's own, which cosign does not read | | Nothing breaks; always attached | No engine finds it; verifying needs a download and `cosign verify-blob-attestation --digest` |

**Choice:** (b). **Applied in the spec:** R-SIG-044, R-SIG-054, R-SIG-056.

### Q-SIG-67 · The format names what Quader attests, and the contract rises to version 9

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `AttestationSubjects` of the adapter and `Attests` of the declaration; check 18 holds them; `ContractVersion` 9 (chosen) | | A future format attests from its first day (the owner's r5); signature carriers stay out | Every format changes again |
| (b) A core rule: every file item of a version | | No contract change | OCI's layers, npm's provenance bundle and every signature carrier would be attested; generic files have no version |
| (c) Lead items only | | Simple | NuGet's `.snupkg` and an OCI image's tag-less manifests pulled by digest fall out; generic has no lead |

**Choice:** (a). **Applied in the spec:** R-SIG-044.

### Q-SIG-68 · Ansible Galaxy: Quader does not sign local collections in stage 2

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Put to the owner as a
question: by the measure of Q-SIG-10 a check by one command exists (`ansible-galaxy collection verify --keyring`), but he named Debian and npm.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) No registry signature; the archives get the receipt attestation (chosen) | | No key lifecycle per Ansible repository; [Q-ANS-14](formats/ansible.md#q-ans-14--signatures-pass-through-quader-signs-nothing-and-verifies-nothing)'s revisit condition (a client of the owner sets `--keyring`) is not met | A team that sets `--keyring` gets no signature of Quader's |
| (b) Sign with an OpenPGP key per repository as a signing service, entries with `signing_service` in `signatures[]`, as Debian keeps its keys | | One command checks it | Debian's key lifecycle a second time; `ansible-galaxy` checks only with a keyring the user sets, so no client asks by default |

**Choice:** (a). **Applied in the spec:** R-SIG-044.

### Q-SIG-69 · Debian: the repository keys get no certification by the identity

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) No certification; the per-repository keys stay as built (chosen) | | apt trusts a key by `Signed-By` or its keyrings and reads no certification on a key (apt's `sources.list(5)` and `apt-secure(8)`, read for stage 1's research); an X.509 identity cannot certify an OpenPGP key without an OpenPGP key of its own | — |
| (b) An OpenPGP key of the installation certifies every repository key | | A chain of trust in OpenPGP's sense | No client reads it; a second installation key to rotate |

**Choice:** (a). **Applied in the spec:** R-SIG-044.

### Q-SIG-70 · The signing identity is a section of the Signatures settings page

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The first section of Admin area › Settings › Signatures (chosen) | | One page for an administrator's trust work: the company CA as an anchor and Quader's certificate of the same CA side by side | A longer page |
| (b) A page of its own, **Signing** | | Shorter pages | One more entry for one card |
| (c) Under System, beside the key ring | | Near the keys | Trust is a setting (Q-SIG-39) |

**Choice:** (a). **Applied in the spec:** R-SIG-056; [api-and-ui.md](api-and-ui.md) R-UI-002.

### Q-SIG-71 · The client setup gains a tab Verify while an identity is current

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A tab `verify` in the client setup, only while an identity is current (chosen) | | Where a user reads how to work with a repository; absent when there is nothing to check | One more tab |
| (b) Text on the admin page only | | Fewer places | A plain user never sees it |
| (c) The manual only | | | Not found where it is needed |

**Choice:** (a). **Applied in the spec:** R-SIG-055, R-SIG-056; [api-and-ui.md](api-and-ui.md) R-API-030.

### Q-SIG-72 · The trusted root and the chain are public

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Anonymous (chosen) | | Admission controllers and pipelines fetch it without a credential; it is public material, like a CA certificate | It names the company CA's subject to whoever reaches the API |
| (b) For readers of some repository | | Hidden | A cluster needs a token for a public certificate |

**Choice:** (a). **Applied in the spec:** R-SIG-052, R-SIG-055.

### Q-SIG-73 · A hardware or cloud key store is not in scope

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Q-SIG-13 (c), a KMS
or HSM, was not the owner's choice.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Not now: the key in memory and in a Secret or the database; sigstore-go's `Keypair` is an interface, so a store can join later (chosen) | | No integration per product | The key can be copied by whoever reads it ([H-1192](../security/artifact-signatures.md#h-1192--the-signing-key-is-in-memory-and-in-a-secret-or-the-database-not-in-hardware)) |
| (b) PKCS#11 or a cloud KMS now | | The key never leaves hardware | One integration per product; npm's signature needs raw ECDSA from it too |

**Choice:** (a). **Applied in the spec:** R-SIG-045.

### Q-SIG-74 · A restore keeps the uploaded identity, also in another installation

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Kept (chosen) | | A recovery signs on at once, as the installation's ID stays (R-BAK-022) | A copy restored for a test signs as production until removed ([H-1191](../security/artifact-signatures.md#h-1191--a-restore-carries-the-uploaded-identity-into-whichever-installation-restores-it)) |
| (b) Set aside after every restore until an administrator confirms | | No copy signs unnoticed | A recovery stops signing, and npm packages read missing until someone confirms |
| (c) Never in a backup | | | A recovery loses the identity; the backup is incomplete |

**Choice:** (a). **Applied in the spec:** R-SIG-059.

### Q-SIG-75 · The gate is a setting of the repository alone

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the owner chose "per repository a detail setting" (Q-SIG-9 (b)) and did not choose "a switch of the installation" (Q-SIG-9 (c),
against it: "most public packages are not publisher-signed"). The malware gate has an installation switch because the owner asked for one
("Es muss im Admin-Bereich einen Schalter geben", R-MAL-019); for signatures he did not.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The repository's setting alone (chosen) | `signatures.onlyVerified` and `signatures.anchors` per repository | The owner's words; a team gates the repository it curates and nothing else changes; one place to read | An administrator who wants every repository gated sets each one |
| (b) Also an installation switch, as the malware gate's | Every repository gated at once | One place | The owner did not choose it, and most public packages carry no publisher signature: every remote of npmjs or Docker Hub would refuse most of its content |
| (c) Also a project default that its new repositories take | A project's repositories gated by default | Fewer clicks for a curated project | A second place a setting comes from; "simple by default" (CLAUDE.md) |

**Choice:** (a). The development agent would revisit (c) when a project wants every repository gated and sets them one by one. **Applied in
the spec:** R-SIG-061, R-SIG-077.

### Q-SIG-76 · "Verified" is the badge's verified states, and any anchor includes the shipped public ones

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Put to the owner as a
question, because it decides how strong the gate is without chosen anchors.

Context: the shipped anchor `sigstore-public` trusts every identity of the public Sigstore (R-SIG-017), where anyone with an e-mail address or
a CI job of a public forge can sign; npm's provenance is verified against it. The owner's words: "only artifacts with a verified signature,
optionally of chosen anchors".

| Option | What it means | For | Against |
|---|---|---|---|
| (a) What reads **Verified · public** or **Verified · internal** passes; chosen anchors narrow (chosen) | The gate equals the badge | The owner's words to the letter; what shows a green shield is what passes, so a user can predict the gate from the screen | Without chosen anchors, a signature of any identity of the public Sigstore passes (H-1194); the setting says so and offers the choice of anchors beside it |
| (b) Without chosen anchors only **Verified · internal** passes; public anchors only when chosen | A gate that never trusts strangers by default | Stronger by default | An artifact that shows **Verified · public** is refused, and nothing on the screen says why; not what "verified" means on the page |
| (c) Anchors must always be chosen | No "any" | No silent trust | Against "optionally of chosen anchors" |

**Choice:** (a), with the sentence at the setting: "This includes the public Sigstore, where anyone can sign. Choose anchors to accept only
your own signers." **Applied in the spec:** R-SIG-061, R-SIG-062, R-SIG-074;
[H-1194](../security/artifact-signatures.md#h-1194--a-gate-that-accepts-any-anchor-passes-what-any-identity-of-the-public-sigstore-signed).

### Q-SIG-77 · Chosen anchors are named by scope and key, and the anchors' own restrictions narrow

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) References `<scope>/<key>` to anchors of the repository's scope; narrower signers through an anchor with a narrower restriction (a CA anchor's subject, a Sigstore identity's pattern) (chosen) | One concept: an anchor is what the installation trusts a signer by | The repository document stays readable and carries over to another installation as configuration as code (R-OPS-029); no second pattern language in a repository | "Only our project's CI jobs" needs an anchor with that pattern, which an administrator adds, or an Owner while project anchors are allowed |
| (b) Anchor IDs | Exact rows | A re-created anchor is not accepted by accident | Numbers in a document meant to be written by hand and copied between installations |
| (c) Anchors and an identity pattern per repository | A gate narrows the signer itself | No anchor needed per team | A second place where trust is decided, which the anchors' page does not show, and a pattern an Owner could widen |
| (d) Anchor kinds | "Only CA signatures" | Simple | Says nothing about who signed |

**Choice:** (a). A reference to a removed anchor stays and matches nothing (fail closed). An anchor re-created under the same key is accepted
again, as its material can be edited in place anyway (R-SIG-034). **Applied in the spec:** R-SIG-061.

### Q-SIG-78 · Only a publisher's signature or a provenance passes

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It follows the
owner's decision Q-SIG-1 ("the icon stands for what proves the publisher's origin; a registry's signature is in the details").

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The roles `publisher` and `provenance`, the badge's; never a registry's signature alone, never an attestation, never Quader's own (chosen) | | The gate passes what the icon shows; npm's and nuget.org's registry signatures are on every package of those registries and would pass everything a remote of them stores | A NuGet package that only nuget.org signed is refused |
| (b) Also a registry's signature | | More passes | A gate on a remote of npmjs would only say "came from npmjs", which the remote's own TLS fetch says already |
| (c) Per repository, which roles count | | Flexible | A detail of a detail; nobody but an expert could set it |

**Choice:** (a). Quader's own npm signature and receipt attestation never pass, also when the company's CA that certified Quader's identity is an
anchor: Quader signs everything it receives (R-SIG-044), so its statement would pass everything; the gate leaves out every signature with
`identity_id` set. **Applied in the spec:** R-SIG-062.

### Q-SIG-79 · The gate acts at download for every format, not at upload

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context, per format: OCI's signatures are pushed after the image (cosign and Notation name the pushed digest); a generic file's signature is a
file of its own and an Ansible version's arrives through the management API (R-SIG-031); only npm's provenance (in the publish) and NuGet's
signature (inside the package) come with the upload, and the integrity check runs in the worker after the commit (R-SIG-011).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) At download, for every format (chosen) | An upload is stored; its download is refused until it is signed | One mechanism for every format and every class, the place the malware gate and the vulnerability block act; a pipeline pushes, signs, and its deployment pulls | Unsigned content is stored and listed, refused at the download |
| (b) At upload where the protocol allows it (npm, NuGet), at download elsewhere | | The publisher learns at once | Two behaviours for one setting; a check inside the publish request, in `api` and `packages`, which the owner placed in the worker (Q-SIG-5) |
| (c) At upload only | | Nothing unsigned is stored | OCI, generic and Ansible could never be signed at all |

**Choice:** (a). **Applied in the spec:** R-SIG-064.

### Q-SIG-80 · Debian: the gate is not offered

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Put to the owner as a
question, because a protocol's limit decides it (his rule of 2026-11-19).

Context: apt checks the signature of the index, which names each package's SHA-256, and no signature of a package (R-SIG-030); a `.deb`
carries no badge (R-SIG-009), and a Debian remote stores only releases its upstream keys signed (R-DEB-020).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Not offered: the members answer `not_applicable` on a Debian repository, and the section says why (chosen) | | Nothing that cannot pass is offered; the remote's own condition on its upstream's index stays what it is | A team cannot demand through Quader that a remote's index be signed by an anchor the administrator chose |
| (b) A `.deb` passes when a covering release of the remote is verified by an anchor of the scope (the registry's signature counts, for this format alone) | The gate reads the coverage of R-SIG-030 | An administrator's anchors, not a repository writer's `deb.upstreamKeys`, decide what a gated remote serves | Against Q-SIG-1 and Q-SIG-78 for one format; local and virtual repositories sign their own indexes and could pass nothing |
| (c) Offered, and every `.deb` refused | Consistent with the badge | — | A setting that turns a repository off |

**Choice:** (a). **Applied in the spec:** R-SIG-061, R-SIG-063; [formats/debian.md](formats/debian.md).

### Q-SIG-81 · The format names what the gate judges, and the contract rises by one

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A method `Gate` of the signature adapter names per item an artifact (with what covers it), an attachment, a signature file or nothing; `ContractVersion` rises by one; check 18 holds it (chosen) | | A future format is gated correctly from its first day (the owner's r5 of Q-SIG-16); signature carriers stay fetchable; the core decides, the format only classifies | Every format changes once more |
| (b) The route's kind decides, as for the malware gate | `Download` routes are gated | No contract change | An OCI tag is a `Metadata` route and would never be gated; a signature file and an OCI signature manifest would be refused, so no client could verify |
| (c) Stage 2's `AttestationSubjects` stored at the commit | | One classification | It names items of a commit, not the items a download asks for; a platform manifest covered by its index is not a subject |

**Choice:** (a). **Applied in the spec:** R-SIG-063.

### Q-SIG-82 · OCI: the gate is on manifests, an index covers its children, and an attachment is known by its content

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: a signature covers a manifest's digest; `cosign sign` of a tag signs the index of a multi-platform image and its children only with
`--recursive` (cosign's `cmd/cosign/cli/sign/sign.go`, read 2026-11-21); a layer belongs to every image that names it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Manifests by tag and by digest are gated; a platform manifest passes on an index of the repository that lists it; an attachment, recognised by its content, passes; blobs are not gated (chosen) | The vulnerability block's place (R-VULN-037) | A multi-platform image signed at its index pulls; signatures and SBOMs of every image stay fetchable, so a client can verify; a runnable image under an attachment tag's name is still judged | A blob fetched by its digest alone passes (H-1196) |
| (b) Also every blob, passing when a passing manifest of the name references it | | No blob of an unsigned image | A lookup per layer request; a layer shared by a signed and an unsigned image is served anyway |
| (c) Each manifest on its own signature | | Simple | Every multi-platform image signed the usual way is refused at its platform |
| (d) An attachment by its tag's name | | No column | A runnable image tagged `sha256-<hex>` passes the gate |

**Choice:** (a). **Applied in the spec:** R-SIG-063; [formats/oci.md](formats/oci.md) R-OCI-007, R-OCI-009.

### Q-SIG-83 · A virtual repository: its own gate and the holding member's, each in its own scope

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Both gates apply; each judges as its own repository sees the artifact, in its own project's scope; a refusal ends the walk (chosen) | | A gated member stays gated through every virtual repository, a virtual repository's gate holds all it serves (the malware setting's rule, Q-MAL-32); a gate means the same wherever its repository is read | Two judgements where two gates apply |
| (b) The virtual repository's gate only | | One judgement | A member's gate is lifted by reading it through a virtual repository |
| (c) Both, in the virtual repository's scope | | One scope | A member gated on its project's own anchor would refuse everything through a virtual repository of another project |

**Choice:** (a). **Applied in the spec:** R-SIG-065.

### Q-SIG-84 · A narrowing reaches the gate at once by subtraction, a widening after the recomputation

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the trust rows lag a change of anchors by the recomputation (R-SIG-012), minutes for a million signatures (H-1175); a block is the
administrator's answer to a stolen key (Q-SIG-7).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The gate takes away from a stale row the anchors that are no longer in force or were narrowed since it was computed, and applies newer blocks, from the replica's snapshot; a widening waits for the job (chosen) | Two epoch columns | A narrowing is a subtraction and needs no cryptography: it acts within seconds on every replica, also with a million rows behind; a widening that lags only refuses for a while | An anchor whose material was edited counts for nothing on older rows until the job reaches them |
| (b) The gate trusts the stored rows | | Simplest | A blocked key's artifacts are served for the minutes of the recomputation |
| (c) A row below the newest epoch is pending | | Safe | Every anchor added makes every gated download wait or fail until the job is through |
| (d) The gate runs the trust step itself | | Exact | Cryptography and anchor material on the download path |

**Choice:** (a). **Applied in the spec:** R-SIG-066.

### Q-SIG-85 · Pending: priority work and a bounded wait, then 503

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The owner decided
the same for the malware gate (Q-MAL-3).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The pending check and the remote's fetch go first; the request waits up to 25 s from its arrival, then `503` with `Retry-After`; a remote's fill is held (chosen) | The malware gate's pattern | A first pull through a gated remote usually passes in one request; npm, NuGet and apt retry the `503` themselves | Docker and `ansible-galaxy` show the `503` when the checks take longer than the wait, and work on the next try (H-1200) |
| (b) `503` at once | | Simple | Every first pull of a gated remote fails for Docker and `ansible-galaxy` |
| (c) Serve while pending | | No latency | What the gate forbids |

**Choice:** (a). **Applied in the spec:** R-SIG-067.

### Q-SIG-86 · One code for a refusal, one for pending, in the shapes that exist

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `403 signature_not_verified` with a `reason`, `503 signature_check_pending`, in each format's shape of the malware gate's answers (chosen) | | No new shape for any client; the text says the reason in plain words | A script cannot tell `missing` from `untrusted` by the code, only by the reason member where the shape carries it |
| (b) A code per reason | | Exact | Six codes each format's kit case must render |
| (c) `404` for a refused artifact | | Hides it | A user debugs a missing package that is listed |

**Choice:** (a). **Applied in the spec:** R-SIG-068.

### Q-SIG-87 · One decision, one wait, in the order of what was found before what is missing

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The three checks decide before any wait; the first refusal in the order malware found, vulnerability block, signature, malware not checked, data missing; one wait for every pending check (chosen) | | No request waits 25 s for a scan and is then refused for a missing signature; the most serious finding names the refusal; one bound for the request inside apt's timeout | The malware gate's `Check` is split into its decision and its wait |
| (b) One after the other, each with its own wait | As built for the malware gate | No change to it | Two waits can add up past apt's timeout; a refusal known at once comes after a wait |
| (c) The signature gate first | | Cheap | A refusal for a missing signature hides malware found in the same file |

**Choice:** (a). **Applied in the spec:** R-SIG-069; [malware-scanning.md](malware-scanning.md) R-MAL-013.

### Q-SIG-88 · A release per repository and artifact, by whoever triages in the security dashboard

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Q-SIG-9 named
"releases" among what the gate brings; the owner's note at the malware gate placed releases in the security dashboard ("Funde gehen in
Security-Dashboard", Q-MAL-5).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A release of one artifact in one repository, by the holders of `vulnerabilities:triage` with `read` on it or `content:metadata-read`, in the security dashboard and at the refused version (chosen) | The triage rule of R-VULN-013 | An urgent unsigned artifact does not force the gate off for the whole repository; another repository's gate is untouched | A release of an artifact whose signer was blocked serves it again, by decision |
| (b) No release: sign it or switch the gate off | | The gate means what it says | The first urgent case switches the gate off for everything |
| (c) Per digest for the whole installation, as the malware release | | One decision per bytes | A team's gate would be lifted by a decision about another repository |
| (d) By the repository's Owner | | Fast | A release weakens the gate, which the Owner may not (R-SIG-061) |

**Choice:** (a). **Applied in the spec:** R-SIG-070, R-SIG-073, R-SIG-074.

### Q-SIG-89 · Metadata documents list what the gate refuses

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The documents are unchanged, as for the vulnerability block (R-VULN-015, H-311); the download is refused with the reason (chosen) | | One rule for every block; the documents and their caches do not depend on anchors, which change; the refusal names the reason | A client that resolves a range takes the newest version and fails, instead of an older signed one (H-1199) |
| (b) Refused versions are left out of the documents | Curation | npm picks a signed version by itself | A document per gate state and caller, invalidated on every change of anchors, and a silent downgrade that nobody asked for |

**Choice:** (a). **Applied in the spec:** R-SIG-077.

### Q-SIG-90 · A preview before switching on, and an impact before a narrowing, both bounded

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The repository's preview applies the gate to its stored items; the impact counts the gated artifacts an anchor's or a block's change refuses; both at most 100,000 rows or items (chosen) | | Nobody locks a team out unknowingly; an administrator sees what a block costs before the incident becomes an outage | Two reads that can be large; beyond the bound the numbers are lower bounds, said so |
| (b) A count of unsigned versions only | | Cheap | Ignores chosen anchors, covering indexes and releases: not what the gate does |
| (c) No preview | | Nothing to build | The owner's concern of a team locked out |

**Choice:** (a). **Applied in the spec:** R-SIG-071, R-SIG-072, R-SIG-074.

### Q-SIG-91 · No report mode: the preview shows what the gate would refuse

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The malware gate has
none either (Q-MAL-36); the catalog entry BEYOND-019 names "off, audit or enforce".

| Option | What it means | For | Against |
|---|---|---|---|
| (a) On or off; the preview before switching on, and stage 1's display and alarm, which already record every state (chosen) | | One meaning of "on"; what an audit mode would record, stage 1 shows | A team cannot watch real requests being "would-be refused" |
| (b) A third value `report` that records would-be refusals and serves | | Measured on real traffic | A third state per repository to explain, and a gate that looks on and is not |

**Choice:** (a). **Applied in the spec:** R-SIG-061, R-SIG-077.

### Q-SIG-92 · No verification summary of the gate's decision

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It is the revisit
Q-SIG-61 named for stage 3.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) None: the receipt attestation stays as it is and carries no decision of the gate (chosen) | | The decision follows the anchors and changes with them; a signed "passed" would contradict the screens after the next change, as the trust state would (Q-SIG-61); a consumer checks the original signatures itself | A cluster cannot demand "passed Quader's gate" by a signature |
| (b) SLSA's verification summary for every artifact that passes | | Policy engines know it | Made again on every change of anchors; "passed" stays signed after a block |

**Choice:** (a). **Applied in the spec:** R-SIG-077.

### Q-SIG-93 · A project anchor counts for the gate of its project's repositories

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The administrator
decides whether project Owners add anchors (the owner's r4 of Q-SIG-16, default off).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) While project anchors are allowed, a project's anchors are in "any anchor" of its repositories and can be chosen (chosen) | The switch's meaning: the project's anchors apply to its repositories | A team that signs with its own key gates its own repository; with anchors chosen by an administrator an Owner cannot add its own (a weakening) | Under "any anchor" an Owner's own anchor lets the Owner's signatures pass the gate (H-1195) |
| (b) Never for a gate | | An Owner never passes the gate by itself | The administrator's switch would mean less for the gate than for the badge |
| (c) Only when chosen explicitly | | Visible | "Any" and a list would not be ordered by inclusion, so neither direction of a change would be a clear narrowing |

**Choice:** (a). **Applied in the spec:** R-SIG-061;
[H-1195](../security/artifact-signatures.md#h-1195--under-any-anchor-a-project-owners-own-anchor-passes-the-gate-of-its-projects-repositories).

### Q-SIG-94 · The Bastion learns a narrowing as content entries for the gated repositories

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A removed or narrowed anchor, a block and project anchors switched off yield `content` for every repository whose gate is on; a withdrawn release for its repository (chosen) | The feed of R-BAS-015 | A Bastion stops serving its copy of what the main installation now refuses at its next feed read; only gated repositories are emptied | A block empties the caches of every gated repository, also of artifacts the block does not touch |
| (b) None; the Bastion learns at its revalidation | As for the malware switch (H-1102) | No cache emptied | A stolen key's artifacts are served by Bastions for up to ten minutes after the block |

**Choice:** (a). **Applied in the spec:** R-SIG-075; [bastion.md](bastion.md) R-BAS-015.

### Q-SIG-95 · No end-to-end case, one measurement by hand

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The gate tested at the unit tier and against the stack; the answers' shapes by the kit; the real clients measured once by hand (chosen) | | The gate is the same for every format and adds nothing to any format's leg ("Tests stay runnable"); the answers' shapes are the malware gate's, which real clients were read for | No pipeline runs `docker pull` against a gated repository |
| (b) One case in `e2e (oci)`: a signed image pulls, an unsigned one is refused | | A real client on every OCI change | About 5 s on a leg for what the integration tier already shows |

**Choice:** (a). **Applied in the spec:** R-SIG-078.

### Q-SIG-96 · The ClamAV definitions declare no signature: their containers' signature is the import's

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Taken when the
format `clamav` (a system format, [formats/clamav.md](formats/clamav.md)) met the contract's version 8, which asks every format for its
declaration.

Context: a ClamAV container carries an RSA signature in its header over the MD5 of its archive (libclamav's legacy check, a 1024-bit key
and a digit encoding of its own), and ClamAV 1.5 publishes a detached `<db>-<version>.cvd.sign` of PKCS#7 signatures. The import checks the
header's signature against ClamAV's key before it stores a container and refuses every other (R-MAL-033); the version shows the outcome as
its verification (R-CLAMAV-003); `clamd` and every `freshclam` check both signatures again. The store's `public-key` scheme verifies a raw
signature over the subject's SHA-256 with an anchor of 2048 bits or more (R-SIG-007, R-SIG-012).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `NoSignatures` with the reason that the container's signature is ClamAV's file format, checked by the import and shown as the verification (chosen) | The format's row says none, with its reason in the details | One place says whether a container is ClamAV's, the one that refuses it before it is stored; nothing is shown twice in two vocabularies; no detector reads 90 MB containers again | The badge of R-SIG-009 never appears on a definition file; the verification row of the version says it instead |
| (b) A `publisher` kind of the scheme `public-key`, the header's signature as the object, and ClamAV's key as an anchor of the installation | The store holds every container's signature | The definitions look like every other signed artifact | The scheme verifies over SHA-256, ClamAV signs an MD5 in an encoding of its own, and its key has 1024 bits, which the anchors refuse: every container would read "Signed · signer not trusted" beside a verification that says it verified |
| (c) A kind of the scheme `unknown` for the `.cvd.sign` | The detached signature detected | Something in the store | Every version would read **Scheme not supported**, for files ClamAV's own tools check |

**Choice:** (a). **Rationale:** a definition file reaches the repository only after its signature verified, so the store would add a
second, weaker statement of a fact the import already decided. The development agent would revisit this if the store learns PKCS#7 for the
`.cvd.sign` (Q-MAL-95's revisit condition) or a client of the format asks Quader for signatures.
**Applied in the spec:** R-SIG-002; [formats/clamav.md](formats/clamav.md) R-CLAMAV-001.

### Q-SIG-97 · The background fetch is a job of the worker with a state row per target

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: R-SIG-026
says what is fetched and how much, not where it runs; "per remote and replica" bounds it; a refresh needs the time of the last fetch.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The format's data port queues the job `signature-fetch` (queue `index`, coalescing per target) in the transaction that stored the manifest or the tarball; a row of `signature_fetches` per target holds when it was queued and fetched, so a target is fetched once and again only as a refresh after `refresh_after`; the worker runs it with the upstream of one run (`Host.BackgroundUpstream`) and the format's `formats.SignatureFetcher`; a budget per remote on the worker bounds the requests per hour, and a spent budget queues the target again after the wait (chosen) | `internal/signatures/fetch`, migration 00096's `signature_fetches` (transient for backups), `RemoteCaps.Signatures` | Durable across restarts; the fill commits nothing that is lost; the engine's background rules hold; one kind for every format, found by the capability | A row per fetched target; a job per stored top-level manifest |
| (b) A goroutine of the replica that filled | | No table, no job | Lost on a restart; runs on web replicas beside clients; no refresh state |
| (c) The client's request waits for the fetch | | Simplest | A pull costs the upstream's latency again |

**Choice:** (a). The metric `quader_signatures_remote_requests_total` of R-SIG-039 is the observer the handler takes
(`HandlerOptions.Observe`); the package of the metrics registers it. The settings of R-SIG-040 (`signatures.remote.*`, the repository member
`signatures.fetch`) are read with their defaults: the job reads `signatures.fetch` from the repository document; the configuration keys and
the member's schema are the package of R-SIG-040's. **Applied in the spec:** R-SIG-026.

### Q-SIG-98 · A HEAD goes through the background upstream, and "no referrers API" is a 404 or a 405

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `discover.Header` (`Head`) on the upstream of a pass and of a run, through `proxy.Get` with the method HEAD; a referrers request answered 404 or 405 is a registry without the API; at most eight layers per signature manifest, each up to `signatures.max_object_size`, and never an SBOM's (chosen) | | A HEAD costs no pull on Docker Hub; distribution answers 404, some registries 405 for a route they lack; cosign's older layout holds one signature per layer, eight bound a hostile manifest | A registry that answers 400 for the route is read as having no referrers and is not asked for its referrers tag |
| (b) Fetch the tag documents by GET | | No new request kind | A pull counted for every image without a signature |

**Choice:** (a). **Applied in the spec:** R-SIG-026.

### Q-SIG-99 · A mirror asks for the signatures of every kept tag in every pass

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The plan of each kept tag runs the fetch with the pass's upstream, every pass; an attachment tag of the upstream's list is no version of the plan; the fill of a newly stored manifest queues the job as well (chosen) | | A signature added upstream after the first pass arrives with the next pass; the pass's window and slots bound it | A pass asks one referrers request and two HEADs per kept tag; a newly stored tag's signatures are asked twice, by the plan and by the job |
| (b) Only the job of the fill | | Fewer requests | An unchanged tag never asks again: a later signature never reaches a mirror nobody pulls from |

**Choice:** (a); the development agent would revisit the doubled first request if a mirror's pass shows it in the upstream's rate limit.
**Applied in the spec:** R-SIG-025.

### Q-SIG-100 · A refresh is queued by a client's tag whose revalidation kept the document

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The handler of a tag's GET queues a refresh when the engine forwarded the request as stale and stored nothing (Cache-Status `fwd=stale` without `stored`); the row's time decides whether the job runs (chosen) | | The revalidation is once a metadata period per tag; the format needs no hook of the engine | A statement per revalidation of a tag |
| (b) A hook of the engine on an unchanged revalidation | | Exact | A contract change of the engine for one format |

**Choice:** (a). **Applied in the spec:** R-SIG-026.

### Q-SIG-101 · Evicting a remote's manifest takes the items of its attachments

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: a remote's
attachment tags are cache entries (tag documents) with an alias item of their manifest; a referrer is an alias item of its own.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The removal of a remote's manifest item (not into the trash) takes the alias items of its stored referrers and of the manifests its attachment tag documents hold; the tag documents stay until their period ends (chosen) | | The format removes what it can through the cascade it has; a tag document left is served as it was and replaced by the next fill | A tag document of an evicted image stays a period |
| (b) Remove the tag documents too | | Complete | A format hook removes no cache entry |

**Choice:** (a). **Applied in the spec:** R-SIG-025.

### Q-SIG-102 · A posted Ansible signature is checked without a key and recorded for the job

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: R-SIG-031
says a posted signature is checked before it is stored; the key of an anchor and the OpenPGP verifier are the trust step's (package 2), and
an OpenPGP signature cannot be matched with its content without a key.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The operation checks what needs no key: one armored signature packet within the bounds, an issuer fingerprint (the key it names, `pubkey_fingerprint`), and the left 16 bits of the hash it signs against the version's `MANIFEST.json`; it stores the signature in `ansible_version_signatures` and records it with the source `upload`, priority 0, for the job's check with an anchor's key; the path names the collection as `<namespace>.<name>` and the permission is `deploy` (`delete`) on the collection (chosen) | | A signature of other content is refused at once (one in 65,536 passes the prefix); no verifier outside `internal/signatures`; the version document renders from the format's own rows; the permission form of the API names one package value | A forged packet with the right prefix is stored and reads `invalid` after the job; the package claim of R-AUTHZ-018 is not asked again |
| (b) Wait for the job's verdict in the request | | An invalid signature never stored | The request depends on the job and its anchors |
| (c) No check before the job | | Simplest | A typo of the file signed is found only in the details |

**Choice:** (a). A signature carries one key, so a block of several packets is refused and posted one by one. **Applied in the spec:**
R-SIG-031; [api/openapi.yaml](../../api/openapi.yaml) `addAnsibleVersionSignature`, `removeAnsibleVersionSignature`.

### Q-SIG-103 · An npm local document carries no dist.attestations in stage 1

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: R-SIG-028
says a local version with a servable bundle carries `dist.attestations`; a local document is rendered once per revision and cached
(R-NPM-013), and a verdict of the trust step changes no revision.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The attestations endpoint of a local repository answers the bundle the trust step vouches for (`Host.Signatures().PublicProvenance`), and the local document names no `dist.attestations` until stage 2, when Quader's own attestation joins it; a virtual repository answers the attestations of its first member in resolution order (chosen) | | No document whose cached rendition contradicts a later verdict; npm never asks an endpoint that would answer 404 | npm does not verify a local package's provenance through Quader in stage 1 |
| (b) Render `dist.attestations` with the verdict and key the rendition by a verdict epoch | | Complete | A statement per document request on the hot path, and an epoch the trust step must raise |

**Choice:** (a); revisit with stage 2's signing of local packages. **Applied in the spec:** R-SIG-028.

### Q-SIG-104 · The image scanning leaves out what is a signature in its repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The selection leaves out a manifest with a subject and a manifest that is the object of a signature attached in its repository (cosign's older layout, BuildKit's attestation manifests) (chosen) | | The detector's recognition decides, as R-SIG-024 asks; one statement of the selection | A manifest selected before its attachment was recorded keeps its scan row |
| (b) Media types alone | | No join | An older cosign signature carries an image configuration |

**Choice:** (a). **Applied in the spec:** R-SIG-024; [vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-033.

### Q-SIG-105 · Debian's coverage is an optional interface of the format over the remote's parsed rows

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `formats.SignatureCoverage` (`Covering(ctx, data, repo, path, sha256)`), read by `CapabilitiesOf`; Debian answers for a remote from `deb_remote_entries` by the file's path and SHA-256 joined to the stored releases; a local or virtual repository covers nothing here, its details name its own key (chosen) | | The API of package 2 asks one capability for every format; the index of entries by path serves it | A package no parsed index lists has no covering release until the parse ran |
| (b) A method of the data port the API reaches directly | | No contract change | The API would name the Debian format |

**Choice:** (a). **Applied in the spec:** R-SIG-030; [architecture.md](architecture.md) R-ARCH-008.

### Q-SIG-106 · One table of fetch state for both stages, keyed by target, with the subject beside it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: stage 1's
background fetch (R-SIG-026) keeps its state per target; stage 3's gate (R-SIG-067) had specified a table of the same name keyed by the
subject, with `due`, `asked`, `failed` and `not_before`. Two tables of one fact drift; the gate is specified, not built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Stage 1's `signature_fetches` is the one table: keyed by repository and target (OCI `<name>@sha256:<hex>`, npm `<name>@<version>` and `keys`), with the item's digest in `subject_sha256` and an index on it for the gate; `queued_at` while due or running, `fetched_at` for the last run that asked to the end (found or not), `attempts` and `failed_at` for runs that failed, written by the job; the retry and its pause are the job queue's; a row goes with its OCI manifest's row; the gate's `asked` is a priority of the waiting job, which its builder raises (chosen) | Migration 00096; `SignatureFetchFailed`, `SignatureFetchForget`; R-SIG-067 reads it | The gate tells "due or running" (`queued_at`, no `fetched_at`) from "fetched, nothing found" (`fetched_at`, no attachment) and from "failed" (`failed_at`) without a column more; npm's keys, which belong to no item, have a row; the failure count and the failed mark the gate needs cost nothing later | One subject can have rows under several image names, so the gate reads them together; the gate's builder adds the statement that raises a waiting job's priority and the signal after a run |
| (b) Stage 3's shape: keyed by subject, with `due`, `asked`, `not_before` | | The gate's own words | npm's keys have no subject; `not_before` and `due` repeat what the job queue keeps (its backoff, its waiting job); a fetch per image name needs the target, which this key loses |
| (c) Two tables: the fetch's per target, the gate's per subject | | Each stage owns its table | Two writers of one fact, kept equal by hand |

**Choice:** (a). **What the gate's builder adds:** the raise of a waiting `signature-fetch` job's priority (an enqueue of a key that waits
returns the waiting job unchanged today), the signal `signature_verdict` after a run's commit, and nothing in this table. **The row's
lifetime:** an OCI row goes with its manifest's row in the data port's `RemoveManifest`, one statement in the transaction of the removal,
so a manifest stored again is fetched again; with its repository by the cascade and the purge of a retired well-known repository; an npm
row stays when the cache loses its tarball, since the npm hook of that removal has no transaction of the data port
([H-1201](../security/artifact-signatures.md#h-1201--an-npm-versions-fetch-state-outlives-its-tarball)). **Applied in the
spec:** R-SIG-026, R-SIG-067, R-SIG-076.

### Q-SIG-107 · Posting and removing an Ansible signature are audit events of the format

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: the
operations of R-SIG-031 change what a version document serves and what the signature store holds; R-SIG-039's events are package 2's and
name no format's write.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `ansible.signature.added` (`name`, `version`, `fingerprint`, `replaced`) and `ansible.signature.removed` (`name`, `version`, `fingerprint`), events of the Ansible format beside `ansible.version.deleted`, transactional, content category, webhooks on, not journaled, written in the transaction of the change; never the signature (chosen) | `internal/audit/events_ansible.go`, the data port's `AddSignature` and `RemoveSignature` | Who signed what, and who took a signature away, is in the audit log; the names follow the format's own events; a restore returns the rows with their snapshot, so nothing needs a replay | Two event types more in the catalog and the UI's labels |
| (b) Generic `signature.attached` / `signature.detached` of R-SIG-039 | | One pair for every source | Package 2's events, for every detection, millions of rows on a busy remote |
| (c) No event: `package.published` covers the version | | Nothing to add | A removed signature leaves no trace |

**Choice:** (a). **Applied in the spec:** R-SIG-031, R-SIG-039; [formats/ansible.md](formats/ansible.md) R-ANS-030.

### Q-SIG-108 · The fetch switch waits for the member of R-SIG-040, and the manual promises none

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: the job
reads `signatures.fetch` from the repository document (default `true`); the document refuses an unknown member (R-REPO-008), and the member
`signatures` with its schema, its validation and its place in the settings is R-SIG-040's, package 2.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Stage 1's package 3 adds no member; until package 2 builds `signatures` every remote fetches, and the manual says nothing of a switch (chosen) | The user manual names the bound of 600 requests an hour instead | No half member that package 2 must reshape; the manual promises nothing an administrator cannot do | A remote with a tight upstream cannot be switched off before package 2 |
| (b) Add `signatures: {fetch}` to the document now | | The switch works at once | A member whose schema, settings section and UI are another package's, written twice |

**Choice:** (a). **Applied in the spec:** R-SIG-026; [manual/user/container-images.md](../manual/user/container-images.md).
**Built with package 2:** the member `signatures: {"fetch": …}` is settable on a remote repository and validated (`not_applicable` on a
local or a virtual one, R-SIG-040), and the user manual says again that an administrator can switch a remote's fetch off.

### Q-SIG-109 · The trust step judges the evidence the check stored

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: the
trust step reads no blob, no item and no upstream (R-SIG-012), but a Sigstore bundle, a Notation envelope and a NuGet `.p7s` live in a
layer, an item or a package, and the signer columns are bounded to 64 KiB of what the screens show.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A column `signatures.evidence` (bytea, at most 512 KiB) holds what the check found the trust step needs: the chain as carried, the timestamps that matched, a Sigstore bundle with the digest it was checked against, a keyless scheme's signature (chosen) | `scheme.Evidence` | An anchor change judges every signature from PostgreSQL alone, with the object store stopped; one shape for every family | A copy of what the object holds, up to 512 KiB a row |
| (b) The trust step reads the object again | | No copy | Reads the object store on every anchor change, against Q-SIG-5 |
| (c) Keep the evidence in `signer` | | One column | 64 KiB is too small for a bundle with an SLSA statement; the API would carry DER and bundles |

**Choice:** (a). Evidence above the bound is dropped and logged; the trust step then finds no signer for that signature (it reads
untrusted). **Applied in the spec:** R-SIG-003, R-SIG-012; migration 00098.

### Q-SIG-110 · What the store decides of mixed verdicts, epochs and narrowing writes

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) An anchor key that refutes a `no_key` signature in any scope makes it `invalid` for all (the bytes decide); the epoch rises only when a write changes a value; a project anchor's count is its project's rows; a narrowed update replayed by the journal switches the anchor off; the trust root's failure event is kept once a day across replicas in the row (chosen) | | The integrity verdict stays a fact of the bytes; no recomputation for a write that changes nothing; a restore never revives trust an administrator narrowed | A replayed narrowing switches off an anchor that may also have gained material |
| (b) Per-scope integrity, an epoch per write, a count of installation rows only | | Simpler code | Two integrity verdicts for one signature; installation rows never hold a project anchor, so its count would always be 0 |

**Choice:** (a). **Applied in the spec:** R-SIG-012, R-SIG-016, R-SIG-039, R-SIG-041.

### Q-SIG-111 · Discovery walks the items by a cursor and starts a new pass once a day

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: "items
that have no attachment yet" are almost every item, because most content carries no signature.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A cursor over items in id order in `signature_settings`; a pass that reached the last item starts again from the first once a day (chosen) | | Each item is asked once a day at most; a row removed is found again; a restore that loaded items before the cursor is reached by the next pass | A signature removed by hand reappears only on the next pass |
| (b) Every run over every item without an attachment | | Literal | The detectors run over every item every five minutes |
| (c) One pass only | | Cheapest | A row removed is never found again |

**Choice:** (a). **Applied in the spec:** R-SIG-011.

### Q-SIG-112 · The first step of the check that fails decides

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The steps run in one order (parse and bounds, read, weak algorithm, subject or digest, chain, signature, log entries and timestamps) and the first that fails gives the state and reason (chosen) | | A changed payload breaks the signature too; checking the subject first shows the more useful `subject_mismatch` | One reason where several apply |
| (b) Every failure listed | | Complete | A list of reasons the screens cannot say in one sentence |

**Choice:** (a). A message digest other than SHA-256 or SHA-512 and a DSSE payload other than in-toto are `unsupported`
(`version_unsupported`); a bundle version outside 0.1 to 0.3 is refused before it is parsed. **Applied in the spec:** R-SIG-006.

### Q-SIG-113 · cosign's older layout is converted to a bundle of version 0.1

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Each layer and its annotations become a Sigstore bundle of version 0.1, which allows an inclusion promise without a proof and a chain beside the leaf (chosen) | | One verifier for both layouts; an identity anchor judges both | A bundle Quader made, not the publisher |
| (b) A verifier of its own for the older layout | | No conversion | Two paths of security code |

**Choice:** (a). **Applied in the spec:** R-SIG-005, R-SIG-023.

### Q-SIG-114 · A trust root knows a log by its key ID and an authority by its key

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A log by its key ID; a timestamp authority by its key: the authority's leaf, or a certificate its chain issued, verifies the token (chosen) | | A token that only names an authority proves nothing and cannot make a good bundle invalid | A token of an unknown authority is silently no proof |
| (b) By the authority's name and serial | | Simple | Anyone can name an authority |

**Choice:** (a). `proven_at` is the earliest time a root verified, `claimed_at` the earliest time none verified. **Applied in the spec:**
R-SIG-007.

### Q-SIG-115 · What a keyless check can tell without a key

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) An OpenPGP digest that misses the packet's hash tag is `invalid` without a key; a `public-key` signature over other bytes stays `no_key` (chosen) | | A replaced file under an OpenPGP signature shows at once; a raw signature names no key, so "other bytes" and "unknown key" cannot be told apart | The `public-key` case cannot meet R-SIG-006's "invalid for a flipped content byte" |
| (b) Both stay `no_key` until a key is known | | Uniform | Hides a replaced file the hash tag already shows |

**Choice:** (a). **Applied in the spec:** R-SIG-006.

### Q-SIG-116 · Every NuGet part checks the primary signature and the package

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A countersignature's part checks the primary signature and the unsigned-package hash too (chosen) | | A valid countersignature never reads "Origin confirmed by registry" on altered bytes | Each part reads the package once |
| (b) A countersignature checks only its own signature over the primary's value | | Cheaper | Vouches for bytes nobody checked |

**Choice:** (a). The attribute rules: signing-certificate-v2 on every signer, content-type on the primary, a repository signature's service
index, all required (`attribute_missing`); a primary signature without a commitment type is an author's; a countersignature is proof of
receipt; the signature content takes CRLF or LF; a Zip64 package is `unsupported`. **Applied in the spec:** R-SIG-006, R-SIG-029.

### Q-SIG-117 · A CA anchor's leaf carries codeSigning explicitly

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The leaf names `codeSigning` in its extended key use (chosen) | | A web server's certificate without the use signs nothing (Q-SIG-30) | A company CA that leaves the use out must add it |
| (b) `crypto/x509`'s rule: a leaf without any extended key use passes | | Fewer refusals | Every certificate of the CA without the extension becomes a signer |

**Choice:** (a). **Applied in the spec:** R-SIG-008.

### Q-SIG-118 · The time of a signature in a scope

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The trust step computes the time of each scope from the evidence (the earliest timestamp under a `timestamping` anchor of the scope; a Sigstore anchor's verified log time), the rows keep no time, the signature keeps the time the check found against every root and authority (chosen) | | Q-SIG-41 holds per scope; no column per row | The details show the check's time, not the reader's scope's |
| (b) A proven time per trust row | | Exact per reader | A column per row for a rare difference |

**Choice:** (a). **Applied in the spec:** R-SIG-007, R-SIG-008.

### Q-SIG-119 · What a block matches

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The signature's key IDs and the keys of the anchors that verified it; hexadecimal fingerprints and key IDs whatever their case, npm's keyid exactly; a serial as `<issuer DN>\|<serial>`; an identity under its required OIDC issuer, anchored; a `no_key` signature too (chosen) | | A block finds the signer by any name it carries | A block of a short key ID can match more than one key |
| (b) Exact values only | | Predictable | An upper-case fingerprint pasted from a tool would block nothing |

**Choice:** (a). **Applied in the spec:** R-SIG-008, R-SIG-020; [http-api.md](../developer/http-api.md).

### Q-SIG-120 · The shipped anchors' titles, presets and the version a refresh compares

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The presets `npmjs`, `debian`, `ubuntu`, `nuget-org` (both of nuget.org's anchors); titles in plain words; a refresh compares the TUF snapshot's version; go-tuf's updater from sigstore-go's embedded root with the task's clock; `NPMKeys` reads the TUF target's shape and the registry's `-/npm/v1/keys` (chosen) | | npm's keys change in a delegated role that leaves `targets.json` alone; sigstore-go's client reads the wall clock | A version that rises with every timestamp |
| (b) The targets' version, sigstore-go's client | | Fewer lines | A key change of npm is missed; no reference time |

**Choice:** (a). **Applied in the spec:** R-SIG-017.

### Q-SIG-121 · The bounds and checks of anchor material the table leaves open

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The checks listed under R-SIG-015's table: key usage and extended key use, pattern and value bounds, a keyid's format, expired public keys kept, OpenPGP algorithms by the key's purpose, a Sigstore issuer required on save (chosen) | | A certificate that can never take part in a signature is refused on save; npm's old key judges old signatures | More refusals to explain |
| (b) Only the table's checks | | Shorter | A TLS-only certificate saved as a code-signing anchor |

**Choice:** (a). **Applied in the spec:** R-SIG-015.

### Q-SIG-122 · Anchors of the configuration are a map and their material a mounted file

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `signatures.anchors` is a map keyed by the anchor's key; material inline or as `*_file` the chart mounts from a `<member>From.configMapKeyRef` into the migrate Job alone (chosen) | | The configuration framework holds no nested lists (R-ARCH-020); a duplicate key cannot be written; replicas read no material | The values keep the list form, the file the map |
| (b) A list as the values, with the framework changed | | One shape | A change of the shared framework for one section |

**Choice:** (a). **Applied in the spec:** R-SIG-018, R-SIG-040; [ha-and-helm.md](ha-and-helm.md).

### Q-SIG-123 · The API: the project list, the virtual members and the original

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A project's list carries the installation's applied anchors read-only; a virtual repository's answer lists every member that holds the version and the caller reads, judged in the virtual repository's project; attestations stand apart from signatures; IDs in paths are digit text; an original is served as an attachment with `nosniff` and a sandboxing policy (chosen) | | A member sees the whole scope without `settings:read`; a media type the uploader chose is never rendered | A longer answer |
| (b) The UI reads the installation's list; only the winning member | | Shorter | Members hold no `settings:read`; a signature of a member that does not win stays hidden |

**Choice:** (a). **Applied in the spec:** R-SIG-034; [http-api.md](../developer/http-api.md).

## What this does not cover

The gaps of the mechanisms are on [security/artifact-signatures.md](../security/artifact-signatures.md#what-this-does-not-cover). This page
records those about a client's result.

### H-1176 · Through a virtual npm repository, `npm audit signatures` reports local packages as missing a registry signature

Component: the keys endpoint of a virtual npm repository (R-SIG-028, Q-SIG-34). Subject: a project whose virtual npm repository lists a local
repository and a remote of a registry with keys (npmjs). Live from stage 1 until stage 2 is built, and after that in an installation without a
signing identity. The union of the members' keys makes npm treat the virtual repository as a registry with keys, and a package of the local
member carries no registry signature without stage 2's signing, so `npm audit signatures` lists it as missing and exits 1; upstream packages
verify. What makes it real: a pipeline that runs `npm audit signatures` against such a repository. Stage 2 closes it for an installation with a
signing identity: Quader signs every local version and its keys join the union (R-SIG-050); what remains of it there is the window of
[H-1183](#h-1183--npm-audit-signatures-reports-a-local-version-as-missing-until-quader-has-signed-it).

### H-1183 · `npm audit signatures` reports a local version as missing until Quader has signed it

Component: the job `signing` (R-SIG-049, R-SIG-050, Q-SIG-56, Q-SIG-59). Subject: pipelines that run `npm audit signatures` against a local or
virtual npm repository. Live from stage 2, in three windows: about a second after a publish, before the job has signed it; the minutes of the
backlog after an installation's first identity, while discovery signs what was published before; and after an administrator retires an
identity, until the job has signed its versions again. In each, the registry offers keys and the version carries no signature, so npm lists
it as missing and exits 1. What makes it real: a pipeline that publishes and audits within a second, or an audit during the backlog.

### H-1184 · `npm audit signatures` needs the public Sigstore TUF repository before it asks Quader

Component: npm's client (npm 12.2.3 `lib/utils/verify-signatures.js`, read 2026-11-21), not Quader. Subject: clients without a way to
`tuf-repo-cdn.sigstore.dev`. Live from stage 1 for remote packages and from stage 2 for local ones. npm asks the public TUF repository for a
registry's keys first; only a missing target sends it to Quader's keys endpoint, and any other failure of that step, such as no route to the
host, aborts the run. Whether a client with a warm TUF cache passes without the network was not verified. What makes it real: a build network
without internet that runs `npm audit signatures`.

### H-1185 · An image with an older cosign signature gets Quader's attestation only through the API

Component: the OCI attestation referrer (R-SIG-054, Q-SIG-66). Subject: clusters and pipelines that demand Quader's attestation of an image
that its publisher signed in the older cosign layout (`sha256-<hex>.sig`, `.att`), and every image of a remote whose upstream has no referrers
API. Live from stage 2. Quader writes no referrer there, because cosign would then stop reading the older signature; the attestation is
served by the API, and cosign's `verify-attestation` and a cluster engine that looks among the referrers do not find it. What makes it real:
an admission policy that demands Quader's receipt for every image, and public images signed with cosign 2 or earlier.

### H-1186 · A policy engine that demands a log or a timestamp cannot verify Quader's attestations

Component: the envelope without a log entry and without a timestamp (R-SIG-052, Q-SIG-63). Subject: clusters that verify with Sigstore's
policy-controller in its bundle mode, which turns the log off only with a timestamp authority (its validator's source, read 2026-11-21), and
every verifier that insists on a log or a timestamp. Live from stage 2. Such a verifier refuses every attestation of Quader's. cosign with
`--insecure-ignore-tlog` and `--insecure-ignore-sct`, and Kyverno's documented options, are the ways that work. What makes it real: a cluster
that runs that engine.

### H-1199 · A client that resolves a range picks a version the gate refuses

Component: the metadata documents under a gate (R-SIG-077, Q-SIG-89). Subject: clients of a gated npm, NuGet or Ansible repository that ask for
a range or the newest version (`npm install pkg@^1`, a floating NuGet version, `ansible-galaxy collection install ns.name`). Specified for
stage 3, not built. The documents list every version, the refused ones included, so the client takes the newest that fits and fails with the
gate's `403`, also when an older signed version would fit. The text says why. What makes it real: a publisher that signs only some releases,
or a release whose signature arrives after its publish.

### H-1200 · Docker and `ansible-galaxy` fail a first pull through a gated remote when the checks outlast the wait

Component: the bounded wait (R-SIG-067, Q-SIG-85). Subject: the first pull of an image or a collection through a gated remote repository by
Docker or `ansible-galaxy`, which do not retry a `503` (the malware gate's record of their sources, Q-MAL-3). Specified for stage 3, not
built. When the fetch of the upstream's signatures and their check take longer than 25 s from the request's arrival, the pull fails with
"Quader is checking the signatures …" and works when it is repeated; npm, NuGet and apt retry by themselves. A mirror pass decides ahead of
demand. What makes it real: a slow or rate-limited upstream, or a backlog of the job `signature-verify`.
