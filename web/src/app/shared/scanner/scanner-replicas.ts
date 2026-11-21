import { DatePipe, NgTemplateOutlet } from '@angular/common';
import { Component, computed, input } from '@angular/core';
import { ScannerList, ScannerReplica } from '../../api/system-api';
import { DataTable, TableColumn } from '../ui/data-table';
import { Disclosure } from '../ui/disclosure';
import { NoValue } from '../ui/no-value';
import { TimeAgo } from '../ui/time-ago';
import { scannerGroup } from './scanner-model';

/** What a scanner's reason code means (R-OPS-048): the readiness check that fails; a code this screen does not know is shown as it is. */
export function scannerReasonText(reason: string): string | null {
  switch (reason) {
    case 'database':
      return $localize`:@@scanners.reason.database:No database loaded yet`;
    case 'clamd':
      return $localize`:@@scanners.reason.clamd:clamd does not answer`;
    case 'analyser':
      return $localize`:@@scanners.reason.analyser:Analyser does not run`;
    case 'certificate':
      return $localize`:@@scanners.reason.certificate:Certificate not loaded`;
    case 'spool':
      return $localize`:@@scanners.reason.spool:Spool not writable`;
    case 'starting':
      return $localize`:@@scanners.reason.starting:Still starting`;
    default:
      return null;
  }
}

/**
 * The replicas of one kind of scanner (R-OPS-048, Q-UI-897), the one table of the three screens that show them: the System page
 * (`view="system"`, the columns of every replica there: name, node, version, state, since when, heartbeat, and the tool in place of
 * Valkey), and the settings of the malware scanning and the advisory sources (`view="settings"`, closed in a fold under the page's own
 * summary: name, state, the tool's version, ClamAV's signature database, the slots). A kind without a replica says why in a sentence:
 * none reported, the scan listener cannot tell the kind, the role `all` scans in its own process, or no scanner is configured. Every
 * value is the scanner's own statement, bounded by the server (H-1161), and is bound as text.
 */
@Component({
  selector: 'quader-scanner-replicas',
  imports: [DataTable, DatePipe, Disclosure, NgTemplateOutlet, NoValue, TimeAgo],
  template: `
    @if (group(); as g) {
      @if (g.rows.length > 0) {
        @if (view() === 'system') {
          <quader-data-table [caption]="caption()" [columns]="systemColumns">
            @for (r of g.rows; track r.instanceUid) {
              <tr>
                <th scope="row" class="wrap-name" translate="no">{{ r.name }}</th>
                <td translate="no">
                  @if (r.node; as n) {
                    {{ n }}
                  } @else {
                    <quader-no-value />
                  }
                </td>
                <td translate="no">{{ r.version }}</td>
                <td class="state-cell">
                  <ng-container *ngTemplateOutlet="state; context: { $implicit: r }" />
                </td>
                <td><quader-time-ago [at]="r.stateSince" absolute="title" /></td>
                <td>
                  <time [attr.datetime]="r.heartbeatAt">{{ r.heartbeatAt | date: 'mediumTime' }}</time>
                </td>
                <td translate="no">
                  @if (r.toolVersion; as v) {
                    {{ g.tool }} {{ v }}
                  } @else {
                    <quader-no-value />
                  }
                </td>
              </tr>
            }
          </quader-data-table>
        } @else {
          <quader-disclosure [inline]="true" [heading]="foldHeading()">
            <quader-data-table [caption]="caption()" [columns]="settingsColumns()">
              @for (r of g.rows; track r.instanceUid) {
                <tr>
                  <th scope="row" class="wrap-name" translate="no">{{ r.name }}</th>
                  <td class="state-cell">
                    <ng-container *ngTemplateOutlet="state; context: { $implicit: r }" />
                  </td>
                  <td translate="no">
                    @if (r.toolVersion; as v) {
                      {{ g.tool }} {{ v }}
                    } @else {
                      <quader-no-value />
                    }
                  </td>
                  @if (hasDatabase()) {
                    <td>
                      @if (r.database; as db) {
                        <!-- One box, so that the date stands under the version also where the row stands as a block. -->
                        <span class="database">
                          <code class="version" translate="no">{{ db.version }}</code>
                          @if (db.builtAt; as built) {
                            <span class="secondary"><time [attr.datetime]="built" [attr.title]="built | date: 'medium'">{{ built | date: 'mediumDate' }}</time></span>
                          }
                        </span>
                      } @else {
                        <quader-no-value />
                      }
                    </td>
                  }
                  <td class="num" i18n="@@scanners.slots-value">{{ r.slots.inUse }} of {{ r.slots.total }}</td>
                </tr>
              }
            </quader-data-table>
          </quader-disclosure>
        }
      } @else {
        <p class="scanner-note" [attr.data-testid]="'scanners-none-' + g.kind">
          @switch (g.replicas) {
            @case ('reporting') {
              <span class="material-icons inline warn" aria-hidden="true">warning</span>
              <span i18n="@@scanners.none.reporting">No replica of <code translate="no">{{ g.deployment }}</code> has reported itself, so no {{ g.tool }} scanner is known to run.</span>
            }
            @case ('unverified') {
              <span class="material-icons inline" aria-hidden="true">info</span>
              <span i18n="@@scanners.none.unverified">The replicas of <code translate="no">{{ g.deployment }}</code> are not listed: without TLS, or without the name of their certificate, the scan listener cannot tell their kind.</span>
            }
            @case ('inProcess') {
              <span class="material-icons inline" aria-hidden="true">info</span>
              <span i18n="@@scanners.none.in-process">This installation runs no <code translate="no">{{ g.deployment }}</code> pod: the development role <code translate="no">all</code> analyses images in its own process.</span>
            }
            @case ('none') {
              <span class="material-icons inline" aria-hidden="true">info</span>
              <span i18n="@@scanners.none.none">This installation runs no <code translate="no">{{ g.deployment }}</code> pod: no {{ g.tool }} scanner is configured (<code translate="no">{{ urlKey() }}</code> is empty).</span>
            }
            @default {
              <span class="material-icons inline" aria-hidden="true">info</span>
              <span i18n="@@scanners.none.off">No replica of <code translate="no">{{ g.deployment }}</code> is listed.</span>
            }
          }
        </p>
      }
    }

    <ng-template #state let-r>
      <div class="state">
        @if (r.lost) {
          <span class="warn"><span class="material-icons" aria-hidden="true">warning</span><ng-container i18n="@@scanners.state.lost">Lost</ng-container></span>
        } @else {
          @switch (r.state) {
            @case ('ready') {
              <span class="ok"><span class="material-icons" aria-hidden="true">check_circle</span><ng-container i18n="@@scanners.state.ready">Ready</ng-container></span>
            }
            @case ('starting') {
              <span class="badge"><span class="material-icons" aria-hidden="true">hourglass_empty</span><ng-container i18n="@@scanners.state.starting">Starting</ng-container></span>
            }
            @case ('draining') {
              <span class="badge"><span class="material-icons" aria-hidden="true">hourglass_empty</span><ng-container i18n="@@scanners.state.draining">Draining</ng-container></span>
            }
            @case ('unready') {
              <span class="bad"><span class="material-icons" aria-hidden="true">error</span><ng-container i18n="@@scanners.state.unready">Unready</ng-container></span>
            }
          }
        }
        @if (r.reason && !r.lost) {
          <span class="reason">
            @if (reasonText(r.reason); as text) {
              {{ text }}
            } @else {
              <code translate="no">{{ r.reason }}</code>
            }
          </span>
        }
      </div>
    </ng-template>
  `,
  styles: `
    :host {
      display: block;
    }
    // The state and, beside it, why the replica is not ready: the reason's first words stand on the line of the replica's name
    // (R-UI-172), as a replica's failing checks do on the System page.
    .state {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      align-items: start;
      column-gap: var(--quader-space-2);
    }
    // In a row that stands as a block the column's name is the cell's first column, so the state starts on the name's line
    // (Q-UI-762), as the System page's replicas do.
    .quader-data-table.stacked td.state-cell {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      align-items: start;
      column-gap: var(--quader-space-1);
    }
    // A version is never broken (Q-UI-428): the column is as wide as it.
    .version {
      white-space: nowrap;
    }
    .database {
      display: inline-block;
      vertical-align: top;
    }
    .secondary {
      display: block;
      color: var(--quader-text-muted);
    }
    .scanner-note {
      margin: 0;
    }
  `,
})
export class ScannerReplicas {
  /** The answer of `GET /api/v1/system/scanners`. */
  readonly list = input.required<ScannerList>();
  /** The kind of scanner whose replicas are shown: `trivy`, `clamav`. */
  readonly kind = input.required<string>();
  /** The System page's columns, or the settings pages' in a fold. */
  readonly view = input<'system' | 'settings'>('settings');

  protected readonly group = computed(() => scannerGroup(this.list(), this.kind()));
  protected readonly reasonText = scannerReasonText;
  /** Only the kinds of scanner that keep a signature database of their own have its column (ClamAV). */
  protected readonly hasDatabase = computed(() => {
    const g = this.group();
    return g !== null && (g.purpose === 'malware' || g.rows.some((r: ScannerReplica) => r.database !== null));
  });
  protected readonly caption = computed(() => {
    const d = this.group()?.deployment ?? this.kind();
    return $localize`:@@scanners.caption:Replicas of ${d}:deployment:`;
  });
  protected readonly foldHeading = computed(() => {
    const g = this.group();
    const d = g?.deployment ?? this.kind();
    const n = g?.rows.length ?? 0;
    return $localize`:@@scanners.fold:Replicas of ${d}:deployment: (${n}:count:)`;
  });
  /** The setting that names the kind's scanner, empty without one (R-VULN-041, R-MAL-005). */
  protected readonly urlKey = computed(() =>
    this.group()?.purpose === 'malware' ? 'malware.scanner.url' : 'vulnerabilities.images.scanner.url',
  );

  /** The columns of every replica of the System page, with the tool in place of the replica's view of Valkey (Q-UI-897). */
  protected readonly systemColumns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@scanners.col.name:Name`, width: '10rem', prefer: true },
    { id: 'node', label: $localize`:@@scanners.col.node:Node`, width: '6rem' },
    { id: 'version', label: $localize`:@@scanners.col.version:Version`, width: '5rem' },
    { id: 'state', label: $localize`:@@scanners.col.state:State`, width: '12rem' },
    {
      id: 'since',
      label: $localize`:@@scanners.col.since:State changed`,
      help: $localize`:@@scanners.col.since-help:When the replica entered the state it shows, or the reason beside it changed.`,
    },
    { id: 'heartbeat', label: $localize`:@@scanners.col.heartbeat:Heartbeat` },
    { id: 'tool', label: $localize`:@@scanners.col.tool:Tool` },
  ];

  /** The settings pages' columns: what an administrator of the scanning decides on, the database only for ClamAV. */
  protected readonly settingsColumns = computed((): readonly TableColumn[] => [
    { id: 'name', label: $localize`:@@scanners.col.name:Name`, width: '9rem', prefer: true },
    { id: 'state', label: $localize`:@@scanners.col.state:State`, width: '12rem' },
    { id: 'tool', label: $localize`:@@scanners.col.tool:Tool` },
    ...(this.hasDatabase()
      ? [{ id: 'database', label: $localize`:@@scanners.col.database:Signature database` } satisfies TableColumn]
      : []),
    {
      id: 'slots',
      label: $localize`:@@scanners.col.slots:Slots`,
      numeric: true,
      help: $localize`:@@scanners.col.slots-help:The scans the replica runs now, of the scans it runs at a time.`,
    },
  ]);
}
