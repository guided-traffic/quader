import { Component, computed, effect, input, signal, untracked } from '@angular/core';
import { setupCommands } from '../../core/project-links';
import { CLAMAV_KEY, isSystemRepositoryName, systemClientSentence, upstreamKeyOf, upstreamName } from '../../core/upstream-project';
import { systemClosed } from '../../core/system-clients';
import { CLAMAV_FORMAT } from './clamav-definitions';
import { RouterLink } from '@angular/router';
import { PackageVersion } from '../../api/packages-api';
import { ClientChoice, clientsOf } from '../setup/clients';
import { InstallCommand } from './install-command';

export { COLLAPSE_AFTER_LINES } from './client-command';

/** The qualified name of the repository of ClamAV's definitions (R-REPO-075). */
const CLAMAV_REF = upstreamName(CLAMAV_KEY);

/**
 * The install panel of a package version (R-UI-009): the command (see {@link InstallCommand}) for the chosen client and
 * repository, and the link below it to the client setup that configures the client once. A version in more than one
 * repository offers a repository picker that starts at a virtual repository containing it, else its first local one. A system
 * repository of the project `upstream` is never offered; a version held by system repositories alone gets one sentence instead of the
 * command (R-UI-193, Q-UI-898, Q-UI-899). Nor is a repository the version is not available through, a disabled one or a virtual
 * one that reaches it only through disabled members; a version held only by disabled repositories says so in one sentence (Q-UI-965).
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
   * The repositories a client can install the version through: every one the version is available through (not disabled, nor a
   * virtual one that reaches it only through disabled members, Q-UI-965) but a system repository of the project `upstream`, which
   * answers every client 404 (R-UI-193, Q-UI-898). The picker offers these, and the command uses one of them.
   */
  protected readonly repositories = computed(() =>
    this.version().repositories.filter(
      (r) => r.available && (!isSystemRepositoryName(r.key) || (r.key === CLAMAV_REF && !this.clamavClosed())),
    ),
  );
  /**
   * A version that no repository serves because the repositories that hold it are disabled: one sentence instead of the command,
   * naming them, as the file of a disabled repository says it (Q-UI-965). Null where a repository serves it or only system
   * repositories hold it.
   */
  protected readonly disabledNote = computed<string | null>(() => {
    if (this.repositories().length > 0) return null;
    const keys = this.version()
      .repositories.filter((r) => !r.available && r.class !== 'virtual' && !isSystemRepositoryName(r.key))
      .map((r) => r.key);
    if (keys.length === 0) return null;
    if (keys.length === 1) {
      return $localize`:@@pkg.install.disabled-note:The repository ${keys[0]}:repository: is disabled: nobody can install this version until it is enabled again.`;
    }
    return $localize`:@@pkg.install.disabled-note-many:The repositories ${keys.join(', ')}:repositories: are disabled: nobody can install this version until one of them is enabled again.`;
  });
  /** `upstream/clamav` answers no client now (Q-UI-908): asked only on a version of the ClamAV definitions. */
  private readonly clamavClosed = systemClosed(() => (this.format() === CLAMAV_FORMAT ? CLAMAV_REF : null));
  /**
   * A version of the ClamAV definitions while ClamAV definition updates is on (R-UI-194): no command per version, since a ClamAV
   * installation always takes the version in use; the panel leads to the client setup of R-CLAMAV-006.
   */
  protected readonly clamavServed = computed(() => this.format() === CLAMAV_FORMAT && this.repositories().length > 0);
  /**
   * A version held by system repositories alone has no command: one sentence says which of Quader's work reads it (Q-UI-899).
   */
  protected readonly systemNote = computed(() => {
    const held = this.version().repositories;
    return held.length > 0 && this.repositories().length === 0 && this.disabledNote() === null
      ? systemClientSentence(upstreamKeyOf(held[0]!.key), 'version')
      : null;
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
    return ref ? setupCommands(ref, ref === CLAMAV_REF && !this.clamavClosed()) : null;
  });

  /** Which version is shown, by value: the page reads the version again while it is open (R-UI-213), which is no other version. */
  private readonly shownVersion = computed(() => `${this.version().packageId}@${this.version().version}`);

  constructor() {
    // Another version or another package starts from the defaults again; the same version read again keeps the choice.
    effect(() => {
      this.shownVersion();
      untracked(() => this.chosenRepository.set(null));
    });
  }

  protected pickRepository(event: Event): void {
    this.chosenRepository.set((event.target as HTMLSelectElement).value);
  }
}
