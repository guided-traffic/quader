import { join, resolve } from 'node:path';

/** The repository root: web/e2e/renders/../../.. */
export const root = resolve(__dirname, '..', '..', '..');
/** The edge of the UI development profile: the dev server, which proxies everything outside /ui/ to Quader. */
export const edge = process.env['UI_RENDERS_EDGE'] ?? 'https://localhost:4200';
/**
 * The profile's Bastion (deploy/ui-dev/bastion.sh): its client listener, where its web interface answers (R-UI-230); renders.sh names
 * the instance's.
 */
export const bastionEdge = process.env['UI_RENDERS_BASTION'] ?? 'https://localhost:19091';
/**
 * The generated state of the profile (deploy/ui-dev/up.sh): the break-glass password. UI_RENDERS_STATE names the state directory of
 * a stack of its own (docs/developer/web-ui.md#local-ui-development).
 */
export const stateDir = process.env['UI_RENDERS_STATE'] ?? join(root, '.dev', 'ui-dev');
/**
 * The language of the run (UI_RENDERS_LOCALE): `en`, or `de` for the German pass, which renders the screens marked `de` against the
 * dev server's German build (deploy/ui-dev/renders.sh starts it, web/angular.json's configuration `de`; Q-UI-1048).
 */
export const locale: 'en' | 'de' = choose('UI_RENDERS_LOCALE', ['en', 'de'] as const);
/** The browser engine of the run (UI_RENDERS_ENGINE): `chromium`, the pipeline's, or `firefox` or `webkit` for a look by hand (Q-UI-1049). */
export const engine: 'chromium' | 'firefox' | 'webkit' = choose('UI_RENDERS_ENGINE', ['chromium', 'firefox', 'webkit'] as const);

/**
 * The part of the screens the run renders (UI_RENDERS_SHARD=i/n, `make ui-renders SHARD=i/n`; Q-ARCH-1071): the i-th of n, counted
 * through the pairs of screen and kind of user in the order of render.spec.ts, every n-th from the i-th. Each pair is in exactly one
 * part, a new screen lands in one by its place in screens.ts, and the parts differ by one pair at most whatever the screens are. Unset,
 * the whole set (1/1). The job ui-layout of Test and Release renders one part per leg of its matrix.
 */
export const shard: { readonly index: number; readonly total: number } = parseShard(process.env['UI_RENDERS_SHARD'] || '1/1');

/** Whether the pair at this place of render.spec.ts's order is in the run's part. */
export const inShard = (place: number): boolean => place % shard.total === shard.index - 1;

function parseShard(value: string): { index: number; total: number } {
  const m = /^([1-9]\d*)\/([1-9]\d*)$/.exec(value);
  const index = Number(m?.[1]);
  const total = Number(m?.[2]);
  if (!m || index > total) throw new Error(`UI_RENDERS_SHARD=${value}: i/n with 1 <= i <= n, such as 2/4`);
  return { index, total };
}

function choose<T extends string>(name: string, values: readonly T[]): T {
  const v = process.env[name] || values[0];
  if (!values.includes(v as T)) throw new Error(`${name}=${v}: one of ${values.join(', ')}`);
  return v as T;
}

/**
 * Where the pictures, the contact sheet and the report go: `.out/ui-renders`, and for a run in another language or engine a folder of
 * its own beside it (`.out/ui-renders-de`, `.out/ui-renders-firefox`, `.out/ui-renders-webkit-de`), so that no run replaces the pictures
 * of another.
 */
const variant = [engine === 'chromium' ? '' : engine, locale === 'en' ? '' : locale].filter((v) => v !== '').join('-');
export const outDir = join(root, '.out', variant === '' ? 'ui-renders' : `ui-renders-${variant}`);
/** The session of one kind of user, kept by the setup. */
export const authFile = (kind: string): string => join(outDir, '.auth', `${kind}.json`);
/** One line of JSON per picture and per failure, written by the renders and read by the contact sheet. */
export const manifestDir = join(outDir, '.manifest');

/**
 * Chromium's arguments for text (Q-UI-1045): no font hinting. On Linux, the runner's, Chromium hints the app's font and rounds
 * every glyph's advance to a whole pixel, which set the same words up to 3 % wider than on macOS (bold "Imported advisory data
 * from the vulnerability source" 366 px against 355.5 px), so the walk passed a table on the development machine that fell to
 * blocks on the runner (2026-11-23). Without hinting Linux measures what macOS measures, to a twentieth of a pixel; Firefox and
 * WebKit measured alike on both without a flag.
 */
export const CHROMIUM_TEXT = ['--font-render-hinting=none'] as const;
