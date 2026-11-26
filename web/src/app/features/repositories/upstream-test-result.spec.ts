import { UpstreamTestResult } from '../../api/repositories-api';
import { sampleUpstreamTestResult } from '../../api/testing/fakes-catalog';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { upstreamTestFacts, upstreamTestReport } from './upstream-test-report';
import { UpstreamTestResultView } from './upstream-test-result';
import { upstreamVerdictText, upstreamVerdictTone } from './upstream-test-words';

const AT = new Date('2026-11-25T10:42:07Z');
const DOCKER = {
  host: 'auth.docker.io',
  allowed: true,
  receivesCredential: true,
  realm: 'https://auth.docker.io/token',
  service: 'registry.docker.io',
  extraHost: 'auth.docker.io',
  httpStatus: null,
} as const;
const HUB = { probeUrl: 'https://registry-1.docker.io/v2/', httpStatus: 401, authenticated: null, tls: { peerSubject: 'CN=*.docker.com', notAfter: '2027-03-01T00:00:00Z' } };
const failed = (code: NonNullable<UpstreamTestResult['error']>['code'], message: string): Partial<UpstreamTestResult> => ({
  reachable: false,
  httpStatus: null,
  authenticated: null,
  tls: null,
  credential: 'none',
  error: { code, message },
});

/** The cases of Q-UI-1226: each with the tone, the sentence's start and the facts it must show. */
const CASES: readonly { name: string; over: Partial<UpstreamTestResult>; tone: string; sentence: string; facts: Record<string, string> }[] = [
  {
    name: 'an anonymous registry answers 200',
    over: { verdict: 'answered', credential: 'none', probeUrl: 'https://registry.npmjs.org/-/ping' },
    tone: 'ok',
    sentence: 'The upstream answered.',
    facts: { 'Address asked': 'https://registry.npmjs.org/-/ping', 'HTTP status': '200 OK', 'Credential sent': 'None', Duration: '184 ms' },
  },
  {
    name: 'a token registry without an account',
    over: { ...HUB, verdict: 'login_asked', credential: 'none', tokenService: { ...DOCKER, receivesCredential: true, extraHost: null } },
    tone: 'info',
    sentence: 'The registry answered and asks for a login',
    facts: {
      'HTTP status': '401 Unauthorized, the registry asks for a login at its token service, as it does before every login',
      'Token service': "auth.docker.io, the upstream's own host, receives the credential",
      'Login address': 'https://auth.docker.io/token',
      Service: 'registry.docker.io',
      Login: 'Not asked: no credential is set.',
      Certificate: 'CN=*.docker.com, valid until 01.03.2027 (Mon.)',
    },
  },
  {
    name: 'a token registry accepts the account (the owner case)',
    over: { ...HUB, verdict: 'accepted', credential: 'basic', authenticated: true, tokenService: { ...DOCKER, httpStatus: 200 } },
    tone: 'ok',
    sentence: 'The upstream answered and accepted the credentials.',
    facts: {
      'Credential sent': 'Username and password',
      'Token service': 'auth.docker.io, allowed by the further host auth.docker.io, receives the credential',
      Login: '200 OK, the account was accepted',
    },
  },
  {
    name: 'a token registry refuses the account',
    over: { ...HUB, verdict: 'refused', credential: 'basic', authenticated: false, tokenService: { ...DOCKER, httpStatus: 401 } },
    tone: 'error',
    sentence: 'The upstream answered and refused the credentials.',
    facts: { Login: '401 Unauthorized, the account was refused' },
  },
  {
    name: 'a token service on a foreign host',
    over: {
      ...HUB,
      verdict: 'token_service_denied',
      credential: 'basic',
      tokenService: { ...DOCKER, host: 'auth.example.com', realm: 'https://auth.example.com/token', allowed: false, receivesCredential: false, extraHost: null },
      error: { code: 'foreign_host_denied', message: "The registry's token service is on auth.example.com; Quader did not ask it." },
    },
    tone: 'warn',
    sentence: 'The upstream answered, and its login is on a host this repository may not reach.',
    facts: {
      'Token service': 'auth.example.com, not allowed for this repository',
      Login: 'Not asked: the repository may not reach the host.',
      Code: 'foreign_host_denied',
      "Server's message": "The registry's token service is on auth.example.com; Quader did not ask it.",
    },
  },
  {
    name: 'a token service whose host receives no credential',
    over: { ...HUB, verdict: 'credential_withheld', credential: 'basic', tokenService: { ...DOCKER, receivesCredential: false } },
    tone: 'warn',
    sentence: 'The registry answered and asks for a login at its token service, which does not receive the credential',
    facts: { Login: 'Not asked: the host does not receive the credential.' },
  },
  {
    name: 'a 404 of the upstream',
    over: { verdict: 'upstream_error', credential: 'none', httpStatus: 404 },
    tone: 'error',
    sentence: 'The upstream answered with an error.',
    facts: { 'HTTP status': '404 Not Found, the upstream has nothing at this address: check the address and the format' },
  },
  {
    name: 'a 500 of the upstream',
    over: { verdict: 'upstream_error', credential: 'basic', httpStatus: 500 },
    tone: 'error',
    sentence: 'The upstream answered with an error.',
    facts: { 'HTTP status': '500 Internal Server Error, the upstream failed' },
  },
  {
    name: 'a name that does not resolve',
    over: { verdict: 'unreachable', ...failed('dns_error', 'The name registry.gitlab.com does not resolve.') },
    tone: 'error',
    sentence: 'The upstream could not be reached.',
    facts: { 'HTTP status': 'No answer', Reason: "The upstream's host name could not be resolved.", Code: 'dns_error' },
  },
  {
    name: 'an untrusted certificate',
    over: { verdict: 'unreachable', ...failed('tls_untrusted', 'The certificate of registry.example.com is not trusted.') },
    tone: 'error',
    sentence: 'The upstream could not be reached.',
    facts: { Reason: "The upstream's certificate is not trusted by this installation.", Code: 'tls_untrusted' },
  },
  {
    name: 'a timeout',
    over: { verdict: 'unreachable', ...failed('timeout', 'The upstream did not answer in time.'), durationMs: 31000 },
    tone: 'error',
    sentence: 'The upstream could not be reached.',
    facts: { Duration: '31,000 ms', Code: 'timeout' },
  },
  {
    name: 'the egress policy refuses the address',
    over: {
      verdict: 'not_sent',
      ...failed('egress_denied', 'The address 10.0.0.5 of registry.internal is refused by the egress address policy (deny_cidrs 10.0.0.0/8).'),
      durationMs: 0,
    },
    tone: 'error',
    sentence: 'No request was sent to the upstream.',
    facts: {
      Reason: "This installation does not allow connections to the upstream's address.",
      "Server's message": 'The address 10.0.0.5 of registry.internal is refused by the egress address policy (deny_cidrs 10.0.0.0/8).',
    },
  },
  {
    name: 'bearer mode, the registry refuses the stored token',
    over: { verdict: 'refused', credential: 'bearer', httpStatus: 401, authenticated: false },
    tone: 'error',
    sentence: 'The upstream answered and refused the credentials.',
    facts: { 'Credential sent': 'Bearer token', 'HTTP status': '401 Unauthorized, the upstream refused the credential' },
  },
];

describe('The result of a connection test (R-UI-082, R-UI-216)', () => {
  it('R-UI-082 R-UI-216 every case says the verdict with its tone and shows what the test learned', () => {
    for (const c of CASES) {
      const r = sampleUpstreamTestResult(c.over);
      expect(upstreamVerdictTone(r.verdict), c.name).toBe(c.tone);
      expect(upstreamVerdictText(r.verdict), c.name).toContain(c.sentence);
      const shown = Object.fromEntries(upstreamTestFacts(r, AT, 'en-US').map((f) => [f.label, f.note ? `${f.value}, ${f.note}` : f.value]));
      for (const [label, value] of Object.entries(c.facts)) expect(shown[label], `${c.name}: ${label}`).toBe(value);
      expect(shown['Time'], c.name).toBeTruthy();
    }
  });

  it('R-UI-082 R-UI-216 the result shows the facts without a click and Copy report copies the sentence, every fact and the time in UTC', async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>(async () => undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    const value = sampleUpstreamTestResult(CASES[2]!.over);
    const r = await renderScreen(UpstreamTestResultView, { inputs: { result: value, at: AT } });
    const root = r.root;
    expect(root.querySelector('.tone-ok')).not.toBeNull();
    expect(text(root.querySelector('.verdict')!)).toBe('check_circleThe upstream answered and accepted the credentials.');
    expect(root.querySelector('quader-disclosure')).toBeNull();
    expect(text(root.querySelector('dl')!)).toContain('auth.docker.io');
    const copy = [...root.querySelectorAll('button')].find((b) => text(b).includes('Copy report'))!;
    copy.click();
    await settle(r.fixture);
    const report = writeText.mock.calls[0]![0];
    expect(report.split('\n')[0]).toBe('The upstream answered and accepted the credentials.');
    expect(report).toContain('Address asked: https://registry-1.docker.io/v2/');
    expect(report).toContain('Login: 200 OK, the account was accepted');
    expect(report).toContain('(2026-11-25T10:42:07.000Z)');
    await expectAccessible(root);
  });

  it("R-UI-082 R-UI-216 an upstream's error answer is folded as text under Technical details and copied with the report", async () => {
    const body = { contentType: 'text/html; charset=utf-8', bytes: 5210, text: '<html><body><h1>502 Bad Gateway</h1>nginx</body></html>', cut: true };
    const value = sampleUpstreamTestResult({ verdict: 'upstream_error', credential: 'none', httpStatus: 502, body });
    const r = await renderScreen(UpstreamTestResultView, { inputs: { result: value, at: AT } });
    const folded = r.root.querySelector('quader-technical-details')!;
    expect(folded).not.toBeNull();
    expect(folded.querySelector('pre')!.textContent).toBe(body.text);
    expect(folded.querySelector('h1')).toBeNull();
    const report = upstreamTestReport(value, AT, 'en-US');
    expect(report).toContain('HTTP status: 502 Bad Gateway, the upstream failed');
    expect(report).toContain('Size of the answer: 5,210 B');
    expect(report.endsWith(body.text)).toBe(true);
    await expectAccessible(r.root);
  });

  it('R-UI-082 R-UI-216 the server\'s sentence is marked as English and the report holds no more than the answer', () => {
    const r = sampleUpstreamTestResult(CASES[4]!.over);
    const message = upstreamTestFacts(r, AT, 'en-US').find((f) => f.label === "Server's message")!;
    expect(message.english).toBe(true);
    const report = upstreamTestReport(r, AT, 'en-US');
    for (const line of report.split('\n').slice(2)) expect(line).toMatch(/^[^:]+: /);
  });
});
