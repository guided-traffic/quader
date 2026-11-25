import german from '../../../../locale/messages.de.xlf' with { loader: 'text' };
import { clearTranslations, loadTranslations } from '@angular/localize';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { vi } from 'vitest';
import { ApiError } from '../../../api/problem';
import { RemoteDownloadsApi } from '../../../api/remote-downloads-api';
import { RepositoriesApi } from '../../../api/repositories-api';
import { SessionApi } from '../../../api/session-api';
import { DeliveryRequestsApi } from '../../../api/delivery-requests-api';
import { DeliveryRulesApi } from '../../../api/delivery-rules-api';
import { UpstreamCatalogueApi } from '../../../api/upstream-catalogue-api';
import { FakeRepositoriesApi, PACKAGE_FORMAT } from '../../../api/testing/fakes-catalog';
import { FakeSessionApi } from '../../../api/testing/fakes';
import { FakeRemoteDownloadsApi } from '../../../api/testing/fakes-remote-downloads';
import {
  DELIVERED,
  NOT_ALLOWED,
  FakeDeliveryRequestsApi,
  FakeDeliveryRulesApi,
  FakeUpstreamCatalogueApi,
  catalogueCaps,
  deliveryRequest,
  deliveryRule,
  upstreamHit,
  upstreamPackage,
  upstreamVersion,
} from '../../../api/testing/fakes-upstream-catalogue';
import { Notifier } from '../../../core/notifier';
import { expectAccessible, renderScreen, settle, text, waitUntil } from '../../../testing/screen';
import { contentShapeOf } from '../content/content-shape';
import { RepositoryContent } from '../content/repository-content';
import { RemoteUpstream } from './remote-upstream';
import { catalogueState, clockAfter, searchHintText } from './upstream-words';

const REMOTE = { key: 'npmjs', project: 'web', class: 'remote', format: 'npm', description: '', remote: { url: 'https://registry.npmjs.org', cacheFillAction: 'read' } };
const LOCAL = { key: 'npm-local', project: 'web', class: 'local', format: 'npm', description: '' };

interface Options {
  readonly manage?: boolean;
  readonly url?: string;
  readonly format?: string;
  readonly fake?: (c: FakeUpstreamCatalogueApi, requests: FakeDeliveryRequestsApi, rules: FakeDeliveryRulesApi) => void;
}

/** The tab of `web/npmjs` as a holder of `manage` or as a reader, at the address `url`. */
async function open(options: Options = {}) {
  const r = await renderScreen(RemoteUpstream, {
    inputs: { repository: 'web/npmjs', project: 'web', format: options.format ?? 'npm', shape: contentShapeOf(options.format ?? 'npm', 'remote') },
    setup: async (get) => {
      const session = get(SessionApi) as FakeSessionApi;
      session.meResult = { capabilities: [], roles: [] };
      session.allow = (c) => c.action !== 'manage' || options.manage === true;
      options.fake?.(get(UpstreamCatalogueApi) as FakeUpstreamCatalogueApi, get(DeliveryRequestsApi) as FakeDeliveryRequestsApi, get(DeliveryRulesApi) as FakeDeliveryRulesApi);
      await get(Router).navigateByUrl(options.url ?? '/projects/web/repositories/npmjs/upstream');
    },
  });
  await settle(r.fixture);
  return {
    ...r,
    catalogue: r.get(UpstreamCatalogueApi) as FakeUpstreamCatalogueApi,
    requests: r.get(DeliveryRequestsApi) as FakeDeliveryRequestsApi,
    rules: r.get(DeliveryRulesApi) as FakeDeliveryRulesApi,
    downloads: r.get(RemoteDownloadsApi) as FakeRemoteDownloadsApi,
  };
}

function hitRow(root: HTMLElement, name: string): HTMLTableRowElement {
  return [...root.querySelectorAll<HTMLTableRowElement>('quader-data-table tbody tr:not(.expansion)')].find((tr) => tr.querySelector('.hit-name')?.textContent === name)!;
}

function versionRow(root: HTMLElement, version: string): HTMLTableRowElement {
  return [...root.querySelectorAll<HTMLTableRowElement>('quader-upstream-package tbody tr')].find((tr) => tr.querySelector('th .version')?.textContent === version)!;
}

/** The text the tooltip describes an element with (the CDK's hidden message, R-UI-044). */
function describedBy(el: HTMLElement): string {
  return (el.getAttribute('aria-describedby') ?? '')
    .split(' ')
    .map((id) => document.getElementById(id)?.textContent?.trim() ?? '')
    .join(' ')
    .trim();
}

function button(root: Element, label: string): HTMLButtonElement {
  return [...root.querySelectorAll<HTMLButtonElement>('button')].find((b) => text(b).startsWith(label))!;
}

/** The dialog over the page: the CDK renders it outside the component. */
function dialog(): HTMLElement {
  return document.querySelector<HTMLElement>('.quader-dialog')!;
}

const SEARCH_HITS = [
  upstreamHit({ name: '@gitlab/ui', description: 'GitLab UI Components', latest: '124.0.3', delivery: { delivered: true, decidedBy: { kind: 'section', subject: '@gitlab/*', effect: 'allow' } }, stored: 'partial' }),
  upstreamHit({ name: 'gitlab-ai-provider', description: 'An AI SDK provider for GitLab', latest: '1.4.0', delivery: NOT_ALLOWED }),
  upstreamHit({ name: '@gitbeaker/rest', description: 'A client of the gitlab API', latest: '43.8.0', delivery: NOT_ALLOWED, request: { id: 'req-1', askers: 2, refusals: 14 } }),
  upstreamHit({ name: 'gitlab', description: 'GitLab API Nodejs library', delivery: { delivered: false, reason: 'blocked', decidedBy: { kind: 'package', subject: 'gitlab', effect: 'block' } } }),
];

describe('The tab Upstream of a remote repository', () => {
  it('R-CUR-018 R-CUR-009 a search lists the packages with their state in this repository, under the mode and the requests set apart', async () => {
    const { root, catalogue } = await open({
      manage: true,
      url: '/projects/web/repositories/npmjs/upstream?q=gitlab',
      fake: (c, requests) => {
        c.capsAnswer = catalogueCaps({ mode: 'onlyAllowed', rules: 42, openRequests: 1, maySetRules: true });
        c.hits = SEARCH_HITS;
        requests.requests = [deliveryRequest({ askers: [{ id: 'u-2', name: 'sam', note: 'For the release tooling.', askedAt: '2026-11-24T07:00:00Z' }, { id: 'u-3', name: 'ada', note: null, askedAt: '2026-11-24T07:30:00Z' }], askerCount: 2 })];
      },
    });
    expect(catalogue.calls).toContain('search gitlab');
    const mode = root.querySelector<HTMLElement>('[data-testid="upstream-mode"]')!;
    expect(text(mode)).toContain('Delivers only what is allowed.');
    expect(text(mode)).toContain('Rules 42');
    // The requests come first, set apart, with who asked, how often clients were refused and what was examined.
    const panel = root.querySelector<HTMLElement>('[data-testid="upstream-requests"]')!;
    expect(text(panel)).toContain('Requests 1');
    expect(text(panel)).toContain('@gitbeaker/rest every version');
    expect(text(panel)).toContain('asked by 2 people');
    expect(text(panel)).toContain('refused to clients 14 times');
    expect(text(panel)).toContain('Examined 39.2.0');
    expect(panel.querySelector('a')?.getAttribute('href')).toBe('/projects/web/packages/npm/@gitbeaker/rest@39.2.0');
    expect(text(panel)).toContain('sam: For the release tooling.');
    expect(text(hitRow(root, '@gitlab/ui'))).toContain('Allowed');
    expect(text(hitRow(root, '@gitlab/ui'))).toContain('Stored');
    expect(text(hitRow(root, 'gitlab-ai-provider'))).toContain('Not allowed');
    expect(text(hitRow(root, 'gitlab-ai-provider'))).toContain('Not stored');
    expect(text(hitRow(root, '@gitbeaker/rest'))).toContain('Requested');
    expect(text(hitRow(root, 'gitlab'))).toContain('Blocked');
    await expectAccessible(root);
  });

  it('R-CUR-018 R-CUR-005 a package opens in place to its versions, and a holder of manage downloads, allows, blocks and discards', async () => {
    const { root, fixture, catalogue, downloads, rules } = await open({
      manage: true,
      url: '/projects/web/repositories/npmjs/upstream?q=gitlab',
      fake: (c) => {
        c.capsAnswer = catalogueCaps({ mode: 'onlyAllowed', maySetRules: true });
        c.hits = SEARCH_HITS;
        c.packages['gitlab-ai-provider'] = upstreamPackage({
          name: 'gitlab-ai-provider',
          delivery: NOT_ALLOWED,
          versions: [
            upstreamVersion({ version: '1.4.0', delivery: NOT_ALLOWED }),
            upstreamVersion({ version: '1.3.9', delivery: NOT_ALLOWED, stored: 'whole' }),
            upstreamVersion({ version: '1.3.8', delivery: { delivered: true, decidedBy: { kind: 'version', subject: 'gitlab-ai-provider@1.3.8', effect: 'allow' } }, size: 2_412_448 }),
          ],
          total: 3893,
          done: false,
          nextCursor: 'more',
        });
      },
    });
    hitRow(root, 'gitlab-ai-provider').querySelector<HTMLButtonElement>('button[aria-expanded]')!.click();
    await settle(fixture);
    expect(catalogue.calls).toContain('package gitlab-ai-provider');
    expect(root.querySelector('#up-gitlab-ai-provider')!.classList).toContain('open');
    expect(text(root.querySelector('quader-upstream-package')!)).toContain('Showing 3 of 3,893');
    // A version that is not stored has its vulnerability verdict under the cache; a stored one leads to its version page.
    expect(text(versionRow(root, '1.4.0').querySelector('td.cache-cell .verdict-line')!)).toBe('No known vulnerability');
    // Three columns and the acts (Q-CUR-157): the size under the version, the rule under the delivery, the verdicts under the cache.
    const heads = [...root.querySelectorAll('quader-upstream-package thead th')].map((th) => text(th));
    expect(heads).toEqual(['Version', 'Delivery', 'Cache', 'Actions']);
    expect(text(versionRow(root, '1.4.0').querySelector('th .secondary')!)).toMatch(/ · Size not told$/);
    expect(versionRow(root, '1.3.9').querySelector('td.cache-cell a')?.getAttribute('href')).toBe('/projects/web/packages/npm/gitlab-ai-provider@1.3.9');
    expect(text(versionRow(root, '1.3.9'))).toContain('Stored for examination');
    expect(text(versionRow(root, '1.3.8').querySelector('td.delivery-cell .rule-line')!)).toBe('Rule: this version');
    expect(text(versionRow(root, '1.3.8').querySelector('th .secondary')!)).toContain('2.3 MiB');
    // Several acts are the row's menu (R-UI-163).
    const menu = versionRow(root, '1.4.0').querySelector<HTMLButtonElement>('td.actions button[aria-haspopup]')!;
    expect(menu.getAttribute('aria-label')).toBe('Actions for gitlab-ai-provider 1.4.0');
    menu.click();
    await settle(fixture);
    const entries = [...document.querySelectorAll<HTMLButtonElement>('.menu .menu-item')];
    expect(entries.map((e) => text(e))).toEqual(['Download for examination', 'Allow', 'Block']);
    entries[0]!.click();
    await settle(fixture);
    expect(downloads.calls).toContain('start web/npmjs version gitlab-ai-provider 1.4.0');
    versionRow(root, '1.3.8').querySelector<HTMLButtonElement>('td.actions button[aria-haspopup]')!.click();
    await settle(fixture);
    // The version's own allow is Remove rule in the place of Allow.
    expect([...document.querySelectorAll('.menu .menu-item')].map((e) => text(e))).toEqual(['Download', 'Remove rule', 'Block']);
    button(document.querySelector('.menu')!, 'Block').click();
    await settle(fixture);
    expect(rules.calls).toContain('put version gitlab-ai-provider@1.3.8 block');
    // The package's acts: every version, future ones included.
    button(root.querySelector('quader-upstream-package')!, 'Allow every version').click();
    await settle(fixture);
    expect(rules.calls).toContain('put package gitlab-ai-provider allow');
    await expectAccessible(root);
  });

  it('R-CUR-018 R-CUR-009 an npm tag stands as a badge on the row of the version it names, and the filter finds a version by its tag', async () => {
    const { root, fixture, catalogue } = await open({
      manage: true,
      url: '/projects/web/repositories/npmjs/upstream?q=gitlab',
      fake: (c) => {
        c.hits = SEARCH_HITS;
        c.packages['gitlab-ai-provider'] = upstreamPackage({
          name: 'gitlab-ai-provider',
          delivery: NOT_ALLOWED,
          // Six tags: five on one version, one on a version past the first page.
          tags: { latest: '1.4.0', next: '2.0.0-rc.1', rc: '2.0.0-rc.1', canary: '2.0.0-rc.1', beta: '2.0.0-rc.1', 'eap-ai-hybridinference': '2.0.0-rc.1', 'lts-v0': '0.9.3' },
          versions: [upstreamVersion({ version: '2.0.0-rc.1', delivery: NOT_ALLOWED }), upstreamVersion({ version: '1.4.0', delivery: NOT_ALLOWED }), upstreamVersion({ version: '1.3.9' })],
          total: 259,
          done: false,
          nextCursor: 'more',
        });
        c.laterVersions['gitlab-ai-provider'] = [upstreamVersion({ version: '0.9.3' })];
      },
    });
    hitRow(root, 'gitlab-ai-provider').querySelector<HTMLButtonElement>('button[aria-expanded]')!.click();
    await settle(fixture);
    const pkg = root.querySelector<HTMLElement>('quader-upstream-package')!;
    // No run of every tag above the versions: each stands on its version's row, `latest` first, the others by name (Q-UI-1190).
    expect(pkg.querySelector('.package-tags')).toBeNull();
    const badges = (version: string) => [...versionRow(root, version).querySelectorAll<HTMLElement>('th .dist-tag')];
    expect(badges('1.4.0').map((b) => text(b))).toEqual(['latest']);
    expect(badges('1.3.9')).toEqual([]);
    // Five tags on one version: three badges and "+2 more", whose tooltip names the others (Q-UI-1191).
    expect(badges('2.0.0-rc.1').map((b) => text(b))).toEqual(['beta', 'canary', 'eap-ai-hybridinference']);
    const more = versionRow(root, '2.0.0-rc.1').querySelector<HTMLElement>('th .more')!;
    expect(text(more)).toBe('+2 more');
    expect(describedBy(more)).toBe('Further npm tags of this version: next, rc');
    // A badge says what it is to a pointer and to assistive technology: the tooltip describes it, and it takes the focus.
    const latest = badges('1.4.0')[0]!;
    expect(latest.getAttribute('tabindex')).toBe('0');
    expect(latest.querySelector('[translate="no"]')).not.toBeNull();
    expect(describedBy(latest)).toBe('npm tag “latest”: npm install gitlab-ai-provider@latest installs this version.');
    // The filter finds a version by its tag's name, also one past the first page (Q-CUR-182).
    expect(text(pkg.querySelector('label[for="vf-gitlab-ai-provider"]')!)).toBe('Filter versions and npm tags');
    const field = pkg.querySelector<HTMLInputElement>('#vf-gitlab-ai-provider')!;
    field.value = 'lts';
    field.dispatchEvent(new Event('input'));
    await waitUntil(fixture, () => catalogue.calls.includes('package gitlab-ai-provider lts'), 'the filter to ask the upstream');
    await settle(fixture);
    expect([...pkg.querySelectorAll('tbody th .version')].map((v) => text(v))).toEqual(['0.9.3']);
    expect(badges('0.9.3').map((b) => text(b))).toEqual(['lts-v0']);
    await expectAccessible(root);
  });

  it('R-CUR-018 the package state is said once: opened in a row only the rule the row does not name, looked up by name the badge too', async () => {
    const allowed = { delivered: true, decidedBy: { kind: 'section' as const, subject: '@gitlab/*', effect: 'allow' as const } };
    const { root, fixture } = await open({
      manage: true,
      url: '/projects/web/repositories/npmjs/upstream?q=gitlab',
      fake: (c) => {
        c.hits = SEARCH_HITS;
        c.packages['@gitlab/ui'] = upstreamPackage({ name: '@gitlab/ui', delivery: allowed, tags: {} });
        c.packages['gitlab-ai-provider'] = upstreamPackage({ name: 'gitlab-ai-provider', delivery: NOT_ALLOWED, tags: {} });
      },
    });
    for (const name of ['@gitlab/ui', 'gitlab-ai-provider']) hitRow(root, name).querySelector<HTMLButtonElement>('button[aria-expanded]')!.click();
    await settle(fixture);
    const [ui, provider] = [...root.querySelectorAll<HTMLElement>('quader-upstream-package')];
    // The row's column State shows "Allowed" and "Not allowed": the opened part repeats neither badge.
    expect(text(hitRow(root, '@gitlab/ui'))).toContain('Allowed');
    expect(ui!.querySelector('[data-testid="upstream-package-badge"]')).toBeNull();
    expect(text(ui!.querySelector('[data-testid="upstream-package-rule"]')!)).toBe('Rule: @gitlab/*');
    expect(provider!.querySelector('.package-state')).toBeNull();
    // Without tags the field is the versions' filter alone.
    expect(text(provider!.querySelector('label.field-label')!)).toBe('Filter versions');
    // A lookup by name has no row above it: the badge and the rule stand in its head.
    const lookup = await open({ manage: true, url: '/projects/web/repositories/npmjs/upstream?name=lodash', fake: (c) => (c.packages['lodash'] = upstreamPackage({ delivery: allowed })) });
    const head = lookup.root.querySelector<HTMLElement>('[data-testid="upstream-lookup"] [data-testid="upstream-package-badge"]')!;
    expect(text(head.querySelector('.badge')!)).toContain('Allowed');
    expect(text(head.querySelector('.muted')!)).toBe('Rule: @gitlab/*');
  });

  it('R-CUR-010 Download every version opens the preview of the bulk; a lowered bound is previewed again and the bulk started with it', async () => {
    const { root, fixture, downloads } = await open({ manage: true, url: '/projects/web/repositories/npmjs/upstream?name=lodash' });
    button(root.querySelector('quader-upstream-package')!, 'Download every version').click();
    await settle(fixture);
    const d = dialog();
    expect(text(d)).toContain('Every version of lodash');
    expect(text(d)).toContain('Size of 3 versions not told by the upstream; the bound of 100.0 GiB stops the download when it is reached.');
    expect(downloads.previews.at(-1)).toEqual({ level: 'package', name: 'lodash' });
    const versions = d.querySelector<HTMLInputElement>('#preview-max-versions')!;
    versions.value = '2';
    versions.dispatchEvent(new Event('change'));
    await settle(fixture);
    expect(downloads.previews.at(-1)).toEqual({ level: 'package', name: 'lodash', maxVersions: 2 });
    const status = vi.spyOn(TestBed.inject(Notifier), 'status');
    button(d, 'Start download').click();
    await settle(fixture);
    expect(downloads.starts.at(-1)).toEqual({ level: 'package', name: 'lodash', maxVersions: 2 });
    await waitUntil(fixture, () => !document.querySelector('.quader-dialog'), 'the preview to close');
    // The bulk has no row yet: the notice says that it reads the versions first and leads to the bulk itself (Q-CUR-168).
    await waitUntil(fixture, () => status.mock.calls.length > 0, 'the notice of the start');
    const [words, action] = status.mock.calls.at(-1)!;
    expect(words).toBe('The bulk download has started; Quader reads its versions first, then queues them.');
    expect(action?.label).toBe('Show bulk download');
    action!.run();
    await settle(fixture);
    expect(TestBed.inject(Router).url).toBe(`/projects/web/repositories/npmjs/downloads?bulk=${downloads.bulks.at(-1)!.id}`);
  });

  it('R-CUR-018 R-CUR-009 a reader in mode onlyAllowed reads the withheld hits, its own requests, and requests a version; no rules, no bulk', async () => {
    const { root, fixture, requests } = await open({
      manage: false,
      url: '/projects/web/repositories/npmjs/upstream?q=gitlab',
      fake: (c, reqs) => {
        c.capsAnswer = catalogueCaps({ mode: 'onlyAllowed', maySetRules: false, openRequests: null });
        c.hits = [SEARCH_HITS[0]!];
        c.withheld = 14;
        c.packages['@gitlab/ui'] = upstreamPackage({ name: '@gitlab/ui', delivery: NOT_ALLOWED, versions: [upstreamVersion({ version: '124.0.3', delivery: NOT_ALLOWED })] });
        reqs.requests = [
          deliveryRequest({ id: 'mine-1', name: 'axios', version: '0.21.1', askers: [{ id: '00000000-0000-0000-0000-000000000001', name: 'Alice', note: null, askedAt: '2026-11-24T07:00:00Z' }], refusals: 0, examined: [] }),
          deliveryRequest({ id: 'mine-2', name: 'left-pad', state: 'declined', declineReason: 'Use padStart.', refusals: 0, examined: [] }),
        ];
      },
    });
    expect(text(root.querySelector('[data-testid="upstream-withheld"]')!)).toBe(
      '14 hits are not allowed in this repository; look a package up by its exact name to ask for it.',
    );
    expect(root.querySelector('[data-testid="upstream-rules"]')).toBeNull();
    expect(requests.listCalls.at(-1)).toEqual({ mine: true });
    const panel = root.querySelector<HTMLElement>('[data-testid="upstream-requests"]')!;
    expect(text(panel)).toContain('Your requests 2');
    expect(text(panel)).toContain('axios 0.21.1 Open');
    expect(text(panel)).toContain('Declined: Use padStart.');
    expect(button(panel, 'Withdraw')).toBeDefined();
    hitRow(root, '@gitlab/ui').querySelector<HTMLButtonElement>('button[aria-expanded]')!.click();
    await settle(fixture);
    const pkg = root.querySelector<HTMLElement>('quader-upstream-package')!;
    expect(pkg.querySelector('[data-testid="upstream-download-every"]')).toBeNull();
    expect(button(pkg, 'Request every version')).toBeDefined();
    const one = versionRow(root, '124.0.3').querySelector<HTMLButtonElement>('td.actions button')!;
    expect(text(one)).toBe('Request @gitlab/ui 124.0.3');
    one.click();
    await settle(fixture);
    const d = dialog();
    expect(text(d)).toContain('Request @gitlab/ui 124.0.3');
    const note = d.querySelector<HTMLTextAreaElement>('textarea')!;
    note.value = 'For the design system.';
    note.dispatchEvent(new Event('input'));
    button(d, 'Send request').click();
    await settle(fixture);
    expect(requests.calls).toContain('ask @gitlab/ui 124.0.3 "For the design system."');
    await expectAccessible(root);
  });

  it('R-CUR-007 R-CUR-018 an upstream without a search offers the lookup by name and says why; the refusal link ?name= opens the package', async () => {
    const { root, catalogue } = await open({
      format: 'oci',
      url: '/projects/web/repositories/npmjs/upstream?name=lodash',
      fake: (c) => {
        c.capsAnswer = catalogueCaps({ search: 'none', searchHint: 'ghcr', levels: [], levelsReason: 'No listing.' });
        c.notCovered = true;
      },
    });
    const form = root.querySelector<HTMLElement>('form.search')!;
    expect(text(form)).toContain('Look up by name');
    expect(text(form.querySelector('[data-testid="upstream-search-hint"]')!)).toBe('ghcr.io offers no search; enter an image\'s full name, for example stefanprodan/podinfo.');
    expect(button(form, 'Look up')).toBeDefined();
    expect([...form.querySelectorAll('button')].some((b) => text(b) === 'Search')).toBe(false);
    expect(catalogue.calls.some((c) => c.startsWith('search'))).toBe(false);
    const lookup = root.querySelector<HTMLElement>('[data-testid="upstream-lookup"]')!;
    expect(text(lookup.querySelector('h2')!)).toBe('lodash');
    expect(text(root.querySelector('[data-testid="upstream-not-covered"]')!)).toBe('An image is analysed for vulnerabilities once its platforms are stored.');
    await expectAccessible(root);
  });

  it('R-CUR-018 the catalogue states its own refusals in words: the upstream offline until a time, the budget used up, a document too large', async () => {
    const offline = await open({ fake: (c) => (c.capsError = new ApiError(503, { code: 'upstream_assumed_offline', detail: 'x' }, 'rid', 600)) });
    expect(text(offline.root.querySelector('[data-testid="upstream-state"]')!)).toContain(`The upstream is assumed offline until ${clockAfter(600)}; Quader retries then.`);
    expect(offline.root.querySelector('quader-error-notice')).toBeNull();
    const budget = new ApiError(429, { code: 'catalogue_budget_exhausted', detail: 'x' }, 'rid', 40);
    expect(catalogueState(budget)).toBe("The upstream catalogue's budget is used up; try again in 40 s.");
    const big = new ApiError(502, { code: 'catalogue_answer_too_large', detail: 'x' }, 'rid');
    expect(catalogueState(big, 'package')).toBe("This package's document is larger than the catalogue reads; Quader does not read it for browsing.");
    const { root } = await open({ url: '/projects/web/repositories/npmjs/upstream?q=gitlab', fake: (c) => (c.searchError = budget) });
    expect(text(root.querySelector('[data-testid="upstream-list-state"]')!)).toContain("The upstream catalogue's budget is used up; try again in 40 s.");
  });

  it('R-CUR-018 a search without a hit says so, and a level of the upstream lists its children', async () => {
    const empty = await open({ url: '/projects/web/repositories/npmjs/upstream?q=zzqx' });
    expect(text(empty.root)).toContain('No package of the upstream matches “zzqx”.');
    const { root } = await open({
      format: 'deb',
      fake: (c) => {
        c.capsAnswer = catalogueCaps({ search: 'index', levels: ['suite', 'component', 'architecture'], searchHint: 'aptIndex' });
        c.levels[''] = { levels: ['trixie', 'bookworm'], packages: [], total: 2, nextCursor: null, done: true, withheld: null, withheldNote: null, unknown: null };
      },
    });
    const bar = root.querySelector<HTMLElement>('[data-testid="upstream-levels"]')!;
    expect(text(bar)).toContain('Suite');
    expect([...bar.querySelectorAll('.level-children a')].map((a) => a.getAttribute('href'))).toEqual([
      '/projects/web/repositories/npmjs/upstream?level=trixie',
      '/projects/web/repositories/npmjs/upstream?level=bookworm',
    ]);
    // The index search of Debian has Names only.
    expect(text(root.querySelector('form.search')!)).toContain('Names only');
  });

  it('R-CUR-018 R-CUR-007 a level the upstream does not have says so, with the rule its name breaks and the level it may have meant', async () => {
    const { root } = await open({
      format: 'oci',
      url: '/projects/web/repositories/npmjs/upstream?level=guided-traffic',
      fake: (c) => {
        c.capsAnswer = catalogueCaps({ search: 'upstream', levels: ['namespace'], searchHint: 'dockerHub' });
        c.levels['guided-traffic'] = {
          levels: [], packages: [], total: 0, nextCursor: null, done: true, withheld: null, withheldNote: null,
          unknown: { upstream: 'Docker Hub', rule: 'lowercaseDigits', forbidden: '-', suggestion: 'guidedtraffic' },
        };
      },
    });
    const empty = root.querySelector<HTMLElement>('quader-empty-state')!;
    expect(text(empty)).toContain('Docker Hub has no namespace “guided-traffic”. Docker Hub allows only lowercase letters and digits in this name, no “-”.');
    expect(text(empty)).not.toContain('This level lists no package.');
    const link = empty.querySelector<HTMLAnchorElement>('[data-testid="upstream-suggestion"]')!;
    expect(text(link)).toBe('Did you mean “guidedtraffic”?');
    expect(link.getAttribute('href')).toBe('/projects/web/repositories/npmjs/upstream?level=guidedtraffic');
    await expectAccessible(root);

    const plain = await open({ format: 'oci', url: '/projects/web/repositories/npmjs/upstream?level=guidedtraffik', fake: (c) => {
      c.capsAnswer = catalogueCaps({ search: 'upstream', levels: ['namespace'], searchHint: 'dockerHub' });
      c.levels['guidedtraffik'] = {
        levels: [], packages: [], total: 0, nextCursor: null, done: true, withheld: null, withheldNote: null,
        unknown: { upstream: 'Docker Hub', rule: null, forbidden: null, suggestion: null },
      };
    } });
    expect(text(plain.root.querySelector('quader-empty-state')!)).toBe('Docker Hub has no namespace “guidedtraffik”.');
    expect(plain.root.querySelector('[data-testid="upstream-suggestion"]')).toBeNull();
  });

  it('R-CUR-015 a holder of manage allows a request as its package with every version, and declines another with a reason', async () => {
    const { root, fixture, requests } = await open({
      manage: true,
      fake: (_c, reqs) => {
        reqs.requests = [deliveryRequest({ id: 'r-a', name: 'axios', version: '0.21.1' }), deliveryRequest({ id: 'r-b', name: 'left-pad' })];
      },
    });
    const status = vi.spyOn(TestBed.inject(Notifier), 'status');
    const items = () => [...root.querySelectorAll<HTMLElement>('[data-testid="upstream-requests"] li')];
    items()[0]!.querySelector<HTMLButtonElement>('button[aria-haspopup]')!.click();
    await settle(fixture);
    button(document.querySelector('.menu')!, 'Allow').click();
    await settle(fixture);
    let d = dialog();
    d.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1]!.click();
    d.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click();
    button(d, 'Allow').click();
    await settle(fixture);
    expect(requests.calls).toContain('allow r-a package every');
    expect(status).toHaveBeenCalledWith('axios 0.21.1 is allowed; its download starts.');
    await waitUntil(fixture, () => !document.querySelector('.quader-dialog'), 'the allow dialog to close');
    const rest = items().find((li) => text(li).includes('left-pad'))!;
    rest.querySelector<HTMLButtonElement>('button[aria-haspopup]')!.click();
    await settle(fixture);
    button(document.querySelector('.menu')!, 'Decline').click();
    await settle(fixture);
    d = dialog();
    const reason = d.querySelector<HTMLTextAreaElement>('textarea')!;
    reason.value = 'Unmaintained.';
    reason.dispatchEvent(new Event('input'));
    button(d, 'Decline').click();
    await settle(fixture);
    expect(requests.calls).toContain('decline r-b "Unmaintained." true');
  });

  it('R-CUR-006 R-CUR-018 the rule list adds a sub-section rule and removes a rule, with the sentence of the decision', async () => {
    const { root, fixture, rules } = await open({
      manage: true,
      fake: (c, _r, rs) => {
        c.capsAnswer = catalogueCaps({ rules: 1 });
        rs.rules = [deliveryRule({ id: 'rule-1', kind: 'package', subject: 'gitlab', effect: 'block', note: 'Unmaintained.' })];
      },
    });
    root.querySelector<HTMLButtonElement>('[data-testid="upstream-rules"]')!.click();
    await settle(fixture);
    const d = dialog();
    expect(text(d)).toContain('The narrowest rule decides; at equal width a block wins.');
    expect(text(d)).toContain('Unmaintained.');
    expect(text(d)).toContain('A name pattern, such as @acme/*.');
    const pattern = d.querySelector<HTMLInputElement>('input[name="pattern"]')!;
    pattern.value = '@acme/*';
    pattern.dispatchEvent(new Event('input'));
    await settle(fixture);
    button(d, 'Allow').click();
    button(d, 'Add rule').click();
    await settle(fixture);
    expect(rules.calls).toContain('put section @acme/* allow');
    const row = [...d.querySelectorAll<HTMLTableRowElement>('tbody tr')].find((tr) => text(tr).includes('gitlab'))!;
    button(row, 'Remove').click();
    await settle(fixture);
    expect(rules.calls).toContain('remove rule-1');
    await expectAccessible(d);
  });

  it('R-CUR-018 R-CUR-007 the hint under the search field is the German sentence for its code in the German build', () => {
    const xml = (s: string): string =>
      s.replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&apos;/g, "'").replace(/&amp;/g, '&');
    const unit = /<unit id="(upstream\.hint\.[^"]+)">[\s\S]*?<target>([\s\S]*?)<\/target>/g;
    const translations = Object.fromEntries([...german.matchAll(unit)].map((m) => [m[1]!, xml(m[2]!)]));
    expect(Object.keys(translations)).toHaveLength(14);
    loadTranslations(translations);
    try {
      expect(searchHintText('dockerHub')).toBe('Suchen Sie nach einem Image-Namen, z. B. nginx; offizielle Images liegen unter library/<Name>.');
      expect(searchHintText('npmRegistry')).toBe(
        'Suchen Sie nach einem Paketnamen, einem Schlüsselwort oder einem Maintainer, z. B. express; eine Suche umfasst 2 bis 64 Zeichen.',
      );
      expect(searchHintText('ghcr')).toBe('ghcr.io bietet keine Suche; geben Sie den vollständigen Namen eines Images ein, z. B. stefanprodan/podinfo.');
      expect(searchHintText(null)).toBeNull();
    } finally {
      clearTranslations();
    }
    expect(searchHintText('dockerHub')).toBe('Search for an image name, for example nginx; official images are under library/<name>.');
  });

  it('R-CUR-018 the German texts follow the glossary: erlauben, sperren, beantragen, ablehnen, verwerfen, In den Cache laden, the two modes', () => {
    const target = (id: string): string => new RegExp(`<unit id="${id.replace(/\./g, '\\.')}">[\\s\\S]*?<target>([\\s\\S]*?)</target>`).exec(german)?.[1] ?? '';
    expect(target('upstream.state.allowed')).toBe('Erlaubt');
    expect(target('upstream.state.blocked')).toBe('Gesperrt');
    expect(target('upstream.act.allow')).toBe('Erlauben');
    expect(target('upstream.act.block')).toBe('Sperren');
    expect(target('upstream.act.request')).toBe('Beantragen');
    expect(target('upstream.act.discard')).toBe('Verwerfen');
    expect(target('upstream.act.download')).toBe('In den Cache laden');
    expect(target('upstream.mode.only-allowed.name')).toBe('Nur Erlaubtes');
    expect(target('upstream.mode.all-except-blocked.name')).toBe('Alles außer Gesperrtem');
    expect(target('requests.decline')).toBe('Ablehnen …');
    // A level the upstream does not have (Q-CUR-165): "Docker Hub kennt keinen Namespace „guided-traffic“."
    const ph = (id: number, name: string): string => `<ph id="${id}" equiv="${name}" disp="${name}"/>`;
    expect(target('upstream.unknown.namespace')).toBe(`${ph(0, 'upstream')} kennt keinen Namespace „${ph(1, 'name')}“.`);
    expect(target('upstream.rule.lowercase-digits')).toBe(`${ph(0, 'upstream')} erlaubt in diesem Namen nur Kleinbuchstaben und Ziffern, kein ${ph(1, 'chars')}.`);
    expect(target('upstream.rule.char')).toBe(`„${ph(0, 'char')}“`);
    expect(target('upstream.unknown.suggestion')).toBe(`Meinten Sie „${ph(0, 'name')}“?`);
  });
});

describe('The tab Upstream reads itself again (R-UI-213)', () => {
  it('R-UI-213 an open package is read every 3 s while one of its versions downloads, every minute after, in place', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const queued = { id: '00000000-0000-0000-0000-0000000000a1', state: 'running' as const, origin: 'person', attempts: 1, maxAttempts: 3, nextAttemptAt: null };
      const { root, fixture, catalogue } = await open({
        manage: true,
        url: '/projects/web/repositories/npmjs/upstream?name=lodash',
        fake: (c) => {
          c.packages['lodash'] = upstreamPackage({ versions: [upstreamVersion({ version: '4.17.21', queued }), upstreamVersion({ version: '4.17.20' })] });
        },
      });
      const reads = () => catalogue.calls.filter((c) => c === 'package lodash').length;
      const row = versionRow(root, '4.17.21');
      expect(text(row)).toContain('Running');
      const first = reads();
      vi.advanceTimersByTime(3_000);
      await settle(fixture);
      expect(reads()).toBe(first + 1);
      // The download ended: the cache holds the version, and the row says so in place.
      catalogue.packages['lodash'] = upstreamPackage({ versions: [upstreamVersion({ version: '4.17.21', stored: 'whole' }), upstreamVersion({ version: '4.17.20' })] });
      vi.advanceTimersByTime(3_000);
      await settle(fixture);
      expect(reads()).toBe(first + 2);
      expect(versionRow(root, '4.17.21')).toBe(row);
      expect(text(row)).not.toContain('Running');
      vi.advanceTimersByTime(3_000);
      await settle(fixture);
      expect(reads()).toBe(first + 2);
      vi.advanceTimersByTime(57_000);
      await settle(fixture);
      expect(reads()).toBe(first + 3);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('The strip of a remote repository with the tab Upstream', () => {
  async function page(repo: object, options: { tab?: 'content' | 'upstream'; open?: number } = {}) {
    const r = await renderScreen(RepositoryContent, {
      inputs: { project: 'web', key: (repo as { key: string }).key, ...(options.tab ? { tab: options.tab } : {}) },
      setup: (get) => {
        const catalog = get(RepositoriesApi) as FakeRepositoriesApi;
        catalog.repositories = [...catalog.repositories, repo as never];
        catalog.formatsResult = [PACKAGE_FORMAT];
        const reqs = get(DeliveryRequestsApi) as FakeDeliveryRequestsApi;
        reqs.requests = Array.from({ length: options.open ?? 0 }, (_, i) => deliveryRequest({ id: `r-${i}`, name: `p-${i}` }));
        (get(UpstreamCatalogueApi) as FakeUpstreamCatalogueApi).capsAnswer = catalogueCaps({ openRequests: options.open ?? 0, maySetRules: true });
      },
    });
    await settle(r.fixture);
    return r;
  }

  it('R-CUR-018 Upstream carries the count of open requests for those who may answer them, and the tab shows the catalogue', async () => {
    const content = await page(REMOTE, { open: 3 });
    expect([...content.root.querySelectorAll('quader-repository-tabs a.tab-link')].map((a) => text(a))).toEqual(['Packages', 'Upstream 3', 'Downloads', 'Details']);
    const { root } = await page(REMOTE, { tab: 'upstream', open: 2 });
    const links = [...root.querySelectorAll<HTMLAnchorElement>('quader-repository-tabs a.tab-link')];
    expect(links[1]!.getAttribute('aria-current')).toBe('page');
    expect(text(links[1]!)).toBe('Upstream 2');
    expect(root.querySelector('quader-remote-upstream')).not.toBeNull();
    expect(root.querySelector('quader-repository-items')).toBeNull();
    expect(DELIVERED.delivered).toBe(true);
  });

  it('R-CUR-018 the address of the tab Upstream of a local repository leads to its content', async () => {
    const { root, get } = await page(LOCAL, { tab: 'upstream' });
    expect(root.querySelector('quader-remote-upstream')).toBeNull();
    expect(get(Router).url).toBe('/projects/web/repositories/npm-local');
  });
});
