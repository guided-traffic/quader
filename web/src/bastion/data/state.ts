import { Injectable, computed, inject, signal } from '@angular/core';
import { ApiError } from '../../app/api/problem';
import { LegalStore } from '../../app/core/legal-store';
import { BastionData } from './http';

/** The answer of `/ui/data/v1/state` (bastion.md R-BAS-118). */
export interface BastionState {
  readonly confirmedAt: string;
  readonly staleAfterSeconds: number;
  readonly stale: boolean;
  readonly docs: boolean;
}

/**
 * The Bastion's state as the application shows it (R-UI-227, W-35's B.2b): the time of the catalogue it serves, for the footer's "As
 * of" and the band above an old page, and whether it serves at all (`not_ready`, a `503` while the replica holds no catalogue). The
 * shell reads it at once and then at the idle cadence of a live view (R-UI-213); the legal pages are read with it, as the main
 * installation reads them with its banner.
 */
@Injectable({ providedIn: 'root' })
export class BastionStateStore {
  private readonly data = inject(BastionData);
  private readonly legal = inject(LegalStore);

  private readonly _state = signal<BastionState | null>(null);
  private readonly _failure = signal<ApiError | null>(null);
  private reading: Promise<void> | null = null;

  /** The state of the last answer; null before the first and while the replica is not ready. */
  readonly state = this._state.asReadonly();
  /** The failure of the last read, null after an answer. */
  readonly failure = this._failure.asReadonly();
  /** The replica holds no catalogue to show yet: its pages say so in place of their content. */
  readonly notReady = computed(() => {
    const f = this._failure();
    return f !== null && f.status === 503 && f.problem?.code === 'not_ready';
  });
  /** The web interface is switched off: every page answers what its read answered. */
  readonly off = computed(() => this._failure()?.status === 404);
  /** The first read has answered or failed. */
  readonly settled = computed(() => this._state() !== null || this._failure() !== null);

  /** Reads the state and the legal pages; concurrent calls share the read. A failure keeps the last state, as the banner's does. */
  refresh(): Promise<void> {
    this.reading ??= this.read().finally(() => {
      this.reading = null;
    });
    return this.reading;
  }

  private async read(): Promise<void> {
    const legal = this.legal.refresh();
    try {
      this._state.set(await this.data.get<BastionState>('state'));
      this._failure.set(null);
    } catch (e) {
      this._failure.set(e instanceof ApiError ? e : new ApiError(0, null, null, null));
      if (this.notReady()) this._state.set(null);
    } finally {
      await legal;
    }
  }
}
