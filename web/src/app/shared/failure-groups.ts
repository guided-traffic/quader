import { Injectable, Signal, computed, signal } from '@angular/core';
import { ApiError } from '../api/problem';

/** A notice of a failure as the groups know it: its element, its failure, and how it reads its view again. */
export interface FailureMember {
  readonly id: number;
  readonly host: HTMLElement;
  readonly error: Signal<ApiError>;
  /** Reads the notice's view again (its `(retry)`), or null where the screen bound none. */
  readonly retry: (() => void) | null;
}

/**
 * The cause of a failure: its status, its code and the server's sentence, and whether it was the page's own or an answer that could
 * not be read. Two failures of one cause are one notice (Q-UI-1185); a 404 of one object and a 404 of another differ by the
 * server's sentence and stay two.
 */
export function causeOf(error: ApiError): string {
  const p = error.problem;
  return [error.status, p?.code ?? '', p?.detail ?? p?.title ?? '', error.answer.unreadable, error.answer.fault ?? ''].join('\u0000');
}

/** Where a notice stands: an overlay (a dialog, a drawer) is a place of its own, the page another. */
function placeOf(host: HTMLElement): Element | null {
  return host.closest('.cdk-overlay-pane') ?? host.closest('main') ?? null;
}

let nextId = 1;

/**
 * One cause, one notice (R-UI-216, Q-UI-1185): the notices shown at once that report the same cause in the same place (the page,
 * or one overlay) are a group. The first of a group in the document's order shows the failure and names every call of the group;
 * the others show nothing. Retry of the first reads every view of the group again. Every `quader-error-notice` joins by itself, so
 * every page that stacks notices gains it without a change of its own.
 */
@Injectable({ providedIn: 'root' })
export class FailureGroups {
  private readonly members = signal<readonly FailureMember[]>([]);

  /** A new member's identity. */
  newId(): number {
    return nextId++;
  }

  join(member: FailureMember): void {
    this.members.update((all) => [...all.filter((m) => m.id !== member.id), member]);
  }

  leave(id: number): void {
    this.members.update((all) => all.filter((m) => m.id !== id));
  }

  /** The group of a member in the document's order: itself alone while it has not joined. */
  groupOf(id: number): Signal<readonly FailureMember[]> {
    return computed(() => {
      const all = this.members();
      const me = all.find((m) => m.id === id);
      if (!me) return [];
      const cause = causeOf(me.error());
      const place = placeOf(me.host);
      return all
        .filter((m) => m.host.isConnected && placeOf(m.host) === place && causeOf(m.error()) === cause)
        .sort((a, b) => (a.host.compareDocumentPosition(b.host) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1));
    });
  }
}
