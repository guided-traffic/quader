import { DestroyRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, it } from 'vitest';
import { LONG_TEXT, app, dialogPanel, expectState, openDialog } from '../testing/states';
import { ConflictResolver } from './conflict-dialog';
import { DiffReview } from './diff-dialog';
import { DraftStore } from './drafts';
import { SessionDialog } from './session-dialog';

/**
 * A draft and the stored version of an editor, as a 412 finds them: long names, a list, a description of German length, a switch, a
 * password, and a member the form does not name (its API name stands, Q-UI-1210).
 */
const mine = {
  description: LONG_TEXT,
  includePatterns: ['@platform-engineering/kubernetes-infrastructure-*', '@platform-engineering/release-tooling/**'],
  remote: {
    url: 'https://registry.npmjs.org/',
    password: { value: 'a-secret-that-never-shows' },
    downloadOnProbe: true,
    cache: { metadataTtlSeconds: 300 },
  },
  retentionDays: 30,
};
const theirs = {
  description: 'Mirrors the public npm registry for the platform engineering teams of Frankfurt and Berlin.',
  includePatterns: ['@platform-engineering/*'],
  remote: {
    url: 'https://npm.registry.platform-engineering.eu-central-1.internal.example.com/repository/npm-proxy/',
    downloadOnProbe: false,
    cache: { metadataTtlSeconds: 900 },
  },
  retentionDays: 90,
};
/** The labels the settings page gives these fields. */
const fields = {
  '/description': 'Description (optional)',
  '/includePatterns': 'Include patterns',
  '/remote/url': 'Upstream URL',
  '/remote/password': { label: 'Password or token', secret: true },
  '/remote/downloadOnProbe': 'Start the download when a version is asked for',
  '/remote/cache/metadataTtlSeconds': 'Metadata cache period in seconds',
};

describe('shared dialogs in a browser (R-UI-004, R-UI-042, R-EP-017, R-UI-173, R-UI-175)', () => {
  it('Changed by someone else since you opened it: the conflict of a save answered 412', async () => {
    await expectState('conflict', async () => {
      await app();
      void TestBed.inject(ConflictResolver).resolve({ mine, theirs, fields });
      await dialogPanel();
    });
  });

  it('What will change, before a save', async () => {
    await expectState('diff', async () => {
      await app();
      void TestBed.inject(DiffReview).confirm({
        title: 'Save the exposure rule platform-engineering-release-candidates?',
        confirmLabel: 'Save exposure rule',
        mine,
        theirs,
        fields,
      });
      await dialogPanel();
    });
  });

  it('Your session has ended, with an editor\'s draft to copy (app.ts opens it on a 401 session_expired)', async () => {
    await expectState('session-ended', () =>
      openDialog(SessionDialog, {
        data: { kind: 'ended' },
        disableClose: true,
        setup: (get) => get(DraftStore).track(() => ({ description: LONG_TEXT }), TestBed.inject(DestroyRef)),
      }),
    );
  });

  it('Confirm it is you: an action that needs a recent sign-in', async () => {
    await expectState('session-reauth', () => openDialog(SessionDialog, { data: { kind: 'reauth' } }));
  });
});
