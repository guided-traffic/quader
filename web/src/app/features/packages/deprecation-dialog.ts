import { Component, computed, inject, signal } from '@angular/core';
import { PackageDeprecation, PackageDeprecationInput, PackagesApi } from '../../api/packages-api';
import { ApiError } from '../../api/problem';
import { TextField } from '../../shared/access/fields';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Busy } from '../../shared/ui/busy';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';

/** NuGet's reasons of a deprecation, in the catalog entry's spelling (R-NUGET-036). */
export const NUGET_REASONS = ['Legacy', 'CriticalBugs', 'Other'] as const;

export interface DeprecationDialogData {
  readonly format: string;
  /** The package's name, as the format spells it. */
  readonly name: string;
  /** The version; the dialog deprecates the whole package where the format deprecates packages (Ansible Galaxy). */
  readonly version: string;
  /** The local repositories that hold the version and in which the reader may deploy; the first is chosen (R-API-085). */
  readonly repositories: readonly string[];
  /** The deprecation the version holds now, which the dialog changes; null for a new one. */
  readonly current: PackageDeprecation | null;
}

/** The words of a NuGet reason, as Visual Studio says them. */
export function reasonText(reason: string): string {
  switch (reason) {
    case 'Legacy':
      return $localize`:@@pkg.deprecate.reason.legacy:Legacy, no longer maintained`;
    case 'CriticalBugs':
      return $localize`:@@pkg.deprecate.reason.critical:Critical bugs`;
    default:
      return $localize`:@@pkg.deprecate.reason.other:Other`;
  }
}

/**
 * **Deprecate** a version, or an Ansible collection as a whole (R-API-085, Q-UI-1301): a small dialog of what the format carries, opened
 * from the menu of the version's head. npm takes a message, which npm install prints; NuGet the reasons, a message and an alternate
 * package, which Visual Studio and `dotnet list package --deprecated` read; Ansible Galaxy the flag, and a message for Quader's pages. A
 * refusal of the server is shown in the dialog whole (R-UI-216). The dialog closes with the deprecation the version holds then.
 */
@Component({
  selector: 'quader-deprecation-dialog',
  imports: [Busy, DialogTitle, DialogClose, ErrorNotice, TextField],
  template: `
    <!-- The package's name is the publisher's word: a token that breaks where it must, never translated (R-UI-171). -->
    <h2 quaderDialogTitle>
      @if (data.current) {
        @if (collection) {
          <ng-container i18n="@@pkg.deprecate.title-change-collection"
            >Change the deprecation of the collection <span translate="no">{{ data.name }}</span></ng-container
          >
        } @else {
          <ng-container i18n="@@pkg.deprecate.title-change"
            >Change the deprecation of <span translate="no">{{ data.name }} {{ data.version }}</span></ng-container
          >
        }
      } @else if (collection) {
        <ng-container i18n="@@pkg.deprecate.title-collection">Deprecate the collection <span translate="no">{{ data.name }}</span></ng-container>
      } @else {
        <ng-container i18n="@@pkg.deprecate.title">Deprecate <span translate="no">{{ data.name }} {{ data.version }}</span></ng-container>
      }
    </h2>
    <div class="dialog-body">
      <p class="lead">{{ effect }}</p>
      @if (data.repositories.length > 1) {
        <div class="field">
          <label class="field-label" for="deprecation-repository" i18n="@@pkg.deprecate.repository">Repository</label>
          <select
            id="deprecation-repository"
            class="select-input field-w-name"
            translate="no"
            (change)="repository.set($any($event.target).value)"
          >
            @for (r of data.repositories; track r) {
              <option [value]="r" [selected]="r === repository()">{{ r }}</option>
            }
          </select>
        </div>
      }
      @if (nuget) {
        <fieldset class="reasons">
          <legend class="field-label" i18n="@@pkg.deprecate.reasons">Reason</legend>
          @for (r of reasons; track r) {
            <label class="check-row">
              <input type="checkbox" [checked]="chosen().includes(r)" (change)="choose(r, $any($event.target).checked)" />
              <span>{{ reasonText(r) }}</span>
            </label>
          }
        </fieldset>
      }
      <quader-text-field
        type="textarea"
        width="full"
        name="message"
        [label]="messageLabel"
        [hint]="messageHint"
        [required]="npm"
        [(value)]="message"
      />
      @if (nuget) {
          <quader-text-field
            width="name"
            name="alternate"
            i18n-label="@@pkg.deprecate.alternate"
            label="Use instead (optional)"
            i18n-hint="@@pkg.deprecate.alternate-hint"
            hint="The ID of the package to use in its place."
            [(value)]="alternate"
          />
          <quader-text-field
            width="bytes"
            name="range"
            i18n-label="@@pkg.deprecate.range"
            label="Its versions"
            i18n-hint="@@pkg.deprecate.range-hint"
            hint="A NuGet range such as [3.0.0, ); empty for any."
            [(value)]="range"
          />
      }
      @if (failure(); as f) {
        <quader-error-notice [error]="f" />
      }
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@pkg.deprecate.cancel">Cancel</button>
      <button class="btn btn-confirm" type="button" [disabled]="busy() || !complete()" [quaderBusy]="busy()" (click)="save()">
        {{ confirmText }}
      </button>
    </div>
  `,
  styles: `
    /* A package's name is one long word (a NuGet ID, a scoped npm name): it breaks rather than widen the dialog. */
    h2 {
      overflow-wrap: anywhere;
    }
    .lead {
      margin-block: 0 var(--quader-space-3);
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-3);
    }
    .reasons {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin: 0 0 var(--quader-space-3);
      padding: 0;
      border: 0;
    }
  `,
})
export class DeprecationDialog {
  protected readonly data = inject<DeprecationDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<PackageDeprecation, DeprecationDialog>>(DialogRef);
  private readonly api = inject(PackagesApi);

  protected readonly npm = this.data.format === 'npm';
  protected readonly nuget = this.data.format === 'nuget';
  protected readonly collection = this.data.format === 'ansible';
  protected readonly reasons = NUGET_REASONS;
  protected readonly reasonText = reasonText;

  protected readonly repository = signal(this.data.repositories[0] ?? '');
  protected readonly message = signal(this.data.current?.message ?? '');
  protected readonly chosen = signal<readonly string[]>(this.data.current?.reasons ?? (this.nuget ? ['Legacy'] : []));
  protected readonly alternate = signal(this.data.current?.alternatePackage?.name ?? '');
  protected readonly range = signal(
    this.data.current?.alternatePackage?.range === '*' ? '' : (this.data.current?.alternatePackage?.range ?? ''),
  );
  protected readonly busy = signal(false);
  protected readonly failure = signal<ApiError | null>(null);

  /** What the format needs before the dialog may send: npm a message, NuGet a reason. */
  protected readonly complete = computed(
    () => (!this.npm || this.message().trim() !== '') && (!this.nuget || this.chosen().length > 0) && this.repository() !== '',
  );

  protected readonly confirmText = this.data.current
    ? $localize`:@@pkg.deprecate.save:Save the deprecation`
    : $localize`:@@pkg.deprecate.confirm:Deprecate`;
  /** What the deprecation does for the format's clients, so the reader knows what the message reaches. */
  protected readonly effect = this.npm
    ? $localize`:@@pkg.deprecate.effect-npm:npm install prints the message as a warning, and a range installs another version where one is not deprecated.`
    : this.nuget
      ? $localize`:@@pkg.deprecate.effect-nuget:Visual Studio and dotnet list package --deprecated show the reasons and the package to use instead. Restores go on as before.`
      : $localize`:@@pkg.deprecate.effect-ansible:Every version of the collection is marked deprecated. ansible-galaxy installs it as before and shows nothing.`;
  protected readonly messageLabel = this.npm
    ? $localize`:@@pkg.deprecate.message:Message`
    : $localize`:@@pkg.deprecate.message-optional:Message (optional)`;
  protected readonly messageHint = this.npm
    ? $localize`:@@pkg.deprecate.message-hint-npm:Plain text, at most 1,024 characters.`
    : $localize`:@@pkg.deprecate.message-hint-quader:Plain text, shown on Quader's pages.`;

  protected choose(reason: string, on: boolean): void {
    this.chosen.update((v) => (on ? NUGET_REASONS.filter((r) => r === reason || v.includes(r)) : v.filter((r) => r !== reason)));
  }

  protected async save(): Promise<void> {
    if (this.busy() || !this.complete()) return;
    const input: PackageDeprecationInput = {
      ...(this.collection ? {} : { version: this.data.version }),
      ...(this.message().trim() ? { message: this.message().trim() } : {}),
      ...(this.nuget ? { reasons: [...this.chosen()] } : {}),
      ...(this.nuget && this.alternate().trim()
        ? { alternatePackage: { name: this.alternate().trim(), ...(this.range().trim() ? { range: this.range().trim() } : {}) } }
        : {}),
    };
    this.busy.set(true);
    this.failure.set(null);
    try {
      this.ref.close(await this.api.setDeprecation(this.repository(), this.data.name, input));
    } catch (e) {
      this.failure.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
