import { Injectable, inject, signal } from '@angular/core';
import { SystemApi } from '../api/system-api';
import { FACTORY_TIME_FORMAT, TimeFormat, isTimeFormat, timeFormat } from '../shared/time-format';

/**
 * Which form of a time is in force in the main application (R-UI-250, R-UI-251, Q-UI-1560): the reader's own choice (the preferences'
 * `timeFormat`, which `PreferencesStore` hands over), else the installation's default (`GET /api/v1/system/time-format`, public, read
 * with the banner at its cadence, so a change an administrator saves reaches every screen within a minute, R-UI-213), else the factory
 * default. It writes the result into `timeFormat` of `shared/time-format.ts`, which every formatted time reads.
 */
@Injectable({ providedIn: 'root' })
export class TimeFormatStore {
  private readonly api = inject(SystemApi);
  private readonly _installation = signal<TimeFormat>(FACTORY_TIME_FORMAT);
  private readonly _own = signal<TimeFormat | null>(null);

  /** The installation's default. */
  readonly installation = this._installation.asReadonly();
  /** The reader's own choice, null where they follow the installation's default or have no session. */
  readonly own = this._own.asReadonly();
  /** The form in force. */
  readonly format = timeFormat.asReadonly();

  /** Reads the installation's default; a failed read keeps the one in force, a time is no reason for an error of its own. */
  async refresh(): Promise<void> {
    try {
      this.adoptInstallation((await this.api.timeFormat()).timeFormat);
    } catch {
      // keep what is in force
    }
  }

  /** The default an administrator saved, or read. */
  adoptInstallation(value: string | null | undefined): void {
    if (isTimeFormat(value)) this._installation.set(value);
    this.apply();
  }

  /** The reader's stored choice; null (or a value this build does not know) follows the installation's default. */
  adoptOwn(value: string | null | undefined): void {
    this._own.set(isTimeFormat(value) ? value : null);
    this.apply();
  }

  private apply(): void {
    timeFormat.set(this._own() ?? this._installation());
  }
}
