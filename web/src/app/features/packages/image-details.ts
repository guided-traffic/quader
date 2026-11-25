import { ChangeDetectionStrategy, Component, computed, input, signal } from '@angular/core';
import { CopyButton } from '../../shared/copy-button';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { NoValue } from '../../shared/ui/no-value';
import { TimeAgo } from '../../shared/ui/time-ago';
import { formatSize } from './format-size';
import { HistoryStep, ImagePart, cutDigest } from './image-facts';

/** How many steps of a build's history stand before **Show all** (the page grows; nothing scrolls by itself, R-UI-215). */
export const HISTORY_SHOWN = 50;

/**
 * One image of a version, opened from its platform's row or under the image's facts (formats/oci.md R-OCI-040): how it runs (the
 * configuration's facts, as `docker inspect` shows them), its environment and its labels, its layers in the manifest's order with their
 * digests and sizes, and its build history with the layer each step added, as `docker history` shows it, oldest first as the Dockerfile
 * reads. Every value is text; a long one wraps and grows the page. The configuration is shown only to a caller who may read its blob
 * (Q-OCI-273): the page then says so instead.
 */
@Component({
  selector: 'quader-image-details',
  imports: [CopyButton, DataTable, NoValue, TimeAgo],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @let c = part().configuration;
    @if (c) {
      <h3 i18n="@@pkg.config.heading">Configuration</h3>
      @switch (c.state) {
        @case ('read') {
          @if (c.facts.length > 0) {
            <dl class="facts">
              @for (f of c.facts; track f.label) {
                <dt>{{ f.label }}</dt>
                <dd translate="no">
                  @if (f.code) {
                    <code class="wrap">{{ f.value }}</code>
                  } @else {
                    {{ f.value }}
                  }
                </dd>
              }
            </dl>
          }
          <h3 i18n="@@pkg.config.env">Environment</h3>
          @if (c.env.length === 0) {
            <p class="empty" i18n="@@pkg.config.env-none">No environment variables.</p>
          } @else {
            <quader-data-table class="vars" [caption]="envCaption" [columns]="varColumns" [empty]="false">
              @for (v of c.env; track $index) {
                <tr>
                  <th scope="row" translate="no"><code class="wrap">{{ v.name }}</code></th>
                  <td translate="no">
                    <code class="wrap">{{ v.value }}</code>
                    @if (v.cut) {
                      <span class="muted" i18n="@@pkg.config.cut">(cut)</span>
                    }
                  </td>
                </tr>
              }
            </quader-data-table>
          }
          @if (c.omitted.env > 0) {
            <p class="hint" i18n="@@pkg.config.env-more">{c.omitted.env, plural, =1 {1 more variable is not shown.} other {{{ c.omitted.env }} more variables are not shown.}}</p>
          }
          <h3 i18n="@@pkg.config.labels">Labels</h3>
          @if (c.labels.length === 0) {
            <p class="empty" i18n="@@pkg.config.labels-none">No labels.</p>
          } @else {
            <quader-data-table class="vars" [caption]="labelsCaption" [columns]="varColumns" [empty]="false">
              @for (v of c.labels; track $index) {
                <tr>
                  <th scope="row" translate="no"><code class="wrap">{{ v.name }}</code></th>
                  <td translate="no">
                    <span class="wrap-text">{{ v.value }}</span>
                    @if (v.cut) {
                      <span class="muted" i18n="@@pkg.config.cut">(cut)</span>
                    }
                  </td>
                </tr>
              }
            </quader-data-table>
          }
          @if (c.omitted.labels > 0) {
            <p class="hint" i18n="@@pkg.config.labels-more">{c.omitted.labels, plural, =1 {1 more label is not shown.} other {{{ c.omitted.labels }} more labels are not shown.}}</p>
          }
        }
        @case ('hidden') {
          <p class="hint" i18n="@@pkg.config.hidden">The configuration, its environment and its labels are shown to whoever may pull this image.</p>
        }
        @case ('not-read') {
          <p class="hint" i18n="@@pkg.config.not-read">The configuration is read when a client first pulls this image through Quader.</p>
        }
        @case ('too-large') {
          <p class="hint" i18n="@@pkg.config.too-large">The configuration is larger than 1 MiB and is not read.</p>
        }
        @default {
          <p class="hint" i18n="@@pkg.config.invalid">The configuration cannot be read: it is not the JSON of an image's configuration, or it names a key twice.</p>
        }
      }
    }

    <h3 i18n="@@pkg.layers.heading">Layers</h3>
    @if (part().layers.length === 0) {
      <p class="empty" i18n="@@pkg.layers.none">No layers.</p>
    } @else {
      <quader-data-table class="layers" [caption]="layersCaption" [columns]="layerColumns" [empty]="false">
        @for (l of part().layers; track $index; let i = $index) {
          <tr>
            <td class="num">{{ i + 1 }}</td>
            <th scope="row" class="digest-cell">
              <code translate="no" [attr.title]="l.digest">{{ cut(l.digest) }}</code>
              <quader-copy-button [value]="l.digest" [label]="copyLayer(i + 1)" />
            </th>
            <td class="media-type" translate="no">
              {{ l.mediaType }}
              @if (l.foreign) {
                <span class="muted block" i18n="@@pkg.layers.foreign">Not held by a registry</span>
              }
            </td>
            <td class="num nowrap">
              @if (l.size !== null) {
                <span [attr.title]="l.size + ' B'">{{ size(l.size) }}</span>
              } @else {
                <quader-no-value />
              }
            </td>
          </tr>
        }
      </quader-data-table>
      @if (part().moreLayers > 0) {
        <p class="hint" i18n="@@pkg.layers.more">{part().moreLayers, plural, =1 {1 more layer is not shown.} other {{{ part().moreLayers }} more layers are not shown.}}</p>
      }
    }

    @if (c?.state === 'read') {
      <h3 i18n="@@pkg.history.heading">History</h3>
      @if (history().length === 0) {
        <p class="empty" i18n="@@pkg.history.none">No build history.</p>
      } @else {
        <quader-data-table class="history" [caption]="historyCaption" [columns]="historyColumns" [empty]="false">
          @for (h of shownHistory(); track $index) {
            <tr>
              <td class="num">{{ $index + 1 }}</td>
              <th scope="row" translate="no">
                <code class="wrap command">{{ h.createdBy }}</code>
                @if (h.cut) {
                  <span class="muted" i18n="@@pkg.config.cut">(cut)</span>
                }
                @if (h.comment) {
                  <span class="muted block">{{ h.comment }}</span>
                }
              </th>
              <td class="num nowrap">
                @if (h.emptyLayer) {
                  <span class="muted" [attr.title]="noLayerTitle">0 B</span>
                } @else if (h.layer?.size; as s) {
                  <span [attr.title]="s + ' B'">{{ size(s) }}</span>
                } @else {
                  <quader-no-value />
                }
              </td>
              <td class="nowrap">
                @if (h.created) {
                  <quader-time-ago [at]="h.created" absolute="title" />
                } @else {
                  <quader-no-value />
                }
              </td>
            </tr>
          }
        </quader-data-table>
        @if (hiddenSteps() > 0) {
          <p class="more-steps">
            <button type="button" class="btn btn-default btn-sm" (click)="all.set(true)" i18n="@@pkg.history.show-all">
              Show all {{ history().length }} steps
            </button>
          </p>
        }
        @if (omittedSteps() > 0) {
          <p class="hint" i18n="@@pkg.history.more">{omittedSteps(), plural, =1 {1 more step is not kept.} other {{{ omittedSteps() }} more steps are not kept.}}</p>
        }
      }
    }
  `,
  styles: `
    :host {
      display: block;
    }
    h3 {
      margin-block: var(--quader-space-4) var(--quader-space-2);
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    h3:first-child {
      margin-block-start: 0;
    }
    /* A value wraps where it must and grows the page (R-UI-215): a command, a path or an environment value is never cut by a box. */
    code.wrap,
    .wrap-text {
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .digest-cell {
      white-space: nowrap;
    }
    .media-type {
      overflow-wrap: anywhere;
    }
    .block {
      display: block;
    }
    .muted {
      color: var(--quader-text-muted);
    }
    .nowrap {
      white-space: nowrap;
    }
    .empty,
    .hint {
      margin: 0;
      color: var(--quader-text-muted);
    }
    .hint {
      margin-block-start: var(--quader-space-1);
    }
    .more-steps {
      margin: var(--quader-space-2) 0 0;
    }
  `,
})
export class ImageDetails {
  readonly part = input.required<ImagePart>();

  /** Every step of the history is shown, after **Show all**. */
  protected readonly all = signal(false);
  protected readonly history = computed<readonly HistoryStep[]>(() => this.part().configuration?.history ?? []);
  protected readonly shownHistory = computed(() => (this.all() ? this.history() : this.history().slice(0, HISTORY_SHOWN)));
  protected readonly hiddenSteps = computed(() => this.history().length - this.shownHistory().length);
  /** The steps the reader's bound did not keep. */
  protected readonly omittedSteps = computed(() => this.part().configuration?.omitted.history ?? 0);

  protected readonly size = formatSize;
  protected readonly cut = cutDigest;
  protected copyLayer(n: number): string {
    return $localize`:@@pkg.layers.copy:Copy the digest of layer ${n}:number:`;
  }
  protected readonly varColumns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@pkg.config.name:Name`, width: '10rem' },
    { id: 'value', label: $localize`:@@pkg.config.value:Value`, prefer: true },
  ];
  protected readonly layerColumns: readonly TableColumn[] = [
    { id: 'n', label: $localize`:@@pkg.layers.number:No.`, numeric: true, width: '3rem' },
    { id: 'digest', label: $localize`:@@pkg.image.digest:Digest`, width: '16rem' },
    { id: 'mediaType', label: $localize`:@@pkg.image.media-type:Media type`, prefer: true },
    { id: 'size', label: $localize`:@@pkg.image.size:Size`, numeric: true, width: '6rem' },
  ];
  protected readonly historyColumns: readonly TableColumn[] = [
    { id: 'n', label: $localize`:@@pkg.history.step:Step`, numeric: true, width: '3rem' },
    { id: 'command', label: $localize`:@@pkg.history.command:Created by`, prefer: true },
    { id: 'size', label: $localize`:@@pkg.image.size:Size`, numeric: true, width: '6rem' },
    { id: 'created', label: $localize`:@@pkg.history.created:Created`, width: '8rem' },
  ];
  protected readonly envCaption = $localize`:@@pkg.config.env-caption:Environment of the image`;
  protected readonly labelsCaption = $localize`:@@pkg.config.labels-caption:Labels of the image`;
  protected readonly layersCaption = $localize`:@@pkg.layers.caption:Layers of the image`;
  protected readonly historyCaption = $localize`:@@pkg.history.caption:Build history of the image`;
  protected readonly noLayerTitle = $localize`:@@pkg.history.no-layer:This step added no layer.`;
}
