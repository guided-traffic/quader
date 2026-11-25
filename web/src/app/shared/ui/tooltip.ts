import { AriaDescriber } from '@angular/cdk/a11y';
import { Overlay, OverlayRef } from '@angular/cdk/overlay';
import { ComponentPortal } from '@angular/cdk/portal';
import {
  Component,
  Directive,
  ElementRef,
  OnDestroy,
  effect,
  inject,
  input,
  signal,
} from '@angular/core';

/**
 * The bubble of a tooltip, the reference's (its design system's tooltip, Q-UI-1065): the strong feedback grey of its toast with white
 * text in both schemes, 12 px on a 16 px line, 8 by 12 px of padding, 4 px round, at most 200 px wide with the text centred, and an
 * arrow of 8 by 4 px that points at the element from the 4 px between them. `placement` is the side of the element it stands on;
 * `arrowAt` is where the element's middle is, from the bubble's left edge, so that the arrow keeps pointing at it where the window's
 * edge pushed the bubble off centre.
 */
@Component({
  selector: 'quader-tooltip',
  // Hidden from assistive technology: the element's description (below) carries the text once.
  host: { 'aria-hidden': 'true', '[attr.data-placement]': 'placement()', '[style.--quader-tooltip-arrow]': 'arrowAt()' },
  template: `<span class="arrow"></span><span class="inner">{{ text() }}</span>`,
  styles: `
    :host {
      display: block;
      position: relative;
      box-sizing: border-box;
      padding: var(--quader-space-1);
    }
    .inner {
      display: block;
      box-sizing: border-box;
      // The reference's 200 px, its tooltip's largest width.
      max-inline-size: 12.5rem;
      padding: var(--quader-space-2) var(--quader-space-3);
      border-radius: var(--quader-radius-s);
      background: var(--quader-feedback-strong-bg);
      color: var(--quader-feedback-strong-fg);
      box-shadow: var(--quader-elevation-2);
      font: var(--quader-type-body-small);
      text-align: center;
      overflow-wrap: break-word;
    }
    .arrow {
      position: absolute;
      left: var(--quader-tooltip-arrow, 50%);
      inline-size: var(--quader-space-2);
      block-size: var(--quader-space-1);
      transform: translateX(-50%);
    }
    .arrow::before {
      content: '';
      position: absolute;
      left: 0;
      border: var(--quader-space-1) solid transparent;
    }
    :host([data-placement='top']) .arrow {
      bottom: 0;
    }
    :host([data-placement='top']) .arrow::before {
      top: 0;
      border-bottom-width: 0;
      border-top-color: var(--quader-feedback-strong-bg);
    }
    :host([data-placement='bottom']) .arrow {
      top: 0;
    }
    :host([data-placement='bottom']) .arrow::before {
      bottom: 0;
      border-top-width: 0;
      border-bottom-color: var(--quader-feedback-strong-bg);
    }
  `,
})
export class TooltipBubble {
  readonly text = signal('');
  readonly placement = signal<'top' | 'bottom'>('top');
  readonly arrowAt = signal<string | null>(null);
}

/**
 * A tooltip for an element, shown under the pointer and on keyboard focus, hidden by Escape, by leaving, and by blur
 * (WCAG 1.4.13: dismissible, hoverable, persistent while it is pointed at). The element is described by it
 * (a hidden message the CDK links with `aria-describedby`); an element whose name already says everything needs no tooltip. An empty
 * text is no tooltip: neither a bubble nor a description, so one cell of a list may carry it where its value needs one (Q-UI-1203).
 */
@Directive({
  selector: '[quaderTooltip]',
  host: {
    '(mouseenter)': 'show()',
    '(mouseleave)': 'hideSoon()',
    '(focus)': 'show()',
    '(blur)': 'hide()',
    '(keydown.escape)': 'hide()',
  },
})
export class Tooltip implements OnDestroy {
  readonly quaderTooltip = input.required<string>();

  private readonly overlay = inject(Overlay);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly describer = inject(AriaDescriber);
  private ref: OverlayRef | null = null;
  private placed: MutationObserver | null = null;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private described: string | null = null;

  constructor() {
    // The element is always described by the text (a hidden message the CDK keeps), so a screen reader announces it on
    // focus whether or not the bubble is showing.
    effect(() => {
      const text = this.quaderTooltip();
      const el = this.host.nativeElement;
      if (this.described !== null) this.describer.removeDescription(el, this.described);
      if (text) this.describer.describe(el, text);
      this.described = text || null;
      if (!text) this.hide();
    });
  }

  protected show(): void {
    clearTimeout(this.timer);
    if (this.ref || !this.quaderTooltip()) return;
    // Above the element, and under it where the window has no room above, as the reference places its tooltip; the arrow fills
    // the 4 px between the bubble and the element.
    const position = this.overlay
      .position()
      .flexibleConnectedTo(this.host)
      .withPositions([
        { originX: 'center', originY: 'top', overlayX: 'center', overlayY: 'bottom' },
        { originX: 'center', originY: 'bottom', overlayX: 'center', overlayY: 'top' },
      ])
      .withPush(true);
    // A scroll moves the bubble with its element, as the reference's does, rather than closing it.
    const ref = this.overlay.create({ positionStrategy: position, scrollStrategy: this.overlay.scrollStrategies.reposition() });
    this.ref = ref;
    const bubble = ref.attach(new ComponentPortal(TooltipBubble)).instance;
    bubble.text.set(this.quaderTooltip());
    // The side and the arrow are read from where the bubble stands: the overlay sets the styles of its pane and of the pane's box
    // whenever it places the bubble (on opening, on a window that changes its size), and a push off centre is no change of position
    // that it reports.
    this.placed = new MutationObserver(() => this.pointArrow(ref, bubble));
    for (const el of [ref.overlayElement, ref.overlayElement.parentElement]) {
      if (el) this.placed.observe(el, { attributes: true, attributeFilter: ['style'] });
    }
    this.pointArrow(ref, bubble);
    // Hoverable: moving from the element onto the tooltip keeps it open.
    ref.overlayElement.addEventListener('mouseenter', () => clearTimeout(this.timer));
    ref.overlayElement.addEventListener('mouseleave', () => this.hideSoon());
  }

  /**
   * Sets the side the bubble stands on and points its arrow at the element's middle, at least 8 px inside its corners, once the
   * bubble stands where its position put it.
   */
  private pointArrow(ref: OverlayRef, bubble: TooltipBubble): void {
    requestAnimationFrame(() => {
      if (this.ref !== ref) return;
      const origin = this.host.nativeElement.getBoundingClientRect();
      const box = ref.overlayElement.getBoundingClientRect();
      if (box.width === 0) return;
      bubble.placement.set(box.top >= origin.top + origin.height / 2 ? 'bottom' : 'top');
      const inset = 8;
      const at = Math.min(Math.max(origin.left + origin.width / 2 - box.left, inset), box.width - inset);
      bubble.arrowAt.set(`${Math.round(at)}px`);
    });
  }

  protected hideSoon(): void {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.hide(), 100);
  }

  protected hide(): void {
    clearTimeout(this.timer);
    this.placed?.disconnect();
    this.placed = null;
    this.ref?.dispose();
    this.ref = null;
  }

  ngOnDestroy(): void {
    this.hide();
    if (this.described !== null) this.describer.removeDescription(this.host.nativeElement, this.described);
  }
}
