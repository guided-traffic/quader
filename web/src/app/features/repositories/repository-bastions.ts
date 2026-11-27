import { Component, computed, input, model } from '@angular/core';
import { ProjectBastion } from '../../api/projects-api';
import { Checkbox } from '../../shared/ui/checkbox';
import { Help } from '../../shared/ui/help';
import { DistributingBastion, bastionHosts } from '../../shared/ui/distribution-mark';
import { VisibilityLevel } from '../../shared/ui/visibility-level';

/** A Bastion of the group as one row: its box, its hosts and the lines its choice makes visible. */
interface Row {
  readonly name: string;
  readonly hosts: string;
  readonly disabled: boolean;
  readonly checked: boolean;
  /** A remote's line: its cache's packages are listed by the Bastion's web interface. */
  readonly remoteLine: boolean;
  /** A virtual's members the Bastion does not distribute, which take no part through it (bastion.md R-BAS-134). */
  readonly missing: string;
  readonly noteId: string;
}

let nextGroupId = 1;

/**
 * The group **Bastions** of a repository's section Visibility (R-UI-055, R-UI-271): one box per Bastion an administrator allowed for
 * the project, its host in grey, for the people who choose (the project's Owner, a holder of `repositories:write-config`, an
 * administrator on a repository of `upstream`). What a checked Bastion makes leave the network stands visible under its box (W-39's
 * D2): for a remote that is Public while that Bastion's web interface is on, that the web interface lists what the builds fetched; for a
 * virtual, the members the Bastion does not distribute. A reader who does not choose sees the names as one sentence, and nothing while
 * the repository is distributed nowhere. The section renders no group at all while no Bastion allows the project (simple by default).
 */
@Component({
  selector: 'quader-repository-bastions',
  imports: [Checkbox, Help],
  template: `
    @if (rows().length > 0) {
      <fieldset class="bastions" [attr.aria-describedby]="error() ? errorId : null" data-testid="repository-bastions">
        <legend class="field-label"
          ><span class="help-anchor">{{ label }}</span><quader-help [text]="help" [about]="label"
        /></legend>
        @for (row of rows(); track row.name) {
          <quader-checkbox
            class="bastion"
            [checked]="row.checked"
            [disabled]="disabled()"
            [describedBy]="row.remoteLine || row.missing ? row.noteId : null"
            (checkedChange)="toggle(row.name, $event)"
            ><span translate="no">{{ row.name }}</span
            >@if (row.hosts) {<span class="host" translate="no">{{ row.hosts }}</span>}@if (row.disabled) {<span class="host">{{
              disabledNow
            }}</span>}</quader-checkbox
          >
          @if (row.checked && row.remoteLine) {
            <p class="muted note" [id]="row.noteId" i18n="@@repo.bastions.remote">
              Its web interface also lists every package your builds fetched through this remote.
            </p>
          } @else if (row.checked && row.missing) {
            <p class="note warning" [id]="row.noteId">
              <span class="material-icons inline warn" aria-hidden="true">warning</span
              ><span i18n="@@repo.bastions.members"
                >Through {{ row.name }} it serves only members distributed there too; not: {{ row.missing }}.</span
              >
            </p>
          }
        }
        @if (error(); as message) {
          <p class="field-error" role="alert" [id]="errorId">{{ message }}</p>
        }
      </fieldset>
    } @else if (readOnlyText(); as text) {
      <p class="muted read-only" data-testid="repository-bastions">{{ text }}</p>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    .bastions {
      min-inline-size: 0;
      margin: var(--quader-space-3) 0 0;
      padding: 0;
      border: 0;
    }
    legend {
      padding: 0;
      margin-block-end: var(--quader-space-2);
    }
    .bastion + .bastion,
    .note + .bastion {
      margin-block-start: var(--quader-space-2);
    }
    /* The host after the name in the muted colour, as the Bastion's card names it (W-41's D2). */
    .host {
      margin-inline-start: var(--quader-space-2);
      color: var(--quader-text-muted);
      overflow-wrap: anywhere;
    }
    /* A line under its box starts where the box's label starts. */
    .note {
      margin: var(--quader-space-1) 0 0 calc(1rem + var(--quader-space-2));
      font-size: var(--quader-text-muted-size);
    }
    /* The warning sign beside its sentence, the sentence's lines under each other, as a notice's. */
    .warning {
      display: flex;
      align-items: flex-start;
    }
    .muted {
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
    }
    .read-only {
      margin: var(--quader-space-3) 0 0;
    }
    .field-error {
      margin-block: var(--quader-space-2) 0;
    }
  `,
})
export class RepositoryBastions {
  /** The Bastions that allow the project; `null` while unknown or not readable by the caller (then the names are a sentence). */
  readonly offered = input<readonly ProjectBastion[] | null>(null);
  /** The Bastions that distribute the repository now (the stored member). */
  readonly stored = input<readonly DistributingBastion[]>([]);
  /** The names chosen in the form. */
  readonly chosen = model.required<readonly string[]>();
  /** The caller chooses: the boxes; otherwise the sentence. */
  readonly canChoose = input(false);
  readonly disabled = input(false);
  readonly repositoryClass = input<'local' | 'remote' | 'virtual'>('local');
  /** The level the form has for the artifacts: a remote's line stands only while it is Public. */
  readonly level = input<VisibilityLevel>('private');
  /** The members of a virtual each Bastion does not distribute, by the Bastion's name. */
  readonly missingMembers = input<ReadonlyMap<string, readonly string[]>>(new Map());
  /** What the server answered for the member (`bastion_not_allowed` at `/bastions/<i>`). */
  readonly error = input<string | null>(null);

  protected readonly label = $localize`:@@repo.bastions.label:Bastions`;
  protected readonly help = $localize`:@@repo.bastions.help:Also serve this repository through a Bastion, outside your network. Who may read it there is decided by its visibility, as here. An administrator decides which Bastions this project may use.`;
  protected readonly disabledNow = $localize`:@@distribution.disabled-now:(disabled now)`;
  private readonly group = `quader-repository-bastions-${nextGroupId++}`;
  protected readonly errorId = `${this.group}-error`;

  protected readonly rows = computed<readonly Row[]>(() => {
    const offered = this.offered();
    if (!this.canChoose() || !offered || offered.length === 0) return [];
    const chosen = new Set(this.chosen());
    return offered.map((b, i) => ({
      name: b.name,
      hosts: bastionHosts(b.addresses).join(', '),
      disabled: b.disabled,
      checked: chosen.has(b.name),
      remoteLine: this.repositoryClass() === 'remote' && this.level() === 'public' && b.webInterface,
      missing: this.repositoryClass() === 'virtual' ? (this.missingMembers().get(b.name) ?? []).join(', ') : '',
      noteId: `${this.group}-${i}`,
    }));
  });

  /** The sentence of a reader who does not choose: the Bastions that distribute the repository; empty for none. */
  protected readonly readOnlyText = computed(() => {
    const names = this.stored().map((b) => b.name);
    if (names.length === 0) return '';
    const list = names.join(', ');
    return $localize`:@@repo.bastions.read-only:Distributed through ${list}:names:.`;
  });

  protected toggle(name: string, on: boolean): void {
    const set = new Set(this.chosen());
    if (on) set.add(name);
    else set.delete(name);
    this.chosen.set([...set].sort());
  }
}
