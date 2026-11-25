import { Component, computed, inject, input, resource, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { FormatDescriptor, RepositoriesApi, RepositorySummary } from '../../api/repositories-api';
import { creatableFormats } from './repository-class';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatTiles } from '../../shared/ui/format-tiles';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { PageHeader } from '../../shared/ui/page-header';

/** The installed formats that match a search, by display name or name, case-insensitive (Q-UI-115). */
export function matchingFormats(formats: readonly FormatDescriptor[], query: string): readonly FormatDescriptor[] {
  const q = query.trim().toLowerCase();
  return formats.filter((f) => q === '' || (f.displayName ?? f.name).toLowerCase().includes(q) || f.name.includes(q));
}

/**
 * The first page of a new repository (R-UI-080, Q-UI-340): the installed formats of `GET /api/v1/formats` that a person creates
 * repositories of as tiles under a search field (the shared tiles of R-UI-127), each tile a link to the form of its format
 * (`new/{format}`). A system-only format (`systemOnly`, R-CLAMAV-001) is no tile and no part of the count: the installation creates
 * its one repository itself (Q-UI-1200). Enter in the search field opens the one format that is left. A repository is created in its
 * project (Q-UI-1040): the page is the project's, `/projects/{project}/repositories/new`. The creation form shows this page in its
 * place for a format the installation does not have (`unknownFormat`), and for a system-only format with one sentence that says so and
 * leads to its repository where the reader may see one (`systemOnly`, Q-UI-1201).
 */
@Component({
  selector: 'quader-repository-format-chooser',
  imports: [ErrorNotice, FormatTiles, PageHeader, RouterLink],
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
    @if (systemOnly(); as f) {
      <!-- A system-only format at the form's address (Q-UI-1201): no form, one sentence, and its repository where the reader sees it. -->
      <p class="banner-warning" role="note">
        <span class="material-icons inline" aria-hidden="true">info</span>
        <span>
          <ng-container i18n="@@repo.new.system-only"
            >This installation creates the repository of <span translate="no">{{ f.displayName ?? f.name }}</span> itself; it is not
            created here.</ng-container
          >
          @if (existingRepo(); as r) {
            &ngsp;<ng-container i18n="@@repo.new.system-only.existing"
              >It is <a [routerLink]="repositoryLink(r)" translate="no">{{ r.project }}/{{ r.key }}</a>.</ng-container
            >
          }
        </span>
      </p>
    }
    @if (loadFailure(); as f) {
      <quader-error-notice [error]="f" (retry)="loadFailure.retry()" />
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
      <quader-format-tiles [formats]="shown()" [linkOf]="linkFn" [label]="tilesLabel" />
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

  /** The project of the page: the route parameter `:project`, or the creation form's project where it shows the chooser. */
  readonly project = input<string | undefined>();
  /** The format named in the address that this installation does not have (R-UI-080). */
  readonly unknownFormat = input<string | null>(null);
  /** The system-only format named in the address, whose one repository the installation creates itself (Q-UI-1201). */
  readonly systemOnly = input<FormatDescriptor | null>(null);

  protected readonly formats = resource({ loader: () => this.api.formats() });
  /**
   * The repository of the system-only format of the address that the reader may see, found by its format (never by a name): the
   * first of the list's answer, none where the reader sees none or the read fails.
   */
  protected readonly existing = resource({
    params: () => this.systemOnly()?.name,
    loader: async ({ params }): Promise<RepositorySummary | null> => {
      try {
        return (await this.api.list({ format: params })(null, 1)).items[0] ?? null;
      } catch {
        return null;
      }
    },
  });
  protected readonly existingRepo = computed(() => valueOf(this.existing) ?? null);
  protected repositoryLink(r: RepositorySummary): string[] {
    return ['/projects', r.project, 'repositories', r.key];
  }
  protected readonly query = signal('');
  protected readonly all = computed(() => creatableFormats(valueOf(this.formats) ?? []));
  protected readonly shown = computed(() => matchingFormats(this.all(), this.query()));
  /** The formats' failure, kept while they read again (R-UI-216). */
  protected readonly loadFailure = failureOf(this.formats);
  protected linkOf(f: FormatDescriptor): string {
    return `/projects/${encodeURIComponent(this.project() ?? '')}/repositories/new/${f.name}`;
  }
  protected readonly linkFn = (f: FormatDescriptor): string => this.linkOf(f);
  protected readonly tilesLabel = $localize`:@@repo.new.formats:Package formats`;

  /** Enter in the search field opens the one format that is left. */
  protected openOnlyMatch(event: Event): void {
    const only = this.shown();
    if (only.length !== 1) return;
    event.preventDefault();
    void this.router.navigate([this.linkOf(only[0]!)]);
  }
}
