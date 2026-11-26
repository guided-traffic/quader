import { NgTemplateOutlet } from '@angular/common';
import { Component, DestroyRef, ElementRef, afterRenderEffect, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { ApiError } from '../../../api/problem';
import { UpstreamAbout, UpstreamCatalogueApi, UpstreamReadme } from '../../../api/upstream-catalogue-api';
import { ErrorNotice } from '../../../shared/error-notice';
import { asApiError } from '../../../shared/paged-resource';
import { Disclosure } from '../../../shared/ui/disclosure';
import { Loading } from '../../../shared/ui/loading';
import { reasonText } from '../../packages/deprecation-dialog';
import { EXTERNAL_REL, safeHttpUrl } from '../../packages/package-links';
import { PackageReadmeView } from '../../packages/package-readme';
import { aroundValue } from '../../../shared/value-list';
import { UpstreamActions } from './upstream-actions';
import { catalogueState, clockAfter } from './upstream-words';

/** A link of the facts line: its words and its address (an http or https address only, R-UI-007). */
interface AboutLink {
  readonly label: string;
  readonly href: string;
}

/** A block of dependencies: its heading and its names with their ranges. */
interface DependencyBlock {
  readonly label: string;
  readonly entries: readonly (readonly [string, string])[];
}

/** The words of a link's kind, as a stored version's page names them (R-UI-007). */
export function linkLabel(kind: string): string {
  switch (kind) {
    case 'homepage':
      return $localize`:@@pkg.meta.homepage:Homepage`;
    case 'repository':
      return $localize`:@@pkg.meta.source:Source repository`;
    case 'issues':
      return $localize`:@@pkg.meta.bugs:Issue tracker`;
    case 'documentation':
      return $localize`:@@pkg.meta.documentation:Documentation`;
    default:
      return $localize`:@@pkg.meta.license-url:License`;
  }
}

/**
 * The dependencies of the newest version in the blocks of a stored version's page (R-UI-007): the plain ones, npm's peer and optional
 * ones, and NuGet's per target framework.
 */
export function dependencyBlocks(about: UpstreamAbout | null | undefined): DependencyBlock[] {
  const blocks = new Map<string, [string, string][]>();
  for (const d of about?.dependencies ?? []) {
    const key = d.group ?? '';
    const list = blocks.get(key) ?? [];
    list.push([d.name, d.range ?? '']);
    blocks.set(key, list);
  }
  return [...blocks.entries()].map(([group, entries]) => ({ label: groupLabel(group), entries }));
}

function groupLabel(group: string): string {
  switch (group) {
    case '':
      return $localize`:@@pkg.meta.dependencies:Dependencies`;
    case 'peer':
      return $localize`:@@pkg.meta.peer-dependencies:Peer dependencies`;
    case 'optional':
      return $localize`:@@pkg.meta.optional-dependencies:Optional dependencies`;
    default:
      return $localize`:@@upstream.docs.framework-dependencies:Dependencies for ${group}:framework:`;
  }
}

/** The formats whose upstream serves a README the tab reads (R-CUR-028): npm, NuGet, Ansible Galaxy. */
const README_FORMATS: ReadonlySet<string> = new Set(['npm', 'nuget', 'ansible']);

/** The height a README shows before **Show the whole README**: the versions stay within reach (R-UI-215, Q-UI-1340). */
export const README_CLIP_REM = 24;

/**
 * What a package of the upstream tells before a version of it is stored (remote-curation.md R-CUR-028, Q-UI-1340 to Q-UI-1342): the
 * deprecation of its newest version, a line of its facts (the version they are of, the licence, the links out), its README read through
 * and not stored, and the dependencies of the newest version folded. The README is wiki content: it is asked only where the package's
 * answer says the reader may read it (`readme`), and a reader who may not is told what a stored version without one tells. Opened in a
 * row of the search the README is folded; on the package's own view it is open, its first 24 rem shown with **Show the whole README**,
 * so the versions and their switches stay as near as before. A failure, a rate limit or a host the repository does not reach is said in
 * place and the versions show all the same. The links leave Quader as every link out does: http and https only, no referrer.
 */
@Component({
  selector: 'quader-upstream-docs',
  imports: [Disclosure, ErrorNotice, Loading, NgTemplateOutlet, PackageReadmeView],
  templateUrl: './upstream-docs.html',
  styleUrl: './upstream-docs.scss',
})
export class UpstreamDocs {
  private readonly api = inject(UpstreamCatalogueApi);
  private readonly actions = inject(UpstreamActions);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  readonly name = input.required<string>();
  /** The facts of the newest version from the package's answer; null where the upstream's answers tell none. */
  readonly about = input<UpstreamAbout | null | undefined>(null);
  /** The package's answer says the README may be asked for: the format serves one and the reader may read the wiki. */
  readonly readme = input(false);
  readonly inRow = input(false);

  protected readonly externalRel = EXTERNAL_REL;
  protected readonly answer = signal<UpstreamReadme | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly loading = signal(false);
  /** The README shown whole: the reader asked for it, or moved the focus into it. */
  protected readonly whole = signal(false);
  /** The README is taller than its first 24 rem, so the button is there. */
  protected readonly clipped = signal(false);
  private asked = '';
  private retryTimer: ReturnType<typeof setTimeout> | null = null;

  protected readonly format = computed(() => this.actions.format());
  /** The format's upstream serves a README, so a package without one, or a reader without the wiki, is told there is none. */
  protected readonly readmeFormat = computed(() => README_FORMATS.has(this.format()));
  protected readonly links = computed<AboutLink[]>(() =>
    (this.about()?.links ?? []).flatMap((l) => {
      const href = safeHttpUrl(l.url);
      return href ? [{ label: linkLabel(l.kind), href }] : [];
    }),
  );
  protected readonly dependencies = computed(() => dependencyBlocks(this.about()));
  protected readonly dependencyCount = computed(() => this.about()?.dependencies.length ?? 0);
  protected readonly dependenciesHeading = computed(() => {
    const v = this.about()?.version;
    return v ? $localize`:@@upstream.docs.dependencies-of:Dependencies of ${v}:version:` : $localize`:@@pkg.meta.dependencies:Dependencies`;
  });
  protected readonly deprecation = computed(() => this.about()?.deprecation ?? null);
  protected readonly reasons = computed(() => (this.deprecation()?.reasons ?? []).map(reasonText).join('; '));
  protected readonly readmeHeading = $localize`:@@upstream.docs.readme:README`;
  /** The sentence around the host the upstream keeps the README on, which stands as a value of its own (Q-UI-1213). */
  protected readonly unreachable = computed(() =>
    aroundValue((host) => $localize`:@@upstream.docs.unreachable:The upstream keeps this README on ${host}:host:, a host this repository does not reach; Quader did not ask there.`),
  );
  /** The catalogue's own states in the tab's words (the budget, the upstream offline); another failure is the shared notice. */
  protected readonly stateSentence = computed(() => {
    const f = this.failure();
    return f ? catalogueState(f, 'package') : null;
  });
  protected readonly limitedSentence = computed(() => {
    const a = this.answer();
    if (a?.state !== 'limited' || !a.until) return null;
    const seconds = Math.max(0, Math.round((Date.parse(a.until) - Date.now()) / 1000));
    return $localize`:@@upstream.docs.limited:The upstream rate-limits this repository until ${clockAfter(seconds)}:time:; the README is read after that.`;
  });

  constructor() {
    // One read per package and version: the package's answer is read again every few seconds (R-UI-213), the README is not.
    effect(() => {
      const name = this.name();
      const version = this.about()?.version ?? null;
      const may = this.readme();
      untracked(() => {
        const key = may ? JSON.stringify([name, version]) : '';
        if (key === this.asked) return;
        this.asked = key;
        this.answer.set(null);
        this.failure.set(null);
        this.whole.set(false);
        if (key !== '') void this.read(name, version);
      });
    });
    afterRenderEffect(() => {
      this.answer();
      this.whole();
      const frame = this.host.nativeElement.querySelector<HTMLElement>('.readme-frame');
      this.clipped.set(frame !== null && !this.whole() && frame.scrollHeight > frame.clientHeight + 1);
    });
    inject(DestroyRef).onDestroy(() => this.clearRetry());
  }

  private async read(name: string, version: string | null): Promise<void> {
    this.clearRetry();
    this.loading.set(true);
    try {
      const a = await this.api.readme(this.actions.repository(), name, version);
      if (this.asked !== JSON.stringify([name, version])) return;
      this.answer.set(a);
      this.failure.set(null);
      if (a.state === 'limited' && a.until) this.retryIn(Date.parse(a.until) - Date.now(), name, version);
    } catch (e) {
      if (this.asked !== JSON.stringify([name, version])) return;
      const f = asApiError(e);
      // A README the reader may not read answers 404 like one there is none of (R-AUTHZ-034): the same sentence.
      if (f.status === 404) {
        this.answer.set({ version: version ?? '', state: 'none', host: null, until: null, readme: null });
      } else {
        this.failure.set(f);
        if (f.code === 'catalogue_budget_exhausted' && f.retryAfterSeconds !== null) this.retryIn(f.retryAfterSeconds * 1000, name, version);
      }
    } finally {
      this.loading.set(false);
    }
  }

  /** A budget or a rate limit names when the README may be asked again: it is, once, then (R-UI-213). */
  private retryIn(ms: number, name: string, version: string | null): void {
    this.clearRetry();
    this.retryTimer = setTimeout(() => void this.read(name, version), Math.min(Math.max(ms, 1000), 15 * 60_000));
  }

  private clearRetry(): void {
    if (this.retryTimer !== null) clearTimeout(this.retryTimer);
    this.retryTimer = null;
  }

  protected retry(): void {
    void this.read(this.name(), this.about()?.version ?? null);
  }
}
