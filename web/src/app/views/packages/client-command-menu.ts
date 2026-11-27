import { CdkTrapFocus } from '@angular/cdk/a11y';
import { NgTemplateOutlet } from '@angular/common';
import { CdkConnectedOverlay, CdkOverlayOrigin, ConnectedPosition, FlexibleConnectedPositionStrategy } from '@angular/cdk/overlay';
import { Component, DestroyRef, ElementRef, computed, effect, inject, input, linkedSignal, resource, signal, untracked, viewChild } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ApiError } from '../../api/problem';
import { setupCommands } from '../../core/project-links';
import { REPOSITORIES_SHOWN } from '../../core/repositories-shown';
import { AddressField } from '../../shared/address-field';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Loading } from '../../shared/ui/loading';
import { TabGroup, TabPage } from '../../shared/ui/tabs';
import { ClientChoice, clientsOfKind } from '../../features/setup/clients';
import { ClientCommand } from './client-command';
import { DESTINATIONS, Destination, destinationLabel, mainDestinationLabel, onDestination, sameDestination } from './destinations';
import { VersionCommandFacts, clamavInstallNote } from './version-command';

let nextId = 0;

/**
 * Below the button, its end on the button's end, as GitLab's **Code** menu opens; above it where the window has no room below; and
 * from the button's start where its end leaves no room on the left. The window's margin keeps it 16 px from every edge.
 */
const POSITIONS: ConnectedPosition[] = [
  { originX: 'end', originY: 'bottom', overlayX: 'end', overlayY: 'top', offsetY: 4 },
  { originX: 'end', originY: 'top', overlayX: 'end', overlayY: 'bottom', offsetY: -4 },
  { originX: 'start', originY: 'bottom', overlayX: 'start', overlayY: 'top', offsetY: 4 },
  { originX: 'start', originY: 'top', overlayX: 'start', overlayY: 'bottom', offsetY: -4 },
];

/**
 * The commands of one version, behind a button (R-UI-052, R-UI-111, Q-UI-1066, Q-UI-1240, Q-UI-1241): the button carries the format's
 * client verb (`Pull`, `Install`, `Download`) and a chevron, as GitLab's **Code** button does, and opens a panel hung from it. The panel
 * is a tab per client of the format in the order of `FORMAT_CLIENTS` (Docker | Podman; npm | pnpm | Yarn Berry | Yarn Classic; .NET CLI
 * | NuGet CLI | Visual Studio), the product's tabs (`<quader-tab-group>`: the arrow keys, the tab remembered while the button stands),
 * then free room and, at the row's end, **Set up client**, which opens the client setup of the repository with the chosen client
 * (`?client=`); under the row the chosen client's command as a read-only field with the joined copy button (`<quader-client-command>`).
 * A format with one client (APT, ansible-galaxy, curl) shows no strip of one tab: the client's name is the row's label. An OCI
 * artifact's `kind` picks its kind's clients (`clientsOfKind`, R-OCI-042: a Helm chart Helm alone, an image Docker and Podman), and an
 * image's `digest` adds the pull by digest as the second line of each tab (R-OCI-041). A client with several forms of installing the
 * package (NuGet's, R-NUGET-037) has a select **Form** above its command in its tab (Q-UI-1360), so the row keeps one tab per client. Where the
 * version is held by more than one repository a client can use (the version's page), a select **Repository** stands above the row, and
 * the command and the setup follow it. Where the chosen repository is distributed through a Bastion as well (R-UI-271, Q-UI-1657), a
 * select **From** stands there too, beside **Repository** where both do: this installation, then each deployment of each distributing
 * Bastion; the commands, the download address and **Set up client** (`bastion`, `address`) follow it, and so does every panel of the page
 * that offers the same place (`DESTINATIONS`). The Bastion's application binds no destinations, so its panels have no **From**. A generic repository's file (`path`, formats/generic.md R-GENERIC-017) has curl, wget and
 * PowerShell, its download address above the row (`address`, Q-UI-1331) and, with its `sha256`, the check of the download as the second
 * line of each tab (Q-UI-1332); a folder's path with a trailing slash and `section="publish"` gives the upload into it. Where the page
 * has a main act of its own (a file's download in the browser, a folder's upload dialog), it is projected as `[mainAct]` and the button
 * is the chevron joined after it (`split`, Q-UI-1330, Q-UI-1334). A row that knows its version but not the repository its command goes
 * through (the flat list of packages, a package's versions, R-UI-269) gives `resolve`: the panel says **Loading the commands.** while it
 * reads the version, then offers what the version's head offers (Q-UI-1634).
 *
 * The panel is a non-modal dialog named by the verb, the package and the version: it takes the focus when it opens and keeps Tab
 * inside; Escape, a click outside it and the button close it, and the focus goes back to the button (a click outside on another control
 * keeps that control's focus). Every client's command is in the panel, the ones not chosen hidden, so it is as wide as its longest
 * command, from 24 to 48 rem, and never wider than the window less 16 px each side; switching tabs moves nothing. Where the row has no
 * room for the tabs and **Set up client** (npm's four at 390 px), the button goes to a line of its own under them. A command is asked
 * only when the panel opens, one request per client (`credentials=none`, R-API-030).
 */
/**
 * 16 px from the window's edges. No margin at the end: the CDK pushes a panel that overflows at the end back to the end of the window
 * narrowed by both margins measured from 0, so a panel as wide as the window less 32 px stood at 0 px from the start (the head of a
 * version's page at 390 px); with the end's margin left out it stands 16 px from either edge, and the panel's own width rule keeps it
 * 16 px from the end.
 */
const MARGIN = { start: 16, end: 0, top: 16, bottom: 16 };

@Component({
  selector: 'quader-client-command-menu',
  imports: [
    AddressField,
    CdkOverlayOrigin,
    CdkConnectedOverlay,
    CdkTrapFocus,
    ClientCommand,
    ErrorNotice,
    Loading,
    NgTemplateOutlet,
    RouterLink,
    TabGroup,
    TabPage,
  ],
  template: `
    <!-- A split button where the page has a main act of its own (a file's download in the browser, a folder's upload dialog): the act,
         then the chevron that opens the commands, joined as GitLab's download button with its menu (Q-UI-1330). -->
    <span class="split" [class.is-split]="split()" cdkOverlayOrigin #origin="cdkOverlayOrigin">
      <ng-content select="[mainAct]" />
      <button
        #trigger
        type="button"
        class="btn btn-default command-trigger"
        [class.btn-sm]="size() === 'row'"
        [class.on-line]="size() === 'row'"
        [class.chevron-only]="split()"
        aria-haspopup="dialog"
        [attr.aria-expanded]="open()"
        [attr.aria-controls]="open() ? panelId : null"
        [attr.aria-label]="split() ? splitLabel() : null"
        (click)="toggle()"
      >
        @if (!split()) {
          <span class="visually-hidden">{{ rowLabel() }}: </span>{{ verb() }}
        }<span class="material-icons" aria-hidden="true">keyboard_arrow_down</span>
      </button>
    </span>
    <ng-template
      cdkConnectedOverlay
      [cdkConnectedOverlayOrigin]="origin"
      [cdkConnectedOverlayOpen]="open()"
      [cdkConnectedOverlayPositions]="positions"
      [cdkConnectedOverlayPush]="true"
      [cdkConnectedOverlayViewportMargin]="margin"
      [cdkConnectedOverlayDisableClose]="true"
      cdkConnectedOverlayPanelClass="client-command-pane"
      (attach)="opened()"
      (detach)="close(true)"
      (overlayOutsideClick)="outside($event)"
      (overlayKeydown)="keydown($event)"
    >
      <div
        #panel
        class="menu client-command-panel"
        role="dialog"
        tabindex="-1"
        [id]="panelId"
        [attr.aria-label]="panelLabel()"
        cdkTrapFocus
      >
        <!-- A list's row reads its version when the panel first opens (R-UI-269): the spinner while it reads, the notice with Retry when
             the read failed, then what the head of the version's page would offer. -->
        @if (failure(); as f) {
          <div class="command-state"><quader-error-notice [error]="f" (retry)="resolveNow()" /></div>
        } @else if (resolve() && !resolved()) {
          <div class="command-state">
            <quader-loading i18n="@@pkg.command-menu.loading">Loading the commands.</quader-loading>
          </div>
        } @else if (resolvedCommand(); as rc) {
          @if (rc.kind === 'note') {
            <p class="command-note" [attr.data-testid]="'install-' + rc.cause">{{ rc.note }}</p>
          } @else if (rc.kind === 'clamav') {
            <!-- ClamAV's definitions: no command per version, freshclam always takes the version in use (R-UI-194). -->
            <div class="command-row">
              <p class="command-client" translate="no">freshclam</p>
              <a class="btn btn-default btn-sm setup-link" [routerLink]="rc.setup" [queryParams]="{ client: 'freshclam' }"
                ><ng-container i18n="@@repo.content.tab.setup">Set up client</ng-container
                ><span class="visually-hidden" translate="no">&ngsp;freshclam</span></a
              >
            </div>
            <p class="command-note" data-testid="install-clamav">{{ clamavNote }}</p>
          } @else {
            <ng-container *ngTemplateOutlet="commands" />
          }
        } @else {
          <ng-container *ngTemplateOutlet="commands" />
        }
      </div>
    </ng-template>
    <!-- The clients' commands through the chosen repository. -->
    <ng-template #commands>
        @if ((repositoriesShown() && offered().length > 1) || destinations().length > 0) {
          <!-- The panel's selects on one line where they fit, stacked where not: the repository, then where the command goes. -->
          <div class="command-selects">
            @if (repositoriesShown() && offered().length > 1) {
              <!-- The version is held by several repositories a client can use: the command and the setup follow the one picked here. -->
              <label class="command-repository">
                <span i18n="@@pkg.install.repository">Repository</span>
                <select class="select-input" (change)="pickRepository($event)">
                  @for (r of offered(); track r) {
                    <option [value]="r" [selected]="r === chosenRepository()" translate="no">{{ r }}</option>
                  }
                </select>
              </label>
            }
            @if (destinations().length > 0) {
              <!-- The repository is distributed through a Bastion as well: the commands and Set up client follow the place picked here
                   (R-UI-271, Q-UI-1657), and every panel of the page with it. -->
              <label class="command-from">
                <span i18n="@@pkg.command-menu.from">From</span>
                <select class="select-input" (change)="pickDestination($event)">
                  <option value="" [selected]="chosenDestination() === null" translate="no">{{ mainLabel() }}</option>
                  @for (d of destinations(); track $index) {
                    <option [value]="$index" [selected]="d === chosenDestination()" translate="no">{{ destinationLabel(d) }}</option>
                  }
                </select>
              </label>
            }
          </div>
        }
        @if (shownAddress(); as a) {
          <!-- A file's address for any other tool, above its clients, as GitLab's menu has its clone addresses (Q-UI-1331). -->
          <div class="command-address">
            <span class="field-label" i18n="@@files.download-address">Download address</span>
            <quader-address-field [value]="a" [label]="addressLabel" [copyLabel]="copyAddressLabel" />
          </div>
        }
        @if (clients().length > 1) {
          <quader-tab-group [(selectedIndex)]="chosenTab" [keepAll]="true" [compact]="true">
            @if (setupLink(); as setup) {
              <a tabsEnd class="btn btn-default btn-sm setup-link" [routerLink]="setup" [queryParams]="setupQuery(chosenClient()?.id)"
                ><ng-container i18n="@@repo.content.tab.setup">Set up client</ng-container
                ><span class="visually-hidden" translate="no">&ngsp;{{ chosenClient()?.label }}</span></a
              >
            }
            @for (c of clients(); track c.id) {
              <quader-tab [label]="c.tab">
                <ng-container *ngTemplateOutlet="body; context: { $implicit: c }" />
              </quader-tab>
            }
          </quader-tab-group>
        } @else if (clients()[0]; as c) {
          <!-- One client: its name is the row's label, no strip of one tab. -->
          <div class="command-row">
            <p class="command-client" translate="no">{{ c.tab }}</p>
            @if (setupLink(); as setup) {
              <a class="btn btn-default btn-sm setup-link" [routerLink]="setup" [queryParams]="setupQuery(c.id)"
                ><ng-container i18n="@@repo.content.tab.setup">Set up client</ng-container
                ><span class="visually-hidden" translate="no">&ngsp;{{ c.label }}</span></a
              >
            }
          </div>
          <ng-container *ngTemplateOutlet="body; context: { $implicit: c }" />
        }
    </ng-template>
    <!-- One client's commands: the command, and its second line where the thing has one. -->
    <ng-template #body let-c>
      <div class="command-body">
        <quader-client-command
          class="in-menu"
          [format]="format()"
          [name]="commandName()"
          [version]="version()"
          [repository]="chosenRepository()"
          [destination]="chosenDestination()"
          [client]="c.id"
          [kind]="commandKind()"
          [path]="path()"
          [tab]="section()"
          [commandLabel]="fieldLabel(c)"
          [copyLabel]="copyLabel(c)"
        />
        <!-- An image pulled by its digest, the second line of Docker's and Podman's tab (R-OCI-041). -->
        @if (commandDigest(); as d) {
          <p class="by-digest" i18n="@@pkg.command-menu.by-digest">By digest</p>
          <quader-client-command
            class="in-menu"
            [format]="format()"
            [name]="commandName()"
            [version]="version()"
            [digest]="d"
            [repository]="chosenRepository()"
            [destination]="chosenDestination()"
            [client]="c.id"
            [commandLabel]="digestFieldLabel(c)"
            [copyLabel]="digestCopyLabel(c)"
          />
        }
        <!-- A file's download checked against its SHA-256, the second line of each client's tab (R-GENERIC-017, Q-UI-1332). -->
        @if (sha256() && path() !== null && section() === 'install') {
          <p class="by-digest" i18n="@@pkg.command-menu.checksum">Check the download</p>
          <quader-client-command
            class="in-menu"
            [format]="format()"
            [name]="commandName()"
            [version]="version()"
            [repository]="chosenRepository()"
            [destination]="chosenDestination()"
            [client]="c.id"
            [path]="path()"
            [sha256]="sha256()"
            [snippetId]="c.id + '.install.checksum'"
            [commandLabel]="checkFieldLabel(c)"
            [copyLabel]="checkCopyLabel(c)"
          />
        }
      </div>
    </ng-template>
  `,
  styles: `
    :host {
      display: inline-block;
    }
    .split {
      display: inline-flex;
      align-items: stretch;
    }
    /* The main act and the chevron as one control: the act's end and the chevron's start square, their borders one hairline. */
    .is-split ::ng-deep > [mainAct] {
      border-start-end-radius: 0;
      border-end-end-radius: 0;
    }
    .is-split .command-trigger {
      margin-inline-start: -1px;
      border-start-start-radius: 0;
      border-end-start-radius: 0;
    }
    .command-trigger.chevron-only {
      padding-inline: var(--quader-space-1);
    }
    .command-trigger {
      gap: var(--quader-space-1);
      padding-inline-end: var(--quader-space-1);
      white-space: nowrap;
    }
    .command-trigger .material-icons {
      font-size: 1rem;
    }
    /* The panel: the menu's fill, hairline, radius and shadow (_shell.scss), as wide as its longest command, so that a command is
       read whole where there is room (Q-UI-455): at least 24 rem, at most 48 rem, and never wider than the window less its
       margins, where a longer command scrolls inside its field from its start, not the page. The strip's fades stand on the
       menu's fill. */
    .client-command-panel {
      --quader-surface: var(--quader-menu-bg);
      box-sizing: border-box;
      inline-size: max-content;
      min-inline-size: min(24rem, calc(100vw - 2rem));
      max-inline-size: min(48rem, calc(100vw - 2rem));
      max-block-size: calc(100vh - 2rem);
      overflow-y: auto;
      padding: var(--quader-space-1) var(--quader-space-3) var(--quader-space-3);
    }
    .client-command-panel:focus {
      outline: none;
    }
    .client-command-panel:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
    /* The selects above the row of the clients: Repository and From on one line where they fit (Q-UI-1657), stacked where not. */
    .command-selects {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-2) var(--quader-space-4);
      padding-block: var(--quader-space-2);
    }
    /* Each select on one line with its label. */
    .command-repository,
    .command-from {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-1) var(--quader-space-2);
      min-inline-size: 0;
      max-inline-size: 100%;
      font-size: 0.875rem;
      font-weight: 600;
    }
    /* As wide as its longest repository's name or place, never the panel's width for its own sake. */
    .command-repository .select-input,
    .command-from .select-input {
      flex: 0 1 auto;
      inline-size: auto;
      min-inline-size: 0;
      max-inline-size: 100%;
      font-weight: 400;
    }
    /* The action at the end of the strip's row, and at the end of its own line where it wrapped under the strip. */
    .setup-link {
      margin-inline-start: auto;
      white-space: nowrap;
    }
    /* One client: its name as the row's label, on the hairline the strip of several stands on. */
    .command-row {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-2) var(--quader-space-3);
      padding-block: var(--quader-space-1);
      border-block-end: 1px solid var(--quader-border);
    }
    .command-client {
      margin: 0;
      padding-block: var(--quader-space-1);
      font-size: 0.875rem;
      font-weight: 600;
    }
    .command-body {
      padding-block-start: var(--quader-space-3);
    }
    .by-digest {
      margin: var(--quader-space-2) 0 var(--quader-space-1);
      color: var(--quader-text-muted);
      font-size: 0.8125rem;
    }
    /* The read of a list's row's version, and its failure, where the commands will stand (R-UI-269). */
    .command-state {
      padding-block: var(--quader-space-3) var(--quader-space-1);
    }
    /* Why no client can install the version, as the version's page says it under its facts: muted, in the panel's width. */
    .command-note {
      max-inline-size: 40rem;
      margin: var(--quader-space-3) 0 var(--quader-space-1);
      color: var(--quader-text-muted);
      font-size: 0.875rem;
      white-space: normal;
    }
    /* A file's address above its clients, its label over its field (Q-UI-320). */
    .command-address {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      padding-block: var(--quader-space-2) var(--quader-space-1);
    }
  `,
})
export class ClientCommandMenu {
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  readonly version = input.required<string>();
  /**
   * The qualified name of the repository the commands go through, the one the panel starts at; empty where the row knows none and
   * `resolve` finds it (R-UI-269).
   */
  readonly repository = input('');
  /**
   * Every repository a client can take the version from, where there are several (the version's page, Q-UI-1241): the panel offers
   * them in a select above its row while the views name repositories (`REPOSITORIES_SHOWN`, Q-UI-1632). Empty or one: no select.
   */
  readonly repositories = input<readonly string[]>([]);
  /**
   * The read of the version's command where the row knows the version but not its repositories (a row of the flat list of packages or
   * of a package's versions, R-UI-269, Q-UI-1634): asked when the panel first opens, its answer kept while the same function is given;
   * the panel then offers what the head of the version's page would (`versionCommandFacts`): the clients through its repositories, the
   * note why none can install it, or ClamAV's Set up client. null: `repository` and the inputs are the commands' (a repository's list).
   */
  readonly resolve = input<(() => Promise<VersionCommandFacts>) | null>(null);
  /** The format's client verb, the button's word: Pull, Install, Download (Q-UI-1066). */
  readonly verb = input.required<string>();
  /** `row`: the small button at the end of a list's row; `page`: the button of a page's head (Q-UI-1241). */
  readonly size = input<'row' | 'page'>('row');
  /** An image's digest: each client has a second command that pulls by it (formats/oci.md R-OCI-041). */
  readonly digest = input<string | null>(null);
  /**
   * A generic repository's file, or folder with a trailing slash (formats/generic.md R-GENERIC-017): the commands are the server's for
   * that path, a file's download or a folder's upload (`section`); `name` is the file's or folder's name, `version` empty.
   */
  readonly path = input<string | null>(null);
  /** The tab of the client setup the commands come from: `install`, or `publish` for the upload into a folder. */
  readonly section = input<'install' | 'publish'>('install');
  /** A file's SHA-256: each client has a second command that checks the download against it (Q-UI-1332). */
  readonly sha256 = input<string | null>(null);
  /** A file's download address, above the clients with its copy button (Q-UI-1331). */
  readonly address = input<string | null>(null);
  /**
   * The page has a main act of its own, projected as `[mainAct]` (a file's download in the browser, a folder's upload dialog): the
   * button is the chevron after it, named by `splitLabel` (Q-UI-1330).
   */
  readonly split = input(false);

  protected readonly positions = POSITIONS;
  protected readonly margin = MARGIN;
  protected readonly panelId = `client-command-${nextId++}`;
  protected readonly open = signal(false);
  /**
   * The kind of the version where its format has one: an OCI artifact's (a Helm chart's commands are Helm's alone, R-OCI-042), a NuGet
   * package's (a .NET tool's and a template's are the .NET CLI's, and each client's forms are those of the kind, R-NUGET-037).
   */
  readonly kind = input<string | null>(null);

  /** The answer of `resolve`, kept while the same function is given; null while none is asked or it has not answered. */
  protected readonly resolved = linkedSignal<(() => Promise<VersionCommandFacts>) | null, VersionCommandFacts | null>({
    source: this.resolve,
    computation: () => null,
  });
  /** The failure of `resolve`, shown in the panel with its Retry (R-UI-216). */
  protected readonly failure = linkedSignal<(() => Promise<VersionCommandFacts>) | null, ApiError | null>({
    source: this.resolve,
    computation: () => null,
  });
  /** The function whose answer is awaited now: a row whose version changed while it was read asks the new one at once. */
  private resolvingFor: (() => Promise<VersionCommandFacts>) | null = null;
  protected readonly resolvedCommand = computed(() => this.resolved()?.command ?? null);
  /** The name the clients install the version by: a role's `role/<id>` where `resolve` says so, the given name otherwise. */
  protected readonly commandName = computed(() => this.resolved()?.name ?? this.name());
  protected readonly commandKind = computed(() => (this.resolve() ? (this.resolved()?.kind ?? null) : this.kind()));
  protected readonly commandDigest = computed(() => (this.resolve() ? (this.resolved()?.digest ?? null) : this.digest()));
  /** The repository the panel starts at and the ones it offers: the resolved menu's, or the given ones. */
  private readonly startRepository = computed(() => {
    const c = this.resolvedCommand();
    return c?.kind === 'menu' ? c.repository : this.repository();
  });
  protected readonly offered = computed(() => {
    const c = this.resolvedCommand();
    return c?.kind === 'menu' ? c.repositories : this.repositories();
  });
  /** Whether the panel may name the repositories (a Bastion that hides them does not, Q-UI-1632): the select stands only then. */
  protected readonly repositoriesShown = inject(REPOSITORIES_SHOWN);
  protected readonly clamavNote = clamavInstallNote();

  protected readonly clients = computed<readonly ClientChoice[]>(() => clientsOfKind(this.format(), this.commandKind()));
  /** The chosen tab, kept while the button stands (Q-UI-1240): the panel opens again on the client last chosen. */
  protected readonly chosenTab = signal(0);
  protected readonly chosenClient = computed(() => this.clients()[this.chosenTab()] ?? this.clients()[0] ?? null);
  /** The picked repository; another repository given starts at the one given again. */
  protected readonly chosenRepository = linkedSignal(() => this.startRepository());
  /**
   * Set up client of the chosen repository (R-UI-126), in both applications (the Bastion's since bastion.md R-BAS-129, Q-UI-1468); none
   * for a system repository of upstream, which answers no client (Q-UI-896).
   */
  protected readonly setupLink = computed(() => setupCommands(this.chosenRepository()));

  // ---- Where the commands install from (R-UI-271, Q-UI-1657) ----

  /** The main installation's reads of a repository's destinations; null in the Bastion's application, which is the destination itself. */
  private readonly destinationReads = inject(DESTINATIONS);
  /** Whether the panel has opened once: its repository's destinations are read from then on, never for a row nobody opens. */
  private readonly wasOpened = signal(false);
  /**
   * The destinations of the chosen repository: read once the panel opened, for the install commands only (a Bastion takes no upload),
   * kept while the repository stays; a read that failed offers no choice, and the next open asks again.
   */
  private readonly destinationsRead = resource({
    params: () => {
      const repository = this.chosenRepository();
      return this.destinationReads && this.wasOpened() && repository && this.section() === 'install' ? { repository } : undefined;
    },
    loader: ({ params }) => this.destinationReads!.of(params.repository).catch(() => null),
  });
  /** The deployments of the Bastions that distribute the chosen repository; none: the panel shows no **From**. */
  protected readonly destinations = computed(() => (this.destinationsRead.hasValue() ? (this.destinationsRead.value()?.bastions ?? []) : []));
  /** The page's choice where this panel offers it (the same entry of `destinations`), else null: this installation. */
  protected readonly chosenDestination = computed<Destination | null>(() => {
    const chosen = this.destinationReads?.chosen() ?? null;
    return this.destinations().find((d) => sameDestination(d, chosen)) ?? null;
  });
  protected readonly mainLabel = computed(() =>
    mainDestinationLabel(this.destinationsRead.hasValue() ? (this.destinationsRead.value()?.main ?? null) : null),
  );
  protected readonly destinationLabel = destinationLabel;
  /** A file's download address on the chosen place: this installation's as given, a Bastion's with its address; none where it cannot be. */
  protected readonly shownAddress = computed(() => {
    const a = this.address();
    const d = this.chosenDestination();
    if (a === null || d === null) return a;
    return onDestination(a, this.destinationsRead.value()?.main ?? null, d);
  });

  /** The query of Set up client's link: the client, and the place the commands install from. */
  protected setupQuery(client: string | undefined): Record<string, string | null> {
    const d = this.chosenDestination();
    return { client: client ?? null, bastion: d?.bastion ?? null, address: d?.address || null };
  }

  protected pickDestination(event: Event): void {
    const value = (event.target as HTMLSelectElement).value;
    this.destinationReads?.chosen.set(value === '' ? null : (this.destinations()[Number(value)] ?? null));
  }
  protected readonly panelLabel = computed(() =>
    this.version() === ''
      ? $localize`:@@pkg.command-menu.label-file:${this.verb()}:verb: ${this.name()}:name:`
      : $localize`:@@pkg.command-menu.label:${this.verb()}:verb: ${this.name()}:name: ${this.version()}:version:`,
  );
  /** What a row's button names before its verb, for a screen reader: the version, or a file's name. */
  protected readonly rowLabel = computed(() => this.version() || this.name());
  /** The name of the chevron of a split button. */
  protected readonly splitLabel = computed(() => $localize`:@@pkg.command-menu.split:${this.verb()}:verb: with a client`);
  protected readonly addressLabel = $localize`:@@files.download-address:Download address`;
  protected readonly copyAddressLabel = $localize`:@@files.copy-download-address:Copy download address`;

  private readonly trigger = viewChild.required<ElementRef<HTMLButtonElement>>('trigger');
  private readonly panel = viewChild<ElementRef<HTMLElement>>('panel');
  private readonly overlay = viewChild(CdkConnectedOverlay);
  private readonly origin = viewChild.required(CdkOverlayOrigin);
  /** How far the end-aligned positions are shifted towards the end, so that the panel keeps 16 px from the window's start. */
  private shift = 0;
  private resized: ResizeObserver | null = null;

  constructor() {
    inject(DestroyRef).onDestroy(() => this.resized?.disconnect());
    // The open panel of a row reads its version (R-UI-269): when it first opens, and again at once when the row's version changes while
    // it is open (the list read again in place, R-UI-213). An answer is kept for the next open; a failure waits for its Retry.
    effect(() => {
      if (!this.open() || !this.resolve()) return;
      untracked(() => {
        if (!this.resolved() && !this.failure()) this.resolveNow();
      });
    });
  }

  protected fieldLabel(c: ClientChoice): string {
    return $localize`:@@pkg.command-menu.field:Command for ${c.label}:client:`;
  }

  protected copyLabel(c: ClientChoice): string {
    return $localize`:@@pkg.command-menu.copy:Copy the command for ${c.label}:client:`;
  }

  protected digestFieldLabel(c: ClientChoice): string {
    return $localize`:@@pkg.command-menu.digest-field:Command for ${c.label}:client: by digest`;
  }

  protected digestCopyLabel(c: ClientChoice): string {
    return $localize`:@@pkg.command-menu.digest-copy:Copy the command for ${c.label}:client: by digest`;
  }

  protected checkFieldLabel(c: ClientChoice): string {
    return $localize`:@@pkg.command-menu.check-field:Check of the download for ${c.label}:client:`;
  }

  protected checkCopyLabel(c: ClientChoice): string {
    return $localize`:@@pkg.command-menu.check-copy:Copy the check of the download for ${c.label}:client:`;
  }

  protected pickRepository(event: Event): void {
    this.chosenRepository.set((event.target as HTMLSelectElement).value);
  }

  protected toggle(): void {
    if (this.open()) {
      this.close(true);
      return;
    }
    this.open.set(true);
    this.wasOpened.set(true);
  }

  /** Reads the version's command through `resolve`, once per function at a time; the notice's Retry calls it again. */
  protected resolveNow(): void {
    const resolve = this.resolve();
    if (!resolve || this.resolvingFor === resolve) return;
    this.resolvingFor = resolve;
    this.failure.set(null);
    void resolve()
      .then(
        (facts) => {
          if (this.resolve() === resolve) this.resolved.set(facts);
        },
        (e: unknown) => {
          if (this.resolve() === resolve) this.failure.set(asApiError(e));
        },
      )
      .finally(() => {
        if (this.resolvingFor === resolve) this.resolvingFor = null;
      });
  }

  /**
   * The panel takes the focus as it opens, so that a screen reader names it and Tab reaches its first field. It grows to its longest
   * command as the commands arrive, and is placed again each time, so that it stays in the window.
   */
  protected opened(): void {
    queueMicrotask(() => {
      const panel = this.panel()?.nativeElement ?? document.getElementById(this.panelId);
      panel?.focus({ preventScroll: true });
      this.place(panel);
      if (panel && typeof ResizeObserver !== 'undefined') {
        this.resized?.disconnect();
        this.resized = new ResizeObserver(() => this.place(panel));
        this.resized.observe(panel);
      }
    });
  }

  /**
   * Places the panel again, its end on the button's end and never nearer than 16 px to the window's start. The CDK counts a panel as
   * inside the window from 0 px, not from the margin: a row's button that ends 357 px into a window of 390 px (a stacked list's row)
   * hung a panel of 358 px at 0 px (R-UI-269). The end-aligned positions are shifted towards the end by what is missing.
   */
  private place(panel: HTMLElement | null): void {
    const ref = this.overlay()?.overlayRef;
    if (!ref || !panel) return;
    const end = this.origin().elementRef.nativeElement.getBoundingClientRect().right;
    // Only where the CDK keeps the end-aligned place (it starts at 0 px or later) but nearer than the margin; a panel that would start
    // before 0 px is pushed by the CDK itself (the head of a NuGet version, whose button stands mid-line at 390 px).
    const start = end - panel.offsetWidth;
    const shift = start > -1.5 && start < MARGIN.start ? Math.ceil(MARGIN.start - start) : 0;
    if (shift !== this.shift) {
      this.shift = shift;
      const strategy = ref.getConfig().positionStrategy;
      if (strategy instanceof FlexibleConnectedPositionStrategy) {
        strategy.withPositions(POSITIONS.map((p) => (p.originX === 'end' ? { ...p, offsetX: shift } : p)));
      }
    }
    ref.updatePosition();
  }

  /**
   * Closes the panel. The focus goes back to the button when it was in the panel or nowhere (Escape, the button, a click on the page's
   * background); a click outside on another control keeps that control's focus.
   */
  close(restore: boolean): void {
    if (!this.open()) return;
    const active = document.activeElement;
    const panel = this.panel()?.nativeElement;
    const lost = !active || active === document.body || (panel?.contains(active) ?? false);
    this.resized?.disconnect();
    this.resized = null;
    this.open.set(false);
    if (restore && lost) this.trigger().nativeElement.focus({ preventScroll: true });
  }

  /** Escape closes the panel and gives the focus back to the button. */
  protected keydown(event: KeyboardEvent): void {
    if (event.key !== 'Escape' || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    event.preventDefault();
    this.close(true);
  }

  /** A click outside the panel closes it; a click on the button is the button's own (it toggles). */
  protected outside(event: MouseEvent): void {
    if (this.trigger().nativeElement.contains(event.target as Node)) return;
    this.close(true);
  }
}
