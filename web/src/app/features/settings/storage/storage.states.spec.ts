import { describe, it } from 'vitest';
import { StorageApi } from '../../../api/storage-api';
import { sampleFinding } from '../../../api/testing/fakes-storage';
import { LONG_TEXT, expectState, failNext, fill, openDialog, serverFailure, submit } from '../../../testing/states';
import { AcknowledgeDialog } from './acknowledge-dialog';

/**
 * The acknowledge dialog of a damaged copy (api-and-ui.md R-UI-265, Q-UI-1607), as the findings' row menu opens it (findings-list.ts,
 * width 32 rem): an open copy no other copy held with a long note, a note longer than 500 characters under its field, and the server's
 * refusal with its request ID.
 */
const open = sampleFinding({
  id: 4190,
  connection: 'aws-frankfurt-backup-copy',
  status: 'missing',
  source: 'download',
  reason: 'not_found',
  unavailable: true,
  resolution: null,
  resolvedAt: null,
});

describe('the acknowledge dialog of a damaged copy in a browser (R-UI-265, R-UI-173, R-UI-175)', () => {
  it('an open copy with a long note', async () => {
    await expectState('storage-acknowledge', async () => {
      const { panel } = await openDialog(AcknowledgeDialog, { width: '32rem', data: open });
      await fill(panel, 'Note', Array(3).fill(LONG_TEXT).join(' '));
    });
  });

  it('a note longer than 500 characters, said under its field', async () => {
    await expectState('storage-acknowledge-long', async () => {
      const { panel } = await openDialog(AcknowledgeDialog, { width: '32rem', data: open });
      await fill(panel, 'Note', Array(5).fill(LONG_TEXT).join(' ').slice(0, 520));
      await submit(panel);
    });
  });

  it('failed: the error notice with its request ID', async () => {
    await expectState('storage-acknowledge-failed', async () => {
      const { panel, get } = await openDialog(AcknowledgeDialog, { width: '32rem', data: open });
      failNext(get(StorageApi), 'acknowledge', serverFailure());
      await submit(panel);
    });
  });
});
