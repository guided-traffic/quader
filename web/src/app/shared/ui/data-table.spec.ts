import { Component, computed, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { ApiError } from '../../api/problem';
import { seriousViolations } from '../../testing/axe';
import { DataTable, TableColumn, TableSort } from './data-table';
import { NoValue } from './no-value';
import { TimeAgo } from './time-ago';

@Component({
  imports: [DataTable],
  template: `
    <quader-data-table
      caption="Widgets"
      [columns]="columns"
      [sort]="sort()"
      (sortChange)="sort.set($event); sorts.push($event)"
      [loaded]="loaded()"
      [loading]="loading()"
      [error]="error()"
      [empty]="rows().length === 0"
      emptyText="No widget matches."
      [hasMore]="hasMore()"
      (more)="mores = mores + 1"
      (retry)="retries = retries + 1"
    >
      @for (r of rows(); track r) {
        <tr>
          <th scope="row">{{ r }}</th>
          <td>detail</td>
        </tr>
      }
    </quader-data-table>
  `,
})
class Host {
  readonly columns: readonly TableColumn[] = [
    { id: 'name', label: 'Name', sortable: true },
    { id: 'detail', label: 'Detail', help: 'Explains the detail column.' },
  ];
  readonly sort = signal<TableSort | null>(null);
  readonly loaded = signal(true);
  readonly loading = signal(false);
  readonly error = signal<ApiError | null>(null);
  readonly rows = signal<readonly string[]>(['alpha', 'beta']);
  readonly hasMore = signal(false);
  readonly sorts: TableSort[] = [];
  mores = 0;
  retries = 0;
}

async function render() {
  await TestBed.configureTestingModule({
    imports: [Host],
  }).compileComponents();
  const fixture = TestBed.createComponent(Host);
  await fixture.whenStable();
  const root = fixture.nativeElement as HTMLElement;
  const settle = async () => {
    fixture.detectChanges();
    await fixture.whenStable();
  };
  return { fixture, root, host: fixture.componentInstance, settle };
}

describe('Data table', () => {
  it('R-UI-020 R-UI-215 shows a captioned table with column headers and the projected rows, no region of its own and no tab stop', async () => {
    const { root } = await render();
    // The page scrolls, not the table: no scroll region to name and to reach by Tab.
    expect(root.querySelector('[role="region"]')).toBeNull();
    expect(root.querySelector('quader-data-table [tabindex]')).toBeNull();
    expect(root.querySelector('table')?.getAttribute('aria-labelledby')).toBeNull();
    expect(root.querySelector('caption')?.textContent?.trim()).toBe('Widgets');
    expect([...root.querySelectorAll('th[scope="col"]')].map((h) => h.textContent?.trim())).toEqual([
      'Nameunfold_more',
      'Detailhelp_outline',
    ]);
    expect(root.querySelectorAll('tbody tr')).toHaveLength(2);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-020 a sortable header is a button that announces the order with aria-sort and asks the screen to sort', async () => {
    const { root, host, settle } = await render();
    const header = root.querySelector('th[scope="col"]')!;
    expect(header.getAttribute('aria-sort')).toBe('none');
    // The column that cannot be sorted carries no aria-sort and no sort button. A stacked list shows exactly the headers with
    // aria-sort, as a row of sort buttons, and hides the others (Q-UI-391, _list-screens.scss).
    expect(root.querySelectorAll('th[scope="col"]')[1]!.hasAttribute('aria-sort')).toBe(false);
    expect(root.querySelectorAll('th button.th-sort')).toHaveLength(1);

    header.querySelector<HTMLButtonElement>('button.th-sort')!.click();
    await settle();
    expect(host.sorts.at(-1)).toEqual({ column: 'name', direction: 'asc' });
    expect(header.getAttribute('aria-sort')).toBe('ascending');
    expect(header.querySelector('.th-arrow')?.textContent).toBe('arrow_upward');

    header.querySelector<HTMLButtonElement>('button.th-sort')!.click();
    await settle();
    expect(host.sorts.at(-1)).toEqual({ column: 'name', direction: 'desc' });
    expect(header.getAttribute('aria-sort')).toBe('descending');
    expect(header.querySelector('.th-arrow')?.textContent).toBe('arrow_downward');
  });

  it('R-UI-044 the help button of a column names the column, is a real button and is described by its tooltip text', async () => {
    const { root } = await render();
    const help = root.querySelectorAll<HTMLButtonElement>('th button.th-help');
    expect(help).toHaveLength(1);
    expect(help[0]!.type).toBe('button');
    expect(help[0]!.getAttribute('aria-label')).toBe('About the column Detail');
    // The tooltip directive points aria-describedby at its message, so the help is not mouse-only.
    expect(help[0]!.getAttribute('aria-describedby')).toBeTruthy();
  });

  it('R-UI-042 shows loading, empty and error states instead of the table', async () => {
    const { root, host, settle } = await render();
    host.loaded.set(false);
    host.loading.set(true);
    await settle();
    expect(root.querySelector('[role="status"]')?.textContent).toContain('Loading.');
    expect(root.querySelector('table')).toBeNull();

    host.loaded.set(true);
    host.loading.set(false);
    host.rows.set([]);
    await settle();
    expect(root.querySelector('quader-empty-state')?.textContent).toContain('No widget matches.');
    expect(root.querySelector('table')).toBeNull();

    host.error.set(new ApiError(503, { code: 'unavailable' }, 'rid-1'));
    await settle();
    const retry = [...root.querySelectorAll('button')].find((b) => b.textContent?.trim() === 'Retry')!;
    retry.click();
    expect(host.retries).toBe(1);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-006 "Load more" appears while the list has more and asks the screen for the next page', async () => {
    const { root, host, settle } = await render();
    expect([...root.querySelectorAll('button')].some((b) => b.textContent?.trim() === 'Load more')).toBe(false);
    host.hasMore.set(true);
    await settle();
    [...root.querySelectorAll('button')].find((b) => b.textContent?.trim() === 'Load more')!.click();
    expect(host.mores).toBe(1);
  });

  it('R-UI-020 the headers carry the least widths, a narrow column is marked, and a stacked cell names its column', async () => {
    @Component({
      imports: [DataTable],
      template: `
        <quader-data-table caption="Things" [columns]="columns">
          <tr>
            <th scope="row">one</th>
            <td>A description</td>
            <td>–</td>
            <td class="actions"><button type="button">x</button></td>
          </tr>
        </quader-data-table>
      `,
    })
    class Budget {
      readonly columns: readonly TableColumn[] = [
        { id: 'name', label: 'Name', width: '16rem' },
        { id: 'description', label: 'Description', stackLabel: false },
        { id: 'when', label: 'When', width: '7rem', narrow: true },
        { id: 'actions', label: 'Actions', hideLabel: true, width: '2rem' },
      ];
    }
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [Budget] }).compileComponents();
    const fixture = TestBed.createComponent(Budget);
    await fixture.whenStable();
    fixture.detectChanges();
    await fixture.whenStable();
    const root = fixture.nativeElement as HTMLElement;
    const table = root.querySelector<HTMLElement>('quader-data-table')!;
    expect(table.classList.contains('stack')).toBe(true);
    // No list sets a share of its width for an address any more (Q-UI-426).
    expect(table.getAttribute('style') ?? '').not.toContain('--quader-list');
    const heads = [...root.querySelectorAll<HTMLElement>('thead th')];
    expect(heads.map((h) => h.style.minInlineSize)).toEqual(['16rem', '', '7rem', '2rem']);
    expect(heads.map((h) => h.classList.contains('narrow-drop'))).toEqual([false, false, true, false]);
    // Stacked, a cell names its column unless its column says not to; an empty value leaves its line out.
    const cells = [...root.querySelectorAll<HTMLElement>('tbody td')];
    expect(cells.map((c) => c.getAttribute('data-label'))).toEqual([null, 'When', null]);
    expect(cells[1]!.hasAttribute('data-empty')).toBe(true);
    expect(cells[0]!.hasAttribute('data-empty')).toBe(false);
    // Without a column that prefers the room, the headers carry their widths as their least only.
    expect(heads.map((h) => h.style.inlineSize)).toEqual(['', '', '', '']);
  });

  it('R-UI-146 a cell without a value is the dash with the accessible name None, and a stacked row leaves its line out', async () => {
    @Component({
      imports: [DataTable, NoValue],
      template: `
        <quader-data-table caption="Things" [columns]="columns">
          <tr>
            <th scope="row">one</th>
            <td><quader-no-value /></td>
            <td>value</td>
          </tr>
        </quader-data-table>
      `,
    })
    class Empty {
      readonly columns: readonly TableColumn[] = [
        { id: 'name', label: 'Name' },
        { id: 'email', label: 'Email' },
        { id: 'origin', label: 'Origin' },
      ];
    }
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [Empty] }).compileComponents();
    const fixture = TestBed.createComponent(Empty);
    await fixture.whenStable();
    fixture.detectChanges();
    await fixture.whenStable();
    const root = fixture.nativeElement as HTMLElement;
    const [none, value] = [...root.querySelectorAll<HTMLElement>('tbody td')];
    expect(none!.querySelector('[aria-hidden="true"]')!.textContent).toBe('–');
    expect(none!.querySelector('.visually-hidden')!.textContent).toBe('None');
    expect(none!.hasAttribute('data-empty')).toBe(true);
    expect(value!.hasAttribute('data-empty')).toBe(false);
  });

  it('a column that prefers the room takes what the others leave, and the other columns keep their widths', async () => {
    @Component({
      imports: [DataTable],
      template: `
        <quader-data-table caption="Things" [columns]="columns">
          <tr>
            <th scope="row">one</th>
            <td>generic</td>
            <td class="actions"><button type="button">x</button></td>
          </tr>
        </quader-data-table>
      `,
    })
    class Prefer {
      readonly columns: readonly TableColumn[] = [
        { id: 'name', label: 'Name', width: '13rem', prefer: true },
        { id: 'format', label: 'Format', width: '8rem' },
        { id: 'actions', label: 'Actions', hideLabel: true, width: '2rem' },
      ];
    }
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [Prefer] }).compileComponents();
    const fixture = TestBed.createComponent(Prefer);
    await fixture.whenStable();
    fixture.detectChanges();
    await fixture.whenStable();
    const root = fixture.nativeElement as HTMLElement;
    const heads = [...root.querySelectorAll<HTMLElement>('thead th')];
    expect(heads.map((h) => h.style.minInlineSize)).toEqual(['13rem', '8rem', '2rem']);
    expect(heads.map((h) => h.style.inlineSize)).toEqual(['100%', '8rem', '2rem']);
  });

  it('R-UI-174 a stacked cell is named after its column also when the columns changed before the rows did', async () => {
    @Component({
      imports: [DataTable],
      template: `
        <quader-data-table caption="Versions" [columns]="columns()">
          @for (r of rows(); track r) {
            <tr>
              <th scope="row">{{ r }}</th>
              @if (graded()) {
                <td>grade</td>
              }
              <td>published</td>
            </tr>
          }
        </quader-data-table>
      `,
    })
    class Changing {
      readonly graded = signal(false);
      readonly rows = signal(['1.0.0']);
      readonly columns = computed<readonly TableColumn[]>(() => [
        { id: 'version', label: 'Version' },
        ...(this.graded() ? [{ id: 'grade', label: 'Vulnerabilities' }] : []),
        { id: 'published', label: 'Published' },
      ]);
    }
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [Changing] }).compileComponents();
    const fixture = TestBed.createComponent(Changing);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.componentInstance.graded.set(true);
    fixture.detectChanges();
    await fixture.whenStable();
    // A later change of the body: the observer labels the cells with the columns of now.
    fixture.componentInstance.rows.set(['1.0.0', '2.0.0']);
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise((r) => setTimeout(r));
    const root = fixture.nativeElement as HTMLElement;
    const labels = [...root.querySelectorAll('tbody tr')].map((tr) => [...tr.querySelectorAll('td')].map((td) => td.getAttribute('data-label')));
    expect(labels).toEqual([
      ['Vulnerabilities', 'Published'],
      ['Vulnerabilities', 'Published'],
    ]);
  });

  it('R-UI-174 the rows stack where the columns at their least widths need more than the region has, and only there', async () => {
    // jsdom lays nothing out: the table's least width and the region's room are what the browser would measure. The table is
    // measured with its width set to nothing (its least), then given its width back.
    let need = 900;
    let has = 700;
    const widths: string[] = [];
    const rect = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      if (this.tagName === 'TABLE') widths.push((this as HTMLElement).style.inlineSize);
      const width = this.tagName === 'TABLE' ? need : has;
      return { width, height: 10, top: 0, left: 0, right: width, bottom: 10, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
    });
    const client = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => has);
    try {
      const { root, host, settle } = await render();
      await settle();
      const table = root.querySelector('quader-data-table')!;
      expect(table.classList.contains('stacked')).toBe(true);
      expect(widths).toContain('0px');
      expect(root.querySelector<HTMLElement>('table.data')!.style.inlineSize).toBe('');
      // Room enough: the rows are a table again when the body changes.
      has = 900;
      host.rows.set(['alpha', 'beta', 'gamma']);
      await settle();
      await new Promise((r) => setTimeout(r));
      expect(table.classList.contains('stacked')).toBe(false);
      need = 901;
      host.rows.set(['alpha']);
      await settle();
      await new Promise((r) => setTimeout(r));
      expect(table.classList.contains('stacked')).toBe(true);
    } finally {
      rect.mockRestore();
      client.mockRestore();
    }
  });

  it('R-UI-174 a stacked list measures its longest column name for the column of names its rows share', async () => {
    // The hidden list of names holds the names the stacked rows show (not the row's own name, which stands on the first line); its
    // width is the list's `--quader-stack-names`, the same for every row.
    const rect = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      const width = this.tagName === 'TABLE' ? 900 : this.classList.contains('stack-names') ? 87.2 : 700;
      return { width, height: 10, top: 0, left: 0, right: width, bottom: 10, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
    });
    const client = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => 700);
    try {
      const { root, settle } = await render();
      await settle();
      const table = root.querySelector<HTMLElement>('quader-data-table')!;
      expect(table.classList.contains('stacked')).toBe(true);
      const names = root.querySelector('.stack-names')!;
      expect(names.getAttribute('aria-hidden')).toBe('true');
      expect([...names.querySelectorAll('span')].map((s) => s.textContent?.trim())).toEqual(['Detail']);
      expect(table.style.getPropertyValue('--quader-stack-names')).toBe('88px');
    } finally {
      rect.mockRestore();
      client.mockRestore();
    }
  });

  it("R-UI-174 a stacked row's column of names is never narrower than its longest word, so a name wraps only between words", async () => {
    // Two fifths of 200 px are 80 px; the names are 300 px on one line and their longest word 120 px ("Veröffentlicht" in a list of
    // German names): the column takes 120 px, and a name of two words wraps between them.
    const rect = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      const names = this.classList.contains('stack-names');
      const width = this.tagName === 'TABLE' ? 900 : names ? ((this as HTMLElement).style.inlineSize === 'min-content' ? 119.4 : 300) : 200;
      return { width, height: 10, top: 0, left: 0, right: width, bottom: 10, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
    });
    const client = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => 200);
    try {
      const { root, settle } = await render();
      await settle();
      const table = root.querySelector<HTMLElement>('quader-data-table')!;
      expect(table.classList.contains('stacked')).toBe(true);
      expect(table.style.getPropertyValue('--quader-stack-names')).toBe('120px');
      // The measurement leaves the hidden list as it found it.
      expect(root.querySelector<HTMLElement>('.stack-names')!.getAttribute('style') ?? '').toBe('');
    } finally {
      rect.mockRestore();
      client.mockRestore();
    }
  });

  it('R-UI-174 a list in a block of a stacked list measures the names of its own rows', async () => {
    // A package's versions opened in a repository's packages at 390 px: the versions fit by themselves and stand as blocks by the
    // rules of the list around them. Without names of their own they took the outer list's column ("Aktualisiert", 82 px), and
    // "Veröffentlicht" broke inside itself there (2026-11-24).
    @Component({
      imports: [DataTable],
      template: `
        <quader-data-table caption="Packages" [columns]="outer">
          <tr>
            <th scope="row">acme-agent</th>
            <td>1.3.0-1</td>
          </tr>
          <tr>
            <td colspan="2">
              <quader-data-table class="inner" caption="Versions" [columns]="inner">
                <tr>
                  <th scope="row">1.2.0-1</th>
                  <td>716 B</td>
                  <td>now</td>
                </tr>
              </quader-data-table>
            </td>
          </tr>
        </quader-data-table>
      `,
    })
    class Nested {
      readonly outer: readonly TableColumn[] = [
        { id: 'name', label: 'Package' },
        { id: 'latest', label: 'Aktualisiert' },
      ];
      readonly inner: readonly TableColumn[] = [
        { id: 'version', label: 'Version' },
        { id: 'size', label: 'Größe' },
        { id: 'published', label: 'Veröffentlicht' },
      ];
    }
    const rect = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      const inner = !!this.closest('quader-data-table.inner');
      let width = 300;
      if (this.tagName === 'TABLE') width = inner ? 100 : 900;
      else if (this.classList.contains('stack-names')) width = inner ? 97.5 : 82;
      return { width, height: 10, top: 0, left: 0, right: width, bottom: 10, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
    });
    const client = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => 300);
    try {
      TestBed.resetTestingModule();
      await TestBed.configureTestingModule({ imports: [Nested] }).compileComponents();
      const fixture = TestBed.createComponent(Nested);
      await fixture.whenStable();
      fixture.detectChanges();
      await fixture.whenStable();
      const root = fixture.nativeElement as HTMLElement;
      const [outer, inner] = [...root.querySelectorAll<HTMLElement>('quader-data-table')];
      expect(outer!.classList.contains('stacked')).toBe(true);
      expect(outer!.style.getPropertyValue('--quader-stack-names')).toBe('82px');
      // The versions are no stacked list of their own, and still name their fields from a column of their own names.
      expect(inner!.classList.contains('stacked')).toBe(false);
      const names = inner!.querySelector(':scope > .stack-names')!;
      expect([...names.querySelectorAll('span')].map((s) => s.textContent)).toEqual(['Größe', 'Veröffentlicht']);
      expect(inner!.style.getPropertyValue('--quader-stack-names')).toBe('98px');
    } finally {
      rect.mockRestore();
      client.mockRestore();
    }
  });

  it('R-UI-103 R-UI-174 a column of relative times holds the widest relative time of the language, whatever age its rows show', async () => {
    // A token created a moment ago reads "now", one of an hour "59 minutes ago": the list keeps its form while its times grow (Q-UI-1069).
    @Component({
      imports: [DataTable, TimeAgo, NoValue],
      template: `
        <quader-data-table caption="Tokens" [columns]="columns">
          <tr>
            <th scope="row">laptop</th>
            <td><quader-time-ago [at]="at" absolute="title" /></td>
            <td>Created <quader-time-ago [at]="at" absolute="title" /></td>
            <td class="no-time">Never</td>
          </tr>
          <tr>
            <th scope="row">ci</th>
            <td><quader-no-value /></td>
            <td>–</td>
            <td><quader-time-ago [at]="at" absolute="title" /></td>
          </tr>
        </quader-data-table>
      `,
    })
    class Times {
      readonly at = new Date().toISOString();
      readonly columns: readonly TableColumn[] = [
        { id: 'name', label: 'Token' },
        { id: 'created', label: 'Created' },
        { id: 'note', label: 'Note' },
        { id: 'lastUsed', label: 'Last used', width: '9rem' },
      ];
    }
    const rect = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      const width = this.classList.contains('time-widths') ? 97.3 : this.tagName === 'TABLE' ? 400 : 700;
      return { width, height: 10, top: 0, left: 0, right: width, bottom: 10, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
    });
    try {
      TestBed.resetTestingModule();
      await TestBed.configureTestingModule({ imports: [Times] }).compileComponents();
      const fixture = TestBed.createComponent(Times);
      await fixture.whenStable();
      fixture.detectChanges();
      await fixture.whenStable();
      const root = fixture.nativeElement as HTMLElement;
      const table = root.querySelector<HTMLElement>('quader-data-table')!;
      // A column whose value starts with a relative time in some row; a time inside a sentence is no column of times.
      const heads = [...root.querySelectorAll<HTMLElement>('thead th')];
      expect(heads.map((h) => h.classList.contains('time-column'))).toEqual([false, true, false, true]);
      // The widest wordings of the language, measured once: their width is every such column's least.
      const words = [...root.querySelectorAll(':scope quader-data-table > .time-widths span')].map((s) => s.textContent);
      expect(words).toEqual(expect.arrayContaining(['now', '59 minutes ago', 'in 59 minutes', '12 months ago', 'in 4 weeks']));
      expect(root.querySelector('.time-widths')!.getAttribute('aria-hidden')).toBe('true');
      expect(table.style.getPropertyValue('--quader-time-width')).toBe('98px');
      // A declared width stays the column's least as an inline style, which the stylesheet's least of a column of times yields to.
      expect(heads[3]!.style.minInlineSize).toBe('9rem');
    } finally {
      rect.mockRestore();
    }
  });

  it('R-UI-174 a change of the list\'s width measures its columns again, a report of the same width does not', async () => {
    // Only the ruler across the list is observed: nothing puts a scrollbar beside the rows any more (R-UI-215). jsdom has no
    // ResizeObserver: a stand-in hands the observer the widths the browser would report.
    const observers: { observed: Element[]; callback: ResizeObserverCallback }[] = [];
    class FakeResizeObserver {
      readonly observed: Element[] = [];
      constructor(readonly callback: ResizeObserverCallback) {
        observers.push(this);
      }
      observe(el: Element): void {
        this.observed.push(el);
      }
      readonly unobserve = vi.fn();
      readonly disconnect = vi.fn();
    }
    vi.stubGlobal('ResizeObserver', FakeResizeObserver);
    const need = 963;
    let has = 968;
    const rect = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      const width = this.tagName === 'TABLE' ? need : has;
      return { width, height: 10, top: 0, left: 0, right: width, bottom: 10, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
    });
    const client = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => has);
    try {
      const { root, settle } = await render();
      await settle();
      const table = root.querySelector('quader-data-table')!;
      expect(table.classList.contains('stacked')).toBe(false);
      const observer = observers.at(-1)!;
      const ruler = root.querySelector('.table-width')!;
      expect(observer.observed).toEqual([ruler]);
      const report = async (width: number): Promise<void> => {
        observer.callback([{ target: ruler, contentRect: { width } } as ResizeObserverEntry], observer as unknown as ResizeObserver);
        await new Promise((r) => requestAnimationFrame(() => r(undefined)));
      };
      await report(968);
      expect(table.classList.contains('stacked')).toBe(false);
      // A narrower window: the room is less than the columns need.
      has = 953;
      await report(953);
      expect(table.classList.contains('stacked')).toBe(true);
      // The same width again (the stacked rows are taller) measures nothing, whatever the room would say now.
      has = 968;
      await report(953);
      expect(table.classList.contains('stacked')).toBe(true);
      await report(968);
      expect(table.classList.contains('stacked')).toBe(false);
    } finally {
      rect.mockRestore();
      client.mockRestore();
      vi.unstubAllGlobals();
    }
  });
});
