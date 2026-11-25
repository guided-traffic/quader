import { describe, it } from 'vitest';
import { DeliveryRulesApi } from '../../api/delivery-rules-api';
import { FilesApi } from '../../api/files-api';
import { RemoteDownloadsApi } from '../../api/remote-downloads-api';
import { click, expectState, failAlways, failNext, fieldRefusal, fill, openDialog, serverFailure, submit } from '../../testing/states';
import { FolderDialog } from './content/folder-dialog';
import { DeliveryRulesDialog } from './upstream/delivery-rules-dialog';
import { DownloadPreviewDialog } from './upstream/download-preview-dialog';
import { AllowDialog, AskDialog, DeclineDialog } from './upstream/request-dialogs';

// The dialogs of a repository's content and of a remote repository's tab Upstream, opened as repository-files.ts,
// upstream-actions.ts, delivery-requests-panel.ts and remote-upstream.ts open them.
const SUBJECT = '@platform-engineering/kubernetes-infrastructure-operators 18.6.0-rc.12';
const rules = () =>
  openDialog(DeliveryRulesDialog, {
    width: '52rem',
    maxWidth: '52rem',
    data: { repository: 'platform-engineering/npmjs-remote', canChange: true, example: '@acme/*' },
  });

describe('dialogs of repositories in a browser (R-CUR-005, R-CUR-010, R-CUR-015, R-CUR-018, R-UI-011, R-UI-042, R-UI-173, R-UI-175)', () => {
  it('Request a version from the upstream, as it opens', async () => {
    await expectState('upstream-request-ask', () => openDialog(AskDialog, { data: { subject: SUBJECT, version: true } }));
  });

  it('Allow a request, as it opens', async () => {
    await expectState('upstream-request-allow', () =>
      openDialog(AllowDialog, { data: { name: '@platform-engineering/kubernetes-infrastructure-operators', version: '18.6.0-rc.12' } }),
    );
  });

  it('Decline a request, submitted without a reason: the sentence under the field', async () => {
    await expectState('upstream-request-decline-invalid', async () => {
      const { panel } = await openDialog(DeclineDialog, { data: { subject: SUBJECT } });
      await submit(panel);
    });
  });

  it('Delivery rules, a sub-section refused: the sentence under the field', async () => {
    await expectState('delivery-rules-invalid', async () => {
      const { panel, get } = await rules();
      failNext(
        get(DeliveryRulesApi),
        'put',
        fieldRefusal({ '/pattern': 'A sub-section is a name pattern with one * at its end, such as @platform-engineering/*.' }),
      );
      await fill(panel, 'Sub-section', '@platform-engineering/*/kubernetes-infrastructure-*');
      await click(panel, 'Add rule');
    });
  });

  it('Delivery rules, a rule that failed: the error notice with its request ID', async () => {
    await expectState('delivery-rules-failed', async () => {
      const { panel, get } = await rules();
      failNext(get(DeliveryRulesApi), 'put', serverFailure());
      await fill(panel, 'Sub-section', '@platform-engineering/*');
      await click(panel, 'Add rule');
    });
  });

  it('a download ahead, whose preview failed: the error notice with its request ID', async () => {
    await expectState('download-preview-failed', () =>
      openDialog(DownloadPreviewDialog, {
        width: '36rem',
        data: {
          repository: 'platform-engineering/npmjs-remote',
          request: { level: 'package', name: '@platform-engineering/kubernetes-infrastructure-operators' },
          title: 'Every version of @platform-engineering/kubernetes-infrastructure-operators',
        },
        setup: (get) => failAlways(get(RemoteDownloadsApi), 'preview', serverFailure()),
      }),
    );
  });

  it('New folder, failed: the error notice with its request ID', async () => {
    await expectState('folder-new-failed', async () => {
      const { panel, get } = await openDialog(FolderDialog, {
        width: '32rem',
        maxWidth: '95vw',
        data: { repository: 'platform-engineering/release-candidates-generic', parent: 'kubernetes/operators/2026' },
      });
      failNext(get(FilesApi), 'createFolder', serverFailure());
      await fill(panel, 'Folder name', 'bereitstellungsverantwortlichkeitsuebertragung/rc-12');
      await click(panel, 'Create folder');
    });
  });
});
