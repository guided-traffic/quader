import { Component } from '@angular/core';
import { Router } from '@angular/router';
import { LimitsApi } from '../../api/limits-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { FakeRepositoriesApi } from '../../api/testing/fakes-catalog';
import { FakeLimitsApi, sampleHistory } from '../../api/testing/fakes-limits';
import { limitNotice, waitText } from '../../core/http/problem-messages-limits';
import { Notifier } from '../../core/notifier';
import { DownloadLink, probeDownload } from '../../shared/download-link';
import { seriousViolations } from '../../testing/axe';
import { buttonByText, renderScreen, settle, sleep, text, type } from '../../testing/screen';
import { LimitExplain, stepText } from './limit-explain';
import { ProfileUsage } from './profile-usage';
import { accountLink } from './usage-detail';
import { UsageDetailPage } from './usage-detail';
import { historyChart } from './usage-history';
import { UsageOverview } from './usage-overview';
import { stateWord } from './usage-words';

const rowOf = (root: ParentNode, name: string): Element => {
  const row = [...root.querySelectorAll('tbody tr')].find((r) => text(r).includes(name));
  if (!row) throw new Error(`no row ${name}`);
  return row;
};

describe('The usage overview (R-UI-183)', () => {
  it('R-UI-183 lists every caller with its figures and the meter now; Audit says would have been limited; passes axe', async () => {
    const { root } = await renderScreen(UsageOverview, { capabilities: ['usage:read'] });
    expect(text(root.querySelector('h1')!)).toBe('Usage');
    expect(text(root)).toContain('Mode: Audit. Limits are counted, not applied.');
    const alice = rowOf(root, 'Alice Example');
    expect(text(alice)).toContain('48,210');
    expect(text(alice)).toContain('1.2 GB');
    expect(text(alice)).toContain('82%');
    expect(text(alice)).toContain('Close to the limit');
    const v6 = rowOf(root, '2001:db8:1:2::/64');
    expect(text(v6)).toContain('Would have been limited');
    expect(text(v6)).not.toContain('Limit reached');
    expect(v6.querySelector('[role="meter"]')!.getAttribute('aria-valuenow')).toBe('100');
    expect(text(rowOf(root, 'Ada Admin'))).toContain('Unlimited');
    expect(text(rowOf(root, '198.51.100.23'))).toContain('Blocked');
    expect(text(root)).toContain('Would have been limited');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('Q-UI-732 the bar is never green and the state comes with its word', async () => {
    const { root } = await renderScreen(UsageOverview, { capabilities: ['usage:read'] });
    expect(rowOf(root, 'Alice Example').querySelector('.fill.warn')).toBeTruthy();
    expect(rowOf(root, '2001:db8:1:2::/64').querySelector('.fill.bad')).toBeTruthy();
    expect(stateWord('limitReached', 'enforce')).toBe('Limit reached');
    expect(stateWord('limitReached', 'audit')).toBe('Would have been limited');
    expect(stateWord('normal', 'enforce')).toBeNull();
  });

  it('R-UI-183 reads its filters from the address and sends them; a sort is a request', async () => {
    const { fixture, root, get } = await renderScreen(UsageOverview, {
      capabilities: ['usage:read'],
      inputs: { kind: 'anonymous,user', state: 'wouldBeLimited', period: 'hour', sort: 'cachedBytes', order: 'desc' },
    });
    const api = get(LimitsApi) as FakeLimitsApi;
    expect(api.usageQueries.at(-1)!.query).toEqual({
      period: 'hour',
      kind: ['anonymous', 'user'],
      state: 'wouldBeLimited',
      q: undefined,
      sort: 'cachedBytes',
      order: 'desc',
    });
    expect(root.querySelector('th[aria-sort="descending"]')!.textContent).toContain('From the cache');
    const navigate = vi.spyOn(get(Router), 'navigate').mockResolvedValue(true);
    buttonByText(root, 'Last 30 days').click();
    await settle(fixture);
    expect(navigate.mock.calls.at(-1)![1]).toMatchObject({ queryParams: { period: '30d' }, queryParamsHandling: 'merge' });
    expect(root.querySelector<HTMLAnchorElement>('a[download]')!.getAttribute('href')).toBe(
      '/api/v1/usage/export?period=hour&kind=anonymous&kind=user&state=wouldBeLimited&sort=cachedBytes&order=desc',
    );
  });

  it('R-UI-183 says what is empty: no downloads, no match, nobody in the project', async () => {
    const empty = await renderScreen(UsageOverview, {
      capabilities: ['usage:read'],
      setup: (get) => ((get(LimitsApi) as FakeLimitsApi).usageRows = []),
    });
    expect(text(empty.root)).toContain('No downloads in the last 24 hours.');
    const filtered = await renderScreen(UsageOverview, {
      capabilities: ['usage:read'],
      inputs: { q: 'nobody' },
    });
    expect(text(filtered.root)).toContain('No caller matches.');
    const project = await renderScreen(UsageOverview, {
      inputs: { scope: 'project', project: 'web' },
      setup: (get) => ((get(LimitsApi) as FakeLimitsApi).usageRows = []),
    });
    expect(text(project.root)).toContain('Nobody downloaded from this project in the last 24 hours.');
  });

  it('R-UI-183 in a project the title names the repository and a row opens the caller in the project', async () => {
    const { root, get } = await renderScreen(UsageOverview, { inputs: { scope: 'project', project: 'web', repository: 'npm-remote' } });
    expect(text(root.querySelector('h1')!)).toBe('Usage of npm-remote');
    expect((get(LimitsApi) as FakeLimitsApi).usageQueries.at(-1)!.scope).toEqual({ project: 'web', repository: 'npm-remote' });
    expect(rowOf(root, 'Alice Example').querySelector('a')!.getAttribute('href')).toBe('/projects/web/usage/user/alice');
    expect(rowOf(root, '2001:db8:1:2::/64').querySelector('a')!.getAttribute('href')).toBe('/projects/web/usage/anonymous/2001:db8:1:2::%2F64');
  });

  it('R-UI-183 in Enforce a state reads Limit reached', async () => {
    const { root } = await renderScreen(UsageOverview, {
      capabilities: ['usage:read'],
      setup: (get) => {
        const api = get(LimitsApi) as FakeLimitsApi;
        api.settingsValue = { ...api.settingsValue, mode: 'enforce' };
        api.usageRows = api.usageRows.map((r) => (r.state === 'wouldBeLimited' ? { ...r, state: 'limitReached' } : r));
      },
    });
    expect(text(rowOf(root, '2001:db8:1:2::/64'))).toContain('Limit reached');
    expect(text(root)).not.toContain('Limits are counted, not applied.');
  });
});

describe("A caller's usage (R-UI-184) and a user's own (R-UI-185)", () => {
  it('R-UI-184 shows the limits per budget key with meters, the rule and why, and the rates as text; passes axe', async () => {
    const { root, get } = await renderScreen(UsageDetailPage, { capabilities: ['usage:read', 'limits:read'], inputs: { kind: 'user', id: 'alice' } });
    // Q-UI-765: the title names the caller, the reference stands under it.
    expect(text(root.querySelector('h1')!)).toBe('Alice Example');
    expect(text(root.querySelector('.reference')!)).toBe('@alice');
    expect(text(root)).toContain('Applies: the default for every signed-in user at the instance, profile Standard.');
    expect(text(root)).toContain('Applies: the rule for the group contractors at web/npm-remote, profile Restricted.');
    expect(text(root)).toContain('246 GB of 300 GB');
    expect(text(root)).toContain('200 per second, 1,000 burst');
    expect(text(root)).toContain('Would have been limited');
    expect((get(LimitsApi) as FakeLimitsApi).calls).toContain('subjectUsage instance user/alice day');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-184 By credential lists three rows whose sum equals the totals', async () => {
    const { root, get } = await renderScreen(UsageDetailPage, { capabilities: ['usage:read'], inputs: { kind: 'user', id: 'alice' } });
    const section = [...root.querySelectorAll('section')].find((s) => text(s).startsWith('By credential'))!;
    const rows = [...section.querySelectorAll('tbody tr')];
    expect(rows.length).toBe(3);
    expect(text(rows[0]!)).toContain('Browser session');
    expect(text(rows[1]!)).toContain('laptop npm');
    expect(text(rows[1]!)).toContain('qdr_pat_3kQ9');
    const d = (get(LimitsApi) as FakeLimitsApi).detailValue;
    const sum = (k: 'requests' | 'cachedBytes' | 'uncachedBytes'): number => d.credentials!.reduce((a, c) => a + c[k], 0);
    expect([sum('requests'), sum('cachedBytes'), sum('uncachedBytes')]).toEqual([d.totals.requests, d.totals.cachedBytes, d.totals.uncachedBytes]);
  });

  it('R-UI-184 Explain for a repository lists the steps of the first worked example', async () => {
    const { fixture, root, get } = await renderScreen(UsageDetailPage, { capabilities: ['usage:read', 'limits:read'], inputs: { kind: 'user', id: 'alice' } });
    buttonByText(root, 'Explain for a repository').click();
    await settle(fixture);
    type(root.querySelector<HTMLInputElement>('#explain-repository')!, 'web/npm-remote');
    await settle(fixture);
    root.querySelector<HTMLButtonElement>('quader-limit-explain button[type="submit"]')!.click();
    await sleep(fixture, 20);
    expect((get(LimitsApi) as FakeLimitsApi).calls).toContain('explain user/alice web/npm-remote');
    const steps = [...root.querySelectorAll('ol.steps li')].map((li) => text(li));
    expect(steps).toEqual(['web/npm-remote: no rule for Alice Example.', 'web/npm-remote: the rule for contractors applies.']);
    expect(text(root)).toContain('50 GB per hour from the upstream, upstream requests per hour: 2,000');
  });

  it('R-UI-193 Q-UI-901 Explain for a repository suggests no system repository of upstream, whose limits apply to no caller', async () => {
    const { fixture, root } = await renderScreen(LimitExplain, {
      setup: (get) => {
        (get(RepositoriesApi) as FakeRepositoriesApi).repositories = [
          { key: 'npm-remote', project: 'web', class: 'remote', format: 'npm' },
          { key: 'scanner-trivy', project: 'upstream', class: 'remote', format: 'oci' },
        ];
      },
    });
    buttonByText(root, 'Explain for a repository').click();
    await settle(fixture);
    expect([...root.querySelectorAll('#explain-repositories option')].map((o) => o.getAttribute('value'))).toEqual(['web/npm-remote']);
  });

  it('R-UI-184 R-LIM-027 a certificate subject lists its certificates by serial under By credential, a renewed one beside the one it replaced', async () => {
    const subject = 'CN=clamav-scan-01.example.com,O=Example GmbH';
    const { root } = await renderScreen(UsageDetailPage, {
      capabilities: ['usage:read'],
      inputs: { kind: 'certificate', id: subject },
      setup: (get) => {
        const api = get(LimitsApi) as FakeLimitsApi;
        api.detailValue = {
          ...api.detailValue,
          subject: { kind: 'certificate', id: subject, name: subject },
          credentials: [
            { credential: { kind: 'certificate', serial: '5c2f9e0b71a4d3e8' }, requests: 96, cachedBytes: 2_300_000_000, uncachedBytes: 0 },
            { credential: { kind: 'certificate', serial: '0a7d33c19f6e2b45' }, requests: 12, cachedBytes: 110_000_000, uncachedBytes: 0 },
          ],
        };
      },
    });
    // Under the title the kind's badge alone: the badge says Client certificate, and the certificates stand under By credential.
    expect(text(root.querySelector('.badge')!)).toBe('Client certificate');
    expect(root.querySelector('.reference')).toBeNull();
    const section = [...root.querySelectorAll('section')].find((s) => text(s).startsWith('By credential'))!;
    const rows = [...section.querySelectorAll('tbody tr th')].map((th) => [...th.querySelectorAll('.token > *')].map((e) => text(e)));
    expect(rows).toEqual([
      ['Client certificate', '5c2f9e0b71a4d3e8'],
      ['Client certificate', '0a7d33c19f6e2b45'],
    ]);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-184 a blocked caller shows the block and Lift block for an administrator', async () => {
    const { root } = await renderScreen(UsageDetailPage, {
      capabilities: ['usage:read', 'limits:read', 'limits:write'],
      inputs: { kind: 'anonymous', id: '198.51.100.23' },
      setup: (get) => {
        const api = get(LimitsApi) as FakeLimitsApi;
        api.detailValue = {
          ...api.detailValue,
          subject: { kind: 'anonymous', id: '198.51.100.23', address: '198.51.100.23' },
          budgets: api.detailValue.budgets.map((b, i) => (i === 0 ? { ...b, blockedUntil: api.blockList[0]!.until } : b)),
        };
      },
    });
    expect(text(root)).toMatch(/Blocked until .+ after 61 refused requests in an hour/);
    expect(buttonByText(root, 'Lift block')).toBeTruthy();
    expect(root.querySelector('section:has(#credentials-title)')).toBeNull();
  });

  it('R-UI-185 the profile shows the own limits as "Your limits come from …" and the Usage tab', async () => {
    const { root } = await renderScreen(ProfileUsage, { capabilities: [] });
    expect(text(root)).toContain('Your limits come from the default for every signed-in user at the instance, profile Standard.');
    expect(text(root)).toContain('Limits are counted, not applied yet.');
    expect(root.querySelector('a[href="/profile/usage"][aria-current="page"]') ?? root.querySelector('a[href="/profile/usage"]')).toBeTruthy();
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-185 says so where nothing was downloaded in the last 24 hours', async () => {
    const { root } = await renderScreen(ProfileUsage, {
      setup: (get) => {
        const api = get(LimitsApi) as FakeLimitsApi;
        api.detailValue = { ...api.detailValue, history: sampleHistory().map((p) => ({ ...p, requests: 0, cachedBytes: 0, uncachedBytes: 0 })), repositories: [], credentials: [] };
      },
    });
    expect(text(root)).toContain('You have not downloaded anything in the last 24 hours.');
  });

  it('Q-UI-754 the history is three charts of one axis each, with a read-out by key and a table', async () => {
    const { fixture, root } = await renderScreen(UsageDetailPage, { capabilities: ['usage:read'], inputs: { kind: 'user', id: 'alice' } });
    expect(root.querySelectorAll('quader-usage-history svg').length).toBe(3);
    const charts = root.querySelector<HTMLElement>('quader-usage-history .charts')!;
    charts.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true }));
    await settle(fixture);
    expect(text(root.querySelector('#history-readout')!)).toMatch(/requests · .+ from the cache · .+ from the upstream/);
    buttonByText(root, 'Show as table').click();
    await settle(fixture);
    expect(root.querySelectorAll('quader-usage-history tbody tr').length).toBe(288);
    const chart = historyChart(sampleHistory(), 'requests');
    expect(chart.marks.length).toBe(1);
    expect(chart.max).toBe(5000);
    // A time label within a tenth of an edge ends or starts there instead of being centred past it.
    for (const label of root.querySelectorAll<HTMLElement>('quader-usage-history .times span')) {
      const at = parseFloat(label.style.insetInlineStart);
      expect(label.classList.contains('end')).toBe(at > 90);
      expect(label.classList.contains('start')).toBe(at < 10);
    }
  });

  it('R-UI-184 names the steps and links a token to its account', () => {
    expect(stepText({ place: { kind: 'instance' }, step: 'kind', outcome: 'matched', rules: [{ id: 'b', who: { kind: 'every', callerKind: 'user' }, profile: 'standard', profileName: 'Standard', builtin: true, capped: false }] }, 'alice')).toBe(
      'Instance: the rule for Every signed-in user applies.',
    );
    expect(accountLink('sa:web/npm-local/ci')).toEqual(['/projects', 'web', 'repositories', 'npm-local', 'service-accounts', 'ci']);
    expect(accountLink('sa:backup-reader')).toEqual(['/admin/service-accounts', 'backup-reader']);
  });
});

@Component({
  selector: 'quader-test-download',
  imports: [DownloadLink],
  template: `<a href="/downloads/big.iso" download quaderDownload>Download</a>`,
})
class DownloadHost {}

describe('What a limited caller sees (R-UI-186)', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('R-UI-186 builds the notice from the problem in the reader’s words', () => {
    expect(limitNotice({ code: 'download_limit_reached', figure: 'cachedBytesPerHour', limit: 300_000_000_000, retryAfterSeconds: 840 }, null, 'en-US')).toBe(
      'Your download limit of 300 GB per hour is used up. Try again in 14 minutes.',
    );
    expect(limitNotice({ code: 'caller_blocked' }, 60, 'en-US', Date.parse('2026-11-20T10:54:00'))).toMatch(
      /^Downloads are paused for you until 10:55\sAM after too many refused requests\.$/,
    );
    expect(limitNotice({ code: 'too_many_parallel_downloads', figure: 'parallelDownloads', limit: 4 }, 1, 'en-US')).toBe(
      'You have 4 downloads running, the most you may have at once. Try again in 1 second.',
    );
    expect(waitText(30)).toBe('30 seconds');
  });

  it('R-UI-186 a refused download shows the notice with See your usage, and the page stays', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ code: 'too_many_parallel_downloads', figure: 'parallelDownloads', limit: 20, retryAfterSeconds: 1, detail: 'x' }), {
        status: 429,
        headers: { 'Content-Type': 'application/problem+json', 'Retry-After': '1' },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const { fixture, root, get } = await renderScreen(DownloadHost);
    root.querySelector('a')!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 }));
    await sleep(fixture, 20);
    expect(fetchMock.mock.calls[0]![1]).toMatchObject({ headers: { Range: 'bytes=0-0' } });
    const notice = get(Notifier).notices().at(-1)!;
    expect(notice.kind).toBe('alert');
    expect(notice.text).toBe('You have 20 downloads running, the most you may have at once. Try again in 1 second.');
    expect(notice.action?.label).toBe('See your usage');
  });

  it('R-UI-186 an admitted probe lets the browser download', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('x', { status: 206 })));
    expect(await probeDownload('/downloads/x', new AbortController().signal)).toEqual({ go: true });
  });
});
