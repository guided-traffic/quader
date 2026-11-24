import { DocumentBlock } from '../../shared/document-block';
import { DatePipe } from '@angular/common';
import {
  Component,
  ElementRef,
  computed,
  effect,
  inject,
  input,
  resource,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { LIVE_SLOW_MS, live } from '../../core/live-refresh';
import { Router, RouterLink } from '@angular/router';
import { AUDIT_CATEGORIES, AuditApi, AuditEvent, AuditFilters } from '../../api/audit-api';
import { filesCommands } from '../../core/project-links';
import { CopyButton } from '../../shared/copy-button';
import { ErrorNotice } from '../../shared/error-notice';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { NoValue } from '../../shared/ui/no-value';
import { ProjectLink } from '../../shared/ui/project-mark';
import { TimeAgo } from '../../shared/ui/time-ago';
import { PagedResource, asApiError, valueOf } from '../../shared/paged-resource';
import { actionSuggestions, auditActionLabel, categoryLabel, outcomeLabel } from './audit-labels';
import { PageHeader } from '../../shared/ui/page-header';
import { ADDRESS_MARKS, KEY_MARKS, PartsText, breakParts } from '../../shared/break-parts';
import { PrincipalField, choiceFromRef, refOf } from '../../shared/ui/principal-field';

/**
 * The kinds of resource the events name most (the `resource.type` of an event), offered by the filter's field; any other kind can be
 * typed (Q-UI-998).
 */
const RESOURCE_KINDS: readonly string[] = [
  'bastion',
  'blob',
  'ci_issuer',
  'ci_mapping',
  'exposure_rule',
  'finding',
  'group',
  'package',
  'permission_policy',
  'project',
  'repository',
  'role_binding',
  'service_account',
  'settings',
  'signature_anchor',
  'signature_block',
  'signing_identity',
  'task',
  'technical_token',
  'token',
  'user',
  'vulnerability_source',
];

/** The page size of the table (the server default and a good fit for one screen). */
export const AUDIT_PAGE_SIZE = 100;

const HOUR_MS = 3_600_000;
const PRESETS: Readonly<Record<string, number>> = {
  '1h': HOUR_MS,
  '24h': 24 * HOUR_MS,
  '7d': 7 * 24 * HOUR_MS,
  '30d': 30 * 24 * HOUR_MS,
};

/**
 * The audit log (R-UI-037): the events of a time range, newest first, with the filters the API
 * offers and "Load more" over the keyset cursor. Times follow the user's locale, with a switch to
 * UTC. A row opens the full event in a panel with links to the actor and the target; the export
 * links download the current filter as CSV or JSON Lines. Filters, the open event and the time
 * zone live in the query parameters (R-UI-003).
 */
@Component({
  selector: 'quader-audit-log',
  imports: [
    PageHeader,
    DatePipe,
    RouterLink,
    CopyButton,
    DocumentBlock,
    DataTable,
    ErrorNotice,
    FilterPanel,
    ListHead,
    NoValue,
    PrincipalField,
    PartsText,
    ProjectLink,
    TimeAgo,
  ],
  templateUrl: './audit-log.html',
  styleUrl: './audit-log.scss',
})
export class AuditLog {
  protected readonly valueOf = valueOf;
  private readonly api = inject(AuditApi);
  private readonly router = inject(Router);

  /** Query parameters, bound by the router's component input binding. */
  readonly range = input<string | undefined>();
  readonly from = input<string | undefined>();
  readonly to = input<string | undefined>();
  readonly actor = input<string | undefined>();
  readonly action = input<string | undefined>();
  readonly category = input<string | undefined>();
  readonly repository = input<string | undefined>();
  readonly resourceType = input<string | undefined>();
  readonly resourceId = input<string | undefined>();
  readonly outcome = input<string | undefined>();
  readonly event = input<string | undefined>();
  readonly utc = input<string | undefined>();

  protected readonly list = new PagedResource<AuditEvent>();
  protected readonly now = signal(Date.now());
  protected readonly categories = AUDIT_CATEGORIES;
  protected readonly knownActions = actionSuggestions();

  // The form controls hold their text until "Apply" puts it into the URL.
  protected readonly draft = signal<Readonly<Record<string, string>>>({});
  protected readonly presetDraft = signal('24h');
  protected readonly panelOpen = signal(false);
  protected readonly caption = $localize`:@@audit.caption:Audit events, newest first`;
  /** The log of the default 24 hours without a filter says there is none; a filtered one that nothing matches (Q-UI-565). */
  protected readonly noEvent = $localize`:@@audit.empty-none:There is no event in the last 24 hours.`;
  protected readonly noMatch = $localize`:@@audit.empty:No event matches.`;
  /**
   * No sort control: the log is always newest first, and the API takes no sort parameter. Five columns that fit 1,024 px, and the log
   * beside an opened event at 1,440 px, whatever the events hold (Q-UI-997): the address an event came from stands under its actor,
   * the project under its target.
   */
  protected readonly columns: readonly TableColumn[] = [
    {
      id: 'time',
      label: $localize`:@@audit.col.time:Time`,
      help: $localize`:@@audit.help.time:Events are listed newest first. Narrow the time range in the filters to reach older ones.`,
    },
    { id: 'actor', label: $localize`:@@audit.col.actor:Actor` },
    // An action's name and key wrap in 7.5 rem; a longer key breaks where it must (audit-log.scss).
    { id: 'action', label: $localize`:@@audit.col.action:Action`, width: '7.5rem' },
    { id: 'target', label: $localize`:@@audit.col.target:Target` },
    {
      id: 'outcome',
      label: $localize`:@@audit.col.outcome:Outcome`,
      help: $localize`:@@audit.help.outcome:Success: the action was carried out. Failure: it was allowed but did not work. Denied: the caller was not permitted. Warning: it was carried out, and an administrator should look at it.`,
    },
  ];

  protected readonly preset = computed(() => this.range() ?? '24h');
  protected readonly timeZone = computed(() => (this.utc() === '1' ? 'UTC' : undefined));

  /** The API filter for the current URL; a preset is resolved against `now`. */
  protected readonly filters = computed<AuditFilters & { from: string; to: string }>(() => this.filtersAt(this.now()));

  /** The filter of the URL at the time `now`: a preset, and a custom range without an end, end at `now`. */
  private filtersAt(now: number): AuditFilters & { from: string; to: string } {
    const preset = this.preset();
    const span = PRESETS[preset];
    const from =
      span !== undefined
        ? new Date(now - span).toISOString()
        : toIso(this.from(), new Date(now - 24 * HOUR_MS).toISOString());
    const to =
      span !== undefined
        ? new Date(now).toISOString()
        : toIso(this.to(), new Date(now).toISOString());
    return {
      from,
      to,
      actor: this.actor(),
      action: this.action(),
      category: this.category(),
      repository: this.repository(),
      resourceType: this.resourceType(),
      resourceId: this.resourceId(),
      outcome: this.outcome(),
    };
  }

  protected readonly filtered = computed(
    () =>
      !!(
        this.actor() ||
        this.action() ||
        this.category() ||
        this.repository() ||
        this.resourceType() ||
        this.resourceId() ||
        this.outcome() ||
        this.range()
      ),
  );
  /** The filters in the URL as the head's chips (Q-UI-422), the time range when it is not the default. */
  protected readonly chips = computed<readonly ListChip[]>(() => {
    const out: ListChip[] = [];
    const range = this.range();
    if (range) out.push({ key: 'range', label: $localize`:@@audit.filter.range:Time range`, value: this.rangeText(range) });
    const actor = this.actor();
    if (actor) out.push({ key: 'actor', label: $localize`:@@audit.col.actor:Actor`, value: actor });
    const action = this.action();
    if (action) out.push({ key: 'action', label: $localize`:@@audit.col.action:Action`, value: this.actionLabel(action) });
    const category = this.category();
    if (category) out.push({ key: 'category', label: $localize`:@@audit.filter.category:Category`, value: this.categoryText(category) });
    const repository = this.repository();
    if (repository) out.push({ key: 'repository', label: $localize`:@@audit.filter.repository:Repository`, value: repository });
    const resourceType = this.resourceType();
    if (resourceType) out.push({ key: 'resourceType', label: $localize`:@@audit.filter.resource-type:Resource kind`, value: resourceType });
    const resourceId = this.resourceId();
    if (resourceId) out.push({ key: 'resourceId', label: $localize`:@@audit.filter.resource-id:Resource ID`, value: resourceId });
    const outcome = this.outcome();
    if (outcome) out.push({ key: 'outcome', label: $localize`:@@audit.col.outcome:Outcome`, value: this.outcomeText(outcome) });
    return out;
  });
  protected readonly csvUrl = computed(() => this.api.exportUrl(this.filters(), 'csv'));
  protected readonly jsonlUrl = computed(() => this.api.exportUrl(this.filters(), 'jsonl'));

  protected readonly selected = resource({
    params: () => this.event() ?? null,
    loader: async ({ params }) => {
      if (params === null) return null;
      return this.list.items().find((e) => e.id === params) ?? (await this.api.event(params));
    },
  });
  protected readonly selectedFailure = computed(() => {
    const e = this.selected.error();
    return e ? asApiError(e) : null;
  });
  protected readonly details = computed(() => valueOf(this.selected)?.details ?? {});

  constructor() {
    effect(() => {
      const filters = this.filters();
      // A preset moves with the clock; the explicit refresh and a filter change reload.
      untracked(() => void this.list.start(this.api.events(filters), AUDIT_PAGE_SIZE));
    });
    // New events arrive all the time: the rows shown are read again every half minute with the range moved to now, in place, so an
    // open event, the scroll and the filters stay (R-UI-213). A custom range with an end stays where it is.
    live({
      reload: () => this.list.refresh(this.api.events(untracked(() => this.filtersAt(Date.now())))),
      every: () => LIVE_SLOW_MS,
    });
    effect(() => this.restage());
    // An event opened from the list takes the focus, so the page brings it into view where it stands above the log (Q-UI-997).
    effect(() => {
      const heading = this.eventHeading();
      if (heading && this.focusEvent) {
        this.focusEvent = false;
        heading.nativeElement.focus();
      }
    });
  }

  private readonly eventHeading = viewChild<ElementRef<HTMLElement>>('eventHeading');
  /** An event was opened from the list: its heading takes the focus once it is shown. */
  private focusEvent = false;

  /** The drawer's values back to the ones in the URL: after a change of the URL, and when the drawer closes without **Apply**. */
  protected restage(): void {
    this.draft.set({
      actor: this.actor() ?? '',
      action: this.action() ?? '',
      category: this.category() ?? '',
      repository: this.repository() ?? '',
      resourceType: this.resourceType() ?? '',
      resourceId: this.resourceId() ?? '',
      outcome: this.outcome() ?? '',
      from: this.from() ?? '',
      to: this.to() ?? '',
    });
    this.presetDraft.set(this.preset());
  }

  protected setDraft(name: string, value: string): void {
    this.draft.update((d) => ({ ...d, [name]: value }));
  }

  /** The actor filter is the shared field (R-UI-150): a user or a service account, of the instance or of a project. */
  protected readonly actorKinds = ['user', 'service_account'] as const;
  /** The reference last chosen, so a service account keeps its kind in the field; the filter itself is the actor's name. */
  private readonly actorRef = signal<string | null>(null);

  protected actorRefs(): readonly string[] {
    const name = this.draft()['actor']?.trim();
    if (!name) return [];
    const ref = this.actorRef();
    return [ref && choiceFromRef(ref).name === name ? ref : refOf('user', name)];
  }

  protected setActor(refs: readonly string[]): void {
    const ref = refs[0] ?? null;
    this.actorRef.set(ref);
    this.setDraft('actor', ref ? choiceFromRef(ref).name : '');
  }

  protected apply(): void {
    const d = this.draft();
    const preset = this.presetDraft();
    this.now.set(Date.now());
    void this.router.navigate([], {
      queryParams: {
        range: preset === '24h' ? null : preset,
        from: preset === 'custom' ? d['from'] || null : null,
        to: preset === 'custom' ? d['to'] || null : null,
        actor: d['actor']?.trim() || null,
        action: d['action']?.trim() || null,
        category: d['category'] || null,
        repository: d['repository']?.trim() || null,
        resourceType: d['resourceType']?.trim() || null,
        resourceId: d['resourceId']?.trim() || null,
        outcome: d['outcome'] || null,
        event: null,
      },
      queryParamsHandling: 'merge',
    });
  }

  protected removeChip(chip: ListChip): void {
    this.now.set(Date.now());
    const change: Record<string, null> = { [chip.key]: null, event: null };
    if (chip.key === 'range') Object.assign(change, { from: null, to: null });
    void this.router.navigate([], { queryParams: change, queryParamsHandling: 'merge' });
  }

  /** A time range's words: the preset's, or the custom bounds as typed. */
  private rangeText(range: string): string {
    switch (range) {
      case '1h':
        return $localize`:@@audit.range.1h:Last hour`;
      case '7d':
        return $localize`:@@audit.range.7d:Last 7 days`;
      case '30d':
        return $localize`:@@audit.range.30d:Last 30 days`;
      case 'custom':
        return `${this.from() ?? '…'} – ${this.to() ?? '…'}`;
      default:
        return range;
    }
  }

  protected clear(): void {
    this.now.set(Date.now());
    void this.router.navigate([], {
      queryParams: {
        range: null,
        from: null,
        to: null,
        actor: null,
        action: null,
        category: null,
        repository: null,
        resourceType: null,
        resourceId: null,
        outcome: null,
        event: null,
      },
      queryParamsHandling: 'merge',
    });
  }

  protected refresh(): void {
    this.now.set(Date.now());
  }

  protected toggleUtc(utc: boolean): void {
    void this.router.navigate([], {
      queryParams: { utc: utc ? '1' : null },
      queryParamsHandling: 'merge',
    });
  }

  protected open(id: string): void {
    // An event already shown keeps its heading: it takes the focus at once; a first one when it is shown.
    const shown = this.eventHeading();
    this.focusEvent = !shown;
    void this.router.navigate([], { queryParams: { event: id }, queryParamsHandling: 'merge' }).then(() => shown?.nativeElement.focus());
  }

  protected close(): void {
    void this.router.navigate([], { queryParams: { event: null }, queryParamsHandling: 'merge' });
  }

  protected actionLabel = auditActionLabel;
  protected categoryText = categoryLabel;
  protected outcomeText = outcomeLabel;

  protected actorName(e: AuditEvent): string {
    return e.actor.name ?? e.actor.id ?? e.actor.type;
  }

  /** The admin screen of the actor, where one exists. */
  protected actorLink(e: AuditEvent): string[] | null {
    const name = e.actor.name;
    if (!name) return null;
    if (e.actor.type === 'user') return ['/admin/users', name];
    if (e.actor.type === 'service_account') {
      // A project's account is named `<project>/<name>` (R-AUTHZ-026) and lives in the project (Q-UI-129).
      const at = name.indexOf('/');
      return at < 0 ? ['/admin/service-accounts', name] : ['/projects', name.slice(0, at), 'service-accounts', name.slice(at + 1)];
    }
    return null;
  }

  /** The project of the repository an event is about (`<project>/<key>`), or null for an event about none. */
  protected projectOf(e: AuditEvent): string | null {
    const at = e.repository?.indexOf('/') ?? -1;
    return e.repository && at > 0 ? e.repository.slice(0, at) : null;
  }

  /** The repository's own screen in its project. */
  protected repositoryLink(repository: string): string[] {
    const [project = '', key = ''] = repository.split('/');
    return ['/projects', project, 'repositories', key];
  }

  /** The page of the item in its repository's files, inside the project of the repository (Q-UI-510). */
  protected fileLink(repository: string, path: string): string[] {
    return filesCommands(repository, path.split('/'));
  }

  /** A repository's target: the repository and, where the event names one, the path in it. */
  protected targetText(e: AuditEvent): string {
    return e.path ? `${e.repository}/${e.path}` : (e.repository ?? '');
  }

  /**
   * The name of the resource an event is about, where the entry knows it (Q-UI-568). The API delivers a resource's kind and
   * identifier, not its name (`AuditResource`); the entry names a principal only as its actor. So a user or a service account
   * that is the event's own actor (a sign-in, a claim missing at it, a token of one's own) is named as the Actor column names it.
   */
  protected targetName(e: AuditEvent): string | null {
    const { type, id } = e.resource;
    if (!id || !e.actor.name || id !== e.actor.id) return null;
    return type === e.actor.type || (type === 'user' && e.actor.type === 'breakglass') ? e.actor.name : null;
  }

  /**
   * An action's key in its parts, each with its dot or underscore, so it breaks after one of them and never inside a word (Q-UI-567):
   * `bastion.data_key.handed` broke as "bastion.data_key.hande" / "d" in a narrow event.
   */
  protected keyParts(action: string): string[] {
    return breakParts(action, KEY_MARKS);
  }

  /** The name of an action in the row: its name, or its key in parts where the action has no name of its own. */
  protected labelParts(action: string): string[] {
    const label = this.actionLabel(action);
    return label === action ? this.keyParts(action) : [label];
  }

  protected readonly copyId = $localize`:@@audit.copy-id:Copy the event ID`;
  protected readonly copyTrace = $localize`:@@audit.copy-trace:Copy the trace ID`;
  protected readonly addressMarks = ADDRESS_MARKS;
  protected readonly copyDetails = $localize`:@@audit.copy-details:Copy the details`;
  protected readonly copyResource = $localize`:@@audit.copy-resource:Copy the resource ID`;
  /** The known kinds of a resource, the suggestions of the filter's field. */
  protected readonly resourceKinds = RESOURCE_KINDS;
}

/** A `datetime-local` value in the browser's zone as an RFC 3339 instant; the fallback for an empty or bad value. */
function toIso(value: string | undefined, fallback: string): string {
  if (!value) return fallback;
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? fallback : d.toISOString();
}
