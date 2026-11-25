import { Component, Injectable, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { newIdempotencyKey } from '../../api/client';
import { UpstreamApi, UpstreamRepository, UpstreamRepositoryBulkResult, UpstreamVisibilityLevel } from '../../api/upstream-api';
import { VisibilityApi, VisibilityChoices } from '../../api/visibility-api';
import { Notifier } from '../../core/notifier';
import { Confirm } from '../../shared/confirm-dialog';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle, QuaderDialog } from '../../shared/ui/dialog';
import { VisibilityLevel, VisibilityLevelControl, installationReasons, levelName, restrictedNotes } from '../../shared/ui/visibility-level';
import { SystemClients } from '../../core/system-clients';
import { repositoryWarningText } from '../repositories/repository-warnings';
import { clientsLoweredWarning, clientsSwitchedText, levelSetText, publicSentence, switchedText } from './upstream-words';

/**
 * A confirmation and the server's warnings as one status (R-UI-042): the toast shows one confirmation at a time, so a warning sent as a
 * status of its own would replace the confirmation it belongs to.
 */
function withWarnings(text: string, saved: Pick<UpstreamRepository, 'warnings'>): string {
  return [text, ...(saved.warnings ?? []).map((w) => repositoryWarningText(w))].join(' ');
}

interface SetAllData {
  readonly choices: VisibilityChoices | null;
}

/**
 * **Set visibility of all** (R-UI-187): the radio group of the levels (Q-UI-228) with the sentences of a well-known repository, and
 * **Set visibility**. It answers the chosen level; the question before Public is the caller's.
 */
@Component({
  selector: 'quader-upstream-set-all-dialog',
  imports: [DialogClose, DialogTitle, VisibilityLevelControl],
  template: `
    <h2 quaderDialogTitle i18n="@@wellknown.set-all.title">Set the visibility of every registry</h2>
    <div class="dialog-body">
      <p i18n="@@wellknown.set-all.text">Every registry that Quader offers takes this level, whether it is on or off.</p>
      <quader-visibility-level
        i18n-label="@@repo.visibility.level"
        label="Visibility level"
        subject="upstream"
        [(value)]="level"
        [reasons]="reasons"
        [notes]="notes"
      />
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@confirm.cancel">Cancel</button>
      <button class="btn btn-confirm" type="button" (click)="ref.close(level())" i18n="@@wellknown.set-all.confirm">Set visibility</button>
    </div>
  `,
})
export class SetAllVisibilityDialog {
  private readonly data = inject<SetAllData>(DIALOG_DATA);
  protected readonly ref = inject<DialogRef<VisibilityLevel, SetAllVisibilityDialog>>(DialogRef);
  protected readonly level = signal<VisibilityLevel>('internal');
  protected readonly reasons = installationReasons(this.data.choices);
  protected readonly notes = restrictedNotes(this.data.choices);
}

/**
 * The writes of the two pages of the admin area's **Upstream registries** (R-UI-187, R-UI-188), with the questions the owner asked
 * for before them: switching off a repository that virtual repositories list, making a level Public, switching all on. Each answers
 * the stored result, or null when the person said no; a refusal of the server is thrown to the page, which shows it where it
 * belongs (a level's at its select, R-UI-187). A success is said in the status line of the `Notifier`.
 */
@Injectable({ providedIn: 'root' })
export class UpstreamActions {
  private readonly api = inject(UpstreamApi);
  private readonly visibility = inject(VisibilityApi);
  private readonly confirm = inject(Confirm);
  private readonly dialog = inject(QuaderDialog);
  private readonly notifier = inject(Notifier);
  private readonly systemClients = inject(SystemClients);

  /** The levels the caller may choose (R-API-076), for the reasons of a row's select and of the dialog; null when unknown. */
  async choices(): Promise<VisibilityChoices | null> {
    try {
      return await this.visibility.choices();
    } catch {
      return null;
    }
  }

  /** Switches one repository on or off; switching off one that virtual repositories list asks first. */
  async toggle(r: UpstreamRepository, on: boolean): Promise<UpstreamRepository | null> {
    if (!on && r.listedBy > 0) {
      const n = r.listedBy;
      const projects = r.listedByProjects;
      const answer = await this.confirm.ask({
        title: $localize`:@@wellknown.off.title:Switch off ${r.displayName}:name:?`,
        lines: [
          n === 1
            ? $localize`:@@wellknown.off.one:1 virtual repository lists it. It skips it until it is on again; nothing of its own changes.`
            : projects === 1
              ? $localize`:@@wellknown.off.many-one-project:${n}:count: virtual repositories in 1 project list it. They skip it until it is on again; nothing of theirs changes.`
              : $localize`:@@wellknown.off.many:${n}:count: virtual repositories in ${projects}:projects: projects list it. They skip it until it is on again; nothing of theirs changes.`,
        ],
        confirmLabel: $localize`:@@wellknown.off.confirm:Switch off`,
      });
      if (answer === null) return null;
    }
    const saved = (await this.api.setState(r.key, on ? 'on' : 'off')).value;
    this.notifier.status(switchedText(saved));
    return saved;
  }

  /**
   * Switches **ClamAV definition updates** of `clamav` on or off (R-UI-187, R-UI-188, Q-UI-903), each after its question: on names
   * the address the installations update from and who can read it (Public, or Internal where the installation allows no public
   * level); off says that the installations get 404 and that Quader's own scan goes on. On asks for Public; the server lowers it
   * where it must and says so in a warning, which the status repeats after its confirmation.
   */
  async toggleClients(r: UpstreamRepository, on: boolean, publicAllowed: boolean): Promise<UpstreamRepository | null> {
    const address = r.clients?.address ?? '';
    const answer = await this.confirm.ask(
      on
        ? {
            title: $localize`:@@clamav.on.title:Offer the ClamAV definitions to your ClamAV installations?`,
            lines: [
              $localize`:@@clamav.on.address:They update from ${address}:address:.`,
              publicAllowed
                ? $localize`:@@clamav.on.public:Public: anyone who reaches this address can download them, without a credential.`
                : clientsLoweredWarning(),
            ],
            confirmLabel: $localize`:@@clamav.on.confirm:Switch on`,
          }
        : {
            title: $localize`:@@clamav.off.title:Stop serving the ClamAV definitions?`,
            lines: [
              $localize`:@@clamav.off.text:ClamAV installations that update from Quader get 404 until they are on again. Quader's own malware scan is not affected.`,
            ],
            confirmLabel: $localize`:@@wellknown.off.confirm:Switch off`,
          },
    );
    if (answer === null) return null;
    const saved = (await this.api.setClients(r.key, on, on ? 'public' : undefined)).value;
    this.systemClients.forget();
    this.notifier.status(withWarnings(clientsSwitchedText(saved), saved));
    return saved;
  }

  /** Sets one repository's level; Public asks first with the owner's sentence. */
  async setLevel(r: UpstreamRepository, level: UpstreamVisibilityLevel): Promise<UpstreamRepository | null> {
    if (level === 'public' && r.visibility !== 'public') {
      const answer = await this.confirm.ask({
        title: $localize`:@@wellknown.public.title:Make ${r.displayName}:name: public?`,
        lines: [publicSentence()],
        confirmLabel: $localize`:@@wellknown.public.confirm:Make public`,
      });
      if (answer === null) return null;
    }
    const saved = (await this.api.setVisibility(r.key, level)).value;
    this.notifier.status(withWarnings(levelSetText(saved), saved));
    return saved;
  }

  /** **Switch all on**: names the registries it switches on, and makes each Internal. Never Public (owner). */
  async switchAllOn(off: readonly UpstreamRepository[]): Promise<UpstreamRepositoryBulkResult | null> {
    const n = off.length;
    const answer = await this.confirm.ask({
      title:
        n === 1
          ? $localize`:@@wellknown.all-on.title-one:Switch on 1 registry?`
          : $localize`:@@wellknown.all-on.title:Switch on ${n}:count: registries?`,
      lines: [
        $localize`:@@wellknown.all-on.text:Each is Internal once it is on: everyone signed in who is not marked external, people and their pipelines and service accounts, can pull through it, directly and through every virtual repository that lists it. Quader contacts each registry when a client asks for something it has not stored.`,
      ],
      items: off.map((r) => r.displayName),
      confirmLabel: $localize`:@@wellknown.all-on.confirm:Switch all on`,
    });
    if (answer === null) return null;
    const result = await this.api.switchOnAll(newIdempotencyKey());
    const changed = result.items.length;
    this.notifier.status(
      changed === 1
        ? $localize`:@@wellknown.all-on.done-one:1 registry switched on.`
        : $localize`:@@wellknown.all-on.done:${changed}:count: registries switched on.`,
    );
    return result;
  }

  /** **Set visibility of all**: the dialog of the levels, then Public's question where it was chosen. */
  async setAllVisibility(): Promise<UpstreamRepositoryBulkResult | null> {
    const choices = await this.choices();
    const ref = this.dialog.open<SetAllVisibilityDialog, SetAllData, VisibilityLevel>(SetAllVisibilityDialog, {
      data: { choices },
      maxWidth: '40rem',
    });
    const level = await firstValueFrom(ref.closed);
    if (!level) return null;
    if (level === 'public') {
      const answer = await this.confirm.ask({
        title: $localize`:@@wellknown.public.all-title:Make every registry public?`,
        lines: [publicSentence()],
        confirmLabel: $localize`:@@wellknown.public.confirm:Make public`,
      });
      if (answer === null) return null;
    }
    const result = await this.api.setAllVisibility(level, newIdempotencyKey());
    const name = levelName(level);
    this.notifier.status($localize`:@@wellknown.set-all.done:Every registry is ${name}:level:.`);
    return result;
  }
}
