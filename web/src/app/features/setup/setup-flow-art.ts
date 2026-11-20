import { Component, computed, input } from '@angular/core';
import { formatGlyph } from '../../shared/format-accent';

interface Stone {
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
  readonly sand: boolean;
}

/**
 * The repository as a piece of Quader's wall: three courses on the mark's grid, a long stone, a short one and a middle one, the
 * middle course with the coloured stone. `u` is one short stone.
 */
export function wallStones(x: number, y: number, u: number): Stone[] {
  const h = u * 0.62;
  const joint = u * 0.14;
  const courses: [readonly number[], number][] = [
    [[2.2, 1, 1.6], -1],
    [[1, 2.2, 1.6], 1],
    [[2.2, 1, 1.6], -1],
  ];
  return courses.flatMap(([widths, sand], row) => {
    let at = x;
    return widths.map((w, i) => {
      const stone = { x: at, y: y + row * (h + joint), w: w * u, h, sand: i === sand };
      at += w * u + joint;
      return stone;
    });
  });
}

/**
 * The picture of the flow in the header of **Set up client** (Q-UI-800): the reader's machine, the token between it and the
 * repository, and the repository as a piece of Quader's wall with parcels on it. The parcels and the glyph on the machine take the
 * format's accent (`--quader-art-accent`, Q-UI-803); the rest is the picture tokens of `_tokens.scss`. The lane to the repository
 * (publish) is drawn only for who may publish, the token only for who is signed in. Inline SVG of Quader's own, no logo of an
 * ecosystem (Q-UI-225 to Q-UI-227); decoration only, so hidden from assistive technology. It shows where the content is at least
 * 55 rem wide, a window of about 1,200 px with the side bar.
 */
@Component({
  selector: 'quader-setup-flow-art',
  template: `
    <svg viewBox="0 0 464 132" aria-hidden="true" focusable="false">
      <rect class="machine" x="10" y="20" width="148" height="104" rx="10" />
      <rect class="bar" x="10" y="20" width="148" height="20" rx="10" />
      <rect class="bar" x="10" y="30" width="148" height="10" />
      <rect class="tab" x="20" y="27" width="40" height="6" rx="3" />
      <rect class="s-num" x="22" y="54" width="6" height="6" rx="1.5" />
      <rect class="s-command" x="34" y="54" width="34" height="6" rx="3" />
      <rect class="s-dim" x="74" y="54" width="46" height="6" rx="3" />
      <rect class="s-key" x="34" y="70" width="26" height="6" rx="3" />
      <rect class="s-url" x="66" y="70" width="64" height="6" rx="3" />
      <rect class="s-num" x="22" y="86" width="6" height="6" rx="1.5" />
      <rect class="s-command" x="34" y="86" width="30" height="6" rx="3" />
      <rect class="s-token" x="70" y="86" width="38" height="6" rx="3" />
      <rect class="s-dim" x="34" y="100" width="7" height="11" rx="1" />
      <svg x="118" y="84" width="30" height="30" viewBox="0 0 24 24" class="glyph">
        @if (glyph() === 'layers') {
          <path d="M12 3l9 4.5-9 4.5-9-4.5z" />
          <path d="M3 12l9 4.5 9-4.5" />
          <path d="M3 16.5l9 4.5 9-4.5" />
        } @else {
          <path d="M12 2.5l8.5 4.7v9.6L12 21.5l-8.5-4.7V7.2z" />
          <path d="M3.5 7.2L12 12l8.5-4.8M12 12v9.5" />
          <path d="M7.7 4.9l8.5 4.8v3.3" />
        }
      </svg>
      @if (publish()) {
        <path class="lane" d="M170 58H200M264 58H296" />
        <path class="arrow" d="M290 52l7 6-7 6" />
        <rect class="parcel" x="176" y="44" width="12" height="12" rx="1.5" />
        <rect class="parcel-shade" x="180.8" y="44" width="2.4" height="12" />
      }
      <path class="lane" d="M296 92H264M200 92H170" />
      <path class="arrow" d="M176 86l-7 6 7 6" />
      <rect class="parcel" x="274" y="86" width="12" height="12" rx="1.5" />
      <rect class="parcel-shade" x="278.8" y="86" width="2.4" height="12" />
      @if (token()) {
        <circle class="paper" cx="232" cy="75" r="26" />
        <g class="key" transform="translate(214 59) scale(2.2)">
          <circle cx="5" cy="8" r="2.8" />
          <path d="M7.8 8h6.2M11.5 8v2.5M13.5 8v1.8" />
        </g>
      } @else {
        <path class="lane" d="M200 92H264" />
      }
      @for (p of parcels; track $index) {
        <rect class="parcel" [attr.x]="p.x" [attr.y]="p.y" width="24" height="24" rx="3" />
        <rect class="parcel-shade" [attr.x]="p.x + 9.6" [attr.y]="p.y" width="4.8" height="24" />
      }
      @for (s of stones; track $index) {
        <rect [class]="s.sand ? 'sand' : 'stone'" [attr.x]="s.x" [attr.y]="s.y" [attr.width]="s.w" [attr.height]="s.h" rx="2" />
      }
    </svg>
  `,
  styles: `
    :host {
      display: none;
      flex: none;
      inline-size: 15.5rem;
      margin-block-start: var(--quader-space-2);
    }
    @container content (min-width: 55rem) {
      :host {
        display: block;
      }
    }
    svg {
      display: block;
      inline-size: 100%;
      block-size: auto;
    }
    .machine {
      fill: var(--quader-art-machine);
      stroke: var(--quader-art-machine-edge);
    }
    .bar {
      fill: var(--quader-art-bar);
    }
    .tab {
      fill: var(--quader-art-tab);
    }
    .s-num {
      fill: var(--quader-code-gutter);
    }
    .s-command {
      fill: var(--quader-code-command);
    }
    .s-dim {
      fill: var(--quader-code-dim);
    }
    .s-key {
      fill: var(--quader-code-key);
    }
    .s-url {
      fill: var(--quader-code-url);
    }
    .s-token {
      fill: var(--quader-code-token-fg);
    }
    .glyph {
      fill: none;
      stroke: var(--quader-art-accent, var(--quader-format-accent));
      stroke-width: 1.8;
      stroke-linecap: round;
      stroke-linejoin: round;
    }
    .lane {
      fill: none;
      stroke: var(--quader-art-line);
      stroke-width: 1.5;
      stroke-dasharray: 3 4;
      stroke-linecap: round;
    }
    .arrow {
      fill: none;
      stroke: var(--quader-art-line);
      stroke-width: 1.8;
      stroke-linecap: round;
      stroke-linejoin: round;
    }
    .parcel {
      fill: var(--quader-art-accent, var(--quader-format-accent));
    }
    .parcel-shade {
      fill: var(--quader-art-shade);
    }
    .paper {
      fill: var(--quader-art-paper);
      stroke: var(--quader-art-line);
      stroke-width: 1.5;
    }
    .key {
      fill: none;
      stroke: var(--quader-art-key);
      stroke-width: 1.3;
      stroke-linecap: round;
      stroke-linejoin: round;
    }
    .stone {
      fill: var(--quader-mark-stone);
    }
    .sand {
      fill: var(--quader-mark-accent);
    }
  `,
})
export class SetupFlowArt {
  /** The format, for its glyph; its accent comes from the page (`--quader-art-accent`). */
  readonly format = input<string | null>(null);
  /** Whether the reader may publish: the lane to the repository. */
  readonly publish = input(false);
  /** Whether the reader is signed in: the token between the machine and the repository. */
  readonly token = input(true);

  protected readonly glyph = computed(() => formatGlyph(this.format()));
  protected readonly parcels = [
    { x: 322, y: 36 },
    { x: 350, y: 36 },
    { x: 336, y: 10 },
    { x: 398, y: 36 },
  ];
  protected readonly stones = wallStones(308, 62, 28);
}
