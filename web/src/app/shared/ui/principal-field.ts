import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  afterRenderEffect,
  ElementRef,
  computed,
  effect,
  inject,
  input,
  model,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { LimitsApi } from '../../api/limits-api';
import { ProjectsApi } from '../../api/projects-api';
import { SearchApi, SuggestionKind } from '../../api/search-api';
import { ActiveOption, heldEnter, optionDomId } from './active-option';
import { QuaderAvatar } from './avatar';

/**
 * The kinds of principal a field names. A technical token and a CI project are the two kinds of the download limits' rule dialog
 * (R-UI-179): no policy names them, so their matches come from the limits' own suggestions.
 */
export type PrincipalKind = 'user' | 'group' | 'service_account' | 'technical_token' | 'ci_project';

/** The kinds the principal lookup of a project and the admin area's suggestions know. */
type DirectoryKind = 'user' | 'group' | 'service_account';

/** One principal the field offers or holds: what its row shows and the reference a write takes. */
export interface PrincipalChoice {
  /** `user:<name>`, `group:<name>` or `sa:<name>` (`sa:<project>/<name>` for a project's account). */
  readonly ref: string;
  /**
   * `other` is a reference the field holds but never offers: a CI identity a stored policy entry names (`ci:…`), which a CI
   * mapping's own policies grant instead (Q-UI-583). Its chip shows the reference and can be removed.
   */
  readonly kind: PrincipalKind | 'other';
  /** The username, the group's name or the service account's stored name; the whole reference for `other`. */
  readonly name: string;
  readonly displayName?: string | null;
  /** The seed of a user's generated avatar. */
  readonly id?: string | null;
  readonly avatarUrl?: string | null;
  /** The project of a service account, null for an instance-wide one. */
  readonly project?: string | null;
  /** The second line of a technical token (its account) or of a CI project (its issuer and ID). */
  readonly detail?: string | null;
}

/** Debounce of the search while the person types. */
export const PRINCIPAL_DEBOUNCE_MS = 250;
/** The least number of characters both searches accept (Q-API-281, Q-AUTHZ-182). */
export const PRINCIPAL_MIN_CHARS = 2;
/** Matches the admin area's search asks for per kind (R-API-060 allows 10). */
const SUGGEST_LIMIT = 10;

/** The reference of a principal as the field holds it: kind and name parsed from `user:`, `group:` or `sa:`. */
export function choiceFromRef(ref: string): PrincipalChoice {
  const at = ref.indexOf(':');
  const prefix = at < 0 ? '' : ref.slice(0, at);
  const name = at < 0 ? ref : ref.slice(at + 1);
  if (prefix === 'group') return { ref, kind: 'group', name };
  if (prefix === TECHNICAL_TOKEN_PREFIX) return { ref, kind: 'technical_token', name };
  if (prefix === CI_PROJECT_PREFIX) return { ref, kind: 'ci_project', name };
  if (prefix === 'sa') {
    const slash = name.indexOf('/');
    return { ref, kind: 'service_account', name, project: slash < 0 ? null : name.slice(0, slash) };
  }
  if (prefix === 'user') return { ref, kind: 'user', name };
  return { ref, kind: 'other', name: ref };
}

/** The field's own prefixes of the two kinds of the limits: no API reads them, the rule dialog turns them into its `who`. */
export const TECHNICAL_TOKEN_PREFIX = 'technical-token';
export const CI_PROJECT_PREFIX = 'ci-project';

/**
 * The reference of a user name, a group name or a service account name the API keeps without its kind; of a technical token its
 * ID, of a CI project `<issuer>:<project ID>`.
 */
export function refOf(kind: PrincipalKind, name: string): string {
  switch (kind) {
    case 'service_account':
      return `sa:${name}`;
    case 'technical_token':
      return `${TECHNICAL_TOKEN_PREFIX}:${name}`;
    case 'ci_project':
      return `${CI_PROJECT_PREFIX}:${name}`;
    default:
      return `${kind}:${name}`;
  }
}

let nextId = 1;

/**
 * Places the open list (fixed, in the top layer) under its field, as wide as the field and at most half the window or 24 rem high;
 * above the field where the room below is smaller than the list and the room above larger, as a menu flips.
 */
function placeList(popup: HTMLElement, anchor: HTMLElement): void {
  const field = anchor.getBoundingClientRect();
  const gap = 4;
  const edge = 8;
  const cap = Math.min(window.innerHeight / 2, 384);
  const below = window.innerHeight - field.bottom - gap - edge;
  const above = field.top - gap - edge;
  const need = Math.min(popup.scrollHeight, cap);
  popup.style.insetInlineStart = `${field.left}px`;
  popup.style.inlineSize = `${field.width}px`;
  if (need > below && above > below) {
    const room = Math.min(cap, above);
    popup.style.maxBlockSize = `${room}px`;
    popup.style.insetBlockStart = `${field.top - gap - Math.min(need, room)}px`;
  } else {
    popup.style.maxBlockSize = `${Math.max(0, Math.min(cap, below))}px`;
    popup.style.insetBlockStart = `${field.bottom + gap}px`;
  }
}

/**
 * The one field that names a person, a group or a service account (R-UI-150, Q-UI-580): it searches while one types and lists
 * the matches under the field, each with the avatar (or the kind's icon), the name and under it the username or the kind; the
 * chosen principals are chips in the field. Nobody types a reference (`user:`, `group:`, `sa:`).
 *
 * Where the matches come from decides who may search (Q-UI-581): inside a project (`project`) the project's principal lookup
 * (Q-AUTHZ-182), which its Owner and a holder of `identities:read` call; everywhere else the suggestions of the admin area's
 * kinds (`GET /api/v1/search/suggestions`, `identities:read`). A technical token and a CI project, the kinds of the download
 * limits' rules, come from `GET /api/v1/limits/subjects` (inside a project with `project`). The field never widens what its
 * caller may search: a refused search shows its failure.
 *
 * A combobox with a listbox (the ARIA pattern): the focus stays in the input, `aria-activedescendant` names the active match,
 * the arrows move it, Enter takes it, Escape closes the list and then empties the text, Backspace on an empty text removes the
 * last chip. A newer answer keeps the active match where it still lists it and leaves none active where it does not
 * (`ActiveOption`, Q-UI-700). Enter never submits the form around the field while there is text to search. The text searches after
 * `PRINCIPAL_MIN_CHARS` characters and a pause of `PRINCIPAL_DEBOUNCE_MS`; a late answer of an older text is dropped.
 */
@Component({
  selector: 'quader-principal-field',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [QuaderAvatar],
  template: `
    <div class="field">
      <label class="field-label" [for]="inputId">{{ label() }}</label>
      <div #anchor class="anchor field-w-name" [class.full]="width() === 'full'">
        <div class="box text-input" [class.invalid]="!!error()" [class.readonly]="readonly()">
          @if (chosen().length > 0) {
            <ul class="chips" [attr.aria-label]="chosenLabel()">
              @for (c of chosen(); track c.ref) {
                <li class="chip">
                  <span class="mark picture" aria-hidden="true">
                    @if (c.kind === 'user') {
                      <quader-avatar class="chip-avatar" [seed]="c.id || c.ref" [imageUrl]="c.avatarUrl" />
                    } @else {
                      <span class="material-icons">{{ iconOf(c) }}</span>
                    }
                  </span>
                  <span class="chip-name" translate="no">{{ primaryOf(c) }}</span>
                  <span class="visually-hidden">{{ secondaryOf(c) }}</span>
                  @if (!readonly()) {
                    <button type="button" class="chip-remove" [attr.aria-label]="removeLabel(c)" (click)="remove(c, $event)">
                      <span class="material-icons" aria-hidden="true">close</span>
                    </button>
                  }
                </li>
              }
            </ul>
          }
          @if (!readonly() && (multiple() || chosen().length === 0)) {
            <input
              #field
              class="bare"
              type="text"
              role="combobox"
              autocomplete="off"
              spellcheck="false"
              aria-autocomplete="list"
              [id]="inputId"
              [name]="name()"
              [attr.aria-expanded]="open()"
              [attr.aria-controls]="listId"
              [attr.aria-activedescendant]="activeId()"
              [attr.aria-describedby]="describedBy()"
              [attr.aria-invalid]="error() ? 'true' : null"
              [value]="text()"
              (input)="onInput($any($event.target).value)"
              (keydown)="onKeydown($event)"
              (focus)="onFocus()"
              (blur)="onBlur()"
            />
          }
        </div>
        @if (open()) {
          <!-- The pointer keeps the focus in the input (mousedown), so a click reaches its row. A row is never focused (the focus
               stays in the input, aria-activedescendant names the row); tabindex -1 and Enter keep it operable should a tool focus it.
               The list stands in the top layer (popover), fixed under its field: a dialog's or a drawer's body scrolls, and a list
               that hung in it was cut by it (Add member, Q-UI-1146). -->
          <div #popup class="popup" popover="manual" (mousedown)="$event.preventDefault()">
            <!-- aria-busy while the text's search runs: assistive technology waits for the answer, and so do the tests (choosePrincipal) -->
            <div role="listbox" class="list" [id]="listId" [attr.aria-label]="listLabel()" [attr.aria-busy]="phase() === 'searching' ? 'true' : null">
              @for (m of matches(); track m.ref; let i = $index) {
                <div
                  role="option"
                  class="option"
                  [id]="optionId(m)"
                  [class.active]="active.index() === i"
                  [attr.aria-selected]="active.index() === i"
                  tabindex="-1"
                  (click)="pick(m)"
                  (keydown.enter)="pick(m)"
                  (mousemove)="active.pointer(i, $event)"
                >
                  <span class="mark picture" aria-hidden="true">
                    @if (m.kind === 'user') {
                      <quader-avatar class="option-avatar" [seed]="m.id || m.ref" [imageUrl]="m.avatarUrl" />
                    } @else {
                      <span class="material-icons">{{ iconOf(m) }}</span>
                    }
                  </span>
                  <span class="who">
                    <span class="primary" translate="no">{{ primaryOf(m) }}</span>
                    <span class="secondary" [attr.translate]="m.kind === 'user' ? 'no' : null">{{ secondaryOf(m) }}</span>
                  </span>
                </div>
              }
            </div>
            @if (state(); as s) {
              <p class="state" [class.failed]="s.failed">{{ s.text }}</p>
            }
          </div>
        }
      </div>
      @if (hint()) {
        <p class="hint" [id]="inputId + '-hint'">{{ hint() }}</p>
      }
      <p class="visually-hidden" role="status" aria-live="polite">{{ announcement() }}</p>
    </div>
    @if (error(); as text) {
      <p class="field-error" [id]="inputId + '-error'">{{ text }}</p>
    }
  `,
  styles: `
    :host {
      display: block;
      inline-size: 100%;
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-3);
    }
    .field:has(+ .field-error) {
      margin-block-end: var(--quader-space-1);
    }
    .hint {
      margin: 0;
    }
    .field-error {
      margin: 0 0 var(--quader-space-3);
      color: var(--quader-status-bad);
      font: var(--quader-type-body-small);
    }
    /* The field is as wide as a name (Q-UI-335); the list under it takes at least the room a name and its username need. */
    .anchor {
      position: relative;
    }
    .anchor.full {
      inline-size: 100%;
    }
    .box {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-1);
      inline-size: 100%;
      cursor: text;
    }
    .box:focus-within {
      outline: var(--quader-focus-ring);
      outline-offset: 1px;
    }
    .box.invalid {
      border-color: var(--quader-status-bad);
      border-width: 2px;
    }
    .box.readonly {
      border-color: transparent;
      background: var(--quader-control-disabled-bg);
      cursor: default;
    }
    .bare {
      flex: 1 1 6rem;
      min-inline-size: 6rem;
      padding: 0;
      border: 0;
      background: transparent;
      color: inherit;
      font: inherit;
      outline: none;
    }
    .chips {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-1);
      margin: 0;
      padding: 0;
      list-style: none;
    }
    .chip {
      display: inline-flex;
      align-items: center;
      gap: var(--quader-space-1);
      max-inline-size: 100%;
      padding-inline: var(--quader-space-1);
      border-radius: var(--quader-radius-full);
      background: var(--quader-badge-neutral-bg);
      color: var(--quader-badge-neutral-fg);
    }
    .chip-name {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .chip-remove {
      display: inline-flex;
      padding: 0;
      border: 0;
      border-radius: 50%;
      background: transparent;
      color: inherit;
      cursor: pointer;
    }
    .chip-remove:focus-visible {
      outline: var(--quader-focus-ring);
    }
    .chip-remove .material-icons {
      font-size: 1rem;
    }
    .mark {
      flex: none;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      color: var(--quader-text-muted);
    }
    .chip .mark .material-icons {
      font-size: 1rem;
    }
    .chip-avatar {
      inline-size: 1rem;
      block-size: 1rem;
      border-radius: 50%;
    }
    /* In the top layer, placed by placeList() under the field (above it where the window has more room there); without the popover
       (an engine without it) it hangs under the field as before. */
    .popup {
      position: absolute;
      inset-block-start: calc(100% + var(--quader-space-1));
      inset-inline-start: 0;
      z-index: 900;
      box-sizing: border-box;
      /* As wide as the field: a list wider than its field leaves a drawer or a dialog at the side. */
      inline-size: 100%;
      max-block-size: min(50vh, 24rem);
      margin: 0;
      overflow-y: auto;
      padding: var(--quader-space-1);
      border: 1px solid var(--quader-menu-border);
      border-radius: var(--quader-radius-m);
      background: var(--quader-menu-bg);
      color: var(--quader-control-fg);
      box-shadow: var(--quader-elevation-2);
      font-size: 0.875rem;
    }
    .popup:popover-open {
      position: fixed;
      inset-block-end: auto;
      inset-inline-end: auto;
    }
    .option {
      display: flex;
      align-items: center;
      gap: var(--quader-space-3);
      padding: var(--quader-space-1) var(--quader-space-2);
      border-radius: var(--quader-radius-s);
      cursor: pointer;
    }
    .option:hover,
    .option.active {
      background: var(--quader-menu-hover);
    }
    .option.active {
      outline: var(--quader-focus-ring);
      outline-offset: -2px;
    }
    /* The avatar of a match and the kind's icon take the same round place (th.person of the lists, 32 px). */
    .option .mark {
      inline-size: 2rem;
      block-size: 2rem;
      border-radius: 50%;
      overflow: hidden;
      background: var(--quader-badge-neutral-bg);
      color: var(--quader-badge-neutral-fg);
    }
    .option .mark .material-icons {
      font-size: 1.25rem;
    }
    .option-avatar {
      inline-size: 2rem;
      block-size: 2rem;
    }
    .who {
      display: flex;
      flex-direction: column;
      min-inline-size: 0;
    }
    .primary,
    .secondary {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .secondary {
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
    }
    .state {
      margin: 0;
      padding: var(--quader-space-2);
      color: var(--quader-text-muted);
    }
    .state.failed {
      color: var(--quader-status-bad);
    }
  `,
})
export class PrincipalField {
  private readonly projectsApi = inject(ProjectsApi);
  private readonly limitsApi = inject(LimitsApi);
  private readonly searchApi = inject(SearchApi);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  readonly label = input.required<string>();
  readonly name = input('');
  readonly hint = input('');
  readonly error = input<string | null>(null);
  readonly readonly = input(false);
  /** The kinds the field names; users only by default. */
  readonly kinds = input<readonly PrincipalKind[]>(['user']);
  /** One choice (the default) or many. */
  readonly multiple = input(false);
  /** The project the principal is named in: its lookup is the source; null takes the admin area's suggestions. */
  readonly project = input<string | null>(null);
  /**
   * Which service accounts the admin area's search offers: `instance` (the default) the instance-wide ones, which role bindings,
   * group members and global policies name; `all` every account, for a filter by owner or actor.
   */
  readonly accounts = input<'instance' | 'all'>('instance');
  /** References that are taken already elsewhere (the members of a list): not offered. */
  readonly exclude = input<readonly string[]>([]);
  /** The field's width: a name (the default) or the column. */
  readonly width = input<'name' | 'full'>('name');
  /** The chosen references. */
  readonly value = model<readonly string[]>([]);
  /** A principal was chosen from the list, with what its row showed. */
  readonly picked = output<PrincipalChoice>();

  private readonly input = viewChild<ElementRef<HTMLInputElement>>('field');
  private readonly anchor = viewChild<ElementRef<HTMLElement>>('anchor');
  private readonly popup = viewChild<ElementRef<HTMLElement>>('popup');
  protected readonly inputId = `quader-principal-${nextId++}`;
  protected readonly listId = `${this.inputId}-list`;
  protected readonly text = signal('');
  protected readonly open = signal(false);
  private readonly found = signal<readonly PrincipalChoice[]>([]);
  private readonly more = signal(false);
  protected readonly phase = signal<'idle' | 'searching' | 'done' | 'failed'>('idle');
  /** What the field learned of a reference it chose or found, so a chip shows the name and the avatar. */
  private readonly known = signal<ReadonlyMap<string, PrincipalChoice>>(new Map());
  private generation = 0;
  private abort: AbortController | null = null;

  protected readonly chosen = computed(() => this.value().map((ref) => this.known().get(ref) ?? choiceFromRef(ref)));
  protected readonly matches = computed(() => {
    const skip = new Set([...this.exclude(), ...this.value()]);
    return this.found().filter((m) => !skip.has(m.ref));
  });
  /** The active match, held by its reference across a new answer (Q-UI-700). */
  protected readonly active = new ActiveOption(computed(() => this.matches().map((m) => m.ref)));
  protected readonly activeId = computed(() => {
    const ref = this.active.current();
    return this.open() && ref !== null ? optionDomId(`${this.inputId}-option`, ref) : null;
  });
  protected readonly describedBy = computed(() => {
    const ids = [this.hint() ? `${this.inputId}-hint` : '', this.error() ? `${this.inputId}-error` : ''].filter(Boolean);
    return ids.length ? ids.join(' ') : null;
  });
  /** The line under the matches: searching, nothing found, more to narrow, or the failure. */
  protected readonly state = computed<{ text: string; failed: boolean } | null>(() => {
    const phase = this.phase();
    if (phase === 'failed') return { text: $localize`:@@principal-field.failed:The search failed. Change the text to try again.`, failed: true };
    if (phase === 'searching' && this.matches().length === 0) return { text: $localize`:@@principal-field.searching:Searching…`, failed: false };
    if (phase === 'done' && this.matches().length === 0) {
      const text = this.text().trim();
      return {
        text: this.kinds().includes('user')
          ? $localize`:@@principal-field.none-user:Nobody matches “${text}:text:”. A person can be found after their first sign-in.`
          : $localize`:@@principal-field.none:Nothing matches “${text}:text:”.`,
        failed: false,
      };
    }
    if (phase === 'done' && this.more()) return { text: $localize`:@@principal-field.more:More match. Type more of the name.`, failed: false };
    return null;
  });
  /** What a screen reader hears when the matches change. */
  protected readonly announcement = computed(() => {
    if (!this.open()) return '';
    const s = this.state();
    if (s && this.matches().length === 0) return s.text;
    const n = this.matches().length;
    return n === 1 ? $localize`:@@principal-field.one-match:1 match.` : $localize`:@@principal-field.matches:${n}:count: matches.`;
  });

  constructor() {
    inject(DestroyRef).onDestroy(() => this.abort?.abort());
    // The open list in the top layer, under its field, again on every scroll and resize while it is open and whenever its rows
    // change: it follows its field as a menu does.
    afterRenderEffect((onCleanup) => {
      const popup = this.popup()?.nativeElement;
      const anchor = this.anchor()?.nativeElement;
      this.matches();
      this.state();
      if (!popup || !anchor || typeof popup.showPopover !== 'function') return;
      if (!popup.matches(':popover-open')) popup.showPopover();
      const place = (): void => placeList(popup, anchor);
      place();
      window.addEventListener('scroll', place, true);
      window.addEventListener('resize', place);
      onCleanup(() => {
        window.removeEventListener('scroll', place, true);
        window.removeEventListener('resize', place);
      });
    });
    // A new text searches after the pause; a text below the minimum clears the list.
    effect((onCleanup) => {
      const text = this.text().trim();
      const generation = ++this.generation;
      this.abort?.abort();
      this.abort = null;
      if (text.length < PRINCIPAL_MIN_CHARS) {
        untracked(() => {
          this.found.set([]);
          this.more.set(false);
          this.phase.set('idle');
          this.open.set(false);
          this.active.clear();
        });
        return;
      }
      untracked(() => {
        this.phase.set('searching');
        this.open.set(true);
      });
      const timer = setTimeout(() => void this.run(text, generation), PRINCIPAL_DEBOUNCE_MS);
      onCleanup(() => clearTimeout(timer));
    });
  }

  protected optionId(match: PrincipalChoice): string {
    return optionDomId(`${this.inputId}-option`, match.ref);
  }

  protected listLabel(): string {
    return $localize`:@@principal-field.list:Matches for ${this.label()}:field:`;
  }

  protected chosenLabel(): string {
    return $localize`:@@principal-field.chosen:Chosen for ${this.label()}:field:`;
  }

  protected removeLabel(c: PrincipalChoice): string {
    return $localize`:@@principal-field.remove:Remove ${this.primaryOf(c)}:name:`;
  }

  protected iconOf(c: PrincipalChoice): string {
    switch (c.kind) {
      case 'other':
      case 'technical_token':
        return 'key';
      case 'ci_project':
        return 'rocket_launch';
      case 'group':
        return 'groups';
      default:
        return 'smart_toy';
    }
  }

  protected primaryOf(c: PrincipalChoice): string {
    return (c.kind === 'user' && c.displayName) || c.name;
  }

  /** The line under the name: the username of a person, the kind of a group or a service account and its project. */
  protected secondaryOf(c: PrincipalChoice): string {
    if (c.kind === 'user') return `@${c.name}`;
    if (c.kind === 'group') return $localize`:@@principal-field.kind-group:Group`;
    if (c.kind === 'other') return $localize`:@@principal-field.kind-other:CI identity`;
    if (c.kind === 'technical_token') return c.detail || $localize`:@@principal-field.kind-technical-token:Technical token`;
    if (c.kind === 'ci_project') return c.detail || $localize`:@@principal-field.kind-ci-project:CI project`;
    return c.project
      ? $localize`:@@principal-field.kind-project-account:Service account of ${c.project}:project:`
      : $localize`:@@principal-field.kind-account:Service account`;
  }

  protected focusInput(): void {
    this.input()?.nativeElement.focus();
  }

  protected onInput(value: string): void {
    this.text.set(value);
    this.active.clear();
  }

  protected onFocus(): void {
    if (this.text().trim().length >= PRINCIPAL_MIN_CHARS) this.open.set(true);
  }

  protected onBlur(): void {
    this.open.set(false);
    this.active.clear();
  }

  protected onKeydown(event: KeyboardEvent): void {
    const count = this.matches().length;
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        if (this.text().trim().length >= PRINCIPAL_MIN_CHARS) this.open.set(true);
        this.active.next();
        return;
      case 'ArrowUp':
        if (!this.open()) return;
        event.preventDefault();
        this.active.previous();
        return;
      case 'Enter': {
        // A held Enter acts once (Q-UI-701): its repeats neither take a match nor submit the form around the field.
        if (heldEnter(event)) {
          event.preventDefault();
          return;
        }
        // Enter takes the active match; while there is text it never submits the form around the field.
        const active = this.active.index();
        const match = this.open() ? this.matches()[active < 0 && count === 1 ? 0 : active] : undefined;
        if (match || this.text().trim() !== '') event.preventDefault();
        if (match) this.pick(match);
        return;
      }
      case 'Escape':
        if (this.open()) {
          event.preventDefault();
          event.stopPropagation();
          this.open.set(false);
          this.active.clear();
        } else if (this.text() !== '') {
          event.preventDefault();
          event.stopPropagation();
          this.text.set('');
        }
        return;
      case 'Backspace':
        if (this.text() === '' && this.multiple() && this.value().length > 0) {
          this.value.set(this.value().slice(0, -1));
        }
        return;
      case 'Tab':
        this.open.set(false);
        this.active.clear();
        return;
    }
  }

  protected pick(match: PrincipalChoice): void {
    this.known.update((m) => new Map(m).set(match.ref, match));
    this.value.set(this.multiple() ? [...this.value().filter((r) => r !== match.ref), match.ref] : [match.ref]);
    this.picked.emit(match);
    this.text.set('');
    this.open.set(false);
    this.active.clear();
    // After a single choice the input gives way to the chip; the focus stays in the field on its remove button.
    queueMicrotask(() => {
      const target = this.input()?.nativeElement ?? this.host.nativeElement.querySelector<HTMLElement>('.chip-remove');
      target?.focus();
    });
  }

  protected remove(c: PrincipalChoice, event: Event): void {
    event.stopPropagation();
    this.value.set(this.value().filter((r) => r !== c.ref));
    queueMicrotask(() => this.focusInput());
  }

  private async run(text: string, generation: number): Promise<void> {
    const abort = new AbortController();
    this.abort = abort;
    try {
      const { items, more } = await this.search(text, abort.signal);
      if (generation !== this.generation) return;
      this.found.set(items);
      this.more.set(more);
      this.phase.set('done');
    } catch {
      if (generation !== this.generation || abort.signal.aborted) return;
      this.found.set([]);
      this.more.set(false);
      this.phase.set('failed');
    }
  }

  /**
   * The matches of the text: the people, groups and accounts from the project's lookup or the admin area's suggestions, the
   * technical tokens and CI projects from the limits' suggestions (`GET /api/v1/limits/subjects`, R-UI-179).
   */
  private async search(text: string, signal: AbortSignal): Promise<{ items: PrincipalChoice[]; more: boolean }> {
    const all = this.kinds();
    const limitKinds = all.flatMap((k) => (k === 'technical_token' ? ['technicalToken' as const] : k === 'ci_project' ? ['ci' as const] : []));
    const kinds = all.filter((k): k is DirectoryKind => k === 'user' || k === 'group' || k === 'service_account');
    const [directory, subjects] = await Promise.all([
      kinds.length > 0 ? this.searchDirectory(text, kinds, signal) : Promise.resolve({ items: [], more: false }),
      limitKinds.length > 0 ? this.searchSubjects(text, limitKinds, signal) : Promise.resolve({ items: [], more: false }),
    ]);
    return { items: [...directory.items, ...subjects.items], more: directory.more || subjects.more };
  }

  private async searchSubjects(
    text: string,
    kinds: readonly ('technicalToken' | 'ci')[],
    signal: AbortSignal,
  ): Promise<{ items: PrincipalChoice[]; more: boolean }> {
    const found = await this.limitsApi.subjects(text, kinds, this.project(), signal);
    const items = found.map((s): PrincipalChoice =>
      s.kind === 'technicalToken'
        ? { ref: refOf('technical_token', s.id), kind: 'technical_token', name: s.name, detail: s.account }
        : {
            ref: refOf('ci_project', s.id),
            kind: 'ci_project',
            name: s.projectPath || s.id,
            detail: `${s.issuer} · ${s.projectId}`,
          },
    );
    return { items, more: found.length >= 20 };
  }

  private async searchDirectory(
    text: string,
    kinds: readonly DirectoryKind[],
    signal: AbortSignal,
  ): Promise<{ items: PrincipalChoice[]; more: boolean }> {
    const key = this.project();
    if (key) {
      const found = await this.projectsApi.lookupPrincipals(key, text, kinds);
      return {
        items: found.map((p) => ({
          ref: p.principal,
          kind: p.kind,
          name: p.name,
          displayName: p.displayName ?? null,
          id: p.id ?? null,
          avatarUrl: p.avatarUrl ?? null,
          project: p.project ?? null,
        })),
        more: found.length >= 20,
      };
    }
    const answer = await this.searchApi.suggest({ q: text, kinds: kinds as readonly SuggestionKind[], limit: SUGGEST_LIMIT, signal });
    const items: PrincipalChoice[] = [];
    for (const u of answer.users?.items ?? []) {
      if (!u.disabled) items.push({ ref: refOf('user', u.username), kind: 'user', name: u.username, displayName: u.displayName, id: u.id, avatarUrl: u.avatarUrl });
    }
    for (const g of answer.groups?.items ?? []) items.push({ ref: refOf('group', g.name), kind: 'group', name: g.name });
    for (const a of answer.serviceAccounts?.items ?? []) {
      if (this.accounts() === 'all' || !a.project) {
        items.push({ ref: refOf('service_account', a.name), kind: 'service_account', name: a.name, project: a.project });
      }
    }
    const more = !!(answer.users?.more || answer.groups?.more || answer.serviceAccounts?.more);
    return { items, more };
  }
}
