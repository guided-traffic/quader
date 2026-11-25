import { Injectable, computed, inject, resource, signal } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { newIdempotencyKey } from '../../../api/client';
import { DeliveryRequestsApi } from '../../../api/delivery-requests-api';
import { DeliveryEffect, DeliveryRuleInput, DeliveryRulesApi } from '../../../api/delivery-rules-api';
import { RemoteDownloadRequest, RemoteDownloadsApi } from '../../../api/remote-downloads-api';
import { Notifier } from '../../../core/notifier';
import { SessionStore } from '../../../core/session-store';
import { Confirm } from '../../../shared/confirm-dialog';
import { valueOf } from '../../../shared/paged-resource';
import { QuaderDialog } from '../../../shared/ui/dialog';
import { AskDialog, AskDialogData } from './request-dialogs';
import { DownloadPreviewDialog, DownloadPreviewData } from './download-preview-dialog';
import { FailureToasts } from '../../../shared/failure-dialog';

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

  private readonly rights = resource({
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
    const ref = this.dialog.open<DownloadPreviewDialog, DownloadPreviewData, string>(DownloadPreviewDialog, {
      data: { repository: this.repository(), request, title },
      width: '36rem',
    });
    const bulk = await firstValueFrom(ref.closed);
    if (bulk === undefined || bulk === null) return;
    this.changed.update((n) => n + 1);
    // The bulk has no row yet: it reads the versions from the upstream first and queues them then, or fails with the cause, which the
    // tab Downloads shows on the bulk (Q-CUR-168).
    this.notifier.status(
      $localize`:@@upstream.download.bulk-started:The bulk download has started; Quader reads its versions first, then queues them.`,
      bulk ? this.showBulk(bulk) : this.showDownloads(),
    );
  }

  /** **Allow** or **Block** a subject: the rule of the package (every version, future ones included), of a version, or of a pattern. */
  async setRule(rule: DeliveryRuleInput, label: string): Promise<boolean> {
    const out = await this.run(label, () => this.rules.put(this.repository(), rule, newIdempotencyKey()));
    if (out === null) return false;
    this.notifier.status(ruleSaved(rule.effect, label, rule.kind));
    return true;
  }

  /** **Remove rule** of a subject: the rule is found by its subject in the list, then removed. */
  async removeRule(kind: DeliveryRuleInput['kind'], subject: string): Promise<void> {
    const out = await this.run(subject, async () => {
      const page = await this.rules.list(this.repository(), { kind, q: subject }, null);
      const rule = page.items.find((r) => r.kind === kind && r.subject === subject);
      if (!rule) return false;
      await this.rules.remove(this.repository(), rule.id);
      return true;
    });
    if (out) this.notifier.status($localize`:@@upstream.rule.removed:The rule for ${subject}:subject: is removed.`);
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

/** The status after a rule was written. */
function ruleSaved(effect: DeliveryEffect, subject: string, kind: DeliveryRuleInput['kind']): string {
  if (effect === 'allow') {
    return kind === 'package'
      ? $localize`:@@upstream.rule.allowed-package:${subject}:subject: is allowed with every version, future ones included.`
      : $localize`:@@upstream.rule.allowed:${subject}:subject: is allowed.`;
  }
  return kind === 'package'
    ? $localize`:@@upstream.rule.blocked-package:${subject}:subject: is blocked with every version, future ones included.`
    : $localize`:@@upstream.rule.blocked:${subject}:subject: is blocked.`;
}
