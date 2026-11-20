/** A package-manager client the setup dialog and the install panel offer for a format. */
export interface ClientChoice {
  /** The value of the `client` parameter of R-API-030. */
  readonly id: string;
  /** A product name; it is not translated. */
  readonly label: string;
}

/**
 * The clients per format (R-UI-016, R-UI-009). The server owns the snippets and rejects a client
 * it does not know; this table only decides what the pickers offer. A format without an entry has
 * no setup screen yet and is not offered. NuGet offers the .NET CLI, nuget.exe
 * and Visual Studio (R-UI-018, Q-UI-93). OCI offers the Docker CLI and Podman (R-OCI-030); a Kubernetes cluster
 * signs in with the image pull secret of the Docker CLI's configure section. Debian offers APT: apt and apt-get read
 * one source format, so they are one client (R-DEB-029). Ansible Galaxy offers ansible-galaxy (R-ANS-025).
 */
export const FORMAT_CLIENTS: Readonly<Record<string, readonly ClientChoice[]>> = {
  generic: [{ id: 'curl', label: 'curl' }],
  npm: [
    { id: 'npm', label: 'npm' },
    { id: 'pnpm', label: 'pnpm' },
    { id: 'yarn', label: 'Yarn Berry (2 and later)' },
    { id: 'yarn-classic', label: 'Yarn Classic (1.x)' },
  ],
  nuget: [
    { id: 'dotnet', label: '.NET CLI (dotnet)' },
    { id: 'nuget', label: 'NuGet CLI (nuget.exe)' },
    { id: 'visual-studio', label: 'Visual Studio' },
  ],
  oci: [
    { id: 'docker', label: 'Docker CLI (with Buildx)' },
    { id: 'podman', label: 'Podman' },
  ],
  deb: [{ id: 'apt', label: 'APT (apt, apt-get)' }],
  ansible: [{ id: 'ansible-galaxy', label: 'Ansible Galaxy (ansible-galaxy)' }],
};

export function clientsOf(format: string): readonly ClientChoice[] {
  return FORMAT_CLIENTS[format] ?? [];
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
    case 'curl.install':
    case 'apt.install':
    case 'ansible-galaxy.install.collection':
    case 'dotnet.publish.shell':
    case 'nuget.publish.cmd':
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
    case 'dotnet.configure.shell':
      return text($localize`:@@setup.caption.dotnet-source:Adds the source to your user configuration. On Linux and macOS the token is stored there in clear text.`);
    case 'nuget.configure.cmd':
      return text($localize`:@@setup.caption.nuget-source:Adds the source to your user configuration.`);
    case 'dotnet.configure.powershell':
    case 'nuget.configure.powershell':
    case 'dotnet.publish.powershell':
    case 'nuget.publish.powershell':
      return text(null, inPowerShell);
    case 'dotnet.configure.nugetconfig':
    case 'nuget.configure.nugetconfig':
      return text($localize`:@@setup.caption.nuget-config:Save next to your solution. Every package then comes through this repository.`, $localize`:@@setup.fold.nuget-config:As a nuget.config file`);
    case 'visual-studio.configure.nugetconfig':
      return text($localize`:@@setup.caption.nuget-config:Save next to your solution. Every package then comes through this repository.`);
    case 'visual-studio.install.packagereference':
      return text($localize`:@@setup.caption.vs-packagereference:Add to the project file inside an \`ItemGroup\`, then restore.`);
    case 'curl.configure.function':
      return text($localize`:@@setup.caption.curl-function:Paste into the shell or the CI script that runs curl. The token stays off curl's command line.`);
    case 'curl.publish':
      return text($localize`:@@setup.caption.curl-publish:Replacing a file also needs the overwrite permission.`);
    case 'docker.configure.shell':
    case 'podman.configure.shell':
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
    case 'ansible-galaxy.publish.collection':
      return text($localize`:@@setup.caption.ansible-publish:Publishes an archive built with \`ansible-galaxy collection build\`.`);
    default:
      return null;
  }
}
