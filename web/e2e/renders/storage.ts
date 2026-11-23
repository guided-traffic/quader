/**
 * The object-store connections of the tab Storage and the dashboard (api-and-ui.md R-UI-196, R-UI-197), answered in the browser:
 * the render profile runs one connection, so every state the tab can show with several (filling, draining, detached, unreachable,
 * damaged copies and a backlog behind) is an answer of the walks, from the real one's members.
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
    keyedNames: false,
    drain: false,
    replicas: { configured: 3, live: 3, circuitOpen: 0 },
    queue: { blobs: 0, oldestQueuedAt: null },
    summary: figures(id),
    scrubPeriodSeconds: 2_592_000,
    filledAt: at(60 * 24 * 9),
    drainedAt: null,
    retiredAt: null,
    lastFullPassAt: at(41),
    capabilities: {
      payloadChecksumEnforced: true,
      batchDelete: true,
      metadata: true,
      copyReplacesMetadata: true,
      serverSideCopyFrom: {},
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
        queue: { blobs: 38, oldestQueuedAt: at(95) },
        summary: figures('aws-frankfurt-backup-copy', { presentCopies: 412_052, missingCopies: 3, corruptCopies: 1, backlogBlobs: 41, backlogBytes: 18_874_368, fillRatio: 0.9999, verifiedWithinPeriod: 377_002, scrubOverdue: 1_208 }),
        settings: settings({ region: 'eu-central-1', storageClass: 'STANDARD_IA', sseMode: 'kms', scrubBytesPerSecond: 104_857_600 }),
        capabilities: { payloadChecksumEnforced: true, batchDelete: true, metadata: true, copyReplacesMetadata: false, serverSideCopyFrom: {}, checkedAt: at(60 * 7) },
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
        summary: figures('archive', { presentCopies: 201_377, presentBytes: 902_212_812_800, backlogBlobs: 210_716, fillRatio: 0.4886, soleCopies: 12, verifiedWithinPeriod: 0, scrubOverdue: 201_377 }),
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

/** The dashboard's facts with two connections of the answering replica (R-UI-197). */
export function twoConnectionFacts(): (body: Json) => Json {
  return (b) => ({
    ...b,
    objectStore: {
      connections: [
        { id: 'default', endpoint: 'https://object.storage.eu01.onstackit.cloud', bucket: 'quader-blobs', prefix: 'quader', credentialSource: 'static', encrypted: false, drain: false },
        { id: 'aws-frankfurt-backup-copy', endpoint: '', bucket: 'acme-quader-artifacts-eu-central-1', prefix: 'production/quader', credentialSource: 'webIdentity', encrypted: false, drain: false },
      ],
    },
  });
}

/** The connections' state behind the dashboard's two lines: the second unreachable on every replica. */
export function secondUnreachable(): (body: Json) => Json {
  return () => ({
    connections: [
      connection('default'),
      connection('aws-frankfurt-backup-copy', { position: 2, endpoint: '', bucket: 'acme-quader-artifacts-eu-central-1', prefix: 'production/quader', replicas: { configured: 3, live: 3, circuitOpen: 3 } }),
    ],
  });
}
