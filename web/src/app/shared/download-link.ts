import { DestroyRef, Directive, ElementRef, LOCALE_ID, inject } from '@angular/core';
import { Router } from '@angular/router';
import { Problem } from '../api/problem';
import { LIMIT_REFUSALS, limitNotice } from '../core/http/problem-messages-limits';
import { Notifier } from '../core/notifier';
import { SIGNED_IN } from '../core/signed-in';

/** What the probe of a download learned: go on with the browser's download, or show the refusal. */
export type ProbeAnswer = { readonly go: true } | { readonly go: false; readonly problem: Partial<Problem> | null; readonly retryAfter: number | null };

/**
 * Asks for the first byte of a download (Q-UI-753): a ranged `GET` passes the same admission as the download (R-LIM-021), so a
 * refusal by the limits arrives before anything is saved; any other answer lets the browser download as before.
 */
export async function probeDownload(href: string, signal: AbortSignal): Promise<ProbeAnswer> {
  let res: Response;
  try {
    res = await fetch(href, { method: 'GET', headers: { Range: 'bytes=0-0' }, credentials: 'same-origin', signal });
  } catch {
    return { go: true };
  }
  if (res.status !== 429) {
    // The answer of the range is one byte; a server that ignores the range sends the whole file, which is not read.
    void res.body?.cancel().catch(() => undefined);
    return { go: true };
  }
  const retry = Number(res.headers.get('Retry-After'));
  let problem: Partial<Problem> | null = null;
  try {
    const body = (await res.json()) as Record<string, unknown>;
    if (typeof body['code'] === 'string') problem = body as Partial<Problem>;
    // npm's shape carries the text as `error`, OCI's as `errors[].message` (R-LIM-020).
    else if (typeof body['error'] === 'string') problem = { detail: body['error'] };
  } catch {
    problem = null;
  }
  return { go: false, problem, retryAfter: Number.isFinite(retry) && retry > 0 ? retry : null };
}

/**
 * A download link whose refusal by the download limits shows a notice instead of a failed download (R-UI-186): a click first asks
 * for one byte; a 429 shows the refusal in words and, for a signed-in caller (`SIGNED_IN`), **See your usage** to `/profile/usage`, and
 * the page stays as it was. Any other answer lets the link download as the browser does. A modified click (a new tab, a save) is left
 * alone.
 */
@Directive({
  selector: 'a[quaderDownload]',
  // A download is no page: a crawler that follows links leaves it alone (R-UI-231), and the robots rules keep it off the package API.
  host: { '(click)': 'click($event)', rel: 'nofollow' },
})
export class DownloadLink {
  private readonly el = inject<ElementRef<HTMLAnchorElement>>(ElementRef);
  private readonly notifier = inject(Notifier);
  /** Whether the reader has a session; an application without sessions binds none (R-UI-223). */
  private readonly signedIn = inject(SIGNED_IN, { optional: true });
  private readonly router = inject(Router);
  private readonly locale = inject(LOCALE_ID);
  private abort: AbortController | null = null;
  /** The second click, which the probe let through to the browser. */
  private passing = false;

  constructor() {
    inject(DestroyRef).onDestroy(() => this.abort?.abort());
  }

  protected click(event: MouseEvent): void {
    if (this.passing) {
      this.passing = false;
      return;
    }
    if (event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
    const href = this.el.nativeElement.href;
    if (!href) return;
    event.preventDefault();
    void this.run(href);
  }

  private async run(href: string): Promise<void> {
    this.abort?.abort();
    const abort = new AbortController();
    this.abort = abort;
    const answer = await probeDownload(href, abort.signal);
    if (abort.signal.aborted) return;
    if (answer.go) {
      this.passing = true;
      this.el.nativeElement.click();
      return;
    }
    const known = answer.problem?.code === undefined || LIMIT_REFUSALS.includes(answer.problem.code);
    const text = known ? limitNotice(answer.problem, answer.retryAfter, this.locale) : (answer.problem?.detail ?? '');
    const action = this.signedIn?.()
      ? { label: $localize`:@@limits.notice.see-usage:See your usage`, run: () => void this.router.navigateByUrl('/profile/usage') }
      : undefined;
    this.notifier.alert(text, action);
  }
}
