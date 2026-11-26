import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NamespaceRules, nameParts, namespaceCommands, namespaceLinkLabel } from './namespaces';

/**
 * A package's name in a list, a hit or a page's head (R-UI-220, Q-UI-1293): each level of its namespace links to the namespace's page,
 * the rest to the package. `acme.tools` is **acme** · **tools**, `@acme/ui` **@acme** · **ui**, `team/app/image` **team** · **app** ·
 * **image**; a name without a namespace is one link. The namespaces are those of the package's key, the name the rules see: a package
 * shown by another name whose key has none (an Ansible role, key `role/<id>`, shown as `<owner>.<name>`, R-ANS-056) is one link, never a
 * member of a namespace its display name seems to name. The package's link carries the whole name for assistive technology, so a list
 * reads the name it shows. The name may break after a separator (`<wbr>`), never inside a part. Without `namespaceLinks` the namespace
 * is text in the same muted colour, the rest still the package's link: an application without a namespace page (the Bastion's,
 * Q-UI-1443).
 */
@Component({
  selector: 'quader-package-name',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `@for (p of parts(); track $index) {@if (p.namespace !== null && !namespaceLinks()) {<span class="ns-text" translate="no"
          >{{ p.text }}</span
        ><span class="ns-sep" aria-hidden="true">{{ separator() }}</span><wbr />} @else if (p.namespace !== null) {<a
          class="ns-link"
          [routerLink]="namespaceLink(p.namespace)"
          [attr.aria-label]="label(p.namespace)"
          [attr.title]="label(p.namespace)"
          data-testid="namespace-link"
          translate="no"
          >{{ p.text }}</a
        ><span class="ns-sep" aria-hidden="true">{{ separator() }}</span><wbr />} @else if (link(); as l) {<a
          [routerLink]="l"
          [attr.aria-label]="parts().length > 1 ? name() : null"
          translate="no"
          >{{ p.text }}</a
        >} @else {<span translate="no">{{ p.text }}</span>}}`,
  styles: `
    :host {
      display: inline;
      /* A long name breaks anywhere, never past its cell (an Ansible name of underscores has no other place to break). */
      overflow-wrap: anywhere;
    }
    /* The namespace reads as the start of the name, a link of its own: muted until it is pointed at. */
    .ns-link:not(:hover):not(:focus-visible) {
      color: var(--quader-text-muted);
    }
    .ns-sep,
    .ns-text {
      color: var(--quader-text-muted);
    }
  `,
})
export class PackageName {
  private readonly rules = inject(NamespaceRules);

  readonly format = input.required<string>();
  readonly name = input.required<string>();
  /** The package's key when it differs from the name shown (R-ANS-056); null when the name is the key. */
  readonly key = input<string | null | undefined>(null);
  /** The router commands of the package; null shows its last part as text (the page of the package itself). */
  readonly link = input<readonly string[] | null>(null);
  /** The project whose namespace page the namespace links to; null for the page across every project. */
  readonly project = input<string | null>(null);
  /** Whether the namespace links to its page; false where the application has none (Q-UI-1443). */
  readonly namespaceLinks = input(true);

  private readonly rule = computed(() => this.rules.of(this.format()));
  protected readonly parts = computed(() => {
    const key = this.key();
    if (key && key !== this.name() && nameParts(this.rule(), key).length === 1) return nameParts(null, this.name());
    return nameParts(this.rule(), this.name());
  });
  protected readonly separator = computed(() => this.rule()?.separator ?? '');

  protected namespaceLink(ns: string): string[] {
    return namespaceCommands(this.project(), this.format(), ns);
  }

  protected label(ns: string): string {
    return namespaceLinkLabel(this.rule()?.kind ?? 'namespace', ns);
  }
}
