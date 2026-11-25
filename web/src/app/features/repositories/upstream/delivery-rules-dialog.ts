import { Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { newIdempotencyKey } from '../../../api/client';
import { DeliveryEffect, DeliveryRule, DeliveryRuleKind, DeliveryRulesApi } from '../../../api/delivery-rules-api';
import { ApiError } from '../../../api/problem';
import { Notifier } from '../../../core/notifier';
import { TextField } from '../../../shared/access/fields';
import { ErrorNotice } from '../../../shared/error-notice';
import { fieldErrorAt } from '../../../shared/field-errors';
import { asApiError } from '../../../shared/paged-resource';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../../shared/ui/dialog';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { FailureToasts } from '../../../shared/failure-dialog';
import { Busy } from '../../../shared/ui/busy';
import { rangeLineText } from './upstream-words';

export interface DeliveryRulesData {
  readonly repository: string;
  /** The caller may add and remove rules (R-CUR-005); otherwise the list is read only. */
  readonly canChange: boolean;
  /** An example of a sub-section in the format's names: `@acme/*`, `library/*`, `lib*`. */
  readonly example: string;
}

/** The kind of a rule in words. */
export function ruleKindText(kind: DeliveryRuleKind): string {
  switch (kind) {
    case 'section':
      return $localize`:@@rules.kind.section:Sub-section`;
    case 'package':
      return $localize`:@@rules.kind.package:Package`;
    case 'version':
      return $localize`:@@rules.kind.version:Version`;
  }
}

/** The effect of a rule in words. */
export function effectText(effect: DeliveryEffect): string {
  return effect === 'allow' ? $localize`:@@rules.effect.allow:Allow` : $localize`:@@rules.effect.block:Block`;
}

/**
 * The rule list of a remote repository (remote-curation.md R-CUR-018, R-CUR-006), opened by **Rules (n)** of the tab Upstream: kind,
 * subject, effect, note, who and when, newest first, with a filter of the subject and **Remove** per rule; a field to add a
 * sub-section's rule (a name pattern) with its effect; and the sentence of the decision. Package and version rules are set where the
 * package is shown. Closes with `true` when a rule changed.
 */
@Component({
  selector: 'quader-delivery-rules-dialog',
  imports: [Busy, DataTable, DialogClose, DialogTitle, ErrorNotice, TextField, TimeAgo],
  template: `
    <h2 quaderDialogTitle i18n="@@rules.title">Delivery rules</h2>
    <div class="dialog-body">
      <p class="hint" i18n="@@rules.decides">The narrowest rule decides; at equal width a block wins. Without a rule, the mode decides.</p>
      @if (data.canChange) {
        <form class="add" (submit)="add($event)" novalidate>
          <quader-text-field
            width="name"
            i18n-label="@@rules.add.pattern"
            label="Sub-section"
            name="pattern"
            [(value)]="pattern"
            [error]="addError('/pattern')"
            [hint]="patternHint()"
          />
          <!-- The effect as two pressed buttons, as the tab Downloads' states, and the rule's Add at the row's end. -->
          <div class="add-row">
            <div class="effect" role="group" [attr.aria-label]="effectLabel">
              <button class="btn btn-default" type="button" [attr.aria-pressed]="effectChoice() === 'allow'" (click)="effectChoice.set('allow')" i18n="@@rules.effect.allow">Allow</button>
              <button class="btn btn-default" type="button" [attr.aria-pressed]="effectChoice() === 'block'" (click)="effectChoice.set('block')" i18n="@@rules.effect.block">Block</button>
            </div>
            <button class="btn btn-confirm" type="submit" [disabled]="adding() || pattern().trim() === ''" i18n="@@rules.add.submit">Add rule</button>
          </div>
          @if (otherAddError(); as e) {
            <quader-error-notice [error]="e" />
          }
        </form>
      }
      <div class="filter">
        <quader-text-field width="name" i18n-label="@@rules.filter" label="Filter rules" name="q" [value]="q()" (valueChange)="setQ($event)" />
      </div>
      <quader-data-table
        [caption]="caption"
        [columns]="columns()"
        [loaded]="loaded()"
        [loading]="loading()"
        loadingText="Loading the rules."
        i18n-loadingText="@@rules.loading"
        [error]="listError()"
        [empty]="rows().length === 0"
        [emptyText]="q() ? emptyFiltered : emptyText"
        [hasMore]="next() !== null"
        (more)="more()"
        (retry)="load(true)"
      >
        @for (r of rows(); track r.id) {
          <tr>
            <th scope="row" class="wrap-name">
              <span class="subject" translate="no">{{ r.subject }}</span>
              @if (r.range; as range) {
                <!-- The range of versions of a package's allow (R-CUR-026), in the words of the line under the package's switch. -->
                <span class="secondary" translate="no">{{ rangeText(range) }}</span>
              }
              @if (r.note) {
                <span class="secondary">{{ r.note }}</span>
              }
            </th>
            <td class="keep">{{ kindText(r.kind) }}</td>
            <td class="keep">
              @if (r.effect === 'block') {
                <span class="warn"><span class="material-icons" aria-hidden="true">block</span>{{ effect(r.effect) }}</span>
              } @else {
                <span class="badge"><span class="material-icons" aria-hidden="true">check</span>{{ effect(r.effect) }}</span>
              }
            </td>
            <td class="keep">
              @if (r.createdBy?.name; as who) {
                <span>{{ who }}</span>
              }
              <span [class.secondary]="!!r.createdBy?.name"><quader-time-ago [at]="r.createdAt" absolute="title" /></span>
            </td>
            @if (data.canChange) {
              <td class="actions">
                <button quaderBusy class="btn btn-tertiary" type="button" [disabled]="removing().has(r.id)" (click)="remove(r)">
                  <ng-container i18n="@@rules.remove">Remove</ng-container><span class="visually-hidden" translate="no">&ngsp;{{ r.subject }}</span>
                </button>
              </td>
            }
          </tr>
        }
      </quader-data-table>
    </div>
    <div class="dialog-actions">
      <button class="btn btn-default" type="button" [quaderDialogClose]="changed()" i18n="@@rules.close">Close</button>
    </div>
  `,
  styles: `
    /* The sentence of the decision is the dialog's lead, a step above the first field: as a field's hint (no room under it) it
       stood against the label Sub-section. */
    .dialog-body > .hint {
      margin-block-end: var(--quader-space-3);
    }
    .add {
      margin-block-end: var(--quader-space-4);
      padding-block-end: var(--quader-space-4);
      border-block-end: 1px solid var(--quader-border);
    }
    .add-row {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2) var(--quader-space-4);
    }
    .effect {
      display: inline-flex;
      gap: var(--quader-space-1);

      button[aria-pressed='true'] {
        background: var(--quader-accent-container);
        color: var(--quader-on-accent-container);
      }
    }
    .filter {
      max-inline-size: var(--quader-field-width-name);
    }
    .subject {
      font-weight: 600;
      overflow-wrap: anywhere;
    }
    th .secondary {
      overflow-wrap: anywhere;
    }
    td.keep {
      white-space: nowrap;
    }
  `,
})
export class DeliveryRulesDialog {
  private readonly api = inject(DeliveryRulesApi);
  private readonly notifier = inject(Notifier);
  private readonly failureToasts = inject(FailureToasts);
  protected readonly data = inject<DeliveryRulesData>(DIALOG_DATA);
  // The reference keeps the dialog typed; the result is the changed flag of `quaderDialogClose`.
  protected readonly ref = inject<DialogRef<boolean, DeliveryRulesDialog>>(DialogRef);

  protected readonly rows = signal<readonly DeliveryRule[]>([]);
  protected readonly next = signal<string | null>(null);
  protected readonly loaded = signal(false);
  /** The filter the rows shown answer. */
  private shownQ = '';
  protected readonly loading = signal(false);
  protected readonly listError = signal<ApiError | null>(null);
  protected readonly q = signal('');
  protected readonly pattern = signal('');
  protected readonly effectChoice = signal<DeliveryEffect>('block');
  protected readonly adding = signal(false);
  protected readonly addFailure = signal<ApiError | null>(null);
  protected readonly removing = signal<ReadonlySet<string>>(new Set());
  protected readonly changed = signal(false);
  private generation = 0;

  protected readonly kindText = ruleKindText;
  protected readonly rangeText = rangeLineText;
  protected readonly effect = effectText;
  protected readonly caption = $localize`:@@rules.caption:Delivery rules of this repository, newest first`;
  protected readonly emptyText = $localize`:@@rules.empty:There is no rule yet.`;
  protected readonly emptyFiltered = $localize`:@@rules.empty-filtered:No rule matches.`;
  protected readonly effectLabel = $localize`:@@rules.add.effect:Effect`;
  protected readonly patternHint = computed(() => $localize`:@@rules.add.pattern-hint:A name pattern, such as ${this.data.example}:example:.`);
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'subject', label: $localize`:@@rules.col.subject:Subject`, width: '12rem', prefer: true },
    { id: 'kind', label: $localize`:@@rules.col.kind:Kind` },
    { id: 'effect', label: $localize`:@@rules.col.effect:Effect` },
    { id: 'by', label: $localize`:@@rules.col.by:Set by` },
    ...(this.data.canChange ? [{ id: 'actions', label: $localize`:@@rules.col.actions:Actions`, hideLabel: true }] : []),
  ]);
  protected readonly otherAddError = computed(() => {
    const e = this.addFailure();
    return e && e.fieldErrors.size === 0 ? e : null;
  });

  constructor() {
    effect(() => {
      this.q();
      untracked(() => void this.load(true));
    });
  }

  protected addError(pointer: string): string | null {
    return fieldErrorAt(this.addFailure(), pointer);
  }

  protected setQ(value: string): void {
    this.q.set(value.trim());
  }

  async load(fresh: boolean): Promise<void> {
    const generation = fresh ? ++this.generation : this.generation;
    const cursor = fresh ? null : this.next();
    // Another filter is a new question: its load shows, not the rules of the filter before (R-UI-217, Q-UI-1234); a read after a
    // write keeps the rows while it reads.
    if (fresh && this.q() !== this.shownQ) {
      this.rows.set([]);
      this.loaded.set(false);
    }
    this.shownQ = this.q();
    this.loading.set(true);
    try {
      const page = await this.api.list(this.data.repository, { q: this.q() || undefined }, cursor);
      if (generation !== this.generation) return;
      this.rows.update((rows) => (fresh ? page.items : [...rows, ...page.items]));
      this.next.set(page.nextCursor);
      this.listError.set(null);
      this.loaded.set(true);
    } catch (e) {
      if (generation === this.generation) this.listError.set(asApiError(e));
    } finally {
      if (generation === this.generation) this.loading.set(false);
    }
  }

  protected more(): void {
    if (this.next() !== null && !this.loading()) void this.load(false);
  }

  protected async add(event: Event): Promise<void> {
    event.preventDefault();
    const pattern = this.pattern().trim();
    if (pattern === '' || this.adding()) return;
    this.adding.set(true);
    this.addFailure.set(null);
    try {
      await this.api.put(this.data.repository, { kind: 'section', pattern, effect: this.effectChoice() }, newIdempotencyKey());
      this.notifier.status(
        this.effectChoice() === 'allow'
          ? $localize`:@@rules.added-allow:The sub-section ${pattern}:pattern: is allowed.`
          : $localize`:@@rules.added-block:The sub-section ${pattern}:pattern: is blocked.`,
      );
      this.pattern.set('');
      this.changed.set(true);
      await this.load(true);
    } catch (e) {
      this.addFailure.set(asApiError(e));
    } finally {
      this.adding.set(false);
    }
  }

  protected async remove(r: DeliveryRule): Promise<void> {
    if (this.removing().has(r.id)) return;
    this.removing.update((s) => new Set([...s, r.id]));
    try {
      await this.api.remove(this.data.repository, r.id);
      this.rows.update((rows) => rows.filter((x) => x.id !== r.id));
      this.changed.set(true);
      this.notifier.status($localize`:@@upstream.rule.removed:The rule for ${r.subject}:subject: is removed.`);
    } catch (e) {
      this.failureToasts.alert(e);
    } finally {
      this.removing.update((s) => {
        const next = new Set(s);
        next.delete(r.id);
        return next;
      });
    }
  }
}
