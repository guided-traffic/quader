# Connecting a package client

How to point npm, pnpm, Yarn, the .NET CLI, nuget.exe, Visual Studio, the Docker CLI, Podman, APT, ansible-galaxy and curl at a Quader repository, sign them in with a token from an environment variable, and install, publish and maintain packages and files through it. The page explains the concepts and the place in the UI that gives you the exact configuration of your installation: the page Set up client of a repository.

## Where the instructions are

Every repository has a page **Set up client**: the configuration and the commands for your client, and below them the guide that explains them, with the addresses of the endpoint you are using, so you can copy what it shows.

| How to open it | What you get |
|---|---|
| **Set up client** at the top of a repository's page (an empty repository has it there too, and says under its list what fills it), or in its row of a list of repositories | The page of that repository |
| **Set up client** in the **Install** panel of a package | The page of the repository chosen in the panel, with the client chosen there |
| **Guides** in the side bar or the user menu | The package types as tiles; a tile lists the repositories of that type you may use, each with **Set up client**, and the guide of each kind of repository for when you have none yet |

Nothing on the page changes anything on the server, except **Create a token**.

## Repository addresses

Every repository belongs to a project, and has one client address, built from the configured address of the endpoint you use, the key of its project and its own key:

| Package type | Address | Example |
|---|---|---|
| npm | `<base URL>/api/npm/<project key>/<repository key>/` | `https://quader.example.com/api/npm/web/npm-all/` |
| NuGet | `<base URL>/api/nuget/<project key>/<repository key>/v3/index.json` (the service index) | `https://quader.example.com/api/nuget/web/nuget-all/v3/index.json` |
| Generic | `<base URL>/files/<project key>/<repository key>/` | `https://quader.example.com/files/web/generic-local/` |
| OCI (Docker) | `<host>/<project key>/<repository key>` as the start of every image reference, without a scheme | `quader.example.com/web/docker/team/app:1.0` |
| Debian (APT) | `<base URL>/api/deb/<project key>/<repository key>/`, the URI of the source, with the suite named beside it | `https://quader.example.com/api/deb/web/deb-local/` |
| Ansible Galaxy | `<base URL>/api/ansible/<project key>/<repository key>/`, the `url` of the server in `ansible.cfg` | `https://quader.example.com/api/ansible/web/ansible-local/` |

The trailing slash belongs to the address of npm, Debian, Ansible Galaxy and generic repositories; a NuGet address ends in `index.json`; a container image reference names the host and no scheme ([Container images with Docker and Podman](container-images.md)). Two projects can each have a repository of the same key, so the project key is always part of the address; a repository that an administrator moves to another project gets a new address, and the old one answers `404`. Which class of repository to use:

- **Virtual** repositories are the recommended address: internal and public packages through one address and one credential. A publish through a virtual repository goes to its deployment target, a local member.
- **Local** repositories hold what your teams publish and serve nothing else.
- **Remote** repositories cache an upstream registry and are read-only.

[Repositories](repositories.md) explains the classes. If the page **Set up client** answers that the service is unavailable, the installation has no configured base URL for the endpoint you use; tell an administrator.

## Use the page Set up client

The page belongs to one repository: its address is `…/projects/<project>/repositories/<repository>/setup`, and its header names the repository, the package type and the class, with the address your client uses and its copy button. It asks for nothing the repository already decides. From top to bottom:

1. **Client**, only where the package type has several: **npm**, **pnpm**, **Yarn Berry (2 and later)** or **Yarn Classic (1.x)** for npm, **.NET CLI (dotnet)**, **nuget.exe** or **Visual Studio 2022** for NuGet, **Docker CLI (with Buildx)** or **Podman** for OCI (Docker). Debian (APT), Ansible Galaxy and generic repositories have one client each, and the page does not ask.
2. **Token**, one framed row: the snippets read your token from the variable `QUADER_TOKEN`. **Create a token** beside it creates one without leaving the page (next section). **More options**, closed, inside the row: a token written into the snippets, and, where a Bastion can be chosen, **Install from**. Signed out, one line says that the snippets hold no credentials, which works for a repository whose artifacts are public.
3. The steps **1 Configure**, **2 Install**, if you may publish **3 Publish**, and while the installation signs its receipts **Verify** last (see [Signatures](signatures.md#quaders-receipt-attestation)), each with its snippets. A configuration file is shown as an editor window: its name on the tab, its lines numbered, **Download** and **Copy snippet** in its bar. A command is shown as a terminal window with its prompt and **Copy snippet**. What you replace with your own value (`<package>`, an example such as `myapp:1.0`) is tinted and underlined with a dashed line; where your token goes is an orange chip. A line too long for the page continues on the next line, indented, so a long repository key never hides the end of an address; the copy button copies the snippet unchanged, without prompts or line numbers. A sentence above a window says only what the window does not, such as where to save the file. A second way of the same step, such as the image pull secret for a Kubernetes cluster or the same command in PowerShell, is a closed fold after the step's own snippets.
4. **Guide** below the steps (see [What the guide on the page adds](#what-the-guide-on-the-page-adds)).

Where the window is wide enough, a small picture beside the title shows the flow: your machine, the token, and the repository.

A repository that serves only artifacts with a verified signature says so above the snippets, with the trust anchors it accepts, to people
who may read its settings ([Signatures](signatures.md#repositories-that-serve-only-verified-artifacts)).

**Publish** shows how to publish through this repository, once for your client: `npm publish --registry <address>`, `pnpm publish --registry <address>`, `yarn publish --registry <address>` for Yarn Classic, `npmPublishRegistry: "<address>"` in `.yarnrc.yml` for Yarn Berry, `dotnet nuget push` and `nuget push` with the service index of the repository as the source and the token as the API key for NuGet (Visual Studio has no push), `docker push`, `docker buildx build --push` and `podman push` of an image tagged with the repository's address for OCI, or the `curl --upload-file` of a `.deb` to `upload/<file>?distribution=<suite>` for Debian. For a virtual repository the publish reaches its default deployment target. There is no **Publish** step when you cannot publish: for a remote repository, for a Bastion, and when you do not hold the deploy permission on the target; the guide then leaves publishing out too. For a virtual repository without a default deployment target, people who may change the repository see the step with the reason and a link to its settings.

### The token

**Create a token** creates a personal token with the description `Client setup: <client>, <repository>, <date>` that may read this repository. Where you may publish, tick **It may also publish** first to let it publish to the repository that receives your uploads. It lives 30 days unless an administrator configured another period, and it appears under your [personal tokens](access-tokens.md), where you revoke it. Creating it can ask you to [sign in again](troubleshooting.md#session-ended-and-re-authentication).

The page shows the token once, in a field with a copy button, and the command that puts it into the variable:

```shell
read -rs QUADER_TOKEN && export QUADER_TOKEN
```

Run it in bash or zsh, paste the token and press Enter: the shell keeps the value without showing it and without writing it into its history. In a CI job, store the token as a masked variable named `QUADER_TOKEN` instead. The snippets keep reading the variable, so the files you save hold no secret and can be committed. Choosing another client, or leaving the page, discards the token from the page.

For a virtual repository the token also reads the packages of its members when they are read through the virtual repository, so `npm install` against it works. It reads nothing more than you do: a member you cannot read stays invisible to the token, and the token cannot read a member addressed directly. The members of a virtual repository may change after you created the token; the token follows them.

### A token in the snippets

Under **More options**, **Token in the snippets** has two ways:

| Option | What the snippets contain | Notes |
|---|---|---|
| **Read it from the variable QUADER_TOKEN** (default) | `${QUADER_TOKEN}`, which the client expands when it runs (`%QUADER_TOKEN%` in a `nuget.config` and in cmd, `$env:QUADER_TOKEN` in PowerShell) | No secret on the page; the file can be committed |
| **Write a token into the snippets** | The token you paste, for example the one you created above | The value stays in the browser and is never sent to the server |

A pasted token is checked in the browser for its prefix, its length of 57 characters, its alphabet and its checksum; the page says which check failed, and a value that fails is not written into the snippets. Until one passes, the snippets show `{{token}}` where it will go. A file that holds a token belongs in your home directory, never in version control.

## Guides without a repository

**Guides** shows the package types as tiles. A tile lists the repositories of that type you may use, virtual ones first and marked **recommended**, each with its class, its description and **Set up client**. Below the list, **Without a repository** has the guide of each kind of repository (local, remote, virtual) of the type: the same page, whose addresses show `<project>` and `<repository>` where the key of your project and of your repository go. Use it to read about a kind of repository before you have one; it creates no token.

## Configure an npm client

| Client | File | Credential setting |
|---|---|---|
| npm | `.npmrc` in the project, or `~/.npmrc` for every project | `//<host>/api/npm/<project>/<key>/:_authToken=${QUADER_TOKEN}` |
| pnpm | `~/.npmrc` (pnpm 11 and 12 ignore a credential that names a variable in a project file) | as npm |
| Yarn Berry | `.yarnrc.yml` in the project (Yarn Berry does not read `.npmrc`) | `npmAuthToken: "${QUADER_TOKEN}"` |
| Yarn Classic | `.npmrc` in the project | as npm |

The npm credential line names the registry address without its scheme, so the client sends the token to this repository and to no other URL. When the repository is not public, Yarn Berry needs `npmAlwaysAuth: true` and Yarn Classic `always-auth=true`, or they send the token for scoped packages only; the snippets add the switch when it is needed. An `.npmrc` for npm looks like this:

```ini
# example: the virtual repository npm-all of the project web
registry=https://quader.example.com/api/npm/web/npm-all/
//quader.example.com/api/npm/web/npm-all/:_authToken=${QUADER_TOKEN}
```

and a `.yarnrc.yml` for Yarn Berry:

```yaml
npmRegistryServer: "https://quader.example.com/api/npm/web/npm-all/"   # example
npmAlwaysAuth: true            # only when the repository is not public
npmAuthToken: "${QUADER_TOKEN}"
```

To take only one scope from Quader and everything else from the client's default registry, replace `registry=` by `@your-scope:registry=<address>` and keep the credential line (Yarn Berry: an `npmScopes` entry); the guide shows this variant for every client.

### Sign the client in

Put a [personal access token](access-tokens.md) into the environment variable before the client runs, without typing it on a command line your shell keeps in its history:

```shell
read -rs QUADER_TOKEN && export QUADER_TOKEN
npm whoami
```

`npm whoami`, `pnpm whoami` and `yarn npm whoami` print your Quader user name (`sa:<name>` for a service-account token, `sa:<project>/<name>` for one of a project); Yarn Classic has no such command. In CI, store a service-account token with only the permissions the job needs as a masked, protected variable named `QUADER_TOKEN`; for GitLab CI without a stored token, see [GitLab CI pipelines](gitlab-ci-pipelines.md). The variable has this name unless an administrator configured another; the snippets always show the name in use. `npm login` and its pnpm and Yarn equivalents do not work with Quader: put the token into the configuration instead.

## Configure a NuGet client

NuGet clients take the address of the **service index**, the `v3/index.json` address of the repository, as a package source. Quader serves NuGet v3 only; a v2 address is answered with `404` and the message names the service index to use.

| Client | How to configure it | Credential |
|---|---|---|
| .NET CLI (`dotnet`) | `dotnet nuget add source <service index> --name quader …` writes the user-level `NuGet.Config`; a `nuget.config` next to the solution applies to that solution | `--username quader --password "${QUADER_TOKEN}" --store-password-in-clear-text`, or `%QUADER_TOKEN%` in the file |
| nuget.exe | `nuget sources add -Name quader -Source <service index> …`, or the `nuget.config` | `-Username quader -Password %QUADER_TOKEN%` (PowerShell: `$env:QUADER_TOKEN`); on Mono, Linux and macOS add `-StorePasswordInClearText` |
| Visual Studio | The `nuget.config` next to the solution file, or Tools › NuGet Package Manager › Package Manager Settings › Package Sources | `%QUADER_TOKEN%` in the file, read from the environment Visual Studio started in; or the credential prompt (any user name, the token as the password) |

The credential parts appear when the repository is not public. The user name is not checked; the snippets use `quader`. On Linux and macOS NuGet cannot encrypt a stored password, so the command-line form needs `--store-password-in-clear-text`, and the token your shell expands is then written into the user-level file; the `nuget.config` form keeps the reference `%QUADER_TOKEN%`, which NuGet expands from the environment on every operating system, and can be committed. A `nuget.config` for a virtual repository looks like this:

```xml
<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
    <add key="quader" value="https://quader.example.com/api/nuget/web/nuget-all/v3/index.json" protocolVersion="3" />
  </packageSources>
  <packageSourceCredentials>   <!-- only when the repository is not public -->
    <quader>
      <add key="Username" value="quader" />
      <add key="ClearTextPassword" value="%QUADER_TOKEN%" />
    </quader>
  </packageSourceCredentials>
</configuration>
```

`<clear />` removes every other source, nuget.org among them, so every package resolves through Quader and a package of the same ID on a public source cannot slip in; put a remote repository for nuget.org into the virtual repository to keep public packages. Where several sources are unavoidable, say with package source mapping which source may serve which ID prefix; the guide shows the element. Sign the client in as for npm: put a [personal access token](access-tokens.md) into `QUADER_TOKEN` before the client runs.

## Restore, push and maintain NuGet packages

| Task | .NET CLI | nuget.exe |
|---|---|---|
| Add a package | `dotnet add package <id> --version <version>` | `nuget install <id> -Version <version> -Source quader` |
| Restore | `dotnet restore` (`--no-http-cache` after a push) | `nuget restore` |
| Restore from the lock file | `dotnet restore --locked-mode` | `nuget restore -LockedMode` |
| Push | `dotnet nuget push <file>.nupkg --source <service index> --api-key "${QUADER_TOKEN}"` | `nuget push <file>.nupkg -Source <service index> -ApiKey %QUADER_TOKEN%` |
| Unlist or delete | `dotnet nuget delete <id> <version> --source <service index> --api-key "${QUADER_TOKEN}" --non-interactive` | `nuget delete <id> <version> -Source <service index> -ApiKey %QUADER_TOKEN%` |

Visual Studio adds packages through Manage NuGet Packages, with the source chosen at the top right, or through a `PackageReference` in the project file; it does not push. What each needs and how the classes behave:

- **Permissions.** Restoring needs read, pushing and relisting deploy, unlisting and deleting delete. The token goes in as the API key for push, unlist, relist and delete, and as the password for everything else. Through a virtual repository you also need read on the members you restore from.
- **Local repositories** accept an ID and version once: pushing the same version with other bytes answers `409` `nuget_version_exists`, and pushing the same file again succeeds without a change, which is what makes a push safe to repeat. A version that was deleted for good is blocked for ever (`409` `nuget_version_deleted`).
- **Unlisting** is what `dotnet nuget delete` does by default: search no longer offers the version, but it stays downloadable and a project that names it keeps restoring. Whether the command deletes for good is the repository's `deleteMode`, which its settings page shows. NuGet clients have no command to list a version again; the guide shows the request.
- **Remote repositories** take no writes (`405`). They serve and cache what their upstream serves; the version lists and registrations are refreshed after the metadata period, five minutes unless the repository sets another.
- **Virtual repositories** send push, unlist, relist and delete to their deployment target; without one they answer `405` `no_deploy_target`.
- **IDs.** The first push of an ID to a local repository records its owners and, by default, claims it: virtual repositories that contain the local repository then never take a public package of that ID from their remote members. The first push of an ID that a remote member already serves needs the manage permission (`403` `shadows_remote_package` otherwise).
- **Not available in this version:** NuGet v2 addresses, and the symbol server.

A failed `dotnet nuget push` or `nuget push` prints the status and the reason phrase only; the code of the answer is in the body that `curl` shows, and the guide's troubleshooting table lists each code with its status. The guide of your client and repository names the exact limits and client messages (`NU1301`, `NU1101`, `NU1102`).

On the package page a NuGet package shows its ID in the casing of its first publish, its dependencies by target framework on the **Details** tab of a version, and the banner **Unlisted.** for a version its publisher unlisted: that version is missing from search and is still downloaded when a project asks for exactly it.

## Install, publish and maintain npm packages

| Task | npm | pnpm | Yarn Berry | Yarn Classic |
|---|---|---|---|---|
| Install | `npm install <package>` | `pnpm add <package>` | `yarn add <package>` | `yarn add <package>` |
| Install from the lockfile | `npm ci` | `pnpm install --frozen-lockfile` | `yarn install --immutable` | `yarn install --frozen-lockfile` |
| Publish | `npm publish` | `pnpm publish` | `yarn npm publish` | `yarn publish --new-version <version>` |
| Set a dist-tag | `npm dist-tag add <name>@<version> <tag>` | `pnpm dist-tag add <name>@<version> <tag>` | `yarn npm tag add <name>@<version> <tag>` | `yarn tag add <name>@<version> <tag>` |
| Unpublish a version | `npm unpublish <name>@<version>` | `pnpm unpublish <name>@<version> --force` | none | none |
| Security audit | `npm audit` | `pnpm audit` (pnpm 11 and later) | `yarn npm audit` | not through Quader |

What each needs and how the classes behave:

- **Permissions.** Installing needs read, publishing and changing dist-tags deploy, unpublishing delete. Through a virtual repository you also need read on the members you install from.
- **Local repositories** accept a name and version once: publishing it again answers `409` `npm_version_exists`, and an unpublished version number is blocked for good (`409` `npm_version_unpublished`). By default, a publish of a version lower than `latest` stores it and leaves `latest` where it is; move the tag yourself. `latest` cannot be removed, only moved.
- **Remote repositories** take no writes (`405` `npm_class_not_writable`). A package document is refreshed from the upstream after its metadata period, two hours unless the repository sets another.
- **Virtual repositories** send publish, dist-tag changes and unpublish to their deployment target; without one they answer `405` `no_deploy_target`. pnpm 11 and 12 cannot unpublish through a virtual repository; unpublish at the deployment target.
- **Package names.** The first publish of a name to a local repository records its owners and, by default, claims the name: virtual repositories that contain the local repository then never take a public package of that name from their remote members. [Errors and troubleshooting](troubleshooting.md#typical-npm-problems) explains the refusals this can cause.
- **Security audit** is forwarded to an advisory provider by remote repositories and by virtual repositories with a forwarding remote member, with only names that are not internal. A local repository answers `501` `npm_audit_unavailable`.
- **Signatures.** `npm audit signatures` checks the registry signatures and the provenance of the packages you install: a remote repository serves its upstream's keys and attestations. When your administrator has set up a signing identity, Quader signs every version of a local repository a moment after its publish, a local repository serves Quader's keys, and a virtual repository serves its remote members' keys together with Quader's, so `npm audit signatures` verifies your own packages and the public ones in one run. Without a signing identity a local repository serves no keys: against a local repository alone the command ends with `found no dependencies to audit that were installed from a supported registry` and exit code 1, and through a virtual repository with a remote member your own packages are reported as missing a signature. npm first asks the public Sigstore service for a registry's keys, so the machine that runs the command needs to reach it.
- **Not available in this version:** `npm deprecate` (it fails with `E404` and changes nothing) and `npm publish --stage` (`501`).

The guide of your client and repository names the exact limits, notices and client quirks.

## Lockfiles

- **npm** and **Yarn Classic** record the address and integrity of every tarball. The addresses lie under the repository the install went through, and an install from the lockfile fetches them directly, whatever the configuration says now. To move an npm lockfile to another repository that serves the same versions, replace the old address by the new one in it; the integrity values stay valid.
- **pnpm** checks every tarball against the integrity in `pnpm-lock.yaml`.
- **Yarn Berry** writes no registry addresses into `yarn.lock`, so pointing `npmRegistryServer` elsewhere needs no change to the lockfile.
- **NuGet** records the resolved version and the content hash of every package in `packages.lock.json` (switched on with `RestorePackagesWithLockFile`), and no source address, so a lock file moves to another repository that serves the same bytes; `dotnet restore --locked-mode` restores exactly what it names. NuGet v3 announces no digest of its own, so the hash in the lock file is your check against a changed upstream.

The integrity and hash values stay valid because a version's bytes never change in a local repository, and a remote npm repository stores a tarball only when it matches the integrity its upstream announces. By default, a version the upstream withdrew answers `404` through remote and virtual repositories, also for an install from a lockfile. `EINTEGRITY` usually means the lockfile was written against a registry that served other bytes for the version.

## Sign in container clients

For an OCI (Docker) repository the **Configure** step holds the login of the Docker CLI or Podman, which reads the token from `QUADER_TOKEN` on standard input, and, for the Docker CLI client, behind **For a Kubernetes cluster**, a `kubectl` command that creates a Kubernetes image pull secret from the same variable. Without a package named, the pull and the push name the example image `myapp:1.0`, which you replace with yours. **Install** pulls an image by its tag, **Publish** tags and pushes one, and for the Docker CLI builds and pushes a multi-platform image with Buildx. [Container images with Docker and Podman](container-images.md) explains the login, the permissions, multi-platform images, virtual repositories and Kubernetes.

## Configure APT

For a Debian (APT) repository the **Configure** step installs the repository's public key from `signing-key.asc` as `/etc/apt/keyrings/quader-<project>-<repository>.asc`, writes the source as `/etc/apt/sources.list.d/quader-<project>-<repository>.sources` with that key in `Signed-By`, and writes the token into `/etc/apt/auth.conf.d/quader-<project>-<repository>.conf`, a file only root can read, never into the source line. A one-line `.list` form, and for a remote repository without upstream keys a source that names the archive's own keyring instead of a key of Quader's, are closed folds of the step. **Install** runs `apt-get update` and `apt-get install`, **Publish** uploads a package with `curl`. [Debian packages with APT](apt-packages.md) explains the suites, the upload, the index wait, key rotation and the client's error messages.

## Download and upload files with curl

The configure snippet for a generic repository defines a shell function, `quader_curl`, that hands curl the token on standard input, so the token appears neither on curl's command line nor in a shell trace:

```shell
quader_curl() {
  curl --silent --show-error --config - "$@" <<EOF
header = "Authorization: Bearer ${QUADER_TOKEN}"
EOF
}
quader_curl --fail --remote-name "https://quader.example.com/files/web/generic-local/tools/app-1.2.0.tar.gz"
quader_curl --fail --upload-file app-1.2.0.tar.gz "https://quader.example.com/files/web/generic-local/tools/app-1.2.0.tar.gz"
```

Downloading needs read, uploading to a free path deploy, replacing a file with other bytes overwrite as well, deleting delete. Add `--header "If-None-Match: *"` to an upload that must never replace a file: it then answers `412` when the path exists. [Browsing and transferring files](files.md) describes paths, digests and folders; the curl guide shows resuming, conditional downloads and uploads with a digest.


## Update ClamAV from Quader

While an administrator has switched **ClamAV definition updates** on, `upstream/clamav` serves the signature databases Quader's own
malware scan uses, and its **Set up client** page shows the guide of `freshclam`. Four ways, depending on the repository's level:

- **Public:** one line in `freshclam.conf`, `PrivateMirror https://quader.example.com/api/clamav/upstream/clamav` *(example)*.
- **A token:** below Public, a token of a service account as the password of the address,
  `PrivateMirror https://clamav:${QUADER_TOKEN}@quader.example.com/api/clamav/upstream/clamav`. `freshclam` writes the whole address,
  the token with it, into its log.
- **A client certificate** (`freshclam` 1.2 or later), at Internal and Public, on an endpoint your operator set up for it: the line names
  that endpoint, and the certificate, its key and the authority of the endpoint go into the environment of `freshclam`, for the systemd
  unit in a drop-in with `FRESHCLAM_CLIENT_CERT`, `FRESHCLAM_CLIENT_KEY` and `CURL_CA_BUNDLE`. The guide shows this way only while such an
  endpoint reads the repository. A missing, expired or foreign certificate ends in a curl error such as `SSL certificate problem` or
  `alert certificate required`.
- **Without freshclam:** a script downloads `main.cvd`, `daily.cvd` and `bytecode.cvd` with `curl` from fixed addresses, which always
  serve the version in use, and reloads `clamd`; with a token, curl reads the header from standard input; with a certificate, it names
  `--cert`, `--key` and `--cacert`.

## Install through a Bastion

An administrator may run a Bastion for you: a read-only cache of this installation at its own address, for clients outside the network ([The Quader Bastion](../admin/bastion.md) explains what it is). You sign in to it with the same personal, service-account or CI token, and you install the same packages and pull the same images from the same repositories, named by the same project and repository keys, for every format; only the address differs.

When an administrator has registered a Bastion and it has reported its address, the page **Set up client** offers it under **More options**, **Install from**, next to **This installation**. Choose it and the snippets and the address in the header use the Bastion's address, for example `https://packages.example.com/api/npm/web/npm-all/`. Without that permission the choice is not shown; an administrator can send you a link to the page that already names the Bastion (`…/setup?bastion=<name>`).

- **Read only.** The page has no **Publish** step for a Bastion, and its banner says why. Publish, push images, change dist-tags and unpublish on this installation.
- **npm** gets one more line in the configure snippet, `replace-registry-host=always`. It makes npm fetch every package from the address in `registry=`, also when a lockfile written against another host names that host, so one lockfile works through this installation and through the Bastion.
- **Images.** For an OCI repository the snippets sign in to the Bastion's host and name the images on it, for example `docker login packages.example.com/web/docker-local` and `docker pull packages.example.com/web/docker-local/myapp:1.0`; Podman's are the same with `podman`. The Kubernetes snippet creates an image pull secret for the same prefix, so a pod whose image names it pulls through the Bastion. An image keeps its digest, so a reference by digest works at both addresses.
- **Debian, Ansible Galaxy and the ClamAV definitions.** The APT source, the download of its key and the line of the credential file name the Bastion's address; `ansible-galaxy` gets it as its server, freshclam as `PrivateMirror`.
- **The guide** describes this installation, so the page shows none for a Bastion; choose **This installation** to read it.
- **A Bastion answers `409` on the page** (`bastion_base_url_unknown`) while it has not reported an address yet or the address is not listed in `server.additional_base_urls` of this installation; tell your administrator.

A Bastion decides what you may read by asking this installation with your token, so permissions, revocations and exposure follow the main installation. A revoked token can keep reading cached content for a short time, and an installation that the Bastion cannot reach is served from its cache for a limited period only.

## What the guide on the page adds

This manual page explains the concepts; the guide below the steps of **Set up client** gives the exact text for your repository and client. Every section is a closed row under the heading **Guide**; open the one you need:

- what a repository of its class is, the first of its sections;
- the addresses of the endpoint you are using, the repository's project and key, and its deployment target where you may publish there;
- sections that depend on the class and the client, and only the ones that apply to you: an anonymous reader gets no sign-in steps, and a reader who may not publish gets no publishing;
- the section **On this endpoint**, shown only on an endpoint with exposure rules, which says what such an endpoint hides and refuses;
- a **Troubleshooting** table with the error codes of the protocol and what to do about each.

Every code block of the guide has a copy button.

When something goes wrong, start with [Errors and troubleshooting](troubleshooting.md).

## Configure ansible-galaxy

For an Ansible Galaxy repository the **Configure** step renders an `ansible.cfg` with the repository as the server `quader`, the token as the environment variable `ANSIBLE_GALAXY_SERVER_QUADER_TOKEN` (also for PowerShell and cmd), and a `~/.netrc` line that roles need. The **Install** step renders `ansible-galaxy collection install` and a `requirements.yml`, and **Publish** renders `ansible-galaxy collection publish`. See [Ansible collections and roles with ansible-galaxy](ansible-collections.md).
