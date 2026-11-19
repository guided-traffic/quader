import { Injectable, inject } from '@angular/core';
import { ApiClient, QueryParams, newIdempotencyKey } from './client';
import { collectAll } from './pagination';
import { ProjectsApi } from './projects-api';
import { cleanQuery, seg } from './query';
import { Versioned } from './repositories-api';
import {
  LimitBlock,
  LimitCeilings,
  LimitExplanation,
  LimitPlace,
  LimitProfile,
  LimitProjectRow,
  LimitProfileWrite,
  LimitRule,
  LimitRuleWrite,
  LimitSettings,
  LimitSubjectSuggestion,
  ModePreview,
  ProjectMaximum,
  ProjectMaximumState,
  ProjectMaximumWrite,
  UsageDetail,
  UsageList,
  UsagePeriod,
  UsageSort,
  UsageStateFilter,
  UsageSubjectKind,
  CeilingFigures,
  LimitMode,
} from './limits-types';

export * from './limits-types';

/*
 * The download limits and the usage overview (R-API-082; bodies R-LIM-031 to R-LIM-035): profiles, rules at the three places,
 * the ceilings of a remote, the mode, a project's maximum, blocks, the explanation, the subjects of the rule dialog and the
 * usage lists and details. The shapes are the generated client's (limits-types.ts); a path without parameters is read through
 * the typed `ApiClient.get`.
 */

/**
 * Where a usage list or detail is read: the instance, a project (its Owner's scope), or one repository of a project. The
 * instance's filter **Project** is `projectFilter`: the instance's list narrowed with `project=` (Q-API-353, Q-UI-755).
 */
export interface UsageScope {
  readonly project?: string | null;
  /** The repository's key within `project`. */
  readonly repository?: string | null;
  /** The instance's list narrowed to one project's repositories; ignored with `project`. */
  readonly projectFilter?: string | null;
}

/** The filters of a usage list (R-LIM-034). */
export interface UsageQuery {
  readonly period?: UsagePeriod;
  readonly kind?: readonly UsageSubjectKind[];
  readonly state?: UsageStateFilter;
  readonly q?: string;
  readonly sort?: UsageSort;
  readonly order?: 'asc' | 'desc';
}

/** One row of the admin area's tab **Projects** (R-UI-177): a project with a maximum or with rules. */
export interface ProjectLimitsRow {
  readonly project: string;
  readonly displayName: string;
  readonly maximum: Versioned<ProjectMaximum> | null;
  /** The rules of the project and of its repositories. */
  readonly rules: number;
}

/** A row of `GET /api/v1/limits/projects` with the project's display name; the maximum's `ETag` is its version (R-API-012). */
export function projectLimitsRow(row: LimitProjectRow, displayName: string | undefined): ProjectLimitsRow {
  return {
    project: row.project,
    displayName: displayName || row.project,
    maximum: row.maximum ? { value: row.maximum, etag: `"${row.maximum.version}"` } : null,
    rules: row.projectRules + row.repositoryRules,
  };
}

/** The base path of a place's rules (R-LIM-032). */
export function rulesPath(place: LimitPlace): string {
  switch (place.kind) {
    case 'instance':
      return '/api/v1/limits/assignments';
    case 'project':
      return `/api/v1/projects/${seg(place.project)}/limits/assignments`;
    case 'repository': {
      const [project, repo] = splitRepository(place.repository);
      return `/api/v1/projects/${seg(project)}/repositories/${seg(repo)}/limits/assignments`;
    }
  }
}

/** `<project>/<repo>` into its two keys. */
export function splitRepository(ref: string): [string, string] {
  const at = ref.indexOf('/');
  return at < 0 ? [ref, ''] : [ref.slice(0, at), ref.slice(at + 1)];
}

/** The base path of a usage scope. */
export function usagePath(scope: UsageScope): string {
  return scope.project ? `/api/v1/projects/${seg(scope.project)}/usage` : '/api/v1/usage';
}

/** The query of a usage list in the API's names; the repository of a project scope is `repository=<repoKey>`. */
export function usageParams(scope: UsageScope, query: UsageQuery): QueryParams {
  return cleanQuery({
    period: query.period,
    kind: query.kind?.length ? query.kind : undefined,
    state: query.state,
    q: query.q,
    sort: query.sort,
    order: query.order,
    repository: scope.project && scope.repository ? scope.repository : undefined,
    project: !scope.project && scope.projectFilter ? scope.projectFilter : undefined,
  });
}

/** The address of a usage list as CSV, with the same filters (R-LIM-034); a link downloads it. */
export function usageExportUrl(scope: UsageScope, query: UsageQuery): string {
  const params = new URLSearchParams();
  for (const [name, value] of Object.entries(usageParams(scope, query))) {
    if (Array.isArray(value)) value.forEach((v) => params.append(name, String(v)));
    else if (value !== undefined) params.append(name, String(value));
  }
  const q = params.toString();
  return `${usagePath(scope)}/export${q ? `?${q}` : ''}`;
}

/** The `{kind}/{id}` of a subject in a path (R-LIM-034): an address or prefix percent-encoded. */
export function subjectPath(kind: UsageSubjectKind, id: string): string {
  return `${seg(kind)}/${seg(id)}`;
}

export abstract class LimitsApi {
  // Profiles (R-LIM-031).
  abstract profiles(): Promise<readonly LimitProfile[]>;
  abstract profile(key: string): Promise<Versioned<LimitProfile>>;
  abstract createProfile(write: LimitProfileWrite, idempotencyKey: string): Promise<Versioned<LimitProfile>>;
  abstract replaceProfile(key: string, write: LimitProfileWrite, etag: string): Promise<Versioned<LimitProfile>>;
  abstract deleteProfile(key: string, etag?: string): Promise<void>;

  // Rules at a place (R-LIM-032).
  abstract rules(place: LimitPlace): Promise<readonly LimitRule[]>;
  abstract rule(place: LimitPlace, id: string): Promise<Versioned<LimitRule>>;
  abstract createRule(place: LimitPlace, write: LimitRuleWrite, idempotencyKey: string): Promise<Versioned<LimitRule>>;
  abstract replaceRule(place: LimitPlace, id: string, write: LimitRuleWrite, etag: string): Promise<Versioned<LimitRule>>;
  abstract deleteRule(place: LimitPlace, id: string, etag?: string): Promise<void>;

  // The ceilings of a remote (R-LIM-032); `ref` is `<project>/<repo>`.
  abstract ceilings(ref: string): Promise<Versioned<LimitCeilings>>;
  abstract replaceCeilings(
    ref: string,
    body: { readonly anonymous: CeilingFigures; readonly signedIn: CeilingFigures },
    etag: string,
  ): Promise<Versioned<LimitCeilings>>;

  // The mode, the block time, the preview (R-LIM-033).
  abstract settings(): Promise<Versioned<LimitSettings>>;
  abstract patchSettings(
    patch: { readonly mode?: LimitMode; readonly blockSeconds?: number },
    etag: string,
  ): Promise<Versioned<LimitSettings>>;
  abstract modePreview(): Promise<ModePreview>;

  // A project's maximum (R-LIM-033): null where the project has none.
  abstract maximum(project: string): Promise<Versioned<ProjectMaximum> | null>;
  /** `etag` null sets a first maximum, which takes no `If-Match` (Q-API-354). */
  abstract putMaximum(project: string, write: ProjectMaximumWrite, etag: string | null): Promise<Versioned<ProjectMaximum>>;
  abstract deleteMaximum(project: string, etag?: string): Promise<void>;
  /** The rows of the admin area's tab **Projects**: every project with a maximum or a rule. */
  abstract projectRows(): Promise<readonly ProjectLimitsRow[]>;

  // Blocks (R-LIM-033).
  abstract blocks(filter: { readonly active?: boolean; readonly place?: string }): Promise<readonly LimitBlock[]>;
  abstract liftBlock(id: string): Promise<void>;

  // Which limit applies (R-LIM-035).
  abstract explain(subject: { kind: UsageSubjectKind; id: string }, repository: string): Promise<LimitExplanation>;
  abstract myLimits(repository: string): Promise<LimitExplanation>;
  /** Technical tokens and CI projects for the rule dialog (R-LIM-034), 20 at most. */
  abstract subjects(
    q: string,
    kinds: readonly ('technicalToken' | 'ci')[],
    project: string | null,
    signal?: AbortSignal,
  ): Promise<readonly LimitSubjectSuggestion[]>;

  // Usage (R-LIM-034).
  abstract usage(scope: UsageScope, query: UsageQuery, cursor: string | null, limit?: number): Promise<UsageList>;
  abstract subjectUsage(scope: UsageScope, kind: UsageSubjectKind, id: string, period: UsagePeriod): Promise<UsageDetail>;
  abstract myUsage(period: UsagePeriod): Promise<UsageDetail>;
}

function versioned<T extends { readonly version: number }>(r: { readonly body: T; readonly etag: string | null }): Versioned<T> {
  return { value: r.body, etag: r.etag ?? `"${r.body.version}"` };
}

/** The items of a list answer, every page of it. */
async function all<T>(client: ApiClient, url: string, params: QueryParams = {}): Promise<T[]> {
  return collectAll(client.page<T>(url, params), 200);
}

@Injectable()
export class HttpLimitsApi extends LimitsApi {
  private readonly client = inject(ApiClient);
  private readonly projects = inject(ProjectsApi);

  override async profiles(): Promise<readonly LimitProfile[]> {
    return (await this.client.get('/api/v1/limits/profiles')).body.items;
  }

  override async profile(key: string): Promise<Versioned<LimitProfile>> {
    return versioned(await this.client.getJson<LimitProfile>(`/api/v1/limits/profiles/${seg(key)}`));
  }

  override async createProfile(write: LimitProfileWrite, idempotencyKey: string): Promise<Versioned<LimitProfile>> {
    return versioned(await this.client.post<LimitProfile>('/api/v1/limits/profiles', write, { idempotencyKey }));
  }

  override async replaceProfile(key: string, write: LimitProfileWrite, etag: string): Promise<Versioned<LimitProfile>> {
    return versioned(await this.client.put<LimitProfile>(`/api/v1/limits/profiles/${seg(key)}`, write, etag));
  }

  override async deleteProfile(key: string, etag?: string): Promise<void> {
    await this.client.delete(`/api/v1/limits/profiles/${seg(key)}`, { etag });
  }

  override rules(place: LimitPlace): Promise<readonly LimitRule[]> {
    return all<LimitRule>(this.client, rulesPath(place));
  }

  override async rule(place: LimitPlace, id: string): Promise<Versioned<LimitRule>> {
    return versioned(await this.client.getJson<LimitRule>(`${rulesPath(place)}/${seg(id)}`));
  }

  override async createRule(place: LimitPlace, write: LimitRuleWrite, idempotencyKey: string): Promise<Versioned<LimitRule>> {
    return versioned(await this.client.post<LimitRule>(rulesPath(place), write, { idempotencyKey }));
  }

  override async replaceRule(place: LimitPlace, id: string, write: LimitRuleWrite, etag: string): Promise<Versioned<LimitRule>> {
    return versioned(await this.client.put<LimitRule>(`${rulesPath(place)}/${seg(id)}`, write, etag));
  }

  override async deleteRule(place: LimitPlace, id: string, etag?: string): Promise<void> {
    await this.client.delete(`${rulesPath(place)}/${seg(id)}`, { etag });
  }

  private ceilingsPath(ref: string): string {
    const [project, repo] = splitRepository(ref);
    return `/api/v1/projects/${seg(project)}/repositories/${seg(repo)}/limits/ceilings`;
  }

  override async ceilings(ref: string): Promise<Versioned<LimitCeilings>> {
    return versioned(await this.client.getJson<LimitCeilings>(this.ceilingsPath(ref)));
  }

  override async replaceCeilings(
    ref: string,
    body: { readonly anonymous: CeilingFigures; readonly signedIn: CeilingFigures },
    etag: string,
  ): Promise<Versioned<LimitCeilings>> {
    return versioned(await this.client.put<LimitCeilings>(this.ceilingsPath(ref), body, etag));
  }

  override async settings(): Promise<Versioned<LimitSettings>> {
    return versioned(await this.client.get('/api/v1/limits/settings'));
  }

  override async patchSettings(
    patch: { readonly mode?: LimitMode; readonly blockSeconds?: number },
    etag: string,
  ): Promise<Versioned<LimitSettings>> {
    return versioned(await this.client.patch<LimitSettings>('/api/v1/limits/settings', patch, etag));
  }

  override async modePreview(): Promise<ModePreview> {
    return (await this.client.get('/api/v1/limits/mode-preview')).body;
  }

  /** The project's maximum, null while it has none: the read answers `maximum` null then, with no ETag (Q-API-356). */
  override async maximum(project: string): Promise<Versioned<ProjectMaximum> | null> {
    const r = await this.client.getJson<ProjectMaximumState>(`/api/v1/projects/${seg(project)}/limits/maximum`);
    const m = r.body.maximum;
    return m ? { value: m, etag: r.etag ?? `"${m.version}"` } : null;
  }

  override async putMaximum(project: string, write: ProjectMaximumWrite, etag: string | null): Promise<Versioned<ProjectMaximum>> {
    const url = `/api/v1/projects/${seg(project)}/limits/maximum`;
    return versioned(etag ? await this.client.put<ProjectMaximum>(url, write, etag) : await this.client.putJson<ProjectMaximum>(url, write));
  }

  override async deleteMaximum(project: string, etag?: string): Promise<void> {
    await this.client.delete(`/api/v1/projects/${seg(project)}/limits/maximum`, { etag });
  }

  /**
   * The rows of the tab **Projects** from `GET /api/v1/limits/projects` (Q-API-351, Q-UI-756): every project with a maximum or a
   * rule, in one read; the projects' display names come from the project list.
   */
  override async projectRows(): Promise<readonly ProjectLimitsRow[]> {
    const [answer, projects] = await Promise.all([
      this.client.get('/api/v1/limits/projects'),
      collectAll(this.projects.list({})).catch(() => []),
    ]);
    const names = new Map(projects.map((p) => [p.key, p.displayName]));
    return answer.body.items.map((r) => projectLimitsRow(r, names.get(r.project)));
  }

  override async blocks(filter: { readonly active?: boolean; readonly place?: string }): Promise<readonly LimitBlock[]> {
    return all<LimitBlock>(this.client, '/api/v1/limits/blocks', cleanQuery(filter));
  }

  override async liftBlock(id: string): Promise<void> {
    await this.client.delete(`/api/v1/limits/blocks/${seg(id)}`, { idempotencyKey: newIdempotencyKey() });
  }

  override async explain(subject: { kind: UsageSubjectKind; id: string }, repository: string): Promise<LimitExplanation> {
    const params = { subject: `${subject.kind}/${subject.id}`, repository };
    return (await this.client.get('/api/v1/limits/explain', { params })).body;
  }

  override async myLimits(repository: string): Promise<LimitExplanation> {
    return (await this.client.get('/api/v1/users/me/limits', { params: { repository } })).body;
  }

  override async subjects(
    q: string,
    kinds: readonly ('technicalToken' | 'ci')[],
    project: string | null,
    signal?: AbortSignal,
  ): Promise<readonly LimitSubjectSuggestion[]> {
    const params = cleanQuery({ q, kinds: kinds.length ? [...kinds] : undefined, project });
    return (await this.client.get('/api/v1/limits/subjects', { params, signal })).body.items;
  }

  override async usage(scope: UsageScope, query: UsageQuery, cursor: string | null, limit = 50): Promise<UsageList> {
    const params: Record<string, QueryParams[string]> = { ...usageParams(scope, query), limit };
    if (cursor !== null) params['cursor'] = cursor;
    return (await this.client.getJson<UsageList>(usagePath(scope), { params })).body;
  }

  override async subjectUsage(scope: UsageScope, kind: UsageSubjectKind, id: string, period: UsagePeriod): Promise<UsageDetail> {
    const url = `${usagePath(scope)}/subjects/${subjectPath(kind, id)}`;
    return (await this.client.getJson<UsageDetail>(url, { params: { period } })).body;
  }

  override async myUsage(period: UsagePeriod): Promise<UsageDetail> {
    return (await this.client.get('/api/v1/users/me/usage', { params: { period } })).body;
  }
}
