import { Component, computed, inject, input, resource, signal } from '@angular/core';
import { Router } from '@angular/router';
import { FormatDescriptor, RepositoriesApi } from '../../api/repositories-api';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatTiles } from '../../shared/ui/format-tiles';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { PageHeader } from '../../shared/ui/page-header';

/** The installed formats that match a search, by display name or name, case-insensitive (Q-UI-115). */
export function matchingFormats(formats: readonly FormatDescriptor[], query: string): readonly FormatDescriptor[] {
  const q = query.trim().toLowerCase();
  return formats.filter((f) => q === '' || (f.displayName ?? f.name).toLowerCase().includes(q) || f.name.includes(q));
}

/**
 * The first page of a new repository (R-UI-080, Q-UI-340): the installed formats of `GET /api/v1/formats` as tiles under a search
 * field (the shared tiles of R-UI-127), each tile a link to the form of its format (`new/{format}`). Enter in
 * the search field opens the one format that is left. The admin area passes its `project` query parameter on. The creation form
 * shows this page in its place for a format the installation does not have (`unknownFormat`).
 */
@Component({
  selector: 'quader-repository-format-chooser',
  imports: [ErrorNotice, FormatTiles, PageHeader],
  template: `
    <quader-page-header
      i18n-heading="@@repo.new.heading"
      heading="New repository"
      i18n-description="@@repo.new.description"
      description="Choose the package format. It decides what the repository can do."
    />
    @if (unknownFormat(); as name) {
      <p class="banner-warning" role="note">
        <span class="material-icons inline warn" aria-hidden="true">warning</span>
        <span i18n="@@repo.new.unknown-format">This installation has no format <span translate="no">{{ name }}</span>.</span>
      </p>
    }
    @if (loadFailure(); as f) {
      <quader-error-notice [error]="f" (retry)="formats.reload()" />
    } @else if (formats.isLoading()) {
      <p role="status" i18n="@@repo.loading">Loading.</p>
    } @else {
      <div class="field search">
        <label class="field-label" for="f-format-search" i18n="@@repo.new.search">Search by package type</label>
        <input
          id="f-format-search"
          class="text-input field-w-name"
          type="search"
          autocomplete="off"
          spellcheck="false"
          aria-describedby="format-count"
          [value]="query()"
          (input)="query.set($any($event.target).value)"
          (keydown.enter)="openOnlyMatch($event)"
        />
      </div>
      <p class="hint" role="status" id="format-count">
        @if (all().length === 0) {
          <ng-container i18n="@@repo.new.no-formats">This installation offers no format to create repositories of.</ng-container>
        } @else if (shown().length === 0) {
          <ng-container i18n="@@repo.new.none">No format matches the search.</ng-container>
        } @else {
          <ng-container i18n="@@repo.new.count">{{ shown().length }} of {{ all().length }} formats</ng-container>
        }
      </p>
      <quader-format-tiles [formats]="shown()" [linkOf]="linkFn" [queryParams]="query$()" [label]="tilesLabel" />
    }
  `,
  styles: `
    .search {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-1);
    }
  `,
})
export class RepositoryFormatChooser {
  private readonly api = inject(RepositoriesApi);
  private readonly router = inject(Router);

  /**
   * The project of the page: the route parameter inside a project, the query parameter `project` in the admin area, where it is
   * the project the form starts with and is carried to the form.
   */
  readonly project = input<string | undefined>();
  /** The format named in the address that this installation does not have (R-UI-080). */
  readonly unknownFormat = input<string | null>(null);

  protected readonly formats = resource({ loader: () => this.api.formats() });
  protected readonly query = signal('');
  protected readonly all = computed(() => valueOf(this.formats) ?? []);
  protected readonly shown = computed(() => matchingFormats(this.all(), this.query()));
  protected readonly loadFailure = computed(() => {
    const e = this.formats.error();
    return e ? asApiError(e) : null;
  });
  /** The admin area keeps its project choice from the list to the form; inside a project the path names it. */
  protected readonly query$ = computed((): Record<string, string> => {
    const project = this.project();
    return project && this.chooserPath().startsWith('/admin/') ? { project } : {};
  });

  /** `…/repositories/new` of the page, also when the creation form shows the chooser in its place at `…/new/{format}`. */
  private readonly chooserPath = computed(() => {
    const path = this.router.url.split(/[?#]/)[0] ?? '';
    const marker = '/repositories/new';
    const at = path.indexOf(marker);
    return at < 0 ? '/admin/repositories/new' : path.slice(0, at + marker.length);
  });

  protected linkOf(f: FormatDescriptor): string {
    return `${this.chooserPath()}/${f.name}`;
  }
  protected readonly linkFn = (f: FormatDescriptor): string => this.linkOf(f);
  protected readonly tilesLabel = $localize`:@@repo.new.formats:Package formats`;

  /** Enter in the search field opens the one format that is left. */
  protected openOnlyMatch(event: Event): void {
    const only = this.shown();
    if (only.length !== 1) return;
    event.preventDefault();
    void this.router.navigate([this.linkOf(only[0]!)], { queryParams: this.query$() });
  }
}
