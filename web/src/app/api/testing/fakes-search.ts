import { Provider } from '@angular/core';
import { ApiError } from '../problem';
import { Page, PageFetcher } from '../pagination';
import {
  DocumentationHit,
  GroupSuggestion,
  PackageSuggestion,
  ProjectSuggestion,
  RepositorySuggestion,
  SearchApi,
  ServiceAccountSuggestion,
  SearchSuggestions,
  SuggestRequest,
  SuggestionKind,
  UserSuggestion,
} from '../search-api';

/*
 * The in-memory fake of the suggestions (R-UI-047 component tier). It matches as the server does in outline (every term of the
 * lower-cased text is part of the qualified name, the name, the key or the display name) without the server's ranks, cuts each
 * kind at `limit` and reports `more`. A spec can hold requests until it releases them, make them fail (a 429 with `Retry-After`),
 * refuse the admin kinds, and sees every request with its signal.
 */

const DEFAULT_KINDS: readonly SuggestionKind[] = ['repository', 'package', 'project'];

function abortError(): DOMException {
  return new DOMException('The request was cancelled.', 'AbortError');
}

export class FakeSearchApi extends SearchApi {
  /** Every request, in order, with its signal. */
  readonly requests: SuggestRequest[] = [];
  /**
   * When each request of `requests` reached the fake (`performance.now()`): a spec measures the field's pauses from them instead of
   * waiting a time of its own, which a loaded host stretches.
   */
  readonly sentAt: number[] = [];
  repositories: RepositorySuggestion[] = [
    { project: 'web', key: 'npm-local', format: 'npm', class: 'local' },
    { project: 'web', key: 'npm-all', format: 'npm', class: 'virtual' },
    { project: 'web-frontend', key: 'npm-vendor', format: 'npm', class: 'remote' },
    { project: 'platform', key: 'collections', format: 'ansible', class: 'local' },
  ];
  packages: PackageSuggestion[] = [
    {
      id: 'pkg_nzyg2acao5swel3vnewww2lu',
      format: 'npm',
      name: '@web/ui-kit',
      project: 'web',
      repository: 'npm-local',
    },
  ];
  projects: ProjectSuggestion[] = [
    { key: 'web', displayName: 'Web platform' },
    { key: 'web-frontend', displayName: 'Web frontend' },
    { key: 'platform', displayName: 'Platform' },
  ];
  users: UserSuggestion[] = [
    {
      id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10',
      username: 'sam',
      displayName: 'Sam Reader',
      disabled: false,
      avatarUrl: null,
    },
    {
      id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a11',
      username: 'ada',
      displayName: null,
      disabled: false,
      avatarUrl: null,
    },
  ];
  groups: GroupSuggestion[] = [{ name: 'web-leads', description: 'The leads of the web team' }];
  /** The entries of the documentation across collections (R-ANS-058). */
  documentationHits: DocumentationHit[] = [
    {
      format: 'ansible',
      packageId: 'pkg_ansibleacmetools',
      package: 'acme.tools',
      version: '1.2.0',
      project: 'platform',
      repository: 'collections',
      type: 'module',
      name: 'firewall_rule',
      shortDescription: 'Manage a rule of the host firewall',
    },
  ];
  /** Each text the documentation list was asked for, in order. */
  readonly documentationQueries: string[] = [];
  serviceAccounts: ServiceAccountSuggestion[] = [
    { name: 'ci-bot', description: 'Publishes from the pipeline', project: null },
    { name: 'web/deploy', description: '', project: 'web' },
  ];
  /** Set to answer every request with this error, such as a 429 with `retryAfterSeconds`. */
  failure: ApiError | null = null;
  /** While false, a request with the kind `user`, `group` or `service_account` answers 403, as for a caller without `identities:read`. */
  identitiesRead = true;
  /** While true, a request waits until `release()` or the abort of its signal. */
  hold = false;
  private waiting: (() => void)[] = [];

  /** Lets every held request answer. */
  release(): void {
    const waiting = this.waiting;
    this.waiting = [];
    for (const go of waiting) go();
  }

  override async suggest(request: SuggestRequest): Promise<SearchSuggestions> {
    this.requests.push(request);
    this.sentAt.push(performance.now());
    if (request.signal?.aborted) throw abortError();
    if (this.hold) await this.held(request.signal);
    if (this.failure) throw this.failure;
    const kinds = request.kinds?.length ? request.kinds : DEFAULT_KINDS;
    if (!this.identitiesRead && (kinds.includes('user') || kinds.includes('group') || kinds.includes('service_account'))) {
      throw new ApiError(403, { status: 403, code: 'forbidden' }, null);
    }
    const terms = request.q.trim().toLowerCase().split(/\s+/);
    const matches = (...texts: readonly (string | null)[]): boolean =>
      terms.every((t) => texts.some((s) => (s ?? '').toLowerCase().includes(t)));
    const limit = request.limit ?? 5;
    const cut = <T>(rows: readonly T[]): { items: T[]; more: boolean } => ({
      items: rows.slice(0, limit),
      more: rows.length > limit,
    });
    const out: SearchSuggestions = {};
    if (kinds.includes('repository')) {
      out.repositories = cut(this.repositories.filter((r) => matches(`${r.project}/${r.key}`)));
    }
    if (kinds.includes('package')) out.packages = cut(this.packages.filter((p) => matches(p.name)));
    if (kinds.includes('project')) {
      out.projects = cut(this.projects.filter((p) => matches(p.key) || matches(p.displayName)));
    }
    if (kinds.includes('user')) {
      out.users = cut(this.users.filter((u) => matches(u.username) || matches(u.displayName)));
    }
    if (kinds.includes('group')) out.groups = cut(this.groups.filter((g) => matches(g.name)));
    if (kinds.includes('service_account')) out.serviceAccounts = cut(this.serviceAccounts.filter((a) => matches(a.name)));
    if (kinds.includes('documentation')) {
      out.documentation = cut(this.documentationHits.filter((d) => matches(d.name, d.shortDescription)));
    }
    return out;
  }

  override documentation(q: string): PageFetcher<DocumentationHit> {
    this.documentationQueries.push(q);
    const terms = q.trim().toLowerCase().split(/\s+/);
    const rows = this.documentationHits.filter((d) => terms.every((t) => `${d.name} ${d.shortDescription}`.toLowerCase().includes(t)));
    return async (cursor: string | null, limit: number): Promise<Page<DocumentationHit>> => {
      const start = cursor ? Number(cursor) : 0;
      const items = rows.slice(start, start + limit);
      return { items, nextCursor: start + limit < rows.length ? String(start + limit) : null };
    };
  }

  private held(signal: AbortSignal | undefined): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      this.waiting.push(resolve);
      signal?.addEventListener('abort', () => reject(abortError()), { once: true });
    });
  }
}

/** The fake of the suggestions; `provideFakeApis` spreads it. */
export function provideSearchFakes(): Provider[] {
  return [{ provide: SearchApi, useClass: FakeSearchApi }];
}
