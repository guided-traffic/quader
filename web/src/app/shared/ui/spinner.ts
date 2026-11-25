import { Component, input } from '@angular/core';
import { MARK } from '../brand/mark-geometry';

/** Where a spinner stands: on a line of text, in a button in place of its icon, or for a page or a large area (R-UI-217). */
export type SpinnerSize = 'line' | 'button' | 'page';

/** One stone of the spinner: its rectangle on the mark's 64 grid. */
export interface SpinnerStone {
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
}

/**
 * The six stones of the mark in the order the light travels (Q-UI-1229): clockwise around the square's middle, from the top left
 * (top left, top right, middle right, bottom right, bottom left, middle left). Derived from the mark's own geometry
 * (`mark-geometry.ts`, written by `web/scripts/brand-logo.mjs`), so the spinner is always the mark.
 */
export function clockwiseStones(): readonly SpinnerStone[] {
  const all: SpinnerStone[] = [...MARK.stones, MARK.coloured];
  const half = 32;
  const angle = (s: SpinnerStone): number => Math.atan2(s.y + s.height / 2 - half, s.x + s.width / 2 - half);
  // The top left stone starts the round; on screen coordinates (y downwards) a growing angle turns clockwise.
  const first = all.reduce((a, b) => (b.x + b.y < a.x + a.y ? b : a));
  const turn = (s: SpinnerStone): number => (angle(s) - angle(first) + 4 * Math.PI) % (2 * Math.PI);
  return all.sort((a, b) => turn(a) - turn(b));
}

/**
 * The spinner of every load (R-UI-217, Q-UI-1229, the owner's choice of 2026-11-25: "Quader spinner sieht gut aus, aber bitte 3 reihen
 * Steine und damit viereckig"): the product's mark, a square of three rows of six stones, drawn inline so the stones stay crisp at
 * 16 px. At rest each stone has the quiet neutral `--quader-spinner-stone`; one after another, clockwise from the top left, a stone
 * lights up in the mark's sandstone (`--quader-mark-accent`) and fades back, a round in 1.8 s. Under `prefers-reduced-motion` no light
 * travels: the top left stone stays lit and the mark breathes slowly (Q-UI-1237). The look is the global stylesheet's
 * (`web/src/styles/_spinner.scss`), so the page before the app starts (`index.html`) draws the same spinner with the same rules.
 *
 * It is decoration for assistive technology: the sentence beside it says what loads (`<quader-loading>`), a busy button keeps its
 * label (`[quaderBusy]`). A spinner on its own does not wait for the delay of Q-UI-1230; use those two.
 */
@Component({
  selector: 'quader-spinner',
  template: `
    <svg viewBox="0 0 64 64" focusable="false" aria-hidden="true">
      @for (s of stones; track $index) {
        <rect [attr.x]="s.x" [attr.y]="s.y" [attr.width]="s.width" [attr.height]="s.height" [attr.rx]="radius" />
      }
    </svg>
  `,
  host: {
    class: 'quader-spinner',
    'aria-hidden': 'true',
    '[class.quader-spinner-page]': 'size() === "page"',
    '[class.quader-spinner-button]': 'size() === "button"',
  },
})
export class Spinner {
  /** `line` beside a sentence (16 px on its line), `button` in a button (its icon's 16 px), `page` for a page or a large area (48 px). */
  readonly size = input<SpinnerSize>('line');

  protected readonly stones = clockwiseStones();
  protected readonly radius = MARK.radius;
}
