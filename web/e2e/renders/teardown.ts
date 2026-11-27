import { existsSync } from 'node:fs';
import { request } from '@playwright/test';
import { authFile, edge } from './paths';
import { Api } from './seed';
import sheet from './sheet';

/**
 * The global teardown of `make ui-renders`: the contact sheet (sheet.ts), then the instance's vulnerability database back. The seed's
 * `resetImageScans` (scanners.ts) removes the database in use, so that the image pages read **No database** whichever backend ran before;
 * without it the instance stayed without a database until the next six-hourly run of `vuln-sync-trivy-db`, and every image it scanned
 * waited (the owner's instance 1 twice on 2026-11-27, R-VULN-053). The run asks the task once through the API as the administrator,
 * as **Import now** does; it does not wait for it. The task revalidates the tag of `upstream/scanner-trivy` and records the layer the
 * repository still holds, so it fetches the layer only when the repository lost it. A job of CI (`CI` set) renders against a profile of its
 * own that ends with the job, and asks nothing of the publisher.
 */
export default async function teardown(): Promise<void> {
  sheet();
  if (process.env['CI'] || !existsSync(authFile('admin'))) return;
  const ctx = await request.newContext({ baseURL: edge, ignoreHTTPSErrors: true, storageState: authFile('admin') });
  try {
    const api = await Api.of(ctx, edge);
    const res = await api.post('/api/v1/system/tasks/vuln-sync-trivy-db/run', undefined);
    // 409: a run of the task is queued or running already, which records the database as well.
    if (![202, 409].includes(res.status())) {
      console.warn(`ui-renders: the run of vuln-sync-trivy-db that gives the instance its database back: ${res.status()} ${(await res.text()).slice(0, 300)}`);
    }
  } catch (e) {
    console.warn(`ui-renders: the run of vuln-sync-trivy-db that gives the instance its database back failed: ${String(e)}`);
  } finally {
    await ctx.dispose();
  }
}
