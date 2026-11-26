import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, signal } from '@angular/core';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { LimitsApi } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Versioned } from '../../api/repositories-api';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Busy } from '../../shared/ui/busy';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';
import { Loading } from '../../shared/ui/loading';
import { PresetChange, PresetChanges, PresetName, changesNothing, presetChanges, presetLabel } from './presets';

export interface PresetDialogData {
  readonly bastion: Bastion;
  readonly preset: PresetName;
}

/**
 * "Apply the preset Partner network?" (Q-UI-1481): before a preset replaces the five kind rules of a Bastion's place, its flood figures
 * and its mode (R-LIM-046), the dialog reads the place's rules and the profiles and names each thing that changes, from what to what,
 * and says that the rules of single callers, groups and addresses stay; where the preset changes nothing it says so alone. **Apply
 * preset** reads the registration for its `ETag` and sends `PATCH` with `limits.preset` (Q-LIM-311); a refusal stands in the dialog.
 */
@Component({
  selector: 'quader-bastion-preset-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Busy, DialogClose, DialogTitle, ErrorNotice, Loading],
  template: `
    <h2 quaderDialogTitle i18n="@@bastion.preset.dialog-title">Apply the preset {{ label }}?</h2>
    <div class="dialog-body">
      <p class="subject" translate="no">{{ data.bastion.name }}</p>
      @if (failure(); as e) {
        <quader-error-notice [error]="e" />
      }
      @if (readFailure(); as e) {
        <quader-error-notice [error]="e" (retry)="read()" />
      } @else if (changes(); as c) {
        @if (nothing()) {
          <p data-testid="preset-nothing" i18n="@@bastion.preset.nothing">The preset changes nothing: the rules, the flood figures and the mode are already its own.</p>
        } @else {
          <p i18n="@@bastion.preset.replaces">The preset replaces:</p>
          @if (c.kinds.length > 0) {
            <h3 i18n="@@bastion.preset.kind-rules">Rules for every caller of a kind</h3>
            <ul class="changes" data-testid="preset-kinds">
              @for (k of c.kinds; track k.what) {
                <li>{{ line(k) }}</li>
              }
            </ul>
          }
          @if (c.flood.length > 0 || c.exemptNetworks) {
            <h3 i18n="@@bastion.preset.flood">Flood protection per address</h3>
            <ul class="changes" data-testid="preset-flood">
              @for (f of c.flood; track f.what) {
                <li>{{ line(f) }}</li>
              }
              @if (c.exemptNetworks; as e) {
                <li>{{ line(e) }}</li>
              }
            </ul>
          }
          @if (c.mode; as m) {
            <p class="mode" data-testid="preset-mode">{{ line(m) }}</p>
          }
          <p class="keeps" i18n="@@bastion.preset.keeps">The rules for single callers, groups and addresses stay.</p>
        }
      } @else {
        <quader-loading i18n="@@bastion.preset.loading">Reading the rules of this Bastion.</quader-loading>
      }
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@limits.cancel">Cancel</button>
      <button class="btn btn-confirm" type="button" [disabled]="busy() || !changes()" [quaderBusy]="busy()" (click)="apply()" i18n="@@bastion.preset.apply">
        Apply preset
      </button>
    </div>
  `,
  styles: `
    .subject {
      margin-block-start: 0;
      font-weight: 600;
    }
    h3 {
      margin: var(--quader-space-3) 0 var(--quader-space-1);
      font: var(--quader-type-title-small);
    }
    .changes {
      margin: 0 0 var(--quader-space-2);
      padding-inline-start: var(--quader-space-5);
    }
    /* The mode is one change of its own, under the groups, with the weight of their headings. */
    .mode {
      margin: var(--quader-space-3) 0 0;
      font-weight: 600;
    }
    .keeps {
      margin-block-start: var(--quader-space-3);
    }
  `,
})
export class BastionPresetDialog {
  protected readonly data = inject<PresetDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<Versioned<Bastion>>>(DialogRef);
  private readonly bastions = inject(BastionsApi);
  private readonly limits = inject(LimitsApi);
  private readonly locale = inject(LOCALE_ID);

  protected readonly label = presetLabel(this.data.preset);
  protected readonly changes = signal<PresetChanges | null>(null);
  protected readonly nothing = computed(() => {
    const c = this.changes();
    return c !== null && changesNothing(c);
  });
  protected readonly readFailure = signal<ApiError | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly busy = signal(false);

  constructor() {
    void this.read();
  }

  /** Reads the place's rules and the profiles; the failure stays until they answer (R-UI-216). */
  protected async read(): Promise<void> {
    const b = this.data.bastion;
    try {
      const [rules, profiles] = await Promise.all([this.limits.rules({ kind: 'bastion', bastion: b.name }), this.limits.profiles()]);
      this.readFailure.set(null);
      this.changes.set(presetChanges(b.limits, rules, profiles, this.data.preset, this.locale));
    } catch (e) {
      this.readFailure.set(asApiError(e));
    }
  }

  /** "Every anonymous caller: Bastion: anonymous → Anonymous". */
  protected line(c: PresetChange): string {
    return $localize`:@@bastion.preset.change:${c.what}:what:: ${c.from}:from: → ${c.to}:to:`;
  }

  protected async apply(): Promise<void> {
    if (this.busy()) return;
    this.busy.set(true);
    this.failure.set(null);
    try {
      const name = this.data.bastion.name;
      const { etag } = await this.bastions.get(name);
      this.ref.close(await this.bastions.configure(name, { limits: { preset: this.data.preset } }, etag));
    } catch (e) {
      this.failure.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
