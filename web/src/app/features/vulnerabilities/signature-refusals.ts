import { DecimalPipe } from '@angular/common';
import { Component, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { ApiError } from '../../api/problem';
import { splitRef } from '../../api/repositories-api';
import {
  SIGNATURE_GATE_REASONS,
  SignatureGateReason,
  SignatureRefusal,
  SignatureRefusalFilters,
  SignatureRelease,
  SignaturesApi,
} from '../../api/signatures-api';
import { ErrorNotice } from '../../shared/error-notice';
import { principalName } from '../../shared/malware/malware-words';
import { PagedResource, asApiError, valueOf } from '../../shared/paged-resource';
import { GateActions } from '../../shared/signatures/gate-releases';
import { gateReasonWord } from '../../shared/signatures/gate-words';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { Disclosure } from '../../shared/ui/disclosure';
import { FilterCategory, FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { TimeAgo } from '../../shared/ui/time-ago';

/** One chip of a filter that is set, with the query change that removes it. */
interface Chip extends ListChip {
  readonly remove: Readonly<Record<string, string | null>>;
}

/** The artifact of a refused download as a reader names it: the package and version, else the file. */
export function refusalArtifact(r: Pick<SignatureRefusal, 'package' | 'version' | 'path'>): string {
  if (r.package) return r.version ? `${r.package} ${r.version}` : r.package;
  return r.path;
}

/**
 * The refused downloads of the gates in the security dashboard's view **Signatures** (R-SIG-074, R-SIG-073): one row per repository and
 * artifact, the newest refusal first, with the repository, the artifact, the reason, the last refusal and how often; filters repository
 * and reason in the address (the repository is the view's, which the findings below share). **Release for this repository** opens the
 * dialog that names the one repository and its reason, for the holder of R-SIG-070's rights; a released row says by whom and when and
 * offers **Withdraw release**, which asks first. "No refused downloads." when empty. The closed section **Releases** lists the releases of
 * one repository, the view's by default, since the API answers them per repository (Q-SIG-227).
 */
@Component({
  selector: 'quader-signature-refusals',
  imports: [DataTable, DecimalPipe, Disclosure, ErrorNotice, FilterCategory, FilterPanel, ListHead, RouterLink, TimeAgo],
  template: `
    <h2 class="vuln-list-title" i18n="@@sig.refusals.title">Refused downloads</h2>
    <p class="hint list-note" i18n="@@sig.refusals.note">
      Downloads that a repository refused because the artifact has no signature it accepts. One row per repository and artifact.
    </p>
    @if (actionError(); as e) {
      <quader-error-notice [error]="e" />
    }
    <quader-list-head [chips]="chips()" (removeChip)="removeChip($event)" [(filtersOpen)]="panelOpen" [controls]="panel.panelId" />
    <quader-filter-panel #panel [(open)]="panelOpen" [activeCount]="chips().length" (apply)="submitFilters()" (clearAll)="clearFilters()" (dismissed)="restage()">
      <quader-filter-category i18n-label="@@malware.filter.repository" label="Repository">
        <select class="select-input" aria-label="Repository" i18n-aria-label="@@malware.filter.repository" (change)="stage('repository', $any($event.target).value)">
          <option value="" [selected]="!staged().repository" i18n="@@malware.filter.any-repository">Any repository</option>
          @for (r of repositories(); track r) {
            <option [value]="r" [selected]="staged().repository === r" translate="no">{{ r }}</option>
          }
        </select>
      </quader-filter-category>
      <quader-filter-category i18n-label="@@sig.refusals.filter.reason" label="Reason">
        <select class="select-input" aria-label="Reason" i18n-aria-label="@@sig.refusals.filter.reason" (change)="stage('reason', $any($event.target).value)">
          <option value="" [selected]="!staged().reason" i18n="@@sig.findings.filter.any">Any</option>
          @for (k of reasons; track k) {
            <option [value]="k" [selected]="staged().reason === k">{{ reasonWord(k) }}</option>
          }
        </select>
      </quader-filter-category>
    </quader-filter-panel>

    <quader-data-table
      [caption]="caption"
      [columns]="columns()"
      [loaded]="list.loaded()"
      [loading]="list.loading()"
      [error]="list.error()"
      [empty]="list.items().length === 0"
      [emptyText]="filtered() ? emptyFiltered : emptyText"
      [hasMore]="list.hasMore()"
      (more)="list.more()"
      (retry)="list.more()"
      data-testid="signature-refusals"
    >
      @for (r of list.items(); track r.repository + ' ' + r.sha256) {
        <tr>
          <th scope="row" class="wrap-name">
            <span class="icon-name-text">
              <span class="refusal-name" translate="no">{{ artifact(r) }}</span>
              @if (r.package) {
                <span class="secondary refusal-path" translate="no">{{ r.path }}</span>
              }
            </span>
          </th>
          <td class="refusal-repo"><a [routerLink]="repositoryLink(r.repository)" translate="no">{{ r.repository }}</a></td>
          <td class="refusal-reason">
            <span class="fstatus bad"
              ><span class="fstatus-main"><span class="material-icons" aria-hidden="true">gpp_bad</span><span>{{ reasonWord(r.reason) }}</span></span></span
            >
            @if (r.release; as rel) {
              <span class="release-line"
                ><span class="fstatus"
                  ><span class="fstatus-main"
                    ><span class="material-icons" aria-hidden="true">lock_open</span><span i18n="@@sig.refusals.released">Released</span></span
                  ></span
                >&ngsp;<span class="secondary"
                  ><ng-container i18n="@@malware.released-by">by {{ principal(rel.releasedBy) }}</ng-container>&ngsp;<quader-time-ago
                    [at]="rel.releasedAt"
                    absolute="title"
                /></span>
              </span>
            }
          </td>
          <td class="nowrap"><quader-time-ago [at]="r.lastAt" absolute="title" /></td>
          <td class="num">{{ r.count | number }}</td>
          @if (canRelease()) {
            <td class="actions">
              <button class="btn btn-tertiary" type="button" [disabled]="busy()" (click)="act(r)">
                {{ r.release ? withdrawLabel : releaseLabel }}<span class="visually-hidden" translate="no">&ngsp;{{ artifact(r) }}, {{ r.repository }}</span>
              </button>
            </td>
          }
        </tr>
      }
    </quader-data-table>

    <quader-disclosure class="releases" i18n-heading="@@sig.releases.heading" heading="Releases" [hint]="releasesHint" (expandedChange)="releasesOpen.set(releasesOpen() || $event)">
      <div class="release-chooser">
        <label class="field-label" for="release-repository" i18n="@@sig.releases.repository">Repository</label>
        <select id="release-repository" class="select-input" (change)="releaseRepository.set($any($event.target).value)">
          <option value="" [selected]="!shownRepository()" i18n="@@sig.releases.choose">Choose a repository</option>
          @for (r of repositories(); track r) {
            <option [value]="r" [selected]="shownRepository() === r" translate="no">{{ r }}</option>
          }
        </select>
      </div>
      @if (shownRepository(); as repo) {
        @if (releasesFailure(); as e) {
          <quader-error-notice [error]="e" (retry)="releases.reload()" />
        } @else {
          <quader-data-table
            [caption]="releasesCaption"
            [columns]="releaseColumns()"
            [loaded]="releases.hasValue()"
            [loading]="releases.isLoading()"
            [empty]="releaseItems().length === 0"
            [emptyText]="releasesEmpty"
            data-testid="signature-releases"
          >
            @for (rel of releaseItems(); track rel.sha256) {
              <tr>
                <th scope="row" class="wrap-name">
                  <span class="icon-name-text">
                    @if (releaseName(rel); as name) {
                      <span class="refusal-name" translate="no">{{ name }}</span>
                    }
                    <code class="secondary refusal-path" translate="no" [attr.title]="rel.sha256">sha256:{{ rel.sha256.slice(0, 16) }}…</code>
                  </span>
                </th>
                <td class="release-why">
                  <span>{{ rel.reason }}</span>
                  <span class="secondary block"><ng-container i18n="@@sig.releases.refused-for">Refused for:</ng-container>&ngsp;{{ reasonWord(rel.refusedReason) }}</span>
                </td>
                <td class="nowrap">
                  <quader-time-ago [at]="rel.releasedAt" absolute="title" />
                  <span class="secondary block" translate="no">{{ principal(rel.releasedBy) }}</span>
                </td>
                @if (canRelease()) {
                  <td class="actions">
                    <button class="btn btn-tertiary" type="button" [disabled]="busy()" (click)="withdrawRelease(rel)">
                      {{ withdrawLabel }}<span class="visually-hidden" translate="no">&ngsp;{{ releaseName(rel) ?? rel.sha256 }}</span>
                    </button>
                  </td>
                }
              </tr>
            }
          </quader-data-table>
        }
      } @else {
        <p class="hint" i18n="@@sig.releases.none-chosen">A release belongs to one repository: choose one to see its releases.</p>
      }
    </quader-disclosure>
  `,
  styles: `
    .list-note {
      margin-block: 0 var(--quader-space-3);
    }
    .refusal-name {
      font-weight: 600;
    }
    .refusal-path,
    .release-why {
      overflow-wrap: anywhere;
    }
    /* A repository's name breaks at its hyphens and slash where it must, not inside a word. */
    .refusal-repo {
      overflow-wrap: break-word;
    }
    .release-line {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      column-gap: var(--quader-space-1);
      margin-block-start: var(--quader-space-1);
    }
    .releases {
      margin-block-start: var(--quader-space-6);
    }
    .release-chooser {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-2);
      margin-block-end: var(--quader-space-3);
    }
    .release-chooser .select-input {
      max-inline-size: 100%;
    }
  `,
})
export class SignatureRefusals {
  private readonly api = inject(SignaturesApi);
  private readonly router = inject(Router);
  private readonly actions = inject(GateActions);

  /** The view's filters, bound by the router through the page. */
  readonly repository = input<string | undefined>();
  readonly reason = input<string | undefined>();
  /** The repositories the filters offer: every one the caller reads that a gate can apply to. */
  readonly repositories = input<readonly string[]>([]);

  protected readonly list = new PagedResource<SignatureRefusal>();
  protected readonly panelOpen = signal(false);
  protected readonly staged = signal<{ repository: string; reason: string }>({ repository: '', reason: '' });
  protected readonly busy = signal(false);
  protected readonly actionError = signal<ApiError | null>(null);
  protected readonly reasons = SIGNATURE_GATE_REASONS;
  protected readonly reasonWord = gateReasonWord;
  protected readonly principal = principalName;
  protected readonly artifact = refusalArtifact;
  protected readonly canRelease = computed(() => this.actions.canRelease());

  protected readonly filters = computed<SignatureRefusalFilters>(() => {
    const reason = this.reason();
    return {
      repository: this.repository() || undefined,
      reason: (SIGNATURE_GATE_REASONS as readonly string[]).includes(reason ?? '') ? (reason as SignatureGateReason) : undefined,
    };
  });
  protected readonly chips = computed<readonly Chip[]>(() => {
    const f = this.filters();
    const out: Chip[] = [];
    if (f.repository) out.push({ key: 'repository', label: $localize`:@@malware.filter.repository:Repository`, value: f.repository, remove: { repository: null } });
    if (f.reason) out.push({ key: 'reason', label: $localize`:@@sig.refusals.filter.reason:Reason`, value: gateReasonWord(f.reason), remove: { reason: null } });
    return out;
  });
  protected readonly filtered = computed(() => this.chips().length > 0);

  protected readonly caption = $localize`:@@sig.refusals.caption:Downloads the repositories refused for their signatures`;
  protected readonly emptyText = $localize`:@@sig.refusals.empty:No refused downloads.`;
  protected readonly emptyFiltered = $localize`:@@sig.refusals.empty-filtered:No refused download matches the filters.`;
  protected readonly releaseLabel = $localize`:@@sig.release.action:Release for this repository`;
  protected readonly withdrawLabel = $localize`:@@sig.withdraw:Withdraw release`;
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'artifact', label: $localize`:@@sig.refusals.col.artifact:Artifact`, width: '14rem', prefer: true },
    { id: 'repository', label: $localize`:@@sig.refusals.col.repository:Repository`, width: '13rem' },
    { id: 'reason', label: $localize`:@@sig.refusals.col.reason:Reason`, width: '12rem' },
    { id: 'last', label: $localize`:@@sig.refusals.col.last:Last refusal` },
    {
      id: 'count',
      label: $localize`:@@sig.refusals.col.count:Count`,
      numeric: true,
      help: $localize`:@@sig.refusals.col.count-help:How often a client was refused this artifact here since the first refusal that is still listed.`,
    },
    ...(this.canRelease() ? [{ id: 'actions', label: $localize`:@@malware.col.actions:Actions`, hideLabel: true }] : []),
  ]);

  // ---- the releases of one repository ----

  protected readonly releasesOpen = signal(false);
  /** The repository chosen in the section; the view's filter until one is chosen. */
  protected readonly releaseRepository = signal<string | null>(null);
  protected readonly shownRepository = computed(() => this.releaseRepository() ?? this.repository() ?? '');
  protected readonly releases = resource({
    params: () => (this.releasesOpen() && this.shownRepository() ? { repository: this.shownRepository() } : undefined),
    loader: ({ params }) => this.api.releases(params.repository),
  });
  protected readonly releaseItems = computed<readonly SignatureRelease[]>(() => valueOf(this.releases) ?? []);
  protected readonly releasesFailure = computed(() => {
    const e = this.releases.error();
    return e ? asApiError(e) : null;
  });
  protected readonly releasesHint = $localize`:@@sig.releases.hint:Artifacts released for one repository, which it serves whatever their signatures say.`;
  protected readonly releasesCaption = $localize`:@@sig.releases.caption:Releases of the repository`;
  protected readonly releasesEmpty = $localize`:@@sig.releases.empty:No artifact is released for this repository.`;
  protected readonly releaseColumns = computed<readonly TableColumn[]>(() => [
    { id: 'artifact', label: $localize`:@@sig.refusals.col.artifact:Artifact`, width: '14rem' },
    { id: 'reason', label: $localize`:@@sig.releases.col.reason:Reason of the release`, width: '16rem', prefer: true },
    { id: 'released', label: $localize`:@@sig.releases.col.released:Released` },
    ...(this.canRelease() ? [{ id: 'actions', label: $localize`:@@malware.col.actions:Actions`, hideLabel: true }] : []),
  ]);

  constructor() {
    effect(() => {
      const filters = this.filters();
      untracked(() => {
        this.restage();
        void this.list.start(this.api.refusals(filters));
      });
    });
  }

  /** A release names a digest only: the artifact of a refused download of the same bytes, where the list shows one (H-1239). */
  protected releaseName(rel: Pick<SignatureRelease, 'repository' | 'sha256'>): string | null {
    const row = this.list.items().find((r) => r.sha256 === rel.sha256 && r.repository === rel.repository);
    return row ? refusalArtifact(row) : null;
  }

  protected repositoryLink(qualified: string): string[] {
    const { project, key } = splitRef(qualified);
    return ['/projects', project, 'repositories', key];
  }

  protected restage(): void {
    const f = this.filters();
    this.staged.set({ repository: f.repository ?? '', reason: f.reason ?? '' });
  }

  protected stage(key: 'repository' | 'reason', value: string): void {
    this.staged.update((s) => ({ ...s, [key]: value }));
  }

  protected submitFilters(): void {
    const s = this.staged();
    this.navigate({ repository: s.repository || null, reason: s.reason || null });
  }

  protected clearFilters(): void {
    this.navigate({ repository: null, reason: null });
  }

  protected removeChip(chip: ListChip): void {
    this.navigate({ ...(chip as Chip).remove });
  }

  private navigate(queryParams: Record<string, string | null>): void {
    void this.router.navigate([], { queryParams, queryParamsHandling: 'merge' });
  }

  protected async act(r: SignatureRefusal): Promise<void> {
    this.actionError.set(null);
    if (r.release) {
      await this.withdrawRelease({ repository: r.repository, sha256: r.sha256 }, refusalArtifact(r));
      return;
    }
    const done = await this.actions.release({ repository: r.repository, sha256: r.sha256, artifact: refusalArtifact(r), reason: r.reason });
    if (done) await this.reloadAll();
  }

  protected async withdrawRelease(rel: Pick<SignatureRelease, 'repository' | 'sha256'>, name?: string): Promise<void> {
    this.busy.set(true);
    this.actionError.set(null);
    try {
      const artifact = name ?? this.releaseName(rel) ?? `sha256:${rel.sha256.slice(0, 16)}…`;
      if (await this.actions.withdraw(rel.repository, rel.sha256, artifact)) await this.reloadAll();
    } catch (e) {
      this.actionError.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }

  private async reloadAll(): Promise<void> {
    if (this.releasesOpen()) this.releases.reload();
    await this.list.reload();
  }
}
