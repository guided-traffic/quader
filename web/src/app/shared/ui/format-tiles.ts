import { Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { FormatDescriptor } from '../../api/repositories-api';
import { FormatIconView } from '../format-icon';

/**
 * The package formats as tiles, each a link (R-UI-127): the first page of a new repository (R-UI-080) and the navigation entry
 * **Guides** (R-UI-124) show the same tiles, so a format looks the same wherever it is chosen. The screen decides where a tile
 * leads (`linkOf`) and which formats it offers; the tile is the format's icon and its display name.
 */
@Component({
  selector: 'quader-format-tiles',
  imports: [RouterLink, FormatIconView],
  template: `
    <ul class="tiles" [attr.aria-label]="label()">
      @for (f of formats(); track f.name) {
        <li>
          <a class="tile" [routerLink]="linkOf()(f)" [queryParams]="queryParams()">
            <quader-format-icon class="tile-icon" [format]="f.name" />
            <span class="tile-name" translate="no">{{ f.displayName ?? f.name }}</span>
          </a>
        </li>
      }
    </ul>
  `,
  styles: `
    :host {
      display: block;
    }
    .tiles {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(9.5rem, 1fr));
      gap: var(--quader-space-3);
      max-inline-size: 56rem;
      margin: var(--quader-space-3) 0 0;
      padding: 0;
      list-style: none;
    }
    .tile {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: var(--quader-space-2);
      block-size: 100%;
      box-sizing: border-box;
      padding: var(--quader-space-4) var(--quader-space-2);
      border: 1px solid var(--quader-border-strong);
      border-radius: var(--quader-radius-l);
      background: var(--quader-surface-raised);
      color: var(--quader-on-surface);
      text-align: center;
      text-decoration: none;
    }
    .tile:hover {
      background: var(--quader-row-hover);
    }
    .tile:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
    .tile-icon {
      font-size: 2rem;
    }
    .tile-name {
      font-weight: 600;
      overflow-wrap: anywhere;
    }
  `,
})
export class FormatTiles {
  readonly formats = input.required<readonly FormatDescriptor[]>();
  /** Where the tile of a format leads: a path of the application. */
  readonly linkOf = input.required<(f: FormatDescriptor) => string>();
  readonly queryParams = input<Record<string, string>>({});
  /** The accessible name of the list. */
  readonly label = input.required<string>();
}
