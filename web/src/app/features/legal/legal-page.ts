import { NgTemplateOutlet } from '@angular/common';
import { Component, LOCALE_ID, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { LegalPageName } from '../../api/legal-api';
import { PackageReadme } from '../../api/packages-api';
import { LegalStore } from '../../core/legal-store';
import { PackageReadmeView } from '../packages/package-readme';
import { QuaderMark } from '../../shared/brand/quader-mark';
import { PageHeader } from '../../shared/ui/page-header';
import { Loading } from '../../shared/ui/loading';

/**
 * The imprint or the privacy notice (R-UI-225, R-UI-226) at `/legal/imprint` and `/legal/privacy`: the operator's text, rendered and
 * sanitized by the server, bound through the README view (the second and third layer of R-UI-008). In the shell for a caller who
 * runs the application, alone with the wordmark for everyone else (`legalBare`). A text in the other language says so above it and
 * carries its `lang`. The route's guard has read the pages, so the spinner shows only while a read after a rollout is under way.
 */
@Component({
  selector: 'quader-legal-page',
  imports: [Loading, NgTemplateOutlet, PackageReadmeView, PageHeader, QuaderMark, RouterLink],
  template: `
    @if (bare()) {
      <div class="bare-panel">
        <a class="bare-logo-link" routerLink="/" i18n-aria-label="@@legal.home" aria-label="Start page">
          <quader-mark class="bare-logo" [wordmark]="true" />
        </a>
        <h1 tabindex="-1">{{ heading() }}</h1>
        <ng-container *ngTemplateOutlet="body" />
      </div>
    } @else {
      <quader-page-header [heading]="heading()" />
      <ng-container *ngTemplateOutlet="body" />
    }
    <ng-template #body>
      @if (page(); as p) {
        @if (p.lang !== locale) {
          <p class="legal-language" role="note">
            @if (p.lang === 'de') {
              <ng-container i18n="@@legal.only-german">This page is available in German only.</ng-container>
            } @else {
              <ng-container i18n="@@legal.only-english">This page is available in English only.</ng-container>
            }
          </p>
        }
        <quader-package-readme class="legal-text" [attr.lang]="p.lang" [readme]="readme()" />
      } @else if (!legal.settled()) {
        <quader-loading size="page" i18n="@@legal.loading">Loading the page.</quader-loading>
      }
    </ng-template>
  `,
  styles: `
    :host {
      display: block;
    }

    /* The text keeps a line length that reads, in the shell as alone. */
    .legal-text,
    .legal-language {
      display: block;
      max-inline-size: 48rem;
    }

    .legal-language {
      margin: 0 0 var(--quader-space-4);
      color: var(--quader-text-muted);
    }

    .bare-panel h1 {
      text-align: center;
    }

    .bare-logo-link {
      display: flex;
      justify-content: center;
      inline-size: fit-content;
      margin-inline: auto;
      border-radius: var(--quader-radius-small, 4px);

      &:focus-visible {
        outline: var(--quader-focus-ring);
        outline-offset: 2px;
      }
    }
  `,
})
export class LegalPage {
  protected readonly legal = inject(LegalStore);
  protected readonly locale: 'en' | 'de' = inject(LOCALE_ID).startsWith('de') ? 'de' : 'en';
  private readonly data = toSignal(inject(ActivatedRoute).data, { requireSync: true });

  protected readonly name = computed<LegalPageName>(() => (this.data()['legal'] === 'privacy' ? 'privacy' : 'imprint'));
  protected readonly bare = computed(() => this.data()['bare'] === true);
  protected readonly heading = computed(() =>
    this.name() === 'imprint'
      ? $localize`:@@legal.imprint:Imprint`
      : $localize`:@@legal.privacy:Privacy notice`,
  );
  protected readonly page = computed(() => this.legal.page(this.name()));
  protected readonly readme = computed<PackageReadme | null>(() => {
    const p = this.page();
    return p ? { contentType: 'text/markdown', html: p.html, truncated: false, externalImagesBlocked: 0 } : null;
  });
}
