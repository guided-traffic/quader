import {
  ApplicationRef,
  Component,
  ComponentRef,
  DestroyRef,
  ElementRef,
  EnvironmentInjector,
  afterRenderEffect,
  computed,
  createComponent,
  inject,
  input,
  ViewEncapsulation,
} from '@angular/core';
import { GuideSection } from '../../api/guides-api';
import { PackageReadme } from '../../api/packages-api';
import { CopyButton } from '../../shared/copy-button';
import { Disclosure } from '../../shared/ui/disclosure';
import { guideTitle } from './guide-titles';
import { PackageReadmeView } from '../../views/packages/package-readme';

/**
 * The explanation of a guide below its snippets (R-UI-121, R-UI-125, Q-UI-805): under the page's heading **Guide**, every section is
 * a closed row titled with its heading, in the server's topic order, the first one ("What a local repository is") too, so the
 * titles are the contents and nothing stands open above or between them. The HTML is the server's, bound like a README (R-UI-008,
 * the same component and finishing pass); the prose keeps a measure, and every code block of it gets a copy button. The text is the
 * format's and English (Q-UI-86), so it carries `lang="en"`; the frame is translated, the sections' headings too (`guideTitle`).
 */
@Component({
  selector: 'quader-guide-view',
  imports: [Disclosure, PackageReadmeView],
  template: `
    @for (s of sections(); track s.id) {
      @let title = titleOf(s);
      <quader-disclosure class="guide-more" [level]="3" [heading]="title.text" [attr.lang]="title.english ? 'en' : null" [attr.data-section]="s.id">
        <quader-package-readme class="guide-prose" [readme]="readmeOf(s)" lang="en" />
      </quader-disclosure>
    }
  `,
  styleUrl: './guide-view.scss',
  encapsulation: ViewEncapsulation.None,
})
export class GuideView {
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly appRef = inject(ApplicationRef);
  private readonly environment = inject(EnvironmentInjector);

  readonly sections = input.required<readonly GuideSection[]>();

  private readonly readmes = computed(
    () =>
      new Map(
        this.sections().map((s) => [
          s.id,
          { contentType: 'text/markdown', html: s.html, truncated: false, externalImagesBlocked: 0 } satisfies PackageReadme,
        ]),
      ),
  );
  private readonly copyLabel = $localize`:@@guide.copy-code:Copy code`;
  private readonly buttons: ComponentRef<CopyButton>[] = [];

  constructor() {
    // A command of the prose is copied like a snippet (R-UI-125): each code block gets the copy button of the snippets.
    afterRenderEffect(() => {
      this.sections();
      // Each cell of a table carries its column's name, which a narrow page shows before the value when the rows stand as blocks
      // (guide-view.scss, Q-UI-971).
      for (const table of this.host.nativeElement.querySelectorAll<HTMLTableElement>('.readme table')) {
        const names = Array.from(table.tHead?.rows[0]?.cells ?? []).map((c) => (c.textContent ?? '').trim());
        for (const body of Array.from(table.tBodies)) {
          for (const row of Array.from(body.rows)) {
            Array.from(row.cells).forEach((cell, i) => {
              const name = names[i];
              if (name) cell.setAttribute('data-label', name);
            });
          }
        }
      }
      for (const pre of this.host.nativeElement.querySelectorAll<HTMLPreElement>('.readme pre')) {
        if (pre.parentElement?.classList.contains('prose-code')) continue;
        const wrap = document.createElement('div');
        wrap.className = 'prose-code';
        pre.replaceWith(wrap);
        wrap.append(pre);
        const slot = document.createElement('quader-copy-button');
        wrap.append(slot);
        const ref = createComponent(CopyButton, { environmentInjector: this.environment, hostElement: slot });
        ref.setInput('value', (pre.textContent ?? '').replace(/\n$/, ''));
        ref.setInput('label', this.copyLabel);
        this.appRef.attachView(ref.hostView);
        ref.changeDetectorRef.detectChanges();
        this.buttons.push(ref);
      }
    });
    inject(DestroyRef).onDestroy(() => {
      for (const ref of this.buttons) ref.destroy();
    });
  }

  protected readonly titleOf = guideTitle;

  protected readmeOf(s: GuideSection): PackageReadme | null {
    return this.readmes().get(s.id) ?? null;
  }
}

