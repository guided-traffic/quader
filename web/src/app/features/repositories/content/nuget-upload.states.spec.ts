import { describe, it, inject as provided } from 'vitest';
import { NuGetApi, NuGetPublished, NuGetPublishOptions } from '../../../api/nuget-api';
import { ApiError } from '../../../api/problem';
import { FakeNuGetApi } from '../../../api/testing/fakes-nuget';
import { Get, REQUEST_ID, click, expectState, openDialog, stable } from '../../../testing/states';
import { NuGetUploadData, NuGetUploadDialog } from './nuget-upload-dialog';

/**
 * Upload package in a browser (R-NUGET-006, R-UI-216, Q-UI-1364), opened as the repository's page opens it (40 rem, at most 95 % of the
 * window): as it opens, a package and its symbols chosen, the package's bytes on their way, the symbols' after it, a published version
 * with and without its symbols, a refused symbol package, and the refusals of the push. Long names, as a team's packages have them.
 * `UI_STATES_PICTURES=1` adds the dark scheme.
 */
const REPOSITORY = 'platform-engineering/nuget-release-candidates-of-the-platform';
const ID = 'PlatformEngineering.Kubernetes.Infrastructure.Operators';
const VERSION = '18.6.0-rc.12';
const PACKAGE = `${ID}.${VERSION}.nupkg`;
const SYMBOLS = `${ID}.${VERSION}.snupkg`;
const PUSH_PATH = ['', 'api', 'nuget', 'platform-engineering', 'nuget-release-candidates-of-the-platform', 'v3', 'package'].join('/');

const refusal = (status: number, code: string, detail: string, path = PUSH_PATH): ApiError =>
  new ApiError(status, { code, status, detail, requestId: REQUEST_ID }, REQUEST_ID, null, {
    // The publish resource, as the server's publishPath or symbolsPath names it (the screens build no protocol address, R-ARCH-007).
    call: { method: 'PUT', path },
  });

const published: NuGetPublished = { id: ID, version: VERSION, symbols: 'none' };

/** Opens the dialog, the fake's push set by `answer`, the files chosen when `files` names them, and Upload pressed with `upload`. */
async function open(
  answer: (api: FakeNuGetApi) => void = () => undefined,
  options: { readonly files?: readonly string[]; readonly upload?: boolean; readonly dark?: boolean } = {},
): Promise<void> {
  if (options.dark) document.documentElement.setAttribute('data-theme', 'dark');
  else document.documentElement.removeAttribute('data-theme');
  const data: NuGetUploadData = { repository: REPOSITORY };
  const { panel } = await openDialog(NuGetUploadDialog, {
    data,
    width: '40rem',
    maxWidth: '95vw',
    setup: (get: Get) => answer(get(NuGetApi) as FakeNuGetApi),
  });
  if (options.files) {
    const input = panel.querySelector<HTMLInputElement>('input[type="file"]')!;
    const dt = new DataTransfer();
    for (const name of options.files) dt.items.add(new File([new Uint8Array(name.endsWith('.snupkg') ? 12 * 1024 : 96 * 1024)], name));
    input.files = dt.files;
    input.dispatchEvent(new Event('change'));
    await stable();
  }
  if (options.upload) await click(panel, 'Upload');
}

/** A push whose package's bytes are `share` of the way (or, with `symbols`, stored, and the symbols' that far), never answered. */
const pending =
  (share: number, symbols = false) =>
  (api: FakeNuGetApi): void => {
    api.publish = (_ref: string, pkg: Blob, sym: Blob | null, o: NuGetPublishOptions = {}): Promise<NuGetPublished> => {
      if (symbols && sym) {
        o.onProgress?.(pkg.size, pkg.size);
        o.onSymbols?.();
        o.onProgress?.(Math.round(sym.size * share), sym.size);
      } else {
        o.onProgress?.(Math.round(pkg.size * share), pkg.size);
      }
      return new Promise(() => undefined);
    };
  };

const both = [PACKAGE, SYMBOLS];
const states: Readonly<Record<string, readonly [(api: FakeNuGetApi) => void, { readonly files?: readonly string[]; readonly upload?: boolean }]>> = {
  'nuget-upload-empty': [() => undefined, {}],
  'nuget-upload-chosen': [() => undefined, { files: both }],
  'nuget-upload-symbols-alone': [() => undefined, { files: [SYMBOLS] }],
  'nuget-upload-sending': [pending(0.4), { files: both, upload: true }],
  'nuget-upload-sending-symbols': [pending(0.6, true), { files: both, upload: true }],
  'nuget-upload-completed': [(api) => void (api.published = published), { files: both, upload: true }],
  'nuget-upload-completed-package': [(api) => void (api.published = published), { files: [PACKAGE], upload: true }],
  'nuget-upload-symbols-refused': [
    (api) => {
      api.published = published;
      api.symbolsRefusal = refusal(400, 'nuget_invalid_package', 'The symbol package holds lib/net8.0/Operators.dll: a .snupkg holds .pdb files and its metadata only.', PUSH_PATH.replace(/package$/, 'symbolpackage'));
    },
    { files: both, upload: true },
  ],
  'nuget-upload-refused-exists': [
    (api) => void (api.published = refusal(409, 'nuget_version_exists', `${ID} ${VERSION} is stored with other bytes; a version is immutable.`)),
    { files: both, upload: true },
  ],
  'nuget-upload-refused-owner': [
    (api) =>
      void (api.published = refusal(403, 'package_owned_by_other_owner', `The package ${ID.toLowerCase()} is owned by other owners; ask one of them or a holder of manage to add you.`)),
    { files: [PACKAGE], upload: true },
  ],
};

describe('Upload package in a browser (R-NUGET-006, R-UI-216)', () => {
  for (const [name, [answer, options]] of Object.entries(states)) {
    it(name, async () => {
      await expectState(name, () => open(answer, options));
      if (provided('picturesDir')) await expectState(`${name}-dark`, () => open(answer, { ...options, dark: true }));
      document.documentElement.removeAttribute('data-theme');
    });
  }
});
