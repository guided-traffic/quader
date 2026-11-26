import { formatNumber } from '@angular/common';
import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input, linkedSignal, output, signal } from '@angular/core';
import { Bastion, BastionConfigurationPatch, BastionsApi } from '../../api/bastions-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { TextField } from '../../shared/access/fields';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Busy } from '../../shared/ui/busy';
import { BastionLimits } from './presets';

/** How a figure reads in its field: a count, a share in per cent, or gigabytes. */
type Unit = 'count' | 'percent' | 'gb';

/** The groups of the details (R-UI-240), each with a legend; `flood` holds the first five. */
type GroupId = 'anonymous' | 'authenticated' | 'gui' | 'failedAuth' | 'blocking' | 'slots' | 'ceiling' | 'throughput';

/** One figure of the details: where it lives in the limits, how it reads, and whether an empty field is unlimited. */
interface DetailField {
  readonly key: string;
  readonly group: GroupId;
  readonly label: string;
  readonly unit: Unit;
  /** Empty is null, "unlimited". */
  readonly nullable: boolean;
  /** The least value a count may take (a wait may be 0). */
  readonly min: number;
  readonly read: (l: BastionLimits) => number | null;
}

const rps = (): string => $localize`:@@bastion.details.rps:Requests a second`;
const burst = (): string => $localize`:@@bastion.details.burst:Burst`;
const misses = (): string => $localize`:@@bastion.details.misses:Misses a minute`;
const share = (): string => $localize`:@@bastion.details.anonymous-share:Share of anonymous callers in %`;

/** The figures of the details in the order they stand (R-UI-240). */
function detailFields(): readonly DetailField[] {
  const count = (key: string, group: GroupId, label: string, read: DetailField['read'], min = 1): DetailField => ({
    key, group, label, unit: 'count', nullable: false, min, read,
  });
  return [
    count('flood.anonymous.requestsPerSecond', 'anonymous', rps(), (l) => l.flood.anonymous.requestsPerSecond),
    count('flood.anonymous.burst', 'anonymous', burst(), (l) => l.flood.anonymous.burst),
    count('flood.anonymous.missesPerMinute', 'anonymous', misses(), (l) => l.flood.anonymous.missesPerMinute),
    count('flood.authenticated.requestsPerSecond', 'authenticated', rps(), (l) => l.flood.authenticated.requestsPerSecond),
    count('flood.authenticated.burst', 'authenticated', burst(), (l) => l.flood.authenticated.burst),
    count('flood.authenticated.missesPerMinute', 'authenticated', misses(), (l) => l.flood.authenticated.missesPerMinute),
    count('flood.gui.requestsPerSecond', 'gui', rps(), (l) => l.flood.gui.requestsPerSecond),
    count('flood.gui.burst', 'gui', burst(), (l) => l.flood.gui.burst),
    count('flood.failedAuth.maxFailures', 'failedAuth', $localize`:@@bastion.details.failures:Failures before a timeout`, (l) => l.flood.failedAuth.maxFailures),
    count('flood.failedAuth.windowSeconds', 'failedAuth', $localize`:@@bastion.details.counted-over:Counted over, in seconds`, (l) => l.flood.failedAuth.windowSeconds),
    count('flood.failedAuth.timeoutSeconds', 'failedAuth', $localize`:@@bastion.details.timeout:Timeout, in seconds`, (l) => l.flood.failedAuth.timeoutSeconds),
    count('flood.blocking.threshold', 'blocking', $localize`:@@bastion.details.block-threshold:Refused requests before a block`, (l) => l.flood.blocking.threshold),
    count('flood.blocking.windowSeconds', 'blocking', $localize`:@@bastion.details.counted-over:Counted over, in seconds`, (l) => l.flood.blocking.windowSeconds),
    count('flood.blocking.durationSeconds', 'blocking', $localize`:@@bastion.details.block-first:First block, in seconds`, (l) => l.flood.blocking.durationSeconds),
    count('flood.blocking.maxDurationSeconds', 'blocking', $localize`:@@bastion.details.block-longest:Longest block, in seconds`, (l) => l.flood.blocking.maxDurationSeconds),
    count('slots.maxConcurrent', 'slots', $localize`:@@bastion.details.in-flight:Requests in flight`, (l) => l.slots.maxConcurrent),
    { key: 'slots.anonymousShare', group: 'slots', label: share(), unit: 'percent', nullable: false, min: 0, read: (l) => l.slots.anonymousShare },
    count('slots.queueTimeoutMillis', 'slots', $localize`:@@bastion.details.wait:Longest wait for a slot, in milliseconds`, (l) => l.slots.queueTimeoutMillis, 0),
    { key: 'anonymousCeiling.requestsPerSecond', group: 'ceiling', label: rps(), unit: 'count', nullable: true, min: 1, read: (l) => l.anonymousCeiling.requestsPerSecond },
    { key: 'anonymousCeiling.burst', group: 'ceiling', label: burst(), unit: 'count', nullable: true, min: 1, read: (l) => l.anonymousCeiling.burst },
    { key: 'anonymousCeiling.requestsPerHour', group: 'ceiling', label: $localize`:@@bastion.details.per-hour:Requests an hour`, unit: 'count', nullable: true, min: 1, read: (l) => l.anonymousCeiling.requestsPerHour },
    { key: 'throughput.anonymousShare', group: 'throughput', label: share(), unit: 'percent', nullable: false, min: 0, read: (l) => l.throughput.anonymousShare },
    { key: 'throughput.bytesPerHour.anonymous', group: 'throughput', label: $localize`:@@bastion.details.gb-anonymous:Anonymous callers, GB an hour`, unit: 'gb', nullable: true, min: 0, read: (l) => l.throughput.bytesPerHour.anonymous },
    { key: 'throughput.bytesPerHour.signedIn', group: 'throughput', label: $localize`:@@bastion.details.gb-signed-in:Signed-in callers, GB an hour`, unit: 'gb', nullable: true, min: 0, read: (l) => l.throughput.bytesPerHour.signedIn },
    { key: 'throughput.bytesPerHour.catalogue', group: 'throughput', label: $localize`:@@bastion.details.gb-catalogue:Web interface, GB an hour`, unit: 'gb', nullable: true, min: 0, read: (l) => l.throughput.bytesPerHour.catalogue },
  ];
}

/** A figure as its field holds it: a count as it is, a share in per cent, bytes in GB; empty for null. */
export function fieldText(f: Pick<DetailField, 'unit'>, value: number | null): string {
  if (value === null) return '';
  switch (f.unit) {
    case 'percent':
      return String(Math.round(value * 10_000) / 100);
    case 'gb':
      return String(Math.round(value / 1e7) / 100);
    default:
      return String(value);
  }
}

/** A field's text read back: the figure, or the error of its field. */
export function readField(f: Pick<DetailField, 'unit' | 'nullable' | 'min'>, text: string): { value: number | null } | { error: string } {
  const t = text.trim().replace(',', '.');
  if (t === '') {
    return f.nullable ? { value: null } : { error: $localize`:@@bastion.details.required:Enter a figure; this one cannot be unlimited.` };
  }
  const n = Number(t);
  switch (f.unit) {
    case 'percent':
      return Number.isFinite(n) && n >= 0 && n <= 100 ? { value: n / 100 } : { error: $localize`:@@bastion.details.percent:A share from 0 to 100.` };
    case 'gb':
      return Number.isFinite(n) && n > 0 ? { value: Math.round(n * 1e9) } : { error: $localize`:@@bastion.details.gb:A number of GB above 0, or empty for unlimited.` };
    default:
      if (!/^\d+$/.test(t) || n < f.min) {
        return f.min === 0
          ? { error: $localize`:@@bastion.details.whole-zero:A whole number from 0.` }
          : { error: $localize`:@@bastion.details.whole:A whole number from 1.` };
      }
      return { value: n };
  }
}

/** Sets `value` at the dotted `key` of `target`, creating the members on the way. */
function setAt(target: Record<string, unknown>, key: string, value: unknown): void {
  const parts = key.split('.');
  let at = target;
  for (const p of parts.slice(0, -1)) {
    at[p] = (at[p] as Record<string, unknown> | undefined) ?? {};
    at = at[p] as Record<string, unknown>;
  }
  at[parts.at(-1)!] = value;
}

/**
 * **Details** of a Bastion's limits (R-UI-240): the flood figures per address (anonymous and signed-in callers and the pages of the web
 * interface, the timeout after failed sign-ins, the blocks, the networks without flood protection), the slots to the main installation,
 * the anonymous ceiling, the throughput's anonymous share and its GB an hour per class. A holder of `limits:write` changes them in one
 * form with one **Save changes**, a merge patch of `limits` with the registration's `ETag`; a refusal stands under the form with every
 * fact of its answer, and the fields keep what was typed (Q-UI-1487). A reader sees the figures as text (Q-UI-960).
 */
@Component({
  selector: 'quader-bastion-limit-details',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Busy, ErrorNotice, TextField],
  template: `
    @if (canWrite()) {
      <form (submit)="save($event)" novalidate>
        <h4 i18n="@@bastion.details.flood">Flood protection per address</h4>
        @for (g of floodGroups; track g) {
          <fieldset>
            <legend>{{ legend(g) }}</legend>
            <div class="fields">
              @for (f of fieldsOf(g); track f.key) {
                <quader-text-field width="number" [label]="f.label" [name]="f.key" [value]="texts()[f.key] ?? ''" (valueChange)="set(f.key, $event)" [error]="errorOf(f.key)" />
              }
            </div>
          </fieldset>
        }
        <quader-text-field
          class="exempt"
          width="full"
          i18n-label="@@bastion.flood.exempt"
          label="Networks without flood protection"
          name="exemptNetworks"
          i18n-hint="@@bastion.details.exempt-hint"
          hint="Addresses or networks, separated by commas: no flood figure, timeout or block applies to them."
          [value]="exempt()"
          (valueChange)="exempt.set($event)"
        />
        @for (g of otherGroups; track g) {
          <h4>{{ legend(g) }}</h4>
          @if (hintOf(g); as h) {
            <p class="hint group-hint">{{ h }}</p>
          }
          <div class="fields">
            @for (f of fieldsOf(g); track f.key) {
              <quader-text-field width="number" [label]="f.label" [name]="f.key" [value]="texts()[f.key] ?? ''" (valueChange)="set(f.key, $event)" [error]="errorOf(f.key)" />
            }
          </div>
        }
        @if (error(); as e) {
          <quader-error-notice [error]="e" />
        }
        <div class="row-actions">
          <button class="btn btn-confirm" type="submit" [disabled]="busy() || !dirty()" [quaderBusy]="busy()" i18n="@@bastion.details.save">Save changes</button>
        </div>
      </form>
    } @else {
      <h4 i18n="@@bastion.details.flood">Flood protection per address</h4>
      <dl class="read" data-testid="bastion-details-read">
        @for (g of floodGroups; track g) {
          <dt>{{ legend(g) }}</dt>
          <dd>{{ readText(g) }}</dd>
        }
        <dt i18n="@@bastion.flood.exempt">Networks without flood protection</dt>
        <dd translate="no">{{ exemptText() }}</dd>
        @for (g of otherGroups; track g) {
          <dt>{{ legend(g) }}</dt>
          <dd>{{ readText(g) }}</dd>
        }
      </dl>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    h4 {
      margin: var(--quader-space-4) 0 var(--quader-space-2);
      font: var(--quader-type-title-small);
    }
    h4:first-child {
      margin-block-start: 0;
    }
    fieldset {
      margin: 0 0 var(--quader-space-2);
      padding: 0;
      border: 0;
      min-inline-size: 0;
    }
    /* A group's name under the topic's heading, smaller than it, so the two levels read apart. */
    legend {
      padding: 0;
      margin-block-end: var(--quader-space-1);
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
      font-weight: 600;
    }
    /* The fields of a group side by side, each as wide as its label needs and its number, their inputs on one line where a label
       wraps; one under the other where the card is narrow. */
    .fields {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(13rem, 1fr));
      align-items: end;
      gap: 0 var(--quader-space-4);
      max-inline-size: var(--quader-settings-measure);
    }
    .exempt {
      max-inline-size: var(--quader-settings-measure);
    }
    .group-hint {
      margin: calc(-1 * var(--quader-space-1)) 0 var(--quader-space-2);
    }
    .read {
      display: grid;
      grid-template-columns: minmax(0, max-content) minmax(0, 1fr);
      gap: var(--quader-space-1) var(--quader-space-4);
      margin: 0;
      max-inline-size: var(--quader-settings-measure);
    }
    .read dt {
      font-weight: 600;
    }
    .read dd {
      margin: 0;
      overflow-wrap: anywhere;
    }
    @media (max-width: 40rem) {
      .read {
        grid-template-columns: minmax(0, 1fr);
      }
      .read dd {
        margin-block-end: var(--quader-space-2);
      }
    }
    quader-error-notice {
      display: block;
      margin-block: var(--quader-space-2);
    }
  `,
})
export class BastionLimitDetails {
  readonly bastion = input.required<Bastion>();
  /** `limits:write`: the figures are fields; otherwise text. */
  readonly canWrite = input(false);
  /** The registration changed: the page reads the list again. */
  readonly changed = output<void>();

  private readonly api = inject(BastionsApi);
  private readonly notifier = inject(Notifier);
  private readonly locale = inject(LOCALE_ID);

  protected readonly floodGroups: readonly GroupId[] = ['anonymous', 'authenticated', 'gui', 'failedAuth', 'blocking'];
  protected readonly otherGroups: readonly GroupId[] = ['slots', 'ceiling', 'throughput'];
  private readonly fields = detailFields();

  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  protected readonly attempted = signal(false);

  /** The server's texts of the fields; the list's reload sets the fields again only when the server's figures changed (Q-UI-1484's rule). */
  private readonly serverTexts = computed(() => this.textsOf(this.bastion().limits));
  protected readonly texts = linkedSignal<string, Record<string, string>>({
    source: () => JSON.stringify(this.serverTexts()),
    computation: () => ({ ...this.serverTexts() }),
  });
  protected readonly exempt = linkedSignal(() => this.bastion().limits.flood.exemptNetworks.join(', '));

  protected readonly dirty = computed(() => {
    const server = this.serverTexts();
    const typed = this.texts();
    return this.fields.some((f) => (typed[f.key] ?? '') !== server[f.key]) || this.exempt() !== this.bastion().limits.flood.exemptNetworks.join(', ');
  });

  private textsOf(l: BastionLimits): Record<string, string> {
    return Object.fromEntries(this.fields.map((f) => [f.key, fieldText(f, f.read(l))]));
  }

  protected fieldsOf(g: GroupId): readonly DetailField[] {
    return this.fields.filter((f) => f.group === g);
  }

  protected legend(g: GroupId): string {
    switch (g) {
      case 'anonymous':
        return $localize`:@@bastion.details.anonymous:Anonymous callers`;
      case 'authenticated':
        return $localize`:@@bastion.details.signed-in:Signed-in callers`;
      case 'gui':
        return $localize`:@@bastion.details.pages:Pages of the web interface`;
      case 'failedAuth':
        return $localize`:@@bastion.details.failed:Failed sign-ins`;
      case 'blocking':
        return $localize`:@@bastion.details.blocks:Blocks`;
      case 'slots':
        return $localize`:@@bastion.details.slots:Slots to the main installation`;
      case 'ceiling':
        return $localize`:@@bastion.details.ceiling:Anonymous ceiling`;
      case 'throughput':
        return $localize`:@@bastion.details.throughput:Throughput by class`;
    }
  }

  protected hintOf(g: GroupId): string {
    switch (g) {
      case 'slots':
        return $localize`:@@bastion.details.slots-hint:The requests this Bastion's replicas send to the main installation at once, together.`;
      case 'ceiling':
        return $localize`:@@bastion.details.ceiling-hint:What all anonymous callers of this Bastion together may ask of the main installation. An empty field is unlimited.`;
      case 'throughput':
        return $localize`:@@bastion.details.throughput-hint:The anonymous callers' share of the maximum throughput, and the volume each class may read an hour. An empty field is unlimited.`;
      default:
        return '';
    }
  }

  /** A group as text: "Requests a second 50 · Burst 1,000 · Misses a minute 1,000". */
  protected readText(g: GroupId): string {
    const l = this.bastion().limits;
    return this.fieldsOf(g)
      .map((f) => `${f.label} ${this.valueText(f, f.read(l))}`)
      .join(' · ');
  }

  private valueText(f: DetailField, v: number | null): string {
    if (v === null) return $localize`:@@limits.unlimited:Unlimited`;
    switch (f.unit) {
      case 'percent':
        return formatNumber(v * 100, this.locale, '1.0-2');
      case 'gb':
        return formatNumber(v / 1e9, this.locale, '1.0-2');
      default:
        return formatNumber(v, this.locale, '1.0-0');
    }
  }

  protected exemptText(): string {
    const list = this.bastion().limits.flood.exemptNetworks;
    return list.length > 0 ? list.join(', ') : $localize`:@@bastion.preset.none:none`;
  }

  protected set(key: string, text: string): void {
    this.texts.update((t) => ({ ...t, [key]: text }));
  }

  protected errorOf(key: string): string | null {
    if (!this.attempted()) return null;
    const f = this.fields.find((x) => x.key === key)!;
    const read = readField(f, this.texts()[key] ?? '');
    if ('error' in read) return read.error;
    if (key === 'anonymousCeiling.burst' && read.value !== null) {
      const rate = readField(this.fields.find((x) => x.key === 'anonymousCeiling.requestsPerSecond')!, this.texts()['anonymousCeiling.requestsPerSecond'] ?? '');
      if ('value' in rate && rate.value === null) return $localize`:@@bastion.details.burst-needs-rate:A burst needs requests a second.`;
    }
    return null;
  }

  /** The merge patch of `limits` the fields hold; null while a field is refused. */
  private patch(): BastionConfigurationPatch['limits'] | null {
    const out: Record<string, unknown> = {};
    for (const f of this.fields) {
      if (this.errorOf(f.key)) return null;
      const read = readField(f, this.texts()[f.key] ?? '');
      if ('error' in read) return null;
      setAt(out, f.key, read.value);
    }
    const exempt = this.exempt()
      .split(/[,\s]+/)
      .map((e) => e.trim())
      .filter((e) => e !== '');
    setAt(out, 'flood.exemptNetworks', exempt);
    return out as BastionConfigurationPatch['limits'];
  }

  protected async save(event: Event): Promise<void> {
    event.preventDefault();
    this.attempted.set(true);
    const limits = this.patch();
    if (!limits || this.busy()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      const name = this.bastion().name;
      const { etag } = await this.api.get(name);
      await this.api.configure(name, { limits }, etag);
      this.attempted.set(false);
      this.notifier.status($localize`:@@bastion.details.saved:The limits of ${name}:name: are saved.`);
      this.changed.emit();
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
