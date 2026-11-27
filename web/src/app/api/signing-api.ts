import { Injectable, forwardRef, inject } from '@angular/core';
import { ApiClient } from './client';
import { Schema } from './types';

/** Quader's signing identity (R-SIG-045, R-SIG-055): the source of the current one, every identity by state, a refused pair of files. */
export type SigningOverview = Schema<'SigningOverview'>;
/** One signing identity, without key material (R-SIG-045). */
export type SigningIdentity = Schema<'SigningIdentity'>;
/** The last pair of the files of the Helm values that failed the checks (R-SIG-046). */
export type SigningRefusal = Schema<'SigningRefusal'>;
/** A key request (R-SIG-047): the subject and the alternative names. */
export type SigningKeyRequest = Schema<'SigningKeyRequest'>;
/** The certificate the company's CA issued for the pending key, with its chain (R-SIG-047). */
export type SigningCertificateUpload = Schema<'SigningCertificateUpload'>;
/** A key and its chain handed out together (R-SIG-047). The key is never answered. */
export type SigningIdentityImport = Schema<'SigningIdentityImport'>;

/** The public material anyone may fetch (R-SIG-052, Q-SIG-72): the current identity's chain, and the trusted root of every listed chain. */
export const SIGNING_CHAIN_URL = '/api/v1/signing/chain.pem';
export const SIGNING_TRUSTED_ROOT_URL = '/api/v1/signing/trusted-root.json';

/**
 * The operations of Quader's signing identity (R-SIG-055, tag `signatures`): the overview for `settings:read`; the key request, the
 * upload of the CA's certificate, the import, the discard of a pending key, the removal of the current uploaded identity and the
 * retirement of a previous one for `security-settings:write` with a recent sign-in (401 `reauthentication_required` otherwise).
 */
@Injectable({ providedIn: 'root', useClass: forwardRef(() => HttpSigningApi) })
export abstract class SigningApi {
  /** `GET /api/v1/signing/identity`. */
  abstract overview(): Promise<SigningOverview>;
  /** `POST /api/v1/signing/identity/key`: the pending identity with its certificate request. */
  abstract createKey(request: SigningKeyRequest): Promise<SigningIdentity>;
  /** `PUT /api/v1/signing/identity/certificate`: the installed identity, announced or current. */
  abstract uploadCertificate(upload: SigningCertificateUpload): Promise<SigningIdentity>;
  /** `POST /api/v1/signing/identity/import`. */
  abstract importIdentity(material: SigningIdentityImport): Promise<SigningIdentity>;
  /** `DELETE /api/v1/signing/identity/pending`. */
  abstract discardPending(): Promise<void>;
  /** `DELETE /api/v1/signing/identity`: the current uploaded identity becomes previous. */
  abstract remove(): Promise<void>;
  /** `POST /api/v1/signing/identities/{id}/retire`. */
  abstract retire(id: number): Promise<SigningIdentity>;
}

const BASE = '/api/v1/signing';

@Injectable()
export class HttpSigningApi extends SigningApi {
  private readonly client = inject(ApiClient);

  override async overview(): Promise<SigningOverview> {
    return (await this.client.getJson<SigningOverview>(`${BASE}/identity`)).body;
  }

  override async createKey(request: SigningKeyRequest): Promise<SigningIdentity> {
    return (await this.client.post<SigningIdentity>(`${BASE}/identity/key`, request)).body;
  }

  override async uploadCertificate(upload: SigningCertificateUpload): Promise<SigningIdentity> {
    return (await this.client.putJson<SigningIdentity>(`${BASE}/identity/certificate`, upload)).body;
  }

  override async importIdentity(material: SigningIdentityImport): Promise<SigningIdentity> {
    return (await this.client.post<SigningIdentity>(`${BASE}/identity/import`, material)).body;
  }

  override async discardPending(): Promise<void> {
    await this.client.delete(`${BASE}/identity/pending`);
  }

  override async remove(): Promise<void> {
    await this.client.delete(`${BASE}/identity`);
  }

  override async retire(id: number): Promise<SigningIdentity> {
    return (await this.client.post<SigningIdentity>(`${BASE}/identities/${id}/retire`)).body;
  }
}
