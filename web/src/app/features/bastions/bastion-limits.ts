import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input, linkedSignal, output, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { LimitMode } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Versioned } from '../../api/repositories-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Busy } from '../../shared/ui/busy';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Disclosure } from '../../shared/ui/disclosure';
import { Setting } from '../../shared/ui/setting';
import { formatCount } from '../limits/figures';
import { LimitModeDialog, ModeDialogData } from '../limits/limit-mode';
import { UsageMeter } from '../usage/usage-meter';
import { stateOfRatio } from '../usage/usage-words';
import { BastionEgress } from './bastion-egress';
import { BastionLimitDetails } from './bastion-limit-details';
import { BastionPresetDialog, PresetDialogData } from './bastion-preset-dialog';
import { bastionState, blockedText, edgeBelowFigures, limitedText, megabytesPerSecond, stateParts } from './bastion-state';
import { PRESET_NAMES, PresetName, modeName, presetLabel } from './presets';

const MB = 1e6;

/**
 * The section **Limits** of a Bastion's card (R-UI-240, Q-UI-1480): simple by default, details on demand.
 *
 * - **In plain view**: first the warnings an administrator has to see, one line each (Q-UI-1624: the edge figure below a request
 *   figure, Q-UI-1486; the addresses the Bastion connects from, Q-UI-1483), then four setting rows (Q-UI-1621), each with its help
 *   behind the info sign (R-UI-267): **Preset**, a segmented control, **Custom** shown and not offered, a change through the dialog that
 *   names what it replaces (Q-UI-1481); **Mode**, its word and the dialog of R-UI-182 for this Bastion, after recent authentication
 *   (R-LIM-044); **Maximum throughput** in MB/s, 200 for a new registration, empty for unlimited (Q-BAS-455), the meter of the
 *   throughput against it under the field; **Now**, the replicas' state, live with the list (R-UI-213), the read rate only where no
 *   maximum is set, and the callers limited and the addresses blocked with a link to the Bastion's **Blocks**, all on one line of
 *   parts (W-41 D27), or "No report in the last 5 minutes".
 * - What the card puts after the rows (`[before-details]`: its section **Projects**, R-BAS-134).
 * - **Details**, closed, with its note: what the card puts first (`[details-start]`: the web interface's **Hide repositories**), the
 *   flood figures, the slots, the anonymous ceiling and the throughput by class (`<quader-bastion-limit-details>`), the addresses the Bastion connects from (R-UI-243), the link to the rules of the Bastion's
 *   place (R-UI-241), and what the card puts at its end (`[details-end]`: **Created**).
 *
 * Who changes what follows Q-BAS-325: `limits:write` the preset, the mode, the throughput and the figures; `endpoints:write` the
 * addresses; a reader sees each as its value in words (Q-UI-960). Every change reads the registration for its `ETag` and sends the merge
 * patch; a refusal stands under its control with every fact of its answer (R-UI-216), and the control keeps the server's value.
 */
@Component({
  selector: 'quader-bastion-limits',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [BastionEgress, BastionLimitDetails, Busy, Disclosure, ErrorNotice, RouterLink, Setting, UsageMeter],
  template: `
    <h3 [id]="headingId()" i18n="@@bastion.limits.heading">Limits</h3>

    <!-- The limits' warnings first, one line each (Q-UI-1624): the edge, then the addresses this Bastion connects from (R-UI-243). -->
    @if (edge(); as e) {
      <p class="hint warn line" data-testid="bastion-edge-warning">
        <span class="material-icons inline" aria-hidden="true">warning</span
        ><span i18n="@@bastion.limits.edge">The ingress refuses more than {{ edgeText(e) }} requests a second per address, below this Bastion's flood figures.</span>
      </p>
    }
    <quader-bastion-egress part="warnings" [bastion]="bastion()" (changed)="changed.emit()" />

    <!-- The preset (R-LIM-046): a holder of limits:write chooses one, a reader reads it. -->
    <quader-setting #presetRow label="Preset" i18n-label="@@bastion.preset.label" [help]="presetHelp" data-testid="bastion-preset-row">
      @if (canLimits()) {
        <div class="presets" role="group" [attr.aria-labelledby]="presetRow.labelId">
          @for (p of presets; track p) {
            <button class="btn btn-default" type="button" [attr.aria-pressed]="preset() === p" (click)="choose(p)">{{ label(p) }}</button>
          }
          @if (preset() === 'custom') {
            <span class="custom" data-testid="bastion-preset-custom" i18n="@@bastion.preset.custom">Custom</span>
          }
        </div>
      } @else {
        <span data-testid="bastion-preset-text">{{ label(preset()) }}</span>
      }
      @if (presetError(); as e) {
        <quader-error-notice below [error]="e" />
      }
    </quader-setting>

    <!-- The mode of this Bastion (R-LIM-044), beside the instance's: the word, and the dialog that switches it. -->
    <quader-setting label="Mode" i18n-label="@@bastion.row.mode" [help]="modeHelp" data-testid="bastion-mode-row">
      <span class="mode-line">
        <span class="mode-word" data-testid="bastion-mode" translate="no">{{ modeWord() }}</span>
        @if (canLimits()) {
          <button class="btn btn-default on-line" type="button" (click)="switchMode()">
            @if (mode() === 'audit') {
              <ng-container i18n="@@limits.mode.to-enforce">Switch to Enforce…</ng-container>
            } @else {
              <ng-container i18n="@@limits.mode.to-audit">Switch to Audit…</ng-container>
            }
            <span class="visually-hidden" translate="no"> {{ bastion().name }}</span>
          </button>
        }
      </span>
    </quader-setting>

    <!-- The maximum throughput (Q-BAS-455): a default is a value in the field, empty is unlimited; the meter under its field. -->
    <quader-setting
      label="Maximum throughput"
      i18n-label="@@bastion.row.throughput"
      [for]="canLimits() ? throughputId() : null"
      [help]="throughputHelp"
      data-testid="bastion-throughput-row"
    >
      @if (canLimits()) {
        <form class="throughput-form" (submit)="saveThroughput($event)" novalidate>
          <input
            class="text-input field-w-number"
            type="text"
            inputmode="numeric"
            name="throughput"
            [id]="throughputId()"
            [value]="throughput()"
            [attr.aria-label]="throughputLabel()"
            [attr.aria-invalid]="throughputError() ? 'true' : null"
            [attr.aria-describedby]="throughputError() ? throughputId() + '-error' : null"
            (input)="throughput.set($any($event.target).value)"
          />
          <span class="unit" aria-hidden="true" i18n="@@bastion.throughput.unit">MB/s</span>
          <button class="btn btn-confirm" type="submit" [disabled]="busy() || !throughputDirty()" [quaderBusy]="busy()" i18n="@@bastion.throughput.save">
            Save changes
          </button>
        </form>
        @if (throughputError(); as message) {
          <p class="field-error" [id]="throughputId() + '-error'">{{ message }}</p>
        }
      } @else {
        <span data-testid="bastion-throughput-text">{{ throughputText() }}</span>
      }
      @if (meterRatio() !== null) {
        <quader-usage-meter below class="meter" [ratio]="meterRatio()" [state]="meterState()" mode="enforce" [label]="meterLabel()" [text]="meterText()" />
      }
      @if (throughputFailure(); as e) {
        <quader-error-notice below [error]="e" />
      }
    </quader-setting>

    <!-- What the replicas reported in the last five minutes, live with the list (R-UI-213). -->
    <quader-setting label="Now" i18n-label="@@bastion.row.now" [help]="nowHelp" data-testid="bastion-state">
      @if (state(); as s) {
        <!-- One line of parts (W-41 D27), each whole, the callers' link the last of them; a narrow card breaks between two parts. -->
        <p class="state-line">
          @for (p of parts(); track $index) {
            <span>{{ p }}</span>
          }
          @if (canReadRules()) {
            <span><a [routerLink]="blocksLink()" data-testid="bastion-state-callers">{{ callersText() }}</a></span>
          } @else {
            <span data-testid="bastion-state-callers">{{ callersText() }}</span>
          }
        </p>
      } @else {
        <p class="muted state-line" data-testid="bastion-no-report" i18n="@@bastion.state.none">No report in the last 5 minutes</p>
      }
    </quader-setting>

    <!-- The closed sections after the rows: what the card puts first (its section Projects, W-41's C2), then Details. -->
    <div class="sections">
      <ng-content select="[before-details]" />

      <quader-disclosure
        class="details"
        i18n-heading="@@bastion.details.heading"
        heading="Details"
        i18n-note="@@bastion.details.note"
        note="Web interface, flood protection, addresses, rules"
        [level]="3"
        [(expanded)]="detailsOpen"
      >
        <!-- What the card puts first into Details: the web interface's expert setting (R-BAS-131). -->
        <ng-content select="[details-start]" />
        <quader-bastion-limit-details [bastion]="bastion()" [canWrite]="canLimits()" (changed)="changed.emit()" />
        <quader-bastion-egress part="list" [bastion]="bastion()" (changed)="changed.emit()" />
        @if (canReadRules()) {
          <p class="rules-link">
            <a [routerLink]="rulesLink()" i18n="@@bastion.limits.rules-link">Rules of this Bastion</a>
          </p>
        }
        <!-- What the card puts last into Details: its creation time (Q-UI-1620). -->
        <ng-content select="[details-end]" />
      </quader-disclosure>
    </div>
  `,
  styles: `
    :host {
      display: block;
    }
    .state-line {
      display: flex;
      flex-wrap: wrap;
      column-gap: 0.25em;
      margin: 0;
    }
    /* The parts of the line, each whole, separated by a dot; a narrow card breaks between them, never inside one. */
    .state-line > span:not(:last-child)::after {
      content: ' · ';
    }
    .state-line > span {
      white-space: nowrap;
    }
    .meter {
      max-inline-size: 20rem;
    }
    .muted {
      color: var(--quader-text-muted);
    }
    /* A warning of the section spans the measure, one line at the wide width where its words allow. */
    .line {
      max-inline-size: var(--quader-settings-measure);
      margin-block: var(--quader-space-1);
    }
    .presets {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-1);
    }
    .mode-line,
    .throughput-form {
      display: inline-flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-2) var(--quader-space-3);
    }
    .throughput-form .unit {
      margin-inline-end: var(--quader-space-2);
    }
    .field-error {
      margin: var(--quader-space-1) 0 0;
    }
    .presets button[aria-pressed='true'],
    .custom {
      background: var(--quader-accent-container);
      color: var(--quader-on-accent-container);
    }
    /* Custom is a state, not a choice: it reads as the pressed segment without being a button. */
    .custom {
      display: inline-flex;
      align-items: center;
      min-block-size: var(--quader-control-height);
      padding-inline: var(--quader-space-3);
      border-radius: var(--quader-control-radius);
    }
    form {
      margin: 0;
    }
    .sections {
      margin-block-start: var(--quader-space-4);
    }
    .rules-link {
      margin-block: var(--quader-space-4) 0;
    }
    quader-error-notice {
      display: block;
      margin-block-start: var(--quader-space-2);
    }
  `,
})
export class BastionLimits {
  readonly bastion = input.required<Bastion>();
  /** The registration changed: the page reads the list again. */
  readonly changed = output<void>();

  private readonly api = inject(BastionsApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly notifier = inject(Notifier);
  private readonly session = inject(SessionStore);
  private readonly locale = inject(LOCALE_ID);

  protected readonly presets = PRESET_NAMES;
  protected readonly label = presetLabel;

  protected readonly presetHelp = $localize`:@@bastion.help.preset:Internet: strict limits, administrators too. Partner network: the installation's own profiles. Internal: no limits, everything counted. A choice first shows what it replaces.`;
  protected readonly modeHelp = $localize`:@@bastion.help.mode:Enforce applies the download limits at this Bastion; Audit only counts them. Flood protection, ceiling, slots and throughput hold in both.`;
  protected readonly throughputHelp = $localize`:@@bastion.help.throughput:The Bastion's share of the link between the two networks: only what it does not hold crosses it. Empty is unlimited.`;
  protected readonly nowHelp = $localize`:@@bastion.help.now:What the Bastion's replicas reported in the last 5 minutes, added up.`;

  protected readonly canLimits = computed(() => this.session.has('limits:write'));
  protected readonly canReadRules = computed(() => this.session.has('limits:read'));
  protected readonly headingId = computed(() => `bastion-limits-${this.bastion().name}`);
  protected readonly throughputId = computed(() => `bastion-throughput-${this.bastion().name}`);
  /** The field's name holds its visible label (WCAG 2.5.3) and says whose throughput and the unit, which stands after it unheard. */
  protected readonly throughputLabel = computed(() => $localize`:@@bastion.throughput.label:Maximum throughput of ${this.bastion().name}:name: in MB/s`);
  protected readonly modeWord = computed(() => modeName(this.mode()));
  protected readonly rulesLink = computed(() => ['/admin/bastions', this.bastion().name, 'limits']);
  protected readonly blocksLink = computed(() => ['/admin/bastions', this.bastion().name, 'limits', 'blocks']);

  protected readonly detailsOpen = signal(false);
  protected readonly busy = signal(false);
  protected readonly presetError = signal<ApiError | null>(null);
  protected readonly throughputFailure = signal<ApiError | null>(null);
  private readonly throughputAttempted = signal(false);

  protected readonly preset = computed(() => this.bastion().limits.preset);
  protected readonly mode = computed(() => this.bastion().limits.mode);

  /** The time the state is judged at: the moment the registration was read. */
  private readonly now = computed(() => {
    this.bastion();
    return Date.now();
  });
  protected readonly state = computed(() => bastionState(this.bastion(), this.now()));
  protected readonly parts = computed(() => {
    const s = this.state();
    // The read rate only where no maximum is set: with one, the meter under the field says it (F6 of W-39).
    return s ? stateParts(s, this.locale, this.cap() === null) : [];
  });
  protected readonly edge = computed(() => edgeBelowFigures(this.bastion()));

  private readonly cap = computed(() => this.bastion().limits.throughput.bytesPerSecond);
  protected readonly meterRatio = computed(() => {
    const s = this.state();
    const cap = this.cap();
    return s && cap ? s.bytesPerSecond / cap : null;
  });
  protected readonly meterState = computed(() => stateOfRatio(this.meterRatio(), 'enforce'));
  protected readonly meterLabel = computed(() => $localize`:@@bastion.state.meter:Throughput of ${this.bastion().name}:name: against its maximum`);
  protected readonly meterText = computed(() => {
    const s = this.state();
    const cap = this.cap();
    return s && cap
      ? $localize`:@@bastion.state.meter-text:${megabytesPerSecond(s.bytesPerSecond, this.locale)}:rate: of ${megabytesPerSecond(cap, this.locale)}:cap:`
      : '';
  });
  protected readonly callersText = computed(() => {
    const s = this.state();
    return s ? `${limitedText(s.limited, this.locale)} · ${blockedText(s.blocked, this.locale)}` : '';
  });

  /** The field's text: the server's figure in MB/s, empty for unlimited; set again whenever the server's figure changes. */
  private readonly serverThroughput = computed(() => {
    const v = this.cap();
    return v === null ? '' : String(Math.round((v / MB) * 1000) / 1000);
  });
  protected readonly throughput = linkedSignal(() => this.serverThroughput());
  protected readonly throughputDirty = computed(() => this.throughput().trim() !== this.serverThroughput());
  protected readonly throughputText = computed(() => {
    const v = this.cap();
    return v === null ? $localize`:@@limits.unlimited:Unlimited` : megabytesPerSecond(v, this.locale);
  });
  protected readonly throughputError = computed(() => {
    if (!this.throughputAttempted()) return null;
    const t = this.throughput().trim();
    return t === '' || (/^\d+$/.test(t) && Number(t) >= 1)
      ? null
      : $localize`:@@bastion.throughput.invalid:A whole number of MB/s from 1, or empty for unlimited.`;
  });

  protected edgeText(n: number): string {
    return formatCount(n, this.locale);
  }

  /** A preset applies after its dialog (Q-UI-1481); the pressed one changes nothing. */
  protected async choose(preset: PresetName): Promise<void> {
    if (preset === this.preset()) return;
    this.presetError.set(null);
    const ref = this.dialog.open<BastionPresetDialog, PresetDialogData, Versioned<Bastion>>(BastionPresetDialog, {
      data: { bastion: this.bastion(), preset },
      maxWidth: '40rem',
    });
    const result = await firstValueFrom(ref.closed);
    if (!result) return;
    this.notifier.status($localize`:@@bastion.preset.applied:The preset ${presetLabel(preset)}:preset: applies to ${this.bastion().name}:name:.`);
    this.changed.emit();
  }

  /** The mode's dialog of R-UI-182 for this Bastion; the server asks for recent authentication (R-LIM-044). */
  protected async switchMode(): Promise<void> {
    const name = this.bastion().name;
    const to: LimitMode = this.mode() === 'audit' ? 'enforce' : 'audit';
    const send = async (mode: LimitMode): Promise<Versioned<Bastion>> => {
      const { etag } = await this.api.get(name);
      return this.api.configure(name, { limits: { mode } }, etag);
    };
    const ref = this.dialog.open<LimitModeDialog, ModeDialogData, unknown>(LimitModeDialog, {
      data: { to, bastion: { name, send } },
      maxWidth: '40rem',
    });
    const result = (await firstValueFrom(ref.closed)) as Versioned<Bastion> | undefined;
    if (!result) return;
    this.notifier.status($localize`:@@bastion.mode.switched:Mode of ${name}:name:: ${modeName(result.value.limits.mode)}:mode:.`);
    this.changed.emit();
  }

  protected async saveThroughput(event: Event): Promise<void> {
    event.preventDefault();
    this.throughputAttempted.set(true);
    if (this.throughputError() || this.busy()) return;
    const t = this.throughput().trim();
    const bytesPerSecond = t === '' ? null : Number(t) * MB;
    this.busy.set(true);
    this.throughputFailure.set(null);
    try {
      const name = this.bastion().name;
      const { etag } = await this.api.get(name);
      await this.api.configure(name, { limits: { throughput: { bytesPerSecond } } }, etag);
      this.throughputAttempted.set(false);
      this.notifier.status($localize`:@@bastion.throughput.saved:The maximum throughput of ${name}:name: is saved.`);
      this.changed.emit();
    } catch (e) {
      this.throughputFailure.set(asApiError(e));
      // The field keeps the server's value (R-UI-240); the refusal says what was sent.
      this.throughput.set(this.serverThroughput());
    } finally {
      this.busy.set(false);
    }
  }
}
