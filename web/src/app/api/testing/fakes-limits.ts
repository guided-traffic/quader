import { Provider } from '@angular/core';
import {
  CeilingFigures,
  Figures,
  LimitBlock,
  LimitCeilings,
  LimitExplanation,
  LimitMode,
  LimitPlace,
  LimitProfile,
  LimitProfileWrite,
  LimitRule,
  LimitRuleWrite,
  LimitSettings,
  LimitSubjectSuggestion,
  LimitsApi,
  ModePreview,
  ProjectLimitsRow,
  ProjectMaximum,
  ProjectMaximumWrite,
  UsageDetail,
  UsageList,
  UsagePeriod,
  UsagePoint,
  UsageQuery,
  UsageRow,
  UsageScope,
  UsageSubjectKind,
} from '../limits-api';
import { ApiError } from '../problem';
import { Versioned } from '../repositories-api';

/*
 * The in-memory fake of the download limits and the usage (R-UI-047 component tier), with fixtures made from the JSON examples
 * of R-LIM-031 to R-LIM-035: the five built-in profiles and an own one, the built-in rules and rules of the three places, a
 * project's maximum, the ceilings of a remote, a block, and a usage list with a long name, an IPv6 prefix, a caller at 100 %, an
 * unlimited one, a blocked one and the system. A spec switches the mode, empties the lists, or makes a call fail.
 */

const MB = 1_000_000;
const GB = 1_000_000_000;

function figures(f: Partial<Figures>): Figures {
  return {
    requestsPerSecond: null,
    requestsBurst: null,
    requestsPerHour: null,
    cachedBytesPerSecond: null,
    cachedBytesPerHour: null,
    uncachedBytesPerSecond: null,
    uncachedBytesPerHour: null,
    upstreamRequestsPerHour: null,
    parallelTransfers: null,
    bytesPerDay: null,
    refusedPerHour: null,
    ...f,
  };
}

/** The built-in profiles of R-LIM-010 with their figures. */
export const PROFILE_FIGURES: Readonly<Record<string, Figures>> = {
  unlimited: figures({}),
  standard: figures({
    requestsPerSecond: 200,
    requestsBurst: 4000,
    requestsPerHour: 100000,
    cachedBytesPerSecond: 250 * MB,
    cachedBytesPerHour: 300 * GB,
    uncachedBytesPerSecond: 125 * MB,
    uncachedBytesPerHour: 100 * GB,
    upstreamRequestsPerHour: 20000,
    parallelTransfers: 250,
    bytesPerDay: 1000 * GB,
    refusedPerHour: 1000,
  }),
  technical: figures({
    requestsPerSecond: 1000,
    requestsBurst: 20000,
    requestsPerHour: 1000000,
    cachedBytesPerSecond: 1250 * MB,
    cachedBytesPerHour: 3000 * GB,
    uncachedBytesPerSecond: 250 * MB,
    uncachedBytesPerHour: 500 * GB,
    upstreamRequestsPerHour: 100000,
    parallelTransfers: 1000,
    bytesPerDay: 30000 * GB,
    refusedPerHour: 5000,
  }),
  restricted: figures({
    requestsPerSecond: 100,
    requestsBurst: 1000,
    requestsPerHour: 25000,
    cachedBytesPerSecond: 100 * MB,
    cachedBytesPerHour: 50 * GB,
    uncachedBytesPerSecond: 50 * MB,
    uncachedBytesPerHour: 20 * GB,
    upstreamRequestsPerHour: 3000,
    parallelTransfers: 64,
    bytesPerDay: 200 * GB,
    refusedPerHour: 300,
  }),
  anonymous: figures({
    requestsPerSecond: 50,
    requestsBurst: 1000,
    requestsPerHour: 20000,
    cachedBytesPerSecond: 100 * MB,
    cachedBytesPerHour: 30 * GB,
    uncachedBytesPerSecond: 50 * MB,
    uncachedBytesPerHour: 20 * GB,
    upstreamRequestsPerHour: 3000,
    parallelTransfers: 64,
    bytesPerDay: 200 * GB,
    refusedPerHour: 300,
  }),
};

const STAMP = { version: 1, createdAt: '2026-11-20T09:00:00Z', createdBy: 'ada', updatedAt: '2026-11-20T09:00:00Z', updatedBy: 'ada' };

function profile(key: string, name: string, builtin: boolean, f: Figures, assignments: number, description = ''): LimitProfile {
  return { key, name, description, builtin, figures: f, usedBy: { assignments, projectMaximums: 0 }, ...STAMP };
}

export function sampleProfiles(): LimitProfile[] {
  return [
    profile('unlimited', 'Unlimited', true, PROFILE_FIGURES['unlimited']!, 1, 'No limit at all; administrators get it.'),
    profile('standard', 'Standard', true, PROFILE_FIGURES['standard']!, 1, 'Every signed-in user without a rule of their own.'),
    profile('technical', 'Technical', true, PROFILE_FIGURES['technical']!, 2, 'Technical tokens and CI jobs.'),
    profile('restricted', 'Restricted', true, PROFILE_FIGURES['restricted']!, 2),
    profile('anonymous', 'Anonymous', true, PROFILE_FIGURES['anonymous']!, 1, 'Callers without a credential, per address.'),
    profile('partners', 'Partners with a contract for the nightly mirrors', false, PROFILE_FIGURES['restricted']!, 0, 'External partners with a contract for nightly mirrors'),
  ];
}

function rule(id: string, place: LimitPlace, who: LimitRule['who'], profileKey: string, extra: Partial<LimitRule> = {}): LimitRule {
  const overrides = extra.overrides ?? {};
  return {
    id,
    place,
    who,
    profile: profileKey,
    overrides,
    effective: { ...PROFILE_FIGURES[profileKey]!, ...overrides } as Figures,
    clamped: {},
    capped: false,
    builtin: false,
    note: '',
    ...STAMP,
    ...extra,
  };
}

const INSTANCE: LimitPlace = { kind: 'instance' };

export function sampleInstanceRules(): LimitRule[] {
  const builtin = (kind: 'administrator' | 'user' | 'technicalToken' | 'ci' | 'anonymous', p: string): LimitRule =>
    rule(`builtin:${kind}`, INSTANCE, { kind: 'every', callerKind: kind }, p, { builtin: true });
  return [
    builtin('administrator', 'unlimited'),
    builtin('user', 'standard'),
    builtin('technicalToken', 'technical'),
    builtin('ci', 'technical'),
    builtin('anonymous', 'anonymous'),
    rule('r-group', INSTANCE, { kind: 'group', group: 'contractors', displayName: 'Contractors' }, 'restricted'),
    rule('r-v6', INSTANCE, { kind: 'address', prefix: '2001:db8:1:2::/64' }, 'restricted', { note: 'A partner office behind one prefix' }),
    rule(
      'r-user',
      INSTANCE,
      { kind: 'user', user: 'release-engineering-bot-for-the-customer-portal', displayName: 'Release engineering for the customer portal' },
      'technical',
      { overrides: { uncachedBytesPerHour: 20 * GB, parallelTransfers: 100, bytesPerDay: null } },
    ),
  ];
}

/** The two profiles of a Bastion (R-LIM-046) with the figures of the measurement of 2026-11-26. */
export function sampleBastionProfiles(): LimitProfile[] {
  return [
    profile(
      'bastion-anonymous',
      'Bastion: anonymous',
      true,
      figures({ requestsPerSecond: 50, requestsBurst: 1250, requestsPerHour: 20000, uncachedBytesPerSecond: 50 * MB, uncachedBytesPerHour: 9 * GB, cachedBytesPerSecond: 100 * MB, cachedBytesPerHour: 30 * GB, upstreamRequestsPerHour: 5200, parallelTransfers: 270, bytesPerDay: 220 * GB, refusedPerHour: 300 }),
      1,
    ),
    profile(
      'bastion-signed-in',
      'Bastion: signed-in',
      true,
      figures({ requestsPerSecond: 200, requestsBurst: 5400, requestsPerHour: 100000, uncachedBytesPerSecond: 125 * MB, uncachedBytesPerHour: 20 * GB, cachedBytesPerSecond: 250 * MB, cachedBytesPerHour: 160 * GB, upstreamRequestsPerHour: 12000, parallelTransfers: 340, bytesPerDay: 1300 * GB, refusedPerHour: 1000 }),
      4,
    ),
  ];
}

/**
 * The rules of a Bastion's place (R-LIM-043): the five kind rules the preset Internet wrote, built in, and a rule of a technical token
 * and of an address, which a preset keeps.
 */
export function sampleBastionRules(bastion = 'partners'): LimitRule[] {
  const place: LimitPlace = { kind: 'bastion', bastion };
  const kind = (k: 'administrator' | 'user' | 'technicalToken' | 'ci' | 'anonymous', p: string): LimitRule =>
    rule(`${bastion}:${k}`, place, { kind: 'every', callerKind: k }, p, { builtin: true });
  return [
    rule('b-address', place, { kind: 'address', prefix: '203.0.113.0/24' }, 'restricted', { note: 'The partner office' }),
    kind('administrator', 'bastion-signed-in'),
    kind('ci', 'bastion-signed-in'),
    kind('technicalToken', 'bastion-signed-in'),
    kind('user', 'bastion-signed-in'),
    kind('anonymous', 'bastion-anonymous'),
    rule('b-token', place, { kind: 'technicalToken', technicalToken: 'tt-1', name: 'deploy-prod', account: 'sa:web/partner' }, 'technical'),
  ];
}

export function sampleRepositoryRules(): LimitRule[] {
  const place: LimitPlace = { kind: 'repository', repository: 'web/npm-remote' };
  return [
    rule('0193f2a0-7c1e-7b55-9a10-3f2c8d4e5a61', place, { kind: 'group', group: 'contractors', displayName: 'Contractors' }, 'restricted', {
      overrides: { uncachedBytesPerHour: 20 * GB },
      capped: true,
      note: 'external contractors, narrowed on the public mirror',
    }),
    rule('r-token', place, { kind: 'technicalToken', technicalToken: 'tt-1', name: 'deploy-prod', account: 'sa:web/partner' }, 'technical', {
      capped: true,
      clamped: { uncachedBytesPerHour: 30 * GB },
    }),
  ];
}

export function sampleCeilings(repository = 'platform/dockerhub'): LimitCeilings {
  return {
    repository,
    anonymous: { uncachedBytesPerHour: 10 * GB, upstreamRequestsPerHour: 100 },
    signedIn: { uncachedBytesPerHour: 50 * GB, upstreamRequestsPerHour: 2000 },
    version: 1,
    updatedAt: '2026-11-20T09:10:00Z',
    updatedBy: 'priya',
  };
}

export function samplePreview(): ModePreview {
  return {
    period: { from: '2026-11-19T10:00:00Z', to: '2026-11-20T10:00:00Z' },
    subjects: 12,
    refusedRequests: 3400,
    slowedDownloads: 25,
    blockedSubjects: 1,
    byFigure: [
      { figure: 'cachedBytesPerHour', subjects: 3, events: 140 },
      { figure: 'requestsPerSecond', subjects: 9, events: 3260 },
      { figure: 'upstreamRequestsPerHour', subjects: 1, events: 12 },
      { figure: 'refusedPerHour', subjects: 1, events: 1 },
    ],
    byKind: [
      { kind: 'anonymous', subjects: 10 },
      { kind: 'user', subjects: 2 },
    ],
  };
}

export function sampleBlocks(now = Date.now()): LimitBlock[] {
  return [
    {
      id: '0193f2b1-0d2c-7c00-8a77-1a2b3c4d5e6f',
      subject: { kind: 'anonymous', id: '198.51.100.23', address: '198.51.100.23' },
      place: { kind: 'repository', repository: 'platform/dockerhub' },
      startedAt: new Date(now - 5 * 60_000).toISOString(),
      until: new Date(now + 10 * 60_000).toISOString(),
      refused: 61,
      limit: 60,
      liftedAt: null,
      liftedBy: null,
    },
  ];
}

const row = (r: Partial<UsageRow> & Pick<UsageRow, 'subject'>): UsageRow => ({
  requests: 0,
  refused: 0,
  cachedBytes: 0,
  uncachedBytes: 0,
  upstreamRequests: 0,
  limitedEvents: 0,
  wouldLimitEvents: 0,
  utilisation: null,
  state: 'normal',
  blockedUntil: null,
  ...r,
});

/** The usage list: a long name, an IPv6 prefix, a caller at 100 %, an unlimited caller, a blocked address, a CI project, the system. */
export function sampleUsageRows(now = Date.now()): UsageRow[] {
  return [
    row({
      subject: { kind: 'user', id: 'alice', name: 'alice', displayName: 'Alice Example', deleted: false },
      requests: 48210,
      refused: 3,
      cachedBytes: 1_203_000_000,
      uncachedBytes: 50_000_000,
      upstreamRequests: 120,
      wouldLimitEvents: 2,
      utilisation: { ratio: 0.82, figure: 'cachedBytesPerHour', place: INSTANCE },
      state: 'close',
    }),
    row({
      subject: { kind: 'anonymous', id: '2001:db8:1:2::/64', address: '2001:db8:1:2::/64' },
      requests: 2400,
      refused: 12,
      cachedBytes: 10_000_000_000,
      uncachedBytes: 2_000_000_000,
      upstreamRequests: 20,
      wouldLimitEvents: 31,
      utilisation: { ratio: 1, figure: 'cachedBytesPerHour', place: INSTANCE },
      state: 'wouldBeLimited',
    }),
    row({
      subject: {
        kind: 'technicalToken',
        id: 'tt-1',
        name: 'nightly-mirror-of-the-customer-portal-release-artifacts',
        account: 'sa:customer-portal-release-delivery/release-pipeline',
        level: 'project',
        deleted: false,
      },
      requests: 120400,
      cachedBytes: 640_000_000_000,
      uncachedBytes: 12_000_000_000,
      upstreamRequests: 800,
      utilisation: { ratio: 0.31, figure: 'requestsPerHour', place: { kind: 'project', project: 'customer-portal-release-delivery' } },
    }),
    row({
      subject: { kind: 'user', id: 'ada', name: 'ada', displayName: 'Ada Admin', deleted: false },
      requests: 310,
      cachedBytes: 45_000_000,
      state: 'unlimited',
    }),
    row({
      subject: { kind: 'anonymous', id: '198.51.100.23', address: '198.51.100.23' },
      requests: 61,
      refused: 61,
      utilisation: { ratio: 1.02, figure: 'refusedPerHour', place: { kind: 'repository', repository: 'platform/dockerhub' } },
      state: 'blocked',
      blockedUntil: new Date(now + 10 * 60_000).toISOString(),
    }),
    row({
      subject: { kind: 'ci', id: 'gitlab-internal:4711', issuer: 'gitlab-internal', projectId: '4711', projectPath: 'acme/web' },
      requests: 9000,
      cachedBytes: 4_200_000_000,
      utilisation: { ratio: 0.05, figure: 'bytesPerDay', place: INSTANCE },
    }),
    row({ subject: { kind: 'system', id: 'system' }, requests: 0, uncachedBytes: 800_000_000, upstreamRequests: 44 }),
  ];
}

/** Five-minute points of the last 24 hours ending at `now`, with a burst and a refused point. */
export function sampleHistory(now = Date.parse('2026-11-20T10:00:00Z')): UsagePoint[] {
  const start = now - 24 * 3_600_000;
  return Array.from({ length: 288 }, (_, i): UsagePoint => {
    const wave = Math.max(0, Math.round(200 + 180 * Math.sin(i / 20)));
    return {
      start: new Date(start + i * 300_000).toISOString(),
      requests: i === 250 ? 4100 : wave,
      cachedBytes: wave * 22_000,
      uncachedBytes: i % 40 === 0 ? 50_000_000 : 0,
      upstreamRequests: i % 40 === 0 ? 3 : 0,
      refused: i === 250 ? 3 : 0,
      limitedEvents: 0,
      wouldLimitEvents: i === 250 ? 2 : 0,
    };
  });
}

/** The detail of R-LIM-034's example. */
export function sampleDetail(mode: LimitMode = 'audit'): UsageDetail {
  return {
    subject: { kind: 'user', id: 'alice', name: 'alice', displayName: 'Alice Example', deleted: false },
    period: { from: '2026-11-19T10:00:00Z', to: '2026-11-20T10:00:00Z' },
    mode,
    totals: { requests: 48210, refused: 3, cachedBytes: 1_203_000_000, uncachedBytes: 50_000_000, upstreamRequests: 120, limitedEvents: 0, wouldLimitEvents: 2 },
    budgets: [
      {
        place: INSTANCE,
        rule: { id: 'builtin:user', who: { kind: 'every', callerKind: 'user' }, profile: 'standard', profileName: 'Standard', builtin: true, capped: false },
        figures: {
          requestsPerHour: { limit: 50000, used: 31000, windowSeconds: 3600 },
          cachedBytesPerHour: { limit: 300 * GB, used: 246 * GB, windowSeconds: 3600 },
          uncachedBytesPerHour: { limit: 30 * GB, used: 50_000_000, windowSeconds: 3600 },
          upstreamRequestsPerHour: { limit: 2000, used: 120, windowSeconds: 3600 },
          bytesPerDay: { limit: 1000 * GB, used: 1_253_000_000, windowSeconds: 86400 },
          parallelTransfers: { limit: 250, used: 14 },
          refusedPerHour: { limit: 500, used: 3, windowSeconds: 3600 },
          requestsPerSecond: { limit: 200, burst: 1000 },
          cachedBytesPerSecond: { limit: 200 * MB },
          uncachedBytesPerSecond: { limit: 50 * MB },
        },
        blockedUntil: null,
      },
      {
        place: { kind: 'repository', repository: 'web/npm-remote' },
        rule: { id: '0193f2a0-7c1e-7b55-9a10-3f2c8d4e5a61', who: { kind: 'group', group: 'contractors' }, profile: 'restricted', profileName: 'Restricted', builtin: false, capped: false },
        figures: {
          requestsPerHour: { limit: 5000, used: 5000, windowSeconds: 3600 },
          cachedBytesPerHour: { limit: 50 * GB, used: 12 * GB, windowSeconds: 3600 },
          uncachedBytesPerHour: { limit: 20 * GB, used: 0, windowSeconds: 3600 },
          upstreamRequestsPerHour: { limit: 200, used: 0, windowSeconds: 3600 },
          bytesPerDay: { limit: null, used: 12 * GB, windowSeconds: 86400 },
          parallelTransfers: { limit: 64, used: 0 },
          refusedPerHour: { limit: 100, used: 0, windowSeconds: 3600 },
          requestsPerSecond: { limit: 50, burst: 200 },
          cachedBytesPerSecond: { limit: 50 * MB },
          uncachedBytesPerSecond: { limit: 10 * MB },
        },
        blockedUntil: null,
      },
    ],
    history: sampleHistory(),
    repositories: [
      { repository: 'web/npm-remote', requests: 30000, cachedBytes: 1_000_000_000, uncachedBytes: 50_000_000, upstreamRequests: 120, refused: 0 },
      { repository: 'web/npm-local', requests: 18210, cachedBytes: 203_000_000, uncachedBytes: 0, upstreamRequests: 0, refused: 3 },
    ],
    credentials: [
      { credential: { kind: 'session' }, requests: 210, cachedBytes: 3_000_000, uncachedBytes: 0 },
      {
        credential: { kind: 'token', id: '0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e11', hint: 'qdr_pat_3kQ9', description: 'laptop npm' },
        requests: 30000,
        cachedBytes: 800_000_000,
        uncachedBytes: 50_000_000,
      },
      {
        credential: { kind: 'token', id: '0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e12', hint: 'qdr_pat_9xZ2', description: 'build server for the release candidates' },
        requests: 18000,
        cachedBytes: 400_000_000,
        uncachedBytes: 0,
      },
    ],
  };
}

/** The explanation of R-LIM-035's example: Alice at web/npm-remote. */
export function sampleExplanation(): LimitExplanation {
  const place: LimitPlace = { kind: 'repository', repository: 'web/npm-remote' };
  const r = { id: '0193f2a0-7c1e-7b55-9a10-3f2c8d4e5a61', who: { kind: 'group' as const, group: 'contractors' }, profile: 'restricted', profileName: 'Restricted', builtin: false, capped: true };
  return {
    subject: { kind: 'user', id: 'alice', name: 'alice', displayName: 'Alice Example', deleted: false },
    repository: 'web/npm-remote',
    mode: 'enforce',
    administrator: false,
    steps: [
      { place, step: 'single', outcome: 'none' },
      { place, step: 'groups', outcome: 'matched', rules: [r] },
    ],
    place,
    rule: r,
    effective: { ...PROFILE_FIGURES['restricted']!, uncachedBytesPerHour: 20 * GB },
    clamped: {},
    figureSources: { uncachedBytesPerHour: 'override' },
    ceiling: { class: 'signedIn', uncachedBytesPerHour: 50 * GB, upstreamRequestsPerHour: 2000 },
  };
}

function placeKey(place: LimitPlace): string {
  switch (place.kind) {
    case 'instance':
      return 'instance';
    case 'project':
      return `project:${place.project}`;
    case 'repository':
      return `repository:${place.repository}`;
    case 'bastion':
      return `bastion:${place.bastion}`;
  }
}

export class FakeLimitsApi extends LimitsApi {
  calls: string[] = [];
  /** The bodies of the writes, in order. */
  bodies: unknown[] = [];
  ifMatch: string[] = [];
  profileList: LimitProfile[] = sampleProfiles();
  rulesByPlace = new Map<string, LimitRule[]>([
    ['instance', sampleInstanceRules()],
    ['project:web', [rule('r-project', { kind: 'project', project: 'web' }, { kind: 'group', group: 'web-devs', displayName: 'Web developers' }, 'technical', { capped: true })]],
    ['repository:web/npm-remote', sampleRepositoryRules()],
    ['bastion:partners', sampleBastionRules()],
  ]);
  ceilingsByRepo = new Map<string, LimitCeilings>([['platform/dockerhub', sampleCeilings()]]);
  settingsValue: LimitSettings = { mode: 'audit', blockSeconds: 900, version: 4, updatedAt: '2026-11-20T08:00:00Z', updatedBy: 'ada' };
  preview: ModePreview = samplePreview();
  maximums = new Map<string, ProjectMaximum>([
    [
      'web',
      {
        project: 'web',
        profile: 'technical',
        overrides: { bytesPerDay: 2000 * GB },
        effective: { ...PROFILE_FIGURES['technical']!, bytesPerDay: 2000 * GB },
        version: 1,
        updatedAt: '2026-11-20T08:30:00Z',
        updatedBy: 'ada',
      },
    ],
  ]);
  projectRowsList: ProjectLimitsRow[] | null = null;
  blockList: LimitBlock[] = sampleBlocks();
  usageRows: UsageRow[] = sampleUsageRows();
  detailValue: UsageDetail = sampleDetail();
  explanation: LimitExplanation = sampleExplanation();
  suggestions: LimitSubjectSuggestion[] = [
    { kind: 'technicalToken', id: 'tt-1', name: 'deploy-prod', account: 'sa:web/partner', level: 'project', deleted: false },
    { kind: 'ci', id: 'gitlab-internal:4711', issuer: 'gitlab-internal', projectId: '4711', projectPath: 'acme/web' },
  ];
  /** A failure for the next calls of a method, by its name. */
  failures = new Map<string, ApiError>();
  /** The queries of the usage lists, in order. */
  usageQueries: { scope: UsageScope; query: UsageQuery }[] = [];

  private fail(method: string): void {
    const f = this.failures.get(method);
    if (f) throw f;
  }

  private etag(v: { version: number }): string {
    return `"${v.version}"`;
  }

  override async profiles(): Promise<readonly LimitProfile[]> {
    this.fail('profiles');
    return this.profileList;
  }

  override async profile(key: string): Promise<Versioned<LimitProfile>> {
    this.fail('profile');
    const p = this.profileList.find((x) => x.key === key);
    if (!p) throw new ApiError(404, { code: 'not_found' }, null);
    return { value: p, etag: this.etag(p) };
  }

  override async createProfile(write: LimitProfileWrite): Promise<Versioned<LimitProfile>> {
    this.fail('createProfile');
    this.calls.push(`createProfile ${write.key}`);
    this.bodies.push(write);
    const p = profile(write.key!, write.name, false, { ...figures({}), ...write.figures } as Figures, 0, write.description);
    this.profileList = [...this.profileList, p];
    return { value: p, etag: this.etag(p) };
  }

  override async replaceProfile(key: string, write: LimitProfileWrite, etag: string): Promise<Versioned<LimitProfile>> {
    this.fail('replaceProfile');
    this.calls.push(`replaceProfile ${key}`);
    this.bodies.push(write);
    this.ifMatch.push(etag);
    const old = this.profileList.find((p) => p.key === key)!;
    const next: LimitProfile = { ...old, name: write.name, description: write.description ?? '', figures: { ...figures({}), ...write.figures } as Figures, version: old.version + 1 };
    this.profileList = this.profileList.map((p) => (p.key === key ? next : p));
    return { value: next, etag: this.etag(next) };
  }

  override async deleteProfile(key: string): Promise<void> {
    this.calls.push(`deleteProfile ${key}`);
    this.profileList = this.profileList.filter((p) => p.key !== key);
  }

  override async rules(place: LimitPlace): Promise<readonly LimitRule[]> {
    this.fail('rules');
    return this.rulesByPlace.get(placeKey(place)) ?? [];
  }

  override async rule(place: LimitPlace, id: string): Promise<Versioned<LimitRule>> {
    const r = (this.rulesByPlace.get(placeKey(place)) ?? []).find((x) => x.id === id);
    if (!r) throw new ApiError(404, { code: 'not_found' }, null);
    return { value: r, etag: this.etag(r) };
  }

  override async createRule(place: LimitPlace, write: LimitRuleWrite): Promise<Versioned<LimitRule>> {
    this.fail('createRule');
    this.calls.push(`createRule ${placeKey(place)}`);
    this.bodies.push(write);
    const r = rule(`r-${this.calls.length}`, place, write.who!, write.profile, { overrides: write.overrides, note: write.note });
    this.rulesByPlace.set(placeKey(place), [...(this.rulesByPlace.get(placeKey(place)) ?? []), r]);
    return { value: r, etag: this.etag(r) };
  }

  override async replaceRule(place: LimitPlace, id: string, write: LimitRuleWrite, etag: string): Promise<Versioned<LimitRule>> {
    this.fail('replaceRule');
    this.calls.push(`replaceRule ${placeKey(place)} ${id}`);
    this.bodies.push(write);
    this.ifMatch.push(etag);
    const list = this.rulesByPlace.get(placeKey(place)) ?? [];
    const old = list.find((r) => r.id === id)!;
    const next = { ...old, profile: write.profile, overrides: write.overrides, note: write.note, version: old.version + 1 };
    this.rulesByPlace.set(placeKey(place), list.map((r) => (r.id === id ? next : r)));
    return { value: next, etag: this.etag(next) };
  }

  override async deleteRule(place: LimitPlace, id: string): Promise<void> {
    this.calls.push(`deleteRule ${placeKey(place)} ${id}`);
    this.rulesByPlace.set(placeKey(place), (this.rulesByPlace.get(placeKey(place)) ?? []).filter((r) => r.id !== id));
  }

  override async ceilings(ref: string): Promise<Versioned<LimitCeilings>> {
    this.fail('ceilings');
    const c = this.ceilingsByRepo.get(ref) ?? {
      repository: ref,
      anonymous: { uncachedBytesPerHour: null, upstreamRequestsPerHour: null },
      signedIn: { uncachedBytesPerHour: null, upstreamRequestsPerHour: null },
      version: 1,
      updatedAt: '2026-11-20T09:10:00Z',
      updatedBy: null,
    };
    return { value: c, etag: this.etag(c) };
  }

  override async replaceCeilings(
    ref: string,
    body: { readonly anonymous: CeilingFigures; readonly signedIn: CeilingFigures },
    etag: string,
  ): Promise<Versioned<LimitCeilings>> {
    this.calls.push(`replaceCeilings ${ref}`);
    this.bodies.push(body);
    this.ifMatch.push(etag);
    const old = (await this.ceilings(ref)).value;
    const next = { ...old, ...body, version: old.version + 1 };
    this.ceilingsByRepo.set(ref, next);
    return { value: next, etag: this.etag(next) };
  }

  override async settings(): Promise<Versioned<LimitSettings>> {
    this.fail('settings');
    return { value: this.settingsValue, etag: this.etag(this.settingsValue) };
  }

  override async patchSettings(patch: { mode?: LimitMode; blockSeconds?: number }, etag: string): Promise<Versioned<LimitSettings>> {
    this.fail('patchSettings');
    this.calls.push(`patchSettings ${JSON.stringify(patch)}`);
    this.ifMatch.push(etag);
    this.settingsValue = { ...this.settingsValue, ...patch, version: this.settingsValue.version + 1 };
    return { value: this.settingsValue, etag: this.etag(this.settingsValue) };
  }

  override async modePreview(): Promise<ModePreview> {
    this.fail('modePreview');
    return this.preview;
  }

  override async maximum(project: string): Promise<Versioned<ProjectMaximum> | null> {
    const m = this.maximums.get(project);
    return m ? { value: m, etag: this.etag(m) } : null;
  }

  override async putMaximum(project: string, write: ProjectMaximumWrite, etag: string | null): Promise<Versioned<ProjectMaximum>> {
    this.calls.push(`putMaximum ${project}`);
    this.bodies.push(write);
    if (etag) this.ifMatch.push(etag);
    const m: ProjectMaximum = {
      project,
      profile: write.profile,
      overrides: write.overrides,
      effective: { ...PROFILE_FIGURES[write.profile]!, ...write.overrides } as Figures,
      version: (this.maximums.get(project)?.version ?? 0) + 1,
      updatedAt: '2026-11-20T10:00:00Z',
      updatedBy: 'ada',
    };
    this.maximums.set(project, m);
    return { value: m, etag: this.etag(m) };
  }

  override async deleteMaximum(project: string): Promise<void> {
    this.calls.push(`deleteMaximum ${project}`);
    this.maximums.delete(project);
  }

  override async projectRows(): Promise<readonly ProjectLimitsRow[]> {
    this.fail('projectRows');
    if (this.projectRowsList) return this.projectRowsList;
    const keys = new Set([...this.maximums.keys(), 'web']);
    return [...keys].map((key) => {
      const m = this.maximums.get(key);
      return {
        project: key,
        displayName: key === 'web' ? 'Web platform' : key,
        maximum: m ? { value: m, etag: this.etag(m) } : null,
        rules: (this.rulesByPlace.get(`project:${key}`)?.length ?? 0) + (this.rulesByPlace.get(`repository:${key}/npm-remote`)?.length ?? 0),
      };
    });
  }

  override async blocks(filter: { active?: boolean; place?: string }): Promise<readonly LimitBlock[]> {
    this.fail('blocks');
    this.calls.push(`blocks active=${filter.active}`);
    return filter.active === false ? this.blockList : this.blockList.filter((b) => b.liftedAt === null);
  }

  override async liftBlock(id: string): Promise<void> {
    this.calls.push(`liftBlock ${id}`);
    this.blockList = this.blockList.map((b) => (b.id === id ? { ...b, liftedAt: new Date().toISOString(), liftedBy: 'ada' } : b));
  }

  override async explain(subject: { kind: UsageSubjectKind; id: string }, repository: string): Promise<LimitExplanation> {
    this.fail('explain');
    this.calls.push(`explain ${subject.kind}/${subject.id} ${repository}`);
    return this.explanation;
  }

  override async myLimits(repository: string): Promise<LimitExplanation> {
    this.fail('myLimits');
    this.calls.push(`myLimits ${repository}`);
    return this.explanation;
  }

  override async subjects(q: string, kinds: readonly ('technicalToken' | 'ci')[], project: string | null): Promise<readonly LimitSubjectSuggestion[]> {
    this.calls.push(`subjects ${q} ${kinds.join(',')} ${project ?? ''}`);
    const t = q.toLowerCase();
    return this.suggestions.filter(
      (s) => kinds.includes(s.kind) && (s.kind === 'technicalToken' ? s.name.includes(t) || s.account.includes(t) : (s.projectPath ?? s.id).includes(t)),
    );
  }

  override async usage(scope: UsageScope, query: UsageQuery): Promise<UsageList> {
    this.fail('usage');
    this.usageQueries.push({ scope, query });
    let items = this.usageRows;
    if (query.kind?.length) items = items.filter((r) => query.kind!.includes(r.subject.kind));
    if (query.state) items = items.filter((r) => r.state === query.state);
    if (query.q) items = items.filter((r) => JSON.stringify(r.subject).toLowerCase().includes(query.q!.toLowerCase()));
    return { period: { from: '2026-11-19T10:00:00Z', to: '2026-11-20T10:00:00Z' }, mode: this.settingsValue.mode, items, nextCursor: null };
  }

  override async subjectUsage(scope: UsageScope, kind: UsageSubjectKind, id: string, period: UsagePeriod): Promise<UsageDetail> {
    this.fail('subjectUsage');
    this.calls.push(`subjectUsage ${scope.project ?? 'instance'} ${kind}/${id} ${period}`);
    return { ...this.detailValue, mode: this.settingsValue.mode };
  }

  override async myUsage(period: UsagePeriod): Promise<UsageDetail> {
    this.fail('myUsage');
    this.calls.push(`myUsage ${period}`);
    return { ...this.detailValue, mode: this.settingsValue.mode };
  }
}

export function provideLimitsFakes(): Provider[] {
  return [{ provide: LimitsApi, useClass: FakeLimitsApi }];
}
