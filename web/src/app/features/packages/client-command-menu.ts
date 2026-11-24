import { CdkTrapFocus } from '@angular/cdk/a11y';
import { CdkConnectedOverlay, CdkOverlayOrigin, ConnectedPosition } from '@angular/cdk/overlay';
import { Component, DestroyRef, ElementRef, computed, inject, input, signal, viewChild } from '@angular/core';
import { ClientChoice, clientsOf } from '../setup/clients';
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
 * The commands of one version in one repository, behind a button beside the version (R-UI-052, Q-UI-1066): the button carries the
 * format's client verb (`Pull`, `Install`, `Download`) and a chevron, as GitLab's **Code** button does, and opens a panel hung from
 * it with a section per client of the format, each its heading (the client's name) and its command as a read-only field with the
 * joined copy button (`<quader-client-command>`). The panel is a non-modal dialog named by the verb, the package and the version: it
 * takes the focus when it opens and keeps Tab inside; Escape, a click outside it and the button close it, and the focus goes back to
 * the button (a click outside on another control keeps that control's focus). It is as wide as its longest command, from 24 to
 * 48 rem, and never wider than the window less 16 px each side, so at 390 px it fits without a sideways scroll. A command is asked only when the panel opens, one
 * request per client (`credentials=none`, R-API-030).
 */
@Component({
  selector: 'quader-client-command-menu',
  imports: [CdkOverlayOrigin, CdkConnectedOverlay, CdkTrapFocus, ClientCommand],
  template: `
    <button
      #trigger
      type="button"
      class="btn btn-default btn-sm on-line command-trigger"
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
      [cdkConnectedOverlayViewportMargin]="16"
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
        @for (c of clients(); track c.id; let first = $first) {
          <div class="command-section" role="group" [class.first]="first" [attr.aria-labelledby]="panelId + '-' + c.id">
            <p class="menu-heading" [id]="panelId + '-' + c.id" translate="no">{{ c.label }}</p>
            <quader-client-command
              class="in-menu"
              [format]="format()"
              [name]="name()"
              [version]="version()"
              [repository]="repository()"
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
       margins, where a longer command scrolls inside its field from its start, not the page. */
    .client-command-panel {
      box-sizing: border-box;
      inline-size: max-content;
      min-inline-size: min(24rem, calc(100vw - 2rem));
      max-inline-size: min(48rem, calc(100vw - 2rem));
      max-block-size: calc(100vh - 2rem);
      overflow-y: auto;
      padding: 0;
    }
    .client-command-panel:focus {
      outline: none;
    }
    .client-command-panel:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
    .command-section {
      padding: var(--quader-space-1) var(--quader-space-3) var(--quader-space-3);
    }
    .command-section:not(.first) {
      border-block-start: 1px solid var(--quader-border);
    }
    .command-section .menu-heading {
      padding-inline: 0;
    }
  `,
})
export class ClientCommandMenu {
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  readonly version = input.required<string>();
  /** The qualified name of the repository the commands go through. */
  readonly repository = input.required<string>();
  /** The format's client verb, the button's word: Pull, Install, Download (Q-UI-1066). */
  readonly verb = input.required<string>();

  protected readonly positions = POSITIONS;
  protected readonly panelId = `client-command-${nextId++}`;
  protected readonly open = signal(false);
  protected readonly clients = computed<readonly ClientChoice[]>(() => clientsOf(this.format()));
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
