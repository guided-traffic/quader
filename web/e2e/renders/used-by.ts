import { APIRequestContext } from '@playwright/test';
import { NpmSeed, expectStatus, mainProject, npmPublishBody, patient } from './seed';

/**
 * "Used by" of a version's page (R-API-086, W-30 G5): the packages that depend on a package. @acme/ui-kit is used by two packages, the
 * portal's shell (facts.ts, a dependency) and the admin console (a development dependency); @acme/icons by eighty widgets, more than the
 * section lists (50), so it says how many more and leads to the package list filtered by the dependency; the admin console by none. The
 * NuGet, Debian and Ansible Galaxy pages are those of facts.ts and debdocs.ts: Acme.Logging.Abstractions is used by Acme.Platform.Http
 * with two ranges, acme-observability-agent by acme-log-forwarder under its own name and a name it provides, acme.platform by
 * acme.network. A few hundred bytes each; a run again answers conflicts.
 */
export const usedByTwo = { format: 'npm', name: '@acme/ui-kit', version: '1.1.0' } as const;
export const usedByMany = { format: 'npm', name: '@acme/icons', version: '2.0.0' } as const;
export const usedByNone = { format: 'npm', name: '@acme/admin-console', version: '0.4.0' } as const;
export const widgetCount = 80;

export const usedByPackages: readonly NpmSeed[] = [
  {
    repo: 'npm-internal',
    name: usedByNone.name,
    version: usedByNone.version,
    dependencies: { '@acme/icons': '^2.0.0', react: '^19.0.0' },
    devDependencies: { '@acme/ui-kit': '^1.0.0', typescript: '~5.7.2' },
  },
  { repo: 'npm-internal', name: usedByMany.name, version: usedByMany.version },
  ...Array.from({ length: widgetCount }, (_, i) => ({
    repo: 'npm-internal',
    name: `@acme/widget-${String(i + 1).padStart(2, '0')}`,
    version: `1.${i % 4}.0`,
    dependencies: { '@acme/icons': i % 3 === 0 ? '^2.0.0' : '>=1.8.0 <3' },
  })),
];

export async function seedUsedBy(ctx: APIRequestContext, edge: string, token: string): Promise<void> {
  const auth = { Authorization: `Bearer ${token}` };
  for (const p of usedByPackages) {
    const url = `/api/npm/${mainProject}/${p.repo}/${p.name.replace('/', '%2f')}`;
    const res = await patient(() => ctx.put(url, { data: npmPublishBody(edge, mainProject, p), headers: auth }));
    await expectStatus(res, [200, 201, 409], `npm publish ${p.name}@${p.version}`);
  }
}
