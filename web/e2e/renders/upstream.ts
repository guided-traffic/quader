import { Page, Route, expect } from '@playwright/test';

/**
 * The tab Upstream of a remote repository (remote-curation.md R-CUR-018) and its states. The seed keeps the remotes of public
 * registries offline (no upstream of the internet is reachable for certain), so the walks answer the catalogue, the requests, the
 * rules and the preview in the browser with what an npm, a Debian and an OCI remote answer: a search with every state of a hit, a
 * package with thousands of versions, the requests of several people and of clients, the upstream assumed offline, the budget used up,
 * a document too large, a reader in mode `onlyAllowed` with hits withheld, an OCI upstream that offers only the lookup by name, and a
 * Debian remote's suites with its index search. Nothing is changed on the server: a write the walk does not make stays unmade.
 */

/** The time `minutes` from now, RFC 3339: the answers' times stay relative to the run. */
function at(minutes: number): string {
  return new Date(Date.now() + minutes * 60_000).toISOString();
}

const delivered = { delivered: true, decidedBy: { mode: 'onlyAllowed' } };
const notAllowed = { delivered: false, reason: 'notAllowed', decidedBy: { mode: 'onlyAllowed' } };
const allowedBy = (subject: string, kind = 'package'): Record<string, unknown> => ({ delivered: true, decidedBy: { kind, subject, effect: 'allow' } });
const blockedBy = (subject: string, kind = 'package'): Record<string, unknown> => ({ delivered: false, reason: 'blocked', decidedBy: { kind, subject, effect: 'block' } });

function json(route: Route, body: unknown, status = 200, headers: Record<string, string> = {}): Promise<void> {
  return route.fulfill({ status, contentType: status < 300 ? 'application/json' : 'application/problem+json', headers, body: JSON.stringify(body) });
}

function problem(route: Route, status: number, code: string, detail: string, retryAfter?: number): Promise<void> {
  return json(route, { code, title: code, status, detail }, status, retryAfter ? { 'Retry-After': String(retryAfter) } : {});
}

const apiOf = (suffix: string) => (url: URL): boolean => new RegExp(`^/api/v1/projects/[^/]+/repositories/[^/]+/${suffix}$`).test(url.pathname);
const capsUrl = apiOf('upstream');
export const searchUrl = apiOf('upstream/search');
const levelsUrl = apiOf('upstream/levels');
export const packageUrl = apiOf('upstream/package');
const verdictsUrl = apiOf('upstream/vulnerabilities');
const requestsUrl = apiOf('delivery-requests');
const rulesUrl = apiOf('delivery-rules');
export const previewUrl = apiOf('downloads/preview');

// ---- npm ----

function npmCaps(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    search: 'upstream',
    searchWindow: 5250,
    searchHint: 'npmRegistry',
    levels: ['scope'],
    levelsReason: null,
    sizes: 'unknown',
    hosts: ['replicate.npmjs.com'],
    mode: 'onlyAllowed',
    rules: 42,
    openRequests: 3,
    queue: { waiting: 12, running: 4, failed: 2, done: 230 },
    maySetRules: true,
    ...over,
  };
}

/** A hit as the API answers it to whoever may set rules: its package level, what it inherits and its version rules (R-CUR-024). */
function hit(name: string, over: Record<string, unknown> = {}): Record<string, unknown> {
  const delivery = over['delivery'] ?? notAllowed;
  return {
    name, description: '', latest: '1.0.0', updated: at(-60 * 24 * 3), flags: [], delivery, stored: 'none', request: null,
    level: delivery, inherited: notAllowed, versionRules: { allow: 0, block: 0 }, ...over,
  };
}

/** The hits of "gitlab" on the npm registry: every state of a hit, a long name and a long description. */
function npmHits(): Record<string, unknown>[] {
  return [
    hit('@gitlab/ui', { description: 'GitLab UI Components', latest: '124.0.3', delivery: allowedBy('@gitlab/*', 'section'), stored: 'partial' }),
    hit('gitlab-ai-provider', { description: 'An AI SDK provider for GitLab Duo and the GitLab language server', latest: '4.98.1', stored: 'partial', versionRules: { allow: 1, block: 1 } }),
    hit('@gitbeaker/rest', { description: 'Cross Platform implementation of the GitLab API', latest: '43.8.0', request: { id: 'req-1', askers: 2, refusals: 14 } }),
    hit('gitlab', { description: 'GitLab API Nodejs library.', latest: '14.2.2', flags: ['deprecated'], delivery: blockedBy('gitlab') }),
    hit('@customer-portal-release-engineering/gitlab-merge-request-pipeline-status-reporter-for-the-frontend-monorepo', {
      description:
        'Reports the state of every merge request pipeline of the frontend monorepo to the release engineering dashboard, with the jobs that failed, their logs, the reviewers who still have to approve and the environments a deployment would touch.',
      latest: '12.0.0-rc.3',
      flags: ['prerelease'],
    }),
    hit('python-gitlab-compat', { description: 'Helpers that read the configuration file of python-gitlab', latest: '0.3.1' }),
  ];
}

/**
 * 3,893 versions, the newest 24 on the first page, with every state a version can have. The API answers 100 a page; the walk answers
 * fewer, since the screen's paging reads the same with 24 rows and a page of 100 stacked at 390 px is a picture of 20,000 px.
 */
function npmVersions(name: string, level: Record<string, unknown> = notAllowed): Record<string, unknown>[] {
  const out: Record<string, unknown>[] = [];
  for (let i = 0; i < 24; i++) {
    const minor = 99 - i;
    const version = i === 0 ? '5.0.0-beta.2' : i === 8 ? '4.91.0-canary.bfb9accdc' : `4.${minor}.${i % 3}`;
    let delivery: Record<string, unknown> = level;
    let stored = 'none';
    let queued: Record<string, unknown> | null = null;
    let request: Record<string, unknown> | null = null;
    const flags: string[] = i === 0 || i === 8 ? ['prerelease'] : [];
    if (i === 1) delivery = allowedBy(`${name}@${version}`, 'version');
    if (i === 1) stored = 'whole';
    if (i === 2) stored = 'whole'; // examined: stored, not delivered
    if (i === 3) queued = { id: 'q-1', state: 'running', origin: 'person', attempts: 1, maxAttempts: 5, nextAttemptAt: null };
    if (i === 4) queued = { id: 'q-2', state: 'waiting', origin: 'request', attempts: 0, maxAttempts: 5, nextAttemptAt: null };
    if (i === 5) request = { id: 'req-3', askers: 1, refusals: 0 };
    if (i === 6) delivery = blockedBy(`${name}@${version}`, 'version');
    if (i === 7) flags.push('deprecated');
    out.push({
      version,
      published: at(-60 * 24 * (i * 4 + 1)),
      size: i % 4 === 0 ? 2_412_448 + i * 1024 : null,
      unpackedSize: null,
      flags,
      platforms: [],
      digest: null,
      delivery,
      stored,
      queued,
      request,
      inherited: level,
    });
  }
  return out;
}

function npmPackage(name: string, over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    name,
    description: 'An AI SDK provider for GitLab Duo and the GitLab language server',
    // Eight npm tags (Q-UI-1190, Q-UI-1191): five on the pre-release, one of them long, so its row shows three and "+2 more"; `latest`
    // on the row the hit's column Latest names; one on a version past the 24 rows the walk answers, which only the filter reaches.
    tags: {
      latest: '4.98.1',
      next: '5.0.0-beta.2',
      beta: '5.0.0-beta.2',
      canary: '5.0.0-beta.2',
      'eap-ai-hybridinference-experimental': '5.0.0-beta.2',
      rc: '5.0.0-beta.2',
      'v4-lts': '4.97.2',
      'legacy-node16': '3.12.4',
    },
    delivery: notAllowed,
    stored: 'partial',
    request: null,
    level: notAllowed,
    inherited: notAllowed,
    versionRules: { allow: 1, block: 1 },
    versions: npmVersions(name),
    total: 3893,
    nextCursor: 'bW9yZQ',
    done: false,
    ...over,
  };
}

function verdicts(body: { name: string; versions: string[] }): Record<string, unknown> {
  return {
    notCovered: false,
    versions: body.versions.map((version, i) =>
      i === 5
        ? { version, stored: false, verdict: 'blocked', reason: 'malicious', advisoryId: 'MAL-2026-1184', enforced: true }
        : { version, stored: false, verdict: 'none', reason: null, advisoryId: null, enforced: true },
    ),
  };
}

function download(over: Record<string, unknown>): Record<string, unknown> {
  return {
    id: 'dl-1',
    name: '',
    version: '',
    origin: 'request',
    state: 'running',
    outcome: null,
    attempts: 1,
    maxAttempts: 5,
    nextAttemptAt: null,
    startedBy: null,
    bulk: null,
    request: null,
    files: 1,
    stored: 0,
    present: 0,
    refused: 0,
    bytes: 0,
    occurrences: 1,
    failure: null,
    createdAt: at(-5),
    startedAt: at(-1),
    finishedAt: null,
    delivery: 'notAllowed',
    ...over,
  };
}

function request(over: Record<string, unknown>): Record<string, unknown> {
  return {
    id: 'req-x',
    name: '',
    version: null,
    state: 'open',
    askers: [],
    askerCount: 0,
    refusals: 0,
    firstAt: at(-60 * 26),
    lastAt: at(-12),
    examination: null,
    examined: [],
    examinationRefused: null,
    decidedBy: null,
    decidedAt: null,
    declineReason: null,
    ...over,
  };
}

/** The open entries those who decide see: people's asks with notes, clients' refusals, an examination running. */
function openRequests(): Record<string, unknown>[] {
  return [
    request({
      id: 'req-1',
      name: '@gitbeaker/rest',
      askers: [
        { id: null, name: 'Sam Example', note: 'We need it for the release tooling that opens merge requests.', askedAt: at(-60 * 20) },
        { id: null, name: 'Priya Example', note: null, askedAt: at(-60 * 5) },
      ],
      askerCount: 2,
      refusals: 14,
      examined: [{ version: '43.8.0', fetchedAt: at(-60 * 19), files: 1 }],
      examination: download({ name: '@gitbeaker/rest', version: '43.8.0', state: 'done', outcome: 'completed', stored: 1 }),
    }),
    request({ id: 'req-2', name: 'axios', version: '0.21.1', refusals: 3, lastAt: at(-40) }),
    request({
      id: 'req-3',
      name: '@customer-portal-release-engineering/gitlab-merge-request-pipeline-status-reporter-for-the-frontend-monorepo',
      version: '12.0.0-rc.3',
      askers: [{ id: null, name: 'Sam Example', note: null, askedAt: at(-30) }],
      askerCount: 1,
      examination: download({ name: 'x', version: '12.0.0-rc.3', state: 'running' }),
    }),
  ];
}

/** A reader's own entries: one open, one declined with its reason. */
function myRequests(): Record<string, unknown>[] {
  return [
    request({ id: 'req-2', name: 'axios', version: '0.21.1', askers: [{ id: null, name: null, note: 'For the legacy portal build.', askedAt: at(-50) }], askerCount: 1 }),
    request({
      id: 'req-9',
      name: 'left-pad',
      state: 'declined',
      askers: [{ id: null, name: null, note: null, askedAt: at(-60 * 48) }],
      askerCount: 1,
      declineReason: 'Use String.prototype.padStart instead; the package is unmaintained.',
      decidedAt: at(-60 * 30),
    }),
  ];
}

function rules(): Record<string, unknown>[] {
  const by = { id: null, name: 'Priya Example' };
  return [
    { id: 'r1', kind: 'section', subject: '@gitlab/*', pattern: '@gitlab/*', effect: 'allow', note: 'Everything GitLab publishes under its scope.', requestId: null, createdBy: by, createdAt: at(-60 * 24 * 2) },
    { id: 'r2', kind: 'package', subject: 'gitlab', name: 'gitlab', effect: 'block', note: '', requestId: null, createdBy: by, createdAt: at(-60 * 24 * 3) },
    {
      id: 'r3',
      kind: 'version',
      subject: 'gitlab-ai-provider@4.98.1',
      name: 'gitlab-ai-provider',
      version: '4.98.1',
      effect: 'allow',
      note: 'Allowed after the examination of 2026-11-20: no malware, no finding.',
      requestId: 'req-0',
      createdBy: by,
      createdAt: at(-60 * 24 * 4),
    },
    {
      id: 'r4',
      kind: 'section',
      subject: '@customer-portal-release-engineering-internal-tools/*',
      pattern: '@customer-portal-release-engineering-internal-tools/*',
      effect: 'block',
      note: '',
      requestId: null,
      createdBy: null,
      createdAt: at(-60 * 24 * 9),
    },
  ];
}

const preview = {
  versions: 212,
  packages: null,
  bytes: null,
  sizeUnknown: 212,
  stored: 2,
  notDelivered: 210,
  notVersions: 0,
  bounds: { maxVersions: 10000, maxBytes: 107374182400, includePrereleases: true, fromVersion: null, toVersion: null, toInclusive: true },
  note: 'sizes_unannounced',
  // What keeping the package in sync would do (R-CUR-027): the two delivered versions it has not stored.
  sync: { versions: 2, delivered: true, intervalSeconds: 3600, paused: false, existing: null },
};

/** The preview of what the dialog asks: a range of versions (Q-CUR-190) counts the 37 versions between its bounds and keeps them. */
function previewOf(body: Record<string, unknown> | null): Record<string, unknown> {
  const from = (body?.['fromVersion'] as string | undefined) ?? null;
  const to = (body?.['toVersion'] as string | undefined) ?? null;
  if (!from && !to) return preview;
  return {
    ...preview,
    versions: 37,
    sizeUnknown: 37,
    stored: 2,
    notDelivered: 35,
    bounds: { ...preview.bounds, fromVersion: from, toVersion: to, toInclusive: body?.['toInclusive'] !== false },
  };
}

/** Every call of the tab answered as an npm remote in mode `onlyAllowed` answers it; `over` changes one call's answer. */
export async function answerNpm(p: Page, over: { caps?: (r: Route) => Promise<void>; search?: (r: Route) => Promise<void>; pkg?: (r: Route) => Promise<void> } = {}): Promise<void> {
  await p.unrouteAll({ behavior: 'ignoreErrors' });
  await p.route(capsUrl, over.caps ?? ((r) => json(r, npmCaps())));
  await p.route(searchUrl, over.search ?? ((r) => {
    const q = new URL(r.request().url()).searchParams.get('q') ?? '';
    if (q === 'zzqx') return json(r, { hits: [], total: 0, nextCursor: null, done: true, truncated: false, note: null, withheld: null, withheldNote: null });
    return json(r, { hits: npmHits(), total: 11235, nextCursor: 'bW9yZQ', done: false, truncated: true, note: null, withheld: null, withheldNote: null });
  }));
  await p.route(levelsUrl, (r) => json(r, { levels: [], packages: [], total: null, nextCursor: null, done: true, withheld: null, withheldNote: null, unknown: null }));
  await p.route(packageUrl, over.pkg ?? ((r) => json(r, packageOf(new URL(r.request().url()).searchParams.get('name') ?? 'gitlab-ai-provider'))));
  await p.route(verdictsUrl, (r) => json(r, verdicts(r.request().postDataJSON())));
  await p.route(requestsUrl, (r) => {
    if (r.request().method() !== 'GET') return r.fallback();
    const mine = new URL(r.request().url()).searchParams.get('mine') === 'true';
    return json(r, { items: mine ? myRequests() : openRequests(), nextCursor: null });
  });
  await p.route(rulesUrl, (r) => (r.request().method() === 'GET' ? json(r, { items: rules(), nextCursor: null }) : r.fallback()));
  await p.route(previewUrl, (r) => json(r, previewOf(r.request().postDataJSON() as Record<string, unknown> | null)));
}

/**
 * The package a lookup answers: `axios` allowed by its package rule with one version blocked (R-CUR-024's mixed switches), every other
 * name the not allowed `gitlab-ai-provider` of the search.
 */
function packageOf(name: string): Record<string, unknown> {
  if (name !== 'axios') return npmPackage(name);
  const level = allowedBy('axios');
  return npmPackage(name, { delivery: level, level, inherited: notAllowed, versionRules: { allow: 1, block: 1 }, versions: npmVersions(name, level) });
}

/** The package and the hits of a repository in mode `allExceptBlocked` without a rule: every switch on, the cache's Download. */
function deliveredByMode(name: string): Record<string, unknown> {
  const all = { delivered: true, decidedBy: { mode: 'allExceptBlocked' } };
  const versions = npmVersions(name, all).map((v, i) => ({ ...v, delivery: all, inherited: all, stored: i === 1 ? 'whole' : 'none', request: null }));
  return npmPackage(name, { delivery: all, level: all, inherited: all, versionRules: { allow: 0, block: 0 }, versions });
}

/**
 * A package allowed with a range of versions in mode `allExceptBlocked` (R-CUR-026): from 4.80.1 below 5.0.0, so the newest
 * pre-release lies above it and the oldest versions below it, each with the badge Outside the allowed versions; one version outside the
 * range switched on by its own allow.
 */
function rangedPackage(name: string): Record<string, unknown> {
  const range = { fromVersion: '4.80.1', toVersion: '5.0.0', toInclusive: false };
  const all = { delivered: true, decidedBy: { mode: 'allExceptBlocked' } };
  const level = { delivered: true, decidedBy: { kind: 'package', subject: name, effect: 'allow', range } };
  const outside = { delivered: false, reason: 'outsideRange', decidedBy: { kind: 'package', subject: name, effect: 'allow', range } };
  const inside = (v: string): boolean => {
    if (v.startsWith('5.')) return false;
    const minor = Number(v.split('.')[1]);
    return minor > 80 || (minor === 80 && !v.endsWith('.0'));
  };
  const versions = npmVersions(name, level).map((v, i) => {
    const version = v['version'] as string;
    const own = i === 1 ? allowedBy(`${name}@${version}`, 'version') : null;
    return { ...v, delivery: own ?? (inside(version) ? level : outside), inherited: inside(version) ? level : outside, queued: null, request: null };
  });
  return npmPackage(name, { delivery: level, level, inherited: all, versionRules: { allow: 1, block: 0 }, versions });
}

/** A query of the tab's address, the page reloaded with the answers in place. */
export async function open(p: Page, query: string): Promise<void> {
  const url = new URL(p.url());
  url.search = query;
  await p.goto(url.toString());
}

export const tab = 'main quader-remote-upstream';

/**
 * The tab as it opens (the seed's offline remote: the offline mode's sentence), then answered as an npm remote: a search with every
 * state of a hit and the requests set apart (a reader sees its own), a package opened to its versions, the rule list and the preview
 * of a bulk for those who decide, a lookup by name, the upstream assumed offline, the budget used up, a document too large, a search
 * without a hit, and a reader in mode `onlyAllowed` with hits withheld. The controls by their place, never their words: the German
 * pass walks the tab too (UI_RENDERS_LOCALE=de).
 */
export async function* upstreamTabWalk(p: Page): AsyncGenerator<string> {
  await expect(p.locator(`${tab} [data-testid="upstream-state"], ${tab} form.search`).first()).toBeVisible();
  yield '';
  await answerNpm(p);
  await open(p, '?q=gitlab');
  await expect(p.locator(`${tab} tbody tr:not(.expansion)`)).toHaveCount(6);
  await expect(p.locator(`${tab} [data-testid="upstream-requests"]`)).toBeVisible();
  yield 'search';
  await p.locator(`${tab} button[aria-controls="up-gitlab-ai-provider"]`).click();
  await expect(p.locator(`${tab} quader-upstream-package tbody tr`).first()).toBeVisible();
  yield 'package';
  const rulesButton = p.locator(`${tab} [data-testid="upstream-rules"]`);
  if ((await rulesButton.count()) > 0) {
    await rulesButton.click();
    await expect(p.locator('quader-delivery-rules-dialog tbody tr').first()).toBeVisible();
    yield 'rules';
    await p.keyboard.press('Escape');
    await expect(p.locator('quader-delivery-rules-dialog')).toBeHidden();
  }
  const every = p.locator(`${tab} [data-testid="upstream-download-every"]`).first();
  if ((await every.count()) > 0) {
    await every.click();
    await expect(p.locator('quader-download-preview-dialog [data-testid="download-preview"]')).toBeVisible();
    yield 'preview';
    // A range of versions (Q-CUR-190, Q-CUR-194): from a version the package lists, below one typed; the preview counts it again.
    await p.locator('#preview-from-version').fill('4.80.1');
    await p.locator('#preview-from-version').press('Tab');
    await p.locator('#preview-to-version').fill('5.0.0');
    await p.locator('#preview-to-version').press('Tab');
    await p.locator('quader-download-preview-dialog select.to-inclusive').selectOption('below');
    await expect(p.locator('quader-download-preview-dialog [data-testid="download-preview"] dd').first()).toHaveText('37');
    yield 'preview-range';
    await p.keyboard.press('Escape');
    await expect(p.locator('quader-download-preview-dialog')).toBeHidden();
  }
  await open(p, '?name=axios');
  await expect(p.locator(`${tab} [data-testid="upstream-lookup"] quader-upstream-package tbody tr`).first()).toBeVisible();
  yield 'lookup';
  // Mode allExceptBlocked without a rule: the package's switch and every version's on, the cache's Download (R-CUR-024).
  await answerNpm(p, {
    caps: (r) => json(r, npmCaps({ mode: 'allExceptBlocked', rules: 0, openRequests: 0 })),
    search: (r) => json(r, { hits: npmHits().map((h) => ({ ...h, delivery: { delivered: true, decidedBy: { mode: 'allExceptBlocked' } }, level: { delivered: true, decidedBy: { mode: 'allExceptBlocked' } } })), total: 11235, nextCursor: null, done: true, truncated: true, note: null, withheld: null, withheldNote: null }),
    pkg: (r) => json(r, deliveredByMode(new URL(r.request().url()).searchParams.get('name') ?? 'gitlab-ai-provider')),
  });
  await open(p, '?q=gitlab');
  await p.locator(`${tab} button[aria-controls="up-gitlab-ai-provider"]`).click();
  await expect(p.locator(`${tab} quader-upstream-package tbody tr`).first()).toBeVisible();
  yield 'package-delivered';
  // A range of the package's allow (R-CUR-026): the line under the switch with Limit versions…, the versions outside it with their
  // badge and the package's range as their rule. The dialog is rendered in the image's walk (upstreamLookupWalk).
  await answerNpm(p, {
    caps: (r) => json(r, npmCaps({ mode: 'allExceptBlocked', rules: 2, openRequests: 0 })),
    pkg: (r) => json(r, rangedPackage(new URL(r.request().url()).searchParams.get('name') ?? 'gitlab-ai-provider')),
  });
  await open(p, '?name=gitlab-ai-provider');
  await expect(p.locator(`${tab} [data-testid="upstream-package-range"]`)).toBeVisible();
  yield 'package-range';
  await answerNpm(p);
  await open(p, '?q=zzqx');
  await expect(p.locator(`${tab} quader-data-table`)).toBeVisible();
  yield 'no-hit';
  await answerNpm(p, { search: (r) => problem(r, 429, 'catalogue_budget_exhausted', 'The upstream catalogue budget of web-frontend/npmjs is used up.', 40) });
  await open(p, '?q=gitlab');
  await expect(p.locator(`${tab} [data-testid="upstream-list-state"]`)).toBeVisible();
  yield 'budget';
  await answerNpm(p, { pkg: (r) => problem(r, 502, 'catalogue_answer_too_large', 'The document is larger than 64 MiB.') });
  await open(p, '?name=typescript');
  await expect(p.locator(`${tab} [data-testid="upstream-package-state"]`)).toBeVisible();
  yield 'too-large';
  await answerNpm(p, { caps: (r) => problem(r, 503, 'upstream_assumed_offline', 'The upstream of web-frontend/npmjs is assumed to be offline.', 600) });
  await open(p, '');
  await expect(p.locator(`${tab} [data-testid="upstream-state"]`)).toBeVisible();
  yield 'offline';
  // A reader in mode onlyAllowed: the hits that are not delivered are withheld, with the sentence that says how to ask for one.
  await answerNpm(p, {
    caps: (r) => json(r, npmCaps({ openRequests: null, maySetRules: false })),
    search: (r) =>
      json(r, { hits: npmHits().filter((h) => (h['delivery'] as { delivered: boolean }).delivered), total: 11235, nextCursor: null, done: true, truncated: true, note: null, withheld: 14, withheldNote: null }),
  });
  await open(p, '?q=gitlab');
  await expect(p.locator(`${tab} [data-testid="upstream-withheld"]`)).toBeVisible();
  yield 'withheld';
  await p.unrouteAll({ behavior: 'ignoreErrors' });
}

/**
 * The list of a package's versions, which keeps the form of a table from 1,024 px on (Q-CUR-157): with seven columns it stood as
 * blocks of five lines per version at 1,024 px (2026-11-24).
 */
export const upstreamVersions = 'quader-upstream-package quader-data-table';

/** The answers of the npm walk that are its states, not failures. */
export const upstreamExpected = [
  { match: /^GET \/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/upstream(\/package|\/search)?(\?\S*)? answered (409|429|502|503)$/, why: 'the seed keeps public remotes offline, and the walk answers the budget, the size bound and the circuit to render their states' },
];

// ---- OCI: an upstream that offers only the lookup by name ----

const ghcrCaps = {
  search: 'none',
  searchWindow: null,
  searchHint: 'ghcr',
  levels: [],
  levelsReason: 'ghcr.io lists no namespaces.',
  sizes: 'perVersion',
  hosts: [],
  mode: 'allExceptBlocked',
  rules: 0,
  openRequests: 0,
  queue: { waiting: 0, running: 0, failed: 0, done: 3 },
  maySetRules: true,
};

function ociTags(): Record<string, unknown>[] {
  return ['v2.4.0', 'v2.3.1', 'v2.3.0', 'v2.2.3', 'latest'].map((version, i) => ({
    version,
    published: null,
    size: i === 4 ? null : 21_495_808 + i * 4096,
    unpackedSize: null,
    flags: [],
    platforms: ['linux/amd64', 'linux/arm64', 'linux/arm/v7'],
    digest: `sha256:${'4f3c2a1b'.repeat(8)}`,
    delivery: { delivered: true, decidedBy: { mode: 'allExceptBlocked' } },
    stored: i === 1 ? 'whole' : 'none',
    queued: null,
    request: null,
  }));
}

/**
 * An OCI remote of ghcr.io: no search and no listing, so the field is **Look up by name** with the sentence that says why; an image
 * looked up with its tags and their platforms; then the upstream assumed offline at the lookup.
 */
export async function* upstreamLookupWalk(p: Page): AsyncGenerator<string> {
  await p.unrouteAll({ behavior: 'ignoreErrors' });
  await p.route(capsUrl, (r) => json(r, ghcrCaps));
  await p.route(requestsUrl, (r) => json(r, { items: [], nextCursor: null }));
  await p.route(packageUrl, (r) =>
    json(r, { name: 'fluxcd/flux-cli', description: '', tags: {}, delivery: { delivered: true, decidedBy: { mode: 'allExceptBlocked' } }, stored: 'partial', request: null, versions: ociTags(), total: 5, nextCursor: null, done: true }),
  );
  await p.route(verdictsUrl, (r) => json(r, { notCovered: true, versions: [] }));
  await p.reload();
  await expect(p.locator(`${tab} form.search`)).toBeVisible();
  yield '';
  await open(p, '?name=fluxcd%2Fflux-cli');
  await expect(p.locator(`${tab} [data-testid="upstream-lookup"] quader-upstream-package tbody tr`).first()).toBeVisible();
  yield 'lookup';
  // An image allowed from v2.3.0 (R-CUR-026): latest is no version of the tag order and lies outside it, with its own switch off, and
  // the head says that docker pull without a tag asks for it (Q-CUR-223).
  const range = { fromVersion: 'v2.3.0', toVersion: null, toInclusive: true };
  const level = { delivered: true, decidedBy: { kind: 'package', subject: 'fluxcd/flux-cli', effect: 'allow', range } };
  const outside = { delivered: false, reason: 'outsideRange', decidedBy: { kind: 'package', subject: 'fluxcd/flux-cli', effect: 'allow', range } };
  const ranged = ociTags().map((t) => {
    const inside = ['v2.4.0', 'v2.3.1', 'v2.3.0'].includes(t['version'] as string);
    return { ...t, delivery: inside ? level : outside, inherited: inside ? level : outside };
  });
  await p.unroute(packageUrl);
  await p.route(packageUrl, (r) =>
    json(r, {
      name: 'fluxcd/flux-cli', description: '', tags: {}, delivery: level, level, inherited: { delivered: true, decidedBy: { mode: 'allExceptBlocked' } },
      versionRules: { allow: 0, block: 0 }, stored: 'partial', request: null, versions: ranged, total: 5, nextCursor: null, done: true,
    }),
  );
  await p.reload();
  await expect(p.locator(`${tab} [data-testid="upstream-package-range"]`)).toBeVisible();
  yield 'image-range';
  // Limit versions… of the image: the dialog as it opens on the range, which says that latest lies outside every range.
  const limit = p.locator(`${tab} [data-testid="upstream-limit-versions"]`);
  if ((await limit.count()) > 0) {
    await limit.click();
    await expect(p.locator('quader-version-range-dialog #range-from-version')).toBeVisible();
    yield 'range-dialog';
    await p.keyboard.press('Escape');
    await expect(p.locator('quader-version-range-dialog')).toBeHidden();
  }
  await p.unroute(packageUrl);
  await p.route(packageUrl, (r) => problem(r, 503, 'upstream_assumed_offline', 'The upstream is assumed to be offline after repeated failures.', 1320));
  await p.reload();
  await expect(p.locator(`${tab} [data-testid="upstream-package-state"]`)).toBeVisible();
  yield 'offline';
  await p.unrouteAll({ behavior: 'ignoreErrors' });
}

// ---- OCI: a namespace Docker Hub does not have ----

const hubCaps = {
  ...ghcrCaps,
  search: 'upstream',
  searchHint: 'dockerHub',
  levels: ['namespace'],
  levelsReason: null,
  sizes: 'known',
  hosts: ['hub.docker.com'],
};

const emptyLevel = { levels: [], packages: [], total: 0, nextCursor: null, done: true, withheld: null, withheldNote: null, unknown: null };

const hubLevels: Record<string, Record<string, unknown>> = {
  '': emptyLevel,
  // The owner's namespace typed with the hyphen of his domain (2026-11-24): no Docker Hub namespace holds one (Q-CUR-165).
  'guided-traffic': { ...emptyLevel, unknown: { upstream: 'Docker Hub', rule: 'lowercaseDigits', forbidden: '-', suggestion: 'guidedtraffic' } },
  guidedtraffik: { ...emptyLevel, unknown: { upstream: 'Docker Hub', rule: null, forbidden: null, suggestion: null } },
};

/**
 * An OCI remote of Docker Hub at a namespace entered by name that Docker Hub does not have: with a hyphen, which no namespace holds,
 * the rule and the namespace it may have meant; then a name that keeps the rule and that Docker Hub does not know (Q-CUR-165).
 */
export async function* upstreamUnknownLevelWalk(p: Page): AsyncGenerator<string> {
  await p.unrouteAll({ behavior: 'ignoreErrors' });
  await p.route(capsUrl, (r) => json(r, hubCaps));
  await p.route(requestsUrl, (r) => json(r, { items: [], nextCursor: null }));
  await p.route(levelsUrl, (r) => json(r, hubLevels[decodeURIComponent(new URL(r.request().url()).searchParams.get('level') ?? '')] ?? emptyLevel));
  await open(p, '?level=guided-traffic');
  await expect(p.locator(`${tab} [data-testid="upstream-suggestion"]`)).toBeVisible();
  yield 'rule';
  await open(p, '?level=guidedtraffik');
  await expect(p.locator(`${tab} quader-empty-state`)).toBeVisible();
  yield 'unknown';
  await p.unrouteAll({ behavior: 'ignoreErrors' });
}

// ---- Debian: suites and the index search ----

const debCaps = {
  search: 'index',
  searchWindow: null,
  searchHint: 'aptIndex',
  levels: ['suite', 'component', 'architecture'],
  levelsReason: null,
  sizes: 'known',
  hosts: [],
  mode: 'allExceptBlocked',
  rules: 1,
  openRequests: 0,
  queue: { waiting: 0, running: 0, failed: 0, done: 0 },
  maySetRules: true,
};

const debLevels: Record<string, Record<string, unknown>> = {
  '': { levels: ['trixie', 'trixie-updates', 'trixie-security', 'bookworm'], packages: [], total: 4, nextCursor: null, done: true, withheld: null, withheldNote: null, unknown: null },
  trixie: { levels: ['main', 'contrib', 'non-free', 'non-free-firmware'], packages: [], total: 4, nextCursor: null, done: true, withheld: null, withheldNote: null, unknown: null },
};

function debHits(): Record<string, unknown>[] {
  const d = { delivered: true, decidedBy: { mode: 'allExceptBlocked' } };
  return [
    { name: 'libssl3t64', description: 'Secure Sockets Layer toolkit - shared libraries', latest: '3.5.1-1+deb13u1', updated: null, flags: [], delivery: d, stored: 'none', request: null },
    { name: 'libssl-dev', description: 'Secure Sockets Layer toolkit - development files', latest: '3.5.1-1+deb13u1', updated: null, flags: [], delivery: d, stored: 'none', request: null },
    { name: 'libssl-doc', description: 'Secure Sockets Layer toolkit - development documentation', latest: '3.5.1-1+deb13u1', updated: null, flags: [], delivery: blockedBy('libssl-doc'), stored: 'none', request: null },
  ];
}

/** A Debian remote: its suites and a suite's components, then the index searched by name only and a package opened. */
export async function* upstreamDebWalk(p: Page): AsyncGenerator<string> {
  await p.unrouteAll({ behavior: 'ignoreErrors' });
  await p.route(capsUrl, (r) => json(r, debCaps));
  await p.route(requestsUrl, (r) => json(r, { items: [], nextCursor: null }));
  await p.route(levelsUrl, (r) => json(r, debLevels[decodeURIComponent(new URL(r.request().url()).searchParams.get('level') ?? '')] ?? debLevels['']));
  await p.route(searchUrl, (r) => json(r, { hits: debHits(), total: 3, nextCursor: null, done: true, truncated: false, note: null, withheld: null, withheldNote: null }));
  await p.route(packageUrl, (r) =>
    json(r, {
      name: 'libssl3t64',
      description: 'Secure Sockets Layer toolkit - shared libraries',
      tags: {},
      delivery: { delivered: true, decidedBy: { mode: 'allExceptBlocked' } },
      stored: 'none',
      request: null,
      versions: ['3.5.1-1+deb13u1', '3.5.1-1', '3.5.0-2'].map((version) => ({
        version,
        published: null,
        size: 2_412_448,
        unpackedSize: null,
        flags: [],
        platforms: ['amd64', 'arm64'],
        digest: null,
        delivery: { delivered: true, decidedBy: { mode: 'allExceptBlocked' } },
        stored: 'none',
        queued: null,
        request: null,
      })),
      total: 3,
      nextCursor: null,
      done: true,
    }),
  );
  await p.route(verdictsUrl, (r) => json(r, { notCovered: true, versions: [] }));
  await p.reload();
  await expect(p.locator(`${tab} [data-testid="upstream-levels"] li a`).first()).toBeVisible();
  yield '';
  await open(p, '?level=trixie');
  await expect(p.locator(`${tab} [data-testid="upstream-levels"] .level-children li`).first()).toBeVisible();
  yield 'suite';
  await open(p, '?q=libssl&namesOnly=true');
  await expect(p.locator(`${tab} tbody tr:not(.expansion)`)).toHaveCount(3);
  await p.locator(`${tab} button[aria-controls="up-libssl3t64"]`).click();
  await expect(p.locator(`${tab} quader-upstream-package tbody tr`).first()).toBeVisible();
  yield 'search';
  await p.unrouteAll({ behavior: 'ignoreErrors' });
}

/**
 * The OCI remote whose upstream is this installation (seed.ts, W-24), with the server's own answers: the image `tools/agent` looked
 * up by its name, with the tags its upstream lists and the ones the cache holds. Nothing is answered in the browser.
 */
export async function* upstreamOwnWalk(p: Page): AsyncGenerator<string> {
  await expect(p.locator(`${tab} form.search`)).toBeVisible();
  yield '';
  await open(p, '?name=tools%2Fagent');
  await expect(
    p.locator(`${tab} [data-testid="upstream-lookup"] quader-upstream-package :is(tbody tr, [data-testid="upstream-package-state"], quader-error-notice)`).first(),
  ).toBeVisible();
  yield 'lookup';
}
