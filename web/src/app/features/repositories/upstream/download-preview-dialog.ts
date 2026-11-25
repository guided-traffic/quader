import { Component, computed, inject, resource, signal } from '@angular/core';
import { newIdempotencyKey } from '../../../api/client';
import { ApiError } from '../../../api/problem';
import { RemoteDownloadPreview, RemoteDownloadRequest, RemoteDownloadsApi } from '../../../api/remote-downloads-api';
import { UpstreamCatalogueApi } from '../../../api/upstream-catalogue-api';
import { Checkbox } from '../../../shared/ui/checkbox';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../../shared/ui/dialog';
import { ErrorNotice } from '../../../shared/error-notice';
import { asApiError, failureOf, valueOf } from '../../../shared/paged-resource';
import { formatSize } from '../../packages/format-size';
import { syncIntervalSentence } from '../downloads/download-words';

/** The code of what a preview's counts leave open (R-CUR-010). */
export type PreviewNote = NonNullable<RemoteDownloadPreview['note']>;

/** The sentence of a preview's note, in the screen's language (R-UI-214): the API answers a code, never the sentence. */
export function previewNoteText(code: PreviewNote): string {
  switch (code) {
    case 'section_uncounted':
      return $localize`:@@upstream.preview.note.section:The upstream does not tell how many versions a sub-section holds or how large they are; the bounds stop the download while it runs.`;
    case 'sizes_unannounced':
      return $localize`:@@upstream.preview.note.sizes:The upstream announces no sizes. A size counts once its file is stored, and the bounds stop the download while it runs.`;
    case 'more_versions':
      return $localize`:@@upstream.preview.note.more:The upstream lists more versions than one answer holds; the rest are counted while the download runs.`;
    case 'size_once_stored':
      return $localize`:@@upstream.preview.note.size-once-stored:The size of the version is known once it is stored.`;
    case 'counted_on_expand':
      return $localize`:@@upstream.preview.note.on-expand:The versions are counted when the download starts; the bounds stop it while it runs.`;
  }
}

/** The pointers of a package's range of versions (R-CUR-010, Q-CUR-190). */
export type RangePointer = '/fromVersion' | '/toVersion';

/**
 * The sentence of a refused bound, in the screen's language (R-UI-214, Q-CUR-192): the server names the field and its code, the screen
 * says what to type; the server's own sentence stays in the error notice under the facts.
 */
export function rangeErrorText(error: ApiError | null, pointer: RangePointer): string | null {
  const problem = error?.fieldErrors.get(pointer);
  if (!problem) return null;
  switch (problem.code) {
    case 'invalid_version':
      return $localize`:@@upstream.preview.range.not-a-version:This is no version of the format. Type a version as the format's clients name it; a tag such as latest has no place in the order of versions.`;
    case 'out_of_range':
      return pointer === '/toVersion'
        ? $localize`:@@upstream.preview.range.empty:No version lies between the two versions. Type an upper version above the lower one.`
        : $localize`:@@upstream.preview.range.too-long:A version has at most 256 characters.`;
    default:
      return problem.detail ?? problem.code;
  }
}

/** How a bulk is downloaded: once, or kept in sync (R-CUR-027). */
export type DownloadMode = 'once' | 'sync';

/** What the dialog closes with: the started bulk, and whether it is the first run of a sync. */
export interface DownloadPreviewResult {
  readonly bulk: string;
  readonly sync: boolean;
}

export interface DownloadPreviewData {
  readonly repository: string;
  /** A package's or a sub-section's bulk, without bounds: the preview answers the installation's. */
  readonly request: RemoteDownloadRequest;
  /** What is downloaded: "Every version of lodash", "The sub-section @acme". */
  readonly title: string;
}

const GIB = 1024 ** 3;

/** A count in the viewer's language, with grouping: "3,893". */
function count(n: number): string {
  return new Intl.NumberFormat(($localize as { locale?: string }).locale || 'en-US').format(n);
}

/**
 * The preview of a bulk download (remote-curation.md R-CUR-010, R-CUR-018, R-CUR-027): **Once** or **Keep in sync** first; with a sync
 * chosen the count is what its first run fetches, the sentence under it says the repository's interval and that a sync stores only
 * what the rules deliver, and a subject the rules do not deliver at all, or one kept in sync already, is said instead of being set up.
 * Then what will be fetched (versions, packages, bytes, the
 * versions whose size the upstream does not tell), what is stored already and what is not delivered here (fetched for examination),
 * the bounds as fields with their values, which may only be lowered, and **Start download**. A package's bulk takes a range of its
 * versions (Q-CUR-190 to Q-CUR-194): **From version** and **Up to version** with **included** or **below**, empty for no bound,
 * offered the versions the opened package lists (its first page) and open to any version typed. A changed bound is previewed again
 * before the start; a bound the server refuses says so at its field, with the whole answer in the error notice (R-UI-216). Closes with
 * the started bulk's ID, which expands into rows while the tab Downloads shows it (Q-CUR-168).
 */
@Component({
  selector: 'quader-download-preview-dialog',
  imports: [Checkbox, DialogClose, DialogTitle, ErrorNotice],
  template: `
    <h2 quaderDialogTitle>{{ data.title }}</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        <fieldset class="plain modes" [disabled]="starting()">
          <legend class="field-label" i18n="@@upstream.preview.mode">How often</legend>
          <label class="mode-option">
            <input type="radio" name="download-mode" value="once" [checked]="mode() === 'once'" (change)="mode.set('once')" />
            <span class="mode-title" i18n="@@upstream.preview.mode.once">Once</span>
            <span class="hint" i18n="@@upstream.preview.mode.once-hint">Downloads what the bounds take now.</span>
          </label>
          <label class="mode-option">
            <input type="radio" name="download-mode" value="sync" [checked]="mode() === 'sync'" (change)="mode.set('sync')" />
            <span class="mode-title" i18n="@@upstream.preview.mode.sync">Keep in sync</span>
            <span class="hint" i18n="@@upstream.preview.mode.sync-hint">Downloads it now and again at every interval: what is new and delivered here.</span>
          </label>
        </fieldset>
        @if (syncing() && preview()?.sync; as s) {
          <!-- What a sync does, said where it is chosen (R-CUR-027): or why it is not set up. -->
          @if (s.existing) {
            <p class="banner-warning sync-block" role="status" data-testid="download-sync-existing" i18n="@@upstream.preview.sync-existing">This is kept in sync already. The tab Downloads lists it, where it can be run, paused or removed.</p>
          } @else if (!s.delivered) {
            <p class="banner-warning sync-block" role="status" data-testid="download-sync-undelivered" i18n="@@upstream.preview.sync-undelivered">This repository does not deliver it, so a sync would store nothing. Allow it first, or download it once for examination.</p>
          } @else {
            <p class="hint sync-block" data-testid="download-sync-note">
              {{ intervalSentence(s.intervalSeconds) }}&ngsp;<ng-container i18n="@@upstream.preview.sync-rules">A sync stores only what the rules deliver and removes nothing.</ng-container>
            </p>
            @if (s.paused) {
              <p class="hint sync-block" i18n="@@upstream.preview.sync-paused">The syncs of this repository are paused in its settings: this one runs now and then waits until they are resumed.</p>
            }
          }
        }
        @if (failure(); as f) {
          <quader-error-notice [error]="f" (retry)="failure.retry()" />
        } @else if (preview(); as p) {
          <dl class="facts" data-testid="download-preview">
            @if (syncing() && !syncRefused() && p.sync; as s) {
              <!-- A sync's first run downloads what is new and delivered here (R-CUR-027): the count the person decides on. -->
              <dt i18n="@@upstream.preview.first-run">First run</dt>
              <dd>
                @if (s.versions !== null) {
                  {{ number(s.versions) }}
                  <span class="secondary" i18n="@@upstream.preview.first-run-hint">New versions delivered here; each later run downloads what is new since.</span>
                } @else {
                  <span i18n="@@upstream.preview.first-run-unknown">Counted when it runs</span>
                }
              </dd>
            }
            <dt i18n="@@upstream.preview.versions">Versions</dt>
            <dd>
              @if (p.versions !== null) {
                {{ number(p.versions) }}
              } @else {
                <span i18n="@@upstream.preview.versions-unknown">Not told by the upstream</span>
              }
            </dd>
            @if (data.request.level === 'section') {
              <dt i18n="@@upstream.preview.packages">Packages</dt>
              <dd>
                @if (p.packages !== null) {
                  {{ number(p.packages) }}
                } @else {
                  <span i18n="@@upstream.preview.packages-unknown">Not told by the upstream</span>
                }
              </dd>
            }
            <dt i18n="@@upstream.preview.size">Size</dt>
            <dd>
              @if (p.bytes !== null && p.bytes > 0) {
                {{ size(p.bytes) }}
              } @else if (p.sizeUnknown === 0) {
                <span i18n="@@upstream.preview.size-unknown">Not told by the upstream</span>
              }
              @if (p.sizeUnknown > 0) {
                <span [class.secondary]="p.bytes !== null && p.bytes > 0" i18n="@@upstream.preview.size-unknown-of">Size of {{ p.sizeUnknown }} versions not told by the upstream; the bound of {{ size(p.bounds.maxBytes) }} stops the download when it is reached.</span>
              }
            </dd>
            <dt i18n="@@upstream.preview.stored">Stored already</dt>
            <dd>{{ number(p.stored) }}</dd>
            <dt i18n="@@upstream.preview.not-delivered">Not delivered here</dt>
            <dd>
              {{ number(p.notDelivered) }}
              @if (p.notDelivered > 0 && syncing()) {
                <span class="secondary" i18n="@@upstream.preview.not-delivered-sync-hint">Left out: a sync stores only what the rules deliver.</span>
              } @else if (p.notDelivered > 0) {
                <span class="secondary" i18n="@@upstream.preview.not-delivered-hint">Fetched for examination: stored and scanned, not delivered until a rule allows them.</span>
              }
            </dd>
            @if (p.notVersions > 0) {
              <dt i18n="@@upstream.preview.not-versions">Left out, no version</dt>
              <dd>
                {{ number(p.notVersions) }}
                <span class="secondary" i18n="@@upstream.preview.not-versions-hint">Tags such as latest name no version and lie outside every range of versions.</span>
              </dd>
            }
          </dl>
          @if (p.note; as code) {
            <p class="hint">{{ noteText(code) }}</p>
          }

        } @else {
          <p role="status" i18n="@@upstream.preview.loading">Reading what the download would fetch.</p>
        }
        @if (ceilingBounds(); as top) {
          <fieldset class="plain bounds">
            <legend class="field-label" i18n="@@upstream.preview.bounds">Bounds</legend>
            @if (data.request.level === 'package') {
              <div class="bound-row">
                <label class="field-label" for="preview-from-version" i18n="@@upstream.preview.from-version">From version</label>
                <input
                  id="preview-from-version"
                  class="text-input field-w-bytes"
                  list="preview-versions"
                  autocomplete="off"
                  spellcheck="false"
                  translate="no"
                  placeholder="The oldest"
                  i18n-placeholder="@@upstream.preview.from-version.empty"
                  [value]="fromVersion()"
                  [attr.aria-invalid]="fromError() ? 'true' : null"
                  aria-describedby="preview-from-version-error"
                  (change)="setFrom($any($event.target).value)"
                />
                @if (fromError(); as text) {
                  <p id="preview-from-version-error" class="field-error">{{ text }}</p>
                }
              </div>
              <div class="bound-row">
                <label class="field-label" for="preview-to-version" i18n="@@upstream.preview.to-version">Up to version</label>
                <div class="to-row">
                  <input
                    id="preview-to-version"
                    class="text-input field-w-bytes"
                    list="preview-versions"
                    autocomplete="off"
                    spellcheck="false"
                    translate="no"
                    placeholder="The newest"
                    i18n-placeholder="@@upstream.preview.to-version.empty"
                    [value]="toVersion()"
                    [attr.aria-invalid]="toError() ? 'true' : null"
                    aria-describedby="preview-to-version-error"
                    (change)="setTo($any($event.target).value)"
                  />
                  <select
                    class="select-input to-inclusive"
                    aria-label="Whether the version itself is downloaded"
                    i18n-aria-label="@@upstream.preview.to-inclusive.label"
                    (change)="toInclusive.set($any($event.target).value === 'included')"
                  >
                    <option value="included" [selected]="toInclusive()" i18n="@@upstream.preview.to-inclusive.included">included</option>
                    <option value="below" [selected]="!toInclusive()" i18n="@@upstream.preview.to-inclusive.below">below it</option>
                  </select>
                </div>
                @if (toError(); as text) {
                  <p id="preview-to-version-error" class="field-error">{{ text }}</p>
                }
              </div>
              <datalist id="preview-versions">
                @for (v of suggestions(); track v) {
                  <option [value]="v"></option>
                }
              </datalist>
            }
            <div class="bound-row">
              <label class="field-label" for="preview-max-versions">
                @if (syncing()) {
                  <ng-container i18n="@@upstream.preview.max-versions-sync">Keep this many of the newest versions</ng-container>
                } @else {
                  <ng-container i18n="@@upstream.preview.max-versions">At most this many versions</ng-container>
                }
              </label>
              <input
                id="preview-max-versions"
                class="text-input field-w-number"
                type="number"
                inputmode="numeric"
                min="1"
                [max]="top.maxVersions"
                [value]="maxVersions() ?? top.maxVersions"
                (change)="setVersions($any($event.target).value)"
              />
            </div>
            <div class="bound-row">
              <label class="field-label" for="preview-max-gib" i18n="@@upstream.preview.max-bytes">At most this many GiB</label>
              <input
                id="preview-max-gib"
                class="text-input field-w-number"
                type="number"
                inputmode="decimal"
                min="0.01"
                step="any"
                [value]="gib(maxBytes() ?? top.maxBytes)"
                (change)="setBytes($any($event.target).value)"
              />
            </div>
            <quader-checkbox [checked]="prereleases() ?? top.includePrereleases" (checkedChange)="prereleases.set($event)">
              <ng-container i18n="@@upstream.preview.prereleases">Include pre-releases</ng-container>
            </quader-checkbox>
          </fieldset>
        }
        @if (startFailure(); as f) {
          <quader-error-notice [error]="f" />
        }
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@upstream.dialog.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="!preview() || !!failure() || starting() || syncRefused()">
          @if (syncing()) {
            <ng-container i18n="@@upstream.preview.start-sync">Keep in sync</ng-container>
          } @else {
            <ng-container i18n="@@upstream.preview.start">Start download</ng-container>
          }
        </button>
      </div>
    </form>
  `,
  styles: `
    /* The two ways as the delivery mode's choices (repository-delivery.ts): a choice and its sentence. */
    .modes {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-2);
      margin: 0 0 var(--quader-space-3);
      padding: 0;
      border: 0;

      legend {
        padding: 0;
        margin-block-end: var(--quader-space-1);
      }
    }
    .mode-option {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      column-gap: var(--quader-space-2);
      padding: var(--quader-space-2) var(--quader-space-3);
      border: 1px solid var(--quader-border);
      border-radius: var(--quader-radius-l);

      input {
        grid-row: 1 / span 2;
      }
      .hint {
        grid-column: 2;
        margin: 0;
      }
    }
    .mode-title {
      font-weight: 600;
    }
    .sync-block {
      margin: 0 0 var(--quader-space-3);
    }
    .facts {
      margin-block: 0 var(--quader-space-3);
      grid-template-columns: 11rem minmax(0, 1fr);
    }
    .secondary {
      display: block;
    }
    .bounds {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-2);
      margin: var(--quader-space-3) 0 0;
      padding: 0;
      border: 0;

      legend {
        padding: 0;
      }
    }
    .bound-row {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
    }
    /* The upper version and whether it is taken, on one line; the choice moves under the field where the line is too narrow. */
    .to-row {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2);
    }
    .to-inclusive {
      inline-size: auto;
      max-inline-size: 100%;
    }
    .field-error {
      margin: 0;
    }
  `,
})
export class DownloadPreviewDialog {
  private readonly api = inject(RemoteDownloadsApi);
  private readonly catalogue = inject(UpstreamCatalogueApi);
  protected readonly data = inject<DownloadPreviewData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<DownloadPreviewResult, DownloadPreviewDialog>>(DialogRef);

  /** Once, or kept in sync (R-CUR-027); the preview answers both, the screen shows the chosen one's counts. */
  protected readonly mode = signal<DownloadMode>('once');
  protected readonly syncing = computed(() => this.mode() === 'sync');
  /** A sync that would store nothing, or one the subject has already: not set up (R-CUR-027). */
  protected readonly syncRefused = computed(() => {
    const s = this.preview()?.sync;
    return this.syncing() && !!s && (!!s.existing || !s.delivered);
  });
  protected readonly intervalSentence = syncIntervalSentence;

  /** The bounds the person lowered; null keeps the installation's. */
  protected readonly maxVersions = signal<number | null>(null);
  protected readonly maxBytes = signal<number | null>(null);
  protected readonly prereleases = signal<boolean | null>(null);
  /** The range of a package's versions as typed; '' is no bound on that side (Q-CUR-190). */
  protected readonly fromVersion = signal('');
  protected readonly toVersion = signal('');
  protected readonly toInclusive = signal(true);
  protected readonly starting = signal(false);
  protected readonly startFailure = signal<ApiError | null>(null);

  /** The request with the bounds the person set. */
  private readonly request = computed<RemoteDownloadRequest>(() => {
    const r: Record<string, unknown> = { ...this.data.request };
    if (this.maxVersions() !== null) r['maxVersions'] = this.maxVersions();
    if (this.maxBytes() !== null) r['maxBytes'] = this.maxBytes();
    if (this.prereleases() !== null) r['includePrereleases'] = this.prereleases();
    if (this.fromVersion()) r['fromVersion'] = this.fromVersion();
    if (this.toVersion()) {
      r['toVersion'] = this.toVersion();
      if (!this.toInclusive()) r['toInclusive'] = false;
    }
    return r as RemoteDownloadRequest;
  });

  protected readonly read = resource({
    params: () => this.request(),
    loader: ({ params }) => this.api.preview(this.data.repository, params),
  });
  /** The last answer stays while a changed bound is previewed again; the first one's bounds are the installation's, the ceiling. */
  private last: RemoteDownloadPreview | undefined;
  private ceiling: RemoteDownloadPreview['bounds'] | undefined;
  protected readonly preview = computed(() => {
    const v = valueOf(this.read);
    if (v) {
      this.last = v;
      this.ceiling ??= v.bounds;
    }
    return v ?? this.last;
  });
  /** The installation's bounds, once a preview answered: the fields stay while a refused bound is corrected. */
  protected readonly ceilingBounds = computed(() => (this.preview() ? (this.ceiling ?? null) : null));
  /** The preview's failure, kept while it reads again (R-UI-216). */
  protected readonly failure = failureOf(this.read);
  protected readonly fromError = computed(() => rangeErrorText(this.failure(), '/fromVersion'));
  protected readonly toError = computed(() => rangeErrorText(this.failure(), '/toVersion'));

  /** The versions the opened package lists on its first page, offered in both fields; a failed read offers none. */
  private readonly listed = resource({
    params: () => (this.data.request.level === 'package' && this.data.request.name ? this.data.request.name : undefined),
    loader: async ({ params }) => (await this.catalogue.package(this.data.repository, params, null, '')).versions.map((v) => v.version),
  });
  protected readonly suggestions = computed<readonly string[]>(() => valueOf(this.listed) ?? []);

  protected readonly number = count;
  protected readonly noteText = previewNoteText;
  protected readonly size = formatSize;

  protected gib(bytes: number): string {
    return String(Math.round((bytes / GIB) * 100) / 100);
  }

  /** A bound of the range: blanks around it are dropped, and an empty field is no bound. */
  protected setFrom(value: string): void {
    this.fromVersion.set(value.trim());
  }

  protected setTo(value: string): void {
    this.toVersion.set(value.trim());
  }

  /** A bound is lowered only: a value above the installation's, or not a number, keeps it. */
  protected setVersions(value: string): void {
    const top = this.ceiling?.maxVersions ?? Number.MAX_SAFE_INTEGER;
    const n = Math.floor(Number(value));
    this.maxVersions.set(Number.isFinite(n) && n >= 1 && n < top ? n : null);
  }

  protected setBytes(value: string): void {
    const top = this.ceiling?.maxBytes ?? Number.MAX_SAFE_INTEGER;
    const n = Math.round(Number(value) * GIB);
    this.maxBytes.set(Number.isFinite(n) && n >= 1 && n < top ? n : null);
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (!this.preview() || this.failure() || this.starting() || this.syncRefused()) return;
    this.starting.set(true);
    this.startFailure.set(null);
    try {
      if (this.syncing()) {
        const run = await this.api.createSync(this.data.repository, this.request(), newIdempotencyKey());
        this.ref.close({ bulk: run.bulk.id, sync: true });
        return;
      }
      const started = await this.api.start(this.data.repository, this.request(), newIdempotencyKey());
      this.ref.close({ bulk: started.bulk?.id ?? '', sync: false });
    } catch (e) {
      this.startFailure.set(asApiError(e));
    } finally {
      this.starting.set(false);
    }
  }
}
