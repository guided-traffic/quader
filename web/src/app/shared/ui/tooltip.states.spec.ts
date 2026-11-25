import { Component, input } from '@angular/core';
import { describe, it } from 'vitest';
import { expectState, openInPage, stable } from '../../testing/states';
import { Tooltip } from './tooltip';

/**
 * An element with a tooltip at the right edge of the page, as a list's last column (its help, a row's Rotate) stands there: at the
 * top of the window, where the bubble has no room above and goes under, and lower down, where it stands above.
 */
@Component({
  selector: 'quader-tooltip-edge',
  imports: [Tooltip],
  template: `
    <div class="row" [class.low]="low()">
      <span>Tier</span>
      <button class="btn btn-icon" type="button" aria-label="About the column Tier" [quaderTooltip]="text()">
        <span class="material-icons" aria-hidden="true">help_outline</span>
      </button>
    </div>
  `,
  styles: `
    .row {
      display: flex;
      justify-content: space-between;
      align-items: center;
    }
    .low {
      margin-block-start: 20rem;
    }
  `,
})
class TooltipEdge {
  readonly text = input.required<string>();
  readonly low = input(false);
}

/** The longest texts of the tooltips (data-table column help, limit-rules.ts, token-table.ts), as the screens set them. */
const LONGEST_HELP =
  "The system tier is the administrators'. A project's administrators manage the tier of their project. The tiers are evaluated separately and their matches combined.";
const CLAMPED =
  "Lowered to what the instance or the project's maximum allows: 400 requests per second; 2,000 in a burst; 1,000 GB per hour from the cache; 5,000 GB per day.";

async function shown(text: string, low: boolean): Promise<void> {
  const fixture = await openInPage(TooltipEdge, { text, low });
  (fixture.nativeElement as HTMLElement).querySelector<HTMLButtonElement>('button')!.focus();
  await stable(fixture);
  if (!document.querySelector('quader-tooltip')) throw new Error('the tooltip did not open');
}

describe('tooltips at the right edge of the window in a browser (R-UI-044, R-UI-173, R-UI-175)', () => {
  it("a column's longest help at the window's top right: under its element, inside the window", async () => {
    await expectState('tooltip-edge-top', () => shown(LONGEST_HELP, false));
  });

  it("a rule's lowered figures lower down at the right edge: above its element, inside the window", async () => {
    await expectState('tooltip-edge', () => shown(CLAMPED, true));
  });
});
