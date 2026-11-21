import { malwareDatabase } from './malware';
import { lit, psql } from './seed';

/**
 * The scanner replicas of the renders (R-OPS-048, Q-UI-897): two ready image scanners, one of them busy; two ready malware scanners
 * with the signature database in use; and one malware scanner of a release with a long name that waits for its first database, with
 * the longest pod name the screens meet. The profile runs no scanner pod (its role all scans images in its own process and has no
 * malware scanner), so the rows are written into its database (`psql`), as a scanner's report through the scan listener writes them.
 * Their heartbeat lies a day ahead: the render database is shared, and a run takes longer than system.heartbeat_stale_after, after
 * which a row of today would be listed as lost. "No scanner" is the real answer changed in the browser by the walks.
 */

interface Row {
  readonly uid: string;
  readonly kind: 'trivy' | 'clamav';
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
];

/** Writes the rows, replacing whatever a run before left. */
export function seedScannerReplicas(): void {
  const text = (s: string | undefined): string => (s === undefined ? 'NULL' : lit(s));
  const values = scannerRows.map(
    (r) =>
      `(${lit(r.uid)}, ${lit(r.kind)}, ${lit(r.name)}, ${lit(r.node)}, 'dev', 'renders', ${lit(r.state)}, ${text(r.reason)}, ` +
      `now() - interval '${r.since} minutes', ${lit(r.tool)}, ${text(r.database)}, ${r.inUse}, ${r.slots}, ` +
      `now() - interval '${r.since} minutes', now() + interval '1 day')`,
  );
  psql(
    [
      'BEGIN;',
      'DELETE FROM scanner_replica;',
      `INSERT INTO scanner_replica (instance_uid, kind, name, node, version, revision, state, reason, state_since, tool_version, db_version,
                                   slots_in_use, slots_total, started_at, heartbeat_at)
       VALUES ${values.join(',\n              ')};`,
      'COMMIT;',
    ].join('\n'),
  );
}
