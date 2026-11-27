import { Injectable, inject } from '@angular/core';
import { Bastion } from '../../api/bastions-api';
import { DownloadLinkConnection } from '../../api/download-links-api';
import { BrowserStorage } from '../../core/storage';
import { SessionStore } from '../../core/session-store';
import { handoverLines } from './bastion-handover';
import { catalogueNote, contactHealth, edgeBelowFigures, tokenHealth } from './bastion-state';
import { unlistedContacts } from './egress';

/**
 * Whether a Bastion's card shows a warning anywhere (R-UI-240, Q-UI-1640): the state **Contact lost** or **Disable not confirmed**, a
 * warning line of a main setting (the web interface builds nothing, its catalogue does not survive a restart, a connection's files still
 * come through the main installation), of **Limits** (the edge, the networks the Bastion connects from), of **Replicas** or of
 * **Tokens**. The same rules as the lines themselves, so a card that warns is never folded away.
 */
export function cardWarns(b: Bastion, now: number, connections: readonly DownloadLinkConnection[] | null): boolean {
  if (b.state === 'contactLost' || b.state === 'disableUnconfirmed') return true;
  const note = catalogueNote(b)?.kind;
  if (note === 'refused' || note === 'unlisted' || note === 'no-url') return true;
  if (b.switches.gui && b.contacts.some((c) => c.catalogueKept === false)) return true;
  if (b.switches.handover && connections && handoverLines(connections).length > 0) return true;
  if (edgeBelowFigures(b) !== null) return true;
  const unlisted = unlistedContacts(b).length;
  if (unlisted > 0 || (b.egress.length === 0 && b.contacts.length > 0)) return true;
  const contacts = contactHealth(b, now);
  if (contacts.kind === 'silent' && contacts.warn) return true;
  if (contacts.kind === 'current' && (contacts.releases.length > 1 || contacts.silentDeployments.length > 0)) return true;
  const tokens = tokenHealth(b, now).kind;
  return tokens === 'expired' || tokens === 'soon';
}

/**
 * Which Bastion cards the viewer folded or opened (R-UI-240, Q-UI-1640): one entry per Bastion, true for open and false for folded, in
 * the browser's local storage under `quader.admin.bastions.open.<principal>`, so two people on one browser keep their own. Without an
 * entry a card follows the page's rule (open where the page holds one Bastion, folded where it holds several, open while it warns). A
 * convenience of one viewer: blocked or cleared storage leaves every card to the rule.
 */
@Injectable({ providedIn: 'root' })
export class BastionFold {
  private readonly storage = inject(BrowserStorage);
  private readonly session = inject(SessionStore);

  private key(): string | null {
    const name = this.session.whoami()?.principal?.name;
    return name ? `quader.admin.bastions.open.${name}` : null;
  }

  private read(): Record<string, boolean> {
    const key = this.key();
    if (!key) return {};
    try {
      const parsed: unknown = JSON.parse(this.storage.get('local', key) ?? '{}');
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
      return Object.fromEntries(Object.entries(parsed).filter((e): e is [string, boolean] => typeof e[1] === 'boolean'));
    } catch {
      return {};
    }
  }

  /** The viewer's choice for the card of `name`; undefined where the viewer made none. */
  choice(name: string): boolean | undefined {
    return this.read()[name];
  }

  /** The viewer opened or folded the card of `name`. */
  set(name: string, open: boolean): void {
    const key = this.key();
    if (!key) return;
    this.storage.set('local', key, JSON.stringify({ ...this.read(), [name]: open }));
  }
}
