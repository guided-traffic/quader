import { Component, LOCALE_ID, computed, inject, input } from '@angular/core';
import { UpstreamTestResult } from '../../api/repositories-api';
import { Notifier } from '../../core/notifier';
import { FAILURE_WORDS } from '../../shared/failure-report';
import { TechnicalDetails } from '../../shared/technical-details';
import { upstreamBodyFacts, upstreamTestFacts, upstreamTestReport } from './upstream-test-report';
import { upstreamVerdictText, upstreamVerdictTone } from './upstream-test-words';

const ICONS = { ok: 'check_circle', info: 'info', warn: 'warning', error: 'error' } as const;

/**
 * The result of **Test connection** (R-UI-082, Q-UI-1226), one component for every place that tests an upstream: a project's remote
 * repository (its settings and the creation flow) and an upstream registry of the admin area. The server's verdict in the reader's
 * language with its tone (Q-UI-1225), what the test learned below it without a click (the address asked, the status and what it
 * means here, the credential sent, the token service and its answer to the login, the duration, the certificate, the proxy and the
 * source address, the reason in words with the code and the server's own sentence, the time), and **Copy report**. A success shows
 * the same facts: a person who tests a connection wants to see what was tested. The content projected into it (Allow <host>) stands
 * under the sentence.
 */
@Component({
  selector: 'quader-upstream-test-result',
  imports: [TechnicalDetails],
  template: `
    <div class="result" [class]="'tone-' + tone()" data-testid="upstream-test-result">
      <p class="verdict">
        <span class="material-icons" aria-hidden="true">{{ icon() }}</span>
        <strong>{{ sentence() }}</strong>
      </p>
      <ng-content />
      <dl class="facts test-facts">
        @for (f of facts(); track $index) {
          <dt>{{ f.label }}</dt>
          @if (f.english) {
            <dd lang="en" translate="no">{{ f.value }}</dd>
          } @else if (f.code) {
            <dd>
              <code translate="no">{{ f.value }}</code
              >@if (f.note) {,&ngsp;<span class="note">{{ f.note }}</span>}
            </dd>
          } @else {
            <dd>
              {{ f.value }}@if (f.note) {,&ngsp;<span class="note">{{ f.note }}</span>}
            </dd>
          }
        }
      </dl>
      <!-- The upstream's own error answer can be long: folded, as text and never as markup (R-UI-216's fold). -->
      @if (result().body; as body) {
        <quader-technical-details>
          <dl class="facts test-facts">
            @for (f of bodyFacts(); track $index) {
              <dt>{{ f.label }}</dt>
              <dd>
                @if (f.code) {
                  <code translate="no">{{ f.value }}</code>
                } @else {
                  {{ f.value }}
                }
              </dd>
            }
            @if (body.text) {
              <dt>{{ words.body }}</dt>
              <dd><pre class="raw" translate="no">{{ body.text }}</pre></dd>
            }
          </dl>
        </quader-technical-details>
      }
      <div class="actions">
        <button class="btn btn-default" type="button" (click)="copyReport()">
          <span class="material-icons" aria-hidden="true">content_copy</span>
          <ng-container i18n="@@error.copy-report">Copy report</ng-container>
        </button>
      </div>
    </div>
  `,
  styles: `
    :host {
      display: block;
    }
    .verdict {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      align-items: start;
      column-gap: var(--quader-space-2);
      margin: 0 0 var(--quader-space-2);
    }
    /* The icon stands on the middle of the sentence's first line (R-UI-175). */
    .verdict .material-icons {
      margin-block: calc((var(--quader-line) - 24px) / 2);
    }
    .tone-ok .verdict .material-icons {
      color: var(--quader-status-ok);
    }
    .tone-info .verdict .material-icons {
      color: var(--quader-text-muted);
    }
    .tone-warn .verdict .material-icons {
      color: var(--quader-status-warn);
    }
    .tone-error .verdict .material-icons {
      color: var(--quader-status-bad);
    }
    .test-facts {
      margin-block: var(--quader-space-2) 0;
    }
    /* An address, a subject and a server's sentence break inside themselves: they never push the page sideways (R-UI-173). */
    code,
    dd {
      overflow-wrap: anywhere;
    }
    .note {
      color: var(--quader-text-muted);
    }
    .raw {
      margin: 0;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 0.8125rem;
    }
    .actions {
      margin-block-start: var(--quader-space-3);
    }
  `,
})
export class UpstreamTestResultView {
  readonly result = input.required<UpstreamTestResult>();
  /** When the answer came: the result's time. */
  readonly at = input<Date>(new Date());

  private readonly locale = inject(LOCALE_ID);
  private readonly notifier = inject(Notifier);

  protected readonly tone = computed(() => upstreamVerdictTone(this.result().verdict));
  protected readonly icon = computed(() => ICONS[this.tone()]);
  protected readonly sentence = computed(() => upstreamVerdictText(this.result().verdict));
  protected readonly facts = computed(() => upstreamTestFacts(this.result(), this.at(), this.locale));
  protected readonly bodyFacts = computed(() => upstreamBodyFacts(this.result(), this.locale));
  protected readonly words = FAILURE_WORDS;

  protected async copyReport(): Promise<void> {
    try {
      await navigator.clipboard.writeText(upstreamTestReport(this.result(), this.at(), this.locale));
      this.notifier.status($localize`:@@copy.done:Copied to the clipboard.`);
    } catch {
      this.notifier.alert($localize`:@@copy.failed:The clipboard is not available. Select the text and copy it.`);
    }
  }
}
