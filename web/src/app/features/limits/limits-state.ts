import { Injectable, signal } from '@angular/core';
import { LimitSettings } from '../../api/limits-api';
import { Versioned } from '../../api/repositories-api';

/**
 * The settings of the limits shared by the tabs of **Download limits** (R-UI-177): the frame's mode line loads them, the tab
 * **Rules** edits the block time with their `ETag`, and a save is handed back to the mode line.
 */
@Injectable()
export class LimitsSettingsState {
  readonly settings = signal<Versioned<LimitSettings> | null>(null);
  /** Set by the frame: called with the settings a tab saved. */
  saved: (settings: Versioned<LimitSettings>) => void = () => undefined;

  store(settings: Versioned<LimitSettings>): void {
    this.settings.set(settings);
    this.saved(settings);
  }
}
