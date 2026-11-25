import { HttpInterceptorFn } from '@angular/common/http';
import {
  ComponentRef,
  DestroyRef,
  Directive,
  ElementRef,
  ViewContainerRef,
  computed,
  effect,
  inject,
  input,
  signal,
  untracked,
} from '@angular/core';
import { finalize } from 'rxjs';
import { Spinner } from './spinner';

/** The busy button the person pressed last, of every screen: an act that several buttons share a flag for spins only in this one. */
const press: { last: Busy | null } = { last: null };
/** How soon after its press a read counts as a button's act. */
const READ_AFTER_PRESS_MS = 100;

/** The document's listener that ends the claim of `press.last` when the person presses something else, installed once. */
let listening = false;

/**
 * A press anywhere but in the busy button pressed last, or in an overlay it opened (its menu, its confirmation), ends that button's claim
 * on the writes that follow: a write that a switch or another control starts then spins no button (Q-UI-1235).
 */
function listen(doc: Document): void {
  if (listening) return;
  listening = true;
  doc.addEventListener(
    'click',
    (e) => {
      const target = e.target instanceof Element ? e.target : null;
      if (!press.last || !target) return;
      if (press.last.contains(target) || target.closest('.cdk-overlay-container')) return;
      if (!target.closest('[quaderBusy], .quader-busy-host')) press.last = null;
    },
    true,
  );
}

/**
 * The writes of the app (every call but a GET or a HEAD) that start while a busy button without a flag of its own holds the person's
 * last press are that button's act: it spins until they have answered (R-UI-217, Q-UI-1235). A read is its act only when it starts
 * within `READ_AFTER_PRESS_MS` of the press (an export, a dry run), so that a live view's read again (R-UI-213) spins no button.
 * Installed in `app.config.ts` beside the interceptors of R-UI-042.
 */
export const busyActInterceptor: HttpInterceptorFn = (req, next) => {
  const read = req.method === 'GET' || req.method === 'HEAD';
  const owner = press.last?.claim(read) ?? null;
  if (!owner) return next(req);
  owner.begin();
  return next(req).pipe(finalize(() => owner.end()));
};

/**
 * A button whose act waits for the server (R-UI-217, Q-UI-1235): while it is busy, the button the person pressed is `aria-busy` and the
 * spinner stands in it, before its label or in place of its icon; the label stays. Two forms:
 *
 *   <button class="btn btn-confirm" type="submit" [disabled]="saving()" [quaderBusy]="saving()" i18n>Save changes</button>
 *   <button class="btn btn-danger" type="button" quaderBusy (click)="remove()" i18n>Delete</button>
 *
 * - **With a flag** (`[quaderBusy]="saving()"`): busy while the flag holds. The button names its own `[disabled]` with the same flag, so
 *   a second click starts nothing (`busy-rule.mjs` of `make web-lint`). Where several buttons of a screen share one flag (a user's
 *   page: Save, Disable, Unlock, Delete are all `busy()`), each carries it and only the one that was pressed shows the spinner.
 * - **Without a flag** (`quaderBusy`): busy while the writes of the app that its press started are open (`busyActInterceptor`): the
 *   act's confirmation, its menu and its dialog may stand between the press and the write. While it is busy it is `aria-disabled` and
 *   a click does nothing. A press elsewhere ends its claim, so a write that something else starts spins no button. A menu's trigger
 *   carries it for the acts of its menu.
 *
 * A form sent with Enter presses its submit button (the browser's implicit submission). The spinner is created when the act starts and
 * is hidden for 300 ms by the global stylesheet (`.btn > .quader-spinner`, display none until then, Q-UI-1230): an act that answers at
 * once changes nothing on the button, one that takes longer widens a button without an icon by the spinner and its gap, as the
 * reference's busy button does, and takes the place of the icon of one with an icon.
 */
@Directive({
  selector: '[quaderBusy]',
  host: {
    class: 'quader-busy-host',
    '[attr.aria-busy]': 'shown() ? "true" : null',
    '[attr.aria-disabled]': 'shown() && auto() ? "true" : null',
    '[class.is-busy]': 'shown()',
  },
})
export class Busy {
  /** True while the act runs; empty (the bare attribute) for a button whose writes say it. */
  readonly quaderBusy = input<boolean | ''>('');

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly container = inject(ViewContainerRef);
  private readonly mine = signal(false);
  private readonly writes = signal(0);
  protected readonly auto = computed(() => this.quaderBusy() === '');
  protected readonly shown = computed(() => (this.auto() ? this.writes() > 0 : this.quaderBusy() === true && this.mine()));
  private spinner: ComponentRef<Spinner> | null = null;
  private pressedAt = 0;

  constructor() {
    const el = this.host.nativeElement;
    listen(el.ownerDocument);
    // In the capture phase, so that a press while busy stops before the button's own (click) runs.
    const onPress = (e: Event): void => this.pressed(e);
    el.addEventListener('click', onPress, true);
    effect(() => {
      if (this.auto()) return;
      if (this.quaderBusy() === true) {
        this.mine.set(press.last === this);
      } else {
        // The act this button started has ended: a later act that no button starts spins none.
        if (untracked(this.mine) && press.last === this) press.last = null;
        this.mine.set(false);
      }
    });
    effect(() => {
      if (this.shown()) this.show();
      else this.hide();
    });
    inject(DestroyRef).onDestroy(() => {
      el.removeEventListener('click', onPress, true);
      this.hide();
      if (press.last === this) press.last = null;
    });
  }

  /** Whether the element is this button or inside it. */
  contains(target: Element): boolean {
    return this.host.nativeElement.contains(target);
  }

  /** The button that takes the call starting now: itself while it holds the last press and has no flag of its own. */
  claim(read: boolean): Busy | null {
    if (!this.auto() || !this.host.nativeElement.isConnected) return null;
    return !read || performance.now() - this.pressedAt <= READ_AFTER_PRESS_MS ? this : null;
  }

  begin(): void {
    this.writes.update((n) => n + 1);
  }

  end(): void {
    this.writes.update((n) => Math.max(0, n - 1));
    if (untracked(this.writes) === 0 && press.last === this) press.last = null;
  }

  private pressed(event: Event): void {
    if (this.auto() && this.shown()) {
      // Busy: the act runs, a second press starts nothing.
      event.preventDefault();
      event.stopImmediatePropagation();
      return;
    }
    press.last = this;
    this.pressedAt = performance.now();
  }

  private show(): void {
    if (this.spinner) return;
    const ref = this.container.createComponent(Spinner);
    ref.setInput('size', 'button');
    // Created beside the button by the container and moved into it, first, where an icon or the label starts.
    this.host.nativeElement.prepend(ref.location.nativeElement);
    this.spinner = ref;
  }

  private hide(): void {
    this.spinner?.destroy();
    this.spinner = null;
  }
}
