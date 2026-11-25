import { UpstreamRepository, UpstreamRepositoryState } from '../../api/upstream-api';
import { systemWork } from '../../core/upstream-project';
import { VisibilityLevel, levelName } from '../../shared/ui/visibility-level';

/**
 * The state of a well-known repository as a member of a virtual repository or a row of a list (R-UI-189, R-UI-190, Q-UI-833): the
 * three states of R-REPO-068 in the owner's terms, and `off` where the caller learns only that it is off (a repository it reads that
 * is disabled, without the admin read that tells the two apart, Q-UI-840).
 */
export type MemberState = Exclude<UpstreamRepositoryState, 'active'> | 'off';

/** The badge of a state that is not `active`. */
export function stateBadge(state: MemberState): string {
  switch (state) {
    case 'not-switched-on':
      return $localize`:@@wellknown.state.not-switched-on:Not switched on`;
    case 'switched-off':
      return $localize`:@@wellknown.state.switched-off:Switched off`;
    case 'retired':
      return $localize`:@@wellknown.state.retired:No longer offered`;
    case 'off':
      return $localize`:@@wellknown.state.off:Off`;
  }
}

/** The sentence of a placeholder member (Q-UI-833): why the virtual repository skips it, and what to do about a retired one. */
export function placeholderSentence(state: MemberState): string {
  switch (state) {
    case 'not-switched-on':
    case 'off':
      return $localize`:@@wellknown.placeholder.not-switched-on:Skipped until an administrator of Quader switches it on.`;
    case 'switched-off':
      return $localize`:@@wellknown.placeholder.switched-off:Skipped: an administrator of Quader switched it off.`;
    case 'retired':
      return $localize`:@@wellknown.placeholder.retired:Skipped: Quader no longer offers it. Remove it.`;
  }
}

/** The sentence under an active member whose level is private (R-UI-189). */
export function privateMemberSentence(): string {
  return $localize`:@@wellknown.member.private:Private: the readers of this virtual repository do not read through it unless a global policy names them.`;
}

/** The muted line under a row's level while the repository is private and on (R-UI-187). */
export function privateRowNote(): string {
  return $localize`:@@wellknown.row.private:Only administrators and the principals a global policy names read it; teams' virtual repositories skip it.`;
}

/** The state's word after the switch, or in its place for a reader: **On** or **Off**. */
export function onOffWord(r: Pick<UpstreamRepository, 'state'>): string {
  return r.state === 'active' ? $localize`:@@switch.on:On` : $localize`:@@switch.off:Off`;
}

/** The badge after the word, where the state says more than off: **Switched off**, **No longer offered** (R-UI-187). */
export function rowBadge(r: Pick<UpstreamRepository, 'state'>): string | null {
  return r.state === 'switched-off' || r.state === 'retired' ? stateBadge(r.state) : null;
}

/** The accessible name of a row's switch: "Switch on Docker Hub" (R-UI-187). */
export function switchLabel(r: Pick<UpstreamRepository, 'displayName'>): string {
  return $localize`:@@wellknown.switch.label:Switch on ${r.displayName}:name:`;
}

/** The status line after a switch: "Docker Hub is on." */
export function switchedText(r: Pick<UpstreamRepository, 'displayName' | 'state'>): string {
  return r.state === 'active'
    ? $localize`:@@wellknown.switched.on:${r.displayName}:name: is on.`
    : $localize`:@@wellknown.switched.off:${r.displayName}:name: is off.`;
}

/** The status line after a level was set: "Docker Hub is Internal." */
export function levelSetText(r: Pick<UpstreamRepository, 'displayName' | 'visibility'>): string {
  const level = levelName(r.visibility);
  return $localize`:@@wellknown.level.set:${r.displayName}:name: is ${level}:level:.`;
}

/** The notes under a row's name (R-UI-187): an address of the administrator's, and an account a release removed. */
export function rowNotes(r: UpstreamRepository): string[] {
  const out: string[] = [];
  if (r.address.overridden) out.push($localize`:@@wellknown.note.overridden:Address differs from Quader's`);
  if (r.account.removedByUpdateAt) {
    out.push($localize`:@@wellknown.note.account-removed:The account was removed when Quader's address moved to another host; enter it again.`);
  }
  return out;
}

/**
 * The word of a system repository's use in its row (R-UI-187): the image scanning reads its database through `scanner-trivy`, the
 * malware scan keeps its signature databases in `clamav` (R-REPO-070, R-REPO-075).
 */
export function inUseBadge(r: Pick<UpstreamRepository, 'inUse' | 'key'>): string {
  if (!r.inUse) return $localize`:@@wellknown.system.not-in-use:Not in use`;
  return systemWork(r.key) === 'malware-scan'
    ? $localize`:@@wellknown.system.in-use-malware:In use by the malware scan`
    : $localize`:@@wellknown.system.in-use:In use by the image scanning`;
}

/** The sentence of a system repository's use and its fixed level on its page (R-UI-188). */
export function systemUseSentence(r: Pick<UpstreamRepository, 'inUse' | 'key'>): string {
  if (systemWork(r.key) === 'malware-scan') {
    return r.inUse
      ? $localize`:@@wellknown.system.in-use-text-clamav:In use by the malware scan.`
      : $localize`:@@wellknown.system.not-in-use-text-clamav:Not in use by the malware scan.`;
  }
  return r.inUse
    ? $localize`:@@wellknown.system.in-use-text:In use by the image scanning. Its visibility is always Private.`
    : $localize`:@@wellknown.system.not-in-use-text:Not in use by the image scanning. Its visibility is always Private.`;
}

/**
 * The statement of a system repository's level on the repository's own settings page (R-UI-190): fixed at Private for
 * `scanner-trivy`; for `clamav` Private while **ClamAV definition updates** is off, and the level it serves at while it is on
 * (`level` is the repository's, which is above Private only while the switch is on, R-REPO-075).
 */
export function systemLevelSentence(key: string | null | undefined, level: VisibilityLevel | null = null): string {
  if (systemWork(key) === 'malware-scan') {
    if (level && level !== 'private') {
      const name = levelName(level);
      return $localize`:@@wellknown.system.level-clamav-on:Its visibility is ${name}:level: while ClamAV definition updates is on.`;
    }
    return $localize`:@@wellknown.system.level-clamav:Its visibility is Private while ClamAV definition updates is off.`;
  }
  return $localize`:@@wellknown.system.level:Its visibility is always Private: only Quader's image scanning reads it.`;
}

/** The note of the client setup of a system repository that answers no client (R-UI-190): Quader's own work reads it. */
export function systemSetupSentence(key: string | null | undefined): string {
  return systemWork(key) === 'malware-scan'
    ? $localize`:@@setup.system-clamav:Quader's malware scan keeps its signature database in this repository. An administrator can offer it to your ClamAV installations.`
    : $localize`:@@setup.system:Quader's image scanning reads its database through this repository. Clients cannot use it.`;
}

// ---- ClamAV definition updates (R-REPO-075, R-UI-187, R-UI-188, Q-UI-903) ----

/** The switch's accessible name and the section's heading: **ClamAV definition updates**. */
export function clientsSwitchLabel(): string {
  return $localize`:@@clamav.switch:ClamAV definition updates`;
}

/** The word after the switch of `clamav`, or in its place for a reader: **On** or **Off**. */
export function clientsWord(r: Pick<UpstreamRepository, 'clients'>): string {
  return r.clients?.enabled ? $localize`:@@switch.on:On` : $localize`:@@switch.off:Off`;
}

/** The status line after the switch of `clamav` was written. */
export function clientsSwitchedText(r: Pick<UpstreamRepository, 'clients'>): string {
  return r.clients?.enabled
    ? $localize`:@@clamav.switched.on:ClamAV definition updates is on.`
    : $localize`:@@clamav.switched.off:ClamAV definition updates is off.`;
}

/** The sentence under the switch on the registry's page (R-UI-188). */
export function clientsSentence(): string {
  return $localize`:@@clamav.section.text:Serves these definitions to the ClamAV installations of your company. Quader's own malware scan uses them either way.`;
}

/**
 * What a ClamAV needs at each level, one muted line under each choice of the fold **Visibility** (R-UI-188, Q-REPO-415, Q-UI-905): a
 * token or a client certificate at Internal (R-CLAMAV-010), and no certificate at Private. While no endpoint admits client
 * certificates, Internal's line says that one needs an endpoint the operator sets up.
 */
export function clientsLevelNotes(certificates: boolean): Readonly<Record<VisibilityLevel, string>> {
  const internal = $localize`:@@clamav.level.internal:A ClamAV authenticates with a token in its address, which freshclam writes into its log, or with a client certificate.`;
  return {
    public: $localize`:@@clamav.level.public:Every ClamAV that reaches this address updates, without credentials.`,
    internal: certificates
      ? internal
      : `${internal} ${$localize`:@@clamav.level.internal-no-endpoint:Client certificates need an endpoint your operator sets up.`}`,
    private: $localize`:@@clamav.level.private:Only tokens of members. Client certificates are not admitted.`,
  };
}

/**
 * The warning of an installation that allows no public level (R-REPO-075, `public_not_allowed_lowered`): the switch turned the
 * definitions on at Internal, so every ClamAV needs a token. Shown while the switch is on below Public and Public cannot be chosen.
 */
export function clientsLoweredWarning(): string {
  return $localize`:@@clamav.level.lowered:This installation allows no public level, so the definitions are served at Internal: every ClamAV needs a token in its address.`;
}

/** The state of a declared account's files on the server that answered (R-REPO-073 `account.configuration.files`). */
export type AccountFiles = 'readable' | 'missing' | 'empty' | 'invalid';

/** The state of the files in a word, as the account's facts show it (Q-UI-1162). */
export function accountFilesText(files: AccountFiles): string {
  switch (files) {
    case 'readable':
      return $localize`:@@wellknown.account.files.readable:Readable on this server`;
    case 'missing':
      return $localize`:@@wellknown.account.files.missing:Missing on this server`;
    case 'empty':
      return $localize`:@@wellknown.account.files.empty:Empty on this server`;
    case 'invalid':
      return $localize`:@@wellknown.account.files.invalid:Not usable on this server`;
  }
}

/**
 * The warning of files a request cannot use (Q-UI-1162): what fails and what makes it work again; null for readable files. A Secret
 * reaches the pods within two minutes of its change (R-HA-030). The Secret's name stands in the line above, not here.
 */
export function accountFilesWarning(files: AccountFiles, registry: string, fromSecret: boolean): string | null {
  switch (files) {
    case 'readable':
      return null;
    case 'missing':
      return fromSecret
        ? $localize`:@@wellknown.account.warn.missing-secret:The Secret's files are missing on this server, so every request to ${registry}:registry: fails until the Secret exists with its keys; a Secret reaches the pods within two minutes.`
        : $localize`:@@wellknown.account.warn.missing:The account's files are missing on this server, so every request to ${registry}:registry: fails until they exist.`;
    case 'empty':
      return $localize`:@@wellknown.account.warn.empty:A file of the account is empty on this server, so every request to ${registry}:registry: fails until it holds a value.`;
    case 'invalid':
      return $localize`:@@wellknown.account.warn.invalid:A file of the account cannot be used on this server (it cannot be read, is larger than 8 KiB, holds a line break, or the username holds a colon), so every request to ${registry}:registry: fails until it is corrected.`;
  }
}

/** The note of a registry whose vendor allows more requests with an account, while it has none (Q-UI-1160). */
export function accountRaisesText(registry: string): string {
  return $localize`:@@wellknown.account.raises:${registry}:registry: allows more requests with an account.`;
}

/** The levels of the select of a row, each by its name. */
export function levelOptions(): readonly { readonly value: VisibilityLevel; readonly label: string }[] {
  return (['private', 'internal', 'public'] as const).map((l) => ({ value: l, label: levelName(l) }));
}

/** The question before a level becomes Public, in the owner's words (R-UI-187). */
export function publicSentence(): string {
  return $localize`:@@wellknown.public.sentence:Public: anyone on the internet can pull through this installation, on its bandwidth and on its upstream allowance (Docker Hub's pull limit, for example).`;
}
