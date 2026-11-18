import { Component, input, model, output, viewChild } from '@angular/core';
import { QuaderDrawer } from './drawer';

let nextPanelId = 1;

/**
 * The button above a list that opens its filter drawer (`<quader-filter-panel>`, Q-UI-420). It says that it opens a dialog,
 * whether it is open, and the number of filters that are set, as text as well as a badge.
 */
@Component({
  selector: 'quader-filter-toggle',
  imports: [],
  template: `
    <button
      type="button"
      class="btn btn-default filter-toggle"
      aria-haspopup="dialog"
      [attr.aria-expanded]="open()"
      [attr.aria-controls]="open() ? controls() : null"
      (click)="open.set(!open())"
    >
      <span class="material-icons" aria-hidden="true">filter_list</span>
      <span i18n="@@filters.toggle">Filters</span>
      @if (activeCount() > 0) {
        <span class="filter-count" [attr.aria-label]="countLabel()">{{ activeCount() }}</span>
      }
    </button>
  `,
  host: { class: 'quader-filter-toggle' },
})
export class FilterToggle {
  readonly open = model(false);
  readonly activeCount = input(0);
  /** The id of the drawer this button opens (`FilterPanel.panelId`). */
  readonly controls = input<string | null>(null);

  protected countLabel(): string {
    return $localize`:@@filters.active-count:${this.activeCount()}:count: filters active`;
  }
}

/**
 * One filter of the drawer whose control has no visible label of its own (a group of checkboxes, a select or a field named only
 * by `aria-label`): the filter's name as the group's legend, the controls under it. The drawer's filters stand open, one under the
 * other (Q-UI-423); a filter whose control is a labelled field (`<quader-text-field>`, `<quader-select-field>`, a `.field`) needs
 * no group.
 */
@Component({
  selector: 'quader-filter-category',
  template: `
    <fieldset class="filter-group">
      <legend class="field-label">{{ label() }}</legend>
      <ng-content />
    </fieldset>
  `,
  host: { class: 'quader-filter-category' },
})
export class FilterCategory {
  readonly label = input.required<string>();
}

/**
 * The filters of a list in a drawer that slides in from the right over the page (Q-UI-420, ADR-200), so the list keeps its width
 * and its columns: the heading **Filters**, **Clear all**, the close button, the filters as content, one under the other and open
 * (Q-UI-423), and the apply button at the drawer's foot, which submits the form and closes the drawer. Focus, Escape, the return of
 * the focus and the page that does not scroll behind it are the drawer's (`<quader-drawer>`).
 *
 * The panel owns no filter values: the screen keeps them as signals (or in the query parameters), stages the drawer's controls,
 * and reacts to `apply`, `clearAll` and `dismissed` (closed without applying: the screen puts the staged values back to the
 * applied ones). Every control in it must have a working backend behind it; the panel adds no filter of its own. The search by
 * name is not in the drawer: it is the field of the list's head (`<quader-list-head>`, Q-UI-421).
 */
@Component({
  selector: 'quader-filter-panel',
  imports: [QuaderDrawer],
  template: `
    <quader-drawer
      #drawer
      [(open)]="open"
      [drawerId]="panelId"
      [heading]="heading"
      [closeLabel]="closeLabel"
      (closed)="onClosed()"
    >
      <button
        drawerActions
        type="button"
        class="btn btn-tertiary clear-all"
        [disabled]="activeCount() === 0"
        (click)="clearAll.emit()"
        i18n="@@filters.clear-all"
      >Clear all</button>
      <form class="filter-form" role="search" [attr.aria-label]="heading" (submit)="submit($event)">
        <div class="filter-body">
          <ng-content />
        </div>
        <footer class="filter-foot">
          <button class="btn btn-confirm" type="submit">{{ applyLabel() }}</button>
        </footer>
      </form>
    </quader-drawer>
  `,
  host: { class: 'quader-filter-panel' },
})
export class FilterPanel {
  private readonly drawer = viewChild.required<QuaderDrawer>('drawer');

  readonly open = model(false);
  /** The number of filters that are set: **Clear all** has nothing to do without one. */
  readonly activeCount = input(0);
  readonly applyLabel = input($localize`:@@filters.apply:Apply filters`);
  readonly apply = output<void>();
  readonly clearAll = output<void>();
  /** The drawer closed without **Apply**: its staged values are void. */
  readonly dismissed = output<void>();

  /** The id of the drawer's dialog; `<quader-filter-toggle [controls]>` points at it. */
  readonly panelId = `quader-filter-panel-${nextPanelId++}`;
  protected readonly heading = $localize`:@@filters.title:Filters`;
  protected readonly closeLabel = $localize`:@@filters.close:Close filters`;

  private applied = false;

  protected submit(event: Event): void {
    event.preventDefault();
    this.applied = true;
    this.apply.emit();
    this.drawer().close();
  }

  protected onClosed(): void {
    if (!this.applied) this.dismissed.emit();
    this.applied = false;
  }
}
