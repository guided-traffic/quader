import { inject } from '@angular/core';
import { PackageReads } from '../../api/package-reads';
import { PackageVersion } from '../../api/packages-api';
import { setupCommands } from '../../core/project-links';
import { SystemClients } from '../../core/system-clients';
import { CLAMAV_KEY, isSystemRepositoryName, systemClientSentence, upstreamKeyOf, upstreamName } from '../../core/upstream-project';
import { clientsOf } from '../../features/setup/clients';
import { roleFacts } from './ansible-role';
import { CLAMAV_FORMAT } from './clamav-definitions';

/** The qualified name of the repository of ClamAV's definitions (R-REPO-075). */
const CLAMAV_REF = upstreamName(CLAMAV_KEY);

/**
 * What the head of a version's page offers for a client (R-UI-009, R-UI-111, Q-UI-1241):
 *
 * - `menu`: the verb's button with the panel of the clients' commands (`<quader-client-command-menu>`), through the repositories a
 *   client can take the version from, `repository` first;
 * - `clamav`: a version of the ClamAV definitions while ClamAV definition updates is on (R-UI-194): no command per version, since a
 *   ClamAV installation always takes the version in use; **Set up client** of `upstream/clamav` with freshclam, and the note why;
 * - `note`: no button, one sentence why no client can install the version: it is held only by remotes whose rules do not deliver it
 *   (R-CUR-014, Q-UI-1260), only by disabled repositories (Q-UI-965) or only by system repositories of the project `upstream`, which
 *   answer every client 404 (R-UI-193, Q-UI-898, Q-UI-899);
 * - `none`: a format without clients: nothing.
 *
 * No command that would fail is offered.
 */
export type VersionCommand =
  | { readonly kind: 'menu'; readonly repository: string; readonly repositories: readonly string[] }
  | { readonly kind: 'clamav'; readonly setup: readonly string[] }
  | { readonly kind: 'note'; readonly note: string; readonly cause: 'undelivered' | 'disabled' | 'system' }
  | { readonly kind: 'none' };

/**
 * The repositories a client can install the version through: every one the version is available through (not disabled, nor a
 * virtual one that reaches it only through disabled members, Q-UI-965) but a system repository of the project `upstream`, which
 * answers every client 404 (R-UI-193, Q-UI-898); `upstream/clamav` only while its switch is on (`clamavClosed` false, Q-UI-908).
 * A virtual repository that contains the version comes first, then the first local one, then the others in the answer's order.
 */
export function clientRepositories(version: PackageVersion, clamavClosed: boolean): readonly string[] {
  const usable = version.repositories.filter(
    (r) => r.available && (!isSystemRepositoryName(r.key) || (r.key === CLAMAV_REF && !clamavClosed)),
  );
  const first = usable.find((r) => r.class === 'virtual') ?? usable.find((r) => r.class === 'local') ?? usable[0];
  return first ? [first.key, ...usable.filter((r) => r !== first).map((r) => r.key)] : [];
}

/** What the head of the version's page offers a client, by the format, the repositories that hold the version and the ClamAV switch. */
export function versionCommand(format: string, version: PackageVersion, clamavClosed: boolean): VersionCommand {
  if (clientsOf(format).length === 0) return { kind: 'none' };
  const repositories = clientRepositories(version, clamavClosed);
  if (format === CLAMAV_FORMAT && repositories.length > 0) {
    const setup = setupCommands(CLAMAV_REF, true);
    return setup ? { kind: 'clamav', setup } : { kind: 'none' };
  }
  if (repositories.length > 0) return { kind: 'menu', repository: repositories[0]!, repositories };
  // Held by remotes whose rules do not deliver it: no command that would be refused; the delivery note below says why for each
  // (R-CUR-014, Q-UI-1260). Such a remote is not disabled.
  if (version.repositories.some((r) => r.unavailableReason !== undefined)) {
    return {
      kind: 'note',
      cause: 'undelivered',
      note: $localize`:@@pkg.install.undelivered:No repository delivers this version: the note below says why.`,
    };
  }
  const keys = version.repositories
    .filter((r) => !r.available && r.class !== 'virtual' && !isSystemRepositoryName(r.key))
    .map((r) => r.key);
  if (keys.length === 1) {
    return {
      kind: 'note',
      cause: 'disabled',
      note: $localize`:@@pkg.install.disabled-note:The repository ${keys[0]}:repository: is disabled: nobody can install this version until it is enabled again.`,
    };
  }
  if (keys.length > 1) {
    return {
      kind: 'note',
      cause: 'disabled',
      note: $localize`:@@pkg.install.disabled-note-many:The repositories ${keys.join(', ')}:repositories: are disabled: nobody can install this version until one of them is enabled again.`,
    };
  }
  const held = version.repositories[0];
  return held ? { kind: 'note', cause: 'system', note: systemClientSentence(upstreamKeyOf(held.key), 'version') } : { kind: 'none' };
}

/**
 * What a client is offered for one version, the head's rule in one place (R-UI-111, R-UI-269, Q-UI-1634): the command (a menu through
 * the repositories a client can take it from, the note why none can, or ClamAV's Set up client), the name the clients install it by (a
 * role's `role/<id>`, Q-ANS-114, the version's name otherwise), its kind (`metadata.kind`: an OCI artifact's, a NuGet package's) and an
 * image's digest (`metadata.digest`, the panel's pull by digest, R-OCI-041; null for any other kind).
 */
export interface VersionCommandFacts {
  readonly command: VersionCommand;
  readonly name: string;
  readonly kind: string | null;
  readonly digest: string | null;
}

/** The facts of `version`'s command, as the head of its page and the panel of a list's row decide them. */
export function versionCommandFacts(version: PackageVersion, clamavClosed: boolean): VersionCommandFacts {
  const k = version.metadata['kind'];
  const kind = typeof k === 'string' ? k : null;
  const d = version.metadata['digest'];
  return {
    command: versionCommand(version.format, version, clamavClosed),
    name: roleFacts(version)?.packageName || version.name,
    kind,
    digest: kind === 'image' && typeof d === 'string' && d.startsWith('sha256:') ? d : null,
  };
}

/**
 * The reads of a list's rows' commands (R-UI-269, Q-UI-1634): a row of the flat list of packages or of a package's versions knows the
 * version, not whether its holders are disabled, refused by a remote's rules or a system repository, so its panel reads the version's
 * details when it opens and decides with the head's rule. Called in an injection context; it gives one function per version and keeps
 * it, so that a row read again in place (R-UI-213) keeps its panel's answer, and a row whose version changed asks anew.
 */
export function versionCommandReads(): (packageId: string, version: string) => () => Promise<VersionCommandFacts> {
  const reads = inject(PackageReads);
  const clients = inject(SystemClients);
  const made = new Map<string, () => Promise<VersionCommandFacts>>();
  return (packageId, version) => {
    const key = `${packageId}@${version}`;
    let read = made.get(key);
    if (!read) {
      read = async () => {
        const v = await reads.version(packageId, version);
        // ClamAV's definitions answer their clients only while their switch is on (Q-UI-908), as the head's `systemClosed` reads it.
        const clamavClosed = v.format === CLAMAV_FORMAT && !(await clients.served(CLAMAV_REF));
        return versionCommandFacts(v, clamavClosed);
      };
      made.set(key, read);
    }
    return read;
  };
}

/** The note beside the button of a version of the ClamAV definitions (R-UI-194, R-CLAMAV-006). */
export function clamavInstallNote(): string {
  return $localize`:@@clamav.install.note:A ClamAV installation always takes the version in use: point freshclam at Quader once, and it updates by itself.`;
}
