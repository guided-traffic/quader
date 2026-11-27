import { Component, computed, inject, input, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { LIVE_IDLE_MS, live, refreshResource } from '../../core/live-refresh';
import { PackageDependent } from '../../api/packages-api';
import { PackageReads } from '../../api/package-reads';
import { ErrorNotice } from '../../shared/error-notice';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { Loading } from '../../shared/ui/loading';
import { versionCommands } from '../../features/packages/package-links';
import { MalwareMark } from '../../shared/malware/malware-mark';
import { MalwareSignaturesStore } from '../../core/malware-signatures';

/** A part of a relation as the section writes it: the kind in the format's word, the name asked for where it is another, the range. */
export interface RelationPart {
  readonly text: string;
  readonly cls: 'kind' | 'via' | 'secondary-inline';
}

/**
 * The relations of a dependent as the section writes them, each its non-empty parts: the kind (NuGet and Ansible Galaxy have one list
 * and none), the name it asks for only where the package provides it (Debian), the range muted.
 */
export function relationsOf(p: PackageDependent, name: string): RelationPart[][] {
  return p.relations.map((r) =>
    [
      { text: r.kind, cls: 'kind' as const },
      { text: r.via === name ? '' : r.via, cls: 'via' as const },
      { text: r.range, cls: 'secondary-inline' as const },
    ].filter((x) => x.text !== ''),
  );
}

/**
 * The section **Used by** of a version's page (R-API-086, Q-UI-1350): the packages of the repositories the reader may read that depend on
 * the package, in the block and the words of its dependencies (R-UI-221): one line each, the package a link to its version that depends,
 * the kind as the format names it (a development dependency says so), the name it asks for where the package provides it (Debian), the
 * range muted. At most 50, then how many more, and the package list of the project filtered by the dependency. What it says when there
 * is none claims no more than the reader may know. Every text of a dependent is a publisher's, shown as text. It reads again while it is
 * seen (R-UI-213).
 */
@Component({
  selector: 'quader-used-by',
  imports: [ErrorNotice, Loading, MalwareMark, RouterLink],
  template: `
    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="failure.retry()" />
    } @else if (answer(); as a) {
      @if (a.items.length === 0) {
        <p class="dependency-none" data-testid="used-by-none">
          {{ a.whole ? noneWhole : noneReadable }}
        </p>
      } @else {
        <ul class="dependency-list" data-testid="used-by">
          @for (p of a.items; track p.packageId) {
            <li>
              <a class="dependent" [routerLink]="link(p)" translate="no"
                >{{ p.name }}&ngsp;<span class="dependent-version">{{ p.version }}</span></a
              >
              &ngsp;<span class="relations dot-separated" translate="no">
                @for (r of relations(p); track $index; let lead = $first) {
                  @if (!lead) {
                    &ngsp;
                  }
                  <span class="relation">
                    @for (x of r; track $index; let first = $first) {
                      @if (!first) {
                        &ngsp;
                      }
                      <span [class]="x.cls">{{ x.text }}</span>
                    }
                  </span>
                }
              </span>
              <!-- The dependent version's malware scan in one word, its details on demand (R-UI-255). -->
              @if (marks && p.malware; as m) {
                &ngsp;<quader-malware-mark class="dependent-malware" [malware]="m" kind="version" [image]="p.format === 'oci'" />
              }
            </li>
          }
        </ul>
        @if (a.total > a.items.length) {
          <p class="used-by-more" data-testid="used-by-more">
            {{ moreText(a.total - a.items.length) }}&ngsp;<a
              [routerLink]="listLink()"
              [queryParams]="listQuery()"
              i18n="@@pkg.used-by.list"
              >Packages of this project that use it</a
            >
          </p>
        }
      }
    } @else {
      <quader-loading i18n="@@pkg.used-by.loading"
        >Loading the packages that use it.</quader-loading
      >
    }
  `,
  styles: `
    :host {
      display: block;
      margin-block-end: var(--quader-space-5);
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
    /* The relations of a dependent stand after its version, each kept on one line, apart by a middle dot as the facts line's are. */
    /* The link names the dependent and its version that depends: the page it opens. */
    .dependent-version {
      font-size: 0.875em;
    }
    /* The relations after the version, apart by the line's middle dot, which no wrapped line starts with (.dot-separated). */
    .relations {
      margin-inline-start: calc(var(--quader-space-3) - var(--quader-dot));
      vertical-align: baseline;
    }
    .relation {
      white-space: nowrap;
    }
    .dependent-malware {
      margin-inline-start: var(--quader-space-2);
    }
    .dependency-none,
    .used-by-more {
      margin: var(--quader-space-1) 0 0;
    }
    .dependency-none {
      color: var(--quader-text-muted);
    }
  `,
})
export class UsedBy {
  private readonly api = inject(PackageReads);
  /** The dependents carry their malware state in the main installation's web (R-MAL-039); the Bastion's are W-36's P8. */
  protected readonly marks = inject(MalwareSignaturesStore).available;

  readonly packageId = input.required<string>();
  /** The package's normalized name: the filter of the package list, and the name a relation that asks for it needs not repeat. */
  readonly name = input.required<string>();
  readonly format = input.required<string>();
  /** The project of the page: the links stay in it, and its package list shows the dependents past the bound. */
  readonly project = input.required<string>();

  protected readonly dependents = resource({
    params: () => this.packageId(),
    loader: ({ params }) => this.api.dependents(params),
  });
  protected readonly answer = computed(() => valueOf(this.dependents));
  protected readonly failure = failureOf(this.dependents);
  protected readonly listLink = computed(() => ['/projects', this.project(), 'packages']);
  protected readonly listQuery = computed(() => ({
    format: this.format(),
    dependsOn: this.name(),
  }));
  protected readonly noneWhole = $localize`:@@pkg.used-by.none-whole:No package the installation holds depends on it.`;
  protected readonly noneReadable = $localize`:@@pkg.used-by.none-readable:No package you may read depends on it.`;

  constructor() {
    // Packages are published and cached while the page is open: the section reads again in place (R-UI-213).
    live({
      reload: async () => {
        const id = this.packageId();
        if (this.dependents.hasValue())
          await refreshResource(this.dependents, () => this.api.dependents(id));
      },
      every: () => LIVE_IDLE_MS,
    });
  }

  protected relations(p: PackageDependent): RelationPart[][] {
    return relationsOf(p, this.name());
  }

  protected link(p: PackageDependent): string[] {
    return versionCommands(this.project(), p.format, p.key, p.version);
  }

  protected moreText(n: number): string {
    const count = new Intl.NumberFormat($localize.locale || 'en-US').format(n);
    return $localize`:@@pkg.used-by.more:and ${count}:count: more.`;
  }
}
