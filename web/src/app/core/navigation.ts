import { Injectable, computed, inject } from '@angular/core';
import { ProjectContext, projectKeyOf } from './project-context';
import { SessionStore } from './session-store';

/**
 * The capability that opens the security dashboard (R-UI-158, Q-AUTHZ-295): read on the metadata of every repository, which is
 * what makes the findings API answer for every project (R-VULN-019). `admin` and `auditor` hold it.
 */
export const SECURITY_DASHBOARD_CAPABILITY = 'content:metadata-read';

export interface NavItem {
  readonly route: string;
  readonly label: string;
  /** Icon font ligature (Q-UI-1); decorative, the label is the name. */
  readonly icon: string;
  /** Capability from `authz/me` that shows the entry (R-UI-002); none means always shown. */
  readonly capability?: string;
}

/** A section of the sidebar with nested entries that can be folded. */
export interface NavGroup {
  readonly id: string;
  readonly label: string;
  readonly icon: string;
  readonly items: readonly NavItem[];
}

export type NavEntry = ({ readonly kind: 'item' } & NavItem) | ({ readonly kind: 'group' } & NavGroup);

/**
 * A titled block of a side bar (R-UI-168, Q-UI-663): its context title and the entries under it. The title names the block for
 * assistive technology (`aria-labelledby="nav-<id>"`). The project's side bar has one block, whose title is the project switcher.
 */
export interface NavSection {
  readonly id: string;
  /** The context title of the block. */
  readonly label: string;
  readonly entries: readonly NavEntry[];
}

export interface NavArea {
  readonly id: 'home' | 'project' | 'admin';
  /** The titled blocks of the side bar in their order; a block without an entry for the caller is left out. */
  readonly sections: readonly NavSection[];
}

/** The block of the home side bar that the shell ends with the folding group **Recently opened** (R-UI-167). */
export const YOUR_WORK_SECTION = 'your-work';

/** Every entry of an area, its blocks one after the other. */
export function areaEntries(area: NavArea | null): readonly NavEntry[] {
  return area ? area.sections.flatMap((s) => s.entries) : [];
}

/** Every item of an area, a folding group's items in its place. */
export function areaItems(area: NavArea | null): readonly NavItem[] {
  return areaEntries(area).flatMap((e) => (e.kind === 'group' ? e.items : [e]));
}

/**
 * The information architecture of R-UI-002 (Q-UI-68, Q-UI-127, Q-UI-129): three sidebars. The **home** sidebar belongs to
 * every screen outside a project and outside the admin area (the start page of your projects, the guides, the profile, the
 * security dashboard); the **project** sidebar belongs to `/projects/<key>/…` and is built for that key (see
 * {@link projectEntries}); the **admin** sidebar belongs to `/admin/…` behind the top bar's Admin button. The home sidebar has two
 * titled blocks (R-UI-168): **Your work** with Projects and Guides, which the shell ends with the folding group **Recently opened**
 * (R-UI-167), and **Security audit** with Security dashboard for the instance's auditors and administrators (R-UI-158). In the
 * admin area, single entries (repositories, endpoints) and folding groups (identity, access, system). Only the screens that exist
 * in the current milestone are listed; the capabilities are those of R-AUTHZ-010. The router and the API decide access, this only
 * hides entries (R-UI-005).
 */
export const NAV_AREAS: readonly NavArea[] = [
  {
    id: 'home',
    sections: [
      {
        id: YOUR_WORK_SECTION,
        label: $localize`:@@nav.context.home:Your work`,
        entries: [
          { kind: 'item', route: '/projects', icon: 'folder_shared', label: $localize`:@@nav.projects:Projects` },
          { kind: 'item', route: '/guides', icon: 'menu_book', label: $localize`:@@nav.guides:Guides` },
        ],
      },
      // The auditor's page, not the administrator's (R-UI-158, Q-UI-620): a second context title, shown to whoever reads the
      // findings of every project.
      {
        id: 'security-audit',
        label: $localize`:@@nav.section.security-audit:Security audit`,
        entries: [
          {
            kind: 'item',
            route: '/security',
            icon: 'security',
            label: $localize`:@@nav.security-dashboard:Security dashboard`,
            capability: SECURITY_DASHBOARD_CAPABILITY,
          },
        ],
      },
    ],
  },
  {
    id: 'admin',
    sections: [
      {
        id: 'admin',
        label: $localize`:@@nav.context.admin:Admin area`,
        entries: [
          { kind: 'item', route: '/admin/dashboard', icon: 'dashboard', label: $localize`:@@nav.dashboard:Dashboard`, capability: 'settings:read' },
          { kind: 'item', route: '/admin/projects', icon: 'folder_shared', label: $localize`:@@nav.projects:Projects`, capability: 'projects:read' },
          { kind: 'item', route: '/admin/repositories', icon: 'inventory_2', label: $localize`:@@nav.repositories:Repositories`, capability: 'repositories:read-config' },
          { kind: 'item', route: '/admin/endpoints', icon: 'router', label: $localize`:@@nav.endpoints:Endpoints`, capability: 'endpoints:read' },
          // >>> M5b bastion
          { kind: 'item', route: '/admin/bastions', icon: 'public', label: $localize`:@@nav.bastions:Bastions`, capability: 'endpoints:read' },
          // <<< M5b bastion
          {
            kind: 'group',
            id: 'identity',
            icon: 'groups',
            label: $localize`:@@nav.section.identity:Identity`,
            items: [
              { route: '/admin/users', icon: 'person', label: $localize`:@@nav.users:Users`, capability: 'identities:read' },
              { route: '/admin/groups', icon: 'group', label: $localize`:@@nav.groups:Groups`, capability: 'identities:read' },
              { route: '/admin/service-accounts', icon: 'smart_toy', label: $localize`:@@nav.service-accounts:Service accounts`, capability: 'identities:read' },
              { route: '/admin/tokens', icon: 'key', label: $localize`:@@nav.tokens:Tokens`, capability: 'tokens:read' },
              // >>> M4 ci
              { route: '/admin/ci', icon: 'rocket_launch', label: $localize`:@@nav.ci:CI identity`, capability: 'ci:read' },
              // <<< M4 ci
            ],
          },
          {
            kind: 'group',
            id: 'access',
            icon: 'shield',
            label: $localize`:@@nav.section.access:Access`,
            items: [
              { route: '/admin/permissions', icon: 'policy', label: $localize`:@@nav.permissions:Permission policies`, capability: 'policies:read' },
              { route: '/admin/roles', icon: 'admin_panel_settings', label: $localize`:@@nav.roles:System roles`, capability: 'roles:read' },
            ],
          },
          {
            kind: 'group',
            id: 'system',
            icon: 'dns',
            label: $localize`:@@nav.section.system:System`,
            items: [
              { route: '/admin/system', icon: 'speed', label: $localize`:@@nav.system:System status`, capability: 'settings:read' },
              { route: '/admin/audit', icon: 'history', label: $localize`:@@nav.audit:Audit log`, capability: 'audit:read' },
              // >>> M4b vulnerabilities
              { route: '/admin/advisory-sources', icon: 'rss_feed', label: $localize`:@@nav.advisory-sources:Advisory sources`, capability: 'settings:read' },
              // <<< M4b vulnerabilities
            ],
          },
          // One group for what the administrator configures at runtime, a page per topic (Q-UI-326).
          {
            kind: 'group',
            id: 'settings',
            icon: 'settings',
            label: $localize`:@@nav.section.settings:Settings`,
            items: [
              { route: '/admin/settings/general', icon: 'tune', label: $localize`:@@nav.settings.general:General`, capability: 'settings:read' },
              { route: '/admin/settings/auth', icon: 'lock', label: $localize`:@@nav.settings.auth:Authentication`, capability: 'settings:read' },
            ],
          },
        ],
      },
    ],
  },
];

/**
 * The entries of the sidebar of project `key` (Q-UI-129, Q-UI-216: the repositories first). Repositories, packages, vulnerabilities and members are
 * for every caller who sees the project (the API decides what each shows); the policies, service accounts and CI
 * identity are for the capability holders and the project's Owners, as their endpoints are (R-AUTHZ-026); the settings
 * are read by every member and changed by the Owner and `projects:write`. A plain user of the project (R-UI-067: at most a reader,
 * no system role) gets the content alone; the project's members and settings are its About section on the repository list
 * (Q-UI-317), the read-only screens stay reachable by their address.
 */
export function projectEntries(key: string, session: SessionStore): readonly NavEntry[] {
  const at = (path: string): string => `/projects/${encodeURIComponent(key)}/${path}`;
  const manages = (capability: string): boolean => session.has(capability) || session.isOwnerOf(key);
  const plain = !session.isAdministrator() && (session.projectRole(key) ?? 'reader') === 'reader';
  const access: NavItem[] = [
    ...(plain ? [] : [{ route: at('members'), icon: 'group', label: $localize`:@@nav.project.members:Members` }]),
    ...(manages('policies:read')
      ? [{ route: at('policies'), icon: 'policy', label: $localize`:@@nav.permissions:Permission policies` }]
      : []),
    ...(manages('identities:read')
      ? [{ route: at('service-accounts'), icon: 'smart_toy', label: $localize`:@@nav.service-accounts:Service accounts` }]
      : []),
    // >>> M4 ci
    ...(manages('ci:read') ? [{ route: at('ci'), icon: 'rocket_launch', label: $localize`:@@nav.ci:CI identity` }] : []),
    // <<< M4 ci
  ];
  return [
    { kind: 'item', route: at('repositories'), icon: 'inventory_2', label: $localize`:@@nav.repositories:Repositories` },
    // >>> M2 ui-packages
    { kind: 'item', route: at('packages'), icon: 'category', label: $localize`:@@nav.packages:Packages` },
    // <<< M2 ui-packages
    // >>> M4b vulnerabilities
    { kind: 'item', route: at('vulnerabilities'), icon: 'security', label: $localize`:@@nav.vulnerabilities:Vulnerabilities` },
    // <<< M4b vulnerabilities
    {
      kind: 'group',
      id: 'project-access',
      icon: 'shield',
      label: $localize`:@@nav.section.project-access:Access`,
      items: access,
    },
    ...(plain
      ? []
      : [{ kind: 'item' as const, route: at('settings'), icon: 'settings', label: $localize`:@@nav.project.settings:Settings` }]),
  ];
}

/** The areas and entries the signed-in caller sees; empty groups, blocks and areas disappear. */
@Injectable({ providedIn: 'root' })
export class Navigation {
  private readonly session = inject(SessionStore);
  private readonly context = inject(ProjectContext);

  /**
   * The fixed areas: the home sidebar, and the admin sidebar for a caller who may use one of its entries. Every entry of the
   * admin area names its capability, so a caller without one of them has no admin area. A block of the home sidebar whose entries
   * the caller lacks goes with its title (R-UI-168: **Security audit** for the holder of `content:metadata-read` alone).
   */
  readonly areas = computed<readonly NavArea[]>(() => {
    const capabilities = this.session.capabilities();
    const allowed = (i: Pick<NavItem, 'capability'>) => !i.capability || capabilities.includes(i.capability);
    const entries = (list: readonly NavEntry[]): NavEntry[] =>
      list.flatMap((e): NavEntry[] => {
        if (e.kind === 'item') return allowed(e) ? [e] : [];
        const items = e.items.filter(allowed);
        return items.length > 0 ? [{ ...e, items }] : [];
      });
    return NAV_AREAS.map(
      (a): NavArea => ({
        ...a,
        sections: a.sections.map((s) => ({ ...s, entries: entries(s.entries) })).filter((s) => s.entries.length > 0),
      }),
    ).filter((a) => a.sections.length > 0);
  });

  /**
   * The top bar's **Admin** button is shown (R-UI-002, Q-UI-621): the admin area has an entry the caller may use. An auditor
   * holds several (the audit log, the users, the dashboard of the installation); a caller whose only capabilities are those of
   * the security dashboard has none and is not led into an empty admin area.
   */
  readonly hasAdminArea = computed(() => this.areas().some((a) => a.id === 'admin'));

  /** The sidebar of the project the URL is in, or null outside a project: one block, titled by the project switcher. */
  readonly projectArea = computed<NavArea | null>(() => {
    const key = this.context.key();
    if (key === null) return null;
    const entries = projectEntries(key, this.session).flatMap((e): NavEntry[] =>
      e.kind === 'item' || e.items.length > 0 ? [e] : [],
    );
    return { id: 'project', sections: [{ id: 'project', label: this.context.current()?.displayName || key, entries }] };
  });

  /** The area a route belongs to: `/admin` and below is the admin area, `/projects/<key>` and below a project. */
  static areaOf(url: string): NavArea['id'] {
    if (url === '/admin' || url.startsWith('/admin/') || url.startsWith('/admin?')) return 'admin';
    return projectKeyOf(url) === null ? 'home' : 'project';
  }

  /** The entries of one area for the caller, or null when the caller has none of it. */
  area(id: NavArea['id']): NavArea | null {
    return id === 'project' ? this.projectArea() : (this.areas().find((a) => a.id === id) ?? null);
  }
}
