import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { vi } from 'vitest';
import { NuGetApi } from '../../../api/nuget-api';
import { ApiError } from '../../../api/problem';
import { provideFakeApis } from '../../../api/testing/fakes';
import { FakeNuGetApi } from '../../../api/testing/fakes-nuget';
import { DIALOG_DATA, DialogRef } from '../../../shared/ui/dialog';
import { expectAccessible, settle, text } from '../../../testing/screen';
import { NuGetUploadData, NuGetUploadDialog } from './nuget-upload-dialog';

/** Opens the dialog as the repository's page opens it, with the in-memory fakes; `close` records its result. */
async function open(setup?: (api: FakeNuGetApi) => void) {
  TestBed.resetTestingModule();
  const close = vi.fn();
  await TestBed.configureTestingModule({
    imports: [NuGetUploadDialog],
    providers: [
      provideRouter([{ path: '**', children: [] }]),
      ...provideFakeApis(),
      { provide: DIALOG_DATA, useValue: { repository: 'web/nuget-local' } satisfies NuGetUploadData },
      { provide: DialogRef, useValue: { close } },
    ],
  }).compileComponents();
  const api = TestBed.inject(NuGetApi) as FakeNuGetApi;
  setup?.(api);
  const fixture = TestBed.createComponent(NuGetUploadDialog);
  await settle(fixture);
  const root = fixture.nativeElement as HTMLElement;
  const choose = async (...names: string[]): Promise<void> => {
    const input = root.querySelector<HTMLInputElement>('input[type="file"]')!;
    Object.defineProperty(input, 'files', { value: names.map((n) => new File([new Uint8Array(1024)], n)), configurable: true });
    input.dispatchEvent(new Event('change'));
    await settle(fixture);
  };
  const upload = async (): Promise<void> => {
    buttonOf(root, 'Upload').click();
    await settle(fixture);
  };
  return { fixture, root, api, close, choose, upload };
}

function buttonOf(root: ParentNode, label: string): HTMLButtonElement {
  const b = [...root.querySelectorAll<HTMLButtonElement>('button')].find((x) => text(x) === label);
  if (!b) throw new Error(`no button ${label}`);
  return b;
}

describe('Upload package (R-NUGET-006)', () => {
  it('R-NUGET-006 sends a package and its symbols once the package is chosen, and a symbol package alone is not sent', async () => {
    const d = await open();
    expect(buttonOf(d.root, 'Upload').disabled).toBe(true);
    await d.choose('Acme.Widgets.1.2.0.snupkg');
    expect(buttonOf(d.root, 'Upload').disabled).toBe(true);
    expect(text(d.root)).toContain('A symbol package follows its package');
    await d.choose('Acme.Widgets.1.2.0.nupkg', 'Acme.Widgets.1.2.0.snupkg');
    await d.upload();
    expect(d.api.calls).toEqual([{ ref: 'web/nuget-local', pkg: 1024, symbols: 1024 }]);
    expect(text(d.root.querySelector('[data-testid="nuget-upload-outcome"]')!)).toBe('The package is published: Contoso.Logging 2.4.0');
    expect(text(d.root)).toContain('Its symbol package is stored beside it.');
    const link = d.root.querySelector<HTMLAnchorElement>('a.btn-confirm')!;
    expect(link.getAttribute('href')).toBe('/projects/web/packages/nuget/Contoso.Logging@2.4.0');
    link.click();
    expect(d.close).toHaveBeenCalledWith(true);
    await expectAccessible(d.root);
  });

  it('R-UI-216 a refused push keeps the files and says why; a refused symbol package says so beside the published version', async () => {
    const refusal = new ApiError(409, { code: 'nuget_version_exists', status: 409, detail: 'Acme.Widgets 1.2.0 is stored with other bytes.' }, 'r-1');
    const d = await open((api) => (api.published = refusal));
    await d.choose('Acme.Widgets.1.2.0.nupkg');
    await d.upload();
    expect(text(d.root.querySelector('quader-error-notice')!)).toContain('stored with other bytes');
    expect(text(d.root)).toContain('Acme.Widgets.1.2.0.nupkg');

    const s = await open((api) => (api.symbolsRefusal = new ApiError(400, { code: 'nuget_invalid_package', status: 400, detail: 'The symbol package holds a .dll.' }, 'r-2')));
    await s.choose('Acme.Widgets.1.2.0.nupkg', 'Acme.Widgets.1.2.0.snupkg');
    await s.upload();
    expect(text(s.root)).toContain('The package is published');
    expect(text(s.root)).toContain('Its symbol package was not stored:');
    expect(text(s.root.querySelector('quader-error-notice')!)).toContain('holds a .dll');
  });
});
