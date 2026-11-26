import { ChangeDetectionStrategy, Component, Injectable, computed, effect, inject, input, signal, untracked, viewChild } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { LimitPlace } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { LIVE_IDLE_MS, live } from '../../core/live-refresh';
import { SessionStore } from '../../core/session-store';
import { asApiError } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { PageHeader } from '../../shared/ui/page-header';
import { TabStrip } from '../../shared/ui/tab-strip';
import { TimeAgo } from '../../shared/ui/time-ago';
import { LimitRules } from '../limits/limit-rules';
import { ReportedBlock, blockReasonText, reportedBlocks } from './bastion-state';

/**
 * The registration the tabs of a Bastion's limits page read (R-UI-241): its name from the address, and the registration with the state
 * its replicas reported, read by the frame and again every minute while the page is seen (R-UI-213).
 */
@Injectable()
export class BastionPageState {
  readonly name = signal('');
  readonly bastion = signal<Bastion | null>(null);
  readonly failure = signal<ApiError | null>(null);
  /** Set by the frame: reads the registration again. */
  reload: () => Promise<void> = async () => undefined;
}

/**
 * **Download limits of <name>** (R-UI-241), `/admin/bastions/{name}/limits` for the holders of `limits:read`: the header, one sentence
 * on the two budgets (Q-LIM-306), and the tabs **Rules** and **Blocks**, whose pages are the outlet. Reached from the card's **Rules of
 * this Bastion** and from its line of callers limited and addresses blocked (R-UI-240).
 */
@Component({
  selector: 'quader-bastion-limits-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [PageHeader, RouterLink, RouterLinkActive, RouterOutlet, TabStrip],
  providers: [BastionPageState],
  template: `
    <quader-page-header [heading]="heading()" [description]="description" />
    <nav quaderTabStrip class="tab-nav limits-nav" i18n-aria-label="@@bastion.limits-page.nav" aria-label="Download limits of this Bastion">
      <a class="tab-link" [routerLink]="base()" routerLinkActive [routerLinkActiveOptions]="{ exact: true }" ariaCurrentWhenActive="page" i18n="@@limits.tab.rules">Rules</a>
      <a class="tab-link" [routerLink]="[base(), 'blocks']" routerLinkActive ariaCurrentWhenActive="page" i18n="@@limits.tab.blocks">Blocks</a>
    </nav>
    <router-outlet />
  `,
  styles: `
    .limits-nav {
      margin-block-end: var(--quader-space-4);
    }
  `,
})
export class BastionLimitsPage {
  private readonly api = inject(BastionsApi);
  private readonly session = inject(SessionStore);
  protected readonly state = inject(BastionPageState);

  /** The registration's name from the address. */
  readonly name = input.required<string>();

  protected readonly heading = computed(() => $localize`:@@bastion.limits-page.heading:Download limits of ${this.name()}:name:`);
  protected readonly description = $localize`:@@bastion.limits-page.description:What each caller may download through this Bastion, and how fast.`;
  protected readonly base = computed(() => `/admin/bastions/${encodeURIComponent(this.name())}/limits`);

  constructor() {
    effect(() => {
      const name = this.name();
      untracked(() => {
        this.state.name.set(name);
        this.state.bastion.set(null);
        void this.load();
      });
    });
    this.state.reload = () => this.load();
    // The blocks and the state come from the replicas' reports, which change without the viewer (R-UI-213).
    live({ reload: () => this.load(), every: () => (this.session.has('endpoints:read') ? LIVE_IDLE_MS : null) });
  }

  /** Reads the registration; a viewer without `endpoints:read` cannot, and the tab **Blocks** says why with the answer. */
  private async load(): Promise<void> {
    try {
      const { value } = await this.api.get(this.state.name());
      this.state.failure.set(null);
      this.state.bastion.set(value);
    } catch (e) {
      this.state.failure.set(asApiError(e));
    }
  }
}

/**
 * The tab **Rules** of a Bastion's limits page (R-UI-241): the sentence on the two budgets over the rule table of R-UI-179 for the
 * Bastion's place, with **Add rule** for a holder of `limits:write`. The five kind rules of the preset come first, marked **From the
 * preset** (`limit-rules.ts`); the dialog is the instance's, with **Every administrator** among the kinds (R-LIM-043).
 */
@Component({
  selector: 'quader-bastion-rules-tab',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [LimitRules],
  template: `
    <p class="hint budgets" i18n="@@bastion.limits-page.budgets">
      These rules count what this Bastion serves. The installation's own rules count what it serves, including what this Bastion fetches.
    </p>
    @if (canWrite()) {
      <div class="tab-head">
        <button class="btn btn-confirm" type="button" (click)="rules()?.add()" i18n="@@limits.rules.add">Add rule</button>
      </div>
    }
    <quader-limit-rules [place]="place()" [canWrite]="canWrite()" />
  `,
  styles: `
    /* The sentence on the two budgets over the table, Add rule at the table's end as on the instance's tab Rules. */
    .budgets {
      margin: 0 0 var(--quader-space-3);
      max-inline-size: var(--quader-settings-measure);
    }
    .tab-head {
      display: flex;
      justify-content: flex-end;
      margin-block-end: var(--quader-space-3);
    }
  `,
})
export class BastionRulesTab {
  private readonly session = inject(SessionStore);
  private readonly state = inject(BastionPageState);

  protected readonly rules = viewChild(LimitRules);
  protected readonly canWrite = computed(() => this.session.has('limits:write'));
  protected readonly place = computed<LimitPlace>(() => ({ kind: 'bastion', bastion: this.state.name() }));
}

/**
 * The tab **Blocks** of a Bastion's limits page (R-UI-241): the active blocks the replicas reported (at most 100 each, R-LIM-045), with
 * the address, its end, the reason and the replica; read-only, since a Bastion's blocks are its own and end there.
 */
@Component({
  selector: 'quader-bastion-blocks-tab',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DataTable, TimeAgo],
  template: `
    <quader-data-table
      [caption]="caption()"
      [columns]="columns"
      [loaded]="state.bastion() !== null"
      [loading]="state.bastion() === null && !state.failure()"
      loadingText="Loading the blocks."
      i18n-loadingText="@@limits.blocks.loading"
      [error]="state.bastion() === null ? state.failure() : null"
      (retry)="state.reload()"
      [empty]="blocks().length === 0"
      [emptyText]="emptyText"
    >
      @for (b of blocks(); track b.replica + b.address) {
        <tr>
          <th scope="row" class="wrap-name" translate="no">{{ b.address }}</th>
          <td><quader-time-ago [at]="b.until" absolute="title" /></td>
          <td>{{ reasonText(b.reason) }}</td>
          <td class="wrap-name replica" translate="no">{{ b.replica }}</td>
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    td.replica {
      overflow-wrap: anywhere;
    }
  `,
})
export class BastionBlocksTab {
  protected readonly state = inject(BastionPageState);

  protected readonly caption = computed(() => $localize`:@@bastion.blocks.caption:Blocks at Bastion ${this.state.name()}:name:`);
  protected readonly emptyText = $localize`:@@bastion.blocks.empty:No replica reported a block in the last hour.`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'address', label: $localize`:@@bastion.blocks.col-address:Address`, width: '12rem', prefer: true },
    { id: 'until', label: $localize`:@@bastion.blocks.col-until:Until`, width: '8rem' },
    { id: 'reason', label: $localize`:@@bastion.blocks.col-reason:Reason`, width: '10rem' },
    { id: 'replica', label: $localize`:@@bastion.blocks.col-replica:Replica`, width: '12rem' },
  ];

  protected readonly reasonText = blockReasonText;

  protected readonly blocks = computed<readonly ReportedBlock[]>(() => {
    const b = this.state.bastion();
    return b ? reportedBlocks(b, Date.now()) : [];
  });
}
