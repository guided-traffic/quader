import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input, model } from '@angular/core';
import { Figure, FigureOverrides, Figures } from '../../api/limits-api';
import { FIGURES, fieldLabel, fieldWidth, figureGroups, fromField, limitText, toField } from './figures';

let nextId = 1;

/** The texts of the eleven fields, by figure. */
export type FigureTexts = Readonly<Record<Figure, string>>;

/** Every field empty. */
export function emptyTexts(): FigureTexts {
  return Object.fromEntries(FIGURES.map((f) => [f, ''])) as FigureTexts;
}

/** The fields of a set of figures: each value in its unit, an unlimited one empty. */
export function textsOf(figures: Figures | FigureOverrides | null | undefined): FigureTexts {
  return Object.fromEntries(FIGURES.map((f) => [f, toField(f, figures?.[f] ?? null)])) as FigureTexts;
}

/** The figures the fields hold, or the errors of the fields that hold no value. */
export function readTexts(texts: FigureTexts): { figures: Record<Figure, number | null>; errors: Partial<Record<Figure, string>> } {
  const figures = {} as Record<Figure, number | null>;
  const errors: Partial<Record<Figure, string>> = {};
  for (const f of FIGURES) {
    const read = fromField(f, texts[f]);
    if (read.ok) figures[f] = read.value;
    else {
      errors[f] = read.error;
      figures[f] = null;
    }
  }
  // R-LIM-004: a burst needs the rate, and is at least the rate.
  const rate = figures.requestsPerSecond;
  const burst = figures.requestsBurst;
  if (!errors.requestsBurst && burst !== null) {
    if (rate === null) errors.requestsBurst = $localize`:@@limits.field.burst-needs-rate:A burst needs requests per second.`;
    else if (burst < rate) errors.requestsBurst = $localize`:@@limits.field.burst-below-rate:The burst is at least the requests per second.`;
  }
  return { figures, errors };
}

/**
 * The eleven fields of a profile in the groups of R-UI-178 (Requests, From the cache, From the upstream, Transfers, Refused
 * requests): a heading, the help line of what happens at the limit, and the fields, each as wide as its number (Q-UI-335). An empty
 * field is unlimited and says so as its placeholder. The value is in the field.
 *
 * With `base` (the rule dialog, a project's maximum) the fields start with the chosen profile's values; a field whose value the
 * person changed is an override and offers **Use the profile's value**, which puts the profile's value back (Q-UI-752).
 */
@Component({
  selector: 'quader-figure-fields',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @for (g of groups; track g.id) {
      <fieldset class="group" [attr.aria-describedby]="id + '-' + g.id + '-help'">
        <legend class="group-heading" [id]="id + '-' + g.id">{{ g.heading }}</legend>
        <p class="hint" [id]="id + '-' + g.id + '-help'">{{ g.help }}</p>
        @if (g.id === 'cache' && cacheNote()) {
          <p class="hint" i18n="@@limits.group.cache-note">
            A very low rate makes a large file take longer than some clients wait for a whole download.
          </p>
        }
        <div class="fields">
          @for (f of g.figures; track f) {
            <div class="field">
              <label class="field-label" [id]="id + '-' + f + '-label'" [for]="id + '-' + f">{{ label(f) }}</label>
              <input
                class="text-input"
                [class]="'field-w-' + width(f)"
                type="text"
                inputmode="decimal"
                autocomplete="off"
                [id]="id + '-' + f"
                [name]="f"
                [attr.aria-labelledby]="id + '-' + g.id + ' ' + id + '-' + f + '-label'"
                [value]="texts()[f]"
                [readonly]="readonly()"
                placeholder="Unlimited"
                i18n-placeholder="@@limits.unlimited"
                [attr.aria-invalid]="errorOf(f) ? 'true' : null"
                [attr.aria-describedby]="describedBy(f)"
                (input)="set(f, $any($event.target).value)"
              />
              @if (errorOf(f); as e) {
                <p class="field-error" [id]="id + '-' + f + '-error'">{{ e }}</p>
              }
              @if (base() && isOverridden(f) && !readonly()) {
                <button class="reset" type="button" (click)="reset(f)" [id]="id + '-' + f + '-reset'">
                  <ng-container i18n="@@limits.use-profile-value">Use the profile's value</ng-container>
                  <span class="visually-hidden"> ({{ baseText(f) }})</span>
                </button>
              }
            </div>
          }
        </div>
      </fieldset>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    .group {
      margin: 0 0 var(--quader-space-4);
      padding: 0;
      border: 0;
      min-inline-size: 0;
    }
    .group-heading {
      padding: 0;
      font-weight: 600;
      color: var(--quader-heading);
    }
    .hint {
      margin: var(--quader-space-1) 0 0;
    }
    .fields {
      display: flex;
      flex-wrap: wrap;
      gap: 0 var(--quader-space-5);
      margin-block-start: var(--quader-space-2);
    }
    .field {
      display: flex;
      flex-direction: column;
      align-items: flex-start;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-2);
    }
    .field-error {
      margin: 0;
      color: var(--quader-status-bad);
      font: var(--quader-type-body-small);
      max-inline-size: 16rem;
    }
    .reset {
      padding: 0;
      border: 0;
      background: none;
      color: var(--quader-link);
      font: var(--quader-type-body-small);
      text-decoration: underline;
      cursor: pointer;
    }
    .reset:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
  `,
})
export class FigureFields {
  private readonly locale = inject(LOCALE_ID);

  readonly texts = model.required<FigureTexts>();
  /** The chosen profile's figures where the fields are overrides of it; null where they are a profile's own. */
  readonly base = input<Figures | null>(null);
  /** The figures the person changed against `base`. */
  readonly overridden = model<ReadonlySet<Figure>>(new Set());
  readonly readonly = input(false);
  /** A message per figure: a field that holds no value, or a cap the server named (R-UI-179). */
  readonly errors = input<Partial<Record<Figure, string>>>({});
  /** The note under From the cache that a very low rate fails some clients (R-UI-178). */
  readonly cacheNote = input(false);

  protected readonly id = `quader-figures-${nextId++}`;
  protected readonly groups = figureGroups();
  private readonly overriddenSet = computed(() => this.overridden());

  protected label(f: Figure): string {
    return fieldLabel(f);
  }

  protected width(f: Figure): string {
    return fieldWidth(f);
  }

  protected errorOf(f: Figure): string | null {
    return this.errors()[f] ?? null;
  }

  protected isOverridden(f: Figure): boolean {
    return this.overriddenSet().has(f);
  }

  protected baseText(f: Figure): string {
    return limitText(f, this.base()?.[f] ?? null, this.locale);
  }

  protected describedBy(f: Figure): string | null {
    const ids = [this.errorOf(f) ? `${this.id}-${f}-error` : ''].filter(Boolean);
    return ids.length ? ids.join(' ') : null;
  }

  protected set(f: Figure, value: string): void {
    this.texts.set({ ...this.texts(), [f]: value });
    const base = this.base();
    if (!base) return;
    const read = fromField(f, value);
    const same = read.ok && read.value === (base[f] ?? null);
    const next = new Set(this.overridden());
    if (same) next.delete(f);
    else next.add(f);
    this.overridden.set(next);
  }

  protected reset(f: Figure): void {
    this.texts.set({ ...this.texts(), [f]: toField(f, this.base()?.[f] ?? null) });
    const next = new Set(this.overridden());
    next.delete(f);
    this.overridden.set(next);
  }
}
