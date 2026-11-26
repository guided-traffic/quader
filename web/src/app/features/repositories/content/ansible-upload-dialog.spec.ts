import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { vi } from 'vitest';
import { AnsibleApi } from '../../../api/ansible-api';
import { ApiError } from '../../../api/problem';
import { provideFakeApis } from '../../../api/testing/fakes';
import { FakeAnsibleApi, importTask } from '../../../api/testing/fakes-ansible';
import { describeAnsibleProblem } from '../../../core/http/problem-messages-ansible';
import { DIALOG_DATA, DialogRef } from '../../../shared/ui/dialog';
import { expectAccessible, settle, text } from '../../../testing/screen';
import { AnsibleUploadData, AnsibleUploadDialog } from './ansible-upload-dialog';

/** Opens the dialog as the repository's page and a role's page open it, with the in-memory fakes; `close` records its result. */
async function open(repositories: readonly string[], setup?: (api: FakeAnsibleApi) => void) {
  TestBed.resetTestingModule();
  const close = vi.fn();
  await TestBed.configureTestingModule({
    imports: [AnsibleUploadDialog],
    providers: [
      provideRouter([{ path: '**', children: [] }]),
      ...provideFakeApis(),
      { provide: DIALOG_DATA, useValue: { repositories } satisfies AnsibleUploadData },
      { provide: DialogRef, useValue: { close } },
    ],
  }).compileComponents();
  const api = TestBed.inject(AnsibleApi) as FakeAnsibleApi;
  setup?.(api);
  const fixture = TestBed.createComponent(AnsibleUploadDialog);
  await settle(fixture);
  const root = fixture.nativeElement as HTMLElement;
  const choose = async (name = 'acme-tools-1.0.0.tar.gz'): Promise<void> => {
    const input = root.querySelector<HTMLInputElement>('input[type="file"]')!;
    const file = new File([new Uint8Array(2048)], name, { type: 'application/gzip' });
    Object.defineProperty(input, 'files', { value: [file], configurable: true });
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

describe('Upload collection (R-ANS-061)', () => {
  it('R-ANS-061 names the repository, offers one file and sends it only once one is chosen, with its name and size shown', async () => {
    const d = await open(['web/ansible-local']);
    expect(text(d.root.querySelector('.target')!)).toBe('web/ansible-local');
    expect(d.root.querySelector('select')).toBeNull();
    expect(buttonOf(d.root, 'Upload').disabled).toBe(true);
    await d.choose();
    expect(text(d.root)).toContain('acme-tools-1.0.0.tar.gz');
    expect(text(d.root.querySelector('.size')!)).toMatch(/2(\.0)? KiB/);
    expect(buttonOf(d.root, 'Upload').disabled).toBe(false);
    await expectAccessible(d.root);
  });

  it('R-ANS-061 R-ANS-062 a completed import shows the task with its warning and leads to the version; the opener is told', async () => {
    const d = await open(['web/ansible-local'], (api) => {
      api.published = importTask({
        messages: [
          { level: 'INFO', message: 'Collection acme.tools 1.0.0 stored (sha256 d3499c…)', time: '2026-11-26T09:00:01Z' },
          { level: 'WARNING', message: 'No changelog found: the collection has none of …', time: '2026-11-26T09:00:01Z' },
        ],
      });
    });
    await d.choose();
    await d.upload();
    expect(d.api.publishedTo).toEqual(['web/ansible-local']);
    expect(text(d.root.querySelector('[data-testid="ansible-upload-outcome"]')!)).toBe('The collection is published: acme.tools 1.0.0');
    expect(d.root.querySelector('.level.warning')?.textContent).toBe('WARNING');
    const link = d.root.querySelector<HTMLAnchorElement>('a.btn-confirm')!;
    expect(link.getAttribute('href')).toBe('/projects/web/packages/ansible/acme.tools@1.0.0');
    link.click();
    expect(d.close).toHaveBeenCalledWith(true);
    await expectAccessible(d.root);
  });

  it('R-ANS-061 R-ANS-009 a failed import says the archive was not published, with the reason and its code, and lets another file be chosen', async () => {
    const d = await open(['web/ansible-local'], (api) => {
      api.published = importTask({
        state: 'failed',
        namespace: '',
        name: '',
        version: '',
        error: { code: 'invalid_archive', description: 'the archive has no MANIFEST.json at its root' },
        messages: [{ level: 'ERROR', message: 'the archive has no MANIFEST.json at its root', time: '2026-11-26T09:00:01Z' }],
      });
    });
    await d.choose();
    await d.upload();
    expect(text(d.root)).toContain('The archive was not published. The import log says why.');
    expect(text(d.root.querySelector('[data-testid="import-log-error"]')!)).toBe('the archive has no MANIFEST.json at its root invalid_archive');
    expect(d.root.querySelector('a.btn-confirm')).toBeNull();
    buttonOf(d.root, 'Choose another file').click();
    await settle(d.fixture);
    expect(buttonOf(d.root, 'Upload').disabled).toBe(true);
    buttonOf(d.root, 'Cancel').click();
    expect(d.close).toHaveBeenCalledWith(false);
  });

  it('R-ANS-061 R-UI-216 a refusal before the archive was read is the notice with every fact, in the reader\'s words; the file stays', async () => {
    const refusal = new ApiError(409, { code: 'ansible_version_exists', status: 409, detail: 'acme.tools 1.0.0 is stored with other bytes.' }, 'r-1');
    const d = await open(['web/ansible-local'], (api) => {
      api.published = refusal;
    });
    await d.choose();
    await d.upload();
    const notice = d.root.querySelector('quader-error-notice')!;
    expect(text(notice)).toContain(describeAnsibleProblem('ansible_version_exists')!);
    expect(text(d.root)).toContain('acme-tools-1.0.0.tar.gz');
    expect(buttonOf(d.root, 'Upload').disabled).toBe(false);
  });

  it('R-ANS-061 opened with several repositories it lets the reader choose, and publishes into the one chosen', async () => {
    const d = await open(['web/ansible-local', 'web/ansible']);
    const select = d.root.querySelector<HTMLSelectElement>('select')!;
    expect([...select.options].map((o) => o.value)).toEqual(['web/ansible-local', 'web/ansible']);
    select.value = 'web/ansible';
    select.dispatchEvent(new Event('change'));
    await d.choose();
    await d.upload();
    expect(d.api.publishedTo).toEqual(['web/ansible']);
  });

  it('R-ANS-061 every code a publish is refused with before its archive was read has a sentence of its own', () => {
    for (const code of ['ansible_version_exists', 'ansible_version_tombstoned', 'ansible_sha256_mismatch', 'ansible_invalid_request', 'shadows_remote_package']) {
      expect(describeAnsibleProblem(code), code).not.toBeNull();
    }
    expect(describeAnsibleProblem('not_an_ansible_code')).toBeNull();
  });
});
