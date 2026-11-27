import { NgTemplateOutlet } from '@angular/common';
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
import { fieldErrorAt, fieldMessage, problemsAt } from '../../shared/field-errors';
import { Disclosure } from '../../shared/ui/disclosure';
import { Busy } from '../../shared/ui/busy';
import { ProjectBastion } from '../../api/projects-api';
import { DistributingBastion } from '../../shared/ui/distribution-mark';
import { RepositoryBastions } from './repository-bastions';

/** What the section's Save changes stores: the levels that changed, and the Bastions while their choice changed (R-UI-271). */
export interface VisibilityChange {
  readonly visibility?: Partial<RepositoryVisibility>;
  readonly bastions?: readonly string[];
}

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
 *
 * Under the level and the wiki's fold stands the group **Bastions** (R-UI-271, Q-REPO-491): the Bastions that distribute the repository
 * beyond the network, chosen among those an administrator allowed for the project, saved with the same Save changes; absent while no
 * Bastion allows the project. On a repository of `upstream` the level is set in the admin area, the group stays the administrator's.
 */
@Component({
  selector: 'quader-repository-visibility',
  imports: [Busy, Disclosure, ErrorNotice, NgTemplateOutlet, RepositoryBastions, VisibilityLevelControl],
  template: `
    @if (levelsShown()) {
      <section class="visibility" aria-labelledby="repo-visibility">
        <h2 id="repo-visibility" i18n="@@repo.visibility.heading">Visibility</h2>
        <ng-container [ngTemplateOutlet]="body" />
      </section>
    } @else if (bastionsChoosable() || storedBastions().length > 0) {
      <ng-container [ngTemplateOutlet]="body" />
    }
    <ng-template #body>
      <form (submit)="submit($event)" novalidate>
        @if (levelsShown()) {
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
        }
        <quader-repository-bastions
          [offered]="offered()"
          [stored]="storedBastions()"
          [(chosen)]="chosen"
          [canChoose]="canChooseBastions()"
          [disabled]="saving()"
          [repositoryClass]="repositoryClass()"
          [level]="artifacts()"
          [missingMembers]="missingMembers()"
          [error]="bastionsError()"
        />
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
          @if (canWrite() || bastionsChoosable()) {
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
    </ng-template>
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
  /** The Bastions that distribute the repository now (its member `bastions`). */
  readonly storedBastions = input<readonly DistributingBastion[]>([]);
  /** The Bastions that allow the project; `null` while unknown or not readable by the caller. */
  readonly offered = input<readonly ProjectBastion[] | null>(null);
  /** The caller chooses the Bastions: the project's Owner, `repositories:write-config`, an administrator on `upstream`. */
  readonly canChooseBastions = input(false);
  readonly repositoryClass = input<'local' | 'remote' | 'virtual'>('local');
  /** A virtual's members each offered Bastion does not distribute, by the Bastion's name. */
  readonly missingMembers = input<ReadonlyMap<string, readonly string[]>>(new Map());
  /**
   * The level's group and the wiki's fold, under the section's own heading. Off for a system repository, whose level is a statement of
   * its page (R-UI-190): the block is then the group Bastions and its Save changes alone, shown while there is something to choose or
   * to say.
   */
  readonly levelsShown = input(true);
  /** Only the parts that differ from the stored levels and Bastions. */
  readonly save = output<VisibilityChange>();

  /** The group has boxes: the caller chooses and a Bastion allows the project. */
  protected readonly bastionsChoosable = computed(() => this.canChooseBastions() && (this.offered()?.length ?? 0) > 0);
  /** The Bastions chosen in the form: the stored ones until the person changes them. */
  protected readonly chosen = linkedSignal<readonly string[]>(() => this.storedBastions().map((b) => b.name).sort());
  private readonly bastionsDirty = computed(
    () => this.chosen().join('\n') !== this.storedBastions().map((b) => b.name).sort().join('\n'),
  );

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
  /** The server's refusal of a chosen Bastion (`bastion_not_allowed` at `/bastions/<i>`), under the group. */
  protected readonly bastionsError = computed(() => {
    const failure = this.failure();
    if (!failure) return null;
    const messages = problemsAt(failure.fieldErrors, '/bastions').map((p) => fieldMessage(p));
    return messages.length > 0 ? [...new Set(messages)].join(' ') : null;
  });
  /** A refusal that names no level and no Bastion shown here is the block's. */
  protected readonly blockFailure = computed(() => {
    const failure = this.failure();
    return failure && this.artifactsError() === null && this.wikiError() === null && this.bastionsError() === null ? failure : null;
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

  private readonly levelsDirty = computed(() => this.artifacts() !== this.stored().artifacts || this.wiki() !== this.stored().wiki);
  protected readonly dirty = computed(() => this.levelsDirty() || this.bastionsDirty());

  protected submit(event: Event): void {
    event.preventDefault();
    if (!this.dirty() || this.saving()) return;
    const levels: { artifacts?: VisibilityLevel; wiki?: VisibilityLevel } = {};
    if (this.artifacts() !== this.stored().artifacts) levels.artifacts = this.artifacts();
    if (this.wiki() !== this.stored().wiki) levels.wiki = this.wiki();
    this.save.emit({
      ...(this.levelsDirty() ? { visibility: levels } : {}),
      ...(this.bastionsDirty() ? { bastions: this.chosen() } : {}),
    });
  }
}
