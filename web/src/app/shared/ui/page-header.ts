import { Component, ViewEncapsulation, input } from '@angular/core';
import { AddressField } from '../address-field';

/**
 * The header of a page (Q-UI-307, the style guide's anatomy of a page): the object's name as the `<h1>`, one muted sentence,
 * the facts that name it (projected with `meta`, the format and class badges for example), its client address field where the
 * object has one, and the page's own actions as buttons at the right end (projected with `actions`: at most one
 * `btn btn-confirm`, `btn btn-default` beside it). Views of the same object are the page's tabs, under the header; a way up to a
 * list is the breadcrumb. A list's page may show its number of entries after the heading (`count`). Where the page is narrow, a
 * header with a client row puts the page's actions on the line of Set up client (Q-UI-554).
 */
@Component({
  selector: 'quader-page-header',
  imports: [AddressField],
  encapsulation: ViewEncapsulation.None,
  template: `
    <header class="page-header-row">
      <div class="page-header-main">
        <h1 tabindex="-1" [attr.translate]="name() ? 'no' : null"
          >{{ heading() }}@if (count() !== null) {&ngsp;<span class="badge heading-count" translate="no">{{ count() }}</span>}</h1
        >
        @if (description()) {
          <p class="page-header-description">{{ description() }}</p>
        }
        <ng-content select="[meta]" />
        @for (a of addresses(); track a) {
          <quader-address-field [value]="a" [label]="addressLabel()" [copyLabel]="copyLabel()" />
        }
      </div>
      <div class="page-header-actions">
        <ng-content select="[actions]" />
      </div>
    </header>
  `,
  styles: `
    quader-page-header {
      display: block;
      margin-block-end: var(--quader-space-4);
    }

    quader-page-header .page-header-row {
      display: flex;
      flex-wrap: wrap;
      align-items: flex-start;
      gap: var(--quader-space-3) var(--quader-space-4);
    }

    quader-page-header .page-header-main {
      display: flex;
      flex: 1 1 24rem;
      flex-direction: column;
      gap: var(--quader-space-2);
      min-inline-size: 0;
    }

    quader-page-header h1 {
      margin: 0;
      overflow-wrap: anywhere;
    }

    /* The number of the list's entries after its noun ("Members 2", Q-UI-550): the neutral badge of a count, as a tab carries it,
       a space and 4 px after the word. */
    quader-page-header h1 .heading-count {
      margin-inline-start: var(--quader-space-1);
    }

    quader-page-header .page-header-description {
      margin: 0;
      color: var(--quader-text-muted);
    }

    /* A badge that names the kind of a detail page's object keeps its own width (Q-UI-307). */
    quader-page-header .page-header-main > .badge {
      align-self: flex-start;
    }

    quader-page-header .page-header-actions {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-2);
    }

    quader-page-header .page-header-actions:empty {
      display: none;
    }

    /* A header with a client row (the address field and Set up client, Q-UI-517), where it is narrow (Q-UI-554): the row is one
       flow instead of two blocks. The heading, the sentence, the facts and the address field each take a line; Set up client and
       the page's actions share the next, the actions as one unit, so that the menu button never stands alone on a line. At
       390 px that line is Set up client, Settings and the menu, about 250 px of the 358 px there are. The order of the elements,
       and so the order of the focus, is the order of the wide layout. */
    @container content (max-width: 36rem) {
      quader-page-header:has(.client-row) .page-header-row {
        align-items: center;
        gap: var(--quader-space-2);
      }

      quader-page-header:has(.client-row) :is(.page-header-main, .client-row) {
        display: contents;
      }

      quader-page-header:has(.client-row) .page-header-main > :not(.client-row) {
        flex: 1 1 100%;
        min-inline-size: 0;
      }

      quader-page-header:has(.client-row) .client-row quader-address-field {
        flex: 1 1 100%;
        max-inline-size: none;
      }

      quader-page-header:has(.client-row) .page-header-actions {
        flex: none;
        flex-wrap: nowrap;
      }
    }
  `,
})
export class PageHeader {
  /** The `<h1>`: the object's name on the page of an object, the list's noun on a list. */
  readonly heading = input.required<string>();
  /** The heading is a name (a key, a package name): it is not translated. */
  readonly name = input(false);
  /** A list's number of entries, a neutral badge after the heading (Q-UI-550); none when null. */
  readonly count = input<number | null>(null);
  /** One muted sentence under the heading; nothing when empty. */
  readonly description = input<string | null | undefined>('');
  /** The object's client addresses, each one field (R-UI-065, Q-UI-301); none when empty. */
  readonly addresses = input<readonly string[]>([]);
  readonly addressLabel = input($localize`:@@address.label:Client address`);
  readonly copyLabel = input($localize`:@@address.copy:Copy client address`);
}
