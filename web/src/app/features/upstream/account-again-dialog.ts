import { Component, computed, inject, signal } from '@angular/core';
import { UpstreamRepositoryAccount } from '../../api/upstream-api';
import { SelectField, SelectOption, TextField } from '../../shared/access/fields';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';
import { ValueList } from '../../shared/value-list';
import { AuthMode, authOptions } from './upstream-account';

export interface AccountAgainData {
  /** The hosts the stored account goes to now; the dialog says them as one list of values (Q-UI-1213). */
  readonly hosts: readonly string[];
  readonly authMode: AuthMode;
  readonly username: string;
}

/** The answer: the account to send with the address, or `null` to remove it. */
export interface AccountAgainAnswer {
  readonly account: UpstreamRepositoryAccount | null;
}

/**
 * The question of R-UI-188 when a new address moves the hosts that receive a stored account: the account was entered for the
 * old hosts, so it is entered again for the new address or removed, and the save sends it with the address in one request
 * (R-REPO-069). Cancel keeps the page as it was.
 */
@Component({
  selector: 'quader-account-again-dialog',
  imports: [DialogClose, DialogTitle, SelectField, TextField, ValueList],
  template: `
    <h2 quaderDialogTitle i18n="@@wellknown.again.title">Enter the account again</h2>
    <div class="dialog-body">
      <p i18n="@@wellknown.again.text">The account goes to <quader-value-list [values]="data.hosts" /> only. Enter it again for the new address, or remove it.</p>
      <form id="account-again" (submit)="save($event)" novalidate>
        <quader-select-field
          i18n-label="@@upstream.auth"
          label="Authentication"
          name="againAuth"
          [options]="modes"
          [value]="mode()"
          (valueChange)="mode.set($any($event))"
        />
        @if (mode() === 'basic') {
          <quader-text-field width="name" i18n-label="@@upstream.username" label="Username" name="againUser" [(value)]="username" />
        }
        @if (mode() !== 'none') {
          <quader-text-field
            width="name"
            type="password"
            [secret]="true"
            autocomplete="new-password"
            i18n-label="@@upstream.password"
            label="Password or token"
            name="againPassword"
            [(value)]="password"
          />
        }
      </form>
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@confirm.cancel">Cancel</button>
      <button class="btn btn-default" type="button" (click)="remove()" i18n="@@wellknown.account.remove">Remove account</button>
      <button class="btn btn-confirm" type="submit" form="account-again" [disabled]="!ready()" i18n="@@wellknown.again.save">Save with this account</button>
    </div>
  `,
})
export class AccountAgainDialog {
  protected readonly data = inject<AccountAgainData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<AccountAgainAnswer, AccountAgainDialog>>(DialogRef);

  protected readonly modes: readonly SelectOption[] = authOptions();
  protected readonly mode = signal<AuthMode>(this.data.authMode);
  protected readonly username = signal(this.data.username);
  protected readonly password = signal('');
  /** The account needs its secret again: the old one was the old hosts'. */
  protected readonly ready = computed(() => this.mode() === 'none' || this.password() !== '');

  protected save(event: Event): void {
    event.preventDefault();
    if (!this.ready()) return;
    const mode = this.mode();
    this.ref.close({
      account:
        mode === 'none'
          ? null
          : {
              authMode: mode,
              ...(mode === 'basic' ? { username: this.username() } : {}),
              password: { value: this.password() },
            },
    });
  }

  protected remove(): void {
    this.ref.close({ account: null });
  }
}
