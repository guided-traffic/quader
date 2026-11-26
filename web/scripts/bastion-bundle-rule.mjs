// R-UI-228: the Bastion's application holds no write, no session and no screen of the main installation's (W-35's B.7: "Es gibt im API
// (und GUI) Interface vom Bastion keine Möglichkeit eine schreibende Operation durchzuführen"). The views it composes are shared with
// the main installation (R-UI-223), and a view reaches core/ and shared/, which the views rule reads one level deep only; what the
// bundle finally holds is what esbuild put into it. This check reads the build's metafile (`ng build bastion-gui --stats-json`, the file
// browser-stats.json) and fails for every source module that contributed a byte to any output file and is one of:
//  - the API client (src/app/api/client.ts, the one place of every write method), the CSRF header (src/app/api/csrf.ts), an HTTP class
//    of the management API (a module src/app/api/*-api.ts or *-api-http.ts other than the abstract reads the Bastion binds itself);
//  - the session (src/app/core/session-store.ts, src/app/api/session-api.ts), the authorization epoch (src/app/core/authz-epoch.ts);
//  - a screen of the main installation that is no page of the Bastion: any module of src/app/features/ outside the folders the views
//    use (packages, repositories, setup, vulnerabilities, legal, not-found) and features/limits/figures.ts (the words of a limit);
//  - a dialog (a module whose name ends in -dialog, src/app/shared/ui/dialog.ts).
// A module that only esbuild's graph names, with no byte in an output, is tree-shaken and passes. Run by build-embed.mjs after the
// Bastion's build, tested by lint-rules.test.mjs.

/** The modules of src/app/api/ a Bastion's bundle may hold: types, the abstract reads it binds, pure helpers. */
const API_ALLOWED = /^src\/app\/api\/(problem|pagination|refs|query|types|package-reads|vulnerability-reads|signature-reads|legal-api)\.ts$/;
/** The folders of features/ the shared views reach. */
const FEATURES_ALLOWED = /^src\/app\/features\/(packages|repositories|setup|vulnerabilities|legal|not-found)\/|^src\/app\/features\/limits\/figures\.ts$/;

/**
 * Why a module may not be in the Bastion's bundle, or null when it may.
 * @param {string} path a path of the metafile, relative to web/
 * @returns {string | null}
 */
export function forbiddenReason(path) {
  if (!path.startsWith('src/app/')) return null;
  if (path === 'src/app/core/session-store.ts' || path === 'src/app/core/authz-epoch.ts') return 'the session';
  if (path.startsWith('src/app/api/') && !path.startsWith('src/app/api/generated/') && !API_ALLOWED.test(path)) {
    return 'a client of the management API';
  }
  if (path.startsWith('src/app/features/') && !FEATURES_ALLOWED.test(path)) return 'a screen of the main installation';
  if (/-dialog\.ts$/.test(path) || path === 'src/app/shared/ui/dialog.ts') return 'a dialog';
  return null;
}

/**
 * The modules of a metafile that put bytes into an output and may not be in the Bastion's bundle.
 * @param {{ outputs: Record<string, { inputs?: Record<string, { bytesInOutput: number }> }> }} meta esbuild's metafile
 * @returns {{ path: string, reason: string, bytes: number }[]}
 */
export function bastionBundleFindings(meta) {
  const used = new Map();
  for (const output of Object.values(meta.outputs ?? {})) {
    for (const [path, input] of Object.entries(output.inputs ?? {})) {
      if (input.bytesInOutput > 0) used.set(path, (used.get(path) ?? 0) + input.bytesInOutput);
    }
  }
  const out = [];
  for (const [path, bytes] of used) {
    const reason = forbiddenReason(path);
    if (reason) out.push({ path, reason, bytes });
  }
  return out.sort((a, b) => a.path.localeCompare(b.path));
}
