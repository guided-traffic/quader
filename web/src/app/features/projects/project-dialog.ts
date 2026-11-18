import { Component, computed, inject, signal } from '@angular/core';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';
import { newIdempotencyKey } from '../../api/client';
import { ApiError } from '../../api/problem';
import { VisibilityApi, VisibilityChoices } from '../../api/visibility-api';
import {
  VisibilityLevel,
  VisibilityLevelControl,
  installationReasons,
  restrictedNotes,
} from '../../shared/ui/visibility-level';
import { PROJECT_KEY_PATTERN, Project, ProjectInput, ProjectsApi } from '../../api/projects-api';
import { Versioned } from '../../api/repositories-api';
import { ProjectContext } from '../../core/project-context';
import { ConflictResolver } from '../../shared/conflict-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { fieldErrorAt } from '../../shared/field-errors';
import { asApiError } from '../../shared/paged-resource';
import { TextField } from '../../shared/access/fields';
import { PrincipalField } from '../../shared/ui/principal-field';

/** What the dialog is opened with: nothing to create a project, the loaded project to edit it. */
export interface ProjectDialogData {
  readonly project?: Versioned<Project>;
}

/**
 * Create and edit a project (R-UI-041, R-AUTHZ-026). The key is fixed at creation. A new project takes its visibility level from the
 * radio group, preset to the installation's default (R-UI-054), with the levels the caller cannot choose disabled and a restricted level
 * marked for an administrator (R-UI-141); an existing project's level changes on its settings page only. A new project names its first
 * Owners (`owners`, Q-AUTHZ-229; the caller holds `projects:write`); an edit changes the display name and the
 * description (`PUT` with `If-Match`; a 412 opens the conflict dialog of R-UI-042), by an Owner or an administrator of
 * Quader. The members and their roles are changed on the project's screen, the repositories are created in the
 * project. The display name and the key are checked before the first request; the idempotency key of the create is
 * replaced after a refusal, because the server stores a 4xx outcome under it.
 */
@Component({
  selector: 'quader-project-dialog',
  imports: [DialogTitle, DialogClose, ErrorNotice, PrincipalField, TextField, VisibilityLevelControl],
  template: `
    <h2 quaderDialogTitle>
      @if (existing(); as p) {
        <ng-container i18n="@@project-dialog.title-edit">Edit project {{ p.value.key }}</ng-container>
      } @else {
        <ng-container i18n="@@project-dialog.title-new">New project</ng-container>
      }
    </h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (error(); as e) {
          <quader-error-notice [error]="e" />
        }
        <quader-text-field
          width="name"
          i18n-label="@@project-dialog.key"
          label="Key"
          name="key"
          [(value)]="key"
          [required]="true"
          [readonly]="existing() !== null"
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

        @if (!existing()) {
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
        }
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@project-dialog.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy()">
          @if (existing()) {
            <ng-container i18n="@@project-dialog.save">Save project</ng-container>
          } @else {
            <ng-container i18n="@@project-dialog.create">Create project</ng-container>
          }
        </button>
      </div>
    </form>
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    .move {
      color: var(--quader-text-muted);
      font: var(--quader-type-label-small);
    }
  `,
})
export class ProjectDialog {
  private readonly api = inject(ProjectsApi);
  private readonly conflicts = inject(ConflictResolver);
  private readonly context = inject(ProjectContext);
  private readonly ref = inject<DialogRef<Project, ProjectDialog>>(DialogRef);
  private readonly data = inject<ProjectDialogData>(DIALOG_DATA);
  private readonly visibilityApi = inject(VisibilityApi);
  /** Replaced after every refusal of the create: the server stores a 4xx outcome under its key (R-API-014). */
  private idempotencyKey = newIdempotencyKey();

  /** The project once it exists: the one being edited, or the one this dialog just created. */
  protected readonly existing = signal<Versioned<Project> | null>(this.data.project ?? null);
  protected readonly key = signal(this.data.project?.value.key ?? '');
  protected readonly displayName = signal(this.data.project?.value.displayName ?? '');
  protected readonly description = signal(this.data.project?.value.description ?? '');
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

  constructor() {
    if (this.data.project === undefined) {
      this.visibilityApi
        .choices()
        .then((c) => this.choices.set(c))
        .catch(() => undefined);
    }
  }

  protected readonly ownerKinds = ['user', 'group'] as const;

  protected keyError(): string | null {
    const server = fieldErrorAt(this.error(), '/key');
    if (server) return server;
    if (this.existing() || !this.attempted()) return null;
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
    const keyValid = this.existing() !== null || PROJECT_KEY_PATTERN.test(this.key().trim());
    if (!keyValid || !this.displayNameValid()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      if (!(await this.saveProject())) return;
      const key = this.existing()?.value.key ?? this.key().trim();
      this.context.refresh().catch(() => undefined);
      this.ref.close((await this.api.get(key)).value);
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }

  private body(): ProjectInput {
    return {
      displayName: this.displayName().trim(),
      description: this.description().trim(),
    };
  }

  /** Creates or replaces the project; false when the person left the conflict dialog without a choice. */
  private async saveProject(): Promise<boolean> {
    const current = this.existing();
    if (!current) {
      let created: Versioned<Project>;
      try {
        created = await this.api.create(
          {
            key: this.key().trim(),
            ...this.body(),
            ...(this.chosenLevel() ?? this.defaultLevel() ? { visibility: this.shownLevel() } : {}),
            ...(this.owners().length > 0 ? { owners: [...this.owners()] } : {}),
          },
          this.idempotencyKey,
        );
      } catch (e) {
        // The refusal is stored under the key; a corrected retry has another body and needs another key.
        // A request still in progress is the same intent: its key stays.
        if (e instanceof ApiError && e.status >= 400 && e.status < 500 && e.code !== 'idempotency_in_progress') {
          this.idempotencyKey = newIdempotencyKey();
        }
        throw e;
      }
      this.existing.set(created);
      return true;
    }
    const body = this.body();
    const stored = current.value;
    const patch: Record<string, unknown> = {};
    if (body.displayName !== stored.displayName) patch['displayName'] = body.displayName;
    if (body.description !== stored.description) patch['description'] = body.description;
    if (Object.keys(patch).length === 0) return true;
    const result = await this.conflicts.save<Project>({
      etag: current.etag,
      patch,
      send: (etag) => this.api.replace(stored.key, { key: stored.key, ...body }, etag),
      reload: () => this.api.get(stored.key),
    });
    if (result.outcome === 'dismissed') return false;
    this.existing.set(result.current);
    if (result.outcome === 'reloaded') {
      // "Reload (discard mine)": the fields show what is stored now; the person looks and saves again.
      const p = result.current.value;
      this.displayName.set(p.displayName);
      this.description.set(p.description);
      return false;
    }
    return true;
  }
}
