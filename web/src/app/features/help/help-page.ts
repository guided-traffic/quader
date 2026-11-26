import { Component, ElementRef, afterRenderEffect, computed, inject, input, resource, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, RouterLink, RouterLinkActive } from '@angular/router';
import { ManualApi } from '../../api/manual-api';
import { PackageReadme } from '../../api/packages-api';
import { ErrorNotice } from '../../shared/error-notice';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { PackageReadmeView } from '../../views/packages/package-readme';
import { Loading } from '../../shared/ui/loading';

/**
 * A page of the manual (R-UI-050): the contents of both guides beside the page, the page's
 * headings as "On this page", its text, and the links to the previous and the next page. The
 * server renders and sanitizes the text; it is bound like a README (R-UI-008), and its links to
 * other pages (`help/<section>/<page>`) open in the application. A fragment in the URL, from such
 * a link, scrolls to its heading once the page is shown.
 */
@Component({
  selector: 'quader-help-page',
  imports: [Loading, RouterLink, RouterLinkActive, ErrorNotice, PackageReadmeView],
  templateUrl: './help-page.html',
  styleUrl: './help-page.scss',
})
export class HelpPage {
  private readonly api = inject(ManualApi);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  /** Path parameters, bound by the router's component input binding. */
  readonly section = input.required<string>();
  readonly page = input.required<string>();

  private readonly fragment = toSignal(inject(ActivatedRoute).fragment, { initialValue: null });

  protected readonly contents = resource({ loader: () => this.api.contents() });
  protected readonly answer = resource({
    params: () => ({ section: this.section(), page: this.page() }),
    loader: ({ params }) => this.api.page(params.section, params.page),
  });
  protected readonly toc = computed(() => valueOf(this.contents));
  protected readonly current = computed(() => valueOf(this.answer));
  protected readonly readme = computed<PackageReadme | null>(() => {
    const p = this.current();
    return p ? { contentType: 'text/markdown', html: p.html, truncated: false, externalImagesBlocked: 0 } : null;
  });
  /**
   * The failure of the page's read, kept while its notice reads it again (R-UI-216); the contents beside it, whose failure leaves
   * the list of pages empty, are read again with it.
   */
  protected readonly failure = failureOf(this.answer, { also: [this.contents] });
  protected readonly prevLabel = $localize`:@@help.previous:Previous page`;
  protected readonly nextLabel = $localize`:@@help.next:Next page`;

  constructor() {
    // A link from another page names a heading; scroll to it once the text is in the document.
    afterRenderEffect(() => {
      const fragment = this.fragment();
      if (!this.current() || !fragment) return;
      untracked(() => this.scrollTo(fragment));
    });
  }

  /** "On this page" scrolls; the document's base href would turn a bare fragment into a navigation. */
  protected jump(event: Event, id: string): void {
    event.preventDefault();
    this.scrollTo(id);
  }

  private scrollTo(id: string): void {
    const target = Array.from(this.host.nativeElement.querySelectorAll<HTMLElement>('.readme [id]')).find((e) => e.id === id);
    target?.scrollIntoView?.({ block: 'start' });
  }
}
