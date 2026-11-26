import { describe, it, inject as provided } from 'vitest';
import { AnsibleApi, AnsibleImportTask } from '../../../api/ansible-api';
import { ApiError } from '../../../api/problem';
import { FakeAnsibleApi, importTask } from '../../../api/testing/fakes-ansible';
import { TransferOptions } from '../../../api/upload-transfer';
import { Get, REQUEST_ID, click, expectState, openDialog, stable } from '../../../testing/states';
import { AnsibleUploadData, AnsibleUploadDialog } from './ansible-upload-dialog';

/**
 * Upload collection in a browser (R-ANS-061, R-ANS-062, R-UI-216, Q-UI-1321), opened as the repository's page and a role's page open it
 * (repository-content.ts, ansible-role.ts: 40 rem, at most 95 % of the window): as it opens, a file chosen, the bytes on their way, the
 * server reading them, a completed import with warnings, a failed one, and the refusals before the archive is read. Long names and long
 * messages, as a team's collection and the import's own warnings have them. `UI_STATES_PICTURES=1` adds the dark scheme.
 */
const REPOSITORY = 'platform-engineering/ansible-collections-release-candidates';
const FILE = 'platform_engineering-kubernetes_infrastructure_operators-18.6.0-rc.12.tar.gz';
const AT = '2026-11-26T09:41:07Z';

const completed = importTask({
  namespace: 'platform_engineering',
  name: 'kubernetes_infrastructure_operators',
  version: '18.6.0-rc.12',
  createdAt: AT,
  finishedAt: AT,
  messages: [
    { level: 'INFO', message: 'Collection platform_engineering.kubernetes_infrastructure_operators 18.6.0-rc.12 stored (sha256 d3499c41a2b7…)', time: AT },
    { level: 'WARNING', message: 'No changelog found: the collection has none of changelogs/changelog.yaml, CHANGELOG.md or CHANGELOG.rst, at its root or in docs/.', time: AT },
    { level: 'WARNING', message: 'meta/runtime.yml sets no requires_ansible: no client can tell which versions of Ansible the collection supports.', time: AT },
    { level: 'WARNING', message: 'documentation: plugins/modules/cluster_bootstrap.py: the literal DOCUMENTATION does not close (docs_literal_unterminated)', time: AT },
  ],
});

const failed = importTask({
  state: 'failed',
  namespace: '',
  name: '',
  version: '',
  createdAt: AT,
  finishedAt: AT,
  error: { code: 'invalid_archive', description: 'the file plugins/modules/cluster_bootstrap.py is listed in FILES.json with another sha256 than the archive holds' },
  messages: [
    { level: 'ERROR', message: 'the file plugins/modules/cluster_bootstrap.py is listed in FILES.json with another sha256 than the archive holds', time: AT },
  ],
});

const refusal = (status: number, code: string, detail: string): ApiError =>
  new ApiError(status, { code, status, detail, requestId: REQUEST_ID }, REQUEST_ID, null, {
    call: { method: 'POST', path: `/api/v1/projects/platform-engineering/repositories/ansible-collections-release-candidates/ansible/collections` },
  });

/** Opens the dialog with the repositories given, the fake's publish set by `answer`, and a file chosen when `file` is set. */
async function open(
  answer: (api: FakeAnsibleApi) => void = () => undefined,
  options: { readonly file?: boolean; readonly upload?: boolean; readonly repositories?: readonly string[]; readonly dark?: boolean } = {},
): Promise<void> {
  if (options.dark) document.documentElement.setAttribute('data-theme', 'dark');
  else document.documentElement.removeAttribute('data-theme');
  const data: AnsibleUploadData = { repositories: options.repositories ?? [REPOSITORY] };
  const { panel } = await openDialog(AnsibleUploadDialog, {
    data,
    width: '40rem',
    maxWidth: '95vw',
    setup: (get: Get) => answer(get(AnsibleApi) as FakeAnsibleApi),
  });
  if (options.file || options.upload) {
    const input = panel.querySelector<HTMLInputElement>('input[type="file"]')!;
    const dt = new DataTransfer();
    dt.items.add(new File([new Uint8Array(48 * 1024)], FILE, { type: 'application/gzip' }));
    input.files = dt.files;
    input.dispatchEvent(new Event('change'));
    await stable();
  }
  if (options.upload) await click(panel, 'Upload');
}

/** A publish whose bytes are `share` of the way, and whose answer never comes. */
const pending =
  (share: number) =>
  (api: FakeAnsibleApi): void => {
    api.publish = (_ref: string, file: Blob, o: TransferOptions = {}): Promise<AnsibleImportTask> => {
      o.onProgress?.(Math.round(file.size * share), file.size);
      return new Promise(() => undefined);
    };
  };

const states: Readonly<Record<string, readonly [(api: FakeAnsibleApi) => void, { readonly file?: boolean; readonly upload?: boolean; readonly repositories?: readonly string[] }]>> = {
  'ansible-upload-empty': [() => undefined, {}],
  'ansible-upload-choice': [() => undefined, { repositories: [REPOSITORY, 'platform-engineering/ansible', 'platform-engineering/ansible-team-b-local'] }],
  'ansible-upload-chosen': [() => undefined, { file: true }],
  'ansible-upload-sending': [pending(0.4), { upload: true }],
  'ansible-upload-importing': [pending(1), { upload: true }],
  'ansible-upload-completed': [(api) => void (api.published = completed), { upload: true }],
  'ansible-upload-failed': [(api) => void (api.published = failed), { upload: true }],
  'ansible-upload-refused-permission': [
    (api) => void (api.published = refusal(403, 'forbidden', 'The action deploy is not permitted on platform-engineering/ansible-collections-release-candidates.')),
    { upload: true },
  ],
  'ansible-upload-refused-claimed': [
    (api) =>
      void (api.published = refusal(
        403,
        'package_owned_by_other_owner',
        'The package platform_engineering.kubernetes_infrastructure_operators is owned by other owners; ask one of them or a holder of manage to add you.',
      )),
    { upload: true },
  ],
  'ansible-upload-refused-exists': [
    (api) =>
      void (api.published = refusal(409, 'ansible_version_exists', 'platform_engineering.kubernetes_infrastructure_operators 18.6.0-rc.12 is stored with other bytes; a version is immutable.')),
    { upload: true },
  ],
  'ansible-upload-refused-too-large': [
    (api) => void (api.published = refusal(413, 'payload_too_large', 'The archive is larger than ansible.max_archive_bytes (209715200 bytes).')),
    { upload: true },
  ],
};

describe('Upload collection in a browser (R-ANS-061, R-ANS-062, R-UI-216)', () => {
  for (const [name, [answer, options]] of Object.entries(states)) {
    it(name, async () => {
      await expectState(name, () => open(answer, options));
      if (provided('picturesDir')) await expectState(`${name}-dark`, () => open(answer, { ...options, dark: true }));
      document.documentElement.removeAttribute('data-theme');
    });
  }
});
