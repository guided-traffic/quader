import german from '../../../../locale/messages.de.xlf' with { loader: 'text' };
import { clearTranslations, loadTranslations } from '@angular/localize';
import { downloadFailure } from '../../../api/testing/fakes-remote-downloads';
import { RemoteDownloadFailure } from '../../../api/remote-downloads-api';
import { DOWNLOAD_FAILURE_CLASSES } from '../../../api/remote-downloads-api';
import { failureSentence, versionRangeText } from './download-words';

// The sentence of a failure is the screen's, written from its code and its members, never the server's detail (R-CUR-011, R-CUR-018).
// Unit tier, milliseconds: pure functions over a failure of a few hundred bytes.

const DETAIL = 'The server says this in English.';

function says(over: Partial<RemoteDownloadFailure>): string {
  return failureSentence(downloadFailure({ address: null, target: null, status: null, retryAfter: null, detail: DETAIL, ...over }));
}

describe('The sentence of a download failure', () => {
  it('R-CUR-011 an answer of the upstream: a host it named and Quader refused is the target, a redirect to http, a document it cannot use', () => {
    expect(
      says({ class: 'upstream_unexpected', code: 'foreign_host_denied', address: 'https://registry.npmjs.org/x', target: 'https://replicate.registry.example.com/x/-/x-1.tgz' }),
    ).toBe(
      "The upstream points to replicate.registry.example.com, which is neither the upstream nor one of the repository's further hosts, so Quader sent no request there. If you trust this host, add it to the repository's further hosts.",
    );
    expect(says({ class: 'upstream_unexpected', code: 'foreign_host_denied' })).toContain('points to a host that is neither');
    expect(says({ class: 'upstream_unexpected', code: 'insecure_redirect', target: 'http://registry.example.org:8080/x' })).toContain(
      'plain http on registry.example.org:8080',
    );
    expect(says({ class: 'upstream_unexpected', code: 'upstream_unexpected_status' })).toContain('a status Quader does not expect');
    expect(says({ class: 'upstream_unexpected', code: 'too_many_redirects' })).toContain('redirected the request more often');
    expect(says({ class: 'upstream_unexpected', code: 'upstream_package_mismatch' })).toContain('another package');
    expect(says({ class: 'upstream_unexpected', code: 'npm_invalid_upstream_document' })).toBe(says({ class: 'upstream_unexpected', code: 'nuget_invalid_upstream_document' }));
  });

  it('R-CUR-011 the way to the upstream: the time it took, the network a replica sends from, the credential; the host of TLS and egress', () => {
    expect(says({ class: 'network', code: 'upstream_timeout' })).toContain('did not answer in time');
    expect(says({ class: 'network', code: 'source_network_unavailable' })).toContain('another replica may have one');
    expect(says({ class: 'network', code: 'upstream_secret_unavailable' })).toContain('enters it again');
    expect(says({ class: 'network', code: 'upstream_unreachable' })).toContain('could not reach the upstream');
    expect(says({ class: 'tls', code: 'tls_untrusted', address: 'https://registry.example.com:8443/x?sig=s' })).toBe(
      'This installation does not trust the certificate of registry.example.com:8443. An administrator adds the certificate authority that signed it.',
    );
    expect(says({ class: 'egress_denied', code: 'egress_denied', address: 'https://10.0.0.1/x' })).toContain('requests to 10.0.0.1.');
    expect(says({ class: 'egress_denied', code: 'egress_denied' })).toContain("requests to the upstream's address");
  });

  it('R-CUR-011 what came does not hold, and the limits: digest, size, media type, the gate, the run and a bulk', () => {
    expect(says({ class: 'integrity', code: 'upstream_integrity_mismatch' })).toContain('does not match the digest');
    expect(says({ class: 'integrity', code: 'upstream_digest_missing' })).toContain('announces no digest');
    expect(says({ class: 'content_refused', code: 'upstream_content_type_blocked' })).toContain('a page in place of the file');
    expect(says({ class: 'bound', code: 'npm_too_large' })).toBe(says({ class: 'bound', code: 'upstream_item_too_large' }));
    expect(says({ class: 'bound', code: 'download_timeout' })).toContain('the next attempt goes on from there');
    expect(says({ class: 'gate_refused', code: 'vulnerability_blocked' })).toContain('A vulnerability finding blocks this version');
    expect(says({ class: 'gate_refused', code: 'populate_not_permitted' })).toContain('not bring new content into its cache');
    expect(says({ class: 'bound', code: 'bulk_max_versions' })).toContain('as many versions as its limit allows');
    expect(says({ class: 'bound', code: 'bulk_max_bytes' })).toContain('announced sizes');
    expect(says({ class: 'bound', code: 'bulk_stored_bytes' })).toContain('stored as much as its size limit');
    expect(says({ class: 'upstream_unexpected', code: 'catalogue_not_offered' })).toContain('lists no sub-sections');
    expect(says({ class: 'upstream_unexpected', code: 'catalogue_unavailable' })).toContain('cannot read the upstream');
  });

  it("R-CUR-011 Quader's own state, the wait the upstream asked for, and the class's sentence for a code the screen does not know", () => {
    expect(says({ class: 'circuit_open', code: 'not_cached_offline' })).toContain('The repository is offline');
    expect(says({ class: 'storage', code: 'spool_busy' })).toContain('waits for room');
    expect(says({ class: 'upstream_rate_limited', code: 'preempted' })).toContain("A client's fetch took");
    expect(says({ class: 'upstream_rate_limited', code: 'upstream_rate_limited', retryAfter: 240 })).toContain('asked to wait 240 s');
    expect(says({ class: 'upstream_rate_limited', code: 'upstream_rate_limited' })).not.toContain('asked to wait');
    expect(says({ class: 'upstream_error', code: 'a_code_of_tomorrow' })).toBe('The upstream answered with an error of its own. A later attempt may succeed.');
    for (const c of DOWNLOAD_FAILURE_CLASSES) {
      const s = says({ class: c, code: 'a_code_of_tomorrow' });
      expect(s.length, c).toBeGreaterThan(20);
      expect(s, c).not.toContain(DETAIL);
    }
  });

  it('R-CUR-018 the German build says the sentence in German, with the host in its place', () => {
    const xml = (s: string): string =>
      s.replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&apos;/g, "'").replace(/&amp;/g, '&');
    const unit = /<unit id="(downloads\.why\.[^"]+)">[\s\S]*?<target>([\s\S]*?)<\/target>/g;
    const ph = (s: string): string => s.replace(/<ph id="\d+" equiv="([^"]+)"[^>]*\/>/g, '{$$$1}');
    const translations = Object.fromEntries([...german.matchAll(unit)].map((m) => [m[1]!, xml(ph(m[2]!))]));
    expect(Object.keys(translations)).toHaveLength(48);
    loadTranslations(translations);
    try {
      expect(
        says({ class: 'upstream_unexpected', code: 'foreign_host_denied', target: 'https://hub.docker.com/v2/namespaces/library/repositories/nginx/tags' }),
      ).toBe(
        'Der Upstream verweist auf hub.docker.com, der weder der Upstream noch einer der weiteren Hosts des Repositorys ist; Quader hat dorthin keine Anfrage gesendet. Wenn Sie diesem Host vertrauen, tragen Sie ihn bei den weiteren Hosts des Repositorys ein.',
      );
      expect(says({ class: 'upstream_rate_limited', code: 'upstream_rate_limited', retryAfter: 90 })).toBe(
        'Der Upstream begrenzt, wie oft Quader ihn fragen darf, und bat, 90 s zu warten; der nächste Versuch wartet so lange.',
      );
      expect(says({ class: 'bound', code: 'bulk_max_versions' })).toBe(
        'Der Sammeldownload hat so viele Versionen eingereiht, wie seine Grenze erlaubt, und reiht keine weiteren ein.',
      );
    } finally {
      clearTranslations();
    }
    expect(says({ class: 'bound', code: 'bulk_max_versions' })).toBe('The bulk download queued as many versions as its limit allows and adds no more.');
  });
});

describe('The range of a bulk in words', () => {
  it('R-CUR-010 a bulk says what it was asked for by version: both bounds, each alone, included or below, none', () => {
    const r = (fromVersion: string | null, toVersion: string | null, toInclusive = true) => versionRangeText({ fromVersion, toVersion, toInclusive });
    expect(r('5.0.0', '5.4.7')).toBe('From 5.0.0 up to 5.4.7');
    expect(r('5.0.0', '6.0.0', false)).toBe('From 5.0.0, below 6.0.0');
    expect(r('5.0.0', null)).toBe('From 5.0.0');
    expect(r(null, '5.4.7')).toBe('Up to 5.4.7');
    expect(r(null, '6.0.0', false)).toBe('Below 6.0.0');
    expect(r(null, null)).toBeNull();
  });
});
