import { Provider } from '@angular/core';
import { Page, PageFetcher } from '../pagination';
import {
  Project,
  ProjectFilters,
  ProjectInput,
  ProjectMember,
  ProjectPrincipal,
  ProjectRole,
  ProjectVisibilityResult,
  ProjectBastion,
  ProjectsApi,
  PROJECT_KEY_PATTERN,
} from '../projects-api';
import { ApiError } from '../problem';
import { LoadedVisibilitySettings, VisibilityApi, VisibilityChoices, VisibilitySettingsPatch } from '../visibility-api';
import { Versioned } from '../repositories-api';

/*
 * In-memory fake of the projects service (R-UI-041, R-UI-047 component tier). It models what the
 * screens depend on: the key pattern, the `If-Match` of the replace (412 with the current tag), the
 * delete of an empty project only (409 `project_not_empty`) and the members with their roles (409
 * `last_owner` for the last Owner, Q-AUTHZ-217).
 */

function problem(status: number, code: string, extra: Record<string, unknown> = {}): ApiError {
  return new ApiError(status, { code, ...extra }, 'req-1');
}

export function sampleProject(over: Partial<Project> = {}): Project {
  return {
    key: 'web',
    displayName: 'Web platform',
    description: "The web team's repositories and partners",
    visibility: 'private',
    owners: ['group:web-leads'],
    members: 1,
    repositories: 0,
    serviceAccounts: 0,
    policies: 0,
    logicalBytes: 0,
    version: 1,
    createdAt: '2026-11-01T08:00:00Z',
    createdBy: 'user:alice',
    updatedAt: '2026-11-01T08:00:00Z',
    updatedBy: 'user:alice',
    ...over,
  };
}

export class FakeProjectsApi extends ProjectsApi {
  /** Every mutating call, for assertions. */
  readonly calls: string[] = [];
  /** The members by project key. */
  memberLists: Record<string, ProjectMember[]> = {
    web: [{ project: 'web', principal: 'group:web-leads', kind: 'group', name: 'web-leads', role: 'owner' }],
  };
  readonly keys: string[] = [];
  /** The `If-Match` of every replace, in order. */
  readonly ifMatch: string[] = [];
  projects: Project[] = [sampleProject()];
  /** The level a project created without one takes (the installation's default). */
  defaultLevel: Project['visibility'] = 'private';
  /** The repositories the next level change lowers, as the server reports them. */
  loweredNext: { repository: string; before: { artifacts: Project['visibility']; wiki: Project['visibility'] } }[] = [];
  principals: ProjectPrincipal[] = [
    {
      kind: 'user',
      name: 'alice',
      principal: 'user:alice',
      displayName: 'Alice Archer',
      id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1b01',
      avatarUrl: null,
    },
    { kind: 'user', name: 'bob', principal: 'user:bob', displayName: null, id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1b02', avatarUrl: null },
    { kind: 'group', name: 'web-leads', principal: 'group:web-leads' },
    { kind: 'group', name: 'web-partners', principal: 'group:web-partners' },
    { kind: 'service_account', name: 'web/deploy', principal: 'sa:web/deploy', project: 'web' },
  ];
  lookups: { key: string; q: string; kinds?: readonly ProjectPrincipal['kind'][] }[] = [];
  /** Rejects the next lookup once. */
  lookupFailure: ApiError | null = null;
  /** Rejects the next write once. */
  failure: ApiError | null = null;
  /** The Bastions that allow each project (R-BAS-134); a project without an entry is allowed by none. */
  bastionOffers: Record<string, ProjectBastion[]> = {};
  /** The answer of the offer's read for a caller who may not read it (a 403), or another failure. */
  bastionsFailure: ApiError | null = null;

  private fail(): void {
    if (this.failure) {
      const e = this.failure;
      this.failure = null;
      throw e;
    }
  }

  private find(key: string): Project {
    const found = this.projects.find((p) => p.key === key);
    if (!found) throw problem(404, 'not_found');
    return found;
  }

  private put(next: Project): void {
    this.projects = this.projects.map((p) => (p.key === next.key ? next : p));
  }

  private etag(p: Project): string {
    return `"${p.version}"`;
  }

  /** Simulates a write by someone else: the stored version moves on. */
  bump(key: string): void {
    const p = this.find(key);
    this.put({ ...p, version: p.version + 1 });
  }

  override list(filters: ProjectFilters): PageFetcher<Project> {
    return async (cursor, limit): Promise<Page<Project>> => {
      const q = filters.q?.toLowerCase();
      const all = this.projects
        .filter((p) => !q || p.key.includes(q) || p.displayName.toLowerCase().includes(q))
        .filter((p) => filters.membership !== true || p.role !== undefined)
        .sort((a, b) => (filters.sort === '-key' ? -1 : 1) * a.key.localeCompare(b.key));
      const start = cursor === null ? 0 : Number.parseInt(cursor, 10);
      const end = start + limit;
      return { items: all.slice(start, end), nextCursor: end < all.length ? String(end) : null };
    };
  }

  override async get(key: string): Promise<Versioned<Project>> {
    const p = this.find(key);
    return { value: p, etag: this.etag(p) };
  }

  /** What the server stored per `Idempotency-Key`: the body and the outcome, a 4xx refusal too (R-API-014). */
  private readonly stored = new Map<string, { body: string; outcome: Versioned<Project> | ApiError }>();

  override async create(input: ProjectInput, idempotencyKey: string): Promise<Versioned<Project>> {
    this.fail();
    this.keys.push(idempotencyKey);
    this.calls.push(`create ${input.key}`);
    const body = JSON.stringify(input);
    const seen = this.stored.get(idempotencyKey);
    if (seen) {
      if (seen.body !== body) throw problem(422, 'idempotency_key_reused');
      if (seen.outcome instanceof ApiError) throw seen.outcome;
      return seen.outcome;
    }
    try {
      const created = this.createNew(input);
      this.stored.set(idempotencyKey, { body, outcome: created });
      return created;
    } catch (e) {
      if (e instanceof ApiError && e.status >= 400 && e.status < 500) this.stored.set(idempotencyKey, { body, outcome: e });
      throw e;
    }
  }

  private createNew(input: ProjectInput): Versioned<Project> {
    const key = input.key ?? '';
    if (!PROJECT_KEY_PATTERN.test(key)) {
      throw problem(422, 'invalid_request', {
        errors: [{ pointer: '/key', code: 'invalid_key', detail: 'The key does not match the pattern.' }],
      });
    }
    if (!(input.displayName ?? '').trim() || (input.displayName ?? '').length > 128) {
      throw problem(422, 'validation_failed', {
        errors: [{ pointer: '/displayName', code: 'invalid_length', detail: 'The display name is required, 1 to 128 characters.' }],
      });
    }
    if (this.projects.some((p) => p.key === key)) throw problem(409, 'already_exists');
    const owners = [...(input.owners ?? [])];
    const created = sampleProject({
      key,
      displayName: input.displayName ?? key,
      description: input.description,
      visibility: input.visibility ?? this.defaultLevel,
      owners,
      members: owners.length,
      repositories: 0,
    });
    this.memberLists[key] = owners.map((o) => memberOf(key, o, 'owner'));
    this.projects = [...this.projects, created];
    return { value: created, etag: this.etag(created) };
  }

  override async replace(key: string, input: ProjectInput, etag: string): Promise<Versioned<Project>> {
    this.fail();
    this.ifMatch.push(etag);
    this.calls.push(`replace ${key}`);
    const current = this.find(key);
    if (etag !== this.etag(current)) {
      throw problem(412, 'precondition_failed', { currentEtag: this.etag(current) });
    }
    const next: Project = {
      ...current,
      displayName: input.displayName ?? current.displayName,
      description: input.description,
      version: current.version + 1,
    };
    this.put(next);
    return { value: next, etag: this.etag(next) };
  }

  override async setVisibility(
    key: string,
    level: Project['visibility'],
    etag: string,
  ): Promise<{ readonly project: Versioned<Project>; readonly lowered: ProjectVisibilityResult['lowered'] }> {
    this.fail();
    this.ifMatch.push(etag);
    this.calls.push(`setVisibility ${key} ${level}`);
    const current = this.find(key);
    if (etag !== this.etag(current)) {
      throw problem(412, 'precondition_failed', { currentEtag: this.etag(current) });
    }
    const next: Project = { ...current, visibility: level, version: current.version + 1 };
    this.put(next);
    const lowered = this.loweredNext;
    this.loweredNext = [];
    return { project: { value: next, etag: this.etag(next) }, lowered };
  }

  override async delete(key: string): Promise<void> {
    this.fail();
    this.calls.push(`delete ${key}`);
    const p = this.find(key);
    if (p.repositories > 0 || p.serviceAccounts > 0) throw problem(409, 'project_not_empty');
    this.projects = this.projects.filter((x) => x.key !== key);
  }

  private refresh(key: string): void {
    const list = this.memberLists[key] ?? [];
    this.put({
      ...this.find(key),
      members: list.length,
      owners: list.filter((m) => m.role === 'owner').map((m) => m.principal).sort(),
    });
  }

  override async members(key: string): Promise<readonly ProjectMember[]> {
    this.find(key);
    return structuredClone(this.memberLists[key] ?? []);
  }

  override async setMember(key: string, principal: string, role: ProjectRole): Promise<ProjectMember> {
    this.fail();
    this.calls.push(`setMember ${key} ${principal} ${role}`);
    this.find(key);
    if (!/^(user|group):/.test(principal)) throw problem(422, 'principal_not_allowed');
    const list = this.memberLists[key] ?? [];
    const current = list.find((m) => m.principal === principal);
    const owners = list.filter((m) => m.role === 'owner');
    if (current?.role === 'owner' && role !== 'owner' && owners.length === 1) throw problem(409, 'last_owner');
    const next = memberOf(key, principal, role);
    this.memberLists[key] = [...list.filter((m) => m.principal !== principal), next].sort((a, b) =>
      a.principal.localeCompare(b.principal),
    );
    this.refresh(key);
    return next;
  }

  override async removeMember(key: string, principal: string): Promise<void> {
    this.fail();
    this.calls.push(`removeMember ${key} ${principal}`);
    this.find(key);
    const list = this.memberLists[key] ?? [];
    const current = list.find((m) => m.principal === principal);
    if (!current) throw problem(404, 'not_found');
    if (current.role === 'owner' && list.filter((m) => m.role === 'owner').length === 1) throw problem(409, 'last_owner');
    this.memberLists[key] = list.filter((m) => m.principal !== principal);
    this.refresh(key);
  }

  /** Matches as the server does in outline: the name, a word of it or a word of the display name starts with the text. */
  override async lookupPrincipals(
    key: string,
    q: string,
    kinds?: readonly ProjectPrincipal['kind'][],
  ): Promise<readonly ProjectPrincipal[]> {
    this.find(key);
    this.lookups.push({ key, q, kinds });
    if (this.lookupFailure) {
      const e = this.lookupFailure;
      this.lookupFailure = null;
      throw e;
    }
    if (q.trim().length < 2) throw problem(422, 'invalid_request');
    const text = q.trim().toLowerCase();
    const words = (p: ProjectPrincipal): string[] => [p.name, ...p.name.split(/[^a-z0-9]+/), ...(p.displayName ?? '').toLowerCase().split(/\s+/)];
    return this.principals
      .filter((p) => (!kinds?.length || kinds.includes(p.kind)) && words(p).some((w) => w.startsWith(text)))
      .slice(0, 20);
  }

  override async bastions(key: string): Promise<readonly ProjectBastion[]> {
    this.find(key);
    if (this.bastionsFailure) throw this.bastionsFailure;
    return this.bastionOffers[key] ?? [];
  }
}

function memberOf(project: string, principal: string, role: ProjectRole): ProjectMember {
  const [kind, name] = principal.split(':') as ['user' | 'group', string];
  return { project, principal, kind, name: name ?? '', role };
}

export class FakeVisibilityApi extends VisibilityApi {
  stored: LoadedVisibilitySettings = {
    etag: '"1"',
    settings: {
      defaultLevel: 'private',
      restrictedLevels: [],
      publicAvailable: true,
      publicObjects: { projects: [], repositories: [], truncated: false },
    },
  };
  readonly patches: { patch: VisibilitySettingsPatch; etag: string }[] = [];
  failure: ApiError | null = null;
  /** Whether the caller of `choices()` is an administrator (`security-settings:write`, Q-AUTHZ-283). */
  mayChooseRestricted = true;

  override async choices(): Promise<VisibilityChoices> {
    const s = this.stored.settings;
    return {
      defaultLevel: s.defaultLevel,
      restrictedLevels: [...s.restrictedLevels],
      mayChooseRestricted: this.mayChooseRestricted,
      publicAvailable: s.publicAvailable,
    };
  }

  override async load(): Promise<LoadedVisibilitySettings> {
    if (this.failure) {
      const e = this.failure;
      this.failure = null;
      throw e;
    }
    return structuredClone(this.stored);
  }

  override async patch(patch: VisibilitySettingsPatch, etag: string): Promise<LoadedVisibilitySettings> {
    this.patches.push({ patch, etag });
    if (this.failure) {
      const e = this.failure;
      this.failure = null;
      throw e;
    }
    if (etag !== this.stored.etag) throw problem(412, 'precondition_failed', { currentEtag: this.stored.etag });
    const level = patch.defaultLevel ?? this.stored.settings.defaultLevel;
    const restricted = patch.restrictedLevels ?? this.stored.settings.restrictedLevels;
    if (restricted.includes(level)) {
      throw problem(422, 'validation_failed', {
        errors: [{ pointer: patch.restrictedLevels ? '/restrictedLevels' : '/defaultLevel', code: 'default_level_restricted', detail: 'The default level cannot be restricted.' }],
      });
    }
    if (level !== this.stored.settings.defaultLevel && level === 'public' && !this.stored.settings.publicAvailable) {
      throw problem(409, 'public_not_allowed');
    }
    this.stored = {
      etag: `"${Number.parseInt(this.stored.etag.replaceAll('"', ''), 10) + 1}"`,
      settings: { ...this.stored.settings, defaultLevel: level, restrictedLevels: [...restricted] },
    };
    return structuredClone(this.stored);
  }
}

export function provideProjectFakes(): Provider[] {
  return [
    { provide: ProjectsApi, useClass: FakeProjectsApi },
    { provide: VisibilityApi, useClass: FakeVisibilityApi },
  ];
}
