import { NgTemplateOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, effect, inject, input, linkedSignal, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { BastionProject, BastionProjectList, BastionsApi } from '../../api/bastions-api';
import { DEFAULT_PAGE_LIMIT, MAX_PAGE_LIMIT } from '../../api/pagination';
import { ApiError } from '../../api/problem';
import { LIVE_IDLE_MS, live } from '../../core/live-refresh';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { Confirm } from '../../shared/confirm-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Busy } from '../../shared/ui/busy';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { Disclosure } from '../../shared/ui/disclosure';
import { Help } from '../../shared/ui/help';
import { ListHead } from '../../shared/ui/list-head';
import { PageHeader } from '../../shared/ui/page-header';
import { PrincipalField, refOf } from '../../shared/ui/principal-field';
import { ProjectMark } from '../../shared/ui/project-mark';
import { TimeAgo } from '../../shared/ui/time-ago';

/** The rows a card's section **Projects** shows at most; with more, its link leads to the Bastion's projects page (Q-UI-1637). */
export const CARD_PROJECTS = 10;
/** The keys a row's **Distributed here** names; the rest it counts, so a project of hundreds of repositories keeps its row short. */
export const REPOSITORIES_NAMED = 5;

/** "5 repositories distributed", "1 repository distributed", "No repository distributed yet": the section's line (R-UI-240). */
export function distributedNote(repositories: number): string {
  if (repositories === 0) return $localize`:@@bastion.projects.note-zero:No repository distributed yet`;
  return repositories === 1
    ? $localize`:@@bastion.projects.note-one:1 repository distributed`
    : $localize`:@@bastion.projects.note:${repositories}:count: repositories distributed`;
}

/**
 * The projects a Bastion allows (R-UI-240, [bastion.md] R-BAS-134, Q-UI-1637): the administrator's allow list, whose projects' Owners may
 * distribute their repositories through the Bastion. One component in two places:
 *
 * - **On the card** (`limit` set): a closed section **Projects** with its help, the number of projects allowed and one line, "5 repositories
 *   distributed" (or "No project may distribute through it yet."), which costs the default card one line. Opened, **Allow a project** (the
 *   shared search field, which lists the matching projects while one types, R-UI-150) with **Allow**, and the table of at most `limit`
 *   projects; with more, "All 37 projects" leads to the Bastion's projects page. A reader without `endpoints:write` sees the list and no
 *   act; with no project, the section is that one line.
 * - **On the page** (`limit` null, `/admin/bastions/<name>/projects`): the same form and table, unbounded, with the filter by key or display
 *   name and **Load more** (R-API-010).
 *
 * Each row: the project's mark, its display name and key; **Distributed here**, the keys of the repositories whose Owner chose the Bastion
 * (the first five, then how many more) or "None yet"; **Allowed**, when and by whom; and **Remove**, whose dialog says that the project's
 * distributed repositories stop being served there and that their Owners' choices go with it. The list is read again every minute while
 * it is seen (R-UI-213): an Owner's choice changes its counts without the viewer.
 */
@Component({
  selector: 'quader-bastion-projects',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Busy, DataTable, Disclosure, ErrorNotice, Help, ListHead, NgTemplateOutlet, PrincipalField, ProjectMark, RouterLink, TimeAgo],
  template: `
    <ng-template #body>
      @if (canWrite()) {
        <!-- The administrator's act: a project chosen from the matches, never a typed key (R-UI-150). -->
        <form class="allow field-action-row" (submit)="allow($event)" novalidate data-testid="bastion-projects-allow">
          <quader-principal-field
            [label]="allowLabel"
            name="project"
            [kinds]="projectKind"
            [exclude]="shownRefs()"
            [(value)]="choice"
          />
          <button class="btn btn-confirm" type="submit" [disabled]="busy() || choice().length === 0" [quaderBusy]="busy()" i18n="@@bastion.projects.allow">
            Allow
          </button>
        </form>
      }
      @if (actionError(); as e) {
        <quader-error-notice [error]="e" />
      }
      @if (page()) {
        <quader-list-head [searchLabel]="filterLabel" [search]="q()" (searchChange)="q.set($event)" [filters]="false" />
      }
      @if (page() || (rows()?.length ?? 0) > 0 || failure()) {
        <quader-data-table
          [caption]="caption()"
          [columns]="columns()"
          [loaded]="rows() !== null"
          [loading]="rows() === null && !failure()"
          loadingText="Loading the projects."
          i18n-loadingText="@@projects.loading"
          [error]="failure()"
          (retry)="load()"
          [empty]="(rows()?.length ?? 0) === 0"
          [emptyText]="emptyText()"
          [hasMore]="page() && next() !== null"
          (more)="more()"
        >
          @for (p of rows(); track p.project) {
            <tr>
              <th scope="row">
                <span class="icon-name">
                  <quader-project-mark [key]="p.project" [name]="p.displayName" size="medium" />
                  <span class="icon-name-text">
                    <a [routerLink]="['/projects', p.project]" translate="no">{{ p.displayName || p.project }}</a>
                    <span class="secondary" translate="no">{{ p.project }}</span>
                  </span>
                </span>
              </th>
              <td data-testid="bastion-project-repositories">
                @if (p.repositories.length === 0) {
                  <span class="muted" i18n="@@bastion.projects.nothing-yet">None yet</span>
                } @else {
                  <span class="keys"
                    >@for (r of named(p); track r) {<a [routerLink]="['/projects', p.project, 'repositories', r]" translate="no">{{ r }}</a
                      >}@if (p.repositories.length > named(p).length) {<span class="muted">{{ moreText(p) }}</span>}</span
                  >
                }
              </td>
              <td>
                <quader-time-ago [at]="p.allowedAt" absolute="title" />
                <span class="secondary" translate="no">{{ p.allowedBy }}</span>
              </td>
              @if (canWrite()) {
                <td class="actions">
                  <button class="btn btn-tertiary" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="remove(p)">
                    <ng-container i18n="@@bastion.projects.remove">Remove</ng-container>
                    <span class="visually-hidden" translate="no"> {{ p.project }}</span>
                  </button>
                </td>
              }
            </tr>
          }
        </quader-data-table>
      }
      @if (!page() && totals().projects > (rows()?.length ?? 0)) {
        <p class="all">
          <a [routerLink]="pageLink()" data-testid="bastion-projects-all" i18n="@@bastion.projects.all">All {{ totals().projects }} projects</a>
        </p>
      }
    </ng-template>

    @if (page()) {
      <ng-container [ngTemplateOutlet]="body" />
    } @else if (!canWrite() && rows()?.length === 0 && !failure()) {
      <!-- Nothing to open: a reader sees the one line in the section's place, as the tokens' none (Q-UI-1623). -->
      <div class="section-plain" data-testid="bastion-projects">
        <h3><span class="help-anchor">{{ heading }}</span><quader-help [text]="help" [about]="heading" /></h3>
        <p class="muted" i18n="@@bastion.projects.none">No project may distribute through it yet.</p>
      </div>
    } @else {
      <quader-disclosure
        data-testid="bastion-projects"
        [heading]="heading"
        [help]="help"
        [level]="3"
        [count]="(rows()?.length ?? 0) > 0 ? totals().projects : null"
        [note]="note()"
        [noteTone]="failure() ? 'warn' : 'muted'"
        [(expanded)]="open"
      >
        <ng-container [ngTemplateOutlet]="body" />
      </quader-disclosure>
    }
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    :host {
      display: block;
    }
    /* The field and its act on one line (Q-UI-335): the field as wide as a name and never wider than the column, the button under it
       where the line is too narrow. */
    .allow quader-principal-field {
      inline-size: var(--quader-field-width-name);
      max-inline-size: 100%;
    }
    quader-error-notice {
      display: block;
      margin-block-end: var(--quader-space-3);
    }
    /* The keys separated by a comma, each whole; a long list breaks between keys. */
    .keys {
      display: flex;
      flex-wrap: wrap;
      column-gap: 0.25em;
      overflow-wrap: anywhere;
    }
    .keys > a:not(:last-child, :has(+ .muted))::after {
      content: ',';
      color: var(--quader-text);
    }
    .all {
      margin: var(--quader-space-3) 0 0;
    }
    .section-plain {
      display: flex;
      flex-wrap: wrap;
      align-items: baseline;
      gap: 0 var(--quader-space-2);
      padding-block: var(--quader-space-3);
      border-block-start: 1px solid var(--quader-border);
    }
    .section-plain h3 {
      margin: 0;
      padding-inline-start: calc(1.25rem + var(--quader-space-1));
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    .section-plain p {
      margin: 0;
      font-size: var(--quader-text-muted-size);
    }
  `,
})
export class BastionProjects {
  private readonly api = inject(BastionsApi);
  private readonly session = inject(SessionStore);
  private readonly confirmer = inject(Confirm);
  private readonly notifier = inject(Notifier);

  /** The registration's name. */
  readonly name = input.required<string>();
  /** The card's bound (`CARD_PROJECTS`): at most this many rows and the link to the page; null on the page, every project, paged. */
  readonly limit = input<number | null>(null);

  protected readonly heading = $localize`:@@bastion.projects.heading:Projects`;
  protected readonly help = $localize`:@@bastion.help.projects:The projects whose owners may distribute their repositories through this Bastion. A new Bastion allows none.`;
  protected readonly allowLabel = $localize`:@@bastion.projects.allow-label:Allow a project`;
  protected readonly filterLabel = $localize`:@@bastion.projects.filter:Filter projects`;
  protected readonly projectKind = ['project'] as const;

  protected readonly canWrite = computed(() => this.session.has('endpoints:write'));
  protected readonly page = computed(() => this.limit() === null);
  protected readonly pageLink = computed(() => ['/admin/bastions', this.name(), 'projects']);
  protected readonly caption = computed(() => $localize`:@@bastion.projects.caption:Projects of ${this.name()}:name:`);

  /** The filter of the page, by key or display name, asked of the server. */
  protected readonly q = signal('');
  protected readonly rows = signal<readonly BastionProject[] | null>(null);
  protected readonly next = signal<string | null>(null);
  protected readonly totals = signal<BastionProjectList['totals']>({ projects: 0, repositories: 0 });
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly actionError = signal<ApiError | null>(null);
  protected readonly busy = signal(false);
  /** The project chosen in **Allow a project**, as the field's reference (`project:<key>`). */
  protected readonly choice = signal<readonly string[]>([]);
  /** The projects listed are not offered again. */
  protected readonly shownRefs = computed(() => (this.rows() ?? []).map((p) => refOf('project', p.project)));

  /** The section is closed at first; a failed read opens it, so its notice with every fact of the answer shows (R-UI-216). */
  protected readonly open = linkedSignal<boolean, boolean>({
    source: () => this.failure() !== null,
    computation: (failed, previous) => failed || (previous?.value ?? false),
  });

  /** The section's one line: the repositories distributed, none allowed yet, or the failure. */
  protected readonly note = computed(() => {
    if (this.failure()) return $localize`:@@bastion.projects.failed:The projects could not be read.`;
    const rows = this.rows();
    if (rows === null) return '';
    if (rows.length === 0 && this.totals().projects === 0) return $localize`:@@bastion.projects.none:No project may distribute through it yet.`;
    return distributedNote(this.totals().repositories);
  });

  protected readonly emptyText = computed(() =>
    this.q().trim() ? $localize`:@@limits.projects.no-match:No project matches.` : $localize`:@@bastion.projects.none:No project may distribute through it yet.`,
  );

  protected readonly columns = computed<readonly TableColumn[]>(() => [
    // Least widths that hold the table's form in a card at 1,024 px (Q-UI-969), as the card's Tokens do: the project takes the room left,
    // and a row's keys break between keys.
    { id: 'project', label: $localize`:@@bastion.projects.col-project:Project`, width: '9rem', prefer: true },
    { id: 'repositories', label: $localize`:@@bastion.projects.col-repositories:Distributed here`, width: '9rem' },
    { id: 'allowed', label: $localize`:@@bastion.projects.col-allowed:Allowed`, width: '7.5rem' },
    ...(this.canWrite() ? [{ id: 'actions', label: $localize`:@@bastion.col.actions:Actions`, hideLabel: true }] : []),
  ]);

  constructor() {
    // A new Bastion or a new filter reads the first page again.
    effect(() => {
      this.name();
      this.q();
      untracked(() => void this.load());
    });
    // An Owner's choice changes the counts without the viewer: read again every minute while seen (R-UI-213).
    live({ reload: () => this.refresh(), every: () => LIVE_IDLE_MS });
  }

  protected named(p: BastionProject): readonly string[] {
    return p.repositories.slice(0, REPOSITORIES_NAMED);
  }

  protected moreText(p: BastionProject): string {
    const n = p.repositories.length - REPOSITORIES_NAMED;
    return $localize`:@@bastion.projects.more:and ${n}:count: more`;
  }

  private pageSize(): number {
    return this.limit() ?? DEFAULT_PAGE_LIMIT;
  }

  /** Reads the first page; the rows shown stay until the answer replaces them. */
  protected async load(): Promise<void> {
    try {
      const list = await this.api.projects(this.name(), { q: this.q().trim(), limit: this.pageSize() });
      this.take(list, list.items);
      this.failure.set(null);
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }

  /** **Load more** of the page: the next page after the rows shown. */
  protected async more(): Promise<void> {
    const cursor = this.next();
    if (cursor === null) return;
    try {
      const list = await this.api.projects(this.name(), { q: this.q().trim(), limit: this.pageSize(), cursor });
      this.take(list, [...(this.rows() ?? []), ...list.items]);
      this.failure.set(null);
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }

  /** The live read: as many rows as are shown, read again in place in one request; a failure is thrown for the back-off. */
  private async refresh(): Promise<void> {
    const shown = this.rows();
    if (shown === null) return;
    const limit = this.page() ? Math.min(MAX_PAGE_LIMIT, Math.max(shown.length, DEFAULT_PAGE_LIMIT)) : this.pageSize();
    const list = await this.api.projects(this.name(), { q: this.q().trim(), limit });
    this.take(list, list.items);
    this.failure.set(null);
  }

  private take(list: BastionProjectList, rows: readonly BastionProject[]): void {
    this.rows.set(rows);
    this.next.set(list.nextCursor);
    this.totals.set(list.totals);
  }

  /** **Allow**: the project chosen may distribute through the Bastion; nothing is distributed until its Owner chooses (R-BAS-134). */
  protected async allow(event: Event): Promise<void> {
    event.preventDefault();
    const ref = this.choice()[0];
    if (!ref || this.busy()) return;
    const project = ref.slice(ref.indexOf(':') + 1);
    const name = this.name();
    await this.act(
      () => this.api.allowProject(name, project),
      $localize`:@@bastion.projects.allowed-done:${project}:project: may distribute through ${name}:name:.`,
    );
    if (!this.actionError()) this.choice.set([]);
  }

  /** **Remove**, after the dialog that says what goes with it. */
  protected async remove(p: BastionProject): Promise<void> {
    const name = this.name();
    const project = p.project;
    const n = p.repositories.length;
    const line =
      n === 0
        ? $localize`:@@bastion.projects.remove-text-zero:None of its repositories is distributed there yet. Its owners can no longer choose this Bastion.`
        : n === 1
          ? $localize`:@@bastion.projects.remove-text-one:Its distributed repository is no longer served there, within seconds. Its owner's choice goes with it: allowing the project again distributes nothing until they choose again.`
          : $localize`:@@bastion.projects.remove-text:Its ${n}:count: distributed repositories are no longer served there, within seconds. Their owners' choices go with it: allowing the project again distributes nothing until they choose again.`;
    const answer = await this.confirmer.ask({
      title: $localize`:@@bastion.projects.remove-title:Remove ${project}:project: from ${name}:name:?`,
      lines: [line],
      confirmLabel: $localize`:@@bastion.projects.remove:Remove`,
    });
    if (answer === null) return;
    await this.act(() => this.api.removeProject(name, project), $localize`:@@bastion.projects.removed-done:${project}:project: removed from ${name}:name:.`);
  }

  private async act(run: () => Promise<void>, done: string): Promise<void> {
    this.busy.set(true);
    this.actionError.set(null);
    try {
      await run();
      this.notifier.status(done);
      await this.load();
    } catch (e) {
      this.actionError.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}

/**
 * **Projects of <name>** (R-UI-240, Q-UI-1637), `/admin/bastions/<name>/projects` for the holders of `endpoints:read`: the header with its
 * one sentence and every project the Bastion allows, with the filter, **Load more** and, for a holder of `endpoints:write`, **Allow a
 * project** and **Remove**. Reached from the card's section **Projects** when it holds more than it shows.
 */
@Component({
  selector: 'quader-bastion-projects-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [BastionProjects, PageHeader],
  template: `
    <quader-page-header [heading]="heading()" [description]="description" />
    <quader-bastion-projects [name]="name()" />
  `,
})
export class BastionProjectsPage {
  /** The registration's name from the address. */
  readonly name = input.required<string>();

  protected readonly heading = computed(() => $localize`:@@bastion.projects.page-title:Projects of ${this.name()}:name:`);
  protected readonly description = $localize`:@@bastion.help.projects:The projects whose owners may distribute their repositories through this Bastion. A new Bastion allows none.`;
}
