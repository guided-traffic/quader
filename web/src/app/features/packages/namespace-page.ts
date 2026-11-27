import { Component, computed, effect, inject, input, resource, untracked } from '@angular/core';
import { MalwareMark, malwareColumn, malwareShown } from '../../shared/malware/malware-mark';
import { MalwareSignaturesStore } from '../../core/malware-signatures';
import { RouterLink } from '@angular/router';
import { NamespaceClaim, NamespaceRepository, PackageSummary, PackagesApi } from '../../api/packages-api';
import { LIVE_IDLE_MS, live } from '../../core/live-refresh';
import { formatName } from '../../core/format-names';
import { ErrorNotice } from '../../shared/error-notice';
import { PagedResource, failureOf, valueOf } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { DeprecationMark } from '../../shared/ui/deprecation-mark';
import { Loading } from '../../shared/ui/loading';
import { PageHeader } from '../../shared/ui/page-header';
import { ProjectLink } from '../../shared/ui/project-mark';
import { TimeAgo } from '../../shared/ui/time-ago';
import { SignatureBadge } from '../../views/signatures/signature-badge';
import { NamespaceRules, namespaceCommands, namespaceHeading, namespacesOf } from '../../views/packages/namespaces';
import { packageCommands } from './package-links';
import { PackageName } from '../../views/packages/package-name';
import { PackageTags } from '../../views/packages/package-tags';
import { collectionUpload } from '../repositories/content/ansible-upload-targets';

/**
 * The page of a name (R-UI-220, Q-UI-1290 to Q-UI-1294): a namespace (Ansible Galaxy), a scope (npm) or a path prefix (OCI), made from
 * what exists, in every format that has one. It lists the packages of the name the reader may read, in the project of its address or,
 * at `/namespaces/…`, in every project; and, to a reader of a repository's configuration, what decides a publish into the name: the
 * local repositories of the format with their claims, their package ownership and their automatic claims, in plain words. It has no
 * description, logo or owner of its own: the name is no object. A reader of the packages alone sees the packages.
 */
@Component({
  selector: 'quader-namespace-page',
  imports: [DataTable, DeprecationMark, ErrorNotice, Loading, MalwareMark, PackageName, PackageTags, PageHeader, ProjectLink, RouterLink, SignatureBadge, TimeAgo],
  templateUrl: './namespace-page.html',
  styleUrl: './namespace-page.scss',
})
export class NamespacePage {
  private readonly api = inject(PackagesApi);
  private readonly rules = inject(NamespaceRules);

  /** Route parameter `:project`; absent at `/namespaces/…`, the page across every project. */
  readonly project = input<string | undefined>();
  readonly format = input.required<string>();
  /** The namespace with its slashes (`team/app`). */
  readonly name = input.required<string>();

  protected readonly formatLabel = formatName(() => this.format());
  protected readonly scope = computed(() => this.project() ?? null);
  private readonly rule = computed(() => this.rules.of(this.format()));

  protected readonly facts = resource({
    params: () => ({ format: this.format(), name: this.name(), project: this.project() }),
    loader: ({ params }) => this.api.namespace(params.format, params.name, params.project),
  });
  protected readonly factsFailure = failureOf(this.facts);
  protected readonly list = new PagedResource<PackageSummary>();

  protected readonly heading = computed(() => namespaceHeading(valueOf(this.facts)?.kind ?? this.rule()?.kind ?? 'namespace', this.name()));
  protected readonly description = computed(() => {
    const where = this.project();
    return where
      ? $localize`:@@ns.description.project:The ${this.formatLabel()}:format: packages of the name in ${where}:project: that you may read.`
      : $localize`:@@ns.description.all:The ${this.formatLabel()}:format: packages of the name in every project that you may read.`;
  });
  /** The namespaces that hold this one (`team` of `team/app`), outermost first, each with its page. */
  protected readonly parents = computed(() => namespacesOf(this.rule(), this.name() + (this.rule()?.separator ?? '') + 'x').slice(0, -1));
  /** What every name of the namespace starts with: `acme.`, `@acme/`, `team/app/`. */
  protected readonly prefix = computed(() => this.name() + (this.rule()?.separator ?? ''));
  protected readonly publishing = computed(() => valueOf(this.facts)?.publishing ?? null);
  /**
   * **Upload collection** in the head of an Ansible namespace's page (R-ANS-061, Q-UI-1324): the dialog with the repositories of the
   * page's scope (its project, or every project) the reader may publish into; none for another format.
   */
  protected readonly upload = collectionUpload(() => (this.format() === 'ansible' ? (this.project() ?? null) : undefined));

  private readonly malwareMarks = inject(MalwareSignaturesStore);
  /** The rows carry the malware state of their newest version (R-MAL-039): the column **Malware scan** (R-UI-255). */
  protected readonly malware = computed(() => malwareShown(this.malwareMarks, this.list.items()));
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'name', label: $localize`:@@pkg.col.package:Package`, width: '16rem', prefer: true },
    ...(this.project() ? [] : [{ id: 'project', label: $localize`:@@projects.col.project:Project`, width: '10rem' }]),
    {
      id: 'latest',
      label: $localize`:@@pkg.col.latest:Latest`,
      help: $localize`:@@pkg.help.latest:The highest stable version you may see. A pre-release is shown only while no stable version exists.`,
    },
    ...(this.malware() ? [malwareColumn('package')] : []),
    {
      id: 'updatedAt',
      label: $localize`:@@pkg.col.updated:Updated`,
      help: $localize`:@@pkg.help.updated:When the latest version you may see was published.`,
    },
  ]);
  protected readonly caption = computed(() => this.heading());
  protected readonly emptyText = $localize`:@@ns.list.empty:No package of this name that you may read.`;

  constructor() {
    effect(() => {
      const filters = { project: this.project(), format: [this.format()], namespace: this.name(), sort: 'name' as const };
      untracked(() => void this.list.start(this.api.list(filters)));
    });
    // Clients publish into the name and its claims change: what the page shows is read again in place while it is seen (R-UI-213).
    live({
      reload: () => {
        this.facts.reload();
        return this.list.refresh();
      },
      every: () => LIVE_IDLE_MS,
    });
  }

  protected placeOf(row: PackageSummary): { project: string; key: string } {
    const [project = '', key = ''] = (row.repositories[0] ?? '').split('/');
    return { project, key };
  }

  protected packageLink(row: PackageSummary): string[] {
    return packageCommands(this.project() ?? this.placeOf(row).project, row.format, row.key ?? row.name);
  }

  protected namespaceLink(ns: string, project: string | null): string[] {
    return namespaceCommands(project, this.format(), ns);
  }

  /** The repositories with a claim that covers the whole namespace: a first publish of a new name elsewhere is no auto claim there. */
  protected claimsAll(r: NamespaceRepository): boolean {
    return r.claims.some((c) => c.covers === 'all');
  }

  protected claimScope(c: NamespaceClaim): string {
    return c.scope === 'global'
      ? $localize`:@@ns.claim.global:in every virtual repository of the format`
      : $localize`:@@ns.claim.members:in the virtual repositories that contain this repository`;
  }

  protected claimCovers(c: NamespaceClaim): string {
    return c.covers === 'all'
      ? $localize`:@@ns.claim.all:every name of it`
      : $localize`:@@ns.claim.some:some names of it`;
  }

  protected claimSource(c: NamespaceClaim): string {
    return c.source === 'manual' ? $localize`:@@ns.claim.manual:set by hand` : $localize`:@@ns.claim.auto:claimed by a first publish`;
  }
}
