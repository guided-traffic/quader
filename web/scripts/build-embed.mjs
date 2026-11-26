// Runs the localized production builds of the two applications and copies the per-locale output into the binary's trees
// (R-ARCH-004): the main installation's (project web) to ../internal/webui/dist/<locale>/, the Bastion's (project bastion-gui,
// bastion.md R-BAS-116) to ../internal/bastion/gui/dist/<locale>/. The committed placeholder index.html of each is never touched; stale
// locale directories are removed first.
//
// Every locale directory gets 3rdpartylicenses.txt (R-UI-001, Q-UI-144): the notices of the scripts and styles that
// Angular collects, the licence of the icon font, which carries no licence comment and so is not in that file, and the
// owners, sources, licences and trademark notices of the format logos in public/formats (Q-UI-227).
// The file is written after the hashed-name check, because it is not a hashed file and is never published to the store.
//
// The Bastion's build is held to its own budget and to the check of R-UI-228 over its metafile (bastion-bundle-rule.mjs): no client of
// the management API, no session, no screen of the main installation and no dialog may put a byte into it.
//
// `node scripts/build-embed.mjs bastion-gui` builds and copies the Bastion's application alone, with the same checks: the UI development
// profile's Bastion (deploy/ui-dev/bastion.sh) embeds it without the main installation's build, which its dev server serves.
//
// QUADER_UI_BUNDLE_MARK=1 builds the second bundle of the upgrade test across two bundles (R-HA-020, Q-HA-124): the licence
// comments of the libraries stay in the scripts instead of going to 3rdpartylicenses.txt, which changes the bytes and so
// the hashed name of nearly every script and leaves what the scripts do as it is. That bundle gets its notices from a
// second build that only extracts them, so every binary ships the notices of its own scripts. The mark is the main installation's;
// the Bastion's bundle is built as in a release.
import { spawnSync } from 'node:child_process';
import {
  cpSync,
  existsSync,
  readdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { bastionBundleFindings } from './bastion-bundle-rule.mjs';
import { checkBudget } from './check-budget.mjs';
import { checkHashed } from './hashed.mjs';

const root = fileURLToPath(new URL('..', import.meta.url));
const marker = 'QUADER_CSP_NONCE';
// The file the handler serves as <locale>/3rdpartylicenses.txt (TestR_UI_001_ThirdPartyNotices in internal/webui).
const notices = '3rdpartylicenses.txt';
const rule = '-'.repeat(80);

/** The two applications: the project, where its build lands, and the tree of the binary it is copied to. */
const allApps = [
  { project: 'web', out: join(root, 'dist', 'web'), target: join(root, '..', 'internal', 'webui', 'dist') },
  { project: 'bastion-gui', out: join(root, 'dist', 'bastion-gui'), target: join(root, '..', 'internal', 'bastion', 'gui', 'dist') },
];
const only = process.argv[2];
if (only !== undefined && only !== 'bastion-gui') {
  console.error(`build-embed: the one argument builds the application bastion-gui alone, not "${only}"`);
  process.exit(1);
}
const apps = only === undefined ? allApps : allApps.filter((a) => a.project === only);

const mark = process.env['QUADER_UI_BUNDLE_MARK'] ?? '';
if (mark !== '' && mark !== '1') {
  console.error(`build-embed: QUADER_UI_BUNDLE_MARK is "${mark}"; it is empty or 1`);
  process.exit(1);
}
const release = (project) => ['--no-install', 'ng', 'build', project, '--configuration', 'production'];
if (mark === '1') {
  console.log(
    'build-embed: QUADER_UI_BUNDLE_MARK=1, the second bundle: licence comments stay in the scripts',
  );
}

function ng(cmd) {
  const r = spawnSync('npx', cmd, { cwd: root, stdio: 'inherit' });
  if (r.status !== 0) process.exit(r.status ?? 1);
}

function fail(message, lines = []) {
  console.error(`build-embed: ${message}`);
  for (const l of lines) console.error(`  ${l}`);
  process.exit(1);
}

const isDir = (p) => statSync(p).isDirectory();

for (const app of apps) rmSync(app.out, { recursive: true, force: true });
if (only === undefined) ng(mark === '1' ? [...release('web'), '--extract-licenses=false'] : release('web'));
ng([...release('bastion-gui'), '--stats-json']);

// R-UI-228: what the Bastion's bundle holds, read from its build's metafile.
const meta = JSON.parse(readFileSync(join(root, 'dist', 'bastion-gui', 'browser-stats.json'), 'utf8'));
const held = bastionBundleFindings(meta);
if (held.length > 0) {
  fail(
    'the Bastion bundle holds modules it may not (R-UI-228, scripts/bastion-bundle-rule.mjs):',
    held.map((f) => `${f.path}: ${f.reason} (${f.bytes} bytes)`),
  );
}

/** The locale directories of an application's build, each checked: the nonce, the hashed names, no name of DiceBear's service. */
function checkedLocales(app) {
  const built = join(app.out, 'browser');
  if (!checkBudget(built)) fail(`the initial bundle of ${app.project} exceeds the budget of R-UI-001`);
  const locales = readdirSync(built).filter((name) => isDir(join(built, name)));
  if (locales.length === 0) fail(`no locale directory under ${built}`);
  for (const locale of locales) {
    const index = join(built, locale, 'index.html');
    if (!existsSync(index) || !readFileSync(index, 'utf8').includes(marker)) {
      fail(`${index} is missing or lacks the ${marker} placeholder`);
    }
    // R-UI-001, Q-UI-140: the server's pattern of hashed files must hold for this build, both ways.
    const problems = checkHashed(join(built, locale), join(root, 'public'));
    if (problems.length > 0) {
      fail(`the files of ${app.project} ${locale} do not match the hashed pattern of internal/webui/hashed.go:`, problems);
    }
    // R-UI-002, Q-UI-167: the generated avatar needs no host but the origin's own. DiceBear also runs as an HTTP service
    // (api.dicebear.com); no script of the build may name it.
    const dir = join(built, locale);
    for (const name of readdirSync(dir).filter((n) => n.endsWith('.js'))) {
      if (/api\.dicebear\.com/.test(readFileSync(join(dir, name), 'utf8'))) {
        fail(`${app.project} ${join(locale, name)} names the DiceBear HTTP service`);
      }
    }
  }
  return locales;
}

// The notices (Q-UI-144). Angular writes its file next to the locale directories; the second bundle extracts its own.
function angularNotices(app) {
  if (mark !== '1' || app.project !== 'web') return readFileSync(join(app.out, notices), 'utf8');
  const extract = join(root, 'dist', 'licences');
  ng([...release('web'), '--output-path', extract]);
  return readFileSync(join(extract, notices), 'utf8');
}

// The icon font (material-icons, Apache-2.0) is not in Angular's file: its CSS carries no licence comment.
function iconFontNotice() {
  const dir = join(root, 'node_modules', 'material-icons');
  const pkg = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'));
  const text = readFileSync(join(dir, 'LICENSE'), 'utf8');
  return `Package: ${pkg.name}\nVersion: ${pkg.version}\nLicense: "${pkg.license}"\n\n${text}`;
}

// The format logos (Q-UI-225, Q-UI-227): public/formats/NOTICE.txt names each logo file with its owner, source, licence and
// trademark notice. The file is also served next to the logos; here it joins the notices of the build.
const formatLogos = 'formats/NOTICE.txt';
function formatLogoNotice() {
  return readFileSync(join(root, 'public', formatLogos), 'utf8');
}

// The notices must name Inter with its OFL-1.1 licence, the icon font with its Apache-2.0 licence, and each format logo with its
// trademark notice and the line that no owner endorses Quader (Q-UI-227), or the build fails; the main installation's must also name
// DiceBear's core with the licence of the Bottts style (the artist's "free for personal and commercial use", Q-UI-167), which the
// Bastion's application does not draw.
const required = [
  '\nPackage: @fontsource-variable/inter\n',
  'License: "OFL-1.1"',
  'SIL OPEN FONT LICENSE Version 1.1',
  '\nPackage: material-icons\n',
  'License: "Apache-2.0"',
  'Quader is not affiliated with, sponsored or endorsed by any of them.',
  'File: formats/deb/openlogo-nd.svg\n',
  'Debian is a registered trademark owned by Software in the Public Interest, Inc.',
  'Files: formats/nuget/nuget.svg, formats/nuget/nuget_96_mono_w.png',
  'Creative Commons Attribution 2.0',
  'Files: formats/oci/oci-icon-color.svg, formats/oci/oci-icon-white.svg',
  'Open Container Initiative(R) is a registered trademark',
];
const requiredMain = [
  '\nPackage: @dicebear/core\n',
  '\nPackage: @dicebear/styles\n',
  '### Bottts\n',
  'Free for personal and commercial use',
];

const done = [];
for (const app of apps) {
  const locales = checkedLocales(app);
  // Angular's file ends with the rule that opens each package's section; the icon font's section follows it, then the logos.
  const angular = angularNotices(app).trimEnd();
  const text = `${angular.endsWith(rule) ? angular : `${angular}\n${rule}`}\n${iconFontNotice()}\n${rule}\n${formatLogoNotice()}`;
  const missing = [...required, ...(app.project === 'web' ? requiredMain : [])].filter((s) => !text.includes(s));
  if (missing.length > 0) {
    fail(`${app.project} ${notices} lacks ${missing.map((s) => JSON.stringify(s.trim())).join(', ')}`);
  }
  for (const name of readdirSync(app.target)) {
    const path = join(app.target, name);
    if (isDir(path)) rmSync(path, { recursive: true, force: true });
  }
  const built = join(app.out, 'browser');
  for (const locale of locales) {
    cpSync(join(built, locale), join(app.target, locale), { recursive: true });
    writeFileSync(join(app.target, locale, notices), text);
  }
  done.push(`${app.project}: ${locales.join(', ')} to ${app.target}`);
}
console.log(`build-embed: copied ${done.join('; ')}, each with ${notices}`);
