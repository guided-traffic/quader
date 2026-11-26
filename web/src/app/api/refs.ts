/*
 * The names and addresses of a repository, without a client: what a view computes from a name (R-UI-223). A module of functions alone,
 * so that the Bastion's application, which has no management API, imports them without the HTTP classes of repositories-api.ts
 * (R-UI-228).
 */

/** The two parts of a qualified repository name; a name without a project has an empty `project`. */
export function splitRef(ref: string): { readonly project: string; readonly key: string } {
  const at = ref.indexOf('/');
  return at < 0 ? { project: '', key: ref } : { project: ref.slice(0, at), key: ref.slice(at + 1) };
}

/** `/api/v1/projects/{projectKey}/repositories/{repoKey}` of a qualified repository name. */
export function repositoryUrl(ref: string): string {
  const { project, key } = splitRef(ref);
  return `/api/v1/projects/${encodeURIComponent(project)}/repositories/${encodeURIComponent(key)}`;
}

/** True for a client address template of the raw files family (`protocolBasePath` of a format). */
export function isFilesAddress(template: string): boolean {
  return template.startsWith('/files/');
}

/** Percent-encodes every segment of a repository path and keeps the slashes. */
export function encodePath(path: string): string {
  return path.split('/').map(encodeURIComponent).join('/');
}

/**
 * The content route of a path in a repository (R-API-025, Q-UI-106): the one place that builds the URL of an upload.
 * The session cookie authenticates it on the role `api`, on the origin the UI was loaded from; the downloads of the UI use
 * the `contentUrl` the server sends, never a string assembled here (R-UI-001).
 */
export function contentPath(repository: string, path: string): string {
  return `${repositoryUrl(repository)}/content/${encodePath(path)}`;
}

/**
 * The repository `<project>/<key>` of a content route (`/api/v1/projects/{project}/repositories/{key}/content/…`), the one place a
 * version's file names the repository that holds it (R-API-025); null for another address. A Bastion's catalogue carries the route
 * below its data path (`/ui/data/v1/…`, bastion.md R-BAS-107), which names the repository alike.
 */
export function repositoryOfContent(url: string): string | null {
  const m = /^(?:\/api\/v1|\/ui\/data\/v1)\/projects\/([^/]+)\/repositories\/([^/]+)\/content\//.exec(url);
  return m ? `${decodeURIComponent(m[1] ?? '')}/${decodeURIComponent(m[2] ?? '')}` : null;
}
