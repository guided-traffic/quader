import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, signal } from '@angular/core';
import { Figure, LimitProfile, LimitsApi, ProjectMaximum } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Versioned } from '../../api/repositories-api';
import { SelectField, SelectOption } from '../../shared/access/fields';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';
import { Disclosure } from '../../shared/ui/disclosure';
import { FigureFields, FigureTexts, readTexts, textsOf } from './figure-fields';
import { FIGURES, compactFigures } from './figures';
import { overridesOf } from './rule-dialog';

export interface MaximumDialogData {
  /** The project; absent for **Add maximum**, which asks for it first. */
  readonly project?: string;
  readonly maximum: Versioned<ProjectMaximum> | null;
  /** The projects **Add maximum** offers: key and display name. */
  readonly projects?: readonly { readonly key: string; readonly displayName: string }[];
}

/**
 * A project's maximum (R-UI-177, R-LIM-016): the profile and its single figures, as the rule dialog has them; the project first
 * where it is a new one. `PUT` sets or replaces it.
 */
@Component({
  selector: 'quader-limit-maximum-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DialogClose, DialogTitle, Disclosure, ErrorNotice, FigureFields, SelectField],
  template: `
    @if (data.maximum) {
      <h2 quaderDialogTitle i18n="@@limits.maximum.change-title">Change maximum</h2>
    } @else {
      <h2 quaderDialogTitle i18n="@@limits.maximum.set-title">Set maximum</h2>
    }
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (failure(); as e) {
          <quader-error-notice [error]="e" />
        }
        <p class="hint" i18n="@@limits.maximum.hint">The project's Owners can set limits up to this maximum, where it is above the instance's values.</p>
        @if (!data.project) {
          <quader-select-field
            i18n-label="@@limits.maximum.project"
            label="Project"
            name="project"
            [options]="projectOptions"
            [(value)]="project"
            [error]="projectError()"
          />
        }
        <quader-select-field
          i18n-label="@@limits.rule.profile"
          label="Profile"
          name="profile"
          [options]="profileOptions()"
          [loading]="profiles() === null ? loadingProfiles : null"
          [value]="profileKey()"
          (valueChange)="chooseProfile($event)"
        />
        @if (profile(); as p) {
          <p class="figures">{{ compact(p) }}</p>
          <quader-disclosure [inline]="true" i18n-heading="@@limits.rule.single" heading="Change single figures" [(expanded)]="singleOpen">
            <quader-figure-fields [(texts)]="texts" [base]="p.figures" [(overridden)]="overridden" [errors]="errors()" />
          </quader-disclosure>
        }
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@limits.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy() || !profileKey()" i18n="@@limits.maximum.save">Save maximum</button>
      </div>
    </form>
  `,
  styles: `
    .hint {
      margin: 0 0 var(--quader-space-3);
    }
    .figures {
      margin: calc(-1 * var(--quader-space-2)) 0 var(--quader-space-3);
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
    }
  `,
})
export class LimitMaximumDialog {
  protected readonly data = inject<MaximumDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<Versioned<ProjectMaximum>>>(DialogRef);
  private readonly api = inject(LimitsApi);
  private readonly locale = inject(LOCALE_ID);

  protected readonly loadingProfiles = $localize`:@@limits.rule.loading-profiles:Loading profiles…`;
  protected readonly project = signal(this.data.project ?? '');
  protected readonly projectOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@limits.maximum.choose-project:Choose a project` },
    ...(this.data.projects ?? []).map((p) => ({ value: p.key, label: p.displayName ? `${p.displayName} (${p.key})` : p.key })),
  ];
  protected readonly profiles = signal<readonly LimitProfile[] | null>(null);
  protected readonly profileKey = signal(this.data.maximum?.value.profile ?? '');
  protected readonly profile = computed(() => this.profiles()?.find((p) => p.key === this.profileKey()) ?? null);
  protected readonly profileOptions = computed<readonly SelectOption[]>(() => [
    ...(this.profileKey() ? [] : [{ value: '', label: $localize`:@@limits.rule.choose-profile:Choose a profile` }]),
    ...(this.profiles() ?? []).map((p) => ({ value: p.key, label: p.name })),
  ]);
  protected readonly texts = signal<FigureTexts>(textsOf(null));
  protected readonly overridden = signal<ReadonlySet<Figure>>(new Set());
  protected readonly singleOpen = signal(Object.keys(this.data.maximum?.value.overrides ?? {}).length > 0);
  protected readonly attempted = signal(false);
  protected readonly busy = signal(false);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly errors = computed(() => {
    const read = readTexts(this.texts());
    return Object.fromEntries(FIGURES.filter((f) => this.overridden().has(f) && read.errors[f]).map((f) => [f, read.errors[f]!]));
  });

  constructor() {
    void this.loadProfiles();
  }

  private async loadProfiles(): Promise<void> {
    try {
      const list = await this.api.profiles();
      this.profiles.set(list);
      const start = list.find((p) => p.key === this.profileKey());
      const overrides = this.data.maximum?.value.overrides ?? {};
      if (start) {
        this.texts.set(textsOf({ ...start.figures, ...overrides }));
        this.overridden.set(new Set(FIGURES.filter((f) => f in overrides)));
      }
    } catch (e) {
      // The select stops saying that it loads; the notice says why it is empty.
      this.profiles.set([]);
      this.failure.set(asApiError(e));
    }
  }

  protected chooseProfile(key: string): void {
    this.profileKey.set(key);
    const p = this.profiles()?.find((x) => x.key === key);
    if (!p) return;
    const texts = { ...textsOf(p.figures) } as Record<Figure, string>;
    for (const f of this.overridden()) texts[f] = this.texts()[f];
    this.texts.set(texts as FigureTexts);
  }

  protected compact(p: LimitProfile): string {
    return compactFigures(p.figures, this.locale);
  }

  protected projectError(): string | null {
    return this.attempted() && !this.project() ? $localize`:@@limits.maximum.project-required:Choose a project.` : null;
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    this.attempted.set(true);
    const profile = this.profile();
    const read = readTexts(this.texts());
    if (!profile || !this.project() || Object.keys(this.errors()).length > 0) return;
    this.busy.set(true);
    this.failure.set(null);
    try {
      const write = { profile: profile.key, overrides: overridesOf(read.figures, this.overridden(), profile.figures) };
      this.ref.close(await this.api.putMaximum(this.project(), write, this.data.maximum?.etag ?? null));
    } catch (e) {
      this.failure.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
