import { expect, expectAccessible, signInLocal, test, unique } from '../support';

// R-BAS-007 journey "Bastions" (R-BAS-049): an administrator opens Admin area > Bastions, opens the card of the e2e Bastion where the
// page folded it (R-UI-240), sees its host in the head and the last contact of its replicas, registers a Bastion, whose card opens,
// issues a token that expires in 20 days, copies it once, and sees the section Tokens open by itself with the warning and the mark on the
// token afterwards. The token's value is never shown again. Then the new Bastion is disabled softly and enabled again from its head
// (R-BAS-133: with no replica the badge says Disable not confirmed at once), and its token is invalidated.

const registration = process.env['QUADER_E2E_BASTION_REGISTRATION'];

test('R-BAS-007 the Bastions screen shows the last contact of the replicas, issues a token once and marks one that expires soon', async ({
  page,
  admin,
  context,
}) => {
  if (!registration) throw new Error('QUADER_E2E_BASTION_REGISTRATION is not set: run the journey through TestBastion_R_BAS_007_Journey');
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await signInLocal(page);

  await page.goto('/ui/en/admin/bastions');
  await expect(page.getByRole('heading', { name: 'Bastions', level: 1 })).toBeVisible();

  // the Bastion of the e2e cluster: both replicas have made contact
  const e2e = page.getByRole('region', { name: registration, exact: true });
  await expect(e2e).toBeVisible();
  // a page of several Bastions folds the cards that do not warn (Q-UI-1640): open it
  const fold = e2e.locator('.head button.fold');
  if ((await fold.getAttribute('aria-expanded')) !== 'true') await fold.click();
  await expect(fold).toHaveAttribute('aria-expanded', 'true');
  // the head names the host its replicas reported, in grey after the name (Q-UI-1641)
  await expect(e2e.locator('[data-testid="bastion-hosts"]')).toContainText('bastion-');
  // the section Replicas is one line while the replicas agree (Q-UI-1622): open it for the table
  const replicas = e2e.locator('[data-testid="bastion-replicas"] button[aria-expanded]');
  if ((await replicas.getAttribute('aria-expanded')) !== 'true') await replicas.click();
  await expect(replicas).toHaveAttribute('aria-expanded', 'true');
  const contacts = e2e.locator('[data-testid="bastion-replicas"] quader-data-table');
  // the table also lists replicas of earlier pods of the release (the main installation keeps their last contact): the live ones are in it
  for (const replica of (process.env['QUADER_E2E_BASTION_REPLICAS'] ?? '').split(',').filter((r) => r !== '')) {
    await expect(contacts).toContainText(replica);
  }
  await expectAccessible(page, 'Bastions, the e2e registration with its contacts');

  // a registration of the journey
  const name = unique('jrn').slice(0, 20).toLowerCase().replace(/[^a-z0-9-]/g, '');
  try {
    await page.getByRole('button', { name: 'New Bastion' }).click();
    await page.getByLabel('Name').fill(name);
    await page.getByLabel('Description').fill('browser journey');
    await expectAccessible(page, 'new Bastion dialog');
    await page.getByRole('button', { name: 'Create Bastion' }).click();
    const card = page.getByRole('region', { name, exact: true });
    await expect(card).toBeVisible();

    await card.getByRole('button', { name: `Issue token ${name}` }).click();
    await page.getByLabel('Description').fill('expires in 20 days');
    await page.getByLabel('Lifetime in days').fill('20');
    await page.getByRole('button', { name: 'Issue token', exact: true }).click();

    // the value is shown once: copy it
    const secret = card.getByRole('region', { name: 'Your new token' });
    await expect(secret).toBeVisible();
    await secret.getByRole('button', { name: 'Show' }).click();
    const value = await secret.locator('#secret-value').inputValue();
    expect(value).toMatch(/^qdr_bst_/);
    await secret.getByRole('button', { name: 'Copy' }).click();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(value);
    await expectAccessible(page, 'the new Bastion token');
    await secret.getByRole('button', { name: 'I have copied it' }).click();
    await expect(secret).toBeHidden();

    // after a reload the list never shows the value, and the token that expires in 20 days opens the section Tokens by itself with
    // the warning line and carries the mark Expires soon (Q-UI-1623)
    await page.reload();
    await expect(page.getByText(value)).toHaveCount(0);
    const again = page.getByRole('region', { name, exact: true });
    const tokens = again.locator('[data-testid="bastion-tokens"]');
    await expect(tokens.locator('button[aria-expanded]')).toHaveAttribute('aria-expanded', 'true');
    await expect(tokens.locator('button[aria-expanded]')).toContainText('“expires in 20 days” expires in');
    const row = tokens.getByRole('row', { name: /expires in 20 days|qdr_bst_/ }).last();
    await expect(row).toContainText('Expires soon');
    await expectAccessible(page, 'Bastions, a token with the warning mark');

    // disable it softly from its head and enable it again (R-BAS-133): no replica was heard, so the badge says so at once
    const state = again.locator('[data-testid="bastion-state"]');
    await again.getByRole('button', { name: `Disable… ${name}` }).click();
    const disable = page.locator('quader-bastion-disable-dialog');
    await expect(disable.getByRole('radio', { name: /^Soft/ })).toBeChecked();
    await expectAccessible(page, 'the dialog that disables a Bastion');
    await disable.getByRole('button', { name: 'Disable', exact: true }).click();
    await expect(state).toHaveText(/Disable not confirmed/);
    await again.getByRole('button', { name: `Enable ${name}` }).click();
    await expect(state).toHaveText(/No contact yet/);

    // invalidate the token: the section offers Issue token again
    await tokens.getByRole('button', { name: 'Invalidate expires in 20 days' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Invalidate', exact: true }).click();
    await expect(again.locator('[data-testid="bastion-tokens"]')).toContainText('No token yet.');
  } finally {
    await admin.delete(`/api/v1/bastions/${name}`);
  }
});
