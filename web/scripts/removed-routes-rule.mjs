// R-UI-126, ADR-202, ADR-203: the routes /setup and /browse are gone. Set up client is a page of its repository
// (`setupCommands` of src/app/core/project-links.ts) and the files are on the repository's page (`filesCommands`); a link to
// either old route would land on "Page not found". Run by check-rules.mjs over src/app (specs excepted: they assert that the routes
// are gone), tested by lint-rules.test.mjs.

/** A route literal of a removed route: `'/setup'`, `"/browse/…"`, `routerLink="/setup"`, `/setup?repository=` in a template. */
const removedRoute = /(['"`])\/(setup|browse)(?=[/?#'"`]|$)/;

/**
 * The lines of a source text that link to a removed route, each as its 1-based line and its text.
 * @param {string} text
 * @returns {{ line: number, text: string }[]}
 */
export function linksToRemovedRoutes(text) {
  const found = [];
  text.split('\n').forEach((line, i) => {
    if (removedRoute.test(line)) found.push({ line: i + 1, text: line.trim() });
  });
  return found;
}
