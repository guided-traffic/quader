import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { RouterLink } from '@angular/router';
import { QuaderAvatar } from '../../shared/ui/avatar';
import { CallerView } from './callers';

/**
 * A caller as the first column of the rule table, the usage overview and the blocks shows it (R-UI-179, R-UI-183): the avatar of
 * a user or the kind's icon in the avatar's round place, the name at weight 600 (a link where the row opens the caller), and the
 * second line muted. An address is in the monospace type; a "who" that was deleted says so on its second line.
 */
@Component({
  selector: 'quader-caller-cell',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [QuaderAvatar, RouterLink],
  template: `
    @let v = view();
    @if (v.icon === 'avatar') {
      <quader-avatar class="avatar picture" aria-hidden="true" [seed]="v.seed ?? v.primary" />
    } @else {
      <span class="avatar picture kind" aria-hidden="true"><span class="material-icons">{{ v.icon }}</span></span>
    }
    <span class="who">
      <span class="line">
        @if (link(); as commands) {
          <a class="name" [class.mono]="v.mono" [routerLink]="commands" [attr.translate]="v.translate ? null : 'no'">{{ v.primary }}</a>
        } @else if (actionable()) {
          <button type="button" class="name open link" [class.mono]="v.mono" [attr.translate]="v.translate ? null : 'no'" (click)="activate.emit()">
            {{ v.primary }}
          </button>
        } @else {
          <span class="name" [class.mono]="v.mono" [class.gone]="v.deleted" [attr.translate]="v.translate ? null : 'no'">{{ v.primary }}</span>
        }
        <ng-content />
      </span>
      @if (v.secondary) {
        <span class="secondary" [attr.translate]="v.deleted ? null : 'no'">{{ v.secondary }}</span>
      }
    </span>
  `,
  styles: `
    /* The picture is centred on the name and its second line, which every caller has (R-UI-175's picture beside two lines). */
    :host {
      display: flex;
      align-items: center;
      gap: var(--quader-space-3);
      min-inline-size: 0;
    }
    .avatar {
      flex: none;
      inline-size: 2rem;
      block-size: 2rem;
      border-radius: 50%;
    }
    .kind {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      background: var(--quader-badge-neutral-bg);
      color: var(--quader-badge-neutral-fg);
    }
    .kind .material-icons {
      font-size: 1.25rem;
    }
    .who {
      display: flex;
      flex-direction: column;
      min-inline-size: 0;
    }
    /* A badge of the caller (Built-in, Blocked) stands on the name's line (R-UI-175). */
    .line {
      display: flex;
      flex-wrap: wrap;
      align-items: flex-start;
      gap: var(--quader-space-1) var(--quader-space-2);
    }
    .name {
      font-weight: 600;
      overflow-wrap: anywhere;
    }
    /* The name opens the rule where the row is one's to change (R-UI-163: opening the thing is its name). */
    .open {
      padding: 0;
      border: 0;
      background: none;
      color: var(--quader-link);
      font: inherit;
      font-weight: 600;
      text-align: start;
      cursor: pointer;
    }
    .open:hover {
      text-decoration: underline;
    }
    .open:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
    .mono {
      font-family: var(--quader-font-mono);
      font-size: 0.875rem;
    }
    .gone {
      color: var(--quader-text-muted);
    }
    .secondary {
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
      overflow-wrap: anywhere;
    }
  `,
})
export class CallerCell {
  readonly view = input.required<CallerView>();
  /** Router commands of the caller's page; none shows the name as text. */
  readonly link = input<readonly unknown[] | null>(null);
  /** The name is a button that emits `activate` (a rule the caller may change). */
  readonly actionable = input(false);
  readonly activate = output<void>();
}
