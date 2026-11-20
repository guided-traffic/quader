import { Injectable, inject } from '@angular/core';
import { FormatDescriptor, RepositoriesApi, RepositorySummary } from '../../api/repositories-api';
import { UpstreamApi, UpstreamRepositoryState } from '../../api/upstream-api';
import { SessionStore } from '../../core/session-store';
import { isUpstreamProject, upstreamKeyOf, upstreamName } from '../../core/upstream-project';
import { VisibilityLevel } from '../../shared/ui/visibility-level';
import { MemberState } from '../upstream/upstream-words';

/**
 * A well-known repository a virtual repository may list (R-UI-189, R-REPO-071): `upstream/<key>`, its display name, and what the
 * caller learns of its state and its level. `state` is null while it is active or while the caller cannot tell; `level` is null
 * where the caller cannot read it.
 */
export interface WellKnownCandidate {
  readonly name: string;
  readonly displayName: string;
  readonly state: MemberState | null;
  readonly level: VisibilityLevel | null;
}

/**
 * The well-known repositories of a format for the member editor (Q-UI-832, Q-UI-840). A holder of `repositories:read-config` reads
 * them with their states from `listUpstreamRepositories`, the retired ones included. Whoever may write the member list (the creation
 * form, and the editor of a caller who may change the repository: an Owner or a holder of `repositories:write-config`) reads the
 * format's presets that are not retired with their states and levels (`listFormatUpstreamRepositories`, Q-API-364). A caller who
 * only reads the member list, or one that read refuses, learns them from the format's upstream presets and of each only what the
 * repository list answers: one it reads is active or **Off**, one it does not read has no state it could show.
 */
@Injectable({ providedIn: 'root' })
export class WellKnownMembers {
  private readonly upstream = inject(UpstreamApi);
  private readonly repositories = inject(RepositoriesApi);
  private readonly session = inject(SessionStore);

  /** Every well-known repository of the format the caller learns of; `writes`: the caller may write the member list. */
  async of(
    format: FormatDescriptor | null,
    readable: readonly RepositorySummary[],
    writes: boolean,
  ): Promise<readonly WellKnownCandidate[] | null> {
    // Without the format's descriptor nothing is known, and no member is said to be retired.
    if (!format) return null;
    if (this.session.has('repositories:read-config')) {
      try {
        return (await this.upstream.list())
          .filter((r) => r.kind === 'preset' && r.format === format.name)
          .map((r) => ({ name: r.name, displayName: r.displayName, state: shown(r.state), level: r.visibility }));
      } catch {
        // The other reads below.
      }
    }
    if (writes) {
      try {
        return (await this.upstream.listForFormat(format.name)).map((r) => ({
          name: r.name,
          displayName: r.displayName,
          state: shown(r.state),
          level: r.visibility,
        }));
      } catch {
        // The presets below are what is offered; the state stays unknown.
      }
    }
    const read = new Map(
      readable.filter((s) => isUpstreamProject(s.project) && s.format === format.name).map((s) => [s.key, s] as const),
    );
    return format.upstreamPresets.map((p) => {
      const s = read.get(p.id);
      return {
        name: upstreamName(p.id),
        displayName: p.displayName,
        state: s?.disabled ? 'off' : null,
        level: s?.visibility.artifacts ?? null,
      };
    });
  }

  /** The formats of the catalog, for a screen that has only a repository's format name. */
  async format(name: string): Promise<FormatDescriptor | null> {
    return (await this.repositories.formats()).find((f) => f.name === name) ?? null;
  }
}

/** A state as the editor shows it: none for an active repository (and for a system one, which has no state). */
function shown(state: UpstreamRepositoryState | null): MemberState | null {
  return state === 'active' || state === null ? null : state;
}

/**
 * The state of a member of a virtual repository (R-UI-189): null for a member of the own project and for an active well-known one;
 * a member `upstream/<key>` that is no well-known repository of the format any more is retired. Nothing is said while the
 * candidates are not known (`null`).
 */
export function memberState(name: string, candidates: readonly WellKnownCandidate[] | null): MemberState | null {
  if (upstreamKeyOf(name) === null || candidates === null) return null;
  const c = candidates.find((x) => x.name === name);
  return c ? c.state : 'retired';
}

/**
 * The warning `member_inactive` of a save in words (R-UI-189, R-REPO-071): the member its pointer names (`/virtual/members/1`), and
 * that the virtual repository skips it until an administrator switches it on.
 */
export function memberInactiveText(pointer: string | undefined, members: readonly string[]): string {
  const index = Number(/^\/virtual\/members\/(\d+)$/.exec(pointer ?? '')?.[1] ?? Number.NaN);
  const name = Number.isInteger(index) ? (members[index] ?? null) : null;
  return name
    ? $localize`:@@wellknown.inactive.named:${name}:name: takes no part until an administrator of Quader switches it on.`
    : $localize`:@@wellknown.inactive:A member takes no part until an administrator of Quader switches it on.`;
}
