import { Component, computed, inject, input, linkedSignal, resource, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { ApiError } from '../../api/problem';
import { RepositoriesApi } from '../../api/repositories-api';
import { UpstreamApi, UpstreamRepository, UpstreamVisibilityLevel } from '../../api/upstream-api';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatIconView } from '../../shared/format-icon';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { ListHead } from '../../shared/ui/list-head';
import { NoValue } from '../../shared/ui/no-value';
import { PageHeader } from '../../shared/ui/page-header';
import { Switch } from '../../shared/ui/switch';
import { VisibilityIcon, installationReasons, levelName } from '../../shared/ui/visibility-level';
import { UpstreamActions } from './upstream-actions';
import { inUseBadge, levelOptions, onOffWord, privateRowNote, rowBadge, rowNotes, switchLabel } from './upstream-words';

/** One group of the list: a format and its well-known repositories, in catalog order. */
interface FormatGroup {
  readonly format: string;
  readonly rows: readonly UpstreamRepository[];
}

/** The answer of a write in place of the row it changed, the others as they are. */
function replaced(items: readonly UpstreamRepository[], next: readonly UpstreamRepository[]): UpstreamRepository[] {
  const byKey = new Map(next.map((r) => [r.key, r] as const));
  return items.map((r) => byKey.get(r.key) ?? r);
}

/**
 * **Upstream registries** of the admin area (R-UI-187, Q-UI-830, the owner's page of 2026-11-20): every well-known repository of
 * the built-in project `upstream` in one list grouped by format in catalog order (`listUpstreamRepositories`), each row with its
 * switch, its level and the number of virtual repositories that list it, and the system repositories under **Used by Quader**
 * (Q-UI-834). **Switch all on** and **Set visibility of all** are the header's actions; a switch and a level are saved at once,
 * with the questions of `UpstreamActions`. A reader (`repositories:read-config` without `repositories:write-config`) sees the
 * words, no control. The search filters by display name and key in the browser: the list is the catalog's and not paged.
 */
@Component({
  selector: 'quader-upstream-registries',
  imports: [DataTable, ErrorNotice, FormatIconView, ListHead, NoValue, PageHeader, RouterLink, Switch, VisibilityIcon],
  template: `
    <quader-page-header [heading]="heading" [count]="items() === null ? null : presets().length" [description]="description">
      @if (canWrite()) {
        <button actions class="btn btn-default" type="button" [disabled]="bulkBusy() || items() === null" (click)="setAll()" i18n="@@wellknown.set-all">
          Set visibility of all
        </button>
      }
      @if (canWrite()) {
        <button
          actions
          class="btn btn-confirm"
          type="button"
          [disabled]="bulkBusy() || offRows().length === 0"
          [attr.aria-describedby]="allOn() ? 'upstream-all-on' : null"
          (click)="switchAllOn()"
          i18n="@@wellknown.all-on"
        >
          Switch all on
        </button>
      }
    </quader-page-header>
    @if (canWrite() && allOn()) {
      <p id="upstream-all-on" class="hint all-on" i18n="@@wellknown.all-on.none">Every registry is on.</p>
    }

    @if (actionError(); as e) {
      <quader-error-notice [error]="e" />
    }

    <quader-list-head [searchLabel]="searchLabel" [search]="search()" (searchChange)="find($event)" [filters]="false" />

    <quader-data-table
      class="registries"
      [caption]="heading"
      [columns]="columns()"
      [loaded]="items() !== null"
      [loading]="items() === null && !loadError()"
      [error]="loadError()"
      (retry)="load()"
      [empty]="groups().length === 0"
      [emptyText]="search() ? noMatch : noRegistry"
    >
      @for (g of groups(); track g.format) {
        <!-- The group's head: the format's icon and its name (R-UI-145), across the row. -->
        <tr class="group-row">
          <th scope="rowgroup" [attr.colspan]="columns().length">
            <span class="format-name"><quader-format-icon [format]="g.format" /><span translate="no">{{ formatLabel(g.format) }}</span></span>
          </th>
        </tr>
        @for (r of g.rows; track r.key) {
          <tr>
            <th scope="row">
              <span class="icon-name-text">
                <a [routerLink]="[r.key]" translate="no">{{ r.displayName }}</a>
                <span class="secondary" translate="no">{{ r.name }}</span>
                @for (note of notes(r); track note) {
                  <span class="secondary">{{ note }}</span>
                }
                <!-- The note of a private registry that is on stands with the row's other notes, so that the level's cell holds its
                     control alone and its line is the row's (R-UI-176, Q-UI-839). -->
                @if (r.visibility === 'private' && r.state === 'active') {
                  <span class="secondary">{{ privateNote }}</span>
                }
              </span>
            </th>
            <td class="state-cell">
              @if (canWrite() && r.state !== 'retired') {
                <quader-switch
                  [checked]="r.state === 'active'"
                  [label]="switchName(r)"
                  [busy]="busy().has(r.key)"
                  (checkedChange)="toggle(r, $event)"
                />
              } @else if (r.state !== 'retired') {
                <span class="word">{{ word(r) }}</span>
              }
              @if (badge(r); as b) {
                <span class="badge warn"><span class="material-icons" aria-hidden="true">warning</span>{{ b }}</span>
              }
            </td>
            <td class="level-cell">
              @if (canWrite() && r.state !== 'retired') {
                <!-- The level's icon and its select as one control, the icon on the select's middle in a row and in a block. -->
                <span class="level-control">
                <quader-visibility-icon [level]="r.visibility" />
                <select
                  class="select-input level-select"
                  [attr.aria-label]="levelLabel(r)"
                  [attr.aria-invalid]="levelErrors().get(r.key) ? 'true' : null"
                  [attr.aria-describedby]="levelErrors().get(r.key) ? 'level-error-' + r.key : null"
                  [disabled]="busy().has(r.key)"
                  (change)="setLevel(r, $any($event.target))"
                >
                  @for (o of levels; track o.value) {
                    <option [value]="o.value" [selected]="o.value === r.visibility" [disabled]="!!reasons()[o.value] && o.value !== r.visibility">
                      {{ o.label }}
                    </option>
                  }
                </select>
                </span>
              } @else {
                <quader-visibility-icon [level]="r.visibility" />{{ levelText(r.visibility) }}
              }
              @if (levelErrors().get(r.key); as message) {
                <span class="field-error" role="alert" [id]="'level-error-' + r.key">{{ message }}</span>
              }
            </td>
            <td class="num">
              @if (r.listedBy > 0) {
                {{ r.listedBy }}
              } @else {
                <quader-no-value />
              }
            </td>
            <td class="actions">
              <a class="btn btn-tertiary" [routerLink]="[r.key]"
                ><ng-container i18n="@@wellknown.details">Details</ng-container
                ><span class="visually-hidden" translate="no">&ngsp;{{ r.displayName }}</span></a
              >
            </td>
          </tr>
        }
      }
    </quader-data-table>

    @if (system().length > 0) {
      <h2 class="system-heading" id="upstream-system" i18n="@@wellknown.system.heading">Used by Quader</h2>
      <p class="hint system-hint" i18n="@@wellknown.system.hint">Repositories Quader reads for its own work. They have no switch: the work that uses them decides.</p>
      <quader-data-table [caption]="systemCaption" [columns]="systemColumns">
        @for (r of system(); track r.key) {
          <tr>
            <th scope="row">
              <span class="icon-name-text">
                <a [routerLink]="[r.key]" translate="no">{{ r.displayName }}</a>
                <span class="secondary" translate="no">{{ r.name }}</span>
                @for (note of notes(r); track note) {
                  <span class="secondary">{{ note }}</span>
                }
              </span>
            </th>
            <td>
              @if (r.inUse) {
                <span class="ok"><span class="material-icons" aria-hidden="true">check_circle</span>{{ inUse(r) }}</span>
              } @else {
                <span class="badge">{{ inUse(r) }}</span>
              }
            </td>
            <td class="level-cell"><quader-visibility-icon [level]="r.visibility" />{{ levelText(r.visibility) }}</td>
            <td class="actions">
              <a class="btn btn-tertiary" [routerLink]="[r.key]"
                ><ng-container i18n="@@wellknown.details">Details</ng-container
                ><span class="visually-hidden" translate="no">&ngsp;{{ r.displayName }}</span></a
              >
            </td>
          </tr>
        }
      </quader-data-table>
    }
  `,
  styleUrl: './upstream.scss',
})
export class UpstreamRegistries {
  private readonly api = inject(UpstreamApi);
  private readonly repositories = inject(RepositoriesApi);
  private readonly actions = inject(UpstreamActions);
  private readonly session = inject(SessionStore);
  private readonly router = inject(Router);

  /** The search of the address (`?q=`, R-UI-003). */
  readonly q = input<string | undefined>();

  protected readonly heading = $localize`:@@wellknown.heading:Upstream registries`;
  protected readonly description = $localize`:@@wellknown.description:The public registries this installation offers to every project. Each is off until you switch it on.`;
  protected readonly searchLabel = $localize`:@@wellknown.search:Filter registries`;
  protected readonly noRegistry = $localize`:@@wellknown.empty:Quader offers no registry yet.`;
  protected readonly noMatch = $localize`:@@wellknown.empty-filtered:No registry matches.`;
  protected readonly systemCaption = $localize`:@@wellknown.system.heading:Used by Quader`;
  protected readonly privateNote = privateRowNote();
  protected readonly levels = levelOptions();
  protected readonly levelText = levelName;
  protected readonly word = onOffWord;
  protected readonly badge = rowBadge;
  protected readonly notes = rowNotes;
  protected readonly switchName = switchLabel;
  protected readonly inUse = inUseBadge;

  protected readonly canWrite = computed(() => this.session.has('repositories:write-config'));
  protected readonly items = signal<readonly UpstreamRepository[] | null>(null);
  protected readonly loadError = signal<ApiError | null>(null);
  protected readonly actionError = signal<ApiError | null>(null);
  /** The keys whose write is open: their controls hold. */
  protected readonly busy = signal<ReadonlySet<string>>(new Set());
  protected readonly bulkBusy = signal(false);
  /** A refusal of a level, at the row's select (R-UI-187: `public_not_allowed`, `visibility_level_restricted`). */
  protected readonly levelErrors = signal<ReadonlyMap<string, string>>(new Map());

  protected readonly search = linkedSignal(() => this.q() ?? '');
  private readonly formats = resource({ loader: () => this.repositories.formats() });
  private readonly choices = resource({ loader: () => this.actions.choices() });
  /** The levels the caller may not choose, by level (R-UI-141); a row keeps the level it has. */
  protected readonly reasons = computed(() => installationReasons(valueOf(this.choices) ?? null));

  protected readonly presets = computed(() => (this.items() ?? []).filter((r) => r.kind === 'preset'));
  protected readonly system = computed(() => (this.items() ?? []).filter((r) => r.kind === 'system'));
  protected readonly offRows = computed(() =>
    this.presets().filter((r) => r.state === 'not-switched-on' || r.state === 'switched-off'),
  );
  /** Nothing is off: **Switch all on** is disabled and says why. */
  protected readonly allOn = computed(() => this.items() !== null && this.offRows().length === 0);
  /** The preset repositories that match the search, grouped by format in the list's order (catalog order). */
  protected readonly groups = computed<readonly FormatGroup[]>(() => {
    const q = this.search().trim().toLowerCase();
    const out: { format: string; rows: UpstreamRepository[] }[] = [];
    for (const r of this.presets()) {
      if (q && !r.key.includes(q) && !r.displayName.toLowerCase().includes(q)) continue;
      const group = out.find((g) => g.format === r.format);
      if (group) group.rows.push(r);
      else out.push({ format: r.format, rows: [r] });
    }
    return out;
  });

  /**
   * The registry names it and says what it is (its least width holds "Ubuntu security updates" beside its address); the state, the
   * level and the count never break and are as wide as their values (Q-UI-563); Details is the row's one action (R-UI-163).
   */
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'registry', label: $localize`:@@wellknown.col.registry:Registry`, width: '14rem' },
    // Stacked, the switch and its word say the state; "On" before "Off" would say both (R-UI-174).
    { id: 'state', label: $localize`:@@wellknown.col.on:On`, stackLabel: false },
    { id: 'visibility', label: $localize`:@@projects.col.visibility:Visibility` },
    {
      id: 'listed',
      label: $localize`:@@wellknown.col.listed:Listed by`,
      numeric: true,
      help: $localize`:@@wellknown.help.listed:The number of virtual repositories, in every project, that list the registry as a member.`,
    },
    { id: 'actions', label: $localize`:@@projects.col.actions:Actions`, hideLabel: true },
  ]);
  protected readonly systemColumns: readonly TableColumn[] = [
    { id: 'repository', label: $localize`:@@wellknown.col.repository:Repository`, width: '14rem' },
    { id: 'use', label: $localize`:@@wellknown.col.use:Use` },
    { id: 'visibility', label: $localize`:@@projects.col.visibility:Visibility` },
    { id: 'actions', label: $localize`:@@projects.col.actions:Actions`, hideLabel: true },
  ];

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    this.loadError.set(null);
    try {
      this.items.set(await this.api.list());
    } catch (e) {
      this.loadError.set(asApiError(e));
    }
  }

  protected formatLabel(name: string): string {
    return (valueOf(this.formats) ?? []).find((f) => f.name === name)?.displayName ?? name;
  }

  protected levelLabel(r: UpstreamRepository): string {
    return $localize`:@@wellknown.level.label:Visibility of ${r.displayName}:name:`;
  }

  /** The search is the address's `q`, as on every list, so a filtered list can be linked; the list filters in the browser. */
  protected find(text: string): void {
    this.search.set(text);
    void this.router.navigate([], { queryParams: { q: text.trim() || null }, queryParamsHandling: 'merge' });
  }

  protected async toggle(r: UpstreamRepository, on: boolean): Promise<void> {
    this.actionError.set(null);
    await this.hold(r.key, async () => {
      const saved = await this.actions.toggle(r, on);
      if (saved) this.take([saved]);
    });
  }

  protected async setLevel(r: UpstreamRepository, select: HTMLSelectElement): Promise<void> {
    const level = select.value as UpstreamVisibilityLevel;
    this.actionError.set(null);
    this.levelErrors.update((m) => {
      const next = new Map(m);
      next.delete(r.key);
      return next;
    });
    await this.hold(r.key, async () => {
      try {
        const saved = await this.actions.setLevel(r, level);
        if (saved) this.take([saved]);
        else select.value = r.visibility;
      } catch (e) {
        select.value = r.visibility;
        const error = asApiError(e);
        if (error.code === 'public_not_allowed' || error.code === 'visibility_level_restricted') {
          this.levelErrors.update((m) => new Map(m).set(r.key, error.problem?.detail ?? error.message));
        } else {
          throw e;
        }
      }
    });
  }

  protected async switchAllOn(): Promise<void> {
    await this.bulk(async () => {
      const result = await this.actions.switchAllOn(this.offRows());
      if (result) this.take(result.items);
    });
  }

  protected async setAll(): Promise<void> {
    await this.bulk(async () => {
      const result = await this.actions.setAllVisibility();
      if (result) this.take(result.items);
    });
  }

  private take(next: readonly UpstreamRepository[]): void {
    this.items.update((items) => (items ? replaced(items, next) : items));
  }

  private async hold(key: string, run: () => Promise<void>): Promise<void> {
    if (this.busy().has(key)) return;
    this.busy.update((s) => new Set(s).add(key));
    try {
      await run();
    } catch (e) {
      this.actionError.set(asApiError(e));
    } finally {
      this.busy.update((s) => {
        const next = new Set(s);
        next.delete(key);
        return next;
      });
    }
  }

  private async bulk(run: () => Promise<void>): Promise<void> {
    if (this.bulkBusy()) return;
    this.bulkBusy.set(true);
    this.actionError.set(null);
    try {
      await run();
    } catch (e) {
      this.actionError.set(asApiError(e));
      await this.load();
    } finally {
      this.bulkBusy.set(false);
    }
  }
}
