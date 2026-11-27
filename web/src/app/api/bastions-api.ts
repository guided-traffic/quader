import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { seg } from './query';
import { Versioned } from './repositories-api';
import { Schema } from './types';

/*
 * The registrations of Quader Bastions (R-BAS-004, R-BAS-007; docs/developer/bastion.md). The shapes
 * are the generated ones. The list is one page (at most `bastions.max_registrations` exist) and
 * carries no tag; a registration read on its own carries the `ETag` a change needs (R-API-012), so a
 * disable or an enable reads the registration first.
 */

/**
 * A registration as one registration's read answers a holder of `limits:read` without `endpoints:read` (R-BAS-093): `id`, `name`,
 * `limits`, `status`, `state` and `running`; every other member is absent.
 */
export type BastionView = Schema<'Bastion'>;
/** A registration with every member, as the list, the writes and the read of a holder of `endpoints:read` answer it. */
export type Bastion = Required<BastionView>;
export type BastionTokenSummary = Schema<'BastionTokenSummary'>;
/** What applying a preset to a Bastion's limits writes (R-LIM-046), as the server applies it. */
export type BastionPreset = Schema<'BastionPreset'>;
export type BastionContact = Schema<'BastionContact'>;
/** The way a Bastion is disabled (R-BAS-133): soft lets what runs finish, hard cuts it. */
export type BastionDisableMode = Schema<'BastionDisable'>['mode'];
export type NewBastion = Schema<'NewBastion'>;
export type NewBastionToken = Schema<'NewBastionToken'>;
export type BastionTokenCreated = Schema<'BastionTokenCreated'>;

type Limits = Bastion['limits'];
type Flood = Limits['flood'];

/** A merge patch of a registration's flood figures (R-BAS-093): a member that is absent is unchanged, a nested one merges. */
export interface BastionFloodPatch {
  readonly anonymous?: Partial<Flood['anonymous']>;
  readonly authenticated?: Partial<Flood['authenticated']>;
  readonly gui?: Partial<Flood['gui']>;
  readonly failedAuth?: Partial<Flood['failedAuth']>;
  readonly blocking?: Partial<Flood['blocking']>;
  readonly exemptNetworks?: readonly string[];
}

/**
 * A merge patch of a registration's configuration (R-BAS-093, `BastionPatch`): `limits` needs `limits:write`, `switches`
 * `endpoints:write` (Q-BAS-325); `limits.preset` applies a preset first (Q-LIM-311), and a change of `limits.mode` needs recent
 * authentication (R-LIM-044).
 */
export interface BastionConfigurationPatch {
  readonly switches?: Partial<Bastion['switches']>;
  readonly limits?: {
    readonly mode?: Limits['mode'];
    readonly preset?: Limits['preset'];
    readonly flood?: BastionFloodPatch;
    readonly slots?: Partial<Limits['slots']>;
    readonly throughput?: {
      readonly bytesPerSecond?: number | null;
      readonly anonymousShare?: number;
      readonly bytesPerHour?: Partial<Limits['throughput']['bytesPerHour']>;
    };
    readonly anonymousCeiling?: Partial<Limits['anonymousCeiling']>;
  };
}

/** The name rule of a registration (R-BAS-004), checked live in the form; the server decides. */
export const BASTION_NAME_PATTERN = /^[a-z][a-z0-9-]{0,19}$/;

export abstract class BastionsApi {
  /** Every registration, by name; each with its unrevoked tokens and the last contact of each replica. */
  abstract list(): Promise<readonly Bastion[]>;
  /** One registration: every member for a holder of `endpoints:read`, its limits and state for a holder of `limits:read` alone. */
  abstract get(name: string): Promise<Versioned<BastionView>>;
  abstract create(body: NewBastion, idempotencyKey: string): Promise<Bastion>;
  /** Disables the Bastion soft or hard, told to its replicas over their connection, or enables it with `null` (R-BAS-133). */
  abstract setDisabled(name: string, disabled: BastionDisableMode | null, etag: string): Promise<Versioned<Bastion>>;
  /** Switches the direct reads from the object store on or off (the handover per blob, R-BAS-071); a new registration has them off. */
  abstract setHandover(name: string, handover: boolean, etag: string): Promise<Versioned<Bastion>>;
  /** Replaces the networks the Bastion connects from (R-BAS-017, R-UI-243); `[]` empties the list. */
  abstract setEgress(name: string, egress: readonly string[], etag: string): Promise<Versioned<Bastion>>;
  /** Changes the switches and the limits of the registration's configuration (R-BAS-093) with a merge patch. */
  abstract configure(name: string, patch: BastionConfigurationPatch, etag: string): Promise<Versioned<Bastion>>;
  /** Deletes the registration and revokes its tokens. */
  abstract delete(name: string): Promise<void>;
  /** The secret is in this answer only (R-AUTH-022). */
  abstract createToken(name: string, body: NewBastionToken, idempotencyKey: string): Promise<BastionTokenCreated>;
  /** Invalidates a token at once, for good, the declared one included (R-BAS-133, Q-UI-1642): the last resort of a leak. */
  abstract invalidateToken(name: string, id: string): Promise<void>;
  /** The three presets in the card's order, each with what applying it writes (`limits:read`; R-LIM-046, Q-UI-1485). */
  abstract presets(): Promise<readonly BastionPreset[]>;
}

@Injectable()
export class HttpBastionsApi extends BastionsApi {
  private readonly client = inject(ApiClient);

  override async list(): Promise<readonly Bastion[]> {
    return (await this.client.getJson<{ items: Bastion[] }>('/api/v1/bastions')).body.items;
  }

  override async get(name: string): Promise<Versioned<BastionView>> {
    const r = await this.client.getJson<BastionView>(`/api/v1/bastions/${seg(name)}`);
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async create(body: NewBastion, idempotencyKey: string): Promise<Bastion> {
    return (await this.client.post<Bastion>('/api/v1/bastions', body, { idempotencyKey })).body;
  }

  override async setDisabled(name: string, disabled: BastionDisableMode | null, etag: string): Promise<Versioned<Bastion>> {
    const r = await this.client.patch<Bastion>(`/api/v1/bastions/${seg(name)}`, { disabled }, etag);
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async setHandover(name: string, handover: boolean, etag: string): Promise<Versioned<Bastion>> {
    const r = await this.client.patch<Bastion>(`/api/v1/bastions/${seg(name)}`, { switches: { handover } }, etag);
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async setEgress(name: string, egress: readonly string[], etag: string): Promise<Versioned<Bastion>> {
    const r = await this.client.patch<Bastion>(`/api/v1/bastions/${seg(name)}`, { egress }, etag);
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async configure(name: string, patch: BastionConfigurationPatch, etag: string): Promise<Versioned<Bastion>> {
    const r = await this.client.patch<Bastion>(`/api/v1/bastions/${seg(name)}`, patch, etag);
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async delete(name: string): Promise<void> {
    await this.client.delete(`/api/v1/bastions/${seg(name)}`);
  }

  override async createToken(name: string, body: NewBastionToken, idempotencyKey: string): Promise<BastionTokenCreated> {
    return (await this.client.post<BastionTokenCreated>(`/api/v1/bastions/${seg(name)}/tokens`, body, { idempotencyKey }))
      .body;
  }

  override async invalidateToken(name: string, id: string): Promise<void> {
    await this.client.delete(`/api/v1/bastions/${seg(name)}/tokens/${seg(id)}`);
  }

  override async presets(): Promise<readonly BastionPreset[]> {
    return (await this.client.getJson<Schema<'BastionPresetList'>>('/api/v1/bastions/presets')).body.items;
  }
}
