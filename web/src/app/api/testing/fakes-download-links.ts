import { Injectable, Provider } from '@angular/core';
import { ApiError } from '../problem';
import {
  DownloadLinkConnection,
  DownloadLinkSettings,
  DownloadLinkSettingsPatch,
  DownloadLinkSwitch,
  DownloadLinkTest,
  DownloadLinksApi,
  LoadedDownloadLinkSettings,
} from '../download-links-api';

/** A connection that can serve links: not encrypted, https, its link check passed. */
export function linkConnection(over: Partial<DownloadLinkConnection> = {}): DownloadLinkConnection {
  return {
    name: 'default',
    encrypted: false,
    endpoint: 'https://s3.eu-central-1.example.com',
    publicAddress: null,
    usable: true,
    reasons: [],
    handoverUsable: true,
    handoverReasons: [],
    ...over,
  };
}

/** The settings of a new installation with one connection that can serve links: the switch off, 1 MiB, 60 s. */
export function downloadLinkSettings(over: Partial<DownloadLinkSettings> = {}): DownloadLinkSettings {
  return {
    enabled: false,
    minSize: 1048576,
    linkLifeSeconds: 60,
    connections: [linkConnection()],
    routes: [
      { format: 'ansible', route: 'ansible.artifact.get' },
      { format: 'oci', route: 'oci.blob.get' },
    ],
    version: 1,
    updatedAt: '2026-11-22T08:00:00Z',
    updatedBy: null,
    ...over,
  };
}

/**
 * The signed links in memory (R-STOR-014, R-STOR-074): a change needs the loaded `ETag` (`412` otherwise) and applies the merge
 * patch, a `null` address returning its connection to the endpoint; the switch follows the settings. Every write is recorded;
 * `failure` makes the next write or test fail.
 */
@Injectable()
export class FakeDownloadLinksApi extends DownloadLinksApi {
  current: DownloadLinkSettings = downloadLinkSettings();
  formats: string[] = ['ansible', 'oci'];
  failure: ApiError | null = null;
  readonly calls: { op: string; body?: unknown }[] = [];

  private etag(): string {
    return `"${this.current.version}"`;
  }

  private fail(): void {
    const f = this.failure;
    if (f) {
      this.failure = null;
      throw f;
    }
  }

  override async settings(): Promise<LoadedDownloadLinkSettings> {
    return { settings: structuredClone(this.current), etag: this.etag() };
  }

  override async updateSettings(patch: DownloadLinkSettingsPatch, etag: string): Promise<LoadedDownloadLinkSettings> {
    this.calls.push({ op: 'settings', body: { patch: structuredClone(patch), etag } });
    this.fail();
    if (etag !== this.etag()) throw new ApiError(412, { code: 'precondition_failed', requestId: 'rid-links-412' }, 'rid-links-412');
    const addresses = patch.publicAddresses ?? {};
    this.current = {
      ...this.current,
      enabled: patch.enabled ?? this.current.enabled,
      minSize: patch.minSize ?? this.current.minSize,
      linkLifeSeconds: patch.linkLifeSeconds ?? this.current.linkLifeSeconds,
      connections: this.current.connections.map((c) =>
        c.name in addresses ? { ...c, publicAddress: addresses[c.name] ?? null } : c,
      ),
      version: this.current.version + 1,
      updatedBy: 'user:ada',
      updatedAt: '2026-11-23T09:00:00Z',
    };
    return this.settings();
  }

  override async test(connection: string, publicAddress: string): Promise<DownloadLinkTest> {
    this.calls.push({ op: 'test', body: { connection, publicAddress } });
    this.fail();
    const url = `${publicAddress}/blobs/quader/_probe/link-test-4f1c?X-Amz-Expires=60&X-Amz-Signature=5e3d`;
    return { url, expiresAt: '2026-11-23T09:01:00Z', command: `curl -fsS '${url}'` };
  }

  override async switch(): Promise<DownloadLinkSwitch> {
    return { enabled: this.current.enabled, formats: [...this.formats] };
  }
}

export function provideDownloadLinksFakes(): Provider[] {
  return [{ provide: DownloadLinksApi, useClass: FakeDownloadLinksApi }];
}
