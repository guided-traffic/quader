import { CodeWindow } from './code-window';
import { expectAccessible, renderScreen, text } from '../../testing/screen';
import { formatAccent, formatGlyph } from '../format-accent';

const npmrc = 'registry=https://q/api/npm/w/r/\n//q/api/npm/w/r/:_authToken=${QUADER_TOKEN}\n';

describe('code window (R-UI-121, Q-UI-801)', () => {
  it('R-UI-121 a file is an editor window: its name on the tab, numbered lines, Download and Copy in the bar', async () => {
    const r = await renderScreen(CodeWindow, {
      inputs: { content: npmrc, language: 'ini', fileName: '.npmrc', marks: [{ text: '${QUADER_TOKEN}', kind: 'token' }], snippetId: 'npm.configure.npmrc' },
    });
    const host = r.root.querySelector('quader-code-window') ?? r.fixture.nativeElement;
    expect(host.classList).toContain('editor');
    expect(text(r.root.querySelector('.tab-title')!)).toBe('.npmrc');
    expect(r.root.querySelector('figure')?.getAttribute('aria-label')).toBe('File .npmrc');
    expect(r.root.querySelector('.bar button.download')?.getAttribute('aria-label')).toBe('Download .npmrc');
    expect(r.root.querySelector('.bar quader-copy-button button')?.getAttribute('aria-label')).toBe('Copy snippet');
    expect(r.root.querySelector('pre')?.getAttribute('data-snippet')).toBe('npm.configure.npmrc');
    expect([...r.root.querySelectorAll('.line')].map((l) => l.getAttribute('data-prompt'))).toEqual([null, null]);
    await expectAccessible(r.root);
  });

  it('R-UI-121 a command is a terminal window: the prompt of its command line, no Download', async () => {
    const r = await renderScreen(CodeWindow, { inputs: { content: 'a `\n  -b\n', language: 'powershell', fileName: null } });
    const host = r.root.querySelector('quader-code-window') ?? r.fixture.nativeElement;
    expect(host.classList).toContain('terminal');
    expect(text(r.root.querySelector('.tab-title')!)).toBe('PowerShell');
    expect(r.root.querySelector('button.download')).toBeNull();
    expect([...r.root.querySelectorAll('.line')].map((l) => l.getAttribute('data-prompt'))).toEqual(['PS>', null]);
  });

  it('R-UI-121 R-UI-043 the text is the snippet\'s exactly: what Copy copies, what a selection takes, with the marks in place', async () => {
    const writes: string[] = [];
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (t: string) => void writes.push(t) } });
    const r = await renderScreen(CodeWindow, {
      inputs: { content: npmrc, language: 'ini', fileName: '.npmrc', marks: [{ text: '${QUADER_TOKEN}', kind: 'token' }] },
    });
    expect(r.root.querySelector('pre')!.textContent).toBe(npmrc);
    const token = r.root.querySelector('.k-token')!;
    expect(token.textContent).toBe('${QUADER_TOKEN}');
    expect(token.getAttribute('title')).toBe('Your token goes here');
    r.root.querySelector<HTMLButtonElement>('quader-copy-button button')!.click();
    await r.fixture.whenStable();
    expect(writes).toEqual([npmrc]);
  });

  it('R-UI-121 what the reader replaces is marked by more than colour: its own element with a title', async () => {
    const r = await renderScreen(CodeWindow, {
      inputs: { content: 'docker pull q/w/r/myapp:1.0\n', language: 'shell', fileName: null, marks: [{ text: 'myapp:1.0', kind: 'value' }] },
    });
    const mark = r.root.querySelector('.k-value')!;
    expect(mark.textContent).toBe('myapp:1.0');
    expect(mark.classList).toContain('keep');
    expect(mark.getAttribute('title')).toBe('Replace with your own value');
  });
});

describe('format accent (Q-UI-803)', () => {
  it('R-UI-121 each format of the catalog has its accent pair, a format without one the neutral pair, and an image its own glyph', () => {
    for (const f of ['npm', 'oci', 'nuget', 'deb', 'ansible', 'generic']) expect(formatAccent(f)).toBe(`var(--quader-format-accent-${f})`);
    expect(formatAccent('cargo')).toBe('var(--quader-format-accent)');
    expect(formatAccent(null)).toBe('var(--quader-format-accent)');
    expect(formatGlyph('oci')).toBe('layers');
    expect(formatGlyph('npm')).toBe('parcel');
  });
});
