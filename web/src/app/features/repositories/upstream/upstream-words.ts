import { ApiError } from '../../../api/problem';
import { notScannedText } from '../content/content-shape';
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
 * while an open request asks for what is not delivered, **Outside the range** of versions of its package's allow
 * (R-CUR-026), **Not allowed** in mode `onlyAllowed` without a rule, and **Delivered** by the mode `allExceptBlocked` without a rule.
 */
export function stateBadge(delivery: DeliveryDecision, requested: boolean): StateBadge {
  if (!delivery.delivered) {
    if (delivery.reason === 'blocked') return { text: $localize`:@@upstream.state.blocked:Blocked`, look: 'warn', icon: 'block' };
    if (requested) return { text: $localize`:@@upstream.state.requested:Requested`, look: 'badge', icon: 'pending' };
    if (delivery.reason === 'outsideRange') {
      return { text: $localize`:@@upstream.state.outside-range:Outside the range`, look: 'badge', icon: 'remove_circle_outline' };
    }
    return { text: $localize`:@@upstream.state.not-allowed:Not allowed`, look: 'badge', icon: 'remove_circle_outline' };
  }
  if (delivery.decidedBy.effect === 'allow') return { text: $localize`:@@upstream.state.allowed:Allowed`, look: 'badge', icon: 'check' };
  return { text: $localize`:@@upstream.state.delivered:Delivered`, look: 'badge', icon: 'check' };
}

/**
 * The name of a package's or a version's switch for assistive technology (R-CUR-024): what it delivers, "Deliver lodash" or "Deliver
 * lodash 4.17.21"; its state is the switch's own "on" or "off".
 */
export function switchLabel(name: string, version?: string): string {
  return version
    ? $localize`:@@upstream.switch.version-label:Deliver ${name}:name: ${version}:version:`
    : $localize`:@@upstream.switch.package-label:Deliver ${name}:name: with every version`;
}

/** The range of versions of a package's allow (R-CUR-026), as the API answers it. */
export type DeliveryRange = NonNullable<DeliveryDecision['decidedBy']['range']>;

/**
 * The bounds of a range in words, after "Versions" or a rule (R-CUR-026), with the download dialog's words (Q-CUR-228): "from 2.0.0 up
 * to 3.0.0", "from 2.0.0 below 3.0.0", "from 2.0.0", "up to 3.0.0", "below 3.0.0". The versions are not translated.
 */
export function rangeBoundsText(r: DeliveryRange): string {
  const from = r.fromVersion;
  const to = r.toVersion;
  if (from && to) {
    return r.toInclusive
      ? $localize`:@@upstream.range.from-up-to:from ${from}:from: up to ${to}:to:`
      : $localize`:@@upstream.range.from-below:from ${from}:from: below ${to}:to:`;
  }
  if (from) return $localize`:@@upstream.range.from:from ${from}:from:`;
  if (to) {
    return r.toInclusive ? $localize`:@@upstream.range.up-to:up to ${to}:to:` : $localize`:@@upstream.range.below:below ${to}:to:`;
  }
  return '';
}

/**
 * The line under a package's switch (R-CUR-026): "All versions" where its allow has no range, "Versions from 2.0.0 below 3.0.0" where it
 * has one.
 */
export function rangeLineText(r: DeliveryRange | null | undefined): string {
  return r && (r.fromVersion || r.toVersion)
    ? $localize`:@@upstream.range.line:Versions ${rangeBoundsText(r)}:bounds:`
    : $localize`:@@upstream.range.all:All versions`;
}

/** The rule that made a decision, in one muted line ("Rule: @acme/*"); nothing where the mode decided. */
export function decidedByText(delivery: DeliveryDecision): string | null {
  const by = delivery.decidedBy;
  if (!by.kind || !by.subject) return null;
  return $localize`:@@upstream.state.rule:Rule: ${by.subject}:subject:`;
}

/** The code of the hint under the search field (R-CUR-007, Q-CUR-166). */
export type SearchHint = NonNullable<UpstreamCatalogue['searchHint']>;

/**
 * The sentence under the search field, by the code the server sends (R-CUR-007, R-CUR-018, Q-CUR-166): what a person can type and
 * find, or why the upstream has no search and what to enter instead. The upstream's own name (Docker Hub, ghcr.io, GitLab) is not
 * translated. Null where the server names no hint.
 */
export function searchHintText(hint: SearchHint | null | undefined): string | null {
  switch (hint) {
    case 'dockerHub':
      return $localize`:@@upstream.hint.docker-hub:Search for an image name, for example nginx; official images are under library/<name>.`;
    case 'dockerHubUnavailable':
      return $localize`:@@upstream.hint.docker-hub-unavailable:Docker Hub's search is not available at the moment; enter an image's full name, for example library/nginx, or open a namespace.`;
    case 'quay':
      return $localize`:@@upstream.hint.quay:Search for an image name or a word of the description, for example prometheus.`;
    case 'ghcr':
      return $localize`:@@upstream.hint.ghcr:ghcr.io offers no search; enter an image's full name, for example stefanprodan/podinfo.`;
    case 'gitlab':
      return $localize`:@@upstream.hint.gitlab:GitLab's registry offers no search; open a project to see its images, or enter an image's full name.`;
    case 'ociRegistry':
      return $localize`:@@upstream.hint.oci-registry:This registry offers no search; enter an image's full name.`;
    case 'npmRegistry':
      return $localize`:@@upstream.hint.npm-registry:Search for a package name, a keyword or a maintainer, for example express; a search takes 2 to 64 characters.`;
    case 'npmNoSearch':
      return $localize`:@@upstream.hint.npm-no-search:This registry offers no search; enter a package's exact name, for example express.`;
    case 'nugetSearch':
      return $localize`:@@upstream.hint.nuget-search:Search for a package ID or a word of the description, for example Serilog; id:, owner: and tags: narrow the search.`;
    case 'nugetNoSearch':
      return $localize`:@@upstream.hint.nuget-no-search:This NuGet upstream offers no search; enter a package's exact ID, for example Serilog.`;
    case 'galaxy':
      return $localize`:@@upstream.hint.galaxy:Search for a namespace, a collection name, a tag or a word of the description, for example docker.`;
    case 'galaxyNoSearch':
      return $localize`:@@upstream.hint.galaxy-no-search:This Galaxy offers no search; open a namespace or enter a collection's exact name, for example community.general.`;
    case 'aptIndex':
      return $localize`:@@upstream.hint.apt-index:Search for words of a package's name or short description, for example nginx; Quader searches only the suites whose index it has read.`;
    case 'noSearch':
      return $localize`:@@upstream.hint.no-search:This upstream offers no search; enter a package's exact name.`;
    case null:
    case undefined:
      return null;
  }
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
 * The sentence of a verdict that cannot exist for a format (R-CUR-014): why Debian and Ansible Galaxy are not checked for vulnerabilities,
 * in the words of every other screen (Q-UI-1244); an OCI image is analysed only once its platforms are stored.
 */
export function notCoveredText(format: string): string {
  switch (format) {
    // The format's reason, the sentence of the version's page and the repository's content (Q-UI-1244, R-UI-221).
    case 'deb':
    case 'ansible':
      return notScannedText(format);
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
