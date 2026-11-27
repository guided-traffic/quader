import { Injectable, forwardRef, inject } from '@angular/core';
import { ApiClient } from './client';
import { Schema } from './types';
import { repositoryBase } from './upstream-catalogue-api';

/** A delivery rule of a remote repository as stored (R-CUR-001, R-CUR-006). */
export type DeliveryRule = Schema<'DeliveryRule'>;
/** A rule as a write gives it: a section's pattern, a package's name, or a name and one exact version. */
export type DeliveryRuleInput = Schema<'DeliveryRuleInput'>;
export type DeliveryRuleWritten = Schema<'DeliveryRuleWritten'>;
export type DeliveryRulePage = Schema<'DeliveryRulePage'>;
export type DeliveryRuleKind = DeliveryRule['kind'];
export type DeliveryEffect = DeliveryRule['effect'];
/** A package's or a version's switch turned (R-CUR-024). */
export type DeliverySwitchInput = Schema<'DeliverySwitchInput'>;
/** What a switch did: the subject's decision after it, its rule or none, the rules it removed (R-CUR-024). */
export type DeliverySwitchResult = Schema<'DeliverySwitchResult'>;
/** A package's version rules by effect (R-CUR-024). */
export type DeliveryRuleCounts = Schema<'DeliveryRuleCounts'>;
/** What a change of the delivery mode affects (R-CUR-025). */
export type DeliveryRulesSummary = Schema<'DeliveryRulesSummary'>;
export type DeliveryAllowStoredResult = Schema<'DeliveryAllowStoredResult'>;

/** The filters of the rule list; absent members filter nothing. */
export interface DeliveryRuleFilters {
  readonly kind?: DeliveryRuleKind;
  readonly effect?: DeliveryEffect;
  /** A substring of the subject. */
  readonly q?: string;
}

/**
 * The delivery rules of a remote repository (tag `remote-curation`, R-CUR-006): the list with the notes (holders of `manage`,
 * `repositories:read-config` and the administrator), the write of a subject's rule (it replaces the rule of the same subject) and its
 * removal (holders of `manage` and the administrator). Who may do what is the server's (R-CUR-005): a refusal is an `ApiError`.
 */
@Injectable({ providedIn: 'root', useClass: forwardRef(() => HttpDeliveryRulesApi) })
export abstract class DeliveryRulesApi {
  /** `GET …/delivery-rules`: newest first. */
  abstract list(repository: string, filters: DeliveryRuleFilters, cursor: string | null): Promise<DeliveryRulePage>;
  /** `POST …/delivery-rules`: creates or replaces the subject's rule. */
  abstract put(repository: string, rule: DeliveryRuleInput, idempotencyKey: string): Promise<DeliveryRuleWritten>;
  /** `DELETE …/delivery-rules/{ruleId}`. */
  abstract remove(repository: string, id: string): Promise<void>;
  /** `POST …/delivery-rules/switch`: the server writes or removes the subject's rule by what it inherits (R-CUR-024). */
  abstract switch(repository: string, input: DeliverySwitchInput, idempotencyKey: string): Promise<DeliverySwitchResult>;
  /** `GET …/delivery-rules/summary`: the counts of the rules and the stored packages the mode decides (R-CUR-025). */
  abstract summary(repository: string): Promise<DeliveryRulesSummary>;
  /** `POST …/delivery-rules/allow-stored`: one allowing package rule for each of those packages (R-CUR-025). */
  abstract allowStored(repository: string, idempotencyKey: string): Promise<DeliveryAllowStoredResult>;
}

@Injectable()
export class HttpDeliveryRulesApi extends DeliveryRulesApi {
  private readonly client = inject(ApiClient);

  async list(repository: string, filters: DeliveryRuleFilters, cursor: string | null): Promise<DeliveryRulePage> {
    const params: Record<string, string | number> = { limit: 100 };
    if (filters.kind) params['kind'] = filters.kind;
    if (filters.effect) params['effect'] = filters.effect;
    if (filters.q) params['q'] = filters.q;
    if (cursor) params['cursor'] = cursor;
    return (await this.client.getJson<DeliveryRulePage>(`${repositoryBase(repository)}/delivery-rules`, { params })).body;
  }

  async put(repository: string, rule: DeliveryRuleInput, idempotencyKey: string): Promise<DeliveryRuleWritten> {
    return (await this.client.post<DeliveryRuleWritten>(`${repositoryBase(repository)}/delivery-rules`, rule, { idempotencyKey })).body;
  }

  async remove(repository: string, id: string): Promise<void> {
    await this.client.delete(`${repositoryBase(repository)}/delivery-rules/${encodeURIComponent(id)}`);
  }

  async switch(repository: string, input: DeliverySwitchInput, idempotencyKey: string): Promise<DeliverySwitchResult> {
    return (await this.client.post<DeliverySwitchResult>(`${repositoryBase(repository)}/delivery-rules/switch`, input, { idempotencyKey })).body;
  }

  async summary(repository: string): Promise<DeliveryRulesSummary> {
    return (await this.client.getJson<DeliveryRulesSummary>(`${repositoryBase(repository)}/delivery-rules/summary`)).body;
  }

  async allowStored(repository: string, idempotencyKey: string): Promise<DeliveryAllowStoredResult> {
    return (await this.client.post<DeliveryAllowStoredResult>(`${repositoryBase(repository)}/delivery-rules/allow-stored`, undefined, { idempotencyKey })).body;
  }
}
