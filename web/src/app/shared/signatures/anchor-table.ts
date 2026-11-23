import { DecimalPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';
import { SignatureAnchor } from '../../api/signatures-api';
import { DataTable, TableColumn } from '../ui/data-table';
import { NoValue } from '../ui/no-value';
import { Switch } from '../ui/switch';
import { TimeAgo } from '../ui/time-ago';
import { anchorKindWords, anchorScopeWords, shortFingerprint } from './signature-words';

/** What a row of the table may offer: its switch, its removal, or nothing. */
export interface AnchorRowControls {
  readonly toggle: boolean;
  readonly remove: boolean;
}

/**
 * A list of trust anchors (R-SIG-015, R-SIG-036, R-SIG-037): per anchor its title with what identifies its material under it (the
 * subject or fingerprint of its first certificate or key, a Sigstore identity's issuer), its kind, where it comes from, the first
 * expiry of its material, the signatures it knows in the installation's scope, and whether it applies. The page decides per row what
 * it offers (`controls`): a shipped anchor only its switch, an administrator's its switch and **Remove**, an anchor of the Helm
 * values nothing; a project anchor that no longer applies says why. `project` names the project that owns each anchor instead of
 * where it comes from.
 *
 * One form at every width but a phone's (Q-UI-969, the rule of the lists of 2026-11-23): a table whose columns fit at 1,440 and
 * 1,024 px, the kind and the source (or the project) as a line under the anchor's name, the expiry as a relative time with the date
 * as its title (Q-UI-431); its rows stand as blocks only where even those columns do not fit (390 px).
 */
@Component({
  selector: 'quader-anchor-table',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DataTable, DecimalPipe, NoValue, Switch, TimeAgo],
  template: `
    <quader-data-table [caption]="caption()" [columns]="columns()" [empty]="anchors().length === 0" [emptyText]="emptyText()">
      @for (a of anchors(); track a.id) {
        @let c = controls()(a);
        <tr [class.off]="!a.applied">
          <th scope="row">
            <span class="icon-name-text">
              <span translate="no">{{ a.title }}</span>
              @if (identifies(a); as id) {
                <span class="secondary anchor-id" translate="no">{{ id }}</span>
              }
              <span class="secondary" data-testid="anchor-kind"
                >{{ kindWords(a.kind) }} ·
                @if (project()) {
                  <span translate="no">{{ a.project }}</span>
                } @else {
                  {{ scopeWords(a.scope) }}
                }
              </span>
              @if (a.scope === 'project' && !a.applied) {
                <span class="secondary" data-testid="anchor-not-applied">{{ notApplied }}</span>
              }
            </span>
          </th>
          <td>
            @if (expiry(a); as at) {
              <quader-time-ago [class.expired]="isPast(at)" [at]="at" absolute="title" />
              @if (isPast(at)) {
                <span class="secondary">{{ expiredWord }}</span>
              }
            } @else {
              <quader-no-value />
            }
          </td>
          <td class="num">{{ a.signatures | number }}</td>
          @if (showState()) {
            <td class="state-cell">
              @if (c.toggle) {
                <quader-switch [checked]="a.enabled" [label]="switchName(a)" [busy]="busy().has(a.id)" (checkedChange)="switched.emit({ anchor: a, on: $event })" />
              } @else if (a.scope !== 'project') {
                <span class="word">{{ a.enabled ? onWord : offWord }}</span>
              } @else {
                <span class="word">{{ a.applied ? appliedWord : offWord }}</span>
              }
            </td>
          }
          <!-- A row without Remove leaves its action cell empty, as every list does for a row with nothing to do: the en dash of a
               missing value made a column of dashes beside the shipped anchors. -->
          @if (anyRemove()) {
            <td class="actions">
              @if (c.remove) {
                <button class="btn btn-tertiary" type="button" [disabled]="busy().has(a.id)" (click)="remove.emit(a)">
                  <ng-container i18n="@@sig.anchor.remove">Remove</ng-container><span class="visually-hidden">&ngsp;{{ a.title }}</span>
                </button>
              }
            </td>
          }
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    .anchor-id {
      overflow-wrap: anywhere;
    }
    .icon-name-text > .secondary {
      display: block;
    }
    .expired {
      color: var(--quader-status-warn);
    }
    .word {
      color: var(--quader-text-muted);
    }
  `,
})
export class AnchorTable {
  readonly anchors = input.required<readonly SignatureAnchor[]>();
  readonly caption = input.required<string>();
  readonly emptyText = input('');
  /** What each row offers. */
  readonly controls = input<(a: SignatureAnchor) => AnchorRowControls>(() => ({ toggle: false, remove: false }));
  /** The column of the project instead of the column of where the anchor comes from. */
  readonly project = input(false);
  /** The column of whether it applies. */
  readonly showState = input(true);
  /** Rows whose change is being saved. */
  readonly busy = input<ReadonlySet<number>>(new Set());
  readonly switched = output<{ readonly anchor: SignatureAnchor; readonly on: boolean }>();
  readonly remove = output<SignatureAnchor>();

  protected readonly anyRemove = computed(() => this.anchors().some((a) => this.controls()(a).remove));
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'title', label: $localize`:@@sig.anchor.col.title:Trust anchor`, width: '10rem', prefer: true },
    { id: 'expiry', label: $localize`:@@sig.anchor.col.expiry:Expires` },
    {
      id: 'signatures',
      label: $localize`:@@sig.anchor.col.signatures:Signatures`,
      numeric: true,
      help: $localize`:@@sig.anchor.col.signatures-help:The signatures this anchor knows in the installation's scope.`,
    },
    ...(this.showState() ? [{ id: 'state', label: $localize`:@@sig.anchor.col.state:State` }] : []),
    ...(this.anyRemove() ? [{ id: 'actions', label: $localize`:@@sig.anchor.col.actions:Actions`, hideLabel: true }] : []),
  ]);

  protected readonly kindWords = anchorKindWords;
  protected readonly scopeWords = anchorScopeWords;
  protected readonly onWord = $localize`:@@switch.on:On`;
  protected readonly offWord = $localize`:@@switch.off:Off`;
  protected readonly expiredWord = $localize`:@@sig.anchor.expired:expired`;
  protected readonly appliedWord = $localize`:@@sig.anchor.applied:Applied`;
  protected readonly notApplied = $localize`:@@sig.anchor.not-applied:Not applied: project anchors are switched off by an administrator.`;

  /** What identifies an anchor's material: a Sigstore identity's issuer and pattern, else its first certificate's or key's subject or fingerprint. */
  protected identifies(a: SignatureAnchor): string {
    if (a.kind === 'sigstore' && a.sigstore) {
      if (!a.sigstore.issuer && a.sigstore.identityPattern === '.*') return $localize`:@@sig.anchor.any-identity:Any identity of any issuer`;
      const who = a.sigstore.identity ?? a.sigstore.identityPattern ?? '';
      return [a.sigstore.issuer, who].filter((v) => v !== '').join(' · ');
    }
    const first = a.fingerprints[0];
    if (!first) return a.key;
    const more = a.fingerprints.length > 1 ? ` +${a.fingerprints.length - 1}` : '';
    return `${first.subject ?? shortFingerprint(first.value)}${more}`;
  }

  /** The first expiry of the anchor's material. */
  protected expiry(a: SignatureAnchor): string | null {
    const times = a.fingerprints.map((f) => f.notAfter).filter((t): t is string => !!t);
    return times.length === 0 ? null : times.reduce((x, y) => (Date.parse(x) <= Date.parse(y) ? x : y));
  }

  protected isPast(at: string): boolean {
    return Date.parse(at) < Date.now();
  }

  protected switchName(a: SignatureAnchor): string {
    const title = a.title;
    return $localize`:@@sig.anchor.switch:Apply ${title}:title:`;
  }
}
