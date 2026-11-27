import { DestroyRef, Directive, ElementRef, computed, effect, inject } from '@angular/core';
import { Tooltip } from './tooltip';

/**
 * A control that cannot act, offered with its reason (R-UI-270, Q-UI-1636): `[quaderUnavailable]="reason"` on a button, a link or a
 * text field. While the reason is not empty the control is `aria-disabled="true"` (never `disabled`, which takes it out of hover, focus
 * and touch) in the disabled look of its kind (_controls.scss), a click, a tap, Enter and Space act on nothing, and a field is
 * read-only; the reason is its tooltip and its accessible description (`quaderTooltip`), shown on hover and focus, and a click or a tap
 * pins it until the next one, a press outside or Escape, as the help sign's (R-UI-267). An empty reason leaves the control as it is,
 * so one binding serves both: `[quaderUnavailable]="declared ? reason : ''"`.
 *
 * The reason of a value or an act the Helm configuration fixes names the key as its path in the chart's values (R-UI-268):
 * "Declared in config.authz.role_bindings of the Helm configuration: remove it there." No paragraph beside the control says it again.
 */
@Directive({
  selector: '[quaderUnavailable]',
  hostDirectives: [{ directive: Tooltip, inputs: ['quaderTooltip: quaderUnavailable'] }],
  host: { '[attr.aria-disabled]': 'unavailable() ? "true" : null' },
})
export class Unavailable {
  private readonly tooltip = inject(Tooltip);
  protected readonly unavailable = computed(() => this.tooltip.quaderTooltip() !== '');

  constructor() {
    const el = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
    // Capturing listeners on the control itself run before the listeners of its template and of its other directives (a router link's),
    // which a stopped event never reaches.
    const click = (e: Event): void => {
      if (!this.unavailable()) return;
      e.preventDefault();
      e.stopImmediatePropagation();
      this.tooltip.togglePin();
    };
    const key = (e: KeyboardEvent): void => {
      if (!this.unavailable() || (e.key !== 'Enter' && e.key !== ' ')) return;
      e.preventDefault();
      e.stopImmediatePropagation();
    };
    el.addEventListener('click', click, true);
    el.addEventListener('keydown', key, true);
    inject(DestroyRef).onDestroy(() => {
      el.removeEventListener('click', click, true);
      el.removeEventListener('keydown', key, true);
    });
    // A field keeps its focus and its value, selectable and copyable, and takes no typing.
    if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
      effect(() => {
        el.readOnly = this.unavailable();
      });
    }
  }
}
