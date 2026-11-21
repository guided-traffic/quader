# Signatures

Quader finds the signatures of the artifacts it stores, reads who signed them, checks that each signature matches the content and
whether a trust anchor of the installation or of your project knows the signer, and shows the result at the artifact with a badge and
its details.

Clients get the signatures the way their tools expect them: `cosign` and `notation` from an image's referrers and tags, npm from its keys
and attestations endpoints, NuGet inside the package, `ansible-galaxy` in the version document.

A signature tells who signed an artifact and whether a trust anchor knows the signer. It does not tell that the content is safe: a trusted
signer can sign malicious content. The malware scan and the vulnerability scanning check that.

## The badge

A signed version carries a badge beside its number in the package's list of versions and in the header of its page; a signed file carries
it in its facts and in the version's **Files** tab. The badge stands for what proves where the artifact comes from: a publisher's
signature or a provenance attestation.

| Badge | Means |
|---|---|
| **Verified · public** (green) | The signature matches the content, and a public trust anchor shipped with Quader knows the signer: the public Sigstore, npm's registry keys, the archive keys of Debian and Ubuntu, nuget.org's certificates |
| **Verified · internal** (green) | The signature matches the content, and a trust anchor your administrator or your project's Owner added knows the signer: your company's CA, your company's Sigstore, a release key |
| **Signed · signer not trusted** (neutral) | The artifact is signed, but no trust anchor knows the signer, or no key of the signer is known yet, or an administrator blocked the signer |
| **Invalid signature** (red) | The only signatures do not match the content: the content changed after it was signed, or the signature names other content |

Three states carry no badge; the details name them: **Not checked yet** (the check runs in the background, usually within seconds),
**Scheme not supported** (a scheme Quader does not read, or a signature it could not read) and **No signature**, with the signatures the
format knows. An invalid signature beside a valid one does not change the badge; it is listed in the details and stands in the security
dashboard.

## The details

The tab **Signatures** of a version, and the section **Signatures** of a file's page, list every signature:

- the kind in plain words: "Publisher signature · Sigstore, keyless", "Provenance attestation", or "Origin confirmed by registry" for a
  registry's own signature (npm's registry signature, nuget.org's countersignature), which never carries the badge;
- the signer, its identity and the issuer; for a CI job its source, ref and run;
- when it was signed and what proves it: "proven by the transparency log", "proven by a timestamp", or "received by Quader" when nothing
  proves the time;
- the certificate's validity ("valid for 10 minutes at signing; covered by the log entry", "expired on 30 Apr 2027"); a certificate that
  expired after signing is information, not a verdict;
- the trust anchor that knows the signer and where it comes from, or the date and reason of an administrator's block;
- **Download original**, the signature as it arrived; **Technical details** fold out its key identifiers, chain, log entries,
  timestamps and where it is attached.

Above the signatures stands where the artifact came from: **Uploaded by** a user, a service account, or "CI job <project> · <ref> ·
pipeline <id>" with whether the ref is protected, or **Fetched from** the remote's upstream with the time. Every text a signature carries
is shown as text.

A version held by several repositories lists each of them on its own: a signature pushed to one repository says nothing about the same
bytes in another. Through a virtual repository the details name the member each signature is attached in.

### Per format

| Format | What you see |
|---|---|
| Container images (OCI) | The image's Notation and Sigstore signatures and cosign's older `.sig` signatures, for an index under each platform; apart from them, **Attestations and SBOMs** in a table with their type, platform and size. They are never versions or tags in the image's list |
| npm | The provenance attestation as the badge's evidence; npm's registry signature as "Origin confirmed by registry" |
| NuGet | The author signature as the badge's evidence; nuget.org's repository countersignature as "Origin confirmed by registry" |
| Debian | No badge: apt checks the signature of the repository's index, not of the package. The details list the releases whose signed index names the package, with their trust |
| Ansible Galaxy | A signature a CI job posted for a local version, or the upstream's signatures of a remote version |
| Generic | A file beside the file is its signature: `tool.tar.gz.asc`, `.sig`, `.sigstore.json`, `.jws.sig`, `.cose.sig`. The signed file carries the badge; the signature file stays a file to download, and its page says **Signature of** the file it belongs to |

## Repositories that serve only verified artifacts

A repository can be set to serve only artifacts with a verified signature. A download of anything else is refused, and the client says why
in its own words (see [Troubleshooting](troubleshooting.md)). Where you look in Quader:

- **The version's page** shows a banner: "Downloads are refused in `<project>/<repository>`: it serves only artifacts with a verified
  signature, and `<version>` has no signature of its publisher or its build." (or what else is missing). The **Files** tab says **Refused
  here** with the reason where the badge stands, and offers no **Download** for that file. The tab **Signatures** says which trust anchors the
  repository accepts and what it decides; an image whose index passes says of each platform that it is signed through its index.
- **The file's page** says **Refused here** with the reason under **Signature**, and its section **Signatures** says why in full.
- A version that is still being checked shows nothing more than **Not checked yet**: the client waits a little and then gets a "try again".

Only people who triage security findings (an administrator, a security administrator, an auditor) can release one artifact for one
repository, with a reason: **Release for this repository** on the banner, in the section or in the security dashboard. A released artifact
says so, by whom, when and why, and the release can be withdrawn. A project's Owner cannot release, because a release loosens the check.

**Setting it.** The repository's **Settings › Signatures** (closed, never part of creating a repository) holds **Only artifacts with a
verified signature can be downloaded from this repository**. Checked, choose the **Accepted trust anchors**: **Any trust anchor of this
repository** (which includes the public Sigstore, where anyone can sign) or **These trust anchors**, ticked from the anchors that apply to the
repository. Under them the section says how many of the repository's artifacts pass now. Before a change refuses anything, Quader asks with
the numbers per reason and up to 20 examples. A project's Owner can switch it on and narrow it; switching it off, accepting any anchor
instead of a list, or adding an anchor needs an administrator or someone who triages security findings. A Debian repository has no such
setting: apt checks the signature of the repository's index, not of a package. The client setup of a repository with the check on says so
above the snippets, with the accepted anchors.

## Quader's receipt attestation

While your installation has a signing identity, Quader signs a receipt of every artifact your repositories receive: an attestation that
this installation received the bytes of that digest, when, from which uploader or upstream (with a pipeline's claims for a CI upload), what
the signatures attached to it said, and what the malware scan and the vulnerability evaluation found, each with its date. It says nothing
about who made the content: that is the publisher's signature above. It is made once the checks have their first verdicts (or after an hour
with what there is), and again when the malware verdict changes.

To check it, open **Set up client** of the repository: the last step, **Verify**, shows the one command for your client. For an image it is
`cosign verify-attestation` against the image by its digest; for every other artifact the command downloads the file's attestation from
`/api/v1/projects/<project>/repositories/<repository>/attestation?path=<path>` as `<file>.quader.sigstore.json` and runs `cosign
verify-blob-attestation` with it. Both read the installation's trusted root (`/api/v1/signing/trusted-root.json`, public) and need no
internet:

```shell
# example: an image of the local repository docker-local of the project web
curl -fsSo quader-trusted-root.json https://quader.example.com/api/v1/signing/trusted-root.json
cosign verify-attestation \
  --trusted-root quader-trusted-root.json \
  --certificate-identity https://quader.example.com \
  --certificate-oidc-issuer-regexp '.*' \
  --insecure-ignore-tlog --insecure-ignore-sct \
  --type https://github.com/guided-traffic/quader/blob/main/docs/spec/signatures.md#receipt-attestation-v1 \
  quader.example.com/web/docker-local/app@sha256:<digest>
```

The two `--insecure-ignore-…` flags are needed because the attestation has no transparency-log entry and no timestamp; the time it names is
Quader's own claim. An image of a local repository carries the attestation among its referrers, unless the image already has a cosign
signature in the older layout (a tag `sha256-<hex>.sig` or `.att`), which the attestation would hide from cosign; such an image, and an image
of a remote repository, is verified with the downloaded attestation (`cosign verify-blob-attestation --bundle … --digest <hex>
--digestAlg sha256`). What the receipt must say (a `clean` malware state, an upload from a protected branch) is your policy: cosign's
`--policy` or your cluster's policy engine reads the predicate.

## Trust anchors of your project

While your administrator allows it, the Owner of a project adds trust anchors for the project under **Settings › Trust anchors**: a CA
certificate, an OpenPGP key, a public key or a Sigstore identity (see [Signatures](../admin/signatures.md#adding-a-trust-anchor) of the
administrator guide for each kind). They apply only to the repositories of the project and to what its virtual repositories serve; they
only add, and a project cannot switch off an anchor of the installation. The section lists the installation's anchors below the
project's ("Also trusted in this project"), so you see the whole scope; members read the list. While project anchors are switched off,
the section is not shown, and a project that holds anchors shows them to its Owner as "Not applied".

## In the security dashboard

The security dashboard's third view, **Signatures**, lists the downloads that repositories refused because of a signature and their
releases, then every invalid signature and every signature of a blocked signer, newest first, with the artifact it covers, the signer as it
claims, the reason, when it was found and the repositories that hold it. See [Security dashboard](security-dashboard.md).
