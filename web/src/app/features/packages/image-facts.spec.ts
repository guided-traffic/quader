import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { expectAccessible, settle } from '../../testing/screen';
import { HISTORY_SHOWN, ImageDetails } from './image-details';
import { ImagePart, cutDigest, duration, imageFact, imageFactList, imageLinks, imageParts, kindWord } from './image-facts';

const D = (n: number): string => `sha256:${String(n).padStart(2, '0').repeat(32)}`;

/** An index with two platforms, one held with its configuration, as the view API sends it (formats/oci.md R-OCI-039 to R-OCI-041). */
function indexMetadata(): Record<string, unknown> {
  return {
    digest: D(1),
    kind: 'image',
    description: 'From the annotation',
    homepage: 'javascript:alert(1)',
    blobs: {
      [D(2)]: {
        config: { digest: D(5), mediaType: 'application/vnd.oci.image.config.v1+json', size: 1200 },
        layers: [
          { digest: D(6), mediaType: 'application/vnd.oci.image.layer.v1.tar+gzip', size: 3_000_000 },
          { digest: D(7), mediaType: 'application/vnd.oci.image.layer.v1.tar+gzip', size: 512 },
        ],
      },
    },
    configs: {
      [D(2)]: {
        kind: 'image',
        image: {
          os: 'linux',
          architecture: 'amd64',
          entrypoint: ['/app/run'],
          cmd: ['--port', '8080'],
          healthcheck: { test: ['CMD', '/app/check'], interval: 30_000_000_000, retries: 2 },
          env: [{ name: 'TOKEN', value: 'secret', cut: true }],
          labels: [{ name: 'org.opencontainers.image.source', value: 'https://example.com/src' }],
          history: [
            { createdBy: 'ADD rootfs.tar.gz /' },
            { createdBy: 'ENV A=1', emptyLayer: true },
            { createdBy: 'RUN make', comment: 'buildkit.dockerfile.v0' },
          ],
          omitted: { history: 3 },
        },
      },
    },
    labeled: { repositoryUrl: 'https://example.com/src', licenses: 'MIT', vendor: 'Acme' },
  };
}

const platforms = [
  { digest: D(2), platform: 'linux/amd64', stored: true },
  { digest: D(3), platform: 'linux/arm64', stored: false },
];

describe('Image facts', () => {
  it('R-OCI-039 an annotation wins over a label, a link is a link only for http and https', () => {
    const m = indexMetadata();
    expect(imageFact(m, 'description')).toBe('From the annotation');
    expect(imageFact(m, 'licenses')).toBe('MIT');
    expect(imageFactList(m)).toEqual([['Vendor', 'Acme']]);
    const links = imageLinks(m);
    expect(links.map((l) => [l.label, l.href])).toEqual([
      ['Homepage', null],
      ['Source repository', 'https://example.com/src'],
    ]);
  });

  it('R-OCI-040 the parts are the held platforms, with the layer each step of the history added', () => {
    const parts = imageParts(indexMetadata(), platforms);
    expect(parts.length).toBe(1);
    const p = parts[0]!;
    expect(p.platform).toBe('linux/amd64');
    expect(p.layers.map((l) => l.digest)).toEqual([D(6), D(7)]);
    const c = p.configuration!;
    expect(c.state).toBe('read');
    expect(c.history.map((h) => h.layer?.digest ?? null)).toEqual([D(6), null, D(7)]);
    expect(c.facts.find((f) => f.label === 'Command')?.value).toBe('["--port","8080"]');
    expect(c.facts.find((f) => f.label === 'Health check')?.value).toBe('["CMD","/app/check"], interval 30s, retries 2');
    expect(c.omitted.history).toBe(3);
  });

  it('R-OCI-040 a configuration the reader may not see, one not read yet and an artifact without one', () => {
    const m = indexMetadata();
    delete m['configs'];
    expect(imageParts(m, platforms)[0]!.configuration?.state).toBe('hidden');
    const unread = { ...indexMetadata(), configs: { [D(2)]: { kind: 'image', unreadable: 'not-read' } } };
    expect(imageParts(unread, platforms)[0]!.configuration?.state).toBe('not-read');
    const chart: Record<string, unknown> = { ...indexMetadata(), kind: 'helm-chart' };
    delete chart['configs'];
    expect(imageParts(chart, platforms)[0]!.configuration).toBeNull();
  });

  it('R-OCI-041 the word for the kind, the digest cut in the middle, a duration as docker inspect writes it', () => {
    expect(kindWord('helm-chart')).toBe('Helm chart');
    expect(kindWord('something')).toBe('Other artifact');
    expect(cutDigest(D(12))).toBe('sha256:1212121212…121212');
    expect(duration(90_000_000_000)).toBe('1m30s');
    expect(duration(500_000_000)).toBe('500ms');
  });
});

describe('Image details', () => {
  @Component({ imports: [ImageDetails], template: `<quader-image-details [part]="part()" />` })
  class Host {
    readonly part = signal<ImagePart>(imageParts(indexMetadata(), platforms)[0]!);
  }

  async function render(part?: ImagePart): Promise<HTMLElement> {
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    if (part) fixture.componentInstance.part.set(part);
    await settle(fixture);
    return fixture.nativeElement as HTMLElement;
  }

  it('R-OCI-040 the configuration, the environment as text, the layers and the history with its sizes', async () => {
    const root = await render();
    const headings = [...root.querySelectorAll('h3')].map((h) => h.textContent?.trim());
    expect(headings).toEqual(['Configuration', 'Environment', 'Labels', 'Layers', 'History']);
    expect(root.textContent).toContain('secret');
    expect(root.textContent).toContain('(cut)');
    expect(root.querySelector('code[title="' + D(6) + '"]')?.textContent).toBe(cutDigest(D(6)));
    expect(root.textContent).toContain('3 more steps are not kept.');
    await expectAccessible(root);
  });

  it('R-UI-215 a long history shows its first steps and the rest on request, growing the page', async () => {
    const base = imageParts(indexMetadata(), platforms)[0]!;
    const steps = Array.from({ length: HISTORY_SHOWN + 10 }, (_, i) => ({
      created: '',
      createdBy: `RUN step ${i}`,
      comment: '',
      emptyLayer: true,
      layer: null,
      cut: false,
    }));
    const root = await render({ ...base, configuration: { ...base.configuration!, history: steps } });
    expect(root.querySelectorAll('.history tbody tr').length).toBe(HISTORY_SHOWN);
    const more = [...root.querySelectorAll('button')].find((b) => b.textContent?.includes('Show all'))!;
    more.click();
    await settle(TestBed.createComponent(Host));
    expect(more.textContent?.trim()).toBe(`Show all ${HISTORY_SHOWN + 10} steps`);
  });

  it('R-OCI-040 Q-OCI-273 a reader who may not pull the image is told whom the configuration is shown to', async () => {
    const base = imageParts(indexMetadata(), platforms)[0]!;
    const root = await render({ ...base, configuration: { ...base.configuration!, state: 'hidden' } });
    expect(root.textContent).toContain('shown to whoever may pull this image');
    expect(root.textContent).not.toContain('secret');
  });
});
