import { Component, computed, input, model } from '@angular/core';
import { Schema } from '../../api/types';
import { VISIBILITY_ICONS, VisibilityLevel, levelName } from './visibility-icon';

export { VisibilityIcon, levelName } from './visibility-icon';
export type { VisibilityLevel } from './visibility-icon';

/** What the forms read from `GET /api/v1/visibility-choices` (R-API-076). */
type Choices = Schema<'VisibilityChoices'>;

export const VISIBILITY_LEVELS: readonly VisibilityLevel[] = ['private', 'internal', 'public'];

/** What the control is choosing the level of; it decides the sentence under each option (Q-UI-228). */
export type VisibilitySubject = 'new-projects' | 'project' | 'artifacts' | 'wiki' | 'upstream';

/** The reason Public is off while the installation admits no request without a credential (R-UI-053). */
export function publicUnavailableReason(): Partial<Record<VisibilityLevel, string>> {
  return {
    public: $localize`:@@visibility.public-unavailable:The Helm configuration admits no request without a credential (endpoints.default.auth.anonymous).`,
  };
}

/** The reasons by level, earlier sets first (R-AUTHZ-038): an option keeps the first reason any set gives it. */
export function firstReasons(
  ...sets: readonly Partial<Record<VisibilityLevel, string>>[]
): Partial<Record<VisibilityLevel, string>> {
  const out: Partial<Record<VisibilityLevel, string>> = {};
  for (const set of sets) {
    for (const level of VISIBILITY_LEVELS) {
      const reason = set[level];
      if (reason !== undefined && out[level] === undefined) out[level] = reason;
    }
  }
  return out;
}

/**
 * The reasons for the options the caller cannot choose because of the installation (R-UI-141, R-AUTHZ-038): Public while it is
 * unavailable, and every restricted level for a caller who is not an administrator, except `kept`, the level the object has,
 * which it may keep (Q-AUTHZ-285). `null` (the choices are not known) restricts nothing; the server decides.
 */
export function installationReasons(
  choices: Choices | null,
  kept: VisibilityLevel | null = null,
): Partial<Record<VisibilityLevel, string>> {
  if (choices === null) return {};
  const out: Partial<Record<VisibilityLevel, string>> = choices.publicAvailable ? {} : { ...publicUnavailableReason() };
  if (choices.mayChooseRestricted) return out;
  for (const level of choices.restrictedLevels) {
    if (level !== kept && out[level] === undefined) {
      out[level] = $localize`:@@visibility.restricted:Only an administrator of Quader can choose this level.`;
    }
  }
  return out;
}

/**
 * The notes under the restricted options that stay enabled (R-UI-141): for an administrator every restricted level says so, and
 * for anyone else the level the object keeps says that it cannot be chosen again once it is left (Q-AUTHZ-285).
 */
export function restrictedNotes(
  choices: Choices | null,
  kept: VisibilityLevel | null = null,
): Partial<Record<VisibilityLevel, string>> {
  const out: Partial<Record<VisibilityLevel, string>> = {};
  if (choices === null) return out;
  for (const level of choices.restrictedLevels) {
    if (choices.mayChooseRestricted) {
      out[level] = $localize`:@@visibility.restricted-admin:Restricted: only administrators can choose this level.`;
    } else if (level === kept) {
      out[level] = $localize`:@@visibility.restricted-kept:Restricted: kept while it is set; only an administrator can choose it again.`;
    }
  }
  return out;
}

/** The lower of two levels. */
export function lowerLevel(a: VisibilityLevel, b: VisibilityLevel): VisibilityLevel {
  return VISIBILITY_LEVELS.indexOf(a) <= VISIBILITY_LEVELS.indexOf(b) ? a : b;
}

/** `true` when `a` is more open than `b`. */
export function isMoreOpen(a: VisibilityLevel, b: VisibilityLevel): boolean {
  return VISIBILITY_LEVELS.indexOf(a) > VISIBILITY_LEVELS.indexOf(b);
}

/**
 * The reasons for the options a repository cannot take because its project is less open (R-UI-055): every level above `parent`
 * is off. `null` (the parent's level is not known) restricts nothing; the server decides.
 */
export function aboveProjectReasons(parent: VisibilityLevel | null): Partial<Record<VisibilityLevel, string>> {
  const out: Partial<Record<VisibilityLevel, string>> = {};
  if (parent === null) return out;
  const p = levelName(parent).toLowerCase();
  for (const level of VISIBILITY_LEVELS) {
    if (isMoreOpen(level, parent)) {
      const l = levelName(level).toLowerCase();
      out[level] = $localize`:@@visibility.above-project:The project is ${p}:project:, so this repository cannot be ${l}:level:.`;
    }
  }
  return out;
}

function sentence(subject: VisibilitySubject, level: VisibilityLevel): string {
  switch (subject) {
    case 'new-projects':
      switch (level) {
        case 'private':
          return $localize`:@@visibility.new-projects.private:Only members and the principals a policy names read new projects.`;
        case 'internal':
          return $localize`:@@visibility.new-projects.internal:Everyone signed in, service accounts and CI jobs included, reads new projects; external users do not.`;
        case 'public':
          return $localize`:@@visibility.new-projects.public:Anyone, also without signing in, reads new projects.`;
      }
      break;
    case 'project':
      switch (level) {
        case 'private':
          return $localize`:@@visibility.project.private:Only members and the principals a policy names read the project's repositories.`;
        case 'internal':
          return $localize`:@@visibility.project.internal:Everyone signed in, service accounts and CI jobs included, can find the project and read what its repositories make internal; external users cannot.`;
        case 'public':
          return $localize`:@@visibility.project.public:Anyone, also without signing in, can find the project and read what its repositories make public.`;
      }
      break;
    case 'artifacts':
      switch (level) {
        case 'private':
          return $localize`:@@visibility.artifacts.private:Only members of the project and the principals a policy names.`;
        case 'internal':
          return $localize`:@@visibility.artifacts.internal:Everyone signed in, service accounts and CI jobs included, except external users.`;
        case 'public':
          return $localize`:@@visibility.artifacts.public:Anyone, also without signing in.`;
      }
      break;
    case 'wiki':
      switch (level) {
        case 'private':
          return $localize`:@@visibility.wiki.private:Only members of the project and the principals a policy names.`;
        case 'internal':
          return $localize`:@@visibility.wiki.internal:Everyone signed in, service accounts and CI jobs included, except external users.`;
        case 'public':
          return $localize`:@@visibility.wiki.public:Anyone, also without signing in.`;
      }
      break;
    // A well-known repository of the project `upstream` (R-UI-187): the project has no members, and a team reads it through its
    // virtual repositories by the repository's own level (R-AUTHZ-041).
    case 'upstream':
      switch (level) {
        case 'private':
          return $localize`:@@visibility.upstream.private:Only administrators and the principals a global policy names. Teams' virtual repositories skip a private registry.`;
        case 'internal':
          return $localize`:@@visibility.upstream.internal:Everyone signed in, service accounts and CI jobs included, except external users, directly and through every virtual repository that lists it.`;
        case 'public':
          return $localize`:@@visibility.upstream.public:Anyone, also without signing in, on this installation's bandwidth and the registry's pull allowance.`;
      }
  }
}

/** The layout of the options of both level controls: icon and name on one line, the sentence and a reason or a note under it. */
const LEVEL_STYLES = `
    :host {
      display: block;
    }
    .levels {
      min-inline-size: 0;
      margin: 0;
      padding: 0;
      border: 0;
    }
    legend {
      padding: 0;
      margin-block-end: var(--quader-space-2);
    }
    .level {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
      margin-block-end: var(--quader-space-3);
    }
    /* The box keeps its 16 px beside a sentence of two lines, as a check box of .check-row does (Q-UI-575): shrunk to 13 px, it
       pulled its option's name and sentence 3 px left of the others (New project at 1,440 px). */
    .level > input {
      flex: none;
    }
    .text {
      display: flex;
      flex-direction: column;
    }
    .name {
      font-weight: 600;
    }
    .muted {
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
    }
    .unavailable .name {
      color: var(--quader-text-muted);
      font-weight: 400;
    }
    fieldset:disabled .name {
      color: var(--quader-text-muted);
    }
    /* The server's answer for the level closes the group: under the last option, before what follows the group. */
    .field-error {
      margin-block: 0 var(--quader-space-3);
    }
`;

let nextGroupId = 1;

/**
 * The control that chooses a visibility level (Q-UI-228, docs/developer/ui-style-guide.md#forms): a radio group of Private,
 * Internal and Public, each with its icon and one muted sentence, as the owner's GitLab has it. An option that cannot be
 * chosen stays visible, muted, with its reason under the sentence (`reasons`); the radio is disabled and the reason is
 * part of the option's description. Native radios, so the arrow keys move and select, and Tab enters and leaves the group once.
 * What the server answered for the level stands under the options (`error`), as an alert that describes the group; a group
 * inside a fold whose toggle names it keeps its legend for assistive technology only (`labelHidden`).
 *
 *   <quader-visibility-level label="Visibility level" subject="artifacts" [(value)]="level" [reasons]="{ public: 'The project is internal.' }" />
 */
@Component({
  selector: 'quader-visibility-level',
  template: `
    <fieldset class="levels" [disabled]="disabled()" [attr.aria-describedby]="error() ? errorId : null">
      <legend class="field-label" [class.visually-hidden]="labelHidden()">{{ label() }}</legend>
      @if (unsetNote(); as note) {
        <p class="muted unset" role="status">{{ note }}</p>
      }
      @for (option of options(); track option.level) {
        <label class="level" [class.unavailable]="option.reason !== null">
          <input
            type="radio"
            [name]="group"
            [value]="option.level"
            [checked]="unsetNote() === null && value() === option.level"
            [disabled]="option.reason !== null"
            [attr.aria-describedby]="option.describedBy"
            (change)="value.set(option.level)"
          />
          <span class="text">
            <span class="name"><span class="material-icons inline" aria-hidden="true">{{ option.icon }}</span>{{ option.name }}</span>
            <span class="muted" [id]="option.sentenceId">{{ option.sentence }}</span>
            @if (option.reason; as reason) {
              <span class="muted reason" [id]="option.reasonId">{{ reason }}</span>
            } @else if (option.note; as note) {
              <span class="muted note" [id]="option.reasonId">{{ note }}</span>
            }
          </span>
        </label>
      }
      @if (error(); as message) {
        <p class="field-error" role="alert" [id]="errorId">{{ message }}</p>
      }
    </fieldset>
  `,
  styles: LEVEL_STYLES,
})
export class VisibilityLevelControl {
  readonly label = input.required<string>();
  /** The group is named by what stands above it (the toggle of the fold that holds it): the legend is for assistive technology only. */
  readonly labelHidden = input(false);
  /** What the server answered for this level when it refused the save (a field problem at the level's member), under the options. */
  readonly error = input<string | null>(null);
  readonly subject = input<VisibilitySubject>('project');
  readonly value = model.required<VisibilityLevel>();
  /** While set, nothing is chosen yet: no option is checked and the note says what is applied instead (the server's default). */
  readonly unsetNote = input<string | null>(null);
  /** The reason an option cannot be chosen, by level; an option without an entry can. */
  readonly reasons = input<Partial<Record<VisibilityLevel, string>>>({});
  /** A muted note under an option that can be chosen (a restricted level an administrator sees, R-UI-141); a reason wins. */
  readonly notes = input<Partial<Record<VisibilityLevel, string>>>({});
  /** The whole group cannot be changed (a reader without the right to write). */
  readonly disabled = input(false);

  protected readonly group = `quader-visibility-level-${nextGroupId++}`;
  protected readonly errorId = `${this.group}-error`;

  protected readonly options = computed(() =>
    VISIBILITY_LEVELS.map((level) => {
      const reason = this.reasons()[level] ?? null;
      const note = reason === null ? (this.notes()[level] ?? null) : null;
      const sentenceId = `${this.group}-${level}`;
      const reasonId = `${this.group}-${level}-reason`;
      return {
        level,
        icon: VISIBILITY_ICONS[level],
        name: levelName(level),
        sentence: sentence(this.subject(), level),
        reason,
        note,
        sentenceId,
        reasonId,
        describedBy: reason === null && note === null ? sentenceId : `${sentenceId} ${reasonId}`,
      };
    }),
  );
}

/** What restricting a level means, under its checkbox (R-UI-140). */
function restrictedSentence(level: VisibilityLevel): string {
  switch (level) {
    case 'private':
      return $localize`:@@visibility.restrict.private:Only an administrator can make a project or a repository private.`;
    case 'internal':
      return $localize`:@@visibility.restrict.internal:Only an administrator can make a project or a repository internal.`;
    case 'public':
      return $localize`:@@visibility.restrict.public:Only an administrator can make a project or a repository public.`;
  }
}

/**
 * The restricted levels of the installation (R-UI-140, Q-UI-541): one checkbox per level, each with the level's icon and name and
 * one muted sentence of what restricting it means, laid out as the options of `quader-visibility-level`. An option with a reason
 * (the default level, which cannot be restricted) is disabled and names the reason in its description.
 *
 *   <quader-restricted-levels label="Restricted visibility levels" [(value)]="restricted" [reasons]="{ private: 'The default.' }" />
 */
@Component({
  selector: 'quader-restricted-levels',
  template: `
    <fieldset class="levels" [disabled]="disabled()">
      <legend class="field-label">{{ label() }}</legend>
      @for (option of options(); track option.level) {
        <label class="level" [class.unavailable]="option.reason !== null">
          <input
            type="checkbox"
            [checked]="option.checked"
            [disabled]="option.reason !== null"
            [attr.aria-describedby]="option.describedBy"
            (change)="toggle(option.level, $any($event.target).checked)"
          />
          <span class="text">
            <span class="name"><span class="material-icons inline" aria-hidden="true">{{ option.icon }}</span>{{ option.name }}</span>
            <span class="muted" [id]="option.sentenceId">{{ option.sentence }}</span>
            @if (option.reason; as reason) {
              <span class="muted reason" [id]="option.reasonId">{{ reason }}</span>
            }
          </span>
        </label>
      }
    </fieldset>
  `,
  styles: LEVEL_STYLES,
})
export class RestrictedLevelsControl {
  readonly label = input.required<string>();
  /** The restricted levels, in the order of the levels. */
  readonly value = model.required<readonly VisibilityLevel[]>();
  /** The reason a level cannot be restricted, by level. */
  readonly reasons = input<Partial<Record<VisibilityLevel, string>>>({});
  readonly disabled = input(false);

  protected readonly group = `quader-restricted-levels-${nextGroupId++}`;

  protected readonly options = computed(() =>
    VISIBILITY_LEVELS.map((level) => {
      const reason = this.reasons()[level] ?? null;
      const sentenceId = `${this.group}-${level}`;
      const reasonId = `${this.group}-${level}-reason`;
      return {
        level,
        checked: this.value().includes(level),
        icon: VISIBILITY_ICONS[level],
        name: levelName(level),
        sentence: restrictedSentence(level),
        reason,
        sentenceId,
        reasonId,
        describedBy: reason === null ? sentenceId : `${sentenceId} ${reasonId}`,
      };
    }),
  );

  protected toggle(level: VisibilityLevel, on: boolean): void {
    const set = new Set(this.value());
    if (on) set.add(level);
    else set.delete(level);
    this.value.set(VISIBILITY_LEVELS.filter((l) => set.has(l)));
  }
}
