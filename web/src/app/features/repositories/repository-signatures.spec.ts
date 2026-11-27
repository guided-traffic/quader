import { Item } from '../../api/files-api';
import { SignaturesApi } from '../../api/signatures-api';
import { FakeSignaturesApi, fileReport } from '../../api/testing/fakes-signatures';
import { button, expectAccessible, helpSigns, openSection, renderScreen, settle, text } from '../../testing/screen';
import { RepositoryFile } from './content/repository-file';
import { RepositorySignaturesSection } from './repository-signatures';

const file = (path: string, over: Partial<Item> = {}): Item => ({
  repository: 'web/files-internal',
  path,
  kind: 'file',
  size: 1024,
  mediaType: 'application/gzip',
  digests: { sha256: 'a'.repeat(64) },
  contentStatus: 'ok',
  downloadUrl: `https://quader.example.net/files/web/files-internal/${path}`,
  contentUrl: `/api/v1/projects/web/repositories/files-internal/content/${path}`,
  created: { at: '2026-11-21T08:00:00Z', by: 'user:priya' },
  modified: { at: '2026-11-21T08:00:00Z', by: 'user:priya' },
  updatedAt: '2026-11-21T08:00:00Z',
  package: null,
  upstreamUrl: null,
  itemUrl: 'https://quader.example.net/api/v1/projects/web/repositories/files-internal/items?path=x',
  ...over,
});

describe('The signatures at a file (R-SIG-035, R-SIG-032)', () => {
  it('R-SIG-035 the facts carry the badge, and the section Signatures reads the details once opened', async () => {
    const { fixture, root, get } = await renderScreen(RepositoryFile, {
      inputs: { item: file('tools/tool.tar.gz', { signatures: { state: 'verified_internal', stale: false } }) },
    });
    expect(text(root.querySelector('[data-testid="signature-fact"]')!)).toContain('Verified · internal');
    const api = get(SignaturesApi) as FakeSignaturesApi;
    api.fileAnswer = fileReport();
    expect(api.calls).toEqual([]);
    await openSection(fixture, root, 'Signatures');
    expect(api.calls).toEqual([{ op: 'file', body: { repository: 'web/files-internal', path: 'tools/tool.tar.gz' } }]);
    expect(root.querySelectorAll('[data-testid="signature-entry"]')).toHaveLength(1);
    await expectAccessible(root);
  });

  it('R-SIG-009 a state without a badge is a word in the facts', async () => {
    const { root } = await renderScreen(RepositoryFile, { inputs: { item: file('tools/other.tar.gz', { signatures: { state: 'none', stale: false } }) } });
    expect(text(root.querySelector('[data-testid="signature-fact"]')!)).toBe('No signature');
    expect(root.querySelector('[data-testid="signature-badge"]')).toBeNull();
  });

  it('R-SIG-032 a generic signature file says whose signature it is, by its longest suffix', async () => {
    const { root } = await renderScreen(RepositoryFile, { inputs: { item: file('tools/tool.tar.gz.jws.sig'), format: 'generic' } });
    const facts = [...root.querySelectorAll('dl.file-facts dt')].map((dt) => `${text(dt)}: ${text(dt.nextElementSibling!)}`);
    expect(facts).toContain('Signature of: tool.tar.gz');
  });
});

describe('The fetch of a remote\'s signatures (R-SIG-040)', () => {
  it('R-SIG-040 a remote\'s detail setting is on by default and saves the change', async () => {
    const { fixture, root } = await renderScreen(RepositorySignaturesSection, {
      inputs: { repository: 'web/docker-hub', project: 'web', format: 'oci', repoClass: 'remote', stored: { fetch: true, onlyVerified: false }, canChange: true },
    });
    const saved = vi.spyOn(fixture.componentInstance.save, 'emit');
    await openSection(fixture, root, 'Signatures');
    const box = [...root.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].find((i) => text(i.labels![0]!).startsWith('Fetch'))!;
    expect(box.checked).toBe(true);
    // What the fetch does and when to switch it off is its help (R-UI-267); nothing stands under the box.
    expect(helpSigns(box.closest('quader-checkbox')!)).toEqual([
      [
        "About Fetch the upstream's signatures",
        'Quader asks the upstream in the background for the signatures of what this repository stores, at most 600 requests an hour. Switch it off for an upstream with tight rate limits, such as Docker Hub without an account.',
      ],
    ]);
    expect(root.querySelector('#repo-signatures-note')).toBeNull();
    box.click();
    await settle(fixture);
    button(root, /^\s*Save changes/).click();
    expect(saved).toHaveBeenCalledWith({ fetch: false });
  });
});
