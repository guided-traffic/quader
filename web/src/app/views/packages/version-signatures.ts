import { ChangeDetectionStrategy, Component, computed, effect, inject, input, output, resource, untracked } from '@angular/core';
import { refreshResource } from '../../core/live-refresh';
import { repositoryOfContent } from '../../api/files-api';
import { PackageFile } from '../../api/packages-api';
import { SignatureReads } from '../../api/signature-reads';
import { SignatureRepositoryReport } from '../../api/signatures-api';
import { ErrorNotice } from '../../shared/error-notice';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { GateArtifact, SignatureGate } from '../signatures/signature-gate';
import { SignatureReport } from '../signatures/signature-report';
import { classLabel } from '../../features/repositories/repository-class';
import { Loading } from '../../shared/ui/loading';

/**
 * The tab **Signatures** of a version (R-SIG-035, R-SIG-024): what `GET …/versions/{version}/signatures` answers, per repository that
 * holds the version and that the reader may read. One repository shows its report alone; several each under their name and class,
 * since a signature pushed to one repository says nothing about the same bytes in another (R-SIG-004). A virtual repository's report
 * names the member each signature is attached in. A repository whose gate is on says first what its gate decides (R-SIG-074).
 */
@Component({
  selector: 'quader-version-signatures',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Loading, ErrorNotice, SignatureGate, SignatureReport],
  template: `
    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="failure.retry()" />
    } @else if (report(); as r) {
      @if (r.repositories.length === 0) {
        <p i18n="@@sig.version.none">No repository you may read holds this version.</p>
      }
      @for (rep of r.repositories; track rep.repository) {
        @if (r.repositories.length > 1) {
          <h2 class="sig-repository">
            <span translate="no">{{ rep.repository }}</span>&ngsp;<span class="muted">· {{ classText(rep.class) }}</span>
          </h2>
        }
        @if (rep.gate; as g) {
          <quader-signature-gate
            [gate]="g"
            [repository]="rep.repository"
            [artifacts]="artifactsOf(rep)"
            [signatures]="rep.signatures"
            [platforms]="platforms()"
            (changed)="reload()"
          />
        }
        <quader-signature-report [report]="rep" [format]="format()" [level]="2" [showRepository]="rep.class === 'virtual' || r.repositories.length > 1" />
      }
    } @else {
      <quader-loading i18n="@@files.signatures.loading">Loading the signatures.</quader-loading>
    }
  `,
  styles: `
    .sig-repository {
      margin: var(--quader-space-5) 0 var(--quader-space-3);
      font: var(--quader-type-title-small);
    }
    .sig-repository:first-child {
      margin-block-start: 0;
    }
    .muted {
      color: var(--quader-text-muted);
      font-weight: 400;
    }
  `,
})
export class VersionSignatures {
  private readonly api = inject(SignatureReads);

  readonly packageId = input.required<string>();
  readonly version = input.required<string>();
  readonly format = input.required<string>();
  /** The version's files: the artifacts a repository's gate judges, by the repository whose content route names them. */
  readonly files = input<readonly PackageFile[]>([]);
  /** The version as a reader names it, "portal/api 1.4.0": the artifact's name where the version has one file in a repository. */
  readonly artifact = input('');
  /** An image index's platforms (R-SIG-063). */
  readonly platforms = input<readonly string[]>([]);
  /** A release was granted or withdrawn here: the page reads the version again for its banner and its facts. */
  readonly changed = output<void>();

  protected readonly answer = resource({
    params: () => ({ id: this.packageId(), version: this.version() }),
    loader: ({ params }) => this.api.version(params.id, params.version),
  });
  protected readonly report = computed(() => valueOf(this.answer));
  /** What the page's own read says of the files' signatures; when it changes, the details here are read again (R-UI-213). */
  private readonly filesStamp = computed(() => JSON.stringify(this.files().map((f) => f.signatures ?? null)));
  /** The signatures' failure, kept while its notice reads them again (R-UI-216). */
  protected readonly failure = failureOf(this.answer);
  /** A system-only format declares no signatures (R-CLAMAV-001), so a report here never holds a repository of one. */
  protected readonly classText = classLabel;

  /** The files a repository's gate judges: those it holds, and for a virtual repository those its members hold. */
  protected artifactsOf(rep: SignatureRepositoryReport): readonly GateArtifact[] {
    const holders = new Set([rep.repository, ...rep.members]);
    const files = this.files().filter((f) => holders.has(repositoryOfContent(f.contentUrl) ?? ''));
    const single = files.length === 1 && this.artifact() !== '';
    return files.map((f) => ({
      name: single ? this.artifact() : (f.path.split('/').pop() ?? f.path),
      sha256: f.sha256,
      refusedReason: f.signatures?.refusedReason ?? null,
    }));
  }

  constructor() {
    let shown: string | null = null;
    effect(() => {
      const stamp = this.filesStamp();
      untracked(() => {
        if (shown !== null && shown !== stamp && this.answer.hasValue()) {
          void refreshResource(this.answer, () => this.api.version(this.packageId(), this.version())).catch(() => undefined);
        }
        shown = stamp;
      });
    });
  }

  protected reload(): void {
    this.answer.reload();
    this.changed.emit();
  }
}
