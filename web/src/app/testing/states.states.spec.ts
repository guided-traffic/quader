import { Component, inject } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { innerScroll, layoutFindings } from '../../../e2e/renders/layout';
import { DIALOG_DATA, DialogClose, DialogTitle } from '../shared/ui/dialog';
import { openDialog, quiet } from './states';

interface Broken {
  /** Copy 8 px under its name, as W-23 found it. */
  readonly low: boolean;
  /** A block of 40 lines capped at 6 rem in a body that scrolls itself: a scroll inside the dialog's one scroll. */
  readonly capped: boolean;
}

/**
 * A copy of a dialog as the application builds one (its title, a name with its Copy button in the body, the actions), broken on
 * demand. The harness's proof that the layout checks measure in this browser.
 */
@Component({
  selector: 'quader-broken-dialog',
  imports: [DialogTitle, DialogClose],
  template: `
    <h2 quaderDialogTitle>Token issued</h2>
    <div class="dialog-body">
      <p class="row">
        <span translate="no">ci-deploy-platform-engineering</span>
        <button class="btn btn-default" type="button" [class.low]="data.low">Copy</button>
      </p>
      @if (data.capped) {
        <pre class="capped" tabindex="0">{{ lines }}</pre>
        <p class="tall">The claims of the job.</p>
      }
    </div>
    <div class="dialog-actions">
      <button class="btn btn-confirm" type="button" quaderDialogClose>Done</button>
    </div>
  `,
  styles: `
    .row {
      display: flex;
      align-items: center;
      gap: var(--quader-space-2);
    }
    .low {
      position: relative;
      inset-block-start: 8px;
    }
    .capped {
      max-block-size: 6rem;
      overflow: auto;
    }
    .tall {
      block-size: 120vh;
    }
  `,
})
class BrokenDialog {
  protected readonly data = inject<Broken>(DIALOG_DATA);
  protected readonly lines = Array.from({ length: 40 }, (_, i) => `"claim_${i}": "value"`).join('\n');
}

async function findingsOf(data: Broken): Promise<string[]> {
  TestBed.resetTestingModule();
  await openDialog(BrokenDialog, { maxWidth: '36rem', data });
  await quiet();
  const found = [...layoutFindings(), ...innerScroll()].map((f) => `${f.check}: ${f.detail}`);
  TestBed.resetTestingModule();
  return found;
}

describe('the layout checks in the states browser (R-UI-175, R-UI-176, R-UI-215)', () => {
  it('a button 8 px under its name in a dialog is off-line, and the same button on the line is no finding', async () => {
    await page.viewport(390, 844);
    const broken = await findingsOf({ low: true, capped: false });
    expect(broken).toEqual([expect.stringMatching(/^off-line: the middle of "Copy" \(\d+ px high\) is 8\.0 px below the middle of its text's first line$/)]);
    expect(await findingsOf({ low: false, capped: false })).toEqual([]);
  });

  it("a block that scrolls inside a dialog's scrolling body is inner-scroll", async () => {
    await page.viewport(390, 844);
    expect(await findingsOf({ low: false, capped: true })).toEqual([expect.stringMatching(/^inner-scroll: scrolls \d+ px inside div\.dialog-body, which scrolls too$/)]);
  });
});
