import { Component, computed, inject, input, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { FormatCatalog } from '../../app/core/format-names';
import { setupCommands } from '../../app/core/project-links';
import { repositoryDescription, wordedByProduct } from '../../app/core/upstream-project';
import { classIcon, classLabel, classTip, shownClass } from '../../app/features/repositories/repository-class';
import { FormatIconView } from '../../app/shared/format-icon';
import { valueOf } from '../../app/shared/paged-resource';
import { PageHeader } from '../../app/shared/ui/page-header';
import { Tooltip } from '../../app/shared/ui/tooltip';
import { BastionProjects } from '../data/projects';
import { PublicRepository } from '../data/repositories';
import { BastionStateStore } from '../data/state';
import { BastionTrail, repositoryTrail } from '../data/trail';

/**
 * The head of a repository's page at the Bastion (R-UI-273, W-40's D5), the main installation's head for a visitor: its key, its
 * description (the product's words for a repository it declares, R-UI-272), the badges of its format and its class, and **Set up
 * client**, none for a system repository that answers no client. No tabs, no Details, no upstream: a visitor's page has no act and names
 * no upstream. While the repositories are hidden (R-UI-274) only a generic repository's files have a head, its key with the format and
 * neither class nor description, which the Bastion's answer then leaves out. It binds the way to the page: Projects › the project ›
 * Repositories › the key, without the step Repositories while hidden; below the files' root the key leads back to it (`here` false).
 */
@Component({
  selector: 'quader-bastion-repository-head',
  imports: [FormatIconView, PageHeader, RouterLink, Tooltip],
  template: `
    <quader-page-header [heading]="key()" [name]="true" [description]="sentence()" [descriptionAsWritten]="asWritten()">
      <div meta class="header-facts">
        @if (repository(); as r) {
          <span class="badge format-badge" translate="no"><quader-format-icon [format]="r.format" />{{ formatName() }}</span>
          @if (kind(); as k) {
            <span class="badge class-badge" [quaderTooltip]="classTip(k)" [attr.tabindex]="classTip(k) ? 0 : null"
              ><span class="material-icons" aria-hidden="true">{{ classIcon(k) }}</span>{{ classLabel(k) }}</span
            >
          }
        }
      </div>
      <ng-container actions>
        @if (setupLink(); as setup) {
          <a class="btn btn-default" [routerLink]="setup" i18n="@@repo.content.tab.setup">Set up client</a>
        }
      </ng-container>
    </quader-page-header>
  `,
  styles: `
    :host {
      display: block;
    }
  `,
})
export class BastionRepositoryHead {
  private readonly catalog = inject(FormatCatalog);
  private readonly projects = inject(BastionProjects);
  private readonly state = inject(BastionStateStore);

  readonly project = input.required<string>();
  readonly key = input.required<string>();
  /** The repository as the Bastion answers it; null until it is read. */
  readonly repository = input<PublicRepository | null>(null);
  /** The head is the page's own place on the way (the repository's page, the files' root); false below it. */
  readonly here = input(true);
  /** The sentence where the repository has no description, or none is shown (a generic repository's files say what they are). */
  readonly fallback = input('');

  private readonly formats = resource({ loader: () => this.catalog.formats() });
  private readonly named = resource({ params: () => this.project(), loader: ({ params }) => this.projects.named(params) });
  protected readonly classIcon = classIcon;
  protected readonly classLabel = classLabel;
  protected readonly classTip = classTip;

  protected readonly formatName = computed(() => {
    const format = this.repository()?.format ?? '';
    return (valueOf(this.formats) ?? []).find((f) => f.name === format)?.displayName ?? format;
  });
  /** The class the head shows: System for a system-only format's (Q-UI-1203); none while hidden, which the answer leaves out. */
  protected readonly kind = computed(() => {
    const r = this.repository();
    return r?.class ? shownClass({ class: r.class, format: r.format }, valueOf(this.formats)) : null;
  });
  private readonly described = computed(() => repositoryDescription(this.project(), this.key(), this.repository()?.description ?? ''));
  protected readonly sentence = computed(() => this.described() || this.fallback());
  /** A description a person wrote is left to the browser as written; the product's words and the fallback are the reader's language. */
  protected readonly asWritten = computed(() => this.described() !== '' && !wordedByProduct(this.project(), this.key()));
  protected readonly setupLink = computed(() => (this.repository() ? setupCommands(`${this.project()}/${this.key()}`, true) : null));

  constructor() {
    inject(BastionTrail).bind(() =>
      repositoryTrail(this.project(), valueOf(this.named)?.displayName ?? this.project(), this.key(), this.state.repositoriesShown(), this.here()),
    );
  }
}
