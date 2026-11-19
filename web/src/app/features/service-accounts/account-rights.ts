import { Component, input, model } from '@angular/core';
import { RepositoryAction } from '../../api/identity-api';
import { Checkbox } from '../../shared/ui/checkbox';
import { actionHelp } from '../permissions/policy-model';

/** The six repository actions in the order of R-AUTHZ-006. */
export const REPOSITORY_ACTIONS: readonly RepositoryAction[] = ['read', 'annotate', 'deploy', 'overwrite', 'delete', 'manage'];

/** The word of an action as the policies' screens name it (R-UI-027). */
export function actionLabel(a: RepositoryAction): string {
  switch (a) {
    case 'read':
      return $localize`:@@sa-rights.read:Read`;
    case 'annotate':
      return $localize`:@@sa-rights.annotate:Annotate`;
    case 'deploy':
      return $localize`:@@sa-rights.deploy:Deploy`;
    case 'overwrite':
      return $localize`:@@sa-rights.overwrite:Overwrite`;
    case 'delete':
      return $localize`:@@sa-rights.delete:Delete`;
    default:
      return $localize`:@@sa-rights.manage:Manage`;
  }
}

/**
 * **Rights on this repository** (R-UI-181, R-AUTHZ-039): one checkbox per action with the one-line help of R-UI-027, in the create
 * dialog of a repository's service account and at the top of its page. At least one stays ticked; the server refuses none
 * (422 at `/actions`), and the error stands below the boxes.
 */
@Component({
  selector: 'quader-account-rights',
  imports: [Checkbox],
  template: `
    <fieldset class="rights">
      <legend i18n="@@sa-rights.legend">Rights on this repository</legend>
      @for (a of all; track a) {
        <div class="right">
          <quader-checkbox
            [checked]="actions().includes(a)"
            [disabled]="readonly()"
            [describedBy]="'right-help-' + a"
            (checkedChange)="toggle(a, $event)"
            >{{ label(a) }}</quader-checkbox
          >
          <p class="hint" [id]="'right-help-' + a">{{ help(a) }}</p>
        </div>
      }
      @if (error(); as text) {
        <p class="field-error">{{ text }}</p>
      }
    </fieldset>
  `,
  styles: `
    .rights {
      margin: 0 0 var(--quader-space-4);
      padding: var(--quader-space-2) var(--quader-space-3);
      border: 1px solid var(--quader-border);
      border-radius: var(--quader-radius-m);
    }
    .right {
      margin-block-end: var(--quader-space-2);
    }
    .hint {
      margin: 0;
      padding-inline-start: calc(16px + var(--quader-space-2));
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
    }
    .field-error {
      margin: 0;
      color: var(--quader-status-bad);
      font: var(--quader-type-body-small);
    }
  `,
})
export class AccountRights {
  readonly actions = model.required<readonly RepositoryAction[]>();
  readonly readonly = input(false);
  readonly error = input<string | null>(null);

  protected readonly all = REPOSITORY_ACTIONS;

  protected label(a: RepositoryAction): string {
    return actionLabel(a);
  }

  protected help(a: RepositoryAction): string {
    return actionHelp(a);
  }

  protected toggle(a: RepositoryAction, on: boolean): void {
    this.actions.update((list) => REPOSITORY_ACTIONS.filter((x) => (x === a ? on : list.includes(x))));
  }
}
