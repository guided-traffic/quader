import { Component, computed, inject, resource, signal } from '@angular/core';
import { newIdempotencyKey } from '../../../api/client';
import { ApiError } from '../../../api/problem';
import { RemoteDownloadPreview, RemoteDownloadRequest, RemoteDownloadsApi } from '../../../api/remote-downloads-api';
import { Checkbox } from '../../../shared/ui/checkbox';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../../shared/ui/dialog';
import { ErrorNotice } from '../../../shared/error-notice';
import { asApiError, valueOf } from '../../../shared/paged-resource';
import { formatSize } from '../../packages/format-size';

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
 * The preview of a bulk download (remote-curation.md R-CUR-010, R-CUR-018): what will be fetched (versions, packages, bytes, the
 * versions whose size the upstream does not tell), what is stored already and what is not delivered here (fetched for examination),
 * the bounds as fields with their values, which may only be lowered, and **Start download**. A changed bound is previewed again
 * before the start. Closes with the started bulk's ID, which expands into rows while the tab Downloads shows it (Q-CUR-168).
 */
@Component({
  selector: 'quader-download-preview-dialog',
  imports: [Checkbox, DialogClose, DialogTitle, ErrorNotice],
  template: `
    <h2 quaderDialogTitle>{{ data.title }}</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (failure(); as f) {
          <quader-error-notice [error]="f" (retry)="read.reload()" />
        } @else if (preview(); as p) {
          <dl class="facts" data-testid="download-preview">
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
              @if (p.notDelivered > 0) {
                <span class="secondary" i18n="@@upstream.preview.not-delivered-hint">Fetched for examination: stored and scanned, not delivered until a rule allows them.</span>
              }
            </dd>
          </dl>
          @if (p.note) {
            <p class="hint" translate="no">{{ p.note }}</p>
          }
          <fieldset class="plain bounds">
            <legend class="field-label" i18n="@@upstream.preview.bounds">Bounds</legend>
            <div class="bound-row">
              <label class="field-label" for="preview-max-versions" i18n="@@upstream.preview.max-versions">At most this many versions</label>
              <input
                id="preview-max-versions"
                class="text-input field-w-number"
                type="number"
                inputmode="numeric"
                min="1"
                [max]="topVersions(p)"
                [value]="maxVersions() ?? p.bounds.maxVersions"
                (change)="setVersions($any($event.target).value, p)"
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
                [value]="gib(maxBytes() ?? p.bounds.maxBytes)"
                (change)="setBytes($any($event.target).value, p)"
              />
            </div>
            <quader-checkbox [checked]="prereleases() ?? p.bounds.includePrereleases" (checkedChange)="prereleases.set($event)">
              <ng-container i18n="@@upstream.preview.prereleases">Include pre-releases</ng-container>
            </quader-checkbox>
          </fieldset>
          @if (startFailure(); as f) {
            <quader-error-notice [error]="f" />
          }
        } @else {
          <p role="status" i18n="@@upstream.preview.loading">Reading what the download would fetch.</p>
        }
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@upstream.dialog.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="!preview() || starting()" i18n="@@upstream.preview.start">Start download</button>
      </div>
    </form>
  `,
  styles: `
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
  `,
})
export class DownloadPreviewDialog {
  private readonly api = inject(RemoteDownloadsApi);
  protected readonly data = inject<DownloadPreviewData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<string, DownloadPreviewDialog>>(DialogRef);

  /** The bounds the person lowered; null keeps the installation's. */
  protected readonly maxVersions = signal<number | null>(null);
  protected readonly maxBytes = signal<number | null>(null);
  protected readonly prereleases = signal<boolean | null>(null);
  protected readonly starting = signal(false);
  protected readonly startFailure = signal<ApiError | null>(null);

  /** The request with the bounds the person set. */
  private readonly request = computed<RemoteDownloadRequest>(() => {
    const r: Record<string, unknown> = { ...this.data.request };
    if (this.maxVersions() !== null) r['maxVersions'] = this.maxVersions();
    if (this.maxBytes() !== null) r['maxBytes'] = this.maxBytes();
    if (this.prereleases() !== null) r['includePrereleases'] = this.prereleases();
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
  protected readonly failure = computed(() => {
    const e = this.read.error();
    return e ? asApiError(e) : null;
  });

  protected readonly number = count;
  protected readonly size = formatSize;

  protected gib(bytes: number): string {
    return String(Math.round((bytes / GIB) * 100) / 100);
  }

  /** A bound is lowered only: a value above the installation's, or not a number, keeps it. */
  protected setVersions(value: string, p: RemoteDownloadPreview): void {
    const top = (this.ceiling ?? p.bounds).maxVersions;
    const n = Math.floor(Number(value));
    this.maxVersions.set(Number.isFinite(n) && n >= 1 && n < top ? n : null);
  }

  protected setBytes(value: string, p: RemoteDownloadPreview): void {
    const top = (this.ceiling ?? p.bounds).maxBytes;
    const n = Math.round(Number(value) * GIB);
    this.maxBytes.set(Number.isFinite(n) && n >= 1 && n < top ? n : null);
  }

  /** The installation's bound of versions: a field's highest value. */
  protected topVersions(p: RemoteDownloadPreview): number {
    return (this.ceiling ?? p.bounds).maxVersions;
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (!this.preview() || this.starting()) return;
    this.starting.set(true);
    this.startFailure.set(null);
    try {
      const started = await this.api.start(this.data.repository, this.request(), newIdempotencyKey());
      this.ref.close(started.bulk?.id ?? '');
    } catch (e) {
      this.startFailure.set(asApiError(e));
    } finally {
      this.starting.set(false);
    }
  }
}
