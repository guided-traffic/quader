import { describe, expect, it } from 'vitest';
import { expectAccessible, renderScreen, text } from '../../../testing/screen';
import { UpstreamPopularity } from './upstream-popularity';

describe('What the upstream tells beside a package (R-ANS-047)', () => {
  it('R-ANS-047 says the downloads as the upstream’s, its signed state as a neutral badge, and its tags as text', async () => {
    const r = await renderScreen(UpstreamPopularity, { inputs: { keywords: ['docker', 'containers'], downloads: 70357068, signed: true } });
    expect(text(r.root.querySelector('[data-testid="upstream-downloads"]')!)).toBe('70,357,068 downloads at the upstream');
    const signed = r.root.querySelector('[data-testid="upstream-signed"]')!;
    expect(text(signed)).toContain('Signed at the upstream');
    expect(signed.classList.contains('success')).toBe(false);
    expect([...r.root.querySelectorAll('.tag')].map((t) => [t.tagName, text(t)])).toEqual([
      ['SPAN', 'docker'],
      ['SPAN', 'containers'],
    ]);
    await expectAccessible(r.root);
  });

  it('R-ANS-047 shows nothing an upstream does not tell, and a count of zero as such', async () => {
    const none = await renderScreen(UpstreamPopularity, { inputs: {} });
    expect(text(none.root)).toBe('');
    const zero = await renderScreen(UpstreamPopularity, { inputs: { downloads: 0, signed: false } });
    expect(text(zero.root.querySelector('[data-testid="upstream-downloads"]')!)).toBe('No download at the upstream');
    expect(zero.root.querySelector('[data-testid="upstream-signed"]')).toBeNull();
  });
});
