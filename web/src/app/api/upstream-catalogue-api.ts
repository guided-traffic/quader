import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { Schema } from './types';

/** What a person can see of a remote repository's upstream, with the mode, the counts and the caller's right (R-CUR-007, R-CUR-009). */
export type UpstreamCatalogue = Schema<'UpstreamCatalogue'>;
/** A package of a search or a level, with the repository's state (R-CUR-009). */
export type UpstreamHit = Schema<'UpstreamHit'>;
export type UpstreamSearchPage = Schema<'UpstreamSearchPage'>;
export type UpstreamLevelPage = Schema<'UpstreamLevelPage'>;
/** A package looked up by its exact name with one page of its versions (R-CUR-009). */
export type UpstreamPackage = Schema<'UpstreamPackage'>;
/** A version as the upstream tells it, with the repository's state (R-CUR-009). */
export type UpstreamVersion = Schema<'UpstreamVersion'>;
/** The vulnerability verdicts of versions the repository has not stored (R-CUR-009, R-VULN-015). */
export type UpstreamVulnerabilities = Schema<'UpstreamVulnerabilities'>;
export type UpstreamVerdict = UpstreamVulnerabilities['versions'][number];
/** The rules' decision of a package or a version and what made it (R-CUR-001). */
export type DeliveryDecision = Schema<'DeliveryDecision'>;
/** The open request entry of a subject, or null (R-CUR-015). */
export type UpstreamRequestState = Schema<'UpstreamRequestState'>;

/** The most versions one call of `upstream/vulnerabilities` takes (R-CUR-009). */
export const VERDICTS_PER_CALL = 50;
/** A page of versions, the API's largest (R-CUR-008). */
export const VERSIONS_PAGE = 100;
/** A page of hits, the API's largest. */
export const HITS_PAGE = 50;
/** A page of a level, the API's largest. */
export const LEVEL_PAGE = 200;

/** The arguments of a search; absent members ask nothing of them. */
export interface UpstreamSearch {
  readonly q: string;
  readonly namesOnly?: boolean;
  readonly cursor?: string | null;
}

/** A level as the API takes it: the path joined by `/`, each part URL-encoded (R-CUR-009). */
export function levelParam(path: readonly string[]): string {
  return path.map((p) => encodeURIComponent(p)).join('/');
}

/** `/api/v1/projects/{project}/repositories/{key}` of a qualified name `<project>/<key>`. */
export function repositoryBase(repository: string): string {
  const slash = repository.indexOf('/');
  return `/api/v1/projects/${encodeURIComponent(repository.slice(0, slash))}/repositories/${encodeURIComponent(repository.slice(slash + 1))}`;
}

/**
 * The upstream catalogue of a remote repository (tag `remote-curation`, R-CUR-009): its capabilities, a search, a level, a package
 * with a page of its versions, and the vulnerability verdicts of versions that are not stored. `repository` is the qualified name
 * `<project>/<key>`. Nothing is stored in the cache (R-CUR-008). A refusal is an `ApiError`; the catalogue's own
 * (`catalogue_budget_exhausted` with its `Retry-After`, `upstream_assumed_offline`, `catalogue_answer_too_large`) are said by the
 * screen in its own words, so the reads are never sent again by the client's automatic retry.
 */
export abstract class UpstreamCatalogueApi {
  /** `GET …/upstream`. */
  abstract caps(repository: string): Promise<UpstreamCatalogue>;
  /** `GET …/upstream/search`. */
  abstract search(repository: string, query: UpstreamSearch): Promise<UpstreamSearchPage>;
  /** `GET …/upstream/levels`: the root for an empty path. */
  abstract level(repository: string, path: readonly string[], cursor: string | null): Promise<UpstreamLevelPage>;
  /** `GET …/upstream/package`: the package and one page of its versions, newest first; `filter` a substring of the version. */
  abstract package(repository: string, name: string, cursor: string | null, filter: string): Promise<UpstreamPackage>;
  /** `POST …/upstream/vulnerabilities`: at most {@link VERDICTS_PER_CALL} versions. */
  abstract vulnerabilities(repository: string, name: string, versions: readonly string[]): Promise<UpstreamVulnerabilities>;
}

@Injectable()
export class HttpUpstreamCatalogueApi extends UpstreamCatalogueApi {
  private readonly client = inject(ApiClient);

  async caps(repository: string): Promise<UpstreamCatalogue> {
    return (await this.client.getJson<UpstreamCatalogue>(`${repositoryBase(repository)}/upstream`, { noRetry: true })).body;
  }

  async search(repository: string, query: UpstreamSearch): Promise<UpstreamSearchPage> {
    const params: Record<string, string | number | boolean> = { q: query.q, limit: HITS_PAGE };
    if (query.namesOnly) params['namesOnly'] = true;
    if (query.cursor) params['cursor'] = query.cursor;
    return (await this.client.getJson<UpstreamSearchPage>(`${repositoryBase(repository)}/upstream/search`, { params, noRetry: true })).body;
  }

  async level(repository: string, path: readonly string[], cursor: string | null): Promise<UpstreamLevelPage> {
    const params: Record<string, string | number> = { limit: LEVEL_PAGE };
    if (path.length > 0) params['level'] = levelParam(path);
    if (cursor) params['cursor'] = cursor;
    return (await this.client.getJson<UpstreamLevelPage>(`${repositoryBase(repository)}/upstream/levels`, { params, noRetry: true })).body;
  }

  async package(repository: string, name: string, cursor: string | null, filter: string): Promise<UpstreamPackage> {
    const params: Record<string, string | number> = { name, limit: VERSIONS_PAGE };
    if (cursor) params['cursor'] = cursor;
    if (filter) params['filter'] = filter;
    return (await this.client.getJson<UpstreamPackage>(`${repositoryBase(repository)}/upstream/package`, { params, noRetry: true })).body;
  }

  async vulnerabilities(repository: string, name: string, versions: readonly string[]): Promise<UpstreamVulnerabilities> {
    return (await this.client.post<UpstreamVulnerabilities>(`${repositoryBase(repository)}/upstream/vulnerabilities`, { name, versions })).body;
  }
}
