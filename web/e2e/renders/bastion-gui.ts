import { Page, Route, expect, request } from '@playwright/test';
import { publicGradedPackage, publicMalwarePackage, publicReadmePackage } from './advisories';
import { markedVersion } from './malware-marks';
import { bastionGradeDetails } from './grade-details';
import { docsDeb } from './debdocs';
import { factsNugetReadme } from './facts';
import { bastionEdge, locale } from './paths';
import { fileCommands } from './file-commands';
import { rowCommandFromList } from './version-heads';
import { Api, ansiblePackage, expectStatus, nugetPackage, otherProject, publicDownloads, remoteImage, remoteSource } from './seed';
import type { Screen } from './screens';

/**
 * The pages of the Bastion's web interface (R-UI-230) against the UI development profile's Bastion (deploy/ui-dev/bastion.sh): registered
 * at the instance as ui-dev with its interface and its documentation on, serving the public catalogue the instance builds from the render
 * seed. A visitor of a Bastion is anonymous (W-35's B.1), so every page is the kind `anonymous`, at the Bastion's own origin, in the
 * language of the run (the Bastion serves both builds, `/ui/en/…` and `/ui/de/…`), and in the German pass too.
 */

const B = `${bastionEdge}/ui/${locale}`;
const P = `${B}/projects/${otherProject}`;
const pkg = (p: { readonly format: string; readonly name: string }): string => `${P}/packages/${p.format}/${p.name}`;
const ver = (p: { readonly format: string; readonly name: string }, version: string): string => `${pkg(p)}@${version}`;
const collection = { format: 'ansible', name: `${ansiblePackage.namespace}.${ansiblePackage.name}` } as const;
const image = { format: 'oci', name: `${remoteSource.prefix}/${remoteImage.name}` } as const;
/** The platform team's public generic repository (seed.ts publicDownloads): its root, a nested folder and a file. */
const downloads = `${P}/repositories/${publicDownloads.repo}`;

/** A Debian package of the platform team's public repository, with its contents, changelog and copyright (docs on). */
const debian = { format: 'deb', name: 'acme-agent', version: '1.3.0-1' } as const;

/**
 * A page's normal answers that are no failure: a dependency's name the catalogue does not hold answers its lookup 404 and stays text
 * (R-UI-221), as at the main installation.
 */
const lookups = [{ match: /^GET \/ui\/data\/v1\/packages\/lookup\?format=[a-z]+&name=[^&\s]+ answered 404$/, why: 'a dependency the catalogue does not hold stays text' }];

/** The search of a project's packages: the list as it opens, then a search by a word of one package's name. */
async function* search(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-package-table tbody tr').first()).toBeVisible();
  yield '';
  await page.locator('main quader-list-head input[type=search]').fill(publicGradedPackage.name.split('-')[0] ?? publicGradedPackage.name);
  await expect(page).toHaveURL(/[?&]q=/);
  await expect(page.locator('main quader-package-table tbody tr').first()).toBeVisible();
  yield 'search';
}

/**
 * A project's search that finds documentation (R-UI-232, bastion.md R-BAS-125): a word of a module of the project's collection, the
 * section Documentation below the packages, one entry opening its page.
 */
async function* searchDocumentation(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-package-table')).toBeVisible();
  await page.locator('main quader-list-head input[type=search]').fill('mount');
  await expect(page).toHaveURL(/[?&]q=mount/);
  await expect(page.locator('main [data-testid="documentation-hits"] tbody tr').first()).toBeVisible();
  yield '';
}

/** The search inside a version's documentation (R-ANS-036 at the Bastion, R-BAS-124): the narrowed tree with its count of matches. */
async function* searchVersion(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-ansible-docs .pane-tree a').first()).toBeVisible();
  yield '';
}

/** The state answered as the Bastion answers it, with the fields a walk sets: a band above an old page. */
async function* stale(page: Page): AsyncGenerator<string> {
  const at = new Date(Date.now() - 3 * 3_600_000).toISOString();
  await page.route('**/ui/data/v1/state', async (route: Route) => {
    const res = await route.fetch();
    const body = (await res.json()) as Record<string, unknown>;
    await route.fulfill({ response: res, body: JSON.stringify({ ...body, confirmedAt: at, stale: true }) });
  });
  await page.reload();
  await expect(page.locator('.stale-band')).toBeVisible();
  yield '';
}

/** A replica without a catalogue: the state answers 503 not_ready, as the Bastion's own problem (R-BAS-117), and the page says so. */
async function* notReady(page: Page): AsyncGenerator<string> {
  await page.route('**/ui/data/v1/state', (route: Route) =>
    route.fulfill({
      status: 503,
      headers: { 'Retry-After': '30', 'Content-Type': 'application/problem+json' },
      body: JSON.stringify({
        type: 'about:blank',
        title: 'Service Unavailable',
        status: 503,
        detail: 'This page is not available right now. Try again in a few minutes.',
        code: 'not_ready',
      }),
    }),
  );
  await page.reload();
  await expect(page.locator('main .not-ready')).toBeVisible();
  yield '';
}

/**
 * The installation's default form of a time as the Bastion's state names it (bastion.md R-BAS-128): ISO 8601 in place of the factory
 * form, with an old state so that the band writes an absolute time in it. The change itself travels as the configuration's display, which
 * the profile shows with Admin › Settings › General; the walk answers the state as a Bastion does after that change.
 */
async function* isoTime(page: Page): AsyncGenerator<string> {
  const at = new Date(Date.now() - 3 * 3_600_000).toISOString();
  await page.route('**/ui/data/v1/state', async (route: Route) => {
    const res = await route.fetch();
    const body = (await res.json()) as Record<string, unknown>;
    await route.fulfill({ response: res, body: JSON.stringify({ ...body, confirmedAt: at, stale: true, timeFormat: 'iso' }) });
  });
  await page.reload();
  await expect(page.locator('.stale-band')).toContainText(/\d{4}-\d{2}-\d{2}/);
  yield '';
}

/** Set up client of a public repository (bastion.md R-BAS-129): the snippets of the chosen client, read from the catalogue. */
async function* setupSteps(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-setup-steps quader-code-window').first()).toBeVisible();
  yield '';
}

const notReadyAnswer = [{ match: /^GET \/ui\/data\/v1\/state answered 503$/, why: 'the walk answers the state as a replica without a catalogue does' }];

/** The public package whose versions carry every malware state (malware.ts, R-UI-258). */
const checksums = { format: publicMalwarePackage.format, name: publicMalwarePackage.name } as const;

const markDetails = (page: Page) => page.locator('.cdk-overlay-container [data-testid="malware-details"]');

/**
 * A package's versions at the Bastion with their marks (R-UI-258): the table as it opens, then the details of a checked version (the word
 * without a class and the page that tells when, Q-UI-1541), of the infected one and of the released one, each opened by its tap.
 */
async function* markedVersions(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main [data-testid="malware-mark"]').first()).toBeVisible();
  yield '';
  for (const [name, state] of [
    ['checked', 'checked'],
    ['malware', 'malware'],
    ['released', 'released'],
  ] as const) {
    await page.locator(`main [data-testid="malware-mark"][data-state="${state}"]`).first().click();
    await expect(markDetails(page)).toBeVisible();
    yield name;
    await page.keyboard.press('Escape');
    await expect(markDetails(page)).toBeHidden();
  }
}

/** A version's header at the Bastion with its mark, then its details opened by a tap: the class computed from the dates (Q-UI-1540). */
async function* markedHead(page: Page): AsyncGenerator<string> {
  const head = page.locator('main .facts-malware [data-testid="malware-mark"]');
  await expect(head).toBeVisible();
  yield '';
  await head.click();
  await expect(markDetails(page)).toBeVisible();
  yield 'details';
}

/** Every page of the Bastion's web interface the renders hold (R-UI-230), each as the anonymous visitor, in the German pass too. */
export const bastionScreens: readonly Screen[] = [
  { id: 'bastion-start', path: `${B}/`, kinds: ['anonymous'], de: true },
  { id: 'bastion-packages', path: `${P}/packages`, kinds: ['anonymous'], walk: search, tables: 'quader-package-table', de: true },
  // The versions of a package with open findings in some of them, and a version with its findings, signatures and files.
  { id: 'bastion-package', path: pkg(publicGradedPackage), kinds: ['anonymous'], de: true },
  {
    id: 'bastion-version-findings',
    path: `${ver(publicGradedPackage, publicGradedPackage.version)}?tab=vulnerabilities`,
    kinds: ['anonymous'],
    expected: lookups,
    de: true,
  },
  // A NuGet package with its signatures (seeded by signatures.ts) and its dependency groups.
  { id: 'bastion-version-nuget', path: ver(nugetPackage, nugetPackage.versions[1]), kinds: ['anonymous'], expected: lookups, de: true },
  { id: 'bastion-version-deb', path: ver(debian, debian.version), kinds: ['anonymous'], expected: lookups, de: true },
  // An image of the public remote: its tags, then a tag with its platforms and layers.
  { id: 'bastion-image', path: pkg(image), kinds: ['anonymous'], de: true },
  // A row's Pull or Install of the flat list and of an image's tags, its panel open (R-UI-269): how a visitor pulls a public image.
  { id: 'bastion-packages-menu', path: `${P}/packages`, kinds: ['anonymous'], walk: rowCommandFromList, de: true },
  { id: 'bastion-image-menu', path: pkg(image), kinds: ['anonymous'], walk: rowCommandFromList, de: true },
  { id: 'bastion-image-version', path: ver(image, remoteImage.single), kinds: ['anonymous'], de: true },
  // An Ansible collection opens on its documentation (docs on), its package's own address on its newest version (R-UI-218); a module's
  // page with its tables; the search inside the version with a hit; a project's search that finds a module (R-UI-232).
  { id: 'bastion-collection', path: ver(collection, ansiblePackage.versions[1]), kinds: ['anonymous'], expected: lookups, de: true },
  // A public generic repository's folders and files (R-UI-229): its root with a folder and a file, a nested folder of files, and a file
  // with the panel of its Download, each client's set-up command.
  { id: 'bastion-files', path: downloads, kinds: ['anonymous'], tables: 'quader-folder-view quader-data-table', de: true },
  { id: 'bastion-files-folder', path: `${downloads}/files/${publicDownloads.nested}`, kinds: ['anonymous'], tables: 'quader-folder-view quader-data-table', de: true },
  { id: 'bastion-files-file', path: `${downloads}/files/${publicDownloads.file}`, kinds: ['anonymous'], walk: fileCommands, de: true },
  { id: 'bastion-collection-latest', path: pkg(collection), kinds: ['anonymous'], expected: lookups },
  {
    id: 'bastion-collection-entry',
    path: `${ver(collection, ansiblePackage.versions[1])}?doc=module/mount_volume`,
    kinds: ['anonymous'],
    expected: lookups,
    tables: 'quader-ansible-doc-entry quader-data-table',
    de: true,
  },
  {
    id: 'bastion-collection-search',
    path: `${ver(collection, ansiblePackage.versions[1])}?q=mount`,
    kinds: ['anonymous'],
    expected: lookups,
    walk: searchVersion,
    de: true,
  },
  { id: 'bastion-packages-docs', path: `${P}/packages`, kinds: ['anonymous'], walk: searchDocumentation, de: true },
  // The README of an npm and of a NuGet package, which their addresses open on (R-UI-218), and a Debian package's changelog and copyright.
  { id: 'bastion-version-npm', path: pkg(publicReadmePackage), kinds: ['anonymous'], expected: lookups, de: true },
  { id: 'bastion-version-nuget-readme', path: pkg(factsNugetReadme), kinds: ['anonymous'], expected: lookups, de: true },
  // The Debian package of debdocs.ts, whose file holds a changelog of thirty entries and a copyright.
  { id: 'bastion-version-deb-changelog', path: `${ver(docsDeb, docsDeb.version)}?tab=changelog`, kinds: ['anonymous'], expected: lookups, de: true },
  { id: 'bastion-imprint', path: `${B}/legal/imprint`, kinds: ['anonymous'], de: true },
  { id: 'bastion-privacy', path: `${B}/legal/privacy`, kinds: ['anonymous'], de: true },
  { id: 'bastion-stale', path: `${P}/packages`, kinds: ['anonymous'], walk: stale, de: true },
  // What a visitor met without (bastion.md R-BAS-128, R-BAS-129): the grade column of a package's versions (one version with open
  // findings, one clean), Set up client of an npm, an OCI and a generic repository (whose configure step defines the function a file's
  // command calls), and the installation's form of a time.
  { id: 'bastion-versions-graded', path: `${pkg(publicGradedPackage)}/-/versions`, kinds: ['anonymous'], de: true },
  { id: 'bastion-setup-npm', path: `${P}/repositories/npm-open-source/setup?client=npm`, kinds: ['anonymous'], walk: setupSteps, de: true },
  { id: 'bastion-setup-oci', path: `${P}/repositories/${remoteSource.key}/setup?client=docker`, kinds: ['anonymous'], walk: setupSteps, de: true },
  { id: 'bastion-setup-generic', path: `${downloads}/setup?client=curl`, kinds: ['anonymous'], walk: setupSteps, de: true },
  { id: 'bastion-time-iso', path: `${P}/packages`, kinds: ['anonymous'], walk: isoTime, de: true },
  { id: 'bastion-not-ready', path: `${B}/`, kinds: ['anonymous'], walk: notReady, expected: notReadyAnswer, de: true },
  { id: 'bastion-not-found', path: `${B}/projects/${otherProject}/nothing-here`, kinds: ['anonymous'], de: true },
  // The malware mark at the Bastion (R-UI-258): a package's versions with their marks and their details; the newest version's header,
  // details and Files tab under today's signatures; a version under older signatures, an overdue one, an infected and a released one; and
  // an image's tag of two platforms with its parts. The image is image-source's, both platforms stored: the Bastion shows public
  // repositories, and the remote image-cache, whose tag has a platform it never fetched (malware-marks-image-partial), is private.
  { id: 'bastion-malware-versions', path: `${pkg(checksums)}/-/versions`, kinds: ['anonymous'], walk: markedVersions, de: true },
  { id: 'bastion-malware-current', path: ver(checksums, '1.4.0'), kinds: ['anonymous'], expected: lookups, walk: markedVersion, de: true },
  { id: 'bastion-malware-older', path: ver(checksums, '1.3.0'), kinds: ['anonymous'], expected: lookups, walk: markedHead, de: true },
  { id: 'bastion-malware-overdue', path: ver(checksums, '1.2.0'), kinds: ['anonymous'], expected: lookups, walk: markedHead, de: true },
  { id: 'bastion-malware-infected', path: ver(checksums, '1.1.0'), kinds: ['anonymous'], expected: lookups, walk: markedHead, de: true },
  { id: 'bastion-malware-released', path: ver(checksums, '1.0.0'), kinds: ['anonymous'], expected: lookups, walk: markedHead, de: true },
  { id: 'bastion-malware-image', path: ver(image, remoteImage.partial), kinds: ['anonymous'], walk: markedVersion, de: true },
  // An image's grade at the Bastion (R-UI-258, R-VULN-052): scanned under a database six days older than today's, its details opened.
  { id: 'bastion-grade-details-image', path: ver(image, remoteImage.single), kinds: ['anonymous'], walk: bastionGradeDetails, de: true },
];

/** The state a job ended in, after at most five minutes. */
async function jobEnd(admin: Api, id: string): Promise<string> {
  for (let i = 0; i < 300; i++) {
    const read = await admin.get(`/api/v1/system/jobs/${id}`);
    await expectStatus(read, [200], 'the run of the public catalogue');
    const state = ((await read.json()) as { state: string }).state;
    if (!['queued', 'running', 'cancelling'].includes(state)) return state;
    await new Promise((r) => setTimeout(r, 1000));
  }
  return 'not ended within five minutes';
}

/** The packages the Bastion's screens open: the setup waits until the Bastion's catalogue holds each. */
const shown: readonly { readonly format: string; readonly name: string }[] = [
  publicGradedPackage,
  publicReadmePackage,
  nugetPackage,
  factsNugetReadme,
  debian,
  docsDeb,
  image,
  collection,
  { format: publicMalwarePackage.format, name: publicMalwarePackage.name },
];

/**
 * After the seed: the public catalogue's builder runs once more (the seed's writes through the database send no signal), and the setup
 * waits until the profile's Bastion has mirrored what that run built and holds every package its screens open. Nothing when no Bastion
 * answers (UI_BASTION=0): its screens fail then.
 */
export async function awaitBastionCatalogue(admin: Api): Promise<void> {
  const bastion = await request.newContext({ baseURL: bastionEdge, ignoreHTTPSErrors: true });
  try {
    const first = await bastion.get('/ui/data/v1/state').catch(() => null);
    if (first === null) {
      console.log(`setup: no Bastion answers at ${bastionEdge}; the Bastion's screens will fail`);
      return;
    }
    // A run that is queued already (a trigger of the seed's publishes) reads everything when it starts and is waited for; one that
    // runs already may have started before the seed's last writes, so another follows it.
    for (let attempt = 0; ; attempt++) {
      const run = await admin.post('/api/v1/system/tasks/bastion-catalogue/run', {});
      await expectStatus(run, [202, 409], 'a run of the public catalogue');
      const job = ((await run.json()) as { job: { id: string; state: string } }).job;
      const ended = await jobEnd(admin, job.id);
      if (ended !== 'succeeded') throw new Error(`the run of the public catalogue ended ${ended}`);
      if (run.status() === 202 || job.state === 'queued') break;
      if (attempt === 3) throw new Error('the public catalogue was running at every one of four starts');
    }
    const built = Date.now();
    for (let i = 0; i < 120; i++) {
      const state = await bastion.get('/ui/data/v1/state');
      const confirmed = state.ok() ? Date.parse(((await state.json()) as { confirmedAt: string }).confirmedAt) : 0;
      if (confirmed > built) {
        const missing: string[] = [];
        for (const p of shown) {
          const found = await bastion.get(`/ui/data/v1/packages/lookup?format=${p.format}&name=${encodeURIComponent(p.name)}`);
          if (!found.ok()) missing.push(`${p.format} ${p.name}`);
        }
        const files = await bastion.get('/ui/data/v1/files');
        const held = files.ok() ? ((await files.json()) as { items: string[] }).items : [];
        if (!held.includes(`${otherProject}/${publicDownloads.repo}`)) missing.push(`the files of ${otherProject}/${publicDownloads.repo}`);
        if (missing.length === 0) return;
        throw new Error(`the Bastion's catalogue lacks ${missing.join(', ')} after the builder's run`);
      }
      await new Promise((r) => setTimeout(r, 1000));
    }
    throw new Error(`the Bastion at ${bastionEdge} did not confirm its catalogue within two minutes of the builder's run`);
  } finally {
    await bastion.dispose();
  }
}
