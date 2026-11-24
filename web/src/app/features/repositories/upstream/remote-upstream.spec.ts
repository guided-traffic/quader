import german from '../../../../locale/messages.de.xlf' with { loader: 'text' };
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
import { catalogueState, clockAfter } from './upstream-words';

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
    button(d, 'Start download').click();
    await settle(fixture);
    expect(downloads.starts.at(-1)).toEqual({ level: 'package', name: 'lodash', maxVersions: 2 });
    await waitUntil(fixture, () => !document.querySelector('.quader-dialog'), 'the preview to close');
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
        c.capsAnswer = catalogueCaps({ search: 'none', searchNote: null, searchReason: 'ghcr.io offers no search and no listing of images; look an image up by its full name.', levels: [], levelsReason: 'No listing.' });
        c.notCovered = true;
      },
    });
    const form = root.querySelector<HTMLElement>('form.search')!;
    expect(text(form)).toContain('Look up by name');
    expect(text(form)).toContain('ghcr.io offers no search and no listing of images; look an image up by its full name.');
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
        c.capsAnswer = catalogueCaps({ search: 'index', levels: ['suite', 'component', 'architecture'], searchNote: 'As apt search.' });
        c.levels[''] = { levels: ['trixie', 'bookworm'], packages: [], total: 2, nextCursor: null, done: true, withheld: null, withheldNote: null };
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
    expect([...content.root.querySelectorAll('quader-remote-tabs a.tab-link')].map((a) => text(a))).toEqual(['Packages', 'Upstream 3', 'Downloads']);
    const { root } = await page(REMOTE, { tab: 'upstream', open: 2 });
    const links = [...root.querySelectorAll<HTMLAnchorElement>('quader-remote-tabs a.tab-link')];
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
