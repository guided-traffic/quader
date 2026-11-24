import { Injectable, Provider } from '@angular/core';
import { DeliveryRequest, DeliveryRequestAllow, DeliveryRequestAllowed, DeliveryRequestDeclined, DeliveryRequestFilters, DeliveryRequestPage, DeliveryRequestsApi, ExaminationDiscarded } from '../delivery-requests-api';
import { DeliveryRule, DeliveryRuleFilters, DeliveryRuleInput, DeliveryRulePage, DeliveryRulesApi, DeliveryRuleWritten } from '../delivery-rules-api';
import { ApiError } from '../problem';
import {
  DeliveryDecision,
  UpstreamCatalogue,
  UpstreamCatalogueApi,
  UpstreamHit,
  UpstreamLevelPage,
  UpstreamPackage,
  UpstreamSearch,
  UpstreamSearchPage,
  UpstreamVersion,
  UpstreamVulnerabilities,
} from '../upstream-catalogue-api';

/** A decision of the mode `allExceptBlocked` without a rule. */
export const DELIVERED: DeliveryDecision = { delivered: true, decidedBy: { mode: 'allExceptBlocked' } };
/** A decision of the mode `onlyAllowed` without a rule. */
export const NOT_ALLOWED: DeliveryDecision = { delivered: false, reason: 'notAllowed', decidedBy: { mode: 'onlyAllowed' } };

/** The capabilities of an npm remote on the public registry: a search of the upstream, scopes, no sizes. */
export function catalogueCaps(over: Partial<UpstreamCatalogue> = {}): UpstreamCatalogue {
  return {
    search: 'upstream',
    searchWindow: 5250,
    searchNote: 'The registry ranks names, descriptions, keywords and maintainers.',
    searchReason: null,
    levels: ['scope'],
    levelsReason: null,
    sizes: 'unknown',
    hosts: ['replicate.npmjs.com'],
    mode: 'allExceptBlocked',
    rules: 0,
    openRequests: null,
    queue: { waiting: 0, running: 0, failed: 0, done: 0 },
    maySetRules: false,
    ...over,
  };
}

export function upstreamHit(over: Partial<UpstreamHit> = {}): UpstreamHit {
  return {
    name: 'lodash',
    description: 'Lodash modular utilities.',
    latest: '4.17.21',
    updated: '2026-11-01T00:00:00Z',
    flags: [],
    delivery: DELIVERED,
    stored: 'none',
    request: null,
    ...over,
  };
}

export function upstreamVersion(over: Partial<UpstreamVersion> = {}): UpstreamVersion {
  return {
    version: '4.17.21',
    published: '2026-11-01T00:00:00Z',
    size: null,
    unpackedSize: null,
    flags: [],
    platforms: [],
    digest: null,
    delivery: DELIVERED,
    stored: 'none',
    queued: null,
    request: null,
    ...over,
  };
}

export function upstreamPackage(over: Partial<UpstreamPackage> = {}): UpstreamPackage {
  return {
    name: 'lodash',
    description: 'Lodash modular utilities.',
    tags: { latest: '4.17.21' },
    delivery: DELIVERED,
    stored: 'none',
    request: null,
    versions: [upstreamVersion(), upstreamVersion({ version: '4.17.20' })],
    total: 2,
    nextCursor: null,
    done: true,
    ...over,
  };
}

/** The catalogue in memory: canned answers per call, every call recorded. */
@Injectable()
export class FakeUpstreamCatalogueApi extends UpstreamCatalogueApi {
  capsAnswer: UpstreamCatalogue = catalogueCaps();
  capsError: ApiError | null = null;
  hits: UpstreamHit[] = [upstreamHit()];
  searchError: ApiError | null = null;
  withheld: number | null = null;
  levels: Record<string, UpstreamLevelPage> = {};
  packages: Record<string, UpstreamPackage> = { lodash: upstreamPackage() };
  packageError: ApiError | null = null;
  notCovered = false;
  readonly calls: string[] = [];

  override async caps(repository: string): Promise<UpstreamCatalogue> {
    this.calls.push(`caps ${repository}`);
    if (this.capsError) throw this.capsError;
    return this.capsAnswer;
  }

  override async search(repository: string, query: UpstreamSearch): Promise<UpstreamSearchPage> {
    this.calls.push(`search ${query.q}${query.namesOnly ? ' names' : ''}`);
    if (this.searchError) throw this.searchError;
    const hits = this.hits.filter((h) => h.name.includes(query.q) || h.description.includes(query.q));
    return { hits, total: hits.length, nextCursor: null, done: true, truncated: false, note: null, withheld: this.withheld, withheldNote: null };
  }

  override async level(repository: string, path: readonly string[]): Promise<UpstreamLevelPage> {
    this.calls.push(`level ${path.join('/')}`);
    return this.levels[path.join('/')] ?? { levels: [], packages: [], total: 0, nextCursor: null, done: true, withheld: null, withheldNote: null, unknown: null };
  }

  override async package(repository: string, name: string, cursor: string | null, filter: string): Promise<UpstreamPackage> {
    this.calls.push(`package ${name}${filter ? ' ' + filter : ''}`);
    if (this.packageError) throw this.packageError;
    const p = this.packages[name];
    if (!p) throw new ApiError(404, { code: 'catalogue_not_found', detail: 'not found' }, 'rid-404');
    return filter ? { ...p, versions: p.versions.filter((v) => v.version.includes(filter)) } : p;
  }

  override async vulnerabilities(repository: string, name: string, versions: readonly string[]): Promise<UpstreamVulnerabilities> {
    this.calls.push(`vulnerabilities ${name} ${versions.length}`);
    return {
      notCovered: this.notCovered,
      versions: this.notCovered ? [] : versions.map((version) => ({ version, stored: false, verdict: 'none', reason: null, advisoryId: null, enforced: true })),
    };
  }
}

export function deliveryRule(over: Partial<DeliveryRule> = {}): DeliveryRule {
  return {
    id: 'rule-1',
    kind: 'package',
    subject: 'lodash',
    name: 'lodash',
    effect: 'allow',
    note: '',
    requestId: null,
    createdBy: { id: 'u-1', name: 'priya' },
    createdAt: '2026-11-24T08:00:00Z',
    ...over,
  };
}

@Injectable()
export class FakeDeliveryRulesApi extends DeliveryRulesApi {
  rules: DeliveryRule[] = [];
  putError: ApiError | null = null;
  readonly calls: string[] = [];

  override async list(repository: string, filters: DeliveryRuleFilters): Promise<DeliveryRulePage> {
    this.calls.push(`list ${filters.q ?? ''}`.trim());
    const items = this.rules.filter((r) => (!filters.q || r.subject.includes(filters.q)) && (!filters.kind || r.kind === filters.kind));
    return { items, nextCursor: null };
  }

  override async put(repository: string, rule: DeliveryRuleInput): Promise<DeliveryRuleWritten> {
    const subject = rule.kind === 'section' ? (rule.pattern ?? '') : rule.kind === 'version' ? `${rule.name}@${rule.version}` : (rule.name ?? '');
    this.calls.push(`put ${rule.kind} ${subject} ${rule.effect}`);
    if (this.putError) throw this.putError;
    const written = deliveryRule({ id: `rule-${this.rules.length + 1}`, kind: rule.kind, subject, effect: rule.effect, note: rule.note ?? '' });
    this.rules = [written, ...this.rules.filter((r) => r.subject !== subject || r.kind !== rule.kind)];
    return { ...written, revision: this.rules.length };
  }

  override async remove(repository: string, id: string): Promise<void> {
    this.calls.push(`remove ${id}`);
    this.rules = this.rules.filter((r) => r.id !== id);
  }
}

export function deliveryRequest(over: Partial<DeliveryRequest> = {}): DeliveryRequest {
  return {
    id: 'req-1',
    name: '@gitbeaker/rest',
    version: null,
    state: 'open',
    askers: [{ id: 'u-2', name: 'sam', note: 'For the release tooling.', askedAt: '2026-11-24T07:00:00Z' }],
    askerCount: 1,
    refusals: 14,
    firstAt: '2026-11-24T07:00:00Z',
    lastAt: '2026-11-24T08:00:00Z',
    examination: null,
    examined: [{ version: '39.2.0', fetchedAt: '2026-11-24T07:01:00Z', files: 1 }],
    examinationRefused: null,
    decidedBy: null,
    decidedAt: null,
    declineReason: null,
    ...over,
  };
}

@Injectable()
export class FakeDeliveryRequestsApi extends DeliveryRequestsApi {
  requests: DeliveryRequest[] = [];
  readonly calls: string[] = [];
  readonly listCalls: DeliveryRequestFilters[] = [];

  override async list(repository: string, filters: DeliveryRequestFilters): Promise<DeliveryRequestPage> {
    this.listCalls.push(filters);
    return { items: this.requests.filter((r) => !filters.state || r.state === filters.state), nextCursor: null };
  }

  override async ask(repository: string, ask: { readonly name: string; readonly version?: string; readonly note?: string }): Promise<DeliveryRequest> {
    this.calls.push(`ask ${ask.name}${ask.version ? ' ' + ask.version : ''}${ask.note ? ' "' + ask.note + '"' : ''}`);
    const r = deliveryRequest({ id: `req-${this.requests.length + 1}`, name: ask.name, version: ask.version ?? null, refusals: 0, examined: [] });
    this.requests = [r, ...this.requests];
    return r;
  }

  override async allow(repository: string, id: string, allow: DeliveryRequestAllow): Promise<DeliveryRequestAllowed> {
    this.calls.push(`allow ${id} ${allow.as}${allow.downloadEveryVersion ? ' every' : ''}`);
    const r = this.set(id, 'allowed');
    return { request: r, rule: { ...deliveryRule(), revision: 1 }, downloads: [], bulk: null };
  }

  override async decline(repository: string, id: string, decline: { readonly reason: string; readonly discard: boolean }): Promise<DeliveryRequestDeclined> {
    this.calls.push(`decline ${id} "${decline.reason}" ${decline.discard}`);
    const r = this.set(id, 'declined');
    return { request: r, discarded: { versions: [], files: 0 } };
  }

  override async withdraw(repository: string, id: string): Promise<DeliveryRequest> {
    this.calls.push(`withdraw ${id}`);
    return this.set(id, 'withdrawn');
  }

  override async discard(repository: string, subject: { readonly name: string; readonly version?: string }): Promise<ExaminationDiscarded> {
    this.calls.push(`discard ${subject.name}${subject.version ? ' ' + subject.version : ''}`);
    return { versions: subject.version ? [subject.version] : [], files: 1 };
  }

  private set(id: string, state: DeliveryRequest['state']): DeliveryRequest {
    const r = this.requests.find((x) => x.id === id);
    if (!r) throw new ApiError(404, { code: 'not_found', detail: 'not found' }, 'rid-404');
    const next = { ...r, state };
    this.requests = this.requests.map((x) => (x.id === id ? next : x));
    return next;
  }
}

export function provideUpstreamCatalogueFakes(): Provider[] {
  return [
    { provide: UpstreamCatalogueApi, useClass: FakeUpstreamCatalogueApi },
    { provide: DeliveryRulesApi, useClass: FakeDeliveryRulesApi },
    { provide: DeliveryRequestsApi, useClass: FakeDeliveryRequestsApi },
  ];
}
