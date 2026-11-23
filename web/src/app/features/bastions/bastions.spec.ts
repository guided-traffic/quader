import { TestBed } from '@angular/core/testing';
import { BastionsApi } from '../../api/bastions-api';
import { DownloadLinkConnection, DownloadLinksApi } from '../../api/download-links-api';
import { ApiError } from '../../api/problem';
import { FakeDownloadLinksApi, downloadLinkSettings, linkConnection } from '../../api/testing/fakes-download-links';
import {
  BASTION_SECRET,
  FakeBastionsApi,
  inDays,
  sampleBastion,
  sampleBastionToken,
} from '../../api/testing/fakes-bastions';
import { helpFor } from '../../core/help-topics';
import { Navigation, areaItems } from '../../core/navigation';
import { SessionStore } from '../../core/session-store';
import { seriousViolations } from '../../testing/axe';
import { DraftStore } from '../../shared/drafts';
import { browserStorage, buttonByText, renderScreen, settle, sleep, storedText, text, type, typeAndLeave, typeInto } from '../../testing/screen';
import { ADDRESS_MARKS, breakParts } from '../../shared/break-parts';
import { BASTION_TOKEN_WARN_DAYS, tokenExpiry } from './bastion-card';
import { handoverLines } from './bastion-handover';
import { BastionList } from './bastion-list';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;
const card = (root: HTMLElement, name: string): HTMLElement =>
  [...root.querySelectorAll<HTMLElement>('quader-bastion-card')].find((c) => c.querySelector('h2')?.textContent?.trim() === name)!;

describe('Bastions screen (R-BAS-007)', () => {
  it('R-BAS-007 lists the registrations with the last contact of every replica, and passes axe', async () => {
    const { root } = await renderScreen(BastionList);
    expect(root.querySelector('h1')?.textContent).toContain('Bastions');
    const partners = card(root, 'partners');
    expect(text(partners)).toContain('Enabled');
    expect(text(partners)).toContain('The partner network');
    const rows = partners.querySelectorAll('table')[0]!.querySelectorAll('tbody tr');
    expect(rows).toHaveLength(2);
    const first = text(rows[0]!);
    for (const part of ['quader-bastion-7d9c-x2k4q', '198.51.100.20', '0.15.0', 'https://packages.example.com']) {
      expect(first).toContain(part);
    }
    // Q-UI-959: three columns; the address, the release and the interface version stand under the replica's name, the time is
    // relative with its date and time as the title, and the URL keeps its text whole where it may break.
    expect([...partners.querySelectorAll('table')[0]!.querySelectorAll('thead th')].map((th) => text(th).replace(/help_outline/g, '').trim())).toEqual([
      'Replica',
      'Time',
      'Reported base URL',
    ]);
    const facts = rows[0]!.querySelector('th [data-testid="bastion-contact-facts"]')!;
    expect([...facts.children].map((c) => text(c))).toEqual(['198.51.100.20', 'Release 0.15.0', 'Interface version 1']);
    const time = rows[0]!.querySelector('td time')!;
    expect(text(time)).toMatch(/minutes? ago|now/);
    expect(time.getAttribute('title')).toMatch(/\d{4}/);
    const url = rows[0]!.querySelectorAll('td')[1]!;
    expect(text(url)).toBe('https://packages.example.com');
    expect(url.querySelectorAll('wbr').length).toBeGreaterThan(1);
    expect(text(card(root, 'standby'))).toContain('Disabled');
    expect(text(card(root, 'standby'))).toContain('No replica has contacted');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-BAS-007 marks a token that expires in less than 30 days and a token that has expired, and no other', async () => {
    const { root } = await renderScreen(BastionList, {
      setup: (get) => {
        (get(BastionsApi) as FakeBastionsApi).bastions = [
          sampleBastion({
            tokens: [
              sampleBastionToken({ id: 't-soon', description: 'twenty days', expiresAt: inDays(20) }),
              sampleBastionToken({ id: 't-far', description: 'far away', expiresAt: inDays(31) }),
              sampleBastionToken({ id: 't-gone', description: 'gone', expiresAt: inDays(-1) }),
            ],
          }),
        ];
      },
    });
    const rows = [...card(root, 'partners').querySelectorAll('table')[1]!.querySelectorAll('tbody tr')];
    // A row by its token's description, the first text of its row header ("gone" is also in "5 months ago" + "next month").
    const byName = (n: string) => text(rows.find((r) => text(r.querySelector('th')!).startsWith(n))!);
    expect(byName('twenty days')).toMatch(/Expires in\s+20 days/);
    expect(byName('far away')).not.toContain('Expires in');
    expect(byName('far away')).not.toContain('Expired');
    expect(byName('gone')).toContain('Expired');
    expect(root.querySelectorAll('.badge.warn')).toHaveLength(1);
    // Q-UI-959: the hint and the creation under the description; the expiry relative, its date and time as the title.
    expect(byName('far away')).toMatch(/qdr_bst_3kQ9\s*Created 5 months ago/);
    expect(byName('far away')).toContain('next month');
  });

  it('Q-UI-959 a URL and an address break only after a dot, a slash or a colon, and every character stays', () => {
    // The card's marks with the shared helper (break-parts.ts), which the card's PartsText shows piece by piece.
    const card = { breakParts: (value: string) => breakParts(value, ADDRESS_MARKS) };
    expect(card.breakParts('https://packages.frankfurt.example.com:8443/bastion/')).toEqual([
      'https://',
      'packages.',
      'frankfurt.',
      'example.',
      'com:',
      '8443/',
      'bastion/',
    ]);
    expect(card.breakParts('2001:0db8:85a3::8a2e:0370:7334')).toEqual(['2001:', '0db8:', '85a3::', '8a2e:', '0370:', '7334']);
    expect(card.breakParts('https://x').join('')).toBe('https://x');
  });

  it('R-BAS-007 the warning window is 30 days to the millisecond', () => {
    const now = Date.parse('2026-11-03T00:00:00Z');
    const at = (ms: number) => ({ expiresAt: new Date(now + ms).toISOString() });
    const day = 86_400_000;
    expect(BASTION_TOKEN_WARN_DAYS).toBe(30);
    expect(tokenExpiry(at(30 * day - 1), now)).toBe('soon');
    expect(tokenExpiry(at(30 * day), now)).toBe('ok');
    expect(tokenExpiry(at(0), now)).toBe('expired');
    expect(tokenExpiry(at(-day), now)).toBe('expired');
  });

  it('R-BAS-007 shows the last use of a token, or that it was not used yet', async () => {
    const { root } = await renderScreen(BastionList, {
      setup: (get) => {
        (get(BastionsApi) as FakeBastionsApi).bastions = [
          sampleBastion({ tokens: [sampleBastionToken({ lastUsedAt: null, description: 'the new one' })] }),
        ];
      },
    });
    expect(text(card(root, 'partners'))).toContain('Not used yet');
  });

  it('R-BAS-004 creates a registration with an idempotency key and lists it', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    buttonByText(root, 'New Bastion').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-bastion-create-dialog') as HTMLElement;
    typeAndLeave(dialog, 'Name', 'Partners');
    await settle(fixture);
    expect(text(dialog)).toContain('must start with a lower-case letter');
    typeInto(dialog, 'Name', 'newcomer');
    typeInto(dialog, 'Description', 'A new network');
    buttonByText(dialog, 'Create Bastion').click();
    await sleep(fixture, 100);
    const api = get(BastionsApi) as FakeBastionsApi;
    expect(api.calls).toContain('create newcomer');
    expect(api.keys).toHaveLength(1);
    expect(card(root, 'newcomer')).toBeTruthy();
  });

  it('R-BAS-004 a refused creation keeps the dialog and shows the error', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    buttonByText(root, 'New Bastion').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-bastion-create-dialog') as HTMLElement;
    typeInto(dialog, 'Name', 'partners');
    buttonByText(dialog, 'Create Bastion').click();
    await settle(fixture);
    expect((get(BastionsApi) as FakeBastionsApi).calls).toContain('create partners');
    expect(overlay().querySelector('quader-bastion-create-dialog')).toBeTruthy();
    expect(text(overlay())).toContain('conflict');
  });

  it('R-BAS-004 issues a token, shows it once and never stores it', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    const partners = card(root, 'partners');
    buttonByText(partners, 'Issue token').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-bastion-token-dialog') as HTMLElement;
    typeInto(dialog, 'Description', 'partners 2027');
    typeInto(dialog, 'Lifetime in days', '365');
    buttonByText(dialog, 'Issue token').click();
    await sleep(fixture, 100);
    const api = get(BastionsApi) as FakeBastionsApi;
    expect(api.calls).toContain('createToken partners partners 2027 31536000');
    expect(api.keys).toHaveLength(1);
    const secret = card(root, 'partners').querySelector('quader-token-secret') as HTMLElement;
    expect(secret).toBeTruthy();
    expect((secret.querySelector('input') as HTMLInputElement).value).toBe(BASTION_SECRET);
    expect(text(secret)).toContain('shown only once');
    // R-UI-043: the value is in the component only, and no other card shows it.
    expect(storedText()).not.toContain('qdr_');
    expect(card(root, 'standby').querySelector('quader-token-secret')).toBeNull();
    // The list was read again and shows the new token by its hint, not by its value.
    expect(text(card(root, 'partners'))).toContain('qdr_bst_Zz09');
    expect(await seriousViolations(root)).toEqual([]);
    buttonByText(secret, 'I have copied it').click();
    await settle(fixture);
    expect(root.querySelector('quader-token-secret')).toBeNull();
    expect(root.innerHTML).not.toContain(BASTION_SECRET);
  });

  it('R-BAS-004 an empty lifetime takes the default, and a lifetime that is no number is refused before the request', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    buttonByText(card(root, 'partners'), 'Issue token').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-bastion-token-dialog') as HTMLElement;
    typeInto(dialog, 'Lifetime in days', '0');
    buttonByText(dialog, 'Issue token').click();
    await settle(fixture);
    const api = get(BastionsApi) as FakeBastionsApi;
    expect(api.calls.filter((c) => c.startsWith('createToken'))).toEqual([]);
    expect(text(dialog)).toContain('greater than zero');
    typeInto(dialog, 'Lifetime in days', '');
    buttonByText(dialog, 'Issue token').click();
    await sleep(fixture, 100);
    expect(api.lastTokenRequest).toEqual({ description: '' });
  });

  it('R-BAS-004 a registration at its token limit answers token_limit_reached and keeps the dialog', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    (get(BastionsApi) as FakeBastionsApi).tokenLimit = 1;
    buttonByText(card(root, 'partners'), 'Issue token').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-bastion-token-dialog') as HTMLElement;
    buttonByText(dialog, 'Issue token').click();
    await settle(fixture);
    expect(overlay().querySelector('quader-bastion-token-dialog')).toBeTruthy();
    expect(root.querySelector('quader-token-secret')).toBeNull();
  });

  it('R-UI-042 a repeated issue whose first answer was lost revokes the lost token and issues again with a new key', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    const api = get(BastionsApi) as FakeBastionsApi;
    api.failure = new ApiError(409, { code: 'token_already_issued', tokenId: 't-lost' }, 'req-1');
    api.bastions = [sampleBastion({ tokens: [sampleBastionToken({ id: 't-lost', description: 'lost' })] })];
    buttonByText(card(root, 'partners'), 'Issue token').click();
    await settle(fixture);
    buttonByText(overlay().querySelector('quader-bastion-token-dialog') as HTMLElement, 'Issue token').click();
    await sleep(fixture, 100);
    expect(api.calls.filter((c) => c.startsWith('revokeToken'))).toEqual(['revokeToken partners t-lost']);
    expect(api.keys).toHaveLength(1);
    expect(root.querySelector('quader-token-secret')).toBeTruthy();
  });

  it('R-API-007 R-UI-042 issuing a token with a session whose sign-in is not recent keeps the non-secret entries as a draft, never a token', async () => {
    browserStorage.clear('session');
    const { fixture, root, get } = await renderScreen(BastionList);
    const api = get(BastionsApi) as FakeBastionsApi;
    api.failure = new ApiError(401, { code: 'reauthentication_required' }, 'req-1');
    buttonByText(card(root, 'partners'), 'Issue token').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-bastion-token-dialog') as HTMLElement;
    typeInto(dialog, 'Description', 'partners 2027');
    typeInto(dialog, 'Lifetime in days', '365');
    buttonByText(dialog, 'Issue token').click();
    await settle(fixture);
    const stored = browserStorage.entries('session').filter(([k]) => k.startsWith('quader.draft:'));
    expect(stored).toHaveLength(1);
    expect(JSON.parse(stored[0]![1])).toEqual({ bastion: 'partners', description: 'partners 2027', days: '365' });
    expect(storedText()).not.toContain('qdr_');
    expect(root.querySelector('quader-token-secret')).toBeNull();
    browserStorage.clear('session');
  });

  it('R-API-007 R-UI-042 after the sign-in the draft reopens the token dialog of its registration, once, and the token is issued', async () => {
    browserStorage.clear('session');
    const { fixture, root, get } = await renderScreen(BastionList, {
      setup: (get) => {
        get(DraftStore).keep({ bastion: 'partners', description: 'partners 2027', days: '365' });
      },
    });
    await settle(fixture);
    const dialogs = overlay().querySelectorAll('quader-bastion-token-dialog');
    expect(dialogs).toHaveLength(1);
    const dialog = dialogs[0] as HTMLElement;
    expect(text(dialog)).toContain('partners');
    expect((dialog.querySelector('input[name="description"]') as HTMLInputElement).value).toBe('partners 2027');
    expect((dialog.querySelector('input[name="days"]') as HTMLInputElement).value).toBe('365');
    expect(browserStorage.entries('session').filter(([k]) => k.startsWith('quader.draft:'))).toHaveLength(0);
    buttonByText(dialog, 'Issue token').click();
    await sleep(fixture, 100);
    expect((get(BastionsApi) as FakeBastionsApi).calls).toContain('createToken partners partners 2027 31536000');
    expect(card(root, 'partners').querySelector('quader-token-secret')).toBeTruthy();
    expect(overlay().querySelectorAll('quader-bastion-token-dialog')).toHaveLength(0);
  });

  it('R-BAS-004 disables a registration after a confirmation, with the ETag it read, and enables it again', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    const api = get(BastionsApi) as FakeBastionsApi;
    buttonByText(card(root, 'partners'), 'Disable').click();
    await settle(fixture);
    buttonByText(overlay().querySelector('quader-confirm-dialog') as HTMLElement, 'Disable').click();
    await sleep(fixture, 100);
    expect(api.calls).toContain('setEnabled partners false');
    expect(api.ifMatch).toEqual(['"1"']);
    expect(text(card(root, 'partners'))).toContain('Disabled');
    buttonByText(card(root, 'partners'), 'Enable').click();
    await sleep(fixture, 100);
    expect(api.calls).toContain('setEnabled partners true');
    expect(api.ifMatch).toEqual(['"1"', '"2"']);
    expect(text(card(root, 'partners'))).toContain('Enabled');
  });

  it('R-BAS-004 a change made by someone else in between is a 412 and is shown', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    const api = get(BastionsApi) as FakeBastionsApi;
    const read = api.get.bind(api);
    api.get = async (name) => {
      const answer = await read(name);
      api.bumpRevision(name);
      return answer;
    };
    buttonByText(card(root, 'standby'), 'Enable').click();
    await sleep(fixture, 100);
    expect(text(card(root, 'standby'))).toContain('Disabled');
    expect(text(card(root, 'standby'))).toContain('Changed by someone else');
  });

  it('R-BAS-004 deleting asks for the name and removes the registration', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    buttonByText(card(root, 'partners'), 'Delete').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-confirm-dialog') as HTMLElement;
    expect((buttonByText(dialog, 'Delete') as HTMLButtonElement).disabled).toBe(true);
    type(dialog.querySelector<HTMLInputElement>('#confirm-name')!, 'partners');
    await settle(fixture);
    buttonByText(dialog, 'Delete').click();
    await sleep(fixture, 100);
    expect((get(BastionsApi) as FakeBastionsApi).calls).toContain('delete partners');
    expect(card(root, 'partners')).toBeUndefined();
    expect(card(root, 'standby')).toBeTruthy();
  });

  it('R-BAS-006 revokes a token after a confirmation that names the renewal order', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    buttonByText(card(root, 'partners'), 'Revoke').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-confirm-dialog') as HTMLElement;
    expect(text(dialog)).toContain('Last used');
    buttonByText(dialog, 'Revoke').click();
    await sleep(fixture, 100);
    expect((get(BastionsApi) as FakeBastionsApi).calls).toContain('revokeToken partners 0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e41');
    expect(text(card(root, 'partners'))).toContain('This registration has no token');
  });

  it('R-BAS-004 endpoints:read alone shows the list and offers no action', async () => {
    const { root } = await renderScreen(BastionList, { capabilities: ['endpoints:read'] });
    expect(card(root, 'partners')).toBeTruthy();
    expect([...root.querySelectorAll('button')].map((b) => text(b)).filter((t) => !/^New Bastion|help_outline/.test(t))).toEqual([]);
    const create = buttonByText(root, 'New Bastion') as HTMLButtonElement;
    expect(create.disabled).toBe(true);
    expect(text(root)).toContain('endpoints:write');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-BAS-004 tokens:revoke alone may revoke a token and nothing else', async () => {
    const { root } = await renderScreen(BastionList, { capabilities: ['endpoints:read', 'tokens:revoke'] });
    const partners = card(root, 'partners');
    expect(buttonByText(partners, 'Revoke')).toBeTruthy();
    expect([...partners.querySelectorAll('button')].map((b) => text(b)).filter((t) => !/help_outline/.test(t))).toEqual(['Revoke partners 2026']);
  });

  it('R-BAS-007 shows the empty state, the loading failure with a retry, and recovers', async () => {
    const { fixture, root, get } = await renderScreen(BastionList, {
      setup: (get) => {
        (get(BastionsApi) as FakeBastionsApi).bastions = [];
      },
    });
    expect(text(root)).toContain('There is no Bastion yet.');
    // The action that fills the list stands once, in the header (Q-UI-565).
    expect([...root.querySelectorAll('button')].filter((b) => b.textContent?.trim() === 'New Bastion')).toHaveLength(1);
    expect(root.querySelector('quader-empty-state button')).toBeNull();
    const api = get(BastionsApi) as FakeBastionsApi;
    api.listFailure = new ApiError(503, { code: 'unavailable' }, 'req-1');
    api.bastions = [sampleBastion()];
    const again = await renderScreen(BastionList, {
      setup: (g) => {
        (g(BastionsApi) as FakeBastionsApi).listFailure = new ApiError(503, { code: 'unavailable' }, 'req-1');
      },
    });
    expect(again.root.querySelector('quader-error-notice')).toBeTruthy();
    expect(again.root.querySelector('quader-bastion-card')).toBeNull();
    (again.get(BastionsApi) as FakeBastionsApi).listFailure = null;
    buttonByText(again.root, 'Retry').click();
    await settle(again.fixture);
    expect(again.root.querySelector('quader-bastion-card')).toBeTruthy();
    void fixture;
  });
});

describe('Direct reads from the object store on a Bastion card (R-BAS-071)', () => {
  const toggle = (root: HTMLElement, name: string): HTMLButtonElement | null =>
    card(root, name).querySelector<HTMLButtonElement>('button[role="switch"]');
  const reasons = (root: HTMLElement, name: string): Element | null =>
    card(root, name).querySelector('[data-testid="bastion-handover-reasons"]');
  /** The lines of the reasons without the glyph names of their icons. */
  const lines = (el: Element): string[] =>
    [...el.querySelectorAll('p.reason')].map((p) => {
      const copy = p.cloneNode(true) as Element;
      copy.querySelectorAll('.material-icons').forEach((i) => i.remove());
      return text(copy);
    });
  /** One connection a Bastion can read, one whose address is plain http, and an encrypted one. */
  const mixed = (links: FakeDownloadLinksApi): void => {
    links.current = downloadLinkSettings({
      connections: [
        linkConnection(),
        linkConnection({
          name: 'eu-backup',
          endpoint: 'http://seaweedfs.quader.svc:8333',
          usable: false,
          reasons: [{ code: 'not_https', detail: null }],
          handoverUsable: false,
          handoverReasons: [{ code: 'not_https', detail: null }],
        }),
        linkConnection({ name: 'archive', encrypted: true, usable: false, reasons: [{ code: 'encrypted', detail: null }] }),
      ],
    });
  };

  /** Counts the reads of the download link settings. */
  const counted = (links: FakeDownloadLinksApi): { reads: number } => {
    const count = { reads: 0 };
    const read = links.settings.bind(links);
    links.settings = async () => {
      count.reads++;
      return read();
    };
    return count;
  };

  it('R-BAS-071 a new registration shows the switch off with its one sentence, and nothing of the connections', async () => {
    let count = { reads: 0 };
    const { root } = await renderScreen(BastionList, { setup: (g) => void (count = counted(g(DownloadLinksApi) as FakeDownloadLinksApi)) });
    const sw = toggle(root, 'partners')!;
    expect(sw.getAttribute('aria-checked')).toBe('false');
    expect(sw.getAttribute('aria-label')).toBe('Direct reads from the object store for partners');
    expect(text(card(root, 'partners'))).toContain(
      "Large files go from the object store to this Bastion by a link per file, and for an encrypted store with that file's key; the main installation decides and counts every download as before.",
    );
    expect(reasons(root, 'partners')).toBeNull();
    // Nothing is on, so the connections' states are not read.
    expect(count.reads).toBe(0);
  });

  it('R-BAS-071 switching on sends the merge patch with the ETag it read; once on, a connection a Bastion cannot read names its reason and the link to the storage settings', async () => {
    const { fixture, root, get } = await renderScreen(BastionList, {
      setup: (g) => mixed(g(DownloadLinksApi) as FakeDownloadLinksApi),
    });
    const api = get(BastionsApi) as FakeBastionsApi;
    toggle(root, 'partners')!.click();
    await sleep(fixture, 100);
    expect(api.calls).toContain('setHandover partners true');
    expect(api.ifMatch).toEqual(['"1"']);
    expect(toggle(root, 'partners')!.getAttribute('aria-checked')).toBe('true');
    const why = reasons(root, 'partners')!;
    // archive is encrypted and its address and link check let a Bastion read it with each file's key: no line (Q-BAS-300).
    expect(lines(why)).toEqual(['A Bastion gets the files of eu-backup through the main installation: its address does not use https.']);
    const link = why.querySelector<HTMLAnchorElement>('a')!;
    expect(text(link)).toBe('Public addresses in the storage settings');
    expect(link.getAttribute('href')).toBe('/admin/settings/storage#details');
    // The other card is off and says nothing of the connections.
    expect(reasons(root, 'standby')).toBeNull();
    expect(await seriousViolations(root)).toEqual([]);
    // Off again, and the reasons go.
    toggle(root, 'partners')!.click();
    await sleep(fixture, 100);
    expect(api.calls).toContain('setHandover partners false');
    expect(reasons(root, 'partners')).toBeNull();
  });

  it('R-BAS-071 on while every connection can be read: the switch and its sentence alone', async () => {
    const { root } = await renderScreen(BastionList, {
      setup: (g) => {
        (g(BastionsApi) as FakeBastionsApi).bastions = [sampleBastion({ handover: true })];
      },
    });
    expect(toggle(root, 'partners')!.getAttribute('aria-checked')).toBe('true');
    expect(reasons(root, 'partners')).toBeNull();
  });

  it('R-BAS-071 Q-BAS-300 an encrypted connection is judged by its handover reasons like a plain one: one a Bastion can read shows nothing, one it cannot names its reason and the link', async () => {
    const encrypted = (name: string, over: Partial<DownloadLinkConnection> = {}): DownloadLinkConnection =>
      linkConnection({ name, encrypted: true, usable: false, reasons: [{ code: 'encrypted', detail: null }], ...over });
    const { root } = await renderScreen(BastionList, {
      setup: (g) => {
        (g(BastionsApi) as FakeBastionsApi).bastions = [sampleBastion({ handover: true })];
        (g(DownloadLinksApi) as FakeDownloadLinksApi).current = downloadLinkSettings({
          connections: [
            linkConnection(),
            encrypted('archive'),
            encrypted('vault', {
              handoverUsable: false,
              handoverReasons: [{ code: 'link_check_failed', detail: 'the link answered 403 with 0 bytes', check: { outcome: 'unexpected_answer', status: 403, bytes: 0, expectedStatus: 200, expectedBytes: 2048 } }],
            }),
          ],
        });
      },
    });
    const why = reasons(root, 'partners')!;
    expect(lines(why)).toEqual([
      'A Bastion gets the files of vault through the main installation: its object store did not answer a test link as expected (the test link answered with status 403 and 0 bytes instead of status 200 and 2,048 bytes).',
    ]);
    expect(why.querySelector<HTMLAnchorElement>('a')!.getAttribute('href')).toBe('/admin/settings/storage#details');
  });

  it('R-BAS-071 each reason of R-STOR-096 has its clause, and the reasons of one connection share its line', () => {
    expect(
      handoverLines([
        linkConnection({
          name: 'default',
          handoverUsable: false,
          handoverReasons: [
            { code: 'quader_host', detail: 'quader.example.com' },
            { code: 'link_check_failed', detail: 'the link answered 403 with 0 bytes', check: { outcome: 'override_ignored', header: 'Content-Disposition', value: '', expectedValue: 'attachment' } },
          ],
        }),
      ]).map((l) => l.text),
    ).toEqual([
      'A Bastion gets the files of default through the main installation: its address lies under quader.example.com, an address of Quader; its object store did not answer a test link as expected (its answer carries no Content-Disposition instead of “attachment”).',
    ]);
    expect(handoverLines([linkConnection(), linkConnection({ name: 'b', handoverUsable: true })])).toEqual([]);
  });

  it('R-BAS-071 a change made in between is a 412 that stands under the sentence, and the switch keeps the state the server has', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    const api = get(BastionsApi) as FakeBastionsApi;
    const read = api.get.bind(api);
    api.get = async (name) => {
      const answer = await read(name);
      api.bumpRevision(name);
      return answer;
    };
    toggle(root, 'partners')!.click();
    await sleep(fixture, 100);
    const block = card(root, 'partners').querySelector('quader-bastion-handover')!;
    expect(text(block)).toContain('Changed by someone else');
    expect(toggle(root, 'partners')!.getAttribute('aria-checked')).toBe('false');
    expect(toggle(root, 'partners')!.getAttribute('aria-busy')).toBeNull();
  });

  it('R-BAS-071 a refusal of the server stands under the sentence', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    (get(BastionsApi) as FakeBastionsApi).failure = new ApiError(403, { code: 'forbidden', detail: 'The caller lacks endpoints:write.' }, 'req-9');
    toggle(root, 'partners')!.click();
    await sleep(fixture, 100);
    const block = card(root, 'partners').querySelector('quader-bastion-handover')!;
    expect(block.querySelector('quader-error-notice')).toBeTruthy();
    expect(toggle(root, 'partners')!.getAttribute('aria-checked')).toBe('false');
  });

  it('R-BAS-071 Q-UI-960 who may only read sees the state as the settings pages say it, and the reasons, and is offered no switch', async () => {
    const { root } = await renderScreen(BastionList, {
      capabilities: ['endpoints:read', 'settings:read'],
      setup: (g) => {
        (g(BastionsApi) as FakeBastionsApi).bastions = [sampleBastion({ handover: true }), sampleBastion({ id: 'b-2', name: 'standby', handover: false })];
        mixed(g(DownloadLinksApi) as FakeDownloadLinksApi);
      },
    });
    expect(toggle(root, 'partners')).toBeNull();
    expect(text(card(root, 'partners').querySelector('[data-testid="bastion-handover-state"]')!)).toBe('Direct reads from the object store are on.');
    expect(card(root, 'partners').querySelector('[data-testid="bastion-handover-state"] strong')).toBeTruthy();
    expect(text(card(root, 'standby').querySelector('[data-testid="bastion-handover-state"]')!)).toBe('Direct reads from the object store are off.');
    // eu-backup's address; the encrypted archive can be read with each file's key and has no line (Q-BAS-300).
    expect(lines(reasons(root, 'partners')!)).toHaveLength(1);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-BAS-071 without settings:read the connections are not read and the card shows the state alone', async () => {
    let count = { reads: 0 };
    const { root } = await renderScreen(BastionList, {
      capabilities: ['endpoints:read', 'endpoints:write'],
      setup: (g) => {
        (g(BastionsApi) as FakeBastionsApi).bastions = [sampleBastion({ handover: true })];
        mixed(g(DownloadLinksApi) as FakeDownloadLinksApi);
        count = counted(g(DownloadLinksApi) as FakeDownloadLinksApi);
      },
    });
    expect(toggle(root, 'partners')!.getAttribute('aria-checked')).toBe('true');
    expect(reasons(root, 'partners')).toBeNull();
    expect(count.reads).toBe(0);
  });

  it('R-BAS-071 connections that cannot be read leave the switch and its sentence alone', async () => {
    const { root } = await renderScreen(BastionList, {
      setup: (g) => {
        (g(BastionsApi) as FakeBastionsApi).bastions = [sampleBastion({ handover: true })];
        (g(DownloadLinksApi) as FakeDownloadLinksApi).settings = () => Promise.reject(new ApiError(503, { code: 'unavailable' }, 'req-2'));
      },
    });
    expect(toggle(root, 'partners')!.getAttribute('aria-checked')).toBe('true');
    expect(reasons(root, 'partners')).toBeNull();
    expect(root.querySelector('quader-error-notice')).toBeNull();
  });
});

describe('Bastions in the navigation and the help (R-BAS-007, R-UI-050)', () => {
  it('R-BAS-007 the entry needs endpoints:read and the "?" opens the Bastion page of the manual', async () => {
    const { get } = await renderScreen(BastionList, { capabilities: ['endpoints:read'] });
    const routes = (): string[] =>
      get(Navigation)
        .areas()
        .flatMap((a) => areaItems(a))
        .map((i) => i.route);
    expect(routes()).toContain('/admin/bastions');
    expect(helpFor('/admin/bastions')).toEqual(['/help', 'admin', 'bastion']);
    TestBed.inject(SessionStore);
    const none = await renderScreen(BastionList, { capabilities: ['repositories:read-config'] });
    expect(
      none
        .get(Navigation)
        .areas()
        .flatMap((a) => areaItems(a))
        .some((i) => i.route === '/admin/bastions'),
    ).toBe(false);
  });
});
