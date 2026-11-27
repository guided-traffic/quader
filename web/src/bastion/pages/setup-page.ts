import { Component, computed, inject, input, resource } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { ClientSetupReads } from '../../app/api/package-reads';
import type { ClientSnippet } from '../../app/api/packages-api';
import { FormatCatalog } from '../../app/core/format-names';
import { classIcon, classLabel, classTip, shownClass } from '../../app/features/repositories/repository-class';
import { clientLabel } from '../../app/features/setup/clients';
import { SetupFlowArt } from '../../app/features/setup/setup-flow-art';
import { BASTION_MARK } from '../../app/shared/brand/mark-geometry';
import { ErrorNotice } from '../../app/shared/error-notice';
import { formatAccent } from '../../app/shared/format-accent';
import { FormatIconView } from '../../app/shared/format-icon';
import { failureOf, valueOf } from '../../app/shared/paged-resource';
import { Loading } from '../../app/shared/ui/loading';
import { PageHeader } from '../../app/shared/ui/page-header';
import { Tooltip } from '../../app/shared/ui/tooltip';
import { SetupSteps } from '../../app/views/setup/setup-steps';
import { BastionProjects } from '../data/projects';
import { BastionRepositories } from '../data/repositories';
import { BastionStateStore } from '../data/state';
import { BastionTrail, TrailStep, projectTrail, repositoryTrail } from '../data/trail';

/**
 * **Set up client** of a public repository at the Bastion (bastion.md R-BAS-129, api-and-ui.md R-UI-120 at the Bastion, Q-UI-1468): the
 * main installation's page without what a visitor cannot do. The address is the main installation's (`/projects/<project>/repositories/
 * <key>/setup?client=<client>`), the target of **Set up client** in a panel of commands and on a repository's page. The snippets are the
 * catalogue's set-up document of the repository and the client, which the main installation rendered for this Bastion's address without a
 * credential and without publish lines (R-BAS-104): Configure, Install and, where the installation signs, Verify, on the rail of the
 * shared view (views/setup/setup-steps.ts). No token, no **More options**, no Bastion to choose and no guide: the guide describes the main
 * installation, and nothing here names it (R-BAS-120). The picture of the flow ends at the Bastion's tower, the mark a visitor sees here
 * (Q-UI-1617). The repository's format and class are its single answer (`repositories/<project>/<key>`, bastion.md R-BAS-132); one the
 * catalogue does not show is not found. While the repositories are hidden (R-UI-274) the page names no repository but in its address
 * and its snippets, which a client needs: no key, no class, and the way ends at the project.
 */
@Component({
  selector: 'quader-bastion-setup',
  imports: [ErrorNotice, FormatIconView, Loading, PageHeader, RouterLink, SetupFlowArt, SetupSteps, Tooltip],
  host: { '[style.--quader-art-accent]': 'accent()' },
  template: `
    <quader-page-header [heading]="heading" [addresses]="addresses()">
      <div meta class="context header-facts">
        @if (shown()) {
          <span class="repo" translate="no"><a [routerLink]="['/projects', project(), 'repositories', key()]">{{ ref() }}</a></span>
        }
        @if (descriptor(); as d) {
          <span class="badge format-badge" translate="no"><quader-format-icon [format]="d.name" />{{ d.displayName ?? d.name }}</span>
        }
        @if (kind(); as k) {
          <span class="badge class-badge" [quaderTooltip]="classTip(k)" [attr.tabindex]="classTip(k) ? 0 : null"
            ><span class="material-icons" aria-hidden="true">{{ classIcon(k) }}</span>{{ classLabel(k) }}</span
          >
        }
      </div>
      <quader-setup-flow-art art [format]="repository()?.format ?? null" [publish]="false" [token]="false" [mark]="bastionMark" />
    </quader-page-header>

    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="failure.retry()" />
    } @else if (read.hasValue() && !repository()) {
      <p data-testid="setup-unknown" i18n="@@bastion.setup.unknown">This address offers no repository of that name.</p>
    } @else {
      @if (clients().length > 1) {
        <p class="client-choice">
          <label class="field-label" for="setup-client" i18n="@@setup.client">Client</label>
          <select id="setup-client" class="select-input" [value]="activeClient()" (change)="pickClient($any($event.target).value)">
            @for (c of clients(); track c.id) {
              <option [value]="c.id" [selected]="c.id === activeClient()" translate="no">{{ clientName(c.id, c.displayName) }}</option>
            }
          </select>
        </p>
      }
      <p class="credential-line" i18n="@@bastion.setup.no-credentials">Everything here is public: the snippets need no credentials.</p>
      @if (setup(); as s) {
        <quader-setup-steps [snippets]="s.snippets" [anonymous]="true" [note]="hostNote" />
      } @else {
        <quader-loading size="page" i18n="@@setup.loading.client">Loading the client setup.</quader-loading>
      }
    }
  `,
  styles: `
    :host {
      display: block;
      max-inline-size: var(--quader-settings-measure);
    }
    .context .repo {
      display: inline-flex;
      align-items: flex-start;
      gap: var(--quader-space-1);
      overflow-wrap: anywhere;
    }
    .client-choice {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-2);
      margin-block: 0 var(--quader-space-3);

      .field-label {
        margin: 0;
      }

      .select-input {
        inline-size: auto;
        min-inline-size: 12rem;
      }
    }
    .credential-line {
      max-inline-size: 70ch;
      margin-block: 0 var(--quader-space-3);
    }
  `,
})
export class SetupPage {
  private readonly repositories = inject(BastionRepositories);
  private readonly reads = inject(ClientSetupReads);
  private readonly catalog = inject(FormatCatalog);
  private readonly router = inject(Router);
  private readonly projects = inject(BastionProjects);
  private readonly state = inject(BastionStateStore);

  /** Route parameters and the query's client, bound by the router's component input binding. */
  readonly project = input.required<string>();
  readonly key = input.required<string>();
  readonly client = input<string | undefined>();

  protected readonly heading = $localize`:@@setup.heading:Set up client`;
  protected readonly bastionMark = BASTION_MARK;
  protected readonly classIcon = classIcon;
  protected readonly classLabel = classLabel;
  protected readonly classTip = classTip;
  protected readonly shown = this.state.repositoriesShown;
  /** npm's line that makes every package come from this address, which the main installation's page says for a Bastion (R-BAS-007). */
  protected readonly hostNote = (sn: ClientSnippet): string | null =>
    sn.content.includes('replace-registry-host=always')
      ? $localize`:@@setup.bastion.replace-host:\`replace-registry-host=always\` makes npm fetch every package from this address, also when a lockfile written against another host names that host.`
      : null;

  protected readonly ref = computed(() => `${this.project()}/${this.key()}`);
  protected readonly read = resource({
    params: () => ({ project: this.project(), key: this.key() }),
    loader: ({ params }) => this.repositories.one(params.project, params.key),
  });
  protected readonly repository = computed(() => valueOf(this.read) ?? null);
  private readonly formats = resource({ loader: () => this.catalog.formats() });
  protected readonly descriptor = computed(() => (valueOf(this.formats) ?? []).find((f) => f.name === this.repository()?.format) ?? null);
  /** The class the head shows: System for a system-only format's (Q-UI-1203); none while hidden, which the answer leaves out. */
  protected readonly kind = computed(() => {
    const r = this.repository();
    return r?.class ? shownClass({ class: r.class, format: r.format }, valueOf(this.formats)) : null;
  });
  protected readonly accent = computed(() => formatAccent(this.repository()?.format ?? null));
  /** The format's clients; a format with one asks nothing (Q-UI-482). */
  protected readonly clients = computed(() => this.descriptor()?.clients ?? []);
  /** A client by the UI's name, in the reader's words (R-UI-272). */
  protected clientName(id: string, served: string): string {
    return clientLabel(this.repository()?.format ?? '', id, served);
  }
  protected readonly activeClient = computed<string | null>(() => {
    const ids = this.clients().map((c) => c.id);
    const given = this.client();
    return given && ids.includes(given) ? given : (ids[0] ?? null);
  });

  private readonly setupAnswer = resource({
    params: () => {
      const client = this.activeClient();
      return this.repository() && client ? { repository: this.ref(), client } : undefined;
    },
    loader: ({ params }) => this.reads.get(params.repository, { client: params.client, credentials: 'none' }),
  });
  protected readonly setup = computed(() => valueOf(this.setupAnswer));
  protected readonly failure = failureOf([this.read, this.setupAnswer], { also: [this.formats] });
  /** The repository's client address at this Bastion: the set-up's, which the main installation rendered on the Bastion's own. */
  protected readonly addresses = computed<readonly string[]>(() => {
    const url = this.setup()?.registryUrl;
    return url ? [url] : [];
  });

  constructor() {
    const named = resource({ params: () => this.project(), loader: ({ params }) => this.projects.named(params) });
    // The way through the repository while the repositories are shown, from the project while hidden (R-UI-273, R-UI-274).
    inject(BastionTrail).bind((): TrailStep[] => {
      const name = valueOf(named)?.displayName ?? this.project();
      const above = this.shown() ? repositoryTrail(this.project(), name, this.key(), true) : projectTrail(this.project(), name);
      return [...above, { label: this.heading, commands: null, literal: false }];
    });
  }

  protected pickClient(client: string): void {
    void this.router.navigate([], { queryParams: { client }, queryParamsHandling: 'merge', replaceUrl: true });
  }
}
