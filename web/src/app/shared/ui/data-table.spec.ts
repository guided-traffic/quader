import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { ApiError } from '../../api/problem';
import { seriousViolations } from '../../testing/axe';
import { DataTable, TableColumn, TableSort } from './data-table';
import { NoValue } from './no-value';

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
  it('R-UI-020 shows a captioned table in a focusable scroll region with column headers and the projected rows', async () => {
    const { root } = await render();
    const region = root.querySelector('[role="region"]')!;
    expect(region.getAttribute('tabindex')).toBe('0');
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

  it('R-UI-020 Q-UI-308 the headers carry the least widths, a narrow column is marked, and a stacked cell names its column', async () => {
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

  it('R-UI-146 Q-UI-561 a cell without a value is the dash with the accessible name None, and a stacked row leaves its line out', async () => {
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

  it('Q-UI-330 a column that prefers the room takes what the others leave, and the other columns keep their widths', async () => {
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
});
