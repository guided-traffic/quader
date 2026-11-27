import { Component, ViewEncapsulation, input } from '@angular/core';
import { describe, it } from 'vitest';
import { expectState, openInPage, stable } from '../../testing/states';
import { Setting } from './setting';
import { Tooltip } from './tooltip';
import { Unavailable } from './unavailable';

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

/**
 * A setting row whose name ends at the right edge of the page, so its help sign stands there (R-UI-267): at 390 px the name stands
 * above its control across the whole width.
 */
@Component({
  selector: 'quader-help-edge',
  imports: [Setting],
  // The row's own styles stay the component's; this only moves its name to the end of its column.
  encapsulation: ViewEncapsulation.None,
  template: `
    <quader-setting class="help-edge" label="Direct reads from the object store" [help]="text()">
      <span>Off</span>
    </quader-setting>
  `,
  styles: `
    .help-edge .setting-label {
      text-align: end;
    }
  `,
})
class HelpEdge {
  readonly text = input.required<string>();
}

/**
 * A row whose last act stands at the right edge of the page and cannot act (R-UI-270), as a declared binding's **Unbind** stands at
 * the end of its row: its reason is the tooltip, a tap pins it.
 */
@Component({
  selector: 'quader-unavailable-edge',
  imports: [Unavailable],
  template: `
    <div class="row">
      <span>group:platform-admins</span>
      <button class="btn btn-tertiary" type="button" [quaderUnavailable]="text()">Unbind</button>
    </div>
  `,
  styles: `
    .row {
      display: flex;
      justify-content: space-between;
      align-items: center;
    }
  `,
})
class UnavailableEdge {
  readonly text = input.required<string>();
}

/** The longest reason of an unavailable control, as the CI issuer's Delete says it. */
const ISSUER_DELETE = 'Declared in config.ci.issuers of the Helm configuration: remove it there first, then delete it here.';

/** The longest texts of the tooltips (data-table column help, limit-rules.ts, token-table.ts), as the screens set them. */
const LONGEST_HELP =
  "The system tier is the administrators'. A project's administrators manage the tier of their project. The tiers are evaluated separately and their matches combined.";
const CLAMPED =
  "Lowered to what the instance or the project's maximum allows: 400 requests per second; 2,000 in a burst; 1,000 GB per hour from the cache; 5,000 GB per day.";

/** The help of a Bastion's direct reads (W-39), as the card sets it. */
const HANDOVER_HELP =
  'Large files go from the object store to this Bastion by a link per file. The main installation still decides and counts every download.';

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

  it("R-UI-267 a setting's help sign at the window's right edge, pinned by a tap: the bubble inside the window", async () => {
    await expectState('help-pinned', async () => {
      const fixture = await openInPage(HelpEdge, { text: HANDOVER_HELP });
      (fixture.nativeElement as HTMLElement).querySelector<HTMLButtonElement>('button.help-sign')!.click();
      await stable(fixture);
      if (!document.querySelector('quader-tooltip')) throw new Error('the help did not open');
    });
  });

  it("R-UI-270 an unavailable button at the window's right edge, pinned by a tap: its reason inside the window", async () => {
    await expectState('unavailable-pinned', async () => {
      const fixture = await openInPage(UnavailableEdge, { text: ISSUER_DELETE });
      (fixture.nativeElement as HTMLElement).querySelector<HTMLButtonElement>('button')!.click();
      await stable(fixture);
      if (!document.querySelector('quader-tooltip')) throw new Error('the reason did not open');
    });
  });
});
