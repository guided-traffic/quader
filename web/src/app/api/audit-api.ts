import { Injectable, inject } from '@angular/core';
import { ApiClient, QueryParams } from './client';
import { PageFetcher } from './pagination';
import { Schema } from './types';

export type AuditEvent = Schema<'AuditEvent'>;
/** A row of the audit log (R-OPS-055, R-OPS-056): an event, and where it begins a group of one act, the group. */
export type AuditRow = Schema<'AuditRow'>;
export type AuditGroup = Schema<'AuditGroup'>;
export type AuditGroupItem = Schema<'AuditGroupItem'>;
export type AuditAct = AuditGroup['act'];

/** The filters of `GET /api/v1/audit/events` that the screen offers (R-UI-037). */
export interface AuditFilters {
  /** RFC 3339, inclusive; the server defaults to the last 24 hours. */
  readonly from?: string;
  /** RFC 3339, exclusive. */
  readonly to?: string;
  readonly actor?: string;
  readonly action?: string;
  readonly category?: string;
  readonly repository?: string;
  /** The kind of the resource an event is about (`bastion`, `repository`), and its identifier (Q-UI-998). */
  readonly resourceType?: string;
  readonly resourceId?: string;
  readonly outcome?: string;
}

export type ExportFormat = 'jsonl' | 'csv';

/** Categories of R-OPS-016, the values of the `category` filter. */
export const AUDIT_CATEGORIES: readonly string[] = [
  'auth',
  'token',
  'identity',
  'authz',
  'repository',
  'content',
  'system',
  'config',
  'webhook',
];

function filterParams(filters: AuditFilters): Record<string, string | undefined> {
  const out: Record<string, string | undefined> = {};
  for (const [name, value] of Object.entries(filters)) out[name] = value || undefined;
  return out;
}

/** The audit trail: R-UI-037, R-OPS-017. */
export abstract class AuditApi {
  abstract events(filters: AuditFilters): PageFetcher<AuditEvent>;
  /**
   * The rows of the screen (`GET /api/v1/audit/rows`, R-OPS-055, R-OPS-056): the events of the filter, a download, a push or a
   * delete of several items as one row.
   */
  abstract rows(filters: AuditFilters): PageFetcher<AuditRow>;
  /** The events of the row whose first event is `id`, oldest first (`GET /api/v1/audit/rows/{id}/events`). */
  abstract rowEvents(id: string): PageFetcher<AuditEvent>;
  abstract event(id: string): Promise<AuditEvent>;
  /** The URL of the export of the filter; `from` and `to` are required there (at most 400 days). */
  abstract exportUrl(
    filters: AuditFilters & { readonly from: string; readonly to: string },
    format: ExportFormat,
  ): string;
}

@Injectable()
export class HttpAuditApi extends AuditApi {
  private readonly client = inject(ApiClient);

  events(filters: AuditFilters): PageFetcher<AuditEvent> {
    return this.client.page<AuditEvent>(
      '/api/v1/audit/events',
      filterParams(filters) as QueryParams,
    );
  }

  rows(filters: AuditFilters): PageFetcher<AuditRow> {
    return this.client.page<AuditRow>('/api/v1/audit/rows', filterParams(filters) as QueryParams);
  }

  rowEvents(id: string): PageFetcher<AuditEvent> {
    return this.client.page<AuditEvent>(`/api/v1/audit/rows/${encodeURIComponent(id)}/events`);
  }

  async event(id: string): Promise<AuditEvent> {
    return (await this.client.getJson<AuditEvent>(`/api/v1/audit/events/${encodeURIComponent(id)}`))
      .body;
  }

  exportUrl(
    filters: AuditFilters & { readonly from: string; readonly to: string },
    format: ExportFormat,
  ): string {
    const query = new URLSearchParams();
    for (const [name, value] of Object.entries({ ...filterParams(filters), format })) {
      if (value) query.set(name, value);
    }
    return `/api/v1/audit/events/export?${query.toString()}`;
  }
}
