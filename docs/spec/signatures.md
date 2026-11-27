# Signatures on artifacts

Status: built, all three stages, except the virtual repository's filter of R-SIG-054 (H-1217), what R-SIG-024 and R-SIG-025 leave to
the trash can, retention and copy of [storage.md](storage.md), and the test cases R-SIG-028 and R-SIG-033 name as not built. Stage 1 is
built in four packages ([developer/signatures.md](../developer/signatures.md)): the format
contract's declaration and detector of every format, kit check 18, the store of signature objects, signatures, attachments and origins
with the commit hook and the fill hook that record them, and OCI's attachment tags without a version row (package 1); the read and the
integrity check of every scheme, time, the trust step with its scopes, the four kinds of anchor with the shipped ones, those of the Helm
values and the projects' with the switch, blocks, the jobs `signature-verify` and `signature-trust` with discovery, the task
`signature-trust-root`, the API of R-SIG-034, the audit events, metrics, alerts and the configuration (package 2); the formats' wire
behaviour of R-SIG-023 to R-SIG-033: an OCI attachment follows its image on delete and eviction and stays out of the image scanning and
of a mirror's versions; the background fetch of a remote's signatures (job `signature-fetch`) and the mirror's; npm's keys and
attestations endpoints of every class; the signatures a client posts for a local Ansible Galaxy version; Debian's coverage port; the
kit's case that a signature follows its subject (package 3); the screens of R-SIG-035 to R-SIG-038 and the repository's section of
R-SIG-040 (package 4, [developer/web-ui.md](../developer/web-ui.md#the-signatures-screens)). Stage 1 of three: Quader detects, reads and verifies the signatures of artifacts of every format, keeps
each original signature with its verdict, decides trust against trust anchors of the installation and, where the administrator allows it,
of a project, shows the result at the artifact, alarms on an invalid signature, and serves signatures to every client the way its
protocol expects. It blocks nothing. Stage 2, Quader signs, is specified in the section
[Quader signs (stage 2)](#quader-signs-stage-2) (R-SIG-044 to R-SIG-060, built): one signing identity per installation,
npm's registry signatures of local packages and a signed receipt attestation of every artifact. Stage 3, the gate per repository, is
specified in the section [The gate per repository (stage 3)](#the-gate-per-repository-stage-3) (R-SIG-061 to R-SIG-078, built): a detail setting of a repository that lets only artifacts with a verified signature, optionally of chosen anchors, be downloaded.

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
- the API, the badge and the details, the admin page, a project's anchors, the page Signatures under Security audit, audit events, metrics, alerts,
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
- **A batch replaces the attachments of its own sources only**
  ([Q-SIG-217](#q-sig-217--a-batch-replaces-the-attachments-of-its-own-sources-only)). A format's detector (a commit, a fill, discovery)
  speaks for every source a detector names: `referrer`, `referrers_tag`, `cosign_tag`, `index_entry`, `document`, `embedded`, `sibling`,
  `release`. `upload` is a signature a client posts through the management API for a stored version (Ansible's, R-SIG-031), which no
  detector names; its batch speaks for `upload`. `issued` (stage 2, R-SIG-054) is written and removed by the job `signing` through
  statements of its own, and no batch speaks for it. An attachment another writer recorded at the same item stays when the item's own batch
  carries nothing: the measurement's defect 3, where discovery's empty batch of an Ansible archive removed the signature posted for it.
  npm's provenance bundle of a publish is stored beside its tarball and attached `sibling`, the signature file its gate judges it as.
- What a reader sees of a subject is the attachments of the repositories it may read: the addressed repository and, for a virtual
  repository, the members that serve the subject to the caller (R-SIG-027 and the format sections).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_004_…`, about 2 s, bytes): the same image digest in `docker-local` and in a second local
  repository, signed only in `docker-local`: the version of the second repository shows no signature and its referrers answer is empty;
  the signed one shows it. The same `.nupkg` bytes pushed to two NuGet repositories show the embedded signature in both after one check.
- Unit (`internal/formathost`, `TestR_SIG_004_ADetectorsBatchSpeaksForTheDetectorsSourcesOnly`, milliseconds): the batch of an item without
  candidates speaks for the eight sources a detector names and not for `upload`. Integration (area `ansiblelocal`,
  `TestR_SIG_031_ASignaturePostedForALocalVersionIsServed`, about 0.1 s more, rows): the worker's discovery run in process over the archive
  after a signature was posted for it, and then the removal of what has no attachment, leave the signature's attachment, row and object.
  Built.

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
  anchor_epoch  bigint  NOT NULL,                 -- the anchor epoch it was computed under (R-SIG-012)
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
  stands on the page Signatures under Security audit (R-SIG-038): it alarms and blocks nothing.
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
| ASN.1 and CBOR (CMS, COSE, timestamps) | the object's size; no nesting deeper than 32 levels, of definite or indefinite length; an ASN.1 length of at most four octets, a tag number in its shortest form of at most four octets (X.690, 8.1.2; since Q-SIG-216) | `malformed` |
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
- Fuzz (since Q-SIG-216, the readers that are code of this repository; their seeds run as unit tests, 0.04 s and 0.02 s in the unit job's
  mode; 30 minutes each in the closing run's `fuzz nuget` legs): `FuzzSignatureP7s` (`internal/signatures/scheme/nugetcms`)
  reads any bytes as a `.signature.p7s` and runs every step of a check on each part it finds, asserting no panic and that each step refuses
  with a closed refusal or stays within the bounds: at most 2 × `max_chain` certificates of at most 16 KiB each, one primary signer,
  16 countersignatures, 4 timestamps a signer, a chain of at most `max_chain`, content no longer than the object; seeded with the real
  tools' two signatures and the tests' own in both forms of a signer, in DER and BER. `FuzzDefinite`
  (`internal/signatures/scheme/rawkey/pki`) asserts that the conversion to definite lengths refuses with `malformed` or returns DER in its
  tags and lengths at every level, nested at most 32 deep, at most 64 bytes longer than its input, passing the bounds and a fixed point of a
  second conversion. A memory bound beyond these counts is not asserted (H-1166). Built; two minutes each were run on 2026-11-22.

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
| NuGet's CMS `SignedData` and its RFC 3161 timestamp | code of this repository on `encoding/asn1` since the measurement's defect 1 ([Q-SIG-216](#q-sig-216--quader-reads-nugets-cms-itself-and-a-signer-named-by-its-key)): the reader of both forms of a signer, the countersignature, the timestamp's TSTInfo, the attribute checks and the unsigned-package hash. `github.com/digitorus/pkcs7` (MIT) and `github.com/digitorus/timestamp` (BSD-2-Clause), measured here first, read a signer named by issuer and serial number only; they stay in the build for sigstore-go's timestamps and Quader's reading of a bundle's timestamp ([proofs.go](../../internal/signatures/scheme/sigstore/proofs.go)) | BSD-3-Clause (the standard library) | none |
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

Acceptance criteria: the scheme tests of R-SIG-006; and unit (`internal/archtest`, milliseconds): only `internal/signatures/...` and,
since stage 2 builds its trusted root and its bundles with sigstore-go (R-SIG-052), `internal/signing/...` import these libraries, so no
format and no other package reaches them.

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
- Blocked signatures stand on the page Signatures under Security audit (R-SIG-038).

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
| The page Signatures under Security audit | `content:metadata-read` (the dashboard's rule, [api-and-ui.md](api-and-ui.md) R-UI-158) |
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
    None         string           // non-empty: the format has no signatures, and why
    Kinds        []SignatureKind
    ReadToSign   bool             // the signing tools read the artifact from the repository first (OCI, Q-SIG-211)
    ArtifactName func(path string, info PathInfo) string // the artifact in its client's words for the gate's answers (Q-SIG-213)
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
    Source        string           // referrer, referrers_tag, cosign_tag, index_entry, document, embedded, sibling, release (upload is no detector's)
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
- **Not a version in a remote and in the upstream catalogue either** (built, [formats/oci.md](formats/oci.md)
  [Q-OCI-268](formats/oci.md#q-oci-268--an-attachment-tag-is-listed-in-no-remote-and-offered-by-no-upstream-catalogue)): a remote's tags are
  cache entries listed by the rules of R-OCI-036, which leave out an attachment tag by the same name rule, also once a client fetched it
  through the remote (cosign verify does) and in a mirror, which lists every tag it stores, the ones the signature fetch stores
  (R-SIG-026) among them; the upstream catalogue of [remote-curation.md](remote-curation.md) R-CUR-007 offers none of an upstream's
  attachment tags as a version, so the Upstream tab, its preview and "download every version" never take a signature for a version.
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
  ([Q-SIG-104](#q-sig-104--the-image-scanning-leaves-out-what-is-a-signature-in-its-repository)). The screens are built (package 4, R-SIG-035).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_024_…`, about 3 s, bytes): an image `app:1.0` with a Notation referrer, a referrers tag and a
  `.sig` tag pushed by the test's client: the package lists one version and counts one; `tags/list` answers `1.0`, `sha256-<hex>` and
  `sha256-<hex>.sig`; the version's signatures answer lists three attachments; the image scanning's selection names one manifest; a
  BuildKit index's attestation entry is in the attachments and not in the platforms.
- Integration (area `ociremote`, `TestR_SIG_026_…`, one request more): after a client's GET of the `.sig` tag through a remote, and in a
  mirror whose signature fetch stored the referrers tag and the `.sig` tag, the version list holds the tag `1.0` alone. Unit
  (`TestR_SIG_024_AttachmentTagsAreNoVersionsOfTheUpstreamCatalogue`, milliseconds): the catalogue of a distribution registry, Docker Hub
  and Quay offers none of an upstream's attachment tags.

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
- **What a remote's version shows** ([Q-SIG-219](#q-sig-219--a-remotes-version-reads-the-document-it-holds-the-version-as)): a remote holds an
  OCI tag as a document and no file (R-OCI-036), so the version's answer and the version list take the tag's subject from that document,
  whose blob is the manifest the tag names: the signatures the manifest's file view shows. A remote's tag that names another manifest than
  the version's metadata (the version read from another repository) asks for its own. The measurement's defect 4, where a remote's version
  listed none of the signatures its fetch had stored. The other formats' remotes hold a version's file as an item (npm's tarball, NuGet's
  package, Ansible's archive, Debian's package), so their versions read their subjects as a local repository's do; a virtual repository
  shows its members' (R-SIG-027).
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
- The same integration test, two requests more and a wait of at most 10 s until the tag's download is counted (the whole test took 42.6 s
  on a loaded machine on 2026-11-22; the addition's share was not isolated): once the tag's download counted, the remote's version answers the image's
  signatures with a badge, and its version list carries the badge. Unit (`internal/api/v1`, `TestR_SIG_034_VersionSignatures`, milliseconds):
  a version read from a local repository and held by a remote whose tag is a document asks the remote's attachments for the manifest and
  its platforms, a virtual repository over both shows the remote's signature, the holders' documents are read in one call, and a remote's
  tag that names another manifest asks for that one. Built.

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
  Q-NPM-58) as `provenance` of the tarball, attached `sibling` (Q-SIG-217).
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
  the keys are the union and a local member adds none. Built since: the stub serves left-pad's recorded documents with the registry
  signatures of registry.npmjs.org's own key, and the job reads them valid by the keys the remote announces. Not built: a local bundle's
  case under the public root.
- End to end (the existing set `e2e (npm)`, one case, about 5 s, bytes): `npm audit signatures` passes for a package of the npm test upstream
  installed through `npm-remote`, with the test upstream's key served at `-/npm/v1/keys`. Built as R-SIG-050's case: left-pad of the
  test upstream, installed through a remote of its own and a virtual repository, verifies by the key its remote announces.

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

Measured on 2026-11-21 (R-SIG-042): an author's signature of `dotnet nuget sign` (SDK 10.0.403) and the signatures of a nuget.org package
both read `malformed`, "the .signature.p7s is no CMS SignedData": their signer is named by `subjectKeyIdentifier`, which the built parser
does not take. `dotnet nuget verify` passes on the same bytes downloaded from Quader. Repaired: the CMS of a NuGet signature, its
countersignatures and their timestamps are read by code of this repository that takes both forms of a signer
([Q-SIG-216](#q-sig-216--quader-reads-nugets-cms-itself-and-a-signer-named-by-its-key)), and a chain that ends in a root signed with SHA-1, as
nuget.org's older repository certificate's does, is no longer `weak_algorithm`
([Q-SIG-218](#q-sig-218--a-self-signed-roots-own-signature-is-not-judged)); the two signatures of the measurement are the unit test's
fixtures and read `valid`.

Acceptance criteria:

- Unit (`internal/signatures/scheme/nugetcms`, milliseconds, packages signed by the test with digitorus/pkcs7 and a test timestamp authority):
  an author signature with a timestamp, a repository countersignature, a package whose bytes changed after signing (`digest_mismatch`), a
  countersignature over another signature value (`signature_mismatch`); the same signatures with every signer, the timestamp authority's
  included, named by its subject key identifier (a leaf without the extension by the SHA-1 of its key), also in BER with indefinite lengths,
  and one whose key identifier names no certificate of the signature (`chain_broken`). Built.
- Unit (the same package, milliseconds, the real tools' signatures as fixtures in `testdata/`, 35 KiB together): two toy packages
  `dotnet nuget sign` of the .NET SDK 10.0.403 signed on 2026-11-23 with leaves of a throwaway root, one naming its signer by its key and one
  (a leaf without the key identifier extension) by issuer and serial number, each read `valid`, `publisher`, `author`, and `invalid` with one
  byte of the signature value changed (`signature_mismatch`) or an entry added after the signing (`digest_mismatch`); in `trust` both are
  internal under their root and untrusted without it; the `.signature.p7s` of `Microsoft.Extensions.Primitives` 8.0.0 as nuget.org serves
  it: Microsoft's author signature and nuget.org's repository countersignature each find their certificate by its key, build and check their
  chain (nuget.org's ends at a root signed with SHA-1), verify, and carry one verified timestamp of DigiCert, and the whole check of each part
  reaches the package's hash. Built.
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

Measured on 2026-11-21 (R-SIG-042): `ansible-galaxy collection install --keyring … --required-valid-signature-count +1` and `collection
verify` pass on a posted signature, and refuse an unsigned version (`Signature verification failed for 'sigm.unsigned': no successful
signatures`); without the `+` an unsigned version installs. A signature posted right after the publish stays in `signatures[]` but leaves
the signature store when the worker's discovery reaches the archive, so the version reads **No signature**. Repaired: the archive's batch
speaks for the detectors' sources and the posted signature's attachment is `upload`, which it no longer replaces
([Q-SIG-217](#q-sig-217--a-batch-replaces-the-attachments-of-its-own-sources-only)); not measured again with `ansible-galaxy`.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_031_…`, about 3 s, the Galaxy stub serving a collection signed with key A): through `galaxy` the
  version's signature reads `internal` with A as an anchor after the archive is stored, and `invalid` for an archive whose `FILES.json`
  names another digest (package 2's check).
- Integration (area `ansiblelocal`, `TestR_SIG_031_ASignaturePostedForALocalVersionIsServed`, about 2 s, a release key made by the test): a
  signature posted to `ansible-local` is served in the version document with its fingerprint and recorded with the source `upload`; one of
  other content answers 422; a reader may not post; the worker's discovery run in process over the archive, and the removal of what has no
  attachment, leave the posted signature's attachment, row and object (Q-SIG-217); the removal empties `signatures[]`; the post and the
  removal each wrote their audit event with the fingerprint. Unit (`internal/formats/untrusted`, milliseconds):
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
- **The format descriptor** of `GET /api/v1/formats` carries `signatureKinds` (`name`, `role`): the kinds of the format's declaration
  (R-SIG-022), which the details of a version without a signature name ([Q-SIG-127](#q-sig-127--the-format-descriptor-names-the-kinds-of-signature-its-format-knows)).
  Built with package 4 (`TestR_SIG_009_FormatDescriptorNamesTheSignatureKinds`).

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
  positive status); "Signed · signer not trusted" is neutral; "Invalid signature" is red. Without a badge nothing is drawn. The untrusted
  state's badge says **Signed** and "signer not trusted" follows it as muted text (in a list's cell on the cell's second line) that wraps
  between its words, since its words as one pill were
  wider than a file's cell at 390 px in German and a badge does not break
  ([Q-UI-1076](api-and-ui.md#q-ui-1076--a-status-too-long-for-a-pill-puts-its-verdict-on-the-badge-and-the-rest-after-it)); a fact or a
  sentence names the state with its whole words.
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
  In the lists by content of an image ([api-and-ui.md](api-and-ui.md) R-UI-052 and R-UI-110, the opened image in the content list and the
  package's page, one row per manifest, Q-UI-1130) the badge stands on the image's row, after its tags: the signatures of a manifest
  are the image's, whichever of its tags leads the row; the API carries them as the leading version's `signatures` (R-SIG-034).
- The screens are rendered and looked at before they land (rule 11 of the plan): light and dark, wide and narrow, a version of each of the
  four states, one with seven signatures and long identities, a Debian package with its covering release, a generic file with three
  siblings, and a version with no signature.
- **Built** (package 4, [Q-SIG-124](#q-sig-124--a-versions-details-are-a-tab-signatures-and-the-badge-opens-it) to
  [Q-SIG-126](#q-sig-126--a-files-page-carries-the-badge-as-a-fact-and-the-details-in-a-closed-section)): the badge
  (`quader-signature-badge`) beside the version in the version list, in the version page's header (a link to the tab) and in a column
  **Signature** of the Files tab while a file carries one; the version page's tab **Signatures** (per holding repository); a file's fact
  **Signature** and its closed section **Signatures**; the details (`quader-signature-report`) with the state's sentence (a badge that
  fell because every counting signature's signer is blocked names the block, its date and its reason, instead of saying that no anchor
  knows the signer), the origin, each signature's kind,
  signer, issuer, identity, time and its proof, certificate, trust anchors or block, a CI job's claims, **Download original**, the
  technical details in a fold, Debian's covering releases and an image's **Attestations and SBOMs** in a table; a generic signature file's
  **Signature of**. Rendered with the seed of the renders (`web/e2e/renders/signatures.ts`): the versions of `chart-render-kit` in every
  state, the image `portal/web` with two signatures, a provenance, an SBOM and an attestation of a scan, an npm version with npm's registry
  signature and a provenance, a NuGet author signature, and a generic file with three signature files uploaded by a pipeline. Not
  rendered: a Debian package with its covering release (the renders' Debian remote holds no content) and a version with seven signatures;
  the details' layout is the same per signature.

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
  2. **Project trust anchors**, open: the checkbox **Project owners may add trust anchors for their project** (off by default) with its
     help behind the sign after its label ([api-and-ui.md](api-and-ui.md) R-UI-267, [Q-UI-1628](api-and-ui.md#q-ui-1628--the-help-sign-on-every-settings-page-and-no-info-icon-beside-a-status)): "Project anchors apply
     only to the repositories of their project. A project cannot switch off an anchor of the installation.", and below it every project's anchors with their project and **Remove**.
  3. **Blocked signers**, closed: the blocks with their reason; **Block signer** asks for the kind, the value and a reason.
  4. **Public Sigstore root**, closed: where the root comes from (refreshed, or the release's snapshot), when it was fetched, and the last
     failure.
- Under the title stands: "A signature tells who signed an artifact and whether a trust anchor knows the signer. It does not tell that the
  content is safe; the malware scan and the vulnerability scanning check that."
- Stage 2 adds the section **Signing identity** before the four (R-SIG-056).
- The page is rendered and looked at before it lands: both schemes, wide and narrow, with the shipped anchors only, with twelve anchors of
  all kinds and long titles, with the switch off and on, and with a block.

- **Built** (package 4, [Q-SIG-129](#q-sig-129--the-admin-pages-controls-a-table-of-anchors-a-dialog-per-form-the-switch-saved-on-its-own)):
  `/admin/settings/signatures`, the fourth entry of the side bar's group Settings for the holder of `settings:read`; the four sections, the
  forms of **Add trust anchor** and **Block signer** as dialogs, the backlog under the anchors, and a link to the dashboard's view for the
  holder of `content:metadata-read`. Stage 2's section **Signing identity** stands first (R-SIG-056, built).
- **The lists' form** ([api-and-ui.md](api-and-ui.md) [Q-UI-969](api-and-ui.md#q-ui-969--the-signatures-lists-keep-one-form-a-table-at-1440-and-1024-px-blocks-at-390-px)): a
  list of anchors (here and in a project, R-SIG-037) has the columns **Trust anchor**, **Expires**, **Signatures**, **State** and the
  row's action; the kind and the source (or the project) are a line under the anchor's name, the expiry a relative time with the date
  as its title. **Blocked signers** has **Signer** (the reason under it), **Signatures**, **Blocked** and the row's action. Each list is
  a table at 1,440 and 1,024 px and stands as blocks only at 390 px.

Acceptance criteria:

- Web unit (Vitest, under a second): the form shows the fields of the kind chosen; the switch is a checkbox for the holder of
  `security-settings:write` and text for an auditor; a shipped anchor offers only its switch.

### R-SIG-037 · Trust anchors in a project
Priority: **P1** · Covers: quader-only

- While the switch is on, the project's settings page shows a closed section **Trust anchors** with the project's anchors and, for its
  Owner, **Add trust anchor** (the form of R-SIG-036); members see the list. While it is off the section is not shown, and a project that
  holds anchors shows them to its Owner with "Not applied: project anchors are switched off by an administrator."
- The installation's anchors are listed read-only below the project's ("Also trusted in this project"), so an Owner sees the whole scope.

- **Built** (package 4): the section `quader-project-signature-anchors` after **Details** of the project's settings; an administrator
  removes a project's anchor through the installation's route whatever the switch says, an Owner through the project's.

Acceptance criteria: web unit (Vitest, under a second): the section is hidden with the switch off and no anchors; read-only for a member.

### R-SIG-038 · The page Signatures under Security audit
Priority: **P1** · Covers: quader-only

The owner's decision ([Q-SIG-9](#q-sig-9--display-or-gate)): stage 1 shows and alarms; "Invalid signature" is an entry of the security
dashboard.

- **Where.** The entry **Signatures** (`/security/signatures`) of the side bar's **Security audit**, after **Security dashboard** and
  **Malware**, a page of its own with the heading **Signatures** and the trail "Security audit / Signatures" ([api-and-ui.md](api-and-ui.md)
  R-UI-158, [Q-UI-1005](api-and-ui.md#q-ui-1005--malware-and-signatures-are-entries-of-their-own-under-security-audit); until 2026-11-23 a view of the dashboard), for the holder of `content:metadata-read`.
- **The list** (`GET /api/v1/signatures/findings`): one row per signature that is `invalid` or `blocked`, with the artifact (repository,
  path, package and version), the kind, the signer as it claims, the reason, when it was found, and the number of repositories that hold it;
  filters kind and repository; newest first. An empty list says "No invalid signatures."
- No action in stage 1: nothing is released, because nothing is refused; a holder of `security-settings:write` finds **Block signer** on the
  admin page. Stage 3 adds the refused downloads of gated repositories and their releases (R-SIG-070, R-SIG-074).

- **Built** (package 4, [Q-SIG-130](#q-sig-130--the-security-dashboards-view-of-signatures)): `/security/signatures`, the third link of the
  dashboard's views, with a search by signer and a drawer of kind and repository; filters live in the address. Its columns are
  **Artifact** (the path, the reason and the repositories that hold it under the name, which breaks after its dots and slashes),
  **Kind**, **Signer** and **Found**: a table at 1,440 and 1,024 px, blocks only at 390 px (Q-UI-969).

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
  **Signatures** of its settings page (built with package 4, **Fetch the upstream's signatures**); on a well-known repository of the
  project `upstream` it is an administrator's member, on a system repository the product's
  ([Q-SIG-128](#q-sig-128--the-fetch-of-a-well-known-registrys-signatures-is-an-administrators-setting)). Stage 3 adds `onlyVerified` and `anchors` on every class (R-SIG-061) and its own keys under
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
- **Revision columns** ([architecture.md](architecture.md) R-ARCH-009, [backup-and-restore.md](backup-and-restore.md) R-BAK-022): the
  ETags `signature_settings.version` and `signature_anchors.version` are registered, so a restore raises them by 2^32. The anchor epoch
  is not: `signature_settings.anchor_epoch` is compared only with its copies, in the trust rows (`signature_trust.anchor_epoch`) and
  from stage 3 in an anchor's `narrowed_epoch` and a block's `anchor_epoch` (R-SIG-066). A restore leaves no trust row (the table is
  transient) and restores the other copies with the counter, so it needs no raise; a raised copy beside an unraised counter would make
  a row look current for good. `signature_trust_roots.tuf_version_number` is the update repository's number (R-SIG-017): raised, the
  stored root would look newer than every snapshot to come and never be refreshed. None of these columns carries a name the rule reserves
  for revision columns (`rev_seq`, `revision`, `epoch`, `*_version`).
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
  notation-core-go, NuGet packages with digitorus/pkcs7 and a timestamp token of a test authority (re-encoded to name each signer by its
  key as well, which digitorus/pkcs7 does not write), cosign's older layout with the leaf. NuGet's case also keeps two signatures of the
  real tools as fixtures (R-SIG-029): a signature made in process by a library shares that library's blind spots, which is how every NuGet
  signature of the real tools read malformed while the tests passed.
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

What only a real client shows and is not tested: cosign, Notation, `dotnet nuget verify` and `ansible-galaxy --keyring` against Quader.
**Measured once on 2026-11-21** with the real tools ([developer/signatures.md](../developer/signatures.md#measured-with-real-clients)):
cosign 3.1.4 with a key pair (its default, a DSSE envelope of the predicate type `https://sigstore.dev/cosign/sign/v1` as a referrer, and
the older `.sig` tag through three deprecated flags), cosign's attestation, Notation 1.3.2 with the company CA, a generic file's `.asc` of
GnuPG and `.sigstore.json` of `cosign sign-blob`, and npm's registry signatures and provenance of the public registry are read and verified,
and turn **Verified · internal** or **· public** when the anchor is added; `cosign verify`, `notation verify`, `npm audit signatures`,
`dotnet nuget verify` and `ansible-galaxy … --keyring` pass through Quader, also through an OCI remote. Falsified: **no real NuGet
signature is readable** (`dotnet nuget sign` of SDK 10.0.403 and nuget.org's both name the signer by `subjectKeyIdentifier`, which the
CMS parser of `nugetcms` does not take; every one reads **Scheme not supported**); a signature posted for a new Ansible version is
removed from the store by the worker's discovery; a remote OCI image's version lists none of the signatures its fetch stored. The three are
repaired: the measurement's two NuGet signatures are fixtures of the unit tests and read `valid` (Q-SIG-216, Q-SIG-218); a detector's
batch replaces only its own sources' attachments (Q-SIG-217), tested with the worker's discovery run in process in `ansiblelocal`; a
remote's version reads its tag's document (Q-SIG-219), tested at the unit tier and in `ociremote`. None was measured again with the real
clients. cosign 3.1.4
with `--certificate-chain` writes a bundle that neither cosign nor Quader reads (bundle v0.3 with a certificate chain); the company CA's
way through cosign works with `--new-bundle-format=false`. Keyless signing was not measured: it needs an interactive sign-in or a signing
service of its own.

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
  (R-SIG-066). It changes stage 1's tables by two columns (`signature_anchors.narrowed_epoch`, `signature_blocks.anchor_epoch`) and one index
  (`signature_trust_anchors`).

Acceptance criteria: none; the stages bring their own.

## Quader signs (stage 2)

Status: specified and built, all four packages: the signing identity with its states, its chart, its API, events, metrics and
alerts, and the frame of the job `signing` (package 1); npm's registry signatures of local versions with the local keys endpoint (package 2);
the receipt attestations of every format with the contract's `AttestationSubjects`, the image's referrer where it hides nothing, the API's
route and the client setup's tab **Verify** (package 3, [developer/signatures.md](../developer/signatures.md#stage-2-quader-signs)); of
package 4 the section **Signing identity** of the admin area (R-SIG-056, first bullet), and Quader's own signature and receipt in the
details of a version, a file and an image (its second half). The second stage is whole. The owner asked for it in his request of 2026-11-21 ("Quader ist ebenso in der Lage Artefakte zu signieren,
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
| Ansible Galaxy | none in stage 2 ([Q-SIG-68](#q-sig-68--ansible-galaxy-quader-does-not-sign-local-collections-in-stage-2)) | every collection archive (and a role archive, which a client fetches the same way) | the API and a download |
| generic | none: no scheme of a registry exists for a file | every file but one that R-SIG-032 reads as a signature of its sibling | the API and a download |
| `clamav` (a system format) | none | none | — |

- **System repositories** of `upstream` (`scanner-trivy`, `clamav`; [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-070,
  R-REPO-075) get no attestation: they hold what the product fetches for its own tools, `clamd` checks the publisher's signature of each
  ClamAV container itself, and the company's ClamAV installations do the same through the definitions' endpoint; a receipt of Quader's own
  tool data would be read by no one.
- **The format names what is attested** ([Q-SIG-67](#q-sig-67--the-format-names-what-quader-attests-and-the-contract-rises-to-version-10)):
  stage 2 adds a method to the adapter of R-SIG-022, and `ContractVersion` rises to 10 in the change that adds it (the gate of stage 3 took
  version 9 first, Q-SIG-132). The job asks it when it signs, for stored items and with the host, as the gate asks `Gate`
  ([Q-SIG-180](#q-sig-180--the-format-is-asked-what-it-attests-when-the-job-signs-for-stored-items-with-the-host)):

  ```go
  // AttestationSubjects names, for stored items of a repository, those Quader's receipt attestation is about: the
  // artifacts a client fetches, never an item that carries a signature or an attestation. One subject per item.
  AttestationSubjects(ctx context.Context, h Host, repo RepoRef, items []AttestationItem) ([]AttestationSubject, error)

  type AttestationSubject struct {
      Attested  bool       // the zero subject is an item Quader does not attest
      Name      string     // the artifact's name in the format's words: an image, a package, a path
      Version   string     // empty where the format has none
      MediaType string     // an image's manifest or index media type
      Scanned   [][32]byte // the blobs whose malware verdicts the statement names (an image's layers); nil: the item's own
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

- **Built** (package 3 of stage 2): `formats.SignatureAdapter.AttestationSubjects`, `AttestationItem`, `AttestationSubject`,
  `AttestNoneFor`, `SignatureDeclaration.Attests` and `.AttestsNone` (`Register` refuses neither or both), `NoSignatures(reason,
  attestsNone)`, `ContractVersion` 10; every format on `main` as the table says, OCI's judgement read from `oci_manifests` when the job signs
  ([Q-SIG-181](#q-sig-181--an-images-manifest-is-judged-when-the-job-signs-and-the-statement-names-no-tag)). Kit check 18 asks every
  `GateCase` (`GateCase.Attested`) and holds a format that attests something to at least one attested case; a format without signatures is
  held to no subject over its content cases. The defect `testformat.AttestsSignature` fails it.

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

- **Built** (package 1 of stage 2, [developer/signatures.md](../developer/signatures.md#stage-2-quader-signs)): the checks of the table (`signing.ParseChain`, `Chain.Check`, the refusal naming its check), npm's keyid (`signing.NPMKeyID`) and keys document (`signing.NPMKeys`, which the npm package serves), the table `signing_identities` with the CHECK amended by [Q-SIG-149](#q-sig-149--retiring-an-uploaded-identity-drops-its-sealed-key) and the moves of [Q-SIG-145](#q-sig-145--the-identitys-states-move-by-a-table-a-retired-identity-never-comes-back-a-previous-one-does) ([states.go](../../internal/signing/states.go)).

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
  "From the Helm configuration" and no controls, an upload or a key request answers `409 signing_identity_managed`, and an uploaded
  identity that is stored stays unused and marked "Not in use: the Helm configuration names the signing identity" (R-UI-268). When the values stop naming one, the identity
  of the files becomes `previous`, and a stored uploaded identity that passes R-SIG-045 is announced; without one Quader signs nothing from
  then on.
- `api` and `packages` read the public part from the database (the keys endpoint, the chain, the trusted root) and never the files.

- **Built:** the worker's watcher ([job/watcher.go](../../internal/signing/job/watcher.go)) reads the three files at its start and every `reload_interval`, registers a passing pair (its key into the worker's holder) and records a refused one once per content in `signing_settings` (Q-SIG-148), which the API answers as `refused`; a worker that starts with no file named lets the identity of the files go (Q-SIG-145). The test shows the identity current, rotated and refused, and, since package 2, a publish to `npm-local` signed with the key of the files and verified against the keys of `npm-local`.

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

- **Built:** the store ([store/store.go](../../internal/signing/store/store.go)) makes and seals the key in the row's transaction (`internal/signing/key`, the sealed column registered with the key ring and walked by `secrets-reencrypt`), answers the request with the defaults of [Q-SIG-147](#q-sig-147--the-key-requests-defaults-are-the-servers-too-and-its-bounds), takes the CA's certificate against the stored public key, imports PKCS#8, SEC 1 and encrypted PKCS#8 (`signing.ParseKey`), and discards a pending key. The refusals beyond the specification's are [Q-SIG-146](#q-sig-146--the-apis-refusals-beyond-the-three-the-specification-names-and-the-public-answers-without-an-identity)'s.

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

- **Built of this** (the identity's own states): the announce period, the switch by the job under the exclusive lock of every identity (`store.SwitchDue`), the shared hold of a signing transaction (`store.HoldCurrent`, refused once the identity is not current or has expired), the removal and the retirement with their events and journal entries, the keys document per state. The attestations' part (made again) is package 3's.
- **Built** (package 2 of stage 2, the npm half): the documents serve the signatures of the current and the previous identities while one is current (`signing.NPMServed`, the statement of the document, [Q-SIG-159](#q-sig-159--a-document-serves-a-signature-by-its-identitys-state-read-in-the-documents-statement)); the job compares that set with the one it stored at its last look in every run, also with signing off and with no identity current, and raises the revision and `time.modified` of every package whose versions carry a signature of an identity that joined or left it, in transactions of 1,000 packages ([Q-SIG-160](#q-sig-160--the-job-compares-what-the-documents-serve-in-every-run-and-raises-the-revisions-itself)); the versions of a retired identity get work of the reason `retired` at priority 2, and the signature made again replaces the retired one's attachment of the version ([Q-SIG-162](#q-sig-162--a-signature-made-again-after-a-retirement-replaces-the-retired-ones-attachment)). The integration test shows the rotation, the retirement and the removal end to end (`TestR_SIG_050_QuaderSignsTheVersionsOfLocalNpmRepositories`, its subtests named R-SIG-048).

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

- **Built:** the frame ([job/job.go](../../internal/signing/job/job.go)): the table `signing_work`, `store.Enqueue` (in the commit's transaction, waking the job), the job `signing` on the queue `signing` with its task, the order by priority and time, the attempts with their back-off and the daily retry, `signing.failed`, the metrics, and `job.Kind` that a package registers ([Q-SIG-150](#q-sig-150--the-jobs-frame-kinds-the-packages-register-one-discovery-step-per-run-a-job-that-only-switches-without-a-kind)). Only the roles worker and all build the holder and the job; `internal/archtest` holds the importers of `internal/signing/key` and `internal/signing/job` (`CheckSigningKey`).
- **Built** (package 2 of stage 2): the kind `npm` ([npmsign](../../internal/signing/npmsign/npmsign.go)): a publish to a local repository writes its row (reason `upload`, priority 0) in the commit's transaction through the npm data port, only while signing is on and an identity is current ([Q-SIG-158](#q-sig-158--a-publish-writes-its-work-only-while-signing-is-on-and-an-identity-is-current)); one operation signs the versions of a package that have due rows together and raises the package's revision once ([Q-SIG-157](#q-sig-157--one-operation-signs-the-due-versions-of-a-package-together)); discovery finds the tarballs of the live versions of local repositories without a served signature. The frame gained `Kind.Served`, called once per run whatever the identity's state (Q-SIG-160). Measured on the development machine: a publish's signature stood in the document 0.33 s after the publish's answer (the integration test asserts 5 s, so that a loaded runner does not fail it).

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

- **Built** (package 2 of stage 2, [developer/signatures.md](../developer/signatures.md#package-2-of-stage-2-built)): the signature
  (`signing.SignNPM`, the entry `{"keyid", "sig"}`) stored through the signature store (`RecordIssued`: scheme `npm-registry`, role
  `registry`, valid and never due, `identity_id` set, attached `issued` to the tarball); the full, the abbreviated and the version document
  render `dist.signatures` from the stored signatures whose identity is listed, read in the statement that reads the document's rows, and
  `document.RenderVersion` is 3; the keys endpoint of a local repository answers `signing.NPMKeys` over the replica's snapshot of the
  identities, and a virtual repository adds Quader's keys once, for its first enabled local member, in member order. The format reads both
  through its data port, the format contract is unchanged ([Q-SIG-156](#q-sig-156--the-npm-format-reads-quaders-keys-and-signatures-through-its-data-port)).
  What deviates from the text above: a version whose identity is retired is signed again and the retired signature's attachment is removed
  (Q-SIG-162); the client setup's tab `verify` of R-SIG-055 comes with the attestations ([Q-SIG-163](#q-sig-163--the-client-setups-tab-verify-comes-with-the-attestations)).
  The end-to-end case runs against a remote (`npm-signed-remote`) and a virtual repository (`npm-signed`) of its own over `npm-local`, so
  that no keys document another case stored of `npm-remote` stands in its way, and the npm test upstream announces, through its control
  API, the key of registry.npmjs.org that signed the recorded left-pad. Run on 2026-11-21 on a kind cluster of the development machine, npm
  12 printed "npm warn Fetching verification keys using TUF failed. Fetching directly from https://localhost:18943/api/npm/e2e/npm-signed/.",
  "audited 2 packages in 1s" and "2 packages have verified registry signatures", exit 0. By npm's source, as read above, that fallback
  follows only a TUF answer without a target for the registry, and any other failure ends the run: the client pod reached the public TUF
  repository (the traffic itself was not captured). H-1184 stays for a pod without that route.

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

- **Built** ([internal/signing/statement](../../internal/signing/statement/statement.go), the facts read by
  [internal/signing/attest](../../internal/signing/attest/attest.go)): the statement and its bounds; more than 16 KiB leaves out signatures and
  counts them in `signaturesOmitted`. A fill's `upstream` is the URL the item was fetched from
  ([Q-SIG-189](#q-sig-189--a-fills-upstream-is-the-url-the-item-was-fetched-from)); an image's `artifact` names the image and the media type
  and no tag (Q-SIG-181); an image's malware state is the worst verdict of its config and layers; the vulnerability snapshot is the report's
  evaluation of the version in its repository ([Q-SIG-190](#q-sig-190--the-vulnerability-snapshot-is-the-reports-evaluation-read-over-every-repository)).

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
  from the protobuf types of Sigstore's bundle specification and read back through sigstore-go's `pkg/bundle` before it is stored; both are
  in the build already (R-SIG-014), so stage 2 adds no module. sigstore-go's own `pkg/sign` is not used: it would link a Fulcio client with an
  OAuth flow into the binary ([Q-SIG-182](#q-sig-182--the-bundle-is-built-from-sigstores-protobuf-types-not-with-sigstore-gos-sign-package)).
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

- **Built** ([internal/signing/bundle](../../internal/signing/bundle/bundle.go)). Verified in the unit test: sigstore-go v1.4.0 takes the
  trusted root Quader builds (no logs, no CT logs, no timestamp authorities) through `root.NewTrustedRootFromJSON`, its verifier with
  `verify.WithCurrentTime()` alone, and verifies a bundle of a company CA's leaf for the identity's URI with an issuer regexp of `.*`; the
  integration test verifies the bundle the API serves against the trusted root the API serves.

Acceptance criteria:

- Unit (`internal/signing/bundle`, milliseconds, a CA of the test): a bundle of the signer verifies with sigstore-go's verifier against the
  trusted root Quader builds (`verify.WithCurrentTime`, no log, no timestamp), and does not with a flipped byte of the statement, under a
  trusted root of another CA, or at a time outside the leaf's validity; the trusted root holds one authority per distinct chain.
- End to end (the set `e2e (oci)`, one case, about 4 s, an image of a few hundred bytes pushed through the registry API): `cosign
  verify-attestation` with the command above exits 0 for the image in the set's local repository, and non-zero with
  `--certificate-identity https://other.example.com`. The cosign binary joins the OCI clients image, pinned once; what it adds to the image
  and to the leg's load time is not measured, and the package that builds it writes the measured numbers here. Built as
  `TestOCIAttestation_R_SIG_052_CosignVerifiesQuadersAttestationOfAPushedImage` ([oci_attestation_test.go](../../test/e2e/oci_attestation_test.go)):
  cosign 3.1.4 of `E2E_OCICLIENTS_COSIGN_IMAGE`, the official binary in an image of its own that only the set `oci` copies onto the
  control-plane node ([Q-SIG-192](#q-sig-192--cosign-reaches-the-set-oci-alone-as-its-binary-copied-onto-the-control-plane-node)).
  Measured on 2026-11-22 on a kind cluster of the package's own at a load of 30 to 60: the verification exits 0 in 0.18 s once the
  attestation stands (2.3 s for the subtest with one wait for the job), the other identity is refused in 0.2 s ("expected SAN value
  "https://other.example.com""); the case takes 37 s when it runs first, most of it the set's shared fixture (the scatter profile, the
  repositories), which the set pays once. cosign is 136.4 MB as `docker save` writes its image (the Docker client image stays at
  326.7 MB); copying the binary onto the node took 1.3 to 4.8 s, about what it added to the Docker client image's `kind load` when it was
  inside it (7 to 9 s without, 10 s with); `oci-closing` and `oci-long` load nothing of it.

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

- **Built** ([internal/signing/attest](../../internal/signing/attest/attest.go)): every file a client or a fill stores in a format that
  attests something becomes work of the kind `attestation` in the commit's transaction (priority 1 an upload, 2 a fill), written by the
  statement of npm's publish only while signing is on and an identity is current (Q-SIG-158's rule: discovery attests what came before),
  and the format says whether it is a subject when the job signs ([Q-SIG-186](#q-sig-186--every-committed-file-of-a-format-that-attests-is-work-the-format-decides-when-the-job-signs));
  a new attestation whose checks have not answered waits without spending an attempt and looks again after a quarter of the item's age
  (5 s to 5 min), woken at once by a changed malware verdict, and the job wakes itself for the earliest waiting row
  ([Q-SIG-187](#q-sig-187--a-new-attestation-waits-without-spending-an-attempt-and-looks-again-after-a-quarter-of-its-items-age));
  a changed verdict or release of the malware scanning (`malware/store.Options.VerdictChanged`), a rotation (discovery) and a retirement
  make it again; the item's removal takes it. "Without the scanning" is the installation's `malware.enabled: false`.

Acceptance criteria:

- Integration (area `signing`, `TestR_SIG_053_…`, about 4 s, files of a kilobyte, the malware stub of the stack): a generic file's
  attestation waits for the verdict and carries `clean`; when the stub's rescan flags the file, it is made again with `infected`; a second
  rescan with the same verdict changes nothing; deleting the file removes the attestation; in a repository without the scanning it is made
  at once with `not_scanned`. Built as `TestR_SIG_053_AFilesAttestationWaitsForItsVerdictAndFollowsTheMalwareState` (about 6 s): the
  malware scanning is on without a scanner, so the attestation is made with `pending` at the bound of `wait_for_checks` (2 s), the test
  writes the verdict as the malware store does (with its word to the job), and a second verdict of the same state changes nothing;
  `not_scanned` is the unit tier's.

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

- **Built of this** (package 2 of stage 2): the source `issued` of `signature_attachments` (migration 00103), the store's `RecordIssued` and
  `DetachRetiredIssued` ([store/issued.go](../../internal/signatures/store/issued.go)), npm's registry signature stored as above; the
  commit hook's and the fill's batches never replace an `issued` attachment, because a batch replaces the attachments of its own sources
  only and no batch speaks for `issued` ([Q-SIG-217](#q-sig-217--a-batch-replaces-the-attachments-of-its-own-sources-only)), and the trust job writes no row for a signature with
  `identity_id` ([Q-SIG-161](#q-sig-161--npms-signatures-are-written-through-a-signature-store-of-their-own-whatever-signaturesenabled)
  for the store npm's signatures are written through).
- **Built of this** (package 3, migration 00105, [database.md](../developer/database.md#the-receipt-attestations-00105)): an attestation's
  signature and its attachment `issued` are written through the same `RecordIssued` (with the predicate type, the signer and
  `Replace`, which drops the item's earlier attestation of the scheme in the same transaction, `SignatureDetachIssuedExcept`); the job's
  index `signing_attestations` (one row per attested item: the identity, the malware state and the blobs it names, where it is served;
  [Q-SIG-185](#q-sig-185--the-job-keeps-an-index-of-what-it-issued-signing_attestations)); stage 1's discovery leaves Quader's referrers
  out. The bundle is `signature_objects.raw` for every format, an image's too, and the image's
  referrer is a copy of it on the format's wire, committed through the item store as the system's write (`system:signing`) and recorded by
  OCI's `OnCommit` as an attachment the gate passes ([Q-SIG-184](#q-sig-184--the-bundle-is-kept-in-postgresql-for-every-format-an-images-referrer-is-a-copy-on-the-wire)).
  The referrer stands in a local repository only; a remote's and a virtual's images are attested through the API
  ([Q-SIG-183](#q-sig-183--the-referrer-stands-in-a-local-repository-only-a-remotes-and-a-virtuals-images-are-attested-through-the-api),
  [H-1215](../security/artifact-signatures.md#h-1215--a-remotes-and-a-virtuals-images-carry-quaders-attestation-only-through-the-api)). An
  older `.sig` or `.att` tag stored for the digest takes the referrer in the transaction of the tag's commit, as the system, whether an
  identity is current or not (`formats.SignatureSourceCosignTag` recorded by the commit hook, `SigningUnplaceReferrers`); the attestation is
  served through the API from then on with the reason `hides_signature`. Verified on 2026-11-21 with cosign 3.1.4 in the end-to-end
  cluster: with Quader's referrer beside a `.sig` tag of the same digest, `cosign verify --key` of the publisher's older signature fails
  ("no matching attestations: expected key signature, not certificate"); the deprecated flag `--new-bundle-format=false` (hidden from
  `--help`) makes it read the `.sig` tag and verify, `--experimental-oci11=false` does not. The route answers the bundle while its identity is current
  or previous. Not built: the virtual repository's union that leaves Quader's referrers out when another member holds an older signature tag of
  the digest ([H-1217](../security/artifact-signatures.md#h-1217--through-a-virtual-repository-quaders-referrer-of-one-member-can-hide-an-older-signature-tag-of-another));
  a virtual repository lists Quader's referrers of a local member, which never stand beside an older signature tag of that member (Q-SIG-183).

Acceptance criteria:

- Integration (area `signing`, `TestR_SIG_054_…`, about 4 s, bytes): an image pushed to `docker-local` lists Quader's referrer in its
  referrers answer with the artifact type and the annotations above, not in `tags/list` and not as a version, and through `docker-all` in
  the union; after a `.sig` tag of the image is pushed, Quader's referrer is gone and the API serves the bundle; through `docker-hub` (the
  OCI stub with the referrers API) the answer holds the stub's referrer and Quader's, and against the stub without the API it stays 404
  with no referrer of Quader's; a generic file's attestation answers the bundle with its file name; a local npm version's document carries
  no `dist.attestations` of Quader's. Built as `TestR_SIG_054_AnImagesAttestationIsAReferrerUntilAnOlderSignatureTagArrives` (about 3 s):
  the local image, the referrer's row (artifact type, the predicate type's annotation, `attachment`), no version, the API's bundle, and the
  referrer removed after a `.sig` tag with the reason `hides_signature`. The repository's gate is on: its preview counts the image as its
  one artifact (the referrer, its config and the bundle are none) and refuses it as `missing`, the file's answer lists the receipt apart as
  an attestation with `issuedBy` and no badge, and its member gate refuses the image with nothing in `passedBy` (R-SIG-062, R-SIG-072,
  R-SIG-073). The impact (R-SIG-071) is not asserted: its candidates are counting signatures (`publisher`, `provenance`) and an issued
  signature never passes, so a receipt can neither be a candidate nor stop passing. Not asserted: the virtual union and the remote (no referrer is
  written there, Q-SIG-183); the generic file's bundle is R-SIG-053's test; npm's documents are package 2's.

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

- **Built:** the rows of the identity (GET, the key, the certificate, the import, the discard, the removal, the retirement, the trusted root and the chain; [signing_api.go](../../internal/api/v1/signing_api.go)). Package 2: the signatures answers of R-SIG-034 list npm's registry signature of a local version with `source: "issued"` and `issuedBy` (`identity`, `subject`, `fingerprint`, and `state`, the identity's state now, so that a screen can tell a retired identity's signature that is no longer served), with `keyId` set to npm's keyid ([signatures_views.go](../../internal/api/v1/signatures_views.go)). Until the second half of the screens (R-SIG-056) gives it its own line, a version's tab **Signatures** lists such an entry as "Origin confirmed by registry" with its key, never as a publisher's signature and without a badge of its own (`entryBadgeState` answers none for `issuedBy`, Q-SIG-1; [signature-words.ts](../../web/src/app/views/signatures/signature-words.ts)); its sentence still reads that no trust anchor knows the signer. Package 3: the attestation's route ([attestation_api.go](../../internal/api/v1/attestation_api.go)); the client setup's tab `verify` with `npm.verify.signatures` for npm's client (Q-SIG-163) and `<client>.verify.attestation` for every client of a format that attests something ([setup_verify.go](../../internal/api/v1/setup_verify.go), [Q-SIG-188](#q-sig-188--the-tab-verify-is-one-snippet-per-client-written-by-the-core)); the answers of R-SIG-034 list Quader's attestation through the same `issuedBy` mechanism, as an attestation with `source: "issued"`, its signer (`identity`, `subject`, `issuer`) and its predicate type, without a badge of its own. The second half of package 4: a receipt's entry carries the decoded predicate in `attestation` (`receivedAt`, `issuedAt`, `origin` in the shape of R-SIG-010's origin with a pipeline's claims through the same allowlist, `signatures` with their integrity states and `signaturesOmitted`, `malware` with its state, database, dates and release, `vulnerabilities` with the evaluation, grade, open findings per severity and the data's date), read from the bundle the store keeps by `statement.Read` within the statement's bounds and left out for a bundle that does not decode ([Q-SIG-234](#q-sig-234--the-predicate-is-decoded-from-the-stored-bundle-when-the-answer-is-read)), and where the job serves it in `servedAs` (`referrer`, `api`) with `servedReason` (`hides_signature`, `no_referrers_api`) for an image served through the API ([Q-SIG-233](#q-sig-233--servedas-is-a-word-and-the-reason-a-member-beside-it)); `SignatureViews` reads both from `signing_attestations` (00105) for an issued signature alone ([signatures_receipt.go](../../internal/api/v1/signatures_receipt.go)). `issuedBy` is the same for both kinds. **With this, the API of stage 2 is whole.**

Acceptance criteria:

- Integration (area `signing`, `TestR_SIG_055_…`, about 2 s): the operations for an administrator, a `security-admin`, an auditor (reads,
  403 on writes) and an anonymous caller (the trusted root and the chain, 401 on the identity); the client setup of `npm-local` holds the
  tab `verify` while an identity is current and not without one.
- Unit (`internal/api/v1`, `TestR_SIG_055_QuadersReceiptInTheSignaturesAnswers`, 0.03 s in the unit job's mode once the package's shared parse is
  done; 6.2 s run alone, nearly all of it that parse): a receipt's entry carries its decoded statement and `servedAs`, an image's `servedReason`, a bundle that does not decode keeps
  `servedAs` alone, a signature that is no receipt carries none of the three, and every answer validates against the API document
  (`specDocument`); `internal/signing/statement` (`TestR_SIG_055_AStatementReadsBackAsItsFacts`, milliseconds) and `internal/signing/bundle`
  (a subtest, milliseconds): `Read` is `Build`'s inverse over the five golden cases, and what is no receipt v1 or beyond the bounds is
  refused. The store's read is the integration area `signing`'s: `TestR_SIG_054_…` asserts `servedAs` and the decoded statement of the
  local image's receipt, and `referrer`, then `api` with `hides_signature` after the older signature tag (two calls, under 0.1 s).

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
  - **From the Helm configuration:** "From the Helm configuration" and the facts, no controls; a refused pair of the files as a line in red with its
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

- **Built** (the first half of package 4, [developer/signatures.md](../developer/signatures.md#package-4-the-section-signing-identity-built)):
  the section **Signing identity** in every state of the first bullet, its forms as dialogs and the request in the section
  ([Q-SIG-170](#q-sig-170--the-sections-forms-are-dialogs-the-request-stands-in-the-section) to
  [Q-SIG-176](#q-sig-176--what-the-section-shows-beyond-the-first-bullet)), every refusal of R-SIG-055 in plain words, read-only without
  `security-settings:write`, both languages, and its renders (`admin-settings-signing`). The tab **Verify** is built (package 3): the last
  step of the client setup page, numbered after Publish where the page offers it, with one sentence for every client
  ([client-setup.html](../../web/src/app/features/setup/client-setup.html), [clients.ts](../../web/src/app/features/setup/clients.ts)).
  **The second half of package 4** ([developer/signatures.md](../developer/signatures.md#package-4-second-half-quaders-own-signatures-at-an-artifact-built)):
  at a version, a file and an image (the shared details, [signature-report.html](../../web/src/app/views/signatures/signature-report.html)),
  Quader's npm signature reads **Origin confirmed by registry · this installation** with its **npm key ID**, its time "by this
  installation's signing identity" and a sentence that says what it confirms, never that no trust anchor knows its signer; under
  **Attestations** (**Attestations and SBOMs** while others stand beside it, in their table) the **Receipt attestation by this
  installation** stands as a block of its own with **Issued**, **Malware scan** (the state the statement names, its scan time and a
  release), **Served** ("As a referrer of the image" or "Through the API", the reason of R-SIG-054 for an image) and **Download
  attestation** (the route of R-SIG-054); its **Technical details** hold the rest of the statement
  ([Q-SIG-238](#q-sig-238--a-receipt-shows-four-facts-and-its-download-the-rest-of-its-statement-is-one-fold-further)). Neither carries
  the badge. A retired identity's entries say that they are no longer served and that Quader signs again, and a retired receipt offers no
  download ([Q-SIG-239](#q-sig-239--a-retired-identitys-entries-say-what-follows-and-a-retired-receipt-offers-no-download)); a receipt made
  before the checks answered names "No verdict yet when it was issued". An artifact without a badge whose list holds such an entry reads
  "No signature of the publisher" ([Q-SIG-235](#q-sig-235--no-badge-beside-listed-signatures-reads-no-signature-of-the-publisher)). The
  section **Signing identity** shows the job's backlog in one quiet line
  ([Q-SIG-236](#q-sig-236--the-jobs-backlog-is-one-quiet-line-of-the-section-signing-identity)). Rendered from the profile's real answers
  changed in the browser ([Q-SIG-237](#q-sig-237--quaders-own-entries-are-rendered-from-the-real-answers-changed-in-the-browser)): the
  state `issued` of `package-version-signatures` (a local npm version with Quader's signature and receipt beside a publisher's provenance),
  the state `receipt` of `package-version-signatures-oci` (a referrer) and its state `untrusted` (an image whose older cosign signature
  puts its receipt behind the API, with the reason), the state `details` of `files-signatures` (a generic file with its receipt beside three
  signature files) and the states `upload` (a busy backlog) and `helm` ("Nothing waits to be signed") of `admin-settings-signing`. Not
  shown: a receipt that waits for its checks
  ([H-1241](#h-1241--a-receipt-that-waits-for-its-checks-is-not-shown-at-the-artifact)). **With this half the second stage is whole.**

Acceptance criteria:

- Web unit (Vitest, under a second): the section in each state; controls only for the holder of `security-settings:write`; the amber and
  red validity; the tab **Verify** hidden without an identity. Built for the section
  ([signing-identity.spec.ts](../../web/src/app/features/settings/signing-identity.spec.ts), seven cases, about 0.3 s); the tab's case
  in [client-setup.spec.ts](../../web/src/app/features/setup/client-setup.spec.ts) (`R-SIG-056 the step Verify …`, about 1 s).
- Web unit (Vitest, under a second), the second half: Quader's npm signature with its words and key ID, no badge and no "No trust anchor
  knows this signer"; a retired identity's signature and the words of Q-SIG-235; the receipt with its issue time, malware state, "As a
  referrer of the image", **Download attestation** and its fold, beside an SBOM in its table, with axe; an image's receipt through the API
  with its reason, a retired receipt without a download and with "No verdict yet when it was issued"; the backlog line busy, idle and absent.
  Built: four cases in [signatures.spec.ts](../../web/src/app/shared/signatures/signatures.spec.ts) and one in
  [signing-identity.spec.ts](../../web/src/app/features/settings/signing-identity.spec.ts), about 0.25 s of test time together.

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

- **Built:** the seven events ([events_signing.go](../../internal/audit/events_signing.go)), the five metrics ([job/metrics.go](../../internal/signing/job/metrics.go)), the four alerts with their promtool tests. Package 2 registers the kind `npm`: `quader_signing_operations_total{kind="npm"}` counts one operation per row of work (one operation can sign several versions of a package, Q-SIG-157), `quader_signing_backlog` and `quader_signing_oldest_due_seconds` carry `kind="npm"`, and `QuaderSigningBacklog` watches it. No event is added: a signature of npm is not an audit event, and `signing.identity.retired` counts the identity's signatures as before. Package 3 registers the kind `attestation`: `quader_signing_operations_total{kind="attestation"}` counts the attestations made (an operation that ends without a signature, an item that is no subject or an attestation that is as it should be, counts none), and `quader_signing_backlog` and `quader_signing_oldest_due_seconds` show the kind `attestation`; no event, metric or alert of its own: an attestation is a write of the job, as each npm signature is.

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

- **Validation** at start, with the key named: `cert_file` and `key_file` both or neither, `ca_file` only with them, each a clean
  absolute path; `reload_interval` from 1 s to 1 h ([Q-SIG-151](#q-sig-151--the-identitys-files-are-read-again-from-every-second-as-the-tls-pairs-are));
  `announce_period` from 0 s to 24 h; `wait_for_checks` from 0 s to 7 days; `concurrency` from 1 to 64; `max_attempts`
  from 1 to 20.
- **The chart's values** are R-HA-071's (`signing.identity.existingSecret`, `signing.identity.certManager`).
- **What it costs:** no Deployment, image or module more (sigstore-go and `golang.org/x/crypto` are in the build); in PostgreSQL about 150
  bytes and a row per npm signature and about 2.5 KiB and a row per attestation, about 3 GB for a million attested artifacts (an
  estimate), with as much written again per rotation (R-SIG-048); for OCI two small objects per attested image in the object store; tens of
  microseconds of the worker's CPU per signature; a revision per npm package and signing run.
- **Built:** `signing.enabled`, `signing.identity.*`, `signing.concurrency`, `signing.max_attempts` and `jobs.workers.signing`
  ([config/signing.go](../../internal/config/signing.go)); the keys `signing.attestations.*` with the attestations (package 3,
  [Q-SIG-152](#q-sig-152--the-identitys-migration-adds-the-column-of-the-issued-signatures-the-attestations-keys-come-with-the-attestations));
  `wait_for_checks` is bounded 0 s to 168 h (7 days). npm's signing adds no key: `signing.enabled` decides it alone, whatever
  `signatures.enabled` of stage 1 says (Q-SIG-161), and with it false a publish writes no work and the job takes none, while the stored
  signatures stay served.

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

- **Built:** the three tables are `data`, and `signing_attestations` of package 3 too ([tables_signing.go](../../internal/meta/postgres/tables_signing.go)); the journal re-applies a removal and a retirement ([store/journal.go](../../internal/signing/store/journal.go)); every replica reads the identities from a snapshot that the change area `signing_identities` refreshes ([store/snapshot.go](../../internal/signing/store/snapshot.go)). Not shown at the integration tier: a full backup restored into a fresh installation; the test shows the sealed key opening under its row's id with the installation's key ring, which a restore keeps. Package 2: npm's signatures are rows of stage 1's tables, which a backup holds as `data`, and the set of identities the documents serve is `signing_settings.served`; a removal or a retirement the journal re-applies after a restore changes that set, and the job's next run raises the revisions of the packages it concerns (Q-SIG-160), while discovery signs what the restored rows lack.

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

Measured for package 3 (the attestations; 2026-11-21, a load of 15 to 48): unit in the job's mode (`-race -covermode=atomic
-coverpkg=./...`) 0.03 s of test time in `internal/signing/statement` and in `internal/signing/bundle`, under 0.01 s in
`internal/signing/attest` and for the job's new case, each new package about 1.4 s with its binary's start; the kit's attestation question
over every format's gate cases a few milliseconds; integration area `signing` 5.9 s and 2.8 s for the two new tests (shard `d`), the
checks of the gate's readers and of npm's tab verify within them (a few HTTP calls); web unit about 1 s for the step Verify; end to end
two cases in `e2e (oci)`: R-SIG-052's (numbers there) and the pusher's `cosign sign` in a gated repository of R-SIG-064, 3.2 s on the
set's fixture (0.4 s for `cosign sign`, the reader served 0.35 s after it; measured 2026-11-22). No new leg.

Measured for package 1 (the identity, its chart, its API; 2026-11-21, the development machine under the load of other builders): unit
about 0.15 s of test time in `internal/signing`, 0.6 s in `internal/signing/job` (two RSA keys for the refusals), 0.6 s in
`internal/signing/key`, 1.5 s for the API's two tests; chart 0.5 s for `TestR_HA_071` and the four alerts' promtool cases; integration area
`signing` 11.6 s for its package with two tests (5.8 s the files, 4.4 s the upload, the rest the stack's start), shard `b`; the replay in
the area `backup` 0.7 s, shard `c`. The integration tests of R-SIG-048's npm half, R-SIG-049's signing of a publish, R-SIG-050 to R-SIG-054
and the end-to-end cases are the later packages'.

Measured for package 2 (npm's signatures; 2026-11-21, the development machine at a load of about 10): unit, in the job's mode
(`-race -covermode=atomic -coverpkg=./...`), under 0.1 s of test time for the new tests together (`internal/signing` 0.00 s, `internal/signing/job`
0.00 s, `internal/formats/npm` 0.06 s, the API's subtest 0.00 s, the npm test upstream's control call a few milliseconds of
`TestControlAPI`), in packages the unit run tests already, so no package's start is added; integration area `signing` 5.3 s for
`TestR_SIG_050_QuaderSignsTheVersionsOfLocalNpmRepositories` (one stack for R-SIG-048's npm half, R-SIG-049 and R-SIG-050 instead of three),
the area 16.9 s now, shard `b`; end to end 4.9 s for `TestNPMSignatures_R_SIG_050_…` beside the fixture and the scatter profile that the set
`e2e (npm)` builds anyway, a package of a few hundred bytes and left-pad's recorded tarball, no new leg and no new client image (the npm test
upstream gains a control call). The legs gain: the unit leg under 0.1 s, shard `b` of the integration about 5 s, `e2e (npm)` about 5 s. With
25 formats: nothing more, because no other format signs as a registry in stage 2. Added after the rebase onto the gate per repository: the
subtest of the same integration test that meets the gate with Quader's real row (0.13 s: two patches of the repository and one refused
download) and one web unit case of the tab **Signatures** with the fake's issued entry (22 ms, in a spec file the web run starts anyway).

What only a real client shows and is not tested: `cosign verify-blob-attestation` against a file's bundle (the same verifier library as
the unit test), Kyverno's and other policy engines' verification in a cluster. Each is measured once by hand when the package that builds
it lands, and the result written into this requirement. Package 3 did not run either: a file's bundle is verified with sigstore-go's
verifier in the integration test, not with cosign, and no cluster engine was installed.

## The gate per repository (stage 3)

Status: packages 1 (the gate), 2 (releases, previews and the API) and 3 (the screens) built. The owner decided it on 2026-11-21 in his working paper, against the development agent's recommendation of no
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
| 1 · The gate | the member and its order (R-SIG-061); the decision (R-SIG-062); the contract's `Gate`, every format's classification, `oci_manifests.attachment`, kit check 18's and check 17's cases (R-SIG-063, R-SIG-068); the serve points and OCI's handlers (R-SIG-064); the view and the scope (R-SIG-065); the subtraction and its two columns (R-SIG-066); the reading of stage 1's fetch state, the priority work, the wait and the held fill (R-SIG-067); one decision, one wait (R-SIG-069); the event of a refusal, the table of refusals, the metrics, the alert, the configuration (R-SIG-073, R-SIG-075, R-SIG-076) | stage 1's packages 1 (on `main`), 2 (verification and trust: the trust rows, the anchors and their snapshot, the job and its priorities) and 3 (the background fetch, which writes and works `signature_fetches`); stage 2's column `signatures.identity_id` (package 1, on `main`), by which the gate leaves out Quader's own signatures | unit under 2 s, the kit milliseconds per format, integration about 14 s (R-SIG-061, R-SIG-064 to R-SIG-067) |
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

Built (package 1): the members `onlyVerified` and `anchors` of `repository.SignatureSettings`, read into the replica's snapshot
(`Repository.SignatureGate`); the order and the replay in [internal/signatures/gateset](../../internal/signatures/gateset/setting.go); the
validation (`validateSignatures`, `checkSignatureAnchors` against the anchors of the scope in the write's transaction, only for the
references a write adds), the weakening refusal (`weakenedSignatures`, `403 signature_weakening_refused`), `block: lifted` with
`signatures` in `weakened`, the part `signatures` of `repository.narrowed` and its replay. On Debian the members answer `422
not_applicable` only for a value that does something (`onlyVerified: true`, a non-empty list): `false` and `[]` are the defaults a screen
may send ([Q-SIG-133](#q-sig-133--debian-refuses-only-a-gate-setting-that-would-do-something)). Anchors stored while the gate is off count
as any anchor for the order and the replay ([Q-SIG-134](#q-sig-134--anchors-stored-while-the-gate-is-off-are-no-constraint)). A well-known
repository of `upstream` takes the whole member from an administrator, a system one keeps it at the product's value
([Q-SIG-138](#q-sig-138--a-well-known-repository-takes-the-member-from-an-administrator-a-system-one-keeps-it)).

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

Built (package 1): `gate.Judge` and `judgeRow` ([decide.go](../../internal/signatures/gate/decide.go)) over the rows the store reads
(`SignatureGateRows`, the roles `publisher` and `provenance` only, so a registry's signature and an attestation are never read). Package 2
reads a release of the gate's repository (`signature_releases`, R-SIG-070) only where the signatures do not pass, so a passing download
reads no more than before ([Q-SIG-205](#q-sig-205--a-release-is-read-only-where-the-signatures-do-not-pass)). The statement reads whether `signatures.identity_id`
is set (`issued`, stage 2's column), and `Judge` leaves such a row out (`Row.Issued`), so Quader's own signature counts for nothing whatever
its role or its trust row; the unit table holds one of Quader's own whose chain ends in an anchor (missing), beside a pending one (pending)
and beside a publisher's (passes).

Acceptance criteria:

- Unit (`internal/signatures/gate`, milliseconds): the table as signature sets to a decision and a reason, with any and with chosen anchors:
  empty; one pending; one valid without a trust row; one internal; one public whose anchor is not chosen; one untrusted and one invalid; one
  blocked beside one internal; one registry signature only; one of Quader's own attestations whose chain ends in an anchor; a release.

### R-SIG-063 · What the gate judges: the format names it
Priority: **P1** · Covers: quader-only

The gate judges artifacts, never what carries their signatures; which stored item is which is the format's knowledge
([Q-SIG-81](#q-sig-81--the-format-names-what-the-gate-judges-and-the-contract-rises-by-one)). The adapter of R-SIG-022 gains a method, and
`ContractVersion` rises by one in the change that adds it (to 9, as stage 2 was not built then; stage 2's receipt attestations raised it
to 10, [Q-SIG-132](#q-sig-132--the-contract-rises-to-version-9-and-signatureverdicts-gains-refuse)); the block is written into
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
  and then its platform's manifest by digest; the second request passes on the index's signature (measured with Docker 29.9.3 on
  2026-11-21, R-SIG-078).
- **NuGet:** a `.snupkg` has the id and the version of its `.nupkg` ([NuGet's documentation on symbol
  packages](https://learn.microsoft.com/en-us/nuget/create-packages/symbol-packages-snupkg), read 2026-11-21) and passes on the `.nupkg`'s
  signature or its own; whether authors or nuget.org sign symbol packages was not verified (nuget.org admits a `.p7s` in them, the same page).
- **Kit check 18** (R-SIG-022) gains the cases of the table: `Fixture.GateCases` names a stored item per row the format fills, with the kind
  and the covering digests it expects; a format without signatures answers `GateNone` for every content case. The test format's defects
  `GatesAttachment` (an attachment named an artifact) and `PassesArtifact` (an artifact named `GateNone`) fail it.

Built (package 1): `formats.SignatureAdapter.Gate`, `GateKind`, `GateItem`, `GateSubject`, `GateNoneFor`; the contract rises from 8 to
9, not to 10, because stage 2 was not built then ([Q-SIG-132](#q-sig-132--the-contract-rises-to-version-9-and-signatureverdicts-gains-refuse)).
Every format on `main` implements it as the table says; NuGet, npm and Ansible Galaxy name what they derive or store as documents
`GateNone` ([Q-SIG-143](#q-sig-143--what-a-format-derives-or-stores-as-a-document-is-gatenone)); OCI records `oci_manifests.attachment` at
the commit (`oci.IsAttachment`, migration 00102) and reads it with the indexes that list a manifest (`Store.GateFacts`). Kit check 18 has
`Fixture.GateCases` and the defects `GatesAttachment` and `PassesArtifact`; a format without signatures is held to `GateNone` over every
content case.

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
- **A document a client reads before the artifact, and whose error text it shows while it shows only the status of the artifact's**
  ([Q-SIG-241](#q-sig-241--what-a-client-is-shown-where-it-prints-no-body-of-the-artifacts-refusal)): Ansible Galaxy's version document,
  which `ansible-galaxy` reads before the archive, answers what the archive's download would answer. The format asks the three checks of
  R-SIG-069 of the stored archive without serving it (`ItemStore.Check`; through a virtual repository `Resolver.Check`, which walks the
  stored tiers of [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-019 step 6 and fetches nothing), and answers their refusal or
  pending answer in its own error shape; the archive's download decides again. An archive no repository of the request stores yet (a remote's
  first download) is judged at its download only
  ([H-1541](#h-1541--ansible-galaxy-shows-only-the-status-of-a-refusal-of-an-archive-that-no-repository-stored-before)).
- **A writer reads to sign** ([Q-SIG-211](#q-sig-211--a-writer-of-the-repository-reads-through-the-gate-where-the-formats-signing-tools-read-the-artifact)):
  where a format's signing tools read the artifact from the repository before they push its signature, which is OCI (`cosign sign`
  `GET`s, `notation sign` `HEAD`s the manifest, by tag and by digest), a principal that may write (`deploy`) to the repository reads it
  through the repository's signature gate. The right is asked only where the gate would refuse or hold the download, never on the pass path,
  and only for a request that addresses the repository holding the item. The malware gate and the vulnerability block still decide. A token
  with the write right therefore pulls an unsigned image of a gated repository
  ([H-1229](../security/artifact-signatures.md#h-1229--a-holder-of-the-write-right-pulls-unverified-images-of-a-gated-oci-repository)),
  and each such read leaves a trace: a served download that passed only for the writer is counted in
  `quader_signatures_downloads_by_writer_total` and written as `signature.download_by_writer` (R-SIG-075), which names the repository, the
  artifact in its client's words, the caller and the reason a reader is refused for, at most once a minute per repository, path, caller and
  client address on each replica ([Q-SIG-240](#q-sig-240--a-writers-head-and-get-of-one-manifest-within-a-minute-are-one-event)). A
  writer's `HEAD`, which reads no content (a push's check whether a manifest exists, a signing tool's resolution of a tag), is decided and
  served as a `GET` and neither counted nor written
  ([Q-SIG-422](#q-sig-422--a-writers-head-through-the-gate-is-not-a-read-it-is-served-and-neither-counted-nor-written)).
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

Built (package 1): the format host's `itemStore.Serve` (`serveChecks`, [signature_gate.go](../../internal/formathost/signature_gate.go)),
`files.Service.Stat`, and the remote OCI tag through `Host.Signatures().Refuse` after `refuseBlockedImage`. Where no gate applies the
download reads no row: `Gate.Applies` reads the snapshot alone. Where one applies the gate reads its rows in a statement of its own after
the item lookup, not joined into it ([Q-SIG-131](#q-sig-131--the-gate-reads-its-rows-in-a-statement-of-its-own-where-a-gate-applies)).
Measured on 2026-11-21 (R-SIG-078): while the gate is on, `cosign sign` (`GET`) and `notation sign` (`HEAD`) read the image's manifest
before they sign it and are refused, so an image pushed into a gated repository cannot be signed there. Repaired the same day (Q-SIG-211):
the format declares it (`SignatureDeclaration.ReadToSign`, OCI alone), the format host gives the gate the caller's write right as a question
(`Download.MayWrite`, decided as the listing form of the decision without an audit event), and the gate asks it only after a decision that
is no pass (`Gate.writerRead`); `Verdict.Writer` says so. The trace (owner, 2026-11-22): `Serve.Run`, once the download is served and no
other check refused it, and `Check` call `Gate.WriterRead`, which counts the metric and queues the asynchronous event; a verified pass and a
refusal never reach it, and it adds no statement to the request. Measured again with the real cosign 3.1.4 (2026-11-22, `e2e (oci)`): the pusher's
`cosign sign --key` by digest without a log exits 0 in a gated repository, and a reader's `docker pull` is refused before the signature and
served once Quader checked it; `notation sign` is not measured again.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_064_…`, about 3 s, files of a kilobyte, the release key of R-SIG-042 as an anchor of the
  installation): with the gate on `files-internal`, an unsigned file answers 403 through `/files/` (`GET`, `HEAD`, and `If-None-Match` with
  its `ETag`); a file with a valid `.asc` of the release key is served, and its `.asc` too; with the gate off both are served, and a
  download's statements are as many as without stage 3.
- Unit (`internal/signatures/gate`, milliseconds): a writer's read passes a refused download and is asked only then; a reader's is refused
  (`TestR_SIG_064_AWriterReadsToSignAndTheAnswersNameTheArtifactAsItsClient`).
- Unit (`internal/signatures/gate`, milliseconds, no payload): a writer's served read through `Serve.Run` and through `Check` leaves one
  `signature.download_by_writer` (outcome `warning`, the caller as actor, `repository`, `path`, `artifact` in the client's words, `sha256`,
  `reason`, `pending` included) and counts `quader_signatures_downloads_by_writer_total{reason}`; its two `GET`s within the minute are
  one event and two counts, its `HEAD` neither (Q-SIG-422), another caller is an event of its own; a verified pass, a reader's refusal and a writer's pass that the malware
  gate then refuses write none, and the verified pass reads one statement of rows (`TestR_SIG_064_AWritersReadIsAuditedAndCounted`).
- Integration (area `signatures`, in `TestR_SIG_070_…`, under a second): an image pushed into a gated OCI repository is read by the pusher
  by digest with `HEAD` and `GET` (200), the `HEAD` counted nowhere and the `GET` once in the replica's
  `quader_signatures_downloads_by_writer_total{reason="missing"}`, and refused to a reader with the text that names `app:1.0`.
- End to end (the set `e2e (oci)`, `TestOCIGate_R_SIG_064_ThePusherSignsWithCosignAndAReaderPullsOnlyThen`, an image of a few hundred
  bytes, a key pair cosign makes in the pod and its anchor): Docker pushes an unsigned image into `oci-e2e-gated` (`onlyVerified`); a
  reader's `docker pull` is refused with the gate's text; the pusher's `cosign sign --key --signing-config nolog.json` by digest exits 0;
  the reader's pull is served once the signature is checked.

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

Built (package 1): `gate.Download.Members` is, for a virtual addressed repository whose gate is on, its members the caller may read
(`Host.readableMembers`, `MemberUsableOn` on the request's index); the addressed gate reads in its project's scope, the holder's in its own;
both walks end on the two answers (`errs.IsMemberMiss` and `errs.IsMemberFailure`, which the virtual engine and OCI's walk call, name neither: an answer no rule names ends the walk, Q-OCI-245). The test is the unit tier's
(`TestR_SIG_065_EachGateJudgesInItsViewAndScope`): the integration test of npm with the company's Sigstore is not written
([Q-SIG-141](#q-sig-141--the-integration-area-tests-the-gate-through-files-the-rest-at-the-unit-tier)).

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
  changed; a Sigstore identity's issuer, pattern or trust root changes); and `signature_blocks.anchor_epoch bigint NOT NULL DEFAULT 0`, the
  anchor epoch the block's creation raised. Neither is a revision a client holds, so neither carries a name the rule of revision columns
  claims ([architecture.md](architecture.md) R-ARCH-009; [Q-SIG-144](#q-sig-144--the-blocks-epoch-is-named-anchor_epoch)).
- **An anchor of a row's `anchor_ids` counts** only while it is in force in the snapshot (it exists, it is switched on, and a project anchor
  only while the switch is on) and its `narrowed_epoch` is not above the row's `anchor_epoch`. A block whose `anchor_epoch` is above the row's and that
  matches one of the signature's `key_ids`, or its issuer and identity, makes the row `blocked`. What remains decides the state: no anchor,
  `untrusted`; shipped anchors only, `public`; else `internal`.
- An anchor that a write narrowed counts for no older row, also where it still knows the signer: until the recomputation reaches those rows
  their artifacts are refused (fail closed). The impact of R-SIG-071 says so before the write.
- **A widening waits for the recomputation:** an anchor added or switched on, a block lifted, the switch turned on reach the gate when the
  trust job has written the rows (R-SIG-012).
- The gate's statement reads of each trust row `state`, `anchor_ids` and `anchor_epoch`, and of its signature `key_ids`, the issuer and the identity
  (R-SIG-005); nothing else.

Built (package 1): the two columns (migration 00102); `UpdateAnchor` sets `narrowed_epoch` for a switch off or any change of material,
and `quader migrate` for a shipped or declared anchor whose material changed
([Q-SIG-135](#q-sig-135--any-change-of-an-anchors-material-narrows-it-for-the-gate)); `CreateBlock` sets the block's `anchor_epoch`; the
replica's snapshot (`signatureGateComponent`, change area `signature_anchors`, a minute as safety period) and `Snapshot.Subtract`.

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

Built (package 1): the gate reads `signature_fetches` (`SignatureGateFetches`); the priority work is `SignatureGatePrioritize` (the
subjects' pending signatures due at priority 0 now, the job `signature-verify` woken), `SignatureGateRaiseFetch` (the waiting job
`signature-fetch` of a due target at priority 0 now, a running one left alone) and a kick of the queues `signatures` and `index`; the
signal area `signature_verdict` is raised after the commit of a verdict and after a fetch's run with the subject's digest, and with `*` after
a batch of the trust step ([Q-SIG-139](#q-sig-139--the-trust-step-signals-every-waiting-download)). The held fill is `holdFill`; a body the
remote engine would pass unstored during an outage of the object store answers pending where a gate applies
([Q-SIG-140](#q-sig-140--an-unstored-body-through-a-gated-remote-answers-pending)).

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
| npm | `403`, `{"error": <text>, "message": <text>, "code": "signature_not_verified"}`; npm prints `E403` and the text | `503` with `Retry-After`; npm retries twice and prints `E503` and the text |
| NuGet | `403`, one line `signature_not_verified: <text>`, and the text in the header `X-NuGet-Warning`, which `dotnet restore` prints as `warning : <text>` before each attempt's status line (Q-SIG-241) | `503` with `Retry-After` and the text in `X-NuGet-Warning`; the client retries at once, five times, and prints the warning each time |
| OCI | `403`, `{"errors": [{"code": "DENIED", "message": <text>, "detail": null}]}`; Docker 29 asks by `HEAD`, then `GET`s the body and prints `error from registry: <text>`; the kubelet `denied: <text>`; Podman `denied: <text>` | `503`, `{"errors": [{"code": "UNAVAILABLE", …}]}` with `Retry-After`; Docker asks by `HEAD`, retries it once and prints only `503 Service Unavailable`; Podman `GET`s it, retries it and prints only the status ([H-1540](#h-1540--docker-podman-and-the-kubelet-show-no-text-while-a-signature-is-being-checked)) |
| Ansible Galaxy | `403`, `{"errors": [{"status": "403", "code": "signature_not_verified", "title": …, "detail": <text>}]}`, at the archive and at its version document (Q-SIG-241); `ansible-galaxy` reads the document first and prints `(HTTP Code: 403, Message: <text> Code: signature_not_verified)` | `503` with `Retry-After`, at both; `ansible-galaxy` prints the document's text and does not retry |
| generic (`/files/`) | `403` problem with `code`, `reason` and `sha256` | `503` problem with `Retry-After` |

What each client prints and whether it retries is the malware gate's record of the same shapes ([malware-scanning.md](malware-scanning.md)
R-MAL-016, Q-MAL-3, read in the clients' sources on 2026-11-20); this stage adds the header of NuGet and the version document of Ansible
Galaxy, which carry the malware gate's and the vulnerability block's answers too (Q-SIG-241). Measured with the real clients on 2026-11-21
(R-SIG-078): npm and curl show the text; Docker 29.9.3 shows it for a refusal without the `denied:` prefix the table of R-MAL-016 names;
`dotnet restore` (NuGet 7.9.0) and `ansible-galaxy` 2.21.6 showed only the status, NuGet retries a 403 and a 503 five times without waiting
for `Retry-After`, and `ansible-galaxy` does not retry. Measured again on 2026-11-23 with Q-SIG-241 built: `dotnet restore` of the .NET SDK
10.0.403 prints the refusal's and the pending answer's text as a warning before each attempt; `ansible-galaxy` 2.21.6 prints both from the
version document and stops there (the pending one after the 25 s wait); Docker 29.9.3 (two `HEAD`s, 50 s) and Podman 5.8.9 (`GET`s, 104 s)
print only `503 Service Unavailable` for a pending image, and Podman prints a refusal after `denied:`. Debian answers none of them: its
repositories have no gate (R-SIG-061).

- **The texts** name the addressed repository by its qualified name, never another repository, and no signer (a signer's name is text an
  uploader chose; the screens show it as text). They name the artifact in its client's words
  ([Q-SIG-213](#q-sig-213--the-gates-answers-name-the-artifact-in-its-clients-words)): `<name>:<tag>` or `<name>@<digest>` for an image,
  `<name>@<version>` for npm, `<id> <version>` for NuGet, `<namespace>.<name>:<version>` for Ansible Galaxy, the path for a generic file
  (`<artifact>` below); and they speak of the signature of its publisher or its build, the only one that counts (R-SIG-062), so that an
  artifact with a registry's signature is not told it has none
  ([Q-SIG-212](#q-sig-212--a-refusal-says-what-is-missing-a-signature-of-the-publisher-or-the-build)):

  | `reason` | Text |
  |---|---|
  | `missing` | "Quader refused `<artifact>` of `<project>/<repo>`: it has no signature of its publisher or its build, and this repository serves only artifacts with a verified signature." |
  | `untrusted` | "Quader refused `<artifact>` of `<project>/<repo>`: no trust anchor this repository accepts knows the signer of its publisher's or its build's signature." |
  | `not_chosen` | "Quader refused `<artifact>` of `<project>/<repo>`: its publisher's or its build's signature is verified only by trust anchors this repository does not accept." |
  | `invalid` | "Quader refused `<artifact>` of `<project>/<repo>`: its publisher's or its build's signature does not match its content." |
  | `blocked` | "Quader refused `<artifact>` of `<project>/<repo>`: an administrator blocked the signer of its publisher's or its build's signature." |
  | `not_checked` | "Quader refused `<artifact>` of `<project>/<repo>`: its publisher's or its build's signature could not be checked: Quader cannot read it, or its check failed." |
  | (pending) | "Quader is checking the signatures of `<artifact>`. Try again in a minute." |

  Every refusal adds "Details: <the address of the version's or the file's page>".
- **Kit check 17** ([malware-scanning.md](malware-scanning.md) R-MAL-029) holds the two answers for every format as it holds the malware
  gate's: the status, `Cache-Control: no-store`, `Retry-After` for the pending one, no byte of the item.

Built (package 1): `errs.CodeSignatureNotVerified` and `errs.CodeSignatureCheckPending`; the answers of `gate.Gate.refusal` and
`pendingError`; kit check 17 renders both for every format. The details link is `<server.base_url>/ui/projects/<project>/repositories/<key>/files/<path>`.
Repaired after the measurement of 2026-11-21: the texts of Q-SIG-212, and the artifact's name of Q-SIG-213 from the format's
`SignatureDeclaration.ArtifactName` (OCI, npm, NuGet, Ansible Galaxy; a generic file and `/files/` keep the path), which the format host
hands to the gate as `Download.Name`. Repaired after the measurement of 2026-11-23 (Q-SIG-241, which replaces Q-SIG-214): NuGet's
`WriteError` puts the text of the answers of the three checks (`signature_not_verified`, `signature_check_pending`, `malware_found`,
`malware_not_checked`, `malware_check_pending`, `vulnerability_blocked`, `vulnerability_data_missing`) into `X-NuGet-Warning` as printable
ASCII on one line, and no other error's ([errors.go](../../internal/formats/nuget/errors.go)); Ansible Galaxy's version document of every class
asks the archive's checks (`localArchiveChecks`, `remoteArchiveChecks`, `virtualArchiveChecks` in
[archive_checks.go](../../internal/formats/ansible/archive_checks.go)) through `ItemStore.Check`, which runs the serve point's checks with the
malware gate asked as at a download, and `Resolver.Check`, the virtual engine's `Stored` walk of the first two tiers.

Acceptance criteria:

- Unit (the format contract's kit, milliseconds per format): check 17 renders `403` for `signature_not_verified` and `503` with
  `Retry-After` for `signature_check_pending`, both with `Cache-Control: no-store`, for every registered format and the test format.
- Unit (`internal/formats/<id>`, microseconds): each declaration's `ArtifactName` (`TestR_SIG_068_TheGatesAnswersNameTheArtifactAsTheClientDoes`);
  the texts in `internal/signatures/gate`.
- Unit (`internal/formats/nuget`, microseconds, no payload): the seven answers of the checks carry their text in `X-NuGet-Warning`, a byte
  outside printable ASCII is replaced, and a 404, a 403 of a permission, a 429 and a 401 carry none
  (`TestR_SIG_068_TheChecksOfADownloadReachNuGetsClientsAsAWarning`).
- Unit (`internal/formats/ansible`, milliseconds, a collection of a few hundred bytes): a local version document answers the archive's refusal
  or pending answer in Galaxy's shape and is served on a pass, the check asked of the archive's path
  (`TestR_SIG_068_TheVersionDocumentAnswersWhatTheArchivesDownloadWould`); a remote's asks it of a cached archive only, a virtual's through
  `Resolver.Check` (`TestR_SIG_068_ARemotesAndAVirtualsVersionDocumentAnswerWhatTheirArchivesDownloadWould`).
- Unit (`internal/virtual`, milliseconds): `Stored` takes the first stored hit of the local members and the remote members' caches, asks no
  upstream, passes over a lost cache, ends at another failure and counts no resolution
  (`TestR_SIG_068_StoredAnswersTheFirstStoredHitWithoutServingIt`).
- Integration (area `ansiblelocal`, in `TestR_SIG_031_…`, under a second): with the gate switched on, the version document of a version
  without a signature answers `403 signature_not_verified` with the gate's text, and its archive `403`.
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

**A fourth check** (specified on 2026-11-23, not built): a remote repository's delivery rules ([remote-curation.md](remote-curation.md)
R-CUR-003) decide a stored item at the same serve points, in this order: `malware_found`, `vulnerability_blocked`,
`not_allowed_in_repository`, `signature_not_verified`, `malware_not_checked`, `vulnerability_data_missing`. A finding in the content holds
whatever the rules say and comes first; the rules come before every check that is missing or pending, and their refusal never waits
(Q-CUR-7).

Built (package 1): `gate.Serve` ([serve.go](../../internal/signatures/gate/serve.go)), which both serve points build; the malware gate's
`Check` is `Decide`, `Refuse` and `Begin` with a `Wait` of its own, and its answers, events and metrics stay. The unit test is in
`internal/signatures/gate`, where the combination lives ([Q-SIG-136](#q-sig-136--one-decision-one-wait-lives-in-the-gates-package)).

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

Built (package 2 of stage 3): the table (migration 00104), the store's `Release`, `Withdraw` and `Releases`
([store/releases.go](../../internal/signatures/store/releases.go)), the operations of R-SIG-073, and the gate's use of a release at the serve
point and in every judgement of the API (`Gate.Decide`, `Gate.Assess`). Where it deviates or adds: the gate is judged as if it were on, with
the stored anchors, so a release can be granted before the gate is switched on
([Q-SIG-196](#q-sig-196--a-release-is-judged-as-the-gate-switched-on-with-the-stored-anchors)); a pending artifact can be released, with the
reason `pending` ([Q-SIG-197](#q-sig-197--a-pending-artifact-can-be-released)); the repository (a virtual one: a member the caller reads) must
hold a stored file with the digest, else 404
([Q-SIG-198](#q-sig-198--a-release-names-a-stored-file-of-the-repository)); the second half of the permission (`read` or
`content:metadata-read`) answers 404 without it, as for a repository that does not exist. A release is kept by the purge of a retired
well-known repository ([Q-SIG-201](#q-sig-201--the-purge-of-a-retired-repository-keeps-its-releases)); it keeps passing its artifact after a later block of the signer ([H-1223](../security/artifact-signatures.md#h-1223--a-release-keeps-passing-an-artifact-after-its-signer-is-blocked)).

Acceptance criteria:

- Unit (`internal/signatures/gate`, milliseconds): a release passes in its repository alone, at the download and in the API's judgement, a
  blocked signer's artifact too; a released index passes its platform manifest; a passing download reads no release
  (`TestR_SIG_070_AReleasePassesInItsRepositoryAlone`).
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

Built (package 2 of stage 3): `POST /api/v1/signatures/impact` and `Gate.Impact` ([gate/impact.go](../../internal/signatures/gate/impact.go)):
the gate's own decision of each candidate artifact under the snapshot of anchors and blocks of now and under the snapshot the change would
make, so the count subtracts exactly as the gate does (R-SIG-066)
([Q-SIG-202](#q-sig-202--the-impact-is-the-gates-decision-under-two-snapshots-counted-per-signed-artifact-and-repository)); the index replaces
00098's partial one (migration 00104). Where it deviates or adds: the answer carries `bound: "exact" | "upper"` always; it counts an artifact
by its signed subject in each gated repository whose view attaches it (an index once, not its platform manifests, and a virtual repository
again beside its member); a block of a key, fingerprint or serial reads the signatures that carry one of the value's spellings
(`signatures_keys`), a block of an identity reads at most `preview_limit` signatures that carry an identity and matches the pattern itself ([H-1225](../security/artifact-signatures.md#h-1225--the-impact-of-a-block-of-an-identity-is-a-lower-bound-beyond-the-preview-limit)); a
switched-off anchor or project anchors switched off count from the anchors in force. The permission: the operation admits a holder of
`security-settings:write` or an Owner of some project, and the handler asks the change's own right (a project anchor's Owner, else
`security-settings:write`).

Acceptance criteria:

- Unit (`internal/signatures/gate`, milliseconds): removal, block, narrowing (an upper bound), project anchors off and the limit
  (`TestR_SIG_071_TheImpactCountsWhatStopsPassing`).
- Integration (area `signatures`, in `TestR_SIG_070_…`, about 1 s): with the gate on `files-internal` and on a second generic repository, the
  impact of removing the release key's anchor counts its two signed files; a file also signed by a second key (the company CA in the first
  plan) and a released file are not counted; a block of the key's fingerprint counts the same two.

### R-SIG-072 · The repository's preview before the gate is switched on
Priority: **P1** · Covers: quader-only

([Q-SIG-90](#q-sig-90--a-preview-before-switching-on-and-an-impact-before-a-narrowing-both-bounded)) What keeps a writer of the settings
from locking a team out.

- `GET /api/v1/projects/{projectKey}/repositories/{repoKey}/signatures/preview?anchors=<ref>,<ref>` applies the gate switched on with the
  given anchors (the stored ones without the parameter; present and empty, any anchor;
  [Q-SIG-199](#q-sig-199--the-preview-always-judges-the-gate-switched-on-and-takes-only-the-anchors)) to the repository's stored items: the format's `Gate` in batches and the decision of
  R-SIG-062, without the wait. It answers `{"examined", "complete", "artifacts", "pass", "pending", "refused": {"missing", "untrusted",
  "notChosen", "invalid", "blocked", "notChecked"}, "examples": [{"path", "package", "version", "reason"}]}`, with at most 20 examples, the
  newest first. It reads at most `signatures.gate.preview_limit` items or for 10 s, else `complete: false`.
- A virtual repository previews its own gate over the items of its members that the caller may read; a remote its cache (what it has not
  stored, it does not know).
- Permission: whoever may change the member: `repositories:read-config`, or the project's Owner.
- The section of R-SIG-074 asks before it saves a narrowing whose preview refuses anything, and shows the examples.

Built (package 2 of stage 3): `Gate.Preview` and `Gate.Assess` ([gate/preview.go](../../internal/signatures/gate/preview.go),
[gate/assess.go](../../internal/signatures/gate/assess.go)) in batches of 500 items, each batch one classification per holding repository
(the format's `Gate` for many items at once), one read of rows per 1,000 digests, one of fetch states for a remote of the view and one of
releases for what does not pass. Where it deviates or adds: the items are read in the order of the index of live paths, and the examples
are the newest refused artifacts among those read, by the item's id, not by a read in time order
([Q-SIG-200](#q-sig-200--the-preview-reads-in-the-order-of-the-live-paths-and-keeps-the-newest-refused-as-examples)); a release passes in
the preview as at the download; a Debian repository and a system repository of `upstream` answer `422 not_applicable`, the signatures off
`409 signatures_not_enabled`; `examples` name the holding repository (a member, for a virtual one). A preview of a virtual repository's
members and of a remote's fetch state is tested at the unit tier, as the download's (Q-SIG-141): the revisit Q-SIG-141 names is not taken,
because it needs a Sigstore or a CA of the test in the integration area.

Acceptance criteria:

- Unit (`internal/signatures/gate`, milliseconds): batches, chosen anchors, a signature file that passes as it is, examples newest first, the
  limit (`TestR_SIG_072_ThePreviewJudgesTheStoredItemsInBatches`).
- Integration (area `signatures`, in `TestR_SIG_070_…`, about 1 s): the preview of `files-internal` with files signed by the release key, one
  also by a second key, a released one and an unsigned one answers 3 passing and 1 `missing` with any anchor, and 2 passing, 1 `notChosen`
  and 1 `missing` with only the second key's anchor chosen.

### R-SIG-073 · The API of stage 3
Priority: **P1** · Covers: quader-only

In `api/openapi.yaml` under the tag `signatures`:

| Method | Path | What | Permission |
|---|---|---|---|
| GET | `/api/v1/projects/{projectKey}/repositories/{repoKey}/signatures/preview?anchors=` | R-SIG-072 | `repositories:read-config`, or the project's Owner |
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

Built (package 2 of stage 3): the six operations in `api/openapi.yaml` (`previewRepositorySignatureGate`, `countSignatureImpact`,
`listSignatureRefusals`, `listSignatureReleases`, `createSignatureRelease`, `deleteSignatureRelease`;
[signatures_gate.go](../../internal/api/v1/signatures_gate.go), [signatures_gate_lists.go](../../internal/api/v1/signatures_gate_lists.go)) and
the members ([signatures_gate_members.go](../../internal/api/v1/signatures_gate_members.go)). Where it deviates or adds: each member is what
the repository's own gate decides now, a virtual repository's over the members that hold the version for the caller; a member's own gate is in
its own entry ([Q-SIG-203](#q-sig-203--the-members-refused-and-gate-are-each-repositorys-own-gate)); a version is refused when one of its
artifacts is, pending when one is, and in the version list when the gate of one repository that holds it for the caller refuses it there
([Q-SIG-203](#q-sig-203--the-members-refused-and-gate-are-each-repositorys-own-gate)); `refusedReason` is absent, not null, while the artifact
passes; `gate` is absent while the repository's gate is off, so `onlyVerified` reads true where it is present; `passedBy` is
`{"kind": "signature" | "release", "signature": <id> | null}` ([Q-SIG-204](#q-sig-204--passedby-names-its-kind-and-the-signatures-id)); the
refusals' list sorts by the last refusal, newest first, with the cursor (last refusal, repository, digest); the releases' list answers at most
1,000, newest first, without paging. The refused downloads' release names the reason, the releaser and the time.

Acceptance criteria:

- Unit (`internal/api/v1`, milliseconds, no document): the impact request's one change, the preview's parameter, a version's summary
  (`TestR_SIG_071_TheImpactRequestNamesOneChange`, `TestR_SIG_073_AVersionIsRefusedWhenOneArtifactIs`).
- Integration (area `signatures`, in `TestR_SIG_070_…`, about 2 s): the operations of the table for an administrator, an auditor, a
  `security-admin` without read (404), the Owner of `web` (403 at a release and an installation anchor's impact), a caller without a grant and
  an anonymous caller (401); `GET …/items?path=` of a refused file carries `signatures.refused: true` and its reason, the file's signatures
  answer `gate`; a second refused download raises nothing but `last_at` and `count`.

### R-SIG-074 · The screens of stage 3
Priority: **P1** · Covers: quader-only

Simple by default ([CLAUDE.md](../../CLAUDE.md#product-simple-by-default-details-on-demand)): nothing of the gate is in a creation flow or
in the main view of a repository; a plain user sees why a download is refused where the file is.

- **The repository's settings page** ([api-and-ui.md](api-and-ui.md) R-UI-087): the closed section **Signatures** (R-SIG-040) holds, before
  `fetch`, the checkbox **Only artifacts with a verified signature can be downloaded from this repository**, unchecked by default, with its
  help behind the sign after its label ([api-and-ui.md](api-and-ui.md) R-UI-267, [Q-UI-1628](api-and-ui.md#q-ui-1628--the-help-sign-on-every-settings-page-and-no-info-icon-beside-a-status)): "Artifacts without a
  signature that a trust anchor of this repository knows are refused. A signature says who signed an artifact, not that it is safe."
  Checked, it shows **Accepted trust anchors** with two choices: **Any trust anchor of this repository** (the default),
  with "This includes the public Sigstore, where anyone can sign. Choose anchors to accept only your own signers.", and **These trust
  anchors**, a list of the scope's anchors with title, kind, scope and fingerprint to tick, a removed one shown as **Removed trust anchor**
  in red. Under them the stored setting's state when the section opens: "<n> of <m> artifacts pass. <k> are refused." (R-SIG-072), with a
  link to the refused downloads on the page Signatures under Security audit for its readers.
  - On a virtual repository its help adds "Applies to everything this repository serves from its members."; on a remote with `fetch` off,
    the visible line under **Fetch the upstream's signatures** "Signatures of images are not fetched: an image whose signature no client
    pulled is refused." The fetch's own help: "Quader asks the upstream in the background for the signatures of what this repository
    stores, at most 600 requests an hour. Switch it off for an upstream with tight rate limits, such as Docker Hub without an account."
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

Built (package 3 of stage 3): the section ([repository-signatures.ts](../../web/src/app/features/repositories/repository-signatures.ts), the order
of R-SIG-061 in [signature-gate-order.ts](../../web/src/app/features/repositories/signature-gate-order.ts)) on every repository but a system
one, the fetch of a remote after the gate in the same section and one save; the gate's block above the signatures of a version's tab and a
file's section ([signature-gate.ts](../../web/src/app/views/signatures/signature-gate.ts)); the banner, "Refused here" and no **Download** at a
version and a file; **Release for this repository** and **Withdraw release** with their dialog and question
([gate-release-dialog.ts](../../web/src/app/shared/signatures/gate-release-dialog.ts), [gate-releases.ts](../../web/src/app/shared/signatures/gate-releases.ts));
the refused downloads and the releases in the dashboard's view ([signature-refusals.ts](../../web/src/app/features/vulnerabilities/signature-refusals.ts));
the impact before a removal, a switch-off, a block and project anchors switched off; the line of the client setup; the words of each reason in
one place ([gate-words.ts](../../web/src/app/views/signatures/gate-words.ts)). Where it deviates or adds: the section learns that the checks
are off from the preview's `409 signatures_not_enabled` and reads the preview whenever it opens, so a gate that is off shows what switching it
on would refuse ([Q-SIG-223](#q-sig-223--the-section-reads-the-preview-when-it-opens-and-learns-from-it-that-the-checks-are-off)); an Owner's
controls open exactly the narrowings ([Q-SIG-224](#q-sig-224--a-writer-who-may-not-weaken-gets-the-narrowings-as-controls-and-the-rest-read-only));
the anchors offered come from the project's list and, for `settings:read`, the installation's own
([Q-SIG-225](#q-sig-225--the-anchors-offered-are-the-projects-list-and-for-settingsread-the-installations-own));
an image index that passes on its own signature lists its platforms as signed through it, a refused one names no platform's decision
([Q-SIG-226](#q-sig-226--an-images-platforms-are-said-to-pass-through-their-index-where-the-answer-decides-it));
the section **Releases** lists one repository's releases ([Q-SIG-227](#q-sig-227--the-section-releases-lists-one-repositorys-releases));
the client setup's line comes from the full repository document ([Q-SIG-228](#q-sig-228--the-client-setups-line-is-read-from-the-full-repository-document));
the gate's decision is a block of its own per repository ([Q-SIG-229](#q-sig-229--the-gates-decision-is-a-block-above-the-signatures-and-the-banner-names-the-version));
a block's impact is a second step inside its dialog ([Q-SIG-230](#q-sig-230--the-impact-of-a-block-is-a-step-of-its-dialog-and-an-impact-that-cannot-be-read-asks-nothing-more));
narrowing an anchor's material has no screen to ask on ([Q-SIG-231](#q-sig-231--no-screen-narrows-an-anchors-material-so-none-asks-its-impact));
the dashboard's two lists share the repository filter ([Q-SIG-232](#q-sig-232--the-dashboards-view-shares-its-repository-filter-and-lists-the-refused-downloads-first)).
Rendered on 2026-11-22 (`repository-settings-signature-gate`, `setup-signature-gate`, `package-version-signature-refused`,
`package-version-signature-gate-oci`, `files-signature-refused`, `security-signature-refusals`, the states `impact` and `block-impact` of
`admin-settings-signatures`, and the Debian section in `repository-settings-deb`): the gate off is the profile's real answer, every other
state its answers changed in the browser, since the profile gates no repository.

Acceptance criteria:

- Web unit (Vitest, under a second): the section's states (off, any, chosen with a removed anchor, read-only for an Owner, Debian, checks
  switched off); the confirmation shows the preview's numbers; the banner for a refused file and none for a pending one; **Release for this
  repository** only for its holders. Built: [repository-signature-gate.spec.ts](../../web/src/app/features/repositories/repository-signature-gate.spec.ts)
  (eleven cases, about 0.4 s), and the impact of a removal in [signature-settings.spec.ts](../../web/src/app/features/settings/signature-settings.spec.ts).

### R-SIG-075 · Audit events, metrics, alerts and the Bastion's feed of stage 3
Priority: **P1** · Covers: quader-only

**Audit events** ([operations-and-observability.md](operations-and-observability.md) R-OPS-016):

| Event | Mode | Journal | Recorded when | Key fields in `details` |
|---|---|---|---|---|
| `signature.download_refused` | A | — | a gate refused a download, at most once a minute per repository, path and client address on each replica | `repository`, `path`, `sha256`, `reason` |
| `signature.download_by_writer` | A | — | a gate served a download without an accepted signature only because the caller may write to the repository (R-SIG-064, Q-SIG-211); outcome `warning`, at most once a minute per repository, path, caller and client address on each replica (Q-SIG-240) | `repository`, `path`, `artifact`, `sha256`, `reason` |
| `signature.release.granted` | T | — | R-SIG-070 | `repository`, `sha256`, `refusedReason`, `reason` |
| `signature.release.withdrawn` | T | yes | R-SIG-070 | `repository`, `sha256`, `reason` |

The setting is recorded as every repository setting: `repository.updated`, with `block: lifted` for a weakening, and `repository.narrowed`
(R-SIG-061).

**Metrics** ([operations-and-observability.md](operations-and-observability.md) R-OPS-008), of the roles that serve downloads:

| Metric | Type | Labels |
|---|---|---|
| `quader_signatures_downloads_refused_total` | counter | `reason` (R-SIG-068) |
| `quader_signatures_downloads_by_writer_total` | counter | `reason` (R-SIG-068, and `pending`): what a reader would have been answered; each served download that passed only for the writer, a `HEAD` not (Q-SIG-422) |
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

Built (package 1): `signature.download_refused` (asynchronous, deduplicated a minute per repository, path and client address; a pending
answer is recorded with the reason `pending` as the malware gate records its own,
[Q-SIG-137](#q-sig-137--a-pending-answer-is-recorded-and-counted-as-the-malware-gates)), the two metrics on every role that builds the
gate, the alert `QuaderSignatureGatePending` with its `promtool` test. Built (package 2): the two release events (`signature.release.granted`
in the grant's transaction, `signature.release.withdrawn` with its journal entry `{"repoId", "repository", "sha256"}`), and the Bastion's feed
([feed.go](../../internal/bastionmain/feed.go)): a withdrawal is `content` of its repository; `signature.anchor.removed` and
`signature.signer.blocked`, `signature.anchor.updated` that switched an anchor off or took a fingerprint from it, and
`signature.settings.changed` that switched project anchors off are `content` of every repository whose member `signatures.onlyVerified` is
true when the feed is read (`BastionGatedRepositories`), and of the virtual repositories above them; with no gated repository the entry is
dropped. The feed reads the audit event, whose details say whether an update narrowed; details that do not read are read as a narrowing.
Built (the owner's trace of 2026-11-22, Q-SIG-211): `signature.download_by_writer` and `quader_signatures_downloads_by_writer_total`, written
by `Gate.WriterRead` ([gate.go](../../internal/signatures/gate/gate.go)) through the same asynchronous buffer as the refusal's event.

Acceptance criteria:

- Unit (`internal/audit`, under a second): the four events are in the registry with their schemas and journal flags; the Bastion's
  classification test (`TestR_BAS_015_EveryJournaledEventIsClassified`) classifies the journaled ones; the feed's mapping of each
  (`TestR_SIG_075_TheGatesNarrowingsAreContentEntries`, `internal/bastionmain`, milliseconds).
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

Built (package 1): `config.Signatures.Gate` with its validation; `signature_refusals` registered `transient` and written by the gate
([Q-SIG-142](#q-sig-142--every-refusal-is-written-to-the-list-of-refused-downloads)), removed by the job's discovery step after the
retention; the member travels with the document and its part with the journal. Built (package 2): `signature_releases` registered `data`
and replayed from `signature.release.withdrawn` (`replayReleaseWithdrawn`, by the repository's id); `preview_limit` bounds the preview's items
and the impact's candidates. Integration (area `backup`, within `TestR_SIG_041_WhatNarrowedTrustIsReplayedIntoARestore`): a withdrawn release
is withdrawn again in a restored database.

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
  referrer is an attachment, which passes as every attachment does (R-SIG-063). npm's registry signature of a local version (built with
  stage 2's package 2) is out of the gate's rows three times over: its role is `registry`, its subject is the SHA-256 of the signed message
  and not the tarball's, and it carries `identity_id` (`Row.Issued`); the integration area `signing` shows the real row meet the gate: the
  tarball Quader signed is refused as `missing` in a gated local repository while its document keeps the signature
  (`TestR_SIG_050_QuaderSignsTheVersionsOfLocalNpmRepositories`, its subtest named R-SIG-062).
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

Measured for package 1 (2026-11-21): the gate's unit tests about 0.8 s (`internal/signatures/gate`), the setting's 0.3 s, the repository's
member 0.3 s, the kit's gate cases milliseconds per format; the integration area `signatures` gains two tests,
`TestR_SIG_061_TheGateIsARepositorySettingAnOwnerMayNarrow` and `TestR_SIG_064_TheGateAtTheServePointOfFiles` (R-SIG-064, R-SIG-066 to
R-SIG-068 as subtests; the second web replica and a worker stopped and started again), about 11 s together with files of a kilobyte.

Measured for package 2 (2026-11-21, on the shared development machine at a load of 6 to 15): the gate's three new unit tests milliseconds (the
package `internal/signatures/gate` stays under a second), the API's two pure tests milliseconds; the integration area `signatures` gains one
test, `TestR_SIG_070_ReleasesPreviewImpactAndTheAPI` (R-SIG-070 to R-SIG-073 as subtests, one web replica and a worker, two OpenPGP keys of
the test), 5.6 s with files of a kilobyte, inside the planned 8 s; the area `backup` gains a row and an entry within an existing test,
nothing measurable. The repair after the measurement with real clients (Q-SIG-211 to Q-SIG-214) adds one unit test of the gate and one per
format that names its artifacts (microseconds each), and one subtest of `TestR_SIG_070_…` that pushes an image of a few bytes and reads it as
the pusher and as a reader (under a second).

Measured for package 3 (2026-11-22, on the shared development machine): the gate's web unit cases about 0.4 s
(`repository-signature-gate.spec.ts`, eleven cases) and two cases of the impact in `signature-settings.spec.ts`, milliseconds; no other tier
gains anything. The renders of R-SIG-074 add 31 states, 186 pictures, to the job `ui-layout`: about 270 s of a browser, about 90 s at its three
workers.

Measured for the trace of a writer's read (Q-SIG-211, Q-SIG-240; 2026-11-22, in the unit job's mode with the race detector and atomic
coverage): `TestR_SIG_064_AWritersReadIsAuditedAndCounted` reports 0.00 s for its three subtests, fakes and no payload, and the registry's row in
`internal/audit` nothing measurable; no other tier gains anything.

What only a real client shows and is not tested: `docker pull` of a multi-platform image signed at its index through a gated repository, a
refused image's `denied:` line in Docker and Kubernetes, and `cosign verify` of a refused image, whose signatures stay fetchable.
**Measured once on 2026-11-21**, after package 1 landed, with Docker 29.9.3, a kind node v1.37.0, npm 11.20.0, the .NET SDK 10.0.403,
ansible-core 2.21.6, cosign 3.1.4, Notation 1.3.2 and curl 8.14.1 (the commands, the outputs and the exit codes are in
[developer/signatures.md](../developer/signatures.md#measured-with-real-clients)):

- **Verified:** Docker pulls a multi-platform image signed at its index only (cosign without `--recursive`): the platform manifest by digest
  passes on the index's signature. A refused image's signatures stay fetchable: `cosign verify` by digest passes, by tag it is refused,
  because the tag is the artifact. Docker prints `Error response from daemon: error from registry: <text>`, without `denied:`; the kubelet's
  event carries `unexpected status from HEAD request to …: 403 Forbidden` and then `denied: <text>`, and the pod stays in
  `ImagePullBackOff`. npm prints the text after `E403`, and after `E503` once it has retried twice (146 s); curl gets the problem documents
  with `reason`, `Cache-Control: no-store` and `Retry-After: 30`. A remote's first pull waits for the fetch and is served. Debian's
  repository refuses the member with `422 not_applicable`.
- **Falsified:** `dotnet restore` and `ansible-galaxy` print the status and never the text (NuGet also with `-v detailed`, and after six
  503s it blames the feed); Docker's pending pull asks by `HEAD`, whose 503 has no body, and waits 50 s; NuGet retries at once, not after
  `Retry-After`. No client can sign an image in a gated repository: cosign and Notation read the manifest before they sign, and the gate
  refuses that read, so R-SIG-064's "OCI's signatures arrive after the push" does not hold while the gate is on. A NuGet package with an
  author's signature cannot pass (R-SIG-042's NuGet finding), nor an Ansible version whose signature was posted right after the publish. The
  text of `missing` says "it has no signature" for an npm package that carries npm's registry signature.

**Measured again on 2026-11-23** against the repairs of 2026-11-21 and Q-SIG-241, with the .NET SDK 10.0.403, ansible-core 2.21.6, Docker
29.9.3, Podman 5.8.9, cosign 3.1.4, Notation 1.3.2 and npm 11.20.0 ([developer/signatures.md](../developer/signatures.md#defects-found)):
every falsified line above holds no more but the pending image's, which no OCI client shows
([H-1540](#h-1540--docker-podman-and-the-kubelet-show-no-text-while-a-signature-is-being-checked)). Q-SIG-241 adds one unit test of NuGet
(microseconds), two of Ansible Galaxy (milliseconds, the kit's documents and a collection of a few hundred bytes), one of the virtual engine
(milliseconds) and one subtest in `ansiblelocal` (under a second); the unit run gains under 0.1 s, the shard with `ansiblelocal` under a
second, no other leg anything, and with 25 formats nothing: a format whose client needs a document of its own adds one unit test.

Acceptance criteria: the coverage generator lists every requirement of stage 3 with a test of the tier this table names.

## Decisions

The owner decided Q-SIG-1 to Q-SIG-16 on 2026-11-21 in his working paper, from the options the development agent put to him; the blocks
keep those options as they were put, translated from the paper's German. Q-SIG-20 onward are the development agent's: each question below
was decided by the development agent from the listed options, and the owner reviews the choices at the walkthrough. Q-SIG-75 to Q-SIG-95
are stage 3's; Q-SIG-131 to Q-SIG-144 were decided when its first package (the gate) was built, Q-SIG-223 to Q-SIG-232 when its third
(the screens) was. Q-SIG-233 to Q-SIG-239 were decided when the second half of stage 2's screens was built, Q-SIG-240 when the owner's
trace of Q-SIG-211 was. The owner confirmed Q-SIG-76, Q-SIG-79 and Q-SIG-80 on 2026-11-22, and decided Q-SIG-211 that day: (a), with a
trace of every such read.

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

Its entry in the decision log: [ADR-227](../decisions.md#adr-227--trust-anchors-of-four-kinds-in-three-scopes-and-the-administrator-decides-whether-project-owners-add-their-own).

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

Its entry in the decision log: [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Quader fetches in the background what the format defines, once an artifact is stored through a remote (chosen) | | The badge is right even if no client asked; mirror passes take signatures | For OCI up to three small requests more per image; how Docker Hub counts them was not checked then |
| (b) Pass through only | | No request more | A signed image reads unsigned until someone runs `cosign verify` |
| (c) In mirror passes only | | Planned load | The commonest path, a first pull through a remote, stays without |

**Applied in the spec:** R-SIG-025, R-SIG-026; Q-SIG-33 sets what and how much.

### Q-SIG-7 · Time, expiry and revocation

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

Its entry in the decision log: [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Valid if valid when made; a timestamp or a log entry proves the time, else Quader's receipt; no CRL or OCSP; the administrator can block a signer (chosen) | | Old signatures do not tip over because a certificate expired since; works without internet; a block acts at once | Quader can show "verified" where a client without a timestamp refuses after expiry; the details name the expiry |
| (b) Strict as the clients | | Quader and client agree | Holdings tip over with time |
| (c) (a) plus revocation lists | | Revoked certificates found | Outbound requests per check; "unknown" without internet |

**Applied in the spec:** R-SIG-007, R-SIG-020; [H-1168](../security/artifact-signatures.md#h-1168--a-revoked-certificate-stays-verified-until-an-administrator-blocks-its-signer).

### Q-SIG-8 · Generic files

Status: **Decided by the owner on 2026-11-21** — option (a), as recommended.

Its entry in the decision log: [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact).

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

Its entry in the decision log: [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact).

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

**Choice:** the development agent's eight readings r1 to r8 as listed: three steps at the signed content, per format what its ecosystem knows, four visible states, one switch for project anchors, an obligation of the format contract, a signature in no list, nothing that breaks an upstream signature, and the signing certificate through Helm or an upload.

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

Its entry in the decision log: [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact).

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

Its entry in the decision log: [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact).

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

Its entry in the decision log: [ADR-227](../decisions.md#adr-227--trust-anchors-of-four-kinds-in-three-scopes-and-the-administrator-decides-whether-project-owners-add-their-own).

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

Its entry in the decision log: [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact).

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

Its entry in the decision log: [ADR-227](../decisions.md#adr-227--trust-anchors-of-four-kinds-in-three-scopes-and-the-administrator-decides-whether-project-owners-add-their-own).

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

Its entry in the decision log: [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact).

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

Its entry in the decision log: [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact).

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

**Choice:** (b). **Applied in the spec:** R-SIG-044, R-SIG-054, R-SIG-056. The planner's finding was confirmed with the real cosign 3.1.4
on 2026-11-21 (package 3, R-SIG-054): a bundle referrer beside an older `.sig` tag makes `cosign verify --key` fail, and only the deprecated,
hidden flag `--new-bundle-format=false` restores the older reading, which a verifier cannot be expected to know.

### Q-SIG-67 · The format names what Quader attests, and the contract rises to version 10

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `AttestationSubjects` of the adapter and `Attests` of the declaration; check 18 holds them; `ContractVersion` 10 (chosen; written as 9 before the gate of stage 3 took 9, Q-SIG-132) | | A future format attests from its first day (the owner's r5); signature carriers stay out | Every format changes again |
| (b) A core rule: every file item of a version | | No contract change | OCI's layers, npm's provenance bundle and every signature carrier would be attested; generic files have no version |
| (c) Lead items only | | Simple | NuGet's `.snupkg` and an OCI image's tag-less manifests pulled by digest fall out; generic has no lead |

**Choice:** (a). **Applied in the spec:** R-SIG-044. The method's shape as built: [Q-SIG-180](#q-sig-180--the-format-is-asked-what-it-attests-when-the-job-signs-for-stored-items-with-the-host).

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

Status: Decided by the development agent; **confirmed by the owner on 2026-11-22**.

Its entry in the decision log: [ADR-231](../decisions.md#adr-231--a-repository-can-serve-only-artifacts-with-a-verified-signature-a-gate-at-download-for-every-format-judged-from-stored-verdicts-and-failing-closed).

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

Its entry in the decision log: [ADR-231](../decisions.md#adr-231--a-repository-can-serve-only-artifacts-with-a-verified-signature-a-gate-at-download-for-every-format-judged-from-stored-verdicts-and-failing-closed).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The roles `publisher` and `provenance`, the badge's; never a registry's signature alone, never an attestation, never Quader's own (chosen) | | The gate passes what the icon shows; npm's and nuget.org's registry signatures are on every package of those registries and would pass everything a remote of them stores | A NuGet package that only nuget.org signed is refused |
| (b) Also a registry's signature | | More passes | A gate on a remote of npmjs would only say "came from npmjs", which the remote's own TLS fetch says already |
| (c) Per repository, which roles count | | Flexible | A detail of a detail; nobody but an expert could set it |

**Choice:** (a). Quader's own npm signature and receipt attestation never pass, also when the company's CA that certified Quader's identity is an
anchor: Quader signs everything it receives (R-SIG-044), so its statement would pass everything; the gate leaves out every signature with
`identity_id` set. **Applied in the spec:** R-SIG-062.

### Q-SIG-79 · The gate acts at download for every format, not at upload

Status: Decided by the development agent; **confirmed by the owner on 2026-11-22**.

Its entry in the decision log: [ADR-231](../decisions.md#adr-231--a-repository-can-serve-only-artifacts-with-a-verified-signature-a-gate-at-download-for-every-format-judged-from-stored-verdicts-and-failing-closed).

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

Status: Decided by the development agent; **confirmed by the owner on 2026-11-22**.

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

Its entry in the decision log: [ADR-231](../decisions.md#adr-231--a-repository-can-serve-only-artifacts-with-a-verified-signature-a-gate-at-download-for-every-format-judged-from-stored-verdicts-and-failing-closed).

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

### Q-SIG-124 · A version's details are a tab Signatures, and the badge opens it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context:
R-SIG-035 names "a section Signatures of the version page"; the version page is tabs (R-UI-111), and its Details tab holds the format's
metadata and the repositories.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A tab **Signatures** after **Files**, read when it is opened; the header's badge is a link to it; the Files tab gains a column **Signature** only while a file carries a badge (chosen) | | The page's own pattern (the grade opens Vulnerabilities); one request only for whom looks; no column of dashes for a version without signatures | One tab more |
| (b) A section inside **Details** | | No new tab | Signatures and their attestations under the format's metadata, read with every Details open |
| (c) A fold under the header | | Seen at once | The header grows by kilobytes of facts for every version |

**Choice:** (a). The tab is shown while the version's answer carries the member `signatures` (the signatures are on); a version held by
several repositories lists each under its name and class. **Applied in the spec:** R-SIG-035.

### Q-SIG-125 · The shields of the badge, and each signature's own verdict

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context:
R-SIG-035 says "a shield mark"; the icon font is Material Icons Outlined, whose `verified_user` is the malware scan's "No malware found".

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `gpp_good` (a shield with a check) for both verified states, `shield` for "Signed · signer not trusted", `gpp_bad` (a shield with a cross) for "Invalid signature"; each signature of the details carries the badge of its own verdict (chosen) | | One family of shields, none shared with the malware scan; the states differ by words and shape, not only by colour | Public and internal share the icon and differ by their words |
| (b) `verified_user` for verified | | The familiar check | The same icon as "No malware found" beside it on a file |

**Choice:** (a). **Applied in the spec:** R-SIG-035.

### Q-SIG-126 · A file's page carries the badge as a fact and the details in a closed section

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The fact **Signature** (the badge, or the word of a state without one) beside **Malware scan**; a closed section **Signatures** with the details and the origin, read when it opens; a generic signature file says **Signature of** the file it belongs to (chosen) | | One click away (R-SIG-035), no request for whom does not look | — |
| (b) The details always open | | No click | A file's page of a few facts grows by every signature |

**Choice:** (a). The repository's browse list shows no badge: its rows (`Child`) carry no member `signatures`, and the specification
names the version's Files tab. **Applied in the spec:** R-SIG-035, R-SIG-032.

### Q-SIG-127 · The format descriptor names the kinds of signature its format knows

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: a
version without a signature says "Signatures this format knows: Notation, cosign." (R-SIG-009); the API's answers of a version carry no
such list.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `GET /api/v1/formats` carries `signatureKinds` (`name`, `role`) from each format's declaration (R-SIG-022); the screen names the kinds of the roles that count for the badge (chosen) | | A future format's kinds appear without a change of the UI (the owner's "automatisch mit berücksichtigt") | The names are the formats' words in English |
| (b) A list per format in the UI | | No API change | A new format needs a UI change |

**Choice:** (a). **Applied in the spec:** R-SIG-034; [http-api.md](../developer/http-api.md).

### Q-SIG-128 · The fetch of a well-known registry's signatures is an administrator's setting

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Context: R-SIG-026
advises `signatures.fetch: false` for Docker Hub without an account, and Docker Hub is a well-known repository of the project `upstream`,
whose members are the product's except the narrowing ones (R-REPO-069), so the setting answered `managed_by_product` there.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `signatures` is a member an administrator sets on a well-known repository, and the product's on a system repository; the section **Signatures** stands on every remote repository's settings but a system one (chosen) | | The one place R-SIG-026 names is the one that can use it | A member that does not narrow access among the narrowing ones |
| (b) Leave it the product's | | R-REPO-069 unchanged | Docker Hub without an account cannot be switched off |

**Choice:** (a). **Applied in the spec:** R-SIG-040; [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-069.

### Q-SIG-129 · The admin page's controls: a table of anchors, a dialog per form, the switch saved on its own

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One table of the installation's anchors with a switch per row where one may switch (shipped, an administrator's) and **Remove** for an administrator's; **Add trust anchor** and **Block signer** open dialogs; the switch for project Owners is a check box saved with the settings' `ETag`, and switching it off asks first while projects hold anchors; the lists take the page's width, the text the settings measure (chosen) | | The list and the forms of the other admin pages (malware, upstream registries); a narrowing of trust is never one click without a question | — |
| (b) A page per anchor | | Room for long material | A second screen for what the list holds |

**Choice:** (a). An anchor's material is not edited in place in stage 1's screens: an administrator removes it and adds the new one
(the API's `PATCH` of the material stays); a public key anchor takes one key in the form, more through the API. **Applied in the spec:**
R-SIG-036.

### Q-SIG-130 · The security dashboard's view of signatures

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A list of the malware view's form: the artifact (package and version, or the file), the kind with the kind of signature, the signer as claimed, the reason (or the block's), the first repository and how many more, when it was found; a search by signer and a drawer of kind and repository; no action (chosen) | | The dashboard's other views; stage 1 refuses nothing (R-SIG-038) | — |
| (b) A row that opens to every file that holds it | | Every holder at once | The answer carries the first holders only (`itemsTruncated`) |

**Choice:** (a). **Applied in the spec:** R-SIG-038.

### Q-SIG-131 · The gate reads its rows in a statement of its own where a gate applies

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Taken when package 1
of stage 3 was built.

Context: R-SIG-064 asked for the counting signatures and their trust rows to be joined into the item lookup's statement, as the malware
verdict is. The lookup is one sqlc statement every download of every repository runs; the signatures of an item depend on the view (a
virtual repository reads its members' attachments) and the scope (the project's trust row before the installation's), which the lookup does
not know.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A statement of its own after the lookup, only where a gate applies (chosen) | `Gate.Applies` reads the snapshot; `SignatureGateRows` (and the fetch state for a remote of the view) only then | An ungated download is unchanged, as R-SIG-064 asks; the lookup stays one statement for every repository; the view and the scope are the gate's | One statement more per gated download, two for a remote in the view |
| (b) Joined into the item lookup | | One round trip | A lateral join of a view and a scope in the statement every download runs, for the few repositories with a gate |

**Choice:** (a). The development agent would revisit (b) when a measurement shows the extra statement in a gated repository's latency.
**Applied in the spec:** R-SIG-064, R-SIG-076.

### Q-SIG-132 · The contract rises to version 9, and SignatureVerdicts gains Refuse

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-SIG-063 said the contract rises to 10 after stage 2's 9; stage 2 is not built, and its package that adds the attestation's
method may land after this one.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The method raises the version by one, to 9 now; whichever stage lands second takes the next (chosen) | | The version counts changes of the contract, not plans | The number named in stage 2's text moves |
| (b) Jump to 10 and leave 9 to stage 2 | | The numbers of the text hold | A version that was never released |

**Choice:** (a). The remote OCI tag, a stored document no item store serves, is judged through `formats.SignatureVerdicts.Refuse`, the
service of the Host that already answered what a format asks of signatures; a host without the gate answers nil. **Applied in the spec:**
R-SIG-063, R-SIG-064; [architecture.md](architecture.md) R-ARCH-008.

### Q-SIG-133 · Debian refuses only a gate setting that would do something

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `onlyVerified: true` and a non-empty `anchors` answer `422 not_applicable`; `false` and `[]` pass (chosen) | | A screen or a configuration as code may send the defaults for every format | A member that is present and does nothing is stored |
| (b) Any presence of the members answers 422, as `fetch` on a local repository does | | Strict | A generic writer must know which formats to leave the member out for |

**Choice:** (a). **Applied in the spec:** R-SIG-061.

### Q-SIG-134 · Anchors stored while the gate is off are no constraint

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-SIG-061 says `anchors` while `onlyVerified` is false is stored and does nothing; its table of changes does not say what a change of
the list alone is while the gate is off, nor what a replay takes from such a list.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) While the gate is off a change of the list is neither a narrowing nor a weakening, and a restored list of a gate that was off counts as any anchor in the replay (chosen) | The order follows what the gate does | An Owner may prepare a list; a replay never makes a gate narrower than either side meant | A list prepared while off passes unchecked as a weakening |
| (b) The list is ordered whatever the switch says | | One rule | An Owner could not add a reference to a gate that is off, which serves everything anyway |

**Choice:** (a). Switching on is always a narrowing, whatever the list. **Applied in the spec:** R-SIG-061.

### Q-SIG-135 · Any change of an anchor's material narrows it for the gate

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A switch off and any change of the material set `narrowed_epoch`, also for a shipped or declared anchor whose material `quader migrate` replaced (chosen) | | No cryptography on the download path; a replaced key or a changed restriction never serves on an old row (fail closed); matches the impact's upper bound of R-SIG-071 | An edit that only adds a key refuses until the trust job ran |
| (b) Only the narrowings the journal knows (a fingerprint removed, switched off) | | Fewer refusals | A changed subject restriction or Sigstore pattern would serve on an old row |

**Choice:** (a). A refresh of the public Sigstore root by the task is no change of an anchor's material and narrows nothing. **Applied in the
spec:** R-SIG-066.

### Q-SIG-136 · One decision, one wait lives in the gate's package

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `gate.Serve` in `internal/signatures/gate`, which both serve points build; its unit test there (chosen) | | `/files/` (a domain package) and the format host (edge) share one combination; the test is where the code is | R-SIG-069 named `internal/formathost` for the test |
| (b) The combination in the format host and a copy in `/files/` | | The test where R-SIG-069 named it | Two places that must agree on the order of refusals |

**Choice:** (a). **Applied in the spec:** R-SIG-069.

### Q-SIG-137 · A pending answer is recorded and counted as the malware gate's

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The 503 after the wait is recorded in `signature.download_refused` with the reason `pending` and counted in `quader_signatures_downloads_refused_total{reason="pending"}`; no row of refused downloads (chosen) | The malware gate's `malware.download_blocked` and its counter do the same | An auditor sees the clients that were turned away; one pattern for both gates | The event's name says refused for an answer that asks to retry |
| (b) Only the histogram's outcome `pending` | | Fewer events | A 503 leaves no trace in the audit |

**Choice:** (a). **Applied in the spec:** R-SIG-075.

### Q-SIG-138 · A well-known repository takes the member from an administrator, a system one keeps it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. One rule with
[Q-SIG-128](#q-sig-128--the-fetch-of-a-well-known-registrys-signatures-is-an-administrators-setting), which made `signatures` an
administrator's member on a well-known repository for its fetch switch; this block extends the same rule to the gate's two fields.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `signatures` is one member an administrator writes on a well-known repository of `upstream` (`fetch`, `onlyVerified`, `anchors`) and the product holds on a system one (chosen) | As `malware` and `vulnerabilities` | R-SIG-061's rule and Q-SIG-128's together; the product's own content is never gated | A member that mixes a fetch switch and a narrowing |
| (b) Only `onlyVerified` and `anchors` an administrator's, `fetch` the product's | | Fewer writers of the fetch | Against Q-SIG-128; a member split between two writers |

**Choice:** (a). **Applied in the spec:** R-SIG-040, R-SIG-061; [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-069.

### Q-SIG-139 · The trust step signals every waiting download

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The verify job and the fetch signal the subject's digest after their commit; a batch of the trust step signals `*`, which wakes every waiter (chosen) | | A batch of 10,000 rows sends one signal; a waiter reads again once anyway | Every waiter of a replica reads once more per batch |
| (b) The trust step signals each subject | | Exact | Up to 10,000 signals per batch |

**Choice:** (a). **Applied in the spec:** R-SIG-067.

### Q-SIG-140 · An unstored body through a gated remote answers pending

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: during an outage of the object store the remote engine passes a body on unstored (R-STOR-036); the malware gate decides it on its
digest's row.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Where a signature gate applies the body answers `503 signature_check_pending` (chosen) | | Nothing stored, nothing judged, nothing served (R-SIG-067's fail-safe) | A signed artifact is not served during the outage |
| (b) Judge the digest's attachments as for a stored item | | Serves what was signed before | The fetch of the remote never ran for a body that was never stored |

**Choice:** (a). **Applied in the spec:** R-SIG-067.

### Q-SIG-141 · The integration area tests the gate through /files/, the rest at the unit tier

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-SIG-065 and R-SIG-067 named npm with the company's Sigstore and an OCI stub with a Notation key for their integration tests;
both need a signing service or a CA chain of the test in the stack.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The stack tests the setting, the serve point, the block on another replica and the wait through `/files/` with a release key; the virtual view, the remote's fetch state and the combination at the unit tier (chosen) | | The gate is format-neutral, so one format shows it; about 11 s, files of a kilobyte | No integration test reads a virtual repository's members or a remote's fetch state against PostgreSQL |
| (b) As R-SIG-065 and R-SIG-067 named them | | The statements of the view and of the fetch run against PostgreSQL | A Sigstore and a CA of the test in the integration area, several seconds more |

**Choice:** (a). The development agent would add (b)'s two cases when package 2's preview reads the same statements. **Applied in the spec:**
R-SIG-065, R-SIG-067, R-SIG-078.

### Q-SIG-142 · Every refusal is written to the list of refused downloads

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Each refusal upserts its row (`last_at`, `count`, the last reason), the audit event stays deduplicated (chosen) | | The count is exact, as R-SIG-073's test asks | One write per refused download |
| (b) A row at most once a minute per replica | | Fewer writes | The count is no count |

**Choice:** (a). A write that fails is logged and the refusal answered. **Applied in the spec:** R-SIG-073, R-SIG-076.

### Q-SIG-143 · What a format derives or stores as a document is GateNone

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-SIG-063's table names the artifacts of NuGet, npm and Ansible Galaxy and leaves the other items they store unnamed.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) NuGet's `.nuspec`, embedded files and symbol files, npm's stored documents (keys, attestations), Ansible's documents are `GateNone` (chosen) | | They are Quader's extraction from an artifact, or documents a client reads to verify one; the artifact they come from is gated | A file extracted from a refused package is served |
| (b) Every item not named an artifact is judged | | Nothing passes unnamed | npm's attestations a client needs to verify would be refused |

**Choice:** (a). Most of them are generated items, which the gate never asks about. **Applied in the spec:** R-SIG-063.
**Revised for NuGet's embedded files** by [formats/nuget.md](formats/nuget.md#q-nuget-136--an-embedded-file-is-content-of-its-package-the-checks-of-its-download-decide-and-its-routes-are-metadata-routes)
Q-NUGET-136: they are read out of the `.nupkg` when a client asks for them and are no items, so each read decides the checks of a
download of that `.nupkg`, the signature gate among them.

### Q-SIG-144 · The block's epoch is named anchor_epoch

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-SIG-066 first named the column `signature_blocks.epoch`; R-ARCH-009 reserves the names `epoch`, `revision`, `rev_seq` and `*_version`
for revision columns a client holds, which a restore raises.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `anchor_epoch`, the name of the counter it copies (`signature_settings.anchor_epoch`), as the trust row's since 00100 (chosen) | | No revision: a restore leaves it, and the trust rows are computed again anyway; one name for every copy of the counter | The name differs from the text that first planned it |
| (b) `epoch`, registered as a revision column | | The planned name | A restore would raise it and the gate would apply every block to rows that hold it already |

**Choice:** (a). `narrowed_epoch` is not claimed by the rule and keeps its name; it is the epoch of a write, not a copy of the counter's
current value. **Applied in the spec:** R-SIG-066.

### Q-SIG-145 · The identity's states move by a table, a retired identity never comes back, a previous one does

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Stage 2's first
package; R-SIG-045 named the states and left the moves between them open.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Seven moves (pending to announced or current; announced to current or previous; current to previous; previous to announced or retired); a pair of the files whose certificate is a retired identity's is refused; a previous identity is announced again when the files name it again, or, at a worker's start with no file named, when it is the newest upload with 24 hours left (chosen) | | A key believed taken cannot return by a values change; the way back from the Helm values to an upload needs no new upload | A worker that restarts with old values during a rollout lets the identity of the files go again |
| (b) No way back: a previous identity is final | | Fewer moves | Taking the identity out of the values leaves an installation that had an upload without signing until someone uploads again, against R-SIG-046 |
| (c) Every move allowed | | Simple | Retiring would mean nothing |

**Choice:** (a). The development agent would revisit the start-only check if rollouts with mixed values cause flapping. **Applied in the spec:**
R-SIG-045, R-SIG-046. Code: [states.go](../../internal/signing/states.go), [store/files.go](../../internal/signing/store/files.go).

### Q-SIG-146 · The API's refusals beyond the three the specification names, and the public answers without an identity

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `409 signing_no_pending_key` (a certificate without a pending key), `409 signing_identity_announced` (an upload or import while another identity waits for its switch; the files instead replace an announced identity that never signed, Helm wins), `409 signing_identity_not_previous`, `409 signing_key_in_use` (retiring a certificate whose key the current or announced identity has), `409 signing_identity_exists` (a stored certificate again); the trusted root and the chain answer `404` without an identity to trust (chosen) | | Every refusal names its cause; nothing silently replaces an identity that is about to sign; a retirement always withdraws a key | Five more codes |
| (b) One `409 conflict` for all | | Fewer codes | The screen cannot tell the administrator what to do |
| (c) An upload during an announce period replaces the announced identity | | No wait | Two identities in ten minutes leave caches with a key that never signs |

**Choice:** (a). **Applied in the spec:** R-SIG-055. Code: [store.go](../../internal/signing/store/store.go), [signing_api.go](../../internal/api/v1/signing_api.go).

### Q-SIG-147 · The key request's defaults are the server's too, and its bounds

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The API applies the form's defaults when a member is absent (common name `Quader signing`; `server.base_url` as the URI when no URI and no e-mail address is given) and refuses at the member: subject fields above 64 characters, a country that is not two capital letters, more than 8 URIs or e-mail addresses, a URI without a host or with a user (chosen) | | A script asks with `{}`; a request that the CA would refuse later is refused now | — |
| (b) The UI alone fills defaults | | | A call through the API makes a request without an alternative name, which R-SIG-045 then refuses |

**Choice:** (a). **Applied in the spec:** R-SIG-047. Code: [request.go](../../internal/signing/request.go).

### Q-SIG-148 · One row of the signing's state, and the code of the last attempt at the work

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `signing_settings`, one row: the last refused pair of the files (its content's SHA-256, the reason, the leaf's fingerprint, the time), the daily retry of failed work, `signing.failed`'s last report per kind and code, discovery's cursor per kind; `signing_work` gains `code`, the reason of its last attempt (chosen) | The pattern of `signature_settings` | "Once per content" and "once an hour per kind and code" hold across workers and restarts; the admin page shows the refusal from any replica | A table more |
| (b) The refusal and the reports in each worker's memory | | No table | Every worker records the refusal, and the API replica cannot show it |

**Choice:** (a). **Applied in the spec:** R-SIG-046, R-SIG-049, R-SIG-057. Code: migration 00101, [store/work.go](../../internal/signing/store/work.go).

### Q-SIG-149 · Retiring an uploaded identity drops its sealed key

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It amends the CHECK
of R-SIG-045's table: an upload holds a sealed key while it is not retired.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `sealed_key` becomes NULL at the retirement, in its transaction; the journal's replay does the same (chosen) | Debian's retired keys hold none either | A key believed taken is in no backup made after the retirement, and no worker can open it again | A retirement cannot be undone, which Q-SIG-145 wants anyway |
| (b) Keep it sealed | | Reversible | The key stays in every backup |

**Choice:** (a). **Applied in the spec:** R-SIG-045, R-SIG-048, R-SIG-059.

### Q-SIG-150 · The job's frame: kinds the packages register, one discovery step per run, a job that only switches without a kind

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A package that signs registers a `job.Kind` (its name, `Sign` in a transaction that holds the current identity's row, an optional `Discover`); the job takes due rows of the registered kinds only, runs one discovery step of 10,000 items per kind and run (a step that reached the last item starts the next pass at the first), and a failed row is due again a day after its last attempt; with no kind registered the job switches and reschedules and signs nothing (chosen) | | The npm and the attestation packages add a kind and nothing of the frame; rows of a kind not built yet stay untouched | Discovery passes start again as often as the task runs; a package with a costly discovery bounds it itself |
| (b) The frame knows npm and attestations by name | | | Every package changes the frame |

**Choice:** (a). **Applied in the spec:** R-SIG-049. Code: [job/kinds.go](../../internal/signing/job/kinds.go), [job/job.go](../../internal/signing/job/job.go).

### Q-SIG-151 · The identity's files are read again from every second, as the TLS pairs are

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It changes R-SIG-058's
lower bound of `signing.identity.reload_interval` from 10 s to 1 s.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) 1 s to 1 h, default 1 minute (chosen) | `server.tls.reload_interval` takes 1 s | The read is three small files compared by content; the integration test rotates in seconds | — |
| (b) 10 s to 1 h | | | No gain, and a test of a rotation waits ten seconds |

**Choice:** (a). **Applied in the spec:** R-SIG-058.

### Q-SIG-152 · The identity's migration adds the column of the issued signatures; the attestations' keys come with the attestations

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Migration 00101 adds `signatures.identity_id` with `signatures_issued` (R-SIG-054), so that a retirement counts the identity's signatures and both later packages write it; `signature_attachments.source` `issued` and the keys `signing.attestations.*` join with the package that first uses them (chosen) | | The frame is complete for the npm and the attestation packages; no key is documented that nothing reads | Two packages touch the stage 1 tables |
| (b) Everything of R-SIG-054's schema and R-SIG-058's keys now | | One migration | Keys and a CHECK value no code reads |

**Choice:** (a). **Applied in the spec:** R-SIG-054, R-SIG-058.

### Q-SIG-153 · A worker keeps the key of the files and the one before it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The holder keeps the newest key of the files and the one before it; an opened uploaded key is dropped once its identity is neither current nor announced (chosen) | R-SIG-048: "the old key stays in the memory of the workers that loaded it" | The old identity signs until the switch after cert-manager replaced the files; a retired key leaves memory at the next look | — |
| (b) Only the key in the files | | Less held | No worker signs between the renewal and the switch |

**Choice:** (a). **Applied in the spec:** R-SIG-048. Code: [job/holder.go](../../internal/signing/job/holder.go).

### Q-SIG-154 · A removal and a retirement yield no entry of the Bastion's feed

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Listed in `noFeedEntry`: what they change on the wire (npm's keys document and the documents with the identity's signatures) reaches a Bastion as those documents' new revisions (chosen) | | No second path for the same change | A Bastion holds the old keys document until its revalidation |
| (b) A feed entry per withdrawal | | Faster | Duplicates the revisions the npm package raises |

**Choice:** (a). **Applied in the spec:** R-SIG-059; [bastion.md](bastion.md). Code: [bastionmain/feed.go](../../internal/bastionmain/feed.go).

### Q-SIG-155 · The public material: a PEM chain, both answers cacheable for a minute

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `chain.pem` as `application/pem-certificate-chain` (RFC 8555), the trusted root as Sigstore's media type, both with `Cache-Control: public, max-age=60` and `nosniff`, read from the replica's snapshot (chosen) | | An admission controller may poll; a switch is announced ten minutes ahead | — |
| (b) `no-store` | | Always current | Every poll reads the database's snapshot |

**Choice:** (a). **Applied in the spec:** R-SIG-055.

### Q-SIG-156 · The npm format reads Quader's keys and signatures through its data port

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Stage 2's second
package; R-SIG-050 says what the documents and the keys endpoint serve and leaves open how the format, which runs no SQL (R-ARCH-008),
learns it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The npm data port: `LocalDocument` and `LocalVersion` read each version's served signatures in the statement that reads the document's rows, and `RegistryKeys` answers Quader's keys, which the composition root takes from the replica's snapshot of the identities (chosen) | The data port is the format's own (`rows`, `internal/formatdata/npm`); it already reaches beyond npm's tables for the background fetch | The signatures belong to the snapshot of the revision the rendition is keyed by; the format contract stays as it is while another package changes it | The data port names a table of the signing tree in one statement |
| (b) A service of the Host (`formats.SignatureVerdicts` gains the keys and the issued signatures) | | Every format could ask the same | A change of the frozen contract for one format; the signatures would be read in a second statement, outside the snapshot of the document |
| (c) The format renders without signatures and a host hook adds them | | | The rendition cache and the validator would not cover them |

**Choice:** (a). **Applied in the spec:** R-SIG-050. Code: [formatdata/npm/read.go](../../internal/formatdata/npm/read.go),
[handlers_signatures.go](../../internal/formats/npm/handlers_signatures.go), [wire_signing.go](../../internal/app/wire_signing.go) `npmSigningPort`.

### Q-SIG-157 · One operation signs the due versions of a package together

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. R-SIG-050 raises a
package's revision "once per package and job run"; the frame of Q-SIG-150 runs one row per operation.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The operation of a row locks the version's package, takes the package's other due rows that no other operation holds (`FOR UPDATE SKIP LOCKED`, at most 1,000 versions), signs every one that carries no served signature, and raises the revision once (chosen) | | Discovery after an installation's first identity raises a package of 500 versions once, not 500 times; no deadlock with an operation that waits for the package's lock, because a held row is skipped | `quader_signing_operations_total` counts operations, not versions |
| (b) One version per operation, a revision per version | | Simplest | A revision, a rendition and a merge per version of a package |
| (c) Sign every row first, raise the touched packages after the run | | One raise per package exactly | Between the signature's commit and the raise a replica renders new bytes under the old revision |

**Choice:** (a). **Applied in the spec:** R-SIG-049, R-SIG-050, R-SIG-057. Code: [npmsign.go](../../internal/signing/npmsign/npmsign.go) `Sign`.

### Q-SIG-158 · A publish writes its work only while signing is on and an identity is current

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The publish's transaction writes the row (reason `upload`, priority 0) only while `signing.enabled` is true and an identity is current, in one statement (`SigningEnqueueWorkWhileCurrent`); otherwise discovery signs the version with the first identity (chosen) | | An installation that never signs keeps no rows of work; the first identity's backlog is discovery's, as R-SIG-049 says | A publish in the moment an identity becomes current waits for discovery, at most the task's ten minutes |
| (b) Every publish writes a row | | No window | A row per publish for ever in an installation without an identity |
| (c) No row; discovery alone | | Nothing in the publish's transaction | Ten minutes without a signature after every publish, against R-SIG-050's two seconds |

**Choice:** (a). **Applied in the spec:** R-SIG-049. Code: [store/work.go](../../internal/signing/store/work.go) `EnqueueWhileCurrent`.

### Q-SIG-159 · A document serves a signature by its identity's state, read in the document's statement

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. R-SIG-045 says
which identities' signatures are on the wire; R-SIG-048 raises the revisions when that changes.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The statement that reads a document's rows joins each version's `issued` attachments to the signing identities and takes the scheme `npm-registry` of a current or previous identity while one is current (chosen) | | One snapshot: rows, revision and signatures agree; no write per signature on a change of the identities | Until the job raised the revisions (below), a replica that renders afresh serves the new set under the old revision ([H-1205](#h-1205--between-a-change-of-the-identities-and-the-jobs-next-run-two-renditions-share-a-validator)) |
| (b) A served flag on each signature, written on every change of the identities | | The document reads a column | A million writes per removal; the flag and the states can disagree |
| (c) The format filters by the keyids of its snapshot and keys the rendition by them | | No revision raise for the replica's own cache | A Bastion and a virtual repository's merge follow revisions, not keyids, and would keep the old document |

**Choice:** (a). **Applied in the spec:** R-SIG-045, R-SIG-048, R-SIG-050. Code: [npm_local.sql](../../internal/meta/postgres/queries/npm_local.sql)
`NPMLocalDocument`, `NPMLocalVersion`; `signing.NPMServed`.

### Q-SIG-160 · The job compares what the documents serve in every run and raises the revisions itself

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `signing_settings.served` keeps per kind the identities whose signatures the documents served at the job's last look; every run compares it with `signing.NPMServed` of the identities, also with signing off and with no identity current, raises the packages whose versions carry a signature of an identity that joined or left the set, 1,000 per transaction, writes work for a retired identity's versions, then stores the set; the frame calls the kind's `Served` (chosen) | | A removal, a retirement, the first current identity after none, a switch and a restore's replay are one rule; a run cut short raises again, which costs a revision | A column more; the raise waits for the job, which every identity change wakes |
| (b) The identity's writes raise the revisions in their own transaction | | No window | A removal in the administrator's request raises a million packages; the files' watcher and the journal's replay would each need it too |
| (c) A queue of changes the identity's writes append | | Exact | Duplicates the comparison of (a) with more state |

**Choice:** (a). **Applied in the spec:** R-SIG-048, R-SIG-059. Code: [npmsign.go](../../internal/signing/npmsign/npmsign.go) `Served`,
[job/job.go](../../internal/signing/job/job.go) `served`, migration 00103.

### Q-SIG-161 · npm's signatures are written through a signature store of their own, whatever signatures.enabled

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Stage 1's store is
built only while `signatures.enabled` is true (R-SIG-040).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The kind `npm` writes through a signature store of the runner alone (no bounds, no wake-up of stage 1's job); `signing.enabled` decides whether Quader signs (chosen) | | One switch per stage; an installation that does not check signatures still signs its packages | With stage 1 off, the signatures a retirement detached stay until stage 1's job runs again (rows of a few hundred bytes) |
| (b) No npm signing while `signatures.enabled` is false | | | Two switches for one feature, against the owner's frame of stage 2 |

**Choice:** (a). **Applied in the spec:** R-SIG-054, R-SIG-058. Code: [wire_signing.go](../../internal/app/wire_signing.go) `npmSigningKind`.

### Q-SIG-162 · A signature made again after a retirement replaces the retired one's attachment

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The operation that signs a version removes the version's `issued` attachments of a retired identity in its transaction (chosen) | | A version carries one served signature and the API lists what is served; the retired signature leaves the tables through stage 1's orphan removal | The API no longer shows that a retired identity once signed the version; `signing.identity.retired` keeps the count |
| (b) Keep the retired identity's attachments | | History at the version | The signatures answer lists signatures that are never served again |

**Choice:** (a). **Applied in the spec:** R-SIG-048, R-SIG-050. Code: [store/issued.go](../../internal/signatures/store/issued.go) `DetachRetiredIssued`.

### Q-SIG-163 · The client setup's tab Verify comes with the attestations

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. R-SIG-055 names one
tab for `npm.verify.signatures` and for every client's attestation check.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The tab, the kit's new section and the API's filter by the identity are built once, by the attestation package, which adds `npm.verify.signatures` beside every client's `<client>.verify.attestation` (chosen) | | One mechanism built once; the format contract's kit is not changed by two packages in parallel | Until the attestations land, the client setup does not show `npm audit signatures` |
| (b) Build the tab here for npm alone | | npm's line sooner | The section, the kit's check 8 and the filter built twice, by two builders at once |
| (c) No snippet for npm | | | npm's one command is what R-SIG-050 is for |

**Choice:** (a). **Applied in the spec:** R-SIG-050, R-SIG-055.

### Q-SIG-170 · The section's forms are dialogs; the request stands in the section

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The first half of
package 4; R-SIG-056 names the controls and leaves their form open.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) **Create signing key**, **Replace**, **Upload certificate** and **Import key and certificate** open dialogs; a pending key's request stands in the section as a file window with **Copy** and **Download** (the client setup's `<quader-code-window>`), with **Upload certificate** and **Discard** below it; **Remove**, **Discard** and **Retire** ask first (chosen) | The page's **Add trust anchor** and **Block signer** (Q-SIG-129) | One way for every form of the page; the request survives a reload because it is the server's, not the dialog's; days may pass between the request and the CA's answer | — |
| (b) The forms inline in the section | | No dialog | The section grows by a form an administrator uses once a year, against simple by default |
| (c) A wizard of three steps | | Guides the first time | The steps lie days apart; a wizard does not survive the wait |

**Choice:** (a). **Applied in the spec:** R-SIG-056. Code: [signing-identity.ts](../../web/src/app/features/settings/signing-identity.ts),
[signing-dialogs.ts](../../web/src/app/features/settings/signing-dialogs.ts).

### Q-SIG-171 · The key request takes one URI and one e-mail address

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One field **URI**, holding the base URL, and one **E-mail address** behind **More options**; more through the API, which takes 8 of each (chosen) | | The first URI is the identity a verifier names (R-SIG-045), and a company names one; the form stays a form of single fields | A second URI needs the API |
| (b) A list editor for each | | Everything the API takes | A list control for what nearly nobody fills twice |

**Choice:** (a). The country is a field of two letters, upper-cased before it is sent. **Applied in the spec:** R-SIG-056.

### Q-SIG-172 · Import key and certificate stays behind More options beside an uploaded identity too

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. R-SIG-056 places it
in the state without an identity and says nothing of the others.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Behind the section's **More options** while the Helm values name no identity and no key is pending or announced, with or without a current upload (chosen) | | A CA that hands out key and certificate together renews that way too; the API takes it and announces the new identity | — |
| (b) Only without an identity | | Shorter section | A renewal by import needs the API |

**Choice:** (a). **Applied in the spec:** R-SIG-056.

### Q-SIG-173 · A refusal of R-SIG-045 reads as its check in plain words, then the server's detail

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The check the refusal names (`(<check>)` at the end of the field error and of a refused pair's reason) as a sentence of the UI's language that says what to ask the CA for, followed by the server's detail with the check's name, which carries what only the server knows (a certificate's dates, a key's curve) (chosen) | | Plain words in both languages without losing the dates; the check is named, as R-SIG-055 wants | The detail stays English in the German UI |
| (b) The server's detail alone | | Short | English only, and no hint what to do |
| (c) The sentence alone | | Short | Drops the dates and the curve |

**Choice:** (a). The refusals beyond the checks (Q-SIG-146's codes, `signing_key_mismatch`, `signing_identity_managed`) are sentences of
the UI that say what to do instead. **Applied in the spec:** R-SIG-056. Code: [signing-words.ts](../../web/src/app/shared/signatures/signing-words.ts),
[problem-messages-signatures.ts](../../web/src/app/core/http/problem-messages-signatures.ts).

### Q-SIG-174 · A write that needs a recent sign-in keeps the key request and the certificate as a draft, never the imported key

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) As the other admin screens do (R-UI-042): the app's dialog asks for the sign-in on `401 reauthentication_required`; the key request's fields and the certificate with its chain, public material, wait as a draft of the route and their dialog opens again with them after the sign-in; the import keeps nothing and clears the key and the passphrase after any answer (chosen) | The Bastion's token dialog, the Debian key import | Nothing public is typed twice; nothing secret is stored in the browser (R-UI-043) | The import is pasted again after a sign-in |
| (b) Keep nothing | | Simplest | A chain of three certificates pasted again |

**Choice:** (a). **Applied in the spec:** R-SIG-056.

### Q-SIG-175 · The section's states are rendered from answers changed in the browser

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The profile's real answer without an identity, and each other state as that answer changed in the browser by the walk (`renders/signing.ts`), as the scanner replicas' databases are (chosen) | | No company CA and no sealed key in the profile; nothing of the seed's SQL has to follow the schema | The renders do not show the store's own answer for an upload |
| (b) Rows of `signing_identities` written by SQL | | The real answer | An upload's row needs a key sealed by the profile's key ring, and the worker would try to open a fake one |
| (c) The real flow through the API with a CA of the seed | | Everything real | Two rotations wait for the announce period, and the seed needs a CA tool on every runner |

**Choice:** (a). **Applied in the spec:** R-SIG-056; [developer/web-ui.md](../developer/web-ui.md#the-signatures-screens).

### Q-SIG-176 · What the section shows beyond the first bullet

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A note when `signing.enabled` is false; an announced identity below the current one with its facts and the time of its switch; **Previous identities** only while there is one, its rows with the common name and the fingerprint's first groups, the source, the end of validity ("expired" beside it), the span it signed and **Retire**; a stored upload under the Helm values marked "Not in use: the Helm values name the signing identity" (R-SIG-046); **Replace** hidden while a key is pending or announced, **Download trusted root** in **Previous identities** while none is current (chosen) | | Every state the API answers has its words; no control that the API would refuse at once | A longer section in the rare states |
| (b) Only what R-SIG-056 names | | Shorter | An announced identity and the switch off would be invisible |

**Choice:** (a). **Applied in the spec:** R-SIG-056.

### Q-SIG-180 · The format is asked what it attests when the job signs, for stored items, with the host

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Package 3 of stage 2;
R-SIG-044 wrote the method as a pure function of a commit's event.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `AttestationSubjects(ctx, h, repo, items)`, as `Gate`: one subject per stored item, the format may read its data port; the job asks it when it signs and when discovery runs (chosen) | | An image's top-level manifest is known only from the rows of the commit's own transaction and from indexes pushed after it; discovery asks the same method in batches; the kit seeds an item and asks, as for `Gate` | A commit writes work for files that turn out to be no subject (Q-SIG-186) |
| (b) `AttestationSubjects(ev SignatureEvent)`, pure, at the commit | The specification's sketch | No work for a file that is no subject | OCI cannot tell a platform manifest from a top-level one at its commit (the index comes later); discovery has no event |

**Choice:** (a). **Applied in the spec:** R-SIG-044. Code: [signatures.go](../../internal/formats/signatures.go), [formathost/attestations.go](../../internal/formathost/attestations.go).

### Q-SIG-181 · An image's manifest is judged when the job signs, and the statement names no tag

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A manifest by digest that is no attachment and that no stored index of the name lists, judged from `oci_manifests` when the job signs; an attestation of a manifest an index now lists is removed; the statement names the image, the digest and the media type, no tag (chosen) | | An index pushed after its children (the order every client pushes in) takes them out before the first verdicts arrive; a tag moves, the digest is the receipt | A child attested before its index arrived (a repository without the checks, signed at once) loses its attestation when the index comes |
| (b) The tag that brought it in the statement | R-SIG-051's sketch | Names how it arrived | Read in a transaction of the commit's only; a later tag on the same digest makes the statement read wrong |

**Choice:** (a). **Applied in the spec:** R-SIG-044, R-SIG-051. Code: [oci/signatures_attest.go](../../internal/formats/oci/signatures_attest.go).

### Q-SIG-182 · The bundle is built from Sigstore's protobuf types, not with sigstore-go's sign package

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. R-SIG-052 named
sigstore-go's `sign.Bundle`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The DSSE envelope and the bundle written from `protobuf-specs`' types, read back through sigstore-go's `pkg/bundle` before they are stored (chosen) | | No module more: `pkg/sign` imports a Fulcio client with an OAuth flow (go-oidc, go-jose, golang.org/x/oauth2, a browser opener), which `go mod tidy` adds for one signature with a key the worker holds | Twenty lines that sign the pre-authentication encoding themselves |
| (b) `sign.Bundle` over `sign.DSSEData` | The specification's text | The library's own path | Four modules more in the binary of every role, none of them used |

**Choice:** (a). **Applied in the spec:** R-SIG-052. Code: [signing/bundle](../../internal/signing/bundle/bundle.go).

### Q-SIG-183 · The referrer stands in a local repository only; a remote's and a virtual's images are attested through the API

Status: **Decided by the development agent** — selected from the options below, within option (b) of Q-SIG-66; the owner reviews it at the
walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A referrer in a local repository whose image has no `.sig` or `.att` tag; a remote's and a virtual's images through the API with the reason `no_referrers_api` (chosen) | | The item store takes the system's write into a local repository only; a remote's referrers answer is the upstream's document, which Quader would have to rewrite; nothing of a publisher's can be hidden; a virtual repository lists the referrers of its local members, where the rule held | A cluster engine finds no attestation of a remote's image ([H-1215](../security/artifact-signatures.md#h-1215--a-remotes-and-a-virtuals-images-carry-quaders-attestation-only-through-the-api)) |
| (b) R-SIG-054's remote: Quader's referrer stored in the cache and appended to the upstream's answer, removed when the background fetch finds an older tag | | Remote images found among their referrers | The cache's referrers are the upstream's documents; a write beside them and a rewrite of the upstream's answer per request; the virtual union's filter per member |

**Choice:** (a). The development agent would revisit (b) when a cluster engine of the owner verifies Quader's receipt of remote images.
**Applied in the spec:** R-SIG-054. Code: [oci/signatures_referrer.go](../../internal/formats/oci/signatures_referrer.go).

### Q-SIG-184 · The bundle is kept in PostgreSQL for every format; an image's referrer is a copy on the wire

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. R-SIG-054 named the
referrer's layer blob as an image's object.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `signature_objects.raw` for every attestation; the referrer (the empty config, the bundle as its layer, the manifest) committed through the item store as `system:signing`, whose commit no hook detects, attests or records an origin for, and which OCI's `OnCommit` admits by a payload of its own (chosen) | | One store and one route for every format; the referrer can be made again from the stored bundle with the same digest; a referrer removed (an older tag arrived, a user deleted it) loses no attestation | The bundle is stored twice for a local image (about 2.5 KiB) |
| (b) The referrer's layer as the object (`item_blob`) | The specification's text | One copy | The attestation's rows depend on items a user may delete; detection of the referrer would record a second signature of no identity |

**Choice:** (a). **Applied in the spec:** R-SIG-054. Code: [formathost/attestations.go](../../internal/formathost/attestations.go), [oci/hooks.go](../../internal/formats/oci/hooks.go).

### Q-SIG-185 · The job keeps an index of what it issued: signing_attestations

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One row per attested item: the signature, the identity, the malware state and release it names, the blobs it names (GIN), where it is served and why (chosen) | | A changed verdict of a layer finds the image's attestation by one statement; discovery finds what lacks the current identity's attestation and what lost its referrer; the route reads one row | A table more, whose rows go with their item |
| (b) Read it all from stage 1's tables | | No table | The blobs an image's statement names and its placement have no column there |

**Choice:** (a). **Applied in the spec:** R-SIG-054, R-SIG-059. Code: migration 00105.

### Q-SIG-186 · Every committed file of a format that attests is work; the format decides when the job signs

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The commit hook writes a row for every file a client or a fill stores in a format whose declaration attests something (not a generated file, not a system repository, not the job's own referrer) (chosen) | | One rule for every format; no format is asked in the commit's transaction | An image's layers and configs become rows the job ends without a signature: one short transaction each, once |
| (b) Ask the format at the commit | | Fewer rows | Q-SIG-180's reasons |

**Choice:** (a). **Applied in the spec:** R-SIG-053. Code: [formathost/hooks.go](../../internal/formathost/hooks.go), [signing/attest/work.go](../../internal/signing/attest/work.go).

### Q-SIG-187 · A new attestation waits without spending an attempt and looks again after a quarter of its item's age

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The operation answers `signing.Later`: the row is due again after a quarter of the item's age (5 s to 5 min, never after the bound of `wait_for_checks`), spends no attempt; a changed malware verdict makes it due at once; the job wakes itself for the earliest waiting row (chosen) | | A fresh upload's first verdicts are met within seconds; an hour of waiting costs about 20 looks; no wiring of the vulnerability and the signature jobs into the signing | The vulnerability evaluation and stage 1's checks are met by the poll, not by a signal |
| (b) Every check that answers wakes the job | | No poll | Three jobs wired to the signing's tables |

**Choice:** (a). **Applied in the spec:** R-SIG-053. Code: [signing.go](../../internal/signing/signing.go) `Later`, [job/job.go](../../internal/signing/job/job.go).

### Q-SIG-188 · The tab verify is one snippet per client, written by the core

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `<client>.verify.attestation`: the trusted root's download and `cosign verify-attestation` against the image by digest, or for every other format the bundle's download from the API (with the token's reference by the credential mode) and `cosign verify-blob-attestation`; answered while an identity is current and signing is on, never for a Bastion, whose URL space has no API (chosen) | | The same for every client of a format, no template per format, the identity known to the core alone | The command names a placeholder digest or path, which the reader replaces |
| (b) A template per format | | The format's words | Twenty formats write the same command |

**Choice:** (a). **Applied in the spec:** R-SIG-055, R-SIG-056. Code: [setup_verify.go](../../internal/api/v1/setup_verify.go).

### Q-SIG-189 · A fill's upstream is the URL the item was fetched from

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `items.upstream_url`, without user info (chosen) | | A fact of the receipt, stored with the item; a mirror's or a background download's fill names its URL too | Longer than the remote's base URL |
| (b) The remote's configured upstream | R-SIG-051's example | Short | The configuration may change; it is no fact of the receipt |

**Choice:** (a). **Applied in the spec:** R-SIG-051.

### Q-SIG-190 · The vulnerability snapshot is the report's evaluation, read over every repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The report of the scanning (`report.Service.Version`) with a read scope over every repository: the evaluation, the grade, the open findings per severity and the date of the data of the version in its repository; an image by its manifest's digest; a format or a file without a version `not_covered` (chosen) | | The same evaluation the screens show; `pending_scan` and `pending_data` are what a new attestation waits for | A worker builds a report of its own at its first attestation |
| (b) A statement of the signing's own | | No report on the worker | A second derivation of R-VULN-001 |

**Choice:** (a). **Applied in the spec:** R-SIG-051, R-SIG-053. Code: [app/wire_attestations.go](../../internal/app/wire_attestations.go).

### Q-SIG-191 · A retired identity's attestation is not served by the route; its referrer stays until the job made it again

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The route serves an attestation while its identity is current or previous; the retirement writes the identity's attestations as work at priority 2, which replaces each, and its referrer with it (chosen) | | No item store in the retirement's transaction; the window is the job's backlog | An image's referrer of the retired identity stays fetchable until then ([H-1216](../security/artifact-signatures.md#h-1216--a-retired-identitys-referrer-stays-among-an-images-referrers-until-the-job-made-it-again)) |
| (b) The retirement removes every referrer | | No window | Thousands of item removals in one administrator's request |

**Choice:** (a). **Applied in the spec:** R-SIG-048, R-SIG-054. Code: [store.go](../../internal/signing/store/store.go) `Retire`.

### Q-SIG-192 · cosign reaches the set `oci` alone, as its binary copied onto the control-plane node

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Package 3 of stage 2
first put cosign into the Docker client image, which the sets `oci`, `oci-closing` and `oci-long` all load into kind; only `oci` runs
cosign (R-SIG-052, and the pusher's signature in a gated repository of R-SIG-064).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A third image of the OCI clients, the official binary alone on `scratch` (`quader-e2e-ociclients-cosign:dev`); for the set `oci` deploy/dev/e2e.sh copies the binary onto the control-plane node, the client pods mount the directory (`hostPath`) and the entrypoint links it as `cosign` (chosen) | | `oci-closing` and `oci-long` load 136 MB less into kind; the binary comes from the clients' cache tar, not from a registry outside; no feature of the cluster is needed | A `hostPath` in the clients' namespace, which is privileged already; `e2e (oci)` still moves the binary's 136 MB per run |
| (b) cosign in the Docker client image | Package 3's first form | One image | Every OCI set loads it, two of them without an assertion that needs it |
| (c) A Kubernetes image volume of the cosign image | | No copy on the node | The feature's state on every runner's kind node would have to be checked and carried; not checked |
| (d) cosign on the runner, outside the cluster | | Nothing in kind | A download per run from outside, which the client images' cache exists to avoid (F-158); the clients' network and CA would have to be rebuilt on the runner |

**Choice:** (a). No smaller real cosign exists: the release binary is the 136 MB. **Applied in the spec:** R-SIG-052, R-SIG-060. Code:
[Dockerfile.cosign](../../test/e2e/ociclients/Dockerfile.cosign), [deploy/dev/e2e.sh](../../deploy/dev/e2e.sh) `oci_cosign_node`,
[oci-clients.yaml](../../deploy/dev/oci-clients.yaml).

### Q-SIG-196 · A release is judged as the gate switched on with the stored anchors

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Taken when package 2
of stage 3 was built.

Context: R-SIG-070 releases "an artifact the gate refuses" and answers 409 for one that passes; it does not say what a release on a repository
whose gate is off is.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The release judges the gate as if it were on, with the stored anchors (chosen) | | The preview of R-SIG-072 shows what switching on would refuse; a team releases those first and switches on without an outage | A release can stand for a gate that is never switched on |
| (b) 409 `nothing_to_release` while the gate is off | | A release exists only where it acts | Switching on refuses the urgent artifacts until someone releases them, in the outage |

**Choice:** (a). **Applied in the spec:** R-SIG-070.

### Q-SIG-197 · A pending artifact can be released

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Whatever the gate does not pass, a pending artifact included, with the reason `pending` (chosen) | | A remote whose fetch never completes (an upstream that does not answer, the worker down) answers 503 to every client; a release ends it | A release of what a check would have passed a minute later |
| (b) Only a refused artifact | | The wording of R-SIG-070 | No way past a check that cannot run but switching the gate off |

**Choice:** (a). **Applied in the spec:** R-SIG-070; the column `signature_releases.refused_reason` takes `pending`.

### Q-SIG-198 · A release names a stored file of the repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The repository (a virtual one: a member the caller reads) must hold a stored file with the digest, else 404; the gate is judged on that file (chosen) | The gate classifies an item by its path | The format decides whether the digest is an artifact at all (a layer passes anyway), and a digest of another repository is not released by mistake | An artifact can be released only while it is stored |
| (b) Any digest | | A release before the upload | The gate cannot say what the digest is, so 409 `nothing_to_release` cannot be decided |

**Choice:** (a). The release itself survives the removal of the items, as R-SIG-070 says. **Applied in the spec:** R-SIG-070.

### Q-SIG-199 · The preview always judges the gate switched on and takes only the anchors

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `?anchors=` alone; absent, the stored anchors; present and empty, any anchor (chosen) | | A preview of the gate switched off answers nothing; one parameter less to explain | The first plan's URL changes |
| (b) `?onlyVerified=true&anchors=` | | The first plan | `onlyVerified=false` would be a preview that previews nothing |

**Choice:** (a). **Applied in the spec:** R-SIG-072, R-SIG-073.

### Q-SIG-200 · The preview reads in the order of the live paths and keeps the newest refused as examples

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The items in the order of the index of live paths (`items_live_path`), the examples the refused artifacts with the highest item ids among those read (chosen) | | No index more on the largest table; a small repository in a large installation is read from its own rows | Beyond the bound the examples are the newest of what was read, not of the repository |
| (b) The items newest first by a new index `(repo_id, id)` | | The examples are the repository's newest | An index more on `items` for one preview |

**Choice:** (a). **Applied in the spec:** R-SIG-072.

### Q-SIG-201 · The purge of a retired repository keeps its releases

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `signature_releases` is in the purge's kept list, with the repository's other decisions (chosen) | Q-REPO-376: content goes, configuration stays | A release is a decision about the repository's gate and survives its items by R-SIG-070 | Rows of a retired repository stay |
| (b) Purged with the content | | Nothing of a retired repository stays | A revived repository loses decisions an administrator took |

**Choice:** (a). **Applied in the spec:** R-SIG-070; the list of `test/integration/upstream/purge_test.go`.

### Q-SIG-202 · The impact is the gate's decision under two snapshots, counted per signed artifact and repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The candidates are the artifacts whose counting signatures the change touches (by the anchor index, the key index, or the identity); each is judged by the gate's `Judge` under the snapshot of now and the one the change makes, per gated repository; one that passes before and not after and that no release passes is counted (chosen) | | The count subtracts as the gate does (R-SIG-066), chosen anchors and another signature included; no second rule to keep equal | An artifact counts once per gated repository that attaches it (a virtual repository beside its member); an index counts once, not its platforms |
| (b) Count trust rows whose anchors contain the anchor | | One statement | Counts artifacts another anchor still passes, and ignores releases and chosen anchors |

**Choice:** (a). **Applied in the spec:** R-SIG-071.

### Q-SIG-203 · The members refused and gate are each repository's own gate

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-SIG-073 says "the gate that applies refuses it now in the addressed repository"; through a virtual repository two gates apply (R-SIG-065), and the
version list names no addressed repository.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Each answer's repository judges its own gate: a file's, a version's in the repository it was read from, a virtual repository's over the members that hold the version for the caller; the version answer's entry per repository; in the version list a version is refused when the gate of one repository that holds it for the caller refuses it (chosen) | | One rule; a member's refusal shows in the member's own entry, which the version answer lists beside the virtual one | A virtual repository's member `refused` does not include its holder's gate, and the version list does not include a virtual repository's own gate (H-1224) |
| (b) Every gate a download through the repository would meet | | What a client would get | A version list row would need a decision per virtual repository and member, per page |

**Choice:** (a). **Applied in the spec:** R-SIG-073; [H-1224](#h-1224--a-virtual-repositorys-member-refused-and-the-version-list-show-one-gate-each).

### Q-SIG-204 · passedBy names its kind and the signature's id

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `{"kind": "signature" \| "release", "signature": <id> \| null}` (chosen) | | One object the generated types describe; the id finds the signature in the same answer's list | Not the first plan's two shapes |
| (b) `{"signature": …}` or `{"release": …}` | The first plan | | A union the generated types render loosely |

**Choice:** (a). **Applied in the spec:** R-SIG-073.

### Q-SIG-205 · A release is read only where the signatures do not pass

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The gate reads `signature_releases` of its repository after its decision, only when it is not a pass (chosen) | | A passing download reads no more than package 1 did (R-SIG-064's cost) | One statement more for a refused or pending download |
| (b) Joined into the rows' statement | | One statement for every download | Every gated download pays for the exceptions |

**Choice:** (a). **Applied in the spec:** R-SIG-062, R-SIG-070.

### Q-SIG-211 · A writer of the repository reads through the gate where the format's signing tools read the artifact

Status: **Decided by the owner on 2026-11-22: (a), with a trace of every such read** ("a mit spur"); first decided by the development
agent in his absence, after the measurement with real clients of 2026-11-21 (defect 2 of
[signatures.md](../developer/signatures.md#defects-found)). Each served download that passes only for the writer is written as
`signature.download_by_writer` and counted in `quader_signatures_downloads_by_writer_total` (R-SIG-075,
[Q-SIG-240](#q-sig-240--a-writers-head-and-get-of-one-manifest-within-a-minute-are-one-event)).

Context: R-SIG-064 says OCI's signatures arrive after the push; `cosign sign` `GET`s and `notation sign` `HEAD`s the manifest before they
sign, the gate refused that read, and an image pushed while the gate is on could never be signed in its repository.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A principal that may write to the repository reads through the signature gate where the format's signing tools read the artifact from the repository, which is OCI: manifest `GET` and `HEAD`, by tag and by digest (chosen) | | Who may put content there may read it back to sign it; a consumer with the read right alone stays gated | A token with the write right pulls unverified images ([H-1229](../security/artifact-signatures.md#h-1229--a-holder-of-the-write-right-pulls-unverified-images-of-a-gated-oci-repository)) |
| (b) Every `HEAD` and every read by digest passes | | No right to look up | A kubelet pulls by digest: the commonest pull would bypass the gate |
| (c) Nothing passes; an image is signed in an ungated repository and copied over with its signatures | | The gate means what it says | Quader has no promotion; the flow would have to be built and documented first |
| (d) An unsigned artifact stays readable to its uploader for some minutes | | Narrow | A hole that depends on time |

**Choice:** (a). The write right is asked only where the gate would refuse or hold, never on the pass path, and only for the repository that
holds the item (not through a virtual repository). The malware gate and the vulnerability block are not opened by it. The other formats need
no such read: npm signs the tarball and makes its provenance before the publish, and a publish carries both; NuGet signs the `.nupkg` on the
author's machine before the push; Ansible Galaxy's signature is made over the local `MANIFEST.json` and posted beside the version; a generic
file's signature is made on the local file and uploaded beside it; Debian has no gate. **Applied in the spec:** R-SIG-064; [ADR-237](../decisions.md#adr-237--the-signature-declaration-says-whether-a-writer-reads-to-sign-and-how-the-gate-names-an-artifact);
`formats.SignatureDeclaration.ReadToSign`.

### Q-SIG-212 · A refusal says what is missing: a signature of the publisher or the build

Status: **Decided by the development agent** after the measurement of 2026-11-21 (defect 6).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every reason's text speaks of the signature of the publisher or the build (chosen) | | Only those count (R-SIG-062); an npm package with npm's registry signature, or a nuget.org package with its repository countersignature, is not told it has none | Longer texts |
| (b) The texts as first planned | | Short | "it has no signature" is false where Quader itself shows a registry's signature verified |

**Choice:** (a). **Applied in the spec:** R-SIG-068.

### Q-SIG-213 · The gate's answers name the artifact in its client's words

Status: **Decided by the development agent** after the measurement of 2026-11-21 (defect 7).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The format names the artifact (`SignatureDeclaration.ArtifactName`): an image `<name>:<tag>` or `<name>@<digest>`, npm `<name>@<version>`, NuGet `<id> <version>`, Ansible Galaxy `<namespace>.<name>:<version>`; a generic file and `/files/` its path (chosen) | An addition to the contract, which keeps version 9 | The client's own words; the core names no format | A NuGet id as stored, in lower case |
| (b) The stored path | | One rule | `unsigned/_tags/1.0` and `_manifests` are words of Quader's file view, not of the client |

**Choice:** (a). The details link keeps the stored path, which the file's page needs. **Applied in the spec:** R-SIG-068.

### Q-SIG-214 · NuGet's client gets no text in a header, for now

Status: **Decided by the development agent** after the measurement of 2026-11-21 (defect 5). **Replaced by
[Q-SIG-241](#q-sig-241--what-a-client-is-shown-where-it-prints-no-body-of-the-artifacts-refusal)** on 2026-11-23, on its own condition: the
header was measured with the pinned SDK, which prints it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) No header now; the measured behaviour stays written down (chosen) | | Nothing claimed that was not seen | `dotnet restore` still shows only the status |
| (b) Send the gate's text in `X-NuGet-Warning` on NuGet's routes | | NuGet's client prints the header for nuget.org's notices | Whether it prints it on an error answer was not measured; showing it needs a run of the pinned .NET SDK against a gated repository, which this change did not make |

**Choice:** (a), revisited when the header is measured with the pinned SDK. **Applied in the spec:** R-SIG-068.

### Q-SIG-216 · Quader reads NuGet's CMS itself, and a signer named by its key

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the measurement of 2026-11-21 ([developer/signatures.md](../developer/signatures.md#defects-found), defect 1). `dotnet nuget sign`
(SDK 10.0.403) and nuget.org write every `SignerInfo` as version 3, naming the signer by `subjectKeyIdentifier` (RFC 5652, section 5.3);
`github.com/digitorus/pkcs7` reads `issuerAndSerialNumber` only, in its newest version too (2026-09-14), as `smallstep/pkcs7` v0.2.3 and
tspclient-go's CMS do. The package already carried its own countersignature reader, attribute checks and signature verification over the
signed attributes; the library parsed the outer `SignedData` and the timestamp tokens.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The reader of `ContentInfo`, `SignedData`, `SignerInfo` (both forms of the signer) and RFC 3161's `TSTInfo` is code of `nugetcms` on `encoding/asn1`; BER is brought to definite lengths by the bounds' walker first; the timestamp token is verified by the same code as the signers (chosen) | | No dependency changes; one reader for the author, the countersignature and the timestamps; the signature is checked over the signed attributes as encoded, not as re-encoded | About 250 lines of ASN.1 that are this repository's to keep |
| (b) Another library that reads both forms | | Written by others | A module more for about 250 lines; none of the CMS readers already in the build reads both (digitorus/pkcs7, tspclient-go's), and a second timestamp reader beside digitorus's; no other library was examined |
| (c) Fork digitorus/pkcs7 through a `replace` | | Smallest diff | A fork to keep against upstream; Renovate cannot raise it |

**Choice:** (a). digitorus/pkcs7 and digitorus/timestamp stay in the build for sigstore-go and for Quader's reading of a Sigstore bundle's
timestamp ([proofs.go](../../internal/signatures/scheme/sigstore/proofs.go)), where the verification is sigstore-go's own; the blind spot
there is [H-1233](../security/artifact-signatures.md#h-1233--a-timestamp-of-a-sigstore-bundle-or-a-notation-signature-whose-authority-names-its-signer-by-its-key-is-not-read).
A certificate without the key identifier extension is named by the SHA-1 of its key (RFC 5280, section 4.2.1.2, method 1); both
signatures of the real tools carry the extension, so that fallback is tested with the tests' own signatures only. **Applied in the
spec:** R-SIG-014, R-SIG-029, R-SIG-042.

### Q-SIG-217 · A batch replaces the attachments of its own sources only

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the measurement's defect 3. A signature posted for a local Ansible version is attached at the archive's item (so that it goes with
the version) with the source `upload`; discovery records the detector's batch of that item, which carries nothing, and the statement that
replaces a carrier's attachments removed every attachment of the item, the posted one with it. Stage 2's signatures Quader issues are
attached at the signed item too (`issued`, R-SIG-054). `upload` was also the source of npm's provenance bundle, which the npm detector
names.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A batch names the sources it speaks for and replaces attachments of those only; a detector's batch speaks for the eight sources a detector names, the management API's for `upload`; `issued` is written and removed by the job `signing` through statements of its own (`RecordIssued`, `DetachRetiredIssued`), so no batch speaks for it and stage 2's exception in the statement (`source <> 'issued'`) becomes a case of the rule; npm's provenance bundle, stored beside its tarball and judged by its gate as a signature file, is attached `sibling`, and `formats.SignatureSourceValid` refuses `upload` from a detector (chosen) | | One rule for every writer and format; no migration, no API change, no change of the format contract's surface; the writers' sources are disjoint and a detector cannot take one of the others' | npm's provenance bundle shows the source `sibling` where it showed `upload` |
| (b) A source of its own for a posted signature (`posted`), npm keeping `upload` | | `upload` keeps its meaning for npm | A migration of the source's CHECK and an API enum for one writer; the same three places stage 2 changes for `issued` |
| (c) Each format declares the sources its detector names, and the batch replaces those | | Per format exact | A change of the frozen contract for a storage rule; nothing stops a format's detector and its API sharing a source |
| (d) The replacement leaves out `upload` and `issued` by name | | Smallest | A special case per writer, the next writer forgotten |

**Choice:** (a). A batch without sources replaces nothing. Rows the defect already removed are not brought back: a version whose posted
signature was lost shows **No signature** and is posted again. **Applied in the spec:** R-SIG-004, R-SIG-028, R-SIG-031, R-SIG-054; the code comment of
`SignatureCandidate.Source` and `SignatureSourceValid` in [architecture.md](architecture.md) R-ARCH-008.

### Q-SIG-218 · A self-signed root's own signature is not judged

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: with defect 1 repaired, nuget.org's repository countersignature of 2023 read `unsupported` (`weak_algorithm`): its chain ends at
DigiCert Assured ID Root CA, self-signed with SHA-1 in 2006, and every certificate of a chain was refused for a weak signature algorithm.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The signature algorithm of a self-signed certificate is not judged; its key is (chosen) | crypto/x509 never verifies a root's own signature; a root is trusted as an anchor | nuget.org's older countersignatures, and a Notation chain to such a root, read as what they are | A root's age is not shown as weak |
| (b) Leave the root out of the chain before the checks | | The same effect | The evidence loses the root the trust step matches an anchor by |
| (c) Keep refusing | | Strict | Most of nuget.org's packages read **Scheme not supported** |

**Choice:** (a), in `pki.CheckCertificate`, so it holds for NuGet and Notation alike. A certificate a chain verifies with SHA-1 (a leaf or an
intermediate) stays `weak_algorithm`. **Applied in the spec:** R-SIG-029.

### Q-SIG-219 · A remote's version reads the document it holds the version as

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the measurement's defect 4. The version's answer takes an OCI tag's subjects from the version's metadata only where the repository
holds the tag as a file whose blob is that manifest; a remote holds its tags as documents (R-OCI-036), so a remote's version had no subject.
The version list had the same gap: it counts the blobs of a version's files.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `SignatureVersionBlobs` also answers the blob of the document a repository lists a version by (`package_version_listing.document_sha256`, a remote's OCI tag), marked as a document; the version's answer reads it for every holder in one call and the version list counts it (chosen) | | Format-neutral: the listing's own column; each holder's own tag, also when the version was read from another repository; the list and the answer agree | One statement more per version answer |
| (b) Trust the metadata's digest for the repository the version was read from | | No statement | A remote that is not that repository still shows nothing, as in the measurement, where the local upstream held the tag first |
| (c) Look up the manifest by its digest path in each holder | | Uses the items API | A remote whose tag moved shows the old image's signatures; OCI's path layout in the API |

**Choice:** (a). npm's, NuGet's, Ansible's and Debian's remotes hold a version's file as an item and needed no change; the generic format has
no remote class; a virtual repository shows the union of its members, a remote member now included. **Applied in the spec:** R-SIG-026.

### Q-SIG-223 · The section reads the preview when it opens and learns from it that the checks are off

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Taken when package 3
of stage 3 (the screens) was built.

Context: R-SIG-074 shows the stored setting's state "when the section opens" and a sentence "while `signatures.enabled` is false"; the switch is
read with `settings:read`, which a project Owner does not hold, and the preview answers `409 signatures_not_enabled` while the checks are off.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The preview is read whenever the section opens, the gate on or off; its `409 signatures_not_enabled` says the checks are off; with the gate off the line reads "Switched on with the stored anchors: …" (chosen) | | One read serves the state line, the sentence and the preview before switching on, for an Owner too | A read of up to 10 s on a large repository each time someone opens the section |
| (b) The preview only while the gate is on, the switch from `GET /api/v1/signatures/settings` | | No read for a gate that is off | An Owner never learns that the checks are off, and nobody sees what switching on would refuse before saving |

**Choice:** (a). The read is bounded (R-SIG-072) and happens only when a writer opens a closed section. **Applied in the spec:** R-SIG-074.

### Q-SIG-224 · A writer who may not weaken gets the narrowings as controls and the rest read-only

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The controls follow the order of R-SIG-061: the box of a gate that is on is read-only; with a stored list, **Any trust anchor** and an anchor not on the list are read-only and the last ticked one cannot be taken away; while the gate is off or accepts any anchor everything is open (chosen) | | The Owner meets no `403 signature_weakening_refused`; a narrowing stays open, as R-SIG-074 says | The rule is spread over the controls instead of one sentence |
| (b) Everything open, the API's `403` shown on save | | Simple | The page offers what it knows fails |
| (c) The whole gate read-only once on | | Simpler | Against "a narrowing stays open to it" |

**Choice:** (a), with the sentence "Only an administrator or someone who triages security findings can loosen this." One **Save changes** writes
the gate and a remote's fetch; a narrowing whose preview refuses anything asks first. **Applied in the spec:** R-SIG-074.

### Q-SIG-225 · The anchors offered are the project's list and, for `settings:read`, the installation's own

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: `GET /api/v1/projects/{p}/signatures/anchors` answers every member the project's anchors and the installation's applied ones; the
installation's list with switched-off anchors needs `settings:read`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The project's list for everyone, the installation's own list (switched-off anchors too) for the holder of `settings:read`; a stored reference neither names is **Removed trust anchor** (chosen) | | An Owner sees every anchor that can count in the repository; an administrator sees a switched-off one as such | For an Owner a switched-off installation anchor that the setting names reads as removed ([H-1237](#h-1237--an-owner-reads-a-switched-off-installation-anchor-that-a-gate-names-as-removed)) |
| (b) A new operation that resolves the references | | Exact for everyone | An operation more for one word |

**Choice:** (a). The gate treats both alike, it matches nothing (R-SIG-061). **Applied in the spec:** R-SIG-074.

### Q-SIG-226 · An image's platforms are said to pass through their index where the answer decides it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-SIG-074 lists an image's platforms "with their decision"; the member `gate` of R-SIG-073 judges the tag's artifact (the index) and
names the signature that passed it, not a decision per platform.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Where the index passes on its own signature (the passing signature names no platform), each platform reads "Signed through its image index"; a refused index names no platform's decision (chosen) | | Says only what the answer decides; the case R-SIG-074's renders name | A platform pulled by its digest with a signature of its own is not shown as passing under a refused index ([H-1238](#h-1238--under-a-refused-image-index-the-screens-name-no-platforms-decision)) |
| (b) A decision per platform derived in the browser from the platforms' signatures | | Every platform has a word | Chosen anchors, blocks and the subtraction of R-SIG-066 are the server's: the browser would guess |
| (c) The API answers a decision per platform | | Exact | A change of the API for a case the gate decides at the download anyway |

**Choice:** (a); the development agent would take (c) when a team asks why a platform passes that the screen does not show. **Applied in the
spec:** R-SIG-074.

### Q-SIG-227 · The section Releases lists one repository's releases

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-SIG-074 puts a closed section **Releases** in the dashboard's view; R-SIG-073 lists releases per repository, and a release names a
digest, not a path.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The section asks for a repository (the view's repository filter by default) and lists its releases; a release's artifact is named where a refused download of the same bytes shows it (chosen) | | Uses the operation as it is; a release is a repository's decision (Q-SIG-88) | No list of every release at once ([H-1239](#h-1239--no-list-of-every-release-and-a-release-names-a-digest)) |
| (b) A new operation that lists every release the caller reads | | One list | An operation more, which this package does not need to show a release where it acts |

**Choice:** (a). A released row of the refused downloads also says by whom and when, and offers **Withdraw release**. **Applied in the spec:**
R-SIG-074.

### Q-SIG-228 · The client setup's line is read from the full repository document

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The line and the accepted anchors' titles come from the repository document a writer reads, the titles from the project's anchors; a reader of the reduced view of R-REPO-010 gets no line (chosen) | | No change of the API; R-SIG-074 says "read from the repository document" | A plain reader setting up a client does not learn of the gate there ([H-1240](#h-1240--a-reader-without-the-repositorys-configuration-sees-no-line-of-the-gate-in-the-client-setup)) |
| (b) The reduced view carries `signatures.onlyVerified` | | Every reader sees it | A member of the configuration in the consumer view, decided at R-REPO-010, not here |

**Choice:** (a); the refusal itself reaches every reader at the file and in the client's answer (R-SIG-068). **Applied in the spec:** R-SIG-074.

### Q-SIG-229 · The gate's decision is a block above the signatures, and the banner names the version

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Per repository a block above its signatures in the version's tab and in the file's section: that it serves only verified artifacts, the accepted anchors, the decision with the reason, the release with who, when and why; the version's banner names the version where the repository holds one file of it, else the file (chosen) | | One place for the gate and its actions; the banner says what a client asked for (`chart-render-kit 3.5.0`, not `chart-render-kit-3.5.0.tgz`), as Q-SIG-213 does for the client | A version with several files names each refused file |
| (b) The decision inside the details of each signature | | Near the signatures | The decision is the repository's, not a signature's |

**Choice:** (a). **Applied in the spec:** R-SIG-074.

### Q-SIG-230 · The impact of a block is a step of its dialog, and an impact that cannot be read asks nothing more

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) **Block signer** asks the impact on its first submit; above 0 the dialog shows the count and the repositories and its button reads **Block signer and refuse them**; a removal, a switch-off and project anchors switched off add the count to their question; an impact that fails leaves the screen's own question (chosen) | | No dialog over a dialog; the count is a help, the change stays the administrator's | A failed count is not said |
| (b) A confirmation over the block's dialog | | One form of question | Two dialogs stacked |

**Choice:** (a). **Applied in the spec:** R-SIG-074.

### Q-SIG-231 · No screen narrows an anchor's material, so none asks its impact

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-SIG-074 asks with the impact when an anchor's material is narrowed; the screens of stage 1 add and remove anchors and switch them,
and edit no material (R-SIG-036).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Nothing asked: the change has no screen; `PATCH` of the material through the API counts as `narrow` for whoever asks the impact (chosen) | | No form built for one question | An administrator who edits material through the API is not asked |
| (b) A form that edits an anchor's material, with the question | | Complete | A screen stage 1 did not specify |

**Choice:** (a). **Applied in the spec:** R-SIG-074.

### Q-SIG-232 · The dashboard's view shares its repository filter and lists the refused downloads first

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) **Refused downloads** first with its own head (repository, reason), then the findings (search, kind, repository); the repository is one filter of the view (`?repository=`), so a repository's settings link to both (chosen) | | The list with an action first; one address names a repository's whole picture | The repository chip stands in both heads |
| (b) Separate filters per list | | Independent | Two parameters for the same choice |

**Choice:** (a). **Applied in the spec:** R-SIG-074, R-SIG-038.

### Q-SIG-233 · servedAs is a word, and the reason a member beside it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The second half of
package 4 of stage 2; R-SIG-055 names `servedAs` (`referrer` or `api`) and the brief "with the reason for an image where R-SIG-054 gives
one".

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `servedAs` the word R-SIG-055 names, and `servedReason` (`hides_signature`, `no_referrers_api`) beside it, present only for an image served through the API (chosen) | | The member reads as the specification writes it; the reason's absence says "by rule" for every other format; two closed enums | Two members for one fact |
| (b) `servedAs: {"as", "reason"}` | | One member | Not the word R-SIG-055 names; an object for a word in the common case |
| (c) The reason inside `attestation` | | No member more | Where it is served is no fact of the signed statement and changes without it (an older tag arriving) |

**Choice:** (a). **Applied in the spec:** R-SIG-055. Code: [signatures_receipt.go](../../internal/api/v1/signatures_receipt.go).

### Q-SIG-234 · The predicate is decoded from the stored bundle when the answer is read

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `SignatureViews` reads the bundle (`signature_objects.raw`, about 2.5 KiB) of an issued attestation alone, and the API decodes its DSSE payload with `bundle.Payload` and `statement.Read`, `Build`'s inverse, within the statement's bounds (16 KiB, 32 signatures, the CI claims through R-SIG-010's allowlist); a bundle that does not decode leaves the member out (chosen) | | The signed bytes are the one source: what the screen shows is what a verifier reads; no migration and no second copy that could disagree | A decode per receipt per answer: microseconds for a statement of a kilobyte |
| (b) The job writes the facts into a column of `signing_attestations` when it issues | | No decode on read | A migration and a copy of the statement beside the statement |
| (c) The browser downloads the bundle and decodes it | | No change of the answer | A request per receipt, and the screen would parse a signed format |

**Choice:** (a). **Applied in the spec:** R-SIG-055. Code: [statement.go](../../internal/signing/statement/statement.go) `Read`,
[bundle.go](../../internal/signing/bundle/bundle.go) `Payload`, [queries/signature_trust.sql](../../internal/meta/postgres/queries/signature_trust.sql) `SignatureViews`.

### Q-SIG-235 · No badge beside listed signatures reads "No signature of the publisher"

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. With stage 2 every
local npm version carries Quader's registry signature and every artifact a receipt, so the details of an artifact without a badge list them
under "No signature. No signature found."

| Option | What it means | For | Against |
|---|---|---|---|
| (a) When the badge is `none` and the details list a signature or a receipt: **No signature of the publisher.** "What is listed below confirms where the artifact was published or received, not who built it." (chosen) | | The words agree with the list and say what the badge stands for (Q-SIG-1); a remote's npmjs signature reads right too | Two more sentences to translate |
| (b) Keep "No signature found." | | Nothing changes | Contradicts the list below it on most local npm versions |

**Choice:** (a). The kinds the format knows still follow. **Applied in the spec:** R-SIG-056. Code:
[signature-words.ts](../../web/src/app/views/signatures/signature-words.ts) `noPublisherSignature`.

### Q-SIG-236 · The job's backlog is one quiet line of the section Signing identity

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. R-SIG-055 answers
`backlog` per kind; the section's builder left it unshown.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One muted line below the identity: "Waiting to be signed: 3 npm versions and 1,204 receipt attestations, the oldest since 14 minutes ago." and, with work at its last attempt, "2 reached their last attempt; the job tries again once a day."; "Nothing waits to be signed." beside a current identity without work; no line without an identity and without work (chosen) | | An administrator sees after a first identity or a rotation that the job works, and that it is done; nothing grows in the section | The kinds are summed into one sentence |
| (b) A facts table per kind | | Every number | Four rows for what is mostly "nothing" |
| (c) Not shown | | | A rotation's hours of work stay invisible |

**Choice:** (a). **Applied in the spec:** R-SIG-056. Code: [signing-identity.html](../../web/src/app/features/settings/signing-identity.html).

### Q-SIG-237 · Quader's own entries are rendered from the real answers changed in the browser

Status: **Decided by the development agent** — selected from the options below, as Q-SIG-175 for the section; the owner reviews it at the
walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The seed's real signatures answers with Quader's entries added (and npmjs's signature of the local version replaced by Quader's) in the browser ([renders/receipts.ts](../../web/e2e/renders/receipts.ts)); two new states (`issued` of the version, `receipt` of the image), and the image whose older signature puts its receipt behind the API, the file's details and the section's backlog folded into states that exist (chosen) | | No row of `signing_identities` with a key sealed by the profile; twelve pictures more, not thirty | The renders do not show the store's own answer of an issued entry (the integration area `signing` asserts it) |
| (b) Rows written by SQL | | The real answer | An issued signature names an identity, which needs a sealed key, and the worker would try to open it |
| (c) A real identity in the profile through the API | | Everything real | A CA tool and an import in every seed of every runner, and the job signing every seeded artifact |

**Choice:** (a). **Applied in the spec:** R-SIG-056; [developer/web-ui.md](../developer/web-ui.md#the-signatures-screens).

### Q-SIG-238 · A receipt shows four facts and its download; the rest of its statement is one fold further

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Simple by default
([CLAUDE.md](../../CLAUDE.md#product-simple-by-default-details-on-demand)).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A block of its own under **Attestations**, above the others' table: **Issued**, **Malware scan**, **Served** with its reason, **Download attestation** (the route, with the item's permission); **Technical details** hold the time of receipt, the origin, the signature database, the vulnerability state with its data's date, the signatures it names with their integrity states, the signing identity, the predicate, the size and where it is attached; no **Download original**, which is the same bundle (chosen) | R-SIG-056's list | What R-SIG-056 names in front, the statement on demand | The origin stands twice when the fold is open (above as the stored origin, in the fold as the receipt said it) |
| (b) A row of the attestations' table | | One table | A table row has no room for a reason and a download of its own |
| (c) Every fact of the statement in front | | Nothing hidden | A block as long as a publisher's signature for what most readers never open |

**Choice:** (a). **Applied in the spec:** R-SIG-056. Code: [signature-report.html](../../web/src/app/views/signatures/signature-report.html).

### Q-SIG-239 · A retired identity's entries say what follows, and a retired receipt offers no download

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The API tells by
`issuedBy.state` (R-SIG-055); a retired identity's npm signature is no longer served (R-SIG-048) and its receipt not by the route (Q-SIG-191).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The entry's sentence says that an administrator retired the identity, that it is no longer served, and that Quader signs again with the current identity; the receipt shows no **Download attestation** (chosen) | | The screen says what the wire does; no button that answers 404 | — |
| (b) Hide a retired identity's entries | | Shorter | The window until the job signs again is invisible; the API lists them |

**Choice:** (a). **Applied in the spec:** R-SIG-056. Code: [signature-words.ts](../../web/src/app/views/signatures/signature-words.ts) `issuedSentence`.

### Q-SIG-240 · A writer's HEAD and GET of one manifest within a minute are one event

Status: **Decided by the development agent** when the owner's trace of Q-SIG-211 was built (2026-11-22); the owner reviews it at the
walkthrough.

Context: the owner kept the writer's read of R-SIG-064 with a trace: an audit event and a metric for each such read. `notation sign` `HEAD`s
and `cosign sign` `GET`s the manifest, and a tool often does both within seconds, so one signing is two or more served downloads. The refusal's
event `signature.download_refused` is written at most once a minute per repository, path and client address on each replica, and its counter
counts every refusal. The auditor's question is who took an artifact without an accepted signature out of a gated repository, which one, and
when.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One event a minute per repository, path, caller and client address on each replica; the metric counts every download (chosen) | The refusal's window with the caller added to its key | One signing is one entry; the caller, the artifact and the time are all there; the counter keeps the number of reads; the bound of the refusal's dedup holds | A second read by the same caller within the minute is in the metric only |
| (b) One event per request | | Every read is a row | One signing is two or more rows; a pipeline that polls a manifest fills the log |
| (c) The refusal's key alone (repository, path, client address) | One window for both events | One rule | Two callers behind one address within a minute are one entry: the caller, which the event exists for, is lost |

**Choice:** (a). The path is the stored path, so a read by tag and one by digest are two events, each naming the artifact as its client
named it (Q-SIG-213). The outcome is `warning`: the download was served, and it is the one served download of a gated repository an auditor
looks at. **Applied in the spec:** R-SIG-064, R-SIG-075. Code: [gate.go](../../internal/signatures/gate/gate.go) `WriterRead`.

### Q-SIG-241 · What a client is shown where it prints no body of the artifact's refusal

Status: **Decided by the development agent** after the measurement with real clients of 2026-11-23 (defect 5 of
[developer/signatures.md](../developer/signatures.md#defects-found)), under the owner's rule of 2026-11-22 that open questions are decided and
written down; it replaces Q-SIG-214. It extends the owner's Q-SIG-79 (the gate acts at download) to the one document a client reads to
download: Ansible Galaxy's version document is refused where its archive is.

Context: R-SIG-068 promises the text of a refusal in every client. Three do not show it. `dotnet restore` prints the status line and never a
body; `ansible-galaxy` prints urllib's status line for the archive (`HTTP Error 403: Forbidden`, the reason phrase Go's server writes
itself); Docker asks a tag by `HEAD`, which has no body, and Podman prints a `503` without its body. Measured on 2026-11-23 with the pinned
clients: `dotnet restore` prints the header `X-NuGet-Warning` of any answer as `warning : <value>`, an error's included, once for every
attempt; `ansible-galaxy` reads the version document before the archive and prints the `detail` of its error (`Error when getting collection
version metadata for <ns>.<name>:<version> from <server> (HTTP Code: 403, Message: <detail> Code: <code>)`); Docker turns a `403` of its
`HEAD` into a `GET` and prints that body, and retries a `503` of its `HEAD` once without one. The same holds for the malware gate's and the
vulnerability block's answers, which these clients show no better (R-MAL-016).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Each client where it reads text: NuGet's answers of the three download checks carry their text in `X-NuGet-Warning`; Ansible Galaxy's version document answers what the archive's download would (the three checks of the stored archive, without serving it); OCI's pending answer stays as it is, and what Docker, Podman and the kubelet show is said per client (chosen) | Two places of the formats, one host call (`ItemStore.Check`, `Resolver.Check`) | Every refusal reaches NuGet's and Ansible's users in words, with what to do; the gate's decision and its texts stay one; the malware gate and the vulnerability block profit alike | NuGet prints the warning once per attempt (six times); a version document of a gated repository costs the checks' statements; a pending image shows no text in any OCI client |
| (b) A reason phrase of Quader's own over HTTP/1.1 | The status line carries the text | `ansible-galaxy` and NuGet print it | Go's server writes the standard phrase, so the connection would be hijacked past the request's logging and metrics; HTTP/2 has none; RFC 9112 tells clients to ignore it |
| (c) For OCI's pending `HEAD`, a status that makes the client `GET` the body (a 4xx in place of the `503`) | | Docker would print the text | A wrong status for a temporary state, chosen for one client's fallback; a kubelet would back off as for a refusal |
| (d) Nothing; the limits written down (Q-SIG-214) | | Nothing to build | `dotnet restore` blames the feed after six 503s, and an Ansible user learns only "Forbidden" |

**Choice:** (a). The header carries only the answers of the checks of R-SIG-069 (`signature_not_verified`, `signature_check_pending`,
`malware_found`, `malware_not_checked`, `malware_check_pending`, `vulnerability_blocked`, `vulnerability_data_missing`): a restore asks
its sources with 404s and warnings on them would bury the one that matters, and the download limits' `429` is
[usage-and-limits.md](usage-and-limits.md)'s Gap 1, the owner's to decide, which now has this measurement. The version document asks the
archive's checks in every class (local, remote, virtual); an archive no repository of the request stores yet is judged at its download alone
([H-1541](#h-1541--ansible-galaxy-shows-only-the-status-of-a-refusal-of-an-archive-that-no-repository-stored-before)). The OCI clients'
pending answer is [H-1540](#h-1540--docker-podman-and-the-kubelet-show-no-text-while-a-signature-is-being-checked). **Applied in the spec:**
R-SIG-064, R-SIG-068, R-SIG-078; [formats/nuget.md](formats/nuget.md) R-NUGET-002; [formats/ansible.md](formats/ansible.md) R-ANS-004;
[architecture.md](architecture.md) R-ARCH-008 (ADR-256).

### Q-SIG-422 · A writer's HEAD through the gate is not a read: it is served and neither counted nor written

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It replaces the
part of [Q-SIG-240](#q-sig-240--a-writers-head-and-get-of-one-manifest-within-a-minute-are-one-event) that counted a `HEAD`. Found by the
builder of the signatures with the real clients on 2026-11-23: after a few pushes and signings with Docker, cosign and Notation the
counter stood at 29. Read in the code the same day: the serve point decides the gate for `GET` and `HEAD` alike
(`serveChecks` in [signature_gate.go](../../internal/formathost/signature_gate.go)), and `Gate.WriterRead` counted and wrote every served
pass of a writer. A push asks whether each manifest it is about to put exists with a `HEAD` by digest, so every push of an image the
repository holds already (a second tag, a retried push, each platform of an index) was a "download by writer"; a signing tool resolves a
tag to its digest with a `HEAD`, which `notation sign` does instead of a `GET`. A `HEAD` answers the digest, the size and the media type,
none of the content the gate holds back from a reader. A blob (a layer, a config) is no artifact of the gate (R-SIG-063), so a push's
`HEAD`s of its blobs never reached the count.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A writer's `HEAD` is decided and served as before, and `WriterRead` records nothing for it; a `GET` (and a `Range` of it) is counted and written as before (chosen) | The trace is the content that left | The event says what its name says: an artifact without an accepted signature taken out of a gated repository; a push leaves none | `notation sign`, which only resolves, leaves no trace; it reads no content either |
| (b) Count a `HEAD` and leave it out of the event | | The metric shows every pass | The metric then says "downloads" of what was not downloaded, and the push's count stays |
| (c) Ask the push's `HEAD` apart from the signing tool's | | — | Nothing in a `HEAD` tells a push from a tool's resolution |

**Choice:** (a). A conditional `GET` answered `304` is still counted, since the gate decides before the condition is read; neither signing
tool nor a push sends one for a manifest. **Applied in the spec:** R-SIG-064, R-SIG-075, the metric of R-SIG-075's table;
[artifact-signatures.md](../security/artifact-signatures.md) H-1229. Code: `Download.Head` and `Gate.WriterRead` in
[gate.go](../../internal/signatures/gate/gate.go), set by `gateDownload` in [signature_gate.go](../../internal/formathost/signature_gate.go)
from the request's method; `serve.go` is unchanged. Tests: `TestR_SIG_064_AWritersReadIsAuditedAndCounted` (unit,
`internal/signatures/gate`: a writer's `HEAD` through `Serve.Run` and through `Check` records nothing, two `GET`s one event and two counts),
and the pusher's read in `TestR_SIG_070_ReleasesPreviewImpactAndTheAPI` (integration, area `signatures`: after the `HEAD` the replica's
`quader_signatures_downloads_by_writer_total{reason="missing"}` has no series, after the `GET` it is 1).

## What this does not cover

The gaps of the mechanisms are on [security/artifact-signatures.md](../security/artifact-signatures.md#what-this-does-not-cover). This page
records those about a client's result.

### H-1176 · Through a virtual npm repository, `npm audit signatures` reports local packages as missing a registry signature

Component: the keys endpoint of a virtual npm repository (R-SIG-028, Q-SIG-34). Subject: a project whose virtual npm repository lists a local
repository and a remote of a registry with keys (npmjs). Live in an installation without a signing identity, and in one whose identity is
removed or whose `signing.enabled` is false while previous signatures are not served. The union of the members' keys makes npm treat the
virtual repository as a registry with keys, and a package of the local member carries no registry signature without stage 2's signing, so
`npm audit signatures` lists it as missing and exits 1; upstream packages verify. What makes it real: a pipeline that runs `npm audit
signatures` against such a repository. Closed by stage 2's package 2 for an installation with a current signing identity: Quader signs every
local version and its keys join the union (R-SIG-050, built); what remains of it there is the window of
[H-1183](#h-1183--npm-audit-signatures-reports-a-local-version-as-missing-until-quader-has-signed-it).

### H-1183 · `npm audit signatures` reports a local version as missing until Quader has signed it

Component: the job `signing` (R-SIG-049, R-SIG-050, Q-SIG-56, Q-SIG-59). Subject: pipelines that run `npm audit signatures` against a local or
virtual npm repository. Live since stage 2's package 2, in four windows: a fraction of a second after a publish, before the job has signed it
(0.33 s measured on an idle development machine, R-SIG-049); the minutes of the backlog after an installation's first identity, while
discovery signs what was published before; up to the task's ten minutes for a version published in the moment the first identity became
current (Q-SIG-158); and after an administrator retires an identity, until the job has signed its versions again. In each, the registry offers
keys and the version carries no signature, so npm lists it as missing and exits 1. What makes it real: a pipeline that publishes and audits
within a second, or an audit during the backlog.

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

### H-1204 · After a retirement, a cached document carries a signature whose key the keys endpoint no longer lists

Component: the retirement of R-SIG-048 and the caches of a package document outside the replica (a Bastion, a remote of another registry in
front of the repository, npm's own cache of a packument). Subject: clients that run `npm audit signatures` through such a cache in the
minutes after an administrator retires an identity. Live since stage 2's package 2. The keys endpoint drops the retired key at once and the
job raises the revisions of the documents that carried its signatures (Q-SIG-160), but a cache keeps the old document until it revalidates,
and npm aborts the whole run when a signature names a keyid the keys answer lacks (R-SIG-050); the keys document itself may be cached too, in
which case the old signature still verifies. A retirement is for a key believed taken, so withdrawing the key at once is the point; what
makes it real is a Bastion or a chained registry that serves the npm document from its cache while the retirement is minutes old.

### H-1205 · Between a change of the identities and the job's next run, two renditions share a validator

Component: the documents of a local npm repository (Q-SIG-159, Q-SIG-160). Subject: clients of a local or virtual npm repository in the
seconds after a removal, a retirement or the first current identity after none. Live since stage 2's package 2. A document renders the
signatures of the listed identities as its statement reads them, and the revision moves only when the job, which the change wakes, has
compared the served set; meanwhile a replica that renders the package afresh serves the new set under the old revision while another
replica's cached rendition keeps the old one, so a conditional request can be answered `304` for bytes that differ in `dist.signatures`. It
ends with the raise, at most one run of the job after the change. What makes it real: a client that revalidates a document during that run
and keeps it until its next change.

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
the pending answer, which Docker shows as `503 Service Unavailable` alone
([H-1540](#h-1540--docker-podman-and-the-kubelet-show-no-text-while-a-signature-is-being-checked)) and `ansible-galaxy` as
`HTTP Error 503` ([H-1541](#h-1541--ansible-galaxy-shows-only-the-status-of-a-refusal-of-an-archive-that-no-repository-stored-before)),
and works when it is repeated; npm, NuGet and apt retry by themselves. A mirror pass decides ahead of
demand. What makes it real: a slow or rate-limited upstream, or a backlog of the job `signature-verify`.

### H-1540 · Docker, Podman and the kubelet show no text while a signature is being checked

Component: the pending answer of OCI (R-SIG-068, Q-SIG-241). Subject: a user who pulls an image of a gated repository whose signatures are
not judged yet. Live since package 1 of stage 3. Docker 29.9.3 asks the tag by `HEAD`, whose `503` has no body, asks once more and prints
`unexpected status from HEAD request to …: 503 Service Unavailable` after 50 s; Podman 5.8.9 `GET`s it, gets the body and prints only
`received unexpected HTTP status: 503 Service Unavailable` after about 104 s; the kubelet reports the `HEAD`'s status and backs off. None says
that a check is running or that a repeat will work; the image's page says **Not checked yet**. A status other than `503` would make Docker
read the text and lie about the state (Q-SIG-241 (c)). What makes it real: a pull right after the push of a signature, a backlog of the job
`signature-verify`, or a slow fetch of a remote's signatures.

### H-1541 · `ansible-galaxy` shows only the status of a refusal of an archive that no repository stored before

Component: the version document's checks (R-SIG-064, Q-SIG-241). Subject: the first install of a collection version through a gated remote
repository, or a virtual repository whose remote member holds no copy yet. Live since Q-SIG-241. The version document is served, because
there are no bytes to judge; the archive's download fetches and judges them and answers `HTTP Error 403: Forbidden` or `HTTP Error 503:
Service Unavailable`; the second install shows the text. The same holds while `ansible-galaxy` serves the version document from its own
response cache of an earlier, passing read. What makes it real: a gate on a remote of a Galaxy whose collections carry no signature the
repository's anchors know.

### H-1224 · A virtual repository's member refused and the version list show one gate each

Component: the members of R-SIG-073 ([Q-SIG-203](#q-sig-203--the-members-refused-and-gate-are-each-repositorys-own-gate)). Subject: a reader
of a version or a file through a virtual repository whose member is gated, and of the version list. Live since package 2 of stage 3. The member
`refused` of a version read from a virtual repository judges the virtual repository's own gate; a download through it meets the holding member's
gate too, which shows in the member's own entry of the version's signatures answer. The version list judges the gates of the repositories that
hold the version, not a virtual repository's own gate. A client's download is decided at the serve point either way. What makes it real: a gated
member under an ungated virtual repository, or a gated virtual repository.

### H-1237 · An Owner reads a switched-off installation anchor that a gate names as removed

Component: the section **Signatures** of a repository's settings ([Q-SIG-225](#q-sig-225--the-anchors-offered-are-the-projects-list-and-for-settingsread-the-installations-own)).
Subject: a project Owner without `settings:read` whose repository's gate names an installation anchor that an administrator switched off. Live
since package 3 of stage 3. The project's list of anchors holds only the applied ones, so the reference shows as **Removed trust anchor**; it
matches nothing either way (R-SIG-061), so what the gate does is shown right and only the word is wrong. What makes it real: a gate with
chosen anchors and an anchor switched off for a while.

### H-1238 · Under a refused image index the screens name no platform's decision

Component: the gate's block at an image ([Q-SIG-226](#q-sig-226--an-images-platforms-are-said-to-pass-through-their-index-where-the-answer-decides-it)).
Subject: readers of a multi-platform image whose index the gate refuses while a platform manifest carries a signature of its own that passes.
Live since package 3 of stage 3. The screen says that the image is refused and lists no platform; a client that pulls that platform by its
digest gets it (R-SIG-063). What makes it real: a publisher that signs platform manifests with `--recursive` and not the index.

### H-1239 · No list of every release, and a release names a digest

Component: the section **Releases** of the dashboard's view ([Q-SIG-227](#q-sig-227--the-section-releases-lists-one-repositorys-releases)).
Subject: an auditor who reviews releases after blocking a signer ([H-1223](../security/artifact-signatures.md#h-1223--a-release-keeps-passing-an-artifact-after-its-signer-is-blocked)).
Live since package 3 of stage 3. The releases are listed per repository, one repository at a time, and a release whose bytes no refused download
of the retention shows is named by its digest only. What makes it real: many gated repositories with releases.

### H-1240 · A reader without the repository's configuration sees no line of the gate in the client setup

Component: the client setup ([Q-SIG-228](#q-sig-228--the-client-setups-line-is-read-from-the-full-repository-document)). Subject: plain
readers who set up a client for a gated repository. Live since package 3 of stage 3. The reduced view of R-REPO-010 carries no member
`signatures`, so the page says nothing of the gate; the first refused download says it, in the client and at the file's page (R-SIG-068,
R-SIG-074). What makes it real: a team that switches a gate on without telling its readers.

### H-1241 · A receipt that waits for its checks is not shown at the artifact

Component: the details of a version and a file (R-SIG-056) and the signatures answers (R-SIG-055). Subject: a reader who opens an artifact
in the seconds to an hour between its commit and its receipt (R-SIG-053: the receipt waits for the checks' first verdicts or
`signing.attestations.wait_for_checks`). Live since the second half of package 4 of stage 2. The answers list what the store holds, and a
receipt that waits is a row of the job's work (`signing_work`), not a signature, so the details show no receipt and no word that one is
coming; the section **Signing identity** counts it in its backlog. What makes it real: a pipeline that verifies an artifact's receipt right
after the push and a reader who looks for it at the page meanwhile.
