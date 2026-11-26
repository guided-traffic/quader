import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input, linkedSignal, output, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { LimitMode } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Versioned } from '../../api/repositories-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { TextField } from '../../shared/access/fields';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Busy } from '../../shared/ui/busy';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Disclosure } from '../../shared/ui/disclosure';
import { formatCount } from '../limits/figures';
import { LimitModeDialog, ModeDialogData } from '../limits/limit-mode';
import { UsageMeter } from '../usage/usage-meter';
import { stateOfRatio } from '../usage/usage-words';
import { BastionEgress } from './bastion-egress';
import { BastionLimitDetails } from './bastion-limit-details';
import { BastionPresetDialog, PresetDialogData } from './bastion-preset-dialog';
import { bastionState, blockReasons, blockedText, edgeBelowFigures, limitedText, megabytesPerSecond, stateParts } from './bastion-state';
import { BastionSwitches } from './bastion-switches';
import { PRESET_NAMES, PresetName, modeName, presetLabel, presetSentence } from './presets';

const MB = 1e6;

/**
 * The section **Limits** of a Bastion's card (R-UI-240, Q-UI-1480): simple by default, details on demand.
 *
 * - **In plain view**: the replicas' state as one line, live with the list (R-UI-213), the throughput against its cap as a meter, the
 *   callers limited and the addresses blocked now with a link to the Bastion's **Blocks**, or "No report in the last 5 minutes"; the
 *   warnings an administrator has to see (the edge figure below a request figure, Q-UI-1486; the addresses the Bastion connects from,
 *   Q-UI-1483; a web interface whose catalogue a restart loses); the preset as a segmented control with its sentence, **Custom** shown and
 *   not offered, a change through the dialog that names what it replaces (Q-UI-1481); the mode with its switch, the dialog of R-UI-182
 *   for this Bastion, after recent authentication (R-LIM-044); the maximum throughput in MB/s, 200 for a new registration, empty for
 *   unlimited (Q-BAS-455).
 * - **Details**, closed: the flood figures, the slots, the anonymous ceiling and the throughput by class (`<quader-bastion-limit-details>`),
 *   the addresses the Bastion connects from (R-UI-243), the switches of the web interface (R-BAS-102) and the link to the rules of the
 *   Bastion's place (R-UI-241).
 *
 * Who changes what follows Q-BAS-325: `limits:write` the preset, the mode, the throughput and the figures; `endpoints:write` the switches
 * and the addresses; a reader sees each as text (Q-UI-960). Every change reads the registration for its `ETag` and sends the merge patch;
 * a refusal stands under its control with every fact of its answer (R-UI-216), and the control keeps the server's value.
 */
@Component({
  selector: 'quader-bastion-limits',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [BastionEgress, BastionLimitDetails, BastionSwitches, Busy, Disclosure, ErrorNotice, RouterLink, TextField, UsageMeter],
  template: `
    <h3 [id]="headingId()" i18n="@@bastion.limits.heading">Limits</h3>

    <div class="state" data-testid="bastion-state">
      @if (state(); as s) {
        <p class="state-line">
          @for (p of parts(); track $index) {
            <span>{{ p }}</span>
          }
        </p>
        @if (meterRatio() !== null) {
          <quader-usage-meter
            class="meter"
            [ratio]="meterRatio()"
            [state]="meterState()"
            mode="enforce"
            [label]="meterLabel()"
            [text]="meterText()"
          />
        }
        <p class="state-line">
          @if (canReadRules()) {
            <a [routerLink]="blocksLink()" data-testid="bastion-state-callers">{{ callersText() }}</a>
          } @else {
            <span data-testid="bastion-state-callers">{{ callersText() }}</span>
          }
        </p>
        @if (reasons().length > 0) {
          <p class="state-line reasons" data-testid="bastion-block-reasons">
            @for (r of reasons(); track r) {
              <span>{{ r }}</span>
            }
          </p>
        }
      } @else {
        <p class="muted state-line" data-testid="bastion-no-report" i18n="@@bastion.state.none">No report in the last 5 minutes</p>
      }
    </div>

    @if (edge(); as e) {
      <p class="hint warn line" data-testid="bastion-edge-warning">
        <span class="material-icons inline" aria-hidden="true">warning</span
        ><span i18n="@@bastion.limits.edge">The ingress of this Bastion refuses more than {{ edgeText(e) }} requests a second per address before these figures apply.</span>
      </p>
    }
    @if (catalogueLost()) {
      <p class="hint warn line" data-testid="bastion-catalogue-lost">
        <span class="material-icons inline" aria-hidden="true">warning</span
        ><span i18n="@@bastion.limits.catalogue-lost"
          >After a restart while the connection to it is down, this Bastion's web interface shows nothing until the connection returns. A
          bucket or persistent volumes keep it.</span
        >
      </p>
    }
    <quader-bastion-egress part="warnings" [bastion]="bastion()" (changed)="changed.emit()" />

    <!-- The preset (R-LIM-046): a holder of limits:write chooses one, a reader reads it. -->
    <div class="block">
      <span class="field-label" [id]="presetId()" i18n="@@bastion.preset.label">Preset</span>
      @if (canLimits()) {
        <div class="presets" role="group" [attr.aria-labelledby]="presetId()" [attr.aria-describedby]="presetNoteId()">
          @for (p of presets; track p) {
            <button class="btn btn-default" type="button" [attr.aria-pressed]="preset() === p" (click)="choose(p)">{{ label(p) }}</button>
          }
          @if (preset() === 'custom') {
            <span class="custom" data-testid="bastion-preset-custom" i18n="@@bastion.preset.custom">Custom</span>
          }
        </div>
      } @else {
        <p class="state-text" data-testid="bastion-preset-text">{{ label(preset()) }}</p>
      }
      <p class="hint" [id]="presetNoteId()">{{ sentence(preset()) }}</p>
      @if (presetError(); as e) {
        <quader-error-notice [error]="e" />
      }
    </div>

    <!-- The mode of this Bastion (R-LIM-044), beside the instance's. -->
    <div class="block mode-line">
      <p class="mode" data-testid="bastion-mode">
        @if (mode() === 'enforce') {
          <ng-container i18n="@@bastion.mode.enforce">Mode: <strong translate="no">Enforce</strong></ng-container>
        } @else {
          <ng-container i18n="@@bastion.mode.audit">Mode: <strong translate="no">Audit</strong>. Limits are counted, not applied at this Bastion.</ng-container>
        }
      </p>
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
    </div>

    <!-- The maximum throughput (Q-BAS-455): a default is a value in the field, empty is unlimited. -->
    <div class="block">
      @if (canLimits()) {
        <form class="field-action-row" (submit)="saveThroughput($event)" novalidate>
          <quader-text-field
            width="number"
            i18n-label="@@bastion.throughput.label"
            label="Maximum throughput to this Bastion in MB/s"
            name="throughput"
            [value]="throughput()"
            (valueChange)="throughput.set($event)"
            [error]="throughputError()"
          />
          <button class="btn btn-confirm" type="submit" [disabled]="busy() || !throughputDirty()" [quaderBusy]="busy()" i18n="@@bastion.throughput.save">
            Save changes
          </button>
        </form>
      } @else {
        <p class="state-text" data-testid="bastion-throughput-text">
          <ng-container i18n="@@bastion.throughput.text">Maximum throughput to this Bastion: {{ throughputText() }}</ng-container>
        </p>
      }
      <p class="hint note" i18n="@@bastion.throughput.note">
        Set this to the Bastion's share of the link between the two networks. Only what the Bastion does not hold crosses it; what it serves
        from its cache does not. Empty is unlimited.
      </p>
      @if (throughputFailure(); as e) {
        <quader-error-notice [error]="e" />
      }
    </div>

    <quader-disclosure class="details" i18n-heading="@@bastion.details.heading" heading="Details" [level]="3" [(expanded)]="detailsOpen">
      <quader-bastion-limit-details [bastion]="bastion()" [canWrite]="canLimits()" (changed)="changed.emit()" />
      <quader-bastion-egress part="list" [bastion]="bastion()" (changed)="changed.emit()" />
      <quader-bastion-switches [bastion]="bastion()" (changed)="changed.emit()" />
      @if (canReadRules()) {
        <p class="rules-link">
          <a [routerLink]="rulesLink()" i18n="@@bastion.limits.rules-link">Rules of this Bastion</a>
        </p>
      }
    </quader-disclosure>
  `,
  styles: `
    :host {
      display: block;
    }
    .state {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      max-inline-size: var(--quader-settings-measure);
    }
    .state-line {
      margin: 0;
    }
    /* The parts of the line, each whole, separated by a dot; a narrow card breaks between them, never inside one. */
    .state-line > span:not(:last-child)::after {
      content: ' · ';
    }
    .state-line > span {
      white-space: nowrap;
    }
    /* A reason is a sentence: it breaks inside itself where a narrow card has no room for it whole. */
    .state-line.reasons > span {
      white-space: normal;
    }
    .state-line {
      display: flex;
      flex-wrap: wrap;
      column-gap: 0.25em;
    }
    .meter {
      max-inline-size: 20rem;
    }
    .muted {
      color: var(--quader-text-muted);
    }
    .line,
    .note {
      max-inline-size: var(--quader-settings-measure);
    }
    .line {
      margin-block: var(--quader-space-2) 0;
    }
    .block {
      margin-block-start: var(--quader-space-4);
    }
    .block .hint {
      margin-block: var(--quader-space-1) 0;
    }
    .presets {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-1);
      margin-block-start: var(--quader-space-1);
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
    .state-text {
      margin: var(--quader-space-1) 0 0;
    }
    .mode-line {
      display: flex;
      flex-wrap: wrap;
      align-items: flex-start;
      gap: var(--quader-space-2) var(--quader-space-4);
    }
    .mode {
      margin: 0;
    }
    form {
      margin: 0;
    }
    /* The field as wide as its five digits, its Save changes beside it (Q-UI-335). */
    .field-action-row quader-text-field {
      inline-size: auto;
    }
    .details {
      display: block;
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
  protected readonly sentence = presetSentence;

  protected readonly canLimits = computed(() => this.session.has('limits:write'));
  protected readonly canReadRules = computed(() => this.session.has('limits:read'));
  protected readonly headingId = computed(() => `bastion-limits-${this.bastion().name}`);
  protected readonly presetId = computed(() => `bastion-preset-${this.bastion().name}`);
  protected readonly presetNoteId = computed(() => `bastion-preset-note-${this.bastion().name}`);
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
    return s ? stateParts(s, this.locale) : [];
  });
  /** Why the reported addresses are blocked, in words, each with its number (the closed list of R-LIM-045). */
  protected readonly reasons = computed(() => blockReasons(this.bastion(), this.now()));
  protected readonly edge = computed(() => edgeBelowFigures(this.bastion()));
  /** The web interface is on and a replica said its catalogue does not survive a restart (W-35 B.2b). */
  protected readonly catalogueLost = computed(() => this.bastion().switches.gui && this.bastion().contacts.some((c) => c.catalogueKept === false));

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
