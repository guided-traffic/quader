import { reasonPhrase } from '../../api/problem';
import { UpstreamTestResult } from '../../api/repositories-api';
import { FAILURE_WORDS, cutNote, timeText } from '../../shared/failure-report';
import { upstreamTestErrorText, upstreamVerdictText } from './upstream-test-words';
import { formatTime } from '../../shared/time-format';

/**
 * What a connection test learned, as the result shows it and **Copy report** copies it (R-UI-216 for a test, Q-UI-1226): every fact
 * of the server's answer, each in the reader's language where it is a word and as code where it is an address, a code or a name.
 * The server's own sentence is English and marked so. Nothing of a credential is in the answer, and nothing is added here.
 */
export interface TestFact {
  /** The term, in the reader's language. */
  readonly label: string;
  readonly value: string;
  /** An address, a code, a name or a status: shown as code and not translated. */
  readonly code?: boolean;
  /** The server's English sentence: `lang="en" translate="no"`. */
  readonly english?: boolean;
  /** What the value means here, in the reader's language, after it. */
  readonly note?: string;
}

const W = {
  address: $localize`:@@upstream.test.fact.address:Address asked`,
  status: $localize`:@@upstream.test.status:HTTP status`,
  noAnswer: $localize`:@@upstream.test.fact.no-answer:No answer`,
  credential: $localize`:@@upstream.test.fact.credential:Credential sent`,
  tokenService: $localize`:@@upstream.test.token-service:Token service`,
  realm: $localize`:@@upstream.test.fact.realm:Login address`,
  service: $localize`:@@upstream.test.fact.service:Service`,
  login: $localize`:@@upstream.test.fact.login:Login`,
  duration: $localize`:@@upstream.test.duration:Duration`,
  certificate: $localize`:@@upstream.test.tls:Certificate`,
  proxy: $localize`:@@upstream.test.fact.proxy:Outbound proxy`,
  source: $localize`:@@upstream.test.fact.source:Source address`,
  reason: $localize`:@@upstream.test.error:Reason`,
  code: $localize`:@@failure.code:Code`,
  serverMessage: $localize`:@@failure.server-message:Server's message`,
  time: $localize`:@@failure.time:Time`,
  size: $localize`:@@upstream.test.fact.size:Size of the answer`,
} as const;

/** "401 Unauthorized", or the bare number of a status RFC 9110 names no reason for. */
export function statusWithReason(status: number): string {
  const reason = reasonPhrase(status);
  return reason ? `${status} ${reason}` : String(status);
}

/** What the probe's status means here (Q-UI-1226): the 401 of a registry's challenge is how a registry answers before a login. */
function statusMeaning(r: UpstreamTestResult): string | undefined {
  const s = r.httpStatus;
  if (s === null) return undefined;
  if (s === 401 && r.tokenService !== null)
    return $localize`:@@upstream.test.meaning.challenge:the registry asks for a login at its token service, as it does before every login`;
  if ((s === 401 || s === 403) && r.credential === 'none') return $localize`:@@upstream.test.meaning.login:the upstream asks for a login`;
  if (s === 401 || s === 403) return $localize`:@@upstream.test.meaning.refused:the upstream refused the credential`;
  if (s === 404) return $localize`:@@upstream.test.meaning.not-found:the upstream has nothing at this address: check the address and the format`;
  if (s >= 500) return $localize`:@@upstream.test.meaning.failed:the upstream failed`;
  if (s >= 400) return $localize`:@@upstream.test.meaning.client:the upstream refused the request`;
  return undefined;
}

function credentialText(c: UpstreamTestResult['credential']): string {
  switch (c) {
    case 'basic':
      return $localize`:@@upstream.test.fact.credential.basic:Username and password`;
    case 'bearer':
      return $localize`:@@upstream.test.fact.credential.bearer:Bearer token`;
    default:
      return $localize`:@@upstream.test.fact.credential.none:None`;
  }
}

/** How the test reached the token service, and whether the credential goes there. */
function tokenServiceNote(ts: NonNullable<UpstreamTestResult['tokenService']>): string {
  if (!ts.allowed) return $localize`:@@upstream.test.token-not-allowed:not allowed for this repository`;
  const via = ts.extraHost
    ? $localize`:@@upstream.test.fact.via-extra:allowed by the further host ${ts.extraHost}:pattern:`
    : $localize`:@@upstream.test.fact.via-origin:the upstream's own host`;
  const cred = ts.receivesCredential
    ? $localize`:@@upstream.test.fact.receives:receives the credential`
    : $localize`:@@upstream.test.fact.receives-not:receives no credential`;
  return `${via}, ${cred}`;
}

/** The token service's answer to the login, or why it was not asked. */
function loginFact(r: UpstreamTestResult, ts: NonNullable<UpstreamTestResult['tokenService']>): TestFact {
  if (ts.httpStatus !== null) {
    const s = ts.httpStatus;
    const note =
      r.authenticated === true
        ? $localize`:@@upstream.test.fact.login.accepted:the account was accepted`
        : r.authenticated === false
          ? $localize`:@@upstream.test.fact.login.refused:the account was refused`
          : $localize`:@@upstream.test.fact.login.no-verdict:no verdict on the account`;
    return { label: W.login, value: statusWithReason(s), code: true, note };
  }
  let why: string;
  if (!ts.allowed) why = $localize`:@@upstream.test.fact.login.not-allowed:Not asked: the repository may not reach the host.`;
  else if (r.credential === 'none') why = $localize`:@@upstream.test.fact.login.no-credential:Not asked: no credential is set.`;
  else if (!ts.receivesCredential) why = $localize`:@@upstream.test.fact.login.withheld:Not asked: the host does not receive the credential.`;
  else why = $localize`:@@upstream.test.fact.login.failed:Asked, and no answer came.`;
  return { label: W.login, value: why };
}

/** The facts of a test's answer, in the order the result shows them. `at` is when the answer came. */
export function upstreamTestFacts(r: UpstreamTestResult, at: Date, locale: string): readonly TestFact[] {
  const facts: TestFact[] = [];
  if (r.probeUrl) facts.push({ label: W.address, value: r.probeUrl, code: true });
  facts.push(r.httpStatus !== null ? { label: W.status, value: statusWithReason(r.httpStatus), code: true, note: statusMeaning(r) } : { label: W.status, value: W.noAnswer });
  facts.push({ label: W.credential, value: credentialText(r.credential) });
  const ts = r.tokenService;
  if (ts) {
    facts.push({ label: W.tokenService, value: ts.host, code: true, note: tokenServiceNote(ts) });
    facts.push({ label: W.realm, value: ts.realm, code: true });
    if (ts.service) facts.push({ label: W.service, value: ts.service, code: true });
    facts.push(loginFact(r, ts));
  }
  if (r.reachable || r.durationMs > 0) facts.push({ label: W.duration, value: `${new Intl.NumberFormat(locale).format(r.durationMs)} ms` });
  if (r.tls) {
    const until = formatTime(r.tls.notAfter, { part: 'date', locale });
    facts.push({
      label: W.certificate,
      value: r.tls.peerSubject,
      code: true,
      note: $localize`:@@upstream.test.fact.valid-until:valid until ${until}:date:`,
    });
  }
  if (r.outboundProxy) facts.push({ label: W.proxy, value: r.outboundProxy, code: true });
  if (r.sourceAddress) facts.push({ label: W.source, value: r.sourceAddress, code: true });
  if (r.error) {
    facts.push({ label: W.reason, value: upstreamTestErrorText(r.error.code) });
    facts.push({ label: W.code, value: r.error.code, code: true });
    facts.push({ label: W.serverMessage, value: r.error.message, english: true });
  }
  facts.push({ label: W.time, value: timeText(at, locale) });
  return facts;
}

/** The upstream's error answer as the result folds it under Technical details: its type and size, and its text. */
export function upstreamBodyFacts(r: UpstreamTestResult, locale: string): readonly TestFact[] {
  const b = r.body;
  if (!b) return [];
  const facts: TestFact[] = [];
  if (b.contentType) facts.push({ label: FAILURE_WORDS.contentType, value: b.contentType, code: true });
  facts.push({ label: W.size, value: `${new Intl.NumberFormat(locale).format(b.bytes)} B` });
  if (b.cut) facts.push({ label: FAILURE_WORDS.body, value: cutNote(new TextEncoder().encode(b.text).length, b.bytes, locale) });
  return facts;
}

/** **Copy report**: the sentence and every fact as plain text, the time in the reader's form and in UTC, then the upstream's answer (Q-UI-1226). */
export function upstreamTestReport(r: UpstreamTestResult, at: Date, locale: string): string {
  const lines = [upstreamVerdictText(r.verdict), ''];
  const line = (f: TestFact): void => {
    const value = f.label === W.time ? `${f.value} (${at.toISOString()})` : f.value;
    lines.push(`${f.label}: ${value}${f.note ? `, ${f.note}` : ''}`);
  };
  for (const f of upstreamTestFacts(r, at, locale)) line(f);
  for (const f of upstreamBodyFacts(r, locale)) line(f);
  if (r.body?.text) lines.push(`${FAILURE_WORDS.body}:`, r.body.text);
  return lines.join('\n');
}
