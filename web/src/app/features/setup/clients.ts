/** A package-manager client the setup dialog and the install panel offer for a format. */
export interface ClientChoice {
  /** The value of the `client` parameter of R-API-030. */
  readonly id: string;
  /** A product name; it is not translated, the words beside it are ("Docker CLI (with Buildx)"). */
  readonly label: string;
  /**
   * The client's short name, the tab of the panel of a version's commands (Q-UI-1240): "Docker", "Yarn Berry", ".NET CLI", so that four
   * of npm's tabs and **Set up client** fit a panel at 390 px. A product name, not translated.
   */
  readonly tab: string;
}

/**
 * The clients per format (R-UI-016, R-UI-009). The server owns the snippets and rejects a client
 * it does not know; this table only decides what the pickers offer. A format without an entry has
 * no setup screen yet and is not offered. NuGet offers the .NET CLI, nuget.exe
 * and Visual Studio (R-UI-018, Q-UI-93). OCI offers the Docker CLI and Podman (R-OCI-030), and Helm for the Helm charts it holds
 * (R-OCI-042; a chart's commands offer Helm alone, an image's never: `clientsOfKind`); a Kubernetes cluster signs in with the image pull
 * secret of the Docker CLI's configure section. Debian offers APT: apt and apt-get read
 * one source format, so they are one client (R-DEB-029). Ansible Galaxy offers ansible-galaxy (R-ANS-025). A generic repository
 * offers curl, wget and PowerShell's Invoke-WebRequest, the clients a file is downloaded and uploaded with (R-GENERIC-017).
 */
export const FORMAT_CLIENTS: Readonly<Record<string, readonly ClientChoice[]>> = {
  generic: [
    { id: 'curl', label: 'curl', tab: 'curl' },
    { id: 'wget', label: 'wget', tab: 'wget' },
    { id: 'powershell', label: 'PowerShell (Invoke-WebRequest)', tab: 'PowerShell' },
  ],
  npm: [
    { id: 'npm', label: 'npm', tab: 'npm' },
    { id: 'pnpm', label: 'pnpm', tab: 'pnpm' },
    { id: 'yarn', label: $localize`:@@setup.client.yarn-berry:Yarn Berry (2 and later)`, tab: 'Yarn Berry' },
    { id: 'yarn-classic', label: 'Yarn Classic (1.x)', tab: 'Yarn Classic' },
  ],
  nuget: [
    { id: 'dotnet', label: '.NET CLI (dotnet)', tab: '.NET CLI' },
    { id: 'nuget', label: 'NuGet CLI (nuget.exe)', tab: 'NuGet CLI' },
    { id: 'visual-studio', label: 'Visual Studio', tab: 'Visual Studio' },
  ],
  oci: [
    { id: 'docker', label: $localize`:@@setup.client.docker:Docker CLI (with Buildx)`, tab: 'Docker' },
    { id: 'podman', label: 'Podman', tab: 'Podman' },
    { id: 'helm', label: 'Helm', tab: 'Helm' },
  ],
  deb: [{ id: 'apt', label: 'APT (apt, apt-get)', tab: 'APT' }],
  ansible: [{ id: 'ansible-galaxy', label: 'Ansible Galaxy (ansible-galaxy)', tab: 'ansible-galaxy' }],
  clamav: [{ id: 'freshclam', label: 'freshclam', tab: 'freshclam' }],
};

export function clientsOf(format: string): readonly ClientChoice[] {
  return FORMAT_CLIENTS[format] ?? [];
}

/**
 * The clients of one artifact of a format (formats/oci.md R-OCI-042, Q-UI-1250): an OCI Helm chart is pulled and installed by Helm
 * alone, and an image (or an artifact whose kind is not known) by every client but Helm. Every other format's clients are its own.
 */
export function clientsOfKind(format: string, kind: string | null): readonly ClientChoice[] {
  const all = clientsOf(format);
  if (format !== 'oci') return all;
  return kind === 'helm-chart' ? all.filter((c) => c.id === 'helm') : all.filter((c) => c.id !== 'helm');
}

/** Who reads the page, as far as the words of a snippet depend on it (R-UI-123). */
export interface SnippetReader {
  /** Signed out, on a repository whose artifacts are public: the snippets hold no credential. */
  readonly anonymous: boolean;
}

/**
 * What the page says about a snippet (R-UI-121, Q-UI-804): at most one short sentence of what its window does not say already
 * (the window names the file and shows the command; a name of a file, a setting or a command stands between backticks and is shown
 * as code), and, for a second way of the same step, the heading of the inline fold it
 * sits behind (the image pull secret of a Kubernetes cluster, the same command in PowerShell). `signedIn` is a snippet that only
 * makes sense with a credential: an anonymous visitor does not see it.
 */
export interface SnippetText {
  readonly caption: string | null;
  readonly fold: string | null;
  readonly signedIn: boolean;
}

const text = (caption: string | null, fold: string | null = null, signedIn = false): SnippetText => ({ caption, fold, signedIn });

/**
 * The words of a snippet, by its stable id (R-API-030: the server sends ids, the text is the UI's and is translated, R-UI-045).
 * `null` is an id the page does not know: it shows the snippet without a sentence, and `clients.spec.ts` fails for every id the
 * API renders that is not here.
 */
export function snippetText(id: string, reader: SnippetReader = { anonymous: false }): SnippetText | null {
  const inPowerShell = $localize`:@@setup.fold.powershell:In PowerShell`;
  const pushArgument = $localize`:@@setup.caption.nuget-push-argument:The API key is only an argument, which the process list shows: push where nobody else is logged in.`;
  switch (id) {
    case 'npm.configure.npmrc':
    case 'yarn-classic.configure.npmrc':
      return text($localize`:@@setup.caption.npmrc:Save as \`.npmrc\` in your project.`);
    case 'pnpm.configure.npmrc':
      return text($localize`:@@setup.caption.npmrc-home:Save in your home directory: pnpm reads a token variable only there.`);
    case 'npm.configure.npmrc-scoped':
    case 'pnpm.configure.npmrc-scoped':
    case 'yarn-classic.configure.npmrc-scoped':
    case 'yarn.configure.yarnrc-scoped':
      return text(
        $localize`:@@setup.caption.scoped:Takes only the packages of one scope from this repository.`,
        $localize`:@@setup.fold.scoped:For one scope only`,
      );
    case 'npm.configure.cli':
      return text(
        $localize`:@@setup.caption.npm-cli:Writes the settings into \`~/.npmrc\`, where your shell stores the token.`,
        $localize`:@@setup.fold.npm-cli:For every project`,
      );
    case 'yarn.configure.yarnrc':
      return text($localize`:@@setup.caption.yarnrc:Save as \`.yarnrc.yml\` in your project.`);
    case 'npm.install':
    case 'pnpm.install':
    case 'yarn.install':
    case 'yarn-classic.install':
    case 'dotnet.install':
    case 'nuget.install':
    case 'docker.install':
    case 'podman.install':
    case 'helm.install':
    case 'curl.install':
    case 'apt.install':
    case 'ansible-galaxy.install.collection':
    case 'ansible-galaxy.install.role':
      return text(null);
    case 'npm.publish':
    case 'pnpm.publish':
    case 'yarn-classic.publish':
      return text($localize`:@@setup.caption.publish-dir:Run in the directory of the package.`);
    case 'yarn.publish':
      return text($localize`:@@setup.caption.publish-berry:Add to \`.yarnrc.yml\`, then run \`yarn npm publish\`.`);
    case 'npm.publish.gitlab-ci.generic':
      return text($localize`:@@setup.snippet.gitlab-generic:For npm 10, pnpm 10 on npm 10 and Yarn 1. A pipeline step exchanges the job's ID token for a short-lived Quader token and revokes it again as its last step.`);
    case 'npm.publish.gitlab-ci.native':
      return text($localize`:@@setup.snippet.gitlab-native:For npm 11.5.1 and later, pnpm 11 and 12 and Yarn 4. The client exchanges the job's ID token itself.`);
    // dotnet and nuget.exe take a source's password and a push's API key only as an argument, which other users of the machine see in
    // the process list while the command runs: the page says so (Q-UI-1336); the nuget.config keeps the password in the variable.
    case 'dotnet.configure.shell':
    case 'nuget.configure.cmd':
      return text($localize`:@@setup.caption.nuget-source-argument:The client takes the token only as an argument, which the process list shows; nuget.config keeps it in the variable.`);
    case 'dotnet.publish.shell':
    case 'nuget.publish.cmd':
      return text(pushArgument);
    case 'dotnet.configure.powershell':
    case 'nuget.configure.powershell':
      return text(null, inPowerShell);
    case 'dotnet.publish.powershell':
    case 'nuget.publish.powershell':
      return text(pushArgument, inPowerShell);
    case 'dotnet.configure.nugetconfig':
    case 'nuget.configure.nugetconfig':
      return text($localize`:@@setup.caption.nuget-config:Save next to your solution. Every package then comes through this repository.`, $localize`:@@setup.fold.nuget-config:As a nuget.config file`);
    case 'visual-studio.configure.nugetconfig':
      return text($localize`:@@setup.caption.nuget-config:Save next to your solution. Every package then comes through this repository.`);
    case 'visual-studio.install.packagereference':
      return text($localize`:@@setup.caption.vs-packagereference:Add to the project file inside an \`ItemGroup\`, then restore.`);
    case 'curl.configure.function':
      return text($localize`:@@setup.caption.curl-function:Paste into the shell or the CI script that runs curl. The token stays off curl's command line.`);
    case 'wget.configure.function':
      return text($localize`:@@setup.caption.wget-function:Paste into the shell or the CI script that runs wget. The token stays off wget's command line.`);
    case 'powershell.configure.headers':
      return text($localize`:@@setup.caption.powershell-headers:Run in the PowerShell session or script that downloads. The token stays in the session, off every command line.`);
    case 'curl.install.checksum':
    case 'wget.install.checksum':
    case 'powershell.install.checksum':
      return text($localize`:@@setup.caption.file-checksum:Checks the download against the SHA-256 on the file's page.`);
    case 'wget.install':
    case 'powershell.install':
      return text(null);
    case 'curl.publish':
    case 'wget.publish':
    case 'powershell.publish':
      return text($localize`:@@setup.caption.curl-publish:Replacing a file also needs the overwrite permission.`);
    case 'docker.configure.shell':
    case 'podman.configure.shell':
    case 'helm.configure.shell':
      // A pull of a public repository needs no credential: the page does not ask a visitor to sign in for it.
      return text(
        reader.anonymous
          ? $localize`:@@setup.caption.oci-login-anonymous:Not needed to pull: this repository can be read without signing in.`
          : $localize`:@@setup.caption.oci-login:Sign in once on each machine.`,
      );
    case 'docker.configure.kubernetes':
      return text(
        $localize`:@@setup.caption.oci-pull-secret:Creates an image pull secret. Name it under \`imagePullSecrets\` of your pods.`,
        $localize`:@@setup.fold.kubernetes:For a Kubernetes cluster`,
        true,
      );
    case 'docker.publish':
    case 'podman.publish':
      return text($localize`:@@setup.caption.oci-push:Name a local image below this repository, then push it.`);
    case 'helm.publish':
      return text($localize`:@@setup.caption.helm-push:Package the chart, then push it below this repository.`);
    case 'docker.publish.buildx':
      return text($localize`:@@setup.caption.oci-buildx:Or build for two platforms and push in one go.`);
    case 'apt.configure.shell':
    case 'apt.configure.shell-scoped':
      return text($localize`:@@setup.caption.apt-configure:Installs the signing key and adds the repository as a verified source. Run it again after a key rotation.`);
    case 'apt.configure.shell.configure':
    case 'apt.configure.shell.configure-scoped':
      return text(
        $localize`:@@setup.caption.apt-upstream:Verifies against the archive's own keyring instead of a key of Quader's.`,
        $localize`:@@setup.fold.apt-upstream:Remote without upstream keys`,
      );
    case 'apt.configure.list':
    case 'apt.configure.list-scoped':
      return text($localize`:@@setup.caption.apt-list:Install the key file as above first.`, $localize`:@@setup.fold.apt-list:As a one-line .list file`);
    case 'apt.publish.curl':
      return text($localize`:@@setup.caption.apt-publish:The package is in the index within moments.`);
    case 'ansible-galaxy.configure.ansible-cfg':
      return text($localize`:@@setup.caption.ansible-cfg:Save next to your playbooks, or as \`~/.ansible.cfg\`.`);
    case 'ansible-galaxy.configure.token-env':
      return text(reader.anonymous ? null : $localize`:@@setup.caption.ansible-token-env:\`ansible.cfg\` does not expand variables, so the token comes from this one.`);
    case 'ansible-galaxy.configure.token-env-powershell':
      return text(null, inPowerShell, true);
    case 'ansible-galaxy.configure.token-env-cmd':
      return text(null, $localize`:@@setup.fold.cmd:In cmd`, true);
    case 'ansible-galaxy.configure.netrc':
      return text(
        $localize`:@@setup.caption.ansible-netrc:A role download falls back to \`~/.netrc\`. One line serves every repository on this host.`,
        $localize`:@@setup.fold.roles:To install roles`,
        true,
      );
    case 'ansible-galaxy.install.requirements':
      return text(
        $localize`:@@setup.caption.ansible-requirements:Install it with \`ansible-galaxy collection install -r requirements.yml\`.`,
        $localize`:@@setup.fold.requirements:With a requirements.yml`,
      );
    case 'freshclam.configure.conf':
      return text($localize`:@@setup.caption.freshclam-conf:Put this line into \`freshclam.conf\` of each ClamAV installation. \`PrivateMirror\` replaces ClamAV's own network.`);
    case 'freshclam.install':
      return text($localize`:@@setup.caption.freshclam-run:Run it once: it fetches each database whole, then reports each as up to date.`);
    case 'ansible-galaxy.install.role-requirements':
      // A role's requirements.yml (R-ANS-056, Q-ANS-114).
      return text(
        $localize`:@@setup.caption.ansible-role-requirements:Install it with \`ansible-galaxy role install -r requirements.yml\`.`,
        $localize`:@@setup.fold.requirements:With a requirements.yml`,
      );
    case 'ansible-galaxy.publish.collection':
      return text($localize`:@@setup.caption.ansible-publish-server:Publishes an archive built with \`ansible-galaxy collection build\` to the server of \`ansible.cfg\`, with the token in its variable.`);
    case 'npm.verify.signatures':
      return text($localize`:@@setup.caption.verify-npm-signatures:Run it in your project: checks the registry signatures of every installed package, this installation's of your own packages included.`);
    default:
      // The tab Verify of every client of a format Quader attests (signatures.md R-SIG-056): one sentence for all of them.
      if (id.endsWith('.verify.attestation')) {
        return text($localize`:@@setup.caption.verify-attestation:Checks with cosign, offline, that this installation received the artifact and what its checks found. Replace the marked values.`);
      }
      return null;
  }
}
