import { NgTemplateOutlet } from '@angular/common';
import { Component, Injector, effect, inject, input, model, untracked } from '@angular/core';
import { DisclosureMemory } from '../../core/disclosure-memory';

let nextDisclosureId = 1;

/**
 * The one collapsible section of every screen (Q-UI-322, Q-UI-319, Q-UI-552): the details of a content page, the groups of a
 * settings page and of an editor, closed at first unless the screen opens it. A small heading at the text's size with a chevron
 * before it, one muted line of hint, and the content that folds with the animation of the style guide.
 *
 * `inline` is the same toggle without the section around it: no heading element, no rule above it, no room above and below. It
 * folds a detail inside a section or a table cell (a pattern syntax, a request body, a certificate, the messages of a row). The
 * app has no bare details element (a lint rule refuses one): every fold is this component, so every fold has the one chevron.
 *
 * `remember` names a **Details** section by its screen (`file.details`): it opens as the viewer left it (R-UI-067, Q-UI-319,
 * Q-UI-967), kept in the browser per user by `DisclosureMemory`.
 */
@Component({
  selector: 'quader-disclosure',
  host: { '[class.inline]': 'inline()' },
  template: `
    <ng-template #toggle>
      <button
        [class.toggle]="!inline()"
        [class.inline-toggle]="inline()"
        type="button"
        [attr.aria-expanded]="expanded()"
        [attr.aria-controls]="bodyId"
        (click)="flip()"
      >
        <span class="material-icons chevron" aria-hidden="true">chevron_right</span>
        <span>{{ heading() }}</span>
        <!-- The space is the heard one between the heading and its number ("System repositories 2"); a flex row gives it no room. -->
        @if (count() !== null) {
          &ngsp;<span class="badge heading-count" translate="no">{{ count() }}</span>
        }
        @if (note()) {
          <span class="note">{{ note() }}</span>
        }
      </button>
    </ng-template>
    <div class="disclosure">
      @if (inline()) {
        <ng-container [ngTemplateOutlet]="toggle" />
      } @else if (level() === 3) {
        <h3 class="heading"><ng-container [ngTemplateOutlet]="toggle" /></h3>
      } @else {
        <h2 class="heading"><ng-container [ngTemplateOutlet]="toggle" /></h2>
      }
      @if (hint()) {
        <p class="hint">{{ hint() }}</p>
      }
      <div class="collapse-region" [class.open]="expanded()" [id]="bodyId">
        <!-- The padding is the inner element's: the element the region folds takes no room when closed, a padding of it would. -->
        <div><div class="body"><ng-content /></div></div>
      </div>
    </div>
  `,
  imports: [NgTemplateOutlet],
  styles: `
    :host {
      display: block;
    }
    .disclosure {
      padding-block: var(--quader-space-3);
      border-block-start: 1px solid var(--quader-border);
    }
    :host(.inline) .disclosure {
      padding-block: 0;
      border-block-start: 0;
    }
    .heading {
      margin: 0;
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    .toggle,
    .inline-toggle {
      display: inline-flex;
      align-items: center;
      gap: var(--quader-space-1);
      padding: 0;
      border: 0;
      background: none;
      color: var(--quader-heading);
      font: inherit;
      cursor: pointer;
    }
    /* Outside a heading the toggle keeps the heading's weight, so a fold reads the same wherever it stands. Its own class keeps it
       out of the list of a page's sections, which reads .toggle. */
    .inline-toggle {
      font-weight: 600;
      text-align: start;
    }
    .toggle:focus-visible,
    .inline-toggle:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
    .chevron {
      font-size: 1.25rem;
      transition: transform var(--quader-motion-duration) var(--quader-motion-easing);
    }
    /* The chevron (20 px) takes 16 px of the line: in a table cell, whose line is 16 px, it is centred on it and adds nothing to
       the row; beside a paragraph's taller line the toggle is that line's height. */
    :host(.inline) .chevron {
      margin-block: -0.125rem;
    }
    .note {
      margin-inline-start: var(--quader-space-2);
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
      font-weight: 400;
    }
    [aria-expanded='true'] > .chevron {
      transform: rotate(90deg);
    }
    .hint {
      margin: var(--quader-space-1) 0 0;
      padding-inline-start: calc(1.25rem + var(--quader-space-1));
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
    }
    .body {
      padding-block-start: var(--quader-space-3);
      padding-inline-start: calc(1.25rem + var(--quader-space-1));
    }
    :host(.inline) .body {
      padding-block-start: var(--quader-space-2);
    }
  `,
})
export class Disclosure {
  readonly heading = input.required<string>();
  /** One muted line under the heading that says what is inside. */
  readonly hint = input('');
  /** A short state after the heading, such as "Unsaved changes" of a closed section that holds one (R-UI-087). */
  readonly note = input('');
  /**
   * The number of entries of a section that is a list, after its heading as the neutral badge of a count, as a page's heading and a
   * tab carry it (Q-UI-550): **System repositories 2** (R-UI-192). `null` shows none.
   */
  readonly count = input<number | null>(null);
  readonly expanded = model(false);
  /** A fold inside a section or a cell: the toggle without a heading, a rule or room around it (Q-UI-552). */
  readonly inline = input(false);
  /** The level of the heading: 3 for the sections under a heading of their own (the **Guide** of Set up client, Q-UI-805). */
  readonly level = input<2 | 3>(2);
  /**
   * The screen and section of a **Details** section whose open state is kept per viewer (Q-UI-967), such as `file.details`; empty
   * for every other fold, which opens as its screen says.
   */
  readonly remember = input('');

  protected readonly bodyId = `quader-disclosure-${nextDisclosureId++}`;
  private readonly injector = inject(Injector);

  constructor() {
    // A section the viewer left open opens again; the store is asked only by a section that is remembered.
    effect(() => {
      const id = this.remember();
      if (!id || !this.memory().isOpen(id)) return;
      untracked(() => {
        if (!this.expanded()) this.expanded.set(true);
      });
    });
  }

  protected flip(): void {
    const open = !this.expanded();
    this.expanded.set(open);
    const id = this.remember();
    if (id) this.memory().set(id, open);
  }

  private memory(): DisclosureMemory {
    return this.injector.get(DisclosureMemory);
  }
}
