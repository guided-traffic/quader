import { TestBed } from '@angular/core/testing';
import { describe, expect, it, vi } from 'vitest';
import { HttpAnsibleApi, galaxyFailure } from './ansible-api';
import { ApiClient } from './client';
import { ApiError } from './problem';
import { importTask } from './testing/fakes-ansible';
import { UploadTransfer } from './upload-transfer';

const galaxyAnswer = (status: number, body: string): ApiError =>
  new ApiError(status, null, 'r-1', null, { contentType: 'application/json', body, call: { method: 'POST', path: '/api/ansible/web/ansible-local/v3/artifacts/collections/' } });

describe('The screens\' publish of a collection (R-ANS-061)', () => {
  it('R-ANS-061 asks for the upload token, sends the archive with it to the client\'s route, and reads the task by its id', async () => {
    const post = vi.fn(async () => ({ body: { id: '9c1e0000-0000-4000-8000-000000000007', token: 'qdr_pat_x', expiresAt: '2026-11-26T10:05:00Z', publishPath: '/api/ansible/web/ansible-local/v3/artifacts/collections/' } }));
    const getJson = vi.fn(async () => ({ body: importTask() }));
    const del = vi.fn(async () => ({ status: 204, body: null }));
    const send = vi.fn(async () => ({ task: 'https://quader.test/api/ansible/web/ansible-local/v3/imports/collections/6f1c0000-0000-4000-8000-000000000001/' }));
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [{ provide: ApiClient, useValue: { post, getJson, delete: del } }, { provide: UploadTransfer, useValue: { send } }] });
    const api = TestBed.runInInjectionContext(() => new HttpAnsibleApi());
    const file = new Blob(['x']);
    await expect(api.publish('web/ansible-local', file)).resolves.toMatchObject({ state: 'completed' });
    expect(post).toHaveBeenCalledWith('/api/v1/projects/web/repositories/ansible-local/ansible/upload-tokens');
    expect(send).toHaveBeenCalledWith('POST', '/api/ansible/web/ansible-local/v3/artifacts/collections/', file,
      { 'Content-Type': 'application/gzip', Authorization: 'Bearer qdr_pat_x' }, {});
    expect(getJson).toHaveBeenCalledWith('/api/v1/projects/web/repositories/ansible-local/ansible/imports/6f1c0000-0000-4000-8000-000000000001');
    // The token served this one publish and is revoked.
    expect(del).toHaveBeenCalledWith('/api/v1/tokens/9c1e0000-0000-4000-8000-000000000007');
  });

  it('R-ANS-061 the token is revoked when the publish is refused or cancelled too', async () => {
    const post = vi.fn(async () => ({ body: { id: 't-1', token: 'qdr_pat_x', expiresAt: '2026-11-26T10:05:00Z', publishPath: '/p/' } }));
    const del = vi.fn(async () => ({ status: 204, body: null }));
    const send = vi.fn(async () => {
      throw new DOMException('The upload was cancelled.', 'AbortError');
    });
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [{ provide: ApiClient, useValue: { post, delete: del } }, { provide: UploadTransfer, useValue: { send } }] });
    const api = TestBed.runInInjectionContext(() => new HttpAnsibleApi());
    await expect(api.publish('web/ansible-local', new Blob(['x']))).rejects.toMatchObject({ name: 'AbortError' });
    expect(del).toHaveBeenCalledWith('/api/v1/tokens/t-1');
  });

  it('R-ANS-061 R-UI-216 a refusal in the Galaxy shape becomes the problem of its code, the management API\'s, with every other fact kept', () => {
    const exists = galaxyFailure(galaxyAnswer(409, '{"errors":[{"status":"409","code":"version_exists","title":"Conflict.","detail":"acme.tools 1.0.0 is stored."}]}')) as ApiError;
    expect(exists.problem).toMatchObject({ code: 'ansible_version_exists', status: 409, detail: 'acme.tools 1.0.0 is stored.' });
    expect(exists.requestId).toBe('r-1');
    expect(exists.answer.call?.path).toBe('/api/ansible/web/ansible-local/v3/artifacts/collections/');
    const shadow = galaxyFailure(galaxyAnswer(403, '{"errors":[{"status":"403","code":"shadows_remote_package","detail":"…"}]}')) as ApiError;
    expect(shadow.problem?.code).toBe('shadows_remote_package');
    const page = galaxyAnswer(502, '<html>Bad Gateway</html>');
    expect(galaxyFailure(page)).toBe(page);
    const problem = new ApiError(403, { code: 'forbidden' }, 'r-2');
    expect(galaxyFailure(problem)).toBe(problem);
  });
});
