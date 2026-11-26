import { Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NuGetApi, NuGetPublished, SymbolsRefused } from '../../../api/nuget-api';
import { ApiError } from '../../../api/problem';
import { splitRef } from '../../../api/repositories-api';
import { asFailure } from '../../../core/http/failure';
import { ErrorNotice } from '../../../shared/error-notice';
import { Busy } from '../../../shared/ui/busy';
import { DIALOG_DATA, DialogRef, DialogTitle } from '../../../shared/ui/dialog';
import { formatSize } from '../../packages/format-size';
import { versionCommands } from '../../packages/package-links';

/** What the dialog is opened with (R-NUGET-006): the repository the package goes into, `<project>/<key>`. */
export interface NuGetUploadData {
  readonly repository: string;
}

/** Where the dialog stands: files to choose, the package's bytes on their way, the symbols' after it, the answer, or a refusal. */
type Phase = 'choose' | 'sending' | 'symbols' | 'done' | 'refused';

/**
 * Upload package (formats/nuget.md R-NUGET-006, Q-NUGET-162, Q-UI-1364): one `.nupkg` and, if wanted, its `.snupkg`, chosen with a button
 * or dropped, and the result of NuGet's push. Nothing of a file is read here but its name and its size: the server reads the ID and the
 * version from the package's `.nuspec` and runs every check of `dotnet nuget push`. While the bytes go the button spins and a bar shows
 * them; the push has no import task, so its answer is the result: the version it stored, with a link to its page, and the symbol package
 * stored after it or refused with the reason. A refusal is the error notice with every fact of the answer (R-UI-216), and the files stay
 * chosen. The frame is the collection upload's (`ansible-upload-dialog.ts`): the same token road, field, drop area, bar and acts.
 */
@Component({
  selector: 'quader-nuget-upload-dialog',
  imports: [DialogTitle, RouterLink, Busy, ErrorNotice],
  template: `
    <h2 quaderDialogTitle i18n="@@nuget.upload.title">Upload package</h2>
    <div class="dialog-body">
      <div class="field">
        <span class="field-label" i18n="@@nuget.upload.repository">Repository</span>
        <span class="target" translate="no">{{ data.repository }}</span>
      </div>

      @if (phase() === 'done' && result(); as r) {
        <p class="outcome" data-testid="nuget-upload-outcome">
          <ng-container i18n="@@nuget.upload.completed">The package is published:</ng-container>&ngsp;<span translate="no"
            >{{ r.id }} {{ r.version }}</span
          >
        </p>
        @if (r.symbols === 'stored') {
          <p class="hint" i18n="@@nuget.upload.symbols-stored">Its symbol package is stored beside it.</p>
        }
        @if (symbolsFailure(); as f) {
          <p class="hint" i18n="@@nuget.upload.symbols-refused">Its symbol package was not stored:</p>
          <quader-error-notice [error]="f" />
        }
      } @else {
        <div
          class="drop"
          [class.over]="over()"
          (dragover)="$event.preventDefault(); over.set(!running())"
          (dragleave)="over.set(false)"
          (drop)="drop($event)"
        >
          @if (pkg() || symbols()) {
            <span class="chosen">
              @for (f of chosen(); track f.name) {
                <span class="file"
                  ><span class="name" translate="no">{{ f.name }}</span>&ngsp;<span class="size">{{ size(f.size) }}</span></span
                >
              }
            </span>
            @if (!running()) {
              <button class="btn btn-default" type="button" (click)="chooser.click()" i18n="@@nuget.upload.choose-other">Choose other files</button>
            }
          } @else {
            <span i18n="@@nuget.upload.drop">Drop the package here, with its symbol package if you have one, or</span>
            <button class="btn btn-default" type="button" (click)="chooser.click()" i18n="@@nuget.upload.choose">Choose files</button>
          }
          <input
            #chooser
            type="file"
            multiple
            accept=".nupkg,.snupkg"
            class="visually-hidden"
            tabindex="-1"
            aria-hidden="true"
            (change)="choose($any($event.target).files)"
          />
        </div>
        @if (running()) {
          <progress [attr.aria-label]="progressLabel" [value]="loaded()" [max]="total()"></progress>
          @if (phase() === 'symbols') {
            <p class="hint" data-testid="nuget-upload-symbols" i18n="@@nuget.upload.sending-symbols">The package is stored. Its symbol package follows.</p>
          }
        } @else if (onlySymbols()) {
          <p class="hint" i18n="@@nuget.upload.needs-package">A symbol package follows its package: choose the .nupkg as well.</p>
        } @else {
          <p class="hint" i18n="@@nuget.upload.hint">
            The .nupkg that dotnet pack makes, and its .snupkg. Quader reads the ID and the version from the package.
          </p>
        }
        @if (failure(); as f) {
          <quader-error-notice [error]="f" />
        }
      }
      <span class="visually-hidden" role="status">{{ announcement() }}</span>
    </div>
    <div class="dialog-actions">
      @if (phase() === 'done' && result(); as r) {
        <button class="btn btn-tertiary" type="button" (click)="close()" i18n="@@nuget.upload.close">Close</button>
        <a class="btn btn-confirm" [routerLink]="versionLink(r)" (click)="close()" i18n="@@nuget.upload.open">Open version</a>
      } @else {
        @if (running()) {
          <button class="btn btn-tertiary" type="button" (click)="cancel()" i18n="@@nuget.upload.cancel">Cancel upload</button>
        } @else {
          <button class="btn btn-tertiary" type="button" (click)="close()" i18n="@@nuget.upload.cancel-dialog">Cancel</button>
        }
        <button
          class="btn btn-confirm"
          type="button"
          [disabled]="running() || !pkg()"
          [quaderBusy]="running()"
          (click)="start()"
          i18n="@@nuget.upload.start"
        >
          Upload
        </button>
      }
    </div>
  `,
  styles: `
    .field {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-3);
    }
    .target {
      overflow-wrap: anywhere;
    }
    /* The button stands on the first line of the files' names, which may wrap (the layout check's on-line rule). */
    .drop {
      display: flex;
      flex-wrap: wrap;
      align-items: baseline;
      gap: var(--quader-space-3);
      padding: var(--quader-space-4);
      border: 2px dashed var(--quader-border-strong);
      border-radius: var(--quader-radius-l);
      margin-block-end: var(--quader-space-2);
    }
    .drop.over {
      border-color: var(--quader-accent);
    }
    .chosen {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-2);
      flex: 1 1 12rem;
      min-inline-size: 0;
    }
    /* A file's name over its size, as the collection upload has them: a size never breaks in two. */
    .file {
      display: flex;
      flex-direction: column;
    }
    .name {
      overflow-wrap: anywhere;
    }
    .size {
      color: var(--quader-text-muted);
    }
    progress {
      inline-size: 100%;
    }
    .hint {
      margin-block: 0 var(--quader-space-3);
    }
    .outcome {
      margin-block: 0 var(--quader-space-3);
      overflow-wrap: anywhere;
    }
    quader-error-notice {
      display: block;
      margin-block-start: var(--quader-space-3);
    }
  `,
})
export class NuGetUploadDialog {
  protected readonly data = inject<NuGetUploadData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<boolean, NuGetUploadDialog>>(DialogRef);
  private readonly api = inject(NuGetApi);

  protected readonly pkg = signal<File | null>(null);
  protected readonly symbols = signal<File | null>(null);
  protected readonly chosen = computed(() => [this.pkg(), this.symbols()].filter((f): f is File => f !== null));
  protected readonly onlySymbols = computed(() => this.symbols() !== null && this.pkg() === null);
  protected readonly phase = signal<Phase>('choose');
  protected readonly loaded = signal(0);
  protected readonly total = signal(1);
  protected readonly result = signal<NuGetPublished | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly symbolsFailure = signal<ApiError | null>(null);
  protected readonly over = signal(false);
  protected readonly announcement = signal('');
  protected readonly running = computed(() => this.phase() === 'sending' || this.phase() === 'symbols');
  protected readonly progressLabel = $localize`:@@nuget.upload.progress:Upload progress`;
  private published = false;
  private abort: AbortController | null = null;

  constructor() {
    // A dialog closed while the bytes go stops the transfer, which stores nothing (R-ARCH-014).
    inject(DestroyRef).onDestroy(() => {
      if (this.running()) this.abort?.abort();
    });
  }

  protected size(bytes: number): string {
    return formatSize(bytes);
  }

  protected choose(list: FileList | null): void {
    if (list) this.pick(Array.from(list));
  }

  protected drop(event: DragEvent): void {
    event.preventDefault();
    this.over.set(false);
    if (!this.running()) this.pick(Array.from(event.dataTransfer?.files ?? []));
  }

  /**
   * The files chosen, by their kind as their names say: a `.snupkg` is the symbol package, any other file the package (the server reads
   * what it is; a file that is no package is refused with the reason). A choice replaces the earlier one.
   */
  private pick(files: readonly File[]): void {
    if (files.length === 0) return;
    const symbols = files.find((f) => f.name.toLowerCase().endsWith('.snupkg')) ?? null;
    const pkg = files.find((f) => f !== symbols) ?? null;
    this.pkg.set(pkg);
    this.symbols.set(symbols);
    this.failure.set(null);
    this.phase.set('choose');
  }

  protected close(): void {
    this.ref.close(this.published);
  }

  protected cancel(): void {
    this.abort?.abort();
  }

  /** The version's page in the repository's project (R-UI-111): the ID as declared, the version in its address's form. */
  protected versionLink(r: NuGetPublished): string[] {
    return versionCommands(splitRef(this.data.repository).project, 'nuget', r.id, r.version.split('+')[0]!.toLowerCase());
  }

  private progress(loaded: number, total: number): void {
    this.loaded.set(loaded);
    this.total.set(Math.max(total, 1));
  }

  protected async start(): Promise<void> {
    const pkg = this.pkg();
    if (!pkg || this.running()) return;
    this.failure.set(null);
    this.symbolsFailure.set(null);
    this.progress(0, pkg.size);
    this.phase.set('sending');
    this.abort = new AbortController();
    try {
      const r = await this.api.publish(this.data.repository, pkg, this.symbols(), {
        signal: this.abort.signal,
        onProgress: (loaded, total) => this.progress(loaded, total),
        onSymbols: () => {
          this.progress(0, this.symbols()?.size ?? 1);
          this.phase.set('symbols');
        },
      });
      this.finish(r);
    } catch (e) {
      if (e instanceof SymbolsRefused) {
        this.symbolsFailure.set(asFailure(e.failure));
        this.finish(e.stored);
        return;
      }
      if (e instanceof DOMException && e.name === 'AbortError') {
        this.phase.set('choose');
        return;
      }
      this.failure.set(asFailure(e));
      this.phase.set('refused');
      this.announcement.set($localize`:@@nuget.upload.announce.refused:The upload was refused.`);
    } finally {
      this.abort = null;
    }
  }

  private finish(r: NuGetPublished): void {
    this.result.set(r);
    this.phase.set('done');
    this.published = true;
    this.announcement.set($localize`:@@nuget.upload.announce.completed:${r.id}:id: ${r.version}:version: is published.`);
  }
}
