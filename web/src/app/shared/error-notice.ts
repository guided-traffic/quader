import {
  Component,
  DestroyRef,
  ElementRef,
  EventEmitter,
  LOCALE_ID,
  OnInit,
  Output,
  afterNextRender,
  computed,
  effect,
  inject,
  input,
  signal,
  untracked,
} from '@angular/core';
import { ApiError } from '../api/problem';
import { LIVE_FAST_MS, LIVE_MAX_BACKOFF_MS } from '../core/live-refresh';
import { Notifier } from '../core/notifier';
import { PageVisibility } from '../core/page-visibility';
import { CopyButton } from './copy-button';
import { FailureGroups } from './failure-groups';
import {
  FAILURE_WORDS,
  callText,
  callsOf,
  cutNote,
  failureReport,
  failureSentence,
  hasTechnicalDetails,
  otherMembers,
  requestIdsOf,
  secondsText,
  serverMessage,
  statusText,
  timeText,
  timesText,
} from './failure-report';
import { TechnicalDetails } from './technical-details';

/** A write may be offered again by the person (never by itself) after these: no answer, or a gateway's or a starting server's. */
const RETRY_WRITE = new Set([0, 502, 503, 504]);

/**
 * A failure that may pass by itself: no answer, a server's or a gateway's failure, a timeout, a rate limit, an answer that could
 * not be read. A refusal of the request (403, 404, 422) does not pass by reading again, and a failure of the page's own code repeats.
 */
export function passes(error: ApiError): boolean {
  if (error.answer.fault !== null && !error.answer.unreadable) return false;
  const s = error.status;
  return s === 0 || s >= 500 || s === 408 || s === 429 || error.answer.unreadable;
}

/**
 * The wait before a notice reads its view again by itself (R-UI-216, Q-UI-1184): 3 s after the first failure of the call, twice as
 * long after each further one, up to 5 minutes (the back-off of R-UI-213), and never less than the answer's `Retry-After`.
 */
export function retryWaitMs(error: ApiError): number {
  const backoff = Math.min(LIVE_FAST_MS * 2 ** Math.max(0, error.answer.inRow - 1), LIVE_MAX_BACKOFF_MS);
  return Math.max(backoff, (error.retryAfterSeconds ?? 0) * 1000);
}

/**
 * Renders a failed API call (R-UI-042, R-UI-216): a sentence in the reader's language for every failure, the facts of the answer
 * visible below it (its status, the call, the time, the request ID with Copy, the code, the server's own sentence, Retry-After, a
 * call sent twice, the failures in a row), what can be long folded under **Technical details** (the body of an answer that is no
 * problem document as text, its content type, its headers, the field problems, the problem's other members), **Copy report** for
 * the whole of it as plain text, and **Retry** for every failed read and for a write that got no answer or a gateway's.
 *
 * A failed read whose failure may pass reads its view again by itself while the window is seen, after 3 s, 6 s, 12 s … up to 5
 * minutes, and says so; a write is never repeated by itself. Notices of one cause in one place are one notice (`FailureGroups`):
 * the first names every call and retries every view; the others show nothing.
 */
@Component({
  selector: 'quader-error-notice',
  imports: [CopyButton, TechnicalDetails],
  host: { '[attr.hidden]': 'follower() ? "" : null' },
  template: `
    @if (!follower()) {
      <div class="notice" data-testid="error-notice">
        <span class="material-icons" aria-hidden="true">error</span>
        <div class="body">
          <!-- The sentence alone is the alert: the facts change at every try (the time, the count), which must not be announced. -->
          <p class="text" role="alert">{{ sentence() }}</p>
          @if (retryNote(); as note) {
            <p class="retry-note">{{ note }}</p>
          }
          <dl class="facts failure-facts">
            <dt>{{ words.status }}</dt>
            <dd>
              @if (status(); as s) {
                <code translate="no">{{ s }}</code>
              } @else {
                {{ words.noAnswer }}
              }
            </dd>
            @if (calls().length > 0) {
              <dt>{{ calls().length > 1 ? words.calls : words.call }}</dt>
              <dd>
                @for (c of calls(); track c) {
                  <code class="call" translate="no">{{ c }}</code>
                }
              </dd>
            }
            <dt>{{ words.time }}</dt>
            <dd><time [attr.datetime]="at().toISOString()">{{ time() }}</time></dd>
            @if (requestIds().length > 0) {
              <dt>{{ words.requestId }}</dt>
              <dd>
                @for (id of requestIds(); track id) {
                  <span class="request">
                    <code translate="no">{{ id }}</code>
                    <quader-copy-button [value]="id" [label]="copyIdLabel" [quiet]="true" />
                  </span>
                }
              </dd>
            }
            @if (error().code; as code) {
              <dt>{{ words.code }}</dt>
              <dd><code translate="no">{{ code }}</code></dd>
            }
            @if (serverText(); as text) {
              <dt>{{ words.serverMessage }}</dt>
              <dd lang="en" translate="no">{{ text }}</dd>
            }
            @if (retryAfterText(); as wait) {
              <dt>{{ words.retryAfter }}</dt>
              <dd>{{ wait }}</dd>
            }
            @if (error().afterRetry) {
              <dt>{{ words.attempts }}</dt>
              <dd>{{ words.sentTwice }}</dd>
            }
            @if (error().answer.inRow > 1) {
              <dt>{{ words.inRow }}</dt>
              <dd>{{ inRowText() }}</dd>
            }
          </dl>
          @if (details()) {
            <quader-technical-details>
              <dl class="facts failure-details">
                @if (error().answer.fault; as fault) {
                  <dt>{{ words.fault }}</dt>
                  <dd><code lang="en" translate="no">{{ fault }}</code></dd>
                }
                @if (error().answer.contentType; as type) {
                  <dt>{{ words.contentType }}</dt>
                  <dd><code translate="no">{{ type }}</code></dd>
                }
                @if (error().problem?.type; as type) {
                  <dt>{{ words.type }}</dt>
                  <dd><code translate="no">{{ type }}</code></dd>
                }
                @if (error().problem?.instance; as instance) {
                  <dt>{{ words.instance }}</dt>
                  <dd><code translate="no">{{ instance }}</code></dd>
                }
                @if (fields().length > 0) {
                  <dt>{{ words.fields }}</dt>
                  <dd>
                    <ul class="fields">
                      @for (f of fields(); track $index) {
                        <li>
                          <code translate="no">{{ f.pointer || '/' }}</code>&ngsp;<code translate="no">{{ f.code }}</code>
                          @if (f.detail) {
                            &ngsp;<span lang="en" translate="no">{{ f.detail }}</span>
                          }
                        </li>
                      }
                    </ul>
                  </dd>
                }
                @for (m of members(); track m[0]) {
                  <dt>{{ words.members }} · <code translate="no">{{ m[0] }}</code></dt>
                  <dd><code translate="no">{{ m[1] }}</code></dd>
                }
                @if (error().answer.headers.length > 0) {
                  <dt>{{ words.headers }}</dt>
                  <dd>
                    <pre class="raw" translate="no">{{ headerText() }}</pre>
                  </dd>
                }
                @if (error().answer.body; as body) {
                  <dt>{{ words.body }}</dt>
                  <dd>
                    @if (cut(); as note) {
                      <p class="cut">{{ note }}</p>
                    }
                    <!-- The body as text, never as markup: a gateway's error page is shown as it was sent (Q-UI-1186). -->
                    <pre class="raw" translate="no">{{ body }}</pre>
                  </dd>
                }
              </dl>
            </quader-technical-details>
          }
          <div class="actions">
            @if (retryable()) {
              <button class="btn btn-default" type="button" (click)="retryAll()" i18n="@@error.retry">Retry</button>
            }
            <button class="btn btn-default" type="button" (click)="copyReport()">
              <span class="material-icons" aria-hidden="true">content_copy</span>
              <ng-container i18n="@@error.copy-report">Copy report</ng-container>
            </button>
          </div>
        </div>
      </div>
    }
  `,
  styles: `
    .notice {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-3);
      padding: var(--quader-space-3) var(--quader-space-4);
      border: 1px solid var(--quader-error);
      border-radius: var(--quader-radius-m);
      background: var(--quader-error-container);
      color: var(--quader-on-error-container);
    }
    /* The icon stands on the middle of the message's first line (R-UI-175): 24 px on a line of 21 px. */
    .notice > .material-icons {
      margin-block: calc((var(--quader-line) - 24px) / 2);
    }
    .body {
      flex: 1 1 auto;
      min-inline-size: 0;
    }
    p {
      margin: 0;
    }
    .text {
      font-weight: 600;
    }
    .retry-note,
    .cut {
      margin-block-start: var(--quader-space-1);
    }
    .failure-facts {
      margin-block-start: var(--quader-space-2);
    }
    /* A path, a request ID, a header and a body break inside themselves where the line ends: they never push the page sideways
       (R-UI-173), and a body grows the notice instead of scrolling by itself (R-UI-215). */
    code,
    .raw {
      overflow-wrap: anywhere;
    }
    .raw {
      margin: 0;
      white-space: pre-wrap;
      font-size: 0.8125rem;
    }
    .call {
      display: block;
    }
    .request {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-1);
    }
    .request code {
      min-inline-size: 0;
    }
    /* The copy button stands on the middle of the request ID's line (R-UI-175). */
    .request quader-copy-button {
      flex: none;
      margin-block: calc((var(--quader-line) - 2rem) / 2);
    }
    .fields {
      margin: 0;
      padding-inline-start: var(--quader-space-4);
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2);
      margin-block-start: var(--quader-space-3);
    }
    quader-technical-details {
      margin-block-start: var(--quader-space-2);
    }
  `,
})
export class ErrorNotice implements OnInit {
  readonly error = input.required<ApiError>();
  /**
   * Reads the notice's view again: by the person's **Retry**, and by itself after a failed read that may pass. A decorated output
   * of an `EventEmitter`, whose `observed` tells whether the screen bound it: without a binding there is nothing to read again, and
   * no **Retry** is offered.
   */
  @Output() readonly retry = new EventEmitter<void>();

  private readonly locale = inject(LOCALE_ID);
  private readonly notifier = inject(Notifier);
  private readonly groups = inject(FailureGroups);
  private readonly visibility = inject(PageVisibility);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
  private readonly id = this.groups.newId();
  private readonly group = this.groups.groupOf(this.id);

  protected readonly words = FAILURE_WORDS;
  protected readonly copyIdLabel = $localize`:@@error.copy-request-id:Copy request ID`;
  /** The screen bound `(retry)`: set once the bindings exist. */
  private readonly bound = signal(false);
  /**
   * A read again runs, started by the notice itself or by the person's Retry: the notice stays in place and says so until its view
   * answers or fails again (Q-UI-1197).
   */
  private readonly trying = signal(false);

  /** Another notice of the group stands first: this one shows nothing. */
  protected readonly follower = computed(() => {
    const g = this.group();
    return g.length > 1 && g[0]?.id !== this.id;
  });
  /** The failures this notice reports: the group's, or its own. */
  private readonly errors = computed(() => {
    const g = this.group();
    return g.length > 1 ? g.map((m) => m.error()) : [this.error()];
  });

  /** The group's failures as such: another notice that joins or leaves the page elsewhere changes nothing here. */
  private readonly failures = computed(() => this.errors(), { equal: (a, b) => a.length === b.length && a.every((e, i) => e === b[i]) });

  protected readonly sentence = computed(() => failureSentence(this.error()));
  protected readonly status = computed(() => statusText(this.error()));
  protected readonly calls = computed(() => callsOf(this.errors()).map(callText));
  protected readonly requestIds = computed(() => requestIdsOf(this.errors()));
  protected readonly at = computed(() => this.errors().reduce((a, e) => (e.answer.at > a ? e.answer.at : a), this.error().answer.at));
  protected readonly time = computed(() => timeText(this.at(), this.locale));
  protected readonly serverText = computed(() => serverMessage(this.error()));
  protected readonly inRowText = computed(() => timesText(this.error().answer.inRow));
  protected readonly retryAfterText = computed(() => {
    const s = this.error().retryAfterSeconds;
    return s === null ? null : secondsText(s);
  });
  protected readonly details = computed(() => hasTechnicalDetails(this.error()));
  protected readonly fields = computed(() => this.error().problem?.errors ?? []);
  protected readonly members = computed(() => otherMembers(this.error()));
  protected readonly headerText = computed(() => this.error().answer.headers.map(([n, v]) => `${n}: ${v}`).join('\n'));
  protected readonly cut = computed(() => {
    const a = this.error().answer;
    return a.body !== null && a.bodyLength !== null ? cutNote(a.body.length, a.bodyLength, this.locale) : null;
  });

  /** The failures of the group whose views may be read again: every read, a write only after no answer or a gateway's. */
  private readonly retried = computed(() => this.errors().filter((e) => e.isRead || e.answer.fault !== null || RETRY_WRITE.has(e.status)));
  protected readonly retryable = computed(() => {
    const g = this.group();
    const own = this.bound() && (this.error().isRead || this.error().answer.fault !== null || RETRY_WRITE.has(this.error().status));
    return own || g.some((m) => m.id !== this.id && m.retry !== null);
  });
  /** The notice reads its view again by itself: a failed read that may pass, with a view to read (Q-UI-1184). */
  private readonly automatic = computed(() => this.retryable() && this.error().isRead && passes(this.error()));
  protected readonly retryNote = computed(() => {
    if (this.trying()) return $localize`:@@error.trying:Trying again.`;
    if (!this.automatic()) return null;
    return $localize`:@@error.tries-again:This page tries again by itself in ${secondsText(Math.round(retryWaitMs(this.error()) / 1000))}:wait:.`;
  });

  constructor() {
    afterNextRender(() =>
      this.groups.join({ id: this.id, host: this.host, error: this.error, retry: this.bound() ? () => this.retry.emit() : null }),
    );
    inject(DestroyRef).onDestroy(() => this.groups.leave(this.id));
    // A new failure of the group ends the attempt that led to it; so does a view of the group that answered and left it.
    effect(() => {
      this.failures();
      untracked(() => this.trying.set(false));
    });
    // The read again by itself, while the window is seen; at once when it is seen again after it was hidden (R-UI-213).
    effect((onCleanup) => {
      const error = this.error();
      if (!this.automatic() || this.follower()) return;
      if (!this.visibility.visible()) {
        untracked(() => (this.away = true));
        return;
      }
      const wait = untracked(() => (this.away ? 0 : retryWaitMs(error)));
      this.away = false;
      const timer = setTimeout(() => this.retryAll(true), wait);
      onCleanup(() => clearTimeout(timer));
    });
  }

  private away = false;

  ngOnInit(): void {
    this.bound.set(this.retry.observed);
  }

  /** Reads every view of the group again: by the person's Retry, or by itself (`auto`), which repeats reads only. */
  protected retryAll(auto = false): void {
    this.trying.set(true);
    const group = this.group();
    if (group.length === 0) {
      if (this.bound()) this.retry.emit();
      return;
    }
    const due = new Set(this.retried());
    for (const m of group) {
      if (!m.retry || !due.has(m.error()) || (auto && !m.error().isRead)) continue;
      m.retry();
    }
  }

  protected async copyReport(): Promise<void> {
    const report = failureReport(this.errors(), this.locale, this.retryNote());
    try {
      await navigator.clipboard.writeText(report);
      this.notifier.status($localize`:@@copy.done:Copied to the clipboard.`);
    } catch {
      this.notifier.alert($localize`:@@copy.failed:The clipboard is not available. Select the text and copy it.`);
    }
  }
}
