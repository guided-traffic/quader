import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Schema } from '../../api/types';
import { expectAccessible, settle } from '../../testing/screen';
import { DistributionMark, distributionText } from './distribution-mark';

type Member = Schema<'RepositoryBastion'>[];

@Component({
  imports: [DistributionMark],
  template: `<p><a href="#">image-source</a><quader-distribution-mark [bastions]="bastions()" [badge]="badge()" /></p>`,
})
class Host {
  readonly bastions = signal<Member>([]);
  readonly badge = signal(false);
}

async function render(bastions: Member, badge = false) {
  TestBed.resetTestingModule();
  await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
  const fixture = TestBed.createComponent(Host);
  fixture.componentInstance.bastions.set(bastions);
  fixture.componentInstance.badge.set(badge);
  await settle(fixture);
  const root = fixture.nativeElement as HTMLElement;
  return { fixture, root, mark: () => root.querySelector<HTMLElement>('[data-testid="distribution-mark"]') };
}

const artifacts = { name: 'artifacts', addresses: ['https://pkgs.example.com'], disabled: false };
const partners = { name: 'partners', addresses: ['https://pkgs.partner.example.com/', 'https://pkgs2.partner.example.com'], disabled: true };

describe('the distribution mark (R-UI-271)', () => {
  it('R-UI-271 shows nothing for a repository distributed through no Bastion', async () => {
    const { mark } = await render([]);
    expect(mark()).toBeNull();
  });

  it('R-UI-271 is the tower, named by the Bastion and its host, focusable, and passes axe', async () => {
    const { root, mark } = await render([artifacts]);
    const m = mark()!;
    expect(m.textContent?.trim()).toBe('fort');
    expect(m.getAttribute('role')).toBe('img');
    expect(m.getAttribute('aria-label')).toBe('Also distributed through the Bastion artifacts (pkgs.example.com)');
    expect(m.getAttribute('tabindex')).toBe('0');
    await expectAccessible(root);
  });

  it('R-UI-271 names every Bastion of several with its hosts, one without an address by its name, and a disabled one says so', () => {
    expect(distributionText([artifacts, partners])).toBe(
      'Also distributed through the Bastions artifacts (pkgs.example.com), partners (pkgs.partner.example.com, pkgs2.partner.example.com) (disabled now)',
    );
    expect(distributionText([{ ...artifacts, disabled: true }])).toBe(
      'Also distributed through the Bastion artifacts (pkgs.example.com) (disabled now)',
    );
    expect(distributionText([{ name: 'fresh', addresses: [], disabled: false }])).toBe('Also distributed through the Bastion fresh');
  });

  it('R-UI-271 is the badge Bastion in a head, described by its sentence, and passes axe', async () => {
    const { root, mark } = await render([artifacts], true);
    const badge = mark()!;
    expect(badge.classList).toContain('badge');
    expect(badge.textContent?.replace('fort', '').trim()).toBe('Bastion');
    const describedBy = badge.getAttribute('aria-describedby') ?? '';
    expect(document.getElementById(describedBy)?.textContent).toBe('Also distributed through the Bastion artifacts (pkgs.example.com)');
    await expectAccessible(root);
  });

  it('R-UI-271 a tap pins the tooltip, so it reads on a touch screen', async () => {
    const { fixture, mark } = await render([artifacts]);
    mark()!.click();
    await settle(fixture);
    expect(document.querySelector('quader-tooltip')?.textContent).toContain('Also distributed through the Bastion artifacts');
    mark()!.dispatchEvent(new MouseEvent('mouseleave'));
    await settle(fixture);
    expect(document.querySelector('quader-tooltip')).not.toBeNull();
  });
});
