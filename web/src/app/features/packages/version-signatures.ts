import { ChangeDetectionStrategy, Component, computed, inject, input, resource } from '@angular/core';
import { SignaturesApi } from '../../api/signatures-api';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { SignatureReport } from '../../shared/signatures/signature-report';

/**
 * The tab **Signatures** of a version (R-SIG-035, R-SIG-024): what `GET …/versions/{version}/signatures` answers, per repository that
 * holds the version and that the reader may read. One repository shows its report alone; several each under their name and class,
 * since a signature pushed to one repository says nothing about the same bytes in another (R-SIG-004). A virtual repository's report
 * names the member each signature is attached in.
 */
@Component({
  selector: 'quader-version-signatures',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice, SignatureReport],
  template: `
    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="answer.reload()" />
    } @else if (report(); as r) {
      @if (r.repositories.length === 0) {
        <p i18n="@@sig.version.none">No repository you may read holds this version.</p>
      }
      @for (rep of r.repositories; track rep.repository) {
        @if (r.repositories.length > 1) {
          <h2 class="sig-repository">
            <span translate="no">{{ rep.repository }}</span>&ngsp;<span class="muted">· {{ classLabels[rep.class] }}</span>
          </h2>
        }
        <quader-signature-report [report]="rep" [format]="format()" [level]="2" [showRepository]="rep.class === 'virtual' || r.repositories.length > 1" />
      }
    } @else {
      <p role="status" i18n="@@pkg.loading">Loading.</p>
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
  private readonly api = inject(SignaturesApi);

  readonly packageId = input.required<string>();
  readonly version = input.required<string>();
  readonly format = input.required<string>();

  protected readonly answer = resource({
    params: () => ({ id: this.packageId(), version: this.version() }),
    loader: ({ params }) => this.api.version(params.id, params.version),
  });
  protected readonly report = computed(() => valueOf(this.answer));
  protected readonly failure = computed(() => {
    const e = this.answer.error();
    return e ? asApiError(e) : null;
  });
  protected readonly classLabels: Record<string, string> = {
    local: $localize`:@@repo.class.local:Local`,
    remote: $localize`:@@repo.class.remote:Remote`,
    virtual: $localize`:@@repo.class.virtual:Virtual`,
  };
}
