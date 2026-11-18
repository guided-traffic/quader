import { InjectionToken } from '@angular/core';

/**
 * Where the vulnerability screens are mounted. The same screens (report, finding, advisory) serve the project's
 * vulnerabilities (`/projects/:project/vulnerabilities`, Q-UI-163) and the security dashboard over every project (`/security`,
 * Q-UI-130, R-UI-158); a route that mounts them provides this token, so that the links between them stay inside the place they
 * were opened in.
 */
export interface VulnContext {
  /** The report reads every project: the project is a filter of the drawer and the link of each row's repository (the dashboard). */
  readonly instanceWide: boolean;
}

export const VULN_CONTEXT = new InjectionToken<VulnContext>('VULN_CONTEXT', {
  providedIn: 'root',
  factory: () => ({ instanceWide: false }),
});

/** The context of the security dashboard (Q-UI-130). */
export const SECURITY_CONTEXT: VulnContext = { instanceWide: true };

/**
 * The router commands of a vulnerability screen below the place it is mounted at: the project's, or the dashboard's
 * when the screens are instance-wide. `project` is the route parameter of `/projects/:project/vulnerabilities`.
 */
export function vulnCommands(ctx: VulnContext, project: string | undefined, ...rest: string[]): string[] {
  return ctx.instanceWide ? ['/security', ...rest] : ['/projects', project ?? '', 'vulnerabilities', ...rest];
}

/**
 * A whole advisory identifier as it is typed or pasted into the dashboard's search (Q-UI-623), in its canonical spelling, or
 * null for any other text. The API compares identifiers and aliases exactly, the prefix of every family is upper case
 * (`CVE-2021-23337`, `MAL-2026-1234`, `OSV-2026-12`, `PYSEC-2021-1`, `GO-2022-0001`, `RUSTSEC-2021-0001`) and a GitHub
 * advisory carries its three groups of four in lower case (`GHSA-35jh-r3h4-6jhm`). A part of one (`CVE-2021`) is no
 * identifier: it stays a text that the search finds inside identifiers, aliases, names and summaries.
 */
export function advisoryIdOf(input: string): string | null {
  const text = input.trim();
  if (/^ghsa(-[0-9a-z]{4}){3}$/i.test(text)) return `GHSA-${text.slice(5).toLowerCase()}`;
  if (/^cve-\d{4}-\d{4,}$/i.test(text)) return text.toUpperCase();
  if (/^(mal|osv|pysec|go|rustsec)-\d{4}-\d+$/i.test(text)) return text.toUpperCase();
  return null;
}
