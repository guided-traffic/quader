import { describe, it } from 'vitest';
import { DeliveryRulesApi, DeliveryRulesSummary } from '../../api/delivery-rules-api';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeDeliveryRulesApi } from '../../api/testing/fakes-upstream-catalogue';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { DeliveryModeData, DeliveryModeDialog } from './delivery-mode-dialog';
import { FilesApi } from '../../api/files-api';
import { ApiError } from '../../api/problem';
import { RemoteDownloadsApi } from '../../api/remote-downloads-api';
import { FakeRemoteDownloadsApi } from '../../api/testing/fakes-remote-downloads';
import {
  REQUEST_ID,
  click,
  expectState,
  failAlways,
  failNext,
  fieldRefusal,
  fill,
  openDialog,
  serverFailure,
  submit,
  until,
} from '../../testing/states';
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

describe('dialogs of repositories in a browser (R-CUR-005, R-CUR-010, R-CUR-015, R-CUR-018, R-CUR-027, R-UI-011, R-UI-042, R-UI-173, R-UI-175)', () => {
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

  it('a download ahead, the upper bound of its range refused: the sentence under the field and the whole answer in the notice', async () => {
    await expectState('download-preview-range-refused', async () => {
      const { panel } = await openDialog(DownloadPreviewDialog, {
        width: '36rem',
        data: {
          repository: 'platform-engineering/npmjs-remote',
          request: { level: 'package', name: '@platform-engineering/kubernetes-infrastructure-operators' },
          title: 'Every version of @platform-engineering/kubernetes-infrastructure-operators',
        },
        setup: (get) => {
          const detail = 'toVersion 17.0.0 lies below fromVersion 18.6.0-rc.12: the range holds no version.';
          (get(RemoteDownloadsApi) as FakeRemoteDownloadsApi).previewError = (r) =>
            r.toVersion
              ? new ApiError(422, { code: 'validation_failed', status: 422, detail, requestId: REQUEST_ID, errors: [{ pointer: '/toVersion', code: 'out_of_range', detail }] }, REQUEST_ID)
              : null;
        },
      });
      for (const [id, value] of [['#preview-from-version', '18.6.0-rc.12'], ['#preview-to-version', '17.0.0']] as const) {
        const field = panel.querySelector<HTMLInputElement>(id)!;
        field.value = value;
        field.dispatchEvent(new Event('change'));
      }
      await until(() => !!panel.querySelector('#preview-to-version-error'), 'the refusal of the upper bound');
    });
  });

  // A sync chosen (R-CUR-027): the first run's count, the interval in words and that a sync stores only what the rules deliver; and a
  // package the rules do not deliver, said instead of being set up.
  for (const [state, delivered] of [['download-preview-sync', true], ['download-preview-sync-undelivered', false]] as const) {
    it(`a download ahead kept in sync${delivered ? '' : ', of a package the rules do not deliver'}`, async () => {
      await expectState(state, async () => {
        const { panel } = await openDialog(DownloadPreviewDialog, {
          width: '36rem',
          data: {
            repository: 'platform-engineering/npmjs-remote',
            request: { level: 'package', name: '@platform-engineering/kubernetes-infrastructure-operators' },
            title: 'Every version of @platform-engineering/kubernetes-infrastructure-operators',
          },
          setup: (get) => {
            const fake = get(RemoteDownloadsApi) as FakeRemoteDownloadsApi;
            fake.previewAnswer = {
              ...fake.previewAnswer,
              notDelivered: delivered ? 1 : 3,
              sync: { versions: delivered ? 2 : 0, delivered, intervalSeconds: 21_600, paused: false, existing: null },
            };
          },
        });
        await until(() => !!panel.querySelector('[data-testid="download-preview"]'), 'the preview');
        panel.querySelector<HTMLInputElement>('input[name="download-mode"][value="sync"]')!.click();
        await until(() => !!panel.querySelector(delivered ? '[data-testid="download-sync-note"]' : '[data-testid="download-sync-undelivered"]'), 'the sync');
      });
    });
  }

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

  // The package's switch where its versions have settings of their own (upstream-actions.ts switchPackage, Q-CUR-203).
  it('A package switched on whose versions have settings of their own: Keep or Reset', async () => {
    await expectState('upstream-switch-package-reset', () =>
      openDialog(ConfirmDialog, {
        maxWidth: '40rem',
        data: {
          title: 'Allow every version of @platform-engineering/kubernetes-infrastructure-operators?',
          lines: ['3 versions have settings of their own: 1 allowed, 2 blocked. They are kept unless you reset them.'],
          choice: 'Reset them: every version follows the package',
          choiceChecked: false,
          confirmLabel: 'Allow',
        },
      }),
    );
  });

  // The change of the delivery mode (repository-delivery.ts submit, R-CUR-025).
  const NAMES = Array.from({ length: 100 }, (_, i) => `@platform-engineering/kubernetes-infrastructure-operator-${String(i).padStart(3, '0')}`);
  const summary = (over: Partial<DeliveryRulesSummary['unruled']>, rules = { allow: 12, block: 3, sections: 1 }, maxRules = 50000): DeliveryRulesSummary => ({
    mode: 'allExceptBlocked',
    rules,
    maxRules,
    unruled: { count: 3412, names: NAMES, counted: true, ...over },
  });
  const mode = (to: DeliveryModeData['to'], answer: DeliveryRulesSummary, manage = true) =>
    openDialog(DeliveryModeDialog, {
      width: '36rem',
      data: { repository: 'platform-engineering/npmjs-remote', to },
      ...(manage ? {} : { capabilities: ['repositories:write-config'] }),
      setup: (get) => {
        (get(DeliveryRulesApi) as FakeDeliveryRulesApi).summaryAnswer = answer;
        if (!manage) (get(SessionApi) as FakeSessionApi).allow = (c) => c.action !== 'manage';
      },
    });

  it('The mode changed to Only what is allowed: the stored packages named and the offer ticked', async () => {
    await expectState('delivery-mode-offer', () => mode('onlyAllowed', summary({})));
  });

  it('The mode changed by a writer of the configuration who may not set rules: the list without the offer', async () => {
    await expectState('delivery-mode-no-right', () => mode('onlyAllowed', summary({}), false));
  });

  it('The mode changed where the repository cannot hold a rule for every stored package: no offer, the sentence why', async () => {
    await expectState('delivery-mode-beyond', () => mode('onlyAllowed', summary({ count: 50001, counted: false }, { allow: 40000, block: 3, sections: 1 })));
  });

  it('The mode changed where no stored package is delivered by the mode alone', async () => {
    await expectState('delivery-mode-none', () => mode('onlyAllowed', summary({ count: 0, names: [] })));
  });

  it('The mode changed to Everything except what is blocked: the allow rules stay', async () => {
    await expectState('delivery-mode-all', () => mode('allExceptBlocked', summary({ count: 0, names: [] })));
  });

  it('The mode dialog whose summary could not be read: the error notice with its request ID', async () => {
    await expectState('delivery-mode-failed', () =>
      openDialog(DeliveryModeDialog, {
        width: '36rem',
        data: { repository: 'platform-engineering/npmjs-remote', to: 'onlyAllowed' },
        setup: (get) => failAlways(get(DeliveryRulesApi), 'summary', serverFailure()),
      }),
    );
  });
});
