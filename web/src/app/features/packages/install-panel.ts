import { Component, computed, effect, input, signal, untracked } from '@angular/core';
import { setupCommands } from '../../core/project-links';
import { isSystemRepositoryName, systemClientSentence, upstreamKeyOf } from '../../core/upstream-project';
import { RouterLink } from '@angular/router';
import { PackageVersion } from '../../api/packages-api';
import { ClientChoice, clientsOf } from '../setup/clients';
import { InstallCommand } from './install-command';

export { COLLAPSE_AFTER_LINES } from './install-command';

/**
 * The install panel of a package version (R-UI-009): the command (see {@link InstallCommand}) for the chosen client and
 * repository, and the link below it to the client setup that configures the client once. A version in more than one
 * repository offers a repository picker that starts at a virtual repository containing it, else its first local one. A system
 * repository of the project `upstream` is never offered; a version held by system repositories alone gets one sentence instead of the
 * command (R-UI-193, Q-UI-898, Q-UI-899).
 */
@Component({
  selector: 'quader-install-panel',
  imports: [RouterLink, InstallCommand],
  templateUrl: './install-panel.html',
  styleUrl: './install-panel.scss',
})
export class InstallPanel {
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  readonly version = input.required<PackageVersion>();

  protected readonly clients = computed<readonly ClientChoice[]>(() => clientsOf(this.format()));
  protected readonly client = signal<string | null>(null);
  protected readonly chosenRepository = signal<string | null>(null);

  /**
   * The repositories a client can install the version through: every one that holds it but a system repository of the project
   * `upstream`, which answers every client 404 (R-UI-193, Q-UI-898). The picker offers these, and the command uses one of them.
   */
  protected readonly repositories = computed(() => this.version().repositories.filter((r) => !isSystemRepositoryName(r.key)));
  /**
   * A version held by system repositories alone has no command: one sentence says which of Quader's work reads it (Q-UI-899).
   */
  protected readonly systemNote = computed(() => {
    const held = this.version().repositories;
    return held.length > 0 && this.repositories().length === 0 ? systemClientSentence(upstreamKeyOf(held[0]!.key), 'version') : null;
  });
  /** A virtual repository that contains the version, else the first local one, else the first. */
  protected readonly defaultRepository = computed<string | null>(() => {
    const repos = this.repositories();
    return (
      (repos.find((r) => r.class === 'virtual') ?? repos.find((r) => r.class === 'local') ?? repos[0])
        ?.key ?? null
    );
  });
  protected readonly repository = computed(() => this.chosenRepository() ?? this.defaultRepository());
  /** Set up client of the chosen repository (R-UI-126); the client goes with it. None for a system repository (Q-UI-896). */
  protected readonly setupLink = computed(() => {
    const ref = this.repository();
    return ref ? setupCommands(ref) : null;
  });

  constructor() {
    // Another version or another package starts from the defaults again.
    effect(() => {
      this.version();
      untracked(() => this.chosenRepository.set(null));
    });
  }

  protected pickRepository(event: Event): void {
    this.chosenRepository.set((event.target as HTMLSelectElement).value);
  }
}
