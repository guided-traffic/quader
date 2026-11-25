import { Injectable, computed, inject, resource, signal } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { newIdempotencyKey } from '../../../api/client';
import { DeliveryRequestsApi } from '../../../api/delivery-requests-api';
import { DeliveryRuleCounts, DeliveryRulesApi, DeliverySwitchResult } from '../../../api/delivery-rules-api';
import { DeliveryDecision } from '../../../api/upstream-catalogue-api';
import { RemoteDownloadRequest, RemoteDownloadsApi } from '../../../api/remote-downloads-api';
import { Notifier } from '../../../core/notifier';
import { SessionStore } from '../../../core/session-store';
import { Confirm } from '../../../shared/confirm-dialog';
import { valueOf } from '../../../shared/paged-resource';
import { QuaderDialog } from '../../../shared/ui/dialog';
import { AskDialog, AskDialogData } from './request-dialogs';
import { DownloadPreviewDialog, DownloadPreviewData, DownloadPreviewResult } from './download-preview-dialog';
import { FailureToasts } from '../../../shared/failure-dialog';
import { VersionRangeData, VersionRangeDialog } from './version-range-dialog';
import { rangeBoundsText } from './upstream-words';

/** How long a switch's answer stands in its row while the catalogue's read does not agree with it yet (R-CUR-006's bound and more). */
export const SETTLE_MS = 30_000;

/** The subject of an act: a package with every version, one version of it, or a sub-section (a name pattern or a level). */
export interface Subject {
  readonly name: string;
  readonly version?: string;
}

/** `name version`, or the name alone. */
export function subjectText(s: Subject): string {
  return s.version ? `${s.name} ${s.version}` : s.name;
}

/**
 * What the tab **Upstream** of one remote repository does (remote-curation.md R-CUR-010, R-CUR-006, R-CUR-015, R-CUR-016), and who may
 * (R-CUR-005): provided by the tab, so its package rows, its level bar and its requests share the caller's rights and the counter
 * `changed` that makes them read again after a write. Each act says its outcome through the notifier; the server decides each call
 * again, and its refusal is said in the problem's words.
 */
@Injectable()
export class UpstreamActions {
  private readonly downloads = inject(RemoteDownloadsApi);
  private readonly rules = inject(DeliveryRulesApi);
  private readonly requests = inject(DeliveryRequestsApi);
  private readonly session = inject(SessionStore);
  private readonly notifier = inject(Notifier);
  private readonly failureToasts = inject(FailureToasts);
  private readonly dialog = inject(QuaderDialog);
  private readonly confirm = inject(Confirm);
  private readonly router = inject(Router);

  /** The qualified name `<project>/<key>` of the remote repository, set by the tab. */
  readonly repository = signal('');
  /** The action a client's fill needs here (`remote.cacheFillAction`); a reader's view does not name it, then `read`. */
  readonly fillAction = signal<'read' | 'deploy'>('read');
  /** The repository's format, for the words of a verdict that cannot exist. */
  readonly format = signal('');
  /** Raised by every write, so that the rows, the requests and the counts read again. */
  readonly changed = signal(0);
  /** The subjects with a call open: their buttons wait. */
  readonly busy = signal<ReadonlySet<string>>(new Set());

  /** The caller's rights in the repository; the tab's notice reads them again with its own read when they failed with it (Q-UI-1196). */
  readonly rights = resource({
    params: () => (this.repository() ? { repository: this.repository(), fill: this.fillAction() } : null),
    loader: async ({ params }) => {
      if (!params || this.session.state() !== 'authenticated') return { manage: false, fill: false };
      const [manage, read, fill] = await this.session.check([
        { repository: params.repository, action: 'manage' },
        { repository: params.repository, action: 'read' },
        { repository: params.repository, action: params.fill },
      ]);
      return { manage: !!manage || this.session.hasRole('admin'), fill: !!read && !!fill };
    },
  });
  /** The holders of `manage` (Maintainer, Owner) and the system role `admin`: they set rules, answer requests, download in bulk. */
  readonly manager = computed(() => valueOf(this.rights)?.manage ?? false);
  /** The caller may fill the repository as a client would: one version the rules deliver. */
  readonly mayFill = computed(() => valueOf(this.rights)?.fill ?? false);
  /** The rights are known: before, no action is offered. */
  readonly known = computed(() => valueOf(this.rights) !== undefined);
  /** Reads the rules with their notes: setting them, or `repositories:read-config`. */
  readonly mayReadRules = computed(() => this.manager() || this.session.has('repositories:read-config'));

  /** The key under which a subject's call waits. */
  key(s: Subject | string): string {
    return typeof s === 'string' ? s : subjectText(s);
  }

  isBusy(s: Subject | string): boolean {
    return this.busy().has(this.key(s));
  }

  private async run<T>(s: Subject | string, call: () => Promise<T>): Promise<T | null> {
    const key = this.key(s);
    if (this.busy().has(key)) return null;
    this.busy.update((b) => new Set([...b, key]));
    try {
      const out = await call();
      this.changed.update((n) => n + 1);
      return out;
    } catch (e) {
      this.failureToasts.alert(e);
      return null;
    } finally {
      this.busy.update((b) => {
        const next = new Set(b);
        next.delete(key);
        return next;
      });
    }
  }

  private showDownloads(): { label: string; run: () => void } {
    const slash = this.repository().indexOf('/');
    const commands = ['/projects', this.repository().slice(0, slash), 'repositories', this.repository().slice(slash + 1), 'downloads'];
    return { label: $localize`:@@upstream.show-downloads:Show downloads`, run: () => void this.router.navigate(commands) };
  }

  /** The tab Downloads opened on one bulk (`?bulk=`): its state, its counters and its failure, and its rows once it queued them. */
  private showBulk(id: string): { label: string; run: () => void } {
    const slash = this.repository().indexOf('/');
    const commands = ['/projects', this.repository().slice(0, slash), 'repositories', this.repository().slice(slash + 1), 'downloads'];
    return {
      label: $localize`:@@upstream.show-bulk:Show bulk download`,
      run: () => void this.router.navigate(commands, { queryParams: { bulk: id } }),
    };
  }

  /** **Download** or **Download for examination** of one version (R-CUR-010, R-CUR-014): one row of the queue, charged to the caller. */
  async downloadVersion(s: Required<Subject>, delivered: boolean): Promise<void> {
    const out = await this.run(s, () =>
      this.downloads.start(this.repository(), { level: 'version', name: s.name, version: s.version }, newIdempotencyKey()),
    );
    if (out === null) return;
    this.notifier.status(
      delivered
        ? $localize`:@@upstream.download.queued:${subjectText(s)}:subject: waits for its download into the cache.`
        : $localize`:@@upstream.download.queued-examination:${subjectText(s)}:subject: waits for its download for examination; it is not delivered.`,
      this.showDownloads(),
    );
  }

  /** **Download every version…** of a package, or **Download this sub-section…**: the preview first, then the bulk (R-CUR-010). */
  async downloadBulk(request: RemoteDownloadRequest, title: string): Promise<void> {
    const ref = this.dialog.open<DownloadPreviewDialog, DownloadPreviewData, DownloadPreviewResult>(DownloadPreviewDialog, {
      data: { repository: this.repository(), request, title },
      width: '36rem',
    });
    const out = await firstValueFrom(ref.closed);
    if (!out) return;
    this.changed.update((n) => n + 1);
    // The bulk has no row yet: it reads the versions from the upstream first and queues them then, or fails with the cause, which the
    // tab Downloads shows on the bulk (Q-CUR-168). A sync's first run is such a bulk; the tab lists the sync above it (R-CUR-027).
    if (out.sync) {
      this.notifier.status(
        $localize`:@@upstream.download.sync-started:Kept in sync: the first run has started, and the tab Downloads lists the sync.`,
        this.showDownloads(),
      );
      return;
    }
    this.notifier.status(
      $localize`:@@upstream.download.bulk-started:The bulk download has started; Quader reads its versions first, then queues them.`,
      out.bulk ? this.showBulk(out.bulk) : this.showDownloads(),
    );
  }

  /**
   * A version's switch turned (R-CUR-024): the server writes the version's rule where the asked state departs from what the version
   * inherits and removes it where it equals it. The answer is returned, so the row shows it at once (R-UI-213); a refusal is said with
   * every fact it carried (R-UI-216) and the row stays as it was.
   */
  async switchVersion(s: Required<Subject>, on: boolean): Promise<DeliverySwitchResult | null> {
    const out = await this.run(s, async () => {
      const answer = await this.rules.switch(this.repository(), { name: s.name, version: s.version, delivered: on, resetVersions: false }, newIdempotencyKey());
      this.remember(this.key(s), answer.decision);
      return answer;
    });
    if (out !== null) this.notifier.status(switched(subjectText(s), out.decision.delivered));
    return out;
  }

  /**
   * The answers of the switches whose catalogue rows may still lag: the catalogue reads the replica's snapshot, which takes the write
   * within its bound (R-CUR-006), so a row read at once could show the state before the switch. Until a read agrees with the answer, at
   * most {@link SETTLE_MS}, the row shows the answer, and the views read every few seconds meanwhile (R-UI-213).
   */
  private readonly settling = new Map<string, { readonly decision: DeliveryDecision; readonly until: number }>();
  /** How many switches' answers are not yet read back: the views read fast while some are. */
  readonly unsettled = signal(0);

  private remember(key: string, decision: DeliveryDecision): void {
    this.settling.set(key, { decision, until: Date.now() + SETTLE_MS });
    this.unsettled.set(this.settling.size);
  }

  /**
   * The decision a row shows of a subject (`name` or `name version`) the catalogue answered `read` for: the switch's answer while the
   * read does not agree with it yet, else the read. Called where the rows are read, not in a template.
   */
  settled(key: string, read: DeliveryDecision): DeliveryDecision {
    const pending = this.settling.get(key);
    if (!pending) return read;
    const agrees = pending.decision.delivered === read.delivered && sameRange(pending.decision, read);
    if (pending.until > Date.now() && !agrees) return pending.decision;
    this.settling.delete(key);
    this.unsettled.set(this.settling.size);
    return read;
  }

  /**
   * A package's switch turned (R-CUR-024): where its versions have rules of their own, a confirmation first asks whether they are kept
   * (the default) or reset with it (Q-CUR-203). Null when cancelled or refused.
   */
  async switchPackage(name: string, on: boolean, versionRules: DeliveryRuleCounts | undefined): Promise<DeliverySwitchResult | null> {
    let reset = false;
    const own = (versionRules?.allow ?? 0) + (versionRules?.block ?? 0);
    if (own > 0) {
      const answer = await this.confirm.ask({
        title: on
          ? $localize`:@@upstream.switch.package-on-title:Allow every version of ${name}:name:?`
          : $localize`:@@upstream.switch.package-off-title:Block every version of ${name}:name:?`,
        lines: [ownSettingsText(own, versionRules?.allow ?? 0, versionRules?.block ?? 0)],
        choice: $localize`:@@upstream.switch.reset:Reset them: every version follows the package`,
        choiceChecked: false,
        confirmLabel: on ? $localize`:@@upstream.switch.allow:Allow` : $localize`:@@upstream.switch.block:Block`,
      });
      if (answer === null) return null;
      reset = answer.checked;
    }
    const out = await this.run(name, async () => {
      const answer = await this.rules.switch(this.repository(), { name, delivered: on, resetVersions: reset }, newIdempotencyKey());
      this.remember(name, answer.decision);
      return answer;
    });
    if (out !== null) this.notifier.status(switchedPackage(name, out.decision.delivered));
    return out;
  }

  /**
   * **Limit versions…** of a package (R-CUR-026): the dialog writes the range through the package's switch and closes with the answer,
   * which the head shows at once (R-UI-213); the versions are read again. A refused save stays in the dialog with its values. Null when
   * cancelled.
   */
  async limitVersions(data: Omit<VersionRangeData, 'repository' | 'format'>): Promise<DeliverySwitchResult | null> {
    const ref = this.dialog.open<VersionRangeDialog, VersionRangeData, DeliverySwitchResult>(VersionRangeDialog, {
      data: { ...data, repository: this.repository(), format: this.format() },
      width: '34rem',
    });
    const out = await firstValueFrom(ref.closed);
    if (!out) return null;
    this.remember(data.name, out.decision);
    this.changed.update((n) => n + 1);
    const range = out.decision.decidedBy.range;
    this.notifier.status(
      range
        ? $localize`:@@upstream.range.saved:${data.name}:name: is delivered with the versions ${rangeBoundsText(range)}:bounds:.`
        : $localize`:@@upstream.range.saved-all:${data.name}:name: is delivered with every version.`,
    );
    return out;
  }

  /** **Request** a package or a version (R-CUR-015): a dialog for the optional note, then the ask. */
  async ask(s: Subject): Promise<void> {
    const ref = this.dialog.open<AskDialog, AskDialogData, string | null>(AskDialog, { data: { subject: subjectText(s), version: !!s.version } });
    const note = await firstValueFrom(ref.closed);
    if (note === undefined || note === null) return;
    const out = await this.run(s, () =>
      this.requests.ask(this.repository(), { name: s.name, ...(s.version ? { version: s.version } : {}), ...(note ? { note } : {}) }, newIdempotencyKey()),
    );
    if (out !== null) this.notifier.status($localize`:@@upstream.request.created:Your request for ${subjectText(s)}:subject: is sent to those who may allow it.`);
  }

  /** **Discard** what was fetched for examination of a subject the rules do not deliver (R-CUR-016), after a confirmation. */
  async discard(s: Subject): Promise<void> {
    const answer = await this.confirm.ask({
      title: $localize`:@@upstream.discard.title:Discard ${subjectText(s)}:subject:?`,
      lines: [$localize`:@@upstream.discard.text:The files fetched for examination are removed from the cache. A version the rules deliver is never discarded.`],
      confirmLabel: $localize`:@@upstream.discard.confirm:Discard`,
    });
    if (answer === null) return;
    const out = await this.run(s, () =>
      this.requests.discard(this.repository(), { name: s.name, ...(s.version ? { version: s.version } : {}) }, newIdempotencyKey()),
    );
    if (out !== null) this.notifier.status($localize`:@@upstream.discard.done:Discarded: ${out.versions.length}:versions: versions, ${out.files}:files: files.`);
  }
}

/** Whether two decisions name the same range of a package's allow (R-CUR-026), or none both. */
function sameRange(a: DeliveryDecision, b: DeliveryDecision): boolean {
  const x = a.decidedBy.range;
  const y = b.decidedBy.range;
  if (!x || !y) return !x === !y;
  return x.fromVersion === y.fromVersion && x.toVersion === y.toVersion && x.toInclusive === y.toInclusive;
}

/** The status after a version's switch: what the server decided. */
function switched(subject: string, delivered: boolean): string {
  return delivered
    ? $localize`:@@upstream.switch.delivered:${subject}:subject: is delivered.`
    : $localize`:@@upstream.switch.not-delivered:${subject}:subject: is not delivered.`;
}

/** The status after a package's switch. */
function switchedPackage(name: string, delivered: boolean): string {
  return delivered
    ? $localize`:@@upstream.switch.package-delivered:${name}:name: is delivered with every version that has no setting of its own, future ones included.`
    : $localize`:@@upstream.switch.package-not-delivered:${name}:name: is not delivered, apart from the versions that have a setting of their own.`;
}

/** The sentence of the confirmation: how many versions have settings of their own, and that they are kept unless reset. */
function ownSettingsText(own: number, allow: number, block: number): string {
  if (own === 1) {
    return allow === 1
      ? $localize`:@@upstream.switch.own-one-allowed:One version has a setting of its own: it is allowed. It is kept unless you reset it.`
      : $localize`:@@upstream.switch.own-one-blocked:One version has a setting of its own: it is blocked. It is kept unless you reset it.`;
  }
  return $localize`:@@upstream.switch.own-many:${own}:count: versions have settings of their own: ${allow}:allowed: allowed, ${block}:blocked: blocked. They are kept unless you reset them.`;
}
