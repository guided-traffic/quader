import { Injectable, signal } from '@angular/core';

export interface NoticeAction {
  readonly label: string;
  readonly run: () => void;
}

export interface Notice {
  readonly id: number;
  /** `status` is announced politely, `alert` immediately (R-UI-042). */
  readonly kind: 'status' | 'alert';
  readonly text: string;
  readonly action?: NoticeAction;
}

/** Time a confirmation without an action stays on screen. */
export const STATUS_LIFETIME_MS = 8000;

/** The least time a confirmation stays once the pointer or the focus has left it, as the reference's toast keeps it. */
export const STATUS_RESUME_MIN_MS = 1000;

interface Expiry {
  timer: ReturnType<typeof setTimeout> | null;
  /** Time left while paused. */
  left: number;
  /** When it expires while running (ms since the epoch). */
  until: number;
}

/**
 * Feedback outside forms (R-UI-042): confirmations through a polite live region, errors through an alert region, shown as toasts
 * ([notices.ts](../shared/notices.ts), Q-UI-1063). One confirmation is shown at a time: a new one replaces the confirmation still on
 * screen, so a row of switches does not stack a toast per switch; the region announces each. Notices with an action and errors keep
 * their own place until dismissed. A confirmation's time stops while the pointer or the focus is on it.
 */
@Injectable({ providedIn: 'root' })
export class Notifier {
  private nextId = 1;
  private readonly _notices = signal<readonly Notice[]>([]);
  private readonly expiries = new Map<number, Expiry>();

  readonly notices = this._notices.asReadonly();

  status(text: string, action?: NoticeAction): number {
    if (!action) {
      for (const n of this._notices()) if (n.kind === 'status' && !n.action) this.dismiss(n.id);
    }
    const id = this.add({ kind: 'status', text, action });
    if (!action) this.expire(id, STATUS_LIFETIME_MS);
    return id;
  }

  alert(text: string, action?: NoticeAction): number {
    return this.add({ kind: 'alert', text, action });
  }

  dismiss(id: number): void {
    const e = this.expiries.get(id);
    if (e?.timer) clearTimeout(e.timer);
    this.expiries.delete(id);
    this._notices.update((list) => list.filter((n) => n.id !== id));
  }

  /** Stops a confirmation's time while the pointer or the focus is on it; a notice that does not expire is left alone. */
  pause(id: number): void {
    const e = this.expiries.get(id);
    if (!e?.timer) return;
    clearTimeout(e.timer);
    e.timer = null;
    e.left = Math.max(e.until - Date.now(), STATUS_RESUME_MIN_MS);
  }

  /** Starts a paused confirmation's time again with what was left of it. */
  resume(id: number): void {
    const e = this.expiries.get(id);
    if (!e || e.timer) return;
    e.until = Date.now() + e.left;
    e.timer = setTimeout(() => this.dismiss(id), e.left);
  }

  private expire(id: number, ms: number): void {
    this.expiries.set(id, { timer: setTimeout(() => this.dismiss(id), ms), left: ms, until: Date.now() + ms });
  }

  private add(partial: Omit<Notice, 'id'>): number {
    const id = this.nextId++;
    this._notices.update((list) => [...list, { ...partial, id }]);
    return id;
  }
}
