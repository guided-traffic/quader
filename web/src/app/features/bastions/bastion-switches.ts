import { ChangeDetectionStrategy, Component, computed, inject, input, linkedSignal, output, signal } from '@angular/core';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Switch } from '../../shared/ui/switch';

type SwitchName = 'gui' | 'docs';

/**
 * The switches **Web interface** and **Documentation** of a registration (W-35, bastion.md R-BAS-102, R-UI-240's **Details**): each with
 * its sentence; **Documentation** takes effect only while the web interface is on, and says so. Who may change a registration
 * (`endpoints:write`, Q-BAS-325) switches it with a merge patch of `switches` and the `ETag` it read; a refusal stands under the switches
 * and each keeps the state the server has. Who may only read gets each state as a sentence (Q-UI-960).
 */
@Component({
  selector: 'quader-bastion-switches',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice, Switch],
  template: `
    <h4 i18n="@@bastion.switches.heading">Web interface</h4>
    @if (canWrite()) {
      <div class="state">
        <quader-switch [checked]="gui()" [label]="guiLabel()" [busy]="busy() === 'gui'" [describedBy]="guiNote()" (checkedChange)="set('gui', $event)" />
      </div>
    } @else {
      <p class="state-text" data-testid="bastion-gui-state">
        @if (gui()) {
          <ng-container i18n="@@bastion.switches.gui-on">The <strong>web interface</strong> is on.</ng-container>
        } @else {
          <ng-container i18n="@@bastion.switches.gui-off">The <strong>web interface</strong> is off.</ng-container>
        }
      </p>
    }
    <p class="hint note" [id]="guiNote()" i18n="@@bastion.switches.gui-note">
      A read-only web interface on this Bastion shows the packages of public repositories to anyone, without signing in. Off, its pages
      answer "not found" within seconds and what it showed is deleted.
    </p>
    @if (canWrite()) {
      <!-- The second switch's name stands over it; the first one's is the heading. -->
      <p class="switch-name" i18n="@@bastion.switches.docs-name">Documentation</p>
      <div class="state">
        <quader-switch [checked]="docs()" [label]="docsLabel()" [busy]="busy() === 'docs'" [describedBy]="docsNote()" (checkedChange)="set('docs', $event)" />
      </div>
    } @else {
      <p class="state-text" data-testid="bastion-docs-state">
        @if (docs()) {
          <ng-container i18n="@@bastion.switches.docs-on"><strong>Documentation</strong> is on.</ng-container>
        } @else {
          <ng-container i18n="@@bastion.switches.docs-off"><strong>Documentation</strong> is off.</ng-container>
        }
      </p>
    }
    <p class="hint note" [id]="docsNote()" i18n="@@bastion.switches.docs-note">
      The web interface also shows each package's README and documentation where its repository's level allows it. Takes effect only
      while the web interface is on.
    </p>
    @if (docs() && !gui()) {
      <p class="hint warn note" data-testid="bastion-docs-idle">
        <span class="material-icons inline" aria-hidden="true">info</span
        ><span i18n="@@bastion.switches.docs-idle">Documentation is on, but the web interface is off: nothing is shown.</span>
      </p>
    }
    @if (error(); as e) {
      <quader-error-notice [error]="e" />
    }
  `,
  styles: `
    :host {
      display: block;
    }
    h4 {
      margin: var(--quader-space-4) 0 var(--quader-space-2);
      font: var(--quader-type-title-small);
    }
    .state {
      display: flex;
      align-items: flex-start;
      margin-block-start: var(--quader-space-2);
    }
    .state-text {
      margin-block: var(--quader-space-2) 0;
    }
    .switch-name {
      margin: var(--quader-space-3) 0 0;
      font-weight: 600;
    }
    .note {
      max-inline-size: var(--quader-settings-measure);
    }
    quader-error-notice {
      display: block;
      margin-block-start: var(--quader-space-2);
    }
  `,
})
export class BastionSwitches {
  readonly bastion = input.required<Bastion>();
  /** The registration changed: the page reads the list again. */
  readonly changed = output<void>();

  private readonly api = inject(BastionsApi);
  private readonly notifier = inject(Notifier);
  private readonly session = inject(SessionStore);

  protected readonly canWrite = computed(() => this.session.has('endpoints:write'));
  protected readonly busy = signal<SwitchName | null>(null);
  protected readonly error = signal<ApiError | null>(null);
  /** The states the server answered last; they follow the registration whenever the list is read again. */
  protected readonly gui = linkedSignal(() => this.bastion().switches.gui);
  protected readonly docs = linkedSignal(() => this.bastion().switches.docs);
  protected readonly guiNote = computed(() => `bastion-gui-note-${this.bastion().name}`);
  protected readonly docsNote = computed(() => `bastion-docs-note-${this.bastion().name}`);
  protected readonly guiLabel = computed(() => $localize`:@@bastion.switches.gui-label:Web interface of ${this.bastion().name}:name:`);
  protected readonly docsLabel = computed(() => $localize`:@@bastion.switches.docs-label:Documentation in the web interface of ${this.bastion().name}:name:`);

  protected async set(which: SwitchName, on: boolean): Promise<void> {
    if (this.busy()) return;
    this.busy.set(which);
    this.error.set(null);
    try {
      const name = this.bastion().name;
      const { etag } = await this.api.get(name);
      const { value } = await this.api.configure(name, { switches: { [which]: on } }, etag);
      this.gui.set(value.switches.gui);
      this.docs.set(value.switches.docs);
      this.notifier.status(this.doneText(which, on));
      this.changed.emit();
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(null);
    }
  }

  private doneText(which: SwitchName, on: boolean): string {
    if (which === 'gui') {
      return on
        ? $localize`:@@bastion.switches.gui-on-done:Web interface switched on.`
        : $localize`:@@bastion.switches.gui-off-done:Web interface switched off.`;
    }
    return on
      ? $localize`:@@bastion.switches.docs-on-done:Documentation switched on.`
      : $localize`:@@bastion.switches.docs-off-done:Documentation switched off.`;
  }
}
