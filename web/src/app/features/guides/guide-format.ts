import { Component, computed, inject, input, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { RepositoriesApi, RepositorySummary, refOf } from '../../api/repositories-api';
import { repositoryCommands, setupCommands } from '../../core/project-links';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { PageHeader } from '../../shared/ui/page-header';
import { VisibilityIcon } from '../../shared/ui/visibility-level';
import { ProjectLink } from '../../shared/ui/project-mark';
import { classIcon, classLabel } from '../repositories/repository-class';

/**
 * The repositories of one format the reader may use (R-UI-124, Q-UI-481): a tile of **Guides** leads here. The rows are built from
 * the parts of every repository list (Q-UI-425, Q-UI-426, Q-UI-430): the visibility icon before the key with the description under
 * it, the project by its mark and its name, the class with its icon, and **Set up client** as the row's action (Q-UI-487), which
 * opens the repository's page (R-UI-120). The virtual repositories come first, the recommended address. A reader without a repository of the format, or one who wants to read before choosing, finds the
 * guide of each class below the list: that page writes `<project>` and `<repository>` into the addresses.
 */
@Component({
  selector: 'quader-guide-format',
  imports: [RouterLink, PageHeader, ErrorNotice, DataTable, VisibilityIcon, ProjectLink],
  template: `
    @if (formatFailure(); as f) {
      <quader-error-notice [error]="f" (retry)="formats.reload()" />
    } @else if (descriptor(); as d) {
      <quader-page-header
        [heading]="heading()"
        i18n-description="@@guides.format.description"
        description="Choose the repository your package manager uses. Set up client shows its configuration and its commands, with the explanation."
      />

      <quader-data-table
        [caption]="caption()"
        [columns]="columns"
        [loaded]="repositories.hasValue()"
        [loading]="repositories.isLoading()"
        [error]="listFailure()"
        [empty]="rows().length === 0"
        [emptyText]="emptyText"
        (retry)="repositories.reload()"
      >
        @for (r of rows(); track refOf(r)) {
          <!-- The parts of the repository list (Q-UI-430): the visibility icon before the key, the description under it, the project
               by its mark and its name, the class with its icon; the format is the page's. -->
          <tr>
            <th scope="row">
              <span class="icon-name">
                <quader-visibility-icon [level]="r.visibility.artifacts" />
                <span class="icon-name-text">
                  <a [routerLink]="repositoryLink(r)" translate="no">{{ r.key }}</a>
                  @if (r.class === 'virtual') {
                    <span class="badge" i18n="@@guides.format.recommended">recommended</span>
                  }
                  @if (r.description) {
                    <span class="secondary">{{ r.description }}</span>
                  }
                </span>
              </span>
            </th>
            <td class="project-cell"><quader-project-link [key]="r.project" /></td>
            <td class="class-cell">
              <span class="class-name"><span class="material-icons" aria-hidden="true">{{ classIcon(r.class) }}</span>{{ classText(r.class) }}</span>
            </td>
            <td class="actions with-setup">
              <!-- The row's action (Q-UI-487): a quiet button, its accessible name ending with the key, as in a project's list. -->
              <a class="btn btn-tertiary row-setup" [routerLink]="setupLink(r)"
                ><ng-container i18n="@@repo.content.tab.setup">Set up client</ng-container
                ><span class="visually-hidden" translate="no">&ngsp;{{ r.key }}</span></a
              >
            </td>
          </tr>
        }
      </quader-data-table>

      <section class="classes" aria-labelledby="guides-classes">
        <h2 id="guides-classes" i18n="@@guides.format.classes">Without a repository</h2>
        <p i18n="@@guides.format.classes-text">The guide of a kind of repository explains it before you choose one. Its addresses show where the project and the repository go.</p>
        <ul>
          @for (c of d.classes; track c) {
            <li>
              <a [routerLink]="['/guides', d.name, c]">{{ classGuideText(c) }}</a>
            </li>
          }
        </ul>
      </section>
    } @else if (formats.hasValue()) {
      <p i18n="@@guides.format.unknown">This installation has no guide for this package type.</p>
    }
  `,
  styles: `
    .badge {
      margin-inline-start: var(--quader-space-2);
    }
    .classes {
      margin-block-start: var(--quader-space-6);

      h2 {
        margin-block: 0 var(--quader-space-2);
        font: var(--quader-type-title-medium);
      }

      p,
      ul {
        max-inline-size: 70ch;
        margin-block: 0 var(--quader-space-2);
      }
    }
  `,
})
export class GuideFormat {
  private readonly api = inject(RepositoriesApi);

  /** The route parameter. */
  readonly format = input.required<string>();

  protected readonly refOf = refOf;
  protected readonly formats = resource({ loader: () => this.api.formats() });
  protected readonly repositories = resource({ loader: () => this.api.listAll() });
  protected readonly descriptor = computed(
    () => (valueOf(this.formats) ?? []).find((f) => f.name === this.format() && f.clients.length > 0) ?? null,
  );
  protected readonly heading = computed(() => {
    const name = this.descriptor()?.displayName ?? this.format();
    return $localize`:@@guides.format.heading:${name}:format: repositories`;
  });
  protected readonly caption = computed(() => this.heading());
  /** The repositories of the format that clients can use: virtual first, then by name. */
  protected readonly rows = computed<readonly RepositorySummary[]>(() =>
    (valueOf(this.repositories) ?? [])
      .filter((r) => r.format === this.format() && !r.disabled)
      .sort((a, b) => Number(b.class === 'virtual') - Number(a.class === 'virtual') || refOf(a).localeCompare(refOf(b))),
  );
  protected readonly formatFailure = computed(() => {
    const e = this.formats.error();
    return e ? asApiError(e) : null;
  });
  protected readonly listFailure = computed(() => {
    const e = this.repositories.error();
    return e ? asApiError(e) : null;
  });
  /** The columns of the repository list (Q-UI-330): the repository takes what the others leave. */
  protected readonly columns: readonly TableColumn[] = [
    { id: 'key', label: $localize`:@@repo.col.repository:Repository`, width: '13rem', prefer: true },
    { id: 'project', label: $localize`:@@repo.col.project:Project`, width: '11rem' },
    { id: 'class', label: $localize`:@@repo.col.class:Class`, width: '5rem' },
    { id: 'actions', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true, width: '9rem' },
  ];
  protected readonly emptyText = $localize`:@@guides.format.empty:You can use no repository of this package type. The guides below explain each kind.`;

  protected repositoryLink(r: RepositorySummary): string[] {
    return repositoryCommands(refOf(r));
  }

  protected setupLink(r: RepositorySummary): string[] {
    return setupCommands(refOf(r));
  }

  protected readonly classText = classLabel;
  protected readonly classIcon = classIcon;

  protected classGuideText(cls: string): string {
    switch (cls) {
      case 'local':
        return $localize`:@@guides.format.class.local:A local repository: what you publish to`;
      case 'remote':
        return $localize`:@@guides.format.class.remote:A remote repository: a cache of another registry`;
      case 'virtual':
        return $localize`:@@guides.format.class.virtual:A virtual repository: several repositories behind one address`;
      default:
        return cls;
    }
  }
}
