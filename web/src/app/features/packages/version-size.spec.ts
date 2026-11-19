import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { expectAccessible, settle, text } from '../../testing/screen';
import { VersionSize } from './version-size';

describe('Version size', () => {
  @Component({ imports: [VersionSize], template: `<p><quader-version-size [bytes]="bytes()" [partial]="partial()" /></p>` })
  class Host {
    readonly bytes = signal<number | null>(48_213);
    readonly partial = signal(false);
  }

  async function render(bytes: number | null, partial = false): Promise<HTMLElement> {
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.bytes.set(bytes);
    fixture.componentInstance.partial.set(partial);
    await settle(fixture);
    return fixture.nativeElement as HTMLElement;
  }

  it('R-API-080 a known size is the number in binary units with the bytes as its title', async () => {
    const root = await render(48_213);
    expect(text(root)).toBe('47.1 KiB');
    expect(root.querySelector('[title]')?.getAttribute('title')).toBe('48213 B');
    await expectAccessible(root);
  });

  it('R-API-080 a size that is not known is the shared no-value with a title that says why, never 0 B', async () => {
    const root = await render(null);
    expect(text(root)).not.toContain('0 B');
    expect(root.querySelector('quader-no-value')).not.toBeNull();
    expect(root.querySelector('[title]')?.getAttribute('title')).toContain('no platform of this image has been fetched');
    await expectAccessible(root);
  });

  it('R-API-080 in a line of facts a size that is not known is said in words, with the same title', async () => {
    @Component({ imports: [VersionSize], template: `<p><quader-version-size [bytes]="null" [spelled]="true" /></p>` })
    class Spelled {}
    await TestBed.configureTestingModule({ imports: [Spelled] }).compileComponents();
    const fixture = TestBed.createComponent(Spelled);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    expect(text(root)).toBe('Size not known');
    expect(root.querySelector('[title]')?.getAttribute('title')).toContain('no platform of this image has been fetched');
  });

  it('R-API-080 Q-OCI-221 a size that counts only the platforms fetched so far reads "at least" and says what it leaves out', async () => {
    const root = await render(1_048_576, true);
    expect(text(root)).toContain('1.0 MiB');
    expect(root.textContent).toContain('≥');
    expect(root.querySelector('.visually-hidden')?.textContent).toContain('at least');
    expect(root.querySelector('[title]')?.getAttribute('title')).toContain('the platforms of this image fetched so far');
    await expectAccessible(root);
  });
});
