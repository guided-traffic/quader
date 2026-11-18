import { Component, computed, inject, input, signal, viewChild } from '@angular/core';
import { ProjectContext } from '../../core/project-context';
import { SessionStore } from '../../core/session-store';
import { ProjectMembers } from './project-members';
import { PageHeader } from '../../shared/ui/page-header';

/**
 * The members of the project and their roles (R-UI-041, Q-UI-129, Q-UI-214): the screen of `/projects/:project/members`. The page
 * has one view, so it has no tab strip; the number of members stands after the heading (R-UI-142, Q-UI-550).
 */
@Component({
  selector: 'quader-project-members-page',
  imports: [PageHeader, ProjectMembers],
  template: `
    <quader-page-header heading="Members" i18n-heading="@@project.members" [description]="hint" [count]="count()">
      @if (canWrite()) {
        <button actions class="btn btn-confirm" type="button" (click)="list().openAdd()" i18n="@@project-members.add">Add member</button>
      }
    </quader-page-header>
    <quader-project-members [project]="project()" (changed)="context.refresh()" (count)="count.set($event)" />
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class ProjectMembersPage {
  protected readonly hint = $localize`:@@project-members.hint:Users and groups with their role in the project. The role decides what a member may do in the project's repositories.`;
  /** Route parameter `:project`. */
  readonly project = input.required<string>();

  protected readonly context = inject(ProjectContext);
  protected readonly list = viewChild.required(ProjectMembers);
  /** The number of members, once the list has read them. */
  protected readonly count = signal<number | null>(null);
  private readonly session = inject(SessionStore);
  /** As in the list; the API decides in any case (R-UI-005). */
  protected readonly canWrite = computed(() => this.session.has('projects:write') || this.session.isOwnerOf(this.project()));
}
