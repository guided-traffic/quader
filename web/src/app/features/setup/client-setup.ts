import { Component, DestroyRef, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { newIdempotencyKey } from '../../api/client';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { Guide, GuidesApi } from '../../api/guides-api';
import { ClientSetupApi, ClientSnippet } from '../../api/packages-api';
import { ApiError } from '../../api/problem';
import { RepositoriesApi, isConsumerView, repoRef } from '../../api/repositories-api';
import { CreateTokenRequest, TokensApi } from '../../api/tokens-api';
import { repositoryCommands } from '../../core/project-links';
import { SessionStore } from '../../core/session-store';
import { CopyButton } from '../../shared/copy-button';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatIconView } from '../../shared/format-icon';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { Disclosure } from '../../shared/ui/disclosure';
import { PageHeader } from '../../shared/ui/page-header';
import { VisibilityIcon } from '../../shared/ui/visibility-level';
import { GuideView } from '../guides/guide-view';
import { classLabel } from '../repositories/repository-class';
import { reportedBastions } from './bastion-targets';
import { snippetExplanation } from './clients';
import { SetupSnippet } from './setup-snippet';
import { checkTokenShape, TokenShape } from './token-shape';

/** How the snippets get their credential (R-UI-122): the variable QUADER_TOKEN, or a token written into them. */
export type CredentialMode = 'env' | 'paste';

/** The literal the server puts into a snippet where the browser inserts the token (R-API-030). */
export const TOKEN_PLACEHOLDER = '{{token}}';

/** Replaces every placeholder in the browser. The replacement is literal: `$&` in a token means nothing. */
export function fillToken(content: string, token: string | null): string {
  return token === null ? content : content.split(TOKEN_PLACEHOLDER).join(token);
}

/** The command that puts a token into the variable without the shell's history seeing it (R-UI-122). */
export const READ_TOKEN_COMMAND = 'read -rs QUADER_TOKEN && export QUADER_TOKEN';

/** The steps of the page, in the order of the job (R-UI-121): the snippets of each are the client setup's tab of that name. */
type Step = 'configure' | 'install' | 'publish';

/**
 * Set up client (R-UI-120 to R-UI-123, ADR-202): the client setup and the guide of a repository as one page. It opens from the
 * repository (`/projects/<project>/repositories/<key>/setup`), so the format, the class and the repository are known and asked
 * for nowhere; the client is one control where the format has several, and none where it has one. The job comes first: the
 * address, the token in one line with **Create a token** in place, then the snippets of Configure, Install and, for who may
 * publish, Publish. The explanation follows them: what a repository of the class is, then the format's sections folded
 * (R-UI-125). The same page without a repository is the guide of a class (`/guides/<format>/<class>`), whose addresses show
 * `<project>` and `<repository>`.
 *
 * Secrets (R-UI-043): the server never fills a token. A pasted token lives in a component signal, is written into the snippets
 * here only, is never put into a service, the URL or browser storage, and is gone when the route is left; it is checked for its
 * prefix, length and CRC-32 locally and sent nowhere. A token created on the page is shown once and kept the same way.
 */
@Component({
  selector: 'quader-client-setup',
  imports: [RouterLink, PageHeader, ErrorNotice, Disclosure, VisibilityIcon, FormatIconView, CopyButton, SetupSnippet, GuideView],
  templateUrl: './client-setup.html',
  styleUrl: './client-setup.scss',
})
export class ClientSetupPage {
  private readonly setupApi = inject(ClientSetupApi);
  private readonly guidesApi = inject(GuidesApi);
  private readonly repositories = inject(RepositoriesApi);
  private readonly tokens = inject(TokensApi);
  private readonly bastionsApi = inject(BastionsApi);
  private readonly router = inject(Router);
  protected readonly session = inject(SessionStore);

  /** The route parameters of a repository's page. */
  readonly project = input<string | undefined>();
  readonly key = input<string | undefined>();
  /** The route parameters of the guide of a class (`/guides/<format>/<class>`). */
  readonly format = input<string | undefined>();
  readonly class = input<string | undefined>();
  /** Query parameters: the client, and a Bastion to install from (R-BAS-007). */
  readonly client = input<string | undefined>();
  readonly bastion = input<string | undefined>();

  protected readonly heading = $localize`:@@setup.heading:Set up client`;
  protected readonly moreHeading = $localize`:@@setup.more:More options`;
  protected readonly placeholder = TOKEN_PLACEHOLDER;
  protected readonly readCommand = READ_TOKEN_COMMAND;
  protected readonly copyTokenLabel = $localize`:@@setup.copy-token:Copy token`;
  protected readonly copyCommandLabel = $localize`:@@setup.copy-command:Copy command`;
  protected readonly explanation = snippetExplanation;

  /** The repository of the page, `<project>/<key>`; null for the guide of a class. */
  protected readonly ref = computed(() => {
    const project = this.project();
    const key = this.key();
    return project && key ? repoRef(project, key) : null;
  });
  protected readonly anonymous = computed(() => this.session.state() === 'anonymous');

  protected readonly formats = resource({ loader: () => this.repositories.formats() });
  protected readonly loaded = resource({
    params: () => this.ref() ?? undefined,
    loader: ({ params }) => this.repositories.get(params),
  });
  protected readonly repository = computed(() => valueOf(this.loaded)?.value);
  protected readonly formatName = computed(() => (this.ref() ? this.repository()?.format : this.format()) ?? null);
  protected readonly descriptor = computed(
    () => (valueOf(this.formats) ?? []).find((f) => f.name === this.formatName() && f.clients.length > 0) ?? null,
  );
  protected readonly activeClass = computed(() => {
    if (this.ref()) return this.repository()?.class ?? null;
    const cls = this.class();
    return cls && (this.descriptor()?.classes as readonly string[] | undefined)?.includes(cls) ? cls : null;
  });
  protected readonly classText = computed(() => classLabel(this.activeClass() ?? ''));
  protected readonly visibility = computed(() => this.repository()?.visibility?.artifacts ?? null);
  protected readonly disabled = computed(() => {
    const r = this.repository();
    return r !== undefined && !isConsumerView(r) && (r.disabled ?? false);
  });
  protected readonly repositoryLink = computed(() => {
    const ref = this.ref();
    return ref ? repositoryCommands(ref) : null;
  });

  /** The format's clients (`GET /api/v1/formats`); a format with one client asks nothing (Q-UI-482). */
  protected readonly clients = computed(() => this.descriptor()?.clients ?? []);
  protected readonly activeClient = computed<string | null>(() => {
    const ids = this.clients().map((c) => c.id);
    const given = this.client();
    return given && ids.includes(given) ? given : (ids[0] ?? null);
  });

  /** The Bastions to install from (R-BAS-007, Q-BAS-96): a repository's page only. */
  protected readonly bastions = resource({
    params: () => (this.ref() ? true : undefined),
    loader: () => reportedBastions(this.bastionsApi, this.session),
  });
  protected readonly bastionOptions = computed<readonly Bastion[]>(() => valueOf(this.bastions) ?? []);
  protected readonly activeBastion = computed<string | null>(() => (this.ref() ? this.bastion()?.trim() || null : null));
  protected readonly showTarget = computed(() => this.bastionOptions().length > 0 || this.activeBastion() !== null);

  protected readonly mode = signal<CredentialMode>('env');
  /** Whether More options is open; a choice inside it keeps it open while the page lives. */
  protected readonly moreOpen = signal(false);
  protected readonly moreHint = computed(() =>
    this.anonymous()
      ? $localize`:@@setup.more.hint-anonymous:Where to install from.`
      : this.showTarget()
        ? $localize`:@@setup.more.hint-target:A token written into the snippets, and where to install from.`
        : $localize`:@@setup.more.hint:A token written into the snippets.`,
  );
  protected readonly offerMore = computed(() => this.ref() !== null && (!this.anonymous() || this.showTarget()));

  protected readonly credentials = computed<'none' | 'env' | 'placeholder'>(() =>
    this.anonymous() ? 'none' : this.mode() === 'paste' ? 'placeholder' : 'env',
  );
  /** The snippets of a repository (R-API-030): the credential mode and the Bastion are the page's. */
  protected readonly setupAnswer = resource({
    params: () => {
      const repository = this.ref();
      const client = this.activeClient();
      if (!repository || !client || !this.repository()) return undefined;
      return { repository, client, credentials: this.credentials(), bastion: this.activeBastion() };
    },
    loader: ({ params }) =>
      this.setupApi.get(params.repository, {
        client: params.client,
        credentials: params.credentials,
        ...(params.bastion ? { bastion: params.bastion } : {}),
      }),
  });
  protected readonly setup = computed(() => valueOf(this.setupAnswer));
  /** The guide (R-API-053, R-API-070): of the repository, or of the class; it describes this installation, not a Bastion. */
  protected readonly guideAnswer = resource({
    params: () => {
      const client = this.activeClient();
      if (!client || this.activeBastion()) return undefined;
      const ref = this.ref();
      if (ref) return this.repository() ? { ref, format: '', cls: '', client } : undefined;
      const format = this.descriptor()?.name;
      const cls = this.activeClass();
      return format && cls ? { ref: null, format, cls, client } : undefined;
    },
    loader: ({ params }): Promise<Guide> =>
      params.ref !== null ? this.guidesApi.repository(params.ref, params.client) : this.guidesApi.format(params.format, params.cls, params.client),
  });
  protected readonly guide = computed(() => valueOf(this.guideAnswer));
  protected readonly loadFailure = computed(() => {
    const e = this.loaded.error() ?? this.setupAnswer.error() ?? this.guideAnswer.error() ?? this.formats.error();
    return e ? asApiError(e) : null;
  });
  /**
   * A repository of a format without clients has nothing to set up; the guide of a class asks for a format with a guide and one of
   * its classes.
   */
  protected readonly unknownGuide = computed(() => {
    if (!this.formats.hasValue()) return false;
    if (this.ref() !== null) return this.repository() !== undefined && this.descriptor() === null;
    return this.descriptor() === null || this.activeClass() === null;
  });

  /** The address of the header: the repository's on this installation or the Bastion, `<project>/<repository>` for a class. */
  protected readonly addresses = computed<readonly string[]>(() => {
    const url = this.ref() ? this.setup()?.registryUrl : this.guide()?.registryUrl;
    return url ? [url] : [];
  });

  // The token of the page: one pasted into More options (written into the snippets), or one created in place (shown once).
  protected readonly pasted = signal('');
  protected readonly reveal = signal(false);
  protected readonly pasteShape = computed<TokenShape | null>(() =>
    this.pasted() === '' ? null : checkTokenShape(this.pasted()),
  );
  private readonly pastedToken = computed(() => (this.pasteShape() === 'ok' ? this.pasted().trim() : null));
  protected readonly createdToken = signal<string | null>(null);
  protected readonly withPublish = signal(false);
  protected readonly creating = signal(false);
  protected readonly createFailure = signal<ApiError | null>(null);
  private idempotencyKey = newIdempotencyKey();
  private generation = 0;

  /** The snippets the reader copies: the client setup's for a repository, the guide's for a class. */
  protected readonly snippets = computed<readonly ClientSnippet[]>(() => {
    if (!this.ref()) return this.guide()?.snippets ?? [];
    const s = this.setup();
    if (!s) return [];
    const token = this.mode() === 'paste' && !this.anonymous() ? this.pastedToken() : null;
    return s.snippets.map((x) => ({ ...x, content: fillToken(x.content, token) }));
  });
  protected readonly byStep = computed<Record<Step, readonly ClientSnippet[]>>(() => ({
    configure: this.snippets().filter((s) => s.tab === 'configure'),
    install: this.snippets().filter((s) => s.tab === 'install'),
    publish: this.snippets().filter((s) => s.tab === 'publish'),
  }));
  protected readonly needsToken = computed(() => !this.anonymous() && this.mode() === 'paste' && this.pastedToken() === null);
  protected readonly publishTarget = computed(() => this.setup()?.publishTarget ?? null);
  protected readonly canReadConfig = computed(() => this.session.has('repositories:read-config'));
  /**
   * Publish is a step for who may publish (R-UI-123): the answer holds publish snippets only then (R-API-030). A virtual
   * repository without a deployment target keeps the step, with the reason, for who can read its configuration and so change it.
   */
  protected readonly offerPublish = computed(
    () =>
      this.byStep().publish.length > 0 ||
      (this.ref() !== null && this.activeClass() === 'virtual' && this.canReadConfig() && this.activeBastion() === null && this.setup() !== undefined),
  );
  protected readonly settingsLink = computed(() => {
    const link = this.repositoryLink();
    return link ? [...link, 'settings'] : null;
  });

  constructor() {
    // A token belongs to one repository and one client: another one drops what the page holds.
    effect(() => {
      this.ref();
      this.activeClient();
      untracked(() => this.forgetTokens());
    });
    // Going back to the variable drops a pasted token.
    effect(() => {
      if (this.mode() === 'env') untracked(() => this.pasted.set(''));
    });
    inject(DestroyRef).onDestroy(() => this.forgetTokens());
  }

  protected pickClient(client: string): void {
    void this.router.navigate([], { queryParams: { client }, queryParamsHandling: 'merge', replaceUrl: true });
  }

  protected pickBastion(bastion: string | null): void {
    void this.router.navigate([], { queryParams: { bastion }, queryParamsHandling: 'merge', replaceUrl: true });
  }

  /** The sentence under a snippet that is for a Bastion and holds what only a Bastion needs (R-BAS-007). */
  protected bastionNote(sn: ClientSnippet): string | null {
    return this.activeBastion() !== null && sn.content.includes('replace-registry-host=always')
      ? $localize`:@@setup.bastion.replace-host:replace-registry-host=always makes npm fetch every package from this address, also when a lockfile written against another host names that host.`
      : null;
  }

  /**
   * Creates a personal token for this repository in place (R-UI-122): read on the repository, and read and deploy on the
   * repository that receives a publish when the reader ticks it. Its value is kept in this component only.
   */
  protected async createToken(): Promise<void> {
    const setup = this.setup();
    const repository = this.ref();
    if (!setup || !repository || this.creating()) return;
    this.creating.set(true);
    this.createFailure.set(null);
    const target = setup.publishTarget;
    const publish = this.withPublish() && target !== null;
    const grants: NonNullable<NonNullable<CreateTokenRequest['scope']>['repositories']> = [
      { repositories: [repository], actions: ['read'] },
      ...(publish && target ? [{ repositories: [target], actions: ['read' as const, 'deploy' as const] }] : []),
    ];
    const request: CreateTokenRequest = {
      description: $localize`:@@setup.token.description:Client setup: ${setup.client}:client:, ${repository}:repository:, ${new Date().toISOString().slice(0, 10)}:date:`,
      ttlSeconds: setup.tokenTtlSeconds,
      scope: { repositories: grants },
      selfRotatable: false,
    };
    // forgetTokens() counts a generation: an answer that arrives after the repository or the client changed belongs to a
    // setup the page no longer shows (R-UI-042, R-UI-043).
    const generation = this.generation;
    try {
      const made = await this.createOnce(request);
      if (generation !== this.generation) {
        // Nobody can copy this token any more: it must not stay alive.
        await this.tokens.revoke(made.id).catch(() => undefined);
        return;
      }
      this.createdToken.set(made.token);
    } catch (e) {
      if (generation === this.generation) this.createFailure.set(asApiError(e));
    } finally {
      this.creating.set(false);
    }
  }

  private async createOnce(request: CreateTokenRequest) {
    try {
      return await this.tokens.create(request, this.idempotencyKey);
    } catch (e) {
      // The first attempt created a token whose value was lost with its answer: revoke it and create again under a new key,
      // so that exactly one working token exists (R-UI-042).
      const lost = e instanceof ApiError && e.code === 'token_already_issued' ? e.problem?.['tokenId'] : null;
      if (typeof lost !== 'string') throw e;
      await this.tokens.revoke(lost);
      this.idempotencyKey = newIdempotencyKey();
      return this.tokens.create(request, this.idempotencyKey);
    }
  }

  protected forgetTokens(): void {
    this.generation++;
    this.createdToken.set(null);
    this.createFailure.set(null);
    this.pasted.set('');
    this.reveal.set(false);
    this.idempotencyKey = newIdempotencyKey();
  }
}
