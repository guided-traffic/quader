import { PackageDocEntry } from '../../api/packages-api';
import { CHECK_HOST_REACHABLE, bareEntry } from '../../api/testing/fakes-ansible-docs';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { AnsibleDocEntry } from './ansible-doc-entry';

async function show(entry: PackageDocEntry = CHECK_HOST_REACHABLE) {
  const r = await renderScreen(AnsibleDocEntry, { inputs: { entry } });
  return { ...r, rows: () => [...r.root.querySelectorAll<HTMLTableRowElement>('tbody tr')] };
}

const headings = (root: Element) => [...root.querySelectorAll('section h3')].map((h) => text(h));

describe('An entry page of the documentation', () => {
  beforeEach(() => vi.spyOn(console, 'warn').mockImplementation(() => undefined));
  afterEach(() => vi.restoreAllMocks());

  it('R-UI-061 shows the head and the sections in the order ansible-doc prints them, each only with content', async () => {
    const r = await show();
    expect(text(r.root.querySelector('.doc-title')!)).toContain('hans_fischer.common.check_host_reachable');
    expect(r.root.querySelector('.doc-title quader-copy-button button')?.getAttribute('aria-label')).toBe('Copy the full name');
    expect(text(r.root.querySelector('.doc-short')!)).toBe('Checks whether a host answers');
    expect(text(r.root.querySelector('.doc-facts')!)).toContain('added in 1.1.0');
    expect(headings(r.root)).toEqual([
      'Synopsis',
      'Parameters',
      'Notes',
      'Requirements',
      'Attributes',
      'See also',
      'Examples',
      'Return values',
      'Authors',
    ]);
    // The prose is the server's HTML: the code element and the link are there.
    expect(r.root.querySelector('#doc-synopsis-heading ~ quader-doc-prose code')?.textContent).toBe('host');
    await expectAccessible(r.root);
  });

  it('R-UI-061 a section without content is not drawn', async () => {
    const r = await show({ type: 'module', name: 'm', fullName: 'a.b.m', hasDocs: true, source: 'generated', description: ['Does it.'] });
    expect(headings(r.root)).toEqual(['Synopsis']);
  });

  it('R-UI-061 the parameters are a table of the name and the description, a text badge for required and the aliases under the name', async () => {
    const r = await show();
    expect([...r.root.querySelectorAll('#doc-parameters-heading ~ quader-data-table thead th')].map((t) => text(t))).toEqual([
      'Name',
      'Description',
    ]);
    const host = r.rows()[0]!;
    expect(text(host)).toContain('host');
    expect(text(host)).toContain('aliases: hostname');
    expect(host.querySelector('.badge')?.textContent?.trim()).toBe('Required');
    // The type and the badge stand under the name, as on the documentation site; the choices one by one, the default bold among them.
    expect(text(host.cells[0]!)).toContain('str');
    const port = r.rows()[1]!;
    expect([...port.querySelectorAll('.choices code')].map((c) => c.textContent)).toEqual(['22', '443']);
    expect(port.querySelector('.choices .is-default')?.textContent).toBe('22');
    expect(text(port.cells[1]!)).toContain('(default)');
  });

  it('R-UI-061 parameters nest three levels deep, each level 16 px further in, with a label for the screen reader', async () => {
    const r = await show();
    const names = r.rows().map((tr) => [tr.getAttribute('data-depth'), tr.querySelector('code')?.textContent]);
    expect(names.slice(2, 6)).toEqual([
      ['0', 'retry'],
      ['1', 'count'],
      ['1', 'backoff'],
      ['2', 'seconds'],
    ]);
    const deepest = r.rows().find((tr) => tr.getAttribute('data-depth') === '2')!;
    expect(deepest.querySelector('th')!.style.paddingInlineStart).toContain('32px');
    expect(text(deepest.querySelector('th .visually-hidden')!)).toBe('suboptions of backoff:');
  });

  it('R-UI-061 a parameter with suboptions folds and unfolds, and a new page starts open', async () => {
    const r = await show();
    const count = () => r.rows().length;
    const all = count();
    const retry = r.rows().find((tr) => tr.querySelector('code')?.textContent === 'retry')!;
    const fold = retry.querySelector<HTMLButtonElement>('button.fold')!;
    expect(fold.getAttribute('aria-expanded')).toBe('true');
    expect(fold.getAttribute('aria-label')).toBe('Hide the suboptions of retry');
    fold.click();
    await settle(r.fixture);
    expect(count()).toBe(all - 4);
    const again = r.rows().find((tr) => tr.querySelector('code')?.textContent === 'retry')!.querySelector('button.fold')!;
    expect(again.getAttribute('aria-expanded')).toBe('false');
    expect(again.getAttribute('aria-label')).toBe('Show the suboptions of retry');
    r.fixture.componentRef.setInput('entry', { ...CHECK_HOST_REACHABLE });
    await settle(r.fixture);
    expect(count()).toBe(all);
  });

  it('R-UI-061 a parameter named <img src=x onerror=1> and a default of <script> appear as that text, never as markup', async () => {
    const r = await show();
    const hostile = r.rows().find((tr) => tr.querySelector('code')?.textContent === '<img src=x onerror=1>')!;
    expect(hostile).toBeDefined();
    expect(text(hostile.cells[1]!)).toContain('Default <script>alert(1)</script>');
    expect(r.root.querySelector('img')).toBeNull();
    expect(r.root.querySelector('script')).toBeNull();
    expect(r.root.querySelectorAll('[onerror]')).toHaveLength(0);
  });

  it('R-UI-061 prose from the server is bound as sanitized HTML: a script element and an event handler do not survive', async () => {
    const r = await show({
      ...CHECK_HOST_REACHABLE,
      description: ['<p>ok <code>x</code></p><script>window.__doc = 1</script><img src="https://x/y.png" onerror="window.__doc = 2">'],
    });
    const prose = r.root.querySelector('#doc-synopsis-heading ~ quader-doc-prose')!;
    expect(prose.querySelector('script')).toBeNull();
    expect(prose.querySelector('[onerror]')).toBeNull();
    expect(prose.querySelector('code')?.textContent).toBe('x');
    expect((window as unknown as Record<string, unknown>)['__doc']).toBeUndefined();
  });

  it('R-UI-061 the examples are a code block with a copy button, the return values a table with a sample as a block when it is long', async () => {
    const r = await show();
    const code = r.root.querySelector('#doc-examples-heading ~ .code-wrap pre.code-block')!;
    expect(code.textContent).toContain('hans_fischer.common.check_host_reachable:');
    expect(code.getAttribute('tabindex')).toBe('0');
    expect(r.root.querySelector('#doc-examples-heading ~ .code-wrap quader-copy-button button')?.getAttribute('aria-label')).toBe('Copy the examples');
    const returns = [...r.root.querySelectorAll<HTMLTableRowElement>('#doc-returns-heading ~ quader-data-table tbody tr')];
    expect(returns.map((tr) => tr.querySelector('code')?.textContent)).toEqual(['reachable', 'details', 'rtt_ms']);
    // `true` stays inline, an object is a block with the copy button.
    expect((returns[0]!.cells[1]!.querySelector('.param-sample code') as HTMLElement).textContent).toBe('true');
    expect(returns[1]!.cells[1]!.querySelector('pre.code-block')?.textContent).toContain('"rtt_ms": 12');
  });

  it('R-UI-061 a see-also link is a link only for http and https', async () => {
    const r = await show({
      ...CHECK_HOST_REACHABLE,
      seealso: [
        { description: 'ok', link: 'https://example.com/reference' },
        { description: 'bad', link: 'javascript:alert(1)' },
      ],
    });
    const links = [...r.root.querySelectorAll<HTMLAnchorElement>('#doc-seealso-heading ~ ul a')];
    expect(links.map((a) => a.getAttribute('href'))).toEqual(['https://example.com/reference']);
    expect(links[0]?.getAttribute('rel')).toBe('nofollow noopener noreferrer');
    expect(r.root.innerHTML).not.toContain('href="javascript:');
  });

  it('R-UI-061 a deprecated plugin carries a warn badge and its notice', async () => {
    const r = await show({
      ...CHECK_HOST_REACHABLE,
      deprecated: { why: 'Replaced by <code>other</code>.', removedIn: '3.0.0', alternative: 'Use other.' },
    });
    const badge = r.root.querySelector('.doc-facts .badge');
    expect(badge?.classList.contains('warn')).toBe(true);
    expect(text(badge!)).toContain('Deprecated');
    const note = text(r.root.querySelector('.doc-deprecation')!);
    expect(note).toContain('Replaced by other.');
    expect(note).toContain('Removed in 3.0.0');
  });

  it('R-UI-061 an entry without documentation shows the head and one line, generated or taken from the upstream, and no empty section', async () => {
    const generated = await show(bareEntry('filter', 'is_cidr'));
    expect(text(generated.root.querySelector('[data-testid="doc-no-docs"]')!)).toBe('This plugin has no documentation in its source.');
    expect(generated.root.querySelectorAll('section')).toHaveLength(0);
    expect(text(generated.root)).toContain('hans_fischer.common.is_cidr');
    const upstream = await show(bareEntry('filter', 'is_cidr', 'upstream'));
    expect(text(upstream.root.querySelector('[data-testid="doc-no-docs"]')!)).toBe('The upstream sent no documentation for this plugin.');
  });

  it('R-UI-061 a role shows its README and no Parameters section', async () => {
    const r = await show({
      type: 'role',
      name: 'web',
      fullName: 'hans_fischer.common.web',
      hasDocs: true,
      source: 'generated',
      readmeHtml: '<h1>web</h1><p>Installs the web server.</p>',
    });
    expect(r.root.querySelector('quader-package-readme .readme h1')?.textContent).toBe('web');
    expect(headings(r.root)).toEqual([]);
    expect(r.root.querySelector('#doc-parameters')).toBeNull();
  });

  it('R-UI-061 a role of an upstream that sends a README and no doc_strings (hasDocs false) still shows its README', async () => {
    const r = await show({
      type: 'role',
      name: 'web',
      fullName: 'hans_fischer.common.web',
      hasDocs: false,
      source: 'upstream',
      readmeHtml: '<h1>web</h1><p>Installs the web server.</p>',
    });
    expect(r.root.querySelector('quader-package-readme .readme h1')?.textContent).toBe('web');
    expect(r.root.querySelector('[data-testid="doc-no-docs"]')).toBeNull();
  });

  it('R-ANS-048 every option and return value has its anchor, an alias one of its own, and a button copies its link', async () => {
    const r = await show();
    const ids = r.rows().map((tr) => tr.id);
    expect(ids).toContain('parameter-host');
    expect(ids).toContain('parameter-retry/backoff/seconds');
    expect(ids).toContain('return-details/rtt_ms');
    expect(r.root.querySelector('#parameter-host .alias-anchor')?.id).toBe('parameter-hostname');
    const copy = r.root.querySelector('#parameter-retry\\/backoff\\/seconds .permalink button');
    expect(copy?.getAttribute('aria-label')).toBe('Copy the link to seconds');
  });

  it('R-ANS-048 the target of the address is marked, and a folded row above it opens', async () => {
    const r = await show();
    const retry = r.rows().find((tr) => tr.id === 'parameter-retry')!;
    retry.querySelector<HTMLButtonElement>('button.fold')!.click();
    await settle(r.fixture);
    expect(r.root.querySelector('#parameter-retry\\/count')).toBeNull();
    r.fixture.componentRef.setInput('target', 'parameter-retry/count');
    await settle(r.fixture);
    await settle(r.fixture);
    expect(r.root.querySelector('#parameter-retry\\/count')?.classList.contains('is-target')).toBe(true);
  });

  it('R-ANS-048 a see-also entry of the tree links to its page in place', async () => {
    const r = await show({ ...CHECK_HOST_REACHABLE, seealso: [{ module: 'hans_fischer.common.wait_for_port', entry: 'module/wait_for_port' }, { module: 'ansible.builtin.ping' }] });
    const link = r.root.querySelector<HTMLAnchorElement>('[data-testid="doc-seealso-entry"]')!;
    expect(link.getAttribute('href')).toContain('doc=module%2Fwait_for_port');
    expect(text(r.root.querySelector('#doc-seealso')!)).toContain('ansible.builtin.ping');
    expect(r.root.querySelectorAll('#doc-seealso a')).toHaveLength(1);
  });

  it('R-ANS-049 an option shows how it is configured, and "added in" names the collection', async () => {
    const r = await show({
      ...CHECK_HOST_REACHABLE,
      type: 'callback',
      versionAddedCollection: 'hans_fischer.common',
      options: [
        {
          name: 'sort_order',
          type: 'str',
          versionAdded: '1.2.0',
          versionAddedCollection: 'hans_fischer.common',
          configuration: {
            ini: [{ section: 'callback_timer', key: 'sort_order' }],
            env: [{ name: 'TIMER_SORT_ORDER' }],
            vars: [{ name: 'timer_sort_order' }],
          },
        },
      ],
    });
    expect(text(r.root.querySelector('[data-testid="doc-added-in"]')!)).toBe('added in hans_fischer.common 1.1.0');
    const config = text(r.root.querySelector('[data-testid="doc-config"]')!);
    expect(config).toContain('INI entry [callback_timer] sort_order');
    expect(config).toContain('Environment variable TIMER_SORT_ORDER');
    expect(config).toContain('Variable timer_sort_order');
    expect(text(r.rows()[0]!)).toContain('added in hans_fischer.common 1.2.0');
  });

  it('R-ANS-050 a lookup shows its terms apart from its keyword parameters, a filter its input and positional parameters', async () => {
    const opts = [{ name: '_terms', type: 'list' }, { name: 'qtype', type: 'str' }, { name: 'flat', type: 'int' }];
    const lookup = await show({ ...CHECK_HOST_REACHABLE, type: 'lookup', options: opts });
    expect(headings(lookup.root).slice(0, 3)).toEqual(['Synopsis', 'Terms', 'Keyword parameters']);
    const filter = await show({
      ...CHECK_HOST_REACHABLE,
      type: 'filter',
      positional: ['_input', 'flat'],
      options: [{ name: '_input', type: 'raw' }, ...opts.slice(1)],
    });
    expect(headings(filter.root).slice(0, 4)).toEqual(['Synopsis', 'Input', 'Positional parameters', 'Keyword parameters']);
    expect(filter.root.querySelector('#doc-positional tbody tr')?.id).toBe('parameter-flat');
  });

  it('R-ANS-050 the page lists its sections, and the examples are highlighted YAML whose text is the source', async () => {
    const r = await show();
    const contents = [...r.root.querySelectorAll('[data-testid="doc-contents"] a')];
    expect(contents.map((a) => text(a))).toEqual(headings(r.root));
    expect(contents[1]?.getAttribute('href')).toContain('#doc-parameters');
    const pre = r.root.querySelector('[data-testid="doc-examples"]')!;
    expect(pre.textContent).toBe(CHECK_HOST_REACHABLE.examples);
    expect([...pre.querySelectorAll('.y-key')].map((s) => s.textContent)).toEqual(['name', 'hans_fischer.common.check_host_reachable', 'host']);
    await expectAccessible(r.root);
  });

  it('R-UI-061 names the documentation fragments that were not merged', async () => {
    const r = await show({ ...CHECK_HOST_REACHABLE, extendsFragments: ['community.general.attributes'] });
    expect(text(r.root.querySelector('[data-testid="doc-fragments"]')!)).toContain('community.general.attributes');
  });
});
