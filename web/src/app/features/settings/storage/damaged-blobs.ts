import { Component, computed, inject, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { DamagedBlob, DamagedBlobCopy, DamagedPage, StorageApi } from '../../../api/storage-api';
import { Notifier } from '../../../core/notifier';
import { FailureToasts } from '../../../shared/failure-dialog';
import { asApiError } from '../../../shared/paged-resource';
import { Busy } from '../../../shared/ui/busy';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { Disclosure } from '../../../shared/ui/disclosure';
import { formatSize } from '../../packages/format-size';

/** The blobs no connection holds whole that the list shows; the operator check lists every one. */
export const DAMAGED_SHOWN = 20;

/**
 * The blobs no connection holds whole (storage.md R-STOR-027, R-STOR-052; api-and-ui.md R-UI-263, moved from R-UI-196 with Q-UI-935):
 * a closed section of Availability while there is one, because such a blob is not served until a copy returns. Each with its digest,
 * its state, the state of each copy and the first item that references it; a holder of `settings:write` checks one again, which reads
 * every copy and writes no object (audited as `storage.blob.rechecked`).
 */
@Component({
  selector: 'quader-storage-damaged-blobs',
  imports: [Busy, DataTable, Disclosure, RouterLink],
  template: `
    @let d = page();
    <quader-disclosure [heading]="caption" [hint]="hint" [count]="d.items.length" [level]="3">
      <quader-data-table [caption]="caption" [columns]="columns()">
        @for (b of d.items; track b.sha256) {
          <tr>
            <th scope="row" translate="no">
              <code [attr.title]="b.sha256">{{ short(b.sha256) }}</code>
              <span class="secondary">{{ size(b.size) }}</span>
            </th>
            <td>
              @if (b.contentStatus === 'corrupt') {
                <span class="bad"
                  ><span class="material-icons" aria-hidden="true">error</span
                  ><ng-container i18n="@@storage.blob.corrupt">Corrupt</ng-container></span
                >
              } @else {
                <span class="bad"
                  ><span class="material-icons" aria-hidden="true">error</span
                  ><ng-container i18n="@@storage.blob.missing">Missing</ng-container></span
                >
              }
            </td>
            <td class="lines-cell">
              <div class="lines">
                @for (cp of copiesOf(b); track cp.connection) {
                  <span class="line"><span translate="no">{{ cp.connection }}</span>: {{ copyStateText(cp.state) }}</span>
                } @empty {
                  <span class="line" i18n="@@storage.damaged.no-copy">No copy</span>
                }
              </div>
            </td>
            <td class="item-cell">
              @if (b.items[0]; as i) {
                @if (i.trashed) {
                  <span translate="no">{{ i.project }}/{{ i.repository }}: {{ i.path }}</span>&ngsp;<span class="secondary" i18n="@@storage.damaged.trashed"
                    >(in the trash)</span
                  >
                } @else {
                  <a [routerLink]="itemLink(i)" translate="no">{{ i.project }}/{{ i.repository }}: {{ i.path }}</a>
                }
                @if (b.itemCount > 1) {
                  &ngsp;<span class="secondary" i18n="@@storage.damaged.more-items"
                    >{moreItems(b), plural, =1 {and 1 more} other {and {{ moreItems(b) }} more}}</span
                  >
                }
              } @else {
                <span class="secondary" i18n="@@storage.damaged.no-item">No item; a cache entry, a UI asset or an avatar</span>
              }
            </td>
            @if (canRecheck()) {
              <td class="actions">
                <button
                  class="btn btn-tertiary"
                  type="button"
                  [disabled]="checking() === b.sha256"
                  [quaderBusy]="checking() === b.sha256"
                  [attr.aria-label]="checkLabel(b)"
                  (click)="recheck(b)"
                >
                  <ng-container i18n="@@storage.damaged.check">Check again</ng-container>
                </button>
              </td>
            }
          </tr>
        }
      </quader-data-table>
      @if (d.more) {
        <p class="note">
          <ng-container i18n="@@storage.damaged.more">The list shows the first {{ d.items.length }}.</ng-container>&ngsp;<code translate="no"
            >quader admin storage verify</code
          >&ngsp;<ng-container i18n="@@storage.damaged.verify">lists every one.</ng-container>
        </p>
      }
    </quader-disclosure>
  `,
  styleUrl: './storage-sections.scss',
})
export class DamagedBlobs {
  private readonly api = inject(StorageApi);
  private readonly notifier = inject(Notifier);
  private readonly failureToasts = inject(FailureToasts);

  /** The first page of `scope=blobs`. */
  readonly page = input.required<DamagedPage>();
  readonly canRecheck = input(false);

  protected readonly size = formatSize;
  protected readonly caption = $localize`:@@storage.damaged.caption:Blobs no connection holds whole`;
  protected readonly hint = $localize`:@@storage.damaged.hint-short:Not served until a copy returns: a later upload of the same file, a refetch or the object's return.`;
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'blob', label: $localize`:@@storage.damaged.col.blob:Blob` },
    { id: 'state', label: $localize`:@@storage.damaged.col.state:State` },
    { id: 'copies', label: $localize`:@@storage.damaged.col.copies:Copies`, width: '13rem' },
    { id: 'item', label: $localize`:@@storage.damaged.col.item:Referenced by`, width: '12rem', prefer: true },
    ...(this.canRecheck() ? [{ id: 'actions', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true }] : []),
  ]);

  /** The copies a recheck answered, by digest, shown in place of the list's until the list is read again. */
  protected readonly rechecked = signal<ReadonlyMap<string, readonly DamagedBlobCopy[]>>(new Map());
  protected readonly checking = signal<string | null>(null);

  protected short(sha256: string): string {
    return sha256.slice(0, 12);
  }

  protected moreItems(b: DamagedBlob): number {
    return Math.max(0, b.itemCount - 1);
  }

  protected copiesOf(b: DamagedBlob): readonly DamagedBlobCopy[] {
    return this.rechecked().get(b.sha256) ?? b.copies;
  }

  protected copyStateText(state: string): string {
    switch (state) {
      case 'present':
        return $localize`:@@storage.copy.present:present`;
      case 'missing':
        return $localize`:@@storage.copy.missing:missing`;
      case 'corrupt':
        return $localize`:@@storage.copy.corrupt:corrupt`;
      case 'deleting':
        return $localize`:@@storage.copy.deleting:being deleted`;
    }
    return state;
  }

  protected itemLink(i: DamagedBlob['items'][number]): string[] {
    return ['/projects', i.project, 'repositories', i.repository, 'files', ...i.path.split('/')];
  }

  protected checkLabel(b: DamagedBlob): string {
    return $localize`:@@storage.damaged.check-label:Check the copies of ${this.short(b.sha256)}:digest: again`;
  }

  /** Reads the blob's copies again (R-STOR-027) and shows the states the server answered; writes no object. */
  protected async recheck(b: DamagedBlob): Promise<void> {
    this.checking.set(b.sha256);
    try {
      const out = await this.api.recheck(b.sha256);
      this.rechecked.update((m) => new Map(m).set(b.sha256, out.copies));
      this.notifier.status(
        out.contentStatus === 'ok'
          ? $localize`:@@storage.damaged.checked-ok:A connection holds ${this.short(b.sha256)}:digest: whole again.`
          : $localize`:@@storage.damaged.checked-still:No connection holds ${this.short(b.sha256)}:digest: whole yet.`,
      );
    } catch (e) {
      this.failureToasts.alert(asApiError(e));
    } finally {
      this.checking.set(null);
    }
  }
}
