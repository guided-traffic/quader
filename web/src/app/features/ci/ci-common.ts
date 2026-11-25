import { Component, input } from '@angular/core';
import { CiWarning } from '../../api/ci-api';

/**
 * The sentence of a warning of a save or a dry run (R-CI-010, R-CI-011), in the screen's language (R-UI-214): by its code and the
 * claims or scope items it names. The API's English `detail` is for a reader of the API and is not shown.
 */
export function ciWarningText(w: CiWarning): string {
  const names = (w.names ?? []).join(', ');
  switch (w.code) {
    case 'unpinned_condition':
      return $localize`:@@ci.warning.unpinned:The condition pins none of the issuer's immutable ID claims (${names}:names:). A path claim follows renames and transfers, so a project that later takes over a released path matches too.`;
    case 'repository_template_unconstrained':
      return $localize`:@@ci.warning.template-unconstrained:A repositories template has no require filter: every project inside the pin chooses the substituted value through its own path, so it can select any existing repository whose key fits the fixed part.`;
    case 'shared_ownership':
      return $localize`:@@ci.warning.shared-ownership:A grant that writes has the ownership None: every project the mapping matches can publish versions of the names it claims.`;
    case 'sub_pattern':
      return $localize`:@@ci.warning.sub-pattern:A glob or a regular expression on the claim sub: a project can change the parts of sub. Test the separate claims instead (project_id, ref_type, ref).`;
    case 'mutable_user_claim':
      return $localize`:@@ci.warning.mutable-user-claim:A condition on ${names}:claim:: users of GitLab can change it.`;
    case 'unguarded_template_claim':
      return $localize`:@@ci.warning.unguarded-claim:The template uses the claim ${names}:claim:, which the issuer does not require and no top-level present condition guards; a token without it fails the exchange.`;
    case 'write_without_pipeline_source':
      return $localize`:@@ci.warning.no-pipeline-source:The mapping writes without a condition on pipeline_source. Allow push, web and schedule; merge requests, external pull requests and triggers should not write.`;
    case 'unprotected_refs_allowed':
      return $localize`:@@ci.warning.unprotected-refs:The mapping may write from unprotected refs: anyone who can push a branch that matches the condition gets its grants.`;
    case 'cross_project_read_only':
      return $localize`:@@ci.warning.cross-project:Pipelines of another project (a fork) get this mapping's grants reduced to reading; the code of a fork can read everything the token can read.`;
    case 'scope_policy_grants_not_checked':
      return $localize`:@@ci.warning.scope-not-checked:The scope asks for ${names}:items:, which only a listed policy or group can give; the dry run reads no policy and took them as held.`;
  }
}

/**
 * Why the last fetch of an issuer's keys failed, in the screen's words (R-CI-003, R-UI-214). The stored line is the failure class,
 * the HTTP status and the host (`http_status status=502 host=gitlab.example.com`); the screen says the class and shows the line
 * as a technical detail.
 */
export function ciKeyErrorText(line: string): string {
  const [failureClass, ...facts] = line.split(' ');
  const status = facts.find((f) => f.startsWith('status='))?.slice('status='.length);
  switch (failureClass) {
    case 'egress_denied':
      return $localize`:@@ci.keys.error.egress:This installation does not allow connections to the issuer's address.`;
    case 'timeout':
      return $localize`:@@ci.keys.error.timeout:The issuer did not answer in time.`;
    case 'connection_failed':
      return $localize`:@@ci.keys.error.connection:The connection to the issuer failed.`;
    case 'tls_verification_failed':
      return $localize`:@@ci.keys.error.tls:The issuer's certificate is not trusted by this installation.`;
    case 'trust_store_unavailable':
      return $localize`:@@ci.keys.error.trust-store:The certificates this installation trusts could not be read.`;
    case 'too_many_redirects':
      return $localize`:@@ci.keys.error.redirects:The issuer redirected too many times.`;
    case 'insecure_redirect':
      return $localize`:@@ci.keys.error.insecure-redirect:The issuer redirected from https to plain http, which is not followed.`;
    case 'http_status':
      return status
        ? $localize`:@@ci.keys.error.status-code:The issuer answered with the error status ${status}:status:.`
        : $localize`:@@ci.keys.error.status:The issuer answered with an error status.`;
    case 'response_too_large':
      return $localize`:@@ci.keys.error.too-large:The issuer's answer is larger than allowed.`;
    case 'invalid_response':
      return $localize`:@@ci.keys.error.invalid:The issuer's answer cannot be read.`;
    case 'issuer_mismatch':
      return $localize`:@@ci.keys.error.issuer-mismatch:The issuer's discovery document names another issuer.`;
    case 'algorithms_not_offered':
      return $localize`:@@ci.keys.error.algorithms:The issuer offers none of the signing algorithms allowed here.`;
    case 'no_usable_keys':
      return $localize`:@@ci.keys.error.no-keys:The issuer's key set holds no key that can be used.`;
    case 'invalid_url':
      return $localize`:@@ci.keys.error.url:The address of the issuer's keys is not valid.`;
    default:
      return $localize`:@@ci.keys.error.other:Fetching the keys failed.`;
  }
}

/** The warnings a save or a dry run returned (R-CI-010, R-CI-011, R-CI-019): each with an icon, its sentence and the member it concerns. */
@Component({
  selector: 'quader-ci-warnings',
  template: `
    @if (warnings().length > 0) {
      <section class="warnings" role="status" aria-labelledby="ci-warnings-title">
        <h2 id="ci-warnings-title">{{ heading() }}</h2>
        <ul>
          @for (w of warnings(); track w.code + (w.pointer ?? '')) {
            <li>
              <span class="material-icons inline" aria-hidden="true">warning</span>
              <span
                >{{ text(w) }}
                @if (w.pointer) {
                  <span class="muted">(<code translate="no">{{ w.pointer }}</code>)</span>
                }
              </span>
            </li>
          }
        </ul>
      </section>
    }
  `,
  styles: `
    .warnings {
      margin-block: var(--quader-space-3);
      padding: var(--quader-space-3) var(--quader-space-4);
      border: 1px solid var(--quader-status-warn);
      border-radius: var(--quader-radius-m);
    }
    h2 {
      margin: 0 0 var(--quader-space-2);
      font: var(--quader-type-title-small);
    }
    ul {
      margin: 0;
      padding: 0;
      list-style: none;
    }
    /* The icon stands on the middle of the warning's first line (R-UI-175), the sentence wraps beside it. */
    li {
      display: flex;
      align-items: flex-start;
      color: var(--quader-on-surface);
    }
    .material-icons {
      flex: none;
      color: var(--quader-status-warn);
    }
    .muted {
      color: var(--quader-text-muted);
    }
  `,
})
export class CiWarnings {
  readonly warnings = input.required<readonly CiWarning[]>();
  /** The heading: a save's by default; the claim tester's dry run saves nothing and says so. */
  readonly heading = input($localize`:@@ci.warnings.title:Saved, with warnings`);
  protected readonly text = ciWarningText;
}

/** The router commands of the CI screens, which exist once for the system tier and once under a project (Q-CI-26). */
export const ciRoutes = {
  issuer: (issuer: string): string[] => ['/admin/ci', issuer],
  mapping: (project: string | null, issuer: string, name: string | null): string[] =>
    project
      ? ['/projects', project, 'ci', 'mappings', name ?? 'new']
      : ['/admin/ci', issuer, 'mappings', name ?? 'new'],
  /** A project's mapping routes carry the issuer in the query; the system routes carry it in the path. */
  mappingQuery: (project: string | null, issuer: string): Record<string, string> | null =>
    project && issuer ? { issuer } : null,
  test: (project: string | null, issuer: string): string[] =>
    project ? ['/projects', project, 'ci', 'test'] : ['/admin/ci', issuer, 'test'],
  overview: (project: string | null, issuer: string | null): string[] =>
    project ? ['/projects', project, 'ci'] : issuer ? ['/admin/ci', issuer] : ['/admin/ci'],
};

/** The changed top-level members of a write body against the stored record: what the conflict dialog compares. */
export function changedMembers(input: object, stored: object): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const a = input as Record<string, unknown>;
  const b = stored as Record<string, unknown>;
  for (const k of Object.keys(a)) {
    if (JSON.stringify(a[k]) !== JSON.stringify(b[k] ?? null) && !(a[k] === undefined && b[k] === undefined)) out[k] = a[k];
  }
  return out;
}

/**
 * The body of a full replace (PUT) after "Overwrite": the stored record as it is now, with only the members
 * the user changed taken from the form. A member the user did not touch keeps the value another administrator
 * gave it in between (a removed `deploy`, a removed audience, a switch turned on), so the overwrite replaces
 * exactly what the dialog showed (R-UI-042, R-CI-002). `omittedWhenEmpty` names list members the form leaves
 * out when empty; one the user did not empty is taken from the stored record as well.
 */
export function overwriteBody<B extends object>(
  body: B,
  patch: Readonly<Record<string, unknown>>,
  current: object,
  loaded: object,
  omittedWhenEmpty: readonly string[] = [],
): B {
  const now = current as Record<string, unknown>;
  const before = loaded as Record<string, unknown>;
  const mine = body as Record<string, unknown>;
  const out: Record<string, unknown> = {};
  for (const k of Object.keys(mine)) out[k] = k in patch ? patch[k] : k in now ? now[k] : mine[k];
  for (const k of omittedWhenEmpty) {
    const userEmptied = Array.isArray(before[k]) && before[k].length > 0;
    if (!(k in mine) && !userEmptied && Array.isArray(now[k]) && now[k].length > 0) out[k] = now[k];
  }
  return out as B;
}
