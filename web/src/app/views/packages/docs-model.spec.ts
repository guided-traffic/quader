import { PackageDocEntry } from '../../api/packages-api';
import { DOCS_SEARCH_FIXTURE } from '../../api/testing/docs-search-fixture';
import { commonFixture, docTree } from '../../api/testing/fakes-ansible-docs';
import {
  README_DOC,
  addedText,
  allBranches,
  configLines,
  branchesOf,
  docEntryMatches,
  docNodes,
  docTerms,
  entrySections,
  firstDoc,
  holdsDoc,
  narrowDocTree,
  optionRows,
  parameterGroups,
  parseDoc,
  returnRows,
  valueText,
  visibleRows,
} from './docs-model';

describe('The model of the documentation screens', () => {
  it('R-UI-060 reads the address of a page: the README, a file, an entry; anything else is no address', () => {
    expect(parseDoc('README.md')).toEqual({ kind: 'file', name: 'README.md' });
    expect(parseDoc('file/install.md')).toEqual({ kind: 'file', name: 'install.md' });
    expect(parseDoc('module/check_host_reachable')).toEqual({ kind: 'entry', type: 'module', name: 'check_host_reachable' });
    expect(parseDoc('filter/is_cidr')).toEqual({ kind: 'entry', type: 'filter', name: 'is_cidr' });
    expect(parseDoc('role/web')).toEqual({ kind: 'entry', type: 'role', name: 'web' });
    for (const bad of [undefined, null, '', 'module', 'module/', '/x', 'install.md']) expect(parseDoc(bad), String(bad)).toBeNull();
  });

  it('R-UI-060 an unknown page is told from the whole tree and the first page is the README, else a file, else an entry', () => {
    const tree = docTree(commonFixture());
    expect(holdsDoc(tree, parseDoc('module/check_host_reachable'))).toBe(true);
    expect(holdsDoc(tree, parseDoc('module/gone'))).toBe(false);
    expect(holdsDoc(tree, parseDoc('file/install.md'))).toBe(true);
    expect(firstDoc(tree)).toBe(README_DOC);
    expect(firstDoc(docTree(commonFixture({ files: [] })))).toBe('module/check_host_reachable');
    expect(firstDoc(docTree(commonFixture({ files: [], entries: [] })))).toBeNull();
  });

  it('R-UI-060 the tree has Documentation, Modules (8) and Plugins (24) with types below the plugins, and keeps the counts under a search', () => {
    const all = docTree(commonFixture());
    const nodes = docNodes(all, all, false);
    expect(nodes.map((n) => [n.label, n.count])).toEqual([
      ['Documentation', 2],
      ['Modules', 8],
      ['Plugins', 24],
    ]);
    expect(nodes[0]?.children.map((c) => [c.label, c.doc])).toEqual([
      ['hans_fischer.common', 'README.md'],
      ['install', 'file/install.md'],
    ]);
    // Modules list their entries; Plugins list their types, each with its count.
    expect(nodes[1]?.children.every((c) => c.kind === 'entry')).toBe(true);
    expect(nodes[2]?.children.map((c) => [c.label, c.count])).toEqual([
      ['filter', 8],
      ['module_utils', 16],
    ]);
    const narrowed = docTree(commonFixture(), 'host');
    const found = docNodes(narrowed, all, true);
    expect(found.find((n) => n.label === 'Modules')?.count).toBe(8);
    expect(found.find((n) => n.label === 'Plugins')?.count).toBe(24);
    const entries = found.flatMap((n) => allLeaves(n)).filter((n) => n.kind === 'entry');
    expect(entries.map((e) => e.label)).toEqual(['check_host_reachable', 'collect_facts', 'cidr_hosts', 'first_host', 'last_host']);
  });

  it('R-UI-060 an entry without documentation is muted, a group with no match is left out of a search, and the branches above a page are found', () => {
    const all = docTree(commonFixture());
    const nodes = docNodes(all, all, false);
    const filter = nodes[2]!.children[0]!.children[0]!;
    expect(filter.muted).toBe(true);
    expect(branchesOf(nodes, 'filter/is_cidr')).toEqual(['group:plugin', 'type:filter']);
    expect(branchesOf(nodes, 'module/check_host_reachable')).toEqual(['group:module']);
    expect(branchesOf(nodes, null)).toEqual([]);
    const none = docTree(commonFixture(), 'zzzz');
    expect(docNodes(none, all, true)).toEqual([]);
    expect(allBranches(nodes)).toEqual(['group:files', 'group:module', 'group:plugin', 'type:filter', 'type:module_utils']);
  });

  it('R-UI-061 shows a typed value as text: a number as 1000, a boolean as false, a string as written, a structure as JSON', () => {
    expect(valueText(1000)).toBe('1000');
    expect(valueText(false)).toBe('false');
    expect(valueText('<b>x</b>')).toBe('<b>x</b>');
    expect(valueText({ a: [1] })).toBe('{"a":[1]}');
    expect(valueText(null)).toBe('');
    expect(valueText(undefined)).toBe('');
  });

  it('R-UI-061 flattens the parameters with their depth and hides what a folded parameter holds', () => {
    const rows = optionRows([
      {
        name: 'a',
        type: 'dict',
        suboptions: [
          { name: 'b', type: 'list', elements: 'str', suboptions: [{ name: 'c', default: false }] },
          { name: 'd' },
        ],
      },
      { name: 'e', required: true, default: 1000, choices: [1, 'x'], aliases: ['f', 'g'] },
    ]);
    expect(rows.map((r) => [r.name, r.depth, r.parent, r.children])).toEqual([
      ['a', 0, '', 2],
      ['b', 1, 'a', 1],
      ['c', 2, 'b', 0],
      ['d', 1, 'a', 0],
      ['e', 0, '', 0],
    ]);
    expect(rows[1]?.type).toBe('list / str');
    expect(rows[2]?.defaultText).toBe('false');
    expect(rows[2]?.hasDefault).toBe(true);
    expect(rows[4]).toMatchObject({ required: true, defaultText: '1000', choices: ['1', 'x'], aliases: 'f, g' });
    expect(visibleRows(rows, new Set()).map((r) => r.name)).toEqual(['a', 'b', 'c', 'd', 'e']);
    expect(visibleRows(rows, new Set([rows[1]!.id])).map((r) => r.name)).toEqual(['a', 'b', 'd', 'e']);
    expect(visibleRows(rows, new Set([rows[0]!.id])).map((r) => r.name)).toEqual(['a', 'e']);
  });

  it('R-UI-061 flattens the return values with what they contain and shows a structure as indented JSON', () => {
    const rows = returnRows([
      { name: 'x', type: 'dict', returned: 'always', sample: { k: 1 }, contains: [{ name: 'k', sample: 1 }] },
      { name: 'y', sample: 'text' },
    ]);
    expect(rows.map((r) => [r.name, r.depth])).toEqual([
      ['x', 0],
      ['k', 1],
      ['y', 0],
    ]);
    expect(rows[0]?.sample).toBe('{\n  "k": 1\n}');
    expect(rows[1]?.sample).toBe('1');
    expect(rows[2]?.sample).toBe('text');
  });

  it('R-ANS-048 a row\'s anchor is parameter- or return- and the path of names; an alias has its own', () => {
    const rows = optionRows([{ name: 'opts', aliases: ['o'], suboptions: [{ name: 'mode', aliases: ['m'] }] }]);
    expect(rows.map((r) => [r.anchor, r.aliasAnchors])).toEqual([
      ['parameter-opts', ['parameter-o']],
      ['parameter-opts/mode', ['parameter-opts/m']],
    ]);
    expect(returnRows([{ name: 'dev', contains: [{ name: 'name' }] }]).map((r) => r.anchor)).toEqual(['return-dev', 'return-dev/name']);
  });

  it('R-ANS-049 "added in" names the collection when the source does, and the configuration reads in the order of the site', () => {
    expect(addedText('1.2.0', 'acme.tools')).toBe('acme.tools 1.2.0');
    expect(addedText('1.2.0', undefined)).toBe('1.2.0');
    expect(addedText(undefined, 'acme.tools')).toBe('');
    expect(
      configLines({
        cli: [{ name: 'x', option: '--x' }],
        keyword: [{ name: 'k' }],
        vars: [{ name: 'v' }],
        env: [{ name: 'E' }],
        ini: [{ section: 's', key: 'k' }, { key: 'only' }],
      }),
    ).toEqual([
      { kind: 'ini', text: '[s] k' },
      { kind: 'ini', text: 'only' },
      { kind: 'env', text: 'E' },
      { kind: 'vars', text: 'v' },
      { kind: 'keyword', text: 'k' },
      { kind: 'cli', text: '--x' },
    ]);
    expect(configLines(undefined)).toEqual([]);
  });

  it('R-ANS-053 the changelog is the last file of Documentation, a page of the tree, and the first page where no file is', () => {
    const changelog = { name: 'changelog', source: 'changelogs/changelog.yaml', format: 'markdown', truncated: false } as const;
    const tree = { ...docTree(commonFixture()), changelog };
    const docs = docNodes(tree, tree, false)[0];
    expect([docs?.label, docs?.count]).toEqual(['Documentation', 3]);
    expect(docs?.children.map((c) => [c.label, c.doc, c.tooltip]).at(-1)).toEqual(['Changelog', 'file/changelog', 'changelogs/changelog.yaml']);
    expect(holdsDoc(tree, parseDoc('file/changelog'))).toBe(true);
    expect(holdsDoc(docTree(commonFixture()), parseDoc('file/changelog'))).toBe(false);
    const alone = { ...docTree(commonFixture({ files: [] })), changelog };
    expect(firstDoc(alone)).toBe('file/changelog');
    expect(docNodes(alone, alone, false)[0]?.children.map((c) => c.label)).toEqual(['Changelog']);
  });

  it('R-ANS-052 a role with argument specs has a group per entry point, whose rows have the anchors of that entry point', () => {
    const entry: PackageDocEntry = {
      type: 'role',
      name: 'agent',
      fullName: 'acme.platform.agent',
      hasDocs: true,
      source: 'generated',
      readmeHtml: '<p>r</p>',
      entryPoints: [
        { name: 'main', options: [{ name: 'tls', suboptions: [{ name: 'cert', aliases: ['crt'] }] }] },
        { name: 'upgrade', options: [{ name: 'tls' }] },
      ],
    };
    const groups = parameterGroups(entry);
    expect(groups.map((g) => [g.id, g.title, g.anchorPrefix])).toEqual([
      ['entry-main', 'Entry point main', 'main--'],
      ['entry-upgrade', 'Entry point upgrade', 'upgrade--'],
    ]);
    const main = optionRows(groups[0]?.options, groups[0]?.anchorPrefix);
    expect(main.map((r) => [r.id, r.anchor, r.aliasAnchors])).toEqual([
      ['main--/tls', 'parameter-main--tls', []],
      ['main--/tls/cert', 'parameter-main--tls/cert', ['parameter-main--tls/crt']],
    ]);
    // One option name in two entry points is two rows: folding one leaves the other.
    expect(optionRows(groups[1]?.options, groups[1]?.anchorPrefix)[0]?.id).toBe('upgrade--/tls');
    expect(entrySections(entry, groups).map((s) => s.id)).toEqual(['doc-entry-main', 'doc-entry-upgrade', 'doc-readme']);
    expect(entrySections({ ...entry, hasDocs: false, entryPoints: undefined }, [])).toEqual([]);
    // A list default marks each of its elements among the choices, as the site marks them.
    expect(optionRows([{ name: 'protocols', choices: ['TLSv1.2', 'TLSv1.3'], default: ['TLSv1.3'] }])[0]?.defaults).toEqual(['TLSv1.3']);
    expect(optionRows([{ name: 'port', default: 22 }])[0]?.defaults).toEqual(['22']);
  });
});

describe('The search of a version at a Bastion (bastion.md R-BAS-124)', () => {
  // The fixture is the main installation's own answers, written by an integration test against PostgreSQL
  // (test/integration/ansiblelocal/catalogue_docs_test.go): the browser's narrowing must answer every text the same.
  it('R-BAS-124 the browser narrows the tree as the main installation searches it, on the fixture of a real installation', () => {
    expect(DOCS_SEARCH_FIXTURE.cases.length).toBeGreaterThan(10);
    for (const c of DOCS_SEARCH_FIXTURE.cases) {
      const narrowed = narrowDocTree(DOCS_SEARCH_FIXTURE.tree, c.q);
      expect(narrowed, c.q).not.toBeNull();
      const entries = (narrowed?.groups ?? []).flatMap((g) => g.types.flatMap((t) => t.entries.map((e) => `${e.type}/${e.name}`)));
      const files = (narrowed?.files ?? []).map((f) => f.name);
      expect({ q: c.q, entries, files, changelog: !!narrowed?.changelog, matchCount: narrowed?.matchCount }).toEqual({ ...c });
      // The counts are those before the search (R-ANS-036).
      expect(narrowed?.groups?.map((g) => g.count)).toEqual(DOCS_SEARCH_FIXTURE.tree.groups?.map((g) => g.count));
    }
  });

  it('R-BAS-124 a text shorter than two characters is refused, the terms are bounded, and the changelog stays for its own name', () => {
    expect(narrowDocTree(DOCS_SEARCH_FIXTURE.tree, ' a ')).toBeNull();
    expect(docTerms('a\u0000b  c\td')).toEqual(['ab', 'c', 'd']);
    expect(docTerms('1 2 3 4 5 6 7 8 9 10')).toHaveLength(8);
    expect(docTerms('x'.repeat(70))[0]).toHaveLength(64);
    const changelog = { name: 'changelog' as const, source: 'CHANGELOG.md', format: 'markdown' as const, truncated: false };
    const withChangelog = { ...DOCS_SEARCH_FIXTURE.tree, changelog };
    expect(narrowDocTree(withChangelog, 'CHANGE')?.changelog).toEqual(changelog);
    expect(narrowDocTree(withChangelog, 'host')?.changelog).toBeUndefined();
    // An entry the catalogue gives no words is found by its name alone.
    expect(docEntryMatches('host_check', undefined, ['CHECK'])).toBe(true);
    expect(docEntryMatches('host_check', undefined, ['answers'])).toBe(false);
  });
});

function allLeaves(n: ReturnType<typeof docNodes>[number]): ReturnType<typeof docNodes> {
  return n.children.length === 0 ? [n] : n.children.flatMap(allLeaves);
}
