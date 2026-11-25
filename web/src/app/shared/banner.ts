import { Component, DestroyRef, inject } from '@angular/core';
import { BannerStore, Notice } from '../core/banner-store';
import { parseBannerText } from './banner-text';

/**
 * The notices above the content (R-UI-002, R-UI-033): the system banner and, while the installation is read-only, the maintenance
 * message. Renders nothing while there is none. A message is text and `https` links only, bound as text and as `href` (R-OPS-025);
 * the severity is an icon and a word as well as a colour (R-UI-044).
 */
@Component({
  selector: 'quader-banner',
  template: `
    @for (notice of store.notices(); track notice.kind) {
      <section
        class="banner"
        role="region"
        [attr.data-kind]="notice.kind"
        [attr.data-severity]="notice.severity"
        [attr.aria-label]="label(notice)"
      >
        <span class="material-icons icon" aria-hidden="true">{{ icon(notice) }}</span>
        <!-- The parts stand as the message writes them: a text part in its own element, so the template's line breaks around it
             add no space ("the change record CHG-2026-1184 ." stood with a space before its full stop, 2026-11-25). The message is the
             administrator's, in the language they wrote it: shown as written, never as a text of the page to translate (R-UI-214). -->
        <p>
          <strong class="lead">{{ lead(notice) }}</strong>&ngsp;
          @for (part of parts(notice); track $index) {
            @if (part.kind === 'link') {
              <a [href]="part.href" target="_blank" rel="noopener noreferrer" translate="no">{{ part.text }}</a>
            } @else {
              <span translate="no">{{ part.text }}</span>
            }
          }
        </p>
      </section>
    }
  `,
  styles: `
    .banner {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
      padding: var(--quader-space-2) var(--quader-space-4);
      background: var(--quader-banner-bg);
      color: var(--quader-banner-fg);
      white-space: pre-line;
    }
    .banner[data-severity='warning'] {
      background: var(--quader-badge-warn-bg);
      color: var(--quader-badge-warn-fg);
    }
    .banner[data-severity='critical'] {
      background: var(--quader-badge-bad-bg);
      color: var(--quader-badge-bad-fg);
    }
    p {
      margin: 0;
    }
    /* The notice's colour, on the sign-in page too, whose links are the link colour (_shell.scss, .bare a). */
    .banner a:not(.btn) {
      color: inherit;
      text-decoration: underline;
    }
    /* The icon is a line of the message high, so its middle is the first line's (R-UI-175): at its own 18 px it stood 1.8 px high. */
    .icon {
      flex: none;
      font-size: 1.25em;
      line-height: var(--quader-line);
    }
  `,
})
export class Banner {
  protected readonly store = inject(BannerStore);

  constructor() {
    this.store.watch(inject(DestroyRef));
  }

  protected parts(n: Notice) {
    return parseBannerText(n.message);
  }

  protected icon(n: Notice): string {
    if (n.kind === 'maintenance') return 'construction';
    return n.severity === 'info' ? 'info' : n.severity === 'warning' ? 'warning' : 'error';
  }

  protected label(n: Notice): string {
    return n.kind === 'maintenance'
      ? $localize`:@@banner.maintenance-label:Maintenance notice`
      : $localize`:@@banner.label:System notice`;
  }

  /** The words that carry the severity and the title, so the colour is never the only signal. */
  protected lead(n: Notice): string {
    if (n.kind === 'maintenance') {
      return $localize`:@@banner.maintenance-lead:Read-only maintenance mode.`;
    }
    const title = n.title;
    switch (n.severity) {
      case 'critical':
        return title === ''
          ? $localize`:@@banner.critical:Critical.`
          : $localize`:@@banner.critical-title:Critical: ${title}:title:.`;
      case 'warning':
        return title === ''
          ? $localize`:@@banner.warning:Warning.`
          : $localize`:@@banner.warning-title:Warning: ${title}:title:.`;
      default:
        return title === ''
          ? $localize`:@@banner.info:Notice.`
          : $localize`:@@banner.info-title:Notice: ${title}:title:.`;
    }
  }
}
