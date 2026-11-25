import { UpstreamTestResult } from '../../api/repositories-api';

/** The code of a failed connection test (R-REPO-064), one of the stable codes of the upstream client. */
export type UpstreamTestErrorCode = NonNullable<UpstreamTestResult['error']>['code'];

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
