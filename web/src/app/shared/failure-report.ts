import { ApiError, FailedCall, reasonPhrase } from '../api/problem';
import { describeProblem } from '../core/http/problem-messages';
import { formatTime } from './time-format';

/**
 * What a failure notice shows and copies (R-UI-216): the sentence in the reader's language, the facts of the answer that are
 * visible without a click, and the parts that can be long, folded under **Technical details**. One failure or several of one
 * cause (calls of one view that failed the same way, Q-UI-1185): their calls and request IDs are listed, the rest is the first's.
 */

/** The words of the notice and of its report, in the reader's language. */
export const FAILURE_WORDS = {
  status: $localize`:@@failure.status:Status`,
  noAnswer: $localize`:@@failure.no-answer:No answer`,
  call: $localize`:@@failure.call:Call`,
  calls: $localize`:@@failure.calls:Calls`,
  time: $localize`:@@failure.time:Time`,
  requestId: $localize`:@@failure.request-id:Request ID`,
  code: $localize`:@@failure.code:Code`,
  serverMessage: $localize`:@@failure.server-message:Server's message`,
  retryAfter: $localize`:@@failure.retry-after:Retry after`,
  attempts: $localize`:@@failure.attempts:Attempts`,
  sentTwice: $localize`:@@failure.sent-twice:Sent twice: the page sent the call again by itself after the first failure.`,
  inRow: $localize`:@@failure.in-row:Failed in a row`,
  contentType: $localize`:@@failure.content-type:Content type`,
  headers: $localize`:@@failure.headers:Response headers`,
  body: $localize`:@@failure.body:Body of the answer`,
  fields: $localize`:@@failure.fields:Field problems`,
  type: $localize`:@@failure.type:Type`,
  instance: $localize`:@@failure.instance:Instance`,
  members: $localize`:@@failure.members:Other members`,
  fault: $localize`:@@failure.fault:Error in the page`,
} as const;

/** "Showing the first 4,000 of 12,345 characters." */
export function cutNote(shown: number, whole: number, locale: string): string {
  const n = new Intl.NumberFormat(locale);
  return $localize`:@@failure.body-cut:Showing the first ${n.format(shown)}:shown: of ${n.format(whole)}:whole: characters.`;
}

/** "30 seconds". */
export function secondsText(seconds: number): string {
  return seconds === 1
    ? $localize`:@@failure.one-second:1 second`
    : $localize`:@@failure.seconds:${seconds}:seconds: seconds`;
}

/** "4 times". */
export function timesText(n: number): string {
  return $localize`:@@failure.times:${n}:count: times`;
}

/** The status as the notice names it: "502 Bad Gateway"; null for no answer (status 0) and for a failure of the page. */
export function statusText(error: ApiError): string | null {
  if (error.status === 0) return null;
  const reason = reasonPhrase(error.status);
  return reason ? `${error.status} ${reason}` : String(error.status);
}

/**
 * The time of a failure in the reader's form of a time (R-UI-250), to the second: "25.11.2026 11:42:07 (Wed.)". The copied report adds
 * the time in ISO 8601 with its zone after it, which a ticket's reader in another zone reads alike (Q-UI-1564).
 */
export function timeText(at: Date, locale: string): string {
  return formatTime(at, { locale });
}

/** The server's own sentence: the problem's `detail`, else its `title`; English, as the server wrote it (R-UI-214). */
export function serverMessage(error: ApiError): string | null {
  return error.problem?.detail ?? error.problem?.title ?? null;
}

/** The members of a problem document that the facts or the field problems do not show, as `name: JSON`. */
export function otherMembers(error: ApiError): readonly (readonly [string, string])[] {
  const p = error.problem;
  if (!p) return [];
  const shown = new Set(['type', 'title', 'status', 'detail', 'instance', 'code', 'requestId', 'errors']);
  const out: (readonly [string, string])[] = [];
  for (const [name, value] of Object.entries(p)) {
    if (shown.has(name) || value === undefined) continue;
    let text: string;
    try {
      text = typeof value === 'string' ? value : JSON.stringify(value);
    } catch {
      text = String(value);
    }
    out.push([name, text]);
  }
  return out;
}

/** "GET /api/v1/projects/platform/repositories/npm-local". */
export function callText(call: FailedCall): string {
  return `${call.method} ${call.path}`;
}

/** The calls of a cause, each once, in the order of their notices. */
export function callsOf(errors: readonly ApiError[]): readonly FailedCall[] {
  const seen = new Set<string>();
  const out: FailedCall[] = [];
  for (const e of errors) {
    const c = e.answer.call;
    if (!c || seen.has(callText(c))) continue;
    seen.add(callText(c));
    out.push(c);
  }
  return out;
}

/** The request IDs of a cause, each once. */
export function requestIdsOf(errors: readonly ApiError[]): readonly string[] {
  return [...new Set(errors.map((e) => e.requestId).filter((id): id is string => !!id))];
}

/** The sentence of a failure in the reader's language: the code's, else the status's (R-UI-042, R-UI-216). */
export function failureSentence(error: ApiError): string {
  return describeProblem(error).text;
}

/** Whether the folded part of the notice has anything to show. */
export function hasTechnicalDetails(error: ApiError): boolean {
  const a = error.answer;
  return (
    a.contentType !== null ||
    a.headers.length > 0 ||
    a.body !== null ||
    a.fault !== null ||
    (error.problem?.errors?.length ?? 0) > 0 ||
    !!error.problem?.type ||
    !!error.problem?.instance ||
    otherMembers(error).length > 0
  );
}

/**
 * The whole report of a failure as plain text, for a ticket to an administrator (R-UI-216 Copy report): the sentence, every fact,
 * and the folded part. The time stands in the reader's form and in UTC, which a log is searched by. Nothing in it carries a
 * credential: the call's credential parameters and the credential headers were left out when the error was made (Q-UI-1186).
 */
export function failureReport(errors: readonly ApiError[], locale: string, retryNote: string | null = null): string {
  const first = errors[0];
  if (!first) return '';
  const w = FAILURE_WORDS;
  const lines: string[] = [failureSentence(first)];
  if (retryNote) lines.push(retryNote);
  lines.push('');
  const fact = (term: string, value: string): void => {
    lines.push(`${term}: ${value}`);
  };
  fact(w.status, statusText(first) ?? w.noAnswer);
  const calls = callsOf(errors);
  if (calls.length === 1) fact(w.call, callText(calls[0]!));
  if (calls.length > 1) {
    lines.push(`${w.calls}:`);
    for (const c of calls) lines.push(`  ${callText(c)}`);
  }
  const latest = errors.reduce((a, e) => (e.answer.at > a ? e.answer.at : a), first.answer.at);
  fact(w.time, `${timeText(latest, locale)} (${latest.toISOString()})`);
  const ids = requestIdsOf(errors);
  if (ids.length > 0) fact(w.requestId, ids.join(', '));
  if (first.code) fact(w.code, first.code);
  const message = serverMessage(first);
  if (message) fact(w.serverMessage, message);
  if (first.retryAfterSeconds !== null) fact(w.retryAfter, secondsText(first.retryAfterSeconds));
  if (first.afterRetry) fact(w.attempts, w.sentTwice);
  if (first.answer.inRow > 1) fact(w.inRow, timesText(first.answer.inRow));
  const a = first.answer;
  if (a.fault) fact(w.fault, a.fault);
  if (a.contentType) fact(w.contentType, a.contentType);
  if (first.problem?.type) fact(w.type, first.problem.type);
  if (first.problem?.instance) fact(w.instance, first.problem.instance);
  for (const [name, value] of otherMembers(first)) fact(`${w.members} · ${name}`, value);
  const fields = first.problem?.errors ?? [];
  if (fields.length > 0) {
    lines.push(`${w.fields}:`);
    for (const f of fields) lines.push(`  ${f.pointer || '/'} ${f.code}${f.detail ? `: ${f.detail}` : ''}`);
  }
  if (a.headers.length > 0) {
    lines.push(`${w.headers}:`);
    for (const [name, value] of a.headers) lines.push(`  ${name}: ${value}`);
  }
  if (a.body !== null) {
    lines.push(`${w.body}:`);
    if (a.bodyLength !== null) lines.push(cutNote(a.body.length, a.bodyLength, locale));
    lines.push(a.body);
  }
  return lines.join('\n');
}
