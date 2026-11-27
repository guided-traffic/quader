import { ChangeDetectionStrategy, Component, computed, inject, input, linkedSignal, output, signal } from '@angular/core';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Setting } from '../../shared/ui/setting';
import { Switch } from '../../shared/ui/switch';
import { TimeAgo } from '../../shared/ui/time-ago';
import { catalogueNote } from './bastion-state';

type SwitchName = 'gui' | 'docs';

/**
 * The rows **Web interface** and **Documentation** of a registration's card (W-35, bastion.md R-BAS-102, R-UI-240, Q-UI-1616,
 * Q-UI-1621): the first of the card's main settings, each a setting row whose help stands behind the info sign after its name
 * (R-UI-267). Visible stays what becomes public: under the switch of the web interface, that a public remote repository shows every
 * package the builds fetched through it. While the web interface is on, the time of the last build stands after the switch's word, or
 * "Not built yet" (R-BAS-104); where the builder stopped, a warning of one line under the row says why, the key named by its path in the
 * Helm configuration (Q-BAS-497, Q-UI-1624); and a warning stands where a replica said its copy of the catalogue does not survive a
 * restart. Who may change a registration (`endpoints:write`, Q-BAS-325) switches it with a merge patch of `switches` and the `ETag` it
 * read; a refusal stands under the rows and each keeps the state the server has. Who may only read sees the word **On** or **Off** in the
 * switch's place (Q-UI-960 for a row).
 */
@Component({
  selector: 'quader-bastion-switches',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice, Setting, Switch, TimeAgo],
  template: `
    <quader-setting label="Web interface" i18n-label="@@bastion.switches.heading" [help]="guiHelp" data-testid="bastion-gui-row">
      <span class="control-line">
        @if (canWrite()) {
          <quader-switch
            [checked]="gui()"
            [label]="guiLabel()"
            [busy]="busy() === 'gui'"
            [describedBy]="guiRemote()"
            (checkedChange)="set('gui', $event)"
          />
        } @else {
          <span data-testid="bastion-gui-state">{{ word(gui()) }}</span>
        }
        <!-- What the main installation last built for the web interface (R-BAS-104), after the switch's word. -->
        @switch (note()?.kind) {
          @case ('built') {
            <span class="muted" data-testid="bastion-catalogue-line" i18n="@@bastion.switches.catalogue-built"
              >Last built <quader-time-ago [at]="builtAt()" absolute="title" /></span>
          }
          @case ('pending') {
            <span class="muted" data-testid="bastion-catalogue-line" i18n="@@bastion.switches.catalogue-pending">Not built yet</span>
          }
          @case ('waits') {
            <span class="muted" data-testid="bastion-catalogue-line" i18n="@@bastion.switches.catalogue-waits">Built once the Bastion has made contact</span>
          }
        }
      </span>
      <!-- What becomes public stays visible (R-UI-267): the dependencies the builds pulled through a public remote (W35-P9f). -->
      <p below class="hint below" [id]="guiRemote()" i18n="@@bastion.switches.gui-remote">
        It also shows every package your builds fetched through a public remote repository.
      </p>
    </quader-setting>
    <!-- Why the builder builds nothing (Q-BAS-497): one line under the row, in place of the time of the last build. -->
    @switch (note()?.kind) {
      @case ('refused') {
        <p class="hint warn line" data-testid="bastion-catalogue-stopped">
          <span class="material-icons inline" aria-hidden="true">warning</span
          ><span i18n="@@bastion.switches.catalogue-refused"
            >The web interface shows nothing: the Helm configuration admits no request without a credential
            <span class="with-stop">(<code translate="no">endpoints.default.auth.anonymous</code>).</span></span
          >
        </p>
      }
      @case ('unlisted') {
        <p class="hint warn line" data-testid="bastion-catalogue-stopped">
          <span class="material-icons inline" aria-hidden="true">warning</span
          ><span i18n="@@bastion.switches.catalogue-unlisted"
            >Not built: <span translate="no">{{ reportedUrl() }}</span> is not in
            <code translate="no">config.server.additional_base_urls</code> of the Helm configuration.</span
          >
        </p>
      }
      @case ('no-url') {
        <p class="hint warn line" data-testid="bastion-catalogue-stopped">
          <span class="material-icons inline" aria-hidden="true">warning</span
          ><span i18n="@@bastion.switches.catalogue-no-url">Not built: the Bastion reported no base URL.</span>
        </p>
      }
    }
    <!-- A web interface whose catalogue a restart loses (W-35 B.2b): the replica's volume does not keep it. -->
    @if (catalogueLost()) {
      <p class="hint warn line" data-testid="bastion-catalogue-lost">
        <span class="material-icons inline" aria-hidden="true">warning</span
        ><span i18n="@@bastion.switches.catalogue-lost"
          >A restart during an outage of the main installation empties the web interface; a bucket or persistent volumes keep it.</span
        >
      </p>
    }
    <quader-setting label="Documentation" i18n-label="@@bastion.switches.docs-name" [help]="docsHelp" data-testid="bastion-docs-row">
      @if (canWrite()) {
        <quader-switch [checked]="docs()" [label]="docsLabel()" [busy]="busy() === 'docs'" (checkedChange)="set('docs', $event)" />
      } @else {
        <span data-testid="bastion-docs-state">{{ word(docs()) }}</span>
      }
    </quader-setting>
    @if (error(); as e) {
      <quader-error-notice [error]="e" />
    }
  `,
  styles: `
    :host {
      display: block;
    }
    /* The switch with its word, and after it the time of the last build; under it where a narrow card has no room beside. */
    .control-line {
      display: inline-flex;
      flex-wrap: wrap;
      align-items: flex-start;
      column-gap: var(--quader-space-4);
    }
    .below {
      margin: 0;
    }
    /* A warning of a row spans the measure from the label's column, one line at the wide width where its words allow. */
    .line {
      max-inline-size: var(--quader-settings-measure);
      margin-block: var(--quader-space-1);
      overflow-wrap: anywhere;
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

  protected readonly guiHelp = $localize`:@@bastion.help.gui:A read-only web interface of this Bastion that shows anyone the packages of public repositories, without signing in. Off, its pages answer “not found” within seconds.`;
  protected readonly docsHelp = $localize`:@@bastion.help.docs:The web interface also shows each package's README and documentation, where its repository's level allows. Only while the web interface is on.`;

  protected readonly canWrite = computed(() => this.session.has('endpoints:write'));
  protected readonly busy = signal<SwitchName | null>(null);
  protected readonly error = signal<ApiError | null>(null);
  /** The states the server answered last; they follow the registration whenever the list is read again. */
  protected readonly gui = linkedSignal(() => this.bastion().switches.gui);
  protected readonly docs = linkedSignal(() => this.bastion().switches.docs);
  /** The builder's last run while the web interface is on, as the registration read last says it. */
  protected readonly note = computed(() =>
    catalogueNote({ switches: { ...this.bastion().switches, gui: this.gui() }, catalogue: this.bastion().catalogue, contacts: this.bastion().contacts }),
  );
  protected readonly builtAt = computed(() => {
    const n = this.note();
    return n?.kind === 'built' ? n.at : '';
  });
  protected readonly reportedUrl = computed(() => {
    const n = this.note();
    return n?.kind === 'unlisted' ? n.url : '';
  });
  /** The web interface is on and a replica said its copy of the catalogue does not survive a restart (W-35 B.2b). */
  protected readonly catalogueLost = computed(() => this.gui() && this.bastion().contacts.some((c) => c.catalogueKept === false));
  protected readonly guiRemote = computed(() => `bastion-gui-remote-${this.bastion().name}`);
  protected readonly guiLabel = computed(() => $localize`:@@bastion.switches.gui-label:Web interface of ${this.bastion().name}:name:`);
  protected readonly docsLabel = computed(() => $localize`:@@bastion.switches.docs-label:Documentation in the web interface of ${this.bastion().name}:name:`);

  /** A reader's word for a switch's state (Q-UI-960 in a row). */
  protected word(on: boolean): string {
    return on ? $localize`:@@switch.on:On` : $localize`:@@switch.off:Off`;
  }

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
