import { Dialog, DialogConfig, DialogRef } from '@angular/cdk/dialog';
import { Overlay } from '@angular/cdk/overlay';
import { Component, DestroyRef, TemplateRef, effect, inject, input, model, output, untracked, viewChild } from '@angular/core';
import { firstValueFrom } from 'rxjs';

let nextDrawerId = 1;

/**
 * A panel that slides in from the right over the page (Q-UI-420, ADR-200): the page behind it keeps its layout, so a list keeps
 * its width and its columns. It is the CDK dialog, at the window's right edge and its full height: a modal `role="dialog"` named
 * by its heading, the focus moved in (to the heading) and trapped inside, Escape, the close button and a click beside it close
 * it, the focus returns to the element that opened it, the page behind it does not scroll and is hidden from assistive
 * technology. It opens and closes with the motion of every panel (`drawer-in`, `drawer-out`, `_controls.scss`).
 *
 * The content is projected: actions for its head (`[drawerActions]`) and the body. The drawer owns no values; whoever opens it
 * binds `[(open)]`. `closed` says that it closed, for whatever reason.
 */
@Component({
  selector: 'quader-drawer',
  template: `
    <ng-template #content>
      <div class="drawer">
        <header class="drawer-head">
          <h2 class="drawer-title" [id]="titleId" tabindex="-1">{{ heading() }}</h2>
          <ng-content select="[drawerActions]" />
          <button type="button" class="btn btn-tertiary btn-icon drawer-close" [attr.aria-label]="closeLabel()" (click)="close()">
            <span class="material-icons" aria-hidden="true">close</span>
          </button>
        </header>
        <ng-content />
      </div>
    </ng-template>
  `,
  host: { class: 'quader-drawer-host' },
})
export class QuaderDrawer {
  private readonly dialog = inject(Dialog);
  private readonly overlay = inject(Overlay);
  private readonly content = viewChild.required<TemplateRef<unknown>>('content');

  readonly open = model(false);
  readonly heading = input.required<string>();
  readonly closeLabel = input($localize`:@@drawer.close:Close`);
  /** The id of the dialog, for an `aria-controls` that points at it. */
  readonly drawerId = input<string | null>(null);
  /** The drawer's width, a CSS length; never wider than the window (the downloads of the top bar take `min(30rem, 100vw)`). */
  readonly width = input('min(24rem, 100vw)');
  /** Emitted once the drawer has closed: by its button, Escape, a click beside it, or `open` set to false. */
  readonly closed = output<void>();

  /** The id of the drawer's heading, which names the dialog. */
  readonly titleId = `quader-drawer-title-${nextDrawerId++}`;

  private ref: DialogRef<unknown, unknown> | null = null;
  private closing = false;
  private destroyed = false;

  constructor() {
    effect(() => {
      const open = this.open();
      untracked(() => (open ? this.show() : void this.hide()));
    });
    inject(DestroyRef).onDestroy(() => this.dispose());
  }

  /** Closes the drawer with its motion; the binding follows. */
  close(): void {
    this.open.set(false);
  }

  private show(): void {
    if (this.ref) return;
    const config = new DialogConfig<unknown, DialogRef<unknown, unknown>>();
    const id = this.drawerId();
    if (id) config.id = id;
    config.role = 'dialog';
    config.ariaModal = true;
    config.ariaLabelledBy = this.titleId;
    config.autoFocus = `#${this.titleId}`;
    config.restoreFocus = true;
    // Escape and the backdrop close through `close()`, so the drawer slides out before the CDK removes it.
    config.disableClose = true;
    config.hasBackdrop = true;
    config.panelClass = 'quader-drawer';
    config.backdropClass = 'quader-drawer-backdrop';
    config.width = this.width();
    config.height = '100dvh';
    config.positionStrategy = this.overlay.position().global().top('0').right('0');
    config.scrollStrategy = this.overlay.scrollStrategies.block();
    const ref = this.dialog.open(this.content(), config);
    this.ref = ref;
    // The focus is trapped in the pane, so its keys reach the pane; the backdrop is the page beside it. Both go with the overlay.
    ref.overlayRef.overlayElement.addEventListener('keydown', (event) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      this.close();
    });
    ref.overlayRef.backdropElement?.addEventListener('click', () => this.close());
    void firstValueFrom(ref.closed).then(() => {
      if (this.ref === ref) this.ref = null;
      // A drawer that goes with its page emits nothing after it is destroyed.
      if (this.destroyed) return;
      if (this.open()) this.open.set(false);
      this.closed.emit();
    });
  }

  private async hide(): Promise<void> {
    const ref = this.ref;
    if (!ref || this.closing) return;
    this.closing = true;
    const pane = ref.overlayRef.overlayElement;
    ref.overlayRef.addPanelClass('closing');
    ref.overlayRef.backdropElement?.classList.add('closing');
    // The motion runs where the browser animates; a test's DOM and a reduced-motion setting have none to wait for.
    const motions = typeof pane.getAnimations === 'function' ? pane.getAnimations({ subtree: true }) : [];
    await Promise.all(motions.map((m) => m.finished.catch(() => undefined)));
    this.closing = false;
    if (this.open()) {
      // Opened again while it slid out: it stays.
      ref.overlayRef.removePanelClass('closing');
      ref.overlayRef.backdropElement?.classList.remove('closing');
      return;
    }
    ref.close();
  }

  private dispose(): void {
    this.destroyed = true;
    this.ref?.close();
    this.ref = null;
  }
}
