import type { operations } from './generated/schema';
import { Schema } from './types';

/*
 * The shapes of the download limits and the usage overview (R-LIM-031 to R-LIM-035 of docs/spec/usage-and-limits.md), as the
 * generated client of `api/openapi.yaml` describes them. Every screen imports these names from here (through limits-api.ts).
 * The names are the screens' words; the schemas behind them are the API's.
 */

/** The figures of a profile, in the order of R-LIM-004 (the API names). */
export type Figure = Schema<'LimitFigureName'>;

/** Every figure of a profile; `null` is unlimited. An answer carries every member (R-LIM-031). */
export type Figures = Schema<'LimitFigures'>;

/** Single figures that replace the profile's (R-LIM-011): a number, or `null` for unlimited; an absent figure keeps the profile's. */
export type FigureOverrides = Schema<'LimitFigureOverrides'>;

/** R-LIM-031. */
export type LimitProfile = Schema<'LimitProfile'>;

/** A write of a profile: `key` on create only; a figure missing from `figures` is `null`. */
export type LimitProfileWrite = Pick<Schema<'LimitProfileWrite'>, 'key' | 'name' | 'description' | 'figures'>;

/** The "who" of a rule as an answer carries it (R-LIM-032); the read-only members are optional. */
export type LimitWho = Schema<'LimitWho'>;

/** The kinds of caller an `every` rule names (R-LIM-011). */
export type CallerKind = Schema<'LimitWhoEvery'>['callerKind'];

export type LimitWhoKind = LimitWho['kind'];

/** A place of rules: the instance, a project, a repository (`<projectKey>/<repoKey>`). */
export type LimitPlace = Schema<'LimitPlace'>;

/** A rule (an assignment), R-LIM-032. */
export type LimitRule = Schema<'LimitAssignment'>;

/** A write of a rule: `who` on create only (it is immutable). A user and a group by name, a token by ID, an issuer by name. */
export type LimitRuleWrite = Pick<Schema<'LimitAssignmentWrite'>, 'who'> &
  Required<Pick<Schema<'LimitAssignmentWrite'>, 'profile' | 'overrides' | 'note'>>;

/** The two figures of a ceiling (R-LIM-015); `null` is unlimited. */
export type CeilingFigures = Schema<'LimitCeiling'>;

/** The ceilings of a remote repository (R-LIM-032). */
export type LimitCeilings = Schema<'LimitCeilings'>;

export type LimitMode = Schema<'LimitMode'>;

/** R-LIM-033. */
export type LimitSettings = Schema<'LimitSettings'>;

/** What Enforce would have done in the last 24 hours (R-LIM-033). */
export type ModePreview = Schema<'LimitModePreview'>;

/** A project's maximum (R-LIM-033). */
export type ProjectMaximum = Schema<'LimitProjectMaximum'>;

export type ProjectMaximumWrite = Required<Pick<Schema<'LimitProjectMaximumWrite'>, 'profile' | 'overrides'>>;

/** One row of `GET /api/v1/limits/projects` (Q-API-351): a project's maximum and its rules. */
export type LimitProjectRow = Schema<'LimitProjectRow'>;

/** A block (R-LIM-033). */
export type LimitBlock = Schema<'LimitBlock'>;

/** The kinds of subject of the usage store (R-LIM-034). */
export type UsageSubjectKind = Schema<'UsageSubjectKind'>;

/** A subject of the usage store as the API names it (R-LIM-034). */
export type UsageSubject = Schema<'UsageSubject'>;

/** The state of a row (R-LIM-034). */
export type UsageState = Schema<'UsageState'>;

type UsageListQuery = NonNullable<operations['listUsage']['parameters']['query']>;

/** The states a list can be filtered by. */
export type UsageStateFilter = NonNullable<UsageListQuery['state']>;

/** The period of a list: the last hour, the last 24 hours, the last 30 days. */
export type UsagePeriod = NonNullable<UsageListQuery['period']>;

export type UsageSort = NonNullable<UsageListQuery['sort']>;

/** The time range an answer covers. */
export type UsagePeriodRange = Schema<'UsagePeriod'>;

/** The tightest hour or day figure of a subject now (Q-UI-732). */
export type Utilisation = Schema<'UsageUtilisation'>;

/** A row of the usage list (R-LIM-034). */
export type UsageRow = Schema<'UsageRow'>;

/** The answer of a usage list; its cursor is `nextCursor` (R-API-010, Q-LIM-46). */
export type UsageList = Schema<'UsageList'>;

/** One figure of a budget in the detail (R-LIM-034): a windowed figure has `used`, a rate only its limit. */
export type BudgetFigure = Schema<'UsageBudgetFigure'>;

/** The figures of a budget key: every figure but the burst, which is the request rate's `burst`. */
export type BudgetFigures = Schema<'UsageBudgetFigures'>;

/** The rule a budget key stands under, as the detail and the explanation name it. */
export type BudgetRule = Schema<'LimitRuleRef'>;

/** One budget key of a subject: a place and its rule (R-LIM-014). */
export type UsageBudget = Schema<'UsageBudget'>;

export type UsageTotals = Schema<'UsageTotals'>;

/** A five-minute point of the last 24 hours. */
export type UsagePoint = Schema<'UsagePoint'>;

export type UsageByRepository = Schema<'UsageByRepository'>;

export type UsageByCredential = Schema<'UsageByCredential'>;

export type UsageCredential = UsageByCredential['credential'];

/** A subject's detail (R-LIM-034); also the answer of `GET /api/v1/users/me/usage`. */
export type UsageDetail = Schema<'UsageDetail'>;

/** One step of the resolution of R-LIM-013 as the explanation lists it (R-LIM-035). */
export type ExplainStep = Schema<'LimitExplainStep'>;

/** Which limit applies (R-LIM-035). */
export type LimitExplanation = Schema<'LimitExplanation'>;

/** A suggestion of `GET /api/v1/limits/subjects` (R-LIM-034) for the rule dialog: a technical token or a CI project. */
export type LimitSubjectSuggestion = Schema<'LimitSubjectSuggestion'>;
