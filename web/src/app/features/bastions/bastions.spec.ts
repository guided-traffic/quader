import { TestBed } from '@angular/core/testing';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { LimitsApi } from '../../api/limits-api';
import { FakeLimitsApi, sampleBastionProfiles, sampleProfiles } from '../../api/testing/fakes-limits';
import { DownloadLinkConnection, DownloadLinksApi } from '../../api/download-links-api';
import { ApiError } from '../../api/problem';
import { FakeDownloadLinksApi, downloadLinkSettings, linkConnection } from '../../api/testing/fakes-download-links';
import {
  BASTION_SECRET,
  FakeBastionsApi,
  handoverSwitches,
  inDays,
  sampleBastion,
  sampleBastionContact,
  sampleBastionToken,
} from '../../api/testing/fakes-bastions';
import { helpFor } from '../../core/help-topics';
import { Navigation, areaItems } from '../../core/navigation';
import { SessionStore } from '../../core/session-store';
import { seriousViolations } from '../../testing/axe';
import { DraftStore } from '../../shared/drafts';
import { browserStorage, button, buttonByText, renderScreen, settle, sleep, storedText, text, type, typeAndLeave, typeInto } from '../../testing/screen';
import { ADDRESS_MARKS, breakParts } from '../../shared/break-parts';
import { BASTION_TOKEN_WARN_DAYS, tokenExpiry } from './bastion-card';
import { handoverLines } from './bastion-handover';
import { inNetworks, unlistedContacts } from './egress';
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

  it('R-BAS-007 a URL and an address break only after a dot, a slash or a colon, and every character stays', () => {
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
    // The fold of a card's Details is no action (R-UI-240).
    expect([...root.querySelectorAll('button')].map((b) => text(b)).filter((t) => !/^New Bastion|help_outline|Details$/.test(t))).toEqual([]);
    const create = buttonByText(root, 'New Bastion') as HTMLButtonElement;
    expect(create.disabled).toBe(true);
    expect(text(root)).toContain('endpoints:write');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-BAS-004 tokens:revoke alone may revoke a token and nothing else', async () => {
    const { root } = await renderScreen(BastionList, { capabilities: ['endpoints:read', 'tokens:revoke'] });
    const partners = card(root, 'partners');
    expect(buttonByText(partners, 'Revoke')).toBeTruthy();
    expect([...partners.querySelectorAll('button')].map((b) => text(b)).filter((t) => !/help_outline|Details$/.test(t))).toEqual(['Revoke partners 2026']);
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
        (g(BastionsApi) as FakeBastionsApi).bastions = [sampleBastion({ switches: handoverSwitches(true) })];
      },
    });
    expect(toggle(root, 'partners')!.getAttribute('aria-checked')).toBe('true');
    expect(reasons(root, 'partners')).toBeNull();
  });

  it('R-BAS-071 an encrypted connection is judged by its handover reasons like a plain one: one a Bastion can read shows nothing, one it cannot names its reason and the link', async () => {
    const encrypted = (name: string, over: Partial<DownloadLinkConnection> = {}): DownloadLinkConnection =>
      linkConnection({ name, encrypted: true, usable: false, reasons: [{ code: 'encrypted', detail: null }], ...over });
    const { root } = await renderScreen(BastionList, {
      setup: (g) => {
        (g(BastionsApi) as FakeBastionsApi).bastions = [sampleBastion({ switches: handoverSwitches(true) })];
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

  it('R-BAS-071 who may only read sees the state as the settings pages say it, and the reasons, and is offered no switch', async () => {
    const { root } = await renderScreen(BastionList, {
      capabilities: ['endpoints:read', 'settings:read'],
      setup: (g) => {
        (g(BastionsApi) as FakeBastionsApi).bastions = [sampleBastion({ switches: handoverSwitches(true) }), sampleBastion({ id: 'b-2', name: 'standby', switches: handoverSwitches(false) })];
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
        (g(BastionsApi) as FakeBastionsApi).bastions = [sampleBastion({ switches: handoverSwitches(true) })];
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
        (g(BastionsApi) as FakeBastionsApi).bastions = [sampleBastion({ switches: handoverSwitches(true) })];
        (g(DownloadLinksApi) as FakeDownloadLinksApi).settings = () => Promise.reject(new ApiError(503, { code: 'unavailable' }, 'req-2'));
      },
    });
    expect(toggle(root, 'partners')!.getAttribute('aria-checked')).toBe('true');
    expect(reasons(root, 'partners')).toBeNull();
    expect(root.querySelector('quader-error-notice')).toBeNull();
  });
});

describe('The addresses a Bastion connects from (R-UI-243)', () => {
  // The list stands in the card's Details, the warnings in plain view on the card (Q-UI-1483, R-UI-240).
  const section = (root: HTMLElement, name: string): HTMLElement => card(root, name).querySelector<HTMLElement>('quader-bastion-egress[part="list"]')!;
  const warnings = (root: HTMLElement, name: string): HTMLElement =>
    card(root, name).querySelector<HTMLElement>('quader-bastion-egress[part="warnings"]')!;
  const lineText = (el: Element): string => {
    const copy = el.cloneNode(true) as Element;
    copy.querySelectorAll('.material-icons, button').forEach((i) => i.remove());
    return text(copy);
  };
  const fields = (el: HTMLElement): HTMLInputElement[] => [...el.querySelectorAll<HTMLInputElement>('input')];
  /** A registration with no network, whose two replicas made contact from one address and an IPv6 one. */
  const unconfigured = () =>
    sampleBastion({
      egress: [],
      contacts: [
        sampleBastionContact({ clientAddress: '198.51.100.21' }),
        sampleBastionContact({ replica: 'quader-bastion-7d9c-m8v2j', clientAddress: '2001:db8:4::7', contactAt: inDays(-0.02) }),
        sampleBastionContact({ replica: 'quader-bastion-7d9c-q1w2e', clientAddress: '198.51.100.21', contactAt: inDays(-0.03) }),
      ],
    });

  it('R-UI-243 an empty list warns, each address a replica came from is offered once, and Add sends one PATCH with the list and that address', async () => {
    const { fixture, root, get } = await renderScreen(BastionList, {
      setup: (g) => void ((g(BastionsApi) as FakeBastionsApi).bastions = [unconfigured()]),
    });
    const api = get(BastionsApi) as FakeBastionsApi;
    const egress = section(root, 'partners');
    const warn = warnings(root, 'partners');
    expect(text(egress.querySelector('h4')!)).toBe('Addresses this Bastion connects from');
    expect(egress.closest('quader-disclosure')).not.toBeNull();
    expect(warn.closest('quader-disclosure')).toBeNull();
    expect(text(egress)).toContain("The main installation believes the caller's address a Bastion forwards only from these addresses.");
    expect(lineText(warn.querySelector('[data-testid="bastion-egress-empty"]')!)).toBe(
      "No address is listed: every caller through this Bastion counts as the Bastion's own address at the main installation, with one budget and one block for all, and the audit log names the Bastion's address instead of the caller's.",
    );
    const offers = [...warn.querySelectorAll('[data-testid="bastion-egress-unlisted"]')];
    expect(offers.map(lineText)).toEqual([
      'The last contact came from 198.51.100.21, which is not listed.',
      'The last contact came from 2001:db8:4::7, which is not listed.',
    ]);
    // The default of a new registration, the empty list, is one empty field, as wide as a host.
    expect(fields(egress).map((f) => f.value)).toEqual(['']);
    expect(fields(egress)[0]!.classList).toContain('field-w-name');
    expect(fields(egress)[0]!.getAttribute('aria-label')).toBe('Address or network 1 of partners');
    expect(button(egress, 'Save changes').disabled).toBe(true);
    expect(await seriousViolations(root)).toEqual([]);

    buttonByText(offers[0]!, 'Add 198.51.100.21').click();
    await sleep(fixture, 100);
    expect(api.calls).toEqual(['setEgress partners 198.51.100.21']);
    expect(api.ifMatch).toEqual(['"1"']);
    expect(warn.querySelector('[data-testid="bastion-egress-empty"]')).toBeNull();
    expect([...warn.querySelectorAll('[data-testid="bastion-egress-unlisted"]')].map(lineText)).toEqual([
      'The last contact came from 2001:db8:4::7, which is not listed.',
    ]);
    expect(fields(egress).map((f) => f.value)).toEqual(['198.51.100.21/32']);
  });

  it('R-UI-243 the list is edited field by field and saved with the ETag it read; a refusal stands under its field and the entries stay', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    const api = get(BastionsApi) as FakeBastionsApi;
    const egress = section(root, 'partners');
    expect(fields(egress).map((f) => f.value)).toEqual(['198.51.100.16/28']);
    expect(warnings(root, 'partners').querySelector('[data-testid="bastion-egress-empty"]')).toBeNull();
    expect(warnings(root, 'partners').querySelector('[data-testid="bastion-egress-unlisted"]')).toBeNull();
    button(egress, 'Add address').click();
    await settle(fixture);
    type(fields(egress)[1]!, '0.0.0.0/0');
    await settle(fixture);
    button(egress, 'Save changes').click();
    await sleep(fixture, 100);
    expect(api.calls).toEqual(['setEgress partners 198.51.100.16/28 0.0.0.0/0']);
    expect(text(egress.querySelector('.field-error')!)).toContain('0.0.0.0/0 covers every address');
    expect(fields(egress)[1]!.getAttribute('aria-invalid')).toBe('true');
    expect(fields(egress).map((f) => f.value)).toEqual(['198.51.100.16/28', '0.0.0.0/0']);
    expect(egress.querySelector('quader-error-notice')).not.toBeNull();

    type(fields(egress)[1]!, '2001:db8:4::/64');
    await settle(fixture);
    button(egress, 'Save changes').click();
    await sleep(fixture, 100);
    expect(api.calls.at(-1)).toBe('setEgress partners 198.51.100.16/28 2001:db8:4::/64');
    expect(egress.querySelector('.field-error')).toBeNull();
    // Removing every network leaves one empty field, and the empty list is saved as [].
    for (const b of [...egress.querySelectorAll<HTMLButtonElement>('.row button')]) b.click();
    await settle(fixture);
    expect(fields(egress).map((f) => f.value)).toEqual(['']);
    button(egress, 'Save changes').click();
    await sleep(fixture, 100);
    expect(api.calls.at(-1)).toBe('setEgress partners ');
    expect(warnings(root, 'partners').querySelector('[data-testid="bastion-egress-empty"]')).not.toBeNull();
  });

  it('R-UI-243 who may only read sees the networks as text and the warnings, and is offered no field and no Add', async () => {
    const { root } = await renderScreen(BastionList, {
      capabilities: ['endpoints:read'],
      setup: (g) =>
        void ((g(BastionsApi) as FakeBastionsApi).bastions = [
          sampleBastion({ egress: ['198.51.100.16/28', '2001:db8:4::/64'], contacts: [sampleBastionContact({ clientAddress: '192.0.2.9' })] }),
          { ...unconfigured(), id: 'b-2', name: 'standby' },
        ]),
    });
    const partners = section(root, 'partners');
    expect(text(partners.querySelector('[data-testid="bastion-egress-list"]')!)).toBe('198.51.100.16/28, 2001:db8:4::/64');
    expect(fields(partners)).toHaveLength(0);
    const partnersWarn = warnings(root, 'partners');
    expect(lineText(partnersWarn.querySelector('[data-testid="bastion-egress-unlisted"]')!)).toBe('The last contact came from 192.0.2.9, which is not listed.');
    expect(partnersWarn.querySelector('[data-testid="bastion-egress-unlisted"] button')).toBeNull();
    expect(warnings(root, 'standby').querySelector('[data-testid="bastion-egress-empty"]')).not.toBeNull();
    expect(text(section(root, 'standby').querySelector('[data-testid="bastion-egress-list"]')!)).toBe('No address is listed.');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-243 an address lies in a network of its own family, an IPv4 address in IPv6 form as the IPv4 one', () => {
    expect(inNetworks('198.51.100.20', ['198.51.100.16/28'])).toBe(true);
    expect(inNetworks('198.51.100.32', ['198.51.100.16/28'])).toBe(false);
    expect(inNetworks('::ffff:198.51.100.20', ['198.51.100.20/32'])).toBe(true);
    expect(inNetworks('2001:db8:4::7', ['2001:db8:4::/64'])).toBe(true);
    expect(inNetworks('2001:db8:5::7', ['2001:db8:4::/64'])).toBe(false);
    expect(inNetworks('2001:0db8:0004:0000:0000:0000:0000:0007', ['2001:db8:4::7/128'])).toBe(true);
    expect(inNetworks('198.51.100.20', ['::/0'])).toBe(false);
    expect(inNetworks('not an address', ['0.0.0.0/1'])).toBe(false);
    expect(
      unlistedContacts({
        egress: ['10.0.0.0/8'],
        contacts: [sampleBastionContact({ clientAddress: '10.1.2.3' }), sampleBastionContact({ clientAddress: null }), sampleBastionContact({ clientAddress: '192.0.2.1' })],
      }),
    ).toEqual(['192.0.2.1']);
  });
});

describe('A registration the configuration declares (R-BAS-127)', () => {
  const declared = () =>
    sampleBastion({
      declared: { since: inDays(-2), secretName: 'quader-bastion-token' },
      egress: ['10.252.0.0/16'],
      tokens: [
        sampleBastionToken({ id: 't-declared', description: 'Bastion partners (declared in the configuration)', expiresAt: null, declared: true }),
        sampleBastionToken({ id: 't-issued', description: 'partners 2026' }),
      ],
    });

  it('R-BAS-127 the card names the Secret, keeps the declared members and the declared token, and offers no Delete', async () => {
    const { root } = await renderScreen(BastionList, {
      setup: (g) => void ((g(BastionsApi) as FakeBastionsApi).bastions = [declared()]),
    });
    const partners = card(root, 'partners');
    expect(text(partners.querySelector('[data-testid="bastion-declared"]')!).replace('lock', '').trim()).toBe(
      'From the Helm values, token from Secret quader-bastion-token. Its description, addresses and that token are changed or removed there; to delete it, remove it there first.',
    );
    const buttons = [...partners.querySelectorAll('button')].map((b) => text(b)).filter((t) => !/help_outline/.test(t));
    expect(buttons.some((b) => b.startsWith('Delete'))).toBe(false);
    expect(buttons.some((b) => b.startsWith('Issue token'))).toBe(true);
    expect(buttons.filter((b) => b.startsWith('Revoke'))).toEqual(['Revoke partners 2026']);
    const rows = partners.querySelectorAll('table')[1]!.querySelectorAll('tbody tr');
    expect(text(rows[0]!)).toContain('Never');
    expect(text(rows[0]!.querySelector('[data-testid="bastion-token-declared"]')!)).toContain('In the configuration');
    // The networks are the configuration's: text, no field, no Save.
    expect(partners.querySelectorAll('quader-bastion-egress input')).toHaveLength(0);
    expect(text(partners.querySelector('[data-testid="bastion-egress-list"]')!)).toBe('10.252.0.0/16');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it("R-BAS-127 without a Secret's name the card says the configuration file, and the declared token never expires", async () => {
    const { root } = await renderScreen(BastionList, {
      setup: (g) => void ((g(BastionsApi) as FakeBastionsApi).bastions = [{ ...declared(), declared: { since: inDays(-2), secretName: '' } }]),
    });
    expect(text(card(root, 'partners').querySelector('[data-testid="bastion-declared"]')!)).toContain("From the installation's configuration file.");
    expect(tokenExpiry({ expiresAt: null }, Date.now())).toBe('ok');
  });
});

describe("A Bastion's limits, throughput, switches and state on its card (R-UI-240)", () => {
  /** An administrator of the registrations and of the download limits. */
  const ADMIN = ['endpoints:read', 'endpoints:write', 'limits:read', 'limits:write'];
  const limitsOf = (root: HTMLElement, name: string): HTMLElement => card(root, name).querySelector<HTMLElement>('quader-bastion-limits')!;
  const details = (root: HTMLElement, name: string): HTMLElement => limitsOf(root, name).querySelector<HTMLElement>('quader-disclosure')!;
  const presetButtons = (el: HTMLElement): HTMLButtonElement[] => [...el.querySelectorAll<HTMLButtonElement>('.presets button')];
  const fieldNamed = (el: ParentNode, name: string): HTMLInputElement => el.querySelector<HTMLInputElement>(`input[name="${name}"]`)!;
  /** What a replica reported a minute ago (R-LIM-045): half of the state line's figures, a block, two callers limited. */
  const report = (replica: string, over: Partial<Bastion['status'][number]> = {}): Bastion['status'][number] => ({
    replica,
    reportedAt: inDays(-1 / 1440),
    cache: { shared: true, bytes: 7e9, capacityBytes: 10e9, hits: 970, misses: 30, evictions: 4, protectedBytes: 4.5e9 },
    throughput: { limited: false, bytesPerSecondByClass: { anonymous: 10e6, signedIn: 10.5e6, catalogue: 0 } },
    limited: 2,
    blocked: 1,
    blocks: [{ address: '203.0.113.9/32', until: inDays(0.01), reason: 'refusals' }],
    ...over,
  });
  const withProfiles = (g: <A>(token: abstract new (...args: never[]) => A) => A): void => {
    (g(LimitsApi) as FakeLimitsApi).profileList = [...sampleProfiles(), ...sampleBastionProfiles()];
  };

  it('R-UI-240 a new registration shows Internet, Enforce and 200 MB/s, the details closed, and no report; passes axe', async () => {
    const { root } = await renderScreen(BastionList, { capabilities: ADMIN });
    const limits = limitsOf(root, 'partners');
    expect(text(limits.querySelector('h3')!)).toBe('Limits');
    expect(presetButtons(limits).map((b) => [text(b), b.getAttribute('aria-pressed')])).toEqual([
      ['Internet', 'true'],
      ['Partner network', 'false'],
      ['Internal', 'false'],
    ]);
    expect(text(limits)).toContain('Strict limits for callers from the internet; administrators too.');
    expect(limits.querySelector('[data-testid="bastion-preset-custom"]')).toBeNull();
    expect(text(limits.querySelector('[data-testid="bastion-mode"]')!)).toBe('Mode: Enforce');
    // A default is a value in the field (Q-BAS-455), as wide as five digits.
    const throughput = fieldNamed(limits, 'throughput');
    expect(throughput.value).toBe('200');
    expect(throughput.classList).toContain('field-w-number');
    expect(text(limits)).toContain("Set this to the Bastion's share of the link between the two networks.");
    expect(details(root, 'partners').querySelector('button[aria-expanded]')!.getAttribute('aria-expanded')).toBe('false');
    expect(text(limits.querySelector('[data-testid="bastion-no-report"]')!)).toBe('No report in the last 5 minutes');
    expect(limits.querySelector('[data-testid="bastion-edge-warning"]')).toBeNull();
    expect(await seriousViolations(root)).toEqual([]);
  });

  it("R-UI-240 on the volume backend the state line adds up each replica's own cache", async () => {
    const own = (replica: string, bytes: number): Bastion['status'][number] =>
      report(replica, { cache: { shared: false, bytes, capacityBytes: 10e9, hits: 970, misses: 30, evictions: 4, protectedBytes: 2e9 } });
    const { root } = await renderScreen(BastionList, {
      capabilities: ADMIN,
      setup: (g) =>
        void ((g(BastionsApi) as FakeBastionsApi).bastions = [
          sampleBastion({ status: [own('quader-bastion-7d9c-x2k4q', 7e9), own('quader-bastion-7d9c-m8v2j', 5e9)] }),
        ]),
    });
    const state = limitsOf(root, 'partners').querySelector('[data-testid="bastion-state"]')!;
    expect([...state.querySelectorAll('.state-line')[0]!.querySelectorAll('span')].map((s) => text(s))).toEqual([
      'Cache 12.0 of 20.0 GB',
      '97 % hits',
      '4.0 GB protected',
      '41 MB/s from the main installation',
    ]);
  });

  it("R-UI-240 on the bucket backend the state line takes the cache once and adds up each replica's reads and callers of the last five minutes, with the meter and the link to the blocks", async () => {
    const { root } = await renderScreen(BastionList, {
      capabilities: ADMIN,
      setup: (g) =>
        void ((g(BastionsApi) as FakeBastionsApi).bastions = [
          sampleBastion({ status: [report('quader-bastion-7d9c-x2k4q'), report('quader-bastion-7d9c-m8v2j')] }),
          sampleBastion({ id: 'b-2', name: 'standby', status: [report('quader-bastion-7d9c-old', { reportedAt: inDays(-10 / 1440) })] }),
        ]),
    });
    const state = limitsOf(root, 'partners').querySelector('[data-testid="bastion-state"]')!;
    expect([...state.querySelectorAll('.state-line')[0]!.querySelectorAll('span')].map((s) => text(s))).toEqual([
      'Cache 7.0 of 10.0 GB',
      '97 % hits',
      '4.5 GB protected',
      '41 MB/s from the main installation',
    ]);
    const meter = state.querySelector('[role="meter"]')!;
    expect(meter.getAttribute('aria-valuenow')).toBe('21');
    expect(text(state)).toContain('41 MB/s of 200 MB/s');
    // The bar is the data colour below 70 %, never green.
    expect(state.querySelector('.fill.warn, .fill.bad')).toBeNull();
    const callers = state.querySelector<HTMLAnchorElement>('[data-testid="bastion-state-callers"]')!;
    expect(text(callers)).toBe('4 callers limited now · 1 address blocked');
    // Why, in words: the closed list's reason, the address once though both replicas name it.
    expect(text(state.querySelector('[data-testid="bastion-block-reasons"]')!)).toBe('Too many requests refused by its flood protection (1)');
    expect(callers.getAttribute('href')).toBe('/admin/bastions/partners/limits/blocks');
    // A report older than five minutes is no state.
    expect(text(limitsOf(root, 'standby').querySelector('[data-testid="bastion-no-report"]')!)).toBe('No report in the last 5 minutes');
  });

  it('R-UI-240 warns where a per-address request figure lies above the edge figure a replica reported, and not below it', async () => {
    const { root } = await renderScreen(BastionList, {
      capabilities: ADMIN,
      setup: (g) =>
        void ((g(BastionsApi) as FakeBastionsApi).bastions = [
          sampleBastion({ contacts: [sampleBastionContact({ edgeLimit: 'rps=150; connections=1000' }), sampleBastionContact({ replica: 'r-2', edgeLimit: 'off' })] }),
          sampleBastion({ id: 'b-2', name: 'standby', contacts: [sampleBastionContact({ edgeLimit: 'rps=1000' })] }),
        ]),
    });
    const warning = limitsOf(root, 'partners').querySelector('[data-testid="bastion-edge-warning"]')!;
    expect(text(warning)).toContain('The ingress of this Bastion refuses more than 150 requests a second per address before these figures apply.');
    expect(warning.classList).toContain('warn');
    expect(limitsOf(root, 'standby').querySelector('[data-testid="bastion-edge-warning"]')).toBeNull();
  });

  it('R-UI-240 a preset applies after a dialog that names each kind rule, flood figure and mode it replaces and keeps the other rules', async () => {
    const { fixture, root, get } = await renderScreen(BastionList, {
      capabilities: ADMIN,
      setup: (g) => {
        withProfiles(g);
        const custom = sampleBastion();
        (g(BastionsApi) as FakeBastionsApi).bastions = [
          { ...custom, limits: { ...custom.limits, mode: 'audit', flood: { ...custom.limits.flood, anonymous: { ...custom.limits.flood.anonymous, requestsPerSecond: 80 } } } },
        ];
      },
    });
    const api = get(BastionsApi) as FakeBastionsApi;
    buttonByText(limitsOf(root, 'partners'), 'Partner network').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-bastion-preset-dialog') as HTMLElement;
    expect(text(dialog.querySelector('h2')!)).toBe('Apply the preset Partner network?');
    expect([...dialog.querySelectorAll('[data-testid="preset-kinds"] li')].map((li) => text(li))).toEqual([
      'Every anonymous caller: Bastion: anonymous → Anonymous',
      'Every signed-in user: Bastion: signed-in → Standard',
      'Every technical token: Bastion: signed-in → Technical',
      'Every CI job: Bastion: signed-in → Technical',
      'Every administrator: Bastion: signed-in → Unlimited',
    ]);
    expect([...dialog.querySelectorAll('[data-testid="preset-flood"] li')].map((li) => text(li))).toEqual(['Anonymous callers: requests a second: 80 → 50']);
    expect(text(dialog.querySelector('[data-testid="preset-mode"]')!)).toBe('Mode: Audit → Enforce');
    expect(text(dialog)).toContain('The rules for single callers, groups and addresses stay.');
    expect(api.calls.filter((c) => c.startsWith('configure'))).toEqual([]);
    buttonByText(dialog, 'Apply preset').click();
    await sleep(fixture, 100);
    expect(api.patches).toEqual([{ limits: { preset: 'partnerNetwork' } }]);
    expect(api.ifMatch).toEqual(['"1"']);
    expect(presetButtons(limitsOf(root, 'partners')).find((b) => b.getAttribute('aria-pressed') === 'true')!.textContent).toContain('Partner network');
  });

  it("R-UI-240 the preset's dialog names the server's table, which it reads when it opens, and holds no copy of its own", async () => {
    const { fixture, root, get } = await renderScreen(BastionList, {
      capabilities: ADMIN,
      setup: (g) => {
        withProfiles(g);
        const api = g(BastionsApi) as FakeBastionsApi;
        api.presetTable = api.presetTable.map((p) => (p.name === 'partnerNetwork' ? { ...p, kinds: { ...p.kinds, administrator: 'standard' } } : p));
      },
    });
    const api = get(BastionsApi) as FakeBastionsApi;
    expect(api.calls).not.toContain('presets');
    buttonByText(limitsOf(root, 'partners'), 'Partner network').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-bastion-preset-dialog') as HTMLElement;
    expect(api.calls).toContain('presets');
    expect([...dialog.querySelectorAll('[data-testid="preset-kinds"] li')].map((li) => text(li))).toContain(
      'Every administrator: Bastion: signed-in → Standard',
    );
  });

  it("R-UI-240 the preset's dialog says nothing changes where the preset changes nothing, and Custom is shown, not offered", async () => {
    const { fixture, root } = await renderScreen(BastionList, {
      capabilities: ADMIN,
      setup: (g) => {
        withProfiles(g);
        const b = sampleBastion();
        (g(BastionsApi) as FakeBastionsApi).bastions = [{ ...b, limits: { ...b.limits, preset: 'custom' } }];
      },
    });
    const limits = limitsOf(root, 'partners');
    expect(text(limits.querySelector('[data-testid="bastion-preset-custom"]')!)).toBe('Custom');
    expect(presetButtons(limits).map((b) => text(b))).toEqual(['Internet', 'Partner network', 'Internal']);
    expect(presetButtons(limits).every((b) => b.getAttribute('aria-pressed') === 'false')).toBe(true);
    // The place's rules are Internet's and the flood figures too: applying Internet changes nothing, and the dialog says so alone.
    buttonByText(limits, 'Internet').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-bastion-preset-dialog') as HTMLElement;
    expect(dialog.querySelector('[data-testid="preset-nothing"]')).not.toBeNull();
    expect(dialog.querySelector('[data-testid="preset-kinds"]')).toBeNull();
  });

  it('R-UI-240 R-LIM-044 the mode switches through the dialog for this Bastion; a session whose sign-in is not recent is asked again and nothing changes', async () => {
    const { fixture, root, get } = await renderScreen(BastionList, { capabilities: ADMIN });
    const api = get(BastionsApi) as FakeBastionsApi;
    buttonByText(limitsOf(root, 'partners'), 'Switch to Audit…').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-limit-mode-dialog') as HTMLElement;
    expect(text(dialog)).toContain('partners');
    expect(text(dialog)).toContain('Its flood protection per address, the anonymous ceiling, the slots and the throughput still hold.');
    api.failure = new ApiError(401, { code: 'reauthentication_required' }, 'req-1');
    buttonByText(dialog.querySelector('.dialog-actions')!, 'Switch to Audit').click();
    await sleep(fixture, 100);
    expect(dialog.querySelector('quader-error-notice')).not.toBeNull();
    expect(api.bastions[0]!.limits.mode).toBe('enforce');
    buttonByText(dialog.querySelector('.dialog-actions')!, 'Switch to Audit').click();
    await sleep(fixture, 100);
    expect(api.patches).toEqual([{ limits: { mode: 'audit' } }]);
    expect(text(limitsOf(root, 'partners').querySelector('[data-testid="bastion-mode"]')!)).toBe(
      'Mode: Audit. Limits are counted, not applied at this Bastion.',
    );
  });

  it('R-UI-240 the throughput is saved in MB/s, empty is unlimited, a word is refused before the request, a refusal keeps the server value', async () => {
    const { fixture, root, get } = await renderScreen(BastionList, { capabilities: ADMIN });
    const api = get(BastionsApi) as FakeBastionsApi;
    const limits = limitsOf(root, 'partners');
    const save = (): void => (limits.querySelector('form.field-action-row button[type="submit"]') as HTMLButtonElement).click();
    type(fieldNamed(limits, 'throughput'), 'fast');
    await settle(fixture);
    save();
    await sleep(fixture, 50);
    expect(text(limits)).toContain('A whole number of MB/s from 1, or empty for unlimited.');
    expect(api.patches).toEqual([]);
    type(fieldNamed(limits, 'throughput'), '');
    await settle(fixture);
    save();
    await sleep(fixture, 100);
    expect(api.patches).toEqual([{ limits: { throughput: { bytesPerSecond: null } } }]);
    expect(fieldNamed(limits, 'throughput').value).toBe('');
    api.failure = new ApiError(422, { code: 'validation_failed', detail: 'a figure of throughput is 0' }, 'req-9');
    type(fieldNamed(limits, 'throughput'), '75');
    await settle(fixture);
    save();
    await sleep(fixture, 100);
    expect(limits.querySelector('quader-error-notice')).not.toBeNull();
    expect(fieldNamed(limits, 'throughput').value).toBe('');
  });

  it('R-UI-240 the details save the flood figures, the slots, the ceiling and the throughput by class in one merge patch', async () => {
    const { fixture, root, get } = await renderScreen(BastionList, { capabilities: ADMIN });
    const api = get(BastionsApi) as FakeBastionsApi;
    const d = details(root, 'partners');
    type(fieldNamed(d, 'flood.anonymous.requestsPerSecond'), '0');
    await settle(fixture);
    buttonByText(d.querySelector('quader-bastion-limit-details')!, 'Save changes').click();
    await sleep(fixture, 50);
    expect(text(d)).toContain('A whole number from 1.');
    expect(api.patches).toEqual([]);
    type(fieldNamed(d, 'flood.anonymous.requestsPerSecond'), '80');
    type(fieldNamed(d, 'anonymousCeiling.requestsPerHour'), '');
    type(fieldNamed(d, 'throughput.bytesPerHour.anonymous'), '50');
    type(fieldNamed(d, 'slots.anonymousShare'), '30');
    await settle(fixture);
    buttonByText(d.querySelector('quader-bastion-limit-details')!, 'Save changes').click();
    await sleep(fixture, 100);
    const sent = api.patches[0]!.limits!;
    expect(sent.flood!.anonymous).toEqual({ requestsPerSecond: 80, burst: 1000, missesPerMinute: 1000 });
    expect(sent.anonymousCeiling).toEqual({ requestsPerSecond: 100, burst: 2000, requestsPerHour: null });
    expect(sent.throughput).toEqual({ anonymousShare: 0.25, bytesPerHour: { anonymous: 50e9, signedIn: null, catalogue: null } });
    expect(sent.slots).toEqual({ maxConcurrent: 64, anonymousShare: 0.3, queueTimeoutMillis: 5000 });
    expect(sent.flood!.exemptNetworks).toEqual([]);
    expect(api.bastions[0]!.limits.preset).toBe('custom');
  });

  it('R-UI-240 R-BAS-093 endpoints:write alone sees the limits as text and changes the switches and the addresses', async () => {
    const { fixture, root, get } = await renderScreen(BastionList, { capabilities: ['endpoints:read', 'endpoints:write'] });
    const api = get(BastionsApi) as FakeBastionsApi;
    const limits = limitsOf(root, 'partners');
    expect(presetButtons(limits)).toEqual([]);
    expect(text(limits.querySelector('[data-testid="bastion-preset-text"]')!)).toBe('Internet');
    expect(limits.querySelector('[data-testid="bastion-mode"] + button')).toBeNull();
    expect(text(limits.querySelector('[data-testid="bastion-throughput-text"]')!)).toBe('Maximum throughput to this Bastion: 200 MB/s');
    expect(limits.querySelector('quader-bastion-limit-details input')).toBeNull();
    expect(text(limits.querySelector('[data-testid="bastion-details-read"]')!)).toContain('Requests a second 50 · Burst 1,000 · Misses a minute 1,000');
    expect(limits.querySelector('a[href$="/limits"]')).toBeNull();
    expect(limits.querySelector('quader-bastion-egress[part="list"] input')).not.toBeNull();
    const gui = limits.querySelector<HTMLElement>('[role="switch"][aria-label="Web interface of partners"]')!;
    expect(gui.getAttribute('aria-checked')).toBe('false');
    // The switch is described by its sentence and by what a public remote shows through it (W35-P9f).
    const described = gui.getAttribute('aria-describedby')!.split(' ').map((id) => text(root.querySelector(`#${id}`)!));
    expect(described[1]).toBe('A public remote repository shows here every package and version your builds fetched through it.');
    gui.click();
    await sleep(fixture, 100);
    expect(api.patches).toEqual([{ switches: { gui: true } }]);
    expect(limits.querySelector('[role="switch"][aria-label="Web interface of partners"]')!.getAttribute('aria-checked')).toBe('true');
  });

  it('R-UI-240 R-BAS-093 limits:write alone changes the limits and sees the switches and the addresses as text', async () => {
    const { root } = await renderScreen(BastionList, { capabilities: ['endpoints:read', 'limits:read', 'limits:write'] });
    const limits = limitsOf(root, 'partners');
    expect(presetButtons(limits)).toHaveLength(3);
    expect(buttonByText(limits, 'Switch to Audit…')).toBeTruthy();
    expect(fieldNamed(limits, 'throughput')).not.toBeNull();
    expect(limits.querySelector('quader-bastion-limit-details input')).not.toBeNull();
    expect(limits.querySelector('[role="switch"]')).toBeNull();
    expect(text(limits.querySelector('[data-testid="bastion-gui-state"]')!)).toBe('The web interface is off.');
    expect(text(limits.querySelector('[data-testid="bastion-docs-state"]')!)).toBe('Documentation is off.');
    expect(limits.querySelector('quader-bastion-egress[part="list"] input')).toBeNull();
    expect(limits.querySelector<HTMLAnchorElement>('a[href="/admin/bastions/partners/limits"]')!.textContent).toContain('Rules of this Bastion');
  });

  it('R-BAS-102 documentation on while the web interface is off says it shows nothing; a web interface whose catalogue a restart loses warns', async () => {
    const { root } = await renderScreen(BastionList, {
      capabilities: ADMIN,
      setup: (g) =>
        void ((g(BastionsApi) as FakeBastionsApi).bastions = [
          sampleBastion({ switches: { handover: false, gui: false, docs: true } }),
          sampleBastion({ id: 'b-2', name: 'standby', switches: { handover: false, gui: true, docs: false }, contacts: [sampleBastionContact({ catalogueKept: false })] }),
        ]),
    });
    expect(limitsOf(root, 'partners').querySelector('[data-testid="bastion-docs-idle"]')).not.toBeNull();
    expect(limitsOf(root, 'partners').querySelector('[data-testid="bastion-catalogue-lost"]')).toBeNull();
    expect(text(limitsOf(root, 'standby').querySelector('[data-testid="bastion-catalogue-lost"]')!)).toContain(
      "After a restart while the connection to it is down, this Bastion's web interface shows nothing until the connection returns.",
    );
  });

  it("R-BAS-104 the card says what the main installation last built for the web interface, and why it builds nothing with what to do", async () => {
    const gui = { handover: false, gui: true, docs: false };
    const at = inDays(-0.002);
    const { root } = await renderScreen(BastionList, {
      capabilities: ADMIN,
      setup: (g) =>
        void ((g(BastionsApi) as FakeBastionsApi).bastions = [
          sampleBastion({ switches: gui, catalogue: { outcome: 'stopped', reason: 'anonymous_requests_refused', checkedAt: at, builtAt: null } }),
          sampleBastion({ id: 'b-2', name: 'standby', switches: gui, catalogue: { outcome: 'stopped', reason: 'bastion_base_url_unknown', checkedAt: at, builtAt: null } }),
          sampleBastion({ id: 'b-3', name: 'edge', switches: gui, catalogue: { outcome: 'built', reason: null, checkedAt: at, builtAt: at } }),
          sampleBastion({ id: 'b-4', name: 'fresh', switches: gui, catalogue: null }),
          sampleBastion({ id: 'b-5', name: 'off', catalogue: { outcome: 'built', reason: null, checkedAt: at, builtAt: at } }),
        ]),
    });
    const stopped = (name: string): Element | null => limitsOf(root, name).querySelector('[data-testid="bastion-catalogue-stopped"]');
    const line = (name: string): Element | null => limitsOf(root, name).querySelector('[data-testid="bastion-catalogue-line"]');
    // The reasons stand in plain view, outside the closed details, each with what to do.
    expect(text(stopped('partners')!)).toContain('the main installation admits no request without a credential');
    expect(text(stopped('partners')!)).toContain('endpoints.default.auth.anonymous');
    expect(text(stopped('standby')!)).toContain('server.additional_base_urls');
    expect(text(stopped('standby')!)).toContain('Connect it, or add its address there');
    expect(text(line('partners')!)).toBe('Not built: the warning above says why.');
    // A built catalogue and one not built yet say so under the switch; no warning.
    expect(stopped('edge')).toBeNull();
    expect(text(line('edge')!)).toMatch(/^Last built .+\.$/);
    expect(text(line('fresh')!)).toBe('Not built yet. The first build follows within seconds.');
    // The web interface off: nothing of a catalogue, also while the builder still holds one until its run.
    expect(stopped('off')).toBeNull();
    expect(line('off')).toBeNull();
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
