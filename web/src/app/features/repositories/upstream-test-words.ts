import { UpstreamTestResult } from '../../api/repositories-api';

/** The code of a failed connection test (R-REPO-064), one of the stable codes of the upstream client. */
export type UpstreamTestErrorCode = NonNullable<UpstreamTestResult['error']>['code'];

/** What the server concluded from a connection test (Q-REPO-488). */
export type UpstreamVerdict = UpstreamTestResult['verdict'];

/**
 * How a verdict stands on a screen: `ok` (green, positive), `info` (the upstream works and nothing is wrong, but nothing was
 * judged), `warn` (it answers, and a setting keeps it from working as meant) and `error`.
 */
export type VerdictTone = 'ok' | 'info' | 'warn' | 'error';

/** The tone of a verdict: the one table every screen of a connection test reads (Q-UI-1225). */
export function upstreamVerdictTone(v: UpstreamVerdict): VerdictTone {
  switch (v) {
    case 'accepted':
    case 'answered':
      return 'ok';
    case 'login_asked':
      return 'info';
    case 'credential_withheld':
    case 'login_required':
    case 'token_service_denied':
      return 'warn';
    default:
      return 'error';
  }
}

/**
 * The server's verdict in the screen's language (Q-REPO-488, Q-UI-1225). The screen says the verdict as it is and never derives
 * one from the status: the 401 of a registry's Bearer challenge is no error when its token service accepted the account.
 */
export function upstreamVerdictText(v: UpstreamVerdict): string {
  switch (v) {
    case 'accepted':
      return $localize`:@@upstream.test.ok-credentials:The upstream answered and accepted the credentials.`;
    case 'answered':
      return $localize`:@@upstream.test.ok:The upstream answered.`;
    case 'login_asked':
      return $localize`:@@upstream.test.login-asked:The registry answered and asks for a login at its token service. No credential was tested.`;
    case 'credential_withheld':
      return $localize`:@@upstream.test.credential-withheld:The registry answered and asks for a login at its token service, which does not receive the credential: the registry is used without it.`;
    case 'refused':
      return $localize`:@@upstream.test.denied:The upstream answered and refused the credentials.`;
    case 'login_required':
      return $localize`:@@upstream.test.login-required:The upstream answered and asks for a login, and no credential is set.`;
    case 'token_service_denied':
      return $localize`:@@upstream.test.token-foreign:The upstream answered, and its login is on a host this repository may not reach.`;
    case 'token_service_failed':
      return $localize`:@@upstream.test.token-failed:The upstream answered, and its token service failed.`;
    case 'upstream_error':
      return $localize`:@@upstream.test.answered-error:The upstream answered with an error.`;
    case 'unreachable':
      return $localize`:@@upstream.test.unreachable:The upstream could not be reached.`;
    case 'not_sent':
      return $localize`:@@upstream.test.not-sent:No request was sent to the upstream.`;
  }
}

/**
 * Why a connection test failed, in the screen's language (R-UI-214). The test's `message` is the client's English diagnosis with
 * the host and the operating system's words; the screen shows it under **Technical details** only.
 */
export function upstreamTestErrorText(code: UpstreamTestErrorCode): string {
  switch (code) {
    case 'dns_error':
      return $localize`:@@upstream.test.error.dns:The upstream's host name could not be resolved.`;
    case 'connection_refused':
      return $localize`:@@upstream.test.error.refused:The upstream's host refused the connection.`;
    case 'timeout':
      return $localize`:@@upstream.test.error.timeout:The upstream did not answer in time.`;
    case 'tls_untrusted':
      return $localize`:@@upstream.test.error.tls:The upstream's certificate is not trusted by this installation.`;
    case 'egress_denied':
      return $localize`:@@upstream.test.error.egress:This installation does not allow connections to the upstream's address.`;
    case 'source_network_unavailable':
      return $localize`:@@upstream.test.error.source-network:The replica that ran the test has no address in the repository's source network.`;
    case 'upstream_secret_unavailable':
      return $localize`:@@upstream.test.error.secret:A secret of the upstream's settings, such as its password, could not be read on this replica. Enter it again.`;
    case 'offline':
      return $localize`:@@upstream.test.error.offline:The repository, or the whole installation, is set to offline, so no upstream is contacted.`;
    case 'too_many_redirects':
      return $localize`:@@upstream.test.error.redirects:The upstream redirected too many times.`;
    case 'insecure_redirect':
      return $localize`:@@upstream.test.error.insecure-redirect:The upstream redirected from https to plain http, which is not followed.`;
    case 'request_failed':
      return $localize`:@@upstream.test.error.request:The request to the upstream failed.`;
    case 'foreign_host_denied':
      return $localize`:@@upstream.test.error.foreign-host:The upstream sends its login to a host this repository may not reach.`;
  }
}
