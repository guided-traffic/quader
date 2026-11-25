import { Component, computed, inject, signal } from '@angular/core';
import { DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';
import { newIdempotencyKey } from '../../api/client';
import { ApiError } from '../../api/problem';
import { VisibilityApi, VisibilityChoices } from '../../api/visibility-api';
import {
  VisibilityLevel,
  VisibilityLevelControl,
  installationReasons,
  restrictedNotes,
} from '../../shared/ui/visibility-level';
import { PROJECT_KEY_PATTERN, Project, ProjectsApi } from '../../api/projects-api';
import { Versioned } from '../../api/repositories-api';
import { ProjectContext } from '../../core/project-context';
import { ErrorNotice } from '../../shared/error-notice';
import { fieldErrorAt } from '../../shared/field-errors';
import { asApiError } from '../../shared/paged-resource';
import { TextField } from '../../shared/access/fields';
import { PrincipalField } from '../../shared/ui/principal-field';
import { Busy } from '../../shared/ui/busy';

/**
 * Create a project (R-UI-041, R-AUTHZ-026), from **New project** of the start page. The key is fixed at creation. A new project takes
 * its visibility level from the radio group, preset to the installation's default (R-UI-054), with the levels the caller cannot choose
 * disabled and a restricted level marked for an administrator (R-UI-141), and names its first Owners (`owners`, Q-AUTHZ-229; the caller
 * holds `projects:write`). A project is edited in place on its settings page (Q-UI-215), where its level changes too; the members and
 * their roles are changed on the project's screen, the repositories are created in the project. The display name and the key are
 * checked before the first request; the idempotency key of the create is replaced after a refusal, because the server stores a 4xx
 * outcome under it.
 */
@Component({
  selector: 'quader-project-dialog',
  imports: [Busy, DialogTitle, DialogClose, ErrorNotice, PrincipalField, TextField, VisibilityLevelControl],
  template: `
    <h2 quaderDialogTitle i18n="@@project-dialog.title-new">New project</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (error(); as e) {
          <quader-error-notice [error]="e" />
        }
        @if (choicesError(); as e) {
          <quader-error-notice [error]="e" (retry)="readChoices()" />
        }
        <quader-text-field
          width="name"
          i18n-label="@@project-dialog.key"
          label="Key"
          name="key"
          [(value)]="key"
          [required]="true"
          [error]="keyError()"
          i18n-hint="@@project-dialog.key-hint"
          hint="Lower-case letters, digits and -, two to 32 characters, starting with a letter and ending with a letter or digit. Cannot be changed later."
        />
        <quader-text-field
          width="name"
          i18n-label="@@project-dialog.display-name"
          label="Display name"
          name="displayName"
          [(value)]="displayName"
          [required]="true"
          [error]="displayNameError()"
        />
        <quader-text-field
          width="name"
          i18n-label="@@project-dialog.description"
          label="Description"
          name="description"
          type="textarea"
          [(value)]="description"
          [error]="fieldError('/description')"
        />

        <quader-visibility-level
          i18n-label="@@project-dialog.visibility"
          label="Visibility level"
          subject="project"
          [value]="shownLevel()"
          (valueChange)="chosenLevel.set($event)"
          [reasons]="levelReasons()"
          [notes]="levelNotes()"
        />
        <quader-principal-field
          i18n-label="@@project-dialog.owners"
          label="First Owners"
          name="owners"
          [kinds]="ownerKinds"
          [multiple]="true"
          [width]="'full'"
          [(value)]="owners"
          i18n-hint="@@project-dialog.owners-hint"
          hint="Users and groups can own a project; members are added on the project's screen."
        />
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@project-dialog.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy()" [quaderBusy]="busy()" i18n="@@project-dialog.create">Create project</button>
      </div>
    </form>
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class ProjectDialog {
  private readonly api = inject(ProjectsApi);
  private readonly context = inject(ProjectContext);
  private readonly ref = inject<DialogRef<Project, ProjectDialog>>(DialogRef);
  private readonly visibilityApi = inject(VisibilityApi);
  /** Replaced after every refusal of the create: the server stores a 4xx outcome under its key (R-API-014). */
  private idempotencyKey = newIdempotencyKey();

  /** The project once this dialog created it: a second submit, after the read of it failed, creates nothing again. */
  private readonly created = signal<Versioned<Project> | null>(null);
  protected readonly key = signal('');
  protected readonly displayName = signal('');
  protected readonly description = signal('');
  protected readonly owners = signal<readonly string[]>([]);
  /** The caller's choices (R-API-076): the installation's default level, the restricted levels, whether Public is available. */
  private readonly choices = signal<VisibilityChoices | null>(null);
  /** The installation's default level (R-UI-054); unknown until the choices are read, and then the server applies it. */
  private readonly defaultLevel = computed<VisibilityLevel | null>(() => this.choices()?.defaultLevel ?? null);
  protected readonly chosenLevel = signal<VisibilityLevel | null>(null);
  protected readonly shownLevel = computed<VisibilityLevel>(() => this.chosenLevel() ?? this.defaultLevel() ?? 'private');
  protected readonly levelReasons = computed(() => installationReasons(this.choices()));
  protected readonly levelNotes = computed(() => restrictedNotes(this.choices()));
  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  private readonly attempted = signal(false);

  /** The failed read of the levels the installation offers, shown in the dialog and read again by its notice (R-UI-216). */
  protected readonly choicesError = signal<ApiError | null>(null);

  constructor() {
    this.readChoices();
  }

  /** The levels the installation offers; without them every level read as open to choose, public among them. */
  protected readChoices(): void {
    this.visibilityApi.choices().then(
      (c) => {
        this.choicesError.set(null);
        this.choices.set(c);
      },
      (e: unknown) => this.choicesError.set(asApiError(e)),
    );
  }

  protected readonly ownerKinds = ['user', 'group'] as const;

  protected keyError(): string | null {
    const server = fieldErrorAt(this.error(), '/key');
    if (server) return server;
    if (this.created() || !this.attempted()) return null;
    return PROJECT_KEY_PATTERN.test(this.key().trim())
      ? null
      : $localize`:@@project-dialog.key-invalid:The key does not match the pattern.`;
  }

  /** The display name is required and at most 128 characters (R-UI-041); the server's word wins when it refused. */
  protected displayNameError(): string | null {
    const server = fieldErrorAt(this.error(), '/displayName');
    if (server) return server;
    return this.attempted() && !this.displayNameValid()
      ? $localize`:@@project-dialog.display-name-invalid:The display name is required, 1 to 128 characters.`
      : null;
  }

  private displayNameValid(): boolean {
    const length = this.displayName().trim().length;
    return length >= 1 && length <= 128;
  }

  protected fieldError(pointer: string): string | null {
    return fieldErrorAt(this.error(), pointer);
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    this.attempted.set(true);
    const keyValid = this.created() !== null || PROJECT_KEY_PATTERN.test(this.key().trim());
    if (!keyValid || !this.displayNameValid()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      await this.createProject();
      const key = this.created()?.value.key ?? this.key().trim();
      this.context.refresh().catch(() => undefined);
      this.ref.close((await this.api.get(key)).value);
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }

  /** Creates the project once; a later submit only reads it again. */
  private async createProject(): Promise<void> {
    if (this.created()) return;
    try {
      this.created.set(
        await this.api.create(
          {
            key: this.key().trim(),
            displayName: this.displayName().trim(),
            description: this.description().trim(),
            ...(this.chosenLevel() ?? this.defaultLevel() ? { visibility: this.shownLevel() } : {}),
            ...(this.owners().length > 0 ? { owners: [...this.owners()] } : {}),
          },
          this.idempotencyKey,
        ),
      );
    } catch (e) {
      // The refusal is stored under the key; a corrected retry has another body and needs another key.
      // A request still in progress is the same intent: its key stays.
      if (e instanceof ApiError && e.status >= 400 && e.status < 500 && e.code !== 'idempotency_in_progress') {
        this.idempotencyKey = newIdempotencyKey();
      }
      throw e;
    }
  }
}
