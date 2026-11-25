import { Component, computed, inject, resource, signal } from '@angular/core';
import { DeliveryRulesApi, DeliveryRulesSummary } from '../../api/delivery-rules-api';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { Checkbox } from '../../shared/ui/checkbox';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';
import { groupedCount } from './upstream/upstream-package';
import { DeliveryMode } from './upstream/upstream-words';

export interface DeliveryModeData {
  /** The qualified name `<project>/<key>` of the remote repository. */
  readonly repository: string;
  /** The mode the change leads to. */
  readonly to: DeliveryMode;
}

/** The answer of the dialog: the change is to be saved, with the stored packages allowed first or not. */
export interface DeliveryModeChoice {
  readonly allowStored: boolean;
}

/** The most names the dialog writes out; the rest is their count (one scrollbar per page, R-UI-215). */
export const SHOWN_NAMES = 6;

/**
 * A change of the delivery mode says what follows before it is saved (remote-curation.md R-CUR-025). Towards **Only what is allowed**
 * it names the packages the repository stores whose package level the mode decides today and offers, ticked, to allow them with every
 * version first, so that they stay delivered (Q-CUR-204); where the repository could not hold that many more rules the offer is not
 * made and the dialog says so; a caller who may set the configuration but not the rules reads the list without the offer (Q-CUR-206).
 * Towards **Everything except what is blocked** it says that every package no rule blocks is delivered and that the allow rules stay
 * (Q-CUR-209). Closes with the choice, or without a result when cancelled.
 */
@Component({
  selector: 'quader-delivery-mode-dialog',
  imports: [Checkbox, DialogClose, DialogTitle, ErrorNotice],
  template: `
    @if (data.to === 'onlyAllowed') {
      <h2 quaderDialogTitle i18n="@@delivery.change.only-title">Deliver only what is allowed?</h2>
    } @else {
      <h2 quaderDialogTitle i18n="@@delivery.change.all-title">Deliver everything except what is blocked?</h2>
    }
    <div class="dialog-body" data-testid="delivery-mode-dialog">
      @if (failure(); as f) {
        <quader-error-notice [error]="f" (retry)="failure.retry()" />
      } @else if (!summary()) {
        <p role="status" i18n="@@delivery.change.reading">Reading what the repository stores.</p>
      }
      @if (summary(); as s) {
        @if (data.to === 'onlyAllowed') {
          <p i18n="@@delivery.change.only-what">From then on a package or a version is delivered only where a rule allows it.</p>
          @if (s.unruled.count === 0) {
            <p data-testid="delivery-mode-none" i18n="@@delivery.change.only-none">No package the repository stores is delivered by the mode alone, so none of them is refused after the change.</p>
          } @else {
            <p data-testid="delivery-mode-names">{{ unruledText() }}</p>
            @if (offer()) {
              <quader-checkbox [checked]="allow()" (checkedChange)="allow.set($event)" describedBy="delivery-mode-offer-hint">{{ offerText() }}</quader-checkbox>
              <p class="hint" id="delivery-mode-offer-hint" i18n="@@delivery.change.offer-hint">One allow rule each, written before the mode, so that no client is refused in between. Each can be switched off on the tab Upstream.</p>
            } @else if (!mayWriteRules()) {
              <p class="hint" data-testid="delivery-mode-no-offer" i18n="@@delivery.change.no-right">Only those who manage the repository may allow them. Ask them before you change the mode, or allow the packages afterwards.</p>
            } @else {
              <p class="hint" data-testid="delivery-mode-beyond" i18n="@@delivery.change.beyond">The repository may hold {{ roomText() }} more rules (proxy.delivery.max_rules), too few to allow every one of them. Allow the packages you need on the tab Upstream, before or after the change.</p>
            }
          }
        } @else {
          <p i18n="@@delivery.change.all-what">From then on every package and every version that no rule blocks is delivered, also those no rule allows.</p>
          @if (s.rules.allow > 0) {
            <p data-testid="delivery-mode-allows">{{ allowsText() }}</p>
          }
        }
      }
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@upstream.dialog.cancel">Cancel</button>
      <button class="btn btn-confirm" type="button" [disabled]="!ready()" (click)="confirm()" i18n="@@delivery.change.confirm">Change the mode</button>
    </div>
  `,
  styles: `
    .dialog-body p {
      margin-block: 0 var(--quader-space-3);
    }
    .hint {
      margin-block-start: var(--quader-space-1);
    }
  `,
})
export class DeliveryModeDialog {
  protected readonly data = inject<DeliveryModeData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<DeliveryModeChoice, DeliveryModeDialog>>(DialogRef);
  private readonly rules = inject(DeliveryRulesApi);
  private readonly session = inject(SessionStore);

  /** The summary and whether the caller may write rules (`manage`, or the role admin), read together. */
  protected readonly read = resource({
    loader: async () => {
      const [summary, rights] = await Promise.all([
        this.rules.summary(this.data.repository),
        this.session.check([{ repository: this.data.repository, action: 'manage' }]),
      ]);
      return { summary, manage: !!rights[0] || this.session.hasRole('admin') };
    },
  });
  protected readonly summary = computed<DeliveryRulesSummary | undefined>(() => valueOf(this.read)?.summary);
  protected readonly mayWriteRules = computed(() => valueOf(this.read)?.manage ?? false);
  protected readonly failure = failureOf(this.read);

  /** The rules the repository may still hold. */
  protected readonly room = computed(() => {
    const s = this.summary();
    return s ? Math.max(0, s.maxRules - s.rules.allow - s.rules.block - s.rules.sections) : 0;
  });
  protected readonly roomText = computed(() => groupedCount(this.room()));
  /** The offer is made where the caller may write rules and the repository can hold one for each package. */
  protected readonly offer = computed(() => {
    const s = this.summary();
    return !!s && this.mayWriteRules() && s.unruled.counted && s.unruled.count <= this.room();
  });
  protected readonly allow = signal(true);

  /** The change may be confirmed once the summary is read; towards allExceptBlocked also when it could not be read. */
  protected readonly ready = computed(() => !!this.summary() || (this.data.to === 'allExceptBlocked' && !!this.failure()));

  protected readonly unruledText = computed(() => {
    const s = this.summary();
    if (!s) return '';
    const shown = s.unruled.names.slice(0, SHOWN_NAMES);
    const names = shown.join(', ');
    const more = s.unruled.count - shown.length;
    const count = groupedCount(s.unruled.count);
    if (!s.unruled.counted) {
      // The count stopped at the bound of the rules plus one: more than the repository could allow, and no exact number.
      return $localize`:@@delivery.change.unruled-uncounted:The repository stores more than ${groupedCount(s.maxRules)}:max: packages that no rule allows or blocks, among them ${names}:names:. They are delivered today because of the mode and refused after the change.`;
    }
    if (s.unruled.count === 1) {
      return $localize`:@@delivery.change.unruled-one:The repository stores one package that no rule allows or blocks, ${names}:names:. It is delivered today because of the mode and refused after the change.`;
    }
    return more > 0
      ? $localize`:@@delivery.change.unruled-more:The repository stores ${count}:count: packages that no rule allows or blocks: ${names}:names: and ${groupedCount(more)}:more: more. They are delivered today because of the mode and refused after the change.`
      : $localize`:@@delivery.change.unruled:The repository stores ${count}:count: packages that no rule allows or blocks: ${names}:names:. They are delivered today because of the mode and refused after the change.`;
  });

  protected readonly offerText = computed(() => {
    const n = this.summary()?.unruled.count ?? 0;
    return n === 1
      ? $localize`:@@delivery.change.offer-one:Allow this package with every version, so that it stays delivered`
      : $localize`:@@delivery.change.offer:Allow these ${groupedCount(n)}:count: packages with every version, so that they stay delivered`;
  });

  protected readonly allowsText = computed(() => {
    const n = this.summary()?.rules.allow ?? 0;
    return n === 1
      ? $localize`:@@delivery.change.allows-one:The one allow rule stays; it decides again when the repository delivers only what is allowed once more.`
      : $localize`:@@delivery.change.allows:The ${groupedCount(n)}:count: allow rules stay; they decide again when the repository delivers only what is allowed once more.`;
  });

  protected confirm(): void {
    if (!this.ready()) return;
    this.ref.close({ allowStored: this.data.to === 'onlyAllowed' && this.offer() && this.allow() });
  }
}
