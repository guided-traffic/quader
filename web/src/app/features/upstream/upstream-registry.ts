import { NgTemplateOutlet } from '@angular/common';
import { TimePipe } from '../../shared/time-pipe';
import { failureMayPass } from '../../shared/none-on-refusal';
import { Tooltip } from '../../shared/ui/tooltip';
import { classHelp, isSystemOnly } from '../repositories/repository-class';
import { Component, LOCALE_ID, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { ApiError } from '../../api/problem';
import { RepositoriesApi, Repository, UpstreamTestResult, isConsumerView, repoRef } from '../../api/repositories-api';
import {
  UpstreamApi,
  UpstreamRepository,
  UpstreamRepositoryAccount,
  UpstreamRepositoryAddress,
  UpstreamVisibilityLevel,
} from '../../api/upstream-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { UPSTREAM_PROJECT, systemWork } from '../../core/upstream-project';
import { SelectField, SelectOption, TextField } from '../../shared/access/fields';
import { Confirm } from '../../shared/confirm-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatIconView } from '../../shared/format-icon';
import { asApiError, failureOf, valueOf } from '../../shared/paged-resource';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Disclosure } from '../../shared/ui/disclosure';
import { PageHeader } from '../../shared/ui/page-header';
import { Switch } from '../../shared/ui/switch';
import { VisibilityIcon, VisibilityLevel, VisibilityLevelControl, installationReasons, levelName } from '../../shared/ui/visibility-level';
import { AddressField } from '../../shared/address-field';
import { CodeWindow } from '../../shared/code/code-window';
import { DOWNLOAD_SCRIPT, FRESHCLAM_CONF, FRESHCLAM_DROP_IN, freshclamCertificate, freshclamLines, plainDownload } from './clamav-setup';
import { MalwareApi } from '../../api/malware-api';
import { ScannerList, SystemApi } from '../../api/system-api';
import { ScannerReplicas } from '../../shared/scanner/scanner-replicas';
import { scannerGroup } from '../../shared/scanner/scanner-model';
import { groupedFingerprint } from '../repositories/deb-signing-keys';
import { UpstreamTestResultView } from '../repositories/upstream-test-result';
import { AccountAgainAnswer, AccountAgainData, AccountAgainDialog } from './account-again-dialog';
import { ValueList, aroundValue } from '../../shared/value-list';
import { AuthMode, HostRow, authOptions, credentialHostsOf, hostOf, sameHosts } from './upstream-account';
import { UpstreamActions } from './upstream-actions';
import {
  accountFilesText,
  accountFilesWarning,
  accountRaisesText,
  clientsLevelNotes,
  clientsLoweredWarning,
  clientsSentence,
  clientsSwitchLabel,
  clientsWord,
  levelOptions,
  onOffWord,
  privateRowNote,
  levelSetText as levelSetTextOf,
  retiredSentence,
  rowBadge,
  switchLabel,
  systemUseSentence,
} from './upstream-words';
import { Loading } from '../../shared/ui/loading';
import { Busy } from '../../shared/ui/busy';

/** The answer carries the problem of a stored account whose hosts the write would move (R-REPO-069). */
function needsAccount(e: ApiError): boolean {
  return e.code === 'secret_value_required' || [...e.fieldErrors.values()].some((f) => f.code === 'secret_value_required');
}

/**
 * The page of one well-known repository in the admin area (R-UI-188, Q-UI-831): its header with **Open repository**, then one
 * column of sections, the style guide's settings page. **State and visibility**, **Address** and **Account** are open; **Details**
 * is closed. The address and the account are written with the loaded `ETag` (R-REPO-073) and each has its own save; the state
 * and the level are saved at once, with the questions of the list (`UpstreamActions`). A retired repository shows only who still
 * lists it; a system repository (`scanner-trivy`) shows its use and its fixed level as text. A caller without
 * `repositories:write-config` reads every value and changes none.
 */
@Component({
  selector: 'quader-upstream-registry',
  imports: [Busy, Loading, 
    AddressField,
    CodeWindow,
    TimePipe,
    Disclosure,
    ErrorNotice,
    FormatIconView,
    NgTemplateOutlet,
    PageHeader,
    RouterLink,
    ScannerReplicas,
    SelectField,
    Switch,
    TextField,
    Tooltip,
    UpstreamTestResultView,
    ValueList,
    VisibilityIcon,
    VisibilityLevelControl,
  ],
  templateUrl: './upstream-registry.html',
  styleUrls: ['./upstream.scss', './upstream-registry.scss'],
})
export class UpstreamRegistry {
  private readonly api = inject(UpstreamApi);
  private readonly repositories = inject(RepositoriesApi);
  private readonly actions = inject(UpstreamActions);
  private readonly session = inject(SessionStore);
  private readonly notifier = inject(Notifier);
  private readonly confirm = inject(Confirm);
  private readonly dialog = inject(QuaderDialog);
  private readonly locale = inject(LOCALE_ID);
  private readonly systemApi = inject(SystemApi);
  private readonly malwareApi = inject(MalwareApi);

  /** The route parameter `:key`, the repository's key in the project `upstream`. */
  readonly key = input.required<string>();

  protected readonly item = signal<UpstreamRepository | null>(null);
  private etag = '';
  protected readonly loadError = signal<ApiError | null>(null);
  protected readonly stateError = signal<ApiError | null>(null);
  protected readonly addressError = signal<ApiError | null>(null);
  protected readonly accountError = signal<ApiError | null>(null);
  protected readonly busy = signal<'state' | 'level' | 'address' | 'reset' | 'account' | 'remove' | 'clients' | null>(null);
  protected readonly levelError = signal<string | null>(null);

  protected readonly canWrite = computed(() => this.session.has('repositories:write-config'));
  protected readonly system = computed(() => this.item()?.kind === 'system');
  /** scanner-trivy, whose registry must serve the image scanning's database image (R-UI-188). */
  protected readonly imageScanning = computed(() => this.system() && systemWork(this.item()?.key) === 'image-scanning');
  /** A repository with no address and no account an administrator sets: clamav, a local repository (R-REPO-075, Q-REPO-412). */
  protected readonly noAddress = computed(() => this.item()?.fixed.includes('address') ?? false);
  protected readonly systemUse = systemUseSentence;
  protected readonly retired = computed(() => this.item()?.state === 'retired');
  protected readonly retiredText = retiredSentence();
  protected readonly projectLink = computed(() => ['/projects', UPSTREAM_PROJECT, 'repositories', this.key()]);
  protected readonly settingsLink = computed(() => [...this.projectLink(), 'settings']);
  protected readonly levelText = levelName;
  protected readonly levels = levelOptions();
  protected readonly word = onOffWord;
  protected readonly badge = rowBadge;
  protected readonly switchName = switchLabel;
  protected readonly privateNote = privateRowNote();
  protected readonly authModes: readonly SelectOption[] = authOptions();
  private readonly choices = resource({ loader: () => this.actions.choices() });
  protected readonly reasons = computed(() => installationReasons(valueOf(this.choices) ?? null));
  private readonly formats = resource({ loader: () => this.repositories.formats() });
  /** A repository of a system-only format is a system repository, not a local one, by the catalog's fact (Q-UI-1203). */
  protected readonly systemOnly = computed(() => isSystemOnly(this.item()?.format, valueOf(this.formats)));
  protected readonly systemTip = classHelp('system');
  protected readonly formatLabel = computed(() => {
    const f = this.item()?.format ?? '';
    return (valueOf(this.formats) ?? []).find((x) => x.name === f)?.displayName ?? f;
  });

  // ---- ClamAV definition updates (R-UI-188 for clamav, Q-UI-903) ----

  /** `clamav`, the one repository with the switch **ClamAV definition updates** (R-REPO-075): its first section is the switch's. */
  protected readonly clamav = computed(() => this.item()?.clients != null);
  protected readonly clientsOn = computed(() => this.item()?.clients?.enabled === true);
  protected readonly clientsLabel = clientsSwitchLabel();
  protected readonly clientsText = clientsSentence();
  protected readonly clientsWord = clientsWord;
  /**
   * The addresses of `upstream/clamav` on the endpoints that admit client certificates (`clients.certificateAddresses`, R-CLAMAV-010):
   * read-only lines of the fold **Visibility** and the certificate's variant of the setup (Q-UI-905); empty while none does.
   */
  protected readonly certificateAddresses = computed(() => this.item()?.clients?.certificateAddresses ?? []);
  protected readonly clientsNotes = computed(() => clientsLevelNotes(this.certificateAddresses().length > 0));
  protected readonly loweredText = clientsLoweredWarning();
  /** The installation allows the level Public (R-API-076); unknown counts as allowed, the server decides. */
  protected readonly publicAllowed = computed(() => valueOf(this.choices)?.publicAvailable !== false);
  /** On below Public where Public cannot be chosen: the switch lowered it (`public_not_allowed_lowered`, R-REPO-075). */
  protected readonly lowered = computed(() => this.clientsOn() && this.item()?.visibility !== 'public' && !this.publicAllowed());
  /** The level chosen in the fold **Visibility**, saved by its button. */
  protected readonly clientsLevel = signal<VisibilityLevel>('public');
  protected readonly levelFoldOpen = signal(false);
  /** The one line of freshclam.conf, without and with a token (R-CLAMAV-006), from the address the item names. */
  protected readonly freshclam = computed(() => freshclamLines(this.item()?.clients?.address ?? ''));
  /** The way with a client certificate, on the first endpoint that admits one (R-CLAMAV-006); empty while none does. */
  protected readonly freshclamCert = computed(() => {
    const first = this.certificateAddresses()[0];
    return first ? freshclamCertificate(first) : null;
  });
  protected readonly freshclamConf = FRESHCLAM_CONF;
  protected readonly freshclamDropIn = FRESHCLAM_DROP_IN;
  protected readonly downloadScript = DOWNLOAD_SCRIPT;
  /** The plain download for an installation without freshclam (Q-CLAMAV-14): without a credential at Public, with a token below it. */
  protected readonly download = computed(() =>
    plainDownload(this.item()?.clients?.address ?? '', this.item()?.visibility === 'public' ? 'public' : 'token'),
  );
  /** The plain download with a client certificate, on the first endpoint that admits one; empty while none does. */
  protected readonly downloadCert = computed(() => {
    const first = this.certificateAddresses()[0];
    return first ? plainDownload(first, 'certificate') : '';
  });
  protected readonly withoutHeading = $localize`:@@clamav.setup.without:Without freshclam`;
  protected readonly certAddressLabel = $localize`:@@clamav.cert.address:Address of the endpoint for client certificates`;
  protected readonly copyCertAddressLabel = $localize`:@@clamav.cert.address.copy:Copy the address of the endpoint for client certificates`;
  protected readonly copyScriptLabel = $localize`:@@clamav.setup.copy-script:Copy the script`;
  protected readonly copySetupLabel = $localize`:@@clamav.setup.copy-certificate:Copy the setup`;
  protected readonly addressLabel = $localize`:@@clamav.address:Address of the endpoint`;
  protected readonly copyAddressLabel = $localize`:@@clamav.address.copy:Copy the address of the endpoint`;
  protected readonly copyLineLabel = $localize`:@@clamav.setup.copy:Copy the line`;
  protected readonly authHeading = $localize`:@@clamav.setup.authenticate:Authenticate`;
  protected readonly levelHeading = $localize`:@@repo.visibility.heading:Visibility`;
  // ---- Updater (R-UI-188 section 2 for clamav, Q-UI-913) ----

  /**
   * The scanner replicas, read for the updater's rows on the page of `clamav` only; a refusal leaves the sentence why there are none.
   * A failure that may pass is shown in the section and read again (R-UI-216): swallowed, the page said "No updater reports".
   */
  private readonly scanners = resource({
    params: () => (this.clamav() ? true : undefined),
    loader: async (): Promise<ScannerList | null> => {
      try {
        return await this.systemApi.scanners();
      } catch (e) {
        if (failureMayPass(e)) throw e;
        return null;
      }
    },
  });
  /** The malware scanning's switch and source, for the sentence of a page without an updater's row. */
  private readonly malware = resource({
    params: () => (this.clamav() ? true : undefined),
    loader: async () => {
      try {
        return (await this.malwareApi.settings()).settings;
      } catch (e) {
        if (failureMayPass(e)) throw e;
        return null;
      }
    },
  });
  /** The failure of the updater's reads that may pass, kept while its notice reads them again (R-UI-216). */
  protected readonly updaterFailure = failureOf([this.scanners, this.malware]);
  /** The list, where the updater has rows: the section shows them in the shared table of the scanners. */
  protected readonly updaterRows = computed(() => {
    const list = valueOf(this.scanners);
    return list && (scannerGroup(list, 'freshclam')?.rows.length ?? 0) > 0 ? list : null;
  });
  /** Why no updater's row stands (R-UI-188): the definitions come from a repository, the scanning is off, or no updater reports. */
  protected readonly updaterNote = computed((): string | null => {
    if (!this.scanners.hasValue() || !this.malware.hasValue()) return null;
    const m = valueOf(this.malware);
    if (m && m.database.source === 'repository') {
      const repo = m.database.sourceRepository ?? '';
      return $localize`:@@clamav.updater.repository:No updater runs: this installation takes its definitions from ${repo}:repository:.`;
    }
    if (m && !m.enabled) return $localize`:@@clamav.updater.off:The malware scan is off, so the definitions are not updated.`;
    return $localize`:@@clamav.updater.none:No updater reports: the definitions are not updated.`;
  });

  /** The repository's page, which lists its databases and their versions (R-UI-194). */
  protected readonly versionsLink = computed(() => this.projectLink());
  protected readonly setupLink = computed(() => [...this.projectLink(), 'setup']);

  // ---- Address (R-UI-188 section 2) ----

  protected readonly url = signal('');
  protected readonly hosts = signal<readonly HostRow[]>([]);
  /** An armored block that replaces the archive keys of an APT archive; empty keeps them. */
  protected readonly keys = signal('');
  protected readonly keysOpen = signal(false);
  protected readonly addressDirty = computed(() => {
    const r = this.item();
    if (!r) return false;
    return (
      this.url().trim() !== r.address.url ||
      JSON.stringify(this.hosts()) !== JSON.stringify(r.address.extraHosts.map(row)) ||
      this.keys().trim() !== ''
    );
  });
  protected readonly productUrl = computed(() => this.item()?.address.product.url ?? '');
  protected readonly archiveKeys = computed(() => this.item()?.address.upstreamKeys ?? null);

  // ---- Account (R-UI-188 section 3) ----

  protected readonly authMode = signal<AuthMode>('none');
  protected readonly username = signal('');
  protected readonly password = signal('');
  protected readonly accountStored = computed(() => this.item()?.account.source !== 'configuration' && (this.item()?.account.authMode ?? 'none') !== 'none');
  /**
   * The account comes from the installation's configuration (R-REPO-077, Q-UI-1161): read-only, no fields, no Save, no Remove; the
   * declaration's Secret, the state of its files on the server that answered, and Test connection.
   */
  protected readonly configured = computed(() => this.item()?.account.source === 'configuration');
  /** The Secret of the Helm values the declared account's files come from; empty for a configuration written without the chart. */
  protected readonly secretName = computed(() => this.item()?.account.configuration?.secretName ?? '');
  protected readonly filesText = computed(() => accountFilesText(this.item()?.account.configuration?.files ?? 'readable'));
  protected readonly filesWarning = computed(() => {
    const r = this.item();
    const c = r?.account.configuration;
    return r && c ? accountFilesWarning(c.files, r.displayName, c.secretName !== '') : null;
  });
  /** The mode of a declared account in the words of the form's choices. */
  protected readonly modeLabel = computed(() => this.authModes.find((o) => o.value === this.item()?.account.authMode)?.label ?? '');
  /** "Docker Hub allows more requests with an account." while the registry has none (Q-UI-1160). */
  protected readonly raisesText = computed(() => {
    const r = this.item();
    return r?.accountRaisesLimit ? accountRaisesText(r.displayName) : null;
  });
  /** The refusal of an address that would send a declared account to other hosts, said before anything is sent (Q-REPO-486). */
  protected readonly addressHeld = signal<string | null>(null);
  protected readonly passwordStored = computed(() => this.item()?.account.password != null);
  protected readonly accountDirty = computed(() => {
    const a = this.item()?.account;
    if (!a) return false;
    return this.authMode() !== a.authMode || (this.authMode() === 'basic' && this.username() !== (a.username ?? '')) || this.password() !== '';
  });
  /**
   * "The credential is sent to registry-1.docker.io and auth.docker.io." (R-UI-082): the sentence around its hosts, which stand as
   * values of running text (Q-UI-1213).
   */
  protected readonly credentialLine = computed(() => {
    if (this.authMode() === 'none') return null;
    const hosts = credentialHostsOf(this.url(), this.hosts());
    return { hosts, ...aroundValue((list) => $localize`:@@upstream.credential-hosts:The credential is sent to ${list}:hosts:.`) };
  });
  protected readonly testing = signal(false);
  /** The last test's answer and when it came, shown by the shared result (Q-UI-1226). */
  protected readonly testResult = signal<{ readonly value: UpstreamTestResult; readonly at: Date } | null>(null);
  protected readonly testError = signal<ApiError | null>(null);
  // ---- Details (R-UI-188 section 4) ----

  protected readonly detailsOpen = signal(false);
  /** The repository's document, for its dates: read when Details is first opened. */
  protected readonly document = resource({
    params: () => (this.detailsOpen() ? this.key() : undefined),
    loader: async ({ params }): Promise<Repository | null> => {
      const read = await this.repositories.get(repoRef(UPSTREAM_PROJECT, params));
      return isConsumerView(read.value) ? null : read.value;
    },
  });
  /** The document's dates where the read answered; a refused read leaves them out and nothing else. */
  protected readonly documentValue = computed(() => valueOf(this.document) ?? null);

  constructor() {
    // The key is the route's parameter, bound after the page is made: the page reads it whenever it is set.
    effect(() => {
      const key = this.key();
      untracked(() => void this.load(key));
    });
  }

  /**
   * Reads the registry; the notice's Retry (R-UI-216). The failure stays until the answer, so the notice stays in place while it reads
   * again; the page's other reads that failed are read with it (the level choices, the formats' names, the updater's reads, the
   * dates of Details), so the page that comes back is the page of a fresh load.
   */
  protected async load(key = this.key()): Promise<void> {
    for (const r of [this.choices, this.formats, this.scanners, this.malware, this.document]) if (r.error()) r.reload();
    try {
      this.take(await this.api.get(key));
      this.loadError.set(null);
    } catch (e) {
      this.loadError.set(asApiError(e));
    }
  }

  /** A stored answer is the page's new baseline: the address and the account show what is stored. */
  private take(loaded: { readonly value: UpstreamRepository; readonly etag: string }): void {
    const r = loaded.value;
    this.etag = loaded.etag;
    this.item.set(r);
    this.clientsLevel.set(r.clients?.enabled ? r.visibility : 'public');
    this.url.set(r.address.url);
    this.hosts.set(r.address.extraHosts.map(row));
    this.keys.set('');
    this.authMode.set(r.account.authMode);
    this.username.set(r.account.username ?? '');
    this.password.set('');
    this.testResult.set(null);
  }

  /** The state or the level changed: the item is the answer, the ETag of the address and the account moved with it. */
  private async refreshAfter(saved: UpstreamRepository | null): Promise<void> {
    if (!saved) return;
    const fresh = await this.api.get(this.key());
    const keepUrl = this.url();
    const keepHosts = this.hosts();
    const dirty = this.addressDirty();
    this.take(fresh);
    if (dirty) {
      this.url.set(keepUrl);
      this.hosts.set(keepHosts);
    }
  }

  // ---- State and level ----

  protected async toggle(on: boolean): Promise<void> {
    const r = this.item();
    if (!r || this.busy()) return;
    this.busy.set('state');
    this.stateError.set(null);
    try {
      await this.refreshAfter(await this.actions.toggle(r, on));
    } catch (e) {
      this.stateError.set(asApiError(e));
    } finally {
      this.busy.set(null);
    }
  }

  protected async setLevel(select: HTMLSelectElement): Promise<void> {
    const r = this.item();
    if (!r || this.busy()) return;
    const level = select.value as UpstreamVisibilityLevel;
    this.busy.set('level');
    this.stateError.set(null);
    this.levelError.set(null);
    try {
      const saved = await this.actions.setLevel(r, level);
      if (!saved) select.value = r.visibility;
      await this.refreshAfter(saved);
    } catch (e) {
      select.value = r.visibility;
      const error = asApiError(e);
      if (error.code === 'public_not_allowed' || error.code === 'visibility_level_restricted') {
        this.levelError.set(error.problem?.detail ?? error.message);
      } else {
        this.stateError.set(error);
      }
    } finally {
      this.busy.set(null);
    }
  }

  /** **ClamAV definition updates**: on or off after the question of `UpstreamActions`; the answer is the page's new baseline. */
  protected async toggleClients(on: boolean): Promise<void> {
    const r = this.item();
    if (!r || this.busy()) return;
    this.busy.set('clients');
    this.stateError.set(null);
    try {
      await this.refreshAfter(await this.actions.toggleClients(r, on, this.publicAllowed()));
    } catch (e) {
      this.stateError.set(asApiError(e));
    } finally {
      this.busy.set(null);
    }
  }

  /**
   * **Set visibility** of the fold **Visibility** (Q-REPO-415): the level the definitions are served at while on. No question before
   * Public: the switch's own question named it, and the definitions are ClamAV's public databases.
   */
  protected async saveClientsLevel(): Promise<void> {
    const r = this.item();
    const level = this.clientsLevel();
    if (!r || this.busy() || level === r.visibility) return;
    this.busy.set('level');
    this.stateError.set(null);
    this.levelError.set(null);
    try {
      const saved = (await this.api.setVisibility(r.key, level)).value;
      this.notifier.status(levelSetTextOf(saved));
      await this.refreshAfter(saved);
    } catch (e) {
      const error = asApiError(e);
      if (error.code === 'public_not_allowed' || error.code === 'visibility_level_restricted') {
        this.levelError.set(error.problem?.detail ?? error.message);
      } else {
        this.stateError.set(error);
      }
    } finally {
      this.busy.set(null);
    }
  }

  // ---- Address ----

  protected setHost(i: number, change: Partial<HostRow>): void {
    this.hosts.update((list) => list.map((h, j) => (j === i ? { ...h, ...change } : h)));
  }

  protected addHost(): void {
    this.hosts.update((list) => [...list, { pattern: '', sendCredentials: false }]);
  }

  protected removeHost(i: number): void {
    this.hosts.update((list) => list.filter((_, j) => j !== i));
  }

  protected discardAddress(): void {
    const r = this.item();
    if (!r) return;
    this.url.set(r.address.url);
    this.hosts.set(r.address.extraHosts.map(row));
    this.keys.set('');
    this.addressError.set(null);
    this.addressHeld.set(null);
  }

  /**
   * **Save address** (R-UI-188): `setUpstreamRepositoryAddress` with the loaded `ETag`. Where the new address moves the hosts that
   * receive a stored account, the account is asked for again or removed, and sent with the address in the one request.
   */
  protected async saveAddress(event: Event): Promise<void> {
    event.preventDefault();
    const r = this.item();
    if (!r || !this.canWrite() || !this.addressDirty() || this.busy()) return;
    const body: { -readonly [K in keyof UpstreamRepositoryAddress]: UpstreamRepositoryAddress[K] } = {
      url: this.url().trim(),
      extraHosts: this.hosts()
        .filter((h) => h.pattern.trim() !== '')
        .map((h) => ({ pattern: h.pattern.trim(), sendCredentials: h.sendCredentials })),
    };
    if (this.keys().trim() !== '') body.upstreamKeys = this.keys();
    const before = credentialHostsOf(r.address.url, r.address.extraHosts.map(row));
    const after = credentialHostsOf(body.url ?? '', body.extraHosts ?? []);
    this.addressHeld.set(null);
    if (this.configured() && !sameHosts(before, after)) {
      this.addressHeld.set(
        $localize`:@@wellknown.address.held:The account of this registry comes from the installation's configuration, so an address that sends it to other hosts is changed there too.`,
      );
      return;
    }
    if (this.accountStored() && !sameHosts(before, after)) {
      const answer = await this.askAccount(before);
      if (!answer) return;
      body.account = answer.account;
    }
    this.busy.set('address');
    this.addressError.set(null);
    try {
      this.take(await this.api.setAddress(r.key, body, this.etag));
      this.notifier.status($localize`:@@wellknown.address.saved:Address saved.`);
    } catch (e) {
      this.addressError.set(asApiError(e));
    } finally {
      this.busy.set(null);
    }
  }

  /** **Reset to Quader's address**: the product's address again; a stored account whose hosts it moves goes, after a question. */
  protected async resetAddress(): Promise<void> {
    const r = this.item();
    if (!r || !this.canWrite() || this.busy()) return;
    this.busy.set('reset');
    this.addressError.set(null);
    try {
      this.take(await this.api.resetAddress(r.key, this.etag));
      this.notifier.status($localize`:@@wellknown.address.reset-done:The address is Quader's again.`);
    } catch (e) {
      const error = asApiError(e);
      if (!needsAccount(error)) {
        this.addressError.set(error);
        return;
      }
      const hosts = new Intl.ListFormat(this.locale, { type: 'conjunction' }).format(
        credentialHostsOf(r.address.url, r.address.extraHosts.map(row)),
      );
      const answer = await this.confirm.ask({
        title: $localize`:@@wellknown.reset.title:Reset the address and remove the account?`,
        lines: [$localize`:@@wellknown.reset.text:The account goes to ${hosts}:hosts: only, and Quader's address uses other hosts. The reset removes it; enter it again afterwards if Quader's address needs one.`],
        confirmLabel: $localize`:@@wellknown.reset.confirm:Reset and remove the account`,
      });
      if (!answer) return;
      try {
        this.take(await this.api.resetAddress(r.key, this.etag, true));
        this.notifier.status($localize`:@@wellknown.address.reset-done:The address is Quader's again.`);
      } catch (again) {
        this.addressError.set(asApiError(again));
      }
    } finally {
      this.busy.set(null);
    }
  }

  private async askAccount(hosts: readonly string[]): Promise<AccountAgainAnswer | undefined> {
    const a = this.item()?.account;
    const ref = this.dialog.open<AccountAgainDialog, AccountAgainData, AccountAgainAnswer>(AccountAgainDialog, {
      data: {
        hosts,
        authMode: a?.authMode ?? 'basic',
        username: a?.username ?? '',
      },
      maxWidth: '40rem',
    });
    return firstValueFrom(ref.closed);
  }

  // ---- Account ----

  protected discardAccount(): void {
    const a = this.item()?.account;
    if (!a) return;
    this.authMode.set(a.authMode);
    this.username.set(a.username ?? '');
    this.password.set('');
    this.accountError.set(null);
  }

  /** **Save account** with the loaded `ETag`: a stored secret that was not typed again stays (`{"set": true}`). */
  protected async saveAccount(event: Event): Promise<void> {
    event.preventDefault();
    const r = this.item();
    if (!r || !this.canWrite() || !this.accountDirty() || this.busy()) return;
    const mode = this.authMode();
    const account: UpstreamRepositoryAccount =
      mode === 'none'
        ? { authMode: 'none' }
        : {
            authMode: mode,
            ...(mode === 'basic' ? { username: this.username() } : {}),
            ...(this.password() !== '' ? { password: { value: this.password() } } : this.passwordStored() ? { password: { set: true } } : {}),
          };
    this.busy.set('account');
    this.accountError.set(null);
    try {
      this.take(await this.api.setAccount(r.key, account, this.etag));
      this.notifier.status($localize`:@@wellknown.account.saved:Account saved.`);
    } catch (e) {
      this.accountError.set(asApiError(e));
    } finally {
      this.busy.set(null);
    }
  }

  protected async removeAccount(): Promise<void> {
    const r = this.item();
    if (!r || !this.canWrite() || this.busy()) return;
    this.busy.set('remove');
    this.accountError.set(null);
    try {
      this.take(await this.api.removeAccount(r.key, this.etag));
      this.notifier.status($localize`:@@wellknown.account.removed:Account removed.`);
    } catch (e) {
      this.accountError.set(asApiError(e));
    } finally {
      this.busy.set(null);
    }
  }

  /**
   * **Test connection**: the saved repository while the address and the account are as stored (the stored secret takes part), the
   * address and the account as typed before they are saved (R-REPO-024; a typed secret is used once and stored nowhere).
   */
  protected async test(): Promise<void> {
    const r = this.item();
    if (!r || this.testing()) return;
    this.testing.set(true);
    this.testError.set(null);
    this.testResult.set(null);
    try {
      // A declared account is the saved repository's: its files take part on the server only (R-REPO-077).
      if (this.configured() || (!this.addressDirty() && !this.accountDirty())) {
        this.testResult.set({ value: await this.api.testSaved(r.key), at: new Date() });
      } else {
        const mode = this.authMode();
        const remote: Record<string, unknown> = {
          url: this.url().trim(),
          extraHosts: this.hosts().filter((h) => h.pattern.trim() !== ''),
          authMode: mode,
          ...(mode === 'basic' ? { username: this.username() } : {}),
          ...(mode !== 'none' && this.password() !== '' ? { password: { value: this.password() } } : {}),
        };
        this.testResult.set({ value: await this.api.testUnsaved(r.format, remote), at: new Date() });
      }
    } catch (e) {
      this.testError.set(asApiError(e));
    } finally {
      this.testing.set(false);
    }
  }

  protected hostOf = hostOf;
  /** A key's fingerprint in groups of four, as the signing keys of a repository show it. */
  protected readonly grouped = groupedFingerprint;

  protected readonly storedHint = $localize`:@@upstream.password.stored:A password is stored and is not shown. Leave the field empty to keep it; type one to replace it.`;

  /** "3 virtual repositories in 2 projects list it." (R-UI-188 Details: `listedBy` and `listedByProjects`, Q-API-363). */
  protected listedText(n: number, projects: number): string {
    if (n === 0) return $localize`:@@wellknown.listed.none:No virtual repository lists it.`;
    if (n === 1) return $localize`:@@wellknown.listed.one:1 virtual repository lists it.`;
    return projects === 1
      ? $localize`:@@wellknown.listed.many-one-project:${n}:count: virtual repositories in 1 project list it.`
      : $localize`:@@wellknown.listed.many:${n}:count: virtual repositories in ${projects}:projects: projects list it.`;
  }

  /** The virtual repositories the answer counts and does not name (at most 100 are named). */
  protected othersText(n: number): string {
    return n === 1
      ? $localize`:@@wellknown.listed.other:And 1 more virtual repository.`
      : $localize`:@@wellknown.listed.others:And ${n}:count: more virtual repositories.`;
  }

  /** A listing virtual repository's settings, where its members are: `<project>/<key>`. */
  protected listingLink(name: string): readonly string[] {
    const at = name.indexOf('/');
    return ['/projects', name.slice(0, at), 'repositories', name.slice(at + 1), 'settings'];
  }
}

function row(h: { readonly pattern: string; readonly sendCredentials?: boolean }): HostRow {
  return { pattern: h.pattern, sendCredentials: h.sendCredentials ?? false };
}
