import { afterEach, describe, expect, it } from 'vitest';
import { aroundValue } from './value-list';

/**
 * A value in running text (a host, a key, a path) as the global stylesheet sets it (R-UI-171): `code translate="no"` moves to the next
 * line whole where it fits on one, and breaks inside itself only where it is longer than the line. Measured in the states' browser
 * with the app's stylesheet and fonts: milliseconds, no stack.
 */
describe('a value in running text (R-UI-171)', () => {
  let box: HTMLElement | null = null;
  afterEach(() => box?.remove());

  /** The lines a value's text stands on: the boxes of its text, those whose tops are less than half a line apart counted once. */
  function linesOf(el: Element): number {
    const range = document.createRange();
    range.selectNodeContents(el);
    const tops = [...range.getClientRects()].map((r) => r.top).sort((a, b) => a - b);
    return tops.filter((t, i) => i === 0 || t - tops[i - 1]! > 8).length;
  }

  it('R-UI-171 a host at the end of a line moves whole to the next one instead of breaking at its hyphen', () => {
    box = document.createElement('p');
    document.body.append(box);
    // The widths at which the sentence with the host as plain text breaks it at its hyphen: the case the rule is for.
    const plain = 'The credential is sent to auth.docker.io and <span id="v">registry-1.docker.io</span>.';
    const marked = 'The credential is sent to auth.docker.io and <code id="v" translate="no">registry-1.docker.io</code>.';
    const widths: number[] = [];
    for (let px = 160; px <= 640; px += 2) {
      box.style.inlineSize = `${px}px`;
      box.innerHTML = plain;
      if (linesOf(box.querySelector('#v')!) > 1) widths.push(px);
    }
    expect(widths.length).toBeGreaterThan(0);
    for (const px of widths) {
      box.style.inlineSize = `${px}px`;
      box.innerHTML = marked;
      expect(linesOf(box.querySelector('#v')!), `at ${px} px`).toBe(1);
    }
  });

  it('R-UI-171 a value longer than the line breaks inside its box and does not run out of it', () => {
    box = document.createElement('p');
    box.style.inlineSize = '10rem';
    box.innerHTML = 'Secret <code id="v" translate="no">quader-upstream-docker-hub-account-of-the-platform-team</code>.';
    document.body.append(box);
    const value = box.querySelector('#v')!;
    expect(linesOf(value)).toBeGreaterThan(1);
    expect(value.getBoundingClientRect().right).toBeLessThanOrEqual(box.getBoundingClientRect().right + 0.5);
  });

  it('R-UI-171 a value and its full stop stay on one line in with-stop (Q-UI-1615)', () => {
    box = document.createElement('p');
    document.body.append(box);
    // The widths at which the value's box ends a line and its full stop begins the next: the case the class is for.
    const sentence = (wrap: boolean) =>
      'The snippets read it from ' +
      (wrap ? '<span class="with-stop">' : '') +
      '<code id="v" translate="no">QUADER_TOKEN</code><span id="s">.</span>' +
      (wrap ? '</span>' : '');
    const top = (id: string) => box!.querySelector(`#${id}`)!.getBoundingClientRect().top;
    const widths: number[] = [];
    for (let px = 120; px <= 480; px += 1) {
      box.style.inlineSize = `${px}px`;
      box.innerHTML = sentence(false);
      if (top('s') - top('v') > 8) widths.push(px);
    }
    expect(widths.length).toBeGreaterThan(0);
    for (const px of widths) {
      box.style.inlineSize = `${px}px`;
      box.innerHTML = sentence(true);
      expect(Math.abs(top('s') - top('v')), `at ${px} px`).toBeLessThan(8);
    }
  });

  it('R-UI-171 a sentence of the translation splits at the place of its value', () => {
    expect(aroundValue((v) => `The credential is sent to ${v}.`)).toEqual({ before: 'The credential is sent to ', after: '.' });
    expect(aroundValue((v) => `${v} geprüft`)).toEqual({ before: '', after: ' geprüft' });
  });
});
