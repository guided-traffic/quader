import { DecimalPipe } from '@angular/common';
import { Component, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { PackageFileEntry, PackageVersionContents, PackagesApi } from '../../api/packages-api';
import { ErrorNotice } from '../../shared/error-notice';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { Busy } from '../../shared/ui/busy';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { Loading } from '../../shared/ui/loading';
import { NoValue } from '../../shared/ui/no-value';
import { TechnicalDetails } from '../../shared/technical-details';
import { formatSize } from './format-size';

/*
 * What a Debian package file tells about itself beyond its control paragraph (formats/debian.md R-DEB-039, Q-UI-1272): its changelog
 * (a tab), its copyright file (a fold of Details) and the files it installs (a section of the tab Files). Quader reads them after the
 * file is stored; until then the page says so and reads again by itself (R-UI-213). The texts are the uploader's or the upstream's and
 * are shown as text, never as markup: no address in them is made a link.
 */

/** Why Quader did not read a package file, in the screen's words for the reason the server names (R-UI-214). */
function reasonText(reason: string | undefined): string {
  switch (reason) {
    case 'unsupported_compression':
      return $localize`:@@deb.contents.why.compression:its data archive is compressed with a method Quader does not read (bzip2 or lzma)`;
    case 'invalid_archive':
      return $localize`:@@deb.contents.why.invalid:its data archive is not a valid archive`;
    case 'too_large':
      return $localize`:@@deb.contents.why.too-large:its data archive is larger than Quader reads (2 GiB unpacked)`;
    case 'too_many_entries':
      return $localize`:@@deb.contents.why.entries:its data archive holds more than 200,000 entries`;
    case 'timeout':
      return $localize`:@@deb.contents.why.timeout:reading it took longer than two minutes`;
    case 'no_content':
      return $localize`:@@deb.contents.why.no-content:its stored content is missing`;
  }
  return '';
}

/** The sentence of a version whose package file Quader has not read, or could not read; null when it has read it. */
function stateNote(c: PackageVersionContents): string | null {
  switch (c.state) {
    case 'pending':
      return $localize`:@@deb.contents.pending:Quader is reading the package file. Its changelog, copyright file and file list appear here in a moment.`;
    case 'unavailable': {
      const why = reasonText(c.reason);
      return why !== ''
        ? $localize`:@@deb.contents.refused-why:Quader did not read the package file's changelog, copyright file and file list: ${why}:reason:. The package is stored and installs as usual.`
        : $localize`:@@deb.contents.refused:Quader could not read the package file, so its changelog, copyright file and file list are not shown. The package is stored and installs as usual.`;
    }
  }
  return null;
}

/** The changelog of a Debian version (the tab Changelog): the newest entries, as text. */
@Component({
  selector: 'quader-deb-changelog',
  imports: [Loading, TechnicalDetails],
  template: `
    @if (!contents()) {
      <quader-loading i18n="@@deb.changelog.loading">Loading the changelog.</quader-loading>
    } @else if (note(); as n) {
      <p class="deb-note" [class.warn]="contents()?.state === 'unavailable'" role="status">{{ n }}</p>
      @if (contents()?.detail; as detail) {
        <quader-technical-details [text]="detail" [code]="contents()?.reason ?? null" />
      }
    } @else if (contents()?.changelog; as log) {
      @if (log.truncated) {
        <p class="deb-note" i18n="@@deb.changelog.cut">The newest entries are shown: Quader keeps the first 64 KiB of the changelog.</p>
      }
      <pre class="deb-text" translate="no" data-testid="deb-changelog">{{ log.text }}</pre>
    } @else if (contents()?.docLink; as link) {
      <p class="deb-note" i18n="@@deb.changelog.linked">This package's documentation is that of <span translate="no">{{ link }}</span>: it has no changelog of its own.</p>
    } @else {
      <p class="deb-note" i18n="@@deb.changelog.none">This package file holds no changelog.</p>
    }
  `,
  styleUrl: './deb-contents.scss',
})
export class DebChangelog {
  /** The contents of the version, null while they load. */
  readonly contents = input<PackageVersionContents | null>(null);
  protected readonly note = computed(() => {
    const c = this.contents();
    return c ? stateNote(c) : null;
  });
}

/** The copyright file of a Debian version (a fold of the tab Details, closed): the text as the package holds it. */
@Component({
  selector: 'quader-deb-copyright',
  template: `
    @if (contents(); as c) {
      @if (c.copyright; as cr) {
        @if (cr.truncated) {
          <p class="deb-note" i18n="@@deb.copyright.cut">The beginning is shown: Quader keeps the first 64 KiB of the copyright file.</p>
        }
        <pre class="deb-text" translate="no" data-testid="deb-copyright">{{ cr.text }}</pre>
      } @else if (note(); as n) {
        <p class="deb-note" role="status">{{ n }}</p>
      } @else {
        <p class="deb-note" i18n="@@deb.copyright.none">This package file holds no copyright file.</p>
      }
    }
  `,
  styleUrl: './deb-contents.scss',
})
export class DebCopyright {
  readonly contents = input<PackageVersionContents | null>(null);
  protected readonly note = computed(() => {
    const c = this.contents();
    return c ? stateNote(c) : null;
  });
}

/** The first page of the list, and what "Show more" adds. */
const FIRST_PAGE = 100;
const MORE = 500;

/**
 * The files a Debian package installs (a section of the tab Files): path, size, a link's target. The list grows the page (one
 * scrollbar, R-UI-215): the first 100, a filter on the path, and "Show more" for the next 500; a version with several architectures
 * offers the file of each.
 */
@Component({
  selector: 'quader-deb-files',
  imports: [DataTable, DecimalPipe, Busy, ErrorNotice, NoValue],
  templateUrl: './deb-files.html',
  styleUrl: './deb-contents.scss',
})
export class DebFiles {
  private readonly api = inject(PackagesApi);

  readonly packageId = input.required<string>();
  readonly version = input.required<string>();
  /** The version's contents as the page read them: the first page of the first architecture's files. */
  readonly contents = input<PackageVersionContents | null>(null);

  protected readonly draft = signal('');
  /** The page the person asked for: the filter, the architecture and how many entries are shown; null for the page's own first one. */
  private readonly asked = signal<{ readonly q: string; readonly architecture: string; readonly limit: number } | null>(null);
  protected readonly page = resource({
    params: () => {
      const a = this.asked();
      return a ? { id: this.packageId(), version: this.version(), ...a } : undefined;
    },
    // The operation answers at most 1,000 entries a read: a longer list is read in pages and joined.
    loader: async ({ params }) => {
      const query = {
        ...(params.architecture !== '' ? { architecture: params.architecture } : {}),
        ...(params.q !== '' ? { q: params.q } : {}),
      };
      let answer = await this.api.contents(params.id, params.version, { ...query, limit: Math.min(params.limit, 1000) });
      let items = answer.files?.items ?? [];
      while (answer.files && items.length < params.limit && items.length < answer.files.matched) {
        const next = await this.api.contents(params.id, params.version, {
          ...query,
          offset: items.length,
          limit: Math.min(params.limit - items.length, 1000),
        });
        if (!next.files || next.files.items.length === 0) break;
        items = [...items, ...next.files.items];
        answer = next;
      }
      return answer.files ? { ...answer, files: { ...answer.files, offset: 0, items } } : answer;
    },
  });
  protected readonly pageFailure = failureOf(this.page);
  /** The last page that answered: kept while the next one loads, so the list does not blink. */
  private readonly last = signal<PackageVersionContents | null>(null);

  /** What the list shows: the person's page once they asked for one, else the page's contents. */
  protected readonly shown = computed(() => (this.asked() ? (valueOf(this.page) ?? this.last()) : null) ?? this.contents());
  protected readonly files = computed(() => this.shown()?.files ?? null);
  protected readonly items = computed<readonly PackageFileEntry[]>(() => this.files()?.items ?? []);
  protected readonly architectures = computed(() => this.contents()?.architectures ?? []);
  protected readonly architecture = computed(() => this.asked()?.architecture || (this.contents()?.architecture ?? ''));
  protected readonly note = computed(() => {
    const c = this.shown();
    return c ? stateNote(c) : null;
  });
  protected readonly more = computed(() => {
    const f = this.files();
    return f ? Math.max(0, f.matched - (f.offset + f.items.length)) : 0;
  });
  protected readonly filtered = computed(() => (this.asked()?.q ?? '') !== '');

  protected readonly columns: readonly TableColumn[] = [
    { id: 'path', label: $localize`:@@deb.files.path:Path`, prefer: true, width: '16rem', stackLabel: false },
    { id: 'size', label: $localize`:@@deb.files.size:Size`, numeric: true, width: '6rem' },
  ];
  protected readonly caption = $localize`:@@deb.files.caption:Files the package installs`;
  protected readonly filterLabel = $localize`:@@deb.files.filter:Filter by path`;
  protected readonly size = formatSize;
  private timer: ReturnType<typeof setTimeout> | undefined;

  constructor() {
    effect(() => {
      const v = valueOf(this.page);
      if (v) untracked(() => this.last.set(v));
    });
    // Another version starts at its first page; the same version read again by the page (R-UI-213) keeps the person's page.
    effect(() => {
      this.packageId();
      this.version();
      untracked(() => {
        this.asked.set(null);
        this.last.set(null);
        this.draft.set('');
      });
    });
  }

  protected kindText(f: PackageFileEntry): string {
    switch (f.kind) {
      case 'link':
        return $localize`:@@deb.files.link:link to ${f.target ?? ''}:target:`;
      case 'hardlink':
        return $localize`:@@deb.files.hardlink:hard link to ${f.target ?? ''}:target:`;
      case 'special':
        return $localize`:@@deb.files.special:device or pipe`;
    }
    return '';
  }

  protected onFilter(value: string): void {
    this.draft.set(value);
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.ask({ q: value.trim(), limit: FIRST_PAGE }), 250);
  }

  protected onArchitecture(value: string): void {
    this.ask({ architecture: value, limit: FIRST_PAGE });
  }

  protected showMore(): void {
    this.ask({ limit: this.items().length + MORE });
  }

  private ask(change: { readonly q?: string; readonly architecture?: string; readonly limit: number }): void {
    const before = this.asked();
    this.asked.set({
      q: change.q ?? before?.q ?? '',
      architecture: change.architecture ?? before?.architecture ?? '',
      limit: change.limit,
    });
  }
}
