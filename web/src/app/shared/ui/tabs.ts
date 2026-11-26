import { NgTemplateOutlet } from '@angular/common';
import {
  Component,
  Directive,
  TemplateRef,
  computed,
  contentChild,
  contentChildren,
  input,
  model,
  viewChild,
} from '@angular/core';
import { Tab as AriaTab, TabContent as AriaTabContent, TabList, TabPanel, Tabs } from '@angular/aria/tabs';
import { TabStrip } from './tab-strip';

/** Marks the lazy content of a tab: `<ng-template quaderTabContent>` is created when the tab is selected and gone when it is left. */
@Directive({ selector: 'ng-template[quaderTabContent]' })
export class TabContent {}

/**
 * One tab of a `quader-tab-group`: its label and its content. The content is created with the screen, as the tabs it
 * replaces did, and only the selected tab's content is in the page, so a form in another tab keeps what was typed.
 */
@Component({
  selector: 'quader-tab',
  template: `<ng-template><ng-content /></ng-template>`,
})
export class TabPage {
  readonly label = input.required<string>();
  /** A count shown in the strip as a neutral badge after the label (R-UI-062); `null` shows none. */
  readonly count = input<number | null>(null);
  readonly eager = viewChild.required(TemplateRef);
  readonly lazy = contentChild(TabContent, { read: TemplateRef });
}

/**
 * Tabs that switch a view in place (Q-UI-126). The strip is the reference's tab strip (`.tab-nav`, `.tab-link` of
 * `_controls.scss`); the roles, the arrow keys, Home and End and the roving tab stop are `@angular/aria`'s
 * WAI-ARIA tabs pattern, and the selected tab's panel is a `tabpanel` named by its tab. Tabs that are links to
 * routes are plain `.tab-nav` links instead. Either strip scrolls as `TabStrip` has it where its tabs do not fit (Q-UI-1074).
 *
 * An action of the whole group (the panel of a version's commands: **Set up client**, Q-UI-1240) is projected with `tabsEnd` and stands
 * at the end of the strip's row, the free room between; where the row is narrower than the tabs and the action, the action goes to a
 * line of its own under the strip, at its end. `keepAll` keeps every tab's content in the page, in one cell, the tabs that are not
 * selected hidden and inert: the group is as wide and as tall as its largest tab, so switching tabs moves nothing (a panel hung from a
 * button, sized by its longest command). `compact` takes the tabs' padding above and below from 16 to 8 px, for a strip in a panel.
 *
 *   <quader-tab-group [(selectedIndex)]="index"><quader-tab label="General">…</quader-tab></quader-tab-group>
 */
@Component({
  selector: 'quader-tab-group',
  imports: [NgTemplateOutlet, Tabs, TabList, AriaTab, TabPanel, AriaTabContent, TabStrip],
  template: `
    <div ngTabs>
      <div class="tab-row">
      <div
        ngTabList
        quaderTabStrip
        class="tab-nav"
        [class.compact]="compact()"
        [selectedTab]="active().toString()"
        (selectedTabChange)="select($event)"
      >
        @for (t of tabs(); track t; let i = $index) {
          <button ngTab class="tab-link" type="button" [value]="i.toString()">
            {{ t.label() }}
            @if (t.count() !== null) {
              <span class="badge">{{ t.count() }}</span>
            }
          </button>
        }
      </div>
      <ng-content select="[tabsEnd]" />
      </div>
      <!--
        Every tab has its panel, so each tab names the panel it controls; a panel holds its tab's content itself, the selected one's
        (every one's with keepAll). The empty ngTabContent is what the directive asks of a panel, and creates nothing.
      -->
      @if (keepAll()) {
        <div class="tab-panels">
          @for (t of tabs(); track t; let i = $index) {
            <div ngTabPanel [value]="i.toString()">
              <ng-template ngTabContent />
              <ng-container [ngTemplateOutlet]="t.lazy() ?? t.eager()" />
            </div>
          }
        </div>
      } @else {
        @for (t of tabs(); track t; let i = $index) {
          <div ngTabPanel [value]="i.toString()">
            <ng-template ngTabContent />
            @if (i === active()) {
              <ng-container [ngTemplateOutlet]="t.lazy() ?? t.eager()" />
            }
          </div>
        }
      }
    </div>
  `,
  styles: `
    :host {
      display: block;
    }
    .tab-link .badge {
      margin-inline-start: var(--quader-space-1);
    }
    /* The strip and an action of the group on one row, the free room between; the action wraps under the strip where the row has
       no room for both (its host puts it at the end). The strip is as wide as its tabs and grows to the action, so its hairline reaches it; it scrolls
       only where its tabs alone are wider than the row. */
    .tab-row {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-2) var(--quader-space-3);
    }
    .tab-row > .tab-nav {
      flex: 1 1 auto;
      min-inline-size: 0;
      max-inline-size: 100%;
    }
    /* Every tab's content in one cell: the cell is as large as the largest, and the ones not selected are hidden (inert by the
       tabs pattern), so switching moves nothing. */
    .tab-panels {
      display: grid;
    }
    .tab-panels > * {
      grid-area: 1 / 1;
      min-inline-size: 0;
    }
    .tab-panels > [inert] {
      visibility: hidden;
    }
  `,
})
export class TabGroup {
  readonly selectedIndex = model(0);
  /** Every tab's content stays in the page, stacked in one cell; the selected one is shown (Q-UI-1240). */
  readonly keepAll = input(false);
  /** The tabs stand closer above and below: a strip in a panel (Q-UI-1240). */
  readonly compact = input(false);
  protected readonly tabs = contentChildren(TabPage, { descendants: true });

  protected readonly active = computed(() =>
    Math.max(0, Math.min(this.selectedIndex(), this.tabs().length - 1)),
  );

  protected select(value: string | undefined): void {
    if (value !== undefined) this.selectedIndex.set(Number(value));
  }
}
