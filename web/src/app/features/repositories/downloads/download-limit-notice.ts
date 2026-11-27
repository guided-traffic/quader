import { Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { RemoteDownloadLimit } from '../../../api/remote-downloads-api';
import { TechnicalDetails } from '../../../shared/technical-details';
import { clockText } from '../../../shared/upstream-downloads/limit-words';

/** The headers of a rate-limit answer in their names' order (Q-CUR-174). */
export function headerEntries(h: Readonly<Record<string, string>>): readonly { readonly name: string; readonly value: string }[] {
  return Object.entries(h)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([name, value]) => ({ name, value }));
}

/**
 * The notice above the tab Downloads while an upstream's rate limit holds the queue (R-CUR-011, Q-UI-1100): the sentence, the offer of
 * an account (Q-UI-1160) and the upstream's answer folded under them. A component of its own so that the tab's stylesheet stays under
 * the budget of a component's styles.
 */
@Component({
  selector: 'quader-download-limit-notice',
  imports: [RouterLink, TechnicalDetails],
  templateUrl: './download-limit-notice.html',
  styleUrl: './download-limit-notice.scss',
})
export class DownloadLimitNotice {
  readonly limit = input.required<RemoteDownloadLimit>();
  readonly sentence = input.required<string>();
  /** The sentence that offers an account; null where an account would not lift the limit. */
  readonly offer = input<string | null>(null);
  /** The page where the administrator of a well-known repository adds its account; null for any other reader. */
  readonly accountLink = input<readonly string[] | null>(null);

  /** A time of day of the limit's facts, with the day where it is not today. */
  protected clock(iso: string): string {
    return clockText(iso);
  }

  protected headerList(h: Readonly<Record<string, string>>): readonly { readonly name: string; readonly value: string }[] {
    return headerEntries(h);
  }
}
