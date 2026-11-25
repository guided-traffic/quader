import { ChangeDetectionStrategy, Component, LOCALE_ID, effect, inject, input, signal, untracked } from '@angular/core';
import { ExplainStep, Figure, LimitExplanation, LimitsApi, UsageSubjectKind } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { RepositoriesApi } from '../../api/repositories-api';
import { isSystemRepository } from '../../core/upstream-project';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { everyPhrase, placeLabel, whoView } from '../limits/callers';
import { FIGURES, compactFigures, figureName, formatCount, limitText } from '../limits/figures';

/**
 * One step of the resolution as a sentence (R-UI-184): "Repository web/npm-remote: no rule for alice." `subjectName` null: the step is the
 * reader's own, said in sentences of their own ("no rule for you", Q-UI-1219), never with a word for the reader put into a sentence.
 */
export function stepText(step: ExplainStep, subjectName: string | null): string {
  const where = placeLabel(step.place);
  if (step.outcome === 'none') {
    switch (step.step) {
      case 'single':
        return subjectName === null
          ? $localize`:@@explain.single-none-you:${where}:place:: no rule for you.`
          : $localize`:@@explain.single-none:${where}:place:: no rule for ${subjectName}:name:.`;
      case 'groups':
        return $localize`:@@explain.groups-none:${where}:place:: no rule for its groups.`;
      case 'kind':
        return $localize`:@@explain.kind-none:${where}:place:: no rule for its kind.`;
    }
  }
  // A kind of caller stands as a phrase of the sentence, not as a list's label ("every signed-in user", Q-UI-1219).
  const names = (step.rules ?? []).map((r) => (r.who.kind === 'every' ? everyPhrase(r.who.callerKind) : whoView(r.who).primary)).join(', ');
  switch (step.step) {
    case 'single':
      return subjectName === null
        ? $localize`:@@explain.single-match-you:${where}:place:: the rule for you applies.`
        : $localize`:@@explain.single-match:${where}:place:: the rule for ${subjectName}:name: applies.`;
    case 'groups':
      return $localize`:@@explain.groups-match:${where}:place:: the rule for ${names}:groups: applies.`;
    case 'kind':
      return $localize`:@@explain.kind-match:${where}:place:: the rule for ${names}:kind: applies.`;
  }
}

/**
 * **Which limit applies** (R-UI-184, R-UI-185, R-LIM-035): a quiet button opens a panel with a repository field; the answer lists
 * the steps of the resolution in order, the effective figures, what a project's maximum lowered and the ceiling of a remote.
 * `subject` asks for another caller (`GET /api/v1/limits/explain`); without it the caller asks for itself
 * (`GET /api/v1/users/me/limits`).
 */
@Component({
  selector: 'quader-limit-explain',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice],
  template: `
    <button class="btn btn-tertiary" type="button" [attr.aria-expanded]="open()" aria-controls="explain-panel" (click)="open.set(!open())">
      <span class="material-icons expand-chevron" aria-hidden="true">expand_more</span>
      <ng-container i18n="@@explain.open">Explain for a repository</ng-container>
    </button>
    <div id="explain-panel" class="collapse-region" [class.open]="open()">
      <div>
        <form class="field-action-row" (submit)="ask($event)" novalidate>
          <div class="field">
            <label class="field-label" for="explain-repository" i18n="@@explain.repository">Repository</label>
            <input
              id="explain-repository"
              class="text-input field-w-name"
              list="explain-repositories"
              autocomplete="off"
              spellcheck="false"
              translate="no"
              placeholder="project/repository"
              [value]="repository()"
              (input)="repository.set($any($event.target).value)"
            />
            <datalist id="explain-repositories">
              @for (r of repositories(); track r) {
                <option [value]="r"></option>
              }
            </datalist>
          </div>
          <button class="btn btn-default" type="submit" [disabled]="busy() || !repository().includes('/')" i18n="@@explain.ask">Explain</button>
        </form>
        @if (failure(); as e) {
          <quader-error-notice [error]="e" />
        }
        @if (answer(); as a) {
          <ol class="steps">
            @for (s of a.steps; track $index) {
              <li>{{ step(s, a) }}</li>
            }
          </ol>
          @if (a.administrator) {
            <p class="hint" i18n="@@explain.administrator">An administrator is decided at the instance alone.</p>
          }
          <dl class="facts">
            <dt i18n="@@explain.effective">Limits that apply</dt>
            <dd>{{ compact(a) }}</dd>
            @for (c of clamped(a); track c) {
              <dt i18n="@@explain.clamp">Above the project's maximum</dt>
              <dd>{{ c }}</dd>
            }
            @if (a.ceiling; as ceiling) {
              <dt i18n="@@explain.ceiling">Ceiling of this remote</dt>
              <dd>
                {{ ceiling.class === 'anonymous' ? anonymousCeiling : signedInCeiling }}:
                {{ ceilingText(ceiling.uncachedBytesPerHour, ceiling.upstreamRequestsPerHour) }}
              </dd>
            }
          </dl>
        }
      </div>
    </div>
  `,
  styles: `
    :host {
      display: block;
    }
    .btn .expand-chevron {
      margin-inline-end: var(--quader-space-1);
    }
    /* The field shrinks with a narrow page; its input is as wide as a name at most. */
    .field {
      display: flex;
      flex: 0 1 auto;
      flex-direction: column;
      gap: var(--quader-space-1);
      min-inline-size: 0;
      max-inline-size: 100%;
      margin-block-end: var(--quader-space-3);
    }
    form {
      margin-block-start: var(--quader-space-2);
    }
    .steps {
      margin: var(--quader-space-2) 0 var(--quader-space-3);
      padding-inline-start: var(--quader-space-5);
    }
    .hint {
      margin: 0 0 var(--quader-space-3);
    }
    .facts {
      display: grid;
      grid-template-columns: max-content 1fr;
      gap: var(--quader-space-1) var(--quader-space-4);
      margin: 0;
    }
    dt {
      font-weight: 600;
    }
    dd {
      margin: 0;
      overflow-wrap: anywhere;
    }
    @media (max-width: 40rem) {
      .facts {
        grid-template-columns: 1fr;
      }
    }
  `,
})
export class LimitExplain {
  private readonly api = inject(LimitsApi);
  private readonly repositoriesApi = inject(RepositoriesApi);
  private readonly locale = inject(LOCALE_ID);

  /** The caller asked about; absent for the caller's own limits. */
  readonly subject = input<{ kind: UsageSubjectKind; id: string } | null>(null);
  /** How the steps name another caller ("alice"); the reader's own limits (no `subject`) are said to the reader. */
  readonly subjectName = input('');
  /** The repositories offered: `<project>/<repo>`; empty takes every repository the caller can read. */
  readonly scopeRepositories = input<readonly string[] | null>(null);

  protected readonly open = signal(false);
  protected readonly repository = signal('');
  protected readonly repositories = signal<readonly string[]>([]);
  protected readonly answer = signal<LimitExplanation | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly busy = signal(false);
  protected readonly anonymousCeiling = $localize`:@@limits.ceilings.anonymous:All anonymous callers`;
  protected readonly signedInCeiling = $localize`:@@limits.ceilings.signed-in:All signed-in callers`;
  private loadedRepositories = false;

  constructor() {
    effect(() => {
      if (!this.open() || this.loadedRepositories) return;
      untracked(() => void this.loadRepositories());
    });
  }

  private async loadRepositories(): Promise<void> {
    this.loadedRepositories = true;
    const given = this.scopeRepositories();
    if (given) {
      this.repositories.set(given);
      return;
    }
    try {
      // A system repository of the project upstream answers every client 404 and charges no caller's budget: no limit of it applies
      // to anyone, so it is no suggestion (R-UI-193, Q-UI-901). The list is the whole one read.
      const all = await this.repositoriesApi.listAll();
      this.repositories.set(all.filter((r) => !isSystemRepository(r.project, r.key)).map((r) => `${r.project}/${r.key}`));
    } catch {
      // The suggestions are read again when the explanation is opened again (R-UI-216).
      this.repositories.set([]);
      this.loadedRepositories = false;
    }
  }

  protected async ask(event: Event): Promise<void> {
    event.preventDefault();
    const repository = this.repository().trim();
    if (!repository) return;
    this.busy.set(true);
    this.failure.set(null);
    try {
      const subject = this.subject();
      this.answer.set(subject ? await this.api.explain(subject, repository) : await this.api.myLimits(repository));
    } catch (e) {
      this.answer.set(null);
      this.failure.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }

  /** A step of another caller names the caller; a step of the reader's own limits is said to the reader (Q-UI-1219). */
  protected step(s: ExplainStep, a: LimitExplanation): string {
    if (this.subject() === null) return stepText(s, null);
    return stepText(s, this.subjectName() || ('name' in a.subject ? a.subject.name : a.subject.id));
  }

  protected compact(a: LimitExplanation): string {
    return compactFigures(a.effective, this.locale);
  }

  /** "From the upstream per hour: 30 GB per hour applies". */
  protected clamped(a: LimitExplanation): string[] {
    return FIGURES.filter((f: Figure) => f in a.clamped).map(
      (f) => $localize`:@@explain.clamped:${figureName(f)}:figure:: ${limitText(f, a.clamped[f] ?? null, this.locale)}:limit: applies`,
    );
  }

  protected ceilingText(bytes: number | null, requests: number | null): string {
    const amount = limitText('uncachedBytesPerHour', bytes, this.locale);
    const count = requests === null ? $localize`:@@limits.unlimited:Unlimited` : formatCount(requests, this.locale);
    return $localize`:@@explain.ceiling-text:${amount}:amount: from the upstream, upstream requests per hour: ${count}:count:`;
  }
}
