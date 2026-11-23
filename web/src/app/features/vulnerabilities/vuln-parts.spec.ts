// The severity badge (R-VULN-022): an icon and a word, the icon in the severity's mark tone of the one scale the bar and the trend
// use. jsdom computes no colours, so the component test holds the structure and the stylesheet's rules hold the colour.
import vulnerabilitiesScss from '../../../styles/_vulnerabilities.scss' with { loader: 'text' };
import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { VulnerabilitySeverity } from '../../api/vulnerabilities-api';
import { expectAccessible, settle, text } from '../../testing/screen';
import { SeverityBadge } from './vuln-parts';

const SEVERITIES: readonly VulnerabilitySeverity[] = ['critical', 'high', 'medium', 'low', 'none', 'unknown'];

@Component({
  imports: [SeverityBadge],
  template: `
    @for (s of severities; track s) {
      <p><quader-severity [severity]="s" [score]="s === 'critical' ? score() : null" /></p>
    }
  `,
})
class Host {
  readonly severities = SEVERITIES;
  readonly score = signal<number | null>(9.8);
}

/** The declarations of the rule whose selector is exactly `selector`, without comments. */
function rule(selector: string): string {
  const at = vulnerabilitiesScss.indexOf(`\n${selector} {`);
  expect(at, `no rule ${selector}`).toBeGreaterThan(-1);
  const open = vulnerabilitiesScss.indexOf('{', at);
  return vulnerabilitiesScss.slice(open + 1, vulnerabilitiesScss.indexOf('}', open)).replace(/\/\/.*$/gm, '').trim();
}

describe('Severity badge (R-VULN-022)', () => {
  it('R-VULN-022 R-UI-044 every severity is its icon and its word, the score beside Critical, and the badge carries its severity\'s class', async () => {
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    const badges = [...root.querySelectorAll<HTMLElement>('quader-severity > .sev')];
    expect(badges.map((b) => b.className)).toEqual(SEVERITIES.map((s) => `sev sev-${s}`));
    expect(badges.map((b) => b.querySelector('.material-icons')?.textContent?.trim())).toEqual([
      'error',
      'report',
      'warning',
      'info',
      'remove_circle_outline',
      'help_outline',
    ]);
    const words = (b: Element) => {
      const copy = b.cloneNode(true) as Element;
      copy.querySelector('.material-icons')?.remove();
      return text(copy);
    };
    expect(badges.map(words)).toEqual(['Critical 9.8', 'High', 'Medium', 'Low', 'None', 'Unknown']);
    // A score is written as CVSS writes it, with one decimal: a whole score and a score of 0 keep it (G5-55: "None 0").
    for (const [score, shown] of [[9, 'Critical 9.0'], [0, 'Critical 0.0'], [10, 'Critical 10.0']] as const) {
      fixture.componentInstance.score.set(score);
      await settle(fixture);
      expect(words(root.querySelector('quader-severity > .sev')!)).toBe(shown);
    }
    // Only the icon is coloured: the word and the score carry no class of their own that could tint them.
    for (const b of badges) {
      const word = b.querySelector('.material-icons + span')!;
      expect(word.className).toBe('');
    }
    await expectAccessible(root);
  });

  it('R-VULN-022 R-UI-092 the icon takes its severity\'s mark tone and the word the text around it, never a status colour: Critical and High differ', () => {
    expect(rule('.sev .material-icons')).toBe('color: var(--sev-mark);');
    const marks = {
      critical: 'var(--quader-sev-critical-mark)',
      high: 'var(--quader-sev-high-mark)',
      medium: 'var(--quader-sev-medium-mark)',
      low: 'var(--quader-sev-low-mark)',
      none: 'var(--quader-sev-none)',
      unknown: 'var(--quader-sev-unknown-mark)',
    };
    for (const [s, mark] of Object.entries(marks)) expect(rule(`.sev.sev-${s}`), s).toBe(`--sev-mark: ${mark};`);
    // No rule of the stylesheet colours a severity class with a status colour or sets the word's colour.
    expect(vulnerabilitiesScss).not.toMatch(/\.sev-(critical|high|medium|low|none|unknown)[^{]*\{[^}]*--quader-status-/);
    expect(vulnerabilitiesScss).not.toMatch(/(^|\n)\.sev[^{\n]*\{[^}]*\bcolor: var\(--quader-(status|text-muted)/);
  });
});
