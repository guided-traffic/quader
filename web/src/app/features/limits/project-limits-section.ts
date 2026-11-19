import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal, untracked, viewChild } from '@angular/core';
import { RouterLink } from '@angular/router';
import { LimitPlace, LimitProfile, LimitsApi, ProjectMaximum } from '../../api/limits-api';
import { Versioned } from '../../api/repositories-api';
import { SessionStore } from '../../core/session-store';
import { Disclosure } from '../../shared/ui/disclosure';
import { LimitRules } from './limit-rules';

/** The title line of a closed section: "2 rules", "1 rule", with the maximum where there is one. */
export function rulesNote(count: number | null, maximum: string | null): string {
  const parts: string[] = [];
  if (count) parts.push(count === 1 ? $localize`:@@limits.section.one-rule:1 rule` : $localize`:@@limits.section.rules:${count}:count: rules`);
  if (maximum) parts.push($localize`:@@limits.section.maximum:Maximum: ${maximum}:profile:`);
  return parts.join(' · ');
}

/**
 * The project settings' section **Download limits** (R-UI-180, Q-UI-731): closed, for an Owner and holders of `limits:read`; its
 * title line names the rules and the maximum. Open, one sentence, the maximum an administrator gave as a read-only line, the rule
 * table of the project with **Add rule** (an Owner's rules are capped), and the way to the project's usage.
 */
@Component({
  selector: 'quader-project-limits',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Disclosure, LimitRules, RouterLink],
  template: `
    <quader-disclosure
      i18n-heading="@@limits.section.heading"
      heading="Download limits"
      i18n-hint="@@limits.section.project-hint"
      hint="How much the callers of this project's repositories may download."
      [note]="note()"
    >
      <p i18n="@@limits.section.project-sentence">Rules here apply to the project's repositories that have no rules of their own.</p>
      <p class="hint">
        @if (maximumName(); as name) {
          <ng-container i18n="@@limits.section.maximum-given">Maximum given by an administrator: {{ name }}</ng-container>
        } @else {
          <ng-container i18n="@@limits.section.no-maximum">No maximum: you can set limits up to the instance's values.</ng-container>
        }
      </p>
      @if (canWrite()) {
        <div class="head">
          <button class="btn btn-default" type="button" (click)="rules()?.add()" i18n="@@limits.rules.add">Add rule</button>
        </div>
      }
      <quader-limit-rules [place]="place()" [canWrite]="canWrite()" [capped]="capped()" [maximum]="maximumName()" (counted)="count.set($event)" />
      @if (showUsage()) {
        <p class="usage">
          <a [routerLink]="['/projects', project(), 'usage']" i18n="@@limits.section.project-usage">Usage of this project</a>
        </p>
      }
    </quader-disclosure>
  `,
  styles: `
    p {
      margin: 0 0 var(--quader-space-3);
    }
    .head {
      display: flex;
      justify-content: flex-end;
      margin-block-end: var(--quader-space-2);
    }
    .usage {
      margin-block-start: var(--quader-space-3);
    }
  `,
})
export class ProjectLimitsSection {
  private readonly api = inject(LimitsApi);
  private readonly session = inject(SessionStore);

  readonly project = input.required<string>();

  protected readonly rules = viewChild(LimitRules);
  protected readonly place = computed<LimitPlace>(() => ({ kind: 'project', project: this.project() }));
  protected readonly count = signal<number | null>(null);
  private readonly maximum = signal<Versioned<ProjectMaximum> | null>(null);
  private readonly profiles = signal<readonly LimitProfile[]>([]);
  protected readonly maximumName = computed(() => {
    const m = this.maximum()?.value;
    return m ? (this.profiles().find((p) => p.key === m.profile)?.name ?? m.profile) : null;
  });
  protected readonly note = computed(() => rulesNote(this.count(), this.maximumName()));
  protected readonly canWrite = computed(() => this.session.has('limits:write') || this.session.isOwnerOf(this.project()));
  protected readonly capped = computed(() => !this.session.has('limits:write'));
  protected readonly showUsage = computed(() => this.session.has('usage:read') || this.session.isOwnerOf(this.project()));

  constructor() {
    effect(() => {
      const key = this.project();
      untracked(() => void this.load(key));
    });
  }

  private async load(key: string): Promise<void> {
    const [maximum, profiles] = await Promise.all([
      this.api.maximum(key).catch(() => null),
      this.api.profiles().catch(() => [] as readonly LimitProfile[]),
    ]);
    this.maximum.set(maximum);
    this.profiles.set(profiles);
  }
}
