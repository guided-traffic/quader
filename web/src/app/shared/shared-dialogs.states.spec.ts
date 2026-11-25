import { DestroyRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, it } from 'vitest';
import { LONG_TEXT, app, dialogPanel, expectState, openDialog } from '../testing/states';
import { ConflictResolver } from './conflict-dialog';
import { DiffReview } from './diff-dialog';
import { DraftStore } from './drafts';
import { SessionDialog } from './session-dialog';

/** A draft and the stored version of an editor, as a 412 finds them: long names, a list, a description of German length. */
const mine = {
  description: LONG_TEXT,
  includePatterns: ['@platform-engineering/kubernetes-infrastructure-*', '@platform-engineering/release-tooling/**'],
  upstreamUrl: 'https://registry.npmjs.org/',
  retentionDays: 30,
};
const theirs = {
  description: 'Mirrors the public npm registry for the platform engineering teams of Frankfurt and Berlin.',
  includePatterns: ['@platform-engineering/*'],
  upstreamUrl: 'https://npm.registry.platform-engineering.eu-central-1.internal.example.com/repository/npm-proxy/',
  retentionDays: 90,
};

describe('shared dialogs in a browser (R-UI-004, R-UI-042, R-EP-017, R-UI-173, R-UI-175)', () => {
  it('Changed by someone else since you opened it: the conflict of a save answered 412', async () => {
    await expectState('conflict', async () => {
      await app();
      void TestBed.inject(ConflictResolver).resolve({ mine, theirs });
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
