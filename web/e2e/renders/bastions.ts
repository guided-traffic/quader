import { Page, expect } from '@playwright/test';
import { Api, expectStatus, psql } from './seed';

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

function contact(replica: string, address: string, baseUrl: string, minutesAgo: number): Json {
  return { replica, contactAt: at(-minutesAgo / 1440), clientAddress: address, release: '0.42.0', interfaceVersion: 6, baseUrl };
}

function token(id: string, description: string, hint: string, createdDays: number, expiresDays: number): Json {
  return { id, hint, description, createdAt: at(-createdDays), expiresAt: at(expiresDays), lastUsedAt: at(-0.002), declared: false };
}

/**
 * `partners`, off, with short values (a pod name of 31 characters, a base URL of 28, an IPv4 address), and `dmz-frankfurt-edge`, the
 * longest name of the rule but two, on, with long ones: a pod name of 63 characters (the most Kubernetes allows), a base URL of 51, an
 * IPv6 address of 39, a token that expires in 20 days beside one of a year. The tables keep one form at a width whatever they hold
 * (Q-UI-959). The addresses each Bastion connects from (R-UI-243): `partners` lists none, `dmz-frankfurt-edge` the /64 of one replica
 * and not the IPv4 address of the other.
 */
function registrations(frankfurtOn: boolean): Json {
  return {
    items: [
      {
        id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a40',
        name: 'dmz-frankfurt-edge',
        description: 'The DMZ of the Frankfurt data centre, for the build agents of our partners',
        enabled: true,
        declared: null,
        switches: { handover: frankfurtOn, gui: false, docs: false },
        // The /64 its IPv6 replica leaves from; the other replica came from an IPv4 address it does not list (R-UI-243).
        egress: ['2001:db8:85a3::/64'],
        createdAt: at(-120),
        tokens: [
          token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e43', 'frankfurt 2027, rolled out with the release of the build agents in January', 'qdr_bst_Xw7p', 2, 365),
          token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e42', 'frankfurt 2026', 'qdr_bst_9Hd2', 345, 20),
        ],
        contacts: [
          contact('quader-bastion-dmz-frankfurt-edge-partners-eu1-6f8b9c7d4f-x2k4q', '2001:0db8:85a3:0000:0000:8a2e:0370:7334', 'https://packages.dmz-frankfurt.partners.example.com', 1),
          contact('quader-bastion-dmz-frankfurt-edge-partners-eu1-6f8b9c7d4f-m8v2j', '198.51.100.21', 'https://packages.dmz-frankfurt.partners.example.com', 2),
        ],
      },
      {
        id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a41',
        name: 'partners',
        description: 'The partner network',
        enabled: true,
        // Declared in the Helm values (R-BAS-127, Q-UI-1497 to Q-UI-1499): the lock line with the Secret, no Delete, the networks as
        // text with the warning of its replica's unlisted address and no Add, the declared token without an expiry and without Revoke
        // beside one issued on the screen.
        declared: { since: at(-2), secretName: 'quader-bastion-token' },
        switches: { handover: false, gui: false, docs: false },
        egress: ['10.252.0.0/16'],
        createdAt: at(-170),
        tokens: [
          { ...token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e44', 'Bastion partners (declared in the configuration)', 'qdr_bst_Lm4c', 2, 0), expiresAt: null, declared: true },
          token('0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e41', 'partners 2026', 'qdr_bst_3kQ9', 165, 200),
        ],
        contacts: [contact('quader-bastion-7d9c5b8f6-p4r7t', '203.0.113.7', 'https://packages.example.com', 1)],
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
}

/** The registrations and the connections' states as the walk wants them, then the page loaded again. */
async function answer(page: Page, frankfurtOn: boolean, connections: readonly Json[]): Promise<void> {
  const list = '**/api/v1/bastions';
  const settings = '**/api/v1/download-links/settings';
  await page.unroute(list);
  await page.route(list, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(registrations(frankfurtOn)) }));
  await page.unroute(settings);
  await page.route(settings, async (route) => {
    const res = await route.fetch();
    await route.fulfill({ response: res, body: JSON.stringify({ ...((await res.json()) as Json), connections }) });
  });
  await page.reload();
  await page.waitForLoadState('networkidle');
  await expect(page.locator('main quader-bastion-card')).toHaveCount(2);
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
  await expect(page.locator('main quader-bastion-card')).toHaveCount(1);
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
}

/** The auditor (`endpoints:read`, `settings:read`): the state as a word, no switch, and the reasons of a registration whose reads are on. */
export async function* bastionsRead(page: Page): AsyncGenerator<string> {
  await answer(page, true, blocked);
  await expect(page.locator('main [data-testid="bastion-handover-reasons"]')).toBeVisible();
  yield 'reasons';
  await page.unroute('**/api/v1/bastions');
  await page.unroute('**/api/v1/download-links/settings');
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
