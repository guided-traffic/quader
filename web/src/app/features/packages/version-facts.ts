import { Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { EXTERNAL_REL, packageCommands } from './package-links';
import { DependencyGroup, MetadataLink } from './version-metadata';

/*
 * The blocks that show a version's general properties the same way in every format (R-UI-221): its dependencies, its keywords and its
 * links. A format feeds them from its metadata (version-metadata.ts); the block, its words and its place are one.
 */

/**
 * A version's dependencies, one block for every format (R-UI-221, Q-UI-1311): a group per kind as the format names it (npm's fields,
 * NuGet's target frameworks, Debian's relation fields; no heading where the format has one list), each dependency on a line of its
 * own with its alternatives side by side, the name a link to its page where this installation holds the package, else text, and what
 * follows the name (a range, a constraint) muted after it. A group the server could not parse shows its text.
 */
@Component({
  selector: 'quader-dependency-list',
  imports: [RouterLink],
  template: `
    @for (g of groups(); track $index) {
      @if (g.label !== null) {
        <h3 class="dependency-group" translate="no">{{ g.label }}</h3>
      }
      @if (g.text !== null) {
        <p class="dependency-text" translate="no">{{ g.text }}</p>
      } @else if (g.entries.length === 0) {
        <p class="dependency-none" i18n="@@pkg.meta.no-dependencies">No dependencies.</p>
      } @else {
        <ul class="dependency-list" translate="no">
          @for (e of g.entries; track $index) {
            <li>
              @for (a of e.alternatives; track $index; let last = $last) {
                <span class="dependency">
                  @if (held().has(a.name)) {
                    <a [routerLink]="link(a.name)">{{ a.written }}</a>
                  } @else {
                    <span>{{ a.written }}</span>
                  }
                  @if (a.qualifiers !== '') {
                    <span class="secondary-inline">&ngsp;{{ a.qualifiers }}</span>
                  }
                </span>
                @if (!last) {
                  &ngsp;<span class="dependency-or secondary-inline" i18n="@@pkg.meta.deb-or">or</span>&ngsp;
                }
              }
              @if (e.mark; as mark) {
                &ngsp;<span class="badge">{{ mark }}</span>
              }
            </li>
          }
        </ul>
      }
    }
  `,
  styles: `
    :host {
      display: block;
      margin-block-end: var(--quader-space-5);
    }
    .dependency-group {
      margin-block: var(--quader-space-3) var(--quader-space-1);
      font: var(--quader-type-title-small);
    }
    .dependency-group:first-child {
      margin-block-start: 0;
    }
    .dependency-list {
      margin: 0;
      padding: 0;
      list-style: none;

      li {
        padding-block: var(--quader-space-1);
        overflow-wrap: anywhere;
      }
    }
    .dependency-or {
      margin-inline: var(--quader-space-2);
    }
    .dependency-text,
    .dependency-none {
      margin: 0;
      overflow-wrap: anywhere;
    }
    .dependency-none {
      color: var(--quader-text-muted);
    }
    /* An alternative stays on one line while it fits; the list wraps between alternatives. */
    .dependency {
      white-space: nowrap;
    }
    .badge {
      vertical-align: baseline;
    }
  `,
})
export class DependencyList {
  readonly groups = input.required<readonly DependencyGroup[]>();
  /** The names of the packages this installation holds and the reader may read: each is a link to its page. */
  readonly held = input<ReadonlySet<string>>(new Set());
  readonly project = input.required<string>();
  readonly format = input.required<string>();

  protected link(name: string): string[] {
    return packageCommands(this.project(), this.format(), name);
  }
}

/**
 * A version's keywords or tags (R-UI-221, Q-UI-1312), as chips that open the package list of the project filtered by the keyword and the
 * format, where the packages that share it are. The words are a publisher's: text, never markup.
 */
@Component({
  selector: 'quader-keyword-chips',
  imports: [RouterLink],
  template: `
    <ul class="keyword-chips" translate="no" [attr.aria-label]="label">
      @for (k of keywords(); track k) {
        <li>
          <a class="keyword-chip" [routerLink]="['/projects', project(), 'packages']" [queryParams]="{ format: format(), keyword: k }">{{ k }}</a>
        </li>
      }
    </ul>
  `,
  styles: `
    /* A block whose bottom room meets the next heading's, so a block of another component after it (a role's facts) keeps the room. */
    :host {
      display: block;
      margin-block-end: var(--quader-space-5);
    }
    .keyword-chips {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2);
      margin: 0;
      padding: 0;
      list-style: none;
    }
    .keyword-chip {
      display: inline-block;
      max-inline-size: 100%;
      padding: 0 var(--quader-space-3);
      overflow: hidden;
      border: 1px solid var(--quader-border-strong);
      border-radius: var(--quader-radius-full);
      background: var(--quader-surface-raised);
      color: var(--quader-on-surface);
      font-size: 0.8125rem;
      line-height: 1.5rem;
      text-decoration: none;
      text-overflow: ellipsis;
      white-space: nowrap;

      &:hover {
        border-color: var(--quader-link);
        color: var(--quader-link);
      }

      &:focus-visible {
        outline: var(--quader-focus-ring);
        outline-offset: 2px;
      }
    }
  `,
})
export class KeywordChips {
  readonly keywords = input.required<readonly string[]>();
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  protected readonly label = $localize`:@@pkg.meta.keywords:Keywords`;
}

/**
 * A version's links in one row under its facts line, in every format (R-UI-221, Q-UI-1313): homepage, documentation, source and issue
 * tracker, each by its label, opening the other site. An address that is not http or https is text with its label.
 */
@Component({
  selector: 'quader-version-links',
  template: `
    <ul class="version-links dot-separated" [attr.aria-label]="label">
      @for (l of links(); track l.label) {
        <li>
          @if (l.href; as href) {
            <a class="external-link" [href]="href" [attr.rel]="rel" target="_blank" [attr.title]="l.text"
              >{{ l.label }}<span class="material-icons" aria-hidden="true">open_in_new</span
              ><span class="visually-hidden" i18n="@@pkg.meta.external">(opens another site)</span></a
            >
          } @else {
            <span
              >{{ l.label }}:&ngsp;<span translate="no">{{ l.text }}</span></span
            >
          }
        </li>
      }
    </ul>
  `,
  styles: `
    .version-links {
      display: flex;
      flex-wrap: wrap;
      margin-block: var(--quader-space-1) 0;
      padding: 0;
      list-style: none;

      li {
        overflow-wrap: anywhere;
      }
    }
    .external-link {
      display: inline-flex;
      align-items: center;
      gap: 2px;

      .material-icons {
        font-size: 0.875rem;
      }
    }
  `,
})
export class VersionLinks {
  readonly links = input.required<readonly MetadataLink[]>();
  protected readonly rel = EXTERNAL_REL;
  protected readonly label = $localize`:@@pkg.meta.links:Links`;
}
