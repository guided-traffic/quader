import { DecimalPipe, NgTemplateOutlet } from '@angular/common';
import { Component, computed, input, output } from '@angular/core';
import { RouterLink } from '@angular/router';
import { StorageOverview } from '../../../api/storage-api';
import { TimePipe } from '../../../shared/time-pipe';
import { formatSize } from '../../packages/format-size';
import { TileKind, Tone, percentOf, problemsNow, tileTone, tileWord, toneIcon } from './storage-model';

/** The sections of the page a tile leads to; a tile without one is no link (Q-UI-1581). */
const SECTIONS: Partial<Record<TileKind, string>> = {
  connections: 'connections',
  availability: 'availability',
  encryption: 'encryption',
};

interface Tile {
  readonly kind: TileKind;
  readonly label: string;
  readonly state: string;
  readonly word: string;
  readonly tone: Tone;
  readonly section: string | null;
}

/**
 * The summary of the stored content (api-and-ui.md R-UI-262, R-UI-266): six tiles, Connections, Availability, Encryption, Integrity,
 * Problems and Stored, each with its label, its state as a word with its icon and tone, and a line or two of figures with their units.
 * Every state is the overview's (Q-STOR-505, Q-UI-1587); a tile with a section on the page is a link to it (Q-UI-1581). The dashboard
 * shows the same tiles in their compact form, the state and one figure each (Q-UI-1589), so the card and the page word a state once.
 * Until the history of the period is recorded, the tile Problems names the problems of now.
 */
@Component({
  selector: 'quader-storage-summary',
  imports: [DecimalPipe, NgTemplateOutlet, RouterLink, TimePipe],
  template: `
    <ul class="tiles" [class.compact]="compact()" role="list">
      @for (t of tiles(); track t.kind) {
        <li class="tile" [attr.data-tile]="t.kind">
          @if (t.section) {
            <a class="tile-link" [routerLink]="page" [fragment]="t.section" (click)="go.emit(t.section)">
              <ng-container [ngTemplateOutlet]="body" [ngTemplateOutletContext]="{ $implicit: t }" />
            </a>
          } @else {
            <div class="tile-link">
              <ng-container [ngTemplateOutlet]="body" [ngTemplateOutletContext]="{ $implicit: t }" />
            </div>
          }
        </li>
      }
    </ul>
    <ng-template #body let-t>
      <span class="label">{{ t.label }}</span>
      @if (t.word) {
        <span class="state" [attr.data-tone]="t.tone"
          ><span class="material-icons" aria-hidden="true">{{ icon(t.tone) }}</span>{{ t.word }}</span
        >
      }
      @let o = overview();
      @switch (t.kind) {
        @case ('connections') {
          <span class="figure" i18n="@@storage.tile.connections.reachable"
            >{{ o.connections.reachable }} of {{ o.connections.configured }} reachable</span
          >
          @if (!compact()) {
            @for (u of o.connections.unreachable; track u.id) {
              <span class="figure muted" i18n="@@storage.tile.connections.unreachable-on"
                ><span translate="no">{{ u.id }}</span> unreachable on {{ u.circuitOpen }} of {{ u.configured }} replicas</span
              >
            }
            @if (o.connections.detached.length > 0) {
              <span class="figure muted" i18n="@@storage.tile.connections.detached"
                >Detached: <span translate="no">{{ o.connections.detached.join(', ') }}</span></span
              >
            }
          }
        }
        @case ('availability') {
          @if (o.availability.belowTarget.blobs > 0) {
            <span class="figure" i18n="@@storage.tile.availability.below"
              >{o.availability.belowTarget.blobs, plural, =1 {1 blob} other {{{ o.availability.belowTarget.blobs | number }} blobs}} ({{
                size(o.availability.belowTarget.bytes)
              }}) below the target</span
            >
          } @else if (o.availability.state === 'one_store') {
            <span class="figure" i18n="@@storage.tile.availability.one">No copy elsewhere</span>
          } @else {
            <span class="figure" i18n="@@storage.tile.availability.every"
              >Every blob on {{ o.availability.target }} connections</span
            >
          }
        }
        @case ('encryption') {
          <span class="figure" i18n="@@storage.tile.encryption.connections"
            >{{ o.encryption.encrypted.length }} of {{ o.encryption.encrypted.length + o.encryption.plain.length }} connections</span
          >
          @if (!compact() && o.encryption.encrypted.length > 0) {
            <span class="figure muted" i18n="@@storage.tile.encryption.share">{{ percent(o.encryption.encryptedShare) }} % of the bytes</span>
          }
        }
        @case ('integrity') {
          @switch (o.integrity.state) {
            @case ('damaged') {
              <span class="figure" i18n="@@storage.tile.integrity.damaged-blobs"
                >{o.integrity.damagedNow.blobs, plural, =1 {1 blob without a whole copy} other {{{ o.integrity.damagedNow.blobs | number }} blobs without a whole copy}}</span
              >
            }
            @case ('repairing') {
              <span class="figure" i18n="@@storage.tile.integrity.damaged-copies"
                >{o.integrity.damagedNow.copies, plural, =1 {1 damaged copy} other {{{ o.integrity.damagedNow.copies | number }} damaged copies}}</span
              >
            }
            @case ('behind') {
              <span class="figure" i18n="@@storage.tile.integrity.overdue"
                >{o.integrity.scrub.overdue, plural, =1 {1 copy overdue} other {{{ o.integrity.scrub.overdue | number }} copies overdue}}</span
              >
            }
            @default {
              <span class="figure" i18n="@@storage.tile.integrity.scrub">Scrub {{ percent(o.integrity.scrub.coverage) }} %</span>
            }
          }
          @if (!compact()) {
            @if (o.integrity.authenticatedReads.length > 0) {
              <span class="figure muted" i18n="@@storage.tile.integrity.every-read"
                >Every read checked: <span translate="no">{{ o.integrity.authenticatedReads.join(', ') }}</span></span
              >
            }
            @if (o.integrity.scrub.lastRunAt; as at) {
              <span class="figure muted" i18n="@@storage.tile.integrity.last">Last scrub {{ at | time }}</span>
            }
          }
        }
        @case ('problems') {
          @switch (t.state) {
            @case ('ongoing') {
              @if (o.integrity.damagedNow.blobs > 0) {
                <span class="figure" i18n="@@storage.tile.problems.unavailable"
                  >{o.integrity.damagedNow.blobs, plural, =1 {1 blob unavailable} other {{{ o.integrity.damagedNow.blobs | number }} blobs unavailable}}</span
                >
              }
              @for (u of o.connections.unreachable; track u.id) {
                <span class="figure" i18n="@@storage.tile.problems.unreachable"><span translate="no">{{ u.id }}</span> unreachable</span>
              }
            }
            @case ('open') {
              <span class="figure" i18n="@@storage.tile.problems.repairing"
                >{o.integrity.damagedNow.copies, plural, =1 {1 copy to repair} other {{{ o.integrity.damagedNow.copies | number }} copies to repair}}</span
              >
            }
          }
        }
        @case ('stored') {
          <span class="figure strong">{{ size(o.stored.bytes) }}</span>
          @if (!compact()) {
            <span class="figure muted" i18n="@@storage.tile.stored.blobs"
              >{o.stored.blobs, plural, =1 {in 1 blob} other {in {{ o.stored.blobs | number }} blobs}}</span
            >
          }
        }
      }
    </ng-template>
  `,
  styleUrl: './storage-summary.scss',
})
export class StorageSummary {
  /** The overview of now. */
  readonly overview = input.required<StorageOverview>();
  /** The dashboard's form: the state and one figure per tile (Q-UI-1589). */
  readonly compact = input(false);
  /** A tile's section was asked for: the page moves to it (the address keeps its fragment). */
  readonly go = output<string>();

  protected readonly page = '/admin/settings/storage';
  protected readonly size = formatSize;
  protected readonly percent = percentOf;
  protected readonly icon = toneIcon;

  protected readonly tiles = computed<readonly Tile[]>(() => {
    const o = this.overview();
    const tile = (kind: TileKind, label: string, state: string): Tile => ({
      kind,
      label,
      state,
      word: tileWord(kind, state),
      tone: tileTone(kind, state),
      section: SECTIONS[kind] ?? null,
    });
    return [
      tile('connections', $localize`:@@storage.tile.connections:Connections`, o.connections.state),
      tile('availability', $localize`:@@storage.tile.availability:Availability`, o.availability.state),
      tile('encryption', $localize`:@@storage.tile.encryption:Encryption`, o.encryption.state),
      tile('integrity', $localize`:@@storage.tile.integrity:Integrity`, o.integrity.state),
      tile('problems', $localize`:@@storage.tile.problems-now:Problems now`, problemsNow(o)),
      { ...tile('stored', $localize`:@@storage.tile.stored:Stored`, ''), word: '', tone: 'neutral' },
    ];
  });
}
