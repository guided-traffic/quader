import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { problemInterceptor } from '../core/http/interceptors';
import { SessionStore } from '../core/session-store';
import { HttpLimitsApi, LimitsApi } from './limits-api';
import { ProjectsApi } from './projects-api';

function setup(): { api: LimitsApi; http: HttpTestingController } {
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({
    providers: [
      provideHttpClient(withInterceptors([problemInterceptor])),
      provideHttpClientTesting(),
      { provide: LimitsApi, useClass: HttpLimitsApi },
      { provide: ProjectsApi, useValue: {} },
      { provide: SessionStore, useValue: { markEnded: () => undefined, markReauthRequired: () => undefined, dropCheckCache: () => undefined } },
    ],
  });
  return { api: TestBed.inject(LimitsApi), http: TestBed.inject(HttpTestingController) };
}

describe("A project's maximum (R-LIM-033)", () => {
  it('Q-API-356 a project without a maximum is read as 200 with maximum null, one with a maximum with its ETag', async () => {
    const { api, http } = setup();
    const none = api.maximum('web');
    http.expectOne('/api/v1/projects/web/limits/maximum').flush({ project: 'web', maximum: null });
    expect(await none).toBeNull();
    const set = api.maximum('web');
    http
      .expectOne('/api/v1/projects/web/limits/maximum')
      .flush({ project: 'web', maximum: { project: 'web', profile: 'technical', version: 3 } }, { headers: { ETag: '"3"' } });
    expect(await set).toMatchObject({ value: { profile: 'technical' }, etag: '"3"' });
    http.verify();
  });
});
