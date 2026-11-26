import { Component, computed, input } from '@angular/core';
import { NoValue } from '../../shared/ui/no-value';
import { formatSize } from '../../features/packages/format-size';

/**
 * The size of a version as the API states it (R-API-080): a number in binary units with the bytes as its title; a size that counts
 * only the parts the repository holds (the platforms of an image index fetched so far, Q-OCI-221) as "at least" that number, with a
 * title that says what it leaves out; a size that is not known as the shared "no value" with a title that says why, never "0 B".
 * The words fit every format, because only an image index has a partial or an unknown size today.
 */
@Component({
  selector: 'quader-version-size',
  imports: [NoValue],
  template: `
    @if (bytes() === null && spelled()) {
      <span [attr.title]="unknownTitle" i18n="@@pkg.size.not-known">Size not known</span>
    } @else if (bytes() === null) {
      <span [attr.title]="unknownTitle"><quader-no-value /></span>
    } @else if (partial()) {
      <span class="nowrap" [attr.title]="partialTitle()"
        ><span aria-hidden="true">≥&#8239;</span><span class="visually-hidden"
          ><ng-container i18n="@@pkg.size.at-least">at least</ng-container>&ngsp;</span
        >{{ text() }}</span
      >
    } @else {
      <span class="nowrap" [attr.title]="bytes() + ' B'">{{ text() }}</span>
    }
  `,
  styles: `
    .nowrap {
      white-space: nowrap;
    }
  `,
})
export class VersionSize {
  /** The size in bytes; null when it is not known. */
  readonly bytes = input.required<number | null>();
  /** The size counts only the parts the repository holds. */
  readonly partial = input(false);
  /** A size that is not known is said in words: in a line of facts, where a dash between two separators would say nothing. */
  readonly spelled = input(false);

  protected readonly text = computed(() => formatSize(this.bytes() ?? 0));
  protected readonly partialTitle = computed(
    () =>
      $localize`:@@pkg.size.partial-title:${this.bytes() ?? 0}:bytes: B: the platforms of this image fetched so far. The others are not counted until a client fetches them.`,
  );
  protected readonly unknownTitle = $localize`:@@pkg.size.unknown-title:Not known yet: no platform of this image has been fetched, and the size of a platform is known only once it is.`;
}
