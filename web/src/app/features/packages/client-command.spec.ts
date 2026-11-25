import { ClientSetupApi } from '../../api/packages-api';
import { ApiError } from '../../api/problem';
import { FakeClientSetupApi } from '../../api/testing/fakes-packages';
import { button, expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { ClientCommand } from './client-command';

const inputs = (name = '@team-a/ui', client = 'npm') => ({ format: 'npm', name, version: '1.4.0', repository: 'web/npm-all', client });
const shown = (root: HTMLElement) => root.querySelector<HTMLInputElement>('quader-address-field input')?.value ?? root.querySelector('pre')?.textContent;

describe('The command of one client for one version', () => {
  it('R-UI-009 is the install snippet of the server for the package and version, asked without credentials', async () => {
    const r = await renderScreen(ClientCommand, { inputs: inputs() });
    const fake = r.get(ClientSetupApi) as FakeClientSetupApi;
    expect(shown(r.root)).toBe('npm install @team-a/ui@1.4.0');
    expect(fake.calls).toHaveLength(1);
    expect(fake.calls[0]?.query).toMatchObject({ client: 'npm', package: '@team-a/ui', version: '1.4.0', credentials: 'none' });
    expect(text(r.root)).not.toContain('QUADER_TOKEN');
    await expectAccessible(r.root);
  });

  it('R-UI-009 R-API-030 a name the shell would interpret arrives quoted from the server and is shown as it is', async () => {
    const r = await renderScreen(ClientCommand, { inputs: inputs('a(b)') });
    expect(shown(r.root)).toBe(`npm install 'a(b)@1.4.0'`);
  });

  it('R-UI-009 R-UI-215 a command of more than three lines is cut behind an expander, and opened it has no cap of its own', async () => {
    const lines = ['yarn add a', 'yarn add b', 'yarn add c', 'yarn add d', 'yarn add e'].join('\n');
    const r = await renderScreen(ClientCommand, {
      inputs: inputs(),
      setup: (get) => {
        const fake = get(ClientSetupApi) as FakeClientSetupApi;
        const real = fake.get.bind(fake);
        fake.get = async (repository, query) => {
          const answer = await real(repository, query);
          return { ...answer, snippets: answer.snippets.map((s) => (s.tab === 'install' ? { ...s, content: lines } : s)) };
        };
      },
    });
    const pre = r.root.querySelector<HTMLElement>('pre')!;
    expect(pre.style.getPropertyValue('--lines')).toBe('3');
    const expander = button(r.root, 'Show the whole command');
    expect(expander.getAttribute('aria-expanded')).toBe('false');
    expander.click();
    await settle(r.fixture);
    expect(pre.style.getPropertyValue('--lines')).toBe('');
    expect(pre.style.maxBlockSize).toBe('');
    expect(shown(r.root)).toBe(lines);
    await expectAccessible(r.root);
  });

  it('R-UI-216 a failing setup answer is shown with a retry, not as an empty field', async () => {
    const r = await renderScreen(ClientCommand, {
      inputs: inputs(),
      setup: (get) => ((get(ClientSetupApi) as FakeClientSetupApi).failure = new ApiError(503, { code: 'unavailable' }, null)),
    });
    expect(text(r.root)).toContain('temporarily unavailable');
    button(r.root, 'Retry').click();
    await settle(r.fixture);
    expect(shown(r.root)).toBe('npm install @team-a/ui@1.4.0');
  });
});
