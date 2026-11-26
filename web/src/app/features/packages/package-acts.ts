import { ChangeDetectionStrategy, Component, Provider, input, signal } from '@angular/core';
import { gateActSlot } from '../../shared/signatures/gate-act';
import { GATE_ACT, ROLE_UPLOAD_ACT, VERSION_IMPORT_LOG } from '../../views/acts';
import { collectionUpload } from '../repositories/content/ansible-upload-targets';
import { AnsibleImportLog } from './ansible-import-log';

/**
 * **Upload collection** under a role's README note (R-ANS-061, Q-UI-1323, `ROLE_UPLOAD_ACT` of views/acts.ts): the project's Ansible
 * Galaxy repositories this reader may publish a collection into (ansible-upload-targets.ts); nothing where there is none.
 */
@Component({
  selector: 'quader-role-upload-act',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (targets().length > 0) {
      <p class="role-upload">
        <button class="btn btn-default" type="button" data-testid="role-upload" (click)="upload()">
          <span class="material-icons" aria-hidden="true">upload</span>
          <ng-container i18n="@@pkg.role.upload">Upload collection</ng-container>
        </button>
      </p>
    }
  `,
  styles: `
    /* A line of the role's note (ansible-role.ts). */
    :host {
      display: contents;
    }
  `,
})
export class RoleUploadAct {
  /** The project of the page: where a collection with the role would be published. */
  readonly project = input('');

  private readonly uploads = collectionUpload(() => this.project() || undefined);
  protected readonly targets = this.uploads.targets;

  protected upload(): void {
    this.uploads.open();
  }
}

/**
 * The acts the package pages of the main installation offer in their views (R-UI-223, views/acts.ts): the signature gate's release and
 * withdrawal, a role's Upload collection, and an Ansible collection's Import log. A page lists them in its `providers`.
 */
export function providePackageActs(): Provider[] {
  return [
    { provide: GATE_ACT, useFactory: gateActSlot },
    { provide: ROLE_UPLOAD_ACT, useValue: { component: RoleUploadAct, offered: signal(true) } },
    { provide: VERSION_IMPORT_LOG, useValue: { component: AnsibleImportLog, offered: signal(true) } },
  ];
}
