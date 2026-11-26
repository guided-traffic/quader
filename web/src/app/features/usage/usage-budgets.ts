import { ChangeDetectionStrategy, Component, LOCALE_ID, inject, input } from '@angular/core';
import { BudgetFigure, BudgetFigures, BudgetRule, Figure, LimitMode, LimitPlace, UsageBudget } from '../../api/limits-api';
import { everyPhrase, placeLabel, placeText, whoView } from '../limits/callers';
import { figureName, formatBytes, formatCount, limitText, unitOf } from '../limits/figures';
import { UsageMeter } from './usage-meter';
import { stateOfRatio } from './usage-words';

/** The windowed figures with a meter, and the rates as text, in the order of R-UI-184. */
export const WINDOWED: readonly (keyof BudgetFigures)[] = [
  'requestsPerHour',
  'cachedBytesPerHour',
  'uncachedBytesPerHour',
  'upstreamRequestsPerHour',
  'bytesPerDay',
  'parallelTransfers',
  'refusedPerHour',
];
export const RATES: readonly (keyof BudgetFigures)[] = ['requestsPerSecond', 'cachedBytesPerSecond', 'uncachedBytesPerSecond'];

/** "the group contractors", "alice", "every signed-in user": a rule's "who" inside a sentence. */
function whoPhrase(rule: BudgetRule): string {
  const w = rule.who;
  switch (w.kind) {
    case 'group':
      return $localize`:@@usage.who.group:the group ${w.displayName || w.group}:group:`;
    case 'every':
      return everyPhrase(w.callerKind);
    case 'address':
      return $localize`:@@usage.who.address:the addresses ${w.prefix}:prefix:`;
    default:
      return whoView(w).primary;
  }
}

/**
 * Why a budget applies, in one sentence (R-UI-184): "Rule for the group contractors at web/npm-remote, profile Restricted.", a
 * built-in rule "Default for every signed-in user at the instance, profile Standard."; on the profile "Your limits come from …"
 * (R-UI-185).
 */
export function ruleSentence(rule: BudgetRule | null, place: LimitPlace, profileName: string, own: boolean): string {
  if (!rule) return $localize`:@@usage.rule.none:No rule applies ${placeText(place)}:place:: every figure is unlimited.`;
  const who = whoPhrase(rule);
  const where = placeText(place);
  // Each case is a sentence of its own (Q-UI-1219): German says "aus der Regel" after "stammen" and "die Regel" after "Es gilt", so the
  // rule's phrase is no part to put into two sentences.
  if (own) {
    return rule.builtin
      ? $localize`:@@usage.rule.own-builtin:Your limits come from the default for ${who}:who: ${where}:place:, profile ${profileName}:profile:.`
      : $localize`:@@usage.rule.own-rule:Your limits come from the rule for ${who}:who: ${where}:place:, profile ${profileName}:profile:.`;
  }
  return rule.builtin
    ? $localize`:@@usage.rule.applies-builtin:Applies: the default for ${who}:who: ${where}:place:, profile ${profileName}:profile:.`
    : $localize`:@@usage.rule.applies-rule:Applies: the rule for ${who}:who: ${where}:place:, profile ${profileName}:profile:.`;
}

/** "246 GB of 300 GB", "31,000 of 50,000", "2 of 20". */
export function usedOfLimit(figure: Figure, used: number, limit: number, locale: string): string {
  const amount = (v: number): string => (unitOf(figure) === 'count' ? formatCount(v, locale) : formatBytes(v, locale));
  return $localize`:@@usage.used-of:${amount(used)}:used: of ${amount(limit)}:limit:`;
}

interface FigureRow {
  readonly figure: Figure;
  readonly name: string;
  readonly ratio: number | null;
  readonly text: string;
}

/**
 * The limits of a caller now, one block per budget key (R-UI-184's **Limits**, R-UI-185): the place, the rule that applies and
 * why, and every figure: a meter of used against the limit for the windowed ones, the rates as text, "Unlimited" where a figure
 * is.
 */
@Component({
  selector: 'quader-usage-budgets',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [UsageMeter],
  template: `
    @for (b of budgets(); track placeKey(b.place)) {
      <section class="budget" [attr.aria-label]="placeName(b.place)">
        <h3 translate="no">{{ placeName(b.place) }}</h3>
        <p class="why">{{ sentence(b) }}</p>
        <table class="figures">
          <caption class="visually-hidden">{{ placeName(b.place) }}</caption>
          <tbody>
            @for (row of windowed(b); track row.figure) {
              <tr>
                <th scope="row">{{ row.name }}</th>
                <td>
                  <quader-usage-meter
                    [ratio]="row.ratio"
                    [state]="stateOf(row.ratio)"
                    [mode]="mode()"
                    [label]="row.name"
                    [text]="row.text"
                  />
                </td>
              </tr>
            }
            @for (row of rates(b); track row.figure) {
              <tr>
                <th scope="row">{{ row.name }}</th>
                <td>{{ row.text }}</td>
              </tr>
            }
          </tbody>
        </table>
      </section>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    .budget {
      margin-block-end: var(--quader-space-5);
    }
    h3 {
      margin: 0;
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    .why {
      margin: var(--quader-space-1) 0 var(--quader-space-2);
      color: var(--quader-text-muted);
    }
    .figures {
      border-collapse: collapse;
      inline-size: 100%;
      max-inline-size: 40rem;
    }
    .figures th,
    .figures td {
      padding: var(--quader-space-1) var(--quader-space-3) var(--quader-space-1) 0;
      text-align: start;
      vertical-align: top;
      border-block-end: 1px solid var(--quader-border);
    }
    .figures th {
      font-weight: 400;
      inline-size: 14rem;
    }
  `,
})
export class UsageBudgets {
  private readonly locale = inject(LOCALE_ID);

  readonly budgets = input.required<readonly UsageBudget[]>();
  readonly mode = input<LimitMode>('audit');
  /** The caller's own limits: "Your limits come from …" (R-UI-185). */
  readonly own = input(false);

  protected readonly placeName = placeLabel;

  protected placeKey(p: LimitPlace): string {
    return p.kind === 'instance' ? 'instance' : p.kind === 'project' ? `p:${p.project}` : `r:${p.repository}`;
  }

  protected sentence(b: UsageBudget): string {
    return ruleSentence(b.rule, b.place, b.rule?.profileName ?? '', this.own());
  }

  protected stateOf(ratio: number | null) {
    return stateOfRatio(ratio, this.mode());
  }

  protected windowed(b: UsageBudget): FigureRow[] {
    return WINDOWED.map((f) => {
      const fig: BudgetFigure = b.figures[f];
      const limit = fig?.limit ?? null;
      const used = fig?.used ?? 0;
      if (limit === null) return { figure: f, name: figureName(f), ratio: null, text: '' };
      return { figure: f, name: figureName(f), ratio: used / limit, text: usedOfLimit(f, used, limit, this.locale) };
    });
  }

  protected rates(b: UsageBudget): FigureRow[] {
    return RATES.map((f) => {
      const fig = b.figures[f];
      let text = limitText(f, fig?.limit ?? null, this.locale);
      if (f === 'requestsPerSecond' && fig?.burst) text += `, ${limitText('requestsBurst', fig.burst, this.locale)}`;
      return { figure: f, name: figureName(f), ratio: null, text };
    });
  }
}
