import { DownloadLinksApi } from '../../api/download-links-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { FakeDownloadLinksApi, downloadLinkSettings } from '../../api/testing/fakes-download-links';
import { FakeRepositoriesApi, GENERIC_FORMAT, NUGET_FORMAT, OCI_FORMAT } from '../../api/testing/fakes-catalog';
import { expectAccessible, openSection, renderScreen, saveButton, sectionHeadings, settle, text } from '../../testing/screen';
import { RepositoryDetail } from './repository-detail';
import { RepositoryDownloadLinksSection } from './repository-download-links';

async function section(inputs: Record<string, unknown>, enabled = true) {
  return renderScreen(RepositoryDownloadLinksSection, {
    inputs: { stored: true, format: 'oci', formatLabel: 'OCI (Docker)', canChange: true, ...inputs },
    setup: (get) => {
      (get(DownloadLinksApi) as FakeDownloadLinksApi).current = downloadLinkSettings({ enabled });
    },
  });
}

const box = (root: ParentNode): HTMLInputElement | null => root.querySelector<HTMLInputElement>('label.check-row input');
const note = (root: ParentNode): string => text(root.querySelector('#repo-links-note')!);

describe("The repository's section Download links (R-UI-201)", () => {
  it('R-UI-201 checked by default; with the installation on it says so, and unchecking saves false', async () => {
    const r = await section({});
    const saved = vi.spyOn(r.fixture.componentInstance.save, 'emit');
    expect(box(r.root)?.checked).toBe(true);
    expect(box(r.root)?.disabled).toBe(false);
    expect(note(r.root)).toBe('The installation delivers large downloads by link.');
    box(r.root)!.click();
    await settle(r.fixture);
    r.root.querySelector<HTMLButtonElement>('button[type="submit"]')!.click();
    expect(saved.mock.calls).toEqual([[false]]);
  });

  it('R-UI-201 a virtual repository says that a member which does not allow it is delivered by Quader', async () => {
    const r = await section({ virtual: true });
    expect(text(r.root.querySelector('[data-testid="repo-links-virtual"]')!)).toBe('A member that does not allow it is delivered by Quader.');
  });

  it('R-UI-201 Q-UI-956 with the installation switched off the box stays usable, says when the choice takes effect, and saves', async () => {
    const r = await section({}, false);
    const saved = vi.spyOn(r.fixture.componentInstance.save, 'emit');
    expect(box(r.root)?.checked).toBe(true);
    expect(box(r.root)?.disabled).toBe(false);
    expect(note(r.root)).toBe(
      'The installation does not hand downloads to links at the moment; your choice takes effect when an administrator switches them on.',
    );
    box(r.root)!.click();
    await settle(r.fixture);
    r.root.querySelector<HTMLButtonElement>('button[type="submit"]')!.click();
    expect(saved.mock.calls).toEqual([[false]]);
  });

  it('R-UI-201 Q-UI-956 a format not every client of which checks is disabled and unchecked, and says why', async () => {
    const r = await section({ format: 'npm', formatLabel: 'npm' });
    expect(box(r.root)?.checked).toBe(false);
    expect(box(r.root)?.disabled).toBe(true);
    expect(note(r.root)).toBe(
      'Not every npm client checks the bytes it downloads against a digest, so Quader delivers every download of this repository itself.',
    );
    expect(r.root.querySelector('button[type="submit"]')).toBeNull();
  });

  it('R-UI-201 a caller who may not change the repository reads the state as text', async () => {
    const r = await section({ canChange: false, stored: false });
    expect(box(r.root)).toBeNull();
    expect(text(r.root.querySelector('[data-testid="repo-links-state"]')!)).toBe('Quader delivers every download of this repository itself.');
  });

  it('TestR_UI_201 on the settings page: closed after Signatures, on a NuGet repository disabled; unchecking on an OCI repository sends storage.downloadLinks false with If-Match', async () => {
    const r = await renderScreen(RepositoryDetail, {
      inputs: { project: 'web', key: 'oci-local' },
      setup: (get) => {
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.formatsResult = [GENERIC_FORMAT, OCI_FORMAT, NUGET_FORMAT];
        fake.repositories.push(
          { key: 'oci-local', project: 'web', class: 'local', format: 'oci' },
          { key: 'nuget-local', project: 'web', class: 'local', format: 'nuget' },
        );
        (get(DownloadLinksApi) as FakeDownloadLinksApi).current = downloadLinkSettings({ enabled: true });
      },
    });
    const headings = sectionHeadings(r.root);
    expect(headings.indexOf('Download links')).toBe(headings.indexOf('Signatures') + 1);
    const toggle = [...r.root.querySelectorAll('quader-disclosure .toggle')].find((b) => text(b).includes('Download links'))!;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    await openSection(r.fixture, r.root, 'Download links');
    const own = r.root.querySelector('quader-repository-download-links')!;
    box(own)!.click();
    await settle(r.fixture);
    saveButton(r.root, 'download links').click();
    await settle(r.fixture);
    const fake = r.get(RepositoriesApi) as FakeRepositoriesApi;
    expect(fake.patches.at(-1)).toMatchObject({ key: 'web/oci-local', patch: { storage: { downloadLinks: false } }, etag: '"1"' });
    await expectAccessible(r.root);

    const nuget = await renderScreen(RepositoryDetail, {
      inputs: { project: 'web', key: 'nuget-local' },
      setup: (get) => {
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.formatsResult = [GENERIC_FORMAT, OCI_FORMAT, NUGET_FORMAT];
        fake.repositories.push({ key: 'nuget-local', project: 'web', class: 'local', format: 'nuget' });
      },
    });
    await openSection(nuget.fixture, nuget.root, 'Download links');
    const disabled = box(nuget.root.querySelector('quader-repository-download-links')!)!;
    expect(disabled.disabled).toBe(true);
    expect(text(nuget.root.querySelector('[data-testid="repo-links-not-applicable"]')!)).toContain('Not every NuGet client checks');
  });
});
