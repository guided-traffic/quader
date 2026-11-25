import { MetadataLink } from './version-metadata';
import { safeHttpUrl } from './package-links';

/*
 * What a version's page reads of an image's facts (formats/oci.md R-OCI-039 to R-OCI-041): the kind of artifact, the facts of its
 * annotations and labels, the config and the layers of each manifest the repository holds, and the configuration a builder wrote. A
 * member of the wrong type is skipped, never an error: a pusher or an upstream wrote it. Every value is text.
 */

/** A blob an image manifest names: its config or a layer. */
export interface ImageBlob {
  readonly digest: string;
  readonly mediaType: string;
  readonly size: number | null;
  /** The layer is not distributed through a registry (urls, a non-distributable media type). */
  readonly foreign: boolean;
}

/** An environment entry or a label: the value ends where the reader's bound did when it is cut. */
export interface ImageVar {
  readonly name: string;
  readonly value: string;
  readonly cut: boolean;
}

/** One step of the build, with the layer it added, as `docker history` lists it. */
export interface HistoryStep {
  readonly created: string;
  readonly createdBy: string;
  readonly comment: string;
  /** The step added no layer (ENV, CMD, LABEL): `docker history` shows 0 B. */
  readonly emptyLayer: boolean;
  /** The layer the step added, matched in order; null for an empty step and where the layers run out. */
  readonly layer: ImageBlob | null;
  readonly cut: boolean;
}

/** Why a configuration is not shown: not read yet, too large, unreadable, or not for this reader. */
export type ConfigState = 'read' | 'not-read' | 'too-large' | 'invalid' | 'hidden';

/** What the reader read of one image's configuration. */
export interface ImageConfigView {
  readonly state: ConfigState;
  /** The facts of how the image runs, as label and text; `code` marks a command or a path shown as code. */
  readonly facts: readonly ImageFact[];
  readonly env: readonly ImageVar[];
  readonly labels: readonly ImageVar[];
  readonly history: readonly HistoryStep[];
  /** What the reader's bounds left out of each list, and whether a value was cut. */
  readonly omitted: { readonly env: number; readonly labels: number; readonly history: number; readonly cut: boolean };
}

export interface ImageFact {
  readonly label: string;
  readonly value: string;
  readonly code: boolean;
}

/** One image of the version: the tag's own manifest, or a platform of an index the repository holds. */
export interface ImagePart {
  /** The manifest's digest. */
  readonly digest: string;
  /** The platform the index names it with; null for the tag's own image. */
  readonly platform: string | null;
  readonly config: ImageBlob | null;
  readonly layers: readonly ImageBlob[];
  /** Layers past the bound that the row does not keep. */
  readonly moreLayers: number;
  /** The configuration; null for an artifact whose config is no image's configuration. */
  readonly configuration: ImageConfigView | null;
}

const text = (v: unknown): string => (typeof v === 'string' ? v : '');
const num = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);
const record = (v: unknown): Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
const texts = (v: unknown): string[] => list(v).filter((x): x is string => typeof x === 'string');

/** A digest cut in the middle: the algorithm, the first ten and the last six hex digits; the whole is its title and its copy. */
export function cutDigest(digest: string): string {
  const i = digest.indexOf(':');
  const hex = digest.slice(i + 1);
  return hex.length > 20 ? `${digest.slice(0, i + 1)}${hex.slice(0, 10)}…${hex.slice(-6)}` : digest;
}

/** The word for what a manifest holds (R-OCI-041), from the member `kind`; the media type is its title. */
export function kindWord(kind: string): string {
  switch (kind) {
    case 'image':
      return $localize`:@@pkg.image.kind.image:Image`;
    case 'helm-chart':
      return $localize`:@@pkg.image.kind.helm-chart:Helm chart`;
    case 'signature':
      return $localize`:@@pkg.image.kind.signature:Signature`;
    case 'sbom':
      return $localize`:@@pkg.image.kind.sbom:SBOM`;
    case 'attestation':
      return $localize`:@@pkg.image.kind.attestation:Attestation`;
    case '':
      return '';
  }
  return $localize`:@@pkg.image.kind.other:Other artifact`;
}

function blob(v: unknown): ImageBlob | null {
  const b = record(v);
  const digest = text(b['digest']);
  return digest === '' ? null : { digest, mediaType: text(b['mediaType']), size: num(b['size']), foreign: b['foreign'] === true };
}

function vars(v: unknown): ImageVar[] {
  return list(v).flatMap((x): ImageVar[] => {
    const r = record(x);
    const name = text(r['name']);
    return name === '' && text(r['value']) === '' ? [] : [{ name, value: text(r['value']), cut: r['cut'] === true }];
  });
}

/** Nanoseconds as `docker inspect` gives a health check's durations: "30s", "1m30s", "500ms". */
export function duration(ns: number): string {
  if (ns <= 0) return '';
  if (ns < 1e9) return `${Math.round(ns / 1e6)}ms`;
  const s = Math.round(ns / 1e9);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const rest = s % 60;
  return `${h > 0 ? `${h}h` : ''}${m > 0 ? `${m}m` : ''}${rest > 0 || (h === 0 && m === 0) ? `${rest}s` : ''}`;
}

/** A command as the Dockerfile's exec form shows it: `["/app/run", "--port", "8080"]`. */
export function execForm(args: readonly string[]): string {
  return args.length === 0 ? '' : JSON.stringify(args);
}

function runFacts(img: Record<string, unknown>): ImageFact[] {
  const platform = [text(img['os']), text(img['architecture']), text(img['variant'])].filter((p) => p !== '').join('/');
  const h = record(img['healthcheck']);
  const test = texts(h['test']);
  const check = [
    execForm(test),
    ...[
      [$localize`:@@pkg.config.interval:interval`, duration(num(h['interval']) ?? 0)],
      [$localize`:@@pkg.config.timeout:timeout`, duration(num(h['timeout']) ?? 0)],
      [$localize`:@@pkg.config.start-period:start period`, duration(num(h['startPeriod']) ?? 0)],
      [$localize`:@@pkg.config.retries:retries`, (num(h['retries']) ?? 0) > 0 ? String(num(h['retries'])) : ''],
    ]
      .filter(([, v]) => v !== '')
      .map(([k, v]) => `${k} ${v}`),
  ]
    .filter((x) => x !== '')
    .join(', ');
  const facts: ImageFact[] = [
    { label: $localize`:@@pkg.config.platform:Platform`, value: platform + (text(img['osVersion']) ? ` (${text(img['osVersion'])})` : ''), code: false },
    { label: $localize`:@@pkg.config.created:Built`, value: text(img['created']), code: false },
    { label: $localize`:@@pkg.config.author:Author`, value: text(img['author']), code: false },
    { label: $localize`:@@pkg.config.entrypoint:Entrypoint`, value: execForm(texts(img['entrypoint'])), code: true },
    { label: $localize`:@@pkg.config.cmd:Command`, value: execForm(texts(img['cmd'])), code: true },
    { label: $localize`:@@pkg.config.workdir:Working directory`, value: text(img['workingDir']), code: true },
    { label: $localize`:@@pkg.config.user:User`, value: text(img['user']), code: true },
    { label: $localize`:@@pkg.config.ports:Exposed ports`, value: texts(img['exposedPorts']).join(', '), code: true },
    { label: $localize`:@@pkg.config.volumes:Volumes`, value: texts(img['volumes']).join(', '), code: true },
    { label: $localize`:@@pkg.config.stop-signal:Stop signal`, value: text(img['stopSignal']), code: true },
    { label: $localize`:@@pkg.config.healthcheck:Health check`, value: check, code: true },
    { label: $localize`:@@pkg.config.shell:Shell`, value: execForm(texts(img['shell'])), code: true },
    { label: $localize`:@@pkg.config.onbuild:On build`, value: texts(img['onBuild']).join('\n'), code: true },
  ];
  return facts.filter((f) => f.value !== '');
}

/** The history with the layer each step added, matched in order as `docker history` matches them. */
function historyOf(v: unknown, layers: readonly ImageBlob[]): HistoryStep[] {
  let next = 0;
  return list(v).map((x): HistoryStep => {
    const r = record(x);
    const emptyLayer = r['emptyLayer'] === true;
    const layer = emptyLayer ? null : (layers[next++] ?? null);
    return {
      created: text(r['created']),
      createdBy: text(r['createdBy']),
      comment: text(r['comment']),
      emptyLayer,
      layer,
      cut: r['cut'] === true,
    };
  });
}

function configurationOf(v: unknown, layers: readonly ImageBlob[]): ImageConfigView {
  const c = record(v);
  const none = { env: 0, labels: 0, history: 0, cut: false };
  const reason = text(c['unreadable']);
  const img = record(c['image']);
  if (reason !== '' || c['image'] === undefined) {
    const state: ConfigState = reason === 'too-large' ? 'too-large' : reason === 'not-read' ? 'not-read' : 'invalid';
    return { state, facts: [], env: [], labels: [], history: [], omitted: none };
  }
  const om = record(img['omitted']);
  return {
    state: 'read',
    facts: runFacts(img),
    env: vars(img['env']),
    labels: vars(img['labels']),
    history: historyOf(img['history'], layers),
    omitted: { env: num(om['env']) ?? 0, labels: num(om['labels']) ?? 0, history: num(om['history']) ?? 0, cut: om['cut'] === true },
  };
}

/**
 * The images of a version: the tag's own manifest and every platform of an index the repository holds, each with its config, its layers
 * in the manifest's order and its configuration. `platforms` are the index's children as `imageFacts` read them (digest and label).
 * A configuration the reader may not see (`configs` absent from an image's metadata) is `hidden`.
 */
export function imageParts(m: Record<string, unknown>, platforms: readonly { digest: string; platform: string; stored: boolean }[]): ImagePart[] {
  const blobs = record(m['blobs']);
  const configs = m['configs'];
  const own = text(m['digest']);
  const kind = text(m['kind']);
  const candidates: { digest: string; platform: string | null }[] =
    platforms.length > 0 ? platforms.filter((p) => p.stored).map((p) => ({ digest: p.digest, platform: p.platform })) : [{ digest: own, platform: null }];
  return candidates.flatMap((c): ImagePart[] => {
    const b = record(blobs[c.digest]);
    if (blobs[c.digest] === undefined) return [];
    const layers = list(b['layers']).flatMap((l) => {
      const x = blob(l);
      return x ? [x] : [];
    });
    const cfg = record(configs)[c.digest];
    let configuration: ImageConfigView | null = null;
    if (cfg !== undefined) configuration = configurationOf(cfg, layers);
    else if (configs === undefined && kind === 'image') {
      configuration = { state: 'hidden', facts: [], env: [], labels: [], history: [], omitted: { env: 0, labels: 0, history: 0, cut: false } };
    }
    return [{ digest: c.digest, platform: c.platform, config: blob(b['config']), layers, moreLayers: num(b['more']) ?? 0, configuration }];
  });
}

/** The keys of the image specification the page shows as facts, beside the description, the licence and the links. */
function factMembers(): [string, string][] {
  return [
    ['title', $localize`:@@pkg.image.fact.title:Title`],
    ['imageVersion', $localize`:@@pkg.image.fact.version:Version`],
    ['revision', $localize`:@@pkg.image.fact.revision:Revision`],
    ['created', $localize`:@@pkg.image.fact.created:Created`],
    ['vendor', $localize`:@@pkg.image.fact.vendor:Vendor`],
    ['baseName', $localize`:@@pkg.image.fact.base:Base image`],
    ['baseDigest', $localize`:@@pkg.image.fact.base-digest:Base image digest`],
  ];
}

/** A fact of the image: the annotations' (top-level members) first, then the labels' (`labeled`), as the server merged them. */
export function imageFact(m: Record<string, unknown>, member: string): string {
  const given = text(m[member]).trim();
  return given !== '' ? given : text(record(m['labeled'])[member]).trim();
}

/** The facts of an image for the section Package details: title, version, revision, created, vendor and base image. */
export function imageFactList(m: Record<string, unknown>): [string, string][] {
  return factMembers()
    .map(([member, label]): [string, string] => [label, imageFact(m, member)])
    .filter((f) => f[1] !== '');
}

/** The links of an image (R-OCI-039): homepage, documentation and source, from the annotations or else the labels. */
export function imageLinks(m: Record<string, unknown>): MetadataLink[] {
  const entries: [string, string][] = [
    [$localize`:@@pkg.meta.homepage:Homepage`, imageFact(m, 'homepage')],
    [$localize`:@@pkg.meta.documentation:Documentation`, imageFact(m, 'documentationUrl')],
    [$localize`:@@pkg.meta.source:Source repository`, imageFact(m, 'repositoryUrl')],
  ];
  return entries.flatMap(([label, value]): MetadataLink[] => (value !== '' ? [{ label, text: value, href: safeHttpUrl(value) }] : []));
}

/** A Helm chart's facts, from its Chart.yaml (formats/oci.md R-OCI-042). */
export interface ChartView {
  /** The facts as label and text: name, version, app version, type, Kubernetes version, keywords. */
  readonly facts: readonly [string, string][];
  readonly maintainers: readonly string[];
  /** The dependencies: the chart's name (or alias) and its version range, with the repository it comes from. */
  readonly dependencies: readonly { readonly name: string; readonly range: string; readonly repository: string }[];
  readonly deprecated: boolean;
}

/** The chart of a version whose kind is a Helm chart; null for anything else or a chart whose Chart.yaml the reader may not see. */
export function chartOf(m: Record<string, unknown>): ChartView | null {
  if (m['kind'] !== 'helm-chart') return null;
  const c = record(record(record(m['configs'])[text(m['digest'])])['chart']);
  if (Object.keys(c).length === 0) return null;
  const facts: [string, string][] = [
    [$localize`:@@pkg.chart.name:Chart`, text(c['name'])],
    [$localize`:@@pkg.chart.version:Chart version`, text(c['version'])],
    [$localize`:@@pkg.chart.app-version:App version`, text(c['appVersion'])],
    [$localize`:@@pkg.chart.type:Type`, text(c['type'])],
    [$localize`:@@pkg.chart.kube-version:Kubernetes version`, text(c['kubeVersion'])],
    [$localize`:@@pkg.chart.keywords:Keywords`, texts(c['keywords']).join(', ')],
  ];
  const maintainers = list(c['maintainers']).flatMap((x) => {
    const r = record(x);
    const who = [text(r['name']), text(r['email']) ? `<${text(r['email'])}>` : ''].filter((s) => s !== '').join(' ');
    return who === '' ? [] : [who];
  });
  const dependencies = list(c['dependencies']).flatMap((x) => {
    const r = record(x);
    const name = text(r['alias']) || text(r['name']);
    return name === '' ? [] : [{ name, range: text(r['version']) || '*', repository: text(r['repository']) }];
  });
  return { facts: facts.filter((f) => f[1] !== ''), maintainers, dependencies, deprecated: c['deprecated'] === true };
}

/** A Helm chart's values.yaml read from its package (R-OCI-042); null where it has none or the reader may not see it. */
export function chartValues(m: Record<string, unknown>): { readonly text: string; readonly cut: boolean } | null {
  const f = record(m['chartFiles']);
  return f['hasValues'] === true ? { text: text(f['values']), cut: f['valuesCut'] === true } : null;
}
