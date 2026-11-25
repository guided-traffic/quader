import { Injectable, Component, inject, input } from '@angular/core';
import { DIALOG_DATA, DialogClose, DialogTitle, QuaderDialog } from './ui/dialog';
import { firstValueFrom } from 'rxjs';
import { Versioned } from '../api/repositories-api';
import { ApiError } from '../api/problem';
import { NoValue } from './ui/no-value';

export type ConflictDecision = 'reload' | 'overwrite';

/**
 * How the form names a field and shows its value (R-UI-042, Q-UI-1210): its label, and the value as the form shows it. Without `show` a
 * list stands as its entries, a switch as On or Off, an empty value as none and any other value as its text.
 */
export interface ConflictField {
  /** The form's label of the field. */
  readonly label: string;
  /** The value as the form shows it: one text, or the entries of a list. An empty answer is none. */
  readonly show?: (value: unknown) => string | readonly string[];
  /** A secret: its value never stands in the dialog, only that one is set. */
  readonly secret?: boolean;
  /** What the form shows for an empty value where that is a choice of its own ("Follow the browser"); left out, none. */
  readonly empty?: string;
}

/**
 * The form's fields by their JSON pointer (`/includePatterns`, `/remote/url`), as a table or as a function for a form whose pointers
 * follow a rule (a format's member, a mirror's settings); a string is the label alone. A pointer the form does not name keeps its API
 * name, the server's text; an object the form does not name is compared member by member.
 */
export type ConflictFields =
  | Readonly<Record<string, ConflictField | string>>
  | ((pointer: string) => ConflictField | string | null | undefined);

export interface ConflictData {
  /** The user's draft. */
  readonly mine: Readonly<Record<string, unknown>>;
  /** The resource as stored now (re-read after the 412). */
  readonly theirs: Readonly<Record<string, unknown>>;
  /** How the form names and shows its fields; left out, every field keeps its API name. */
  readonly fields?: ConflictFields;
}

/** A value as the dialog shows it: the entries of a list, one entry for any other value, none for an empty one. */
export interface ShownValue {
  readonly entries: readonly string[];
}

export interface ConflictRow {
  /** The field's JSON pointer in the compared documents. */
  readonly pointer: string;
  /** The form's label, or the field's API name (the pointer without its first slash) where the form does not show the field. */
  readonly field: string;
  /** True where the form does not name the field: its API name stands, as the server's text. */
  readonly apiName: boolean;
  readonly mine: ShownValue;
  readonly theirs: ShownValue;
}

/** Member names that hold a secret wherever they stand: their values never reach the dialog, whatever the form says. */
const SECRET_MEMBERS = new Set(['password', 'secret', 'token', 'clientSecret', 'privateKey', 'secretAccessKey']);

function isPlainObject(v: unknown): v is Readonly<Record<string, unknown>> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/** Empty in the eyes of a reader: no value, an empty text, an empty list or an object without members. */
function isEmpty(v: unknown): boolean {
  if (v === undefined || v === null || v === '') return true;
  if (Array.isArray(v)) return v.length === 0;
  return isPlainObject(v) && Object.keys(v).length === 0;
}

function describer(fields: ConflictFields | undefined): (pointer: string) => ConflictField | null {
  if (!fields) return () => null;
  const look = typeof fields === 'function' ? fields : (p: string) => fields[p];
  return (pointer) => {
    const f = look(pointer);
    if (f === null || f === undefined) return null;
    return typeof f === 'string' ? { label: f } : f;
  };
}

/** Escapes a member name as a JSON pointer's segment (RFC 6901). */
function segment(name: string): string {
  return name.replace(/~/g, '~0').replace(/\//g, '~1');
}

/** The fields of a document by pointer: a member the form names, or a value that is no object, is one field; an object is its members. */
function leaves(
  value: unknown,
  pointer: string,
  describe: (pointer: string) => ConflictField | null,
  out: Map<string, unknown>,
): void {
  if (pointer !== '' && describe(pointer)) {
    out.set(pointer, value);
    return;
  }
  if (isPlainObject(value)) {
    for (const [name, member] of Object.entries(value)) leaves(member, `${pointer}/${segment(name)}`, describe, out);
    return;
  }
  if (pointer !== '') out.set(pointer, value);
}

/** One value of a list, or of an object the form does not name, as text. */
function entryText(v: unknown): string {
  if (typeof v === 'string') return v;
  if (typeof v === 'boolean') return v ? $localize`:@@switch.on:On` : $localize`:@@switch.off:Off`;
  if (typeof v === 'number' || typeof v === 'bigint') return String(v);
  if (isPlainObject(v)) {
    return Object.entries(v)
      .filter(([, m]) => !isEmpty(m))
      .map(([name, m]) => `${name}: ${Array.isArray(m) ? m.map(entryText).join(', ') : entryText(m)}`)
      .join(', ');
  }
  if (Array.isArray(v)) return v.map(entryText).join(', ');
  return String(v);
}

/** The value of the field at `pointer` as the form shows it (Q-UI-1210). */
export function shownValue(value: unknown, pointer: string, field: ConflictField | null): ShownValue {
  if (isEmpty(value)) return { entries: field?.empty ? [field.empty] : [] };
  const secret = field?.secret ?? pointer.split('/').some((s) => SECRET_MEMBERS.has(s));
  if (secret) return { entries: [$localize`:@@conflict.secret:Hidden`] };
  if (field?.show) {
    const shown = field.show(value);
    const entries = typeof shown === 'string' ? [shown] : [...shown];
    return { entries: entries.filter((e) => e !== '') };
  }
  if (Array.isArray(value)) return { entries: value.map(entryText) };
  return { entries: [entryText(value)] };
}

/**
 * The fields whose values differ (R-UI-042, Q-UI-1210), in the order of the draft then the stored object, each named by the form's
 * label and shown as the form shows it. An object the form does not name is compared member by member; two empty values (none, an
 * empty text, an empty list) do not differ.
 */
export function diffFields(data: ConflictData): ConflictRow[] {
  const describe = describer(data.fields);
  const mine = new Map<string, unknown>();
  const theirs = new Map<string, unknown>();
  leaves(data.mine, '', describe, mine);
  leaves(data.theirs, '', describe, theirs);
  const pointers = [...new Set([...mine.keys(), ...theirs.keys()])];
  const same = (a: unknown, b: unknown): boolean => (isEmpty(a) && isEmpty(b)) || JSON.stringify(a) === JSON.stringify(b);
  return pointers
    .filter((p) => !same(mine.get(p), theirs.get(p)))
    .map((pointer) => {
      const field = describe(pointer);
      return {
        pointer,
        field: field?.label ?? pointer.slice(1),
        apiName: field === null,
        mine: shownValue(mine.get(pointer), pointer, field),
        theirs: shownValue(theirs.get(pointer), pointer, field),
      };
    });
}

/** One value of a compared field (Q-UI-1210): the en dash for none (Q-UI-561), one text, or the entries of a list one per line. */
@Component({
  selector: 'quader-compare-value',
  imports: [NoValue],
  template: `
    @switch (value().entries.length) {
      @case (0) {
        <quader-no-value />
      }
      @case (1) {
        <span translate="no">{{ value().entries[0] }}</span>
      }
      @default {
        <ul class="compare-entries" translate="no">
          @for (e of value().entries; track $index) {
            <li>{{ e }}</li>
          }
        </ul>
      }
    }
  `,
})
export class CompareValue {
  readonly value = input.required<ShownValue>();
}

/** The name of a compared field (Q-UI-1210): the form's label, or the field's API name as the server's text where the form has none. */
@Component({
  selector: 'quader-compare-field',
  template: `
    @if (row().apiName) {
      <code translate="no">{{ row().field }}</code>
    } @else {
      {{ row().field }}
    }
  `,
})
export class CompareField {
  readonly row = input.required<ConflictRow>();
}

/** "Changed by someone else since you opened it" (R-UI-042): their version against yours. */
@Component({
  selector: 'quader-conflict-dialog',
  imports: [DialogTitle, DialogClose, CompareField, CompareValue],
  template: `
    <h2 quaderDialogTitle i18n="@@conflict.title">Changed by someone else since you opened it</h2>
    <div class="dialog-body">
      @if (rows.length === 0) {
        <p i18n="@@conflict.same">The stored version already equals yours.</p>
      } @else {
        <!-- The comparison's layout is the dialogs' (_dialog.scss, .compare): one line per field, or a block per field at 390 px. -->
        <div class="compare-region">
          <table class="compare">
            <caption class="visually-hidden" i18n="@@conflict.caption">
              Fields that differ between the stored version and your version
            </caption>
            <thead>
              <tr>
                <th scope="col" i18n="@@conflict.field">Field</th>
                <th scope="col" i18n="@@conflict.theirs">Stored now</th>
                <th scope="col" i18n="@@conflict.mine">Yours</th>
              </tr>
            </thead>
            <tbody>
              @for (row of rows; track row.pointer) {
                <tr>
                  <th scope="row"><quader-compare-field [row]="row" /></th>
                  <td data-label="Stored now" i18n-data-label="@@conflict.theirs"><quader-compare-value [value]="row.theirs" /></td>
                  <td data-label="Yours" i18n-data-label="@@conflict.mine"><quader-compare-value [value]="row.mine" /></td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      }
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" (click)="copyMine()" i18n="@@conflict.copy">
        Copy my changes
      </button>
      <button class="btn btn-tertiary" type="button" [quaderDialogClose]="'reload'" i18n="@@conflict.reload">
        Reload (discard mine)
      </button>
      <button class="btn btn-confirm"
        type="button"
        [quaderDialogClose]="'overwrite'"
        i18n="@@conflict.overwrite"
      >
        Overwrite
      </button>
    </div>
  `,
  styles: `
    .visually-hidden {
      position: absolute;
      inline-size: 1px;
      block-size: 1px;
      overflow: hidden;
      clip-path: inset(50%);
    }
  `,
})
export class ConflictDialog {
  private readonly data = inject<ConflictData>(DIALOG_DATA);
  protected readonly rows = diffFields(this.data);

  protected async copyMine(): Promise<void> {
    await navigator.clipboard.writeText(JSON.stringify(this.data.mine, null, 2));
  }
}

/** A merge-patch write guarded by the loaded `ETag`, with the way to re-read the resource. */
export interface GuardedPatch<T> {
  /** The `ETag` the draft was loaded with. */
  readonly etag: string;
  /** The members the user changed (the merge patch); only these are compared in the dialog. */
  readonly patch: Readonly<Record<string, unknown>>;
  /** How the form names and shows the members of `patch` in the dialog (Q-UI-1210). */
  readonly fields?: ConflictFields;
  /**
   * Sends the write with the given `ETag`. `current` is null on the first attempt. After "Overwrite" it is
   * the stored resource read now: a write that replaces the whole resource (PUT) must build its body from
   * `current` plus the members of `patch`, so that a change another administrator made to a member the
   * user did not touch is kept and not undone unseen (R-UI-042, R-CI-002). A merge-patch write ignores it.
   */
  send(etag: string, current: T | null): Promise<Versioned<T>>;
  /** Reads the stored resource again. */
  reload(): Promise<Versioned<T>>;
}

/**
 * `saved`: the write went through (first try, after "Overwrite", or because a retried first
 * attempt had committed); `reloaded`: the user discarded the draft for the stored version;
 * `dismissed`: the dialog was closed without a choice.
 */
export type GuardedOutcome<T> =
  | { readonly outcome: 'saved' | 'reloaded'; readonly current: Versioned<T> }
  | { readonly outcome: 'dismissed' };

/** Opens the conflict dialog; resolves with the choice, or null when it was dismissed. */
@Injectable({ providedIn: 'root' })
export class ConflictResolver {
  private readonly dialog = inject(QuaderDialog);

  async resolve(data: ConflictData): Promise<ConflictDecision | null> {
    const ref = this.dialog.open<ConflictDialog, ConflictData, ConflictDecision>(ConflictDialog, {
      data,
      maxWidth: '48rem',
      width: '48rem',
    });
    return (await firstValueFrom(ref.closed)) ?? null;
  }

  /**
   * Sends a merge patch with the loaded `ETag` (R-API-012). A 412 reads the stored resource and
   * opens the dialog (R-UI-042): "Overwrite" sends again with the tag read now and the stored resource. Every
   * other failure is thrown for the caller's error notice.
   */
  async save<T>(write: GuardedPatch<T>): Promise<GuardedOutcome<T>> {
    try {
      return { outcome: 'saved', current: await write.send(write.etag, null) };
    } catch (e) {
      if (!(e instanceof ApiError) || e.status !== 412) throw e;
      const theirs = await write.reload();
      const stored = theirs.value as Readonly<Record<string, unknown>>;
      const mine = write.patch;
      const theirPart = Object.fromEntries(Object.keys(mine).map((k) => [k, stored[k] ?? null]));
      if (e.afterRetry && JSON.stringify(theirPart) === JSON.stringify(mine)) {
        // The first attempt of a retried write did commit: that is a success, not a conflict.
        return { outcome: 'saved', current: theirs };
      }
      const decision = await this.resolve({ mine, theirs: theirPart, fields: write.fields });
      if (decision === 'overwrite') return { outcome: 'saved', current: await write.send(theirs.etag, theirs.value) };
      if (decision === 'reload') return { outcome: 'reloaded', current: theirs };
      return { outcome: 'dismissed' };
    }
  }
}
