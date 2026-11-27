import { Component, computed, inject, input, resource } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { ClientSetupReads } from '../../app/api/package-reads';
import type { ClientSnippet } from '../../app/api/packages-api';
import { FormatCatalog } from '../../app/core/format-names';
import { classIcon, classLabel, classTip } from '../../app/features/repositories/repository-class';
import { clientLabel } from '../../app/features/setup/clients';
import { SetupFlowArt } from '../../app/features/setup/setup-flow-art';
import { ErrorNotice } from '../../app/shared/error-notice';
import { formatAccent } from '../../app/shared/format-accent';
import { FormatIconView } from '../../app/shared/format-icon';
import { failureOf, valueOf } from '../../app/shared/paged-resource';
import { Loading } from '../../app/shared/ui/loading';
import { PageHeader } from '../../app/shared/ui/page-header';
import { Tooltip } from '../../app/shared/ui/tooltip';
import { SetupSteps } from '../../app/views/setup/setup-steps';
import { BastionData } from '../data/http';

/** A public repository as `/ui/data/v1/repositories` names it (bastion.md R-BAS-129). */
interface PublicRepository {
  readonly name: string;
  readonly format: string;
  readonly class: string;
}

/**
 * **Set up client** of a public repository at the Bastion (bastion.md R-BAS-129, api-and-ui.md R-UI-120 at the Bastion, Q-UI-1468): the
 * main installation's page without what a visitor cannot do. The address is the main installation's (`/projects/<project>/repositories/
 * <key>/setup?client=<client>`), the target of **Set up client** in a panel of commands. The snippets are the catalogue's set-up
 * document of the repository and the client, which the main installation rendered for this Bastion's address without a credential and
 * without publish lines (R-BAS-104): Configure, Install and, where the installation signs, Verify, on the rail of the shared view
 * (views/setup/setup-steps.ts). No token, no **More options**, no Bastion to choose and no guide: the guide describes the main
 * installation, and nothing here names it (R-BAS-120). The repository's format and class come from the public set; one the catalogue does
 * not show is not found.
 */
@Component({
  selector: 'quader-bastion-setup',
  imports: [ErrorNotice, FormatIconView, Loading, PageHeader, RouterLink, SetupFlowArt, SetupSteps, Tooltip],
  host: { '[style.--quader-art-accent]': 'accent()' },
  template: `
    <quader-page-header [heading]="heading" [addresses]="addresses()">
      <div meta class="context header-facts">
        <span class="repo" translate="no"><a [routerLink]="repositoryLink()">{{ ref() }}</a></span>
        @if (descriptor(); as d) {
          <span class="badge format-badge" translate="no"><quader-format-icon [format]="d.name" />{{ d.displayName ?? d.name }}</span>
        }
        @if (repository(); as r) {
          <span class="badge class-badge" [quaderTooltip]="classTip(r.class)" [attr.tabindex]="classTip(r.class) ? 0 : null"
            ><span class="material-icons" aria-hidden="true">{{ classIcon(r.class) }}</span>{{ classLabel(r.class) }}</span
          >
        }
      </div>
      <quader-setup-flow-art art [format]="repository()?.format ?? null" [publish]="false" [token]="false" />
    </quader-page-header>

    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="failure.retry()" />
    } @else if (repositories.hasValue() && !repository()) {
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
  private readonly data = inject(BastionData);
  private readonly reads = inject(ClientSetupReads);
  private readonly catalog = inject(FormatCatalog);
  private readonly router = inject(Router);

  /** Route parameters and the query's client, bound by the router's component input binding. */
  readonly project = input.required<string>();
  readonly key = input.required<string>();
  readonly client = input<string | undefined>();

  protected readonly heading = $localize`:@@setup.heading:Set up client`;
  protected readonly classIcon = classIcon;
  protected readonly classLabel = classLabel;
  protected readonly classTip = classTip;
  /** npm's line that makes every package come from this address, which the main installation's page says for a Bastion (R-BAS-007). */
  protected readonly hostNote = (sn: ClientSnippet): string | null =>
    sn.content.includes('replace-registry-host=always')
      ? $localize`:@@setup.bastion.replace-host:\`replace-registry-host=always\` makes npm fetch every package from this address, also when a lockfile written against another host names that host.`
      : null;

  protected readonly ref = computed(() => `${this.project()}/${this.key()}`);
  protected readonly repositories = resource({
    loader: () => this.data.get<{ readonly items: readonly PublicRepository[] }>('repositories'),
  });
  protected readonly repository = computed(() => valueOf(this.repositories)?.items.find((r) => r.name === this.ref()) ?? null);
  private readonly formats = resource({ loader: () => this.catalog.formats() });
  protected readonly descriptor = computed(() => (valueOf(this.formats) ?? []).find((f) => f.name === this.repository()?.format) ?? null);
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
  protected readonly failure = failureOf([this.repositories, this.setupAnswer], { also: [this.formats] });
  /** The repository's client address at this Bastion: the set-up's, which the main installation rendered on the Bastion's own. */
  protected readonly addresses = computed<readonly string[]>(() => {
    const url = this.setup()?.registryUrl;
    return url ? [url] : [];
  });
  /** A generic repository's files are its page; a repository of packages leads to its project's packages. */
  protected readonly repositoryLink = computed(() =>
    this.descriptor()?.patternSubject === 'path'
      ? ['/projects', this.project(), 'repositories', this.key()]
      : ['/projects', this.project(), 'packages'],
  );

  protected pickClient(client: string): void {
    void this.router.navigate([], { queryParams: { client }, queryParamsHandling: 'merge', replaceUrl: true });
  }
}
