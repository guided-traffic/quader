import { Component, computed, inject, signal } from '@angular/core';
import { ApiError } from '../../api/problem';
import { RegistryPreset } from '../../api/upstream-api';
import { SelectField, SelectOption, TextField } from '../../shared/access/fields';
import { ErrorNotice } from '../../shared/error-notice';
import { fieldMessage, problemsAt } from '../../shared/field-errors';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';

/** What the dialog of one registry of the installation is opened with (R-UI-195). */
export interface RegistryPresetDialogData {
  /** The formats with the remote class, by display name. */
  readonly formats: readonly SelectOption[];
  /** The entry to edit, or null for a new one. */
  readonly preset: RegistryPreset | null;
  /** The entry's place in the list the save sends: its field errors come back at `/presets/<index>/…`. */
  readonly index: number;
  /** Saves the entry within the whole list; resolves with null when it was saved, with the refusal otherwise. */
  readonly save: (preset: RegistryPreset) => Promise<ApiError | null>;
}

interface HostRow {
  readonly pattern: string;
  readonly sendCredentials: boolean;
}

/**
 * **Add a registry** and **Edit <name>** of the section Registries of this installation (R-UI-195, R-REPO-076): the format, the name of
 * the button, the upstream URL and the further hosts with the registry's token service. There is no credential field: each project
 * enters its own (Q-REPO-417). **Save** sends the whole list; a field error of this entry stands at its field and the dialog stays.
 */
@Component({
  selector: 'quader-registry-preset-dialog',
  imports: [DialogClose, DialogTitle, ErrorNotice, SelectField, TextField],
  template: `
    <h2 quaderDialogTitle>{{ title }}</h2>
    <div class="dialog-body">
      <!-- At the top, where the eye is after Save: at the form's end it stood out of sight at 390 px (Q-UI-1149). -->
      @if (other(); as e) {
        <quader-error-notice [error]="e" />
      }
      <form id="registry-preset" (submit)="submit($event)" novalidate>
        <quader-select-field
          i18n-label="@@presets.field.format"
          label="Format"
          name="presetFormat"
          [options]="data.formats"
          [value]="format()"
          [error]="error('format')"
          (valueChange)="format.set($event)"
        />
        <quader-text-field
          width="name"
          i18n-label="@@presets.field.name"
          label="Name"
          name="presetName"
          i18n-hint="@@presets.field.name.hint"
          hint="The label of the button in the form of a new remote repository."
          [error]="error('displayName')"
          [(value)]="name"
        />
        <quader-text-field
          width="full"
          i18n-label="@@upstream.url"
          label="Upstream URL"
          name="presetUrl"
          [error]="error('url')"
          [(value)]="url"
        />
        <div class="field hosts" role="group" aria-labelledby="preset-hosts-label">
          <span id="preset-hosts-label" class="field-label" i18n="@@upstream.hosts">Further hosts</span>
          <p class="hint" i18n="@@presets.hosts.hint">
            Name the registry's token service here and let it receive the credential, so that the credential a project enters reaches it.
          </p>
          @for (h of hosts(); track $index; let i = $index) {
            <div class="host-row">
              <label class="visually-hidden" [for]="'preset-host-' + i" i18n="@@upstream.hosts.pattern">Host pattern</label>
              <input
                class="text-input field-w-name"
                type="text"
                autocomplete="off"
                spellcheck="false"
                translate="no"
                [id]="'preset-host-' + i"
                [value]="h.pattern"
                [attr.aria-invalid]="hostError(i) ? 'true' : null"
                (input)="setHost(i, { pattern: $any($event.target).value })"
              />
              <label class="check-row">
                <input type="checkbox" [checked]="h.sendCredentials" (change)="setHost(i, { sendCredentials: $any($event.target).checked })" />
                <span i18n="@@upstream.hosts.send">Receives the credential</span>
              </label>
              <button class="btn btn-tertiary" type="button" (click)="removeHost(i)">
                <ng-container i18n="@@upstream.hosts.remove">Remove</ng-container><span class="visually-hidden" translate="no">&ngsp;{{ h.pattern }}</span>
              </button>
            </div>
            @if (hostError(i); as message) {
              <p class="field-error">{{ message }}</p>
            }
          } @empty {
            <p class="hint" i18n="@@upstream.hosts.none">No further host.</p>
          }
          <div>
            <button class="btn btn-default" type="button" (click)="addHost()" i18n="@@upstream.hosts.add">Add host</button>
          </div>
          @if (listError(); as message) {
            <p class="field-error">{{ message }}</p>
          }
        </div>
      </form>
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@confirm.cancel">Cancel</button>
      <button class="btn btn-confirm" type="submit" form="registry-preset" [disabled]="saving()" i18n="@@presets.save">Save</button>
    </div>
  `,
  styles: `
    .hosts {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-3);
    }
    .hosts .hint {
      margin: 0 0 var(--quader-space-1);
    }
    .host-row {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-1) var(--quader-space-4);
      margin-block-end: var(--quader-space-2);
    }
    /* The pattern, its checkbox and Remove on one line of the dialog; a long host scrolls inside its field. */
    .host-row .text-input {
      flex: 1 1 16rem;
      min-inline-size: 0;
    }
    .check-row {
      display: flex;
      align-items: center;
      gap: var(--quader-space-2);
    }
    .field-error {
      margin: 0 0 var(--quader-space-2);
      color: var(--quader-status-bad);
      font: var(--quader-type-body-small);
    }
  `,
})
export class RegistryPresetDialog {
  protected readonly data = inject<RegistryPresetDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<RegistryPreset, RegistryPresetDialog>>(DialogRef);

  protected readonly title = this.data.preset
    ? $localize`:@@presets.edit.title:Edit ${this.data.preset.displayName}:name:`
    : $localize`:@@presets.add.title:Add a registry`;
  protected readonly format = signal(this.data.preset?.format ?? this.data.formats[0]?.value ?? '');
  protected readonly name = signal(this.data.preset?.displayName ?? '');
  protected readonly url = signal(this.data.preset?.url ?? '');
  protected readonly hosts = signal<readonly HostRow[]>((this.data.preset?.extraHosts ?? []).map((h) => ({ ...h })));
  protected readonly saving = signal(false);
  private readonly failure = signal<ApiError | null>(null);
  private readonly prefix = `/presets/${this.data.index}`;

  /** A refusal that names no member of this entry, such as a conflict or the limit per format: it stands in the dialog. */
  protected readonly other = computed(() => {
    const e = this.failure();
    if (!e) return null;
    const own = [...e.fieldErrors.keys()].some((p) => p.startsWith(`${this.prefix}/`));
    return own ? null : e;
  });

  protected error(member: string): string | null {
    const problem = problemsAt(this.failure()?.fieldErrors ?? new Map(), `${this.prefix}/${member}`)[0];
    return problem ? fieldMessage(problem) : null;
  }

  /**
   * A refusal of the list of hosts itself (more hosts than allowed), under **Add host**. A host's own refusal stands under its row
   * alone: read below the list's pointer as well, it was said twice (Q-UI-1149).
   */
  protected listError(): string | null {
    const problem = this.failure()?.fieldErrors.get(`${this.prefix}/extraHosts`);
    return problem ? fieldMessage(problem) : null;
  }

  protected hostError(i: number): string | null {
    return this.error(`extraHosts/${i}/pattern`);
  }

  protected setHost(i: number, change: Partial<HostRow>): void {
    this.hosts.update((rows) => rows.map((h, j) => (j === i ? { ...h, ...change } : h)));
  }

  protected removeHost(i: number): void {
    this.hosts.update((rows) => rows.filter((_, j) => j !== i));
  }

  protected addHost(): void {
    this.hosts.update((rows) => [...rows, { pattern: '', sendCredentials: true }]);
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (this.saving()) return;
    const preset: RegistryPreset = {
      format: this.format(),
      displayName: this.name().trim(),
      url: this.url().trim(),
      // Every row is sent, an empty one too: its refusal comes back at its index and stands under it.
      extraHosts: this.hosts().map((h) => ({ pattern: h.pattern.trim(), sendCredentials: h.sendCredentials })),
    };
    this.saving.set(true);
    this.failure.set(null);
    try {
      const refused = await this.data.save(preset);
      if (refused) this.failure.set(refused);
      else this.ref.close(preset);
    } finally {
      this.saving.set(false);
    }
  }
}
