import { UpstreamApi } from '../../api/upstream-api';
import { CLAMAV_ENDPOINT, FakeUpstreamApi } from '../../api/testing/fakes-upstream';
import { GuidesApi } from '../../api/guides-api';
import { ClientSetupApi } from '../../api/packages-api';
import { ApiError } from '../../api/problem';
import { RepositoriesApi } from '../../api/repositories-api';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeRepositoriesApi, GENERIC_FORMAT, NUGET_FORMAT, OCI_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { FakeGuidesApi } from '../../api/testing/fakes-guides';
import { FakeClientSetupApi, GENERIC_REPOSITORY, NPM_REPOSITORIES, NUGET_REPOSITORIES, OCI_REPOSITORIES } from '../../api/testing/fakes-packages';
import { FakeTokensApi } from '../../api/testing/access-fakes';
import { TokensApi } from '../../api/tokens-api';
import { button, expectAccessible, renderScreen, settle, storedText, text, type } from '../../testing/screen';
import { ClientSetupPage, READ_TOKEN_COMMAND, fillToken } from './client-setup';
import { base62, crc32 } from './token-shape';

type Opened = Awaited<ReturnType<typeof open>>;

/** A syntactically valid token (prefix, 43 base62 digits, CRC-32) that the server never issued. */
function validToken(fill = 'x'): string {
  const body = 'qdr_pat_' + fill.repeat(43);
  return body + base62(crc32(body), 6);
}

async function open(
  inputs: Record<string, unknown> = { project: 'web', key: 'npm-all' },
  options: { capabilities?: readonly string[]; anonymous?: boolean; setup?: (get: Opened['get']) => void } = {},
) {
  const r = await renderScreen(ClientSetupPage, {
    inputs,
    capabilities: options.capabilities,
    setup: (get) => {
      const repos = get(RepositoriesApi) as FakeRepositoriesApi;
      repos.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT, NUGET_FORMAT, OCI_FORMAT];
      repos.repositories = [...NPM_REPOSITORIES, ...NUGET_REPOSITORIES, ...OCI_REPOSITORIES, { ...GENERIC_REPOSITORY }];
      if (options.anonymous) {
        (get(SessionApi) as FakeSessionApi).whoamiResult = new ApiError(401, { code: 'unauthenticated' }, null);
        (get(GuidesApi) as FakeGuidesApi).anonymous = true;
        // The server sends an anonymous caller no publish snippets (R-API-030).
        for (const r of NPM_REPOSITORIES) (get(ClientSetupApi) as FakeClientSetupApi).readOnly.add(`${r.project}/${r.key}`);
      }
      options.setup?.(get as Opened['get']);
    },
  });
  return {
    ...r,
    setup: r.get(ClientSetupApi) as FakeClientSetupApi,
    guides: r.get(GuidesApi) as FakeGuidesApi,
    tokens: r.get(TokensApi) as FakeTokensApi,
  };
}

/** The snippets of a step, as the page shows them. */
const snippets = (r: Opened, step?: 'configure' | 'install' | 'publish' | 'verify') =>
  [...r.root.querySelectorAll<HTMLElement>(`${step ? `section[aria-labelledby="step-${step}"] ` : ''}quader-setup-snippet pre`)].map(
    (p) => [...p.querySelectorAll('.line')].map((l) => l.textContent ?? '').join('\n'),
  );
const stepHeadings = (r: Opened) => [...r.root.querySelectorAll<HTMLElement>('section.step > h2')].map((h) => text(h));
const radio = (r: Opened, name: string, value: string) =>
  r.root.querySelector<HTMLInputElement>(`input[name="${name}"][value="${value}"]`)!;

async function choose(r: Opened, name: string, value: string) {
  const input = radio(r, name, value);
  input.checked = true;
  input.dispatchEvent(new Event('change', { bubbles: true }));
  await settle(r.fixture);
}

describe('Set up client', () => {
  describe('token replacement', () => {
    it('R-UI-016 R-UI-043 the browser replaces every {{token}} of a snippet and nothing else', () => {
      expect(fillToken('a={{token}}\nb={{token}}', 'T')).toBe('a=T\nb=T');
      expect(fillToken('no placeholder', 'T')).toBe('no placeholder');
      expect(fillToken('x={{token}}', null)).toBe('x={{token}}');
    });

    it('R-UI-016 the replacement is literal: $& or $1 in a token do not expand', () => {
      expect(fillToken('x={{token}}', 'a$&b$1')).toBe('x=a$&b$1');
    });
  });

  it('R-UI-120 opened from a repository, the page names it, its format and its class, shows its address and asks for none of them', async () => {
    const r = await open();
    expect(text(r.root.querySelector('h1')!)).toBe('Set up client');
    const context = text(r.root.querySelector('.context')!);
    expect(context).toContain('web/npm-all');
    expect(context).toContain('npm');
    expect(context).toContain('Virtual');
    expect(r.root.querySelector('.context a')?.getAttribute('href')).toBe('/projects/web/repositories/npm-all');
    // The format and the class are the badges of the repository's own header.
    expect(text(r.root.querySelector('.context .badge.format-badge')!)).toContain('npm');
    expect(text(r.root.querySelector('.context .badge.class-badge')!)).toContain('Virtual');
    expect(r.root.querySelector('quader-address-field input')?.getAttribute('value') ?? (r.root.querySelector<HTMLInputElement>('quader-address-field input')?.value)).toBe(
      'https://quader.example.net/api/npm/web/npm-all/',
    );
    for (const name of ['format', 'repository', 'class', 'guide-format', 'guide-class']) {
      expect(r.root.querySelector(`input[name="${name}"]`)).toBeNull();
    }
    expect(r.setup.calls.at(-1)).toMatchObject({ repository: 'web/npm-all', query: { client: 'npm', credentials: 'env' } });
    expect(r.guides.calls.at(-1)).toEqual({ repository: 'web/npm-all', client: 'npm' });
    await expectAccessible(r.root);
  });

  it('R-UI-121 the job comes first: Configure, Install and Publish with their snippets, then what the class is, then the rest folded', async () => {
    const r = await open();
    expect(stepHeadings(r)).toEqual(['1Configure', '2Install', '3Publish']);
    expect(snippets(r, 'configure').some((s) => s.startsWith('registry=https://quader.example.net/api/npm/web/npm-all/'))).toBe(true);
    expect(snippets(r, 'install')).toContain('npm install <package>');
    expect(snippets(r, 'publish')).toContain('npm publish --registry https://quader.example.net/api/npm/web/npm-all/');
    // The explanation follows the steps: the class's section stands open, every other is a closed disclosure titled with its heading.
    const explanation = r.root.querySelector('quader-guide-view')!;
    const lastStep = [...r.root.querySelectorAll('section.step')].at(-1)!;
    expect(lastStep.compareDocumentPosition(explanation) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // Under the heading Guide every section is a closed row, the first one, what the class is, too (Q-UI-805).
    expect(text(r.root.querySelector('section.explanation > h2')!)).toBe('Guide');
    const folded = [...explanation.querySelectorAll<HTMLElement>('quader-disclosure.guide-more')].map((d) => d.dataset['section']);
    expect(folded[0]).toMatch(/\.what$/);
    expect(folded.slice(1)).toEqual(['npm.configure', 'npm.authenticate', 'npm.install', 'npm.publish', 'npm.troubleshooting']);
    expect(explanation.querySelectorAll('quader-disclosure button[aria-expanded="true"]').length).toBe(0);
    // No boxed table of contents above everything.
    expect(r.root.querySelector('nav.toc')).toBeNull();
    // A snippet keeps only the sentence its window does not say; the install command needs none (Q-UI-804).
    expect(text(r.root.querySelector('section[aria-labelledby="step-configure"] quader-setup-snippet .why')!)).toBe('Save as .npmrc in your project.');
    expect(r.root.querySelector('section[aria-labelledby="step-install"] .why')).toBeNull();
  });

  it('R-SIG-056 the step Verify follows the others while the installation has a signing identity, and is not there without one', async () => {
    const without = await open({ project: 'web', key: 'oci-local' }, { setup: (get) => ((get(ClientSetupApi) as FakeClientSetupApi).signingIdentity = null) });
    expect(stepHeadings(without)).not.toContain('4Verify');
    expect(without.root.querySelector('section[aria-labelledby="step-verify"]')).toBeNull();
    const r = await open(
      { project: 'web', key: 'oci-local' },
      { setup: (get) => ((get(ClientSetupApi) as FakeClientSetupApi).signingIdentity = 'https://quader.example.net') },
    );
    expect(stepHeadings(r)).toEqual(['1Configure', '2Install', '3Publish', '4Verify']);
    const verify = snippets(r, 'verify');
    expect(verify).toHaveLength(1);
    expect(verify[0]).toContain('cosign verify-attestation');
    expect(verify[0]).toContain('--certificate-identity https://quader.example.net');
    expect(text(r.root.querySelector('section[aria-labelledby="step-verify"] quader-setup-snippet .why')!)).toContain('cosign');
    await expectAccessible(r.root);
  });

  it('R-SIG-056 npm\'s step Verify starts with npm audit signatures, then the attestation\'s check', async () => {
    const r = await open(
      { project: 'web', key: 'npm-local' },
      { setup: (get) => ((get(ClientSetupApi) as FakeClientSetupApi).signingIdentity = 'https://quader.example.net') },
    );
    const verify = snippets(r, 'verify');
    expect(verify).toHaveLength(2);
    expect(verify[0]).toContain('npm audit signatures');
    expect(verify[1]).toContain('cosign verify-blob-attestation');
    expect(text(r.root.querySelector('section[aria-labelledby="step-verify"] quader-setup-snippet .why')!)).toContain('registry signatures');
  });

  it('R-UI-016 acceptance: the default produces a .npmrc with the literal ${QUADER_TOKEN} and shows no secret', async () => {
    const r = await open();
    expect(snippets(r, 'configure')[0]).toContain('//quader.example.net/api/npm/web/npm-all/:_authToken=${QUADER_TOKEN}');
    expect(text(r.root)).not.toMatch(/qdr_(pat|sat|cit)_/);
    expect(text(r.root.querySelector('.credential')!)).toContain('The snippets read it from QUADER_TOKEN.');
  });

  describe('the client (R-UI-120)', () => {
    it('R-UI-120 a format with several clients offers them in one control, preselecting the one the link names', async () => {
      const r = await open({ project: 'web', key: 'npm-local', client: 'pnpm' });
      const select = r.root.querySelector<HTMLSelectElement>('select#setup-client')!;
      expect([...select.options].map((o) => o.value)).toEqual(['npm', 'pnpm', 'yarn', 'yarn-classic']);
      expect(select.value).toBe('pnpm');
      expect(r.root.querySelector('input[name="client"]')).toBeNull();
      expect(r.setup.calls.at(-1)).toMatchObject({ repository: 'web/npm-local', query: { client: 'pnpm' } });
      expect(r.guides.calls.at(-1)).toEqual({ repository: 'web/npm-local', client: 'pnpm' });
      expect(snippets(r, 'install')).toContain('pnpm add <package>');
    });

    it('R-UI-120 a format with one client asks nothing', async () => {
      const r = await open({ project: 'web', key: 'generic-local' });
      expect(r.root.querySelector('select#setup-client')).toBeNull();
      expect(r.setup.calls.at(-1)).toMatchObject({ repository: 'web/generic-local', query: { client: 'curl' } });
      expect(snippets(r, 'configure')[0]).toContain('quader_curl() {');
    });

    it('R-UI-017 Yarn Berry gets its .yarnrc.yml and its publish registry', async () => {
      const r = await open({ project: 'web', key: 'npm-all', client: 'yarn' });
      const yml = snippets(r, 'configure')[0]!;
      expect(yml).toContain('npmRegistryServer: "https://quader.example.net/api/npm/web/npm-all/"');
      expect(yml).toContain('npmAuthToken: "${QUADER_TOKEN}"');
      expect(snippets(r, 'publish').some((s) => s.includes('npmPublishRegistry: "https://quader.example.net/api/npm/web/npm-all/"'))).toBe(true);
    });
  });

  it('R-UI-016 R-UI-125 each snippet shows its file and has a copy button; a file can be downloaded; a long line wraps inside its block', async () => {
    const r = await open();
    // A file is an editor window: its tab names the file, Download and the copy button stand in its bar (Q-UI-801).
    const first = r.root.querySelector('quader-setup-snippet quader-code-window')!;
    expect(first.classList).toContain('editor');
    expect(text(first.querySelector('.tab-title')!)).toBe('.npmrc');
    expect(first.querySelector('.bar quader-copy-button button')?.getAttribute('aria-label')).toBe('Copy snippet');
    expect(first.querySelector('.bar button.download')?.getAttribute('aria-label')).toBe('Download .npmrc');
    // One element per line: a long line wraps with an indented continuation instead of being cut off (Q-UI-486).
    expect(first.querySelectorAll('pre .line').length).toBe(2);
    // A command is a terminal window, without Download.
    const cli = [...r.root.querySelectorAll('quader-code-window')].find((s) => text(s).includes('npm config set'))!;
    expect(cli.classList).toContain('terminal');
    expect(cli.querySelector('button.download')).toBeNull();
  });

  it('R-UI-016 R-UI-043 a download goes through a Blob URL that is revoked right after the click', async () => {
    const created: Blob[] = [];
    const revoked: string[] = [];
    URL.createObjectURL = (blob: Blob | MediaSource) => {
      created.push(blob as Blob);
      return 'blob:quader/test';
    };
    URL.revokeObjectURL = (url: string) => void revoked.push(url);
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    try {
      const r = await open();
      r.root.querySelector<HTMLButtonElement>('quader-setup-snippet button.download')!.click();
      expect(created).toHaveLength(1);
      expect(await created[0]!.text()).toContain('${QUADER_TOKEN}');
      expect(click).toHaveBeenCalledOnce();
      expect(revoked).toEqual(['blob:quader/test']);
    } finally {
      click.mockRestore();
    }
  });

  describe('a token created in place (R-UI-122)', () => {
    it('R-UI-122 R-UI-043 creates a read-only token for the repository with the server\'s lifetime, shows it once with the line that puts it into the variable, and leaves the snippets on the variable', async () => {
      const r = await open();
      expect([...r.root.querySelectorAll('a')].some((a) => a.getAttribute('href')?.startsWith('/profile/tokens'))).toBe(false);
      button(r.root, 'Create a token').click();
      await settle(r.fixture);

      const request = r.tokens.lastRequest!;
      expect(request.description).toMatch(/^Client setup: npm, web\/npm-all, \d{4}-\d{2}-\d{2}$/);
      expect(request.ttlSeconds).toBe(2_592_000);
      expect(request.scope).toEqual({ repositories: [{ repositories: ['web/npm-all'], actions: ['read'] }] });
      const shown = r.root.querySelector<HTMLInputElement>('.token-value input')?.value ?? '';
      expect(shown).toMatch(/^qdr_pat_/);
      expect(text(r.root)).toContain('This token is shown once.');
      expect(text(r.root)).toContain(READ_TOKEN_COMMAND);
      // The snippets keep the reference: the token goes into the variable, not into a file.
      expect(snippets(r).join('\n')).not.toContain(shown);
      expect(snippets(r, 'configure')[0]).toContain('${QUADER_TOKEN}');
      await expectAccessible(r.root);
    });

    it('R-UI-122 the token may also publish to the deployment target, only when the reader ticks it and may publish', async () => {
      const r = await open();
      const withPublish = [...r.root.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].find((i) =>
        text(i.parentElement!).includes('It may also publish'),
      )!;
      withPublish.checked = true;
      withPublish.dispatchEvent(new Event('change', { bubbles: true }));
      await settle(r.fixture);
      button(r.root, 'Create a token').click();
      await settle(r.fixture);
      expect(r.tokens.lastRequest?.scope?.repositories).toEqual([
        { repositories: ['web/npm-all'], actions: ['read'] },
        { repositories: ['web/npm-local'], actions: ['read', 'deploy'] },
      ]);
      expect(text(r.root)).toContain('publish to web/npm-local');

      const readOnly = await open({ project: 'web', key: 'npm-remote' });
      expect(text(readOnly.root)).not.toContain('It may also publish');
    });

    it('R-UI-043 the value is in no storage and no URL; another repository or leaving the page discards it', async () => {
      const r = await open();
      button(r.root, 'Create a token').click();
      await settle(r.fixture);
      const token = r.root.querySelector<HTMLInputElement>('.token-value input')!.value;
      expect(storedText()).not.toContain('qdr_');
      expect(location.href).not.toContain('qdr_');

      r.fixture.componentRef.setInput('key', 'npm-local');
      await settle(r.fixture);
      expect(r.root.querySelector('.token-value')).toBeNull();
      expect(r.root.textContent).not.toContain(token);

      button(r.root, 'Create a token').click();
      await settle(r.fixture);
      const second = r.root.querySelector<HTMLInputElement>('.token-value input')!.value;
      const held = r.fixture.componentInstance as unknown as { createdToken: () => string | null };
      expect(held.createdToken()).toBe(second);
      r.fixture.destroy();
      expect(held.createdToken()).toBeNull();
      expect(storedText()).not.toContain(second);
    });

    it('R-UI-042 R-UI-043 a token whose answer arrives after the repository changed is revoked and never shown', async () => {
      const r = await open();
      let release!: () => void;
      const gate = new Promise<void>((resolve) => (release = resolve));
      const create = r.tokens.create.bind(r.tokens);
      r.tokens.create = async (request, key) => {
        await gate;
        return create(request, key);
      };
      button(r.root, 'Create a token').click();
      await settle(r.fixture);
      r.fixture.componentRef.setInput('key', 'npm-local');
      await settle(r.fixture);
      release();
      await settle(r.fixture);
      expect(r.root.querySelector('.token-value')).toBeNull();
      expect(r.tokens.calls.filter((c) => c.startsWith('revoke '))).toHaveLength(1);
      expect(r.tokens.lastRequest?.scope?.repositories?.[0]?.repositories).toEqual(['web/npm-all']);
    });

    it('R-UI-122 a failed creation is shown and no token appears; a double click creates one token', async () => {
      const r = await open();
      r.tokens.failure = new ApiError(403, { code: 'forbidden' }, 'rid');
      button(r.root, 'Create a token').click();
      await settle(r.fixture);
      expect(r.root.querySelector('[role="alert"]')).not.toBeNull();
      expect(r.root.querySelector('.token-value')).toBeNull();

      const twice = await open();
      const create = button(twice.root, 'Create a token');
      create.click();
      create.click();
      await settle(twice.fixture);
      expect(twice.tokens.keys).toHaveLength(1);
    });
  });

  describe('a token written into the snippets (More options)', () => {
    async function paste(r: Opened, value: string) {
      if (!radio(r, 'credentials', 'paste').checked) await choose(r, 'credentials', 'paste');
      type(r.root.querySelector<HTMLInputElement>('#setup-token')!, value);
      await settle(r.fixture);
    }

    it('R-UI-122 More options is closed and holds the way to write a token into the snippets; until one is pasted they show the placeholder', async () => {
      const r = await open();
      const more = r.root.querySelector('quader-disclosure.more-options')!;
      expect(more.querySelector('button[aria-expanded]')?.getAttribute('aria-expanded')).toBe('false');
      expect(radio(r, 'credentials', 'env').checked).toBe(true);
      expect(r.root.querySelector('input[name="credentials"][value="create"]')).toBeNull();
      await choose(r, 'credentials', 'paste');
      expect(r.setup.calls.at(-1)?.query.credentials).toBe('placeholder');
      expect(snippets(r, 'configure')[0]).toContain(':_authToken={{token}}');
      expect(text(r.root)).toContain('Until a token is pasted');
    });

    it('R-UI-016 R-UI-043 the field is a password field with a reveal toggle, autocomplete off and no spellcheck', async () => {
      const r = await open();
      await choose(r, 'credentials', 'paste');
      const field = r.root.querySelector<HTMLInputElement>('#setup-token')!;
      expect(field.type).toBe('password');
      expect(field.getAttribute('autocomplete')).toBe('off');
      expect(field.getAttribute('spellcheck')).toBe('false');
      button(r.root, 'Show').click();
      await settle(r.fixture);
      expect(r.root.querySelector<HTMLInputElement>('#setup-token')!.type).toBe('text');
      await expectAccessible(r.root);
    });

    it('R-UI-016 a valid token is written into the snippets and sent nowhere', async () => {
      const r = await open();
      const token = validToken();
      const before = r.setup.calls.length;
      await paste(r, token);
      expect(snippets(r, 'configure')[0]).toContain(`:_authToken=${token}`);
      expect(text(r.root)).toContain('looks like a Quader token');
      expect(r.tokens.calls).toEqual([]);
      for (const c of [...r.setup.calls.slice(before), ...r.guides.calls]) expect(JSON.stringify(c)).not.toContain(token);
    });

    it('R-UI-016 acceptance: a token with a wrong checksum shows a validation error and sends no request', async () => {
      const r = await open();
      const good = validToken();
      const bad = good.slice(0, -1) + (good.endsWith('a') ? 'b' : 'a');
      const before = r.setup.calls.length;
      await paste(r, bad);
      expect(text(r.root)).toContain('The checksum does not match.');
      expect(r.root.querySelector('#setup-token')?.getAttribute('aria-invalid')).toBe('true');
      expect(snippets(r).join('')).not.toContain(bad);
      expect(r.tokens.calls).toEqual([]);
      for (const c of r.setup.calls.slice(before)) expect(JSON.stringify(c)).not.toContain(bad);
    });

    it('R-UI-016 names what is wrong with a pasted value: prefix, length, characters', async () => {
      const r = await open();
      await paste(r, 'ghp_' + 'a'.repeat(36));
      expect(text(r.root)).toContain('starts with qdr_pat_, qdr_sat_ or qdr_cit_');
      await paste(r, validToken().slice(0, 40));
      expect(text(r.root)).toContain('57 characters');
      await paste(r, 'qdr_pat_' + '-'.repeat(49));
      expect(text(r.root)).toContain('only letters and digits');
    });

    it('R-UI-043 a pasted value is in no storage and is gone after going back to the variable', async () => {
      const r = await open();
      const token = validToken('q');
      await paste(r, token);
      expect(storedText()).not.toContain(token);
      await choose(r, 'credentials', 'env');
      await choose(r, 'credentials', 'paste');
      expect(r.root.querySelector<HTMLInputElement>('#setup-token')!.value).toBe('');
      expect(snippets(r).join('')).not.toContain(token);
    });
  });

  describe('what each reader sees (R-UI-123)', () => {
    it('R-UI-123 a reader who may not publish gets Configure and Install only, and no publishing in the explanation', async () => {
      const r = await open({ project: 'web', key: 'npm-remote' });
      expect(stepHeadings(r)).toEqual(['1Configure', '2Install']);
      expect(r.root.querySelector('[data-section="npm.publish"]')).toBeNull();
      expect(text(r.root)).not.toContain('You cannot publish');
    });

    it('R-UI-123 an anonymous visitor of a public repository gets snippets without credentials, no token line and no More options', async () => {
      const r = await open({ project: 'web', key: 'npm-all' }, { anonymous: true });
      expect(r.setup.calls.at(-1)?.query.credentials).toBe('none');
      expect(text(r.root)).toContain('You are not signed in: the snippets hold no credentials.');
      expect(r.root.querySelector('input[name="credentials"]')).toBeNull();
      expect(text(r.root)).not.toContain('Create a token');
      expect(text(r.root)).not.toContain('More options');
      expect(snippets(r).join('\n')).not.toContain('_authToken');
      expect(stepHeadings(r)).toEqual(['1Configure', '2Install']);
      await expectAccessible(r.root);
    });

    it('R-UI-123 an anonymous visitor of a public image is not asked to sign in to pull, and gets no pull secret', async () => {
      const r = await open({ project: 'web', key: 'oci-local' }, { anonymous: true });
      const configure = r.root.querySelector('section[aria-labelledby="step-configure"]')!;
      expect(text(configure)).toContain('Not needed to pull: this repository can be read without signing in.');
      expect(text(configure)).not.toContain('Sign in once');
      expect(text(configure)).not.toContain('For a Kubernetes cluster');
      // The pull names the image the push names (Q-UI-802), marked as the reader's.
      const pull = r.root.querySelector('section[aria-labelledby="step-install"] pre')!;
      expect(pull.textContent).toContain('/myapp:1.0');
      expect(text(pull.querySelector('.k-value')!)).toBe('myapp:1.0');

      const member = await open({ project: 'web', key: 'oci-local' });
      const fold = member.root.querySelector('section[aria-labelledby="step-configure"] quader-disclosure.second-way')!;
      expect(text(fold.querySelector('button')!)).toContain('For a Kubernetes cluster');
      expect(fold.querySelector('button')?.getAttribute('aria-expanded')).toBe('false');
      expect(fold.querySelector('pre')?.textContent).toContain('kubectl create secret');
    });

    it('R-UI-123 a virtual repository without a deployment target tells who can change it why there is nothing to publish, and links its settings', async () => {
      const r = await open({ project: 'web', key: 'npm-readonly' }, { capabilities: ['repositories:read-config'] });
      expect(stepHeadings(r)).toContain('3Publish');
      expect(text(r.root)).toContain('has no default deployment target');
      const link = [...r.root.querySelectorAll('a')].find((a) => text(a).includes("Open the repository's settings"));
      expect(link?.getAttribute('href')).toBe('/projects/web/repositories/npm-readonly/settings');

      const plain = await open({ project: 'web', key: 'npm-readonly' }, { capabilities: [] });
      expect(stepHeadings(plain)).toEqual(['1Configure', '2Install']);
    });
  });

  describe('the guide of a class, without a repository (R-UI-124)', () => {
    it('R-UI-124 the page shows the class guide\'s snippets with the placeholders of the address and asks for no repository', async () => {
      const r = await open({ format: 'npm', class: 'local' });
      expect(r.setup.calls).toEqual([]);
      expect(r.guides.calls.at(-1)).toEqual({ format: 'npm', cls: 'local', client: 'npm' });
      expect(text(r.root.querySelector('.context')!)).toContain('Local');
      expect(text(r.root)).toContain('The addresses show <project> and <repository>');
      expect(snippets(r, 'configure')[0]).toContain('quader.example.net/api/npm/<project>/<repository>/');
      expect(stepHeadings(r)).toEqual(['1Configure', '2Install', '3Publish']);
      expect(text(r.root)).not.toContain('Create a token');
      await expectAccessible(r.root);
    });

    it('R-UI-124 a format without a guide or a class the format does not have says so', async () => {
      const r = await open({ format: 'npm', class: 'federated' });
      expect(text(r.root)).toContain('This installation has no guide for this package type and kind of repository.');
      expect(r.guides.calls).toEqual([]);
    });
  });

  it('R-UI-016 a failing answer is shown with a retry', async () => {
    const r = await open({ project: 'web', key: 'npm-all' }, { setup: (get) => ((get(ClientSetupApi) as FakeClientSetupApi).failure = new ApiError(503, { code: 'unavailable' }, null)) });
    expect(r.root.querySelector('quader-error-notice')).not.toBeNull();
  });

  it('R-UI-018 R-OCI-030 NuGet and OCI repositories get their clients in the control and their snippets', async () => {
    const nuget = await open({ project: 'web', key: 'nuget-all' });
    expect([...nuget.root.querySelectorAll<HTMLOptionElement>('select#setup-client option')].map((o) => o.value)).toEqual([
      'dotnet',
      'nuget',
      'visual-studio',
    ]);
    expect(snippets(nuget, 'configure').join('\n')).toContain('https://quader.example.net/api/nuget/web/nuget-all/v3/index.json');

    const oci = await open({ project: 'web', key: 'oci-local' });
    expect([...oci.root.querySelectorAll<HTMLOptionElement>('select#setup-client option')].map((o) => o.value)).toEqual(['docker', 'podman']);
    expect(snippets(oci, 'configure')[0]).toContain('--password-stdin');
  });
});

describe('Set up client of a system repository (R-UI-190)', () => {
  it('R-UI-190 the page of upstream/scanner-trivy says that only the image scanning reads through it, and asks for no snippet or guide', async () => {
    const r = await open(
      { project: 'upstream', key: 'scanner-trivy' },
      {
        setup: (get) =>
          (get(RepositoriesApi) as FakeRepositoriesApi).repositories.push({
            key: 'scanner-trivy',
            project: 'upstream',
            class: 'remote',
            format: 'oci',
            description: 'Image scanning database',
          } as never),
      },
    );
    expect(text(r.root.querySelector('[data-testid="setup-system"]')!)).toBe(
      "Quader's image scanning reads its database through this repository. Clients cannot use it.",
    );
    expect(r.root.querySelector('quader-setup-snippet, quader-code-window')).toBeNull();
    expect(r.root.querySelector('quader-address-field')).toBeNull();
    expect(r.setup.calls).toEqual([]);
    expect(r.guides.calls).toEqual([]);
    await expectAccessible(r.root);
  });

  it('R-UI-190 R-REPO-075 the page of upstream/clamav says that the malware scan keeps its signature database there while ClamAV definition updates is off', async () => {
    const r = await open(
      { project: 'upstream', key: 'clamav' },
      {
        setup: (get) =>
          (get(RepositoriesApi) as FakeRepositoriesApi).repositories.push({
            key: 'clamav',
            project: 'upstream',
            class: 'local',
            format: 'generic',
            description: 'ClamAV definitions',
          } as never),
      },
    );
    expect(text(r.root.querySelector('[data-testid="setup-system"]')!)).toBe(
      "Quader's malware scan keeps its signature database in this repository. An administrator can offer it to your ClamAV installations.",
    );
    expect(r.root.querySelector('quader-setup-snippet, quader-code-window')).toBeNull();
    expect(r.setup.calls).toEqual([]);
  });

  it('R-UI-190 while ClamAV definition updates is on, the page of upstream/clamav is its client setup', async () => {
    const r = await open(
      { project: 'upstream', key: 'clamav' },
      {
        setup: (get) => {
          (get(RepositoriesApi) as FakeRepositoriesApi).repositories.push({
            key: 'clamav',
            project: 'upstream',
            class: 'local',
            format: 'generic',
            description: 'ClamAV definitions',
          } as never);
          (get(UpstreamApi) as FakeUpstreamApi).set('clamav', { clients: { enabled: true, address: CLAMAV_ENDPOINT, certificateAddresses: [] }, visibility: 'public' });
        },
      },
    );
    expect(r.root.querySelector('[data-testid="setup-system"]')).toBeNull();
    expect(r.setup.calls.map((c) => c.repository)).toContain('upstream/clamav');
  });
});
