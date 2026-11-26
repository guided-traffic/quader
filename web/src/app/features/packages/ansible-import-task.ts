import { DatePipe } from '@angular/common';
import { Component, input } from '@angular/core';
import { AnsibleImportTask } from '../../api/ansible-api';
import { importStateText } from '../repositories/ansible-import-tasks';

/**
 * One import task of a publish (R-ANS-009), as the tab Import log of a version and the dialog Upload collection show it (R-UI-063,
 * R-ANS-061): its state as a badge and its time, the reason and the code of a failed one, and its messages in the order the import
 * wrote them, each with its level; a warning's and an error's level stand in their colour, as Galaxy's import log colours them. The
 * messages are the server's English text and are not translated.
 */
@Component({
  selector: 'quader-ansible-import-task',
  imports: [DatePipe],
  template: `
    @let t = task();
    <header class="task-head">
      <span class="badge" [class.ok]="t.state === 'completed'" [class.bad]="t.state === 'failed'">
        <span class="material-icons inline" aria-hidden="true">{{ t.state === 'completed' ? 'check_circle' : 'error' }}</span>
        {{ stateText(t.state) }}
      </span>
      <time [attr.datetime]="t.createdAt">{{ t.createdAt | date: 'medium' }}</time>
    </header>
    @if (t.error; as error) {
      <p class="reason" data-testid="import-log-error">
        <span lang="en" translate="no">{{ error.description }}</span>&ngsp;<code translate="no">{{ error.code }}</code>
      </p>
    }
    @if (t.messages.length > 0) {
      <ol class="messages">
        @for (m of t.messages; track $index) {
          <li>
            <span class="level" [class.warning]="m.level === 'WARNING'" [class.error]="m.level === 'ERROR'" translate="no">{{ m.level }}</span
            >&ngsp;<span lang="en" translate="no">{{ m.message }}</span>
          </li>
        }
      </ol>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    .task-head {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-3);
    }
    .reason {
      margin-block: var(--quader-space-2) 0;
      overflow-wrap: anywhere;
    }
    .messages {
      margin: var(--quader-space-2) 0 0;
      padding-inline-start: var(--quader-space-6);
      overflow-wrap: anywhere;
    }
    .level {
      font-weight: 600;
    }
    .level.warning {
      color: var(--quader-status-warn);
    }
    .level.error {
      color: var(--quader-error);
    }
  `,
})
export class AnsibleImportTaskView {
  readonly task = input.required<AnsibleImportTask>();
  protected readonly stateText = importStateText;
}
