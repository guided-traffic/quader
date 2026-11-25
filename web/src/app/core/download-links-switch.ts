import { Injectable, computed, inject, signal } from '@angular/core';
import { ReadAgain } from './read-again';
import { DownloadLinkSwitch, DownloadLinksApi } from '../api/download-links-api';

/**
 * Whether the installation delivers large downloads by signed links and which formats its switch reaches
 * (`getDownloadLinkSwitch`, R-STOR-014), read once per page load for every signed-in caller and kept: a repository's settings show
 * the section **Download links** with its state (R-UI-201). The settings page hands a saved switch on (`adoptEnabled`). A failed read
 * leaves the switch unknown: the section says nothing of the installation, and the API still decides.
 */
@Injectable({ providedIn: 'root' })
export class DownloadLinksSwitchStore {
  private readonly api = inject(DownloadLinksApi);
  private readonly value = signal<DownloadLinkSwitch | null>(null);
  private reading: Promise<void> | null = null;
  private readonly again = new ReadAgain(() => this.load());

  /** The installation's switch; false until it is read. */
  readonly enabled = computed(() => this.value()?.enabled ?? false);
  /** The formats with a route whose clients check a digest (R-STOR-071); empty until read. */
  readonly formats = computed<readonly string[]>(() => this.value()?.formats ?? []);
  /** The answer has arrived. */
  readonly known = computed(() => this.value() !== null);

  /** Reads the switch once; a second call waits for the first read. */
  load(): Promise<void> {
    this.reading ??= this.read();
    return this.reading;
  }

  /** What a save of the settings answered: the switch, with the formats of its routes. */
  adopt(enabled: boolean, formats: readonly string[]): void {
    this.value.set({ enabled, formats: [...new Set(formats)].sort() });
  }

  private async read(): Promise<void> {
    try {
      this.value.set(await this.api.switch());
      this.again.answered();
    } catch (e) {
      // Not known until a read answers: the next caller reads again, and so does the store itself after a failure that may pass
      // (R-UI-216), which used to keep the screens on "not known" until the next page load.
      this.reading = null;
      this.again.failed(e);
    }
  }
}
