import { Component, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { ApiError } from '../../api/problem';
import { DownloadLinksSwitchStore } from '../../core/download-links-switch';
import { ErrorNotice } from '../../shared/error-notice';
import { Checkbox } from '../../shared/ui/checkbox';
import { Disclosure } from '../../shared/ui/disclosure';
import { Busy } from '../../shared/ui/busy';

/**
 * The repository's opt-out of the delivery by signed links (R-UI-201, storage.md R-STOR-014): the closed section **Download links**
 * after **Signatures** of the settings page, never part of the creation form. One check box, **Downloads of this repository may go by
 * signed links of the object store**, checked by default (`storage.downloadLinks`), saved on its own with the page's `ETag`. It is
 * disabled with its reason only where it cannot act (Q-UI-956): not every client of the format checks a digest (the member is
 * `not_applicable`). While the installation's switch is off it stays usable, so that an Owner opts a repository out before an
 * administrator switches links on, and a sentence says when the choice takes effect. A virtual repository adds that a member which
 * does not allow it is delivered by Quader. A caller who may not change the repository reads the state as text.
 */
@Component({
  selector: 'quader-repository-download-links',
  imports: [Busy, Checkbox, Disclosure, ErrorNotice],
  template: `
    <quader-disclosure
      i18n-heading="@@repo.links.heading"
      heading="Download links"
      i18n-hint="@@repo.links.hint"
      hint="Whether large downloads of this repository may go directly from the object store."
      [note]="dirty() ? unsavedNote : ''"
    >
      <form (submit)="submit($event)" novalidate>
        @if (failure(); as e) {
          <quader-error-notice [error]="e" />
        }
        @if (canChange()) {
          <quader-checkbox
            [checked]="applicable() && allowed()"
            [disabled]="locked() || saving()"
            [describedBy]="'repo-links-note'"
            (checkedChange)="allowed.set($event)"
          >
            <ng-container i18n="@@repo.links.allowed">Downloads of this repository may go by signed links of the object store</ng-container>
          </quader-checkbox>
        } @else {
          <p data-testid="repo-links-state">
            @if (applicable() && stored()) {
              <ng-container i18n="@@repo.links.state-allowed">Downloads of this repository may go by signed links of the object store.</ng-container>
            } @else {
              <ng-container i18n="@@repo.links.state-quader">Quader delivers every download of this repository itself.</ng-container>
            }
          </p>
        }
        <div id="repo-links-note" class="notes">
          @if (!applicable()) {
            <p class="hint" data-testid="repo-links-not-applicable" i18n="@@repo.links.not-applicable">
              Not every {{ formatLabel() }} client checks the bytes it downloads against a digest, so Quader delivers every download of
              this repository itself.
            </p>
          } @else if (known()) {
            @if (installationOn()) {
              <p class="hint" data-testid="repo-links-installation-on" i18n="@@repo.links.installation-on">
                The installation delivers large downloads by link.
              </p>
            } @else {
              <p class="hint" data-testid="repo-links-installation-off" i18n="@@repo.links.installation-off">
                The installation does not hand downloads to links at the moment; your choice takes effect when an administrator switches
                them on.
              </p>
            }
            @if (virtual()) {
              <p class="hint" data-testid="repo-links-virtual" i18n="@@repo.links.virtual">A member that does not allow it is delivered by Quader.</p>
            }
          }
        </div>
        @if (canChange() && !locked()) {
          <div class="actions">
            <button class="btn btn-confirm" type="submit" [disabled]="!dirty() || saving()" [quaderBusy]="saving()">
              <ng-container i18n="@@repo.settings.save">Save changes</ng-container>
              <span class="visually-hidden" i18n="@@repo.links.save-for"> (download links)</span>
            </button>
            @if (dirty()) {
              <button class="btn btn-tertiary" type="button" (click)="allowed.set(stored())" i18n="@@repo.edit.reset">Discard changes</button>
            }
          </div>
        }
      </form>
    </quader-disclosure>
  `,
  styles: `
    .notes p {
      margin-block: var(--quader-space-1) 0;
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2);
      margin-block-start: var(--quader-space-4);
    }
  `,
})
export class RepositoryDownloadLinksSection {
  private readonly links = inject(DownloadLinksSwitchStore);

  /** `storage.downloadLinks` as stored: true unless it says false. */
  readonly stored = input.required<boolean>();
  /** The repository's format, by name, and its display name. */
  readonly format = input.required<string>();
  readonly formatLabel = input('');
  readonly virtual = input(false);
  /** The caller may write the repository's settings, and the repository is not being deleted. */
  readonly canChange = input(false);
  readonly saving = input(false);
  readonly failure = input<ApiError | null>(null);
  readonly save = output<boolean>();

  protected readonly allowed = signal(true);
  /** The switch was read; until then, and after a failed read, the section says nothing of the installation. */
  protected readonly known = computed(() => this.links.known());
  /** The format has a route that every client of it checks against a digest; unknown until the switch is read, when the API decides. */
  protected readonly applicable = computed(() => !this.links.known() || this.links.formats().includes(this.format()));
  /** The installation's switch; it decides only the sentence under the box (Q-UI-956). */
  protected readonly installationOn = computed(() => this.links.enabled());
  /** The box cannot act: not every client of the format checks. The installation's switch never disables it. */
  protected readonly locked = computed(() => !this.applicable());
  protected readonly dirty = computed(() => !this.locked() && this.allowed() !== this.stored());
  protected readonly unsavedNote = $localize`:@@repo.settings.unsaved:Unsaved changes`;

  constructor() {
    void this.links.load();
    // The box follows what is stored, after the load and after a save.
    effect(() => {
      const stored = this.stored();
      untracked(() => this.allowed.set(stored));
    });
  }

  protected submit(event: Event): void {
    event.preventDefault();
    if (this.dirty() && !this.saving()) this.save.emit(this.allowed());
  }
}
