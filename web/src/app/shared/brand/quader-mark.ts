import { Component, input } from '@angular/core';
import { LOCKUP, MARK } from './mark-geometry';

/** A stone of a mark on its 64 grid. */
interface Stone {
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
}

/** A mark of mark-geometry.ts: Quader's wall (`MARK`) or the Bastion's tower (`BASTION_MARK`). */
export interface MarkGeometry {
  readonly viewBox: string;
  readonly radius: number;
  readonly stones: readonly Stone[];
  readonly coloured: Stone;
}

/**
 * A mark drawn inline (web/scripts/brand-logo.mjs generates the geometry): by default Quader's, a wall of three courses,
 * one stone coloured; the Bastion's application passes its tower (`[mark]="BASTION_MARK"`, Q-UI-1617). The stones take
 * `--quader-mark-stone` and the coloured one `--quader-mark-accent`, so the mark follows the theme the viewer chose,
 * which the SVG files in public/brand cannot (an `<img>` only knows the system's colour scheme).
 *
 * Without `wordmark` it is decorative, the element next to it carries the name. With `wordmark` it is Quader's
 * horizontal lockup, the wall and the name as outlined paths, and an image named Quader (the sign-in and signed-out
 * screens). Its height is the host's `block-size`; a multiple of 16 px puts every stone and joint on whole pixels.
 */
@Component({
  selector: 'quader-mark',
  template: `
    <svg
      [attr.viewBox]="wordmark() ? lockup.viewBox : mark().viewBox"
      [attr.role]="wordmark() ? 'img' : null"
      [attr.aria-label]="wordmark() ? 'Quader' : null"
      [attr.aria-hidden]="wordmark() ? null : 'true'"
      focusable="false"
    >
      @for (s of mark().stones; track $index) {
        <rect
          class="stone"
          [attr.x]="s.x"
          [attr.y]="s.y"
          [attr.width]="s.width"
          [attr.height]="s.height"
          [attr.rx]="mark().radius"
        />
      }
      <rect
        class="accent"
        [attr.x]="mark().coloured.x"
        [attr.y]="mark().coloured.y"
        [attr.width]="mark().coloured.width"
        [attr.height]="mark().coloured.height"
        [attr.rx]="mark().radius"
      />
      @if (wordmark()) {
        <path class="stone" [attr.d]="lockup.word" />
      }
    </svg>
  `,
  styles: `
    :host {
      display: inline-block;
      line-height: 0;
    }
    svg {
      block-size: 100%;
      inline-size: auto;
    }
    .stone {
      fill: var(--quader-mark-stone);
    }
    .accent {
      fill: var(--quader-mark-accent);
    }
  `,
})
export class QuaderMark {
  /** The mark to draw; Quader's wall unless an application of its own (the Bastion) passes its mark. */
  readonly mark = input<MarkGeometry>(MARK);
  /** Draws Quader's lockup with the name instead of the mark alone. */
  readonly wordmark = input(false);

  protected readonly lockup = LOCKUP;
}
