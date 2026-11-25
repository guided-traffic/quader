import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
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
import { TimeAgo } from '../../../shared/ui/time-ago';
import { DistTags, tagsByVersion } from '../../packages/dist-tags';
import { formatSize } from '../../packages/format-size';
import { versionCommands } from '../../packages/package-links';
import { downloadStateText } from '../downloads/download-words';
import { UpstreamActions } from './upstream-actions';
import { LIVE_FAST_MS, LIVE_IDLE_MS, live } from '../../../core/live-refresh';
import { catalogueState, decidedByText, flagText, notCoveredText, stateBadge, verdictText } from './upstream-words';

/** What a version row offers (R-CUR-018): one is the row's word, several are its menu (R-UI-163). */
export type VersionAct = 'download' | 'examine' | 'allow' | 'block' | 'remove-rule' | 'discard' | 'request';

/** How long the version filter waits after the last key before it asks the upstream. */
export const FILTER_DEBOUNCE_MS = 300;

/** A count in the viewer's language, with grouping: "3,893". */
export function groupedCount(n: number): string {
  return new Intl.NumberFormat(($localize as { locale?: string }).locale || 'en-US').format(n);
}

/**
 * A package of the upstream opened in place (remote-curation.md R-CUR-018): its description and its state in this repository where no
 * row above shows them (Q-UI-1192), with the package's acts (**Download every version…** with its preview, **Allow every version**, **Block every version**, **Remove
 * rule**; **Request every version** for a reader), and its versions newest first, 100 at a time with **Load more** and a filter:
 * per version its published time, its size or that the upstream does not tell it, its flags and the npm tags that name it (Q-UI-1190; the
 * filter finds a version by a tag's name, Q-CUR-182), its delivery state and the rule that
 * decided it, whether the cache holds it or downloads it, its vulnerability verdict where it is not stored (a stored version's
 * verdicts are its version page's, Q-CUR-61), and its acts.
 */
@Component({
  selector: 'quader-upstream-package',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, DataTable, DistTags, ErrorNotice, RouterLink, TimeAgo],
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
  private readonly appliedFilter = signal('');
  private generation = 0;
  private filterTimer: ReturnType<typeof setTimeout> | null = null;

  /** The vulnerability verdicts of the versions that are not stored, by version (R-CUR-009). */
  protected readonly verdicts = signal<ReadonlyMap<string, UpstreamVerdict>>(new Map());
  /** The repository's format is not analysed for vulnerabilities: one sentence instead of a verdict per version (R-CUR-014). */
  protected readonly notCovered = signal(false);

  protected readonly badge = stateBadge;
  protected readonly ruleLine = decidedByText;
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
        return { words: $localize`:@@upstream.rule.package-line:Rule: the package`, pattern: false };
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

  protected readonly hasActions = computed(() => this.versions().some((v) => this.acts(v).length > 0));
  // Three columns and the acts (Q-CUR-157): the published time and the size stand under the version, the rule that decided under the
  // delivery, the verdicts under the cache, so that the list keeps the form of a table at 1,024 px beside the side bar; with seven
  // columns it stood as blocks of five lines per version there. A format without vulnerability verdicts says so once, above the
  // list (R-CUR-014); a stored version still leads to its malware and signature verdicts.
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'version', label: this.versionLabel() },
    { id: 'delivery', label: $localize`:@@upstream.col.delivery:Delivery` },
    { id: 'cache', label: $localize`:@@upstream.col.cache:Cache` },
    ...(this.hasActions() ? [{ id: 'actions', label: $localize`:@@upstream.col.actions:Actions`, hideLabel: true }] : []),
  ]);

  /** A download of a version shown waits or runs: the versions are read every few seconds until it has ended (R-UI-213). */
  protected readonly downloading = computed(() => this.versions().some((v) => v.queued !== null));

  constructor() {
    effect(() => {
      const name = this.name();
      const filter = this.appliedFilter();
      this.actions.changed();
      untracked(() => void this.load(name, filter, true));
    });
    live({ reload: () => this.refresh(), every: () => (this.downloading() ? LIVE_FAST_MS : LIVE_IDLE_MS) });
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
    this.pkg.set(page);
    this.versions.set(versions);
    this.next.set(cursor);
    void this.readVerdicts(name, versions.filter((v) => !this.verdicts().has(v.version)), generation);
  }

  private async load(name: string, filter: string, fresh: boolean): Promise<void> {
    const generation = fresh ? ++this.generation : this.generation;
    this.loading.set(true);
    try {
      const page = await this.api.package(this.actions.repository(), name, fresh ? null : this.next(), filter);
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

  /** The acts of a version for this caller (R-CUR-005, R-CUR-018); the server decides each call again. */
  acts(v: UpstreamVersion): readonly VersionAct[] {
    if (!this.actions.known()) return [];
    const out: VersionAct[] = [];
    const delivered = v.delivery.delivered;
    const manager = this.actions.manager();
    if (v.stored === 'none' && !v.queued) {
      if (delivered && (manager || this.actions.mayFill())) out.push('download');
      else if (!delivered && manager) out.push('examine');
    }
    if (manager) {
      // Allow and Block on every version: an explicit rule of the version is narrower than the package's and the sections' and
      // outlasts a change of the mode (R-CUR-001), so both mean something whatever the version's state; the effect its own rule
      // has already is Remove rule instead. So every row of a holder of manage has two acts or more, one menu in every row.
      const own = v.delivery.decidedBy.kind === 'version' ? v.delivery.decidedBy.effect : null;
      out.push(own === 'allow' ? 'remove-rule' : 'allow');
      out.push(own === 'block' ? 'remove-rule' : 'block');
      if (v.stored !== 'none' && !delivered) out.push('discard');
    } else if (!delivered && !v.request && v.delivery.reason !== 'blocked') {
      out.push('request');
    }
    return out;
  }

  protected actText(act: VersionAct): string {
    switch (act) {
      case 'download':
        return $localize`:@@upstream.act.download:Download`;
      case 'examine':
        return $localize`:@@upstream.act.examine:Download for examination`;
      case 'allow':
        return $localize`:@@upstream.act.allow:Allow`;
      case 'block':
        return $localize`:@@upstream.act.block:Block`;
      case 'remove-rule':
        return $localize`:@@upstream.act.remove-rule:Remove rule`;
      case 'discard':
        return $localize`:@@upstream.act.discard:Discard`;
      case 'request':
        return $localize`:@@upstream.act.request:Request`;
    }
  }

  protected menuLabel(v: UpstreamVersion): string {
    return $localize`:@@upstream.version.menu:Actions for ${this.name()}:name: ${v.version}:version:`;
  }

  protected run(act: VersionAct, v: UpstreamVersion): void {
    const s = { name: this.name(), version: v.version };
    const label = `${s.name} ${s.version}`;
    switch (act) {
      case 'download':
      case 'examine':
        void this.actions.downloadVersion(s, act === 'download');
        return;
      case 'allow':
      case 'block':
        void this.actions.setRule({ kind: 'version', name: s.name, version: s.version, effect: act }, label);
        return;
      case 'remove-rule':
        void this.actions.removeRule('version', v.delivery.decidedBy.subject ?? `${s.name}@${s.version}`);
        return;
      case 'discard':
        void this.actions.discard(s);
        return;
      case 'request':
        void this.actions.ask(s);
        return;
    }
  }

  // ---- The package's acts ----

  /** The effect of the package's own rule, where it has one. */
  protected readonly packageRule = computed(() => {
    const d = this.pkg()?.delivery.decidedBy;
    return d?.kind === 'package' ? (d.effect ?? null) : null;
  });

  protected downloadEvery(): void {
    const name = this.name();
    void this.actions.downloadBulk({ level: 'package', name }, $localize`:@@upstream.preview.title-package:Every version of ${name}:name:`);
  }

  protected setPackageRule(effect: 'allow' | 'block'): void {
    void this.actions.setRule({ kind: 'package', name: this.name(), effect }, this.name());
  }

  protected removePackageRule(): void {
    void this.actions.removeRule('package', this.name());
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
