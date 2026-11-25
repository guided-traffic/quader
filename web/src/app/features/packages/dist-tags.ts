import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Tooltip } from '../../shared/ui/tooltip';

/** How many tags a row of versions shows before "+N more" (Q-UI-1191): a package may point five names at one version. */
export const TAGS_PER_ROW = 3;

/**
 * The tags of a version in the order they stand, on every screen (Q-UI-1190): `latest` first, the version a plain `npm install`
 * takes, then the others by name, as the server names them to its filter (Q-CUR-182).
 */
export function orderTags(tags: readonly string[]): string[] {
  const rest = tags.filter((t) => t !== 'latest').sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
  return tags.includes('latest') ? ['latest', ...rest] : rest;
}

/** The tags that point at each version, from the package's tags (tag to version, npm's dist-tags); a version no tag names is not in it. */
export function tagsByVersion(tags: Readonly<Record<string, string>> | null | undefined): ReadonlyMap<string, readonly string[]> {
  const out = new Map<string, string[]>();
  for (const [tag, version] of Object.entries(tags ?? {})) out.set(version, [...(out.get(version) ?? []), tag]);
  return new Map([...out].map(([version, list]) => [version, orderTags(list)]));
}

/** What a row shows of a version's tags: the first `limit` (every one for null) and the rest, which the "+N more" names. */
export function shownDistTags(tags: readonly string[], limit: number | null): { readonly shown: readonly string[]; readonly hidden: readonly string[] } {
  const ordered = orderTags(tags);
  if (limit === null || ordered.length <= limit) return { shown: ordered, hidden: [] };
  return { shown: ordered.slice(0, limit), hidden: ordered.slice(limit) };
}

/**
 * The npm tags of one version as badges (R-UI-110, R-CUR-018, Q-UI-1190): the names an upstream's maintainer points at one version
 * each, where `npm install <package>@<tag>` installs that version. Each badge says so to a pointer and on focus by the shared tooltip,
 * which also describes it to assistive technology; at most `limit` of them and then "+N more", whose tooltip names the others
 * (Q-UI-1191). A name is the upstream's: never translated, and a name wider than its cell alone is cut with an ellipsis, the whole
 * in its tooltip. The host is no box of its own: the badges are items of the cell's line beside the version, and wrap as its items.
 * The package's page, a version's page and the tab Upstream draw them with this one component.
 */
@Component({
  selector: 'quader-dist-tags',
  imports: [Tooltip],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'quader-dist-tags' },
  template: `
    @for (t of view().shown; track t) {
      <span class="badge info dist-tag" tabindex="0" [quaderTooltip]="tagText(t)"><span class="name" translate="no">{{ t }}</span></span>
    }
    @if (view().hidden.length > 0) {
      <span class="more" tabindex="0" [quaderTooltip]="othersText()">{{ moreText() }}</span>
    }
  `,
  styles: `
    :host {
      display: contents;
    }
    /* A grid of one track of at least 0 and at most the name's width: the badge is as wide as its name where the cell has the room,
       and asks no least width of its column, so a long name never makes the list stand as blocks (German at 1,024 px with a name of
       35 characters, Q-UI-1191); where the cell is narrower the name is cut with an ellipsis, the whole in the tooltip. */
    .dist-tag {
      display: inline-grid;
      grid-template-columns: minmax(0, max-content);
      max-inline-size: 100%;
      min-inline-size: 0;
    }
    .name {
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .more {
      color: var(--quader-text-muted);
      font-weight: 400;
      white-space: nowrap;
    }
  `,
})
export class DistTags {
  /** The tags that point at the version. */
  readonly tags = input.required<readonly string[]>();
  /** The package's name, for the command the tooltip names. */
  readonly package = input.required<string>();
  /** How many badges stand before "+N more"; null for every one. */
  readonly limit = input<number | null>(TAGS_PER_ROW);

  protected readonly view = computed(() => shownDistTags(this.tags(), this.limit()));

  protected tagText(tag: string): string {
    const command = `npm install ${this.package()}@${tag}`;
    return $localize`:@@pkg.dist-tag.title:npm tag “${tag}:tag:”: ${command}:command: installs this version.`;
  }

  protected readonly moreText = computed(() => $localize`:@@pkg.dist-tag.more:+${this.view().hidden.length}:count: more`);
  protected readonly othersText = computed(() => $localize`:@@pkg.dist-tag.others:Further npm tags of this version: ${this.view().hidden.join(', ')}:tags:`);
}
