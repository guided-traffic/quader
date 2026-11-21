import { DownloadLink } from '../../../shared/download-link';
import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { VisibilityIcon, levelName } from '../../../shared/ui/visibility-level';
import { Component, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { newIdempotencyKey } from '../../../api/client';
import { FilesApi, Item, isFilesAddress } from '../../../api/files-api';
import { ApiError } from '../../../api/problem';
import {
  FormatDescriptor,
  JobReference,
  RepositoriesApi,
  Repository,
  RepositoryConsumerView,
  isConsumerView,
  repoRef,
} from '../../../api/repositories-api';
import { Notifier } from '../../../core/notifier';
import { filesCommands, setupCommands } from '../../../core/project-links';
import { ServerAddress } from '../../../core/server-address';
import { SessionStore } from '../../../core/session-store';
import { isSystemRepository } from '../../../core/upstream-project';
import { Confirm } from '../../../shared/confirm-dialog';
import { CopyButton } from '../../../shared/copy-button';
import { ErrorNotice } from '../../../shared/error-notice';
import { JobProgress } from '../../../shared/job-progress';
import { asApiError, valueOf } from '../../../shared/paged-resource';
import { PageHeader } from '../../../shared/ui/page-header';
import { AddressField } from '../../../shared/address-field';
import { Disclosure } from '../../../shared/ui/disclosure';
import { FrequentPlaces } from '../../../core/frequent-places';
import { clientAddress } from '../repository-model';
import { classIcon, classLabel } from '../repository-class';
import { contentShapeOf } from './content-shape';
import { FilePermissions, NO_FILE_PERMISSIONS, nameOf, parentOf } from './file-paths';
import { RepositoryFile } from './repository-file';
import { RepositoryFiles } from './repository-files';
import { ItemSort, RepositoryItems } from './repository-items';
import { FormatIconView } from '../../../shared/format-icon';

const SORTS: readonly ItemSort[] = ['name', '-name', 'updatedAt', '-updatedAt', 'grade', '-grade'];

/** A delete of a folder with more items than this asks for the folder name (R-UI-011). */
export const TYPE_TO_CONFIRM_ABOVE = 100;

/**
 * The page of a repository, `/projects/<project>/repositories/<key>` (R-UI-052, Q-UI-216), and its folders and files,
 * `…/<key>/files/<path…>` (R-UI-010, ADR-203): what the repository holds, in one shell for every format. A package format lists its
 * images or packages and opens their versions in the row, and shows its raw files on demand (**Files** in the header's menu); a
 * generic repository's content is its files. A folder lists its entries, a file shows its facts and its download; upload, new
 * folder and delete are offered where the caller holds the permission (R-UI-011, R-UI-012). The header (Q-UI-307, Q-UI-313,
 * Q-UI-664) leads to **Set up client**, where the client address stands (R-UI-169); the settings are a separate route
 * (`…/settings`).
 */
@Component({
  selector: 'quader-repository-content',
  imports: [
    FormatIconView,
    VisibilityIcon,
    CdkMenu,
    CdkMenuItem,
    CdkMenuTrigger,
    RouterLink,
    CopyButton,
    ErrorNotice,
    AddressField,
    Disclosure,
    JobProgress,
    PageHeader,
    RepositoryFile,
    RepositoryFiles,
    RepositoryItems,
    DownloadLink,
  ],
  templateUrl: './repository-content.html',
  styleUrl: './repository-content.scss',
})
export class RepositoryContent {
  private readonly api = inject(RepositoriesApi);
  private readonly files = inject(FilesApi);
  private readonly router = inject(Router);
  private readonly session = inject(SessionStore);
  private readonly notifier = inject(Notifier);
  private readonly confirm = inject(Confirm);
  private readonly frequent = inject(FrequentPlaces);
  private readonly serverAddress = inject(ServerAddress);
  private readonly baseUrl = resource({ loader: () => this.serverAddress.baseUrl() });

  /** Route parameters, bound by the router's component input binding. */
  readonly project = input.required<string>();
  readonly key = input.required<string>();
  /** The path after `files` (the matcher of Q-UI-510); undefined on the repository's own address. */
  readonly filePath = input<string | undefined>();
  /** Query parameters (R-UI-003): the filter and the sort of a package format's list. */
  readonly q = input<string | undefined>();
  readonly sort = input<string | undefined>();

  protected readonly ref = computed(() => repoRef(this.project(), this.key()));
  /** Set up client of this repository (R-UI-126); none for a system repository of `upstream`, which answers no client (Q-UI-896). */
  protected readonly setupLink = computed(() => setupCommands(this.ref()));
  /**
   * A system repository of the project `upstream` (R-REPO-070, R-REPO-075): Quader's own work fills and reads it, every client's
   * request and every deploy answers 404, and the management API lists and deletes its items. The page offers it no client
   * action: no **Set up client**, no upload or new folder, no sentence about what clients fetch or publish (Q-UI-896); no download of
   * a file, no address for downloads by path, no address of a file to copy and no install command (Q-UI-898).
   */
  protected readonly system = computed(() => isSystemRepository(this.project(), this.key()));
  protected readonly loaded = resource({
    params: () => this.ref(),
    loader: ({ params }) => this.api.get(params),
  });
  protected readonly formats = resource({ loader: () => this.api.formats() });
  protected readonly repository = computed<Repository | RepositoryConsumerView | undefined>(() => valueOf(this.loaded)?.value);
  protected readonly failure = computed(() => {
    const e = this.loaded.error();
    return e ? asApiError(e) : null;
  });
  protected readonly descriptor = computed<FormatDescriptor | undefined>(() =>
    (valueOf(this.formats) ?? []).find((f) => f.name === this.repository()?.format),
  );
  protected readonly levelText = levelName;
  protected readonly classIcon = classIcon;
  protected readonly formatName = computed(() => this.descriptor()?.displayName ?? this.repository()?.format ?? '');
  protected readonly isGeneric = computed(() => this.repository()?.format === 'generic');
  protected readonly scanned = computed(() => this.descriptor()?.vulnerabilityScanning ?? false);
  protected readonly shape = computed(() => contentShapeOf(this.repository()?.format ?? '', this.repository()?.class ?? ''));
  protected readonly classText = computed(() => classLabel(this.repository()?.class ?? 'local'));
  /** The settings exist for whoever can read the configuration: the capability, or an Owner of the project. */
  protected readonly canConfigure = computed(
    () => this.session.has('repositories:read-config') || this.session.canWriteRepositories(this.project()),
  );
  protected readonly canReadFindings = computed(() => this.session.state() !== 'anonymous');
  /** What the header says about the repository: its description, or what it is. */
  protected readonly sentence = computed(() => {
    const r = this.repository();
    if (!r) return '';
    if (r.description) return r.description;
    switch (r.class) {
      case 'remote':
        return $localize`:@@repo.content.sentence.remote:A cache of another registry. It lists what clients have fetched through it.`;
      case 'virtual':
        return $localize`:@@repo.content.sentence.virtual:Combines other repositories behind one address. It lists what they resolve to.`;
      default:
        return $localize`:@@repo.content.sentence.local2:Holds what is published to it.`;
    }
  });
  /** The members a virtual repository resolves from, in order; only the full document has them. */
  protected readonly members = computed<readonly string[]>(() => {
    const r = this.repository();
    return r && !isConsumerView(r) && r.class === 'virtual' ? (r.virtual?.members ?? []) : [];
  });
  /** The address of a remote's upstream; only the full document has it. */
  protected readonly upstream = computed<string | null>(() => {
    const r = this.repository();
    return r && !isConsumerView(r) && r.class === 'remote' ? (r.remote?.url ?? null) : null;
  });
  protected readonly disabled = computed(() => {
    const r = this.repository();
    return r !== undefined && !isConsumerView(r) && (r.disabled ?? false);
  });

  /**
   * The repository's addresses: the consumer view carries them; the full document does not, so the address is built from the
   * server's base URL and the format, as the settings page does.
   */
  protected readonly addresses = computed<readonly string[]>(() => {
    const r = this.repository();
    if (!r) return [];
    if (isConsumerView(r)) return r.addresses;
    const a = clientAddress(valueOf(this.baseUrl), this.descriptor(), this.ref());
    return a ? [a] : [];
  });
  /**
   * The client address, the first of the repository's addresses, is Set up client's (R-UI-169). The API lists the address of the
   * format's protocol first and the raw file address after it, once, where the two differ (R-REPO-002, `ConsumerView` of
   * internal/repository/document.go), so a further address is the one for downloads by path; Details shows it with that label, so
   * that no field stands without a name (Q-UI-320). Set up client does not show it, so it stays here (R-UI-169).
   */
  protected readonly otherAddresses = computed(() =>
    // A system repository answers every download by path 404: no address of it is offered (Q-UI-898).
    (this.system() ? [] : this.addresses())
      .slice(1)
      .map((value) => {
        const label = $localize`:@@repo.content.files-address:Address for downloads by path (curl, wget)`;
        return { value, label, copyLabel: $localize`:@@repo.content.copy-files-address:Copy address for downloads by path` };
      }),
  );
  /**
   * The address for downloads by path, the raw files address of R-REPO-002: the last of the server's addresses in the consumer
   * view; for the full document the generic format's address template of the catalog, which is the raw files family's (R-API-025).
   */
  protected readonly filesAddress = computed(() => {
    const r = this.repository();
    if (!r) return '';
    if (isConsumerView(r)) return r.addresses.at(-1) ?? '';
    return clientAddress(valueOf(this.baseUrl), (valueOf(this.formats) ?? []).find((f) => f.name === 'generic'), this.ref());
  });
  protected readonly actionsLabel = $localize`:@@repo.content.more:More actions`;
  protected readonly detailsHeading = $localize`:@@repo.content.details:Details`;
  protected readonly detailsText = $localize`:@@repo.content.details-text2:Further addresses, and where this repository fetches from or what it resolves from.`;

  protected readonly filter = computed(() => this.q() ?? '');
  protected readonly order = computed<ItemSort>(() => {
    const s = this.sort();
    return (SORTS as readonly string[]).includes(s ?? '') ? (s as ItemSort) : 'name';
  });

  // ---- The files (R-UI-010, Q-UI-510, Q-UI-511) ----

  /** The page shows files: a generic repository always, a package format below `files` (Q-UI-511). */
  protected readonly showsFiles = computed(() => this.filePath() !== undefined || this.isGeneric());
  /** The folder or file of the page; '' is the root of the repository. */
  protected readonly path = computed(() => (this.filePath() ?? '').split('/').filter((s) => s !== '').join('/'));
  /** **Files** in the header's menu: every package format; a generic repository's page is its files (Q-UI-511). */
  protected readonly offersFiles = computed(() => {
    const r = this.repository();
    return !!r && r.format !== 'generic';
  });
  /** The item at the path, once: it says whether the page is a folder or a file. */
  protected readonly itemRead = resource<Item | undefined, { repo: string; path: string } | null>({
    params: () => (this.showsFiles() && this.path() !== '' ? { repo: this.ref(), path: this.path() } : null),
    loader: async ({ params }) => (params ? this.files.item(params.repo, params.path) : undefined),
  });
  protected readonly item = computed(() => valueOf(this.itemRead) ?? null);
  protected readonly itemFailure = computed(() => {
    const e = this.itemRead.error();
    return e ? asApiError(e) : null;
  });
  /** What the page shows below the header: the packages, a folder, a file, or nothing yet. */
  protected readonly view = computed<'items' | 'folder' | 'file' | 'loading'>(() => {
    if (!this.showsFiles()) return 'items';
    if (this.path() === '') return 'folder';
    const item = this.item();
    if (!item || item.path !== this.path()) return 'loading';
    return item.kind === 'file' ? 'file' : 'folder';
  });
  /** The folders of the path as links, the last one the page itself (Q-UI-510). */
  protected readonly trail = computed(() => {
    const parts = this.path().split('/').filter((s) => s !== '');
    return parts.map((name, i) => ({ name, commands: filesCommands(this.ref(), parts.slice(0, i + 1)) }));
  });
  /** The folders between Files and the page's own segment: they collapse into one menu where the path is narrow (Q-UI-521). */
  protected readonly between = computed(() => this.trail().slice(0, -1));
  /** The page's own segment: the folder or the file it shows. */
  protected readonly here = computed(() => this.trail().at(-1) ?? null);
  protected readonly betweenLabel = $localize`:@@files.trail.between:Show the folders in between`;
  /** The root of the files: the repository's own address for a generic repository, `files` for a package format. */
  protected readonly filesRoot = computed(() =>
    this.isGeneric() ? ['/projects', this.project(), 'repositories', this.key()] : filesCommands(this.ref()),
  );

  /**
   * What the caller may do at the path (R-UI-005): one batched check, cached by the session for 60 seconds. The list of a package
   * format asks it at the repository's root, for the words of its empty state (`fill`).
   */
  protected readonly allowed = resource({
    params: () => (this.repository() ? { repo: this.ref(), path: this.showsFiles() ? this.path() : '' } : null),
    loader: async ({ params }): Promise<FilePermissions> => {
      if (!params || this.session.state() !== 'authenticated') return NO_FILE_PERMISSIONS;
      const at = { repository: params.repo, ...(params.path ? { path: params.path } : {}) };
      const [deploy, overwrite, del] = await this.session.check([
        { ...at, action: 'deploy' },
        { ...at, action: 'overwrite' },
        { ...at, action: 'delete' },
      ]);
      return { deploy: !!deploy, overwrite: !!overwrite, delete: !!del };
    },
  });
  /**
   * The operations offered (R-UI-011, Q-UI-512): uploads and new folders in a local repository of a format served below the
   * files path (R-API-025) for a holder of `deploy`; deletes for a holder of `delete`, never in a virtual repository (it holds no
   * items) and never of the root. A disabled repository offers none of them; a system repository no upload, which the server
   * refuses there (R-REPO-075), and the deletes the management API admits.
   */
  protected readonly permissions = computed<FilePermissions>(() => {
    const r = this.repository();
    const can = valueOf(this.allowed) ?? NO_FILE_PERMISSIONS;
    if (!r || this.disabled()) return NO_FILE_PERMISSIONS;
    const uploads = r.class === 'local' && isFilesAddress(this.descriptor()?.protocolBasePath ?? '') && !this.system();
    return {
      deploy: can.deploy && uploads,
      overwrite: can.overwrite && uploads,
      delete: can.delete && r.class !== 'virtual' && this.path() !== '',
    };
  });

  /**
   * What the empty state says fills the repository (R-UI-144, Q-UI-555): **Set up client** stands once, in the header, and the
   * empty state says in a sentence what to do and who does it. A caller who may deploy here is told to publish; anyone else who
   * does; a remote repository fills by every client's fetches; a disabled one says only that it is empty.
   */
  protected readonly fill = computed<'publisher' | 'other' | 'none'>(() => {
    // A system repository is filled by Quader's own work, not by a client the header could set up (Q-UI-896).
    if (this.disabled() || this.system()) return 'none';
    return valueOf(this.allowed)?.deploy ? 'publisher' : 'other';
  });

  /** A delete that runs as a job (R-UI-042), and the folder whose list reloads when it has ended. */
  protected readonly deletion = signal<{ readonly job: JobReference; readonly path: string } | null>(null);
  protected readonly actionFailure = signal<ApiError | null>(null);
  /** Counts the changes the folder list did not make itself (a delete job that ended): the list reloads on each. */
  protected readonly revision = signal(0);
  protected readonly deleteProgress = $localize`:@@files.delete.progress:Deleting items`;
  protected readonly copyPathLabel = $localize`:@@files.copy-path:Copy path`;

  constructor() {
    // The visit counts once the server answered with the repository; one in deletion, and one this page does not find, is no
    // place to go back to (R-UI-096, R-UI-167).
    effect(() => {
      const project = this.project();
      const key = this.key();
      const found = this.repository();
      const missing = this.failure()?.status === 404;
      untracked(() => {
        if (missing) this.frequent.forgetRepository(project, key);
        else if (found && (isConsumerView(found) || found.lifecycle !== 'deleting')) this.frequent.visitRepository(project, key);
      });
    });
    effect(() => {
      this.path();
      untracked(() => this.actionFailure.set(null));
    });
  }

  protected settingsLink(): string[] {
    return ['/projects', this.project(), 'repositories', this.key(), 'settings'];
  }

  protected setFilter(q: string): void {
    this.navigate({ q: q || null });
  }

  protected setSort(sort: ItemSort): void {
    this.navigate({ sort: sort === 'name' ? null : sort });
  }

  private navigate(change: Readonly<Record<string, string | null>>): void {
    void this.router.navigate([], { queryParams: change, queryParamsHandling: 'merge', replaceUrl: true });
  }

  /**
   * Deletes the folder or the file of the page after a confirmation that names it (R-UI-011): a folder says how many items it
   * holds, and above 100 the folder's name is typed. The page then shows the folder above; a delete that runs as a job shows its
   * progress there, and the list reloads when it has ended.
   */
  protected async remove(): Promise<void> {
    const repo = this.ref();
    const path = this.path();
    const item = this.item();
    if (!item || path === '') return;
    this.actionFailure.set(null);
    const lines: string[] = [];
    let typeToConfirm: string | undefined;
    if (item.kind === 'folder') {
      try {
        const { count, capped } = await this.files.countBelow(repo, path, TYPE_TO_CONFIRM_ABOVE);
        lines.push(
          capped
            ? $localize`:@@files.delete.folder-many:The folder holds more than ${TYPE_TO_CONFIRM_ABOVE}:limit: items. They are deleted for good.`
            : $localize`:@@files.delete.folder:The folder holds ${count}:count: items. They are deleted for good.`,
        );
        if (capped) typeToConfirm = nameOf(path);
      } catch (e) {
        this.actionFailure.set(asApiError(e));
        return;
      }
    } else {
      lines.push($localize`:@@files.delete.file:The file is deleted for good.`);
    }
    const answer = await this.confirm.ask({
      title: $localize`:@@files.delete.title:Delete ${path}:path:?`,
      lines,
      confirmLabel: $localize`:@@files.delete.confirm:Delete`,
      typeToConfirm,
    });
    if (answer === null) return;
    try {
      const outcome = await this.files.deleteItem(repo, path, newIdempotencyKey());
      const parent = parentOf(path);
      if (!outcome.done) this.deletion.set({ job: outcome.job, path });
      else this.notifier.status($localize`:@@files.deleted:${path}:path: deleted.`);
      await this.router.navigate(this.folderCommands(parent));
    } catch (e) {
      this.actionFailure.set(asApiError(e));
    }
  }

  protected jobEnded(state: string): void {
    const d = this.deletion();
    if (!d) return;
    if (state === 'succeeded') this.notifier.status($localize`:@@files.deleted:${d.path}:path: deleted.`);
    else this.notifier.alert($localize`:@@files.delete.failed:The deletion failed. Some items may be gone.`);
    this.deletion.set(null);
    this.revision.update((n) => n + 1);
  }

  /** The address of a folder of this repository: the root of the files, or the folder below `files`. */
  private folderCommands(path: string): string[] {
    return path === '' ? this.filesRoot() : filesCommands(this.ref(), path.split('/'));
  }
}
