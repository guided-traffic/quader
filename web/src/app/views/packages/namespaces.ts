import { Injectable, Signal, computed, inject, resource } from '@angular/core';
import { FormatDescriptor } from '../../api/repositories-api';
import { FormatCatalog } from '../../core/format-names';
import { valueOf } from '../../shared/paged-resource';

/**
 * The namespaces of a format's package names (R-UI-220, Q-UI-1290): Ansible Galaxy's namespace (`acme` of `acme.tools`), npm's scope
 * (`@acme` of `@acme/ui`), every path prefix of an OCI image (`team` and `team/app` of `team/app/image`). The rule is the format's,
 * served by `GET /api/v1/formats` (`namespaces`); the screens name no format here.
 */
export type NamespaceRule = NonNullable<FormatDescriptor['namespaces']>;

/** The namespaces of a name, outermost first; none for a name without one or a rule of none. */
export function namespacesOf(rule: NamespaceRule | null | undefined, name: string): string[] {
  if (!rule || !rule.separator || !name.startsWith(rule.marker)) return [];
  const out: string[] = [];
  let i = rule.marker.length;
  while (i < name.length) {
    const j = name.indexOf(rule.separator, i);
    if (j <= i) break;
    out.push(name.slice(0, j));
    if (!rule.nested) break;
    i = j + rule.separator.length;
  }
  return out;
}

/** Whether a text is a namespace of the rule: what the server's `Prefix` takes. */
export function isNamespace(rule: NamespaceRule | null | undefined, text: string): boolean {
  if (!rule || !rule.separator || !text.startsWith(rule.marker) || text.length <= rule.marker.length) return false;
  const rest = text.slice(rule.marker.length);
  if (rest.startsWith(rule.separator) || rest.endsWith(rule.separator)) return false;
  return !rest.includes(rule.separator) || (rule.nested && !rest.includes(rule.separator + rule.separator));
}

/** A part of a name as the screens show it: a namespace's last level that links to its page, or the rest that links to the package. */
export interface NamePart {
  /** The text of the part, without the separator. */
  readonly text: string;
  /** The namespace this part ends, or null for the part after the last namespace. */
  readonly namespace: string | null;
}

/** The parts of a name: each level of its namespaces, then the rest (`acme` · `tools`; `team` · `app` · `image`). */
export function nameParts(rule: NamespaceRule | null | undefined, name: string): NamePart[] {
  const namespaces = namespacesOf(rule, name);
  if (!rule || namespaces.length === 0) return [{ text: name, namespace: null }];
  const out: NamePart[] = [];
  let start = 0;
  for (const ns of namespaces) {
    out.push({ text: name.slice(start, ns.length), namespace: ns });
    start = ns.length + rule.separator.length;
  }
  out.push({ text: name.slice(start), namespace: null });
  return out;
}

/** The address of a namespace's page: in a project, or across every project the reader may read (Q-UI-1291). */
export function namespaceCommands(project: string | null, format: string, namespace: string): string[] {
  const segments = namespace.split('/');
  return project ? ['/projects', project, 'packages', '-', 'namespaces', format, ...segments] : ['/namespaces', format, ...segments];
}

/** The word of the kind, as a heading names it: **Namespace acme**, **Scope @acme**, **Path team/app**. */
export function namespaceHeading(kind: NamespaceRule['kind'], namespace: string): string {
  switch (kind) {
    case 'scope':
      return $localize`:@@ns.heading.scope:Scope ${namespace}:name:`;
    case 'path':
      return $localize`:@@ns.heading.path:Path ${namespace}:name:`;
  }
  return $localize`:@@ns.heading.namespace:Namespace ${namespace}:name:`;
}

/** The label of a link to a namespace's page, for assistive technology and a tooltip. */
export function namespaceLinkLabel(kind: NamespaceRule['kind'], namespace: string): string {
  switch (kind) {
    case 'scope':
      return $localize`:@@ns.link.scope:The packages of the scope ${namespace}:name:`;
    case 'path':
      return $localize`:@@ns.link.path:The packages under ${namespace}:name:`;
  }
  return $localize`:@@ns.link.namespace:The collections of the namespace ${namespace}:name:`;
}

/**
 * The rules of every format by its key, read once per page load through the format catalog (FormatCatalog): one read for every name
 * the page shows.
 */
@Injectable({ providedIn: 'root' })
export class NamespaceRules {
  private readonly catalog = inject(FormatCatalog);
  private readonly formats = resource({ loader: () => this.catalog.formats() });

  /** The rules by the format's key; empty until the formats are read. */
  readonly rules: Signal<ReadonlyMap<string, NamespaceRule>> = computed(() => {
    const out = new Map<string, NamespaceRule>();
    for (const f of valueOf(this.formats) ?? []) if (f.namespaces) out.set(f.name, f.namespaces);
    return out;
  });

  /** The rule of a format, null for a format without namespaces or before the formats are read. */
  of(format: string): NamespaceRule | null {
    return this.rules().get(format) ?? null;
  }
}
