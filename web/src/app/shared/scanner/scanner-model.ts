import { ScannerKind, ScannerList, ScannerReplica } from '../../api/system-api';

/**
 * The scanner replicas as the screens group them (R-OPS-048, Q-OPS-223, Q-UI-897): one group per kind of scanner, the kinds of the
 * installation first in the API's order, then a kind that only a row names (its scanning was switched off and the row is not pruned
 * yet). Pure functions over the answer of `GET /api/v1/system/scanners`; they add no knowledge of the server.
 */

/** A kind of scanner with its replicas and their counts. */
export interface ScannerGroup {
  /** `trivy`, `clamav`: `process.scanner_kind`. */
  readonly kind: string;
  /** The name of the kind of pod with its tool, `vuln-scanner-trivy` (Q-HA-372). */
  readonly deployment: string;
  /** The tool's name, `Trivy`, `ClamAV`. */
  readonly tool: string;
  readonly purpose: ScannerKind['purpose'] | null;
  /** Whether the kind's replicas report themselves; null for a kind only a row names. */
  readonly replicas: ScannerKind['replicas'] | null;
  readonly rows: readonly ScannerReplica[];
  /** Rows that are not lost. */
  readonly live: number;
  /** Live rows that are ready. */
  readonly ready: number;
  /** The calls the live rows run now, and at a time. */
  readonly inUse: number;
  readonly slots: number;
}

/** The deployment of a kind the API did not describe: the rule of Q-HA-372 for the two kinds the chart renders, else the kind. */
function fallbackDeployment(kind: string): string {
  switch (kind) {
    case 'trivy':
      return 'vuln-scanner-trivy';
    case 'clamav':
      return 'malware-scanner-clamav';
    default:
      return kind;
  }
}

/** One group per kind: the installation's kinds in the API's order, then the kinds only a row names, by name. */
export function scannerGroups(list: ScannerList): ScannerGroup[] {
  const kinds: string[] = list.kinds.map((k) => k.kind);
  for (const r of list.items) if (!kinds.includes(r.kind)) kinds.push(r.kind);
  return kinds.map((kind) => {
    const meta = list.kinds.find((k) => k.kind === kind);
    const rows = list.items.filter((r) => r.kind === kind);
    const live = rows.filter((r) => !r.lost);
    return {
      kind,
      deployment: meta?.deployment ?? fallbackDeployment(kind),
      tool: meta?.tool ?? kind,
      purpose: meta?.purpose ?? null,
      replicas: meta?.replicas ?? null,
      rows,
      live: live.length,
      ready: live.filter((r) => r.state === 'ready').length,
      inUse: live.reduce((n, r) => n + r.slots.inUse, 0),
      slots: live.reduce((n, r) => n + r.slots.total, 0),
    };
  });
}

/** The group of one kind, or null while the installation neither runs nor lists it. */
export function scannerGroup(list: ScannerList, kind: string): ScannerGroup | null {
  return scannerGroups(list).find((g) => g.kind === kind) ?? null;
}

/**
 * A kind whose scanning waits (R-OPS-048): its replicas report themselves, and none of them is ready, or none has reported. The header
 * of the System page warns of it; a kind whose replicas do not report (the role all, no TLS) has nothing to warn of here.
 */
export function waitingScanners(groups: readonly ScannerGroup[]): string[] {
  return groups.filter((g) => g.replicas === 'reporting' && g.ready === 0).map((g) => g.deployment);
}
