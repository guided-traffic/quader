import {
  PackageDocEntry,
  PackageDocEntryPoint,
  PackageDocOption,
  PackageDocOptionConfiguration,
  PackageDocReturnValue,
  PackageDocTree,
} from '../../api/packages-api';

/*
 * The pure part of the documentation screens (R-UI-060 to R-UI-062): the address of a page in the URL, the nodes of the tree, the rows of
 * the parameter tables and the text of a typed value. Nothing here touches the DOM or the network.
 */

/** The README, as the query parameter `doc` spells it. */
export const README_DOC = 'README.md';
/** The name of the changelog among the files (R-ANS-053): `file/changelog`; no file of `docs/` has it, those end in `.md`. */
export const CHANGELOG_FILE = 'changelog';

export type DocAddress =
  | { readonly kind: 'file'; readonly name: string }
  | { readonly kind: 'entry'; readonly type: string; readonly name: string };

/** `README.md`, `file/<name>` and `<type>/<name>` (`module/x`, `filter/x`, `role/x`); anything else is no address. */
export function parseDoc(doc: string | undefined | null): DocAddress | null {
  if (!doc) return null;
  if (doc === README_DOC) return { kind: 'file', name: README_DOC };
  const slash = doc.indexOf('/');
  if (slash <= 0 || slash === doc.length - 1) return null;
  const head = doc.slice(0, slash);
  const rest = doc.slice(slash + 1);
  return head === 'file' ? { kind: 'file', name: rest } : { kind: 'entry', type: head, name: rest };
}

/** The `doc` value of a file of the tree: the README by its name, every other Markdown file below `file/`. */
export function fileDoc(name: string): string {
  return name === README_DOC ? README_DOC : `file/${name}`;
}

export function entryDoc(type: string, name: string): string {
  return `${type}/${name}`;
}

/** Whether the tree holds the page; a search narrows the tree, so the question is asked of the whole one. */
export function holdsDoc(tree: PackageDocTree, address: DocAddress | null): boolean {
  if (!address) return false;
  if (address.kind === 'file')
    return (tree.files ?? []).some((f) => f.name === address.name) || (address.name === CHANGELOG_FILE && !!tree.changelog);
  return (tree.groups ?? []).some((g) =>
    g.types.some((t) => t.type === address.type && t.entries.some((e) => e.name === address.name)),
  );
}

/**
 * The page shown when `doc` is absent or unknown: the README, else the first file, else the changelog, else the first entry; null for
 * an empty tree.
 */
export function firstDoc(tree: PackageDocTree): string | null {
  const file = tree.files?.[0];
  if (file) return fileDoc(file.name);
  if (tree.changelog) return fileDoc(CHANGELOG_FILE);
  for (const g of tree.groups ?? [])
    for (const t of g.types) {
      const e = t.entries[0];
      if (e) return entryDoc(e.type, e.name);
    }
  return null;
}

/** One row of the tree pane. A leaf has a `doc`; a group or a type has children and a count. */
export interface DocNode {
  /** The tree value: the `doc` of a leaf, `group:<category>` or `type:<type>` of a branch. */
  readonly id: string;
  readonly label: string;
  readonly kind: 'files' | 'group' | 'type' | 'file' | 'entry';
  readonly count: number | null;
  readonly doc: string | null;
  readonly tooltip: string;
  /** An entry whose source holds no documentation: muted and labelled. */
  readonly muted: boolean;
  readonly children: readonly DocNode[];
}

/**
 * The nodes of the tree pane from the answer of the tree (the narrowed one while one searches). The first row is **Documentation**
 * (the README, the other Markdown files and the changelog as children, R-ANS-053), then Modules, Plugins and Roles. A group that holds one type of its own
 * name (modules, roles) lists the entries directly; Plugins lists its types, each with its count. The counts are those the server
 * sends, which are the counts before the search.
 */
export function docNodes(view: PackageDocTree, all: PackageDocTree, searching: boolean): DocNode[] {
  const out: DocNode[] = [];
  const files = view.files ?? [];
  if (files.length > 0 || view.changelog) {
    const children: DocNode[] = files.map((f) => ({
      id: fileDoc(f.name),
      label: f.title,
      kind: 'file' as const,
      count: null,
      doc: fileDoc(f.name),
      tooltip: f.name,
      muted: false,
      children: [],
    }));
    if (view.changelog) {
      children.push({
        id: fileDoc(CHANGELOG_FILE),
        label: $localize`:@@docs.tree.changelog:Changelog`,
        kind: 'file',
        count: null,
        doc: fileDoc(CHANGELOG_FILE),
        tooltip: view.changelog.source,
        muted: false,
        children: [],
      });
    }
    out.push({
      id: 'group:files',
      label: $localize`:@@docs.tree.documentation:Documentation`,
      kind: 'files',
      count: (all.files ?? []).length + (all.changelog ? 1 : 0),
      doc: null,
      tooltip: '',
      muted: false,
      children,
    });
  }
  for (const g of view.groups ?? []) {
    if (searching && g.types.every((t) => t.entries.length === 0)) continue;
    if (!searching && g.count === 0) continue;
    const leaves = (type: string, entries: typeof g.types[number]['entries']): DocNode[] =>
      entries.map((e) => ({
        id: entryDoc(e.type, e.name),
        label: e.name,
        kind: 'entry' as const,
        count: null,
        doc: entryDoc(type, e.name),
        tooltip: e.shortDescription,
        // A role has its README and no doc_strings: it is not "without documentation" (the page shows the README).
        muted: !e.hasDocs && e.type !== 'role',
        children: [],
      }));
    const flat = g.types.length === 1 && g.types[0]?.type === g.category;
    out.push({
      id: `group:${g.category}`,
      label: groupLabel(g.category, g.label),
      kind: 'group',
      count: g.count,
      doc: null,
      tooltip: '',
      muted: false,
      children: flat
        ? leaves(g.category, g.types[0]?.entries ?? [])
        : g.types.map((t) => ({
            id: `type:${t.type}`,
            label: t.type,
            kind: 'type' as const,
            count: t.count,
            doc: null,
            tooltip: '',
            muted: false,
            children: leaves(t.type, t.entries),
          })),
    });
  }
  return out;
}

function groupLabel(category: string, fallback: string): string {
  switch (category) {
    case 'module':
      return $localize`:@@docs.tree.modules:Modules`;
    case 'plugin':
      return $localize`:@@docs.tree.plugins:Plugins`;
    case 'role':
      return $localize`:@@docs.tree.roles:Roles`;
    default:
      return fallback;
  }
}

/** The ids of the branches that hold the node with this id, from the top. */
export function branchesOf(nodes: readonly DocNode[], id: string | null): string[] {
  if (id === null) return [];
  const walk = (list: readonly DocNode[], trail: string[]): string[] | null => {
    for (const n of list) {
      if (n.id === id) return trail;
      const found = walk(n.children, [...trail, n.id]);
      if (found) return found;
    }
    return null;
  };
  return walk(nodes, []) ?? [];
}

/** The ids of every branch of the tree (a search opens them all: only the groups with matches are left). */
export function allBranches(nodes: readonly DocNode[]): string[] {
  return nodes.flatMap((n) => (n.children.length > 0 ? [n.id, ...allBranches(n.children)] : []));
}

/** The text of a typed JSON value: a string as written, a number or a boolean as JSON writes it, a structure as compact JSON. */
export function valueText(v: unknown): string {
  if (v === undefined || v === null) return '';
  if (typeof v === 'string') return v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  try {
    return JSON.stringify(v);
  } catch {
    return '';
  }
}

/** A table of parameters of an entry, with the section that holds it (R-ANS-050), or one entry point of a role (R-ANS-052). */
export interface ParamGroup {
  /** The section's id: `doc-<id>`; `parameters`, `terms`, `input`, `positional`, `keyword`, or `entry-<entry point>`. */
  readonly id: string;
  readonly title: string;
  readonly options: readonly PackageDocOption[];
  /** What the anchors of the table's rows hold before the path: `<entry point>--` for an entry point, else nothing. */
  readonly anchorPrefix: string;
  /** The entry point a role's group is, with its prose. */
  readonly entryPoint?: PackageDocEntryPoint;
}

/** The types whose parameters the documentation site splits into terms or input, positional and keyword parameters. */
const SPLIT_TYPES = new Set(['lookup', 'filter', 'test']);

/**
 * The tables of an entry's parameters (R-ANS-050): one **Parameters** table for a module and most plugins; for a lookup plugin the
 * option `_terms` as **Terms**, for a filter or a test the option `_input` as **Input** and the options named in `positional` as
 * **Positional parameters**, the others of the three types as **Keyword parameters**. An empty table is left out.
 */
export function parameterGroups(entry: PackageDocEntry): ParamGroup[] {
  if (entry.entryPoints?.length) {
    // A role's entry points, each a group of its own with the anchors parameter-<entry point>--<path> (R-ANS-052, Q-ANS-97).
    return entry.entryPoints.map((ep) => ({
      id: `entry-${ep.name}`,
      title: $localize`:@@docs.entry-point:Entry point ${ep.name}:name:`,
      options: ep.options ?? [],
      anchorPrefix: `${ep.name}--`,
      entryPoint: ep,
    }));
  }
  const options = entry.options ?? [];
  if (!SPLIT_TYPES.has(entry.type)) {
    return options.length > 0 ? [{ id: 'parameters', title: $localize`:@@docs.parameters:Parameters`, options, anchorPrefix: '' }] : [];
  }
  const byName = new Map(options.map((o) => [o.name, o]));
  const positional = (entry.positional ?? []).filter((n) => n !== '_input' && n !== '_terms' && byName.has(n));
  const taken = new Set(['_terms', '_input', ...positional]);
  const groups: ParamGroup[] = [
    { id: 'terms', title: $localize`:@@docs.terms:Terms`, options: options.filter((o) => o.name === '_terms'), anchorPrefix: '' },
    { id: 'input', title: $localize`:@@docs.input:Input`, options: options.filter((o) => o.name === '_input'), anchorPrefix: '' },
    {
      id: 'positional',
      title: $localize`:@@docs.positional:Positional parameters`,
      options: positional.map((n) => byName.get(n)).filter((o): o is PackageDocOption => o !== undefined),
      anchorPrefix: '',
    },
    {
      id: 'keyword',
      title: $localize`:@@docs.keyword-parameters:Keyword parameters`,
      options: options.filter((o) => !taken.has(o.name)),
      anchorPrefix: '',
    },
  ];
  return groups.filter((g) => g.options.length > 0);
}

/** A section of an entry's page, as its list of contents names it. */
export interface DocSection {
  readonly id: string;
  readonly label: string;
}

/** The sections an entry's page shows, in its order: the list of contents that heads the page (R-ANS-050). */
export function entrySections(entry: PackageDocEntry, groups: readonly ParamGroup[]): DocSection[] {
  if (!entry.hasDocs) return [];
  if (entry.entryPoints?.length) {
    // A role: its entry points, then its README (R-ANS-052).
    const out = groups.map((g) => ({ id: `doc-${g.id}`, label: g.title }));
    if (entry.readmeHtml) out.push({ id: 'doc-readme', label: $localize`:@@docs.readme:README` });
    return out;
  }
  if (entry.readmeHtml) return [];
  const has = (v: readonly unknown[] | undefined): boolean => (v?.length ?? 0) > 0;
  const out: DocSection[] = [];
  if (has(entry.description)) out.push({ id: 'doc-synopsis', label: $localize`:@@docs.synopsis:Synopsis` });
  for (const g of groups) out.push({ id: `doc-${g.id}`, label: g.title });
  if (has(entry.notes)) out.push({ id: 'doc-notes', label: $localize`:@@docs.notes:Notes` });
  if (has(entry.requirements)) out.push({ id: 'doc-requirements', label: $localize`:@@docs.requirements:Requirements` });
  if (has(entry.attributes)) out.push({ id: 'doc-attributes', label: $localize`:@@docs.attributes:Attributes` });
  if (has(entry.seealso)) out.push({ id: 'doc-seealso', label: $localize`:@@docs.see-also:See also` });
  if (entry.examples) out.push({ id: 'doc-examples', label: $localize`:@@docs.examples:Examples` });
  if (has(entry.returnValues)) out.push({ id: 'doc-returns', label: $localize`:@@docs.returns:Return values` });
  if (has(entry.author)) out.push({ id: 'doc-authors', label: $localize`:@@docs.authors:Authors` });
  return out;
}

/** One way an option of a plugin is configured besides the task (R-ANS-049): what it is, and the text of the entry. */
export interface ConfigLine {
  readonly kind: 'ini' | 'env' | 'vars' | 'keyword' | 'cli';
  readonly text: string;
}

/** The lines of an option's configuration, in the order the documentation site shows them: ini, environment, variable, keyword, cli. */
export function configLines(c: PackageDocOptionConfiguration | undefined): ConfigLine[] {
  if (!c) return [];
  const out: ConfigLine[] = [];
  for (const i of c.ini ?? []) {
    const text = [i.section ? `[${i.section}]` : '', i.key ?? ''].filter((s) => s !== '').join(' ');
    if (text) out.push({ kind: 'ini', text });
  }
  for (const kind of ['env', 'vars', 'keyword'] as const) for (const n of c[kind] ?? []) out.push({ kind, text: n.name });
  for (const x of c.cli ?? []) {
    const text = x.option ?? x.name ?? x.switch ?? '';
    if (text) out.push({ kind: 'cli', text });
  }
  return out;
}

/** "added in": the collection the version counts in and the version, or the version alone where the source names no collection. */
export function addedText(version: string | undefined, collection: string | undefined): string {
  if (!version) return '';
  return collection ? `${collection} ${version}` : version;
}

/** The anchor of a row of a table (R-ANS-048): `parameter-<path>` or `return-<path>`, the path the names from the top joined by `/`. */
export function rowAnchor(kind: 'parameter' | 'return', id: string): string {
  return `${kind}-${id.replace(/^\//, '')}`;
}

/** A row of a parameter table or a return value table, flattened with its depth. */
export interface DocRow {
  /** The path of names from the top, unique in a table. */
  readonly id: string;
  /** The row's anchor, the fragment a link to it ends in; an alias of an option has an anchor of its own (`aliasAnchors`). */
  readonly anchor: string;
  readonly aliasAnchors: readonly string[];
  readonly depth: number;
  /** The name of the row above, empty at the top. */
  readonly parent: string;
  readonly name: string;
  readonly aliases: string;
  readonly type: string;
  readonly required: boolean;
  readonly defaultText: string;
  readonly hasDefault: boolean;
  /** The texts of the choices the default names: the default, or each element of a list default (`[TLSv1.3]` marks TLSv1.3). */
  readonly defaults: readonly string[];
  readonly choices: readonly string[];
  readonly returned: string;
  readonly sample: string;
  /** "added in" with its collection (`addedText`), empty when the row names no version. */
  readonly versionAdded: string;
  readonly configuration: readonly ConfigLine[];
  readonly description: readonly string[];
  readonly children: number;
}

function typeText(type: string | undefined, elements: string | undefined): string {
  if (!type) return '';
  return elements ? `${type} / ${elements}` : type;
}

/**
 * The rows of the options, in document order, each followed by its suboptions. `prefix` is the group's `anchorPrefix`: the rows of an
 * entry point of a role have the ids and anchors of that entry point (`parameter-main--tls/cert`), so two entry points' options of one
 * name are two rows.
 */
export function optionRows(options: readonly PackageDocOption[] | undefined, prefix = ''): DocRow[] {
  const out: DocRow[] = [];
  const anchor = (path: string): string => rowAnchor('parameter', `${prefix}${path.replace(/^\//, '')}`);
  const walk = (list: readonly PackageDocOption[], depth: number, parent: string, path: string): void => {
    for (const o of list) {
      const id = `${path}/${o.name}`;
      const subs = o.suboptions ?? [];
      out.push({
        id: `${prefix}${id}`,
        anchor: anchor(id),
        aliasAnchors: (o.aliases ?? []).map((a) => anchor(`${path}/${a}`)),
        depth,
        parent,
        name: o.name,
        aliases: (o.aliases ?? []).join(', '),
        type: typeText(o.type, o.elements),
        required: o.required === true,
        defaultText: valueText(o.default),
        hasDefault: o.default !== undefined && o.default !== null,
        defaults: o.default === undefined || o.default === null ? [] : Array.isArray(o.default) ? o.default.map(valueText) : [valueText(o.default)],
        choices: (o.choices ?? []).map(valueText),
        returned: '',
        sample: '',
        versionAdded: addedText(o.versionAdded, o.versionAddedCollection),
        configuration: configLines(o.configuration),
        description: o.description ?? [],
        children: subs.length,
      });
      walk(subs, depth + 1, o.name, id);
    }
  };
  walk(options ?? [], 0, '', '');
  return out;
}

/** The rows of the return values, each followed by what it contains. */
export function returnRows(values: readonly PackageDocReturnValue[] | undefined): DocRow[] {
  const out: DocRow[] = [];
  const walk = (list: readonly PackageDocReturnValue[], depth: number, parent: string, path: string): void => {
    for (const r of list) {
      const id = `${path}/${r.name}`;
      const subs = r.contains ?? [];
      out.push({
        id,
        anchor: rowAnchor('return', id),
        aliasAnchors: [],
        depth,
        parent,
        name: r.name,
        aliases: '',
        type: typeText(r.type, r.elements),
        required: false,
        defaultText: '',
        hasDefault: false,
        defaults: [],
        choices: [],
        returned: r.returned ?? '',
        sample: r.sample === undefined ? '' : sampleText(r.sample),
        versionAdded: addedText(r.versionAdded, r.versionAddedCollection),
        configuration: [],
        description: r.description ?? [],
        children: subs.length,
      });
      walk(subs, depth + 1, r.name, id);
    }
  };
  walk(values ?? [], 0, '', '');
  return out;
}

/** A sample: a string as written, anything else as indented JSON, so that a structure reads as a block. */
function sampleText(v: unknown): string {
  if (typeof v === 'string') return v;
  if (v !== null && typeof v === 'object') {
    try {
      return JSON.stringify(v, null, 2);
    } catch {
      return '';
    }
  }
  return valueText(v);
}

/** The rows that are visible: a row hides while any row above it on its path is collapsed. */
export function visibleRows(rows: readonly DocRow[], collapsed: ReadonlySet<string>): DocRow[] {
  const out: DocRow[] = [];
  let hiddenBelow = Infinity;
  for (const r of rows) {
    if (r.depth > hiddenBelow) continue;
    hiddenBelow = Infinity;
    out.push(r);
    if (collapsed.has(r.id)) hiddenBelow = r.depth;
  }
  return out;
}

/** The reason a version's documentation is unavailable, in words (R-UI-060). */
export function unavailableText(reason: PackageDocTree['reason']): string {
  switch (reason) {
    case 'upstream_error':
      return $localize`:@@docs.unavailable.upstream:The upstream did not deliver the documentation of this version.`;
    case 'too_large':
      return $localize`:@@docs.unavailable.too-large:The documentation of this version is larger than Quader keeps.`;
    case 'invalid_document':
      return $localize`:@@docs.unavailable.invalid:The upstream sent a documentation document that Quader cannot read.`;
    default:
      return $localize`:@@docs.unavailable.unknown:The documentation of this version is not available.`;
  }
}
