# Signatures on artifacts

Status: stage 1 is in work. Built (package 1 of 4): the format contract's declaration and detector of every format, kit check 18, the
store of signature objects, signatures, attachments and origins with the commit hook and the fill hook that record them, and OCI's
attachment tags without a version row ([developer/signatures.md](../developer/signatures.md)). Not built: the read, the integrity check,
the trust step and its anchors, the job, the API, the screens, the formats' wire behaviour. Stage 1 of three: Quader detects, reads and verifies the signatures of artifacts of every format, keeps
each original signature with its verdict, decides trust against trust anchors of the installation and, where the administrator allows it,
of a project, shows the result at the artifact, alarms on an invalid signature, and serves signatures to every client the way its
protocol expects. It blocks nothing. Stage 2 (Quader signs) and stage 3 (a gate per repository) are not specified here; what stage 1's
model already carries for them is [R-SIG-043](#r-sig-043--what-stage-1-keeps-for-stage-2-and-stage-3).

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
  the configuration, backups and replicas, and the tests with their cost.

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
that. The six formats declare (R-SIG-022; sources read 2026-11-21, marked where a fact rests on a secondary source):

| Format | Signatures | Subject | Role | Where the client gets them | Requirement |
|---|---|---|---|---|---|
| OCI | Notation (`notary.x509`, JWS or COSE); a Sigstore bundle (cosign 3, keyless or with a key or its own PKI); the older cosign layout (tag `sha256-<hex>.sig`, layers `application/vnd.dev.cosign.simplesigning.v1+json`); attestations (`.att`, DSSE in-toto, Sigstore bundles of attestations); SBOMs (`.sbom`, referrers of an SBOM type); BuildKit's attestation entries of an index | the manifest or index digest | `publisher`, `provenance`, `attestation` | the referrers API; the referrers tag `sha256-<hex>`; the tags `sha256-<hex>.sig`, `.att`, `.sbom` | R-SIG-023 to R-SIG-027 |
| npm | the registry signature over `<name>@<version>:<integrity>` (ECDSA P-256, `dist.signatures`); the provenance attestation (Sigstore, SLSA provenance); the registry's publish attestation | the version and its tarball | `registry`, `provenance` | `dist.signatures` of the document; `-/npm/v1/keys`; `dist.attestations` and `-/npm/v1/attestations/<name>@<version>` | R-SIG-028 |
| NuGet | the author signature or the repository signature as the primary signature, a repository countersignature; CMS `SignedData` with an RFC 3161 timestamp | the package (a hash of its unsigned bytes) | `publisher` (author), `registry` (repository) | `.signature.p7s` inside the `.nupkg` | R-SIG-029 |
| Debian | OpenPGP signatures of the release document (`InRelease`, `Release.gpg`); apt checks no signature of a package | the release document, which names the index files, which name each package's SHA-256 | `registry` | `dists/<suite>/InRelease`, `Release`, `Release.gpg` | R-SIG-030 |
| Ansible Galaxy | detached OpenPGP signatures of `MANIFEST.json`, which names `FILES.json`, which names every file | the collection archive | `registry` (a signing service of the server), `publisher` (an uploaded signature) | `signatures[]` of the version document | R-SIG-031 |
| generic | a file beside the file: `<file>.asc` and `<file>.sig` (OpenPGP, or a raw signature of `cosign sign-blob`), `<file>.sigstore.json` (Sigstore bundle), `<file>.jws.sig` and `<file>.cose.sig` (Notation blob signatures) | the file of the same directory | `publisher` | the sibling file, downloaded as a file | R-SIG-032 |

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
  | `no_key` | the scheme carries no key (OpenPGP, `npm-registry`, `public-key`) and no anchor or announced key with the signer's ID is known; the signature cannot be matched with the content yet | "Signed · signer not trusted" |
  | `invalid` | the signature does not match its subject, or a part the scheme requires is broken (a digest mismatch, a payload that names another digest, a certificate chain that does not sign the signature, a log entry that does not verify) | "Invalid signature" |
  | `unsupported` | a scheme, an algorithm or a version Quader does not read, or a weak algorithm (`weak_algorithm`: SHA-1, RSA below 2048 bits) | no icon; "Scheme not supported" |
  | `malformed` | the object does not parse within the bounds of R-SIG-013 | no icon; "Signature could not be read" |
  | `failed` | the check could not run `max_attempts` times (the object store, a subject that went) | no icon; "Not checked yet" |

- **The verdict follows the bytes.** Every repository that holds the subject and an attachment of the signature (R-SIG-004) shows the same
  integrity verdict; a re-check changes it for all of them at once.
- **Orphans.** The job removes a signature whose last attachment went, and a signature object whose last signature went (R-SIG-011).

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

Acceptance criteria:

- Unit (`internal/signatures/scheme/…`, milliseconds each, bytes made by the test): each scheme answers `valid` for its signature, and
  `invalid` with the reason of the table for a flipped byte of the content, of the signature and of the payload's digest; NuGet's
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
  project_id    bigint  NULL REFERENCES projects (id) ON DELETE CASCADE,   -- NULL: the installation's scope
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
- **Discovery.** The job's first step and the task `signature-verify` (default `*/5 * * * *`) ask the formats for the signatures of items
  that have no attachment yet, in batches of 10,000 in item order: what was stored before stage 1 and what a restore loaded.
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
- **Cost:** a CA anchor's chain building takes tens of microseconds per signature, an OpenPGP or ECDSA check of a stored digest well under
  a millisecond; a million signatures take minutes of one worker's CPU (an estimate, to be measured by the package that builds it).

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
| ASN.1 and CBOR (CMS, COSE, timestamps) | the object's size; no indefinite length deeper than 32 levels | `malformed` |
| RSA keys | 2048 to 8192 bits | `unsupported` (`weak_algorithm`) or `malformed` |
| `signatures.check_timeout`: one check | 5 s | `failed` after the attempts |
| `signatures.max_content_size`: content a keyless scheme or NuGet reads | 8 GiB | `unsupported`, `limits_exceeded` |

- The readers are `internal/formats/untrusted`'s where a format reads (R-ARCH-028) and the scheme packages' own bounded parsers; a panic in a
  library is recovered per check and recorded as `malformed`, `content_unreadable`.
- A check writes nothing to disk; the content of a keyless scheme is streamed from the blob store and hashed.

Acceptance criteria:

- Unit (`internal/signatures/scheme/…`, under a second, crafted inputs built by the test): an object of 257 KiB, a chain of nine
  certificates, a JSON bundle 100,000 levels deep, a COSE envelope with a 2 GiB declared length and a CMS with an indefinitely nested
  attribute are refused within the bounds without a panic and with the heap within 64 MiB of its baseline.

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
- They are pinned once in `go.mod`, as every dependency is; Renovate raises them.
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
  project_id    bigint NULL REFERENCES projects (id) ON DELETE CASCADE,
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
| `nuget-org-timestamping` | `ca`, use `timestamping` | the roots of the timestamp authorities whose tokens nuget.org's repository countersignatures carry, so that a countersignature made with a certificate that expired since is judged at its stamped time ([Q-SIG-29](#q-sig-29--nugetorgs-preset-ships-the-timestamping-roots-its-countersignatures-need)) | the release |

- Each is a shipped anchor of the preset that names it ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-062), present whether
  or not the well-known repository is switched on; `sigstore-public` belongs to no preset.
- **Packaging.** The release embeds each in the binary (`internal/signatures/shipped`): Sigstore's trusted root and the npm keys as the TUF
  targets of the release's build, read by `make shipped-anchors` from the TUF repository through sigstore-go's embedded TUF root, and
  committed; the Debian, Ubuntu and nuget.org material as files.
- **Kept current online.** The task `signature-trust-root` (queue `signatures`, `exclusive`, default `17 4 * * *`) fetches the TUF
  metadata from `signatures.trust_root.url` (default `https://tuf-repo-cdn.sigstore.dev`) through the outbound client and its egress rules,
  verifies the chain from the embedded TUF root with sigstore-go's TUF client, and stores the targets `trusted_root.json` and
  `registry.npmjs.org/keys.json` in `signature_trust_roots` when they are newer than the release's; a stored document raises the anchor
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
  `title`, `kind` and material, the material inline or from a `configMapKeyRef` the chart mounts (public material, no Secret):

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
          trustRootConfigMap: { name: company-sigstore-root, key: trusted_root.json }
  ```

- `quader migrate` applies them in one transaction with the scope `configuration`: a declared anchor exists afterwards with its material,
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
  `anchor_epoch`, `version`, `updated_at`, `updated_by`), read and changed through `GET` and `PATCH /api/v1/signatures/settings`
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
  BuildKit's attestation entries leave the version's list of platforms (today `unknown/unknown` rows) and stand there.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_024_…`, about 3 s, bytes): an image `app:1.0` with a Notation referrer, a referrers tag and a
  `.sig` tag pushed by the test's client: the package lists one version and counts one; `tags/list` answers `1.0`, `sha256-<hex>` and
  `sha256-<hex>.sig`; the version's signatures answer lists three attachments; the image scanning's selection names one manifest; a
  BuildKit index's attestation entry is in the attachments and not in the platforms.

### R-SIG-025 · OCI: an attachment follows its image
Priority: **P1** · Covers: BEYOND-016

The owner's reading ([Q-SIG-16](#q-sig-16--the-eight-readings-of-the-request) r6): the signature follows its image on delete, cleanup,
copy and mirror.

- **Delete** (R-OCI-018): removing a manifest removes, besides its referrers (as today), the attachment tags of its digest of the same name
  (`sha256-<hex>`, `.sig`, `.att`, `.sbom`) and the manifests those tags point at unless another tag or manifest of the name references
  them, in the hook's transaction and with the same refusals; deleting only a tag of the image leaves the image and its attachments.
- **Trash:** an image that goes to the trash takes its attachment tags with it (the cascade's reason, Q-OCI-24); a restore from the trash
  brings them back.
- **The sweep** of unreferenced blobs and **untagged manifests:** a referrer stays while its subject stays (unchanged); attachment tags are
  tags and keep their manifests.
- **Eviction of a remote's cache** ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-045): evicting a manifest evicts its
  attachments.
- **Retention and copy** (R-STOR-039 and R-STOR-019, not built): the unit of an image is the image with its attachments; a retention run
  never removes an attachment of an image it keeps and removes the attachments with the image; a copy or a promotion of an image takes its
  attachments. The page that builds them adds the cases of this rule.
- **Mirror** ([formats/oci.md](formats/oci.md) R-OCI-026): the plan of each kept top-level manifest asks for its signatures as the
  background fetch does (R-SIG-026) and stores what it finds; "Referrers are not mirrored" is superseded.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_025_…`, about 3 s): deleting `app@<digest>` removes its referrer, its referrers tag, its
  `.sig` tag and their manifests; a `.sig` manifest that a second tag also names stays; a trashed image restored from the trash has its
  attachments again. Unit (`internal/formats/oci`, milliseconds): the mirror plan of a tag names its signature requests.

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
- **Through the engine** as background requests (`proxy.PriorityBackground`): the token handshake, the circuit, the fetch slots, the egress
  rules apply; the fetched manifests and documents are stored like any (R-OCI-022), and the commit hook detects them (R-SIG-011). It is
  system work: recorded per repository under `system` and charged to no caller's budget and no ceiling
  ([usage-and-limits.md](usage-and-limits.md) R-LIM-001, R-LIM-015); `signatures.remote.requests_per_hour` (default 600 per remote and
  replica) bounds it ([H-1173](../security/artifact-signatures.md#h-1173--the-background-fetch-asks-an-upstream-for-what-no-client-asked)).
- **A client's referrers query** through a remote passes through as today (R-OCI-024); its answer and the manifests the client then fetches
  are stored and detected without a request more.
- **A repository's detail setting** `signatures.fetch` (remote repositories, default `true`) switches the background fetch off for an
  upstream whose limits are tight; it is in the closed section **Signatures** of the repository's settings, never in the creation form.
- **What it costs against Docker Hub** (read 2026-11-21 on [Docker's page on pulls](https://docs.docker.com/docker-hub/usage/pulls/)):
  "Using GET emulates a real pull and counts towards the limit. Using HEAD won't"; the limits are 100 pulls per six hours per IPv4 address or
  IPv6 /64 without an account and 200 with a personal account. The `HEAD` requests count nothing; a `GET` of a signature manifest counts
  as a pull; whether the referrers request counts is not stated (it is no manifest URL), and whether Docker Hub serves the referrers API
  was not verified (a 404 sends Quader to the tags). So an unsigned image costs no pull more, an image with an older cosign signature one,
  and at most four with the bound. A mirror of Docker Hub without an account should set `signatures.fetch: false` or an account.

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_026_…`, about 4 s, the OCI stub of R-OCI-035 with a referrers API and one without): a pull of
  a signed image through `docker-hub` stores the image's Notation referrer and its `.sig` signature without a client asking, and the badge
  reads its state after the job; against the stub without the API the referrers tag is asked by `HEAD` and the `.sig` found; the stub
  records `HEAD` requests for absent tags and no `GET` for them; with `signatures.fetch: false` nothing is asked. An npm tarball fetched
  through `npm-remote` makes one request of the attestations and stores them.

### R-SIG-027 · OCI virtual repositories
Priority: **P1** · Covers: BEYOND-015

- The referrers of a virtual repository stay the union of its members' (R-OCI-027, Q-OCI-161); the tags list stays the union and holds
  attachment tags.
- The version page of an image through a virtual repository shows the attachments of the members that serve the image to the caller, in
  the scope of the virtual repository's project (R-SIG-008).
- The background fetch runs at the remote member that stored the manifest.

Acceptance criteria: integration (area `ocivirtual`, about 2 s): through `docker-all`, an image of `docker-hub` whose signature `docker-local`
holds as a referrer of the same digest lists both members' attachments in its signatures answer.

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
  - a **local** repository answers `{"keys": []}` in stage 1: Quader signs no npm package before stage 2 (d10, R-SIG-043);
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
- **Through a virtual repository with a remote member, a local package reads as missing a registry signature** to `npm audit signatures`
  until stage 2 signs local packages ([H-1176](#h-1176--through-a-virtual-npm-repository-npm-audit-signatures-reports-local-packages-as-missing-a-registry-signature)).

Acceptance criteria:

- Unit (`internal/formats/npm`, milliseconds): `Detect` over a stored document with `dist.signatures` and over a publish with a bundle; the
  keys answers of the three classes; the rewritten `dist.attestations.url`.
- Integration (area `signatures`, `TestR_SIG_028_…`, about 4 s, the npm stub signing with a test key that the test adds as a public-key
  anchor): through `npm-remote` the document's `dist.signatures` equals the stub's bytes, the keys endpoint answers the stub's keys, the
  attestations endpoint the stub's; through `npm-all` the keys are the union; a local publish with a bundle of the test's public-root
  stand-in serves `dist.attestations` and the bundle, one of the company's Sigstore serves neither and shows `internal` in the UI.
- End to end (the existing set `e2e (npm)`, one case, about 5 s, bytes): `npm audit signatures` passes for a package of the npm test upstream
  installed through `npm-remote`, with the test upstream's key served at `-/npm/v1/keys`.

### R-SIG-029 · NuGet: the signature inside the package
Priority: **P1** · Covers: quader-only

- **Detected** at the commit of every `.nupkg` (a push, a fill): a `.signature.p7s` entry is the primary signature (`nuget-cms`, the author's
  `publisher`, a repository's `registry`) and each repository countersignature a `registry` signature of its own (`part` 1, …), attached
  `embedded` to every repository that holds the blob (R-SIG-004). The package stays byte for byte as received (R-NUGET-005).
- **Read and checked** by R-SIG-005 and R-SIG-006: the unsigned-package hash reads the package once.
- **Trust:** nuget.org's countersignature under the shipped `nuget-org` anchor at the time its timestamp proves under
  `nuget-org-timestamping` (R-SIG-017); an author's signature under a public code-signing CA is **Signed · signer not trusted** until an
  administrator adds the CA (d4); one of the company CA under its anchor.
- **No `RepositorySignatures` resource** ([Q-SIG-36](#q-sig-36--nuget-lists-no-repository-signatures-resource)): the resource announces the
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
  it through its data port (`Covering`, the rows of R-DEB-023). The details of a `.deb` say "apt checks the signature of the index, not of the
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
- **A local version** accepts detached signatures through the management API (R-SIG-034): `POST
  /api/v1/projects/{projectKey}/repositories/{repoKey}/ansible/collections/{namespace}/{name}/versions/{version}/signatures` with
  `{"signature": "<armored OpenPGP signature of MANIFEST.json>"}`, at most 16 per version, decided as a publish of the version (`deploy` on
  its path and its ownership); it is checked before it is stored (an invalid one answers 422 `signature_invalid`; one whose key no anchor
  has is stored as `no_key`), served in `signatures[]` as `{"signature", "pubkey_fingerprint", "signing_service": null}`, and removed with
  `DELETE …/signatures/{fingerprint}` (`delete`). `ansible-galaxy` has no command to upload a signature; a CI job uses its token on this
  operation. Q-ANS-14 is superseded in part.
- A virtual version document carries the signatures of the member whose document it serves (R-ANS-013, unchanged).

Acceptance criteria:

- Integration (area `signatures`, `TestR_SIG_031_…`, about 3 s, the Galaxy stub serving a collection signed with key A): through `galaxy` the
  version's signature reads `internal` with A as an anchor after the archive is stored, and `invalid` for an archive whose `FILES.json`
  names another digest; a signature posted to `ansible-local` is served in the version document and listed in the signatures answer.

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

Acceptance criteria: integration (area `bastion`, about 2 s): `-/npm/v1/keys` and an attestation document of `npm-remote` are served by the
Bastion and cached once for two callers.

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

- **The member `signatures` of a stored file and of a version** (the items API, a version's files, a version's summary in the package
  views): `{"state", "stale"}` with `state` one of `verified_public`, `verified_internal`, `untrusted`, `invalid`, `not_checked`,
  `unsupported`, `none`, absent for a folder and while `signatures.enabled` is false; the version list carries it per version for the badge.
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
  admin page.

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

The actor of the job's and the task's events is `system:signatures`; of the Helm anchors `system:configuration`.

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
  anchors: []                       # default; anchors declared by the Helm values (R-SIG-018)
jobs:
  workers:
    signatures: 1                   # default
```

- **The repository member** `signatures` (R-REPO-004): `{"fetch": true}` on a remote repository (default), switched in the closed section
  **Signatures** of its settings page.
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
| Integration | the flows of R-SIG-003 to R-SIG-041 against the stack, with the format stubs that exist, which gain signed samples | about 50 s for the new area `signatures` (about twenty tests of 1 to 4 s), about 2 s more in `ocivirtual` and `bastion` (estimates) | bytes to kilobytes | `test/integration/signatures` |
| Chart | the three alerts | about 1 s | rendered YAML | `test/chart` |
| End to end | one case in `e2e (npm)`: `npm audit signatures` through `npm-remote` | about 5 s, no new leg, no client image | a package of the test upstream | `test/e2e` |

What every leg gains: the unit run under 5 s; the kit milliseconds per format; the integration shard that runs `signatures` about 50 s; the
chart leg about 1 s; `e2e (npm)` about 5 s. No other leg gains anything. With 25 formats the cost is the same plus 25 kit cases of
milliseconds and one small case per format that serves signatures in its own integration test; no leg loads another format's client.

What only a real client shows and is not tested: cosign, Notation, `dotnet nuget verify` and `ansible-galaxy --keyring` against Quader. Each
is measured once by hand when its format's part is built, and the result written into this requirement.

Acceptance criteria: the coverage generator lists every requirement of this page with a test of the tier this table names.

### R-SIG-043 · What stage 1 keeps for stage 2 and stage 3
Priority: **P1** · Covers: quader-only

Nothing of stages 2 and 3 is specified here; stage 1's model already carries:

- **Stage 2, Quader signs** ([Q-SIG-10](#q-sig-10--quader-signs-as-a-registry), [Q-SIG-11](#q-sig-11--quader-issues-attestations),
  [Q-SIG-13](#q-sig-13--the-signing-identity)): the keys endpoint of R-SIG-028 is where Quader's npm key joins (a local repository's
  `{"keys": []}` becomes Quader's key, and the virtual union with it closes H-1176); `artifact_origins` (R-SIG-010), the integrity and trust
  verdicts, the malware verdict and the vulnerability state are the facts of an attestation; an attestation Quader issues is a signature of
  the role `attestation` attached with a new `source`, read by the same model. The signing identity of the Helm values or the UI (sealed by
  the key ring like Debian's keys, Helm winning) has no place in this page's tables.
- **Stage 3, the gate per repository** ([Q-SIG-9](#q-sig-9--display-or-gate)): `signature_trust` holds per signature and scope the state and
  every anchor that knows the signer (`anchor_ids`), so a gate "only artifacts with a verified signature, optionally of chosen anchors" is a
  join of the item's blob to its signatures, their attachments in the repository and the trust row of the addressed repository's scope, at
  the place where the malware gate sits ([malware-scanning.md](malware-scanning.md) R-MAL-013), for local, remote and virtual repositories
  alike; the remote's background fetch (R-SIG-026) is what lets a remote's first pull be judged. What stage 3 will have to add: the answer in
  each client's shape, the wait for a pending check, and the stale rows of H-1175.

Acceptance criteria: none; the stages bring their own.

## Decisions

The owner decided Q-SIG-1 to Q-SIG-16 on 2026-11-21 in his working paper, from the options the development agent put to him; the blocks
keep those options as they were put, translated from the paper's German. Q-SIG-20 onward are the development agent's: each question below
was decided by the development agent from the listed options, and the owner reviews the choices at the walkthrough.

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

**Applied in the spec:** R-SIG-038 (stage 1's display and alarm), R-SIG-043 (what the gate of stage 3 finds).

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

**Choice:** (a). The builder reads which roots the countersignatures carry from a sample of nuget.org packages and records them. **Applied
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

## What this does not cover

The gaps of the mechanisms are on [security/artifact-signatures.md](../security/artifact-signatures.md#what-this-does-not-cover). This page
records the one about a client's result.

### H-1176 · Through a virtual npm repository, `npm audit signatures` reports local packages as missing a registry signature

Component: the keys endpoint of a virtual npm repository (R-SIG-028, Q-SIG-34). Subject: a project whose virtual npm repository lists a local
repository and a remote of a registry with keys (npmjs). Live from stage 1. The union of the members' keys makes npm treat the virtual
repository as a registry with keys, and a package of the local member carries no registry signature before stage 2, so `npm audit signatures`
lists it as missing and exits 1; upstream packages verify. What makes it real: a pipeline that runs `npm audit signatures` against such a
repository. Stage 2 closes it, when Quader signs local packages and its key joins the union (R-SIG-043).
