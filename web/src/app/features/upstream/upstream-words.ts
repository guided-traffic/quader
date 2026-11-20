import { UpstreamRepository, UpstreamRepositoryState } from '../../api/upstream-api';
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

/** The sentence of a system repository's use (R-REPO-070): the image scanning reads its database through `scanner-trivy`. */
export function inUseBadge(r: Pick<UpstreamRepository, 'inUse'>): string {
  return r.inUse
    ? $localize`:@@wellknown.system.in-use:In use by the image scanning`
    : $localize`:@@wellknown.system.not-in-use:Not in use`;
}

/** The levels of the select of a row, each by its name. */
export function levelOptions(): readonly { readonly value: VisibilityLevel; readonly label: string }[] {
  return (['private', 'internal', 'public'] as const).map((l) => ({ value: l, label: levelName(l) }));
}

/** The question before a level becomes Public, in the owner's words (R-UI-187). */
export function publicSentence(): string {
  return $localize`:@@wellknown.public.sentence:Public: anyone on the internet can pull through this installation, on its bandwidth and on its upstream allowance (Docker Hub's pull limit, for example).`;
}
