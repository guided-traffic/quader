import { freshclamCertificate, freshclamLines, plainDownload } from './clamav-setup';

describe('The one line of freshclam.conf (R-CLAMAV-006)', () => {
  it('R-CLAMAV-006 writes the address with its scheme and without its trailing slash, and the token as the password of the address', () => {
    expect(freshclamLines('https://quader.example.com/api/clamav/upstream/clamav/')).toEqual({
      plain: 'PrivateMirror https://quader.example.com/api/clamav/upstream/clamav',
      token: 'PrivateMirror https://clamav:${QUADER_TOKEN}@quader.example.com/api/clamav/upstream/clamav',
    });
    expect(freshclamLines('')).toEqual({ plain: '', token: '' });
  });

  it('R-CLAMAV-006 R-CLAMAV-010 the certificate variant names the endpoint and the two variables and the CA, and holds no key', () => {
    const out = freshclamCertificate('https://clamav.quader.example.com/api/clamav/upstream/clamav/');
    expect(out.conf).toBe('PrivateMirror https://clamav.quader.example.com/api/clamav/upstream/clamav\n');
    expect(out.dropIn).toContain('Environment=FRESHCLAM_CLIENT_CERT=/etc/clamav/tls/client.crt');
    expect(out.dropIn).toContain('Environment=FRESHCLAM_CLIENT_KEY=/etc/clamav/tls/client.key');
    expect(out.dropIn).toContain('Environment=CURL_CA_BUNDLE=/etc/pki/quader-ca.pem');
    expect(out.conf + out.dropIn).not.toMatch(/BEGIN|PRIVATE/);
  });

  it('R-CLAMAV-006 the plain download fetches the three files: without a credential, with the token on standard input, with a certificate', () => {
    const base = 'https://quader.example.com/api/clamav/upstream/clamav';
    expect(plainDownload(base + '/', 'public')).toContain(`curl -fsS -o "$db.cvd.part" "${base}/$db.cvd" && mv "$db.cvd.part" "$db.cvd"`);
    const token = plainDownload(base, 'token');
    expect(token).toContain('--config -');
    expect(token).toContain('header = "Authorization: Bearer ${QUADER_TOKEN}"');
    expect(token.match(/QUADER_TOKEN/g)).toHaveLength(1);
    expect(plainDownload(base, 'certificate')).toContain('--cert /etc/clamav/tls/client.crt --key /etc/clamav/tls/client.key --cacert /etc/pki/quader-ca.pem');
    for (const way of ['public', 'token', 'certificate'] as const) {
      expect(plainDownload(base, way)).toContain('for db in main daily bytecode; do');
      expect(plainDownload(base, way)).toContain('clamdscan --reload');
    }
  });
});
