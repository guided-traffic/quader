import { DecimalPipe } from '@angular/common';
import { Component, computed, input, signal } from '@angular/core';
import { StorageConnection } from '../../../api/storage-api';
import { EncryptionStatus } from '../../../api/system-api';
import { TimePipe } from '../../../shared/time-pipe';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { NoValue } from '../../../shared/ui/no-value';
import { Help } from '../../../shared/ui/help';
import { StorageMeter } from './storage-meter';
import { EncryptsRow, appliedSse, holdingConnections, inFormat, keyRing, percentOf, whatQuaderEncrypts } from './storage-model';

/**
 * The section **Encryption** of Settings › Storage (api-and-ui.md R-UI-264; storage.md R-STOR-075, R-STOR-032): per connection whether
 * Quader encrypts it, since when and how much of it is in Quader's format; what the object store is asked for, beside what it applied to
 * the start check's probe (B.7: configured and actual in two columns, "Differs" where the store did not apply what it is asked for, a
 * bucket default under `none` named, Q-UI-1610; "not checked yet" before a probe); its storage keys by identifier. Then what Quader
 * encrypts, family by family (Q-UI-1588), and the root key's ring. Identifiers and times only: never key material, a sealed value or a
 * credential.
 */
@Component({
  selector: 'quader-storage-encryption',
  imports: [DataTable, DecimalPipe, NoValue, Help, StorageMeter, TimePipe],
  templateUrl: './encryption-section.html',
  styleUrls: ['./storage-sections.scss', './storage-encryption.scss'],
})
export class EncryptionSection {
  readonly connections = input.required<readonly StorageConnection[]>();
  /** The key ring of the database's sealed values; null while it is not read. */
  readonly ring = input<EncryptionStatus | null>(null);

  protected readonly inFormat = inFormat;
  protected readonly applied = appliedSse;
  protected readonly keyRing = keyRing;
  protected readonly percent = percentOf;

  protected readonly caption = $localize`:@@storage.encryption.caption:Encryption per connection`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'connection', label: $localize`:@@storage.col.connection:Connection`, width: '13rem' },
    {
      id: 'quader',
      label: $localize`:@@storage.col.quader:Quader`,
      width: '13rem',
      prefer: true,
      help: $localize`:@@storage.col.quader.help:Quader encrypts every blob in its own format before it leaves the pod (encrypted). Every read is authenticated.`,
    },
    {
      id: 'store',
      label: $localize`:@@storage.col.object-store:Object store`,
      width: '13rem',
      help: $localize`:@@storage.col.object-store.help:The server-side encryption the store is asked for (sse_mode), beside the one it applied.`,
    },
    { id: 'keys', label: $localize`:@@storage.col.storage-keys:Storage keys`, width: '12rem' },
  ];

  protected readonly rows = computed(() => holdingConnections(this.connections()));
  protected readonly encrypts = computed(() => whatQuaderEncrypts(this.connections()));
  /** The connections whose list of storage keys is open. */
  protected readonly keysOpen = signal<ReadonlySet<string>>(new Set());

  protected toggleKeys(c: StorageConnection): void {
    const next = new Set(this.keysOpen());
    if (next.has(c.id)) next.delete(c.id);
    else next.add(c.id);
    this.keysOpen.set(next);
  }

  /** A storage key's state in words (R-STOR-088, R-STOR-097). */
  protected keyState(state: string): string {
    switch (state) {
      case 'active':
        return $localize`:@@storage.keys.state.active:active`;
      case 'retired':
        return $localize`:@@storage.keys.state.retired:retired`;
      case 'destroyed':
        return $localize`:@@storage.keys.state.destroyed:destroyed`;
    }
    return state;
  }

  protected keysLabel(c: StorageConnection): string {
    return $localize`:@@storage.keys.toggle:Storage keys of ${c.id}:id:`;
  }

  protected formatLabel(c: StorageConnection): string {
    return $localize`:@@storage.format.meter:Copies of ${c.id}:id: in Quader's format`;
  }

  /** The words of a family of "What Quader encrypts". */
  protected familyText(r: EncryptsRow): string {
    const on = r.connections.join(', ');
    switch (r.family) {
      case 'artifacts':
        return $localize`:@@storage.encrypts.artifacts:Artifacts on ${on}:connections:`;
      case 'upload-parts':
        return $localize`:@@storage.encrypts.upload-parts:Upload parts on ${on}:connections:`;
      case 'upload-spool':
        return $localize`:@@storage.encrypts.upload-spool:Upload spool (pod disk)`;
      case 'database-secrets':
        return $localize`:@@storage.encrypts.database-secrets:Secrets in the database`;
      case 'database-other':
        return $localize`:@@storage.encrypts.database-other:Other database content`;
      case 'backup-artifacts':
        return $localize`:@@storage.encrypts.backup-artifacts:Backups: artifacts as stored`;
      case 'backup-database':
        return $localize`:@@storage.encrypts.backup-database:Backups: database`;
      case 'journal':
        return $localize`:@@storage.encrypts.journal:Journal, marker, probes: plain on purpose`;
      case 'bastion-caches':
        return $localize`:@@storage.encrypts.bastion-caches:Bastion caches: per Bastion`;
    }
  }

  /** The help of a family that has an open gap or a condition (H-2432). */
  protected familyHelp(r: EncryptsRow): string {
    switch (r.family) {
      case 'artifacts':
        return r.gap ? $localize`:@@storage.encrypts.artifacts.help:A plain connection beside an encrypted one keeps the content in the clear.` : '';
      case 'upload-spool':
        return $localize`:@@storage.encrypts.upload-spool.help:An upload waits on the pod's disk in the clear until it is stored.`;
      case 'database-secrets':
        return $localize`:@@storage.encrypts.database-secrets.help:Credentials, signing keys and storage keys are sealed under the root key.`;
      case 'database-other':
        return $localize`:@@storage.encrypts.database-other.help:Names, metadata and the audit log stand in the clear; encrypt the database's volume.`;
      case 'backup-artifacts':
        return $localize`:@@storage.encrypts.backup-artifacts.help:A backup's pool keeps each blob as its connection stores it.`;
      case 'backup-database':
        return $localize`:@@storage.encrypts.backup-database.help:A backup's database files are not encrypted by Quader.`;
      case 'journal':
        return $localize`:@@storage.encrypts.journal.help:The revocation journal, the marker and the probes hold no content.`;
      case 'bastion-caches':
        return $localize`:@@storage.encrypts.bastion-caches.help:A Bastion encrypts its cache when its chart gives it a key.`;
    }
    return '';
  }

  protected markIcon(r: EncryptsRow): string {
    switch (r.mark) {
      case 'yes':
        return 'lock';
      case 'partly':
        return 'contrast';
      case 'plain':
        return 'remove';
    }
    return 'lock_open';
  }

  protected markText(r: EncryptsRow): string {
    switch (r.mark) {
      case 'yes':
        return $localize`:@@storage.encrypts.mark.yes:Encrypted`;
      case 'partly':
        return $localize`:@@storage.encrypts.mark.partly:Depends`;
      case 'plain':
        return $localize`:@@storage.encrypts.mark.plain:Plain on purpose`;
    }
    return $localize`:@@storage.encrypts.mark.no:Not encrypted`;
  }
}
