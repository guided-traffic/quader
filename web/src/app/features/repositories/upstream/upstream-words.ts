import { ApiError } from '../../../api/problem';
import { DeliveryDecision, UpstreamCatalogue, UpstreamVerdict } from '../../../api/upstream-catalogue-api';

/** The delivery mode of a remote repository (R-CUR-001). */
export type DeliveryMode = UpstreamCatalogue['mode'];

/** The line about the mode at the top of the tab Upstream (R-CUR-018). */
export function modeSentence(mode: DeliveryMode): string {
  return mode === 'onlyAllowed'
    ? $localize`:@@upstream.mode.only-allowed:Delivers only what is allowed.`
    : $localize`:@@upstream.mode.all-except-blocked:Delivers everything except what is blocked.`;
}

/** The name of a mode where a choice is offered (the settings' section **Delivery**, R-CUR-018). */
export function modeName(mode: DeliveryMode): string {
  return mode === 'onlyAllowed'
    ? $localize`:@@upstream.mode.only-allowed.name:Only what is allowed`
    : $localize`:@@upstream.mode.all-except-blocked.name:Everything except what is blocked`;
}

/** What a state badge of a package or a version says, and its look. */
export interface StateBadge {
  readonly text: string;
  /** `badge` neutral, `warn` for a block. */
  readonly look: 'badge' | 'warn';
  readonly icon: string;
}

/**
 * The state of a package or a version in this repository (R-CUR-018): **Blocked** by a rule, **Allowed** by a rule, **Requested**
 * while an open request asks for what is not delivered, **Not allowed** in mode `onlyAllowed` without a rule, and **Delivered** by the
 * mode `allExceptBlocked` without a rule.
 */
export function stateBadge(delivery: DeliveryDecision, requested: boolean): StateBadge {
  if (!delivery.delivered) {
    if (delivery.reason === 'blocked') return { text: $localize`:@@upstream.state.blocked:Blocked`, look: 'warn', icon: 'block' };
    if (requested) return { text: $localize`:@@upstream.state.requested:Requested`, look: 'badge', icon: 'pending' };
    return { text: $localize`:@@upstream.state.not-allowed:Not allowed`, look: 'badge', icon: 'remove_circle_outline' };
  }
  if (delivery.decidedBy.effect === 'allow') return { text: $localize`:@@upstream.state.allowed:Allowed`, look: 'badge', icon: 'check' };
  return { text: $localize`:@@upstream.state.delivered:Delivered`, look: 'badge', icon: 'check' };
}

/** The rule that made a decision, in one muted line ("Rule: @acme/*"); nothing where the mode decided. */
export function decidedByText(delivery: DeliveryDecision): string | null {
  const by = delivery.decidedBy;
  if (!by.kind || !by.subject) return null;
  return $localize`:@@upstream.state.rule:Rule: ${by.subject}:subject:`;
}

/** The kind of a level above a package, as a noun (R-CUR-007): scope, namespace, project, suite, component, architecture. */
export function levelKindText(kind: string): string {
  switch (kind) {
    case 'scope':
      return $localize`:@@upstream.level.scope:Scope`;
    case 'namespace':
      return $localize`:@@upstream.level.namespace:Namespace`;
    case 'project':
      return $localize`:@@upstream.level.project:Project`;
    case 'suite':
      return $localize`:@@upstream.level.suite:Suite`;
    case 'component':
      return $localize`:@@upstream.level.component:Component`;
    case 'architecture':
      return $localize`:@@upstream.level.architecture:Architecture`;
    default:
      return kind;
  }
}

/**
 * A level the upstream does not have, in one sentence by the level's kind (R-CUR-018, Q-CUR-165): "Docker Hub has no namespace
 * “guided-traffic”." The upstream's name is a product name or a host and is not translated.
 */
export function unknownLevelText(kind: string, upstream: string, name: string): string {
  switch (kind) {
    case 'scope':
      return $localize`:@@upstream.unknown.scope:${upstream}:upstream: has no scope “${name}:name:”.`;
    case 'namespace':
      return $localize`:@@upstream.unknown.namespace:${upstream}:upstream: has no namespace “${name}:name:”.`;
    case 'project':
      return $localize`:@@upstream.unknown.project:${upstream}:upstream: has no public project “${name}:name:”.`;
    case 'suite':
      return $localize`:@@upstream.unknown.suite:${upstream}:upstream: has no suite “${name}:name:”.`;
    case 'component':
      return $localize`:@@upstream.unknown.component:${upstream}:upstream: has no component “${name}:name:” in this suite.`;
    case 'architecture':
      return $localize`:@@upstream.unknown.architecture:${upstream}:upstream: has no architecture “${name}:name:” in this suite.`;
    default:
      return $localize`:@@upstream.unknown.level:${upstream}:upstream: has no level “${name}:name:”.`;
  }
}

/**
 * The naming rule a level's name breaks, built from the rule and the characters it forbids (R-CUR-018, Q-CUR-165): "Docker Hub allows
 * only lowercase letters and digits in this name, no “-”." Null for a rule the screen does not know.
 */
export function nameRuleText(rule: string | null, upstream: string, forbidden: string | null): string | null {
  const chars = [...(forbidden ?? '')].map((char) => $localize`:@@upstream.rule.char:“${char}:char:”`).join(', ');
  switch (rule) {
    case 'lowercaseDigits':
      return chars
        ? $localize`:@@upstream.rule.lowercase-digits:${upstream}:upstream: allows only lowercase letters and digits in this name, no ${chars}:chars:.`
        : $localize`:@@upstream.rule.lowercase-digits-only:${upstream}:upstream: allows only lowercase letters and digits in this name.`;
    case 'lowercaseDigitsUnderscore':
      return chars
        ? $localize`:@@upstream.rule.lowercase-digits-underscore:${upstream}:upstream: allows only lowercase letters, digits and underscores in this name, no ${chars}:chars:.`
        : $localize`:@@upstream.rule.lowercase-digits-underscore-only:${upstream}:upstream: allows only lowercase letters, digits and underscores in this name.`;
    default:
      return null;
  }
}

/** The link to a level the name may have meant (Q-CUR-165): "Did you mean “guidedtraffic”?" */
export function suggestionText(name: string): string {
  return $localize`:@@upstream.unknown.suggestion:Did you mean “${name}:name:”?`;
}

/** The flags of a hit or a version that the screen names (R-CUR-007); a flag it does not know is shown as the upstream's word. */
export function flagText(flag: string): string {
  switch (flag) {
    case 'deprecated':
      return $localize`:@@upstream.flag.deprecated:Deprecated`;
    case 'yanked':
      return $localize`:@@upstream.flag.yanked:Yanked`;
    case 'unlisted':
      return $localize`:@@upstream.flag.unlisted:Unlisted`;
    case 'prerelease':
      return $localize`:@@upstream.flag.prerelease:Pre-release`;
    case 'official':
      return $localize`:@@upstream.flag.official:Official`;
    case 'verified':
      return $localize`:@@upstream.flag.verified:Verified publisher`;
    case 'vulnerable':
      return $localize`:@@upstream.flag.vulnerable:Vulnerable`;
    case 'insecure':
      return $localize`:@@upstream.flag.insecure:Insecure`;
    case 'unsigned-index':
      return $localize`:@@upstream.flag.unsigned-index:Unsigned index`;
    default:
      return flag;
  }
}

/**
 * The vulnerability verdict of a version that is not stored (R-CUR-009, R-CUR-014), in words that say what they judge, since they
 * stand under the cache's state (Q-CUR-157); null where it says nothing.
 */
export function verdictText(v: UpstreamVerdict): string | null {
  switch (v.verdict) {
    case 'none':
      return $localize`:@@upstream.verdict.none-known:No known vulnerability`;
    case 'blocked':
      switch (v.reason) {
        case 'malicious':
          return $localize`:@@upstream.verdict.malicious:Known as malicious`;
        case 'triage':
          return $localize`:@@upstream.verdict.triage:Blocked in triage`;
        case 'missing_data':
          return $localize`:@@upstream.verdict.missing-data:Vulnerability data incomplete`;
        default:
          return $localize`:@@upstream.verdict.blocked:Blocked: vulnerability`;
      }
    default:
      return null;
  }
}

/**
 * The sentence of a verdict that cannot exist for a format (R-CUR-014): Debian and Ansible Galaxy are not analysed for
 * vulnerabilities; an OCI image only once its platforms are stored.
 */
export function notCoveredText(format: string): string {
  switch (format) {
    case 'deb':
      return $localize`:@@upstream.verdict.not-covered.deb:Debian packages are not analysed for vulnerabilities.`;
    case 'ansible':
      return $localize`:@@upstream.verdict.not-covered.ansible:Ansible collections are not analysed for vulnerabilities.`;
    case 'oci':
      return $localize`:@@upstream.verdict.not-covered.oci:An image is analysed for vulnerabilities once its platforms are stored.`;
    default:
      return $localize`:@@upstream.verdict.not-covered:This repository's versions are not analysed for vulnerabilities.`;
  }
}

/** An example of a sub-section in the format's names (the rule list's hint, R-CUR-001's terms). */
export function sectionExample(format: string): string {
  switch (format) {
    case 'nuget':
      return 'Contoso.*';
    case 'oci':
      return 'library/*';
    case 'ansible':
      return 'community.*';
    case 'deb':
      return 'lib*';
    default:
      return '@acme/*';
  }
}

/** The time `seconds` from now in the viewer's language, hours and minutes: "10:42". */
export function clockAfter(seconds: number, now: number = Date.now()): string {
  const locale = (typeof $localize !== 'undefined' && $localize.locale) || 'en-US';
  return new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit' }).format(new Date(now + seconds * 1000));
}

/**
 * The catalogue's own states in the tab's words (R-CUR-018): the upstream assumed offline until the circuit's next probe, the offline
 * mode, the budget used up with its wait, an answer above the bound, a call the upstream does not answer; null for any other
 * failure, which the shared notice says with **Retry**.
 */
export function catalogueState(error: ApiError, what: 'package' | 'list' = 'list'): string | null {
  const wait = error.retryAfterSeconds;
  switch (error.code) {
    case 'upstream_assumed_offline':
      return wait !== null && wait > 1
        ? $localize`:@@upstream.state.offline-until:The upstream is assumed offline until ${clockAfter(wait)}:time:; Quader retries then.`
        : $localize`:@@upstream.state.offline-retry:The upstream is busy with a client's fetch or assumed offline; try again in a moment.`;
    case 'offline':
      return $localize`:@@upstream.state.offline-mode:This repository is in offline mode: it asks its upstream nothing.`;
    case 'catalogue_budget_exhausted':
      return wait !== null
        ? $localize`:@@upstream.state.budget-wait:The upstream catalogue's budget is used up; try again in ${wait}:seconds: s.`
        : $localize`:@@upstream.state.budget:The upstream catalogue's budget is used up; try again in a minute.`;
    case 'catalogue_answer_too_large':
      return what === 'package'
        ? $localize`:@@upstream.state.too-large-package:This package's document is larger than the catalogue reads; Quader does not read it for browsing.`
        : $localize`:@@upstream.state.too-large:The upstream's answer is larger than the catalogue reads; Quader does not read it for browsing.`;
    case 'catalogue_not_offered':
      return $localize`:@@upstream.state.not-offered:The upstream of this repository does not answer this.`;
    default:
      return null;
  }
}
