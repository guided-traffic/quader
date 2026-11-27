import { Overlay, OverlayRef } from '@angular/cdk/overlay';
import { ComponentPortal } from '@angular/cdk/portal';
import { NgTemplateOutlet } from '@angular/common';
import { Component, Directive, ElementRef, OnDestroy, TemplateRef, inject, input, signal } from '@angular/core';

/** How long the pointer rests on the button before the details open (R-UI-256): a pointer that passes over a list opens nothing. */
export const DETAIL_POPOVER_DELAY_MS = 300;

let nextId = 0;

/**
 * The card of a popover's details (R-UI-256): the menu's surface, its border and its shadow, a width that holds a line of facts, never
 * wider than the window less its gutters. Its content is the template of the button that opened it.
 */
@Component({
  selector: 'quader-detail-popover-panel',
  imports: [NgTemplateOutlet],
  host: { '[id]': 'id()', class: 'detail-popover' },
  template: `<ng-container *ngTemplateOutlet="content()" />`,
  styles: `
    :host {
      display: block;
      box-sizing: border-box;
      inline-size: max-content;
      max-inline-size: min(28rem, calc(100vw - 2 * var(--quader-space-4)));
      /* Never taller than the window less its gutters: a card that would be (its Technical details opened on a short phone) scrolls
         in itself rather than stand past the window's edge, where nothing could reach its end. */
      max-block-size: calc(100dvh - 2 * var(--quader-space-4));
      overflow-y: auto;
      padding: var(--quader-space-3) var(--quader-space-4);
      border: 1px solid var(--quader-menu-border);
      border-radius: var(--quader-radius-m);
      background: var(--quader-menu-bg);
      color: var(--quader-on-surface);
      box-shadow: var(--quader-elevation-2);
      font-size: var(--quader-text-size);
      line-height: 1.5;
      text-align: start;
      white-space: normal;
      overflow-wrap: anywhere;
    }
  `,
})
export class DetailPopoverPanel {
  readonly id = signal('');
  readonly content = signal<TemplateRef<unknown> | null>(null);
}

/**
 * Details on demand (R-UI-256, Q-UI-1512): a button whose details open in a card beside it, on hover after
 * `DETAIL_POPOVER_DELAY_MS`, on keyboard focus and on a tap or a click, the same for the pointer, the keyboard and the touch screen.
 * It stays open while the pointer is on the button or the card (WCAG 1.4.13: hoverable, persistent); a click or a tap keeps it open
 * until the next one; Escape closes it and gives the focus back, and so do a tap outside it and the focus leaving both. The button
 * says whether it is open (`aria-expanded`) and which card it opens (`aria-controls`). Its content is a template of the host's, so
 * every fact of it comes with the host's data and the card needs no request of its own (R-UI-217 does not apply).
 *
 * `quaderTooltip` stays for a sentence of plain text; this is for facts in lines, links and a disclosure, and a target a keyboard
 * reaches because it is a button.
 */
@Directive({
  selector: 'button[quaderDetailPopover]',
  host: {
    type: 'button',
    '[attr.aria-expanded]': 'open()',
    '[attr.aria-controls]': "open() ? panelId : null",
    '(mouseenter)': 'hoverIn()',
    '(mouseleave)': 'hoverOut()',
    '(pointerdown)': 'pointerDown()',
    '(focus)': 'focused()',
    '(blur)': 'blurred($event)',
    '(click)': 'clicked()',
    '(keydown.escape)': 'escape($event)',
  },
})
export class DetailPopover implements OnDestroy {
  /** What the card shows. */
  readonly quaderDetailPopover = input.required<TemplateRef<unknown>>();

  private readonly overlay = inject(Overlay);
  private readonly host = inject<ElementRef<HTMLButtonElement>>(ElementRef);
  private ref: OverlayRef | null = null;
  private resized: ResizeObserver | null = null;
  private timer: ReturnType<typeof setTimeout> | undefined;
  /** A click or a tap opened it, or kept it open: leaving with the pointer does not close it. */
  private pinned = false;
  /** The focus that comes is a pointer's, not the keyboard's: a click decides, not the focus. */
  private byPointer = false;
  private readonly outside = (e: Event): void => {
    const t = e.target as Node | null;
    if (t && (this.host.nativeElement.contains(t) || this.ref?.overlayElement.contains(t))) return;
    this.close();
  };

  readonly panelId = `quader-detail-popover-${nextId++}`;
  /** The card is open. */
  readonly open = signal(false);

  protected hoverIn(): void {
    clearTimeout(this.timer);
    if (this.ref) return;
    this.timer = setTimeout(() => this.show(), DETAIL_POPOVER_DELAY_MS);
  }

  protected hoverOut(): void {
    clearTimeout(this.timer);
    if (!this.pinned) this.closeSoon();
  }

  protected pointerDown(): void {
    this.byPointer = true;
  }

  protected focused(): void {
    // A pointer's focus comes before its click, which decides; the keyboard's opens the card at once.
    if (this.byPointer) return;
    this.show();
  }

  protected blurred(e: FocusEvent): void {
    this.byPointer = false;
    const to = e.relatedTarget as Node | null;
    if (to && this.ref?.overlayElement.contains(to)) return;
    if (to === null && this.pinned) return; // a click on nothing focusable: the tap outside decides
    this.close();
  }

  protected clicked(): void {
    this.byPointer = false;
    clearTimeout(this.timer);
    if (this.ref && this.pinned) {
      this.close();
      return;
    }
    this.pinned = true;
    this.show();
  }

  protected escape(e: Event): void {
    if (!this.ref) return;
    e.stopPropagation();
    this.close();
    this.host.nativeElement.focus();
  }

  /** Opens the card under the button, above it where the window has no room below, pushed inside the window's gutters. */
  show(): void {
    clearTimeout(this.timer);
    if (this.ref) return;
    const position = this.overlay
      .position()
      .flexibleConnectedTo(this.host)
      .withPositions([
        { originX: 'start', originY: 'bottom', overlayX: 'start', overlayY: 'top', offsetY: 4 },
        { originX: 'start', originY: 'top', overlayX: 'start', overlayY: 'bottom', offsetY: -4 },
        { originX: 'end', originY: 'bottom', overlayX: 'end', overlayY: 'top', offsetY: 4 },
        { originX: 'end', originY: 'top', overlayX: 'end', overlayY: 'bottom', offsetY: -4 },
      ])
      .withViewportMargin(16)
      .withPush(true);
    const ref = this.overlay.create({ positionStrategy: position, scrollStrategy: this.overlay.scrollStrategies.reposition() });
    this.ref = ref;
    const panel = ref.attach(new ComponentPortal(DetailPopoverPanel)).instance;
    panel.id.set(this.panelId);
    panel.content.set(this.quaderDetailPopover());
    ref.overlayElement.addEventListener('mouseenter', () => clearTimeout(this.timer));
    ref.overlayElement.addEventListener('mouseleave', () => {
      if (!this.pinned) this.closeSoon();
    });
    ref.overlayElement.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') this.escape(e);
    });
    ref.overlayElement.addEventListener('focusout', (e) => {
      const to = e.relatedTarget as Node | null;
      if (to && (ref.overlayElement.contains(to) || this.host.nativeElement.contains(to))) return;
      if (to !== null) this.close();
    });
    document.addEventListener('pointerdown', this.outside, true);
    // A card that grows (its Technical details opened) is placed again, so that it stays inside the window: one that opened above its
    // button grew past the window's top edge at 390 px.
    if (typeof ResizeObserver !== 'undefined') {
      // Placed again in the next frame: placing it in the observer's own turn changes the size it observes (a loop the browser reports).
      this.resized = new ResizeObserver(() => requestAnimationFrame(() => this.ref === ref && ref.updatePosition()));
      this.resized.observe(ref.overlayElement);
    }
    this.open.set(true);
  }

  private closeSoon(): void {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.close(), 100);
  }

  /** Closes the card. */
  close(): void {
    clearTimeout(this.timer);
    this.pinned = false;
    document.removeEventListener('pointerdown', this.outside, true);
    this.resized?.disconnect();
    this.resized = null;
    this.ref?.dispose();
    this.ref = null;
    this.open.set(false);
  }

  ngOnDestroy(): void {
    this.close();
  }
}
