import { Locator, Page, expect } from '@playwright/test';
import { ciIssuer, mainProject } from './seed';

/**
 * The failures a server reports, each where a screen says it (R-UI-214, Q-UI-1080): the screen's own sentence from the code, and the
 * server's English text folded under **Technical details**, opened in the picture. The seed's installation fails at none of them (no
 * upstream, no CI issuer and no Valkey fails in the profile), so each walk answers the failing state in the browser from the real
 * answer and saves nothing. The controls are found by their place, never by their words: every screen here is in the German pass,
 * whose check `english` finds a server's English sentence shown as the page's own (layout.ts).
 */

/** The time `minutes` from now, RFC 3339. */
function at(minutes: number): string {
  return new Date(Date.now() + minutes * 60_000).toISOString();
}

/** A GET of the app's API answered by the server and changed in the browser. */
async function changeAnswer(page: Page, url: string, change: (body: Record<string, unknown>) => Record<string, unknown>): Promise<void> {
  await page.unroute(url);
  await page.route(url, async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const res = await route.fetch();
    await route.fulfill({ response: res, body: JSON.stringify(change((await res.json()) as Record<string, unknown>)) });
  });
}

/** The same, and the page loaded again so that it reads the changed answer. */
async function reloadWith(page: Page, url: string, change: (body: Record<string, unknown>) => Record<string, unknown>): Promise<void> {
  await changeAnswer(page, url, change);
  const answered = page.waitForResponse(url);
  await page.reload();
  await answered;
  await page.waitForLoadState('networkidle');
}

/** Opens every **Technical details** inside `scope` and waits for the fold's animation. */
async function openDetails(page: Page, scope: Locator): Promise<void> {
  const toggles = scope.locator('quader-technical-details button.inline-toggle');
  await expect(toggles.first()).toBeVisible();
  for (const toggle of await toggles.all()) {
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  }
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
}

/**
 * A remote repository's settings (`npmjs`): a save that the server answers with two warnings, said in the screen's words above the
 * form (R-REPO-008); then **Test connection** of its upstream answered with an untrusted certificate, the reason in words and the
 * client's message with its code folded under it (R-REPO-064). The save is answered in the browser with the stored document: nothing
 * is written.
 */
export async function* remoteSettingsFailures(page: Page): AsyncGenerator<string> {
  const repo = `**/api/v1/projects/${mainProject}/repositories/npmjs`;
  await expect(page.locator('main form.top #f-description')).toBeVisible();
  await page.route(repo, async (route) => {
    if (route.request().method() !== 'PATCH') return route.fallback();
    const res = await route.fetch({ method: 'GET', postData: undefined });
    const body = (await res.json()) as Record<string, unknown>;
    const warnings = [
      { code: 'plain_http_upstream', pointer: '/remote/url', detail: 'the upstream is reached over plain http: credentials and content cross the network unprotected' },
      {
        code: 'public_populates_remote',
        pointer: '/visibility/artifacts',
        detail: 'A request without a credential fills the cache of this repository: its cacheFillAction is read. Set it to deploy to let only a deploying caller fill it.',
      },
    ];
    await route.fulfill({ response: res, body: JSON.stringify({ ...body, warnings }) });
  });
  await page.locator('main form.top #f-description').fill('The public npm registry, cached for the web team.');
  await page.locator('main form.top button[type="submit"]').click();
  await expect(page.locator('main .settings > p.banner-warning')).toHaveCount(2);
  yield 'saved-warnings';
  await page.unroute(repo);
  const upstream = page.locator('main quader-upstream-settings');
  await page.route('**/upstream-tests', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        verdict: 'unreachable',
        reachable: false,
        httpStatus: null,
        authenticated: null,
        durationMs: 412,
        tls: null,
        outboundProxy: null,
        sourceAddress: null,
        tokenService: null,
        error: {
          code: 'tls_untrusted',
          message: 'tls: failed to verify certificate: x509: certificate signed by unknown authority (registry.npm.example-company.com:443)',
        },
      }),
    }),
  );
  await upstream.locator('.test button').click();
  await expect(upstream.locator('[data-testid="upstream-test-result"]')).toBeVisible();
  await openDetails(page, upstream.locator('.test-result'));
  yield 'test-failed';
  await page.unroute('**/upstream-tests');
}

/**
 * A well-known registry of the project `upstream` (Debian): **Test connection** answered with a refused connection, the reason in
 * words beside the verdict and the client's message folded (R-REPO-064, R-UI-192).
 */
export async function* registryTestFailure(page: Page): AsyncGenerator<string> {
  const test = page.locator('main form .actions button[type="button"]').first();
  await expect(test).toBeVisible();
  // The saved registry is tested at its own address (`…/upstream-test`), a changed one at the project's (`…/upstream-tests`).
  const tests = (url: URL): boolean => /\/upstream-tests?$/.test(url.pathname);
  await page.route(tests, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        verdict: 'unreachable',
        reachable: false,
        httpStatus: null,
        authenticated: null,
        durationMs: 3,
        tls: null,
        outboundProxy: null,
        sourceAddress: null,
        tokenService: null,
        error: { code: 'connection_refused', message: 'dial tcp 151.101.2.132:443: connect: connection refused (deb.debian.org)' },
      }),
    }),
  );
  await test.click();
  await expect(page.locator('main .test-result .verdict')).toBeVisible();
  await openDetails(page, page.locator('main .test-result'));
  yield 'test-failed';
  await page.unroute(tests);
}

/**
 * The claim tester of the seed's CI issuer: a dry run answered as one that matches no mapping, with the failure in words and its
 * reason folded, a mapping looked at whose failed condition's sentence is folded, and two warnings in the screen's words
 * (R-CI-019, R-CI-010). The seed's issuer has no mapping, so the dry run's answer is the browser's.
 */
export async function* claimTesterFailure(page: Page): AsyncGenerator<string> {
  const evaluate = `**/api/v1/ci/issuers/${ciIssuer}/evaluate`;
  await page.route(evaluate, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        crossProject: false,
        selected: [],
        failure: 'no_matching_mapping',
        detail: 'No mapping of the issuer matches these claims.',
        trace: [
          {
            tier: 'system',
            mapping: 'web-frontend-release',
            matched: false,
            contributes: false,
            failedLeaf: '/match/all/1',
            detail: 'ref_protected equals true: value false',
            leaves: [
              { pointer: '/match/all/0', claim: 'project_id', operator: 'equals', expected: ['4711'], actual: ['4711'], absent: false, passed: true },
              { pointer: '/match/all/1', claim: 'ref_protected', operator: 'equals', expected: ['true'], actual: ['false'], absent: false, passed: false },
            ],
          },
        ],
        warnings: [
          { code: 'scope_policy_grants_not_checked', pointer: '/scope', names: ['delete:web-frontend/npm-internal'], detail: 'the scope asks for delete:web-frontend/npm-internal, which only a listed policy or group may give; the dry run reads no policy and took the item as held' },
          { code: 'mutable_user_claim', pointer: '/match/all/2', names: ['user_login'], detail: 'a condition on user_login: GitLab users can change it' },
        ],
      }),
    }),
  );
  await page.locator('main .row-actions button.btn-default').click();
  await expect(page.locator('main #ci-t-claims')).not.toHaveValue('');
  await page.locator('main .row-actions button.btn-confirm').click();
  await expect(page.locator('main [data-testid="ci-test-failure"]')).toBeVisible();
  await openDetails(page, page.locator('main'));
  yield 'failed';
  await page.unroute(evaluate);
}

/** The keys tab of the seed's CI issuer whose last fetch failed: the class and the status in words, the stored line folded (R-CI-003). */
export async function* issuerKeysFailure(page: Page): AsyncGenerator<string> {
  await reloadWith(page, `**/api/v1/ci/issuers/${ciIssuer}/keys`, (b) => ({
    ...b,
    lastError: 'http_status status=502 host=gitlab.renders.invalid',
    lastErrorAt: at(-7),
  }));
  await openDetails(page, page.locator('main'));
  yield 'failed';
  await page.unroute(`**/api/v1/ci/issuers/${ciIssuer}/keys`);
}

/** The list of CI issuers, the seed's issuer with a failed key fetch said in words (R-CI-003, R-UI-035). */
export async function* issuerListFailure(page: Page): AsyncGenerator<string> {
  await reloadWith(page, '**/api/v1/ci/issuers', (b) => ({
    ...b,
    items: (b['items'] as Record<string, unknown>[]).map((i) => ({
      ...i,
      keys: { usable: 0, ignored: 0, fetchedAt: null, lastError: 'tls_verification_failed host=gitlab.renders.invalid', lastErrorAt: at(-3) },
    })),
  }));
  await expect(page.locator('main td .bad').first()).toBeVisible();
  yield 'failed';
  await page.unroute('**/api/v1/ci/issuers');
}

/** A failed job of every ending (R-UI-038): the reason in words, the attempt, and the handler's text folded, opened for the first. */
export async function* jobsFailures(page: Page): AsyncGenerator<string> {
  const job = (over: Record<string, unknown>): Record<string, unknown> => ({
    id: crypto.randomUUID(),
    kind: 'vuln-sync',
    state: 'failed',
    replica: 'quader-worker-7c9f8d6b5-x2kqp',
    enqueuedAt: at(-50),
    startedAt: at(-49),
    finishedAt: at(-45),
    attempt: 3,
    maxAttempts: 3,
    progress: null,
    result: null,
    triggeredBy: null,
    target: null,
    href: '/api/v1/system/jobs/x',
    error: null,
    ...over,
  });
  const failed = [
    job({
      error: {
        code: 'attempts_exhausted',
        detail: 'the archive is not a valid zip archive: entry "MAL-2026-16369.json" is larger than the limit of 4 MiB (vulnerabilities.osv.max_record_bytes)',
        attempt: 3,
      },
    }),
    job({ kind: 'mirror-pass', state: 'queued', finishedAt: null, attempt: 1, error: { code: 'job_timeout', detail: 'the pass ran past its 30 minutes', attempt: 1 } }),
    job({ kind: 'gc', state: 'cancelled', attempt: 1, error: { code: 'cancelled' } }),
  ];
  const id = failed[0]!['id'] as string;
  failed[0]!['href'] = `/api/v1/system/jobs/${id}`;
  await reloadWith(page, '**/api/v1/system/jobs?**', (b) => ({ ...b, items: [...failed, ...((b['items'] as unknown[]) ?? [])] }));
  const first = page.locator('main tbody tr').first();
  await openDetails(page, first);
  yield 'failed';
  await page.unroute('**/api/v1/system/jobs?**');
}

/** **Settings › Malware scanning** with a signature database whose update fails: the title from the code, the task's text folded (R-MAL-023). */
export async function* malwareDatabaseFailure(page: Page): AsyncGenerator<string> {
  const settings = '**/api/v1/malware/settings';
  await reloadWith(page, settings, (b) => ({
    ...b,
    database: {
      ...(b['database'] as Record<string, unknown>),
      lastError: {
        code: 'database_missing',
        detail: 'The repository ops/clamav-db holds no main.cvd, daily.cvd or bytecode.cvd at its root.',
        at: at(-25),
        failures: 12,
      },
    },
  }));
  await openDetails(page, page.locator('main [data-testid="malware-db-error"]'));
  yield 'failed';
  await page.unroute(settings);
}

/** **Advisory sources** with a source whose last run failed on a known code and one on a code the screen does not know (R-VULN-022). */
export async function* advisorySourceFailures(page: Page): AsyncGenerator<string> {
  const sources = '**/api/v1/vulnerabilities/sources';
  await reloadWith(page, sources, (b) => ({
    ...b,
    items: (b['items'] as Record<string, unknown>[]).map((s, i) =>
      i === 0
        ? {
            ...s,
            failures: 2,
            lastError: {
              code: 'archive_limit',
              status: 502,
              title: 'The archive crosses a limit.',
              detail: 'the archive is not a valid zip archive: entry "MAL-2026-16369.json" is larger than the limit of 4 MiB',
            },
          }
        : i === 1
          ? { ...s, failures: 1, lastError: { code: 'upstream_changed', status: 502, title: 'The source changed.', detail: 'the source answered 418 to the request of the index' } }
          : s,
    ),
  }));
  await openDetails(page, page.locator('main quader-data-table tbody tr').first());
  yield 'failed';
  await page.unroute(sources);
}

/** **Settings › Signatures**, the public root's last refresh failed: the screen's sentence, the task's text folded (R-SIG-036). */
export async function* signatureRootFailure(page: Page): AsyncGenerator<string> {
  const settings = '**/api/v1/signatures/settings';
  await reloadWith(page, settings, (b) => ({
    ...b,
    trustRoot: {
      source: 'refreshed',
      fetchedAt: at(-3 * 1440),
      tufVersion: 171,
      lastError: 'tuf: fetching timestamp.json from https://tuf-repo-cdn.sigstore.dev: dial tcp: i/o timeout',
      lastErrorAt: at(-120),
    },
  }));
  const section = page.locator('main quader-disclosure').filter({ has: page.locator('[data-testid="signature-root"]') });
  const toggle = section.locator('button.toggle').first();
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await openDetails(page, section.locator('[data-testid="signature-root"]'));
  yield 'failed';
  await page.unroute(settings);
}

/**
 * The section Mirror of the remote `npmjs` stored as a mirror whose passes fail: the reason of the last error in words with the pass's
 * message and code folded, and a last complete pass whose discovery did not see everything, the format's note folded (R-UI-021).
 */
export async function* mirrorFailure(page: Page): AsyncGenerator<string> {
  const repo = `**/api/v1/projects/${mainProject}/repositories/npmjs`;
  const status = {
    mode: 'mirror',
    state: 'failing',
    nextPassAt: at(40),
    lastFullPassAt: at(-3 * 1440),
    pass: null,
    lastCompletePass: {
      kind: 'full',
      method: 'listing',
      outcome: 'partial',
      startedAt: at(-3 * 1440),
      finishedAt: at(-3 * 1440 + 52),
      packages: 9874,
      packagesFailed: 126,
      itemsStored: 41210,
      bytesStored: 9_126_805_504,
      discoveryComplete: false,
      discoveryNote: 'the upstream search stops at 10000 results; the names past it are not discovered',
    },
    errors: {
      consecutiveFailures: 4,
      lastAt: at(-35),
      lastCode: 'upstream_unavailable',
      lastMessage: 'registry.npmjs.org did not answer within 30 s (3 attempts)',
    },
    rateLimit: { waits: 0, waitSeconds: 0, lastAt: null, lastStatus: null, until: null },
    bound: { reached: null, at: null, maxBytes: null, usedBytes: 9_126_805_504 },
  };
  await page.route(`${repo}/mirror`, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(status) }));
  await changeAnswer(page, repo, (body) => ({ ...body, remote: { ...(body['remote'] as Record<string, unknown>), mode: 'mirror' } }));
  await page.reload();
  await page.waitForLoadState('networkidle');
  const section = page.locator('main quader-disclosure').filter({ has: page.locator('quader-mirror-settings') });
  const toggle = section.locator('button.toggle').first();
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(section.locator('[data-testid="mirror-error"]')).toBeVisible();
  await openDetails(page, section.locator('quader-mirror-status'));
  yield 'failed';
  await page.unroute(`${repo}/mirror`);
  await page.unroute(repo);
}

/** A version's invalid signature with the verifier's note, which the entry's Technical details hold (R-SIG-035). */
export async function* signatureNote(page: Page): AsyncGenerator<string> {
  const report = '**/api/v1/packages/*/versions/*/signatures';
  const note = 'the signature does not verify with the certificate of the bundle: crypto/ecdsa: verification error';
  await reloadWith(page, report, (b) => ({
    ...b,
    repositories: (b['repositories'] as Record<string, unknown>[]).map((r) => ({
      ...r,
      signatures: ((r['signatures'] as Record<string, unknown>[] | undefined) ?? []).map((s, i) => (i === 0 ? { ...s, detail: note } : s)),
    })),
  }));
  for (const fold of await page.locator('main quader-signature-report quader-disclosure button.inline-toggle').all()) {
    await fold.click();
    await expect(fold).toHaveAttribute('aria-expanded', 'true');
  }
  await expect(page.locator('main [data-testid="signature-note"]').first()).toBeVisible();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'note';
  await page.unroute(report);
}

/**
 * **System status** with Valkey degraded on the profile's replica (R-OPS-003): the reason in words on the card and in the replica's
 * row, and the client's own text, which names an address, folded in the row.
 */
export async function* valkeyDegraded(page: Page): AsyncGenerator<string> {
  await changeAnswer(page, '**/api/v1/system/info', (b) => ({ ...b, valkey: { ...(b['valkey'] as Record<string, unknown>), degradedReplicas: 1 } }));
  await reloadWith(page, '**/api/v1/system/replicas', (b) => ({
    ...b,
    items: (b['items'] as Record<string, unknown>[]).map((r) => ({
      ...r,
      degraded: ['valkey'],
      valkey: {
        ...(r['valkey'] as Record<string, unknown>),
        state: 'degraded',
        reasonCode: 'probe_failed',
        reason: 'probe failed: dial tcp 10.42.3.17:6379: i/o timeout',
      },
    })),
  }));
  await openDetails(page, page.locator('main'));
  yield 'valkey-degraded';
  await page.unroute('**/api/v1/system/replicas');
  await page.unroute('**/api/v1/system/info');
}
