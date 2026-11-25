import {
  Component,
  ElementRef,
  ViewEncapsulation,
  afterRenderEffect,
  inject,
  input,
} from '@angular/core';
import { Location } from '@angular/common';
import { Router, UrlTree } from '@angular/router';
import { PackageReadme } from '../../api/packages-api';
import { EXTERNAL_REL } from './package-links';

/** The prefix of every heading anchor of a README (R-UI-008). */
const USER_CONTENT = 'user-content-';

/** A link target a README may keep: a fragment, http, https or mailto. */
const LINK_TARGET = /^(#|https?:|mailto:)/i;
/**
 * A link of the Ansible documentation to itself (R-ANS-048, Q-ANS-94), the grammar the server builds and its sanitizer keeps: an entry
 * (`?doc=<type>/<name>`), an option or a return value of the open entry (`#parameter-<path>`, `#return-<path>`), or both.
 */
const DOC_LINK = /^(\?doc=([a-z_]{1,32}\/[a-z_][a-z0-9_.]{0,255}))?(#((?:parameter|return)-[A-Za-z0-9_-]+(?:\/[A-Za-z0-9_-]+)*))?$/;
/** An image source a README may keep: an https address, or a path on this origin the renderer generated. */
const IMAGE_SOURCE = /^(https:\/\/|\/(?!\/))/i;

/**
 * The anchor name of a heading, the way GitHub-style renderers spell it: lower case, punctuation
 * dropped, spaces to hyphens. `seen` counts a repeated name so that the second `Usage` is `usage-1`.
 */
export function headingSlug(heading: string, seen: Map<string, number>): string {
  const base = heading
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s_-]/gu, '')
    .replace(/\s/g, '-');
  const n = seen.get(base) ?? 0;
  seen.set(base, n + 1);
  return n === 0 ? base : `${base}-${n}`;
}

/**
 * The README of a version (R-UI-008). The server renders Markdown and sanitizes the result; this
 * component binds that HTML with `[innerHTML]`, so Angular's own sanitizer runs a second time and
 * the Trusted Types policy of the document is the third layer. It never builds HTML from Markdown
 * or from strings. Because Angular's sanitizer is the second layer, it also removes every `id` and
 * `name` and lets `vbscript:` links through; the component therefore finishes the job:
 *
 * - a link target other than a fragment, http, https or mailto is removed (the text stays); links
 *   that leave Quader carry `rel="nofollow noopener noreferrer"` and no `target`;
 * - an image whose source is neither an https address nor a path of this origin is removed;
 * - each heading gets the anchor `user-content-<slug>` again, so that `#user-content-…` links of a
 *   table of contents work. The server derives its heading anchors with the same rule ([headingSlug]);
 * - such a link scrolls inside the README and does not navigate, because the document's
 *   `<base href>` would turn a bare fragment into a navigation to the UI root;
 * - with `pageLinks`, a link whose target starts with that prefix stays and opens in the application:
 *   the manual's links to its own pages (`help/<section>/<page>`, R-UI-050), which resolve against the
 *   base href when they are opened in a new tab;
 * - code blocks can take focus, so that a keyboard user can scroll them sideways;
 * - a table sits in a `.readme-table` container that takes the full width and scrolls sideways when
 *   the table is wider, and that can take focus for the same reason.
 */
@Component({
  selector: 'quader-package-readme',
  template: `
    @if (readme(); as r) {
      @if (r.truncated) {
        <p class="readme-note" role="note" i18n="@@pkg.readme.truncated">
          This README is shortened because it is larger than the display limit.
        </p>
      }
      @if (r.externalImagesBlocked === 1) {
        <p class="readme-note" role="note" i18n="@@pkg.readme.image-blocked">
          One external image is shown as a link.
        </p>
      } @else if (r.externalImagesBlocked > 1) {
        <p class="readme-note" role="note" i18n="@@pkg.readme.images-blocked">
          {{ r.externalImagesBlocked }} external images are shown as links.
        </p>
      }
      <div class="readme" translate="no" [innerHTML]="r.html"></div>
    }
  `,
  styleUrl: './package-readme.scss',
  encapsulation: ViewEncapsulation.None,
  // Clicks on the links inside the bound HTML bubble to the host; the links are the focusable elements.
  host: { '(click)': 'inPageLink($event)' },
})
export class PackageReadmeView {
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly router = inject(Router);
  private readonly location = inject(Location);

  readonly readme = input.required<PackageReadme | null>();
  /** The start of the link targets that are routes of the application; none for a README. */
  readonly pageLinks = input<string | null>(null);
  /**
   * The prose of an Ansible documentation entry (R-ANS-048): a link to an entry, an option or a return value of the version's
   * documentation stays, gets the address of the page it opens (so that it can be opened in a new tab) and opens in place.
   */
  readonly docLinks = input(false);

  constructor() {
    afterRenderEffect(() => {
      this.readme();
      const pages = this.pageLinks();
      const docs = this.docLinks();
      const root = this.host.nativeElement.querySelector('.readme');
      if (!root) return;
      for (const a of root.querySelectorAll<HTMLAnchorElement>('a[href]')) {
        const href = (a.getAttribute('href') ?? '').trim();
        const doc = docs ? (a.getAttribute('data-doc-link') ?? href) : '';
        if (docs && DOC_LINK.test(doc) && doc !== '') {
          a.setAttribute('data-doc-link', doc);
          a.setAttribute('href', this.docUrl(doc));
          a.removeAttribute('target');
          a.removeAttribute('rel');
        } else if (pages && isPageLink(href, pages)) {
          a.removeAttribute('target');
          a.removeAttribute('rel');
        } else if (!LINK_TARGET.test(href)) {
          a.removeAttribute('href');
        } else if (!href.startsWith('#')) {
          a.removeAttribute('target');
          a.setAttribute('rel', EXTERNAL_REL);
        }
      }
      for (const img of root.querySelectorAll('img')) {
        if (!IMAGE_SOURCE.test((img.getAttribute('src') ?? '').trim())) img.remove();
      }
      for (const pre of root.querySelectorAll('pre')) pre.setAttribute('tabindex', '0');
      for (const table of root.querySelectorAll('table')) {
        if (table.parentElement?.classList.contains('readme-table')) continue;
        const wrap = document.createElement('div');
        wrap.className = 'readme-table';
        wrap.setAttribute('tabindex', '0');
        table.replaceWith(wrap);
        wrap.append(table);
      }
      const seen = new Map<string, number>();
      for (const h of root.querySelectorAll('h1, h2, h3, h4, h5, h6')) {
        const slug = headingSlug(h.textContent ?? '', seen);
        if (slug) h.id = USER_CONTENT + slug;
      }
    });
  }

  /** The address a link of the documentation opens: the page's own, with the entry it names and the anchor as its fragment. */
  private docTree(link: string): UrlTree {
    const m = DOC_LINK.exec(link);
    const doc = m?.[2];
    return this.router.createUrlTree([], {
      queryParams: doc ? { doc } : {},
      queryParamsHandling: doc ? 'merge' : 'preserve',
      fragment: m?.[4],
    });
  }

  private docUrl(link: string): string {
    return this.location.prepareExternalUrl(this.router.serializeUrl(this.docTree(link)));
  }

  protected inPageLink(event: Event): void {
    const pages = this.pageLinks();
    const anchor = (event.target as Element | null)?.closest('a[href]');
    const docLink = this.docLinks() ? anchor?.getAttribute('data-doc-link') : null;
    if (docLink) {
      const m = event as MouseEvent;
      if (m.button > 0 || m.ctrlKey || m.metaKey || m.shiftKey || m.altKey) return;
      event.preventDefault();
      const fragment = DOC_LINK.exec(docLink)?.[4];
      void this.router.navigateByUrl(this.docTree(docLink)).then(() => {
        // A link to the anchor that is already the address navigates nowhere; the page still goes to it.
        if (fragment) document.getElementById(fragment)?.scrollIntoView?.({ block: 'start' });
      });
      return;
    }
    const href = anchor?.getAttribute('href') ?? '';
    if (pages && isPageLink(href, pages)) {
      // A modified click opens a new tab or window through the browser; a plain one stays in the application.
      const m = event as MouseEvent;
      if (m.button > 0 || m.ctrlKey || m.metaKey || m.shiftKey || m.altKey) return;
      event.preventDefault();
      void this.router.navigateByUrl('/' + href);
      return;
    }
    const link = (event.target as Element | null)?.closest('a[href^="#"]');
    if (!link) return;
    event.preventDefault();
    let id: string;
    try {
      id = decodeURIComponent((link.getAttribute('href') ?? '').slice(1));
    } catch {
      return;
    }
    if (!id.startsWith(USER_CONTENT)) return;
    const target = Array.from(this.host.nativeElement.querySelectorAll<HTMLElement>('.readme [id]')).find(
      (e) => e.id === id,
    );
    target?.scrollIntoView?.({ block: 'start' });
  }
}

/** A route of the application under the prefix: a relative path of plain segments (no scheme, no step upwards) and an optional fragment. */
function isPageLink(href: string, prefix: string): boolean {
  return href.startsWith(prefix) && /^[a-z0-9/_-]+(#\S*)?$/i.test(href);
}
