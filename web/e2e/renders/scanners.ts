import { malwareDatabase } from './malware';
import { lit, psql } from './seed';

/**
 * The scanner replicas of the renders (R-OPS-048, Q-UI-897): two ready image scanners, one of them busy, that hold no database yet;
 * two ready malware scanners with the signature database in use; and one malware scanner of a release with a long name that waits for its
 * first database, with the longest pod name the screens meet; and the malware scanning's updater, which runs no calls. The profile runs no
 * scanner pod (its role all builds none), so the rows are written into its database (`psql`), as a scanner's report through the scan
 * listener writes them; the database each image scanner holds is answered in the browser (`trivyDatabases`).
 * Their heartbeat lies a day ahead: the render database is shared, and a run takes longer than system.heartbeat_stale_after, after
 * which a row of today would be listed as lost. "No scanner" is the real answer changed in the browser by the walks.
 */

interface Row {
  readonly uid: string;
  readonly kind: 'trivy' | 'clamav' | 'freshclam';
  readonly name: string;
  readonly node: string;
  readonly state: 'starting' | 'ready' | 'unready';
  readonly reason?: string;
  /** Minutes since the state began. */
  readonly since: number;
  readonly tool: string;
  readonly database?: string;
  readonly inUse: number;
  readonly slots: number;
  /** Hours since the build of the newest daily the updater's directory holds (`freshclam` only). */
  readonly dailyBuilt?: number;
}

export const scannerRows: readonly Row[] = [
  { uid: '5c0f8a52-1d4e-4b7a-9f3c-2e6d7a8b9c01', kind: 'trivy', name: 'quader-vuln-scanner-trivy-7d9f6c5b8-abcde', node: 'pool-a-7x2k', state: 'ready', since: 190, tool: '0.67.2', inUse: 1, slots: 1 },
  { uid: '5c0f8a52-1d4e-4b7a-9f3c-2e6d7a8b9c02', kind: 'trivy', name: 'quader-vuln-scanner-trivy-7d9f6c5b8-fghij', node: 'pool-b-3m9q', state: 'ready', since: 188, tool: '0.67.2', inUse: 0, slots: 1 },
  { uid: '5c0f8a52-1d4e-4b7a-9f3c-2e6d7a8b9c03', kind: 'clamav', name: 'quader-malware-scanner-clamav-6d9c7b5f4-x2v4q', node: 'pool-a-7x2k', state: 'ready', since: 300, tool: '1.5.5', database: malwareDatabase, inUse: 1, slots: 2 },
  { uid: '5c0f8a52-1d4e-4b7a-9f3c-2e6d7a8b9c04', kind: 'clamav', name: 'quader-malware-scanner-clamav-6d9c7b5f4-k8m2p', node: 'pool-b-3m9q', state: 'ready', since: 298, tool: '1.5.5', database: malwareDatabase, inUse: 0, slots: 2 },
  {
    uid: '5c0f8a52-1d4e-4b7a-9f3c-2e6d7a8b9c05', kind: 'clamav', name: 'customer-portal-release-delivery-malware-scanner-clamav-5c8d7f9b6d-q7w3z',
    node: 'pool-c-8n4r', state: 'starting', reason: 'database', since: 4, tool: '1.5.5', inUse: 0, slots: 2,
  },
  // The updater (R-MAL-032, Q-UI-913): no slots, the set its directory holds and the build time of its daily.
  {
    uid: '5c0f8a52-1d4e-4b7a-9f3c-2e6d7a8b9c06', kind: 'freshclam', name: 'quader-malware-updater-freshclam-7c9f5d6b8-m4k2t',
    node: 'pool-a-7x2k', state: 'ready', since: 290, tool: '1.5.5', database: 'main-63.daily-28190.bytecode-339', inUse: 0, slots: 0, dailyBuilt: 5,
  },
];

/**
 * The image scanners' pods in each state of the database they hold (R-VULN-049, Q-OPS-225, Q-UI-915), as the walks answer the list in the
 * browser: the list sets a row's build time only while its digest is the database in use, which the render profile, fetching no database,
 * never has. `mixed`: one pod on the database in use (busy), one on an older database, one ready pod without a database yet and one that
 * starts; `current`: every pod on the database in use. The rows of the other kinds stay as the profile answered them.
 */
export function trivyDatabases(mode: 'mixed' | 'current'): (body: Record<string, unknown>) => Record<string, unknown> {
  const inUse = { version: '3f2a9c0d1e7b5a4c8d6e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c', builtAt: new Date(Date.now() - 5 * 3_600_000).toISOString() };
  const older = { version: '8b1d7e3c9a2f4d6b0e5c1a7f3d9b2e8c4a6f0d1b7e3c9a5f2d8b4e0c6a1f7d3b', builtAt: null };
  const at = (minutes: number): string => new Date(Date.now() - minutes * 60_000).toISOString();
  const pod = (suffix: string, node: string, over: Record<string, unknown>): Record<string, unknown> => ({
    kind: 'trivy', instanceUid: `5c0f8a52-1d4e-4b7a-9f3c-2e6d7a8b9d${suffix}`, name: `quader-vuln-scanner-trivy-7d9f6c5b8-${suffix}xyz`, node,
    version: 'dev', revision: 'renders', state: 'ready', reason: null, stateSince: at(190), lost: false, startedAt: at(190),
    heartbeatAt: new Date().toISOString(), toolVersion: '0.67.2', slots: { inUse: 0, total: 1 }, database: inUse, ...over,
  });
  const pods =
    mode === 'current'
      ? [pod('01', 'pool-a-7x2k', { slots: { inUse: 1, total: 1 } }), pod('02', 'pool-b-3m9q', {})]
      : [
          pod('01', 'pool-a-7x2k', { slots: { inUse: 1, total: 1 } }),
          pod('02', 'pool-b-3m9q', { database: older }),
          pod('03', 'pool-c-8n4r', { database: null, stateSince: at(2), startedAt: at(2) }),
          pod('04', 'pool-a-7x2k', { state: 'starting', reason: 'starting', toolVersion: null, database: null, stateSince: at(0), startedAt: at(0) }),
        ];
  return (b) => ({ ...b, items: [...pods, ...(b['items'] as Record<string, unknown>[]).filter((r) => r['kind'] !== 'trivy')] });
}

/** Writes the rows, replacing whatever a run before left. */
export function seedScannerReplicas(): void {
  const text = (s: string | undefined): string => (s === undefined ? 'NULL' : lit(s));
  const values = scannerRows.map(
    (r) =>
      `(${lit(r.uid)}, ${lit(r.kind)}, ${lit(r.name)}, ${lit(r.node)}, 'dev', 'renders', ${lit(r.state)}, ${text(r.reason)}, ` +
      `now() - interval '${r.since} minutes', ${lit(r.tool)}, ${text(r.database)}, ${r.inUse}, ${r.slots}, ` +
      `now() - interval '${r.since} minutes', now() + interval '1 day', ` +
      `${r.dailyBuilt === undefined ? 'NULL' : `now() - interval '${r.dailyBuilt} hours'`})`,
  );
  psql(
    [
      'BEGIN;',
      'DELETE FROM scanner_replica;',
      `INSERT INTO scanner_replica (instance_uid, kind, name, node, version, revision, state, reason, state_since, tool_version, db_version,
                                   slots_in_use, slots_total, started_at, heartbeat_at, db_built_at)
       VALUES ${values.join(',\n              ')};`,
      'COMMIT;',
    ].join('\n'),
  );
}

/**
 * The image scanning as the profile's backend of the renders has it, which runs no image scanner and fetches no database
 * (`UI_SCANNERS=0`, Q-ARCH-1064): no database for images, and every image's scan waiting, as on a database no scanner has seen. A
 * backend that once ran `vuln-scanner-trivy` on the instance (`make ui-dev-backend` runs it) left its database recorded and its scans
 * done, and the image pages read a grade with no findings instead of **No database**: the seed puts both back, so a screen shows the
 * same whichever backend the instance ran before. The states a list shows of a scan are answered in the browser (scan-states.ts).
 */
export function resetImageScans(): void {
  psql(
    [
      'BEGIN;',
      `UPDATE vuln_image_scan SET state = 'pending', not_before = now(), attempts = 0, db_digest = NULL, scanned_at = NULL, error_code = NULL,
         error_detail = NULL, judged = NULL, started_at = NULL, os_family = NULL, os_name = NULL, os_eosl = false, packages = 0,
         db_built_at = NULL, tool_version = NULL
       WHERE state <> 'pending' OR db_digest IS NOT NULL OR started_at IS NOT NULL;`,
      `DELETE FROM vuln_source_state WHERE source = 'trivy';`,
      'COMMIT;',
    ].join('\n'),
  );
}
