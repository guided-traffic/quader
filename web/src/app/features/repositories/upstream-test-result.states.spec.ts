import { describe, it, inject as provided } from 'vitest';
import { UpstreamTestResult } from '../../api/repositories-api';
import { sampleUpstreamTestResult } from '../../api/testing/fakes-catalog';
import { expectState, openInPage, stable } from '../../testing/states';
import { UpstreamTestResultView } from './upstream-test-result';

/**
 * The result of Test connection in a browser (Q-UI-1226, R-UI-173, R-UI-175, R-UI-215): the six answers a person meets at a registry,
 * with long values (a deep address, a long certificate subject, a server's sentence, a gateway's error page), measured by the render
 * set's checks at 390 and 1440 px. `UI_STATES_PICTURES=1` adds a picture of each in the dark scheme; the German words are the
 * render set's (the walk of an upstream registry's page, web/e2e/renders/server-texts.ts).
 */
const AT = new Date('2026-11-25T10:42:07Z');
const TLS = { peerSubject: 'CN=*.docker.com,O=Docker Inc.,L=Palo Alto,ST=California,C=US', notAfter: '2027-03-01T00:00:00Z' };
const HUB: Partial<UpstreamTestResult> = { probeUrl: 'https://registry-1.docker.io/v2/', httpStatus: 401, authenticated: null, durationMs: 360, tls: TLS };
const TOKEN = {
  host: 'auth.docker.io',
  allowed: true,
  receivesCredential: true,
  realm: 'https://auth.docker.io/token',
  service: 'registry.docker.io',
  extraHost: 'auth.docker.io',
  httpStatus: null,
};

const states: Readonly<Record<string, Partial<UpstreamTestResult>>> = {
  'test-accepted-token': { ...HUB, verdict: 'accepted', credential: 'basic', authenticated: true, tokenService: { ...TOKEN, httpStatus: 200 } },
  'test-anonymous-token': { ...HUB, verdict: 'login_asked', credential: 'none', tokenService: TOKEN },
  'test-refused': { ...HUB, verdict: 'refused', credential: 'basic', authenticated: false, tokenService: { ...TOKEN, httpStatus: 401 } },
  'test-upstream-error': {
    verdict: 'upstream_error',
    probeUrl: 'https://harbor.platform-engineering.eu-central-1.internal.example.com/v2/',
    credential: 'basic',
    httpStatus: 502,
    authenticated: true,
    durationMs: 1840,
    tls: { peerSubject: 'CN=harbor.platform-engineering.eu-central-1.internal.example.com', notAfter: '2027-01-12T00:00:00Z' },
    body: {
      contentType: 'text/html',
      bytes: 157,
      text: '<html>\r\n<head><title>502 Bad Gateway</title></head>\r\n<body>\r\n<center><h1>502 Bad Gateway</h1></center>\r\n<hr><center>nginx</center>\r\n</body>\r\n</html>\r\n',
      cut: false,
    },
  },
  'test-dns': {
    verdict: 'unreachable',
    probeUrl: 'https://registry.platform-engineering.eu-central-1.internal.example.com/v2/',
    credential: 'none',
    reachable: false,
    httpStatus: null,
    authenticated: null,
    durationMs: 12,
    tls: null,
    error: { code: 'dns_error', message: 'The name registry.platform-engineering.eu-central-1.internal.example.com does not resolve.' },
  },
  'test-token-foreign': {
    ...HUB,
    probeUrl: 'https://registry.gitlab.example-company.com/v2/',
    verdict: 'token_service_denied',
    credential: 'basic',
    tls: { peerSubject: 'CN=registry.gitlab.example-company.com', notAfter: '2027-02-01T00:00:00Z' },
    tokenService: {
      ...TOKEN,
      host: 'gitlab.example-company.com',
      realm: 'https://gitlab.example-company.com/jwt/auth',
      service: 'container_registry',
      allowed: false,
      receivesCredential: false,
      extraHost: null,
    },
    error: {
      code: 'foreign_host_denied',
      message: "The registry's token service is on gitlab.example-company.com, which is neither the upstream nor one of the repository's further hosts; Quader did not ask it.",
    },
  },
};

async function open(over: Partial<UpstreamTestResult>, dark: boolean): Promise<void> {
  if (dark) document.documentElement.setAttribute('data-theme', 'dark');
  else document.documentElement.removeAttribute('data-theme');
  await openInPage(UpstreamTestResultView, { result: sampleUpstreamTestResult(over), at: AT });
  for (const toggle of Array.from(document.querySelectorAll<HTMLButtonElement>('quader-technical-details button.inline-toggle'))) toggle.click();
  await stable();
}

describe('the result of Test connection in a browser (R-UI-082, R-UI-216, R-UI-173, R-UI-175, R-UI-215)', () => {
  for (const [name, over] of Object.entries(states)) {
    it(name, async () => {
      await expectState(name, () => open(over, false));
      if (provided('picturesDir')) await expectState(`${name}-dark`, () => open(over, true));
      document.documentElement.removeAttribute('data-theme');
    });
  }
});
