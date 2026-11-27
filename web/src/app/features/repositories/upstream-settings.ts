import { ValueList, aroundValue } from '../../shared/value-list';
import { Component, LOCALE_ID, computed, inject, input, model, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { upstreamName } from '../../core/upstream-project';
import { ApiError, FieldProblem } from '../../api/problem';
import {
  FormatDescriptor,
  RegistryPresetChoice,
  RepositoriesApi,
  UpstreamPreset,
  UpstreamTestResult,
} from '../../api/repositories-api';
import { ErrorNotice } from '../../shared/error-notice';
import { heldWhileFocused } from '../../shared/held-while-focused';
import { Disclosure } from '../../shared/ui/disclosure';
import { fieldMessage, problemsAt } from '../../shared/field-errors';
import { asApiError } from '../../shared/paged-resource';
import { RepositoryForm } from './repository-model';
import {
  UpstreamAuthMode,
  UpstreamForm,
  UpstreamHost,
  credentialHosts,
  effectiveUrl,
  normalizedUrl,
  shownHosts,
  upstreamDefaultsOf,
  upstreamTestBody,
} from './upstream-model';
import { controlIdOf, numberOrText } from './settings-schema';
import { UpstreamTestResultView } from './upstream-test-result';
import { Loading } from '../../shared/ui/loading';
import { Busy } from '../../shared/ui/busy';

/** The id of the section; a server error at a pointer of the upstream settings that has no control lands here. */
export const UPSTREAM_SECTION_ID = 'f-remote-upstream';

const MEMBER_POINTERS = [
  '/remote/url',
  '/remote/authMode',
  '/remote/username',
  '/remote/password',
  '/remote/extraHosts',
  '/remote/cache/metadataTtlSeconds',
  '/remote/cache/notFoundTtlSeconds',
  '/remote/availability/mode',
  '/remote/downloadOnProbe',
];

/** The pointers whose controls sit in the region Upstream details: an error at one of them opens it (R-UI-083). */
const DETAIL_POINTERS = ['/remote/extraHosts', '/remote/cache', '/remote/availability', '/remote/downloadOnProbe'];

/**
 * The control a field error of the upstream settings belongs to (`/remote/url`, `/remote/password/value`),
 * or null for a pointer that is not theirs (the mode and the mirror members are the Mirror section's).
 */
export function upstreamTarget(pointer: string): string | null {
  if (pointer === '/remote' || pointer.startsWith('/remote/mirror') || pointer === '/remote/mode') {
    return null;
  }
  if (!pointer.startsWith('/remote/')) return null;
  const known = MEMBER_POINTERS.find((m) => pointer === m || pointer.startsWith(`${m}/`));
  return known ? controlIdOf(known) : UPSTREAM_SECTION_ID;
}

/** The label a summary shows for a field error of the upstream settings, or null. */
export function upstreamPointerLabel(pointer: string): string | null {
  if (upstreamTarget(pointer) === null) return null;
  if (pointer.startsWith('/remote/url')) return $localize`:@@upstream.url:Upstream URL`;
  if (pointer.startsWith('/remote/authMode')) return $localize`:@@upstream.auth:Authentication`;
  if (pointer.startsWith('/remote/username')) return $localize`:@@upstream.username:Username`;
  if (pointer.startsWith('/remote/password')) return $localize`:@@upstream.password:Password or token`;
  if (pointer.startsWith('/remote/extraHosts')) return $localize`:@@upstream.hosts:Further hosts`;
  if (pointer.startsWith('/remote/cache/metadataTtlSeconds')) {
    return $localize`:@@upstream.metadata-ttl:Metadata cache period in seconds`;
  }
  if (pointer.startsWith('/remote/cache/notFoundTtlSeconds')) {
    return $localize`:@@upstream.notfound-ttl:Negative cache period in seconds`;
  }
  if (pointer.startsWith('/remote/availability')) return $localize`:@@upstream.offline:Offline`;
  if (pointer.startsWith('/remote/downloadOnProbe')) {
    return $localize`:@@upstream.download-on-probe:Start the download when a version is asked for`;
  }
  return $localize`:@@upstream.heading:Upstream`;
}

/** What the select **Authentication** shows for a value of `remote.authMode`. */
export function upstreamAuthLabel(mode: string): string {
  switch (mode) {
    case 'basic':
      return $localize`:@@upstream.auth.basic:Username and password`;
    case 'bearer':
      return $localize`:@@upstream.auth.bearer:Bearer token`;
    default:
      return $localize`:@@upstream.auth.none:None`;
  }
}

/**
 * The upstream of a remote repository (R-UI-082, R-UI-084, R-UI-085, R-REPO-024, R-REPO-033, R-REPO-062, R-REPO-064): a button per
 * well-known registry of the format, the URL starting with the default one, the credentials (the password is write-only: sent
 * when typed, never read back into the field) with the hosts that receive them named under it, **Test connection** right there,
 * and the closed region **Upstream details** with the further hosts, the cache periods as values and, on the settings page,
 * Offline. A test that finds the registry's token service on a host the repository may not reach offers to allow that host.
 * What the section writes is `upstreamOf` of upstream-model.ts: on a create only what differs from the defaults (Q-UI-80).
 */
@Component({
  selector: 'quader-upstream-settings',
  imports: [Busy, Loading, Disclosure, ErrorNotice, RouterLink, UpstreamTestResultView, ValueList],
  template: `
    <section class="upstream" [id]="sectionId" tabindex="-1" aria-labelledby="upstream-heading">
      <h2 id="upstream-heading" i18n="@@upstream.heading">Upstream</h2>
      <p class="hint" i18n="@@upstream.intro.registry">The registry this repository fetches from.</p>

      <div class="upstream-grid">
        @if (presets().length > 0) {
          <div class="field" role="group" aria-labelledby="upstream-presets-label">
            <span id="upstream-presets-label" class="field-label" i18n="@@upstream.presets">Well-known registries</span>
            <div class="presets">
              @for (p of presets(); track p.id) {
                <button
                  class="btn btn-default preset"
                  type="button"
                  translate="no"
                  [attr.aria-pressed]="pressed(p)"
                  (click)="choose(p)"
                >
                  {{ p.displayName }}
                </button>
              }
            </div>
            <!-- The registry is offered to every project already (R-UI-191, Q-UI-836): one line, and nothing stops the create. -->
            @if (centralOf(); as c) {
              <p class="hint central" data-testid="upstream-central">
                <ng-container i18n="@@wellknown.central">{{ c.displayName }} is offered to every project as <span class="with-stop"><code translate="no">{{ c.name }}</code>.</span> A virtual repository can list it instead of a remote of your own.</ng-container>&ngsp;<a [routerLink]="virtualLink()" [queryParams]="virtualQuery()" i18n="@@wellknown.central.link">New virtual repository</a>
              </p>
            }
          </div>
        }

        <!-- The installation's own registries, after the public ones (R-UI-082, R-REPO-076, Q-UI-907): a button sets the URL, the
             further hosts follow it until they are edited, and no button sets a credential. -->
        @if (own().length > 0) {
          <div class="field" role="group" aria-labelledby="upstream-own-label">
            <span id="upstream-own-label" class="field-label" i18n="@@upstream.own-presets">Registries of this installation</span>
            <div class="presets">
              @for (p of own(); track p.url) {
                <button
                  class="btn btn-default preset"
                  type="button"
                  translate="no"
                  [attr.aria-pressed]="pressed(p)"
                  (click)="choose(p)"
                >
                  {{ p.displayName }}
                </button>
              }
            </div>
          </div>
        }

        <div class="field">
          <label class="field-label" for="f-remote-url" i18n="@@upstream.url">Upstream URL</label>
          <input
            id="f-remote-url"
            class="text-input field-w-full"
            type="text"
            inputmode="url"
            autocomplete="off"
            spellcheck="false"
            [value]="upstream().url"
            [attr.aria-invalid]="urlError() ? 'true' : invalid('/remote/url')"
            aria-describedby="d-remote-url"
            (input)="set({ url: $any($event.target).value })"
            (blur)="leaveUrl()"
          />
          <!-- The hint names the buttons the form offers above the field: the public ones, the installation's, or both (R-UI-082). -->
          <p id="d-remote-url" class="hint">
            @if (presets().length > 0 && own().length > 0) {
              <ng-container i18n="@@upstream.url.hint-both">Choose a well-known registry or one of this installation, or enter the address of another.</ng-container>
            } @else if (presets().length > 0) {
              <ng-container i18n="@@upstream.url.hint-presets">Choose a well-known registry or enter the address of another.</ng-container>
            } @else if (own().length > 0) {
              <ng-container i18n="@@upstream.url.hint-own">Choose a registry of this installation or enter the address of another.</ng-container>
            } @else {
              <ng-container i18n="@@upstream.url.hint-required"
                >This format has no well-known upstream: enter the address of the registry.</ng-container
              >
            }
          </p>
          @if (urlError()) {
            <p class="field-error" i18n="@@upstream.url.required">Enter the address of the registry, such as https://registry.example.com/.</p>
          }
          @for (e of problems('/remote/url'); track $index) {
            <p class="field-error">{{ message(e) }}</p>
          }
        </div>

        <div class="field">
          <label class="field-label" for="f-remote-authMode" i18n="@@upstream.auth">Authentication</label>
          <select
            id="f-remote-authMode"
            class="select-input field-w-name"
            [attr.aria-invalid]="invalid('/remote/authMode')"
            (change)="set({ authMode: $any($event.target).value })"
          >
            @for (m of authModes; track m) {
              <option [value]="m" [selected]="upstream().authMode === m">{{ authLabel(m) }}</option>
            }
          </select>
          @for (e of problems('/remote/authMode'); track $index) {
            <p class="field-error">{{ message(e) }}</p>
          }
        </div>

        @if (upstream().authMode === 'basic') {
          <div class="field">
            <label class="field-label" for="f-remote-username" i18n="@@upstream.username">Username</label>
            <input
              id="f-remote-username"
              class="text-input field-w-name"
              type="text"
              autocomplete="off"
              spellcheck="false"
              [value]="upstream().username"
              [attr.aria-invalid]="invalid('/remote/username')"
              (input)="set({ username: $any($event.target).value })"
            />
            @for (e of problems('/remote/username'); track $index) {
              <p class="field-error">{{ message(e) }}</p>
            }
          </div>
        }

        @if (upstream().authMode !== 'none') {
          <div class="field">
            <label class="field-label" for="f-remote-password" i18n="@@upstream.password">Password or token</label>
            <input
              id="f-remote-password"
              class="text-input field-w-name"
              type="password"
              autocomplete="new-password"
              spellcheck="false"
              [value]="upstream().password"
              [attr.aria-invalid]="invalid('/remote/password')"
              aria-describedby="d-remote-password d-remote-credential-hosts"
              (input)="set({ password: $any($event.target).value })"
            />
            <p id="d-remote-password" class="hint">
              @switch (upstream().passwordStored) {
                @case ('sealed') {
                  <ng-container i18n="@@upstream.password.stored"
                    >A password is stored and is not shown. Leave the field empty to keep it; type one to replace it.</ng-container
                  >
                }
                @case ('reference') {
                  <ng-container i18n="@@upstream.password.reference"
                    >A reference to a secret file or variable is stored. Leave the field empty to keep it; type a password to replace it.</ng-container
                  >
                }
                @default {
                  <ng-container i18n="@@upstream.password.hint"
                    >Sealed when you save, then never shown again. With bearer authentication this is the token.</ng-container
                  >
                }
              }
            </p>
            @if (credentialLine(); as line) {
              <p id="d-remote-credential-hosts" class="hint credential-hosts">{{ line.before }}<quader-value-list [values]="line.hosts" />{{ line.after }}</p>
            } @else {
              <p id="d-remote-credential-hosts" class="hint credential-hosts"></p>
            }
            @if (upstream().passwordStored !== 'none') {
              <label class="check-row" for="f-remote-clearPassword">
                <input
                  id="f-remote-clearPassword"
                  type="checkbox"
                  [checked]="upstream().clearPassword"
                  (change)="set({ clearPassword: $any($event.target).checked })"
                />
                <span i18n="@@upstream.password.clear">Remove the stored password</span>
              </label>
            }
            @for (e of problems('/remote/password'); track $index) {
              <p class="field-error">{{ message(e) }}</p>
            }
          </div>
        }
      </div>

      <div class="test">
        <button class="btn btn-default" type="button" [disabled]="testing()" [quaderBusy]="testing()" (click)="test()">
          <ng-container i18n="@@upstream.test">Test connection</ng-container>
        </button>
        @if (upstream().passwordStored !== 'none' && upstream().password === '') {
          <span class="hint" i18n="@@upstream.test.stored"
            >The test does not use the stored password; type it to test with it.</span
          >
        }
      </div>
      <div class="test-result" role="status" aria-live="polite">
        @if (testing()) {
          <quader-loading [inline]="true" [status]="false" i18n="@@upstream.test.running">Testing the connection.</quader-loading>
        }
        @if (failure(); as f) {
          <quader-error-notice [error]="f" />
        }
        @if (outcome(); as o) {
          <quader-upstream-test-result [result]="o.result" [at]="o.at">
            @if (foreignTokenHost(); as host) {
              <div class="allow">
                <p i18n="@@upstream.test.foreign">
                  The registry asks for a login at <span class="with-stop"><code translate="no">{{ host }}</code>,</span> which this repository may not reach.
                  Allow it as a further host if it is the registry's own token service.
                </p>
                <button class="btn btn-default" type="button" data-testid="upstream-allow-host" (click)="allow(host)">
                  <ng-container i18n="@@upstream.test.allow">Allow</ng-container>&ngsp;<span translate="no">{{ host }}</span>
                </button>
              </div>
            }
          </quader-upstream-test-result>
        }
      </div>

      <quader-disclosure
        class="upstream-details"
        [inline]="true"
        i18n-heading="@@upstream.details"
        heading="Upstream details"
        [expanded]="detailsOpen()"
        (expandedChange)="detailsOpened.set($event)"
      >
        <div class="upstream-grid details">
          <div class="field" role="group" aria-labelledby="upstream-hosts-label" [id]="hostsId">
            <span id="upstream-hosts-label" class="field-label" i18n="@@upstream.hosts">Further hosts</span>
            <p class="hint" i18n="@@upstream.hosts.hint">
              Hosts the upstream's documents and its token service may lead to. Only a host that receives the credential gets it.
            </p>
            @for (h of hosts(); track $index; let i = $index) {
              <div class="host-row">
                <label class="visually-hidden" [for]="'f-remote-extraHosts-' + i" i18n="@@upstream.hosts.pattern">Host pattern</label>
                <input
                  class="text-input field-w-name"
                  type="text"
                  autocomplete="off"
                  spellcheck="false"
                  translate="no"
                  [id]="'f-remote-extraHosts-' + i"
                  [value]="h.pattern"
                  [attr.aria-invalid]="invalid('/remote/extraHosts/' + i)"
                  (input)="setHost(i, { pattern: $any($event.target).value })"
                />
                <label class="check-row" [for]="'f-remote-extraHosts-' + i + '-send'">
                  <input
                    type="checkbox"
                    [id]="'f-remote-extraHosts-' + i + '-send'"
                    [checked]="h.sendCredentials"
                    (change)="setHost(i, { sendCredentials: $any($event.target).checked })"
                  />
                  <span i18n="@@upstream.hosts.send">Receives the credential</span>
                </label>
                <button class="btn btn-tertiary" type="button" (click)="removeHost(i)">
                  <ng-container i18n="@@upstream.hosts.remove">Remove</ng-container><span class="visually-hidden">&ngsp;{{ h.pattern }}</span>
                </button>
              </div>
              @for (e of problems('/remote/extraHosts/' + i); track $index) {
                <p class="field-error">{{ message(e) }}</p>
              }
            } @empty {
              <p class="hint" i18n="@@upstream.hosts.none">No further host.</p>
            }
            <div>
              <button class="btn btn-default" type="button" (click)="addHost()">
                <ng-container i18n="@@upstream.hosts.add">Add host</ng-container>
              </button>
            </div>
            @for (e of problems('/remote/extraHosts'); track $index) {
              <p class="field-error">{{ message(e) }}</p>
            }
          </div>

          <div class="field">
            <label class="field-label" for="f-remote-cache-metadataTtlSeconds" i18n="@@upstream.metadata-ttl"
              >Metadata cache period in seconds</label
            >
            <input
              id="f-remote-cache-metadataTtlSeconds"
              class="text-input field-w-duration"
              type="text"
              inputmode="numeric"
              autocomplete="off"
              spellcheck="false"
              [value]="shown(upstream().metadataTtl, periodDefaults().metadataTtlSeconds)"
              [attr.aria-invalid]="invalid('/remote/cache/metadataTtlSeconds')"
              aria-describedby="d-remote-metadata-ttl"
              (input)="set({ metadataTtl: number($any($event.target).value) })"
              (focus)="periodFocused.set(true)"
              (blur)="periodFocused.set(false); leavePeriod('metadataTtl')"
            />
            <p id="d-remote-metadata-ttl" class="hint" i18n="@@upstream.metadata-ttl.hint-value">
              How long fetched metadata is served without asking the upstream again.
            </p>
            @for (e of problems('/remote/cache/metadataTtlSeconds'); track $index) {
              <p class="field-error">{{ message(e) }}</p>
            }
          </div>

          <div class="field">
            <label class="field-label" for="f-remote-cache-notFoundTtlSeconds" i18n="@@upstream.notfound-ttl"
              >Negative cache period in seconds</label
            >
            <input
              id="f-remote-cache-notFoundTtlSeconds"
              class="text-input field-w-duration"
              type="text"
              inputmode="numeric"
              autocomplete="off"
              spellcheck="false"
              [value]="shown(upstream().notFoundTtl, periodDefaults().notFoundTtlSeconds)"
              [attr.aria-invalid]="invalid('/remote/cache/notFoundTtlSeconds')"
              aria-describedby="d-remote-notfound-ttl"
              (input)="set({ notFoundTtl: number($any($event.target).value) })"
              (focus)="periodFocused.set(true)"
              (blur)="periodFocused.set(false); leavePeriod('notFoundTtl')"
            />
            <p id="d-remote-notfound-ttl" class="hint" i18n="@@upstream.notfound-ttl.hint-value">
              How long a "not found" answer of the upstream is remembered.
            </p>
            @for (e of problems('/remote/cache/notFoundTtlSeconds'); track $index) {
              <p class="field-error">{{ message(e) }}</p>
            }
          </div>

          <div class="field">
            <label class="check-row" for="f-remote-downloadOnProbe">
              <input
                id="f-remote-downloadOnProbe"
                type="checkbox"
                [checked]="upstream().downloadOnProbe"
                [attr.aria-invalid]="invalid('/remote/downloadOnProbe')"
                aria-describedby="d-remote-downloadOnProbe"
                (change)="set({ downloadOnProbe: $any($event.target).checked })"
              />
              <span i18n="@@upstream.download-on-probe">Start the download when a version is asked for</span>
            </label>
            <p id="d-remote-downloadOnProbe" class="hint" i18n="@@upstream.download-on-probe.hint">
              A client that asks for one version without downloading it, such as for an image's manifest, starts its download in the
              background, charged to that client's limits.
            </p>
            @for (e of problems('/remote/downloadOnProbe'); track $index) {
              <p class="field-error">{{ message(e) }}</p>
            }
          </div>

          @if (edit()) {
            <div class="field">
              <label class="check-row" for="f-remote-availability-mode">
                <input
                  id="f-remote-availability-mode"
                  type="checkbox"
                  [checked]="upstream().offline"
                  [attr.aria-invalid]="invalid('/remote/availability/mode')"
                  aria-describedby="d-remote-offline"
                  (change)="set({ offline: $any($event.target).checked })"
                />
                <span i18n="@@upstream.offline">Offline</span>
              </label>
              <p id="d-remote-offline" class="hint" i18n="@@upstream.offline.hint">
                The upstream is not contacted: clients get what is already stored.
              </p>
              @for (e of problems('/remote/availability/mode'); track $index) {
                <p class="field-error">{{ message(e) }}</p>
              }
            </div>
          }
        </div>
      </quader-disclosure>
    </section>
  `,
  styles: `
    .upstream {
      margin-block-start: var(--quader-space-6);
    }
    .upstream > .hint {
      margin-block: 0 var(--quader-space-4);
    }
    .test-result {
      max-inline-size: var(--quader-settings-measure);
    }
    .upstream-grid {
      display: grid;
      grid-template-columns: minmax(0, var(--quader-settings-measure));
      gap: var(--quader-space-4);
    }
    .upstream-grid .field {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
    }
    .upstream-grid.details {
      padding-block: var(--quader-space-2) var(--quader-space-4);
    }
    .presets {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2);
    }
    .central {
      margin: var(--quader-space-2) 0 0;
    }
    .preset[aria-pressed='true'] {
      background: var(--quader-accent-container);
      color: var(--quader-on-accent-container);
      border-color: var(--quader-accent);
      font-weight: 600;
    }
    .host-row {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-1) var(--quader-space-4);
      margin-block-end: var(--quader-space-2);
    }
    .host-row .text-input {
      flex: 0 1 var(--quader-field-width-name);
    }
    .test {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-3);
      margin-block: var(--quader-space-4) var(--quader-space-2);
    }
    .allow {
      display: flex;
      flex-direction: column;
      align-items: flex-start;
      gap: var(--quader-space-2);
      margin-block-end: var(--quader-space-2);
    }
    .allow p {
      margin: 0;
      overflow-wrap: anywhere;
    }
    .allow .btn {
      max-inline-size: 100%;
      white-space: normal;
      overflow-wrap: anywhere;
      text-align: start;
    }
    /* The details are the app's one disclosure, as a fold inside the section (Q-UI-552). */
    .upstream-details {
      margin-block-start: var(--quader-space-3);
    }
  `,
})
export class UpstreamSettings {
  private readonly api = inject(RepositoriesApi);
  private readonly locale = inject(LOCALE_ID);

  /** The form of the repository; the section edits `upstream`. */
  readonly form = model.required<RepositoryForm>();
  readonly format = input.required<FormatDescriptor>();
  /** The project the upstream is tested in: the test runs inside a project (`POST /projects/{project}/upstream-tests`). */
  readonly project = input.required<string>();
  /** Server field problems by pointer (`ApiError.fieldErrors`). */
  readonly fieldErrors = input<ReadonlyMap<string, FieldProblem>>(new Map());
  /** The `repository` document of `getRepositoryDefaults` (R-REPO-063) when the form read it: its remote's periods are the defaults shown. */
  readonly defaults = input<Readonly<Record<string, unknown>> | null>(null);
  /** The settings page of a stored repository: Offline is offered (R-UI-082, R-UI-083). */
  readonly edit = input(false);
  /** The form was sent: a missing URL shows its error whether or not the field was left (R-UI-086). */
  readonly submitted = input(false);
  /**
   * The creation form's: the keys of the format's well-known repositories that are active and that the caller reads (R-UI-191).
   * While the chosen preset's is one of them, a line under the presets names it and leads to a new virtual repository.
   */
  readonly central = input<ReadonlySet<string>>(new Set());
  /**
   * The creation form's: the installation's registry presets of the format (`registryPresets` of R-REPO-063, R-REPO-076), offered as
   * the group **Registries of this installation** after the well-known ones (Q-UI-907). The settings page passes none.
   */
  readonly own = input<readonly RegistryPresetChoice[]>([]);
  /** The router commands and the query of the form of a new virtual repository of the same format. */
  readonly virtualLink = input<readonly string[]>([]);
  readonly virtualQuery = input<Readonly<Record<string, string>>>({});

  protected readonly sectionId = UPSTREAM_SECTION_ID;
  protected readonly hostsId = controlIdOf('/remote/extraHosts');
  protected readonly authModes: readonly UpstreamAuthMode[] = ['none', 'basic', 'bearer'];

  protected readonly testing = signal(false);
  /** The last test's answer and when it came. */
  private readonly result = signal<{ readonly value: UpstreamTestResult; readonly at: Date } | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  /** The user opened or closed Upstream details; a field error inside it opens it whatever this says. */
  protected readonly detailsOpened = signal(false);
  private readonly urlLeft = signal(false);

  protected readonly upstream = computed(() => this.form().upstream);
  protected readonly proposed = computed(() => upstreamDefaultsOf(this.format(), this.remoteDefaults(), this.own()));
  /** A cache period's field has the focus. */
  protected readonly periodFocused = signal(false);
  /** The defaults the period fields show: one that arrives while the person is in one of them waits until it is left (Q-UI-702). */
  protected readonly periodDefaults = heldWhileFocused(this.proposed, this.periodFocused);
  protected readonly presets = computed<readonly UpstreamPreset[]>(() => this.format().upstreamPresets);
  protected readonly hosts = computed(() => shownHosts(this.upstream(), this.proposed()));

  private readonly remoteDefaults = computed(() => {
    const r = this.defaults()?.['remote'];
    return typeof r === 'object' && r !== null ? (r as Readonly<Record<string, unknown>>) : null;
  });

  protected readonly detailsOpen = computed(
    () =>
      this.detailsOpened() ||
      [...this.fieldErrors().keys()].some((p) => DETAIL_POINTERS.some((d) => p === d || p.startsWith(`${d}/`))),
  );

  /** The chosen preset's well-known repository, where it is active and read (R-UI-191); never on the settings page. */
  protected readonly centralOf = computed(() => {
    if (this.edit()) return null;
    const chosen = this.presets().find((p) => this.pressed(p));
    return chosen && this.central().has(chosen.id) ? { displayName: chosen.displayName, name: upstreamName(chosen.id) } : null;
  });

  /** A format without presets needs the URL: once the field was left empty, it says what to type (R-UI-086). */
  protected readonly urlError = computed(
    () => (this.urlLeft() || this.submitted()) && this.presets().length === 0 && this.upstream().url.trim() === '',
  );

  /**
   * "The credential is sent to registry.gitlab.com and gitlab.com." (R-UI-082): the sentence around its hosts, which stand as values
   * of running text (Q-UI-1213).
   */
  protected readonly credentialLine = computed(() => {
    const hosts = credentialHosts(this.upstream(), this.proposed());
    if (hosts.length === 0) return null;
    return { hosts, ...aroundValue((list) => $localize`:@@upstream.credential-hosts:The credential is sent to ${list}:hosts:.`) };
  });

  /** The answer of the last test, shown by the shared result (Q-UI-1226): the server's verdict in words and every fact of it. */
  protected readonly outcome = computed(() => {
    const r = this.result();
    return r ? { result: r.value, at: r.at } : null;
  });

  /** The host of a token service the repository may not reach, which the user may allow (R-UI-082, Q-UI-346). */
  protected readonly foreignTokenHost = computed(() => {
    const r = this.result()?.value;
    return r?.error?.code === 'foreign_host_denied' && r.tokenService && !r.tokenService.allowed ? r.tokenService.host : null;
  });

  /** A preset is pressed while the URL the repository would use is its URL, compared as the server stores URLs (Q-UI-345). */
  protected pressed(p: Pick<UpstreamPreset, 'url'>): boolean {
    const url = effectiveUrl(this.upstream(), this.proposed());
    return url !== null && normalizedUrl(url.trim()) === normalizedUrl(p.url);
  }

  protected choose(p: Pick<UpstreamPreset, 'url'>): void {
    this.set({ url: p.url });
  }

  /** An emptied URL of a format with presets shows the default preset's again when it is left (R-UI-084). */
  protected leaveUrl(): void {
    this.urlLeft.set(true);
    const def = this.proposed().url;
    if (this.upstream().url.trim() === '' && def !== null) this.set({ url: def });
  }

  protected authLabel(mode: UpstreamAuthMode): string {
    return upstreamAuthLabel(mode);
  }

  protected problems(pointer: string): readonly FieldProblem[] {
    return problemsAt(this.fieldErrors(), pointer);
  }

  protected invalid(pointer: string): 'true' | null {
    return this.problems(pointer).length > 0 ? 'true' : null;
  }

  protected message(problem: FieldProblem): string {
    return fieldMessage(problem);
  }

  /**
   * A period shows its default while it holds none: the default is a value in the field (R-UI-084). A field the user empties
   * stays empty while it is edited (the form holds '', which is written as the default) and shows the default again once left.
   */
  protected shown(value: number | string | null, fallback: number | null): string {
    const v = value ?? fallback;
    return v === null ? '' : String(v);
  }

  protected number(text: string): number | string {
    const v = numberOrText(text);
    return v === undefined ? '' : (v as number | string);
  }

  protected leavePeriod(member: 'metadataTtl' | 'notFoundTtl'): void {
    if (this.upstream()[member] === '') this.set({ [member]: null });
  }

  protected setHost(i: number, change: Partial<UpstreamHost>): void {
    this.setHosts(this.hosts().map((h, j) => (j === i ? { ...h, ...change } : h)));
  }

  protected removeHost(i: number): void {
    this.setHosts(this.hosts().filter((_, j) => j !== i));
  }

  protected addHost(): void {
    this.setHosts([...this.hosts(), { pattern: '', sendCredentials: false }]);
  }

  /**
   * Allow a token service the test found on a host the repository may not reach (Q-UI-346): the host becomes a further host,
   * receiving the credential when the repository has one, and the test runs again. The click is the user's consent to the host
   * the upstream named (H-610).
   */
  protected async allow(host: string): Promise<void> {
    const rest = this.hosts().filter((h) => h.pattern.trim().toLowerCase() !== host);
    this.setHosts([...rest, { pattern: host, sendCredentials: this.upstream().authMode !== 'none' }]);
    await this.test();
  }

  private setHosts(hosts: readonly UpstreamHost[]): void {
    this.set({ extraHosts: hosts });
  }

  /** A change of the settings makes an earlier test result stale: it goes. */
  protected set(change: Partial<UpstreamForm>): void {
    this.result.set(null);
    this.failure.set(null);
    this.form.update((f) => ({ ...f, upstream: { ...f.upstream, ...change } }));
  }

  protected async test(): Promise<void> {
    if (this.testing()) return;
    this.testing.set(true);
    this.result.set(null);
    this.failure.set(null);
    try {
      const value = await this.api.testUpstream(this.project(), upstreamTestBody(this.upstream(), this.format(), this.own()));
      this.result.set({ value, at: new Date() });
    } catch (e) {
      this.failure.set(asApiError(e));
    } finally {
      this.testing.set(false);
    }
  }
}
