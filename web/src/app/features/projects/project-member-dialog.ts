import { Component, inject, signal } from '@angular/core';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';
import { PROJECT_ROLES, ProjectRole } from '../../api/projects-api';
import { SelectField, SelectOption } from '../../shared/access/fields';
import { PrincipalField } from '../../shared/ui/principal-field';
import { projectRoleHint, projectRoleLabel } from './project-roles';

export interface ProjectMemberDialogData {
  /** The project: its principal lookup finds the users and groups (Q-AUTHZ-182). */
  readonly project: string;
  /** The references that are members already. */
  readonly exclude: readonly string[];
}

export interface ProjectMemberChoice {
  readonly principal: string;
  readonly role: ProjectRole;
}

/**
 * Adds a member to a project (R-UI-041, Q-UI-214): the user or group from the shared principal field over the project's lookup
 * (R-UI-150), and its role.
 */
@Component({
  selector: 'quader-project-member-dialog',
  imports: [DialogTitle, DialogClose, PrincipalField, SelectField],
  template: `
    <h2 quaderDialogTitle i18n="@@project-members.add">Add member</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        <quader-principal-field
          i18n-label="@@project-members.find"
          label="User or group"
          name="member"
          [kinds]="kinds"
          [project]="data.project"
          [exclude]="data.exclude"
          [width]="'full'"
          [(value)]="chosen"
          i18n-hint="@@project-members.find-hint"
          hint="Type a name or a username."
        />
        @if (chosen().length > 0) {
          <quader-select-field
            i18n-label="@@project-members.role"
            label="Role"
            name="new-role"
            [options]="roleOptions"
            [value]="role()"
            (valueChange)="role.set($any($event))"
          />
          <p class="muted">{{ hint() }}</p>
        }
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@project-members.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="chosen().length === 0" i18n="@@project-members.add-confirm">
          Add member
        </button>
      </div>
    </form>
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class ProjectMemberDialog {
  protected readonly data = inject<ProjectMemberDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<ProjectMemberChoice>>(DialogRef);

  protected readonly kinds = ['user', 'group'] as const;
  protected readonly chosen = signal<readonly string[]>([]);
  protected readonly role = signal<ProjectRole>('developer');
  protected readonly roleOptions: readonly SelectOption[] = PROJECT_ROLES.map((r) => ({ value: r, label: projectRoleLabel(r) }));

  protected hint(): string {
    return projectRoleHint(this.role());
  }

  protected submit(event: Event): void {
    event.preventDefault();
    const principal = this.chosen()[0];
    if (principal) this.ref.close({ principal, role: this.role() });
  }
}
