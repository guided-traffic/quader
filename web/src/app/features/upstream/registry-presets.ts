import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, inject, input, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { ApiError } from '../../api/problem';
import { FormatDescriptor } from '../../api/repositories-api';
import { RegistryPreset, UpstreamApi } from '../../api/upstream-api';
import { Notifier } from '../../core/notifier';
import { SelectOption } from '../../shared/access/fields';
import { Confirm } from '../../shared/confirm-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatIconView } from '../../shared/format-icon';
import { asApiError } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { QuaderDialog } from '../../shared/ui/dialog';
import { NoValue } from '../../shared/ui/no-value';
import { RegistryPresetDialog, RegistryPresetDialogData } from './registry-preset-dialog';

/** One group of the table: a format and its presets in the administrator's order. */
interface PresetGroup {
  readonly format: string;
  readonly rows: readonly { readonly preset: RegistryPreset; readonly index: number }[];
}

/**
 * **Registries of this installation** on the page Upstream registries (R-UI-195, R-REPO-076, Q-UI-906): the presets an administrator
 * keeps of the company's own registries, which the form of a new remote repository offers as buttons in every project. The table is
 * grouped by format in catalog order; **Add registry**, **Edit** and **Remove** send the whole list with the `ETag` it was read with
 * (`setRegistryPresets`). A reader sees the table alone.
 */
@Component({
  selector: 'quader-registry-presets',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, DataTable, ErrorNotice, FormatIconView, NoValue],
  template: `
    <h2 class="presets-heading" id="registry-presets">{{ heading }}</h2>
    <p class="hint presets-hint" i18n="@@presets.hint">
      Your organization's own registries. The form of a new remote repository offers them as buttons in every project, after the
      well-known registries. A preset holds no credential: each project enters its own.
    </p>
    @if (canWrite()) {
      <p class="presets-actions">
        <button class="btn btn-default" type="button" [disabled]="list() === null || busy()" (click)="add()" i18n="@@presets.add">
          Add registry
        </button>
      </p>
    }
    @if (actionError(); as e) {
      <quader-error-notice [error]="e" />
    }
    <quader-data-table
      class="presets"
      [caption]="heading"
      [columns]="columns()"
      [loaded]="list() !== null"
      [loading]="list() === null && !loadError()"
      [error]="loadError()"
      (retry)="load()"
      [empty]="groups().length === 0"
      [emptyText]="canWrite() ? emptyWriter : emptyReader"
    >
      @for (g of groups(); track g.format) {
        <tr class="group-row">
          <th scope="rowgroup" [attr.colspan]="columns().length">
            <span class="format-name"><quader-format-icon [format]="g.format" /><span translate="no">{{ formatLabel(g.format) }}</span></span>
          </th>
        </tr>
        @for (row of g.rows; track row.index) {
          <tr>
            <th scope="row">
              <span class="icon-name-text">
                <span class="name" translate="no">{{ row.preset.displayName }}</span>
                <span class="secondary address" translate="no">{{ row.preset.url }}</span>
              </span>
            </th>
            <td class="hosts-cell">
              <!-- One inline box of the hosts, so that the first starts beside the column's name when the row is stacked (R-UI-174). -->
              @if (row.preset.extraHosts.length > 0) {
                <span class="host-list">
                  @for (h of row.preset.extraHosts; track h.pattern) {
                    <span class="host">
                      <span translate="no">{{ h.pattern }}</span>
                      @if (h.sendCredentials) {
                        &ngsp;<span class="secondary" i18n="@@presets.receives">receives the credential</span>
                      }
                    </span>
                  }
                </span>
              } @else {
                <quader-no-value />
              }
            </td>
            @if (canWrite()) {
              <!-- A writer's two actions are the row's menu (R-UI-163, Q-UI-640). -->
              <td class="actions">
                <button
                  class="btn btn-tertiary btn-icon"
                  type="button"
                  [disabled]="busy()"
                  [attr.aria-label]="actionsLabel(row.preset)"
                  [cdkMenuTriggerFor]="rowMenu"
                >
                  <span class="material-icons" aria-hidden="true">more_vert</span>
                </button>
                <ng-template #rowMenu>
                  <div class="menu" cdkMenu>
                    <button class="menu-item" cdkMenuItem type="button" (cdkMenuItemTriggered)="edit(row.index)" i18n="@@presets.edit">Edit</button>
                    <button class="menu-item" cdkMenuItem type="button" (cdkMenuItemTriggered)="remove(row.index)" i18n="@@presets.remove">Remove</button>
                  </div>
                </ng-template>
              </td>
            }
          </tr>
        }
      }
    </quader-data-table>
  `,
  styles: `
    :host {
      display: block;
    }
    .presets-heading {
      margin: var(--quader-space-8) 0 var(--quader-space-1);
      font-size: var(--quader-heading-3);
      font-weight: 600;
      line-height: 1.4;
    }
    .presets-hint {
      margin: 0 0 var(--quader-space-3);
      max-inline-size: var(--quader-settings-measure);
    }
    .presets-actions {
      margin: 0 0 var(--quader-space-4);
    }
    tr.group-row > th {
      padding-block-start: var(--quader-space-5);
      font-weight: 600;
    }
    tr.group-row .format-name {
      display: inline-flex;
      align-items: center;
      gap: var(--quader-space-2);
      margin-block: calc((1rem - 1.25rem) / 2);
      white-space: nowrap;
      vertical-align: top;
    }
    tr.group-row quader-format-icon {
      font-size: 1.25rem;
    }
    .address {
      overflow-wrap: anywhere;
    }
    .host-list {
      display: inline-flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      vertical-align: top;
      max-inline-size: 100%;
    }
    .host {
      overflow-wrap: anywhere;
    }
  `,
})
export class RegistryPresets {
  private readonly api = inject(UpstreamApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly confirm = inject(Confirm);
  private readonly notifier = inject(Notifier);

  /** The formats of the catalog, for the order of the groups, their names and the formats a preset can have. */
  readonly formats = input<readonly FormatDescriptor[]>([]);
  /** The caller holds `repositories:write-config`. */
  readonly canWrite = input(false);

  protected readonly heading = $localize`:@@presets.heading:Registries of this installation`;
  protected readonly emptyReader = $localize`:@@presets.empty:No registry of this installation yet.`;
  protected readonly emptyWriter = $localize`:@@presets.empty-writer:No registry of this installation yet. Add one to offer it as a button in every project.`;

  protected readonly list = signal<readonly RegistryPreset[] | null>(null);
  private etag = '';
  protected readonly loadError = signal<ApiError | null>(null);
  protected readonly actionError = signal<ApiError | null>(null);
  protected readonly busy = signal(false);

  /** The formats a preset can have: those with the remote class (R-REPO-076), by display name. */
  private readonly remoteFormats = computed<readonly SelectOption[]>(() =>
    this.formats()
      .filter((f) => f.classes.includes('remote'))
      .map((f) => ({ value: f.name, label: f.displayName ?? f.name })),
  );

  /** The presets grouped by format in catalog order (the formats' order), each in the administrator's order. */
  protected readonly groups = computed<readonly PresetGroup[]>(() => {
    const list = this.list() ?? [];
    const order = this.formats().map((f) => f.name);
    const rank = (format: string): number => {
      const i = order.indexOf(format);
      return i < 0 ? order.length : i;
    };
    const out: { format: string; rows: { preset: RegistryPreset; index: number }[] }[] = [];
    list.forEach((preset, index) => {
      const group = out.find((g) => g.format === preset.format);
      if (group) group.rows.push({ preset, index });
      else out.push({ format: preset.format, rows: [{ preset, index }] });
    });
    return out.sort((a, b) => rank(a.format) - rank(b.format));
  });

  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'registry', label: $localize`:@@wellknown.col.registry:Registry`, width: '16rem' },
    { id: 'hosts', label: $localize`:@@upstream.hosts:Further hosts` },
    ...(this.canWrite() ? [{ id: 'actions', label: $localize`:@@projects.col.actions:Actions`, hideLabel: true }] : []),
  ]);

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    this.loadError.set(null);
    try {
      const loaded = await this.api.registryPresets();
      this.list.set(loaded.presets);
      this.etag = loaded.etag;
    } catch (e) {
      this.loadError.set(asApiError(e));
    }
  }

  protected actionsLabel(p: RegistryPreset): string {
    return $localize`:@@presets.actions-of:Actions for ${p.displayName}:name:`;
  }

  protected formatLabel(name: string): string {
    return this.formats().find((f) => f.name === name)?.displayName ?? name;
  }

  protected add(): void {
    void this.open(null, (this.list() ?? []).length, (preset, list) => [...list, preset]);
  }

  protected edit(index: number): void {
    const list = this.list() ?? [];
    const current = list[index];
    if (!current) return;
    void this.open(current, index, (preset, now) => now.map((p, i) => (i === index ? preset : p)));
  }

  protected async remove(index: number): Promise<void> {
    const list = this.list() ?? [];
    const current = list[index];
    if (!current) return;
    const answer = await this.confirm.ask({
      title: $localize`:@@presets.remove.title:Remove ${current.displayName}:name:?`,
      lines: [$localize`:@@presets.remove.text:Remote repositories created from it keep their address and hosts.`],
      confirmLabel: $localize`:@@presets.remove:Remove`,
    });
    if (!answer) return;
    this.busy.set(true);
    this.actionError.set(null);
    try {
      const refused = await this.send(list.filter((_, i) => i !== index));
      if (refused) this.actionError.set(refused);
      else this.notifier.status($localize`:@@presets.removed:${current.displayName}:name: is removed.`);
    } finally {
      this.busy.set(false);
    }
  }

  /** Opens the dialog; next builds the list a save sends from the list as it is then, read again after a 412. */
  private async open(
    preset: RegistryPreset | null,
    index: number,
    next: (p: RegistryPreset, list: readonly RegistryPreset[]) => RegistryPreset[],
  ): Promise<void> {
    this.actionError.set(null);
    const data: RegistryPresetDialogData = {
      formats: this.remoteFormats(),
      preset,
      index,
      save: (p) => this.send(next(p, this.list() ?? [])),
    };
    const ref = this.dialog.open<RegistryPresetDialog, RegistryPresetDialogData, RegistryPreset>(RegistryPresetDialog, {
      data,
      maxWidth: '44rem',
      width: '44rem',
    });
    const saved = await firstValueFrom(ref.closed);
    if (saved) this.notifier.status($localize`:@@presets.saved:${saved.displayName}:name: is saved.`);
  }

  /**
   * Sends the whole list with the `ETag` it was read with. Null when it was stored; the refusal otherwise. A 412 means another screen
   * changed the list: it is read again, so the next save starts from what is stored (R-UI-042).
   */
  private async send(presets: readonly RegistryPreset[]): Promise<ApiError | null> {
    try {
      const saved = await this.api.setRegistryPresets(presets, this.etag);
      this.list.set(saved.presets);
      this.etag = saved.etag;
      return null;
    } catch (e) {
      const error = asApiError(e);
      if (error.status === 412) await this.load();
      return error;
    }
  }
}
