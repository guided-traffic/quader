import { Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { ApiError } from '../../api/problem';
import { UpstreamDownloadsApi } from '../../api/upstream-downloads-api';
import { PageVisibility } from '../../core/page-visibility';
import { SessionStore } from '../../core/session-store';
import { countText } from './download-words';
import { UpstreamDownloadsPanel } from './upstream-downloads-panel';

/** How often the closed panel's button reads the number of running downloads while the window is seen (R-UI-210). */
export const SUMMARY_POLL_MS = 10_000;
/** The capability that reads the downloads of every repository (operations-and-observability.md R-OPS-052). */
export const DOWNLOADS_CAPABILITY = 'content:metadata-read';

/**
 * The button **Downloads** of the top bar (api-and-ui.md R-UI-210): shown to a signed-in holder of `content:metadata-read` only,
 * between **Documentation** and the theme. While downloads from upstreams run, a badge on its icon carries their number ("99+" above
 * 99), and its name says it ("Downloads, 3 running"). It reads the count every 10 s while the panel is closed and the window is seen,
 * and at once when the window is seen again; the open panel's list keeps the count. A refusal (403) hides the button until the next
 * sign-in; a failed read keeps the last count. It opens the panel in the drawer from the right (R-UI-211).
 */
@Component({
  selector: 'quader-upstream-downloads-button',
  imports: [UpstreamDownloadsPanel],
  template: `
    @if (shown()) {
      <button
        type="button"
        class="btn btn-icon topbar-icon downloads-button"
        aria-haspopup="dialog"
        [attr.aria-expanded]="open()"
        [attr.aria-controls]="open() ? panelId : null"
        [attr.aria-label]="name()"
        [attr.title]="name()"
        (click)="open.set(!open())"
      >
        <span class="material-icons" aria-hidden="true">download</span>
        @if (count() > 0) {
          <span class="downloads-count" aria-hidden="true">{{ countText(count()) }}</span>
        }
      </button>
      <quader-upstream-downloads-panel [(open)]="open" [panelId]="panelId" (counted)="count.set($event)" (refused)="refuse()" />
    }
  `,
  styles: `
    :host {
      display: contents;
    }
    .downloads-button {
      position: relative;
    }
    /* The count from the glyph's middle towards the end, above its arrow, so a wide 99+ covers neither the arrow nor the next icon, in the information colour of the badges (readable in both schemes); no motion. */
    .downloads-count {
      position: absolute;
      inset-block-start: -0.375rem;
      inset-inline-start: calc(50% + 0.125rem);
      box-sizing: border-box;
      min-inline-size: 1rem;
      block-size: 1rem;
      padding-inline: 0.1875rem;
      border-radius: var(--quader-radius-full);
      background: var(--quader-badge-info-bg);
      color: var(--quader-badge-info-fg);
      font-size: 0.6875rem;
      font-weight: 600;
      line-height: 1rem;
      text-align: center;
      /* A ring of the bar's fill sets the count apart from the glyph it touches. */
      box-shadow: 0 0 0 2px var(--quader-nav-bg);
      pointer-events: none;
    }
  `,
})
export class UpstreamDownloadsButton {
  private readonly api = inject(UpstreamDownloadsApi);
  private readonly session = inject(SessionStore);
  private readonly page = inject(PageVisibility);

  protected readonly panelId = 'upstream-downloads-panel';
  protected readonly open = signal(false);
  protected readonly count = signal(0);
  /** The principal whose read was refused (403): the button stays hidden until another sign-in. */
  private readonly refusedFor = signal<string | null>(null);
  private readonly principal = computed(() => (this.session.authenticated() ? (this.session.whoami()?.principal.id ?? null) : null));

  protected readonly shown = computed(() => {
    const principal = this.principal();
    return principal !== null && this.session.has(DOWNLOADS_CAPABILITY) && this.refusedFor() !== principal;
  });

  protected readonly name = computed(() => {
    const n = this.count();
    return n > 0
      ? $localize`:@@upstream-downloads.button.running:Downloads, ${n}:count: running`
      : $localize`:@@upstream-downloads.button:Downloads`;
  });

  protected readonly countText = countText;

  constructor() {
    effect((onCleanup) => {
      if (!this.shown() || !this.page.visible() || this.open()) return;
      untracked(() => void this.poll());
      const timer = setInterval(() => void this.poll(), SUMMARY_POLL_MS);
      onCleanup(() => clearInterval(timer));
    });
    // A new principal starts without a count of the one before.
    effect(() => {
      this.principal();
      untracked(() => this.count.set(0));
    });
  }

  private async poll(): Promise<void> {
    try {
      this.count.set((await this.api.summary()).running);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) this.refuse();
      // Any other failure keeps the last count; the next tick reads again.
    }
  }

  protected refuse(): void {
    this.open.set(false);
    this.refusedFor.set(this.principal());
  }
}
