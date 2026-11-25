import { Component, computed, inject, signal } from '@angular/core';
import { newIdempotencyKey } from '../../../api/client';
import { DeliveryRulesApi, DeliverySwitchResult } from '../../../api/delivery-rules-api';
import { ApiError } from '../../../api/problem';
import { ErrorNotice } from '../../../shared/error-notice';
import { asApiError } from '../../../shared/paged-resource';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../../shared/ui/dialog';
import { rangeErrorText } from './download-preview-dialog';
import { DeliveryRange } from './upstream-words';

export interface VersionRangeData {
  readonly repository: string;
  readonly name: string;
  /** The repository's format: an OCI image says what a tag that is no version does under a range. */
  readonly format: string;
  /** The range the package's allow has; null for every version or no allow. */
  readonly range: DeliveryRange | null;
  /** The package is delivered (its switch is on); saving turns it on otherwise. */
  readonly delivered: boolean;
  /** The versions the opened package lists, offered in both fields; any version can be typed. */
  readonly versions: readonly string[];
}

/** The pointers of the range in the switch's body (R-CUR-026). */
const AT = '/range';

/**
 * **Limit versions…** of a package (remote-curation.md R-CUR-026, Q-CUR-228): the range of versions the package's allow delivers, with
 * the download dialog's two fields (Q-CUR-194): **From version** (included) and **Up to version** with **included** or **below it**, each
 * offered the versions the opened package lists and open to any version typed; both empty is every version. An OCI image says that a tag
 * that is no version (latest) lies outside the range and that `docker pull` without a tag asks for it (Q-CUR-223). **Save** turns the
 * package's switch on with the range, which the server writes whatever the package inherits; a refused save keeps the values and says
 * the refusal at its field with every fact of the answer (R-UI-216). Closes with the switch's answer.
 */
@Component({
  selector: 'quader-version-range-dialog',
  imports: [DialogClose, DialogTitle, ErrorNotice],
  template: `
    <h2 quaderDialogTitle i18n="@@upstream.range.title">Limit the versions of {{ data.name }}</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        <p class="lead" i18n="@@upstream.range.lead">The package is delivered with the versions in the range; a version outside it is not, unless its own switch is on. Leave both fields empty to deliver every version.</p>
        @if (!data.delivered) {
          <p class="hint" data-testid="version-range-turns-on" i18n="@@upstream.range.turns-on">Saving allows the package with these versions.</p>
        }
        @if (data.format === 'oci') {
          <p class="hint" data-testid="version-range-tags" i18n="@@upstream.range.oci-tags">A tag that is no version, such as latest, lies outside every range: docker pull without a tag asks for latest and is refused until latest's own switch is on.</p>
        }
        <div class="bound-row">
          <label class="field-label" for="range-from-version" i18n="@@upstream.preview.from-version">From version</label>
          <input
            id="range-from-version"
            class="text-input field-w-version"
            list="range-versions"
            autocomplete="off"
            spellcheck="false"
            translate="no"
            placeholder="The oldest"
            i18n-placeholder="@@upstream.preview.from-version.empty"
            [value]="fromVersion()"
            [disabled]="saving()"
            [attr.aria-invalid]="fromError() ? 'true' : null"
            aria-describedby="range-from-version-error"
            (input)="fromVersion.set($any($event.target).value)"
          />
          @if (fromError(); as text) {
            <p id="range-from-version-error" class="field-error">{{ text }}</p>
          }
        </div>
        <div class="bound-row">
          <label class="field-label" for="range-to-version" i18n="@@upstream.preview.to-version">Up to version</label>
          <div class="to-row">
            <input
              id="range-to-version"
              class="text-input field-w-version"
              list="range-versions"
              autocomplete="off"
              spellcheck="false"
              translate="no"
              placeholder="The newest"
              i18n-placeholder="@@upstream.preview.to-version.empty"
              [value]="toVersion()"
              [disabled]="saving()"
              [attr.aria-invalid]="toError() ? 'true' : null"
              aria-describedby="range-to-version-error"
              (input)="toVersion.set($any($event.target).value)"
            />
            <select
              class="select-input to-inclusive"
              aria-label="Whether the version itself is delivered"
              i18n-aria-label="@@upstream.range.to-inclusive.label"
              [disabled]="saving()"
              (change)="toInclusive.set($any($event.target).value === 'included')"
            >
              <option value="included" [selected]="toInclusive()" i18n="@@upstream.preview.to-inclusive.included">included</option>
              <option value="below" [selected]="!toInclusive()" i18n="@@upstream.preview.to-inclusive.below">below it</option>
            </select>
          </div>
          @if (toError(); as text) {
            <p id="range-to-version-error" class="field-error">{{ text }}</p>
          }
        </div>
        <datalist id="range-versions">
          @for (v of data.versions; track v) {
            <option [value]="v"></option>
          }
        </datalist>
        @if (failure(); as f) {
          <quader-error-notice [error]="f" />
        }
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@upstream.dialog.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="saving()" i18n="@@upstream.range.save">Save</button>
      </div>
    </form>
  `,
  styles: `
    .lead {
      margin-block: 0 var(--quader-space-3);
    }
    .hint {
      margin-block: 0 var(--quader-space-3);
    }
    .bound-row {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-3);
    }
    /* The upper version and whether it is delivered, on one line; the choice moves under the field where the line is too narrow. */
    .to-row {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2);
    }
    .to-inclusive {
      inline-size: auto;
      max-inline-size: 100%;
    }
    .field-error {
      margin: 0;
    }
  `,
})
export class VersionRangeDialog {
  private readonly rules = inject(DeliveryRulesApi);
  protected readonly data = inject<VersionRangeData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<DeliverySwitchResult, VersionRangeDialog>>(DialogRef);

  /** The bounds as typed; '' is no bound on that side. */
  protected readonly fromVersion = signal(this.data.range?.fromVersion ?? '');
  protected readonly toVersion = signal(this.data.range?.toVersion ?? '');
  protected readonly toInclusive = signal(this.data.range?.toInclusive ?? true);
  protected readonly saving = signal(false);
  /** The refused save, kept with the values until the next save (R-UI-216). */
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly fromError = computed(() => rangeErrorText(this.failure(), '/fromVersion', AT));
  protected readonly toError = computed(() => rangeErrorText(this.failure(), '/toVersion', AT));

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (this.saving()) return;
    const from = this.fromVersion().trim();
    const to = this.toVersion().trim();
    this.saving.set(true);
    this.failure.set(null);
    try {
      const out = await this.rules.switch(
        this.data.repository,
        {
          name: this.data.name,
          delivered: true,
          resetVersions: false,
          range: { ...(from ? { fromVersion: from } : {}), ...(to ? { toVersion: to, toInclusive: this.toInclusive() } : {}) },
        },
        newIdempotencyKey(),
      );
      this.ref.close(out);
    } catch (e) {
      this.failure.set(asApiError(e));
    } finally {
      this.saving.set(false);
    }
  }
}
