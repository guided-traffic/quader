import { SelectOption } from '../../shared/access/fields';

export type AuthMode = 'none' | 'basic' | 'bearer';

/** One further host of an address (`remote.extraHosts`, R-REPO-024). */
export interface HostRow {
  readonly pattern: string;
  readonly sendCredentials: boolean;
}

/** The three kinds of account, in the words of a remote's upstream section (R-UI-082). */
export function authOptions(): SelectOption[] {
  return [
    { value: 'none', label: $localize`:@@upstream.auth.none:None` },
    { value: 'basic', label: $localize`:@@upstream.auth.basic:Username and password` },
    { value: 'bearer', label: $localize`:@@upstream.auth.bearer:Bearer token` },
  ];
}

/** The host of a URL as a client sends it (`registry-1.docker.io`), or the text itself where it is no URL. */
export function hostOf(url: string): string {
  try {
    return new URL(url.trim()).host;
  } catch {
    return url.trim();
  }
}

/**
 * The hosts that receive an account (R-REPO-033): the URL's host and every further host with `sendCredentials`, sorted, so that
 * two addresses can be compared. A save that changes them while an account is stored asks for the account again (R-UI-188).
 */
export function credentialHostsOf(url: string, hosts: readonly HostRow[]): string[] {
  const out = new Set<string>([hostOf(url)]);
  for (const h of hosts) if (h.sendCredentials && h.pattern.trim()) out.add(h.pattern.trim());
  return [...out].filter((h) => h !== '').sort();
}

export function sameHosts(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((h, i) => h === b[i]);
}
