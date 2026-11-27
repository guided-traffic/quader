/**
 * The stored content of Settings › Storage and the dashboard's card (api-and-ui.md R-UI-262 to R-UI-266), answered in the browser: the
 * render profile runs one connection, so every state the page can show with several (filling, draining, detached, unreachable,
 * encrypted, nearly full, damaged copies and a backlog behind) is an answer of the walks, from the real one's members, with the
 * storage overview the server would derive for it.
 */

const at = (minutes: number): string => new Date(Date.now() - minutes * 60_000).toISOString();
const summaryAt = at(23);

type Json = Record<string, unknown>;

function figures(id: string, over: Json = {}): Json {
  return {
    id,
    computedAt: summaryAt,
    presentCopies: 412_093,
    presentBytes: 1_893_341_184_512,
    missingCopies: 0,
    corruptCopies: 0,
    verifiedWithinPeriod: 409_870,
    scrubOverdue: 0,
    backlogBlobs: 0,
    backlogBytes: 0,
    fillRatio: 1,
    soleCopies: 0,
    soleBytes: 0,
    keyedCopies: 0,
    ...over,
  };
}

function settings(over: Json = {}): Json {
  return {
    region: 'eu01',
    forcePathStyle: false,
    payloadChecksum: 'sha256',
    requestChecksumCalculation: 'when_supported',
    responseChecksumValidation: 'when_supported',
    batchDelete: true,
    storageClass: '',
    sseMode: 'none',
    sseKmsKeyId: '',
    sseBucketKey: true,
    scrubBytesPerSecond: 52_428_800,
    ...over,
  };
}

function connection(id: string, over: Json = {}): Json {
  return {
    id,
    position: 1,
    state: 'active',
    endpoint: 'https://object.storage.eu01.onstackit.cloud',
    bucket: 'quader-blobs',
    prefix: 'quader',
    credentialSource: 'static',
    encrypted: false,
    encryptedSince: null,
    keyedNames: false,
    drain: false,
    capacityBytes: null,
    replicas: { configured: 3, live: 3, circuitOpen: 0 },
    queue: { blobs: 0, oldestQueuedAt: null },
    summary: figures(id),
    scrubPeriodSeconds: 2_592_000,
    firstSeenAt: at(60 * 24 * 10),
    filledAt: at(60 * 24 * 9),
    drainedAt: null,
    retiredAt: null,
    passStartedAt: null,
    lastFullPassAt: at(41),
    capabilities: {
      payloadChecksumEnforced: true,
      batchDelete: true,
      metadata: true,
      copyReplacesMetadata: true,
      serverSideCopyFrom: {},
      sseApplied: { algorithm: '', kmsKeyId: '' },
      checkedAt: at(60 * 7),
    },
    settings: settings(),
    storageKeys: [],
    ...over,
  };
}

/** One connection as a default installation shows it once the summary has run: `default`, active, every blob on it. */
export function oneConnection(): (body: Json) => Json {
  return (b) => {
    const real = ((b['connections'] as Json[] | undefined) ?? [])[0] ?? {};
    return {
      connections: [
        connection('default', {
          endpoint: real['endpoint'] ?? 'https://object.storage.eu01.onstackit.cloud',
          bucket: real['bucket'] ?? 'quader-blobs',
          prefix: real['prefix'] ?? 'quader',
          summary: figures('default', { presentCopies: 18_342, presentBytes: 7_816_329_216, verifiedWithinPeriod: 18_342 }),
        }),
      ],
    };
  };
}

/**
 * Six connections in every state: `default` active, `stackit-copy` filling at 63 % with a queue whose oldest waits 7 minutes,
 * `aws-frankfurt-backup-copy` (a long id) unreachable on 1 of 3 replicas with damaged copies and a backlog behind for over an hour,
 * `minio-old` draining, `ceph-2024` drained and configured on 2 of 3 replicas (a rolling update), `archive` detached with blobs only
 * it holds; `legacy` retired.
 */
export function severalConnections(): (body: Json) => Json {
  return () => ({
    connections: [
      connection('default', {
        capabilities: { payloadChecksumEnforced: true, batchDelete: true, metadata: true, copyReplacesMetadata: true, serverSideCopyFrom: { 'stackit-copy': true }, checkedAt: at(60 * 7) },
      }),
      connection('stackit-copy', {
        position: 2,
        state: 'filling',
        bucket: 'quader-blobs-copy',
        filledAt: null,
        lastFullPassAt: null,
        queue: { blobs: 1_204, oldestQueuedAt: at(7) },
        summary: figures('stackit-copy', { presentCopies: 259_618, presentBytes: 1_192_804_946_042, backlogBlobs: 152_475, backlogBytes: 700_536_238_470, fillRatio: 0.63, verifiedWithinPeriod: 259_618 }),
        capabilities: { payloadChecksumEnforced: true, batchDelete: true, metadata: true, copyReplacesMetadata: true, serverSideCopyFrom: { default: true }, checkedAt: at(60 * 7) },
      }),
      connection('aws-frankfurt-backup-copy', {
        position: 3,
        endpoint: '',
        bucket: 'acme-quader-artifacts-eu-central-1',
        prefix: 'production/quader',
        credentialSource: 'webIdentity',
        replicas: { configured: 3, live: 3, circuitOpen: 1 },
        capacityBytes: 1_960_000_000_000,
        queue: { blobs: 38, oldestQueuedAt: at(95) },
        summary: figures('aws-frankfurt-backup-copy', { presentCopies: 412_052, missingCopies: 3, corruptCopies: 1, backlogBlobs: 41, backlogBytes: 18_874_368, fillRatio: 0.9999, verifiedWithinPeriod: 377_002, scrubOverdue: 1_208 }),
        settings: settings({ region: 'eu-central-1', storageClass: 'STANDARD_IA', sseMode: 'kms', scrubBytesPerSecond: 104_857_600 }),
        capabilities: {
          payloadChecksumEnforced: true, batchDelete: true, metadata: true, copyReplacesMetadata: false, serverSideCopyFrom: {},
          sseApplied: { algorithm: 'AES256', kmsKeyId: '' }, checkedAt: at(60 * 7),
        },
      }),
      connection('minio-old', {
        position: 4,
        state: 'draining',
        drain: true,
        endpoint: 'https://minio.internal.example:9000',
        bucket: 'quader',
        settings: settings({ region: 'us-east-1', forcePathStyle: true, payloadChecksum: 'md5', requestChecksumCalculation: 'when_required', responseChecksumValidation: 'when_required', batchDelete: false }),
        summary: figures('minio-old', { presentCopies: 98_211, presentBytes: 401_216_839_680, backlogBlobs: 313_882, fillRatio: 0.2383 }),
        capabilities: { payloadChecksumEnforced: false, batchDelete: false, metadata: true, copyReplacesMetadata: true, serverSideCopyFrom: {}, checkedAt: at(60 * 7) },
      }),
      connection('ceph-2024', {
        position: 5,
        state: 'drained',
        drain: true,
        endpoint: 'https://rgw.ceph.internal.example',
        bucket: 'quader',
        replicas: { configured: 2, live: 3, circuitOpen: 0 },
        drainedAt: at(60 * 30),
        summary: figures('ceph-2024', { presentCopies: 0, presentBytes: 0, verifiedWithinPeriod: 0, backlogBlobs: 412_093, backlogBytes: 1_893_341_184_512, fillRatio: 0 }),
      }),
      connection('archive', {
        position: null,
        state: 'detached',
        endpoint: null,
        credentialSource: null,
        drain: null,
        scrubPeriodSeconds: null,
        settings: null,
        bucket: 'quader-archive',
        replicas: { configured: 0, live: 3, circuitOpen: 0 },
        summary: figures('archive', { presentCopies: 201_377, presentBytes: 902_212_812_800, backlogBlobs: 210_716, fillRatio: 0.4886, soleCopies: 12, soleBytes: 52_428_800, verifiedWithinPeriod: 0, scrubOverdue: 201_377 }),
      }),
      connection('legacy', { position: null, state: 'retired', retiredAt: at(60 * 24 * 40), summary: null, settings: null, endpoint: null, credentialSource: null, drain: null }),
    ],
  });
}

/** Three blobs no connection holds whole and more, one corrupt, one referenced only by a trashed item, one by many items. */
export function damagedBlobs(): (body: Json) => Json {
  const copy = (connection: string, state: string): Json => ({ connection, state, verifiedAt: at(60 * 24 * 12), checkedAt: at(31), stateChangedAt: at(31) });
  return () => ({
    items: [
      {
        sha256: '3a7bd3e2360a3d29eea436fcfb7e44c735d117c42d1c1835420b6b9942dd4f1b', size: 18_432, contentStatus: 'missing', itemCount: 1,
        copies: [copy('default', 'missing'), copy('aws-frankfurt-backup-copy', 'missing')],
        items: [{ project: 'web-frontend', repository: 'npm-internal', path: '@acme/build-helpers/-/build-helpers-2.4.1.tgz', trashed: false }],
      },
      {
        sha256: '9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08', size: 5_242_880, contentStatus: 'corrupt', itemCount: 14,
        copies: [copy('default', 'corrupt'), copy('aws-frankfurt-backup-copy', 'missing')],
        items: [{ project: 'customer-portal-release-delivery', repository: 'signed-release-artifacts', path: 'releases/2026.11/customer-portal-release-delivery-2026.11.3-linux-amd64.tar.gz', trashed: false }],
      },
      {
        sha256: 'b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9', size: 911, contentStatus: 'missing', itemCount: 1,
        copies: [copy('default', 'missing')],
        items: [{ project: 'platform', repository: 'generic-local', path: 'docs/old-notes.txt', trashed: true }],
      },
    ],
    nextCursor: 'b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9',
  });
}

/** No blob is damaged. */
export function noDamagedBlob(): (body: Json) => Json {
  return () => ({ items: [], nextCursor: null });
}

const TIB = 1024 ** 4;
const GIB = 1024 ** 3;

/** The period the page asked the real overview for, so that a fixture's tile names the period the page shows. */
function realPeriod(b: Json): string {
  return ((b['problems'] as Json | undefined)?.['period'] as string | undefined) ?? '30d';
}

/**
 * The storage overview (R-STOR-107) of one store as a default installation answers it once the summary has run: healthy, one store,
 * no encryption, every copy read whole within its period.
 */
export function overviewOne(): (body: Json) => Json {
  return (b) => ({
    at: new Date().toISOString(),
    figuresAt: summaryAt,
    connections: { state: 'healthy', configured: 1, reachable: 1, unreachable: [], detached: [] },
    stored: { blobs: 18_342, bytes: 7_816_329_216 },
    availability: {
      state: 'one_store', target: 1, minCopies: 1,
      perBlob: [{ copies: 1, blobs: 18_342, bytes: 7_816_329_216 }],
      belowTarget: { blobs: 0, bytes: 0 },
      loss: [{ connection: 'default', blobs: 18_342, bytes: 7_816_329_216 }],
    },
    encryption: { state: 'off', encrypted: [], plain: ['default'], providerConfigured: [], providerApplied: [], encryptedShare: 0 },
    integrity: {
      state: 'verified', downloadVerify: true, authenticatedReads: [],
      scrub: { coverage: 1, overdue: 0, lastRunAt: at(32), nextRunAt: at(-28), running: false },
      damagedNow: { copies: 0, blobs: 0 },
    },
    problems: { period: realPeriod(b), state: 'none', count: 0, open: 0, acknowledged: 0, worst: null },
  });
}

/**
 * The plan's two connections: `default` plain and nearly full (1.7 of 2 TiB) with the only copy of 1,234 blobs, its store applying
 * nothing asked; `archive` encrypted by Quader since its first key, filling, unreachable on 1 of 3 replicas, asking its store for kms,
 * with an active and a retired storage key and no capacity stated.
 */
export function twoForThePage(): (body: Json) => Json {
  return () => ({
    connections: [
      connection('default', {
        capacityBytes: 2 * TIB,
        capabilities: {
          payloadChecksumEnforced: true, batchDelete: true, metadata: true, copyReplacesMetadata: true, serverSideCopyFrom: {},
          sseApplied: { algorithm: 'AES256', kmsKeyId: '' }, checkedAt: at(60 * 7),
        },
        summary: figures('default', { presentCopies: 412_093, presentBytes: 1_869_169_767_219, soleCopies: 1_234, soleBytes: 12 * GIB, verifiedWithinPeriod: 412_093 }),
        passStartedAt: at(14),
      }),
      connection('archive', {
        position: 2,
        state: 'filling',
        endpoint: 'https://s3.eu-central-1.amazonaws.com',
        bucket: 'quader-archive-eu-central-1',
        prefix: 'production/quader',
        credentialSource: 'webIdentity',
        encrypted: true,
        encryptedSince: at(60 * 24 * 4 + 37),
        keyedNames: true,
        filledAt: null,
        lastFullPassAt: null,
        passStartedAt: at(14),
        replicas: { configured: 3, live: 3, circuitOpen: 1 },
        queue: { blobs: 1_234, oldestQueuedAt: at(4) },
        settings: settings({ region: 'eu-central-1', sseMode: 'kms', sseKmsKeyId: 'alias/quader-archive', storageClass: 'STANDARD_IA' }),
        capabilities: {
          payloadChecksumEnforced: true, batchDelete: true, metadata: true, copyReplacesMetadata: true, serverSideCopyFrom: {},
          sseApplied: { algorithm: 'aws:kms', kmsKeyId: 'alias/quader-archive' }, checkedAt: at(60 * 7),
        },
        summary: figures('archive', { presentCopies: 410_859, presentBytes: 1_856_284_865_331, backlogBlobs: 1_234, backlogBytes: 12 * GIB, fillRatio: 0.997, keyedCopies: 410_859, verifiedWithinPeriod: 398_112 }),
        storageKeys: [
          { id: 'sk-archive-2-7f3c91', generation: 2, state: 'active', createdAt: at(60 * 24 * 2), retiredAt: null, destroyedAt: null, copies: 352_008 },
          { id: 'sk-archive-1-2b84e0', generation: 1, state: 'retired', createdAt: at(60 * 24 * 4 + 37), retiredAt: at(60 * 24 * 2), destroyedAt: null, copies: 58_851 },
        ],
      }),
    ],
  });
}

/** The overview of `twoForThePage`: degraded, at risk, partly encrypted, the scrub still checking the new copies. */
export function overviewTwo(): (body: Json) => Json {
  return (b) => ({
    at: new Date().toISOString(),
    figuresAt: summaryAt,
    connections: { state: 'degraded', configured: 2, reachable: 1, unreachable: [{ id: 'archive', circuitOpen: 1, configured: 3 }], detached: [] },
    stored: { blobs: 412_093, bytes: 1_869_169_767_219 },
    availability: {
      state: 'at_risk', target: 2, minCopies: 1,
      perBlob: [{ copies: 2, blobs: 410_859, bytes: 1_856_284_865_331 }, { copies: 1, blobs: 1_234, bytes: 12 * GIB }],
      belowTarget: { blobs: 1_234, bytes: 12 * GIB },
      loss: [{ connection: 'default', blobs: 1_234, bytes: 12 * GIB }],
    },
    encryption: { state: 'partly', encrypted: ['archive'], plain: ['default'], providerConfigured: ['archive'], providerApplied: ['default', 'archive'], encryptedShare: 0.498 },
    integrity: {
      state: 'checking', downloadVerify: true, authenticatedReads: ['archive'],
      scrub: { coverage: 0.984, overdue: 0, lastRunAt: at(32), nextRunAt: at(-28), running: false },
      damagedNow: { copies: 0, blobs: 0 },
    },
    problems: {
      period: realPeriod(b), state: 'ongoing', count: 3, open: 0, acknowledged: 0,
      worst: { kind: 'unreachable', connection: 'archive', startedAt: at(60 * 24 * 7 + 75), seconds: 4_320, tone: 'warning' },
    },
  });
}

/** The overview of `severalConnections`: a detached connection, damaged copies, blobs without a whole copy and copies overdue. */
export function overviewSeveral(): (body: Json) => Json {
  return (b) => ({
    at: new Date().toISOString(),
    figuresAt: summaryAt,
    connections: {
      state: 'degraded', configured: 4, reachable: 3,
      unreachable: [{ id: 'aws-frankfurt-backup-copy', circuitOpen: 1, configured: 3 }], detached: ['archive'],
    },
    stored: { blobs: 412_093, bytes: 1_893_341_184_512 },
    availability: {
      state: 'at_risk', target: 3, minCopies: 2,
      perBlob: [
        { copies: 3, blobs: 259_577, bytes: 1_192_616_071_270 },
        { copies: 2, blobs: 152_500, bytes: 700_554_000_000 },
        { copies: 1, blobs: 12, bytes: 52_428_800 },
        { copies: 0, blobs: 4, bytes: 5_262_225 },
      ],
      belowTarget: { blobs: 152_516, bytes: 700_611_691_025 },
      loss: [{ connection: 'archive', blobs: 12, bytes: 52_428_800 }, { connection: 'default', blobs: 3, bytes: 911_000 }],
    },
    encryption: { state: 'off', encrypted: [], plain: ['default', 'stackit-copy', 'aws-frankfurt-backup-copy', 'minio-old', 'archive'], providerConfigured: ['aws-frankfurt-backup-copy'], providerApplied: ['aws-frankfurt-backup-copy'], encryptedShare: 0 },
    integrity: {
      state: 'damaged', downloadVerify: true, authenticatedReads: [],
      scrub: { coverage: 0.81, overdue: 202_585, lastRunAt: at(32), nextRunAt: at(-28), running: false },
      damagedNow: { copies: 4, blobs: 3 },
    },
    problems: {
      period: realPeriod(b), state: 'ongoing', count: 4, open: 2, acknowledged: 1,
      worst: { kind: 'copies_damaged', connection: 'default', startedAt: at(31), seconds: 1_860, tone: 'error' },
    },
  });
}

/** The cells of a period: the real answer's period and times, with a strip per connection and the cells `cells` names changed. */
export function historyFor(
  ids: readonly string[],
  cells: Readonly<Record<string, Readonly<Record<number, Json>>>> = {},
  findings: Readonly<Record<string, Json>> = {},
): (body: Json) => Json {
  return (b) => {
    const bucketSeconds = (b['bucketSeconds'] as number | undefined) ?? 86_400;
    const to = Date.parse((b['to'] as string | undefined) ?? new Date().toISOString());
    const n = Math.round((to - Date.parse((b['from'] as string | undefined) ?? new Date(to - 30 * 86_400_000).toISOString())) / (bucketSeconds * 1000));
    const from = to - n * bucketSeconds * 1000;
    // A cell named counted from the end: -1 is the last cell, so a fixture means the same day whatever the period's length.
    const cellOf = (id: string, i: number): Json => cells[id]?.[i] ?? cells[id]?.[i - n] ?? {};
    return {
      ...b,
      connections: ids.map((id) => ({
        id,
        buckets: Array.from({ length: n }, (_, i) => {
          const over = cellOf(id, i);
          return {
            start: new Date(from + i * bucketSeconds * 1000).toISOString(),
            tone: 'none', unreachableSeconds: 0, replicas: 0, startChecksFailed: 0, damagedCopies: 0, damagedBytes: 0, unavailableBlobs: 0,
            readFallovers: 0, readsUnavailable: 0, writeFallovers: 0, uploadsRefused: 0, copiesFailed: 0, readsRefused: 0, unknownKeyReads: 0,
            ...over,
          };
        }),
        findings: { count: 0, bytes: 0, unlisted: 0, byOutcome: {}, bySource: {}, ...findings[id] },
      })),
    };
  };
}

/** A list answered whole: the items, no next page. */
export function listOf(items: readonly Json[]): (body: Json) => Json {
  return () => ({ items, nextCursor: null });
}

function problem(over: Json): Json {
  return {
    kind: 'unreachable', connection: 'archive', startedAt: at(60 * 24 * 7 + 75), endedAt: at(60 * 24 * 7 + 3), seconds: 4_320, tone: 'warning',
    replicas: 3, occurrences: 4, count: 0, bytes: 0, resolved: 0, open: 0, acknowledged: 0, unavailable: 0, codes: ['timeout'], sources: [], steps: [],
    ...over,
  };
}

function finding(over: Json): Json {
  return {
    id: 4182, detectedAt: at(60 * 24 * 5 + 12), connection: 'default', sha256: '5be0c1d2a3f4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e',
    status: 'corrupt', source: 'scrub', reason: 'digest', storedSize: 1_153_433, unavailable: false, resolvedAt: at(60 * 24 * 5 + 11),
    resolution: 'rewritten', acknowledged: null,
    items: [{ project: 'web-frontend', repository: 'npm-internal', path: 'lodash/-/lodash-4.17.21.tgz', trashed: false }], itemCount: 3,
    ...over,
  };
}

/** The two connections' period: an outage of archive on 3 replicas a week ago, three copies of default the scrub repaired. */
export function historyTwo(): (body: Json) => Json {
  return historyFor(
    ['default', 'archive'],
    {
      default: { [-6]: { tone: 'warning', damagedCopies: 3, damagedBytes: 3_250_585 } },
      archive: {
        [-8]: { tone: 'warning', unreachableSeconds: 4_320, replicas: 3, readFallovers: 310, writeFallovers: 41, copiesFailed: 12 },
        [-1]: { tone: 'warning', unreachableSeconds: 600, replicas: 1, readFallovers: 18 },
      },
    },
    {
      default: { count: 3, bytes: 3_250_585, byOutcome: { rewritten: 3 }, bySource: { scrub: 3 } },
    },
  );
}

export function problemsTwo(): (body: Json) => Json {
  return listOf([
    problem({ startedAt: at(10), endedAt: null, seconds: 600, replicas: 1, occurrences: 2, codes: ['http_5xx'] }),
    problem({ kind: 'copies_damaged', connection: 'default', startedAt: at(60 * 24 * 5 + 12), endedAt: at(60 * 24 * 5 + 11), seconds: 64, tone: 'warning',
      replicas: 0, occurrences: 0, count: 3, bytes: 3_250_585, resolved: 3, codes: ['digest'], sources: ['scrub'] }),
    problem({}),
  ]);
}

export function findingsTwo(): (body: Json) => Json {
  return listOf([
    finding({}),
    finding({ id: 4181, detectedAt: at(60 * 24 * 5 + 12), sha256: '0a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9', storedSize: 1_048_576, itemCount: 1,
      items: [{ project: 'platform', repository: 'generic-local', path: 'tools/jq-1.7.1-linux-amd64', trashed: false }] }),
    finding({ id: 4180, detectedAt: at(60 * 24 * 5 + 13), sha256: 'f0e1d2c3b4a5968778695a4b3c2d1e0f0e1d2c3b4a5968778695a4b3c2d1e0f', storedSize: 1_048_576, itemCount: 0, items: [] }),
  ]);
}

/** The six connections' period: refused uploads and an open damaged copy no other copy held, outages and a failing start check. */
export function historySeveral(): (body: Json) => Json {
  return historyFor(
    ['default', 'stackit-copy', 'aws-frankfurt-backup-copy', 'minio-old', 'ceph-2024', 'archive'],
    {
      default: { [-1]: { tone: 'error', damagedCopies: 3, damagedBytes: 5_262_225, unavailableBlobs: 2, readsUnavailable: 14 } },
      'aws-frankfurt-backup-copy': {
        [-1]: { tone: 'error', unreachableSeconds: 5_400, replicas: 1, uploadsRefused: 2, readFallovers: 1_208, writeFallovers: 96 },
        [-2]: { tone: 'warning', startChecksFailed: 1 },
        [-12]: { tone: 'warning', readsRefused: 12, unknownKeyReads: 12 },
      },
      'minio-old': { [-20]: { tone: 'warning', copiesFailed: 3 } },
    },
    {
      default: { count: 3, bytes: 5_262_225, byOutcome: { open: 2, acknowledged: 1 }, bySource: { download: 2, scrub: 1 } },
      'aws-frankfurt-backup-copy': { count: 1, bytes: 911, byOutcome: { found_again: 1 }, bySource: { read: 1 } },
    },
  );
}

export function problemsSeveral(): (body: Json) => Json {
  return listOf([
    problem({ kind: 'copies_damaged', connection: 'default', startedAt: at(31), endedAt: null, seconds: 1_860, tone: 'error', replicas: 0, occurrences: 0,
      count: 3, bytes: 5_262_225, open: 2, acknowledged: 1, unavailable: 2, codes: ['not_found', 'digest'], sources: ['download', 'scrub'] }),
    problem({ kind: 'reads_unavailable', connection: 'default', startedAt: at(40), endedAt: null, seconds: 2_400, tone: 'error', replicas: 0, occurrences: 0,
      count: 14, bytes: 74_448, codes: ['not_found'] }),
    problem({ kind: 'uploads_refused', connection: 'aws-frankfurt-backup-copy', startedAt: at(60), endedAt: at(0), seconds: 3_600, tone: 'error', replicas: 0,
      occurrences: 0, count: 2, bytes: 18_874_368, codes: ['timeout'] }),
    problem({ connection: 'aws-frankfurt-backup-copy', startedAt: at(90), endedAt: null, seconds: 5_400, replicas: 1, occurrences: 7, codes: ['timeout', 'http_5xx'] }),
    problem({ kind: 'start_check_failed', connection: 'aws-frankfurt-backup-copy', startedAt: at(60 * 26), endedAt: at(60 * 25 + 40), seconds: 1_200,
      replicas: 1, occurrences: 1, codes: ['access_denied'], steps: ['write'] }),
  ]);
}

export function findingsSeveral(): (body: Json) => Json {
  return listOf([
    finding({ id: 4190, detectedAt: at(31), sha256: '3a7bd3e2360a3d29eea436fcfb7e44c735d117c42d1c1835420b6b9942dd4f1b', status: 'missing', source: 'download',
      reason: 'not_found', storedSize: 18_432, unavailable: true, resolvedAt: null, resolution: null, itemCount: 1,
      items: [{ project: 'web-frontend', repository: 'npm-internal', path: '@acme/build-helpers/-/build-helpers-2.4.1.tgz', trashed: false }] }),
    finding({ id: 4189, detectedAt: at(33), sha256: '9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08', source: 'scrub', storedSize: 5_242_880,
      unavailable: true, resolvedAt: null, resolution: null, itemCount: 14,
      items: [{ project: 'customer-portal-release-delivery', repository: 'signed-release-artifacts', path: 'releases/2026.11/customer-portal-release-delivery-2026.11.3-linux-amd64.tar.gz', trashed: false }] }),
    finding({ id: 4188, detectedAt: at(35), sha256: 'b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9', status: 'missing', source: 'download',
      reason: 'not_found', storedSize: 911, unavailable: false, resolvedAt: null, resolution: null, itemCount: 1,
      acknowledged: { at: at(20), by: { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10', name: 'priya.raman' }, note: 'A note in the trash; it may go.' },
      items: [{ project: 'platform', repository: 'generic-local', path: 'docs/old-notes.txt', trashed: true }] }),
  ]);
}
