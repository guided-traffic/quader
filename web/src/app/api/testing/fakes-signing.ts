import { Injectable, Provider } from '@angular/core';
import { ApiError } from '../problem';
import {
  SigningApi,
  SigningCertificateUpload,
  SigningIdentity,
  SigningIdentityImport,
  SigningKeyRequest,
  SigningOverview,
} from '../signing-api';

const DAY = 86_400_000;
/** A moment `days` from now, as the API writes it. */
export const daysFromNow = (days: number): string => new Date(Date.now() + days * DAY).toISOString();

/** A certificate request of a P-256 key for the company's subject, as `openssl req` writes it. */
export const SIGNING_CSR = `-----BEGIN CERTIFICATE REQUEST-----
MIIBhzCCAS4CAQAwZjEXMBUGA1UEAwwOUXVhZGVyIHNpZ25pbmcxHzAdBgNVBAoM
Fk5vcmR3aW5kIExvZ2lzdGlrIEdtYkgxHTAbBgNVBAsMFFBsYXRmb3JtIEVuZ2lu
ZWVyaW5nMQswCQYDVQQGEwJERTBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABJXU
grWD3jgTR9j7FdRx9rQxjwgfD29fao5KlfQvaCZIYaXca39Jow+Sb4S/XcFDhl4k
/PSuauQMRGdF7ThWkzSgZjBkBgkqhkiG9w0BCQ4xVzBVMC4GA1UdEQQnMCWGI2h0
dHBzOi8vcXVhZGVyLm5vcmR3aW5kLWxvZ2lzdGlrLmRlMA4GA1UdDwEB/wQEAwIH
gDATBgNVHSUEDDAKBggrBgEFBQcDAzAKBggqhkjOPQQDAgNHADBEAiBSF/C8p7kv
lXtX6YpKJYdTGC0D+2SRMXiPZIlRYfoCyQIgWRtAoNSJeeUCv+8tVtfJGVTSTkPf
wrEbyRdAYBN1QNo=
-----END CERTIFICATE REQUEST-----
`;

/** The current identity of an upload: a code-signing certificate of the company's intermediate CA, valid for a year from now. */
export function signingIdentity(over: Partial<SigningIdentity> = {}): SigningIdentity {
  return {
    id: 3,
    source: 'upload',
    state: 'current',
    subject: 'CN=Quader signing,O=Nordwind Logistik GmbH,C=DE',
    issuer: 'CN=Nordwind Code Signing CA 2026,O=Nordwind Logistik GmbH,C=DE',
    names: ['https://quader.nordwind-logistik.de'],
    notBefore: daysFromNow(-30),
    notAfter: daysFromNow(335),
    fingerprint: '8ab2bd4682527e32b5c568365fabc54476a3d52b3f6393468135139cad32636a',
    npmKeyId: 'SHA256:ppEfO0+zms4YTC+AuPEyWvx63580lKut8w80mgHwf0A',
    keySha256: 'c3'.repeat(32),
    csr: null,
    createdAt: daysFromNow(-31),
    createdBy: 'user:ada',
    announcedAt: null,
    currentAt: daysFromNow(-30),
    switchAt: null,
    expiresAt: null,
    retiredAt: null,
    ...over,
  };
}

/** A pending key with its request (R-SIG-047). */
export function pendingIdentity(over: Partial<SigningIdentity> = {}): SigningIdentity {
  return signingIdentity({
    id: 7,
    state: 'pending',
    subject: 'CN=Quader signing,OU=Platform Engineering,O=Nordwind Logistik GmbH,C=DE',
    issuer: '',
    notBefore: null,
    notAfter: null,
    fingerprint: '',
    npmKeyId: 'SHA256:7/H0KjW795YpM9mdW5QTPUsCyMRBbOwhHqPCM7ioij4',
    csr: SIGNING_CSR,
    createdAt: daysFromNow(-0.01),
    currentAt: null,
    ...over,
  });
}

/** The overview of an installation without an identity: signing on, nothing from the Helm values, nothing refused. */
export function signingOverview(over: Partial<SigningOverview> = {}): SigningOverview {
  return {
    state: 'none',
    enabled: true,
    managed: false,
    announcePeriodSeconds: 600,
    current: null,
    announced: null,
    pending: null,
    previous: [],
    refused: null,
    backlog: [
      { kind: 'npm', due: 0, failed: 0, oldestDueAt: null },
      { kind: 'attestation', due: 0, failed: 0, oldestDueAt: null },
    ],
    ...over,
  };
}

/**
 * The signing identity in memory (R-SIG-055): a key request makes the pending key, the certificate installs it as current (or
 * announced beside a current one), a removal makes the current one previous, a retirement drops a previous one. While `managed` the
 * writes answer `409 signing_identity_managed`, as the server does. Every write is recorded; `failure` makes the next write fail.
 */
@Injectable()
export class FakeSigningApi extends SigningApi {
  current: SigningOverview = signingOverview();
  failure: ApiError | null = null;
  readonly calls: { op: string; body?: unknown }[] = [];

  private fail(): void {
    const f = this.failure;
    if (f) {
      this.failure = null;
      throw f;
    }
    if (this.current.managed) throw new ApiError(409, { code: 'signing_identity_managed', requestId: 'rid-signing-409' }, 'rid-signing-409');
  }

  override async overview(): Promise<SigningOverview> {
    return structuredClone(this.current);
  }

  override async createKey(request: SigningKeyRequest): Promise<SigningIdentity> {
    this.calls.push({ op: 'createKey', body: request });
    this.fail();
    const pending = pendingIdentity();
    this.current = { ...this.current, pending };
    return structuredClone(pending);
  }

  override async uploadCertificate(upload: SigningCertificateUpload): Promise<SigningIdentity> {
    this.calls.push({ op: 'uploadCertificate', body: upload });
    this.fail();
    return this.install();
  }

  override async importIdentity(material: SigningIdentityImport): Promise<SigningIdentity> {
    this.calls.push({ op: 'importIdentity', body: { ...material, key: '(the key)' } });
    this.fail();
    return this.install();
  }

  private install(): SigningIdentity {
    const announced = this.current.current !== null;
    const made = signingIdentity({ id: 8, state: announced ? 'announced' : 'current', csr: null });
    this.current = announced
      ? { ...this.current, pending: null, announced: made }
      : { ...this.current, state: 'upload', pending: null, current: made };
    return structuredClone(made);
  }

  override async discardPending(): Promise<void> {
    this.calls.push({ op: 'discardPending' });
    this.fail();
    this.current = { ...this.current, pending: null };
  }

  override async remove(): Promise<void> {
    this.calls.push({ op: 'remove' });
    this.fail();
    const was = this.current.current;
    if (!was) return;
    this.current = { ...this.current, state: 'none', current: null, previous: [{ ...was, state: 'previous', expiresAt: new Date().toISOString() }, ...this.current.previous] };
  }

  override async retire(id: number): Promise<SigningIdentity> {
    this.calls.push({ op: 'retire', body: { id } });
    const f = this.failure;
    if (f) {
      this.failure = null;
      throw f;
    }
    const was = this.current.previous.find((p) => p.id === id);
    if (!was) throw new ApiError(404, { code: 'not_found', requestId: 'rid-signing-404' }, 'rid-signing-404');
    this.current = { ...this.current, previous: this.current.previous.filter((p) => p.id !== id) };
    return { ...was, state: 'retired', retiredAt: new Date().toISOString() };
  }
}

export function provideSigningFakes(): Provider[] {
  return [{ provide: SigningApi, useClass: FakeSigningApi }];
}
