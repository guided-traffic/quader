import { Component, computed, effect, inject, input, output, resource, signal, untracked } from '@angular/core';
import { RepositoryVisibility } from '../../api/repositories-api';
import { VisibilityApi, VisibilityChoices } from '../../api/visibility-api';
import { valueOf } from '../../shared/paged-resource';
import {
  VisibilityLevel,
  VisibilityLevelControl,
  aboveProjectReasons,
  firstReasons,
  installationReasons,
  lowerLevel,
  isMoreOpen,
  restrictedNotes,
} from '../../shared/ui/visibility-level';
import { ApiError } from '../../api/problem';
import { ErrorNotice } from '../../shared/error-notice';

/**
 * The block Visibility of a repository's Access tab (R-UI-055, Q-UI-231): the radio group for the artifacts and, for a format with
 * wiki content, one for the wiki, with its own Save changes. An option above the project's level is disabled with the reason; a
 * wiki option above the artifacts' level is disabled with the reason, and choosing an artifacts level below the wiki's lowers the
 * wiki's choice with it. The block sends only the parts that changed; the screen applies the answer.
 *
 * The installation's reasons follow the project's (R-AUTHZ-038, R-UI-141): Public while it is unavailable, and a restricted level
 * for a caller who is not an administrator, except the level the part has now, which it keeps; an administrator sees a restricted
 * level enabled with a note. The choices are read from `GET /api/v1/visibility-choices`.
 */
@Component({
  selector: 'quader-repository-visibility',
  imports: [ErrorNotice, VisibilityLevelControl],
  template: `
    <section class="visibility" aria-labelledby="repo-visibility">
      <h2 id="repo-visibility" i18n="@@repo.visibility.heading">Visibility</h2>
      <form (submit)="submit($event)" novalidate>
        <div class="groups">
          <quader-visibility-level
            [label]="artifactsLabel()"
            subject="artifacts"
            [value]="artifacts()"
            (valueChange)="setArtifacts($event)"
            [reasons]="artifactReasons()"
            [notes]="artifactNotes()"
            [disabled]="!canWrite()"
          />
          @if (hasWiki()) {
            <quader-visibility-level
              i18n-label="@@repo.visibility.wiki"
              label="Wiki"
              subject="wiki"
              [value]="wiki()"
              (valueChange)="wiki.set($event)"
              [reasons]="wikiReasons()"
              [notes]="wikiNotes()"
              [disabled]="!canWrite()"
            />
          }
        </div>
        @if (hasWiki()) {
          <p class="muted" i18n="@@repo.visibility.wiki-note">
            Artifacts: who can install, download and list the packages. Wiki: who can read the README and the documentation.
          </p>
        }
        @for (w of warnings(); track w) {
          <p class="banner-warning" role="status">
            <span class="material-icons inline warn" aria-hidden="true">warning</span>
            <span>{{ w }}</span>
          </p>
        }
        @if (failure(); as f) {
          <quader-error-notice [error]="f" />
        }
        @if (canWrite()) {
          <button class="btn btn-confirm" type="submit" [disabled]="saving() || !dirty()">
            <ng-container i18n="@@repo.visibility.save">Save changes</ng-container>
            <span class="visually-hidden" i18n="@@repo.visibility.save-for"> (visibility)</span>
          </button>
        } @else {
          <p class="muted" i18n="@@repo.visibility.read-only">
            Only the project's Owner and holders of repositories:write-config change the visibility.
          </p>
        }
      </form>
    </section>
  `,
  styles: `
    .visibility {
      margin-block-end: var(--quader-space-5);
    }
    .groups {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-4);
    }
    .muted {
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
    }
  `,
})
export class RepositoryVisibilityBlock {
  /** The stored levels. */
  readonly stored = input.required<RepositoryVisibility>();
  /** The project's level; `null` while it is unknown, which restricts nothing (the server decides). */
  readonly projectLevel = input<VisibilityLevel | null>(null);
  /** The format has wiki content (`wiki` of `GET /api/v1/formats`). */
  readonly hasWiki = input(false);
  readonly canWrite = input(false);
  readonly saving = input(false);
  readonly failure = input<ApiError | null>(null);
  /** The sentences of the answer's warnings, shown at the block. */
  readonly warnings = input<readonly string[]>([]);
  /** Only the parts that differ from the stored levels. */
  readonly save = output<Partial<RepositoryVisibility>>();

  protected readonly artifacts = signal<VisibilityLevel>('private');
  protected readonly wiki = signal<VisibilityLevel>('private');

  protected readonly artifactsLabel = computed(() =>
    this.hasWiki() ? $localize`:@@repo.visibility.artifacts:Artifacts` : $localize`:@@repo.visibility.single:Visibility`,
  );

  private readonly visibilityApi = inject(VisibilityApi);
  /** The caller's choices; while unknown, or for a caller who cannot read them, the server decides. */
  private readonly choices = resource({
    loader: async (): Promise<VisibilityChoices | null> => {
      try {
        return await this.visibilityApi.choices();
      } catch {
        return null;
      }
    },
  });
  private readonly known = computed(() => (this.canWrite() ? (valueOf(this.choices) ?? null) : null));

  protected readonly artifactReasons = computed(() =>
    firstReasons(aboveProjectReasons(this.projectLevel()), installationReasons(this.known(), this.stored().artifacts)),
  );
  protected readonly artifactNotes = computed(() => restrictedNotes(this.known(), this.stored().artifacts));

  protected readonly wikiReasons = computed(() => {
    const above: Partial<Record<VisibilityLevel, string>> = {};
    for (const level of ['internal', 'public'] as const) {
      if (isMoreOpen(level, this.artifacts())) {
        above[level] = $localize`:@@repo.visibility.wiki-above-artifacts:The wiki cannot be more open than the artifacts.`;
      }
    }
    return firstReasons(aboveProjectReasons(this.projectLevel()), above, installationReasons(this.known(), this.stored().wiki));
  });
  protected readonly wikiNotes = computed(() => restrictedNotes(this.known(), this.stored().wiki));

  protected readonly dirty = computed(
    () => this.artifacts() !== this.stored().artifacts || this.wikiValue() !== this.stored().wiki,
  );

  constructor() {
    effect(() => {
      const s = this.stored();
      untracked(() => {
        this.artifacts.set(s.artifacts);
        this.wiki.set(s.wiki);
      });
    });
  }

  /** A format without wiki content keeps the wiki at the artifacts' level, so the one group says everything. */
  private wikiValue(): VisibilityLevel {
    return this.hasWiki() ? this.wiki() : lowerLevel(this.stored().wiki, this.artifacts());
  }

  protected setArtifacts(level: VisibilityLevel): void {
    this.artifacts.set(level);
    this.wiki.update((w) => lowerLevel(w, level));
  }

  protected submit(event: Event): void {
    event.preventDefault();
    if (!this.dirty() || this.saving()) return;
    const change: { artifacts?: VisibilityLevel; wiki?: VisibilityLevel } = {};
    if (this.artifacts() !== this.stored().artifacts) change.artifacts = this.artifacts();
    if (this.wikiValue() !== this.stored().wiki) change.wiki = this.wikiValue();
    this.save.emit(change);
  }
}
