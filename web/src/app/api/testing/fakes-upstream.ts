import { Injectable, Provider } from '@angular/core';
import { ApiError } from '../problem';
import { UpstreamPreset, UpstreamTestResult } from '../repositories-api';
import {
  LoadedRegistryPresets,
  LoadedUpstreamRepository,
  RegistryPreset,
  UpstreamApi,
  UpstreamRepository,
  UpstreamRepositoryAccount,
  UpstreamRepositoryAddress,
  UpstreamRepositoryBulkResult,
  UpstreamRepositoryState,
  UpstreamRepositorySummary,
  UpstreamVisibilityLevel,
} from '../upstream-api';
import { ANSIBLE_PRESETS, DEB_PRESETS, NPM_PRESETS, NUGET_PRESETS, OCI_PRESETS } from './fakes-catalog';

/** The format of each fake preset list, in catalog order: the formats by their ID, the presets in the format's order (R-REPO-073). */
const CATALOG: readonly (readonly [string, readonly UpstreamPreset[]])[] = [
  ['ansible', ANSIBLE_PRESETS],
  ['deb', DEB_PRESETS],
  ['npm', NPM_PRESETS],
  ['nuget', NUGET_PRESETS],
  ['oci', OCI_PRESETS],
];

/** The account of a repository without one (R-REPO-073): stored by nobody, declared by no configuration. */
export function noAccount(): UpstreamRepository['account'] {
  return {
    authMode: 'none',
    username: null,
    password: null,
    removedByUpdateAt: null,
    source: 'api',
    configuration: null,
    lastDownloadAt: null,
    lastFailure: null,
  };
}

/**
 * An account the installation's configuration declares (R-REPO-077): the Helm values' Secret, applied by quader migrate, its files in the
 * state `files` on the answering server; `over` sets the rest a case needs.
 */
export function configuredAccount(
  files: 'readable' | 'missing' | 'empty' | 'invalid' = 'readable',
  over: Partial<UpstreamRepository['account']> = {},
): UpstreamRepository['account'] {
  return {
    ...noAccount(),
    authMode: 'basic',
    source: 'configuration',
    configuration: { secretName: 'quader-upstream-docker-hub', since: '2026-11-25T10:02:11Z', files },
    ...over,
  };
}

/** Docker Hub alone allows a caller with an account more (Q-UI-1160); a repository that sends an account needs none. */
function raisesLimit(key: string, account: UpstreamRepository['account']): boolean {
  return key === 'docker-hub' && account.authMode === 'none';
}

/**
 * One well-known repository of a preset as a fresh installation stores it: off, never switched on, Internal (the owner's decision
 * Q-REPO-388), with the product's address.
 */
export function presetRepository(format: string, p: UpstreamPreset, over: Partial<UpstreamRepository> = {}): UpstreamRepository {
  const extraHosts = p.extraHosts.map((h) => ({ pattern: h.pattern, sendCredentials: h.sendCredentials }));
  const keys = format === 'deb' ? [{ fingerprint: 'A7236886F3CCCAAD148A27F80E98404D386FA1D9', userId: 'Debian Archive Automatic Signing Key (12/bookworm) <ftpmaster@debian.org>' }] : null;
  return {
    key: p.id,
    name: `upstream/${p.id}`,
    format,
    kind: 'preset',
    displayName: p.displayName,
    state: 'not-switched-on',
    inUse: null,
    clients: null,
    visibility: 'internal',
    fixed: [],
    address: {
      url: p.url,
      extraHosts,
      upstreamKeys: keys,
      overridden: false,
      product: { url: p.url, extraHosts, upstreamKeys: keys },
    },
    account: over.account ?? noAccount(),
    accountRaisesLimit: raisesLimit(p.id, over.account ?? noAccount()),
    listedBy: 0,
    listedByProjects: 0,
    configVersion: 1,
    ...over,
  };
}

/** `scanner-trivy`, the system repository of the image scanning's database (R-REPO-070). */
export function scannerTrivy(over: Partial<UpstreamRepository> = {}): UpstreamRepository {
  const product = { url: 'https://mirror.gcr.io/', extraHosts: [], upstreamKeys: null };
  return {
    key: 'scanner-trivy',
    name: 'upstream/scanner-trivy',
    format: 'oci',
    kind: 'system',
    displayName: 'Image scanning database',
    state: null,
    inUse: true,
    clients: null,
    visibility: 'private',
    fixed: ['state', 'visibility'],
    address: { ...product, overridden: false, product },
    account: noAccount(),
    accountRaisesLimit: false,
    listedBy: 0,
    listedByProjects: 0,
    configVersion: 1,
    ...over,
  };
}

/** The address of the endpoint of the fake `clamav` (R-CLAMAV-004). */
export const CLAMAV_ENDPOINT = 'https://quader.example.com/api/clamav/upstream/clamav';

/**
 * `clamav`, the local system repository of ClamAV's definitions (R-REPO-075): no address, no account, and the switch **ClamAV
 * definition updates** off, as a fresh installation has it.
 */
export function clamavRepository(over: Partial<UpstreamRepository> = {}): UpstreamRepository {
  const product = { url: '', extraHosts: [], upstreamKeys: null };
  return {
    key: 'clamav',
    name: 'upstream/clamav',
    format: 'clamav',
    kind: 'system',
    displayName: 'ClamAV definitions',
    state: null,
    inUse: true,
    clients: { enabled: false, address: CLAMAV_ENDPOINT, certificateAddresses: [] },
    visibility: 'private',
    fixed: ['state', 'visibility', 'address', 'account'],
    address: { ...product, overridden: false, product },
    account: noAccount(),
    accountRaisesLimit: false,
    listedBy: 0,
    listedByProjects: 0,
    configVersion: 1,
    ...over,
  };
}

/** The twelve preset repositories of the fake catalog, `scanner-trivy` and `clamav`, every one as a fresh installation has it. */
export function upstreamFixture(): UpstreamRepository[] {
  return [...CATALOG.flatMap(([format, presets]) => presets.map((p) => presetRepository(format, p))), scannerTrivy(), clamavRepository()];
}

function notFound(key: string): ApiError {
  return new ApiError(404, { code: 'not_found', detail: `No repository ${key} in the project upstream.`, requestId: 'rid-upstream-404' }, 'rid-upstream-404');
}

/** A test that reached the upstream and was let in. */
export const REACHED: UpstreamTestResult = {
  verdict: 'accepted',
  reachable: true,
  httpStatus: 200,
  authenticated: true,
  durationMs: 84,
  tls: { peerSubject: 'CN=registry-1.docker.io', notAfter: '2027-05-01T00:00:00Z' },
  outboundProxy: null,
  sourceAddress: null,
  tokenService: null,
  error: null,
};

/**
 * The well-known repositories in memory (R-REPO-073), with the rules the screens rely on: switching sets the state and counts
 * the first switch-on; Switch all on makes what is off `internal` and active and leaves the system repository; the level of every
 * preset follows Set visibility of all; an address that differs from the product's is `overridden`; an address that moves the
 * hosts of a stored account without `account` answers `422 secret_value_required`. The list answers the counts of the listing
 * virtual repositories, every other answer also `listedByNames` (Q-API-363); the read of a format answers its presets that are not
 * retired (Q-API-364). Every call is recorded; `failure` makes the next write fail, `formatFailure` the read of a format.
 */
@Injectable()
export class FakeUpstreamApi extends UpstreamApi {
  items: UpstreamRepository[] = upstreamFixture();
  failure: ApiError | null = null;
  listFailure: ApiError | null = null;
  formatFailure: ApiError | null = null;
  /** The qualified names of the virtual repositories that list a repository, by its key; `listedBy` counts more where it is set so. */
  listedByNames: Record<string, string[]> = {};
  readonly calls: { op: string; key?: string; body?: unknown; etag?: string; removeAccount?: boolean }[] = [];
  testResult: UpstreamTestResult = REACHED;
  /** The installation's registry presets (R-REPO-076) and their version, the `ETag`. */
  presets: RegistryPreset[] = [];
  presetsVersion = 1;
  presetsFailure: ApiError | null = null;

  /**
   * The installation allows no public level (the default endpoint admits no request without a credential): switching the clients on at
   * `public` lowers them to `internal` with the warning `public_not_allowed_lowered` (R-REPO-075).
   */
  publicAllowed = true;

  /** Replaces one repository of the fixture by its key. */
  set(key: string, over: Partial<UpstreamRepository>): void {
    this.items = this.items.map((r) => (r.key === key ? { ...r, ...over } : r));
  }

  override async list(): Promise<readonly UpstreamRepository[]> {
    if (this.listFailure) throw this.listFailure;
    return structuredClone(this.items);
  }

  override async listForFormat(format: string): Promise<readonly UpstreamRepositorySummary[]> {
    this.calls.push({ op: 'format', body: { format } });
    if (this.formatFailure) throw this.formatFailure;
    return this.items
      .filter((r) => r.kind === 'preset' && r.format === format && r.state !== 'retired' && r.state !== null)
      .map((r) => ({ key: r.key, name: r.name, displayName: r.displayName, state: r.state!, visibility: r.visibility }));
  }

  override async get(key: string): Promise<LoadedUpstreamRepository> {
    return this.loaded(this.find(key));
  }

  override async setState(key: string, state: 'on' | 'off'): Promise<LoadedUpstreamRepository> {
    this.calls.push({ op: 'state', key, body: { state } });
    this.fail();
    const r = this.find(key);
    const next: UpstreamRepositoryState = state === 'on' ? 'active' : 'switched-off';
    return this.store({ ...r, state: next, configVersion: r.configVersion + 1 });
  }

  override async switchOnAll(): Promise<UpstreamRepositoryBulkResult> {
    this.calls.push({ op: 'switch-on-all' });
    this.fail();
    const changed: UpstreamRepository[] = [];
    this.items = this.items.map((r) => {
      if (r.kind !== 'preset' || r.state === 'active' || r.state === 'retired') return r;
      const next = { ...r, state: 'active' as const, visibility: 'internal' as const, configVersion: r.configVersion + 1 };
      changed.push(next);
      return next;
    });
    return { items: changed.map((r) => this.loaded(r).value), bulkId: '6f1c2e8a-1d6b-4d3f-9c43-2f9b4f1c0a77' };
  }

  override async setClients(key: string, enabled: boolean, level?: UpstreamVisibilityLevel): Promise<LoadedUpstreamRepository> {
    this.calls.push({ op: 'clients', key, body: level ? { enabled, level } : { enabled } });
    this.fail();
    const r = this.find(key);
    if (!r.clients) {
      throw new ApiError(409, { code: 'managed_by_product', detail: `${r.name} has no clients to switch.`, requestId: 'rid-409' }, 'rid-409');
    }
    const asked = level ?? 'public';
    const lowered = enabled && asked === 'public' && !this.publicAllowed;
    const visibility: UpstreamVisibilityLevel = !enabled ? 'private' : lowered ? 'internal' : asked;
    const next: UpstreamRepository = {
      ...r,
      clients: { ...r.clients, enabled },
      visibility,
      fixed: enabled ? ['state', 'address', 'account'] : ['state', 'visibility', 'address', 'account'],
      configVersion: r.configVersion + 1,
    };
    const stored = this.store(next);
    return lowered
      ? {
          ...stored,
          value: {
            ...stored.value,
            warnings: [{ code: 'public_not_allowed_lowered', detail: 'This installation allows no public level: ClamAV definition updates is on at Internal.' }],
          },
        }
      : stored;
  }

  override async setVisibility(key: string, level: UpstreamVisibilityLevel): Promise<LoadedUpstreamRepository> {
    this.calls.push({ op: 'visibility', key, body: { level } });
    this.fail();
    const r = this.find(key);
    return this.store({ ...r, visibility: level, configVersion: r.configVersion + 1 });
  }

  override async setAllVisibility(level: UpstreamVisibilityLevel): Promise<UpstreamRepositoryBulkResult> {
    this.calls.push({ op: 'set-visibility', body: { level } });
    this.fail();
    const changed: UpstreamRepository[] = [];
    this.items = this.items.map((r) => {
      if (r.kind !== 'preset' || r.state === 'retired' || r.visibility === level) return r;
      const next = { ...r, visibility: level, configVersion: r.configVersion + 1 };
      changed.push(next);
      return next;
    });
    return { items: changed.map((r) => this.loaded(r).value), bulkId: '0d1f7b52-74c4-4c8e-8a8d-3e0f1e9f5b21' };
  }

  override async setAddress(key: string, address: UpstreamRepositoryAddress, etag: string): Promise<LoadedUpstreamRepository> {
    this.calls.push({ op: 'address', key, body: structuredClone(address), etag });
    this.fail();
    const r = this.find(key);
    const url = address.url ?? r.address.product.url;
    const extraHosts = address.extraHosts ?? r.address.product.extraHosts;
    const hostOf = (u: string): string => {
      try {
        return new URL(u).host;
      } catch {
        return u;
      }
    };
    const moved = hostOf(url) !== hostOf(r.address.url);
    if (moved && r.account.authMode !== 'none' && address.account === undefined) {
      throw new ApiError(
        422,
        {
          code: 'validation_failed',
          detail: 'The account goes to the old host only.',
          requestId: 'rid-422',
          errors: [{ pointer: '/account/password', code: 'secret_value_required', detail: 'Enter the account again or remove it.' }],
        },
        'rid-422',
      );
    }
    const overridden = url !== r.address.product.url || JSON.stringify(extraHosts) !== JSON.stringify(r.address.product.extraHosts);
    const account =
      address.account === null
        ? noAccount()
        : address.account
          ? this.accountOf(address.account)
          : r.account;
    return this.store({
      ...r,
      address: { ...r.address, url, extraHosts, overridden },
      account,
      configVersion: r.configVersion + 1,
    });
  }

  override async resetAddress(key: string, etag: string, removeAccount = false): Promise<LoadedUpstreamRepository> {
    this.calls.push({ op: 'reset-address', key, etag, removeAccount });
    this.fail();
    const r = this.find(key);
    const p = r.address.product;
    return this.store({
      ...r,
      address: { url: p.url, extraHosts: p.extraHosts, upstreamKeys: p.upstreamKeys, overridden: false, product: p },
      account: removeAccount ? noAccount() : r.account,
      configVersion: r.configVersion + 1,
    });
  }

  override async setAccount(key: string, account: UpstreamRepositoryAccount, etag: string): Promise<LoadedUpstreamRepository> {
    this.calls.push({ op: 'account', key, body: structuredClone(account), etag });
    this.fail();
    const r = this.find(key);
    return this.store({ ...r, account: this.accountOf(account), configVersion: r.configVersion + 1 });
  }

  override async removeAccount(key: string, etag: string): Promise<LoadedUpstreamRepository> {
    this.calls.push({ op: 'remove-account', key, etag });
    this.fail();
    const r = this.find(key);
    return this.store({
      ...r,
      account: noAccount(),
      configVersion: r.configVersion + 1,
    });
  }

  override async testSaved(key: string): Promise<UpstreamTestResult> {
    this.calls.push({ op: 'test-saved', key });
    return structuredClone(this.testResult);
  }

  override async testUnsaved(format: string, remote: Readonly<Record<string, unknown>>): Promise<UpstreamTestResult> {
    this.calls.push({ op: 'test-unsaved', body: structuredClone({ format, remote }) });
    return structuredClone(this.testResult);
  }

  override async registryPresets(): Promise<LoadedRegistryPresets> {
    if (this.presetsFailure) throw this.presetsFailure;
    return { presets: structuredClone(this.presets), etag: `"${this.presetsVersion}"` };
  }

  /**
   * Replaces the list as the server does (R-REPO-076): `If-Match` must be the current version (412 otherwise), and a URL of a
   * well-known preset of the format is refused at its pointer; `failure` makes the next call fail.
   */
  override async setRegistryPresets(presets: readonly RegistryPreset[], etag: string): Promise<LoadedRegistryPresets> {
    this.calls.push({ op: 'registry-presets', body: structuredClone(presets), etag });
    this.fail();
    if (etag !== `"${this.presetsVersion}"`) {
      throw new ApiError(412, { code: 'precondition_failed', detail: 'The registry presets changed.', requestId: 'rid-412' }, 'rid-412');
    }
    const taken = presets.findIndex((p) =>
      CATALOG.some(([format, list]) => format === p.format && list.some((w) => w.url === p.url)),
    );
    if (taken >= 0) {
      throw new ApiError(
        422,
        {
          code: 'validation_failed',
          detail: 'The registry presets are invalid.',
          requestId: 'rid-422',
          errors: [{ pointer: `/presets/${taken}/url`, code: 'invalid_value', detail: 'is the address of a well-known registry, which has its button already' }],
        },
        'rid-422',
      );
    }
    this.presets = structuredClone([...presets]);
    this.presetsVersion++;
    return this.registryPresets();
  }

  private accountOf(a: UpstreamRepositoryAccount): UpstreamRepository['account'] {
    return {
      ...noAccount(),
      authMode: a.authMode,
      username: a.authMode === 'basic' ? (a.username ?? null) : null,
      password: a.authMode === 'none' ? null : { set: true },
    };
  }

  private find(key: string): UpstreamRepository {
    const r = this.items.find((x) => x.key === key);
    if (!r) throw notFound(key);
    return r;
  }

  private fail(): void {
    if (this.failure) {
      const e = this.failure;
      this.failure = null;
      throw e;
    }
  }

  private store(written: UpstreamRepository): LoadedUpstreamRepository {
    const next = { ...written, accountRaisesLimit: written.kind === 'preset' && raisesLimit(written.key, written.account) };
    this.items = this.items.map((r) => (r.key === next.key ? next : r));
    return this.loaded(next);
  }

  private loaded(r: UpstreamRepository): LoadedUpstreamRepository {
    return { value: { ...structuredClone(r), listedByNames: [...(this.listedByNames[r.key] ?? [])] }, etag: `"${r.configVersion}"` };
  }
}

export function provideUpstreamFakes(): Provider[] {
  return [{ provide: UpstreamApi, useClass: FakeUpstreamApi }];
}
