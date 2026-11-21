import { Component, computed, effect, input, signal, untracked } from '@angular/core';
import { setupCommands } from '../../core/project-links';
import { RouterLink } from '@angular/router';
import { PackageVersion } from '../../api/packages-api';
import { ClientChoice, clientsOf } from '../setup/clients';
import { InstallCommand } from './install-command';

export { COLLAPSE_AFTER_LINES } from './install-command';

/**
 * The install panel of a package version (R-UI-009): the command (see {@link InstallCommand}) for the chosen client and
 * repository, and the link below it to the client setup that configures the client once. A version in more than one
 * repository offers a repository picker that starts at a virtual repository containing it, else its first local one.
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

  /** A virtual repository that contains the version, else the first local one, else the first. */
  protected readonly defaultRepository = computed<string | null>(() => {
    const repos = this.version().repositories;
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
