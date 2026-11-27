import { Page, expect } from '@playwright/test';
import { Api, expectStatus, mainProject, psql } from './seed';

/**
 * The Bastions screen's direct reads from the object store (bastion.md R-BAS-071), answered in the browser: the render profile has no
 * registration and one plain connection, so two registrations with their replicas and tokens, the switch off and on, and connections a
 * Bastion cannot read (a failed link check, an http address, an encrypted one under a host of Quader) beside an encrypted one it reads
 * with each file's key are answers of the walks. Nothing is saved: the switch is never sent.
 */

type Json = Record<string, unknown>;

const DAY = 86_400_000;

/** A link check the store answered 403 with no bytes (Q-UI-996): the fields the pages word the reason from. */
export const forbiddenLink = { outcome: 'unexpected_answer', status: 403, bytes: 0, expectedStatus: 200, expectedBytes: 2048 };
const at = (days: number): string => new Date(Date.now() + days * DAY).toISOString();

/** The read of a Bastion's projects (R-BAS-134), which a walk answers wherever it answers the registrations. */
const projectsUrl = (url: URL): boolean => /\/api\/v1\/bastions\/[^/]+\/projects$/.test(url.pathname);

function contact(replica: string, address: string, baseUrl: string, minutesAgo: number, over: Json = {}): Json {
  return {
    replica,
    contactAt: at(-minutesAgo / 1440),
    clientAddress: address,
    release: '0.42.0',
    interfaceVersion: 9,
    baseUrl,
    edgeLimit: 'rps=1000; connections=256',
    catalogueKept: null,
    ...over,
  };
}

/** The limits of a new registration (R-LIM-046, R-BAS-093): the preset Internet in Enforce, 200 MB/s. */
function limits(over: Json = {}): Json {
  return {
    mode: 'enforce',
    preset: 'internet',
    flood: {
      anonymous: { requestsPerSecond: 50, burst: 1000, missesPerMinute: 1000 },
      authenticated: { requestsPerSecond: 200, burst: 4000, missesPerMinute: 4000 },
      gui: { requestsPerSecond: 20, burst: 100 },
      failedAuth: { maxFailures: 20, windowSeconds: 600, timeoutSeconds: 900 },
      blocking: { threshold: 3000, windowSeconds: 60, durationSeconds: 900, maxDurationSeconds: 86400 },
      exemptNetworks: [],
    },
    slots: { maxConcurrent: 64, anonymousShare: 0.25, queueTimeoutMillis: 5000 },
    throughput: { bytesPerSecond: 200_000_000, anonymousShare: 0.25, bytesPerHour: { anonymous: null, signedIn: null, catalogue: null } },
    anonymousCeiling: { requestsPerSecond: 100, burst: 2000, requestsPerHour: 36000 },
    ...over,
  };
}

/** What a replica reported a minute ago (R-LIM-045): half of the card's line, the read rate near its cap, blocks. */
function report(replica: string, blocks: readonly Json[], over: Json = {}): Json {
  return {
    replica,
    reportedAt: at(-1 / 1440),
    // The volume backend: each replica's cache is its own, and the card adds them up (Q-BAS-480).
    cache: { shared: false, bytes: 7_000_000_000, capacityBytes: 10_000_000_000, hits: 9_700, misses: 300, evictions: 12, protectedBytes: 4_500_000_000 },
    // Together 132 of the registration's 150 MB/s: the meter close to its cap, in the warning colour.
    throughput: { limited: false, bytesPerSecondByClass: { anonymous: 18_000_000, signedIn: 46_000_000, catalogue: 2_000_000 } },
    limited: 2,
    blocked: blocks.length,
    blocks,
    // Working (R-BAS-133): no disable applied, nothing to confirm.
    operation: { disabled: null, running: 0 },
    ...over,
  };
}

function token(id: string, description: string, hint: string, createdDays: number, expiresDays: number): Json {
  return { id, hint, description, createdAt: at(-createdDays), expiresAt: at(expiresDays), lastUsedAt: at(-0.002), declared: false };
}

/**
 * `partners`, off, with short values (a pod name of 31 characters, a base URL of 28, an IPv4 address), and `dmz-frankfurt-edge`, the
 * longest name of the rule but two, on, with long ones: a pod name of 63 characters (the most Kubernetes allows), a base URL of 51, an
 * IPv6 address of 39, a token that expires in 20 days beside one of a year. The tables keep one form at a width whatever they hold
 * (Q-UI-959). The addresses each Bastion connects from (R-UI-243): `partners` lists none, `dmz-frankfurt-edge` the /64 of one replica
 * and not the IPv4 address of the other. `dmz-hamburg` is registered and never contacted: no token, no replica, its web interface off.
 * The sections Replicas and Tokens (Q-UI-1622, Q-UI-1623): `dmz-frankfurt-edge` runs two versions and has a token expiring in 20 days,
 * `partners` one in 21, so their sections warn and open by themselves; `dmz-munich` is healthy, every section closed and one line.
 */
function registrations(frankfurtOn: boolean): { items: Json[] } {
  return {
    items: [
      {
        id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a40',
        name: 'dmz-frankfurt-edge',
        description: 'The DMZ of the Frankfurt data centre, for the build agents of our partners',
        disabled: null,
        state: 'connected',
        running: 0,
        declared: null,
        switches: { handover: frankfurtOn, gui: true, docs: true, hideRepositories: false },
        // What the main installation last built for its web interface (R-BAS-104): built a few minutes ago in the walks of the direct
        // reads, nothing while the main installation admits no request without a credential in the walk of the limits.
        catalogue: frankfurtOn
          ? { outcome: 'built', reason: null, checkedAt: at(-3 / 1440), builtAt: at(-40 / 1440) }
          : { outcome: 'stopped', reason: 'anonymous_requests_refused', checkedAt: at(-3 / 1440), builtAt: null },
        // Custom, in Audit, at 150 MB/s with a flood figure above the edge's (R-UI-240): every state of the card's section at once.
        limits: limits({
          mode: 'audit',
          preset: 'custom',
          flood: { ...(limits()['flood'] as Json), anonymous: { requestsPerSecond: 80, burst: 1600, missesPerMinute: 1000 }, exemptNetworks: ['198.51.100.0/24'] },
          throughput: { bytesPerSecond: 150_000_000, anonymousShare: 0.25, bytesPerHour: { anonymous: 50_000_000_000, signedIn: null, catalogue: null } },
        }),
        status: [
          report('quader-bastion-dmz-frankfurt-edge-partners-eu1-6f8b9c7d4f-x2k4q', [
            { address: '203.0.113.9/32', until: at(0.01), reason: 'refusals' },
          ]),
          report('quader-bastion-dmz-frankfurt-edge-partners-eu1-6f8b9c7d4f-m8v2j', [
            { address: '203.0.113.9/32', until: at(0.01), reason: 'refusals' },
            { address: '2001:db8:4::/64', until: at(0.02), reason: 'failed_auth' },
          ]),
        ],
        // The /64 its IPv6 replica leaves from; the other replica came from an IPv4 address it does not list (R-UI-243).
        egress: ['2001:db8:85a3::/64'],
        createdAt: at(-120),
        tokens: [
          token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e43', 'frankfurt 2027, rolled out with the release of the build agents in January', 'qdr_bst_Xw7p', 2, 365),
          token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e42', 'frankfurt 2026', 'qdr_bst_9Hd2', 345, 20),
        ],
        contacts: [
          contact('quader-bastion-dmz-frankfurt-edge-partners-eu1-6f8b9c7d4f-x2k4q', '2001:0db8:85a3:0000:0000:8a2e:0370:7334', 'https://packages.dmz-frankfurt.partners.example.com', 1, { edgeLimit: 'rps=60; connections=256', catalogueKept: false }),
          // A replica still on the release before (Q-UI-1622): the section Replicas warns and opens by itself.
          contact('quader-bastion-dmz-frankfurt-edge-partners-eu1-6f8b9c7d4f-m8v2j', '198.51.100.21', 'https://packages.dmz-frankfurt.partners.example.com', 2, {
            edgeLimit: 'rps=60; connections=256',
            catalogueKept: false,
            release: '0.41.3',
          }),
        ],
      },
      {
        id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a41',
        name: 'partners',
        description: 'The partner network',
        disabled: null,
        state: 'connected',
        running: 0,
        // Declared in the Helm values (R-BAS-127, Q-UI-1497 to Q-UI-1499): the lock line with the Secret, no Delete, the networks in
        // their fields and the warning of its replica's unlisted address with its Add, unavailable (R-UI-270), the declared token
        // without an expiry and without Revoke beside one issued on the screen.
        declared: { since: at(-2), secretName: 'quader-bastion-token' },
        // The web interface on, but the address its replica reported is not listed: the builder stops and says why (R-BAS-104).
        switches: { handover: false, gui: true, docs: false, hideRepositories: false },
        catalogue: { outcome: 'stopped', reason: 'bastion_base_url_unknown', checkedAt: at(-3 / 1440), builtAt: null },
        // A new registration's limits, and no replica that reported its state (R-UI-240).
        limits: limits(),
        status: [],
        egress: ['10.252.0.0/16'],
        createdAt: at(-170),
        // The declared token as the server stored it (its phrase); the screen names it itself (Q-UI-1629). The issued one expires in
        // three weeks: the section Tokens warns and opens by itself (Q-UI-1623).
        tokens: [
          { ...token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e44', 'Bastion partners (declared in the configuration)', 'qdr_bst_Lm4c', 2, 0), expiresAt: null, declared: true },
          token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e41', 'partners 2026', 'qdr_bst_3kQ9', 165, 21),
        ],
        contacts: [contact('quader-bastion-7d9c5b8f6-p4r7t', '203.0.113.7', 'https://packages.example.com', 1)],
      },
      {
        id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a42',
        name: 'dmz-hamburg',
        description: '',
        disabled: null,
        state: 'noContact',
        running: 0,
        declared: null,
        // Registered a moment ago and never contacted: no token, no replica, no report, the web interface off, nothing built (Q-UI-1616:
        // its section in plain view all the same).
        switches: { handover: false, gui: false, docs: false, hideRepositories: false },
        catalogue: null,
        limits: limits(),
        status: [],
        egress: [],
        createdAt: at(-0.01),
        tokens: [],
        contacts: [],
      },
      {
        id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a43',
        name: 'dmz-munich',
        description: 'The DMZ of the Munich office',
        disabled: null,
        state: 'connected',
        running: 0,
        declared: null,
        // Healthy (W-39's 2.2): one replica a minute ago, its address listed, a token a year away, the web interface built, the direct
        // reads off, Internet in Enforce at 200 MB/s: every section closed and one line.
        switches: { handover: false, gui: true, docs: true, hideRepositories: false },
        catalogue: { outcome: 'built', reason: null, checkedAt: at(-3 / 1440), builtAt: at(-60 / 1440) },
        limits: limits(),
        status: [report('quader-bastion-dmz-munich-5c7d9f8b6-q7w3n', [], { limited: 0, throughput: { limited: false, bytesPerSecondByClass: { anonymous: 9_000_000, signedIn: 32_000_000, catalogue: 0 } } })],
        egress: ['192.0.2.64/28'],
        createdAt: at(-60),
        tokens: [token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e45', 'munich 2027', 'qdr_bst_Q2vn', 30, 365)],
        contacts: [contact('quader-bastion-dmz-munich-5c7d9f8b6-q7w3n', '192.0.2.70', 'https://packages.dmz-munich.example.com', 1, { edgeLimit: 'rps=1000; connections=256' })],
      },
    ],
  };
}

function connection(name: string, endpoint: string, over: Json = {}): Json {
  return { name, encrypted: false, endpoint, publicAddress: null, usable: true, reasons: [], handoverUsable: true, handoverReasons: [], ...over };
}

/** One connection, which a Bastion can read: the card shows the switch and its sentence alone. */
const readable = [connection('default', 'https://object.storage.eu01.onstackit.cloud', { publicAddress: 'https://objects.example.com' })];

/**
 * A connection a Bastion can read, one whose link check failed, one on an in-cluster http address, an encrypted one a Bastion reads with
 * each file's key (no line) and an encrypted one whose address lies under a host of Quader (Q-BAS-300).
 */
const blocked = [
  ...readable,
  connection('eu-backup-frankfurt', 'https://s3.backup.eu-central-2.example.net', {
    usable: false,
    handoverUsable: false,
    reasons: [{ code: 'link_check_failed', detail: "the link answered 403 with 0 bytes, want 200 with the probe's 2048", check: forbiddenLink }],
    handoverReasons: [{ code: 'link_check_failed', detail: "the link answered 403 with 0 bytes, want 200 with the probe's 2048", check: forbiddenLink }],
  }),
  connection('onprem', 'http://seaweedfs.quader-storage.svc:8333', {
    usable: false,
    handoverUsable: false,
    reasons: [{ code: 'not_https', detail: null }],
    handoverReasons: [{ code: 'not_https', detail: null }],
  }),
  connection('archive', 'https://objects.archive.internal.example.com', { encrypted: true, usable: false, reasons: [{ code: 'encrypted', detail: null }] }),
  connection('vault-eu-west', 'https://vault.s3.quader.example.com', {
    encrypted: true,
    usable: false,
    reasons: [{ code: 'encrypted', detail: null }, { code: 'quader_host', detail: 'quader.example.com' }],
    handoverUsable: false,
    handoverReasons: [{ code: 'quader_host', detail: 'quader.example.com' }],
  }),
];

/**
 * The registration of the seed (R-BAS-004, R-BAS-007): a Bastion that reported its base URL at a contact of one replica a moment ago, so
 * that **Set up client** offers it under **Install from** and answers its snippets (`setup-npm-bastion`). The base URL is an entry of
 * `server.additional_base_urls` of the profile (deploy/ui-dev/config.sh), as the setup answer requires. It holds one token, and the
 * contact is written as a Bastion's handshake with that token leaves it, through the profile's database: the profile runs no Bastion.
 * A second run finds the registration and its token and moves the contact to now.
 */
export const seededBastion = { name: 'dmz-berlin', baseUrl: 'https://packages.dmz-berlin.example.com' } as const;

export async function seedBastion(admin: Api): Promise<void> {
  const created = await admin.post('/api/v1/bastions', { name: seededBastion.name, description: 'The DMZ of the Berlin office, for the build agents of our partners' });
  await expectStatus(created, [201, 409], `the Bastion ${seededBastion.name}`);
  // The token its chart reads, issued once: the replica made contact with it.
  const read = await admin.get(`/api/v1/bastions/${seededBastion.name}`);
  await expectStatus(read, [200], `the Bastion ${seededBastion.name}`);
  if (((await read.json()) as { tokens: unknown[] }).tokens.length === 0) {
    const token = await admin.post(`/api/v1/bastions/${seededBastion.name}/tokens`, { description: 'berlin 2026' });
    await expectStatus(token, [201], `a token of the Bastion ${seededBastion.name}`);
  }
  psql(`INSERT INTO bastion_contact (bastion_id, replica, contact_at, client_address, release, interface_version, base_url, token_id)
SELECT p.id, 'quader-bastion-6c9f7d5b8-k2m4x', now(), '192.0.2.41', '0.42.0', 6, '${seededBastion.baseUrl}',
       (SELECT t.id FROM api_token t WHERE t.principal_id = p.id ORDER BY t.created_at DESC LIMIT 1)
  FROM principal p WHERE p.kind = 'bastion' AND p.name = '${seededBastion.name}'
ON CONFLICT (bastion_id, replica) DO UPDATE SET contact_at = now(), base_url = EXCLUDED.base_url, token_id = EXCLUDED.token_id;
UPDATE api_token t SET last_used_at = now(), last_used_addr = '192.0.2.41'
  FROM principal p WHERE t.principal_id = p.id AND p.kind = 'bastion' AND p.name = '${seededBastion.name}';`);
  await seedBastionConfiguration(admin);
}

/**
 * What the Bastions of the profile distribute (bastion.md R-BAS-134, authorization.md R-AUTHZ-043): a Bastion serves nothing until an
 * administrator allows a project and its Owner chooses the repository. The profile's own Bastion `ui-dev` (deploy/ui-dev/bastion.sh)
 * distributes every public repository of the installation, the project upstream's included, so its web interface keeps the catalogue its
 * renders show; the seed's registration `dmz-berlin` distributes the npm repository and the image repository whose **Set up client**
 * offers it as the place to install from (`setup-npm-bastion`, `setup-oci-bastion`) and whose panels offer it as **From**, and the
 * well-known `upstream/ubuntu`, whose tower stands beside its level on **Upstream registries** (R-UI-271). A registration that does not
 * exist (a run with UI_BASTION=0 has no `ui-dev`) is left out. Every
 * write is idempotent: a second run changes nothing.
 */
export async function seedDistribution(admin: Api): Promise<void> {
  const chosen = new Map<string, Set<string>>(); // "<project>/<key>" → the Bastions that distribute it
  const choose = (repository: string, bastion: string): void => {
    chosen.set(repository, (chosen.get(repository) ?? new Set<string>()).add(bastion));
  };
  if ((await admin.get('/api/v1/bastions/ui-dev')).status() === 200) {
    for (let cursor = ''; ; ) {
      const page = await admin.get(`/api/v1/repositories?visibility=public&limit=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`);
      await expectStatus(page, [200], 'the public repositories');
      const body = (await page.json()) as { items: { project: string; key: string }[]; nextCursor: string | null };
      for (const r of body.items) choose(`${r.project}/${r.key}`, 'ui-dev');
      if (!body.nextCursor) break;
      cursor = body.nextCursor;
    }
  }
  choose(`${mainProject}/npm`, seededBastion.name);
  // An image repository too: Set up client's Install from and every Pull panel's From (W-41's D2, setup-oci-bastion).
  choose(`${mainProject}/images`, seededBastion.name);
  choose('upstream/ubuntu', seededBastion.name);
  for (const [repository, bastions] of chosen) {
    const [project, key] = repository.split('/');
    for (const bastion of bastions) {
      const allowed = await admin.put(`/api/v1/bastions/${bastion}/projects/${project}`, {});
      await expectStatus(allowed, [204], `allow the project ${project} for the Bastion ${bastion}`);
    }
    const set = await admin.patch(`/api/v1/projects/${project}/repositories/${key}`, { bastions: [...bastions].sort() }, { 'If-Match': '*' });
    await expectStatus(set, [200], `the Bastions of ${repository}`);
  }
}

/**
 * The configuration of the seed's registration (R-UI-240, R-UI-241): a throughput of 150 MB/s with a volume an hour for anonymous callers,
 * an anonymous ceiling without its hour, the web interface on, a rule of an address beside the preset's kind rules, the edge figure its
 * replica reported and a state it reported now with two blocks, written through the profile's database as the intake writes it (the
 * profile runs no Bastion; E9b's sender is not built). A second run leaves the rule and sets the rest again.
 */
async function seedBastionConfiguration(admin: Api): Promise<void> {
  const url = `/api/v1/bastions/${seededBastion.name}`;
  const read = await admin.get(url);
  await expectStatus(read, [200], `the Bastion ${seededBastion.name}`);
  const patched = await admin.patch(
    url,
    {
      switches: { gui: true },
      limits: {
        throughput: { bytesPerSecond: 150_000_000, bytesPerHour: { anonymous: 50_000_000_000 } },
        anonymousCeiling: { requestsPerHour: null },
      },
    },
    { 'If-Match': read.headers()['etag'] ?? '' },
  );
  await expectStatus(patched, [200], `the configuration of the Bastion ${seededBastion.name}`);
  const rule = await admin.post(
    `${url}/limits/assignments`,
    { who: { kind: 'address', prefix: '203.0.113.0/24' }, profile: 'restricted', overrides: {}, note: 'The partner office in Lyon' },
    { 'Idempotency-Key': `render-bastion-rule-${Date.now()}` },
  );
  await expectStatus(rule, [201, 409], `a rule of the Bastion ${seededBastion.name}`);
  psql(`UPDATE bastion_contact c SET edge_limit = 'rps=1000; connections=256', catalogue_kept = true
  FROM principal p WHERE c.bastion_id = p.id AND p.kind = 'bastion' AND p.name = '${seededBastion.name}';`);
  reportBastionState();
}

/**
 * The state the seed's replica reported now, with its three blocks, written as the intake writes it (R-LIM-045). A replica's state goes an
 * hour after its last report (Q-BAS-443) and a block once it ends: the seed writes it, and the walk of the limits page writes it again, since
 * a whole run of the renders on one machine reaches that page more than an hour after the seed (2026-11-27).
 */
function reportBastionState(): void {
  const until = (hours: number): string => new Date(Date.now() + hours * 3_600_000).toISOString();
  const status = {
    replica: 'quader-bastion-6c9f7d5b8-k2m4x',
    at: new Date().toISOString(),
    cache: { shared: true, bytes: 7_000_000_000, capacityBytes: 20_000_000_000, hits: 9_700, misses: 300, evictions: 12, protectedBytes: 4_500_000_000 },
    throughput: { limited: false, bytesPerSecondByClass: { anonymous: 12_000_000, signedIn: 29_000_000, catalogue: 0 } },
    limited: 4,
    blocked: 3,
    // One block of each reason of the closed list (R-LIM-045).
    blocks: [
      { address: '203.0.113.9/32', until: until(0.25), reason: 'refusals' },
      { address: '2001:db8:4::/64', until: until(12), reason: 'failed_auth' },
      { address: '198.51.100.77/32', until: until(1), reason: 'refused_requests' },
    ],
  };
  psql(`INSERT INTO bastion_status (bastion_id, replica, reported_at, status)
SELECT p.id, 'quader-bastion-6c9f7d5b8-k2m4x', now(), '${JSON.stringify(status)}'::jsonb
  FROM principal p WHERE p.kind = 'bastion' AND p.name = '${seededBastion.name}'
ON CONFLICT (bastion_id, replica) DO UPDATE SET reported_at = now(), status = EXCLUDED.status;`);
}

/** The kind rules a preset wrote at `partners`, as the place's rules answer them (R-LIM-043), for the preset's dialog. */
function partnersRules(): Json {
  const place = { kind: 'bastion', bastion: 'partners' };
  const kinds: [string, string][] = [
    ['anonymous', 'bastion-anonymous'],
    ['user', 'bastion-signed-in'],
    ['technicalToken', 'bastion-signed-in'],
    ['ci', 'bastion-signed-in'],
    ['administrator', 'bastion-signed-in'],
  ];
  const stamp = { version: 1, createdAt: at(-170), createdBy: null, updatedAt: at(-170), updatedBy: null };
  return {
    items: kinds.map(([kind, profile]) => ({
      id: `0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a5${kinds.findIndex(([k]) => k === kind)}`,
      place,
      who: { kind: 'every', callerKind: kind },
      profile,
      overrides: {},
      effective: {},
      clamped: {},
      capped: false,
      builtin: true,
      note: '',
      ...stamp,
    })),
    nextCursor: null,
  };
}

/** The registrations of the walk and the place's rules of `partners` answered in the browser, then the page loaded again. */
async function answerLimits(page: Page): Promise<void> {
  await page.route('**/api/v1/bastions', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(registrations(false)) }),
  );
  await page.route('**/api/v1/bastions/partners/limits/assignments**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(partnersRules()) }),
  );
  await answerProjects(page);
  await page.reload();
  await page.waitForLoadState('networkidle');
  await expect(page.locator('main quader-bastion-card')).toHaveCount(registrations(false).items.length);
}

/**
 * The section **Limits** of a card (R-UI-240), in both languages, so every locator is the markup's, never a word: `dmz-frankfurt-edge`
 * Custom in Audit at 150 MB/s with the state of two replicas, the edge warning, the catalogue's sentence and the egress offer, and
 * `partners` as a new registration; then the details of `dmz-frankfurt-edge` opened, the preset's dialog of `partners` naming what
 * Partner network replaces, and the mode's dialog of `dmz-frankfurt-edge`, each cancelled; then the section Replicas of
 * `dmz-frankfurt-edge`, which its warning opened, and the help sign of its direct reads tapped. Nothing is saved.
 */
export async function* bastionLimits(page: Page): AsyncGenerator<string> {
  await answerLimits(page);
  const frankfurt = page.locator('main quader-bastion-card').nth(0);
  const partners = page.locator('main quader-bastion-card').nth(1);
  await expect(frankfurt.locator('[data-testid="bastion-edge-warning"]')).toBeVisible();
  yield '';
  const details = frankfurt.locator('quader-bastion-limits quader-disclosure.details button[aria-expanded]').first();
  await details.click();
  await expect(details).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'details';
  await details.click();
  await expect(details).toHaveAttribute('aria-expanded', 'false');
  const presets = partners.locator('.presets button');
  if ((await presets.count()) > 0) {
    await presets.nth(1).click();
    const dialog = page.locator('quader-bastion-preset-dialog');
    await expect(dialog.locator('[data-testid="preset-kinds"]')).toBeVisible();
    yield 'preset';
    await dialog.locator('.dialog-actions button').first().click();
    await expect(dialog).toHaveCount(0);
    await frankfurt.locator('.mode-line button').click();
    const mode = page.locator('quader-limit-mode-dialog');
    await expect(mode).toBeVisible();
    yield 'mode';
    await mode.locator('.dialog-actions button').first().click();
    await expect(mode).toHaveCount(0);
  }
  // The replicas on two versions opened their section by itself (Q-UI-1622): its table in the card, the warning as its line.
  const replicas = frankfurt.locator('[data-testid="bastion-replicas"]');
  await expect(replicas.locator('button[aria-expanded]')).toHaveAttribute('aria-expanded', 'true');
  await replicas.scrollIntoViewIfNeeded();
  yield 'replicas-open';
  // A help sign tapped (R-UI-267): the direct reads' sentence pinned in its bubble, inside the window at every width.
  const sign = frankfurt.locator('[data-testid="bastion-handover-row"] button.help-sign');
  await sign.scrollIntoViewIfNeeded();
  await sign.click();
  await expect(page.locator('.cdk-overlay-container quader-tooltip')).toBeVisible();
  yield 'help';
  await page.keyboard.press('Escape');
  await expect(page.locator('.cdk-overlay-container quader-tooltip')).toHaveCount(0);
  // The declared registration's networks (R-UI-270): their field and Add address unavailable in its Details, Add address tapped, its
  // reason pinned.
  const declaredDetails = partners.locator('quader-bastion-limits quader-disclosure.details button[aria-expanded]').first();
  await declaredDetails.click();
  await expect(declaredDetails).toHaveAttribute('aria-expanded', 'true');
  const addAddress = partners.locator('quader-bastion-egress .row-actions button[aria-disabled="true"]');
  await addAddress.scrollIntoViewIfNeeded();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  // Playwright holds an aria-disabled control for not enabled and waits; the tap a person makes reaches it.
  await addAddress.click({ force: true });
  await expect(page.locator('.cdk-overlay-container quader-tooltip')).toBeVisible();
  yield 'declared-networks';
  await page.keyboard.press('Escape');
  await page.unroute('**/api/v1/bastions');
  await page.unroute('**/api/v1/bastions/partners/limits/assignments**');
  await page.unroute(projectsUrl);
}

/**
 * A healthy registration (W-41's 3.1): its replicas heard a minute ago from the networks it lists, an edge above its flood figures, its
 * web interface built, a token a year away, its direct reads off, Internet in Enforce at 200 MB/s. `over` changes what a state needs.
 */
function healthy(name: string, id: string, contacts: Json[], over: Json = {}): Json {
  return {
    id,
    name,
    description: '',
    disabled: null,
    state: 'connected',
    running: 0,
    declared: null,
    switches: { handover: false, gui: true, docs: true, hideRepositories: false },
    catalogue: { outcome: 'built', reason: null, checkedAt: at(-1 / 1440), builtAt: at(-3 / 1440) },
    limits: limits(),
    status: [],
    egress: ['192.0.2.64/28', '198.51.100.32/28', '203.0.113.0/24'],
    createdAt: at(-90),
    tokens: [token(`${id}-token`, `${name} 2027`, 'qdr_bst_T4kd', 30, 365)],
    contacts,
    ...over,
  };
}

/** What a replica of a deployment on the bucket backend reports: its deployment's bucket, which each of its replicas reports alike. */
function bucketReport(replica: string, bytesPerSecond: number, operation: Json = { disabled: null, running: 0 }): Json {
  return report(replica, [], {
    cache: { shared: true, bytes: 3_500_000_000, capacityBytes: 5_000_000_000, hits: 9_700, misses: 300, evictions: 12, protectedBytes: 2_250_000_000 },
    throughput: { limited: false, bytesPerSecondByClass: { anonymous: bytesPerSecond / 4, signedIn: (bytesPerSecond * 3) / 4, catalogue: 0 } },
    limited: 0,
    operation,
  });
}

/**
 * The page of several Bastions (W-41, R-UI-240, Q-UI-1640): `artifacts`, healthy, a pair of deployments under one token (two replicas
 * behind pkgs-eu.example.com, one behind pkgs-us.example.com, each with its bucket: the card adds the two, 7.0 of 10.0 GB); `dmz-bonn`,
 * disabled softly three minutes ago and not confirmed (its replicas silent since before), which opens by itself; `dmz-cologne`, disabled
 * hard two hours ago and confirmed; `dmz-leipzig`, disabling softly with three downloads still running; `partners`, declared in the Helm
 * configuration, healthy: **Delete** unavailable with its reason.
 */
function stateRegistrations(): { items: Json[] } {
  const soft = (minutes: number): Json => ({ mode: 'soft', at: at(-minutes / 1440) });
  return {
    items: [
      healthy(
        'artifacts',
        '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a60',
        [
          contact('quader-bastion-6f8b9c7d4f-x2k4q', '192.0.2.70', 'https://pkgs-eu.example.com', 1),
          contact('quader-bastion-6f8b9c7d4f-m8v2j', '192.0.2.71', 'https://pkgs-eu.example.com', 2),
          contact('quader-bastion-5d7c8b6f9-q7w3n', '198.51.100.40', 'https://pkgs-us.example.com', 1),
        ],
        {
          description: 'Packages for our customers and partners',
          status: [
            bucketReport('quader-bastion-6f8b9c7d4f-x2k4q', 15_000_000),
            bucketReport('quader-bastion-6f8b9c7d4f-m8v2j', 14_000_000),
            bucketReport('quader-bastion-5d7c8b6f9-q7w3n', 12_000_000),
          ],
          tokens: [
            token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e61', 'artifacts 2027', 'qdr_bst_T4kd', 30, 365),
            token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e62', 'artifacts 2027, us', 'qdr_bst_W8mr', 28, 367),
          ],
        },
      ),
      healthy('dmz-bonn', '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a61', [contact('quader-bastion-4c6d8e9f7-b2n5k', '192.0.2.72', 'https://packages.dmz-bonn.example.com', 7)], {
        disabled: soft(3),
        state: 'disableUnconfirmed',
      }),
      healthy('dmz-cologne', '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a62', [contact('quader-bastion-8e9f7c6d5-c4l7p', '192.0.2.73', 'https://packages.dmz-cologne.example.com', 0.1)], {
        disabled: { mode: 'hard', at: at(-2 / 24) },
        state: 'disabled',
        status: [bucketReport('quader-bastion-8e9f7c6d5-c4l7p', 0, { disabled: 'hard', running: 0 })],
      }),
      healthy('dmz-leipzig', '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a63', [contact('quader-bastion-9f7e6d5c4-l3p8z', '192.0.2.74', 'https://packages.dmz-leipzig.example.com', 0.1)], {
        disabled: soft(0.7),
        state: 'disabling',
        running: 3,
        status: [bucketReport('quader-bastion-9f7e6d5c4-l3p8z', 9_000_000, { disabled: 'soft', running: 3 })],
      }),
      healthy('partners', '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a64', [contact('quader-bastion-7d9c5b8f6-p4r7t', '203.0.113.7', 'https://pkgs.partner.example.com', 1)], {
        description: 'The partner network',
        declared: { since: at(-2), secretName: 'quader-bastion-token' },
        tokens: [{ ...token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e64', 'Bastion partners (declared in the configuration)', 'qdr_bst_Lm4c', 2, 0), expiresAt: null, declared: true }],
      }),
    ],
  };
}

/**
 * **Admin area › Bastions** with several Bastions (W-41, R-UI-240): the page as it opens (every card folded to its head but `dmz-bonn`,
 * whose disable is not confirmed); `artifacts` opened, the healthy card whose default lines W-41's 3.1 counts; its **Disable…** dialog with
 * the two ways, cancelled; the declared `partners`' **Delete** tapped, its reason pinned (R-UI-270). Locators are the markup's: the walk
 * renders in German too. The viewer's folds are cleared first, so every run opens on the page's rule. Nothing is saved.
 */
export async function* bastionStates(page: Page): AsyncGenerator<string> {
  const clearFolds = (): Promise<void> =>
    page.evaluate(() => {
      for (const k of Object.keys(localStorage)) if (k.startsWith('quader.admin.bastions.open.')) localStorage.removeItem(k);
    });
  await clearFolds();
  await page.route('**/api/v1/bastions', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(stateRegistrations()) }),
  );
  await answerProjects(page);
  await page.reload();
  await page.waitForLoadState('networkidle');
  const cards = page.locator('main quader-bastion-card');
  await expect(cards).toHaveCount(stateRegistrations().items.length);
  const named = (name: string) => cards.filter({ has: page.locator('[data-testid="bastion-name"]', { hasText: new RegExp(`^${name}$`) }) });
  const fold = (name: string) => named(name).locator('.head button.fold');
  await expect(fold('artifacts')).toHaveAttribute('aria-expanded', 'false');
  await expect(fold('dmz-bonn')).toHaveAttribute('aria-expanded', 'true');
  yield '';
  await fold('artifacts').click();
  await expect(fold('artifacts')).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'open';
  await named('artifacts').locator('.head .row-actions button').first().click();
  const dialog = page.locator('quader-bastion-disable-dialog');
  await expect(dialog.locator('input[type="radio"]')).toHaveCount(2);
  yield 'disable';
  await dialog.locator('.dialog-actions button').first().click();
  await expect(dialog).toHaveCount(0);
  const remove = named('partners').locator('[data-testid="bastion-delete"]');
  await remove.scrollIntoViewIfNeeded();
  // Playwright holds an aria-disabled control for not enabled and waits; the tap a person makes reaches it.
  await remove.click({ force: true });
  await expect(page.locator('.cdk-overlay-container quader-tooltip')).toBeVisible();
  yield 'declared';
  await page.keyboard.press('Escape');
  await clearFolds();
  await page.unroute('**/api/v1/bastions');
  await page.unroute(projectsUrl);
}

/** A project a Bastion allows (R-BAS-134), allowed `days` ago by `by`, with the repositories whose Owner chose the Bastion. */
function allowed(project: string, displayName: string, days: number, repositories: readonly string[], by = 'root'): Json {
  return { project, displayName, allowedAt: at(-days), allowedBy: by, repositories };
}

/**
 * The twelve projects `artifacts` allows (W-41's 3.8): one with seven repositories distributed (the cell names five and counts the rest),
 * two with none chosen yet, display names long and short.
 */
const twelveProjects: readonly Json[] = [
  allowed('customer-portal', 'Customer portal', 40, ['web-app', 'npm-components', 'images']),
  allowed('data-science', 'Data science and machine learning platform', 30, []),
  allowed('developer-tools', 'Developer tools', 28, ['cli', 'homebrew-tap']),
  allowed('documentation', 'Documentation', 21, ['site'], 'priya'),
  allowed('identity', 'Identity', 14, ['images', 'helm-charts']),
  allowed('mobile', 'Mobile apps', 12, []),
  allowed('observability', 'Observability', 9, ['dashboards', 'agents', 'helm-charts', 'images']),
  allowed('payments', 'Payments', 7, ['sdk-java', 'sdk-dotnet']),
  allowed('platform', 'Platform engineering', 2, ['image-source', 'npm-open-source', 'nuget-internal', 'helm-charts', 'terraform-modules', 'ansible-collections', 'base-images']),
  allowed('search', 'Search', 2, ['images']),
  allowed('security-tooling', 'Security tooling', 1, ['scanners', 'policies']),
  allowed('upstream', 'Upstream', 1, ['npmjs', 'clamav']),
];

/**
 * Answers a Bastion's projects in the browser: `bastion` the first `shown` of the twelve, every other Bastion none; the registrations a
 * walk answers do not exist in the stack, whose read of their projects answers 404.
 */
async function answerProjects(page: Page, bastion = '', shown = 0): Promise<void> {
  await page.route(
    projectsUrl,
    (route) => {
      const name = /\/bastions\/([^/]+)\/projects$/.exec(new URL(route.request().url()).pathname)?.[1] ?? '';
      const items = name === bastion ? twelveProjects.slice(0, shown) : [];
      const repositories = items.reduce((n, p) => n + (p['repositories'] as unknown[]).length, 0);
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items, nextCursor: null, totals: { projects: items.length, repositories } }),
      });
    },
  );
}

/**
 * The card's section **Projects** (W-41's C2, R-UI-240, Q-UI-1637): `artifacts`, which allows two projects, and `dmz-hamburg`, which allows
 * none, each card opened with its section opened (`two`, `empty`); then the search of **Allow a project** with the projects of the stack
 * that match "pla" (`search`). Locators are the markup's: the walk renders in German too. Nothing is saved.
 */
export async function* bastionProjects(page: Page): AsyncGenerator<string> {
  await page.evaluate(() => {
    for (const k of Object.keys(localStorage)) if (k.startsWith('quader.admin.bastions.open.')) localStorage.removeItem(k);
  });
  const registrations = stateRegistrations().items.filter((b) => b['name'] === 'artifacts');
  registrations.push(healthy('dmz-hamburg', '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a65', [contact('quader-bastion-3b5c7d9e8-h6k2v', '192.0.2.75', 'https://packages.dmz-hamburg.example.com', 1)]));
  await page.route('**/api/v1/bastions', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: registrations }) }));
  await answerProjects(page, 'artifacts', 2);
  await page.reload();
  await page.waitForLoadState('networkidle');
  const cards = page.locator('main quader-bastion-card');
  await expect(cards).toHaveCount(2);
  const settled = (): Promise<unknown> => page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  for (const [i, state] of [[0, 'two'], [1, 'empty']] as const) {
    const card = cards.nth(i);
    const fold = card.locator('.head button.fold');
    await fold.click();
    await expect(fold).toHaveAttribute('aria-expanded', 'true');
    const section = card.locator('[data-testid="bastion-projects"] button[aria-expanded]');
    await section.click();
    await expect(section).toHaveAttribute('aria-expanded', 'true');
    await settled();
    await card.locator('[data-testid="bastion-projects"]').scrollIntoViewIfNeeded();
    yield state;
    await section.click();
    await fold.click();
    await expect(fold).toHaveAttribute('aria-expanded', 'false');
  }
  const artifacts = cards.nth(0);
  await artifacts.locator('.head button.fold').click();
  await artifacts.locator('[data-testid="bastion-projects"] button[aria-expanded]').click();
  await settled();
  const field = artifacts.locator('[data-testid="bastion-projects-allow"] input[role="combobox"]');
  await field.scrollIntoViewIfNeeded();
  await field.fill('pla');
  await expect(page.locator('quader-principal-field [role="listbox"]:not([aria-busy])')).toBeVisible();
  yield 'search';
  await page.keyboard.press('Escape');
  await page.evaluate(() => {
    for (const k of Object.keys(localStorage)) if (k.startsWith('quader.admin.bastions.open.')) localStorage.removeItem(k);
  });
  await page.unroute('**/api/v1/bastions');
  await page.unroute(projectsUrl);
}

/** **Projects of dmz-berlin** (Q-UI-1637): the page of the seed's Bastion with the twelve projects it allows, answered in the browser. */
export async function* bastionProjectsPage(page: Page): AsyncGenerator<string> {
  await answerProjects(page, seededBastion.name, twelveProjects.length);
  await page.reload();
  await expect(page.locator('main quader-bastion-projects tbody tr')).toHaveCount(twelveProjects.length);
  yield '';
}

/**
 * **Download limits of dmz-berlin** (R-UI-241): the rules of the seed's Bastion, the preset's five first and the address's rule after them,
 * then the tab **Blocks** with the two blocks its replica reported. Locators are the markup's: the screen renders in German too.
 */
export async function* bastionLimitsPage(page: Page): AsyncGenerator<string> {
  reportBastionState();
  await page.reload();
  await expect(page.locator('main quader-limit-rules tbody tr').first()).toBeVisible();
  yield '';
  await page.locator('main .limits-nav a').nth(1).click();
  await expect(page.locator('main quader-bastion-blocks-tab tbody tr').first()).toBeVisible();
  yield 'blocks';
}

/** The registrations and the connections' states as the walk wants them, then the page loaded again. */
async function answer(page: Page, frankfurtOn: boolean, connections: readonly Json[]): Promise<void> {
  const list = '**/api/v1/bastions';
  const settings = '**/api/v1/download-links/settings';
  await page.unroute(list);
  await page.route(list, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(registrations(frankfurtOn)) }));
  await page.unroute(projectsUrl);
  await answerProjects(page);
  await page.unroute(settings);
  await page.route(settings, async (route) => {
    const res = await route.fetch();
    await route.fulfill({ response: res, body: JSON.stringify({ ...((await res.json()) as Json), connections }) });
  });
  await page.reload();
  await page.waitForLoadState('networkidle');
  await expect(page.locator('main quader-bastion-card')).toHaveCount(registrations(false).items.length);
}

/**
 * **Admin area › Bastions** (R-BAS-007, R-BAS-071): as the stack answers (the seed's registration, its token, one replica that
 * reported its base URL); no registration at all; two registrations with their replicas and tokens, `partners` with the direct reads off and
 * `dmz-frankfurt-edge` with them on while every connection can be read (the switch and its sentence alone); the same with three
 * connections a Bastion cannot read, an encrypted one among them, each line with its reason, and the link to the storage settings, while
 * the encrypted one it reads with each file's key has no line.
 */
export async function* bastionsHandover(page: Page): AsyncGenerator<string> {
  await expect(page.getByRole('heading', { name: 'Bastions', level: 1 })).toBeVisible();
  // The seed's registration; the profile's own Bastion (`make ui-dev-bastion`, registered as `ui-dev` by the render run) may stand beside it.
  await expect(page.locator('main quader-bastion-card', { hasText: seededBastion.name })).toBeVisible();
  yield '';
  await page.route('**/api/v1/bastions', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [] }) }));
  await page.reload();
  await expect(page.locator('main quader-empty-state')).toBeVisible();
  yield 'empty';
  await answer(page, true, readable);
  await expect(page.getByRole('switch', { name: 'Direct reads from the object store for dmz-frankfurt-edge' })).toHaveAttribute('aria-checked', 'true');
  await expect(page.locator('main [data-testid="bastion-declared"]')).toBeVisible();
  yield 'switch';
  await answer(page, true, blocked);
  await expect(page.locator('main [data-testid="bastion-handover-reasons"]')).toBeVisible();
  yield 'reasons';
  await page.unroute('**/api/v1/bastions');
  await page.unroute('**/api/v1/download-links/settings');
  await page.unroute(projectsUrl);
}

/**
 * The auditor (`endpoints:read`, `settings:read`): the state as a word, no switch, and the reasons of a registration whose reads are on;
 * then the limits as text, the details of `dmz-frankfurt-edge` opened (R-UI-240).
 */
export async function* bastionsRead(page: Page): AsyncGenerator<string> {
  await answer(page, true, blocked);
  await expect(page.locator('main [data-testid="bastion-handover-reasons"]')).toBeVisible();
  yield 'reasons';
  const details = page.locator('main quader-bastion-card').first().locator('quader-bastion-limits quader-disclosure.details button[aria-expanded]').first();
  await details.click();
  await expect(details).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'details';
  await page.unroute('**/api/v1/bastions');
  await page.unroute('**/api/v1/download-links/settings');
  await page.unroute(projectsUrl);
}

/** The traces of data keys handed to a Bastion (storage.md R-STOR-096) as the main installation writes them, with the files' paths. */
function handed(id: string, minutesAgo: number, path: string, sha256: string, replica: string): Json {
  return {
    id,
    occurredAt: at(-minutesAgo / 1440),
    action: 'bastion.data_key.handed',
    category: 'content',
    outcome: 'success',
    actor: { type: 'user', id: '0192f0b1-2c3d-7e4f-8a5b-6c7d8e9f0a11', name: 'priya', tokenId: '0192f0b1-2c3d-7e4f-8a5b-6c7d8e9f0a12', authMethod: 'bearer' },
    source: { ip: '2001:db8:85a3::8a2e:370:7334', userAgent: 'quader-bastion/0.42.0' },
    traceId: '4317e1747b20e23b669f9b71ee8b4cc5',
    replica: 'quader-packages-7f6d9c8b5-x2k4q',
    endpoint: 'default',
    resource: { type: 'bastion', id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a40' },
    repository: 'web-frontend/images',
    path,
    reason: null,
    details: {
      bastion: 'dmz-frankfurt-edge',
      replica,
      sha256,
      connection: 'archive',
      storageKeyId: 'sk-archive-2-9b1d77e0',
      linkExpiresAt: at(-minutesAgo / 1440 + 60 / 86_400),
    },
  };
}

/**
 * **Admin area › System › Audit log** with the traces of data keys handed to a Bastion (R-STOR-096), answered in the browser: the render
 * profile has no encrypted connection and no Bastion. The list with two such events beside a switch of the direct reads: the label "Data
 * key of a file handed to a Bastion" and the file. The opened event is not a state of the walk: at 390 px the page's facts break its time
 * over two lines, a defect of the audit log at that width that is not this event's (reported with the join of the handover's data keys),
 * which the walk would put into every run of the job ui-layout; the opened event was looked at by hand.
 */
export async function* auditDataKeys(page: Page): AsyncGenerator<string> {
  const first = handed(
    '01a2072f-9651-714c-a946-b356166142b7', 2, 'portal/api/_blobs/sha256:8bd5b2fc80a85cae4f043abaf3c194acdd15788099478c26cf1df6ac40e4dfc3',
    '8bd5b2fc80a85cae4f043abaf3c194acdd15788099478c26cf1df6ac40e4dfc3', 'quader-bastion-dmz-frankfurt-edge-partners-eu1-6f8b9c7d4f-x2k4q',
  );
  const items = [
    first,
    handed(
      '01a2072f-9674-73ab-bc4b-abf89fa119e8', 3, 'portal/web/_blobs/sha256:08e0c1f337c4349064ac7608662d853bfdfbc4ae09e5f56efeb51b4ed0c53a93',
      '08e0c1f337c4349064ac7608662d853bfdfbc4ae09e5f56efeb51b4ed0c53a93', 'quader-bastion-dmz-frankfurt-edge-partners-eu1-6f8b9c7d4f-m8v2j',
    ),
    {
      ...first,
      id: '01a2072f-960a-77b9-85fb-2c5df71a485e',
      occurredAt: at(-30 / 1440),
      action: 'bastion.handover.enabled',
      category: 'identity',
      source: { ip: '198.51.100.21', userAgent: 'Mozilla/5.0' },
      repository: null,
      path: null,
      details: {},
    },
  ];
  // The log reads its rows (R-OPS-055): each of these events is a row of its own.
  const isList = (url: URL): boolean => url.pathname.endsWith('/api/v1/audit/rows');
  const rows = items.map((event) => ({ event, group: null }));
  await page.route(isList, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: rows, nextCursor: null }) }),
  );
  await page.reload();
  await page.waitForLoadState('networkidle');
  await expect(page.getByRole('button', { name: 'Data key of a file handed to a Bastion' }).first()).toBeVisible();
  yield '';
  await page.unroute(isList);
}
