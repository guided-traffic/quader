import { ChangeDetectionStrategy, Component, computed, inject, input, linkedSignal, output, signal } from '@angular/core';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Switch } from '../../shared/ui/switch';
import { TimeAgo } from '../../shared/ui/time-ago';
import { catalogueLine } from './bastion-state';

type SwitchName = 'gui' | 'docs';

/**
 * The section **Web interface** of a registration's card (W-35, bastion.md R-BAS-102, R-UI-240, Q-UI-1616): in the card's plain view,
 * after **Direct reads from the object store** and before **Limits**, since a public web interface is the card's decision about what the
 * internet sees, not a tuning detail. The switches **Web interface** and **Documentation**, each with its sentence; **Documentation**
 * takes effect only while the web interface is on, and says so. Who may change a registration (`endpoints:write`, Q-BAS-325) switches it
 * with a merge patch of `switches` and the `ETag` it read; a refusal stands under the switches and each keeps the state the server has.
 * Who may only read gets each state as a sentence (Q-UI-960). While the web interface is on, a line says what the main installation last
 * built for it (`catalogue` of the registration, R-BAS-104), or in its place why it builds nothing, with what to do (Q-BAS-497); and a
 * warning stands where a replica said its copy of the catalogue does not survive a restart.
 */
@Component({
  selector: 'quader-bastion-switches',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice, Switch, TimeAgo],
  template: `
    <h3 i18n="@@bastion.switches.heading">Web interface</h3>
    @if (canWrite()) {
      <div class="state">
        <quader-switch
          [checked]="gui()"
          [label]="guiLabel()"
          [busy]="busy() === 'gui'"
          [describedBy]="guiNote() + ' ' + guiRemote()"
          (checkedChange)="set('gui', $event)"
        />
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
    <!-- What a public remote shows, which only the manual said (W35-P9f): the dependencies the builds pulled through it. -->
    <p class="hint note" [id]="guiRemote()" i18n="@@bastion.switches.gui-remote">
      A public remote repository shows here every package and version your builds fetched through it.
    </p>
    <!-- What the main installation last built for the web interface (R-BAS-104), or in its place why it builds nothing, with what to do. -->
    @if (catalogue(); as c) {
      @if (c.kind === 'stopped') {
        <p class="hint warn note line" data-testid="bastion-catalogue-stopped">
          <span class="material-icons inline" aria-hidden="true">warning</span>
          @if (c.reason === 'anonymous_requests_refused') {
            <span i18n="@@bastion.switches.catalogue-refused"
              >This Bastion's web interface shows nothing: <code translate="no">endpoints.default.auth.anonymous</code> is false, so the main
              installation admits no request without a credential and nothing is public. Admit such requests there to show your public
              repositories, or switch the web interface off.</span
            >
          } @else {
            <span i18n="@@bastion.switches.catalogue-base-url"
              >The main installation does not build this Bastion's web interface: the Bastion has not connected yet, or the address it reported
              is not in <span class="with-stop"><code translate="no">server.additional_base_urls</code>.</span> Connect it, or add its address there and restart the main
              installation: it is built within seconds of either.</span
            >
          }
        </p>
      } @else {
        <p class="state-text note" data-testid="bastion-catalogue-line">
          @if (c.kind === 'built') {
            <ng-container i18n="@@bastion.switches.catalogue-built">Last built <quader-time-ago [at]="c.at" absolute="title" />.</ng-container>
          } @else {
            <ng-container i18n="@@bastion.switches.catalogue-pending">Not built yet. The first build follows within seconds.</ng-container>
          }
        </p>
      }
    }
    <!-- A web interface whose catalogue a restart loses (W-35 B.2b): the replica's volume does not keep it. -->
    @if (catalogueLost()) {
      <p class="hint warn note line" data-testid="bastion-catalogue-lost">
        <span class="material-icons inline" aria-hidden="true">warning</span
        ><span i18n="@@bastion.switches.catalogue-lost"
          >After a restart while the connection to it is down, this Bastion's web interface shows nothing until the connection returns. A
          bucket or persistent volumes keep it.</span
        >
      </p>
    }
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
    .state {
      display: flex;
      align-items: flex-start;
      margin-block-start: var(--quader-space-2);
    }
    /* Under the heading the switch stands as the direct reads' switch stands under theirs. */
    h3 + .state {
      margin-block-start: 0;
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
    .line {
      margin-block: var(--quader-space-2) 0;
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
  /** The builder's last run while the web interface is on, as the registration read last says it. */
  protected readonly catalogue = computed(() => catalogueLine({ switches: { ...this.bastion().switches, gui: this.gui() }, catalogue: this.bastion().catalogue }));
  /** The web interface is on and a replica said its copy of the catalogue does not survive a restart (W-35 B.2b). */
  protected readonly catalogueLost = computed(() => this.gui() && this.bastion().contacts.some((c) => c.catalogueKept === false));
  protected readonly guiNote = computed(() => `bastion-gui-note-${this.bastion().name}`);
  protected readonly guiRemote = computed(() => `bastion-gui-remote-${this.bastion().name}`);
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
