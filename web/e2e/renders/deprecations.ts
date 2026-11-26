import { createHash } from 'node:crypto';
import { APIRequestContext, Page, expect } from '@playwright/test';
import { collection, expectStatus, lit, longNamePackage, mainProject, nugetPackage, otherProject, patient, psql } from './seed';
import { versionHead } from './version-heads';

/*
 * The deprecation of a version or a package (R-API-085, R-NUGET-036, R-ANS-060): the seed deprecates, through the management operation,
 * an npm version of the long-named package with a long message and an address, the newest version of another npm package (so that the
 * list marks it and its filter finds it), a NuGet version with two reasons, a long message and the package to use instead, and a
 * collection of its own as a whole; and it unlists a NuGet version beside the deprecated one. The screens are the banner of each format,
 * the table of versions and the switch with their marks, the act's menu and dialog, and the list Packages with its filter.
 */

/** The collection the seed deprecates as a whole: a name of its own, so that the documentation's renders keep theirs. */
export const deprecatedCollection = { format: 'ansible', repo: 'collections', namespace: 'acme', name: 'legacy_tools', versions: ['1.0.0', '1.1.0'] } as const;
/** The npm package whose only version is deprecated: the list marks it and its filter finds it. */
export const deprecatedNpm = { repo: 'npm-internal', name: '@acme/eslint-config', version: '3.2.1' } as const;
/** The NuGet version the seed unlists beside the deprecated one. */
export const unlistedNuget = nugetPackage.versions[1];

/** A message long enough to wrap the banner at 390 px, with an address that stays text. */
const longMessage =
  'This line of the generated client is no longer maintained: its models were generated from the 2025 schema of the portal API, which ' +
  'the portal stopped answering on 2026-10-01. Use 0.4.0 or later; see https://git.example.invalid/portal/api-client/-/blob/main/MIGRATING.md for the changes.';

/** Publishes the collection and deprecates what the screens show, as the power user. Every write is idempotent. */
export async function seedDeprecations(ctx: APIRequestContext, token: string): Promise<void> {
  const auth = { Authorization: `Bearer ${token}` };
  for (const version of deprecatedCollection.versions) {
    const archive = collection(deprecatedCollection.namespace, deprecatedCollection.name, version);
    const res = await patient(() =>
      ctx.post(`/api/ansible/${otherProject}/${deprecatedCollection.repo}/v3/artifacts/collections/`, {
        headers: auth,
        multipart: {
          sha256: createHash('sha256').update(archive).digest('hex'),
          file: { name: `${deprecatedCollection.namespace}-${deprecatedCollection.name}-${version}.tar.gz`, mimeType: 'application/gzip', buffer: archive },
        },
      }),
    );
    await expectStatus(res, [200, 201, 202, 409], `ansible publish ${deprecatedCollection.name} ${version}`);
  }
  const deprecate = async (project: string, repo: string, name: string, body: Record<string, unknown>): Promise<void> => {
    const url = `/api/v1/projects/${project}/repositories/${repo}/packages/${encodeURIComponent(name)}/deprecation`;
    // The collection's versions are imported by a task: its package row may not be there at the first try.
    for (let i = 0; ; i++) {
      const res = await patient(() => ctx.put(url, { data: body, headers: auth }));
      if (res.status() === 200 || i >= 20) return expectStatus(res, [200], `deprecate ${repo}/${name}`);
      await new Promise((r) => setTimeout(r, 500));
    }
  };
  await deprecate(otherProject, deprecatedCollection.repo, `${deprecatedCollection.namespace}.${deprecatedCollection.name}`, {
    message: 'Use acme.platform; its roles replace every module of this collection.',
  });
  await deprecate(otherProject, nugetPackage.repo, nugetPackage.name, {
    version: nugetPackage.versions[0],
    reasons: ['Legacy', 'CriticalBugs'],
    message:
      'The 1.0 line writes the scopes of a log entry twice under load and gets no fixes. Move to 1.1.0 or later, which keeps the same API; ' +
      'see https://git.example.invalid/platform/logging/-/releases for the changes.',
    alternatePackage: { name: nugetPackage.name, range: '[1.1.0, )' },
  });
  await deprecate(mainProject, 'npm-internal', longNamePackage.name, { version: '0.3.0', message: longMessage });
  await deprecate(mainProject, deprecatedNpm.repo, deprecatedNpm.name, {
    version: deprecatedNpm.version,
    message: 'Moved to @acme/lint-config.',
  });
  // NuGet's unlist (R-NUGET-007): beside the deprecated version, a version with a mark of its own word. The power user may deploy and
  // not delete in that project, and an unlist is a protocol DELETE: the seed writes the state `dotnet nuget delete` leaves, and raises
  // the package's revision as the unlist does.
  const pkg = `(SELECT p.id FROM packages p JOIN repositories r ON r.id = p.repo_id JOIN project pr ON pr.id = r.project_id
                 WHERE pr.key = ${lit(otherProject)} AND r.key = ${lit(nugetPackage.repo)} AND p.name = ${lit(nugetPackage.name.toLowerCase())})`;
  psql(`UPDATE nuget_version nv SET listed = false FROM package_versions pv
         WHERE nv.version_id = pv.id AND pv.package_id = ${pkg} AND pv.version = ${lit(unlistedNuget)} AND nv.listed;
        UPDATE packages SET rev_seq = rev_seq + 1 WHERE id = ${pkg};`);
}

/** A version's head with the act's menu open and its dialog, after the switch and the command of {@link versionHead}. */
export async function* deprecationActs(page: Page): AsyncGenerator<string> {
  yield* versionHead(page);
  const acts = page.locator('main [data-testid="version-acts"]');
  if ((await acts.count()) === 0) return;
  await acts.click();
  await expect(page.locator('.cdk-overlay-container .menu')).toBeVisible();
  yield 'acts';
  await page.locator('.cdk-overlay-container .menu-item').first().click();
  await expect(page.locator('.quader-dialog')).toBeVisible();
  yield 'dialog';
  await page.keyboard.press('Escape');
}
