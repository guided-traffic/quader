import { CallerKind, LimitPlace, LimitWho, UsageSubject, UsageSubjectKind } from '../../api/limits-api';

/*
 * How a caller is named on the limit and usage screens (R-UI-179's first column, R-UI-183's Caller): the same picture, first and
 * second line for a rule's "who", a usage subject and a block's subject, so that a caller looks the same on every screen.
 */

/** What a caller's cell shows: an avatar (a user) or the kind's icon, the name, the second line. */
export interface CallerView {
  /** `avatar` for a user; otherwise the Material icon's ligature. */
  readonly icon: 'avatar' | string;
  /** The seed of the avatar. */
  readonly seed?: string;
  readonly primary: string;
  readonly secondary: string | null;
  /** An address: shown in the monospace type. */
  readonly mono: boolean;
  /** A "who" that no longer exists. */
  readonly deleted: boolean;
  /** Keys and names are not translated. */
  readonly translate: boolean;
}

/** "Every signed-in user", "Every technical token", … (R-UI-179). */
export function everyLabel(kind: CallerKind): string {
  switch (kind) {
    case 'user':
      return $localize`:@@limits.every.user:Every signed-in user`;
    case 'technicalToken':
      return $localize`:@@limits.every.technical-token:Every technical token`;
    case 'ci':
      return $localize`:@@limits.every.ci:Every CI job`;
    case 'anonymous':
      return $localize`:@@limits.every.anonymous:Every anonymous caller`;
    case 'administrator':
      return $localize`:@@limits.every.administrator:Every administrator`;
  }
}

/**
 * A kind of caller as it stands in a sentence after "for" (Q-UI-1219): "every signed-in user", "alle angemeldeten Benutzer". The label of
 * a list ("Every signed-in user") lowered to a phrase lowered German nouns too ("alle angemeldeten benutzer").
 */
export function everyPhrase(kind: CallerKind): string {
  switch (kind) {
    case 'user':
      return $localize`:@@usage.who.every.user:every signed-in user`;
    case 'technicalToken':
      return $localize`:@@usage.who.every.technical-token:every technical token`;
    case 'ci':
      return $localize`:@@usage.who.every.ci:every CI job`;
    case 'anonymous':
      return $localize`:@@usage.who.every.anonymous:every anonymous caller`;
    case 'administrator':
      return $localize`:@@usage.who.every.administrator:every administrator`;
  }
}

/** The kind of a usage subject as a word ("User", "Technical token", …). */
export function subjectKindLabel(kind: UsageSubjectKind): string {
  switch (kind) {
    case 'user':
      return $localize`:@@usage.kind.user:User`;
    case 'technicalToken':
      return $localize`:@@usage.kind.technical-token:Technical token`;
    case 'ci':
      return $localize`:@@usage.kind.ci:CI project`;
    case 'certificate':
      return $localize`:@@usage.kind.certificate:Client certificate`;
    case 'anonymous':
      return $localize`:@@usage.kind.anonymous:Anonymous address`;
    case 'system':
      return $localize`:@@usage.kind.system:System`;
  }
}

/** The kinds as the filter names them, plural (R-UI-183). */
export function subjectKindFilterLabel(kind: UsageSubjectKind): string {
  switch (kind) {
    case 'user':
      return $localize`:@@usage.filter.kind.user:Users`;
    case 'technicalToken':
      return $localize`:@@usage.filter.kind.technical-token:Technical tokens`;
    case 'ci':
      return $localize`:@@usage.filter.kind.ci:CI`;
    case 'certificate':
      return $localize`:@@usage.filter.kind.certificate:Client certificates`;
    case 'anonymous':
      return $localize`:@@usage.filter.kind.anonymous:Anonymous addresses`;
    case 'system':
      return $localize`:@@usage.filter.kind.system:System`;
  }
}

const deletedSuffix = (): string => $localize`:@@limits.who.deleted:(deleted)`;

/** The picture and lines of a rule's "who" (R-UI-179). */
export function whoView(who: LimitWho): CallerView {
  const base = { mono: false, deleted: false, translate: false };
  switch (who.kind) {
    case 'user':
      return {
        ...base,
        icon: 'avatar',
        seed: who.user,
        primary: who.displayName || who.user,
        secondary: who.deleted ? deletedSuffix() : `@${who.user}`,
        deleted: !!who.deleted,
      };
    case 'group':
      return {
        ...base,
        icon: 'groups',
        primary: who.displayName || who.group,
        secondary: who.deleted ? deletedSuffix() : $localize`:@@limits.who.group:Group`,
        deleted: !!who.deleted,
      };
    case 'technicalToken':
      return {
        ...base,
        icon: 'key',
        primary: who.name || who.technicalToken,
        secondary: who.deleted ? deletedSuffix() : (who.account ?? null),
        deleted: !!who.deleted,
      };
    case 'ciProject':
      return {
        ...base,
        icon: 'rocket_launch',
        primary: who.projectPath || `${who.issuer}:${who.projectId}`,
        secondary: who.deleted ? deletedSuffix() : who.issuer,
        deleted: !!who.deleted,
      };
    case 'address':
      return { ...base, icon: 'lan', primary: who.prefix, secondary: $localize`:@@limits.who.address:Address or range`, mono: true };
    case 'every':
      return {
        ...base,
        icon: everyIcon(who.callerKind),
        primary: everyLabel(who.callerKind),
        secondary: $localize`:@@limits.who.kind:Kind of caller`,
        translate: true,
      };
  }
}

function everyIcon(kind: CallerKind): string {
  switch (kind) {
    case 'user':
      return 'person';
    case 'technicalToken':
      return 'key';
    case 'ci':
      return 'rocket_launch';
    case 'anonymous':
      return 'public';
    case 'administrator':
      return 'admin_panel_settings';
  }
}

/** The picture and lines of a usage subject (R-UI-183's Caller). */
export function subjectView(subject: UsageSubject): CallerView {
  const base = { mono: false, deleted: false, translate: false };
  switch (subject.kind) {
    case 'user':
      return {
        ...base,
        icon: 'avatar',
        seed: subject.name,
        primary: subject.displayName || subject.name,
        secondary: subject.deleted ? deletedSuffix() : `@${subject.name}`,
        deleted: subject.deleted,
      };
    case 'technicalToken':
      return {
        ...base,
        icon: 'key',
        primary: subject.name,
        secondary: subject.deleted ? deletedSuffix() : subject.account,
        deleted: subject.deleted,
      };
    case 'ci':
      return {
        ...base,
        icon: 'rocket_launch',
        primary: subject.projectPath || `${subject.issuer}:${subject.projectId}`,
        secondary: subject.issuer,
      };
    case 'anonymous':
      return { ...base, icon: 'public', primary: subject.address, secondary: $localize`:@@usage.kind.anonymous:Anonymous address`, mono: true };
    case 'certificate':
      // A ClamAV installation admitted by its client certificate (R-CLAMAV-010): the certificate's subject, not translated.
      return { ...base, icon: 'badge', primary: subject.name, secondary: $localize`:@@usage.kind.certificate:Client certificate`, mono: true };
    case 'system':
      return {
        ...base,
        icon: 'settings',
        primary: $localize`:@@usage.kind.system:System`,
        secondary: $localize`:@@usage.kind.system-hint:Mirror passes and other background work`,
        translate: true,
      };
  }
}

/**
 * The title of a caller's page (R-UI-184, Q-UI-765): the thing, not its reference. A user's display name ("Priya Raman"), a
 * technical token's name, a CI project's path, an address; the page's breadcrumb ends with the same words.
 */
export function subjectTitle(subject: UsageSubject): string {
  return subjectView(subject).primary;
}

/**
 * The reference under a caller's title (R-UI-184, Q-UI-765): a user's `@name`, a CI project's issuer, the background work of
 * System; null where the line holds it otherwise (a technical token's account is a link there) or the title is the reference
 * (an address, a certificate's subject: the kind's badge says "Client certificate" already, and the page lists the certificates by
 * serial under By credential).
 */
export function subjectReference(subject: UsageSubject): string | null {
  switch (subject.kind) {
    case 'user':
    case 'ci':
    case 'system':
      return subjectView(subject).secondary;
    case 'technicalToken':
      return subject.deleted ? deletedSuffix() : null;
    case 'certificate':
    case 'anonymous':
      return null;
  }
}

/** A place as a sentence part: "at the instance", "in project web", "at web/npm-remote". */
export function placeText(place: LimitPlace): string {
  switch (place.kind) {
    case 'instance':
      return $localize`:@@limits.place.instance:at the instance`;
    case 'project':
      return $localize`:@@limits.place.project:in project ${place.project}:project:`;
    case 'repository':
      return $localize`:@@limits.place.repository:at ${place.repository}:repository:`;
    case 'bastion':
      return $localize`:@@limits.place.bastion:at Bastion ${place.bastion}:bastion:`;
  }
}

/** A place as a cell names it: "Instance", "Project web", "web/npm-remote". */
export function placeLabel(place: LimitPlace): string {
  switch (place.kind) {
    case 'instance':
      return $localize`:@@limits.place.instance-label:Instance`;
    case 'project':
      return $localize`:@@limits.place.project-label:Project ${place.project}:project:`;
    case 'repository':
      return place.repository;
    case 'bastion':
      return $localize`:@@limits.place.bastion-label:Bastion ${place.bastion}:bastion:`;
  }
}

/** The order of resolution (R-LIM-013): single callers and addresses, then groups, then the kinds. */
export function whoRank(who: LimitWho): number {
  switch (who.kind) {
    case 'user':
    case 'technicalToken':
    case 'ciProject':
    case 'address':
      return 0;
    case 'group':
      return 1;
    case 'every':
      return 2;
  }
}

/** The name a rule's "who" sorts by within its rank. */
export function whoSortName(who: LimitWho): string {
  const view = whoView(who);
  return view.primary.toLocaleLowerCase();
}

/** The path segments of a subject's page (R-UI-184): its kind and its identifier. */
export function subjectRoute(subject: UsageSubject): [UsageSubjectKind, string] {
  return [subject.kind, subject.id];
}
