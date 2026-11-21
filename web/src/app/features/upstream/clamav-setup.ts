/** The file `freshclam` reads its mirror from, with the packages of most distributions (R-CLAMAV-006). */
export const FRESHCLAM_CONF = '/etc/clamav/freshclam.conf';

/** The one line of `freshclam.conf`, without and with a token as the password of the address (R-CLAMAV-006). */
export interface FreshclamLines {
  /** `PrivateMirror <address>`: the plain path at the level Public. */
  readonly plain: string;
  /** `PrivateMirror https://clamav:${QUADER_TOKEN}@<host>/<path>`: a level below Public; empty for an address that is no URL. */
  readonly token: string;
}

/**
 * The line that points a stock `freshclam` at the endpoint (R-CLAMAV-006), as the format's snippet writes it
 * (`internal/formats/clamav/setup.go`): the address with its scheme and without its trailing slash, which `freshclam` adds itself
 * (Q-CLAMAV-16), and, for a level below Public, a Quader token as the password of the address with any user name. The admin page shows
 * both from the item's `clients.address`, so it needs no read of the client setup; the repository's **Set up client** is the full
 * guide. The token is the variable `QUADER_TOKEN`, never a value.
 */
export function freshclamLines(address: string): FreshclamLines {
  const trimmed = address.replace(/\/+$/, '');
  const at = trimmed.indexOf('://');
  return {
    plain: trimmed === '' ? '' : `PrivateMirror ${trimmed}`,
    token: at < 0 ? '' : `PrivateMirror ${trimmed.slice(0, at + 3)}clamav:\${QUADER_TOKEN}@${trimmed.slice(at + 3)}`,
  };
}
