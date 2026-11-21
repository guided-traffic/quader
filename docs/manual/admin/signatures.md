# Signatures

Quader checks the signatures of every artifact against trust anchors and shows the result at the artifact. This page covers what an
administrator sets: the trust anchors, the switch for project Owners, blocked signers, the public Sigstore root, and the fetch of a
remote's signatures.

What a reader sees at an artifact is in [Signatures](../user/signatures.md) of the user guide.

The settings are **Admin area › Settings › Signatures** (`/ui/<locale>/admin/settings/signatures`). Whoever holds `settings:read`
(`admin`, `security-admin`, `auditor`) reads the page; changes need `security-settings:write` (`admin`, `security-admin`), and the switch
for project Owners asks you to sign in again if your sign-in is not recent.

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

## Operations

The audit log records every anchor created, changed and removed, every change of the switch, every block and its lifting, and every
signature found invalid (`signature.*`). The metrics `quader_signatures_*` and the alerts `QuaderSignatureInvalid`,
`QuaderSignatureBacklog` and `QuaderSignatureTrustRootStale` are described in [Monitoring and operations](operations.md). The
configuration keys under `signatures` are in [Configuration](configuration.md).
