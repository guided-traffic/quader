/*
 * The shapes of the download limits and the usage overview (R-LIM-031 to R-LIM-035 of docs/spec/usage-and-limits.md), written
 * by hand from the JSON examples of the specification until `api/openapi.yaml` describes the operations. Every screen imports
 * these names from here (through limits-api.ts); once the generated client carries the operations, each name below becomes
 * `Schema<'…'>` of ./types and nothing else changes. Only what the specification fixes is here.
 */

/** The figures of a profile, in the order of R-LIM-004 (the API names). */
export type Figure =
  | 'requestsPerSecond'
  | 'requestsBurst'
  | 'requestsPerHour'
  | 'cachedBytesPerSecond'
  | 'cachedBytesPerHour'
  | 'uncachedBytesPerSecond'
  | 'uncachedBytesPerHour'
  | 'upstreamRequestsPerHour'
  | 'parallelDownloads'
  | 'bytesPerDay'
  | 'refusedPerHour';

/** Every figure of a profile; `null` is unlimited. An answer carries every member (R-LIM-031). */
export type Figures = Readonly<Record<Figure, number | null>>;

/** Single figures that replace the profile's (R-LIM-011): a number, or `null` for unlimited; an absent figure keeps the profile's. */
export type FigureOverrides = Readonly<Partial<Record<Figure, number | null>>>;

/** R-LIM-031. */
export interface LimitProfile {
  readonly key: string;
  readonly name: string;
  readonly description: string;
  readonly builtin: boolean;
  readonly figures: Figures;
  readonly usedBy: { readonly assignments: number; readonly projectMaximums: number };
  readonly version: number;
  readonly createdAt: string;
  readonly createdBy: string | null;
  readonly updatedAt: string;
  readonly updatedBy: string | null;
}

/** A write of a profile: `key` on create only; a figure missing from `figures` is `null`. */
export interface LimitProfileWrite {
  readonly key?: string;
  readonly name: string;
  readonly description: string;
  readonly figures: FigureOverrides;
}

/** The kinds of caller an `every` rule names (R-LIM-011). */
export type CallerKind = 'user' | 'technicalToken' | 'ci' | 'anonymous' | 'administrator';

/** The "who" of a rule as an answer carries it (R-LIM-032); the read-only members are optional. */
export type LimitWho =
  | { readonly kind: 'user'; readonly user: string; readonly displayName?: string | null; readonly deleted?: boolean }
  | {
      readonly kind: 'technicalToken';
      readonly technicalToken: string;
      readonly name?: string;
      readonly account?: string;
      readonly deleted?: boolean;
    }
  | {
      readonly kind: 'ciProject';
      readonly issuer: string;
      readonly projectId: string;
      readonly projectPath?: string;
      readonly deleted?: boolean;
    }
  | { readonly kind: 'address'; readonly prefix: string }
  | { readonly kind: 'group'; readonly group: string; readonly displayName?: string; readonly deleted?: boolean }
  | { readonly kind: 'every'; readonly callerKind: CallerKind };

export type LimitWhoKind = LimitWho['kind'];

/** A place of rules: the instance, a project, a repository (`<projectKey>/<repoKey>`). */
export type LimitPlace =
  | { readonly kind: 'instance' }
  | { readonly kind: 'project'; readonly project: string }
  | { readonly kind: 'repository'; readonly repository: string };

/** A rule (an assignment), R-LIM-032. */
export interface LimitRule {
  readonly id: string;
  readonly place: LimitPlace;
  readonly who: LimitWho;
  /** The profile's key. */
  readonly profile: string;
  readonly overrides: FigureOverrides;
  readonly effective: Figures;
  /** The figures the project's maximum or the instance lowers now, with the value that applies (R-LIM-016). */
  readonly clamped: FigureOverrides;
  readonly capped: boolean;
  readonly builtin: boolean;
  readonly note: string;
  readonly version: number;
  readonly createdAt: string;
  readonly createdBy: string | null;
  readonly updatedAt: string;
  readonly updatedBy: string | null;
}

/** A write of a rule: `who` on create only (it is immutable). A user and a group by name, a token by ID, an issuer by name. */
export interface LimitRuleWrite {
  readonly who?: LimitWho;
  readonly profile: string;
  readonly overrides: FigureOverrides;
  readonly note: string;
}

/** The two figures of a ceiling (R-LIM-015); `null` is unlimited. */
export interface CeilingFigures {
  readonly uncachedBytesPerHour: number | null;
  readonly upstreamRequestsPerHour: number | null;
}

/** The ceilings of a remote repository (R-LIM-032). */
export interface LimitCeilings {
  readonly repository: string;
  readonly anonymous: CeilingFigures;
  readonly signedIn: CeilingFigures;
  readonly version: number;
  readonly updatedAt: string;
  readonly updatedBy: string | null;
}

export type LimitMode = 'audit' | 'enforce';

/** R-LIM-033. */
export interface LimitSettings {
  readonly mode: LimitMode;
  readonly blockSeconds: number;
  readonly version: number;
  readonly updatedAt: string;
  readonly updatedBy: string | null;
}

/** What Enforce would have done in the last 24 hours (R-LIM-033). */
export interface ModePreview {
  readonly period: UsagePeriodRange;
  readonly subjects: number;
  readonly refusedRequests: number;
  readonly slowedDownloads: number;
  readonly blockedSubjects: number;
  readonly byFigure: readonly { readonly figure: string; readonly subjects: number; readonly events: number }[];
  readonly byKind: readonly { readonly kind: UsageSubjectKind; readonly subjects: number }[];
}

/** A project's maximum (R-LIM-033). */
export interface ProjectMaximum {
  readonly project: string;
  readonly profile: string;
  readonly overrides: FigureOverrides;
  readonly effective: Figures;
  readonly version: number;
  readonly updatedAt: string;
  readonly updatedBy: string | null;
}

export interface ProjectMaximumWrite {
  readonly profile: string;
  readonly overrides: FigureOverrides;
}

/** A block (R-LIM-033). */
export interface LimitBlock {
  readonly id: string;
  readonly subject: UsageSubject;
  readonly place: LimitPlace;
  readonly startedAt: string;
  readonly until: string;
  readonly refused: number;
  readonly limit: number;
  readonly liftedAt: string | null;
  readonly liftedBy: string | null;
}

/** The kinds of subject of the usage store (R-LIM-034). */
export type UsageSubjectKind = 'user' | 'technicalToken' | 'ci' | 'anonymous' | 'system';

/** A subject of the usage store as the API names it (R-LIM-034). */
export type UsageSubject =
  | { readonly kind: 'user'; readonly id: string; readonly name: string; readonly displayName?: string | null }
  | {
      readonly kind: 'technicalToken';
      readonly id: string;
      readonly name: string;
      readonly account: string;
      readonly level: 'instance' | 'project' | 'repository';
      readonly deleted?: boolean;
    }
  | {
      readonly kind: 'ci';
      readonly id: string;
      readonly issuer: string;
      readonly projectId: string;
      readonly projectPath?: string | null;
    }
  | { readonly kind: 'anonymous'; readonly id: string; readonly address: string }
  | { readonly kind: 'system'; readonly id: string };

/** The state of a row (R-LIM-034). */
export type UsageState = 'normal' | 'close' | 'limitReached' | 'wouldBeLimited' | 'blocked' | 'unlimited';

/** The states a list can be filtered by. */
export type UsageStateFilter = 'close' | 'limitReached' | 'wouldBeLimited' | 'blocked';

export type UsagePeriod = 'hour' | 'day' | '30d';

export interface UsagePeriodRange {
  readonly from: string;
  readonly to: string;
}

/** The tightest hour or day figure of a subject now (Q-UI-732). */
export interface Utilisation {
  readonly ratio: number;
  readonly figure: Figure;
  readonly place: LimitPlace;
}

/** A row of the usage list (R-LIM-034). */
export interface UsageRow {
  readonly subject: UsageSubject;
  readonly requests: number;
  readonly refused: number;
  readonly cachedBytes: number;
  readonly uncachedBytes: number;
  readonly upstreamRequests: number;
  readonly limitedEvents: number;
  readonly wouldLimitEvents: number;
  readonly utilisation: Utilisation | null;
  readonly state: UsageState;
  readonly blockedUntil: string | null;
}

/** The answer of a usage list. R-LIM-034 names the cursor `next`; R-API-010's envelope `nextCursor`: either is read. */
export interface UsageList {
  readonly period: UsagePeriodRange;
  readonly mode: LimitMode;
  readonly items: readonly UsageRow[];
  readonly next?: string | null;
  readonly nextCursor?: string | null;
}

export type UsageSort = 'requests' | 'cachedBytes' | 'uncachedBytes' | 'upstreamRequests' | 'refused' | 'utilisation' | 'name';

/** One figure of a budget in the detail (R-LIM-034): a windowed figure has `used`, a rate only its limit. */
export interface BudgetFigure {
  readonly limit: number | null;
  readonly used?: number;
  readonly windowSeconds?: number;
  readonly burst?: number | null;
}

/** The rule a budget key stands under, as the detail and the explanation name it. */
export interface BudgetRule {
  readonly id: string;
  readonly who: LimitWho;
  readonly profile: string;
  readonly builtin?: boolean;
  readonly capped?: boolean;
}

/** One budget key of a subject: a place and its rule (R-LIM-014). */
export interface UsageBudget {
  readonly place: LimitPlace;
  readonly rule: BudgetRule;
  readonly figures: Readonly<Partial<Record<Figure, BudgetFigure>>>;
  readonly blockedUntil: string | null;
}

export interface UsageTotals {
  readonly requests: number;
  readonly refused: number;
  readonly cachedBytes: number;
  readonly uncachedBytes: number;
  readonly upstreamRequests: number;
  readonly limitedEvents: number;
  readonly wouldLimitEvents: number;
}

/** A five-minute point of the last 24 hours. */
export interface UsagePoint {
  readonly start: string;
  readonly requests: number;
  readonly cachedBytes: number;
  readonly uncachedBytes: number;
  readonly upstreamRequests: number;
  readonly refused: number;
  readonly limitedEvents: number;
  readonly wouldLimitEvents: number;
}

export interface UsageByRepository {
  readonly repository: string;
  readonly requests: number;
  readonly cachedBytes: number;
  readonly uncachedBytes: number;
  readonly upstreamRequests: number;
  readonly refused: number;
}

export type UsageCredential =
  | { readonly kind: 'session' }
  | { readonly kind: 'token'; readonly id: string; readonly hint?: string | null; readonly description?: string | null };

export interface UsageByCredential {
  readonly credential: UsageCredential;
  readonly requests: number;
  readonly cachedBytes: number;
  readonly uncachedBytes: number;
}

/** A subject's detail (R-LIM-034); also the answer of `GET /api/v1/users/me/usage`. */
export interface UsageDetail {
  readonly subject: UsageSubject;
  readonly period: UsagePeriodRange;
  readonly mode: LimitMode;
  readonly totals: UsageTotals;
  readonly budgets: readonly UsageBudget[];
  readonly history: readonly UsagePoint[];
  readonly repositories: readonly UsageByRepository[];
  readonly credentials?: readonly UsageByCredential[];
}

/** One step of the resolution of R-LIM-013 as the explanation lists it (R-LIM-035). */
export interface ExplainStep {
  readonly place: LimitPlace;
  readonly step: 'single' | 'groups' | 'kind';
  readonly outcome: 'none' | 'matched';
  readonly rules?: readonly BudgetRule[];
}

/** Which limit applies (R-LIM-035). */
export interface LimitExplanation {
  readonly subject: UsageSubject;
  readonly repository: string;
  readonly mode: LimitMode;
  readonly administrator: boolean;
  readonly steps: readonly ExplainStep[];
  readonly place: LimitPlace;
  readonly rule: BudgetRule;
  readonly effective: Figures;
  readonly clamped: FigureOverrides;
  /** For a figure that did not come from the rule's profile: `override`, `group:<name>` or `clamp`. */
  readonly figureSources: Readonly<Partial<Record<Figure, string>>>;
  readonly ceiling: ({ readonly class: 'anonymous' | 'signedIn' } & CeilingFigures) | null;
}

/**
 * A suggestion of `GET /api/v1/limits/subjects` (R-LIM-034) for the rule dialog. The specification names the operation and not
 * its body; this is the subject's form of R-LIM-034 for the two kinds it suggests.
 */
export type LimitSubjectSuggestion = Extract<UsageSubject, { kind: 'technicalToken' | 'ci' }>;
