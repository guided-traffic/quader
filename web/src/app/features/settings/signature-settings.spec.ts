import { SessionApi } from '../../api/session-api';
import { SignaturesApi } from '../../api/signatures-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeSignaturesApi, signatureAnchor, signatureBlock, signatureSettings } from '../../api/testing/fakes-signatures';
import { Navigation, areaItems } from '../../core/navigation';
import { ProjectSignatureAnchors } from '../projects/project-signature-anchors';
import { button, buttonByText, expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { SignatureSettingsPage } from './signature-settings';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;
const ADMIN = ['settings:read', 'settings:write', 'security-settings:write', 'content:metadata-read'];
const AUDITOR = ['settings:read', 'content:metadata-read'];

const shipped = signatureAnchor({ id: 1, scope: 'shipped', key: 'sigstore-public', title: 'Public Sigstore', kind: 'sigstore', ca: undefined, sigstore: { issuer: '', identityPattern: '.*', trustRoot: 'public' }, createdBy: null });
const helm = signatureAnchor({ id: 2, scope: 'configuration', key: 'gitlab-ci', title: 'GitLab CI signing CA', createdBy: null });
const own = signatureAnchor({ id: 3, scope: 'installation', key: 'release-key', title: 'Release key', kind: 'openpgp', ca: undefined, openpgp: { keys: '…' } });

async function render(capabilities: readonly string[], setup?: (api: FakeSignaturesApi) => void) {
  const r = await renderScreen(SignatureSettingsPage, {
    capabilities,
    setup: (get) => {
      const api = get(SignaturesApi) as FakeSignaturesApi;
      api.anchorList = [own, helm, shipped];
      setup?.(api);
    },
  });
  return { ...r, api: r.get(SignaturesApi) as FakeSignaturesApi };
}

const rowOf = (root: ParentNode, title: string): HTMLTableRowElement =>
  [...root.querySelectorAll<HTMLTableRowElement>('tr')].find((tr) => text(tr.querySelector('th') ?? tr).startsWith(title))!;

describe('Admin area › Settings › Signatures (R-SIG-036)', () => {
  it('R-SIG-036 lists the anchors by where they come from: a shipped one with only its switch, a Helm one with nothing, an administrator\'s with its switch and Remove; passes axe', async () => {
    const { root } = await render(ADMIN);
    expect(text(root.querySelector('h1')!)).toBe('Signatures');
    expect(text(root.querySelector('[data-testid="signature-promise"]')!)).toContain('It does not tell that the content is safe');
    const titles = [...root.querySelectorAll('section[aria-labelledby="sig-anchors"] tbody th')].map((th) => text(th).split(' ')[0]);
    expect(titles).toEqual(['Public', 'GitLab', 'Release']);
    expect(rowOf(root, 'Public Sigstore').querySelector('quader-switch')).not.toBeNull();
    expect(rowOf(root, 'Public Sigstore').querySelector('td.actions button')).toBeNull();
    expect(rowOf(root, 'GitLab CI signing CA').querySelector('quader-switch')).toBeNull();
    expect(text(rowOf(root, 'GitLab CI signing CA'))).toContain('From the Helm values');
    expect(rowOf(root, 'Release key').querySelector('quader-switch')).not.toBeNull();
    expect(text(rowOf(root, 'Release key').querySelector('td.actions')!)).toContain('Remove');
    await expectAccessible(root);
  });

  it('R-SIG-036 the switch is a check box for the security settings\' writer and text for an auditor, who is offered no action', async () => {
    const admin = await render(ADMIN);
    expect(admin.root.querySelector('section[aria-labelledby="sig-project-anchors"] input[type="checkbox"]')).not.toBeNull();
    const auditor = await render(AUDITOR);
    expect(text(auditor.root.querySelector('[data-testid="project-anchors-state"]')!)).toBe('Project owners may add trust anchors for their project is off.');
    expect(auditor.root.querySelector('quader-switch')).toBeNull();
    expect([...auditor.root.querySelectorAll('button')].map((b) => text(b))).not.toContain('Add trust anchor');
  });

  it('R-SIG-019 saving the switch sends it with the ETag', async () => {
    const { fixture, root, api } = await render(ADMIN);
    root.querySelector<HTMLInputElement>('section[aria-labelledby="sig-project-anchors"] input[type="checkbox"]')!.click();
    await settle(fixture);
    button(root, /^\s*Save changes/).click();
    await settle(fixture);
    expect(api.calls).toEqual([{ op: 'settings', body: { projectAnchors: true, etag: '"1"' } }]);
  });

  it('R-SIG-017 R-SIG-071 switching a shipped anchor off asks its impact, and with none patches only enabled with its version', async () => {
    const { fixture, root, api } = await render(ADMIN);
    rowOf(root, 'Public Sigstore').querySelector<HTMLButtonElement>('quader-switch button')!.click();
    await settle(fixture);
    expect(api.calls).toEqual([
      { op: 'impact', body: { anchor: 1, change: 'disable' } },
      { op: 'updateAnchor', body: { id: 1, patch: { enabled: false }, version: 1 } },
    ]);
  });

  it('R-SIG-074 R-SIG-071 removing an anchor that gated repositories rely on asks with the impact and its repositories', async () => {
    const { fixture, root, api } = await render(ADMIN, (fake) => {
      fake.impactAnswer = { artifacts: 132, repositories: 3, complete: true, bound: 'exact', byRepository: [{ repository: 'web/docker-local', artifacts: 120 }, { repository: 'web/npm-local', artifacts: 10 }, { repository: 'ops/files', artifacts: 2 }] };
    });
    rowOf(root, 'Release key').querySelector<HTMLButtonElement>('td.actions button')!.click();
    await settle(fixture);
    const dialog = text(overlay());
    expect(dialog).toContain('This change makes 132 artifacts in 3 repositories that serve only verified artifacts refused:');
    expect([...overlay().querySelectorAll('ul.items li')].map((li) => text(li))).toEqual(['web/docker-local (120)', 'web/npm-local (10)', 'ops/files (2)']);
    expect(api.calls).toEqual([{ op: 'impact', body: { anchor: 3, change: 'remove' } }]);
  });

  it('R-SIG-020 lists the blocks with their reasons, and lifting one asks first', async () => {
    const { fixture, root, api } = await render(ADMIN, (fake) => {
      fake.blockList = [signatureBlock()];
    });
    expect(text(root)).toContain('The key of the old build server was copied.');
    buttonByText(root, 'Lift block').click();
    await settle(fixture);
    buttonByText(overlay(), 'Lift block').click();
    await settle(fixture);
    expect(api.calls).toEqual([{ op: 'deleteBlock', body: { id: 3 } }]);
  });

  it('R-SIG-017 shows where the public root comes from and its last failure', async () => {
    const { root } = await render(ADMIN, (fake) => {
      fake.current = signatureSettings({ trustRoot: { source: 'refreshed', fetchedAt: '2026-11-21T04:17:00Z', tufVersion: 14, lastError: 'tuf: timeout', lastErrorAt: '2026-11-21T04:18:00Z' } });
    });
    const facts = text(root.querySelector('[data-testid="signature-root"]')!);
    expect(facts).toContain('Refreshed from the Sigstore update repository');
    // The sentence stands on a line of its own below the state and its time; the task's English text is a technical detail.
    const failure = root.querySelector('[data-testid="signature-root"] dd .root-failure')!;
    expect(failure.textContent).toContain('The last refresh of the public root failed');
    expect(failure.previousElementSibling?.querySelector('quader-time-ago')).not.toBeNull();
    expect(text(root.querySelector('[data-testid="signature-root"] [data-testid="technical-details"]')!)).toBe('tuf: timeout');
  });

  it('R-SIG-036 the side bar offers the page to the holder of settings:read', async () => {
    const { get } = await render(AUDITOR);
    const routes = areaItems(get(Navigation).areas().find((a) => a.id === 'admin') ?? null).map((i) => i.route);
    expect(routes).toContain('/admin/settings/signatures');
  });
});

describe('Add trust anchor (R-SIG-015)', () => {
  it('R-SIG-036 the form shows the fields of the kind chosen and the key follows the title', async () => {
    const page = await render(ADMIN);
    const { fixture, api } = page;
    buttonByText(page.root, 'Add trust anchor').click();
    await settle(fixture);
    const root = overlay().querySelector('quader-anchor-dialog')!;
    const labels = (): string[] => [...root.querySelectorAll('label.field-label')].map((l) => text(l));
    expect(labels()).toContain('Certificates (PEM)');
    const kind = root.querySelector<HTMLSelectElement>('select[name="kind"]')!;
    kind.value = 'sigstore';
    kind.dispatchEvent(new Event('change'));
    await settle(fixture);
    expect(labels()).toContain('OIDC issuer');
    expect(labels()).not.toContain('Certificates (PEM)');
    const title = root.querySelector<HTMLInputElement>('input[name="title"]')!;
    title.value = 'Company Sigstore';
    title.dispatchEvent(new Event('input'));
    await settle(fixture);
    expect(root.querySelector<HTMLInputElement>('input[name="key"]')!.value).toBe('company-sigstore');
    for (const [name, value] of [
      ['issuer', 'https://gitlab.example.com'],
      ['identity', 'https://gitlab.example.com/platform/portal//.gitlab-ci.yml@refs/heads/main'],
    ]) {
      const input = root.querySelector<HTMLInputElement>(`input[name="${name}"]`)!;
      input.value = value!;
      input.dispatchEvent(new Event('input'));
    }
    await settle(fixture);
    button(root, 'Add trust anchor').click();
    await settle(fixture);
    expect(api.calls[0]).toEqual({
      op: 'createAnchor',
      body: {
        key: 'company-sigstore',
        title: 'Company Sigstore',
        kind: 'sigstore',
        enabled: true,
        sigstore: { issuer: 'https://gitlab.example.com', identity: 'https://gitlab.example.com/platform/portal//.gitlab-ci.yml@refs/heads/main', trustRoot: 'public' },
      },
    });
  });
});

describe('Trust anchors in a project (R-SIG-037)', () => {
  const asMember = (role: 'owner' | 'reader') => (get: <A>(token: abstract new (...args: never[]) => A) => A) => {
    (get(SessionApi) as FakeSessionApi).meResult = { capabilities: [], projects: [{ key: 'web', role }] as never };
  };

  it('R-SIG-037 the section is hidden with the switch off and no anchors', async () => {
    const { root } = await renderScreen(ProjectSignatureAnchors, { inputs: { project: 'web' }, setup: asMember('owner') });
    expect(root.querySelector('[data-testid="project-anchors"]')).toBeNull();
  });

  it('R-SIG-037 with the switch on, an Owner may add; the installation\'s anchors stand read-only below', async () => {
    const { root } = await renderScreen(ProjectSignatureAnchors, {
      inputs: { project: 'web' },
      setup: (get) => {
        asMember('owner')(get);
        const api = get(SignaturesApi) as FakeSignaturesApi;
        api.current = signatureSettings({ projectAnchors: true });
        api.anchorList = [own];
      },
    });
    expect(root.querySelector('[data-testid="project-anchors"]')).not.toBeNull();
    expect([...root.querySelectorAll('button')].map((b) => text(b))).toContain('Add trust anchor');
    expect(text(root)).toContain('Also trusted in this project');
    expect(text(root)).toContain('Release key');
  });

  it('R-SIG-037 read-only for a member', async () => {
    const { root } = await renderScreen(ProjectSignatureAnchors, {
      inputs: { project: 'web' },
      setup: (get) => {
        asMember('reader')(get);
        const api = get(SignaturesApi) as FakeSignaturesApi;
        api.current = signatureSettings({ projectAnchors: true });
        api.projectList = { web: [signatureAnchor({ id: 9, scope: 'project', project: 'web', title: 'Team key' })] };
      },
    });
    expect(text(root)).toContain('Team key');
    expect([...root.querySelectorAll('button')].map((b) => text(b))).not.toContain('Add trust anchor');
    expect(root.querySelector('td.actions button')).toBeNull();
  });

  it('R-SIG-019 with the switch off, an Owner sees the anchors the project holds as not applied', async () => {
    const { root } = await renderScreen(ProjectSignatureAnchors, {
      inputs: { project: 'web' },
      setup: (get) => {
        asMember('owner')(get);
        (get(SignaturesApi) as FakeSignaturesApi).projectList = { web: [signatureAnchor({ id: 9, scope: 'project', project: 'web', title: 'Team key', applied: false })] };
      },
    });
    expect(text(root.querySelector('[data-testid="project-anchors-off"]')!)).toBe('infoNot applied: project anchors are switched off by an administrator.');
    expect([...root.querySelectorAll('button')].map((b) => text(b))).not.toContain('Add trust anchor');
  });
});
