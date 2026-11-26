import { alicePassword, bobPassword, expect, expectAccessible, repositoryApi, signInWithKeycloak, signOut, test, unique, watchCsp } from '../support';

// R-UI-047 journey "upstream" (remote-curation.md R-CUR-018, R-CUR-015, R-CUR-005): the main path of the tab Upstream of an npm remote
// that delivers only what is allowed. alice, a Reader of the project, looks a package of the npm test upstream up by its exact name and
// asks for a version; bob, a Maintainer, sees the request set apart and allows it; the version is then delivered through the package
// protocol, and alice reads her request as allowed. The remote's upstream is the counting npm test upstream of the end-to-end tests
// (test/npmstub), which Quader reaches as https://npmstub.<ns>.svc; the browser needs nothing of it. TestE1_11Journeys publishes the
// package of this run there (QUADER_E2E_UPSTREAM_PACKAGE, version 1.0.0, a few hundred bytes) and exports the stub's host
// (QUADER_E2E_NPMSTUB_HOST, deploy/dev/e2e.sh with QUADER_E2E_NPMSTUB=1). Every other state of the tab is the renders' and the web unit
// tests'; the queue, the requests and the rules are the integration area npm's.

function env(name: string): string {
  const v = process.env[name];
  if (!v) throw new Error(`${name} is not set; the journey runs through TestE1_11Journeys with the npm test upstream deployed (QUADER_E2E_NPMSTUB=1)`);
  return v;
}

const pkg = env('QUADER_E2E_UPSTREAM_PACKAGE');
const stubUrl = `https://${env('QUADER_E2E_NPMSTUB_HOST')}/`;
const version = '1.0.0';
const project = unique('ui-curation').replace(/[^a-z0-9-]/g, '');
const repoKey = 'npm-upstream';
const tab = `/ui/en/projects/${project}/repositories/${repoKey}/upstream`;

test.beforeAll(async ({ admin }) => {
  const created = await admin.post('/api/v1/projects', { data: { key: project, displayName: `Journey ${project}`, description: '' } });
  expect(created.status(), await created.text()).toBe(201);
  const remote = await admin.post(`/api/v1/projects/${project}/repositories`, {
    data: { key: repoKey, class: 'remote', format: 'npm', remote: { url: stubUrl, delivery: { mode: 'onlyAllowed' } } },
  });
  expect(remote.status(), await remote.text()).toBe(201);
});

test.afterAll(async ({ admin }) => {
  await admin.delete(repositoryApi(repoKey, project));
  await admin.delete(`/api/v1/projects/${project}`);
});

test('R-CUR-018 R-CUR-015 a reader looks a package up and asks for a version, a Maintainer allows it, and the version is delivered', async ({ browser, admin }) => {
  // alice and bob sign in first, so that they exist (R-AUTH-007); then they become a Reader and a Maintainer of the project.
  const people = async (user: 'alice' | 'bob', password: string) => {
    const context = await browser.newContext({ ignoreHTTPSErrors: true, locale: 'en-US' });
    const page = await context.newPage();
    const csp = await watchCsp(page);
    const replicas = new Set<string>();
    page.on('response', (r) => {
      const id = r.headers()['x-quader-replica'];
      if (id) replicas.add(id);
    });
    await signInWithKeycloak(page, user, password);
    return { page, csp, replicas, context };
  };
  const alice = await people('alice', alicePassword);
  const bob = await people('bob', bobPassword);
  for (const [user, role] of [
    ['alice', 'reader'],
    ['bob', 'maintainer'],
  ] as const) {
    const member = await admin.put(`/api/v1/projects/${project}/members/${encodeURIComponent(`user:${user}`)}`, { data: { role } });
    expect(member.ok(), await member.text()).toBeTruthy();
  }
  // The versions' table, not a table of the README the package shows above it (R-CUR-028).
  const versionRow = (page: typeof alice.page) => page.getByTestId('upstream-lookup').locator('quader-data-table').getByRole('row').filter({ hasText: version });

  await test.step('alice opens the tab Upstream: the remote delivers only what is allowed; the lookup by exact name shows the package, not allowed', async () => {
    await alice.page.goto(tab);
    await expect(alice.page.getByTestId('upstream-mode')).toContainText('Delivers only what is allowed.');
    await alice.page.getByLabel('Search the upstream').fill(pkg);
    await alice.page.getByRole('button', { name: 'Look up by exact name' }).click();
    await expect(alice.page).toHaveURL(new RegExp(`[?&]name=${pkg}(&|$)`));
    await expect(alice.page.getByRole('heading', { name: pkg, level: 2 })).toBeVisible();
    await expect(versionRow(alice.page)).toContainText('Not allowed');
    await expectAccessible(alice.page, 'tab Upstream, a package looked up by a reader');
  });

  await test.step('alice asks for the version with a note; her requests list it as open', async () => {
    await versionRow(alice.page).getByRole('button', { name: `Request ${pkg} ${version}` }).click();
    const dialog = alice.page.getByRole('dialog', { name: `Request ${pkg} ${version}` });
    await dialog.getByLabel('Why you need it (optional)').fill('For the journey of the tab Upstream.');
    await expectAccessible(alice.page, 'the dialog of a request');
    await dialog.getByRole('button', { name: 'Send request' }).click();
    await expect(alice.page.getByText(`Your request for ${pkg} ${version} is sent to those who may allow it.`)).toBeVisible();
    const mine = alice.page.getByTestId('upstream-requests');
    await expect(mine.getByRole('heading', { name: /^Your requests/ })).toBeVisible();
    await expect(mine.getByRole('listitem').filter({ hasText: pkg })).toContainText('Open');
  });

  await test.step('bob sees the request set apart with alice\'s note and allows the version', async () => {
    await bob.page.goto(tab);
    const requests = bob.page.getByTestId('upstream-requests');
    await expect(requests.getByRole('heading', { name: /^Requests/ })).toBeVisible();
    const entry = requests.getByRole('listitem').filter({ hasText: pkg });
    await expect(entry).toContainText('For the journey of the tab Upstream.');
    await expectAccessible(bob.page, 'tab Upstream with the requests, as a Maintainer');
    await entry.getByRole('button', { name: `Actions for ${pkg} ${version}` }).click();
    await bob.page.getByRole('menuitem', { name: 'Allow…' }).click();
    const dialog = bob.page.getByRole('dialog', { name: `Allow ${pkg} ${version}` });
    await expect(dialog.getByRole('radio', { name: /This version/ })).toBeChecked();
    await dialog.getByRole('button', { name: 'Allow', exact: true }).click();
    await expect(bob.page.getByText(`${pkg} ${version} is allowed; its download starts.`)).toBeVisible();
    await expect(requests).toHaveCount(0);
  });

  await test.step('the version is delivered: the package protocol lists and serves it, and the tab shows it allowed and stored', async () => {
    const doc = `/api/npm/${project}/${repoKey}/${pkg}`;
    await expect
      .poll(async () => {
        const r = await admin.get(doc);
        return r.ok() ? Object.keys(((await r.json()) as { versions?: Record<string, unknown> }).versions ?? {}) : [r.status()];
      })
      .toEqual([version]);
    const tarball = await admin.get(`${doc}/-/${pkg}-${version}.tgz`);
    expect(tarball.status(), await tarball.text()).toBe(200);
    await expect
      .poll(async () => {
        await bob.page.goto(`${tab}?name=${pkg}`);
        return (await versionRow(bob.page).innerText()).replace(/\s+/g, ' ');
      })
      .toMatch(/Allowed.*Stored/);
  });

  await test.step('alice reads her request as allowed', async () => {
    await alice.page.reload();
    await expect(alice.page.getByTestId('upstream-requests').getByRole('listitem').filter({ hasText: pkg })).toContainText('Allowed');
    await expect(versionRow(alice.page)).toContainText('Allowed');
  });

  await test.step('Documentation on the tab opens the manual page of curating a remote', async () => {
    const help = alice.page.getByRole('link', { name: 'Documentation', exact: true });
    await expect(help).toHaveAttribute('href', /\/help\/user\/remote-curation$/);
  });

  for (const who of [alice, bob]) {
    expect(await who.csp(), 'securitypolicyviolation events').toEqual([]);
    expect(who.replicas.size, `replicas that answered: ${[...who.replicas]}`).toBeGreaterThanOrEqual(2);
    await signOut(who.page);
    await who.context.close();
  }
});
