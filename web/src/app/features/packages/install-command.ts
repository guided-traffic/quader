import { Component, computed, input, model } from '@angular/core';
import { ClientChoice, clientsOf } from '../setup/clients';
import { ClientCommand } from './client-command';

export { COLLAPSE_AFTER_LINES } from './client-command';

/**
 * The install command of one version in one repository, with its client picker (R-UI-009): the picker offers the clients of the
 * format, and the command is `<quader-client-command>` for the chosen one. The version page's install panel (a version in several
 * repositories, whose repository picker it projects beside the client's) uses it.
 */
@Component({
  selector: 'quader-install-command',
  imports: [ClientCommand],
  templateUrl: './install-command.html',
  styleUrl: './install-panel.scss',
})
export class InstallCommand {
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  /** The version string; null while none is selected. */
  readonly version = input.required<string | null>();
  /** The qualified name of the repository the command installs through; null while none is chosen. */
  readonly repository = input.required<string | null>();
  /** The chosen client, a two-way binding so that the host can follow it. */
  readonly client = model<string | null>(null);

  protected readonly clients = computed<readonly ClientChoice[]>(() => clientsOf(this.format()));
  protected readonly selectedClient = computed(() => this.client() ?? this.clients()[0]?.id ?? null);

  protected pickClient(event: Event): void {
    this.client.set((event.target as HTMLSelectElement).value);
  }
}
