import { Router } from '@angular/router';
import { DownloadLinksApi } from '../../../api/download-links-api';
import { ApiError } from '../../../api/problem';
import { RepositoriesApi } from '../../../api/repositories-api';
import { ANSIBLE_FORMAT, FakeRepositoriesApi, OCI_FORMAT } from '../../../api/testing/fakes-catalog';
import { FakeDownloadLinksApi, downloadLinkSettings, linkConnection } from '../../../api/testing/fakes-download-links';
import { DownloadLinksSwitchStore } from '../../../core/download-links-switch';
import { Navigation, areaItems } from '../../../core/navigation';
import { addressValues, button, expectAccessible, helpSigns, renderScreen, settle, text } from '../../../testing/screen';
import { DownloadLinksSection, isHttpsOrigin } from './download-links-section';

const ADMIN = ['settings:read', 'settings:write'];
const AUDITOR = ['settings:read'];

async function render(capabilities: readonly string[], setup?: (api: FakeDownloadLinksApi) => void) {
  const r = await renderScreen(DownloadLinksSection, {
    capabilities,
    setup: (get) => {
      (get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [OCI_FORMAT, ANSIBLE_FORMAT];
      setup?.(get(DownloadLinksApi) as FakeDownloadLinksApi);
    },
  });
  return { ...r, api: r.get(DownloadLinksApi) as FakeDownloadLinksApi };
}

/** The text of an element without the glyph names of its icons. */
const words = (el: Element): string => {
  const copy = el.cloneNode(true) as Element;
  copy.querySelectorAll('.material-icons').forEach((i) => i.remove());
  return text(copy);
};

const box = (root: ParentNode): HTMLInputElement | null =>
  [...root.querySelectorAll<HTMLElement>('.check-row')]
    .find((l) => text(l).startsWith('Deliver large downloads by signed links'))
    ?.querySelector('input') ?? null;
const field = (root: ParentNode, id: string): HTMLInputElement => root.querySelector<HTMLInputElement>(`#${id}`)!;
const type = (input: HTMLInputElement, value: string): void => {
  input.value = value;
  input.dispatchEvent(new Event('input'));
};
const status = (root: ParentNode): string => words(root.querySelector('[data-testid="links-status"]')!);

/** On, three connections: default serves links, archive is encrypted, eu-backup failed the link check. */
function mixed(api: FakeDownloadLinksApi): void {
  api.current = downloadLinkSettings({
    enabled: true,
    connections: [
      linkConnection(),
      linkConnection({ name: 'archive', encrypted: true, usable: false, reasons: [{ code: 'encrypted', detail: null }] }),
      linkConnection({
        name: 'eu-backup',
        endpoint: 'https://objects.backup.example.net',
        usable: false,
        reasons: [{ code: 'link_check_failed', detail: 'the link answered 403 with 0 bytes, want 200 with the probe\'s 2048', check: { outcome: 'unexpected_answer', status: 403, bytes: 0, expectedStatus: 200, expectedBytes: 2048 } }],
        handoverUsable: false,
        handoverReasons: [{ code: 'link_check_failed', detail: 'the link answered 403 with 0 bytes', check: { outcome: 'unexpected_answer', status: 403, bytes: 0, expectedStatus: 200, expectedBytes: 2048 } }],
      }),
    ],
  });
}

describe('Admin area › Settings › Storage: the section Download links (R-UI-200)', () => {
  it('R-UI-200 off: the status says Quader delivers every download, the switch with its note and the speed limits, the details closed with their values; passes axe', async () => {
    const { root } = await render(ADMIN);
    expect(text(root.querySelector('h2')!)).toBe('Download links');
    expect(status(root)).toBe('Off Quader delivers every download itself.');
    expect(root.querySelector('[data-testid="links-status"] .ok')).toBeNull();
    expect(box(root)?.checked).toBe(false);
    // What the switch does and the speed limits are its help (R-UI-267); what it gives away stays one visible line (Q-UI-1628).
    expect(helpSigns(root.querySelector('form')!).filter(([name]) => name.startsWith('About Deliver'))).toEqual([
      [
        'About Deliver large downloads by signed links of the object store',
        "A client fetches the file from the object store by a short-lived link; Quader still decides who may download. No speed limit slows the download; its size counts against the caller's budgets.",
      ],
    ]);
    expect(text(root.querySelector('#links-bytes')!)).toBe('Quader no longer sees the bytes: the client checks them, and must reach the object store.');
    expect(box(root)?.getAttribute('aria-describedby')).toBe('links-bytes');
    expect(text(root)).not.toMatch(/profile/i);
    expect([...root.querySelectorAll('[data-testid="links-connections"] :is(dt, dd)')].map(text)).toEqual(['Can serve links', 'default']);
    const details = root.querySelector('quader-disclosure button')!;
    expect(details.getAttribute('aria-expanded')).toBe('false');
    expect(field(root, 'links-min-size').value).toBe('1');
    expect(field(root, 'links-life').value).toBe('60');
    expect(field(root, 'links-address-default').value).toBe('https://s3.eu-central-1.example.com');
    expect([...root.querySelectorAll('[data-testid="links-routes"] li')].map(text)).toEqual(['Ansible Galaxy: ansible.artifact.get', 'OCI (Docker): oci.blob.get']);
    await expectAccessible(root);
  });

  it('R-UI-200 on: the status names the formats of the routes, green; a format to come appears from the answer alone', async () => {
    const { root } = await render(ADMIN, (api) => {
      api.current = downloadLinkSettings({
        enabled: true,
        routes: [
          { format: 'oci', route: 'oci.blob.get' },
          { format: 'ansible', route: 'ansible.artifact.get' },
          { format: 'cargo', route: 'cargo.crate.get' },
        ],
      });
    });
    expect(status(root)).toBe('On Large downloads of Ansible Galaxy, cargo, and OCI (Docker) go directly from the object store.');
    expect(root.querySelector('[data-testid="links-status"] .ok')).not.toBeNull();
  });

  it('R-UI-200 several connections: the plain path names which serve links, Details holds each reason under its field', async () => {
    const { root } = await render(ADMIN, mixed);
    expect([...root.querySelectorAll('[data-testid="links-connections"] :is(dt, dd)')].map(text)).toEqual([
      'Can serve links',
      'default',
      'Cannot serve links',
      'archive and eu-backup (Details says why)',
    ]);
    expect(root.querySelector('[data-testid="links-none"]')).toBeNull();
    expect(words(root.querySelector('[data-testid="links-reason-archive"]')!)).toBe('The connection archive is encrypted. Quader delivers its content to clients itself.');
    expect(words(root.querySelector('[data-testid="links-reason-eu-backup"]')!)).toBe(
      // Q-UI-996: the sentence is built from the check's fields, not from the server's English detail.
      'The object store of eu-backup did not answer a test link as expected: the test link answered with status 403 and 0 bytes instead of status 200 and 2,048 bytes. Quader delivers its content itself.',
    );
  });

  it('R-UI-200 when no connection can serve a link the reasons stand above the switch, which stays usable', async () => {
    const { root } = await render(ADMIN, (api) => {
      api.current = downloadLinkSettings({
        enabled: true,
        connections: [
          linkConnection({ name: 'default', usable: false, reasons: [{ code: 'quader_host', detail: 'quader.example.com' }] }),
          linkConnection({ name: 'archive', encrypted: true, usable: false, reasons: [{ code: 'encrypted', detail: null }] }),
        ],
      });
    });
    expect(status(root)).toBe('On, but no link is issued No connection can serve a link; the reasons are below.');
    expect([...root.querySelectorAll('[data-testid="links-none"] p')].map(text)).toEqual([
      'The address of default lies under quader.example.com, an address of Quader. Image clients would send their credentials to it, and the object store refuses such a link.',
      'The connection archive is encrypted. Quader delivers its content to clients itself.',
    ]);
    expect(box(root)?.disabled).toBe(false);
  });

  it('R-UI-200 saving sends only the changed members as a merge patch with If-Match; an address left as the endpoint is not sent', async () => {
    const { fixture, root, api } = await render(ADMIN);
    box(root)!.click();
    type(field(root, 'links-min-size'), '16');
    await settle(fixture);
    button(root, /^\s*Save changes/).click();
    await settle(fixture);
    expect(api.calls).toEqual([{ op: 'settings', body: { patch: { enabled: true, minSize: 16777216 }, etag: '"1"' } }]);
    expect(fixture.debugElement.injector.get(DownloadLinksSwitchStore).enabled()).toBe(true);
    type(field(root, 'links-address-default'), 'https://objects.example.com');
    await settle(fixture);
    button(root, /^\s*Save changes/).click();
    await settle(fixture);
    expect(api.calls[1]).toEqual({ op: 'settings', body: { patch: { publicAddresses: { default: 'https://objects.example.com' } }, etag: '"2"' } });
    // Cleared, the saved address goes and the connection's endpoint is followed again.
    type(field(root, 'links-address-default'), '');
    await settle(fixture);
    button(root, /^\s*Save changes/).click();
    await settle(fixture);
    expect(api.calls[2]).toEqual({ op: 'settings', body: { patch: { publicAddresses: { default: null } }, etag: '"3"' } });
    expect(field(root, 'links-address-default').value).toBe('https://s3.eu-central-1.example.com');
  });

  it('R-UI-200 a value outside its range or an address that is no https origin is refused at its field before anything is sent, and Details opens', async () => {
    const { fixture, root, api } = await render(ADMIN);
    type(field(root, 'links-life'), '5');
    type(field(root, 'links-address-default'), 'http://objects.example.com/bucket');
    await settle(fixture);
    button(root, /^\s*Save changes/).click();
    await settle(fixture);
    expect(api.calls).toEqual([]);
    expect(root.querySelector('quader-disclosure button')!.getAttribute('aria-expanded')).toBe('true');
    expect(text(root)).toContain('Enter a number of seconds from 10 to 3,600.');
    expect(text(root)).toContain('Enter an https address without a path, such as https://objects.example.com.');
    expect(field(root, 'links-life').getAttribute('aria-invalid')).toBe('true');
  });

  it('R-UI-200 the server\'s refusal of an address stands at its field', async () => {
    const { fixture, root, api } = await render(ADMIN);
    api.failure = new ApiError(
      422,
      {
        code: 'validation_failed',
        errors: [{ pointer: '/publicAddresses/default', code: 'invalid_value', detail: 'the host s3.quader.example.com is a host of Quader' }],
      },
      'rid-422',
    );
    type(field(root, 'links-address-default'), 'https://s3.quader.example.com');
    await settle(fixture);
    button(root, /^\s*Save changes/).click();
    await settle(fixture);
    expect(text(root.querySelector('[data-testid="links-address-default"]')!)).toContain('the host s3.quader.example.com is a host of Quader');
    expect(root.querySelector('quader-error-notice')).toBeNull();
  });

  it('R-UI-200 Test link sends the address as typed and shows the link, the command and what to expect; nothing is saved', async () => {
    const { fixture, root, api } = await render(ADMIN);
    type(field(root, 'links-address-default'), 'https://objects.example.com');
    await settle(fixture);
    button(root, /Test link/).click();
    await settle(fixture);
    expect(api.calls).toEqual([{ op: 'test', body: { connection: 'default', publicAddress: 'https://objects.example.com' } }]);
    const result = root.querySelector('[data-testid="links-test-default"]')!;
    const open = result.querySelector<HTMLAnchorElement>('a')!;
    expect(text(open)).toBe('Open test link');
    expect(open.target).toBe('_blank');
    expect(open.href).toContain('https://objects.example.com/blobs/quader/_probe/link-test-');
    expect(addressValues(result)[0]).toMatch(/^curl -fsS 'https:\/\/objects\.example\.com\//);
    expect(text(result)).toContain('Valid for 60 seconds.');
  });

  it('R-UI-200 R-STOR-096 an encrypted connection has its address and its test link for a Bastion, says no client gets a link and a Bastion reads with each file\'s key, and saves its address', async () => {
    const { fixture, root, api } = await render(ADMIN, (fake) => {
      mixed(fake);
      // A store that ignores the overrides fails a client's link, which an encrypted connection never serves: that reason does not stand
      // under its field; why a Bastion cannot read it does (its handoverReasons).
      fake.current = {
        ...fake.current,
        connections: fake.current.connections.map((c) =>
          c.name === 'archive'
            ? {
                ...c,
                reasons: [{ code: 'encrypted', detail: null }, { code: 'link_check_failed', detail: 'the store ignored the overrides' }],
                handoverUsable: false,
                handoverReasons: [{ code: 'quader_host', detail: 'quader.example.com' }],
              }
            : c,
        ),
      };
    });
    const archive = root.querySelector('[data-testid="links-address-archive"]')!;
    expect(field(root, 'links-address-archive').value).toBe('https://s3.eu-central-1.example.com');
    expect(text(archive)).toContain(
      "The address a Bastion reaches the object store at. It must not be an address of Quader. No client gets a link to an encrypted connection; a Bastion whose direct reads from the object store are on reads each file there with a link and that file's key.",
    );
    expect(text(archive)).not.toContain('your clients');
    expect([...archive.querySelectorAll('[data-testid="links-reason-archive"]')].map(words)).toEqual([
      'The connection archive is encrypted. Quader delivers its content to clients itself.',
      'The address of archive lies under quader.example.com, an address of Quader. A Bastion refuses a link there.',
    ]);
    type(field(root, 'links-address-archive'), 'https://archive.objects.example.com');
    await settle(fixture);
    button(archive, /Test link/).click();
    await settle(fixture);
    expect(api.calls).toEqual([{ op: 'test', body: { connection: 'archive', publicAddress: 'https://archive.objects.example.com' } }]);
    const result = root.querySelector('[data-testid="links-test-archive"]')!;
    expect(addressValues(result)[0]).toMatch(/^curl -fsS 'https:\/\/archive\.objects\.example\.com\//);
    expect(text(result)).toContain("Run the command on a machine of a Bastion's network to test that network.");
    expect(text(result)).not.toContain('your clients');
    button(root, /^\s*Save changes/).click();
    await settle(fixture);
    expect(api.calls[1]).toEqual({ op: 'settings', body: { patch: { publicAddresses: { archive: 'https://archive.objects.example.com' } }, etag: '"1"' } });
    await expectAccessible(root);
  });

  it("R-UI-200 a plain connection's test link names the clients' and a Bastion's network", async () => {
    const { fixture, root } = await render(ADMIN);
    button(root, /Test link/).click();
    await settle(fixture);
    expect(text(root.querySelector('[data-testid="links-test-default"]')!)).toContain(
      "Run the command on a machine of your clients or of a Bastion's network to test that network.",
    );
  });

  it('R-BAS-071 the fragment details, which the Bastions screen links with, opens Details', async () => {
    const { root } = await renderScreen(DownloadLinksSection, {
      capabilities: ADMIN,
      setup: async (get) => {
        (get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [OCI_FORMAT, ANSIBLE_FORMAT];
        await get(Router).navigateByUrl('/admin/settings/storage#details');
      },
    });
    expect(root.querySelector('quader-disclosure button')!.getAttribute('aria-expanded')).toBe('true');
  });

  it('R-UI-200 Test link of an address that is no https origin says so at the field and sends nothing', async () => {
    const { fixture, root, api } = await render(ADMIN, (fake) => {
      fake.current = downloadLinkSettings({ connections: [linkConnection({ endpoint: 'http://seaweedfs.quader.svc:8333' })] });
    });
    button(root, /Test link/).click();
    await settle(fixture);
    expect(api.calls).toEqual([]);
    expect(text(root.querySelector('[data-testid="links-address-default"]')!)).toContain('Enter an https address without a path');
  });

  it('R-UI-200 the auditor reads the state as text, the fields read-only, and is offered no action', async () => {
    const { root } = await render(AUDITOR, mixed);
    expect(box(root)).toBeNull();
    const state = root.querySelector('[data-testid="links-switch-state"]')!;
    expect(text(state.querySelector('.help-anchor')!)).toBe('Deliver large downloads by signed links of the object store is on.');
    expect(helpSigns(state).map(([name]) => name)).toEqual(['About Deliver large downloads by signed links of the object store']);
    expect(field(root, 'links-address-default').readOnly).toBe(true);
    expect(root.querySelector('button[type="submit"]')).toBeNull();
    expect([...root.querySelectorAll('button')].map((b) => text(b))).not.toContain('Test link (default)');
  });

  it('R-UI-200 the side bar offers Storage as the fifth page of Settings', async () => {
    const { get } = await render(ADMIN);
    const routes = areaItems(get(Navigation).area('admin')).map((i) => i.route);
    expect(routes.indexOf('/admin/settings/storage')).toBe(routes.indexOf('/admin/settings/signatures') + 1);
  });

  it('R-STOR-074 an address is an https origin', () => {
    for (const [value, ok] of [
      ['https://objects.example.com', true],
      ['https://objects.example.com:8443/', true],
      ['http://objects.example.com', false],
      ['https://objects.example.com/bucket', false],
      ['https://u:p@objects.example.com', false],
      ['https://objects.example.com?x=1', false],
      ['objects.example.com', false],
    ] as const) {
      expect(isHttpsOrigin(value), value).toBe(ok);
    }
  });
});
