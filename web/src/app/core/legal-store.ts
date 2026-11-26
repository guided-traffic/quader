import { Injectable, LOCALE_ID, computed, inject, signal } from '@angular/core';
import { LegalApi, LegalPage, LegalPageName, LegalPages } from '../api/legal-api';

/**
 * The imprint and the privacy notice of the installation (R-UI-225, `GET /api/v1/legal`) in the locale of the interface. It has no
 * timer of its own: the banner store reads it with the banner, at once and then at the banner's cadence while a screen that shows
 * either is seen (R-UI-213), and the browser turns an unchanged answer into a 304 by its ETag. A failed read keeps what was shown,
 * as the banner's does; the footer is no reason for an error of its own.
 */
@Injectable({ providedIn: 'root' })
export class LegalStore {
  private readonly api = inject(LegalApi);
  /** The locale of the running build (`/ui/{locale}/`, Q-UI-5). */
  private readonly locale: 'en' | 'de' = inject(LOCALE_ID).startsWith('de') ? 'de' : 'en';

  private readonly _pages = signal<LegalPages | null>(null);
  /** The first read has answered, or failed. */
  private readonly _settled = signal(false);
  private reading: Promise<void> | null = null;

  readonly settled = this._settled.asReadonly();
  readonly imprint = computed(() => this._pages()?.imprint ?? null);
  readonly privacy = computed(() => this._pages()?.privacy ?? null);
  /** Neither page has a text: no footer line at all. */
  readonly empty = computed(() => this.imprint() === null && this.privacy() === null);

  page(name: LegalPageName): LegalPage | null {
    return name === 'imprint' ? this.imprint() : this.privacy();
  }

  /** Reads the pages; concurrent calls share the request. */
  refresh(): Promise<void> {
    this.reading ??= this.read().finally(() => {
      this.reading = null;
    });
    return this.reading;
  }

  /** The first answer, read now if no read has answered yet: what a legal page's guard waits for. */
  async ready(): Promise<void> {
    if (!this._settled()) await this.refresh();
  }

  private async read(): Promise<void> {
    try {
      this._pages.set(await this.api.pages(this.locale));
    } catch {
      // keep what is shown
    } finally {
      this._settled.set(true);
    }
  }
}
