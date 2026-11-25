import { TestBed } from '@angular/core/testing';
import { describe, it } from 'vitest';
import { ApiError } from '../../api/problem';
import { app, dialogPanel, expectState, fieldRefusal, openDialog, serverFailure, submit } from '../../testing/states';
import { AccountAgainDialog } from './account-again-dialog';
import { RegistryPresetDialog, RegistryPresetDialogData } from './registry-preset-dialog';
import { UpstreamActions } from './upstream-actions';

/** An entry of the well-known registries with long values, edited at its place in the list (registry-presets.ts). */
function preset(refusal: ApiError): RegistryPresetDialogData {
  return {
    formats: [
      { value: 'oci', label: 'OCI (Docker)' },
      { value: 'npm', label: 'npm' },
    ],
    index: 3,
    preset: {
      format: 'oci',
      displayName: 'Harbor der Bereitstellungsverantwortlichen Frankfurt',
      url: 'https://harbor.platform-engineering.eu-central-1.internal.example.com/v2/',
      extraHosts: [
        { pattern: 'auth.harbor.platform-engineering.eu-central-1.internal.example.com', sendCredentials: true },
        { pattern: '*.blob.core.windows.net', sendCredentials: false },
      ],
    },
    save: () => Promise.resolve(refusal),
  };
}

const openPreset = (refusal: ApiError) => openDialog(RegistryPresetDialog, { maxWidth: '44rem', width: '44rem', data: preset(refusal) });

describe('dialogs of the upstream registries in a browser (R-UI-187, R-UI-188, R-UI-042, R-UI-173, R-UI-175)', () => {
  it('Set the visibility of every registry, as it opens', async () => {
    await expectState('upstream-set-all', async () => {
      await app();
      void TestBed.inject(UpstreamActions).setAllVisibility();
      await dialogPanel();
    });
  });

  it('Enter the account again, after a new address moved the hosts', async () => {
    await expectState('upstream-account-again', () =>
      openDialog(AccountAgainDialog, {
        maxWidth: '40rem',
        data: {
          hosts: 'harbor.platform-engineering.eu-central-1.internal.example.com and auth.harbor.platform-engineering.eu-central-1.internal.example.com',
          authMode: 'basic',
          username: 'robot$platform-engineering+release-candidates-pull',
        },
      }),
    );
  });

  it("a well-known registry's entry, its fields refused: each sentence under its field", async () => {
    await expectState('upstream-preset-invalid', async () => {
      const { panel } = await openPreset(
        fieldRefusal({
          '/presets/3/displayName': 'Another entry of the format OCI has the name Harbor der Bereitstellungsverantwortlichen Frankfurt.',
          '/presets/3/url': 'The URL must use https and name a host; harbor.platform-engineering.eu-central-1.internal.example.com does not resolve.',
          '/presets/3/extraHosts/0/pattern': 'A host pattern holds at most one * and only at its start.',
        }),
      );
      await submit(panel);
    });
  });

  it("a well-known registry's entry, refused as a whole: the error notice with its request ID", async () => {
    await expectState('upstream-preset-failed', async () => {
      const { panel } = await openPreset(serverFailure());
      await submit(panel);
    });
  });
});

