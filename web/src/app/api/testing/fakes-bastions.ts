import { Provider } from '@angular/core';
import {
  Bastion,
  BastionConfigurationPatch,
  BastionContact,
  BastionPreset,
  BastionsApi,
  BastionView,
  BastionTokenCreated,
  BastionTokenSummary,
  NewBastion,
  NewBastionToken,
} from '../bastions-api';
import { ApiError } from '../problem';
import { Versioned } from '../repositories-api';

/*
 * In-memory fake of the Bastion registrations (R-BAS-004, R-BAS-007, R-UI-047 component tier). It
 * models what the screen depends on: the `ETag` of a change (412 for a stale tag), the name rule and
 * the duplicate (422, 409), the limit of active tokens (409 `token_limit_reached`), a token secret
 * that is returned once and never again, the switch of the direct reads (R-BAS-071) and the networks a Bastion connects from
 * (R-BAS-017; a /0 is refused at its entry, 422, as the server refuses it).
 */

const problem = (status: number, code: string, extra: Record<string, unknown> = {}): ApiError =>
  new ApiError(status, { code, ...extra }, 'req-1');

/** A secret of the shape of a Bastion token: `qdr_bst_` and 49 characters (R-BAS-004). */
export const BASTION_SECRET = 'qdr_bst_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklm';

const DAY = 86_400_000;

/** The flood figures every preset writes, as `bastionmain.Presets` holds them (R-LIM-046). */
const PRESET_FLOOD: BastionPreset['flood'] = {
  anonymous: { requestsPerSecond: 50, burst: 1000, missesPerMinute: 1000 },
  authenticated: { requestsPerSecond: 200, burst: 4000, missesPerMinute: 4000 },
  gui: { requestsPerSecond: 20, burst: 100 },
  failedAuth: { maxFailures: 20, windowSeconds: 600, timeoutSeconds: 900 },
  blocking: { threshold: 3000, windowSeconds: 60, durationSeconds: 900, maxDurationSeconds: 86400 },
  exemptNetworks: [],
};

const presetKinds = (anonymous: string, user: string, machine: string, administrator: string): BastionPreset['kinds'] => ({
  anonymous,
  user,
  technicalToken: machine,
  ci: machine,
  administrator,
});

/** What `GET /api/v1/bastions/presets` answers: the server's table of R-LIM-046 (`bastionmain.Presets`). */
export const BASTION_PRESETS: readonly BastionPreset[] = [
  { name: 'internet', kinds: presetKinds('bastion-anonymous', 'bastion-signed-in', 'bastion-signed-in', 'bastion-signed-in'), flood: PRESET_FLOOD, mode: 'enforce' },
  { name: 'partnerNetwork', kinds: presetKinds('anonymous', 'standard', 'technical', 'unlimited'), flood: PRESET_FLOOD, mode: 'enforce' },
  { name: 'internal', kinds: presetKinds('unlimited', 'unlimited', 'unlimited', 'unlimited'), flood: PRESET_FLOOD, mode: 'enforce' },
];

/** A time `days` from now (negative: ago) as the API writes it. */
export function inDays(days: number): string {
  return new Date(Date.now() + days * DAY).toISOString();
}

export function sampleBastionToken(over: Partial<BastionTokenSummary> = {}): BastionTokenSummary {
  return {
    id: '0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e41',
    hint: 'qdr_bst_3kQ9',
    description: 'partners 2026',
    createdAt: inDays(-165),
    expiresAt: inDays(200),
    lastUsedAt: inDays(-1),
    declared: false,
    ...over,
  };
}

export function sampleBastionContact(over: Partial<BastionContact> = {}): BastionContact {
  return {
    replica: 'quader-bastion-7d9c-x2k4q',
    contactAt: inDays(-1 / 1440),
    clientAddress: '198.51.100.20',
    release: '0.15.0',
    interfaceVersion: 1,
    baseUrl: 'https://packages.example.com',
    edgeLimit: null,
    catalogueKept: null,
    ...over,
  };
}

/** The limits of a new registration (R-BAS-093, R-LIM-046): the preset Internet in Enforce. */
export function sampleBastionLimits(): Bastion['limits'] {
  return {
    mode: 'enforce',
    preset: 'internet',
    flood: {
      anonymous: { requestsPerSecond: 50, burst: 1000, missesPerMinute: 1000 },
      authenticated: { requestsPerSecond: 200, burst: 4000, missesPerMinute: 4000 },
      gui: { requestsPerSecond: 20, burst: 100 },
      failedAuth: { maxFailures: 20, windowSeconds: 600, timeoutSeconds: 900 },
      blocking: { threshold: 3000, windowSeconds: 60, durationSeconds: 900, maxDurationSeconds: 86400 },
      exemptNetworks: [],
    },
    slots: { maxConcurrent: 64, anonymousShare: 0.25, queueTimeoutMillis: 5000 },
    throughput: { bytesPerSecond: 200000000, anonymousShare: 0.25, bytesPerHour: { anonymous: null, signedIn: null, catalogue: null } },
    anonymousCeiling: { requestsPerSecond: 100, burst: 2000, requestsPerHour: 36000 },
  };
}

/** The switches of a registration with the handover per blob as given (R-BAS-071). */
export function handoverSwitches(handover: boolean): Bastion['switches'] {
  return { handover, gui: false, docs: false };
}

export function sampleBastion(over: Partial<Bastion> = {}): Bastion {
  return {
    id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a40',
    name: 'partners',
    description: 'The partner network',
    enabled: true,
    switches: { handover: false, gui: false, docs: false },
    catalogue: null,
    limits: sampleBastionLimits(),
    status: [],
    egress: ['198.51.100.16/28'],
    declared: null,
    createdAt: inDays(-170),
    tokens: [sampleBastionToken()],
    contacts: [
      sampleBastionContact(),
      sampleBastionContact({ replica: 'quader-bastion-7d9c-m8v2j', clientAddress: '198.51.100.21', contactAt: inDays(-2 / 1440) }),
    ],
    ...over,
  };
}

export class FakeBastionsApi extends BastionsApi {
  readonly calls: string[] = [];
  /** The `If-Match` of every change, in order. */
  readonly ifMatch: string[] = [];
  readonly keys: string[] = [];
  /** The last token request. */
  lastTokenRequest: NewBastionToken | null = null;
  /** At most this many unrevoked tokens per registration (`auth.tokens.max_active_per_principal.bst`). */
  tokenLimit = 3;
  /** What `presets()` answers: the server's table unless a test changes it. */
  presetTable: readonly BastionPreset[] = BASTION_PRESETS;
  failure: ApiError | null = null;
  listFailure: ApiError | null = null;
  bastions: Bastion[] = [
    sampleBastion(),
    sampleBastion({
      id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a41',
      name: 'standby',
      description: '',
      enabled: false,
      tokens: [],
      contacts: [],
    }),
  ];
  private readonly revisions = new Map<string, number>();

  etagOf(name: string): string {
    return `"${this.revisions.get(name) ?? 1}"`;
  }

  /** Simulates a change by someone else: the stored revision moves on. */
  bumpRevision(name: string): void {
    this.revisions.set(name, (this.revisions.get(name) ?? 1) + 1);
  }

  private fail(): void {
    if (this.failure) {
      const e = this.failure;
      this.failure = null;
      throw e;
    }
  }

  private find(name: string): Bastion {
    const found = this.bastions.find((b) => b.name === name);
    if (!found) throw problem(404, 'not_found');
    return found;
  }

  override async list(): Promise<readonly Bastion[]> {
    if (this.listFailure) throw this.listFailure;
    return [...this.bastions].sort((a, b) => a.name.localeCompare(b.name));
  }

  override async get(name: string): Promise<Versioned<BastionView>> {
    return { value: this.find(name), etag: this.etagOf(name) };
  }

  override async presets(): Promise<readonly BastionPreset[]> {
    this.calls.push('presets');
    return this.presetTable;
  }

  override async create(body: NewBastion, idempotencyKey: string): Promise<Bastion> {
    this.fail();
    this.keys.push(idempotencyKey);
    this.calls.push(`create ${body.name}`);
    if (!/^[a-z][a-z0-9-]{0,19}$/.test(body.name)) {
      throw new ApiError(422, { code: 'validation_failed', errors: [{ pointer: '/name', code: 'invalid', detail: 'invalid' }] }, 'req-1');
    }
    if (this.bastions.some((b) => b.name === body.name)) throw problem(409, 'conflict');
    const created = sampleBastion({
      id: `b-${body.name}`,
      name: body.name,
      description: body.description ?? '',
      egress: [...(body.egress ?? [])],
      createdAt: new Date().toISOString(),
      tokens: [],
      contacts: [],
    });
    this.bastions = [...this.bastions, created];
    return created;
  }

  override async setEnabled(name: string, enabled: boolean, etag: string): Promise<Versioned<Bastion>> {
    this.fail();
    this.ifMatch.push(etag);
    this.calls.push(`setEnabled ${name} ${enabled}`);
    const current = this.find(name);
    if (etag !== this.etagOf(name)) throw problem(412, 'precondition_failed', { currentEtag: this.etagOf(name) });
    this.bumpRevision(name);
    const changed = { ...current, enabled };
    this.bastions = this.bastions.map((b) => (b.name === name ? changed : b));
    return { value: changed, etag: this.etagOf(name) };
  }

  override async setHandover(name: string, handover: boolean, etag: string): Promise<Versioned<Bastion>> {
    this.fail();
    this.ifMatch.push(etag);
    this.calls.push(`setHandover ${name} ${handover}`);
    const current = this.find(name);
    if (etag !== this.etagOf(name)) throw problem(412, 'precondition_failed', { currentEtag: this.etagOf(name) });
    this.bumpRevision(name);
    const changed = { ...current, switches: { ...current.switches, handover } };
    this.bastions = this.bastions.map((b) => (b.name === name ? changed : b));
    return { value: changed, etag: this.etagOf(name) };
  }

  override async setEgress(name: string, egress: readonly string[], etag: string): Promise<Versioned<Bastion>> {
    this.fail();
    this.ifMatch.push(etag);
    this.calls.push(`setEgress ${name} ${egress.join(' ')}`);
    const current = this.find(name);
    if (etag !== this.etagOf(name)) throw problem(412, 'precondition_failed', { currentEtag: this.etagOf(name) });
    const wide = egress.findIndex((e) => e.endsWith('/0'));
    if (wide >= 0) {
      throw new ApiError(
        422,
        { code: 'validation_failed', errors: [{ pointer: `/egress/${wide}`, code: 'invalid_network', detail: `${egress[wide]} covers every address` }] },
        'req-1',
      );
    }
    this.bumpRevision(name);
    const changed = { ...current, egress: egress.map((e) => (e.includes('/') ? e : `${e}/${e.includes(':') ? 128 : 32}`)) };
    this.bastions = this.bastions.map((b) => (b.name === name ? changed : b));
    return { value: changed, etag: this.etagOf(name) };
  }

  /** The merge patches of `configure`, in order. */
  readonly patches: BastionConfigurationPatch[] = [];

  /**
   * A merge patch of the configuration (R-BAS-093): a preset writes its flood figures and Enforce first (R-LIM-046, every preset's flood
   * figures are the Internet ones), then the other members merge; the preset reads `custom` once a flood figure differs from it.
   */
  override async configure(name: string, patch: BastionConfigurationPatch, etag: string): Promise<Versioned<Bastion>> {
    this.fail();
    this.ifMatch.push(etag);
    this.patches.push(patch);
    this.calls.push(`configure ${name} ${JSON.stringify(patch)}`);
    const current = this.find(name);
    if (etag !== this.etagOf(name)) throw problem(412, 'precondition_failed', { currentEtag: this.etagOf(name) });
    this.bumpRevision(name);
    let limits = current.limits;
    const l = patch.limits;
    if (l?.preset && l.preset !== 'custom') {
      limits = { ...limits, preset: l.preset, flood: sampleBastionLimits().flood, mode: 'enforce' };
    }
    if (l) {
      const f = l.flood ?? {};
      const flood = {
        anonymous: { ...limits.flood.anonymous, ...f.anonymous },
        authenticated: { ...limits.flood.authenticated, ...f.authenticated },
        gui: { ...limits.flood.gui, ...f.gui },
        failedAuth: { ...limits.flood.failedAuth, ...f.failedAuth },
        blocking: { ...limits.flood.blocking, ...f.blocking },
        exemptNetworks: [...(f.exemptNetworks ?? limits.flood.exemptNetworks)],
      };
      limits = {
        ...limits,
        mode: l.mode ?? limits.mode,
        flood,
        slots: { ...limits.slots, ...l.slots },
        throughput: {
          ...limits.throughput,
          ...l.throughput,
          bytesPerHour: { ...limits.throughput.bytesPerHour, ...l.throughput?.bytesPerHour },
        },
        anonymousCeiling: { ...limits.anonymousCeiling, ...l.anonymousCeiling },
      };
      if (l.flood && JSON.stringify(flood) !== JSON.stringify(sampleBastionLimits().flood)) limits = { ...limits, preset: 'custom' };
    }
    const changed: Bastion = { ...current, switches: { ...current.switches, ...patch.switches }, limits };
    this.bastions = this.bastions.map((b) => (b.name === name ? changed : b));
    return { value: changed, etag: this.etagOf(name) };
  }

  override async delete(name: string): Promise<void> {
    this.fail();
    this.find(name);
    this.calls.push(`delete ${name}`);
    this.bastions = this.bastions.filter((b) => b.name !== name);
  }

  override async createToken(name: string, body: NewBastionToken, idempotencyKey: string): Promise<BastionTokenCreated> {
    this.fail();
    this.keys.push(idempotencyKey);
    this.lastTokenRequest = body;
    this.calls.push(`createToken ${name} ${body.description ?? ''} ${body.ttlSeconds ?? 'default'}`);
    const current = this.find(name);
    if (current.tokens.length >= this.tokenLimit) throw problem(409, 'token_limit_reached');
    const ttl = body.ttlSeconds ?? 365 * 86_400;
    const summary = sampleBastionToken({
      id: `t-${current.tokens.length + 1}`,
      hint: 'qdr_bst_Zz09',
      description: body.description ?? '',
      createdAt: new Date().toISOString(),
      expiresAt: new Date(Date.now() + ttl * 1000).toISOString(),
      lastUsedAt: null,
    });
    this.bastions = this.bastions.map((b) => (b.name === name ? { ...b, tokens: [summary, ...b.tokens] } : b));
    return {
      id: summary.id,
      token: BASTION_SECRET,
      hint: summary.hint,
      description: summary.description,
      createdAt: summary.createdAt,
      expiresAt: summary.expiresAt ?? '',
    };
  }

  override async revokeToken(name: string, id: string): Promise<void> {
    this.fail();
    this.calls.push(`revokeToken ${name} ${id}`);
    const current = this.find(name);
    if (!current.tokens.some((t) => t.id === id)) throw problem(404, 'not_found');
    this.bastions = this.bastions.map((b) => (b.name === name ? { ...b, tokens: b.tokens.filter((t) => t.id !== id) } : b));
  }
}

export function provideBastionFakes(): Provider[] {
  return [{ provide: BastionsApi, useClass: FakeBastionsApi }];
}
