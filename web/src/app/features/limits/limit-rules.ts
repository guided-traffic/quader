import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { Figure, LimitPlace, LimitProfile, LimitRule, LimitsApi } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { Confirm } from '../../shared/confirm-dialog';
import { asApiError } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Tooltip } from '../../shared/ui/tooltip';
import { CallerCell } from './caller-cell';
import { whoRank, whoSortName, whoView } from './callers';
import { FIGURES, figureName, limitText, overridesSummary } from './figures';
import { LimitRuleDialog, RuleDialogData } from './rule-dialog';
import { Busy } from '../../shared/ui/busy';

/** The rules in the order resolution tries them (R-LIM-013, Q-UI-751): single callers and addresses, groups, kinds; by name within. */
export function orderRules(rules: readonly LimitRule[]): LimitRule[] {
  return [...rules].sort((a, b) => whoRank(a.who) - whoRank(b.who) || whoSortName(a.who).localeCompare(whoSortName(b.who)));
}

/** The order of the five kind rules a Bastion's preset writes (R-LIM-046). */
const PRESET_ORDER: readonly string[] = ['anonymous', 'user', 'technicalToken', 'ci', 'administrator'];

/**
 * The rules of a Bastion's place (R-UI-241): the five kind rules of the preset first, in the preset's order, then the others as
 * resolution tries them.
 */
export function orderBastionRules(rules: readonly LimitRule[]): LimitRule[] {
  const rank = (r: LimitRule): number => (r.builtin && r.who.kind === 'every' ? PRESET_ORDER.indexOf(r.who.callerKind) : PRESET_ORDER.length);
  const preset = rules.filter((r) => rank(r) < PRESET_ORDER.length).sort((a, b) => rank(a) - rank(b));
  return [...preset, ...orderRules(rules.filter((r) => rank(r) === PRESET_ORDER.length))];
}

/**
 * The rules of one place (R-UI-179), the same table at the instance, a project, a repository and a Bastion: who it applies to, the
 * profile with its changed figures, the note. The name opens the rule's dialog where the caller may change it, and **Delete** is the
 * row's one action; a built-in rule has none (Q-UI-758). At a Bastion's place the five kind rules its preset wrote come first, marked
 * **From the preset** (R-UI-241). A capped rule whose
 * figures are clamped now carries **Above the maximum** with what applies. **Add rule** is the parent's: it calls `add()`.
 */
@Component({
  selector: 'quader-limit-rules',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Busy, CallerCell, DataTable, Tooltip],
  template: `
    <quader-data-table
      [caption]="caption()"
      [columns]="columns()"
      [loaded]="rules() !== null"
      [loading]="rules() === null && !failure()"
      loadingText="Loading the rules."
      i18n-loadingText="@@limits.rules.loading"
      [error]="failure()"
      (retry)="load()"
      [empty]="(rules() ?? []).length === 0"
      emptyText="No rule here. Callers get the instance's limits."
      i18n-emptyText="@@limits.rules.empty"
    >
      @for (r of ordered(); track r.id) {
        <tr>
          <th scope="row" class="wrap-name">
            <quader-caller-cell [view]="view(r)" [actionable]="canWrite()" (activate)="change(r)">
              @if (r.builtin) {
                @if (place().kind === 'bastion') {
                  <span class="badge" i18n="@@limits.rules.from-preset">From the preset</span>
                } @else {
                  <span class="badge" i18n="@@limits.rules.builtin">Built-in</span>
                }
              }
            </quader-caller-cell>
            <!-- The note under whom the rule applies to (Q-UI-1001): the list keeps its table in a settings column at 1,024 px. -->
            @if (r.note) {
              <span class="secondary rule-note">{{ r.note }}</span>
            }
          </th>
          <td>
            <span class="profile">
              <span>{{ profileName(r.profile) }}</span>
              @if (clampedText(r); as clamp) {
                <span class="badge warn" tabindex="0" [quaderTooltip]="clamp"
                  ><span class="material-icons" aria-hidden="true">warning</span
                  ><ng-container i18n="@@limits.rules.above-maximum">Above the maximum</ng-container></span
                >
              }
            </span>
            @for (o of overrides(r); track o) {
              <span class="override">{{ o }}</span>
            }
          </td>
          @if (canWrite()) {
            <td class="actions">
              @if (!r.builtin) {
                <button quaderBusy class="btn btn-tertiary" type="button" (click)="remove(r)">
                  <ng-container i18n="@@limits.rules.delete">Delete</ng-container>
                  <span class="visually-hidden"> {{ view(r).primary }}</span>
                </button>
              }
            </td>
          }
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    :host {
      display: block;
    }
    /*
     * Running text, so that a stacked row's "Profile" stands on the profile's first line, also where a long name wraps (Q-UI-762):
     * as a box of its own the name stood under its column's name.
     */
    .profile > .badge {
      margin-inline-start: var(--quader-space-2);
    }
    .rule-note {
      display: block;
      font-weight: 400;
    }
    .override {
      display: block;
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
    }
  `,
})
export class LimitRules {
  private readonly api = inject(LimitsApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly confirmer = inject(Confirm);
  private readonly notifier = inject(Notifier);
  private readonly locale = inject(LOCALE_ID);

  readonly place = input.required<LimitPlace>();
  /** The caller may add, change and delete rules here: `limits:write`, or an Owner of the place's project. */
  readonly canWrite = input(false);
  /** The caller writes without `limits:write`: its rules are capped (R-LIM-016). */
  readonly capped = input(false);
  /** The project's maximum's profile name, for the dialog's line of the cap. */
  readonly maximum = input<string | null>(null);
  /** The number of rules, after each load. */
  readonly counted = output<number>();

  protected readonly rules = signal<readonly LimitRule[] | null>(null);
  protected readonly profiles = signal<readonly LimitProfile[]>([]);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly ordered = computed(() =>
    this.place().kind === 'bastion' ? orderBastionRules(this.rules() ?? []) : orderRules(this.rules() ?? []),
  );

  protected readonly caption = computed(() => {
    const place = this.place();
    switch (place.kind) {
      case 'instance':
        return $localize`:@@limits.rules.caption-instance:Rules of the instance`;
      case 'project':
        return $localize`:@@limits.rules.caption-project:Rules of project ${place.project}:project:`;
      case 'repository':
        return $localize`:@@limits.rules.caption-repository:Rules of ${place.repository}:repository:`;
      case 'bastion':
        return $localize`:@@limits.rules.caption-bastion:Rules of Bastion ${place.bastion}:bastion:`;
    }
  });

  protected readonly columns = computed<readonly TableColumn[]>(() => [
    // One form at 1,440 and 1,024 px (Q-UI-1001, the rule of Q-UI-969): a rule's note stands under whom it applies to.
    { id: 'who', label: $localize`:@@limits.rules.col-who:Applies to`, width: '14rem' },
    { id: 'profile', label: $localize`:@@limits.rules.col-profile:Profile`, width: '10rem' },
    ...(this.canWrite() ? [{ id: 'actions', label: $localize`:@@limits.rules.col-actions:Actions`, hideLabel: true }] : []),
  ]);

  constructor() {
    effect(() => {
      this.place();
      untracked(() => void this.load());
    });
  }

  async load(): Promise<void> {
    try {
      const [rules, profiles] = await Promise.all([this.api.rules(this.place()), this.api.profiles().catch((e: unknown) => asApiError(e))]);
      // The failure stays until the answer, so the notice stays in place while it reads again (R-UI-216). A failed read of the
      // profiles is shown beside the rules, whose profile then stands by its key, and the notice's Retry reads it again.
      this.failure.set(profiles instanceof ApiError ? profiles : null);
      this.profiles.set(profiles instanceof ApiError ? [] : profiles);
      this.rules.set(rules);
      this.counted.emit(rules.length);
    } catch (e) {
      this.rules.set(null);
      this.failure.set(asApiError(e));
    }
  }

  /** **Add rule** of the place's head. */
  async add(): Promise<void> {
    await this.open({ place: this.place(), capped: this.capped(), maximum: this.maximum() });
  }

  protected view(r: LimitRule) {
    return whoView(r.who);
  }

  protected profileName(key: string): string {
    return this.profiles().find((p) => p.key === key)?.name ?? key;
  }

  protected overrides(r: LimitRule): string[] {
    return overridesSummary(r.overrides, this.locale);
  }

  /** The tooltip of **Above the maximum**: each clamped figure and what applies now. */
  protected clampedText(r: LimitRule): string | null {
    const figures = FIGURES.filter((f) => f in r.clamped);
    if (figures.length === 0) return null;
    const parts = figures.map((f: Figure) => `${figureName(f)}: ${limitText(f, r.clamped[f] ?? null, this.locale)}`);
    return $localize`:@@limits.rules.clamped:Lowered to what the instance or the project's maximum allows: ${parts.join('; ')}:figures:.`;
  }

  protected async change(r: LimitRule): Promise<void> {
    await this.open({ place: this.place(), rule: r, capped: this.capped(), maximum: this.maximum() });
  }

  private async open(data: RuleDialogData): Promise<void> {
    const ref = this.dialog.open<LimitRuleDialog, RuleDialogData, unknown>(LimitRuleDialog, { data, maxWidth: '44rem', width: '44rem' });
    const result = await firstValueFrom(ref.closed);
    if (result) {
      this.notifier.status($localize`:@@limits.rules.saved:Rule saved.`);
      await this.load();
    }
  }

  protected async remove(r: LimitRule): Promise<void> {
    const name = this.view(r).primary;
    const answer = await this.confirmer.ask({
      title: $localize`:@@limits.rules.delete-title:Delete the rule for ${name}:name:?`,
      lines: [$localize`:@@limits.rules.delete-text:The caller gets the limits of the next place or of its kind from the next request on.`],
      confirmLabel: $localize`:@@limits.rules.delete-confirm:Delete rule`,
    });
    if (answer === null) return;
    try {
      const stored = await this.api.rule(this.place(), r.id);
      await this.api.deleteRule(this.place(), r.id, stored.etag);
      this.notifier.status($localize`:@@limits.rules.deleted:Rule deleted.`);
      await this.load();
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }
}
