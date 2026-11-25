import { CdkTrapFocus } from '@angular/cdk/a11y';
import { CdkConnectedOverlay, CdkOverlayOrigin, ConnectedPosition } from '@angular/cdk/overlay';
import { Component, DestroyRef, ElementRef, computed, inject, input, linkedSignal, signal, viewChild } from '@angular/core';
import { RouterLink } from '@angular/router';
import { setupCommands } from '../../core/project-links';
import { TabGroup, TabPage } from '../../shared/ui/tabs';
import { ClientChoice, clientsOfKind } from '../setup/clients';
import { ClientCommand } from './client-command';

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
 * A format with one client (APT, ansible-galaxy, curl) shows no strip of one tab: the client's name is the row's label. Where the
 * version is held by more than one repository a client can use (the version's page), a select **Repository** stands above the row, and
 * the command and the setup follow it.
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
  imports: [CdkOverlayOrigin, CdkConnectedOverlay, CdkTrapFocus, ClientCommand, RouterLink, TabGroup, TabPage],
  template: `
    <button
      #trigger
      type="button"
      class="btn btn-default command-trigger"
      [class.btn-sm]="size() === 'row'"
      [class.on-line]="size() === 'row'"
      cdkOverlayOrigin
      #origin="cdkOverlayOrigin"
      aria-haspopup="dialog"
      [attr.aria-expanded]="open()"
      [attr.aria-controls]="open() ? panelId : null"
      (click)="toggle()"
    >
      <span class="visually-hidden">{{ version() }}: </span>{{ verb()
      }}<span class="material-icons" aria-hidden="true">keyboard_arrow_down</span>
    </button>
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
        @if (repositories().length > 1) {
          <!-- The version is held by several repositories a client can use: the command and the setup follow the one picked here. -->
          <label class="command-repository">
            <span i18n="@@pkg.install.repository">Repository</span>
            <select class="select-input" (change)="pickRepository($event)">
              @for (r of repositories(); track r) {
                <option [value]="r" [selected]="r === chosenRepository()" translate="no">{{ r }}</option>
              }
            </select>
          </label>
        }
        @if (clients().length > 1) {
          <quader-tab-group [(selectedIndex)]="chosenTab" [keepAll]="true" [compact]="true">
            @if (setupLink(); as setup) {
              <a tabsEnd class="btn btn-default btn-sm setup-link" [routerLink]="setup" [queryParams]="{ client: chosenClient()?.id }"
                ><ng-container i18n="@@repo.content.tab.setup">Set up client</ng-container
                ><span class="visually-hidden" translate="no">&ngsp;{{ chosenClient()?.label }}</span></a
              >
            }
            @for (c of clients(); track c.id) {
              <quader-tab [label]="c.tab">
                <div class="command-body">
                  <quader-client-command
                    class="in-menu"
                    [format]="format()"
                    [name]="name()"
                    [version]="version()"
                    [repository]="chosenRepository()"
                    [client]="c.id"
                    [commandLabel]="fieldLabel(c)"
                    [copyLabel]="copyLabel(c)"
                  />
                  <!-- An image pulled by its digest, the second line of Docker's and Podman's tab (R-OCI-041). -->
                  @if (digest(); as d) {
                    <p class="by-digest" i18n="@@pkg.command-menu.by-digest">By digest</p>
                    <quader-client-command
                      class="in-menu"
                      [format]="format()"
                      [name]="name()"
                      [version]="version()"
                      [digest]="d"
                      [repository]="chosenRepository()"
                      [client]="c.id"
                      [commandLabel]="digestFieldLabel(c)"
                      [copyLabel]="digestCopyLabel(c)"
                    />
                  }
                </div>
              </quader-tab>
            }
          </quader-tab-group>
        } @else if (clients()[0]; as c) {
          <!-- One client: its name is the row's label, no strip of one tab. -->
          <div class="command-row">
            <p class="command-client" translate="no">{{ c.tab }}</p>
            @if (setupLink(); as setup) {
              <a class="btn btn-default btn-sm setup-link" [routerLink]="setup" [queryParams]="{ client: c.id }"
                ><ng-container i18n="@@repo.content.tab.setup">Set up client</ng-container
                ><span class="visually-hidden" translate="no">&ngsp;{{ c.label }}</span></a
              >
            }
          </div>
          <div class="command-body">
            <quader-client-command
              class="in-menu"
              [format]="format()"
              [name]="name()"
              [version]="version()"
              [repository]="chosenRepository()"
              [client]="c.id"
              [commandLabel]="fieldLabel(c)"
              [copyLabel]="copyLabel(c)"
            />
          </div>
        }
      </div>
    </ng-template>
  `,
  styles: `
    :host {
      display: inline-block;
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
    /* The repository's select on one line with its label, above the row of the clients. */
    .command-repository {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-1) var(--quader-space-2);
      padding-block: var(--quader-space-2);
      font-size: 0.875rem;
      font-weight: 600;
    }
    /* As wide as its longest repository's name, never the panel's width for its own sake. */
    .command-repository .select-input {
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
  `,
})
export class ClientCommandMenu {
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  readonly version = input.required<string>();
  /** The qualified name of the repository the commands go through, the one the panel starts at. */
  readonly repository = input.required<string>();
  /**
   * Every repository a client can take the version from, where there are several (the version's page, Q-UI-1241): the panel offers
   * them in a select above its row. Empty or one: no select.
   */
  readonly repositories = input<readonly string[]>([]);
  /** The format's client verb, the button's word: Pull, Install, Download (Q-UI-1066). */
  readonly verb = input.required<string>();
  /** `row`: the small button at the end of a list's row; `page`: the button of a page's head (Q-UI-1241). */
  readonly size = input<'row' | 'page'>('row');
  /** An image's digest: each client has a second command that pulls by it (formats/oci.md R-OCI-041). */
  readonly digest = input<string | null>(null);

  protected readonly positions = POSITIONS;
  protected readonly margin = MARGIN;
  protected readonly panelId = `client-command-${nextId++}`;
  protected readonly open = signal(false);
  /** The kind of an OCI artifact where it is known: a Helm chart's commands are Helm's alone (R-OCI-042). */
  readonly kind = input<string | null>(null);
  protected readonly clients = computed<readonly ClientChoice[]>(() => clientsOfKind(this.format(), this.kind()));
  /** The chosen tab, kept while the button stands (Q-UI-1240): the panel opens again on the client last chosen. */
  protected readonly chosenTab = signal(0);
  protected readonly chosenClient = computed(() => this.clients()[this.chosenTab()] ?? this.clients()[0] ?? null);
  /** The picked repository; another repository given starts at the one given again. */
  protected readonly chosenRepository = linkedSignal(() => this.repository());
  /** Set up client of the chosen repository (R-UI-126); none for a system repository of upstream, which answers no client (Q-UI-896). */
  protected readonly setupLink = computed(() => setupCommands(this.chosenRepository()));
  protected readonly panelLabel = computed(
    () => $localize`:@@pkg.command-menu.label:${this.verb()}:verb: ${this.name()}:name: ${this.version()}:version:`,
  );

  private readonly trigger = viewChild.required<ElementRef<HTMLButtonElement>>('trigger');
  private readonly panel = viewChild<ElementRef<HTMLElement>>('panel');
  private readonly overlay = viewChild(CdkConnectedOverlay);
  private resized: ResizeObserver | null = null;

  constructor() {
    inject(DestroyRef).onDestroy(() => this.resized?.disconnect());
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

  protected pickRepository(event: Event): void {
    this.chosenRepository.set((event.target as HTMLSelectElement).value);
  }

  protected toggle(): void {
    if (this.open()) this.close(true);
    else this.open.set(true);
  }

  /**
   * The panel takes the focus as it opens, so that a screen reader names it and Tab reaches its first field. It grows to its longest
   * command as the commands arrive, and is placed again each time, so that it stays in the window.
   */
  protected opened(): void {
    queueMicrotask(() => {
      const panel = this.panel()?.nativeElement ?? document.getElementById(this.panelId);
      panel?.focus({ preventScroll: true });
      if (panel && typeof ResizeObserver !== 'undefined') {
        this.resized?.disconnect();
        this.resized = new ResizeObserver(() => this.overlay()?.overlayRef?.updatePosition());
        this.resized.observe(panel);
      }
    });
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
