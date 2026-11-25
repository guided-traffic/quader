import { Component, input } from '@angular/core';
import { describe, it } from 'vitest';
import { expectState, openInPage, stable } from '../../testing/states';
import { DistTags } from './dist-tags';

/**
 * A version's npm tags at the right end of a row, where the tab Upstream and a package's versions put them (R-UI-110, Q-UI-1190,
 * Q-UI-1191): five tags on one version, one of them long, so the row shows three and "+2 more"; a badge's tooltip names the install
 * command it stands for, "+2 more"'s the tags it folds. The same component on every screen: a state of the component, not of a format.
 */
@Component({
  selector: 'quader-dist-tags-row',
  imports: [DistTags],
  template: `
    <div class="row">
      <span class="version" translate="no">5.0.0-beta.2</span>
      <span class="tags"><quader-dist-tags [tags]="tags" [package]="name()" /></span>
    </div>
  `,
  styles: `
    .row {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: var(--quader-space-2);
    }
    .version {
      flex: none;
      white-space: nowrap;
    }
    .tags {
      display: flex;
      flex-wrap: wrap;
      justify-content: flex-end;
      gap: var(--quader-space-1);
      min-inline-size: 0;
    }
  `,
})
class DistTagsRow {
  readonly name = input.required<string>();
  protected readonly tags = ['next', 'beta', 'canary', 'eap-ai-hybridinference-experimental', 'rc'];
}

/** The longest name of the seed's npm packages of the upstream walk (renders/upstream.ts). */
const PACKAGE = '@customer-portal-release-engineering/gitlab-merge-request-pipeline-status-reporter-for-the-frontend-monorepo';

async function focused(which: 'badge' | 'more'): Promise<void> {
  const fixture = await openInPage(DistTagsRow, { name: PACKAGE });
  const host = fixture.nativeElement as HTMLElement;
  const target = which === 'badge' ? host.querySelectorAll<HTMLElement>('.dist-tag')[2] : host.querySelector<HTMLElement>('.more');
  target!.focus();
  await stable(fixture);
  if (!document.querySelector('quader-tooltip')) throw new Error('the tooltip did not open');
}

describe("a version's npm tags in a browser (R-UI-110, R-UI-044, R-UI-173, R-UI-175)", () => {
  it("a tag's tooltip names its install command of the longest package name, at the window's right edge", async () => {
    await expectState('dist-tag-tooltip', () => focused('badge'));
  });

  it('"+2 more" names the tags it folds', async () => {
    await expectState('dist-tags-more', () => focused('more'));
  });
});
