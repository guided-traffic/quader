import { Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { AnsibleApi, AnsibleImportTask } from '../../../api/ansible-api';
import { ApiError } from '../../../api/problem';
import { splitRef } from '../../../api/repositories-api';
import { asFailure } from '../../../core/http/failure';
import { ErrorNotice } from '../../../shared/error-notice';
import { Busy } from '../../../shared/ui/busy';
import { DIALOG_DATA, DialogRef, DialogTitle } from '../../../shared/ui/dialog';
import { AnsibleImportTaskView } from '../../packages/ansible-import-task';
import { formatSize } from '../../packages/format-size';
import { versionCommands } from '../../packages/package-links';

/** What the dialog is opened with (R-ANS-061). */
export interface AnsibleUploadData {
  /**
   * The repositories the collection may go into, `<project>/<key>`, the first chosen: one is named, several are a choice (the page of a
   * role, which a remote repository holds, offers the project's repositories the reader may publish into).
   */
  readonly repositories: readonly string[];
}

/** Where the dialog stands: a file to choose, the bytes on their way, the server reading them, the task it answered, or a refusal. */
type Phase = 'choose' | 'sending' | 'importing' | 'done' | 'refused';

/**
 * Upload collection (R-ANS-061, Q-UI-1321): one archive, chosen with a button or dropped, the repository it goes into, and the result.
 * Nothing of the archive is read here but its name and its size, for the dialog: the server reads its namespace, name and version from
 * its `MANIFEST.json` and runs every check of the client's publish. While the bytes go the button spins and a bar shows them; then the
 * server reads the archive within the same request, and its answer is the import task, final when it comes (Q-ANS-8), so nothing is
 * read again. A completed task leads to the version's page; a failed one shows why the archive was not taken; a refusal before the
 * archive was read is the error notice with every fact of the answer (R-UI-216), and the file stays chosen.
 */
@Component({
  selector: 'quader-ansible-upload-dialog',
  imports: [DialogTitle, RouterLink, Busy, ErrorNotice, AnsibleImportTaskView],
  template: `
    <h2 quaderDialogTitle i18n="@@ansible.upload.title">Upload collection</h2>
    <div class="dialog-body">
      <div class="field">
        @if (data.repositories.length > 1) {
          <label class="field-label" for="ansible-upload-repository" i18n="@@ansible.upload.repository">Repository</label>
          <select
            id="ansible-upload-repository"
            class="select-input field-w-name"
            [disabled]="running() || phase() === 'done'"
            (change)="repository.set($any($event.target).value)"
          >
            @for (r of data.repositories; track r) {
              <option [value]="r" [selected]="r === repository()" translate="no">{{ r }}</option>
            }
          </select>
        } @else {
          <span class="field-label" i18n="@@ansible.upload.repository">Repository</span>
          <span class="target" translate="no">{{ repository() }}</span>
        }
      </div>

      @if (phase() === 'done' && task(); as t) {
        <p class="outcome" data-testid="ansible-upload-outcome">
          @if (t.state === 'completed') {
            <ng-container i18n="@@ansible.upload.completed">The collection is published:</ng-container>&ngsp;<span translate="no"
              >{{ t.namespace }}.{{ t.name }} {{ t.version }}</span
            >
          } @else {
            <ng-container i18n="@@ansible.upload.failed">The archive was not published. The import log says why.</ng-container>
          }
        </p>
        <section class="log" i18n-aria-label="@@ansible.upload.log" aria-label="Import log">
          <quader-ansible-import-task [task]="t" />
        </section>
      } @else {
        <div
          class="drop"
          [class.over]="over()"
          (dragover)="$event.preventDefault(); over.set(!running())"
          (dragleave)="over.set(false)"
          (drop)="drop($event)"
        >
          @if (file(); as f) {
            <span class="chosen">
              <span class="name" translate="no">{{ f.name }}</span>
              <span class="size">{{ size(f.size) }}</span>
            </span>
            @if (!running()) {
              <button class="btn btn-default" type="button" (click)="chooser.click()" i18n="@@ansible.upload.choose-other">Choose another file</button>
            }
          } @else {
            <span i18n="@@ansible.upload.drop">Drop the collection's archive here, or</span>
            <button class="btn btn-default" type="button" (click)="chooser.click()" i18n="@@ansible.upload.choose">Choose file</button>
          }
          <input
            #chooser
            type="file"
            accept=".tar.gz,.tgz,application/gzip"
            class="visually-hidden"
            tabindex="-1"
            aria-hidden="true"
            (change)="choose($any($event.target).files)"
          />
        </div>
        @if (phase() === 'sending') {
          <progress [attr.aria-label]="progressLabel" [value]="loaded()" [max]="total()"></progress>
        } @else if (phase() === 'importing') {
          <p class="hint" data-testid="ansible-upload-importing" i18n="@@ansible.upload.importing">
            The archive is sent. Quader reads it and runs the checks of a publish.
          </p>
        } @else {
          <p class="hint" i18n="@@ansible.upload.hint">
            The archive that ansible-galaxy collection build makes. Quader reads the namespace, the name and the version from it.
          </p>
        }
        @if (failure(); as f) {
          <quader-error-notice [error]="f" />
        }
      }
      <span class="visually-hidden" role="status">{{ announcement() }}</span>
    </div>
    <div class="dialog-actions">
      @if (phase() === 'done' && task(); as t) {
        <button class="btn btn-tertiary" type="button" (click)="close()" i18n="@@ansible.upload.close">Close</button>
        @if (t.state === 'completed') {
          <a class="btn btn-confirm" [routerLink]="versionLink(t)" (click)="close()" i18n="@@ansible.upload.open">Open version</a>
        } @else {
          <button class="btn btn-default" type="button" (click)="again()" i18n="@@ansible.upload.again">Choose another file</button>
        }
      } @else {
        @if (running()) {
          <button class="btn btn-tertiary" type="button" (click)="cancel()" i18n="@@ansible.upload.cancel">Cancel upload</button>
        } @else {
          <button class="btn btn-tertiary" type="button" (click)="close()" i18n="@@ansible.upload.cancel-dialog">Cancel</button>
        }
        <button
          class="btn btn-confirm"
          type="button"
          [disabled]="running() || !file()"
          [quaderBusy]="running()"
          (click)="start()"
          i18n="@@ansible.upload.start"
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
    /* The button stands on the first line of the file's name, which may wrap (the layout check's on-line rule). */
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
      flex: 1 1 12rem;
      min-inline-size: 0;
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
export class AnsibleUploadDialog {
  protected readonly data = inject<AnsibleUploadData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<boolean, AnsibleUploadDialog>>(DialogRef);
  private readonly api = inject(AnsibleApi);

  protected readonly repository = signal(this.data.repositories[0] ?? '');
  protected readonly file = signal<File | null>(null);
  protected readonly phase = signal<Phase>('choose');
  protected readonly loaded = signal(0);
  protected readonly total = signal(1);
  protected readonly task = signal<AnsibleImportTask | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly over = signal(false);
  protected readonly announcement = signal('');
  protected readonly running = computed(() => this.phase() === 'sending' || this.phase() === 'importing');
  protected readonly progressLabel = $localize`:@@ansible.upload.progress:Upload progress`;
  /** A task was published in this dialog: the opener's list has a version more. */
  private published = false;
  private abort: AbortController | null = null;

  constructor() {
    // A dialog closed while the bytes go stops the transfer, which stores nothing (R-ARCH-014); once they are sent the request is left
    // to its answer, so that the server's publish is not cut in the middle.
    inject(DestroyRef).onDestroy(() => {
      if (this.phase() === 'sending') this.abort?.abort();
    });
  }

  protected size(bytes: number): string {
    return formatSize(bytes);
  }

  protected choose(list: FileList | null): void {
    const f = list?.[0];
    if (f) this.pick(f);
  }

  protected drop(event: DragEvent): void {
    event.preventDefault();
    this.over.set(false);
    const f = event.dataTransfer?.files?.[0];
    if (f && !this.running()) this.pick(f);
  }

  private pick(f: File): void {
    this.file.set(f);
    this.failure.set(null);
    this.phase.set('choose');
  }

  /** Back to the choice of a file after a failed import, in the same repository. */
  protected again(): void {
    this.task.set(null);
    this.file.set(null);
    this.phase.set('choose');
  }

  protected close(): void {
    this.ref.close(this.published);
  }

  protected cancel(): void {
    this.abort?.abort();
  }

  /** The version's page in the repository's project (R-UI-111): the collection's newest documentation, of this version. */
  protected versionLink(t: AnsibleImportTask): string[] {
    return versionCommands(splitRef(this.repository()).project, 'ansible', `${t.namespace}.${t.name}`, t.version);
  }

  protected async start(): Promise<void> {
    const f = this.file();
    if (!f || this.running()) return;
    this.failure.set(null);
    this.loaded.set(0);
    this.total.set(Math.max(f.size, 1));
    this.phase.set('sending');
    this.abort = new AbortController();
    try {
      const t = await this.api.publish(this.repository(), f, {
        signal: this.abort.signal,
        onProgress: (loaded, total) => {
          this.loaded.set(loaded);
          this.total.set(Math.max(total, 1));
          if (loaded >= total) this.phase.set('importing');
        },
      });
      this.task.set(t);
      this.phase.set('done');
      this.published ||= t.state === 'completed';
      const name = `${t.namespace}.${t.name}`;
      this.announcement.set(
        t.state === 'completed'
          ? $localize`:@@ansible.upload.announce.completed:${name}:name: ${t.version}:version: is published.`
          : $localize`:@@ansible.upload.announce.failed:The archive was not published.`,
      );
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') {
        this.phase.set('choose');
        return;
      }
      this.failure.set(asFailure(e));
      this.phase.set('refused');
      this.announcement.set($localize`:@@ansible.upload.announce.refused:The upload was refused.`);
    } finally {
      this.abort = null;
    }
  }
}
