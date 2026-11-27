import { DecimalPipe } from '@angular/common';
import { Component, computed, input, signal } from '@angular/core';
import { DownloadLinkSettings } from '../../../api/download-links-api';
import { DEFAULT_STORAGE_PERIOD, StorageConnection, StorageHistory, StorageHistoryConnection, StoragePeriod } from '../../../api/storage-api';
import { TimePipe } from '../../../shared/time-pipe';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { NoValue } from '../../../shared/ui/no-value';
import { formatSize } from '../../packages/format-size';
import { StorageMeter } from './storage-meter';
import { periodWord } from './storage-history';
import { Timeline } from './timeline';
import { capacityUse, connectionState, hostOf, partlyConfigured, retiredConnections, roleOf, shownConnections, unreachable } from './storage-model';

/** A connection's marks of the delivery by links: whether clients get links to it, and whether a Bastion reads it directly. */
export interface LinkRoles {
  readonly links: boolean;
  readonly bastion: boolean;
}

/**
 * The section **Connections** of Settings › Storage (api-and-ui.md R-UI-263, which R-UI-196's tab moved into): one row per connection
 * in the order of the configuration with its name and place, its state in words, its role in the list's order with the marks of the
 * download links, its region and its use against the capacity its element states. A row's fold holds every non-secret key of its
 * element with the key's name (Q-UI-1585), what the start check found, its storage keys by identifier and the times of its life.
 * Retired connections are one line under the table. Nothing here changes a connection (the owner's decision 9).
 */
@Component({
  selector: 'quader-storage-connections',
  imports: [DataTable, DecimalPipe, NoValue, StorageMeter, TimePipe, Timeline],
  templateUrl: './connections-section.html',
  styleUrls: ['./storage-sections.scss', './storage-connections.scss'],
})
export class ConnectionsSection {
  readonly connections = input.required<readonly StorageConnection[]>();
  /** The download links' settings, for the marks Links and Bastion; null while they are not read. */
  readonly links = input<DownloadLinkSettings | null>(null);
  /** The period's cells, for each row's strip (tone only); null while they are read. */
  readonly history = input<StorageHistory | null>(null);
  readonly period = input<StoragePeriod>(DEFAULT_STORAGE_PERIOD);

  protected readonly formatSize = formatSize;
  protected readonly connectionState = connectionState;
  protected readonly unreachable = unreachable;
  protected readonly partlyConfigured = partlyConfigured;
  protected readonly capacityUse = capacityUse;
  protected readonly roleOf = roleOf;

  protected readonly yes = $localize`:@@storage.yes:Yes`;
  protected readonly no = $localize`:@@storage.no:No`;
  protected readonly caption = $localize`:@@storage.connections.caption:Object-store connections in the order of the Helm configuration`;
  protected readonly noConnection = $localize`:@@storage.connections.empty:No replica has recorded a connection yet.`;
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'connection', label: $localize`:@@storage.col.connection:Connection`, width: '12rem', prefer: true },
    { id: 'state', label: $localize`:@@storage.col.state:State`, width: '13.5rem' },
    {
      id: 'role',
      label: $localize`:@@storage.col.role:Role`,
      width: '11rem',
      help: $localize`:@@storage.col.role.help:Uploads go to the first connection that takes them, reads try the copies in this order. Links: clients get signed links. Bastion: a Bastion reads it directly.`,
    },
    { id: 'region', label: $localize`:@@storage.col.region:Region`, width: '5rem' },
    {
      id: 'use',
      label: $localize`:@@storage.col.use:Use`,
      width: '12rem',
      help: $localize`:@@storage.col.use.help:The stored bytes against the capacity set in the connection's element (capacity). Shown, never enforced.`,
    },
    {
      id: 'period',
      label: periodWord(this.period()),
      width: '8rem',
      narrow: true,
      help: $localize`:@@storage.col.period.help:The cells of the period: red when clients were refused, amber when Quader absorbed it. The section Problems names each.`,
    },
  ]);

  protected readonly shown = computed(() => shownConnections(this.connections()));
  protected readonly retiredIds = computed(() => retiredConnections(this.connections()).map((c) => c.id).join(', '));

  /** The connections whose fold is open. */
  protected readonly opened = signal<ReadonlySet<string>>(new Set());

  protected isOpen(c: StorageConnection): boolean {
    return this.opened().has(c.id);
  }

  protected toggle(c: StorageConnection): void {
    const next = new Set(this.opened());
    if (next.has(c.id)) next.delete(c.id);
    else next.add(c.id);
    this.opened.set(next);
  }

  protected openLabel(c: StorageConnection): string {
    return $localize`:@@storage.fold.label:Details of the connection ${c.id}:id:`;
  }

  /** The place cut after each dot and slash, where the line may break (Q-UI-563): the endpoint's host and bucket/prefix. */
  protected locationParts(c: StorageConnection): readonly string[] {
    const where = `${c.bucket}/${c.prefix}`;
    const host = hostOf(c.endpoint);
    return (host ? `${host} · ${where}` : where).split(/(?<=[./])/);
  }

  /** The marks Links and Bastion of a connection; null while the settings are not read or do not name it. */
  protected linkRoles(c: StorageConnection): LinkRoles | null {
    const s = this.links();
    const l = s?.connections.find((x) => x.name === c.id);
    if (!s || !l) return null;
    return { links: s.enabled && l.usable, bastion: l.handoverUsable };
  }

  protected serverSideCopies(c: StorageConnection): readonly { readonly id: string; readonly ok: boolean }[] {
    return Object.entries(c.capabilities?.serverSideCopyFrom ?? {})
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([id, ok]) => ({ id, ok }));
  }

  protected periodDays(c: StorageConnection): number | null {
    return c.scrubPeriodSeconds === null ? null : Math.round(c.scrubPeriodSeconds / 86_400);
  }

  /** A connection's cells of the period; null while they are read or when the period names no such connection. */
  protected cellsOf(c: StorageConnection): StorageHistoryConnection | null {
    return this.history()?.connections.find((h) => h.id === c.id) ?? null;
  }

  protected stripLabel(c: StorageConnection): string {
    return $localize`:@@storage.connections.strip:Cells of ${c.id}:id: in the period`;
  }

  protected useLabel(c: StorageConnection): string {
    return $localize`:@@storage.use.meter:Use of ${c.id}:id:`;
  }
}
