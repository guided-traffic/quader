import { Component, effect, inject, input, untracked } from '@angular/core';
import { LIVE_SLOW_MS, live } from '../../core/live-refresh';
import { AnsibleApi, AnsibleImportTask } from '../../api/ansible-api';
import { ErrorNotice } from '../../shared/error-notice';
import { PagedResource } from '../../shared/paged-resource';
import { AnsibleImportTaskView } from './ansible-import-task';
import { Loading } from '../../shared/ui/loading';
import { Busy } from '../../shared/ui/busy';

/**
 * The Import log tab of a version held by a local repository (R-UI-063): the import tasks of that version, newest first, each as the
 * dialog Upload collection shows its one (ansible-import-task.ts): its state as a badge, its time and its messages, the warnings of the
 * import (R-ANS-062) and of the generator (R-ANS-034) among them. It is the task of
 * R-ANS-009, readable by whoever reads the repository, and not wiki content.
 */
@Component({
  selector: 'quader-ansible-import-log',
  imports: [Busy, Loading, ErrorNotice, AnsibleImportTaskView],
  template: `
    @if (list.error(); as e) {
      <quader-error-notice [error]="e" (retry)="load()" />
    } @else if (!list.loaded()) {
      <quader-loading i18n="@@pkg.import-log.loading">Loading the import log.</quader-loading>
    } @else if (list.items().length === 0) {
      <p class="empty" data-testid="import-log-empty" i18n="@@docs.import-log.empty">
        No import task of this version is kept. A task is deleted after the retention period; the version stays.
      </p>
    } @else {
      @for (task of list.items(); track task.id) {
        <section class="task" [attr.data-state]="task.state" [attr.aria-label]="label(task)">
          <quader-ansible-import-task [task]="task" />
        </section>
      }
      @if (list.hasMore()) {
        <p>
          <button type="button" class="btn btn-default" [disabled]="list.loading()" [quaderBusy]="list.loading()" (click)="list.more()" i18n="@@list.load-more">
            Load more
          </button>
        </p>
      }
    }
  `,
  styles: `
    .task {
      margin-block-end: var(--quader-space-4);
      padding-block-end: var(--quader-space-3);
      border-block-end: 1px solid var(--quader-border);
    }
  `,
})
export class AnsibleImportLog {
  private readonly api = inject(AnsibleApi);

  /** The qualified name `<project>/<key>` of the local repository that holds the version. */
  readonly repository = input.required<string>();
  /** The collection, `<namespace>.<name>`. */
  readonly collection = input.required<string>();
  readonly version = input.required<string>();

  protected readonly list = new PagedResource<AnsibleImportTask>();

  constructor() {
    effect(() => {
      this.repository();
      this.collection();
      this.version();
      untracked(() => this.load());
    });
    // A publish of the version again adds an import: the tasks shown are read again every half minute in place (R-UI-213).
    live({ reload: () => this.list.refresh(), every: () => LIVE_SLOW_MS });
  }

  protected label(task: AnsibleImportTask): string {
    const collection = `${task.namespace}.${task.name}`;
    return $localize`:@@docs.import-log.task:Import of ${collection}:collection: ${task.version}:version:`;
  }

  protected load(): void {
    void this.list.start(
      this.api.importTasks(this.repository(), { package: this.collection(), version: this.version() }),
    );
  }
}
