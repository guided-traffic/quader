import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NamespaceRules, nameParts, namespaceCommands, namespaceLinkLabel } from './namespaces';

/**
 * The namespace of a package in the facts of its page's head (R-UI-220): **Namespace** `acme`, **Scope** `@acme`, **Path** `team` ›
 * `app`, each level a link to its page in the project. Nothing for a name without one or a format without namespaces. Without `links`
 * the levels are text: an application that has no namespace page (the Bastion's, Q-UI-1443) names them and leads nowhere.
 */
@Component({
  selector: 'quader-namespace-of',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
    @if (levels().length > 0) {
      <span class="namespace-of" data-testid="namespace-of"
        ><span class="muted">{{ word() }}</span
        >&ngsp;@for (l of levels(); track l.namespace; let last = $last) {@if (links()) {<a
              [routerLink]="link(l.namespace)"
              [attr.aria-label]="label(l.namespace)"
              translate="no"
              >{{ l.text }}</a
            >} @else {<span translate="no">{{ l.text }}</span>}@if (!last) {<span class="muted" aria-hidden="true">&ngsp;›&ngsp;</span>}}</span
      >
    }
  `,
  styles: `
    :host {
      display: inline;
    }
    .namespace-of {
      overflow-wrap: anywhere;
    }
  `,
})
export class NamespaceOf {
  private readonly rules = inject(NamespaceRules);

  readonly format = input.required<string>();
  readonly name = input.required<string>();
  readonly project = input<string | null>(null);
  /** Whether each level links to its namespace's page; false where the application has none (Q-UI-1443). */
  readonly links = input(true);

  private readonly rule = computed(() => this.rules.of(this.format()));
  protected readonly levels = computed(() =>
    nameParts(this.rule(), this.name()).filter((p): p is { text: string; namespace: string } => p.namespace !== null),
  );
  protected readonly word = computed(() => {
    switch (this.rule()?.kind) {
      case 'scope':
        return $localize`:@@ns.word.scope:Scope`;
      case 'path':
        return $localize`:@@ns.word.path:Path`;
    }
    return $localize`:@@ns.word.namespace:Namespace`;
  });

  protected link(ns: string): string[] {
    return namespaceCommands(this.project(), this.format(), ns);
  }

  protected label(ns: string): string {
    return namespaceLinkLabel(this.rule()?.kind ?? 'namespace', ns);
  }
}
