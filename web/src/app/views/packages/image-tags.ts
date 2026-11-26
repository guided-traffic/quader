import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { SignatureStatus } from '../../api/signatures-api';
import { SignatureBadge } from '../signatures/signature-badge';

/**
 * Whether a format's lists of versions are lists by content (`group=content`, Q-API-432): one row per content its versions name, led
 * by the highest, with the others as its aliases. OCI is the one format whose versions name content (an image's tags and its manifest),
 * and its lists show one row per image (the owner's word of 2026-11-25, Q-UI-1130); every other format lists its versions.
 */
export function listsByContent(format: string): boolean {
  return format === 'oci';
}

/** What a row shows of an image's other tags: the tags, how many it leaves out, and whether the answer named all of them. */
export interface ShownTags {
  readonly shown: readonly string[];
  /** The tags of the answer the row leaves out (past `limit`). */
  readonly hidden: number;
  /** The answer names 100 tags and the image has more (`aliasesMore`): how many is not known. */
  readonly more: boolean;
}

/** The other tags a row shows (Q-UI-1130): every one, or the first `limit` of them. */
export function shownTags(aliases: readonly string[], more: boolean, limit: number | null): ShownTags {
  const shown = limit === null ? aliases : aliases.slice(0, limit);
  return { shown, hidden: aliases.length - shown.length, more };
}

/**
 * The cell of an image's row in a list by content (R-UI-052, R-UI-110, Q-UI-1130, Q-UI-1150): the leading tag as the link to its page
 * (R-UI-111) with **Pre-release** where it is one, the image's other tags muted after it, dot-separated (the style guide's separator,
 * so no line starts with a dot), at most `limit` of them and then "+N more", and the badge of the image's signatures (R-SIG-035), whose
 * signatures are the manifest's whichever tag leads. Tags wrap between tags; a tag breaks inside itself only when it alone is wider than
 * the cell (a tag is up to 128 characters). Where the list stands as blocks (R-UI-174) the leading tag, the other tags and the badge
 * each start a line of their own. Tags are data: never translated.
 */
@Component({
  selector: 'quader-image-tags',
  imports: [RouterLink, SignatureBadge],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <span class="tags dot-separated">
      <span class="lead"
        ><a [routerLink]="link()" translate="no">{{ tag() }}</a
        >@if (prerelease()) {&ngsp;<span class="badge" i18n="@@pkg.badge.prerelease">Pre-release</span>}</span
      >
      @for (t of view().shown; track t; let first = $first) {
        <span class="other"
          >@if (first) {<span class="visually-hidden">{{ othersLabel() }}: </span>}<span translate="no">{{ t }}</span></span
        >
      }
      @if (moreText(); as m) {
        <span class="other more">{{ m }}</span>
      }
    </span>
    <quader-signature-badge [state]="signatures()?.state" [stale]="signatures()?.stale ?? false" />
  `,
  styles: `
    /* The tags and the badge after them, wrapping as a whole; "signer not trusted" after an untrusted signer's Signed is the cell's
       next line (Q-UI-1076), as in the version's cell of a package's list. */
    :host {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-1) var(--quader-space-2);
    }
    quader-signature-badge {
      display: contents;
    }
    .other {
      min-inline-size: 0;
      color: var(--quader-text-muted);
      font-weight: 400;
      /* A tag is the flex line's item at its whole width, so it wraps to the next line as a whole; only one wider than the cell alone is
         shrunk to it, and breaks inside itself (R-UI-171). */
      overflow-wrap: anywhere;
    }
    .more {
      white-space: nowrap;
    }
    /* In a block (a stacked list, R-UI-174): the leading tag on its line, the other tags under it, the badge under them. */
    :host-context(.quader-data-table.stacked) .tags {
      flex-basis: 100%;
    }
    :host-context(.quader-data-table.stacked) .lead {
      flex-basis: 100%;
    }
  `,
})
export class ImageTags {
  /** The leading tag. */
  readonly tag = input.required<string>();
  /** The address of the leading tag's page (R-UI-111). */
  readonly link = input.required<readonly string[]>();
  readonly prerelease = input(false);
  /** The image's name, for the words assistive technology reads before its other tags. */
  readonly name = input.required<string>();
  /** The image's other tags, in the list's order (`aliases`). */
  readonly aliases = input<readonly string[]>([]);
  /** The answer names 100 other tags and the image has more (`aliasesMore`). */
  readonly more = input(false);
  /** How many other tags the row shows; null for every one. */
  readonly limit = input<number | null>(null);
  readonly signatures = input<SignatureStatus | null>(null);

  protected readonly view = computed(() => shownTags(this.aliases(), this.more(), this.limit()));
  protected readonly othersLabel = computed(() => $localize`:@@repo.content.other-tags:Other tags of ${this.name()}:name: ${this.tag()}:tag:`);
  /** "+3 more" for the tags the row leaves out; "+ more" where the answer did not name them all. */
  protected readonly moreText = computed(() => {
    const v = this.view();
    if (v.more) return $localize`:@@pkg.ver.more-tags-unnamed:+ more`;
    return v.hidden > 0 ? $localize`:@@repo.content.more-tags:+${v.hidden}:count: more` : null;
  });
}
