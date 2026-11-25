import { Component, inject } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { layoutFindings } from '../../../e2e/renders/layout';
import { DIALOG_DATA, DialogClose, DialogTitle } from '../shared/ui/dialog';
import { openDialog, quiet } from './states';

/**
 * A copy of a dialog as the application builds one (its title, a name with its Copy button in the body, the actions), with the
 * button as W-23 found it: 8 px under the name. The harness's proof that the layout checks measure in this browser.
 */
@Component({
  selector: 'quader-broken-dialog',
  imports: [DialogTitle, DialogClose],
  template: `
    <h2 quaderDialogTitle>Token issued</h2>
    <div class="dialog-body">
      <p class="row">
        <span translate="no">ci-deploy-platform-engineering</span>
        <button class="btn btn-default" type="button" [class.low]="low">Copy</button>
      </p>
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
  `,
})
class BrokenDialog {
  protected readonly low = inject<{ low: boolean }>(DIALOG_DATA).low;
}

async function findingsOf(low: boolean): Promise<string[]> {
  TestBed.resetTestingModule();
  await openDialog(BrokenDialog, { maxWidth: '36rem', data: { low } });
  await quiet();
  const found = layoutFindings().map((f) => `${f.check}: ${f.detail}`);
  TestBed.resetTestingModule();
  return found;
}

describe('the layout checks in the states browser (R-UI-175, R-UI-176)', () => {
  it('a button 8 px under its name in a dialog is off-line, and the same button on the line is no finding', async () => {
    await page.viewport(390, 844);
    const broken = await findingsOf(true);
    expect(broken).toEqual([expect.stringMatching(/^off-line: the middle of "Copy" \(\d+ px high\) is 8\.0 px below the middle of its text's first line$/)]);
    expect(await findingsOf(false)).toEqual([]);
  });
});
