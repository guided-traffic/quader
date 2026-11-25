import { DatePipe } from '@angular/common';
import { Component, effect, inject, input, untracked } from '@angular/core';
import { LIVE_SLOW_MS, live } from '../../core/live-refresh';
import { AnsibleApi, AnsibleImportTask } from '../../api/ansible-api';
import { PagedResource } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { Disclosure } from '../../shared/ui/disclosure';

/** The words of a task state; the colour is a second signal, never the only one (R-UI-044). */
export function importStateText(state: AnsibleImportTask['state']): string {
  return state === 'completed'
    ? $localize`:@@ansible.imports.state.completed:Completed`
    : $localize`:@@ansible.imports.state.failed:Failed`;
}

/**
 * The import tasks of the publishes to a local Ansible Galaxy repository, newest first (R-ANS-009, R-ANS-026): the collection and
 * version, the state, the time, and the messages the client printed, the reason of a failed one first. A virtual repository shows the
 * tasks of its deployment target. A task is final when it is created and is deleted after the retention period; the version stays.
 */
@Component({
  selector: 'quader-ansible-import-tasks',
  imports: [DatePipe, DataTable, Disclosure],
  template: `
    <section class="imports" aria-labelledby="ansible-imports-heading">
      <h2 id="ansible-imports-heading" class="visually-hidden" i18n="@@ansible.imports.heading">Import tasks</h2>
      <p class="hint" i18n="@@ansible.imports.intro">
        Every publish that got as far as reading the archive has a task. It is already final when ansible-galaxy first asks, and it is kept
        for a limited time; the published version stays.
      </p>
      <quader-data-table
        [caption]="caption"
        [columns]="columns"
        [loaded]="list.loaded()"
        [loading]="list.loading()"
        loadingText="Loading the import tasks."
        i18n-loadingText="@@ansible.imports.loading"
        [error]="list.error()"
        [empty]="list.loaded() && list.items().length === 0"
        [emptyText]="emptyText"
        [hasMore]="list.hasMore()"
        (more)="list.more()"
        (retry)="load()"
        data-testid="ansible-imports-table"
      >
        @for (task of list.items(); track task.id) {
          <tr [attr.data-state]="task.state">
            <th scope="row" translate="no">{{ task.namespace }}.{{ task.name }}</th>
            <td translate="no">{{ task.version }}</td>
            <td>
              <span class="badge" [class.ok]="task.state === 'completed'" [class.bad]="task.state === 'failed'">
                <span class="material-icons inline" aria-hidden="true">{{ task.state === 'completed' ? 'check_circle' : 'error' }}</span>
                {{ stateText(task.state) }}
              </span>
              @if (task.error; as error) {
                <p class="reason" data-testid="ansible-import-error">{{ error.description }}</p>
              }
            </td>
            <td><time [attr.datetime]="task.createdAt">{{ task.createdAt | date: 'medium' }}</time></td>
            <td>
              @if (task.messages.length > 0) {
                <quader-disclosure [inline]="true" [heading]="messagesOf(task.messages.length)">
                  <ul class="messages">
                    @for (m of task.messages; track $index) {
                      <li><span class="level" translate="no">{{ m.level }}</span>&ngsp;<span lang="en" translate="no">{{ m.message }}</span></li>
                    }
                  </ul>
                </quader-disclosure>
              } @else {
                –
              }
            </td>
          </tr>
        }
      </quader-data-table>
    </section>
  `,
  styles: `
    .imports {
      margin-block-end: var(--quader-space-6);
    }
    .reason {
      margin: var(--quader-space-1) 0 0;
      max-width: 40rem;
    }
    /* A message's word longer than its line (a digest, a path) breaks where it must, never a shorter one. */
    .messages {
      margin: var(--quader-space-1) 0 0;
      padding-inline-start: var(--quader-space-4);
      overflow-wrap: break-word;
    }
    .level {
      font-weight: 600;
    }
  `,
})
export class AnsibleImportTasks {
  private readonly api = inject(AnsibleApi);

  /** The qualified name `<project>/<key>` of the repository. */
  readonly key = input.required<string>();

  protected readonly list = new PagedResource<AnsibleImportTask>();
  protected readonly stateText = importStateText;
  protected readonly caption = $localize`:@@ansible.imports.caption:Import tasks of this repository, newest first`;
  /** The toggle of a row's messages names how many there are: "1 message", "3 messages". */
  protected messagesOf(count: number): string {
    return count === 1 ? $localize`:@@ansible.imports.one-message:1 message` : $localize`:@@ansible.imports.messages:${count}:count: messages`;
  }
  protected readonly emptyText = $localize`:@@ansible.imports.empty:No collection has been published to this repository yet, or the tasks have been deleted.`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'collection', label: $localize`:@@ansible.imports.col.collection:Collection` },
    // The short columns keep their width and the messages take the rest: a row's messages unfold in a fold that asks its column
    // for no room, and the column was 90 px wide at 1,024 px, a word a line, its digest 10 px into the next cell (2026-11-25).
    { id: 'version', label: $localize`:@@ansible.imports.col.version:Version`, width: '6rem' },
    { id: 'state', label: $localize`:@@ansible.imports.col.state:State`, width: '9rem' },
    { id: 'created', label: $localize`:@@ansible.imports.col.created:Published`, width: '12rem' },
    {
      id: 'messages',
      label: $localize`:@@ansible.imports.col.messages:Messages`,
      help: $localize`:@@ansible.imports.help.messages:The number of messages of the import. Open the row to read them.`,
    },
  ];

  constructor() {
    effect(() => {
      this.key();
      untracked(() => this.load());
    });
    // A publish adds an import: the tasks shown are read again every half minute in place (R-UI-213).
    live({ reload: () => this.list.refresh(), every: () => LIVE_SLOW_MS });
  }

  protected load(): void {
    void this.list.start(this.api.importTasks(this.key()));
  }
}
