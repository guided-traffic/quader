import { splitRef } from '../api/repositories-api';

/**
 * The router commands of a repository's own screen, given its qualified name `<project>/<key>` (R-REPO-002): a
 * repository is shown inside its project (Q-UI-129).
 */
export function repositoryCommands(ref: string): string[] {
  const { project, key } = splitRef(ref);
  return ['/projects', project, 'repositories', key];
}

/**
 * The router commands of the page **Set up client** of a repository (R-UI-120): the snippets and the guide of that repository, the
 * one target of every **Set up client** (R-UI-126).
 */
export function setupCommands(ref: string): string[] {
  return [...repositoryCommands(ref), 'setup'];
}

/**
 * The router commands of the folder or the file at `path` of a repository's files (R-UI-010, Q-UI-510): its page with the path
 * after `files`; an empty path is the root of the files.
 */
export function filesCommands(ref: string, path: readonly string[] = []): string[] {
  const { project, key } = splitRef(ref);
  return ['/projects', project, 'repositories', key, 'files', ...path.filter((s) => s !== '')];
}
