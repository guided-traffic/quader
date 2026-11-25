import { describe, it } from 'vitest';
import { SigningApi } from '../../api/signing-api';
import { expectState, failNext, fieldRefusal, fill, openDialog, serverFailure, submit } from '../../testing/states';
import { SigningCertificateDialog, SigningImportDialog, SigningKeyDialog } from './signing-dialogs';

// The dialogs of the installation's signing identity, opened as signing-identity.ts opens them.
const createKey = () =>
  openDialog(SigningKeyDialog, { maxWidth: '40rem', data: { replace: true, baseUrl: 'https://quader.platform-engineering.eu-central-1.example.com' } });
const uploadCertificate = () => openDialog(SigningCertificateDialog, { maxWidth: '44rem', data: {} });
const importIdentity = () => openDialog(SigningImportDialog, { maxWidth: '44rem' });

/** A certificate's PEM as a CA sends it: lines of 64 characters. */
const PEM = [
  '-----BEGIN CERTIFICATE-----',
  'MIIDdzCCAl+gAwIBAgIUQ3VhZGVyU2lnbmluZ0NlcnRpZmljYXRlMA0GCSqGSIb3DQEB',
  'CwUAMEsxCzAJBgNVBAYTAkRFMRswGQYDVQQKDBJQbGF0Zm9ybSBFbmdpbmVlcmluZzEf',
  '-----END CERTIFICATE-----',
].join('\n');

describe('dialogs of the signing identity in a browser (R-SIG-047, R-SIG-056, R-UI-042, R-UI-173, R-UI-175)', () => {
  it('Replace the signing key, its fields refused: each sentence under its field', async () => {
    await expectState('signing-key-invalid', async () => {
      const { panel, get } = await createKey();
      failNext(
        get(SigningApi),
        'createKey',
        fieldRefusal({
          '/subject/CN': 'The common name is longer than 64 characters: Bereitstellungsverantwortlichkeitsübertragung Platform Engineering Release Signing.',
          '/subject/C': 'A country is two upper-case letters (ISO 3166-1), not Deutschland.',
        }),
      );
      await fill(panel, 'Common name', 'Bereitstellungsverantwortlichkeitsübertragung Platform Engineering Release Signing');
      await fill(panel, 'Country', 'Deutschland');
      await submit(panel);
    });
  });

  it('Replace the signing key, failed: the error notice with its request ID', async () => {
    await expectState('signing-key-failed', async () => {
      const { panel, get } = await createKey();
      failNext(get(SigningApi), 'createKey', serverFailure());
      await submit(panel);
    });
  });

  it('Upload certificate, as it opens', async () => {
    await expectState('signing-certificate', uploadCertificate);
  });

  it('Upload certificate, its chain refused: the sentence under the field', async () => {
    await expectState('signing-certificate-invalid', async () => {
      const { panel, get } = await uploadCertificate();
      failNext(
        get(SigningApi),
        'uploadCertificate',
        fieldRefusal({ '/chain': 'The chain does not end at a root: the issuer of the last certificate, CN=Platform Engineering Intermediate CA 2026, is not in it.' }),
      );
      await fill(panel, 'Certificate and chain (PEM)', PEM);
      await submit(panel);
    });
  });

  it('Upload certificate, failed: the error notice with its request ID', async () => {
    await expectState('signing-certificate-failed', async () => {
      const { panel, get } = await uploadCertificate();
      failNext(get(SigningApi), 'uploadCertificate', serverFailure());
      await fill(panel, 'Certificate and chain (PEM)', PEM);
      await submit(panel);
    });
  });

  it('Import key and certificate, as it opens', async () => {
    await expectState('signing-import', importIdentity);
  });

  it('Import key and certificate, its key refused: the sentence under the field', async () => {
    await expectState('signing-import-invalid', async () => {
      const { panel, get } = await importIdentity();
      failNext(
        get(SigningApi),
        'importIdentity',
        fieldRefusal({
          '/key': 'The key is not PKCS#8 or SEC 1: a PKCS#12 bundle was sent.',
          '/chain': 'The certificate is for another key than the one sent.',
        }),
      );
      await fill(panel, 'Private key (PEM)', '-----BEGIN ENCRYPTED PRIVATE KEY-----');
      await fill(panel, 'Certificate and chain (PEM)', PEM);
      await submit(panel);
    });
  });

  it('Import key and certificate, failed: the error notice with its request ID', async () => {
    await expectState('signing-import-failed', async () => {
      const { panel, get } = await importIdentity();
      failNext(get(SigningApi), 'importIdentity', serverFailure());
      await fill(panel, 'Private key (PEM)', '-----BEGIN ENCRYPTED PRIVATE KEY-----');
      await fill(panel, 'Certificate and chain (PEM)', PEM);
      await submit(panel);
    });
  });
});
