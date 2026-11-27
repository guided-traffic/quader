import { Component, computed, input, output } from '@angular/core';
import { Spinner } from './spinner';
import { Unavailable } from './unavailable';

/**
 * A switch (Q-UI-830, the style guide's Forms): something that is on or off and changes at once, without a save, such as a
 * well-known repository of **Upstream registries**. A native `<button role="switch">` with `aria-checked`, so Space and Enter
 * toggle it and a screen reader says its name and "on" or "off"; the name is `label` ("Switch on Docker Hub"), the visible word
 * after it (**On**, **Off**) repeats the state and is hidden from assistive technology. A checkbox stays the control of a
 * choice that a form saves.
 *
 * The control does not change itself: a click asks for the other state (`checkedChange`) and the screen sets `checked` from the
 * server's answer. `busy` holds it while that answer is open (`aria-busy`, no second click) and shows the spinner in place of the word
 * after the delay of every load (R-UI-217); `disabled` where it cannot change. `unavailable` is the reason a switch the Helm
 * configuration fixes cannot change (R-UI-270, Q-UI-1636): the switch stays in its place, greyed out, and says the reason as its
 * tooltip on hover, focus and tap (`quaderUnavailable` on its button); an empty reason leaves it as it is.
 * The look follows the reference's toggle: a track of 40 by 24 px, the accent's fill when on and the control's border colour
 * when off (never green, which is a positive status), a knob of 16 px with a check or a cross in the track's colour, the focus
 * ring of every control. It stands on the middle of its text's line (R-UI-175).
 *
 *   <quader-switch [checked]="on" [label]="'Switch on Docker Hub'" (checkedChange)="toggle($event)" />
 */
@Component({
  selector: 'quader-switch',
  template: `
    <button
      type="button"
      role="switch"
      class="switch"
      [attr.aria-checked]="checked() ? 'true' : 'false'"
      [attr.aria-label]="label()"
      [attr.aria-describedby]="describedBy()"
      [attr.aria-busy]="busy() ? 'true' : null"
      [disabled]="disabled() && !unavailable()"
      [quaderUnavailable]="unavailable()"
      (click)="toggle()"
    >
      <span class="knob" aria-hidden="true"><span class="material-icons">{{ checked() ? 'check' : 'close' }}</span></span>
    </button>
    @if (busy()) {
      <!-- The answer is open: the spinner in place of the word, or after the track where there is none (R-UI-217, Q-UI-1235). -->
      <quader-spinner class="switch-busy" [class.alone]="!showWord()" />
    }
    @if (showWord()) {
      <span class="word" aria-hidden="true" [class.busy]="busy()">{{ word() }}</span>
    }
  `,
  imports: [Spinner, Unavailable],
  host: { class: 'quader-switch' },
  styles: `
    :host {
      position: relative;
      display: inline-flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
      vertical-align: top;
      white-space: nowrap;
    }
    .switch {
      --quader-line: inherit;
      position: relative;
      box-sizing: border-box;
      flex: none;
      inline-size: 2.5rem;
      block-size: 1.5rem;
      /* On the middle of the line it stands in, and no taller than it (R-UI-175). */
      margin-block: calc((var(--quader-line) - 1.5rem) / 2);
      padding: 0;
      border: 0;
      border-radius: var(--quader-radius-full);
      background: var(--quader-control-border);
      cursor: pointer;
      transition: background-color var(--quader-motion-duration) var(--quader-motion-easing);
    }
    .switch:hover:not(:disabled, [aria-disabled='true']) {
      background: var(--quader-control-border-hover);
    }
    .switch[aria-checked='true'] {
      background: var(--quader-accent-fill);
    }
    .switch[aria-checked='true']:hover:not(:disabled, [aria-disabled='true']) {
      background: var(--quader-accent-fill-hover);
    }
    .switch:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
    .switch:disabled,
    .switch[aria-disabled='true'] {
      background: var(--quader-control-disabled-fg);
      cursor: not-allowed;
    }
    .switch[aria-busy='true'] {
      cursor: progress;
    }
    .knob {
      position: absolute;
      inset-block-start: 0.25rem;
      inset-inline-start: 0.25rem;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      inline-size: 1rem;
      block-size: 1rem;
      border-radius: var(--quader-radius-full);
      background: var(--quader-on-accent);
      color: var(--quader-control-border);
      transition: transform var(--quader-motion-duration) var(--quader-motion-easing);
    }
    .switch[aria-checked='true'] .knob {
      transform: translateX(1rem);
      color: var(--quader-accent-fill);
    }
    :host-context([dir='rtl']) .switch[aria-checked='true'] .knob {
      transform: translateX(-1rem);
    }
    .switch:is(:disabled, [aria-disabled='true']) .knob {
      color: var(--quader-control-disabled-fg);
    }
    .knob .material-icons {
      font-size: 0.75rem;
    }
    /* The spinner and the word swap after the delay of every load (Q-UI-1230); an answer within it changes nothing. */
    .switch-busy {
      animation: quader-busy-in 0s linear 300ms both;
    }
    .switch-busy.alone {
      position: absolute;
      inset-inline-start: calc(100% + var(--quader-space-2));
    }
    .word.busy {
      animation: quader-busy-out 0s linear 300ms both;
    }
  `,
})
export class Switch {
  readonly checked = input(false);
  /** The accessible name: what the switch switches ("Switch on Docker Hub"). */
  readonly label = input.required<string>();
  readonly disabled = input(false);
  /** A change is being saved: the switch holds until the answer. */
  readonly busy = input(false);
  /** Why the switch cannot change, as its tooltip (R-UI-270); empty where it can. */
  readonly unavailable = input('');
  /** The id of a note that describes the switch. */
  readonly describedBy = input<string | null>(null);
  /** The word **On** or **Off** after the switch; false where the place says the state otherwise. */
  readonly showWord = input(true);
  /** The state the person asked for; the screen sets `checked` once the server agreed. */
  readonly checkedChange = output<boolean>();

  protected readonly word = computed(() =>
    this.checked() ? $localize`:@@switch.on:On` : $localize`:@@switch.off:Off`,
  );

  protected toggle(): void {
    if (this.disabled() || this.busy() || this.unavailable() !== '') return;
    this.checkedChange.emit(!this.checked());
  }
}
