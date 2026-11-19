import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { LimitProfile, LimitsApi } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { Confirm } from '../../shared/confirm-dialog';
import { asApiError } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { NoValue } from '../../shared/ui/no-value';
import { compactFigures, formatCount } from './figures';

/** "4 rules, 1 project maximum", "Nothing" (R-UI-178). */
export function usedByText(p: LimitProfile, locale: string): string {
  const parts: string[] = [];
  const a = p.usedBy.assignments;
  const m = p.usedBy.projectMaximums;
  if (a > 0) parts.push(a === 1 ? $localize`:@@limits.profiles.one-rule:1 rule` : $localize`:@@limits.profiles.rules:${formatCount(a, locale)}:count: rules`);
  if (m > 0)
    parts.push(
      m === 1
        ? $localize`:@@limits.profiles.one-maximum:1 project maximum`
        : $localize`:@@limits.profiles.maximums:${formatCount(m, locale)}:count: project maximums`,
    );
  return parts.join(', ');
}

/**
 * The tab **Profiles** of **Download limits** (R-UI-178): every profile with the badge **Built-in**, its description, its figures
 * in one compact line and what uses it. The name opens the editor; **New profile** makes one. **Delete** is offered for an own
 * profile, disabled with the reason while something uses it.
 */
@Component({
  selector: 'quader-limits-profiles-tab',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DataTable, NoValue, RouterLink],
  template: `
    @if (canWrite()) {
      <div class="tab-head">
        <a class="btn btn-confirm" routerLink="/admin/limits/profiles/new" i18n="@@limits.profiles.new">New profile</a>
      </div>
    }
    <quader-data-table
      caption="Profiles"
      i18n-caption="@@limits.profiles.caption"
      [columns]="columns()"
      [loaded]="profiles() !== null"
      [loading]="profiles() === null && !failure()"
      [error]="failure()"
      (retry)="load()"
      [empty]="(profiles() ?? []).length === 0"
      emptyText="There is no profile yet."
      i18n-emptyText="@@limits.profiles.empty"
    >
      @for (p of profiles(); track p.key) {
        <tr>
          <th scope="row">
            <span class="name-line">
              <a [routerLink]="['/admin/limits/profiles', p.key]" translate="no">{{ p.name }}</a>
              @if (p.builtin) {
                <span class="badge" i18n="@@limits.rules.builtin">Built-in</span>
              }
            </span>
          </th>
          <td>
            @if (p.description) {
              {{ p.description }}
            } @else {
              <quader-no-value />
            }
          </td>
          <td class="figures">{{ compact(p) }}</td>
          <td>
            @if (usedBy(p); as used) {
              {{ used }}
            } @else {
              <quader-no-value />
            }
          </td>
          @if (canWrite()) {
            <td class="actions">
              @if (!p.builtin) {
                @let inUse = p.usedBy.assignments + p.usedBy.projectMaximums > 0;
                <button
                  class="btn btn-tertiary"
                  type="button"
                  [disabled]="inUse"
                  [attr.aria-describedby]="inUse ? 'used-' + p.key : null"
                  (click)="remove(p)"
                >
                  <ng-container i18n="@@limits.profiles.delete">Delete</ng-container>
                  <span class="visually-hidden" translate="no"> {{ p.name }}</span>
                </button>
                <!-- The reason is the row's column Used by; the disabled button names it to a screen reader (R-UI-178). -->
                @if (inUse) {
                  <span class="visually-hidden" [id]="'used-' + p.key" i18n="@@limits.profiles.in-use">Used by {{ usedBy(p) }}</span>
                }
              }
            </td>
          }
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    .tab-head {
      display: flex;
      justify-content: flex-end;
      margin-block-end: var(--quader-space-3);
    }
    .name-line {
      display: flex;
      flex-wrap: wrap;
      align-items: flex-start;
      gap: var(--quader-space-1) var(--quader-space-2);
    }
    .figures {
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
    }
  `,
})
export class LimitsProfilesTab {
  private readonly api = inject(LimitsApi);
  private readonly session = inject(SessionStore);
  private readonly confirmer = inject(Confirm);
  private readonly notifier = inject(Notifier);
  private readonly locale = inject(LOCALE_ID);

  protected readonly canWrite = computed(() => this.session.has('limits:write'));
  protected readonly profiles = signal<readonly LimitProfile[] | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'name', label: $localize`:@@limits.profiles.col-name:Profile`, width: '9rem' },
    { id: 'description', label: $localize`:@@limits.profiles.col-description:Description`, width: '10rem' },
    { id: 'figures', label: $localize`:@@limits.profiles.col-figures:Figures`, width: '14rem', prefer: true },
    { id: 'used', label: $localize`:@@limits.profiles.col-used:Used by`, width: '8rem' },
    ...(this.canWrite() ? [{ id: 'actions', label: $localize`:@@limits.rules.col-actions:Actions`, hideLabel: true }] : []),
  ]);

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    this.failure.set(null);
    try {
      this.profiles.set(await this.api.profiles());
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }

  protected compact(p: LimitProfile): string {
    return compactFigures(p.figures, this.locale);
  }

  protected usedBy(p: LimitProfile): string {
    return usedByText(p, this.locale);
  }

  protected async remove(p: LimitProfile): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@limits.profiles.delete-title:Delete profile ${p.name}:name:?`,
      lines: [$localize`:@@limits.profiles.delete-text:Nothing uses the profile. It is deleted for good.`],
      confirmLabel: $localize`:@@limits.profiles.delete-confirm:Delete profile`,
    });
    if (answer === null) return;
    this.failure.set(null);
    try {
      const stored = await this.api.profile(p.key);
      await this.api.deleteProfile(p.key, stored.etag);
      this.notifier.status($localize`:@@limits.profiles.deleted:Profile deleted.`);
      await this.load();
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }
}
