import { Component, computed, inject, input, linkedSignal, output, signal } from '@angular/core';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { fieldErrorAt } from '../../shared/field-errors';
import { asApiError } from '../../shared/paged-resource';
import { Busy } from '../../shared/ui/busy';
import { Help } from '../../shared/ui/help';
import { unlistedContacts } from './egress';

/** One row of the list being edited: a key that survives removing a row above it, and the text. */
interface Row {
  readonly id: number;
  readonly text: string;
}

/**
 * **Addresses this Bastion connects from** on a registration's card (R-UI-243, bastion.md R-BAS-017): the main installation believes
 * the caller's address a Bastion forwards, and the address its check names, only from these networks (Q-BAS-334, Q-BAS-430).
 *
 * The plain path is the warning (Q-UI-1483, part `warnings`), one line each (Q-UI-1624): for every address a replica's last contact came
 * from that the list does not hold, each with **Add <address>**, one `PATCH` of the list with that address (Q-UI-1482), so an
 * administrator who registered a Bastion enters its address in one click after its first handshake; and while the list is empty, after a
 * contact that no such offer covers. The list itself (part `list`) stands in the card's **Details** (R-UI-240), its help behind the info
 * sign beside its heading (R-UI-267), and is a field per
 * network, each as wide as the longest IPv6 network (the width of a host), with **Add address**, **Remove** and **Save changes**; one
 * empty field stands for the empty list, the default of a new registration. Who may change a registration (`endpoints:write`) edits it;
 * who may only read gets the networks as text (Q-UI-960). A refusal stands under the field it names and as a whole under the list
 * (R-UI-216); the entries typed stay, so a mistyped network can be corrected (Q-UI-1484).
 */
@Component({
  selector: 'quader-bastion-egress',
  imports: [Busy, ErrorNotice, Help],
  template: `
    @if (part() === 'warnings') {
      <!-- The empty list warns only where it matters and no offer says it already (Q-UI-1624): after a contact. -->
      @if (emptyWarning()) {
        <p class="hint warn line" data-testid="bastion-egress-empty">
          <span class="material-icons inline" aria-hidden="true">warning</span
          ><span i18n="@@bastion.egress.empty">No address this Bastion connects from is listed: all its callers share one budget and one block.</span>
        </p>
      }
      @for (a of unlisted(); track a) {
        <div class="line offer" data-testid="bastion-egress-unlisted">
          <p class="hint warn">
            <span class="material-icons inline" aria-hidden="true">warning</span
            ><span i18n="@@bastion.egress.unlisted"
              >Its last contact came from <span translate="no">{{ a }}</span>, which is not listed: callers through it share one budget and one
              block.</span
            >
          </p>
          @if (canWrite()) {
            <button class="btn btn-default btn-sm" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="add(a)">
              <ng-container i18n="@@bastion.egress.add-contact">Add <span translate="no">{{ a }}</span></ng-container>
            </button>
          }
        </div>
      }
    } @else {
      <h4><span class="help-anchor" [id]="headingId()">{{ heading }}</span><quader-help [text]="help" [about]="heading" /></h4>
      @if (canWrite()) {
        <div class="rows" role="group" [attr.aria-labelledby]="headingId()">
          @for (row of rows(); track row.id; let i = $index) {
            <div class="row">
              <input
                class="text-input field-w-name"
                type="text"
                [id]="fieldId(i)"
                [value]="row.text"
                spellcheck="false"
                autocomplete="off"
                translate="no"
                [attr.aria-label]="fieldLabel(i)"
                [attr.aria-invalid]="errorAt(i) ? true : null"
                [attr.aria-describedby]="errorAt(i) ? fieldId(i) + '-error' : null"
                (input)="setRow(row.id, $any($event.target).value)"
              />
              @if (rows().length > 1 || row.text !== '') {
                <button class="btn btn-tertiary" type="button" (click)="removeRow(row.id)">
                  <ng-container i18n="@@bastion.egress.remove">Remove</ng-container>
                  <span class="visually-hidden"> {{ row.text }}</span>
                </button>
              }
              @if (errorAt(i); as message) {
                <p class="field-error" [id]="fieldId(i) + '-error'">{{ message }}</p>
              }
            </div>
          }
        </div>
        <div class="row-actions">
          <button class="btn btn-default" type="button" [disabled]="rows().length >= max" (click)="addRow()" i18n="@@bastion.egress.add-row">Add address</button>
          <button class="btn btn-confirm" type="button" [disabled]="busy() || !dirty()" [quaderBusy]="busy()" (click)="save()" i18n="@@bastion.egress.save">
            Save changes
          </button>
        </div>
      } @else {
        <p class="state-text" data-testid="bastion-egress-list">
          <span translate="no">{{ empty() ? noneText : listed().join(', ') }}</span>
          @if (bastion().declared) {
            <span class="muted"> · <ng-container i18n="@@bastion.egress.from-helm">from the Helm configuration</ng-container></span>
          }
        </p>
      }
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
    .note,
    .line {
      max-inline-size: var(--quader-settings-measure);
    }
    .line {
      margin-block: var(--quader-space-2) 0;
    }
    /* The offer's button stands under its sentence, so the warning sign keeps to the sentence's first line as on every warning line. */
    .offer p {
      margin: 0;
    }
    .offer button {
      margin-block-start: var(--quader-space-1);
    }
    /* One column that never grows past the card: a field as wide as a host shrinks to the room a narrow card has. */
    .rows {
      display: grid;
      grid-template-columns: minmax(0, 1fr);
      gap: var(--quader-space-2);
      margin-block-start: var(--quader-space-3);
    }
    .row {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-1) var(--quader-space-2);
    }
    .row input {
      min-inline-size: 0;
    }
    .row .field-error {
      flex-basis: 100%;
      margin: 0;
    }
    .state-text {
      margin-block: var(--quader-space-2) 0;
      overflow-wrap: anywhere;
    }
    quader-error-notice {
      display: block;
      margin-block-start: var(--quader-space-2);
    }
  `,
})
export class BastionEgress {
  readonly bastion = input.required<Bastion>();
  /**
   * `warnings`: the empty list's warning and the offers of the contacts' addresses, which stand on the card in plain view; `list`: the
   * heading, the help and the fields, in the card's **Details** (Q-UI-1483, R-UI-240).
   */
  readonly part = input.required<'warnings' | 'list'>();
  /** The registration changed: the page reads the list again. */
  readonly changed = output<void>();

  private readonly api = inject(BastionsApi);
  private readonly notifier = inject(Notifier);
  private readonly session = inject(SessionStore);

  /** The most networks a registration lists (Q-BAS-431). */
  protected readonly max = 32;
  protected readonly noneText = $localize`:@@bastion.egress.none:No address is listed.`;
  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  /** A registration the configuration declares takes its networks from there (R-BAS-127): the list is text then. */
  protected readonly canWrite = computed(() => this.session.has('endpoints:write') && !this.bastion().declared);
  protected readonly headingId = computed(() => `bastion-egress-${this.bastion().name}`);
  protected readonly heading = $localize`:@@bastion.egress.heading:Addresses this Bastion connects from`;
  protected readonly help = $localize`:@@bastion.egress.note:The main installation believes the caller's address a Bastion forwards only from these addresses. While none is listed, all callers count as the Bastion's own address.`;

  /** The list the server answered last; it follows the registration whenever that is read again. */
  protected readonly listed = computed(() => this.bastion().egress);
  protected readonly empty = computed(() => this.listed().length === 0);
  protected readonly unlisted = computed(() => unlistedContacts(this.bastion()));
  /**
   * The empty list's warning: only after a replica made contact, and only where no offer of a contact's address stands, which says
   * the same with the address to add (Q-UI-1624). Before a contact there is nothing to act on.
   */
  protected readonly emptyWarning = computed(() => this.empty() && this.bastion().contacts.length > 0 && this.unlisted().length === 0);

  private nextId = 0;
  /**
   * The fields being edited: the server's list, one empty field for an empty one. The list page reads the registrations again every
   * minute (R-UI-213); the fields are set again only when the server's list itself changed, so what is being typed stays.
   */
  protected readonly rows = linkedSignal<string, Row[]>({
    source: () => this.listed().join('\n'),
    computation: () => this.rowsOf(this.listed()),
  });
  protected readonly dirty = computed(() => {
    const typed = this.entries();
    const server = this.listed();
    return typed.length !== server.length || typed.some((t, i) => t !== server[i]);
  });

  private rowsOf(list: readonly string[]): Row[] {
    return (list.length > 0 ? list : ['']).map((text) => ({ id: this.nextId++, text }));
  }

  /** The entries to send: the fields' texts, trimmed, without the empty ones. */
  private entries(): string[] {
    return this.rows()
      .map((r) => r.text.trim())
      .filter((t) => t !== '');
  }

  protected fieldId(i: number): string {
    return `${this.headingId()}-${i}`;
  }

  protected fieldLabel(i: number): string {
    return $localize`:@@bastion.egress.field-label:Address or network ${i + 1}:position: of ${this.bastion().name}:name:`;
  }

  /** The refusal of the entry the field holds: the server's pointer counts the entries sent, which leave out the empty fields. */
  protected errorAt(i: number): string | null {
    const rows = this.rows();
    if (rows[i]?.text.trim() === '') return null;
    const sent = rows.slice(0, i).filter((r) => r.text.trim() !== '').length;
    return fieldErrorAt(this.error(), `/egress/${sent}`);
  }

  protected setRow(id: number, text: string): void {
    this.rows.update((rows) => rows.map((r) => (r.id === id ? { ...r, text } : r)));
  }

  protected addRow(): void {
    this.rows.update((rows) => [...rows, { id: this.nextId++, text: '' }]);
  }

  protected removeRow(id: number): void {
    this.rows.update((rows) => {
      const rest = rows.filter((r) => r.id !== id);
      return rest.length > 0 ? rest : [{ id: this.nextId++, text: '' }];
    });
  }

  /** Sends the fields' list. */
  protected async save(): Promise<void> {
    await this.send(this.entries(), $localize`:@@bastion.egress.saved:Addresses saved.`);
  }

  /** Adds the address of a contact to the server's list, in one PATCH (Q-UI-1482). */
  protected async add(address: string): Promise<void> {
    await this.send([...this.listed(), address], $localize`:@@bastion.egress.added:${address}:address: added.`);
  }

  private async send(egress: readonly string[], done: string): Promise<void> {
    if (this.busy()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      const name = this.bastion().name;
      const { etag } = await this.api.get(name);
      const { value } = await this.api.setEgress(name, egress, etag);
      this.rows.set(this.rowsOf(value.egress));
      this.notifier.status(done);
      this.changed.emit();
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
