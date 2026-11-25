import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Component, inject, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';
import indexHtml from '../../../index.html' with { loader: 'text' };
import spinnerScss from '../../../styles/_spinner.scss' with { loader: 'text' };
import { Busy, busyActInterceptor } from './busy';
import { Loading } from './loading';
import { Spinner, clockwiseStones } from './spinner';
import { Switch } from './switch';

@Component({
  imports: [Spinner, Loading, Busy, Switch],
  template: `
    <quader-spinner id="line" />
    <quader-spinner id="page" size="page" />
    <quader-loading id="loading">Loading the members.</quader-loading>
    <quader-loading id="quiet" [inline]="true" [status]="false">Testing the connection.</quader-loading>
    <button id="save" class="btn" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="busy.set(true)">Save changes</button>
    <button id="delete" class="btn" type="button" [disabled]="busy()" [quaderBusy]="busy()">
      <span class="material-icons" aria-hidden="true">delete</span>Delete
    </button>
    <quader-switch label="Switch on Docker Hub" [checked]="true" [busy]="busy()" />
  `,
})
class Host {
  readonly busy = signal(false);
}

@Component({
  imports: [Busy],
  template: `
    <button id="remove" class="btn" type="button" quaderBusy (click)="remove()">Remove</button>
    <button id="other" class="btn" type="button">Other</button>
  `,
})
class ActHost {
  private readonly http = inject(HttpClient);
  presses = 0;
  remove(): void {
    this.presses++;
    void firstValueFrom(this.http.delete('/api/v1/things/a'));
  }
  poll(): void {
    void firstValueFrom(this.http.get('/api/v1/things'));
  }
  write(): void {
    void firstValueFrom(this.http.post('/api/v1/things/b', {}));
  }
}

async function render() {
  await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
  const fixture = TestBed.createComponent(Host);
  await fixture.whenStable();
  const root = fixture.nativeElement as HTMLElement;
  const settle = async (): Promise<void> => {
    fixture.detectChanges();
    await fixture.whenStable();
  };
  return { root, host: fixture.componentInstance, settle };
}

const rects = (svg: ParentNode): string[] =>
  [...svg.querySelectorAll('rect')].map((r) => ['x', 'y', 'width', 'height', 'rx'].map((a) => r.getAttribute(a)).join(' '));

describe('The spinner', () => {
  it('R-UI-217 is the mark: six stones of three rows, lit clockwise from the top left', () => {
    // Top left, top right, middle right, bottom right, bottom left, middle left (Q-UI-1229).
    expect(clockwiseStones().map((s) => `${s.x},${s.y}`)).toEqual(['4,4', '44,4', '24,24', '44,44', '4,44', '4,24']);
  });

  it('R-UI-217 the first paint of index.html draws the same stones in the same order as the component', async () => {
    const { root } = await render();
    const page = new DOMParser().parseFromString(indexHtml, 'text/html');
    const boot = page.querySelector('quader-root .quader-boot')!;
    expect(boot.getAttribute('role')).toBe('status');
    expect(rects(boot.querySelector('.quader-spinner svg')!)).toEqual(rects(root.querySelector('#line svg')!));
    // Its sentence in both languages of the build; the stylesheet shows the one of <html lang> (Q-UI-1236).
    expect([...boot.querySelectorAll('.quader-loading-text [lang]')].map((s) => `${s.getAttribute('lang')}: ${s.textContent}`)).toEqual([
      'en: Loading Quader.',
      'de: Quader wird geladen.',
    ]);
    expect(spinnerScss).toContain("html:lang(de) .quader-boot [lang='en']");
  });

  it('R-UI-217 is decoration; the sentence beside it is the one status, and a load shows after 300 ms', async () => {
    const { root } = await render();
    expect(root.querySelector('#line')?.getAttribute('aria-hidden')).toBe('true');
    expect(root.querySelector('#page')?.classList).toContain('quader-spinner-page');
    const loading = root.querySelector<HTMLElement>('#loading')!;
    expect(loading.getAttribute('role')).toBe('status');
    expect(loading.querySelector('quader-spinner')).toBeTruthy();
    expect(loading.textContent?.trim()).toBe('Loading the members.');
    expect(root.querySelector('#quiet')?.getAttribute('role')).toBeNull();
    // The delay is the stylesheet's (Q-UI-1230), so a fast answer shows nothing and no timer of the page's own runs.
    expect(spinnerScss).toMatch(/\.quader-loading \{[^}]*animation: quader-appear 0s linear \$delay both;/);
    expect(spinnerScss).toContain('$delay: 300ms;');
    expect(spinnerScss).toContain('@media (prefers-reduced-motion: reduce)');
  });

  it('R-UI-217 a busy button keeps its label and shows the spinner only in the button that was pressed', async () => {
    const { root, host, settle } = await render();
    const save = root.querySelector<HTMLButtonElement>('#save')!;
    const del = root.querySelector<HTMLButtonElement>('#delete')!;
    save.click();
    await settle();
    expect(save.disabled).toBe(true);
    expect(save.getAttribute('aria-busy')).toBe('true');
    expect(save.firstElementChild?.tagName).toBe('QUADER-SPINNER');
    expect(save.textContent?.trim()).toBe('Save changes');
    // The other button of the same flag is disabled, not busy.
    expect(del.disabled).toBe(true);
    expect(del.getAttribute('aria-busy')).toBeNull();
    expect(del.querySelector('quader-spinner')).toBeNull();
    host.busy.set(false);
    await settle();
    expect(save.querySelector('quader-spinner')).toBeNull();
    expect(save.getAttribute('aria-busy')).toBeNull();
    // An act that no busy button started spins none.
    host.busy.set(true);
    await settle();
    expect(root.querySelectorAll('button quader-spinner')).toHaveLength(0);
  });

  it('R-UI-217 a busy button without a flag spins while the write its press started is open, and a second press starts nothing', async () => {
    await TestBed.configureTestingModule({
      imports: [ActHost],
      providers: [provideHttpClient(withInterceptors([busyActInterceptor])), provideHttpClientTesting()],
    }).compileComponents();
    const fixture = TestBed.createComponent(ActHost);
    await fixture.whenStable();
    const root = fixture.nativeElement as HTMLElement;
    const http = TestBed.inject(HttpTestingController);
    const settle = async (): Promise<void> => {
      fixture.detectChanges();
      await fixture.whenStable();
    };
    const remove = root.querySelector<HTMLButtonElement>('#remove')!;
    remove.click();
    await settle();
    const write = http.expectOne('/api/v1/things/a');
    expect(remove.getAttribute('aria-busy')).toBe('true');
    expect(remove.getAttribute('aria-disabled')).toBe('true');
    expect(remove.querySelector('quader-spinner')).toBeTruthy();
    remove.click();
    expect(fixture.componentInstance.presses).toBe(1);
    write.flush(null);
    await settle();
    expect(remove.querySelector('quader-spinner')).toBeNull();
    // A live view's read and a write that something else starts later spin no button.
    fixture.componentInstance.poll();
    http.expectOne('/api/v1/things').flush([]);
    root.querySelector<HTMLButtonElement>('#other')!.click();
    fixture.componentInstance.write();
    await settle();
    expect(remove.querySelector('quader-spinner')).toBeNull();
    http.expectOne('/api/v1/things/b').flush(null);
    http.verify();
  });

  it('R-UI-217 a switch whose answer is open shows the spinner in place of its word', async () => {
    const { root, host, settle } = await render();
    expect(root.querySelector('quader-switch quader-spinner')).toBeNull();
    host.busy.set(true);
    await settle();
    expect(root.querySelector('quader-switch button')?.getAttribute('aria-busy')).toBe('true');
    expect(root.querySelector('quader-switch quader-spinner')).toBeTruthy();
    expect(root.querySelector('quader-switch .word')?.classList).toContain('busy');
  });
});
