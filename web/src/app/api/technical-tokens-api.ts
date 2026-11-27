import { Injectable, forwardRef, inject } from '@angular/core';
import { ApiClient } from './client';
import { serviceAccountUrl } from './identity-api';
import { seg } from './query';
import { Versioned } from './repositories-api';
import { Schema } from './types';

/*
 * The technical tokens of a service account on the instance, a project or a repository (R-AUTH-045, R-UI-181;
 * docs/developer/identity.md). A technical token is named for the system that uses it and keeps its ID, its budget and its
 * history across rotations and new secrets; its secrets are `qdr_sat_` tokens, rotated and revoked through the token API
 * (`TokensApi`). A created secret carries its value once: the caller keeps it in a component signal and nowhere else (R-UI-043).
 */

export type TechnicalToken = Schema<'TechnicalToken'>;
export type TechnicalTokenCreated = Schema<'TechnicalTokenCreated'>;
export type NewTechnicalToken = Schema<'NewTechnicalToken'>;
export type NewTechnicalTokenSecret = Schema<'NewTechnicalTokenSecret'>;

/** The account a technical token belongs to: its name within its place, its project and its repository (R-AUTH-046). */
export interface AccountAddress {
  readonly name: string;
  readonly project: string | null;
  readonly repository: string | null;
}

@Injectable({ providedIn: 'root', useClass: forwardRef(() => HttpTechnicalTokensApi) })
export abstract class TechnicalTokensApi {
  abstract list(account: AccountAddress): Promise<readonly TechnicalToken[]>;
  abstract create(account: AccountAddress, request: NewTechnicalToken, idempotencyKey: string): Promise<TechnicalTokenCreated>;
  /** A new secret for a technical token without a live one (`409 technical_token_active` otherwise, Q-AUTH-416). */
  abstract newSecret(
    account: AccountAddress,
    tokenName: string,
    request: NewTechnicalTokenSecret,
    idempotencyKey: string,
  ): Promise<TechnicalTokenCreated>;
  /** One technical token with its `ETag`, which a change of its description takes in `If-Match`. */
  abstract get(account: AccountAddress, tokenName: string): Promise<Versioned<TechnicalToken>>;
  abstract update(account: AccountAddress, tokenName: string, description: string, etag: string): Promise<Versioned<TechnicalToken>>;
  abstract remove(account: AccountAddress, tokenName: string): Promise<void>;
}

function base(account: AccountAddress): string {
  return `${serviceAccountUrl(account.name, account.project, account.repository)}/technical-tokens`;
}

@Injectable()
export class HttpTechnicalTokensApi extends TechnicalTokensApi {
  private readonly client = inject(ApiClient);

  override async list(account: AccountAddress): Promise<readonly TechnicalToken[]> {
    return (await this.client.getJson<{ items: TechnicalToken[] }>(base(account))).body.items;
  }

  override async create(account: AccountAddress, request: NewTechnicalToken, idempotencyKey: string): Promise<TechnicalTokenCreated> {
    return (await this.client.post<TechnicalTokenCreated>(base(account), request, { idempotencyKey })).body;
  }

  override async newSecret(
    account: AccountAddress,
    tokenName: string,
    request: NewTechnicalTokenSecret,
    idempotencyKey: string,
  ): Promise<TechnicalTokenCreated> {
    return (await this.client.post<TechnicalTokenCreated>(`${base(account)}/${seg(tokenName)}/secrets`, request, { idempotencyKey })).body;
  }

  override async get(account: AccountAddress, tokenName: string): Promise<Versioned<TechnicalToken>> {
    const answer = await this.client.getJson<TechnicalToken>(`${base(account)}/${seg(tokenName)}`);
    return { value: answer.body, etag: answer.etag ?? '' };
  }

  override async update(account: AccountAddress, tokenName: string, description: string, etag: string): Promise<Versioned<TechnicalToken>> {
    const answer = await this.client.patch<TechnicalToken>(`${base(account)}/${seg(tokenName)}`, { description }, etag);
    return { value: answer.body, etag: answer.etag ?? '' };
  }

  override async remove(account: AccountAddress, tokenName: string): Promise<void> {
    await this.client.delete(`${base(account)}/${seg(tokenName)}`);
  }
}
