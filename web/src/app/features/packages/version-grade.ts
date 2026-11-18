import { Component, computed, inject, input, LOCALE_ID } from '@angular/core';
import { VulnerabilityGrade } from '../../api/vulnerabilities-api';
import { Tooltip } from '../../shared/ui/tooltip';
import { GradeShield } from '../../shared/vulnerability/grade-shield';
import { gradeCountsText, gradeRule } from '../../shared/vulnerability/grade-words';

/** The open findings of a grade, summed over the severities. */
export function totalOf(g: VulnerabilityGrade): number {
  const o = g.open;
  return o.critical + o.high + o.medium + o.low + o.none + o.unknown;
}

/**
 * What a version's row and its page say of its vulnerabilities (R-UI-112, Q-UI-454): the shield, which carries the letter, then the total of
 * its open findings and how many of them have a fix, "1,145 findings · 112 fixable", one statement of one grade; a screen reader
 * hears the letter before it. The counts per
 * severity and the rule of the letter are the tooltip; the card of the version's page has them in full.
 */
export function gradeSummaryText(g: VulnerabilityGrade, locale: string): string {
  if (g.grade === 'N/A') return $localize`:@@pkg.grade.not-scanned:not scanned`;
  const total = totalOf(g);
  if (total === 0) return $localize`:@@pkg.grade.no-findings:no findings`;
  const n = new Intl.NumberFormat(locale);
  const findings =
    total === 1
      ? $localize`:@@pkg.grade.one-finding:1 finding`
      : $localize`:@@pkg.grade.findings:${n.format(total)}:count: findings`;
  return `${findings} · ${$localize`:@@pkg.grade.fixable:${n.format(g.fixable)}:count: fixable`}`;
}

@Component({
  selector: 'quader-version-grade',
  imports: [GradeShield, Tooltip],
  template: `
    <span class="version-grade" [quaderTooltip]="tooltip()">
      <quader-grade-shield [grade]="grade().grade" />
      <span class="version-grade-text"
        ><span class="visually-hidden"
          ><ng-container i18n="@@vuln.grade.prefix">Grade</ng-container>&ngsp;{{ grade().grade }} · </span
        >{{ text() }}</span
      >
    </span>
  `,
  styles: `
    :host {
      display: inline-block;
      max-inline-size: 100%;
      vertical-align: top;
    }
    .version-grade {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
    }
    /* The shield (20 px) is centred on the line it stands in, whatever that line's height (16 px in a table, 21 in a paragraph),
       and adds nothing to it. */
    quader-grade-shield {
      flex: none;
      margin-block: calc((1lh - 1.25rem) / 2);
    }
    .version-grade-text {
      min-inline-size: 0;
      white-space: nowrap;
    }
  `,
})
export class VersionGrade {
  private readonly locale = inject(LOCALE_ID);

  readonly grade = input.required<VulnerabilityGrade>();

  protected readonly text = computed(() => gradeSummaryText(this.grade(), this.locale));
  /** The rule of the letter and every count, for a reader who wants to know why it is that letter. */
  protected readonly tooltip = computed(() => {
    const g = this.grade();
    return g.grade === 'N/A' ? gradeRule(g, false) : `${gradeRule(g, false)}: ${gradeCountsText(g)}`;
  });
}
