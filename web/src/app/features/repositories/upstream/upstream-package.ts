import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { UpstreamPopularity } from './upstream-popularity';
import { RouterLink } from '@angular/router';
import { ApiError } from '../../../api/problem';
import {
  UpstreamCatalogueApi,
  UpstreamPackage,
  UpstreamVerdict,
  UpstreamVersion,
  VERDICTS_PER_CALL,
} from '../../../api/upstream-catalogue-api';
import { ErrorNotice } from '../../../shared/error-notice';
import { asApiError } from '../../../shared/paged-resource';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { DeprecationMark } from '../../../shared/ui/deprecation-mark';
import { Switch } from '../../../shared/ui/switch';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { DistTags, tagsByVersion } from '../../packages/dist-tags';
import { formatSize } from '../../packages/format-size';
import { versionCommands } from '../../packages/package-links';
import { downloadStateText } from '../downloads/download-words';
import { UpstreamActions } from './upstream-actions';
import { UpstreamDocs } from './upstream-docs';
import { LIVE_FAST_MS, LIVE_IDLE_MS, live } from '../../../core/live-refresh';
import {
  catalogueState,
  decidedByText,
  flagText,
  notCoveredText,
  rangeLineText,
  stateBadge,
  switchLabel,
  verdictText,
} from './upstream-words';
import { Loading } from '../../../shared/ui/loading';

/** The one act of a version's cache cell (R-CUR-024): it changes what the cell shows. */
export type CacheAct = 'download' | 'examine' | 'discard';

/** How long the version filter waits after the last key before it asks the upstream. */
export const FILTER_DEBOUNCE_MS = 300;

/** A count in the viewer's language, with grouping: "3,893". */
export function groupedCount(n: number): string {
  return new Intl.NumberFormat(($localize as { locale?: string }).locale || 'en-US').format(n);
}

/**
 * A package of the upstream opened in place (remote-curation.md R-CUR-018, R-CUR-024): its description and its state in this repository
 * where no row above shows them (Q-UI-1192); **Download every version…** with its preview at the left and the package's switch at the
 * right for those who decide (**Request every version** for a reader); and its versions newest first, 100 at a time with **Load more**
 * and a filter: per version its published time, its size or that the upstream does not tell it, its flags and the npm tags that name it
 * (Q-UI-1190; the filter finds a version by a tag's name, Q-CUR-182), its switch beside its delivery's badge with the rule that decided
 * under it (a reader's **Request** there), whether the cache holds it or downloads it with the cache's one act on that line, and its
 * vulnerability verdict where it is not stored (a stored version's verdicts are its version page's, Q-CUR-61). No row has a menu or an
 * action cell (Q-CUR-201).
 */
@Component({
  selector: 'quader-upstream-package',
  imports: [Loading, DataTable, DeprecationMark, DistTags, ErrorNotice, RouterLink, Switch, TimeAgo, UpstreamDocs, UpstreamPopularity],
  templateUrl: './upstream-package.html',
  styleUrl: './upstream-package.scss',
})
export class UpstreamPackageView {
  private readonly api = inject(UpstreamCatalogueApi);
  protected readonly actions = inject(UpstreamActions);

  readonly name = input.required<string>();
  readonly project = input.required<string>();
  /** The upstream tells the size of a version: `known`, `perVersion`, `unknown` (R-CUR-007). */
  readonly sizes = input<string>('known');
  /** The words of the list's columns: Version or Tag. */
  readonly versionLabel = input.required<string>();
  /**
   * Opened in a row of the list, whose columns show the package's description, its newest version and its state already: the head
   * says only what the row does not (Q-UI-1192). A lookup by name has no row and says all of it.
   */
  readonly inRow = input(false);

  protected readonly pkg = signal<UpstreamPackage | null>(null);
  protected readonly versions = signal<readonly UpstreamVersion[]>([]);
  protected readonly next = signal<string | null>(null);
  protected readonly loading = signal(false);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly filter = signal('');
  /** The name and the filter the versions shown answer. */
  private shownQuestion = '';
  private readonly appliedFilter = signal('');
  private generation = 0;
  private filterTimer: ReturnType<typeof setTimeout> | null = null;

  /** The vulnerability verdicts of the versions that are not stored, by version (R-CUR-009). */
  protected readonly verdicts = signal<ReadonlyMap<string, UpstreamVerdict>>(new Map());
  /** The repository's format is not analysed for vulnerabilities: one sentence instead of a verdict per version (R-CUR-014). */
  protected readonly notCovered = signal(false);

  protected readonly badge = stateBadge;
  protected readonly ruleLine = decidedByText;
  protected readonly rangeText = rangeLineText;
  /**
   * The rule that decided, the line under the delivery: the version's own and the package's in words (the row names them already),
   * a sub-section's by its pattern after "Rule:"; null where the mode decided.
   */
  protected ruleOf(v: UpstreamVersion): { readonly words: string; readonly pattern: boolean } | null {
    const by = v.delivery.decidedBy;
    switch (by.kind) {
      case 'version':
        return { words: $localize`:@@upstream.rule.own-line:Rule: this version`, pattern: false };
      case 'package':
        // A version outside the range of the package's allow names the range, which the line under the package's switch says in
        // full (R-CUR-026); a version inside it, as every version of a package without a range, the package.
        return by.range && !v.delivery.delivered
          ? { words: $localize`:@@upstream.rule.package-range-line:Rule: the package's range`, pattern: false }
          : { words: $localize`:@@upstream.rule.package-line:Rule: the package`, pattern: false };
      case 'section':
        return by.subject ? { words: by.subject, pattern: true } : null;
      default:
        return null;
    }
  }
  protected readonly flag = flagText;
  protected readonly size = formatSize;
  protected readonly count = groupedCount;
  protected readonly queuedText = downloadStateText;
  protected readonly notCoveredSentence = computed(() => notCoveredText(this.actions.format()));
  protected readonly stateSentence = computed(() => {
    const f = this.failure();
    return f ? catalogueState(f, 'package') : null;
  });

  protected readonly noVersions = $localize`:@@upstream.versions.empty:The upstream lists no version of this package.`;
  protected readonly noVersionMatches = $localize`:@@upstream.versions.empty-filtered:No version matches.`;

  // Three columns (Q-CUR-157 as revised by Q-CUR-201): the published time and the size stand under the version, the switch beside the
  // delivery's badge and the rule that decided under it, the cache's one act on its state's line and the verdicts under it, so that the
  // list keeps the form of a table at 1,024 px beside the side bar. A format without vulnerability verdicts says so once, above the
  // list (R-CUR-014); a stored version still leads to its malware and signature verdicts.
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'version', label: this.versionLabel() },
    { id: 'delivery', label: $localize`:@@upstream.col.delivery:Delivery` },
    { id: 'cache', label: $localize`:@@upstream.col.cache:Cache` },
  ]);

  /** A download of a version shown waits or runs: the versions are read every few seconds until it has ended (R-UI-213). */
  protected readonly downloading = computed(() => this.versions().some((v) => v.queued !== null));

  /** The name and the filter the versions shown were read for: a write reads them again in place, a new query from the first page. */
  private shownFor = '';

  constructor() {
    effect(() => {
      const name = this.name();
      const filter = this.appliedFilter();
      this.actions.changed();
      untracked(() => {
        const key = JSON.stringify([name, filter]);
        if (key === this.shownFor && this.pkg() !== null && !this.loading()) {
          // A write (a switch, a download, a request): the pages shown are read again, so Load more and the place stay (R-UI-213).
          this.refresh().catch((e: unknown) => this.failure.set(asApiError(e)));
          return;
        }
        this.shownFor = key;
        void this.load(name, filter, true);
      });
    });
    live({ reload: () => this.refresh(), every: () => (this.downloading() || this.actions.unsettled() > 0 ? LIVE_FAST_MS : LIVE_IDLE_MS) });
  }

  /**
   * The versions shown again, in place (R-UI-213): the pages from the first until as many versions as are shown, then the rows at
   * once, so the open menu, the focus and the filter stay. A failure keeps the rows and is thrown (the back-off of `live`).
   */
  private async refresh(): Promise<void> {
    if (this.loading() || this.pkg() === null) return;
    const generation = this.generation;
    const name = this.name();
    const filter = this.appliedFilter();
    const shown = this.versions().length;
    const versions: UpstreamVersion[] = [];
    let cursor: string | null = null;
    let page: UpstreamPackage;
    do {
      page = await this.api.package(this.actions.repository(), name, cursor, filter);
      if (generation !== this.generation || this.loading()) return;
      versions.push(...page.versions.filter((v) => !versions.some((x) => x.version === v.version)));
      cursor = page.done ? null : page.nextCursor;
    } while (cursor !== null && versions.length < shown);
    this.pkg.set(this.settle(page));
    this.versions.set(versions.map((v) => this.settleVersion(v)));
    this.next.set(cursor);
    void this.readVerdicts(name, versions.filter((v) => !this.verdicts().has(v.version)), generation);
  }

  private async load(name: string, filter: string, fresh: boolean): Promise<void> {
    const generation = fresh ? ++this.generation : this.generation;
    // Another version filter is a new question: its load shows, not the versions of the filter before (R-UI-217, Q-UI-1234). A read
    // after a write asks the same question and keeps the rows while it reads.
    const question = `${name} ${filter}`;
    if (fresh && question !== this.shownQuestion) this.versions.set([]);
    this.shownQuestion = question;
    this.loading.set(true);
    try {
      const page = this.settle(await this.api.package(this.actions.repository(), name, fresh ? null : this.next(), filter));
      if (generation !== this.generation) return;
      this.pkg.set(page);
      this.versions.update((vs) => (fresh ? page.versions : [...vs, ...page.versions.filter((v) => !vs.some((x) => x.version === v.version))]));
      this.next.set(page.done ? null : page.nextCursor);
      this.failure.set(null);
      void this.readVerdicts(name, page.versions, generation);
    } catch (e) {
      if (generation === this.generation) this.failure.set(asApiError(e));
    } finally {
      if (generation === this.generation) this.loading.set(false);
    }
  }

  /** A page as the rows show it: a switch's answer stands until the catalogue's read agrees with it (UpstreamActions.settled). */
  private settle(page: UpstreamPackage): UpstreamPackage {
    return {
      ...page,
      ...(page.level ? { level: this.actions.settled(this.name(), page.level) } : {}),
      versions: page.versions.map((v) => this.settleVersion(v)),
    };
  }

  private settleVersion(v: UpstreamVersion): UpstreamVersion {
    return { ...v, delivery: this.actions.settled(`${this.name()} ${v.version}`, v.delivery) };
  }

  /** The verdicts of a page's versions that are not stored, 50 at a time; a failure leaves them unsaid. */
  private async readVerdicts(name: string, page: readonly UpstreamVersion[], generation: number): Promise<void> {
    const missing = page.filter((v) => v.stored === 'none').map((v) => v.version);
    for (let i = 0; i < missing.length; i += VERDICTS_PER_CALL) {
      try {
        const answer = await this.api.vulnerabilities(this.actions.repository(), name, missing.slice(i, i + VERDICTS_PER_CALL));
        if (generation !== this.generation) return;
        if (answer.notCovered) {
          this.notCovered.set(true);
          return;
        }
        this.verdicts.update((m) => {
          const next = new Map(m);
          for (const v of answer.versions) next.set(v.version, v);
          return next;
        });
      } catch {
        return;
      }
    }
  }

  protected retry(): void {
    void this.load(this.name(), this.appliedFilter(), true);
  }

  protected more(): void {
    if (this.next() !== null && !this.loading()) void this.load(this.name(), this.appliedFilter(), false);
  }

  protected setFilter(value: string): void {
    this.filter.set(value);
    if (this.filterTimer !== null) clearTimeout(this.filterTimer);
    this.filterTimer = setTimeout(() => this.appliedFilter.set(value.trim()), FILTER_DEBOUNCE_MS);
  }

  /** "Showing 100 of 3,893", where the upstream tells the total and more are left. */
  protected readonly showing = computed(() => {
    const p = this.pkg();
    const n = this.versions().length;
    if (!p || p.total === null || p.total <= n) return null;
    return $localize`:@@upstream.versions.showing:Showing ${groupedCount(n)}:shown: of ${groupedCount(p.total)}:total:`;
  });

  protected versionLink(version: string): string[] {
    return versionCommands(this.project(), this.actions.format(), this.name(), version);
  }

  protected verdictOf(v: UpstreamVersion): string | null {
    const verdict = this.verdicts().get(v.version);
    return verdict ? verdictText(verdict) : null;
  }

  protected blockedVerdict(v: UpstreamVersion): boolean {
    return this.verdicts().get(v.version)?.verdict === 'blocked';
  }

  /**
   * The cache's one act of a version (R-CUR-024, Q-CUR-201), by the state it changes: **Download** where it is not stored and delivered
   * (a holder of `manage`, or a reader who may fill the repository), **Download for examination** where it is not stored and not
   * delivered, **Discard** where it is stored for examination (both `manage`); none while a download waits or runs. The server decides
   * each call again.
   */
  cacheAct(v: UpstreamVersion): CacheAct | null {
    if (!this.actions.known() || v.queued) return null;
    const manager = this.actions.manager();
    if (v.stored === 'none') {
      if (v.delivery.delivered) return manager || this.actions.mayFill() ? 'download' : null;
      return manager ? 'examine' : null;
    }
    return manager && !v.delivery.delivered ? 'discard' : null;
  }

  protected cacheActText(act: CacheAct): string {
    switch (act) {
      case 'download':
        return $localize`:@@upstream.act.download:Download`;
      case 'examine':
        return $localize`:@@upstream.act.examine:Download for examination`;
      case 'discard':
        return $localize`:@@upstream.act.discard:Discard`;
    }
  }

  protected runCache(act: CacheAct, v: UpstreamVersion): void {
    const s = { name: this.name(), version: v.version };
    if (act === 'discard') void this.actions.discard(s);
    else void this.actions.downloadVersion(s, act === 'download');
  }

  /** A reader may ask for a version that is not delivered, that no block holds and whose request is not open (R-CUR-015). */
  mayRequest(v: UpstreamVersion): boolean {
    return this.actions.known() && !this.actions.manager() && !v.delivery.delivered && !v.request && v.delivery.reason !== 'blocked';
  }

  protected request(v: UpstreamVersion): void {
    void this.actions.ask({ name: this.name(), version: v.version });
  }

  /** The switch's name for assistive technology: what it delivers ("Deliver lodash 4.17.21"). */
  protected switchLabel(v: UpstreamVersion): string {
    return switchLabel(this.name(), v.version);
  }

  protected busyVersion(v: UpstreamVersion): boolean {
    return this.actions.isBusy({ name: this.name(), version: v.version });
  }

  /**
   * A version's switch turned (R-CUR-024): the row takes the server's decision at once and the versions are read again in place; a
   * refused write leaves the row, and so the switch, as it was.
   */
  protected async switchVersion(v: UpstreamVersion, on: boolean): Promise<void> {
    const out = await this.actions.switchVersion({ name: this.name(), version: v.version }, on);
    if (out === null) return;
    this.versions.update((vs) => vs.map((x) => (x.version === v.version ? { ...x, delivery: out.decision } : x)));
  }

  // ---- The package's acts ----

  /** The package level's decision, which the package's switch shows (R-CUR-024); the package's for a reader, who has no switch. */
  protected readonly level = computed(() => {
    const p = this.pkg();
    return p ? (p.level ?? p.delivery) : null;
  });

  /** The rule under the package's switch: the package's own, or a sub-section's pattern; none where the mode decides. */
  protected readonly levelRule = computed(() => {
    const level = this.level();
    const by = level?.decidedBy;
    if (by?.kind === 'package') return $localize`:@@upstream.rule.this-package:Rule: this package`;
    if (level && by?.kind === 'section' && by.subject) return decidedByText(level);
    return null;
  });

  protected readonly packageSwitchLabel = computed(() => switchLabel(this.name()));

  /** The package's switch is shown to whoever may set rules, once the catalogue answered the package level. */
  protected readonly hasPackageSwitch = computed(() => this.actions.manager() && !!this.pkg()?.level);

  protected downloadEvery(): void {
    const name = this.name();
    void this.actions.downloadBulk({ level: 'package', name }, $localize`:@@upstream.preview.title-package:Every version of ${name}:name:`);
  }

  /** The package's switch turned (R-CUR-024): its head takes the server's decision at once; the versions are read again. */
  protected async switchPackage(on: boolean): Promise<void> {
    const p = this.pkg();
    if (!p) return;
    const out = await this.actions.switchPackage(this.name(), on, p.versionRules);
    if (out === null) return;
    this.pkg.update((x) => (x ? { ...x, level: out.decision } : x));
  }

  /**
   * The line under the package's switch (R-CUR-026): "All versions", or the range of the package's allow in words, where the package is
   * delivered; nothing where it is not. A reader without a switch reads it from the package's decision.
   */
  protected readonly rangeLine = computed(() => {
    const d = this.level();
    if (!d?.delivered) return null;
    return rangeLineText(this.levelRange());
  });

  /** The range of the package's allow, null where it has none. */
  protected readonly levelRange = computed(() => {
    const by = this.level()?.decidedBy;
    return by?.kind === 'package' && by.effect === 'allow' ? (by.range ?? null) : null;
  });

  /** **Limit versions…** for those who decide, beside the range's line or in its place where the package is not delivered. */
  protected readonly mayLimit = computed(() => this.hasPackageSwitch() && this.actions.known());

  /**
   * An image with a range whose latest is not delivered (Q-CUR-223): `docker pull` without a tag asks for latest and is refused; the head
   * says so to those who can switch latest on, until a row read shows it delivered.
   */
  protected readonly latestOutside = computed(
    () =>
      this.actions.format() === 'oci' &&
      this.hasPackageSwitch() &&
      !!this.levelRange() &&
      !!this.level()?.delivered &&
      !this.versions().some((v) => v.version === 'latest' && v.delivery.delivered),
  );

  /** **Limit versions…** (R-CUR-026): the dialog's answer stands in the head at once, the versions are read again (R-UI-213). */
  protected async limitVersions(): Promise<void> {
    const l = this.level();
    if (!l) return;
    const out = await this.actions.limitVersions({
      name: this.name(),
      range: this.levelRange(),
      delivered: l.delivered,
      versions: this.versions().map((v) => v.version),
    });
    if (out === null) return;
    this.pkg.update((x) => (x ? { ...x, level: out.decision } : x));
  }

  protected requestPackage(): void {
    void this.actions.ask({ name: this.name() });
  }

  /** A reader may ask for the package while it is not delivered, no block holds it and nobody's request for it is open. */
  protected readonly mayRequestPackage = computed(() => {
    const p = this.pkg();
    return !!p && this.actions.known() && !this.actions.manager() && !p.delivery.delivered && !p.request && p.delivery.reason !== 'blocked';
  });

  /** The npm tags of the package by the version they name, `latest` first (Q-UI-1190); empty for every other format. */
  protected readonly tags = computed(() => tagsByVersion(this.pkg()?.tags));
}
