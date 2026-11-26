import { Location, NgTemplateOutlet } from '@angular/common';
import { Component, ElementRef, afterRenderEffect, computed, inject, input, linkedSignal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink } from '@angular/router';
import { filter, map } from 'rxjs';
import { PackageDocAttribute, PackageDocEntry, PackageDocEntryPoint, PackageDocSeeAlso, PackageReadme } from '../../api/packages-api';
import { CopyButton } from '../../shared/copy-button';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { NoValue } from '../../shared/ui/no-value';
import { YamlToken, highlightYaml } from '../../shared/yaml-highlight';
import { DocProse } from './doc-prose';
import { ConfigLine, DocRow, ParamGroup, addedText, entrySections, optionRows, parameterGroups, returnRows, visibleRows } from './docs-model';
import { PackageReadmeView } from './package-readme';
import { safeHttpUrl, EXTERNAL_REL } from './package-links';

/** A sample of this many characters or fewer on one line stays inline in its cell; anything else is a code block with a copy button. */
const INLINE_SAMPLE = 60;

/** A table of parameters as the page draws it: the group and its rows, those of folded options left out. */
interface ParamTable {
  readonly group: ParamGroup;
  readonly rows: readonly DocRow[];
}

/**
 * One page of the documentation of a module, a plugin or a role (R-UI-061, Q-UI-242), in the order ansible-doc prints it: the head with
 * the list of the page's sections (R-ANS-050), then Synopsis, the parameters (one table, or for a lookup, a filter or a test the terms or
 * the input, the positional and the keyword parameters), Notes, Requirements, Attributes, See also, Examples, Return values and Authors,
 * each only when it has content.
 *
 * Every option and return value has an anchor, `parameter-<path>` or `return-<path>` (R-ANS-048, Q-ANS-93), a fragment of the page's own
 * address: `target` is the fragment of the address, and the page scrolls to it, unfolds what hides it and marks it. The link of a row is
 * copied by its button. A see-also entry of the version's tree links to it.
 *
 * Only the prose (`description`, `notes`, `requirements`, the description of an option) is HTML, and it is bound through the README's
 * component. Every other value of the entry, a name, a type, a default, a choice, a sample, an author, the examples, is bound as text, so
 * that a hostile default cannot become markup; the examples and a sample of several lines are drawn as the tokens of the YAML highlighter.
 */
@Component({
  selector: 'quader-ansible-doc-entry',
  imports: [CopyButton, DataTable, DocProse, NgTemplateOutlet, NoValue, PackageReadmeView, RouterLink],
  templateUrl: './ansible-doc-entry.html',
  styleUrl: './ansible-doc-entry.scss',
})
export class AnsibleDocEntry {
  private readonly router = inject(Router);
  private readonly location = inject(Location);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  readonly entry = input.required<PackageDocEntry>();
  /** The fragment of the page's address: an anchor of a row or a section, or none. */
  readonly target = input<string | null>(null);

  protected readonly externalRel = EXTERNAL_REL;
  protected readonly safeHttpUrl = safeHttpUrl;

  /** The parameters that are folded; a new page starts with every parameter open. */
  protected readonly foldedOptions = linkedSignal<PackageDocEntry, ReadonlySet<string>>({
    source: this.entry,
    computation: () => new Set<string>(),
  });
  protected readonly foldedReturns = linkedSignal<PackageDocEntry, ReadonlySet<string>>({
    source: this.entry,
    computation: () => new Set<string>(),
  });

  protected readonly groups = computed(() => parameterGroups(this.entry()));
  protected readonly sections = computed(() => entrySections(this.entry(), this.groups()));
  private readonly allTables = computed(() => this.groups().map((group) => ({ group, rows: optionRows(group.options, group.anchorPrefix) })));
  private readonly allReturns = computed(() => returnRows(this.entry().returnValues));
  protected readonly tables = computed<readonly ParamTable[]>(() =>
    this.allTables().map((t) => ({ group: t.group, rows: visibleRows(t.rows, this.foldedOptions()) })),
  );
  protected readonly returns = computed(() => visibleRows(this.allReturns(), this.foldedReturns()));
  protected readonly addedIn = computed(() => addedText(this.entry().versionAdded, this.entry().versionAddedCollection));
  /** The examples without the blank lines a source's literal starts with. */
  protected readonly examples = computed(() => highlightYaml((this.entry().examples ?? '').replace(/^(\s*\n)+/, '')));
  /** The full name in its parts: a long name breaks after a dot at 390 px, not inside a word. */
  protected readonly titleParts = computed(() => this.entry().fullName.split('.'));

  protected readonly readme = computed<PackageReadme | null>(() => {
    const html = this.entry().readmeHtml;
    return html ? { contentType: 'text/markdown', html, truncated: false, externalImagesBlocked: 0 } : null;
  });

  /** The README of a role without entry points: the page is the README alone; a role with them shows it after its entry points. */
  protected readonly readmeAlone = computed(() => (this.entry().entryPoints?.length ? null : this.readme()));

  /** The address of the page without its fragment, as a link copied from it is opened elsewhere: it follows every navigation. */
  private readonly pageUrl = toSignal(
    this.router.events.pipe(
      filter((e) => e instanceof NavigationEnd),
      map(() => this.currentUrl()),
    ),
    { initialValue: this.currentUrl() },
  );

  /**
   * Two columns, as the documentation site has them (Q-UI-1246): the name with its aliases, type, Required and "added in"; the
   * description with the choices or the default, the configuration, and for a return value when it is returned and its sample. Six
   * columns stood as blocks in the page pane beside the tree at 1,440 px and at 1,024 px.
   */
  protected readonly optionColumns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@docs.col.name:Name`, width: '12rem' },
    { id: 'description', label: $localize`:@@docs.col.description:Description`, prefer: true },
  ];
  protected readonly returnColumns: readonly TableColumn[] = this.optionColumns;
  protected readonly choicesLabel = $localize`:@@docs.col.choices:Choices`;
  protected readonly defaultLabel = $localize`:@@docs.col.default:Default`;
  protected readonly returnedLabel = $localize`:@@docs.col.returned:Returned`;
  protected readonly sampleLabel = $localize`:@@docs.col.sample:Sample`;
  protected readonly copySample = $localize`:@@docs.copy-sample:Copy the sample`;
  protected readonly optionsCaption = $localize`:@@docs.options.caption:Parameters of this entry`;
  protected readonly returnsCaption = $localize`:@@docs.returns.caption:Return values of this entry`;
  protected readonly copyName = $localize`:@@docs.copy-name:Copy the full name`;
  protected readonly copyExamples = $localize`:@@docs.copy-examples:Copy the examples`;
  protected readonly contentsLabel = $localize`:@@docs.contents:Contents of this page`;

  constructor() {
    // A target that a folded row hides is unfolded: the folded ids are the paths above it.
    afterRenderEffect(() => {
      const t = this.target();
      untracked(() => {
        if (!t) return;
        const owner = [...this.allTables().flatMap((x) => x.rows), ...this.allReturns()].find(
          (r) => r.anchor === t || r.aliasAnchors.includes(t),
        );
        if (!owner) return;
        const above = (s: ReadonlySet<string>): ReadonlySet<string> =>
          new Set([...s].filter((id) => !owner.id.startsWith(`${id}/`)));
        if (owner.anchor.startsWith('return-')) this.foldedReturns.update(above);
        else this.foldedOptions.update(above);
      });
    });
    // The page goes to its target once the target is drawn: on a deep link, after the entry has loaded, and after a link inside it.
    let shown: string | null = null;
    afterRenderEffect(() => {
      const t = this.target();
      const key = `${this.entry().type}/${this.entry().name}#${t ?? ''}`;
      this.tables();
      this.returns();
      untracked(() => {
        if (!t || key === shown) return;
        const el = this.host.nativeElement.querySelector<HTMLElement>(`[id="${CSS.escape(t)}"]`);
        if (!el) return;
        shown = key;
        el.scrollIntoView?.({ block: 'start' });
      });
    });
  }

  /** The list a template of attributes is given: the entry's or an entry point's. */
  protected asAttributes(list: unknown): readonly PackageDocAttribute[] {
    return Array.isArray(list) ? (list as PackageDocAttribute[]) : [];
  }

  /** The list a template of see-also entries is given: the entry's or an entry point's. */
  protected asSeeAlso(list: unknown): readonly PackageDocSeeAlso[] {
    return Array.isArray(list) ? (list as PackageDocSeeAlso[]) : [];
  }

  /** "added in" of a role's entry point (R-ANS-052). */
  protected entryPointAdded(ep: PackageDocEntryPoint): string {
    return addedText(ep.versionAdded, ep.versionAddedCollection);
  }

  /** The absolute address of a row: the page's address and the row's anchor. */
  protected rowLink(anchor: string): string {
    return `${this.pageUrl()}#${encodeURIComponent(anchor).replace(/%2F/g, '/')}`;
  }

  protected copyRowLabel(row: DocRow): string {
    return $localize`:@@docs.copy-link:Copy the link to ${row.name}:name:`;
  }

  protected configLabel(line: ConfigLine): string {
    switch (line.kind) {
      case 'ini':
        return $localize`:@@docs.config.ini:INI entry`;
      case 'env':
        return $localize`:@@docs.config.env:Environment variable`;
      case 'vars':
        return $localize`:@@docs.config.vars:Variable`;
      case 'keyword':
        return $localize`:@@docs.config.keyword:Keyword`;
      case 'cli':
        return $localize`:@@docs.config.cli:Command line`;
    }
  }

  /** The sample of a row as highlighted lines, when it is a block. */
  protected sampleLines(row: DocRow): YamlToken[][] {
    return highlightYaml(row.sample);
  }

  /** 16 px per level (Q-UI-242). */
  protected indent(row: DocRow): string {
    return `calc(var(--quader-space-3) + ${row.depth * 16}px)`;
  }

  protected inlineSample(row: DocRow): boolean {
    return row.sample.length <= INLINE_SAMPLE && !row.sample.includes('\n');
  }

  protected toggleOption(row: DocRow): void {
    this.foldedOptions.update((s) => flip(s, row.id));
  }

  protected toggleReturn(row: DocRow): void {
    this.foldedReturns.update((s) => flip(s, row.id));
  }

  protected foldLabel(row: DocRow, folded: ReadonlySet<string>): string {
    return folded.has(row.id)
      ? $localize`:@@docs.unfold:Show the suboptions of ${row.name}:name:`
      : $localize`:@@docs.fold:Hide the suboptions of ${row.name}:name:`;
  }

  private currentUrl(): string {
    const url = this.router.url.replace(/#.*$/, '');
    return `${window.location.origin}${this.location.prepareExternalUrl(url)}`;
  }
}

function flip(set: ReadonlySet<string>, id: string): ReadonlySet<string> {
  const next = new Set(set);
  if (!next.delete(id)) next.add(id);
  return next;
}
