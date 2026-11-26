import { Component, ViewEncapsulation, input } from '@angular/core';
import { AddressField } from '../address-field';

/**
 * The header of a page (Q-UI-307, the style guide's anatomy of a page): the object's name as the `<h1>`, one muted sentence,
 * the facts that name it (projected with `meta`, the format and class badges for example), the client address field on the page
 * Set up client (`addresses`, R-UI-169), and the page's own actions as buttons at the right end (projected with `actions`: at
 * most one `btn btn-confirm`, `btn btn-default` beside it), which go under the rest where the page is narrow. Views of the same
 * object are the page's tabs, under the header; a way up to a list is the breadcrumb. A list's page may show its number of
 * entries after the heading (`count`). A picture of the page (projected with `art`, the flow on Set up client, Q-UI-800) stands at the
 * row's end and decides itself where it shows. The object's own picture (projected with `picture`, a package's icon, R-NUGET-038) stands
 * before the heading.
 */
@Component({
  selector: 'quader-page-header',
  imports: [AddressField],
  encapsulation: ViewEncapsulation.None,
  template: `
    <header class="page-header-row">
      <div class="page-header-main">
        <div class="page-header-title">
          <ng-content select="[picture]" />
          <h1 tabindex="-1" [attr.translate]="name() ? 'no' : null"
            >{{ heading() }}@if (count() !== null) {&ngsp;<span class="badge heading-count" translate="no">{{ count() }}</span>}</h1
          >
        </div>
        @if (description()) {
          <p class="page-header-description" [attr.translate]="descriptionAsWritten() ? 'no' : null">{{ description() }}</p>
        }
        <ng-content select="[meta]" />
        @for (a of addresses(); track a) {
          <quader-address-field [value]="a" [label]="addressLabel()" [copyLabel]="copyLabel()" />
        }
      </div>
      <div class="page-header-actions">
        <ng-content select="[actions]" />
      </div>
      <ng-content select="[art]" />
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
      min-inline-size: 0;
      overflow-wrap: anywhere;
    }

    /* The heading, and before it the object's own picture where it has one (projected with \`picture\`: a package's icon, R-NUGET-038),
       centred on the heading's first line. */
    quader-page-header .page-header-title {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-3);
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

    /* The actions stand on the middle of the heading's first line (R-UI-175): a box as high as that line (main.scss: the heading's
       size at a line of 1.25) centres them on it; at the row's top they stood 2 px above it, a small button 6 px. */
    quader-page-header .page-header-actions {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-2);
      min-block-size: calc(var(--quader-heading-1) * 1.25);
    }

    quader-page-header .page-header-actions:empty {
      display: none;
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
  /**
   * The description is what a publisher or an upstream wrote (a package's description, an advisory's summary): it is shown as it
   * came and not translated (R-UI-214).
   */
  readonly descriptionAsWritten = input(false);
  /** The object's client addresses, each one field (R-UI-065, Q-UI-301); none when empty. */
  readonly addresses = input<readonly string[]>([]);
  readonly addressLabel = input($localize`:@@address.label:Client address`);
  readonly copyLabel = input($localize`:@@address.copy:Copy client address`);
}
