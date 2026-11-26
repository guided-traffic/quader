import { DecimalPipe } from '@angular/common';
import { Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { PackageVersion } from '../../api/packages-api';
import { repositoryCommands } from '../../core/project-links';
import { collectionUpload } from '../repositories/content/ansible-upload-targets';

/*
 * A role an Ansible Galaxy remote read (formats/ansible.md R-ANS-056): the package `role/<id>`, shown by its display name `<owner>.<name>`,
 * whose version carries the role's facts under `metadata.role`. A member of the wrong type is skipped, never an error: the upstream wrote it.
 */

/** The state of a role's README on the server. */
export type RoleReadmeState = 'none' | 'queued' | 'available' | 'absent' | 'unavailable';

/** What a version of a role says of the role. */
export interface RoleFacts {
  readonly owner: string;
  readonly name: string;
  /** The package name the rules, the patterns and the grants see: `role/<id>`. */
  readonly packageName: string;
  readonly readme: RoleReadmeState;
  readonly readmeReason: string;
  /** The download count at the upstream; null when the upstream named none. */
  readonly upstreamDownloads: number | null;
  readonly branch: string;
  readonly dependencies: readonly string[];
  /**
   * The remote holds the role's package but no facts of it: a role cached before the remote kept them, or one whose lookup did not come
   * through it. Its owner, name and README come with its next lookup (Q-ANS-113).
   */
  readonly bare: boolean;
}

/** The package name of a role, `role/<id>` (Q-ANS-36). */
const ROLE_PACKAGE = /^role\/[0-9]{1,12}$/;

const README_STATES: readonly RoleReadmeState[] = ['none', 'queued', 'available', 'absent', 'unavailable'];

/** The facts of a role's version; null for any other version (a collection's, another format's). */
export function roleFacts(d: PackageVersion | undefined | null): RoleFacts | null {
  if (d?.format !== 'ansible') return null;
  const r = d.metadata['role'];
  if (typeof r !== 'object' || r === null || Array.isArray(r)) {
    return ROLE_PACKAGE.test(d.name)
      ? { owner: '', name: '', packageName: d.name, readme: 'none', readmeReason: '', upstreamDownloads: null, branch: '', dependencies: [], bare: true }
      : null;
  }
  const m = r as Record<string, unknown>;
  const text = (v: unknown): string => (typeof v === 'string' ? v : '');
  const state = README_STATES.find((s) => s === m['readme']) ?? 'none';
  return {
    owner: text(m['owner']),
    name: text(m['name']),
    packageName: text(m['packageName']),
    readme: state,
    readmeReason: text(m['readmeReason']),
    upstreamDownloads: typeof m['upstreamDownloads'] === 'number' ? m['upstreamDownloads'] : null,
    branch: text(m['branch']),
    dependencies: Array.isArray(m['dependencies']) ? m['dependencies'].filter((x): x is string => typeof x === 'string') : [],
    bare: false,
  };
}

/** The first remote repository that holds the version, `<project>/<key>`: where its role is looked up in the tab Upstream. */
export function roleRemote(d: PackageVersion): string | null {
  return d.repositories.find((r) => r.class === 'remote')?.key ?? null;
}

/** The sentence of a README the page does not have, by its state. */
function readmeSentence(f: RoleFacts): string {
  if (f.bare) {
    return $localize`:@@pkg.role.readme.bare:Quader has not read what the upstream says of this role yet. Its owner and name, its description and its README come with the role's next lookup, the first request of ansible-galaxy role install or role info.`;
  }
  switch (f.readme) {
    case 'queued':
      return $localize`:@@pkg.role.readme.queued:Quader is fetching the README of this role from the upstream. It appears here when it arrives.`;
    case 'absent':
      return $localize`:@@pkg.role.readme.absent:The upstream has no README for this role.`;
    case 'unavailable':
      switch (f.readmeReason) {
        case 'too_large':
          return $localize`:@@pkg.role.readme.too-large:The upstream's README of this role is larger than Quader keeps (512 KiB).`;
        case 'invalid_document':
          return $localize`:@@pkg.role.readme.invalid:The upstream answered something Quader cannot read as the README of this role.`;
        default:
          return $localize`:@@pkg.role.readme.failed:Quader could not fetch the README of this role from the upstream. It asks again when a client installs the role.`;
      }
    default:
      return $localize`:@@pkg.role.readme.none:Quader has no README of this role yet: it fetches it from the upstream when a client installs the role, unless the repository fetches no documentation.`;
  }
}

/**
 * The README tab of a role when the page holds no README, and the line every role's README ends with: a role is published inside a
 * collection (the owner's choice of W-30, Q-ANS-110), with the way to the manual, and, for a reader who may publish into a repository of
 * the project, **Upload collection**, the dialog of R-ANS-061 with those repositories to choose from (Q-UI-1323).
 */
@Component({
  selector: 'quader-ansible-role-readme-note',
  imports: [RouterLink],
  template: `
    @if (!hasReadme()) {
      <p class="empty" data-testid="role-readme-state">{{ sentence() }}</p>
    }
    <p class="muted role-publish" data-testid="role-publish-note">
      <span i18n="@@pkg.role.publish"
        >Roles come to Quader from a Galaxy. To publish a role of your own, put it into the roles directory of a collection and publish
        the collection.</span
      >&ngsp;<a routerLink="/help/user/ansible-collections" fragment="publish-a-role-put-it-into-a-collection" i18n="@@pkg.role.publish-how"
        >How to publish a role</a
      >
    </p>
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
    :host {
      display: block;
    }
    .role-publish {
      margin-top: var(--space-4, 16px);
    }
  `,
})
export class AnsibleRoleReadmeNote {
  readonly facts = input.required<RoleFacts>();
  readonly hasReadme = input.required<boolean>();
  /** The project of the page: where a collection with the role would be published. */
  readonly project = input('');
  protected readonly sentence = computed(() => readmeSentence(this.facts()));

  /** The project's Ansible Galaxy repositories this reader may publish a collection into (R-ANS-061, ansible-upload-targets.ts). */
  private readonly uploads = collectionUpload(() => this.project() || undefined);
  protected readonly targets = this.uploads.targets;

  protected upload(): void {
    this.uploads.open();
  }
}

/**
 * The facts of a role on the tab Details (R-ANS-056): the owner and the name the client installs it by, the downloads at the upstream named
 * apart from Quader's own, the branch, the dependencies, and the name the rules see, with the way to the tab Upstream that decides them.
 */
@Component({
  selector: 'quader-ansible-role-facts',
  imports: [DecimalPipe, RouterLink],
  template: `
    @let f = facts();
    <h2 i18n="@@pkg.role.heading">Role</h2>
    <dl class="facts" data-testid="role-facts">
      @if (!f.bare) {
        <dt i18n="@@pkg.role.owner">Owner</dt>
        <dd translate="no">{{ f.owner }}</dd>
        <dt i18n="@@pkg.role.name">Name</dt>
        <dd translate="no">{{ f.name }}</dd>
        <dt i18n="@@pkg.role.upstream-downloads">Downloads at the upstream</dt>
        <dd>
          @if (f.upstreamDownloads !== null) {
            {{ f.upstreamDownloads | number }}
          } @else {
            <span class="muted" i18n="@@pkg.role.not-named">Not named by the upstream</span>
          }
        </dd>
      }
      @if (f.branch) {
        <dt i18n="@@pkg.role.branch">Branch</dt>
        <dd translate="no">{{ f.branch }}</dd>
      }
      @if (f.dependencies.length > 0) {
        <dt i18n="@@pkg.role.dependencies">Dependencies</dt>
        <dd translate="no">{{ f.dependencies.join(', ') }}</dd>
      }
      <dt i18n="@@pkg.role.rules-name">Name in rules and permissions</dt>
      <dd>
        <code translate="no">{{ f.packageName }}</code>
        @if (upstreamLink(); as link) {
          &ngsp;<a [routerLink]="link" [queryParams]="{ name: f.packageName }" i18n="@@pkg.role.upstream-link">Delivery in the tab Upstream</a>
        }
        <span class="muted rules-note" i18n="@@pkg.role.rules-note"
          >Delivery rules, include and exclude patterns and permissions name a role by the Galaxy's number, not by its owner and name.</span
        >
      </dd>
    </dl>
  `,
  styles: `
    /* The headings of the tab Details (package-version.scss .tab h2): this one is first. */
    h2 {
      margin-block: 0 var(--quader-space-2);
      font: var(--quader-type-title-medium);
    }
    .rules-note {
      display: block;
    }
  `,
})
export class AnsibleRoleFacts {
  readonly facts = input.required<RoleFacts>();
  /** The remote repository whose tab Upstream decides the role, `<project>/<key>`; null for none the reader sees. */
  readonly remote = input<string | null>(null);
  protected readonly upstreamLink = computed(() => {
    const r = this.remote();
    return r ? [...repositoryCommands(r), 'upstream'] : null;
  });
}
