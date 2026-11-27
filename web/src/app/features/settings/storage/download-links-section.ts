import { personName } from '../../../shared/person-name';
import { Component, LOCALE_ID, computed, inject, output, resource, signal } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';
import {
  DownloadLinkConnection,
  DownloadLinkReason,
  DownloadLinkSettings,
  DownloadLinkSettingsPatch,
  DownloadLinkTest,
  DownloadLinksApi,
  LoadedDownloadLinkSettings,
} from '../../../api/download-links-api';
import { ApiError } from '../../../api/problem';
import { RepositoriesApi } from '../../../api/repositories-api';
import { DownloadLinksSwitchStore } from '../../../core/download-links-switch';
import { Notifier } from '../../../core/notifier';
import { SessionStore } from '../../../core/session-store';
import { AddressField } from '../../../shared/address-field';
import { ConflictField, ConflictResolver } from '../../../shared/conflict-dialog';
import { ErrorNotice } from '../../../shared/error-notice';
import { fieldErrorAt } from '../../../shared/field-errors';
import { linkCheckClause } from '../../../shared/link-check';
import { asApiError, valueOf } from '../../../shared/paged-resource';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Disclosure } from '../../../shared/ui/disclosure';
import { Help } from '../../../shared/ui/help';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { Busy } from '../../../shared/ui/busy';
import { Loading } from '../../../shared/ui/loading';

/** One MiB, the unit of the minimum size on the screen (Q-UI-951). */
const MIB = 1048576;
/** The ranges of R-STOR-014 in the screen's units. */
const MAX_MIN_SIZE_MIB = 1048576;
const MIN_LIFE = 10;
const MAX_LIFE = 3600;

/** The sentence of a reason a connection cannot serve links, in the words of R-UI-200's table. */
export function linkReasonText(name: string, reason: DownloadLinkReason): string {
  const detail = reason.code === 'link_check_failed' ? linkCheckClause(reason) : (reason.detail ?? '');
  switch (reason.code) {
    case 'encrypted':
      return $localize`:@@links.reason.encrypted:The connection ${name}:name: is encrypted. Quader delivers its content to clients itself.`;
    case 'not_https':
      return $localize`:@@links.reason.not-https:The address of ${name}:name: does not use https. Links need https.`;
    case 'quader_host':
      return $localize`:@@links.reason.quader-host:The address of ${name}:name: lies under ${detail}:host:, an address of Quader. Image clients would send their credentials to it, and the object store refuses such a link.`;
    default:
      return $localize`:@@links.reason.link-check:The object store of ${name}:name: did not answer a test link as expected: ${detail}:detail:. Quader delivers its content itself.`;
  }
}

/**
 * The sentence of a reason a Bastion cannot read an encrypted connection's files from the object store (its `handoverReasons`,
 * storage.md R-STOR-096), under that connection's field (Q-UI-990). An address under a host of Quader is refused by the Bastion; image
 * clients never get a link to an encrypted connection.
 */
export function bastionReasonText(name: string, reason: DownloadLinkReason): string {
  if (reason.code === 'quader_host') {
    const detail = reason.detail ?? '';
    return $localize`:@@links.reason.quader-host-bastion:The address of ${name}:name: lies under ${detail}:host:, an address of Quader. A Bastion refuses a link there.`;
  }
  return linkReasonText(name, reason);
}

/** Whether a typed address is an https origin without a path, a query or user information (R-STOR-074), as a save checks it. */
export function isHttpsOrigin(value: string): boolean {
  let u: URL;
  try {
    u = new URL(value);
  } catch {
    return false;
  }
  return (
    u.protocol === 'https:' &&
    u.hostname !== '' &&
    u.username === '' &&
    u.password === '' &&
    u.pathname === '/' &&
    u.search === '' &&
    u.hash === '' &&
    !value.trim().endsWith('?') &&
    !value.trim().endsWith('#')
  );
}

/** The minimum size in MiB as the field shows it: whole MiB without decimals, a stored size that is not one with up to three. */
function mibOf(bytes: number): string {
  const mib = bytes / MIB;
  return Number.isInteger(mib) ? String(mib) : String(Math.round(mib * 1000) / 1000);
}

/** The outcome of a test link, per connection. */
interface TestOutcome {
  readonly link?: DownloadLinkTest;
  /** The server's refusal of the test. */
  readonly error?: ApiError;
  /** The client's check of the address, before anything was sent. */
  readonly invalid?: string;
}

/**
 * The section **Download links** of Admin area › Settings › Storage (R-UI-200, a section of R-UI-262 since W-37, Q-UI-1590; moved
 * unchanged from the page it was built as, Q-UI-950): the installation's switch of the delivery
 * by signed links of the object store (storage.md R-STOR-014). The plain path is the status, which connections serve links, and the
 * switch with what it means; the minimum size, the link's life, each connection's public address with its test link and reason (an
 * encrypted connection's too: no client gets a link to it, a Bastion's direct reads read its files with each file's key, Q-UI-958,
 * Q-UI-990), and the downloads the switch reaches are details on demand
 * behind **Details**, which the fragment `details` opens. The formats the status names are the settings'
 * `routes`, so a format that declares later appears without a change of this page. A holder of `settings:read` without
 * `settings:write` (the auditor) reads the state as text and is offered no action.
 */
@Component({
  selector: 'quader-storage-download-links',
  imports: [Loading, Busy, AddressField, Checkbox, Disclosure, ErrorNotice, Help, RouterLink, TimeAgo],
  templateUrl: './download-links-section.html',
  styleUrl: '../../../shared/access/access.scss',
  styles: `
    .settings {
      max-inline-size: var(--quader-settings-measure);
    }
    .open-section h2 {
      margin: 0 0 var(--quader-space-3);
      font: var(--quader-type-title-medium);
    }
    h4 {
      margin: var(--quader-space-4) 0 var(--quader-space-1);
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    dl.facts {
      margin-block-end: var(--quader-space-3);
    }
    .banner-warning p + p {
      margin-block-start: var(--quader-space-1);
    }
    /* A connection's field, its button and its test fill the column, so that the field shrinks at 390 px as a text field does. */
    .link-address {
      align-self: stretch;
    }
    .row-actions {
      margin-block-start: var(--quader-space-3);
    }
    .test-result {
      margin-block-start: var(--quader-space-2);
    }
    .test-result p,
    .reason {
      margin-block: var(--quader-space-2) 0;
    }
    .routes-hint {
      margin: 0 0 var(--quader-space-2);
    }
    .routes {
      margin: 0;
      padding-inline-start: var(--quader-space-5);
    }
  `,
})
export class DownloadLinksSection {
  private readonly api = inject(DownloadLinksApi);
  private readonly repositories = inject(RepositoriesApi);
  private readonly notifier = inject(Notifier);
  private readonly conflicts = inject(ConflictResolver);
  private readonly switchStore = inject(DownloadLinksSwitchStore);
  private readonly locale = inject(LOCALE_ID);
  protected readonly session = inject(SessionStore);

  /** The settings as read or saved, for the connections' marks Links and Bastion on the page. */
  readonly settingsChange = output<DownloadLinkSettings>();

  protected readonly loaded = signal<LoadedDownloadLinkSettings | null>(null);
  protected readonly loadError = signal<ApiError | null>(null);
  protected readonly saveError = signal<ApiError | null>(null);
  protected readonly busy = signal(false);
  /** A save was tried: the client's checks show at their fields from then on. */
  protected readonly attempted = signal(false);
  protected readonly detailsOpen = signal(false);

  protected readonly enabled = signal(false);
  protected readonly minSize = signal('1');
  protected readonly linkLife = signal('60');
  protected readonly addresses = signal<Readonly<Record<string, string>>>({});
  protected readonly testing = signal<string | null>(null);
  protected readonly tests = signal<Readonly<Record<string, TestOutcome>>>({});

  protected readonly settings = computed<DownloadLinkSettings | null>(() => this.loaded()?.settings ?? null);
  protected readonly canWrite = computed(() => this.session.has('settings:write'));
  /**
   * Every connection has its public address and its test link (R-UI-200, Q-UI-958): a plain one's is where clients' links and a
   * Bastion's direct reads go, an encrypted one's serves no client and is where a Bastion's direct reads go, each file with its key
   * (R-STOR-095, R-STOR-096).
   */
  protected readonly connections = computed(() => this.settings()?.connections ?? []);
  protected readonly usable = computed(() => (this.settings()?.connections ?? []).filter((c) => c.usable));
  protected readonly unusable = computed(() => (this.settings()?.connections ?? []).filter((c) => !c.usable));
  /** No connection can serve a link (Q-UI-954): the reasons stand above the switch, which stays usable. */
  protected readonly noneUsable = computed(() => this.settings() !== null && this.usable().length === 0);

  private readonly formats = resource({ loader: () => this.repositories.formats() });
  /** A format's display name from the format catalog, its name until the catalog is read. */
  protected formatName(name: string): string {
    return (valueOf(this.formats) ?? []).find((f) => f.name === name)?.displayName ?? name;
  }
  /** The formats the switch reaches, by display name, joined as a sentence lists them. */
  protected readonly reach = computed(() => {
    const names = [...new Set((this.settings()?.routes ?? []).map((r) => this.formatName(r.format)))].sort((a, b) => a.localeCompare(b));
    return new Intl.ListFormat(this.locale, { type: 'conjunction' }).format(names);
  });
  protected readonly usableNames = computed(() => this.list(this.usable().map((c) => c.name)));
  protected readonly unusableNames = computed(() => this.list(this.unusable().map((c) => c.name)));

  protected readonly minSizeError = computed(() => {
    const v = Number(this.minSize().trim());
    if (this.minSize().trim() === '' || !Number.isFinite(v) || v < 0 || v > MAX_MIN_SIZE_MIB) {
      return $localize`:@@links.min-size.invalid:Enter a size from 0 to 1,048,576 MiB.`;
    }
    return null;
  });
  protected readonly linkLifeError = computed(() => {
    const v = Number(this.linkLife().trim());
    if (!Number.isInteger(v) || v < MIN_LIFE || v > MAX_LIFE) {
      return $localize`:@@links.life.invalid:Enter a number of seconds from 10 to 3,600.`;
    }
    return null;
  });

  /** The patch of what changed: only the changed members, a connection's `null` where its address returns to the endpoint. */
  protected readonly patch = computed<DownloadLinkSettingsPatch>(() => {
    const s = this.settings();
    if (!s) return {};
    const out: { -readonly [K in keyof DownloadLinkSettingsPatch]: DownloadLinkSettingsPatch[K] } = {};
    if (this.enabled() !== s.enabled) out.enabled = this.enabled();
    if (this.minSizeError() === null) {
      const bytes = Math.round(Number(this.minSize().trim()) * MIB);
      if (bytes !== s.minSize) out.minSize = bytes;
    }
    if (this.linkLifeError() === null) {
      const life = Number(this.linkLife().trim());
      if (life !== s.linkLifeSeconds) out.linkLifeSeconds = life;
    }
    const addresses: Record<string, string | null> = {};
    for (const c of this.connections()) {
      const want = this.wantedAddress(c);
      if (want !== c.publicAddress) addresses[c.name] = want;
    }
    if (Object.keys(addresses).length > 0) out.publicAddresses = addresses;
    return out;
  });
  protected readonly dirty = computed(() => Object.keys(this.patch()).length > 0 || this.minSizeError() !== null || this.linkLifeError() !== null);

  protected readonly principal = personName;
  /** The switch's name, for its help sign ("About …"). */
  protected readonly switchName = $localize`:@@links.switch:Deliver large downloads by signed links of the object store`;
  /** What the switch does, behind its help sign (R-UI-267); the speed limits of Q-LIM-99 in its second sentence. */
  protected readonly switchHelp = $localize`:@@links.help:A client fetches the file from the object store by a short-lived link; Quader still decides who may download. No speed limit slows the download; its size counts against the caller's budgets.`;
  private readonly addressInvalidText = $localize`:@@links.address.invalid:Enter an https address without a path, such as https://objects.example.com.`;

  constructor() {
    // The Bastions screen links here with the fragment `details` (R-BAS-071): the public addresses and their test links are in Details.
    if (inject(ActivatedRoute).snapshot.fragment === 'details') this.detailsOpen.set(true);
    void this.load();
  }

  /** The page notice's Retry (R-UI-216): the settings, and the formats' names when their read failed with them. */
  protected retry(): void {
    void this.load();
    if (this.formats.error()) this.formats.reload();
  }

  protected async load(): Promise<void> {
    try {
      this.adopt(await this.api.settings());
      this.loadError.set(null);
    } catch (e) {
      this.loadError.set(asApiError(e));
    }
  }

  private adopt(loaded: LoadedDownloadLinkSettings): void {
    const s = loaded.settings;
    this.loaded.set(loaded);
    this.enabled.set(s.enabled);
    this.minSize.set(mibOf(s.minSize));
    this.linkLife.set(String(s.linkLifeSeconds));
    this.addresses.set(Object.fromEntries(s.connections.map((c) => [c.name, c.publicAddress ?? c.endpoint])));
    this.attempted.set(false);
    this.switchStore.adopt(s.enabled, s.routes.map((r) => r.format));
    this.settingsChange.emit(s);
  }

  private list(names: readonly string[]): string {
    return new Intl.ListFormat(this.locale, { type: 'conjunction' }).format(names);
  }

  /** The address a save stores: null where the field is empty or holds the endpoint (Q-UI-951: an endpoint is followed, not saved). */
  private wantedAddress(c: DownloadLinkConnection): string | null {
    const typed = (this.addresses()[c.name] ?? '').trim();
    return typed === '' || typed === c.endpoint ? null : typed;
  }

  protected address(c: DownloadLinkConnection): string {
    return this.addresses()[c.name] ?? '';
  }

  protected setAddress(c: DownloadLinkConnection, value: string): void {
    this.addresses.update((a) => ({ ...a, [c.name]: value }));
    // A typed address makes the last test's result stale.
    this.tests.update((t) => Object.fromEntries(Object.entries(t).filter(([name]) => name !== c.name)));
  }

  /** The client's check of an address a save would send: an https origin (R-STOR-074). */
  protected addressError(c: DownloadLinkConnection): string | null {
    const want = this.wantedAddress(c);
    if (want === null || want === c.publicAddress || isHttpsOrigin(want)) return null;
    return this.addressInvalidText;
  }

  /** What stands at a field: the client's check after a save was tried, else the server's refusal of the last save. */
  protected shownError(pointer: string, client: string | null): string | null {
    if (this.attempted() && client !== null) return client;
    return fieldErrorAt(this.saveError(), pointer);
  }

  protected addressPointer(c: DownloadLinkConnection): string {
    return `/publicAddresses/${c.name.replaceAll('~', '~0').replaceAll('/', '~1')}`;
  }

  protected reasonsOf(c: DownloadLinkConnection): readonly string[] {
    return c.reasons.map((r) => linkReasonText(c.name, r));
  }

  /**
   * The reasons under a connection's field: all of a plain one's; of an encrypted one that it is encrypted and why a Bastion cannot
   * read its files (`handoverReasons`), since its other reasons are about clients' links, which it never serves (Q-UI-958, Q-UI-990).
   */
  protected fieldReasons(c: DownloadLinkConnection): readonly DownloadLinkReason[] {
    return c.encrypted ? [...c.reasons.filter((r) => r.code === 'encrypted'), ...c.handoverReasons] : c.reasons;
  }

  protected reasonText(c: DownloadLinkConnection, r: DownloadLinkReason): string {
    return c.encrypted ? bastionReasonText(c.name, r) : linkReasonText(c.name, r);
  }

  /** A failed save whose problems all stand at their fields shows no notice besides them. */
  protected readonly formError = computed(() => {
    const e = this.saveError();
    if (!e) return null;
    const pointers = ['/minSize', '/linkLifeSeconds', ...this.connections().map((c) => this.addressPointer(c))];
    return [...e.fieldErrors.keys()].length > 0 && [...e.fieldErrors.keys()].every((p) => pointers.includes(p)) ? null : e;
  });

  protected async save(event: Event): Promise<void> {
    event.preventDefault();
    const current = this.loaded();
    if (!current || this.busy()) return;
    this.attempted.set(true);
    if (this.minSizeError() !== null || this.linkLifeError() !== null || this.connections().some((c) => this.addressError(c) !== null)) {
      this.detailsOpen.set(true);
      return;
    }
    const patch = this.patch();
    if (Object.keys(patch).length === 0) return;
    this.busy.set(true);
    this.saveError.set(null);
    try {
      this.adopt(await this.api.updateSettings(patch, current.etag));
      this.notifier.status($localize`:@@links.saved:Download link settings saved.`);
    } catch (e) {
      const error = asApiError(e);
      if (error.status === 412) await this.resolveConflict(patch);
      else {
        this.saveError.set(error);
        if (error.fieldErrors.size > 0 && !error.fieldErrors.has('/enabled')) this.detailsOpen.set(true);
      }
    } finally {
      this.busy.set(false);
    }
  }

  /** 412: the settings another administrator saved against mine; "Overwrite" sends my patch with the `ETag` read now. */
  private async resolveConflict(patch: DownloadLinkSettingsPatch): Promise<void> {
    let theirs: LoadedDownloadLinkSettings;
    try {
      theirs = await this.api.settings();
    } catch (e) {
      this.saveError.set(asApiError(e));
      return;
    }
    const s = theirs.settings;
    const stored: Record<string, unknown> = {
      enabled: s.enabled,
      minSize: s.minSize,
      linkLifeSeconds: s.linkLifeSeconds,
      publicAddresses: Object.fromEntries(s.connections.map((c) => [c.name, c.publicAddress])),
    };
    const theirPart = Object.fromEntries(Object.keys(patch).map((k) => [k, stored[k]]));
    const decision = await this.conflicts.resolve({ mine: { ...patch }, theirs: theirPart, fields: (p) => this.conflictField(p) });
    if (decision === 'overwrite') {
      try {
        this.adopt(await this.api.updateSettings(patch, theirs.etag));
      } catch (e) {
        this.saveError.set(asApiError(e));
      }
    } else {
      this.adopt(theirs);
    }
  }

  /** How the conflict dialog names and shows a field of the section (R-UI-042, Q-UI-1210): the minimum size in MiB, as the field. */
  private conflictField(pointer: string): ConflictField | null {
    switch (pointer) {
      case '/enabled':
        return { label: $localize`:@@links.switch:Deliver large downloads by signed links of the object store` };
      case '/minSize':
        return { label: $localize`:@@links.min-size:Minimum size in MiB`, show: (v) => mibOf(Number(v)) };
      case '/linkLifeSeconds':
        return { label: $localize`:@@links.life:Link valid for, in seconds` };
    }
    const c = this.connections().find((x) => this.addressPointer(x) === pointer);
    return c ? { label: $localize`:@@conflict.links.address:Public address of ${c.name}:connection:` } : null;
  }

  /** **Test link** (Q-UI-952): a link to a probe on the connection, signed against the address as typed; nothing is saved. */
  protected async test(c: DownloadLinkConnection): Promise<void> {
    if (this.testing() !== null) return;
    const typed = this.address(c).trim() || c.endpoint;
    if (!isHttpsOrigin(typed)) {
      this.tests.update((t) => ({ ...t, [c.name]: { invalid: this.addressInvalidText } }));
      return;
    }
    this.testing.set(c.name);
    try {
      const link = await this.api.test(c.name, typed);
      this.tests.update((t) => ({ ...t, [c.name]: { link } }));
    } catch (e) {
      this.tests.update((t) => ({ ...t, [c.name]: { error: asApiError(e) } }));
    } finally {
      this.testing.set(null);
    }
  }

  /** The error a test answered at the address, shown at the field; another error stands as a notice under the button. */
  protected testFieldError(c: DownloadLinkConnection): string | null {
    const t = this.tests()[c.name];
    return t?.invalid ?? (t?.error ? fieldErrorAt(t.error, '/publicAddress') : null);
  }

  protected testNotice(c: DownloadLinkConnection): ApiError | null {
    const e = this.tests()[c.name]?.error;
    return e && fieldErrorAt(e, '/publicAddress') === null ? e : null;
  }
}
