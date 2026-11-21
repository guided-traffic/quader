import { splitRef } from '../api/repositories-api';
import { isSystemRepository } from './upstream-project';

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
 * one target of every **Set up client** (R-UI-126). `null` for a system repository of the project `upstream`, which answers no
 * client (R-REPO-070, R-REPO-075): no screen offers **Set up client** for it (the owner's Q-UI-891, Q-UI-896), and the type makes
 * every place that builds the button decide that. Its page `…/setup`, opened by its address, says why instead (R-UI-190).
 */
export function setupCommands(ref: string): string[] | null {
  const { project, key } = splitRef(ref);
  return isSystemRepository(project, key) ? null : [...repositoryCommands(ref), 'setup'];
}

/**
 * The router commands of the folder or the file at `path` of a repository's files (R-UI-010, Q-UI-510): its page with the path
 * after `files`; an empty path is the root of the files.
 */
export function filesCommands(ref: string, path: readonly string[] = []): string[] {
  const { project, key } = splitRef(ref);
  return ['/projects', project, 'repositories', key, 'files', ...path.filter((s) => s !== '')];
}
