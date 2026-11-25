import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input, output, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { LimitMode, LimitSettings, LimitsApi, ModePreview } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Versioned } from '../../api/repositories-api';
import { Notifier } from '../../core/notifier';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle, QuaderDialog } from '../../shared/ui/dialog';
import { subjectKindFilterLabel } from './callers';
import { figureName, formatCount } from './figures';

/** The owner's two words (decision 17), the same in every language. */
export function modeWord(mode: LimitMode): string {
  return mode === 'enforce' ? 'Enforce' : 'Audit';
}

/** What the mode does, one sentence (R-UI-177). */
export function modeSentence(mode: LimitMode): string {
  return mode === 'enforce'
    ? $localize`:@@limits.mode.enforce-sentence:Limits refuse and slow requests.`
    : $localize`:@@limits.mode.audit-sentence:Every limit is counted and shown; nothing is refused or slowed.`;
}

export interface ModeDialogData {
  readonly settings: Versioned<LimitSettings>;
  readonly to: LimitMode;
}

/**
 * The dialog of the mode switch (R-UI-182, Q-UI-733). **Switch to Enforce** shows first what the last 24 hours would have been
 * in Enforce, with the figures that would have refused most and the kinds of caller, each a link to the overview filtered by
 * **Would have been limited**; **Switch to Audit** says in one sentence that it removes the protection (Q-LIM-4). The switch sends `PATCH` with the `ETag`; recent
 * authentication is asked for by the session dialog of R-UI-042.
 */
@Component({
  selector: 'quader-limit-mode-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DialogClose, DialogTitle, ErrorNotice, RouterLink],
  template: `
    @if (data.to === 'enforce') {
      <h2 quaderDialogTitle i18n="@@limits.mode.to-enforce-title">Switch to Enforce</h2>
    } @else {
      <h2 quaderDialogTitle i18n="@@limits.mode.to-audit-title">Switch to Audit</h2>
    }
    <div class="dialog-body">
      @if (failure(); as e) {
        <quader-error-notice [error]="e" />
      }
      @if (data.to === 'enforce') {
        @if (previewFailure(); as e) {
          <quader-error-notice [error]="e" (retry)="loadPreview()" />
        } @else if (preview(); as p) {
          @if (p.subjects === 0) {
            <p i18n="@@limits.mode.preview-none">In the last 24 hours no caller would have been limited.</p>
          } @else {
            <ul class="facts">
              <li>{{ callersText(p.subjects) }}</li>
              <li>{{ refusedText(p.refusedRequests) }}</li>
              <li>{{ slowedText(p.slowedDownloads) }}</li>
              <li>{{ blockedText(p.blockedSubjects) }}</li>
            </ul>
            @if (topFigures().length > 0) {
              <h3 i18n="@@limits.mode.preview-figures">Would have refused most</h3>
              <ul class="links">
                @for (f of topFigures(); track f.figure) {
                  <li>
                    <a routerLink="/admin/usage" [queryParams]="{ state: 'wouldBeLimited' }" (click)="close()">{{ figureName(f.figure) }}</a>
                    &ngsp;<span class="muted">· {{ figureCount(f.subjects, f.events) }}</span>
                  </li>
                }
              </ul>
            }
            @if (p.byKind.length > 0) {
              <h3 i18n="@@limits.mode.preview-kinds">Kinds of caller</h3>
              <ul class="links">
                @for (k of p.byKind; track k.kind) {
                  <li>
                    <a routerLink="/admin/usage" [queryParams]="{ state: 'wouldBeLimited', kind: k.kind }" (click)="close()">{{ kindLabel(k.kind) }}</a>
                    &ngsp;<span class="muted">· {{ callerCount(k.subjects) }}</span>
                  </li>
                }
              </ul>
            }
          }
          <p class="consequence" i18n="@@limits.mode.consequence">From now on, limits refuse and slow requests on every replica within seconds.</p>
        } @else {
          <p role="status" i18n="@@limits.mode.preview-loading">Reading the last 24 hours…</p>
        }
      } @else {
        <p i18n="@@limits.mode.to-audit-removes">In Audit nothing is refused or slowed on the package and registry addresses, not even by the request limits; every limit is counted and shown as "Would have been limited".</p>
      }
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@limits.cancel">Cancel</button>
      @if (data.to === 'enforce') {
        <button class="btn btn-confirm" type="button" [disabled]="busy()" (click)="switch()" i18n="@@limits.mode.to-enforce-title">Switch to Enforce</button>
      } @else {
        <button class="btn btn-confirm" type="button" [disabled]="busy()" (click)="switch()" i18n="@@limits.mode.to-audit-title">Switch to Audit</button>
      }
    </div>
  `,
  styles: `
    .facts,
    .links {
      margin: 0 0 var(--quader-space-3);
      padding-inline-start: var(--quader-space-5);
    }
    h3 {
      margin: var(--quader-space-3) 0 var(--quader-space-1);
      font: var(--quader-type-title-small);
    }
    .muted {
      color: var(--quader-text-muted);
    }
    .consequence {
      margin-block-start: var(--quader-space-3);
      font-weight: 600;
    }
  `,
})
export class LimitModeDialog {
  protected readonly data = inject<ModeDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<Versioned<LimitSettings>>>(DialogRef);
  private readonly api = inject(LimitsApi);
  private readonly locale = inject(LOCALE_ID);

  protected readonly preview = signal<ModePreview | null>(null);
  protected readonly previewFailure = signal<ApiError | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly busy = signal(false);
  protected readonly figureName = figureName;
  /** The three figures of the preview with the most events. */
  protected readonly topFigures = computed(() => [...(this.preview()?.byFigure ?? [])].sort((a, b) => b.events - a.events).slice(0, 3));

  constructor() {
    if (this.data.to === 'enforce') void this.loadPreview();
  }

  protected async loadPreview(): Promise<void> {
    try {
      this.preview.set(await this.api.modePreview());
      this.previewFailure.set(null);
    } catch (e) {
      this.previewFailure.set(asApiError(e));
    }
  }

  protected kindLabel(kind: Parameters<typeof subjectKindFilterLabel>[0]): string {
    return subjectKindFilterLabel(kind);
  }

  protected callersText(n: number): string {
    const count = formatCount(n, this.locale);
    return n === 1
      ? $localize`:@@limits.mode.one-caller:1 caller would have been limited.`
      : $localize`:@@limits.mode.callers:${count}:count: callers would have been limited.`;
  }

  protected refusedText(n: number): string {
    const count = formatCount(n, this.locale);
    return n === 1
      ? $localize`:@@limits.mode.one-refused:1 request would have been refused.`
      : $localize`:@@limits.mode.refused:${count}:count: requests would have been refused.`;
  }

  protected slowedText(n: number): string {
    const count = formatCount(n, this.locale);
    return n === 1
      ? $localize`:@@limits.mode.one-slowed:1 download would have been slowed down.`
      : $localize`:@@limits.mode.slowed:${count}:count: downloads would have been slowed down.`;
  }

  protected blockedText(n: number): string {
    const count = formatCount(n, this.locale);
    return n === 1
      ? $localize`:@@limits.mode.one-blocked:1 caller would have been blocked.`
      : $localize`:@@limits.mode.blocked:${count}:count: callers would have been blocked.`;
  }

  /** "9 callers, 3,260 times", "1 caller, 12 times". */
  protected figureCount(subjects: number, events: number): string {
    return `${this.callerCount(subjects)}, ${this.times(events)}`;
  }

  protected callerCount(n: number): string {
    return n === 1 ? $localize`:@@limits.mode.caller-count-one:1 caller` : $localize`:@@limits.mode.caller-count:${formatCount(n, this.locale)}:count: callers`;
  }

  private times(n: number): string {
    return n === 1 ? $localize`:@@limits.mode.times-one:once` : $localize`:@@limits.mode.times:${formatCount(n, this.locale)}:count: times`;
  }

  protected close(): void {
    this.ref.close();
  }

  protected async switch(): Promise<void> {
    this.busy.set(true);
    this.failure.set(null);
    try {
      this.ref.close(await this.api.patchSettings({ mode: this.data.to }, this.data.settings.etag));
    } catch (e) {
      this.failure.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}

/**
 * The mode under the header of every tab of **Download limits** (R-UI-177): its word and one sentence, and for a holder of
 * `limits:write` the switch beside it (R-UI-182).
 */
@Component({
  selector: 'quader-limit-mode',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice],
  template: `
    @if (failure(); as e) {
      <quader-error-notice [error]="e" (retry)="load()" />
    } @else if (settings(); as s) {
      <div class="mode-line">
        <p class="mode">
          <ng-container i18n="@@limits.mode.label">Mode:</ng-container>&ngsp;<strong translate="no">{{ word(s.value.mode) }}</strong>.
          {{ sentence(s.value.mode) }}
        </p>
        @if (canWrite()) {
          <button class="btn btn-default on-line" type="button" (click)="openSwitch(s)">
            @if (s.value.mode === 'audit') {
              <ng-container i18n="@@limits.mode.to-enforce">Switch to Enforce…</ng-container>
            } @else {
              <ng-container i18n="@@limits.mode.to-audit">Switch to Audit…</ng-container>
            }
          </button>
        }
      </div>
    }
  `,
  styles: `
    .mode-line {
      display: flex;
      flex-wrap: wrap;
      align-items: flex-start;
      gap: var(--quader-space-2) var(--quader-space-4);
      margin-block-end: var(--quader-space-4);
    }
    .mode {
      margin: 0;
    }
  `,
})
export class LimitModeLine {
  private readonly api = inject(LimitsApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly notifier = inject(Notifier);

  readonly canWrite = input(false);
  /** The settings after each load or switch, for the tab that edits the block time. */
  readonly loaded = output<Versioned<LimitSettings>>();

  protected readonly settings = signal<Versioned<LimitSettings> | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly word = modeWord;
  protected readonly sentence = modeSentence;

  constructor() {
    void this.load();
  }

  async load(): Promise<void> {
    try {
      const s = await this.api.settings();
      this.failure.set(null);
      this.settings.set(s);
      this.loaded.emit(s);
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }

  /** Takes settings another part of the page saved (the block time), so that the next switch carries their `ETag`. */
  take(settings: Versioned<LimitSettings>): void {
    this.settings.set(settings);
  }

  protected async openSwitch(s: Versioned<LimitSettings>): Promise<void> {
    const to: LimitMode = s.value.mode === 'audit' ? 'enforce' : 'audit';
    const ref = this.dialog.open<LimitModeDialog, ModeDialogData, Versioned<LimitSettings>>(LimitModeDialog, {
      data: { settings: s, to },
      maxWidth: '40rem',
    });
    const result = await firstValueFrom(ref.closed);
    if (!result) return;
    this.settings.set(result);
    this.loaded.emit(result);
    this.notifier.status(
      $localize`:@@limits.mode.switched:Mode: ${modeWord(result.value.mode)}:mode:. ${modeSentence(result.value.mode)}:sentence:`,
    );
  }
}
