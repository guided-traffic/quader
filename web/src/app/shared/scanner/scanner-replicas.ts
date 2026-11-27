import { NgTemplateOutlet } from '@angular/common';
import { TimePipe } from '../time-pipe';
import { Component, computed, input } from '@angular/core';
import { MalwareItemRef } from '../../api/malware-api';
import { ScannerList, ScannerReplica } from '../../api/system-api';
import { DataTable, TableColumn } from '../ui/data-table';
import { Disclosure } from '../ui/disclosure';
import { NoValue } from '../ui/no-value';
import { TimeAgo } from '../ui/time-ago';
import { podDatabase, scannerGroup } from './scanner-model';

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
    case 'seed':
      return $localize`:@@scanners.reason.seed:Seed not finished yet`;
    default:
      return null;
  }
}

/**
 * The replicas of one kind of scanner (R-OPS-048, Q-UI-897), the one table of the screens that show them: the System page
 * (`view="system"`, the columns of every replica there: name, node, version, state, since when, heartbeat, and the tool in place of
 * Valkey), and the settings of the malware scanning and the advisory sources (`view="settings"`, closed in a fold under the page's own
 * summary: name, state, the tool's version, the database, the slots), and the section **Updater** of `upstream/clamav`'s
 * page (`view="open"`, the settings' columns without the fold, R-UI-188). The updater (`freshclam`) runs no calls: its rows have no
 * slots, and its database is the set its directory holds with the age of its newest `daily` (Q-UI-913). An image scanner's rows say
 * whether the pod holds the database in use with the age of its build, an older one, or none yet: in the column **Database** of the
 * settings, under the tool on the System page (R-VULN-049, Q-OPS-225, Q-UI-915). A malware scanner's rows say in the settings' column
 * **Working on** what each of its calls checks now and since when, from its own report (R-MAL-041, Q-UI-1527): the file where the page
 * knows it (`callItems`, the controller's queue for a reader with `content:metadata-read`), else the blob's digest. A kind without a replica says why in a sentence:
 * none reported, the scan listener cannot tell the kind, or no scanner is configured. Every value is the scanner's own statement,
 * bounded by the server (H-1161), and is bound as text.
 */
@Component({
  selector: 'quader-scanner-replicas',
  imports: [DataTable, TimePipe, Disclosure, NgTemplateOutlet, NoValue, TimeAgo],
  template: `
    @if (group(); as g) {
      @if (g.rows.length > 0) {
        @if (view() === 'system') {
          <quader-data-table [caption]="caption()" [columns]="systemColumns()">
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
                  <time [attr.datetime]="r.heartbeatAt">{{ r.heartbeatAt | time: 'time' }}</time>
                </td>
                <!-- The tool, and under it the database an image scanner's pod detects with (Q-UI-915): the tool and its data in one cell,
                     so that the table keeps its columns at 1440 px. -->
                <td class="tool-cell">
                  <span class="tool" translate="no">
                    @if (r.toolVersion; as v) {
                      {{ g.tool }} {{ v }}
                    } @else {
                      <quader-no-value />
                    }
                  </span>
                  @if (podDatabases()) {
                    <ng-container *ngTemplateOutlet="podDatabaseCell; context: { $implicit: r }" />
                  }
                </td>
              </tr>
            }
          </quader-data-table>
        } @else if (view() === 'open') {
          <ng-container *ngTemplateOutlet="settingsTable; context: { $implicit: g }" />
        } @else {
          <quader-disclosure [inline]="true" [heading]="foldHeading()">
            <ng-container *ngTemplateOutlet="settingsTable; context: { $implicit: g }" />
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
              <!-- This line and the two below inform: no icon, as ⓘ is the help sign alone (Q-UI-1628). -->
              <span i18n="@@scanners.none.unverified">The replicas of <code translate="no">{{ g.deployment }}</code> are not listed: without TLS, or without the name of their certificate, the scan listener cannot tell their kind.</span>
            }
            @case ('none') {
              <!-- No icon (above).
                   The updater is no scanner and has a setting of its own (R-MAL-032): it said "no freshclam scanner is configured
                   (malware.scanner.url is empty)" on a start without it (Q-UI-1003). The pod's name is wrapped for the German sentence,
                   which puts its colon right after it (Q-UI-1615). -->
              @if (updater()) {
                <span i18n="@@scanners.none.updater">This installation runs no <span class="with-stop"><code translate="no">{{ g.deployment }}</code></span> pod: no {{ g.tool }} updater is configured (<code translate="no">{{ urlKey() }}</code> is empty).</span>
              } @else {
                <span i18n="@@scanners.none.none">This installation runs no <span class="with-stop"><code translate="no">{{ g.deployment }}</code></span> pod: no {{ g.tool }} scanner is configured (<code translate="no">{{ urlKey() }}</code> is empty).</span>
              }
            }
            @default {
              <!-- No icon (above). -->
              <span i18n="@@scanners.none.off">No replica of <code translate="no">{{ g.deployment }}</code> is listed.</span>
            }
          }
        </p>
      }
    }

    <ng-template #settingsTable let-g>
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
            @if (podDatabases()) {
              <td class="database-cell">
                <ng-container *ngTemplateOutlet="podDatabaseCell; context: { $implicit: r }" />
              </td>
            } @else if (hasDatabase()) {
              <td>
                @if (r.database; as db) {
                  <!-- One box, so that the date stands under the version also where the row stands as a block. -->
                  <span class="database">
                    <code class="version" translate="no">{{ db.version }}</code>
                    @if (db.builtAt; as built) {
                      @if (updater()) {
                        <!-- The updater's newest daily with its age: how current the definitions it offers are (R-UI-188). -->
                        <span class="secondary"
                          ><ng-container i18n="@@scanners.updater.daily">daily built</ng-container>&ngsp;<quader-time-ago [at]="built" absolute="title"
                        /></span>
                      } @else {
                        <span class="secondary"><time [attr.datetime]="built" [attr.title]="built | time">{{ built | time: 'date' }}</time></span>
                      }
                    }
                  </span>
                } @else {
                  <quader-no-value />
                }
              </td>
            }
            @if (hasSlots()) {
              <td class="num" i18n="@@scanners.slots-value">{{ r.slots.inUse }} of {{ r.slots.total }}</td>
            }
            @if (hasCalls()) {
              <td class="calls-cell" data-testid="scanner-calls">
                @if (r.calls?.length) {
                  <ul class="calls">
                    @for (c of r.calls; track c.sha256) {
                      <li>
                        @if (callItem(c.sha256); as item) {
                          <code class="call-path" translate="no" [attr.title]="item.repository + ' · sha256:' + c.sha256">{{ item.path }}</code>
                        } @else {
                          <code translate="no" [attr.title]="'sha256:' + c.sha256">sha256:{{ c.sha256.slice(0, 12) }}</code>
                        }
                        &ngsp;<span class="secondary-inline"><quader-time-ago [at]="c.since" absolute="title" /></span>
                      </li>
                    }
                  </ul>
                } @else {
                  <quader-no-value />
                }
              </td>
            }
          </tr>
        }
      </quader-data-table>
    </ng-template>

    <!-- The database an image scanner's pod holds (R-VULN-049, Q-OPS-225, Q-UI-915): a badge that says which, and beside it, for the
         database in use, how long ago it was built. A block of its own: the settings' column **Database**, or the lines under the tool
         on the System page. The digest is its title: it names the database for whoever compares two pods, and says nothing at a
         glance. -->
    <ng-template #podDatabaseCell let-r>
      <span class="database-state" [attr.title]="r.database ? 'sha256:' + r.database.version : null">
        @switch (database(r)) {
          @case ('current') {
            <span class="ok"><span class="material-icons" aria-hidden="true">check_circle</span><ng-container i18n="@@scanners.db.current">Database in use</ng-container></span>
            <span class="built"
              ><ng-container i18n="@@scanners.db.built">built</ng-container>&ngsp;<quader-time-ago [at]="r.database.builtAt" absolute="title"
            /></span>
          }
          @case ('older') {
            <span class="badge"><span class="material-icons" aria-hidden="true">hourglass_empty</span><ng-container i18n="@@scanners.db.older">Older database</ng-container></span>
          }
          @default {
            <span class="badge"><span class="material-icons" aria-hidden="true">hourglass_empty</span><ng-container i18n="@@scanners.db.none">No database yet</ng-container></span>
          }
        }
      </span>
    </ng-template>

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
    // (Q-UI-762), as the System page's replicas do; the name keeps the width of the list's column of names (Q-UI-1050).
    .quader-data-table.stacked :is(td.state-cell, td.database-cell) {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      align-items: start;
      column-gap: 0;
    }
    // A version is never broken (Q-UI-428): the column is as wide as it.
    .version {
      white-space: nowrap;
    }
    // In a row that stands as a block it breaks inside itself where the column of values ends (Q-UI-1050): a database's version of
    // 32 characters beside the column of names pushed the block 64 px past the panel at 390 px, as a media type did (W-24).
    .quader-data-table.stacked .version {
      white-space: normal;
      overflow-wrap: anywhere;
    }
    .database {
      display: inline-block;
      vertical-align: top;
    }
    // An image scanner's database: the badge, and the age of the database in use. In the settings' column the age stands beside the
    // badge, so that the cell's first text is on the row's line (R-UI-176); under the tool on the System page it stands under the
    // badge, so that the column is no wider than the badge and the table keeps its columns at 1440 px.
    .database-state {
      display: flex;
      align-items: flex-start;
      column-gap: var(--quader-space-2);
    }
    // In a row that stands as a block (390 px) the age stands under the badge, as under the tool on the System page: beside it the German
    // "Datenbank in Verwendung" and "erstellt vor 5 Stunden" reached 81 px past the panel (R-VULN-053's renders).
    .quader-data-table.stacked .database-state {
      flex-direction: column;
    }
    td.tool-cell .database-state {
      flex-direction: column;
      margin-block-start: var(--quader-space-1);
    }
    // The tool and its version are one value, never broken, as a cell marked translate="no" is (Q-UI-428); so is the age of a
    // build, which beside the badge stood as "built" over "5 hours ago" while the name's column had the room.
    .tool,
    .built {
      white-space: nowrap;
    }
    .secondary {
      display: block;
      color: var(--quader-text-muted);
    }
    .scanner-note {
      margin: 0;
    }
    // What a malware scanner checks now (Q-UI-1527): one line a call, the file or the digest and how long it has been at it.
    .calls {
      margin: 0;
      padding: 0;
      list-style: none;
    }
    .call-path {
      overflow-wrap: anywhere;
    }
    .secondary-inline {
      color: var(--quader-text-muted);
      white-space: nowrap;
    }
  `,
})
export class ScannerReplicas {
  /** The answer of `GET /api/v1/system/scanners`. */
  readonly list = input.required<ScannerList>();
  /** The kind of scanner whose replicas are shown: `trivy`, `clamav`. */
  readonly kind = input.required<string>();
  /** The System page's columns, the settings pages' in a fold, or the settings' columns open (the section **Updater**, R-UI-188). */
  readonly view = input<'system' | 'settings' | 'open'>('settings');
  /**
   * The files the page knows by their blob's digest (Q-UI-1527): **Working on** names a call by its file where this holds it, by the
   * digest otherwise. Only a reader with `content:metadata-read` is given paths by the server (Q-MAL-137).
   */
  readonly callItems = input<ReadonlyMap<string, MalwareItemRef>>(new Map());

  protected readonly group = computed(() => scannerGroup(this.list(), this.kind()));
  protected readonly reasonText = scannerReasonText;
  /** The malware scanning's updater (`freshclam`, R-MAL-032): no calls, so no slots; its database is the set its directory holds. */
  protected readonly updater = computed(() => this.kind() === 'freshclam');
  /** A kind that runs calls has the column of its slots; the updater has none (Q-UI-913). */
  protected readonly hasSlots = computed(() => !this.updater());
  /** Only the kinds of scanner that keep a signature database of their own have its column (ClamAV). */
  protected readonly hasDatabase = computed(() => {
    const g = this.group();
    return g !== null && (g.purpose === 'malware' || g.rows.some((r: ScannerReplica) => r.database !== null));
  });
  /**
   * The image scanner (`trivy`): each pod holds a copy of the advisory database that its detections need, so its rows say on every
   * screen, the System page too, whether that is the database in use (R-VULN-049, Q-OPS-225, Q-UI-915).
   */
  protected readonly podDatabases = computed(() => {
    const g = this.group();
    return g !== null && (g.purpose === 'vuln' || g.kind === 'trivy');
  });
  protected readonly database = podDatabase;
  /** A malware scanner says what it works on (`calls`, R-MAL-041); the other kinds' rows carry none. */
  protected readonly hasCalls = computed(() => {
    const g = this.group();
    if (g === null || this.updater()) return false;
    return g.kind === 'clamav' || g.rows.some((r: ScannerReplica) => r.calls !== null && r.calls !== undefined);
  });

  protected callItem(sha256: string): MalwareItemRef | null {
    return this.callItems().get(sha256) ?? null;
  }
  /** The column of an image scanner's database, and what its three words mean. */
  private readonly podDatabaseColumn: TableColumn = {
    id: 'database',
    label: $localize`:@@scanners.col.pod-database:Database`,
    help: $localize`:@@scanners.col.pod-database-help:The advisory database the pod detects with. Database in use: the one Quader uses now, with how long ago it was built. Older database: the pod is still loading the one in use, which takes under a minute, or its container database cannot load it; that container's log says why. No database yet: the pod's first load has not finished.`,
  };
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
  /** The setting that names the kind's scanner or the updater, empty without one (R-VULN-041, R-MAL-005, R-MAL-032). */
  protected readonly urlKey = computed(() =>
    this.updater() ? 'malware.updater.url' : this.group()?.purpose === 'malware' ? 'malware.scanner.url' : 'vulnerabilities.images.scanner.url',
  );

  /**
   * The columns of every replica of the System page, with the tool in place of the replica's view of Valkey (Q-UI-897); for the image
   * scanner the database its pod holds stands under the tool, whose help then says what its words mean (Q-UI-915).
   */
  protected readonly systemColumns = computed((): readonly TableColumn[] => [
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
    this.podDatabases()
      ? { id: 'tool', label: $localize`:@@scanners.col.tool:Tool`, help: this.podDatabaseColumn.help }
      : { id: 'tool', label: $localize`:@@scanners.col.tool:Tool` },
  ]);

  /** The settings pages' columns: what an administrator of the scanning decides on, the database the pod holds or scans with. */
  protected readonly settingsColumns = computed((): readonly TableColumn[] => [
    { id: 'name', label: $localize`:@@scanners.col.name:Name`, width: '9rem', prefer: true },
    { id: 'state', label: $localize`:@@scanners.col.state:State`, width: '12rem' },
    { id: 'tool', label: $localize`:@@scanners.col.tool:Tool` },
    ...(this.podDatabases()
      ? [this.podDatabaseColumn]
      : this.hasDatabase()
      ? [
          (this.updater()
            ? { id: 'database', label: $localize`:@@scanners.col.updater-database:Definitions it holds` }
            : { id: 'database', label: $localize`:@@scanners.col.database:Signature database` }) satisfies TableColumn,
        ]
      : []),
    ...(this.hasSlots()
      ? [
          {
            id: 'slots',
            label: $localize`:@@scanners.col.slots:Slots`,
            numeric: true,
            help: $localize`:@@scanners.col.slots-help:The scans the replica runs now, of the scans it runs at a time.`,
          } satisfies TableColumn,
        ]
      : []),
    ...(this.hasCalls()
      ? [
          {
            id: 'calls',
            label: $localize`:@@scanners.col.calls:Working on`,
            help: $localize`:@@scanners.col.calls-help:What the replica checks now, by its own report, and for how long.`,
          } satisfies TableColumn,
        ]
      : []),
  ]);
}
