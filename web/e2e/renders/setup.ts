import { mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { Browser, chromium, expect, request } from '@playwright/test';
import {
  Api,
  expectStatus,
  kinds,
  mainProject,
  publicRemotesOnline,
  seedAuditMode,
  seedContent,
  seedGrants,
  seedLimits,
  seedPoliciesAndMappings,
  seedProjectsAndRepositories,
  seedRegistryPresets,
  seedUpstreamRegistries,
  seedUsage,
  syncAdvisories,
} from './seed';
import { authFile, edge, outDir, stateDir } from './paths';
import { seedMalwareContent, seedMalwareVerdicts } from './malware';
import { seedSignatures, seedSignatureSettings } from './signatures';
import { seedScannerReplicas } from './scanners';
import { seedBastion } from './bastions';
import { removeCachedSources, seedCachedVersions } from './cached';
import { seedDebDocs } from './debdocs';
import { seedRoles } from './roles';

/**
 * The global setup of `make ui-renders`: waits for the edge (the dev server of the UI development profile), seeds the data
 * through the API as the break-glass account, signs the kinds of user in through the profile's sign-in provider and
 * keeps their sessions for the renders, and publishes the packages and files as the power user.
 */
export default async function setup(): Promise<void> {
  // A run of every screen starts from an empty folder; a run of some (--grep) replaces their pictures and keeps the rest,
  // so the contact sheet shows both.
  if (process.env['UI_RENDERS_CLEAN'] === '1') rmSync(outDir, { recursive: true, force: true });
  mkdirSync(join(outDir, '.auth'), { recursive: true });
  await waitForEdge();

  const password = readFileSync(join(stateDir, 'secrets', 'break-glass-password'), 'utf8').trim();
  const ctx = await request.newContext({ baseURL: edge, ignoreHTTPSErrors: true, extraHTTPHeaders: { Origin: edge } });
  await expectStatus(await ctx.post('/auth/local/login', { data: { username: 'admin', password } }), [204], 'break-glass sign-in');
  const admin = await Api.of(ctx, edge);
  await seedAuditMode(admin);
  await seedProjectsAndRepositories(admin);
  // The roles the remote galaxy read, written as its fills would have (R-ANS-056): the profile reaches no Galaxy.
  await seedRoles(admin);
  await seedUpstreamRegistries(admin);
  await seedRegistryPresets(admin);
  await seedBastion(admin);

  const browser = await chromium.launch();
  try {
    for (const kind of ['plain', 'power', 'admin', 'auditor'] as const) await signIn(browser, kind);
    await seedGrants(admin);
    // Before the power user's requests: its rule at the main project makes it a caller at the limit (R-UI-183).
    await seedLimits(admin);
    // After the CI issuer (seedLimits) and the group and service accounts (seedGrants) their entries and grants name.
    await seedPoliciesAndMappings(admin);
    await publishAsPowerUser(browser);
    await removeCachedSources(admin);
    await plainUserToken(browser);
    // After the technical tokens and the sign-ins, whose principals the usage names (R-LIM-001).
    seedUsage();
    // After the content, whose blobs the verdicts name, and the auditor's sign-in, who released two of them (R-MAL-024, R-MAL-025).
    await seedMalwareContent(admin);
    seedMalwareVerdicts();
    // The scanner replicas, with the database in use the verdicts name (R-OPS-048).
    seedScannerReplicas();
    // The anchors, the switch, a block and the generic file with its signature files, then the signatures of the content with their
    // verdicts, after the long sync so that the worker's own record of the signature files is done (R-SIG-035 to R-SIG-038).
    await seedSignatureSettings(admin);
    await syncAdvisories(admin);
    seedSignatures();
    // The remotes of public registries were offline through the seed, which so reached no public registry.
    await publicRemotesOnline(admin);
    for (const kind of ['plain', 'power', 'admin'] as const) await openRecent(browser, kind);
  } finally {
    await browser.close();
    await ctx.dispose();
  }
}

/** The edge answers once Quader behind the dev server does; `deploy/ui-dev/renders.sh` started both or found them running. */
async function waitForEdge(): Promise<void> {
  const ctx = await request.newContext({ baseURL: edge, ignoreHTTPSErrors: true });
  try {
    for (let i = 0; i < 120; i++) {
      const res = await ctx.get('/api/v1/auth/login-options').catch(() => null);
      if (res?.ok()) {
        const options = (await res.json()) as { providers: { id: string; available: boolean }[] };
        if (options.providers.some((p) => p.id === 'ui-dev' && p.available)) return;
      }
      await new Promise((r) => setTimeout(r, 1000));
    }
    throw new Error(`${edge} did not offer the sign-in provider ui-dev within two minutes: is make ui-dev-backend running?`);
  } finally {
    await ctx.dispose();
  }
}

/**
 * Signs one kind of user in through the provider's page and keeps the session (`.out/ui-renders/.auth/<kind>.json`). A
 * stored theme is cleared, so the browser's colour scheme decides, as "Follow the system" does (R-API-021).
 */
async function signIn(browser: Browser, kind: keyof typeof kinds): Promise<void> {
  const context = await browser.newContext({ baseURL: edge, ignoreHTTPSErrors: true });
  try {
    const page = await context.newPage();
    await page.goto('/ui/login');
    await page.getByRole('button', { name: /UI development users/ }).click();
    await page.getByRole('button', { name: new RegExp(`\\(${kinds[kind]}\\)`) }).click();
    // The login returns under the dev server's base href, /ui/ (UI-A-49): the start page opens signed in.
    await page.waitForURL((url) => url.origin === edge && url.pathname.startsWith('/ui/'));
    // By its place, not its words: the German pass signs in through the German app (UI_RENDERS_LOCALE=de).
    await expect(page.locator('button.user-button')).toBeVisible({ timeout: 30_000 });
    const api = await Api.of(context.request, edge);
    const prefs = await api.get('/api/v1/users/me/preferences');
    await expectStatus(prefs, [200], `preferences of ${kinds[kind]}`);
    const reset = await api.patch('/api/v1/users/me/preferences', { theme: null }, { 'If-Match': prefs.headers()['etag'] ?? '*' });
    await expectStatus(reset, [200], `reset the theme of ${kinds[kind]}`);
    await context.storageState({ path: authFile(kind) });
  } finally {
    await context.close();
  }
}

/**
 * Two repositories opened before, so that the side bar of the main area shows its folding group "Recently opened" (R-UI-167;
 * browser storage, kept with the session); after the grants, so that every kind may open them.
 */
async function openRecent(browser: Browser, kind: keyof typeof kinds): Promise<void> {
  const context = await browser.newContext({ baseURL: edge, ignoreHTTPSErrors: true, storageState: authFile(kind) });
  try {
    const page = await context.newPage();
    for (const key of ['assets', 'npm-internal']) {
      await page.goto(`/ui/projects/${mainProject}/repositories/${key}`);
      await expect(page.getByRole('heading', { level: 1, name: key })).toBeVisible({ timeout: 30_000 });
    }
    await context.storageState({ path: authFile(kind) });
  } finally {
    await context.close();
  }
}

/**
 * One personal token of the plain user, so that the token pages show a list and the closed section Advanced with **Revoke all
 * tokens** (the profile's, the user's in the admin area). Made once: a second run finds it and leaves it.
 */
async function plainUserToken(browser: Browser): Promise<void> {
  const context = await browser.newContext({ baseURL: edge, ignoreHTTPSErrors: true, storageState: authFile('plain') });
  try {
    const api = await Api.of(context.request, edge);
    const listed = await api.get('/api/v1/tokens?owner=me&state=active');
    await expectStatus(listed, [200], 'the plain user\'s tokens');
    const items = ((await listed.json()) as { items: { description: string }[] }).items;
    if (items.some((t) => t.description === 'laptop')) return;
    const created = await api.post('/api/v1/tokens', { description: 'laptop', ttlSeconds: 30 * 86_400, selfRotatable: false });
    await expectStatus(created, [201], 'create the plain user\'s token');
  } finally {
    await context.close();
  }
}

/** The power user's packages and files, with a personal access token of an hour that is revoked afterwards. */
async function publishAsPowerUser(browser: Browser): Promise<void> {
  const context = await browser.newContext({ baseURL: edge, ignoreHTTPSErrors: true, storageState: authFile('power') });
  try {
    const api = await Api.of(context.request, edge);
    // The membership was granted a moment ago; the epoch of R-AUTHZ-032 makes every replica decide with it.
    const me = await api.get('/api/v1/authz/me');
    await expectStatus(me, [200], 'authz/me');
    const created = await api.post('/api/v1/tokens', { description: 'make ui-renders seed', ttlSeconds: 3600, scope: { inherit: true } });
    await expectStatus(created, [201], 'create the seed token');
    const token = (await created.json()) as { id: string; value?: string; token?: string };
    const value = token.value ?? token.token;
    if (!value) throw new Error('the token answer carries no value');
    try {
      await seedContent(context.request, edge, value);
      await seedCachedVersions(context.request, edge, value);
      await seedDebDocs(context.request, value);
    } finally {
      await expectStatus(await api.delete(`/api/v1/tokens/${token.id}`), [204, 404], 'revoke the seed token');
    }
    writeFileSync(join(outDir, '.auth', 'seeded'), `${mainProject}\n`);
  } finally {
    await context.close();
  }
}
