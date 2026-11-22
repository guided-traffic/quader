# Signatures

Quader checks the signatures of every artifact against trust anchors and shows the result at the artifact, and it signs with an identity
of your company's CA. This page covers what an administrator sets: Quader's signing identity, the trust anchors, the switch for project
Owners, blocked signers, the public Sigstore root, and the fetch of a remote's signatures.

What a reader sees at an artifact is in [Signatures](../user/signatures.md) of the user guide.

The settings are **Admin area › Settings › Signatures** (`/ui/<locale>/admin/settings/signatures`). Whoever holds `settings:read`
(`admin`, `security-admin`, `auditor`) reads the page; changes need `security-settings:write` (`admin`, `security-admin`), and the switch
for project Owners and every change of the signing identity ask you to sign in again if your sign-in is not recent.

## Signing identity

The first section shows the identity Quader signs with: one per installation, an ECDSA P-256 key with a code-signing certificate of your
company's CA. Without one Quader signs nothing: packages of local npm repositories carry no registry signature and no artifact gets a
receipt attestation, and the section says so.

**From the Helm values** (`signing.identity.existingSecret`, or `signing.identity.certManager` for a certificate cert-manager issues): the
section shows the identity's facts and no control; change it in the values ([Monitoring and operations](operations.md#quaders-signing-identity)).
While the values name an identity, nothing here can replace it. When a worker refuses the files (an RSA key, a certificate without the
use `codeSigning`, a chain without its root, less than a day left), a red line says why, with the check it failed; the identity in use
keeps signing.

**Uploaded in the admin area**, the way without a Secret:

1. **Create signing key**. The form holds the defaults: the common name `Quader signing` and the installation's address as the URI, the
   identity a verifier names; add the organization and the country (two letters). Organizational unit, locality, state and an e-mail
   address are behind **More options**. Quader makes the key and keeps it sealed with the key ring; it never leaves Quader.
2. The section shows the **Certificate request**. **Copy** or **Download** it (`quader-signing.csr`) and have your company's CA issue a
   code-signing certificate for it. The request stays in the section until you upload the certificate or **Discard** the key, also when
   the CA takes days.
3. **Upload certificate**: paste what the CA issued with its chain, the certificate first, then its intermediates, the root last (or the root
   alone behind **More options**). A certificate that fails a check is refused under the field with the check in plain words: no
   `codeSigning`, no URI or e-mail address among the alternative names, less than 24 hours left, a chain that does not end in a
   self-signed root, or a certificate for another key.

**Import key and certificate**, behind the section's **More options**, takes a key and its chain that a CA handed out together, or a key
moved from another installation: a PEM key (PKCS#8 or SEC 1, encrypted or not, with its passphrase); convert PKCS#12 first with
`openssl pkcs12 -in <file> -nodes`. The key passes through your browser once; the key request keeps it out of any browser.

**The facts** of an identity: its subject and issuer, its alternative names, valid from and until ("expires in 12 days" in amber from 14
days before the end, "expired" in red), the certificate's SHA-256 fingerprint, npm's key ID and the state. **Download chain** and
**Download trusted root** give the public material a verifier needs (`/api/v1/signing/chain.pem`, `/api/v1/signing/trusted-root.json`).

**Replace** an uploaded identity before it expires: it starts a new key request. The current identity keeps signing; the new one is
**Announced** for `signing.identity.announce_period` (10 minutes), so that caches learn its key, and then becomes current. **Remove**
makes the current uploaded identity previous: Quader signs nothing until another one is current.

**What Quader signs with it:** a receipt attestation of every artifact a repository receives (the statement of the user guide's
[Signatures](../user/signatures.md#quaders-receipt-attestation)), made by the worker's job `signing` once the item's checks have their first
verdicts or after `signing.attestations.wait_for_checks` (1 hour), made again when the malware verdict or the identity changes, and removed
with the item. Repositories of the built-in project `upstream` that hold the product's own tool data get none. `signing.attestations.enabled:
false` stops them (made ones stay served), `signing.attestations.fills: false` attests uploads only. Each takes about 2.5 KiB in PostgreSQL;
the first identity after many stored artifacts, and every rotation, makes them all again in the background (minutes per million). The
**Verify** step of every repository's client setup shows the command your users run. One quiet line of the section says what the job has left:
"Waiting to be signed: 3 npm versions and 1,204 receipt attestations, the oldest since 14 minutes ago.", with those that reached their last
attempt (the job tries them again once a day), or "Nothing waits to be signed." beside a current identity.

**Previous identities**, closed, lists the identities that signed before, with their dates. Their npm signatures stay valid. **Retire** one
whose key you believe taken: "Retire this identity? Its key leaves npm's keys at once; Quader signs its packages and attestations again with
the current identity." A retirement cannot be undone ([runbook](runbook.md#a-signing-key-believed-taken)).

## Trust anchors

A signature reads **Verified · public** when an anchor shipped with Quader knows its signer, and **Verified · internal** when an anchor of
this installation (or of the project, below) does. The section **Trust anchors** lists every anchor of the installation with its kind,
where it comes from, the first expiry of its material, the number of signatures it knows and whether it applies.

| From | What you can do |
|---|---|
| **Shipped with Quader**: the public Sigstore root (`sigstore-public`), npm's registry keys (`npmjs`), the Debian and Ubuntu archive keys, nuget.org's repository certificates and their timestamping roots | Switch it off and on. It stays listed |
| **From the Helm values**: `signatures.anchors` of the chart | Nothing here: change the values. They are listed read-only |
| **Added by an administrator** | Switch it off and on, and **Remove** it |

An anchor change reads no artifact again: the trust of the signatures it concerns is recomputed in the background, and the badges follow
within seconds to minutes ("Trust being recomputed" counts what is left). Anchors only add trust; removing or switching one off makes the
signatures only it knew read **Signed · signer not trusted**.

### Adding a trust anchor

**Add trust anchor** opens a form whose first field is the kind. A title (shown at the signatures) and a key (lower-case letters, digits
and hyphens, unique) follow; the key follows the title until you edit it. Anchor material is public data: a private key in any field is
refused.

| Kind | Use it for | Fields |
|---|---|---|
| **CA certificate** | Your company's CA that issues code-signing certificates (Notation, NuGet author signatures, cosign with its own PKI), or the signer's own certificate to pin it | **Certificates (PEM)**: 1 to 16 certificates. Behind **More options**: the use (**Code signing**, the default, or **Timestamping** for the authority whose timestamps prove when a signature was made) and a restriction of the signer's certificate subject (`O`, `OU`, `CN`, `C`, and patterns of its alternative names) |
| **OpenPGP key** | A release key that signs files, Debian indexes or Ansible collections | **Public keys (armored)**: 1 to 32 keys as `gpg --armor --export` writes them |
| **Public key** | A raw key, such as the key of `cosign sign-blob --key` or a registry's npm key | **Public key (PEM)**, with npm's **Key ID** and an **Expires** date where they apply |
| **Sigstore identity** | A CI job's keyless signature, from the public Sigstore or your company's own | **OIDC issuer** (your GitLab's address), the **Identity** exact or as a pattern of the whole identity (for example `^https://gitlab.example.com/platform/.+//\.gitlab-ci\.yml@refs/heads/main$`), and the **Trust root**: the public Sigstore, or your own with its `trusted_root.json` |

cosign 3 with a certificate of your CA (`cosign sign --certificate … --certificate-chain …`) writes, in its default format, a signature
that neither `cosign verify` nor Quader can read (Quader shows **Scheme not supported**); sign with `--new-bundle-format=false`, or use
Notation for your CA's certificates.

The server checks the material on save: a CA certificate must be a CA or a pinned signer, RSA of 2048 bits or more, ECDSA or Ed25519,
not expired; an OpenPGP key must have a signing key and be neither revoked nor expired; a Sigstore pattern must compile and an own trust
root must hold a certificate authority and a log or a timestamp authority. A refusal stands under the field.

Anchors can also be declared in the chart's values, read-only on the page:

```yaml
signatures:
  anchors:
    - key: gitlab-ci-signing           # example
      title: GitLab CI signing CA      # example
      kind: ca
      ca:
        certificatesFrom: { configMapKeyRef: { name: company-ca, key: ca.crt } }   # example
        use: codeSigning
        subject: { O: "Example GmbH" } # example
```

## Project trust anchors

The checkbox **Project owners may add trust anchors for their project** (off by default) lets the Owner of each project add anchors for
it under the project's **Settings › Trust anchors**. They apply only to the repositories of their project; a project cannot switch off an
anchor of the installation. The section lists every project's anchors with **Remove**, which works whatever the switch says. Switching
it off keeps the project anchors stored and stops them applying; the page asks first while projects hold anchors.

## Blocked signers

Quader asks no revocation list. When a signing key or certificate is compromised, **Block signer** in the closed section **Blocked
signers** takes its kind (a certificate's or an OpenPGP key's fingerprint, an issuer and a serial, npm's or a raw key's ID, or a Sigstore
identity with its issuer), the value and a reason. The block acts at once on every signature of that signer in every project: the badge
falls to **Signed · signer not trusted**, and the details say who blocked it, when and why. **Lift block** undoes it. Signatures of
blocked signers stand in the security dashboard's view **Signatures**.

## Public Sigstore root

The closed section **Public Sigstore root** shows where the public Sigstore root and npm's registry keys in use come from: refreshed daily
from the Sigstore update repository, or the snapshot the release ships. Without internet the snapshot holds and ages with the release
(the alert `QuaderSignatureTrustRootStale` fires after 14 days); an operator can set `signatures.trust_root.url` to a mirror of the
company. The last failure of the refresh is shown with its time.

## Signatures of remote repositories

For content a remote repository fetched, Quader asks the upstream for its signatures in the background, so that the badge is right
before any client asks, at most 600 requests an hour per remote. A remote repository's settings have the closed section **Signatures**
with **Fetch the upstream's signatures** (on by default); switch it off for an upstream with tight rate limits, such as Docker Hub without
an account, where fetching a signature counts as a pull. It is never part of the form that creates a repository, and it is also an
administrator's setting of a well-known registry of the project `upstream`.

## Only artifacts with a verified signature

A repository can serve only artifacts with a verified signature: its member `signatures.onlyVerified`, optionally with the anchors it
accepts in `signatures.anchors`, set in the repository's **Settings › Signatures** (see [Signatures](../user/signatures.md#repositories-that-serve-only-verified-artifacts)
of the user guide) or through the API of the repository document. The section shows how many artifacts pass, and asks with the numbers before
a change refuses anything. On this page, removing or switching off an anchor, blocking a signer and switching project anchors off ask with the
number of artifacts the change makes refused in such repositories, and in which, when it is not 0; a project's Owner is asked the same
before removing a project anchor. Releases and refused downloads are in the security dashboard's view **Signatures**
([Security dashboard](../user/security-dashboard.md#signatures)). The operations behind the screens, for scripts:

- **Preview:** `GET /api/v1/projects/<project>/repositories/<repository>/signatures/preview` answers how many of the repository's stored
  artifacts would pass, wait for a check and be refused, per reason, with up to 20 examples; `?anchors=installation/release-key` previews a
  choice of anchors. It reads at most `signatures.gate.preview_limit` items (100,000) or for 10 seconds and says when it stopped early.
- **Impact:** `POST /api/v1/signatures/impact` with `{"anchor": <id>, "change": "remove"}` (or `disable`, `narrow` for a change of its
  keys), `{"block": {"kind": "fingerprint", "value": "<fingerprint>"}}` or `{"projectAnchors": false}` answers how many artifacts of
  repositories that serve only verified artifacts would be refused, and in which repositories. A change of an anchor's keys is counted as
  its removal, an upper bound.
- **Releases:** a holder of the triage right (`admin`, `security-admin`, `auditor`) who reads the repository can release one artifact for
  one repository's check with a reason: `POST /api/v1/projects/<project>/repositories/<repository>/signatures/releases` with
  `{"sha256": "<digest>", "reason": "<why>"}`. The artifact is then served from that repository, and only that one, whatever its signatures
  say, until the release is withdrawn with `DELETE …/releases/<digest>`. A project's Owner cannot release. A release keeps passing the
  artifact after its signer is blocked: review a repository's releases (`GET …/releases`) when you block a signer.
- **Refused downloads:** `GET /api/v1/signatures/refusals` lists the downloads the repositories refused in the last
  `signatures.gate.refusals_retention` (30 days), one entry per repository and artifact with its reason, count and release.

## Operations

The audit log records every anchor created, changed and removed, every change of the switch, every block and its lifting, every
signature found invalid, every refused download and every release granted and withdrawn (`signature.*`). The metrics `quader_signatures_*` and the alerts `QuaderSignatureInvalid`,
`QuaderSignatureBacklog` and `QuaderSignatureTrustRootStale` are described in [Monitoring and operations](operations.md). The
configuration keys under `signatures` are in [Configuration](configuration.md).
