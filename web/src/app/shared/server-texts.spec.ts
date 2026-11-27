import german from '../../locale/messages.de.xlf' with { loader: 'text' };
import { clearTranslations, loadTranslations } from '@angular/localize';
import { TestBed } from '@angular/core/testing';
import { ciKeyErrorText, ciWarningText } from '../features/ci/ci-common';
import { ciFailureText } from '../features/ci/claim-tester';
import { mirrorErrorText } from '../features/repositories/mirror-status';
import { repositoryWarningText } from '../features/repositories/repository-warnings';
import { previewNoteText } from '../features/repositories/upstream/download-preview-dialog';
import { upstreamTestErrorText } from '../features/repositories/upstream-test-words';
import { jobErrorText } from '../features/system/job-table';
import { valkeyReasonText } from '../features/system/system-status-model';
import { TechnicalDetails } from './technical-details';

/** Every German target of the translation file, its placeholders as `$localize` writes them (`{$NAME}`). */
function germanTranslations(): Record<string, string> {
  const xml = (s: string): string =>
    s
      .replace(/<ph [^>]*equiv="([^"]+)"[^>]*\/>/g, '{$$$1}')
      .replace(/&lt;/g, '<')
      .replace(/&gt;/g, '>')
      .replace(/&quot;/g, '"')
      .replace(/&apos;/g, "'")
      .replace(/&amp;/g, '&');
  const unit = /<unit id="([^"]+)">[\s\S]*?<target>([\s\S]*?)<\/target>/g;
  return Object.fromEntries([...german.matchAll(unit)].map((m) => [m[1]!, xml(m[2]!)]));
}

/** Every unit of the translation file: its id, its English source and its German target, as they stand in the file. */
function germanUnits(): { id: string; source: string; target: string }[] {
  const unit = /<unit id="([^"]+)">[\s\S]*?<source>([\s\S]*?)<\/source>\s*<target>([\s\S]*?)<\/target>/g;
  return [...german.matchAll(unit)].map((m) => ({ id: m[1]!, source: m[2]!, target: m[3]! }));
}

/** What the screens say of every code the server sends, as one list of [code, sentence]. */
function sentences(): [string, string][] {
  const out: [string, string][] = [];
  const add = <C extends string | null>(prefix: string, codes: readonly C[], say: (code: C) => string): void => {
    for (const c of codes) out.push([`${prefix} ${c ?? 'none'}`, say(c)]);
  };
  add(
    'repository warning',
    [
      'member_inactive',
      'no_members',
      'dependency_confusion_unprotected',
      'pattern_unscoped_only',
      'claim_overlaps',
      'deb_upstream_unverified',
      'plain_http_upstream',
      'source_network_single_address',
      'mirror_unbounded',
      'public_populates_remote',
      'public_not_allowed_lowered',
    ],
    (c) => repositoryWarningText({ code: c as never, pointer: '/virtual/members/0', detail: 'the API sentence' }, ['npm-local']),
  );
  add('preview note', ['section_uncounted', 'sizes_unannounced', 'more_versions', 'size_once_stored', 'counted_on_expand'], (c) =>
    previewNoteText(c as never),
  );
  add(
    'connection test',
    [
      'dns_error',
      'connection_refused',
      'timeout',
      'tls_untrusted',
      'egress_denied',
      'source_network_unavailable',
      'upstream_secret_unavailable',
      'offline',
      'too_many_redirects',
      'insecure_redirect',
      'request_failed',
      'foreign_host_denied',
    ],
    (c) => upstreamTestErrorText(c as never),
  );
  add('job error', ['job_failed', 'job_timeout', 'attempts_exhausted', 'cancelled', 'superseded'], (c) => jobErrorText(c as never));
  add(
    'mirror error',
    ['discovery_unavailable', 'discovery_failed', 'discovery_stalled', 'cursor_too_large', 'not_applicable', 'upstream_unavailable', 'internal', null] as const,
    mirrorErrorText,
  );
  add(
    'CI warning',
    [
      'unpinned_condition',
      'repository_template_unconstrained',
      'shared_ownership',
      'sub_pattern',
      'mutable_user_claim',
      'unguarded_template_claim',
      'write_without_pipeline_source',
      'unprotected_refs_allowed',
      'cross_project_read_only',
      'scope_policy_grants_not_checked',
    ],
    (c) => ciWarningText({ code: c as never, names: ['project_id'], detail: 'the API sentence' }),
  );
  add(
    'CI failure',
    [
      'no_matching_mapping',
      'cross_project_source_denied',
      'grant_render_failed',
      'scope_malformed',
      'scope_not_granted',
      'required_claim_missing',
      'token_expired',
      'token_not_yet_valid',
      'subject_token_expires_too_soon',
      'ttl_below_minimum',
      'unknown',
    ],
    ciFailureText,
  );
  add(
    'CI key fetch',
    [
      'egress_denied',
      'timeout',
      'connection_failed',
      'tls_verification_failed',
      'trust_store_unavailable',
      'too_many_redirects',
      'insecure_redirect',
      'http_status status=502 host=gitlab.example.com',
      'http_status host=gitlab.example.com',
      'response_too_large',
      'invalid_response',
      'issuer_mismatch',
      'algorithms_not_offered',
      'no_usable_keys',
      'invalid_url',
      'unknown',
    ],
    ciKeyErrorText,
  );
  add(
    'Valkey reason',
    ['connecting', 'no_sentinel', 'timeout', 'auth_failed', 'config_error', 'unsupported', 'circuit_open', 'probe_failed', 'closed'] as const,
    valkeyReasonText,
  );
  return out;
}

describe('A server text in the screen\'s language (R-UI-214)', () => {
  it('R-UI-214 every code the server sends has a sentence of the screen, and a German one in the German build', () => {
    const english = sentences();
    expect(english.length).toBe(87);
    for (const [code, sentence] of english) {
      expect(sentence, code).not.toBe('');
      expect(sentence, code).not.toContain('the API sentence');
    }
    loadTranslations(germanTranslations());
    try {
      const inGerman = new Map(sentences());
      const untranslated = english.filter(([code, sentence]) => inGerman.get(code) === sentence).map(([code]) => code);
      expect(untranslated).toEqual([]);
      expect(inGerman.get('job error job_failed')).toBe('Der Versuch ist fehlgeschlagen.');
      expect(inGerman.get('CI warning mutable_user_claim')).toBe('Eine Bedingung auf project_id: Benutzer von GitLab können sie ändern.');
    } finally {
      clearTranslations();
    }
  });

  it('R-UI-268 no screen text says the configuration without Helm', () => {
    // A text that marks a value of the chart says "Helm configuration" ("Helm-Konfiguration"), never the configuration, its file or
    // the Helm values. Allowed: a screen's text about a runtime setting or an object's own settings; an allowed unit that leaves the
    // file leaves this list too.
    const allowed = new Set([
      'general.mode.confirm-1', // every change of the runtime configuration
      'pkg.config.held', // an image's configuration
      'pkg.config.hidden',
      'pkg.config.not-read',
      'pkg.config.too-large',
      'pkg.config.invalid',
    ]);
    const english = /\bthe configuration\b|configuration file|Helm values|installation's configuration|system configuration/i;
    const inGerman = /Helm-Values|Helm-Werte|Konfigurationsdatei|Konfiguration der Installation|Systemkonfiguration/;
    const plain = (s: string): string => s.replace(/<[^>]*>/g, ' ').replace(/&apos;/g, "'");
    const units = germanUnits();
    const found = units.filter((u) => !allowed.has(u.id) && (english.test(plain(u.source)) || inGerman.test(plain(u.target)))).map((u) => u.id);
    expect(found).toEqual([]);
    const ids = new Set(units.map((u) => u.id));
    expect([...allowed].filter((id) => !ids.has(id))).toEqual([]);
  });

  it('R-UI-214 technical details are folded, English, not translated, and show the text as it came with its code', async () => {
    const fixture = TestBed.createComponent(TechnicalDetails);
    fixture.componentRef.setInput('text', 'dial tcp: lookup registry.example.org: no such host');
    fixture.componentRef.setInput('code', 'dns_error');
    await fixture.whenStable();
    const root = fixture.nativeElement as HTMLElement;
    const toggle = root.querySelector('button')!;
    expect(toggle.textContent).toContain('Technical details');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    const raw = root.querySelector('[data-testid="technical-details"]')!;
    expect(raw.textContent).toBe('dial tcp: lookup registry.example.org: no such host dns_error');
    expect(raw.querySelector('code')?.textContent).toBe('dns_error');
    expect(raw.getAttribute('lang')).toBe('en');
    expect(raw.getAttribute('translate')).toBe('no');
  });
});
