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

/**
 * The name of the drop-in a stock `freshclam` unit reads its environment from (R-CLAMAV-006), in
 * `/etc/systemd/system/clamav-freshclam.service.d/` on most distributions (the unit's name is the distribution's), which the hint names.
 */
export const FRESHCLAM_DROP_IN = 'quader.conf';

/** The way with a client certificate: the line of `freshclam.conf` and the drop-in of the systemd unit, two files. */
export interface FreshclamCertificate {
  /** `PrivateMirror <the endpoint that admits certificates>`. */
  readonly conf: string;
  /** The drop-in with `FRESHCLAM_CLIENT_CERT`, `FRESHCLAM_CLIENT_KEY` and `CURL_CA_BUNDLE`. */
  readonly dropIn: string;
}

/**
 * The way with a client certificate (R-CLAMAV-006, R-CLAMAV-010), as the format's guide writes it: the line that names the endpoint which
 * admits certificates, and the drop-in of the systemd unit with the certificate, its key and the authority of the endpoint's pair, which
 * `freshclam.conf` has no option for. The files are examples; nothing of the certificate is a value of Quader.
 */
export function freshclamCertificate(certificateAddress: string): FreshclamCertificate {
  const trimmed = certificateAddress.replace(/\/+$/, '');
  return {
    conf: `PrivateMirror ${trimmed}\n`,
    dropIn: [
      '[Service]',
      'Environment=FRESHCLAM_CLIENT_CERT=/etc/clamav/tls/client.crt',
      'Environment=FRESHCLAM_CLIENT_KEY=/etc/clamav/tls/client.key',
      'Environment=CURL_CA_BUNDLE=/etc/pki/quader-ca.pem',
      '',
    ].join('\n'),
  };
}

/** The file name the plain download's script is shown under (an example; R-CLAMAV-006). */
export const DOWNLOAD_SCRIPT = 'update-clamav.sh';

/**
 * The plain download of the three databases for an installation without `freshclam` (R-CLAMAV-006, Q-CLAMAV-14), as the format's guide
 * writes it: from the fixed addresses that always serve the version in use, into the database directory, then `clamd` reloads. With a
 * token, curl reads the header from standard input, so no log and no process list shows it (Q-GENERIC-9); with a client certificate it
 * names the certificate, its key and the authority, and the address of the endpoint that admits certificates.
 */
export function plainDownload(address: string, way: 'public' | 'token' | 'certificate'): string {
  const base = address.replace(/\/+$/, '');
  const fetch: Record<typeof way, string[]> = {
    public: [`  curl -fsS -o "$db.cvd.part" "${base}/$db.cvd" && mv "$db.cvd.part" "$db.cvd"`],
    token: [
      `  curl -fsS --config - -o "$db.cvd.part" "${base}/$db.cvd" <<EOF && mv "$db.cvd.part" "$db.cvd"`,
      'header = "Authorization: Bearer ${QUADER_TOKEN}"',
      'EOF',
    ],
    certificate: [
      '  curl -fsS --cert /etc/clamav/tls/client.crt --key /etc/clamav/tls/client.key --cacert /etc/pki/quader-ca.pem \\',
      `    -o "$db.cvd.part" "${base}/$db.cvd" && mv "$db.cvd.part" "$db.cvd"`,
    ],
  };
  return [
    '#!/bin/sh',
    "# example; the directory is the distribution's",
    'set -e',
    'cd /var/lib/clamav',
    'for db in main daily bytecode; do',
    ...fetch[way],
    'done',
    'clamdscan --reload',
    '',
  ].join('\n');
}
