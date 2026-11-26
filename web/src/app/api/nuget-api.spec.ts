import { HttpHeaders } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it, vi } from 'vitest';
import { ApiClient } from './client';
import { HttpNuGetApi, SymbolsRefused, nugetFailure } from './nuget-api';
import { ApiError } from './problem';
import { UploadTransfer } from './upload-transfer';

const issued = {
  id: '9c1e0000-0000-4000-8000-000000000007',
  token: 'qdr_pat_x',
  expiresAt: '2026-11-26T10:05:00Z',
  publishPath: '/api/nuget/web/nuget-local/v3/package',
  symbolsPath: '/api/nuget/web/nuget-local/v3/symbolpackage',
};

function api(sendForHeaders: (...args: unknown[]) => Promise<HttpHeaders>) {
  const post = vi.fn(async () => ({ body: issued }));
  const del = vi.fn(async () => ({ status: 204, body: null }));
  const send = vi.fn(sendForHeaders);
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({ providers: [{ provide: ApiClient, useValue: { post, delete: del } }, { provide: UploadTransfer, useValue: { sendForHeaders: send } }] });
  return { api: TestBed.runInInjectionContext(() => new HttpNuGetApi()), post, del, send };
}

const stored = new HttpHeaders({ 'Quader-Package': 'Acme.Widgets', 'Quader-Version': '1.2.0-Beta.1' });

describe("The screens' push of a NuGet package (R-NUGET-006)", () => {
  it('R-NUGET-006 asks for the upload token, puts the package and then its symbols with it, reads the version from the answer and revokes the token', async () => {
    const t = api(async () => stored);
    const pkg = new Blob(['p']);
    const sym = new Blob(['s']);
    const onSymbols = vi.fn();
    await expect(t.api.publish('web/nuget-local', pkg, sym, { onSymbols })).resolves.toEqual({ id: 'Acme.Widgets', version: '1.2.0-Beta.1', symbols: 'stored' });
    expect(t.post).toHaveBeenCalledWith('/api/v1/projects/web/repositories/nuget-local/upload-tokens');
    const headers = { 'Content-Type': 'application/octet-stream', Authorization: 'Bearer qdr_pat_x' };
    expect(t.send.mock.calls.map((c) => [c[0], c[1], c[2], c[3]])).toEqual([
      ['PUT', issued.publishPath, pkg, headers],
      ['PUT', issued.symbolsPath, sym, headers],
    ]);
    expect(onSymbols).toHaveBeenCalledOnce();
    expect(t.del).toHaveBeenCalledWith(`/api/v1/tokens/${issued.id}`);
  });

  it('R-NUGET-006 a refused package is read from its plain-text answer, and the token is revoked; a refused symbol package keeps what was stored', async () => {
    const refusal = new ApiError(409, null, 'r-1', null, {
      contentType: 'text/plain; charset=utf-8',
      body: 'nuget_version_exists: Acme.Widgets 1.2.0 is stored with other bytes.',
      call: { method: 'PUT', path: issued.publishPath },
    });
    const t = api(async () => {
      throw refusal;
    });
    await expect(t.api.publish('web/nuget-local', new Blob(['p']), null)).rejects.toMatchObject({ code: 'nuget_version_exists', status: 409 });
    expect(t.del).toHaveBeenCalledOnce();

    let n = 0;
    const s = api(async () => {
      if (n++ === 0) return stored;
      throw refusal;
    });
    const e = await s.api.publish('web/nuget-local', new Blob(['p']), new Blob(['s'])).catch((x: unknown) => x);
    expect(e).toBeInstanceOf(SymbolsRefused);
    expect((e as SymbolsRefused).stored).toEqual({ id: 'Acme.Widgets', version: '1.2.0-Beta.1', symbols: 'none' });
    expect(nugetFailure(new Error('x'))).toBeInstanceOf(Error);
  });
});
