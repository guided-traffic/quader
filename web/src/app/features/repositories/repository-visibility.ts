import { Component, computed, inject, input, linkedSignal, output, resource } from '@angular/core';
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
import { fieldErrorAt } from '../../shared/field-errors';
import { Disclosure } from '../../shared/ui/disclosure';
import { Busy } from '../../shared/ui/busy';

/**
 * The section Visibility of a repository's settings page (R-UI-055, Q-UI-770 to Q-UI-773): one radio group, **Visibility level**,
 * which is the level of the artifacts, and its own Save changes. For a format with wiki content the wiki's own level is the fold
 * **Wiki visibility** under it, closed while the wiki has the repository's level and opened by the block while it differs or the
 * server refused it, so a differing level is never hidden.
 *
 * While the wiki has no level of its own (it is stored at the artifacts' level and the person chose none in this edit) it follows
 * the visibility level, up and down. A wiki with its own level keeps it and is only lowered where the visibility level falls
 * below it; raising the level again gives it back. An option above the project's level is disabled with the reason, and a wiki
 * option above the visibility level too. The block sends only the parts that changed; the screen applies the answer.
 *
 * The installation's reasons follow the project's (R-AUTHZ-038, R-UI-141): Public while it is unavailable, and a restricted level
 * for a caller who is not an administrator, except the level the part has now, which it keeps; an administrator sees a restricted
 * level enabled with a note. The choices are read from `GET /api/v1/visibility-choices`.
 *
 * What the server answers for one level (`/visibility/artifacts`, `/visibility/wiki` of a 422 or a 403) stands under that
 * level's group; an answer that names no level (409 `public_not_allowed`) is the block's notice.
 */
@Component({
  selector: 'quader-repository-visibility',
  imports: [Busy, Disclosure, ErrorNotice, VisibilityLevelControl],
  template: `
    <section class="visibility" aria-labelledby="repo-visibility">
      <h2 id="repo-visibility" i18n="@@repo.visibility.heading">Visibility</h2>
      <form (submit)="submit($event)" novalidate>
        <quader-visibility-level
          i18n-label="@@repo.visibility.level"
          label="Visibility level"
          [subject]="managed() ? 'upstream' : 'artifacts'"
          [value]="artifacts()"
          (valueChange)="artifacts.set($event)"
          [reasons]="artifactReasons()"
          [notes]="artifactNotes()"
          [error]="artifactsError()"
          [disabled]="!canWrite()"
        />
        <!-- A well-known repository has one level for its packages and its documentation, set in the admin area: no fold. -->
        @if (hasWiki() && !managed()) {
          <quader-disclosure class="wiki" [inline]="true" [heading]="wikiHeading" [note]="wikiNote()" [(expanded)]="wikiOpen">
            <p class="muted wiki-text" i18n="@@repo.visibility.wiki-text">
              The wiki is the README and the documentation of the packages. It has the repository's visibility level unless a lower
              one is chosen here.
            </p>
            <quader-visibility-level
              [label]="wikiHeading"
              [labelHidden]="true"
              subject="wiki"
              [value]="wiki()"
              (valueChange)="wikiChoice.set($event)"
              [reasons]="wikiReasons()"
              [notes]="wikiNotes()"
              [error]="wikiError()"
              [disabled]="!canWrite()"
            />
          </quader-disclosure>
        }
        @for (w of warnings(); track w) {
          <p class="banner-warning" role="status">
            <span class="material-icons inline warn" aria-hidden="true">warning</span>
            <span>{{ w }}</span>
          </p>
        }
        @if (blockFailure(); as f) {
          <quader-error-notice [error]="f" />
        }
        <div class="foot">
          @if (canWrite()) {
            <button class="btn btn-confirm" type="submit" [disabled]="saving() || !dirty()" [quaderBusy]="saving()">
              <ng-container i18n="@@repo.visibility.save">Save changes</ng-container>
              <span class="visually-hidden" i18n="@@repo.visibility.save-for"> (visibility)</span>
            </button>
          } @else if (!managed()) {
            <p class="muted" i18n="@@repo.visibility.read-only">
              Only the project's Owner and holders of repositories:write-config change the visibility.
            </p>
          }
        </div>
      </form>
    </section>
  `,
  styles: `
    .visibility {
      margin-block-end: var(--quader-space-5);
    }
    .muted {
      margin: 0;
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
    }
    /* The fold stands apart from the options above it, a little more than they stand from each other. */
    .wiki {
      margin-block-start: var(--quader-space-2);
    }
    /* The fold's sentence, then its options: the room a label has above its group. */
    .wiki-text {
      margin-block-end: var(--quader-space-2);
    }
    .banner-warning,
    quader-error-notice {
      display: block;
      margin-block-start: var(--quader-space-3);
    }
    /* The save follows the last thing above it at one distance, whether that is the closed fold, an option or a notice. */
    .foot {
      margin-block-start: var(--quader-space-4);
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
  /**
   * A well-known repository's level is set in the admin area, which the page says under the block (R-UI-190): no note of its own,
   * and no fold Wiki visibility, because its one level covers its packages and its documentation and the repository document
   * refuses a change of either (`422 managed_by_product` at `/visibility`).
   */
  readonly managed = input(false);
  readonly saving = input(false);
  readonly failure = input<ApiError | null>(null);
  /** The sentences of the answer's warnings, shown at the block. */
  readonly warnings = input<readonly string[]>([]);
  /** Only the parts that differ from the stored levels. */
  readonly save = output<Partial<RepositoryVisibility>>();

  /** The visibility level of the repository, the level of its artifacts: the stored one until the person chooses another. */
  protected readonly artifacts = linkedSignal<VisibilityLevel>(() => this.stored().artifacts);
  /** The level the person chose for the wiki in this edit; `null` while they chose none, and again when the stored levels change. */
  protected readonly wikiChoice = linkedSignal<RepositoryVisibility, VisibilityLevel | null>({
    source: this.stored,
    computation: () => null,
  });
  /** The wiki has no level of its own: stored at the artifacts' level, and none chosen here. It goes where the level goes. */
  private readonly wikiFollows = computed(() => this.wikiChoice() === null && this.stored().wiki === this.stored().artifacts);
  /** The wiki's level as the form has it: the visibility level while it follows, else its own, at most the visibility level. */
  protected readonly wiki = computed<VisibilityLevel>(() =>
    this.wikiFollows() ? this.artifacts() : lowerLevel(this.wikiChoice() ?? this.stored().wiki, this.artifacts()),
  );

  /** The fold's heading and the name of the group inside it, which shows no label of its own. */
  protected readonly wikiHeading = $localize`:@@repo.visibility.wiki-fold:Wiki visibility`;

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
        above[level] = $localize`:@@repo.visibility.wiki-above-repository:The wiki cannot be more open than the repository.`;
      }
    }
    return firstReasons(aboveProjectReasons(this.projectLevel()), above, installationReasons(this.known(), this.stored().wiki));
  });
  protected readonly wikiNotes = computed(() => restrictedNotes(this.known(), this.stored().wiki));

  /** What the server answered for one level, under its group; the wiki's only where the format shows the wiki. */
  protected readonly artifactsError = computed(() => fieldErrorAt(this.failure(), '/visibility/artifacts'));
  protected readonly wikiError = computed(() => (this.hasWiki() ? fieldErrorAt(this.failure(), '/visibility/wiki') : null));
  /** A refusal that names no level shown here is the block's. */
  protected readonly blockFailure = computed(() => {
    const failure = this.failure();
    return failure && this.artifactsError() === null && this.wikiError() === null ? failure : null;
  });

  /**
   * The fold is open while the person opened it, and the block opens it when the wiki's level starts to differ from the
   * visibility level or the server refused it: a differing level and its error are never behind a closed fold by themselves.
   * The person may close it again.
   */
  protected readonly wikiOpen = linkedSignal<boolean, boolean>({
    source: () => this.wiki() !== this.artifacts() || this.wikiError() !== null,
    computation: (needed, previous) => needed || (previous?.value ?? false),
  });
  /** "Unsaved changes" after the heading of the fold that holds one, as a closed section says it (R-UI-087). */
  protected readonly wikiNote = computed(() =>
    !this.wikiFollows() && this.wiki() !== this.stored().wiki ? $localize`:@@repo.settings.unsaved:Unsaved changes` : '',
  );

  protected readonly dirty = computed(() => this.artifacts() !== this.stored().artifacts || this.wiki() !== this.stored().wiki);

  protected submit(event: Event): void {
    event.preventDefault();
    if (!this.dirty() || this.saving()) return;
    const change: { artifacts?: VisibilityLevel; wiki?: VisibilityLevel } = {};
    if (this.artifacts() !== this.stored().artifacts) change.artifacts = this.artifacts();
    if (this.wiki() !== this.stored().wiki) change.wiki = this.wiki();
    this.save.emit(change);
  }
}
