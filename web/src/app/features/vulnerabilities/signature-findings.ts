import { Component, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { LIVE_IDLE_MS, live } from '../../core/live-refresh';
import { Router, RouterLink } from '@angular/router';
import { RepositoriesApi, refOf, splitRef } from '../../api/repositories-api';
import { SIGNATURE_FINDING_KINDS, SignatureFinding, SignatureFindingFilters, SignatureFindingKind, SignaturesApi } from '../../api/signatures-api';
import { SessionStore } from '../../core/session-store';
import { isSystemRepository } from '../../core/upstream-project';
import { PagedResource, valueOf } from '../../shared/paged-resource';
import { entryKind, signaturePromise, signatureReason } from '../../views/signatures/signature-words';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { FilterCategory, FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { TimeAgo } from '../../shared/ui/time-ago';
import { PageHeader } from '../../shared/ui/page-header';
import { SignatureRefusals } from './signature-refusals';
import { IDENTITY_MARKS, PartsText } from '../../shared/break-parts';

/** One chip of a filter that is set, with the query change that removes it. */
interface Chip extends ListChip {
  readonly remove: Readonly<Record<string, string | null>>;
}

/** The drawer's filters before **Apply**. */
interface Staged {
  readonly kind: string;
  readonly repository: string;
}

/** The word of a finding's kind. */
export function findingKindWord(kind: SignatureFindingKind): string {
  return kind === 'invalid' ? $localize`:@@sig.badge.invalid:Invalid signature` : $localize`:@@sig.finding.blocked:Blocked signer`;
}

/**
 * The page Signatures under Security audit (R-SIG-038, R-SIG-074, Q-SIG-9, Q-UI-1005): first the downloads the repositories' gates refused
 * and their releases (`SignatureRefusals`); then one row per signature that is invalid or whose signer an administrator blocked, newest
 * first, with the artifact it covers and the repository that holds it, the kind, the signer as it claims, the reason, when it was found
 * and how many repositories hold it. The head searches a signer, the drawer filters by kind and repository; the repository is the view's
 * filter, which both lists share; filters live in the address. A holder of `security-settings:write` finds **Block signer** on the
 * admin page.
 */
@Component({
  selector: 'quader-signature-findings',
  imports: [DataTable, FilterCategory, FilterPanel, ListHead, PageHeader, PartsText, RouterLink, SignatureRefusals, TimeAgo],
  template: `
    <!-- An entry of its own under Security audit (Q-UI-1005), the page named as its entry. -->
    <quader-page-header i18n-heading="@@security.signatures.heading" heading="Signatures" />
    <p class="hint" role="note" data-testid="signature-promise">{{ promise }}</p>

    <!-- The downloads the repositories' gates refused, and their releases (R-SIG-074): first, since a release is an action. -->
    <quader-signature-refusals [repository]="repository()" [reason]="reason()" [repositories]="repositoryOptions()" />

    <h2 class="vuln-list-title findings-title" i18n="@@sig.findings.title">Findings</h2>
    <quader-list-head
      [searchLabel]="searchLabel"
      [search]="q() ?? ''"
      (searchChange)="searchFor($event)"
      [chips]="chips()"
      (removeChip)="removeChip($event)"
      [(filtersOpen)]="panelOpen"
      [controls]="panel.panelId"
    />
    <quader-filter-panel #panel [(open)]="panelOpen" [activeCount]="chips().length" (apply)="submitFilters()" (clearAll)="clearFilters()" (dismissed)="restage()">
      <quader-filter-category i18n-label="@@sig.findings.filter.kind" label="Kind">
        <select class="select-input" aria-label="Kind" i18n-aria-label="@@sig.findings.filter.kind" (change)="stage('kind', $any($event.target).value)">
          <option value="" [selected]="!staged().kind" i18n="@@sig.findings.filter.any">Any</option>
          @for (k of kinds; track k) {
            <option [value]="k" [selected]="staged().kind === k">{{ kindWord(k) }}</option>
          }
        </select>
      </quader-filter-category>
      <quader-filter-category i18n-label="@@malware.filter.repository" label="Repository">
        <select class="select-input" aria-label="Repository" i18n-aria-label="@@malware.filter.repository" (change)="stage('repository', $any($event.target).value)">
          <option value="" [selected]="!staged().repository" i18n="@@malware.filter.any-repository">Any repository</option>
          @for (r of repositoryOptions(); track r) {
            <option [value]="r" [selected]="staged().repository === r" translate="no">{{ r }}</option>
          }
        </select>
      </quader-filter-category>
    </quader-filter-panel>

    <quader-data-table
      [caption]="caption"
      [columns]="columns"
      [loaded]="list.loaded()"
      [loading]="list.loading()"
      loadingText="Loading the signature findings."
      i18n-loadingText="@@sig.findings.loading"
      [error]="list.error()"
      [empty]="list.items().length === 0"
      [emptyText]="filtered() ? emptyFiltered : emptyText"
      [hasMore]="list.hasMore()"
      (more)="list.more()"
      (retry)="retry()"
    >
      @for (f of list.items(); track f.id) {
        <tr>
          <!-- One form at 1,440 and 1,024 px (Q-UI-969): what the row is about under the artifact's name: its path, the reason, the
               repositories that hold it; blocks only at 390 px. -->
          <th scope="row" class="wrap-name">
            <span class="icon-name-text">
              <!-- The name breaks after its dots, slashes and at signs, never inside a word, and before its version first; the version
                   stays whole while it fits on a line (Q-UI-567, Q-UI-969). -->
              <span class="finding-name" translate="no"
                ><quader-parts-text [value]="artifactName(f)" [marks]="identityMarks" />@if (artifactVersion(f); as version) {&ngsp;<span
                    class="finding-version"
                    >{{ version }}</span
                  >}</span
              >
              @if (artifactPath(f); as path) {
                <!-- The path keeps the version whole wherever it stands in it. -->
                <span class="secondary finding-text" translate="no"
                  ><quader-parts-text [value]="path" [marks]="identityMarks" [keep]="artifactVersion(f)"
                /></span>
              }
              <span class="finding-reason finding-text">
                @if (f.block; as b) {
                  <span translate="no">{{ b.reason }}</span>
                } @else {
                  {{ reasonText(f.reason) }}
                }
              </span>
              <span class="secondary finding-text"
                ><a [routerLink]="repositoryLink(f.firstRepository)" translate="no">{{ f.firstRepository }}</a
                >@if (f.repositoryCount > 1) {&ngsp;<span i18n="@@sig.findings.more-repositories">and {{ f.repositoryCount - 1 }} more</span>}</span
              >
            </span>
          </th>
          <td>
            <!-- The kind as the list's status is shown: its icon and word on the row's line, the kind of signature under it (Q-UI-876). -->
            <span [class]="'fstatus ' + (f.kind === 'invalid' ? 'bad' : '')"
              ><span class="fstatus-main"
                ><span class="material-icons" aria-hidden="true">{{ f.kind === 'invalid' ? 'gpp_bad' : 'block' }}</span><span>{{ kindWord(f.kind) }}</span></span
              ></span
            >&ngsp;<span class="secondary block">{{ kindOf(f) }}</span>
          </td>
          <!-- The signer as it claims and its issuer: a URL or a distinguished name breaks after a slash, a dot, an at sign or a comma, and
               inside a word only where one piece is longer than the line. -->
          <td class="finding-text">
            <span translate="no"><quader-parts-text [value]="f.signer ?? f.identity ?? f.keyId ?? '–'" [marks]="identityMarks" /></span>
            @if (f.issuer; as issuer) {
              <span class="secondary block" translate="no"><quader-parts-text [value]="issuer" [marks]="identityMarks" /></span>
            }
          </td>
          <td><quader-time-ago [at]="f.foundAt" absolute="title" /></td>
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    .finding-name {
      font-weight: 600;
      overflow-wrap: anywhere;
    }
    /* A version stays whole while it fits on a line, and wraps inside itself only where it is longer than the line. */
    .finding-version {
      display: inline-block;
      max-inline-size: 100%;
    }
    .finding-text {
      overflow-wrap: anywhere;
    }
    .icon-name-text > .finding-text {
      display: block;
    }
    .finding-reason {
      margin-block-start: var(--quader-space-1);
      font-weight: 400;
    }
    .findings-title {
      margin-block-start: var(--quader-space-6);
    }
  `,
})
export class SignatureFindings {
  private readonly api = inject(SignaturesApi);
  private readonly repositories = inject(RepositoriesApi);
  private readonly router = inject(Router);
  protected readonly session = inject(SessionStore);

  /** Query parameters, bound by the router. */
  readonly kind = input<string | undefined>();
  readonly repository = input<string | undefined>();
  readonly q = input<string | undefined>();
  /** The refused downloads' filter of a reason (R-SIG-074). */
  readonly reason = input<string | undefined>();

  protected readonly list = new PagedResource<SignatureFinding>();
  protected readonly panelOpen = signal(false);
  protected readonly staged = signal<Staged>({ kind: '', repository: '' });
  protected readonly kinds = SIGNATURE_FINDING_KINDS;
  protected readonly kindWord = findingKindWord;
  /** A name, a path or an identity breaks after a slash, a dot, an at sign or a comma. */
  protected readonly identityMarks = IDENTITY_MARKS;
  protected readonly reasonText = signatureReason;
  protected readonly promise = signaturePromise();

  private readonly repositoryList = resource({ loader: () => this.repositories.listAll() });
  /** The repositories the filter offers; a system repository of `upstream` holds no signature (R-SIG-011). */
  protected readonly repositoryOptions = computed(() => {
    const names = (valueOf(this.repositoryList) ?? []).filter((r) => !isSystemRepository(r.project, r.key)).map((r) => refOf(r));
    const current = this.repository();
    return current && !names.includes(current) ? [current, ...names] : names;
  });

  protected readonly filters = computed<SignatureFindingFilters>(() => {
    const kind = this.kind();
    return {
      kind: (SIGNATURE_FINDING_KINDS as readonly string[]).includes(kind ?? '') ? (kind as SignatureFindingKind) : undefined,
      repository: this.repository() || undefined,
      q: this.q() || undefined,
    };
  });

  protected readonly chips = computed<readonly Chip[]>(() => {
    const f = this.filters();
    const out: Chip[] = [];
    if (f.kind) out.push({ key: 'kind', label: $localize`:@@sig.findings.filter.kind:Kind`, value: findingKindWord(f.kind), remove: { kind: null } });
    if (f.repository) out.push({ key: 'repository', label: $localize`:@@malware.filter.repository:Repository`, value: f.repository, remove: { repository: null } });
    return out;
  });
  protected readonly filtered = computed(() => this.chips().length > 0 || !!this.q());

  protected readonly searchLabel = $localize`:@@sig.findings.search:Search by signer`;
  protected readonly caption = $localize`:@@sig.findings.caption:Invalid signatures and blocked signers`;
  protected readonly emptyText = $localize`:@@sig.findings.empty:No invalid signatures.`;
  protected readonly emptyFiltered = $localize`:@@sig.findings.empty-filtered:No finding matches the filters.`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'artifact', label: $localize`:@@sig.findings.col.artifact:Artifact`, width: '10rem', prefer: true },
    { id: 'kind', label: $localize`:@@sig.findings.col.kind:Kind`, width: '8rem' },
    { id: 'signer', label: $localize`:@@sig.findings.col.signer:Signer`, width: '9rem' },
    { id: 'found', label: $localize`:@@sig.findings.col.found:Found` },
  ];

  /**
   * The list's Retry (R-UI-216): the page of the list that failed, and the drawer's repositories that failed with it; read once and
   * never again, the drawer offered no repository after the page came back.
   */
  protected retry(): void {
    void this.list.more();
    if (this.repositoryList.error()) this.repositoryList.reload();
  }

  constructor() {
    // The drawer's repositories that failed are read again when it opens (R-UI-216).
    effect(() => {
      if (this.panelOpen() && untracked(() => this.repositoryList.error())) untracked(() => this.repositoryList.reload());
    });
    effect(() => {
      const filters = this.filters();
      untracked(() => {
        this.restage();
        void this.list.start(this.api.findings(filters));
      });
    });
    // A signature check adds findings: the rows shown are read again every minute in place (R-UI-213).
    live({ reload: () => this.list.refresh(), every: () => LIVE_IDLE_MS });
  }

  /** The artifact a finding's signature covers: its package and version, else its file, else the npm subject's name. */
  /** What the finding covers by name: the package, else the file's name, else the subject. */
  protected artifactName(f: SignatureFinding): string {
    const item = f.items[0];
    if (item?.package) return item.package;
    if (item?.path) return item.path.split('/').pop() ?? item.path;
    return f.subjectName ?? `sha256:${f.subject.slice(0, 12)}…`;
  }

  /** The package's version after its name, or none. */
  protected artifactVersion(f: SignatureFinding): string | null {
    const item = f.items[0];
    return item?.package && item.version ? item.version : null;
  }

  /** The path of the file under the name, where the name is the package's. */
  protected artifactPath(f: SignatureFinding): string | null {
    const item = f.items[0];
    return item?.package && item.path ? item.path : null;
  }

  protected kindOf(f: SignatureFinding): string {
    return entryKind(f);
  }

  protected repositoryLink(qualified: string): string[] {
    const { project, key } = splitRef(qualified);
    return ['/projects', project, 'repositories', key];
  }

  protected restage(): void {
    const f = this.filters();
    this.staged.set({ kind: f.kind ?? '', repository: f.repository ?? '' });
  }

  protected stage(key: 'kind' | 'repository', value: string): void {
    this.staged.update((s) => ({ ...s, [key]: value }));
  }

  protected submitFilters(): void {
    const s = this.staged();
    this.navigate({ kind: s.kind || null, repository: s.repository || null });
  }

  protected clearFilters(): void {
    this.navigate({ kind: null, repository: null });
  }

  protected removeChip(chip: ListChip): void {
    this.navigate({ ...(chip as Chip).remove });
  }

  protected searchFor(text: string): void {
    this.navigate({ q: text || null });
  }

  private navigate(queryParams: Record<string, string | null>): void {
    void this.router.navigate([], { queryParams, queryParamsHandling: 'merge' });
  }
}
