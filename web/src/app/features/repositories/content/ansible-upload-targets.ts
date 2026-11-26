import { Signal, computed, inject, resource } from '@angular/core';
import { RepositoriesApi, refOf } from '../../../api/repositories-api';
import { SessionStore } from '../../../core/session-store';
import { valueOf } from '../../../shared/paged-resource';
import { QuaderDialog } from '../../../shared/ui/dialog';
import { AnsibleUploadData, AnsibleUploadDialog } from './ansible-upload-dialog';

/** The most repositories the choice reads: a project, or an installation, holds a handful of Ansible Galaxy repositories. */
const TARGETS_READ = 100;

/** Upload collection opened from a page that is no repository's: the repositories to choose from, and the act. */
export interface CollectionUpload {
  /** The Ansible Galaxy repositories the reader may publish into, `<project>/<key>`; empty offers no act. */
  readonly targets: Signal<readonly string[]>;
  /** Opens the dialog with them (R-ANS-061). */
  open(): void;
}

/**
 * Upload collection for a page that names no repository (R-ANS-061, Q-UI-1323): a role's page and an Ansible namespace's page. The
 * choice is the local and virtual Ansible Galaxy repositories that are not disabled, of the project `scope` names (null: every project
 * the reader sees), whose `deploy` the server allows this reader (a virtual one without a deployment target is refused it). `undefined`
 * reads nothing; a visitor without a session and a failed read offer none. Called in an injection context (a field's initializer).
 */
export function collectionUpload(scope: () => string | null | undefined): CollectionUpload {
  const repositories = inject(RepositoriesApi);
  const session = inject(SessionStore);
  const dialog = inject(QuaderDialog);
  const read = resource({
    params: () => {
      const s = scope();
      return s !== undefined && session.state() === 'authenticated' ? { project: s } : undefined;
    },
    loader: async ({ params }): Promise<readonly string[]> => {
      try {
        const page = await repositories.list({ format: 'ansible', ...(params.project ? { project: params.project } : {}) })(null, TARGETS_READ);
        const candidates = page.items.filter((r) => r.class !== 'remote' && !r.disabled).map(refOf);
        const allowed = await session.check(candidates.map((repository) => ({ repository, action: 'deploy' })));
        return candidates.filter((_, i) => allowed[i]);
      } catch {
        return [];
      }
    },
  });
  const targets = computed(() => valueOf(read) ?? []);
  return {
    targets,
    open: () => {
      const data: AnsibleUploadData = { repositories: targets() };
      dialog.open<AnsibleUploadDialog, AnsibleUploadData, boolean>(AnsibleUploadDialog, { data, width: '40rem', maxWidth: '95vw' });
    },
  };
}
