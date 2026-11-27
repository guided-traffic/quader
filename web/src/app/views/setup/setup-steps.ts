import { NgTemplateOutlet } from '@angular/common';
import { Component, computed, input } from '@angular/core';
import type { ClientSnippet } from '../../api/packages-api';
import { SnippetText, snippetText } from '../../features/setup/clients';
import { SetupSnippet } from '../../features/setup/setup-snippet';
import { Disclosure } from '../../shared/ui/disclosure';

/** The steps of the job, in its order (R-UI-121): the snippets of each are the client setup's tab of that name. */
type Step = 'configure' | 'install' | 'publish' | 'verify';

/**
 * The steps of the page **Set up client** on one rail (R-UI-121, Q-UI-800, Q-UI-804): Configure where the answer holds its snippets,
 * Install, Publish where the page offers it, and Verify where the answer holds its snippets, each snippet a window with at most one short sentence, a second way of a step behind
 * an inline fold. A view (R-UI-223): the main installation's page and the Bastion's (bastion.md R-BAS-129, Q-UI-1469) compose it with the
 * snippets they read, and the main installation projects what its Publish step says without a target (`[publishWhy]`), which only it
 * has. A snippet that only makes sense with a credential is not shown to an anonymous reader (R-UI-123).
 */
@Component({
  selector: 'quader-setup-steps',
  imports: [NgTemplateOutlet, Disclosure, SetupSnippet],
  template: `
    <ng-template #snippetList let-list>
      @for (sn of list; track sn.id) {
        @let words = textOf(sn);
        @if (words?.fold; as fold) {
          <quader-disclosure class="second-way" [inline]="true" [heading]="fold">
            <quader-setup-snippet [snippet]="sn" [explanation]="words?.caption ?? null" [note]="note()(sn)" />
          </quader-disclosure>
        } @else {
          <quader-setup-snippet [snippet]="sn" [explanation]="words?.caption ?? null" [note]="note()(sn)" />
        }
      }
    </ng-template>
    <div class="steps">
      @if (byStep().configure.length > 0) {
        <section class="step" aria-labelledby="step-configure">
          <h2 id="step-configure"><span class="step-no" aria-hidden="true">1</span><span i18n="@@setup.tab.configure">Configure</span></h2>
          <ng-container [ngTemplateOutlet]="snippetList" [ngTemplateOutletContext]="{ $implicit: byStep().configure }" />
        </section>
      }
      <section class="step" aria-labelledby="step-install">
        <h2 id="step-install"><span class="step-no" aria-hidden="true">{{ installNumber() }}</span><span i18n="@@setup.tab.install">Install</span></h2>
        <ng-container [ngTemplateOutlet]="snippetList" [ngTemplateOutletContext]="{ $implicit: byStep().install }" />
      </section>
      @if (offerPublish()) {
        <section class="step" aria-labelledby="step-publish">
          <h2 id="step-publish"><span class="step-no" aria-hidden="true">{{ installNumber() + 1 }}</span><span i18n="@@setup.tab.publish">Publish</span></h2>
          @if (byStep().publish.length === 0) {
            <ng-content select="[publishWhy]" />
          }
          <ng-container [ngTemplateOutlet]="snippetList" [ngTemplateOutletContext]="{ $implicit: byStep().publish }" />
        </section>
      }
      @if (byStep().verify.length > 0) {
        <section class="step" aria-labelledby="step-verify">
          <h2 id="step-verify"><span class="step-no" aria-hidden="true">{{ verifyNumber() }}</span><span i18n="@@setup.tab.verify">Verify</span></h2>
          <ng-container [ngTemplateOutlet]="snippetList" [ngTemplateOutletContext]="{ $implicit: byStep().verify }" />
        </section>
      }
    </div>
  `,
  styles: `
    :host {
      display: block;
    }

    /* The steps of the job on one rail: the number at the rail, the heading beside it, the windows under the heading. The rail runs
       from a number to the next and ends at the last step, whichever that is for the reader. */
    .steps {
      margin-block-start: var(--quader-space-5);
    }

    .step {
      position: relative;
      padding-inline-start: 2.5rem;
      padding-block-end: var(--quader-space-5);

      &:not(:last-child)::before {
        content: '';
        position: absolute;
        inset-block: 1.875rem var(--quader-space-2);
        inset-inline-start: calc(0.75rem - 0.5px);
        border-inline-start: 1px solid var(--quader-border);
      }

      > h2 {
        display: flex;
        align-items: center;
        gap: var(--quader-space-2);
        margin-block: 0 var(--quader-space-2);
        font: var(--quader-type-title-medium);
      }

      > h2 .step-no {
        position: absolute;
        inset-inline-start: 0;
      }

      /* A second way of the step (Q-UI-804): an inline fold after the step's own windows. */
      .second-way {
        margin-block: 0 var(--quader-space-3);
      }

      .second-way:last-child {
        margin-block-end: 0;
      }
    }

    .step-no {
      display: inline-flex;
      flex: none;
      align-items: center;
      justify-content: center;
      inline-size: 1.5rem;
      block-size: 1.5rem;
      border-radius: var(--quader-radius-full);
      background: var(--quader-accent);
      color: var(--quader-on-accent);
      font-size: 0.8125rem;
    }

    /* Narrow: no rail; the number stands before the heading. */
    @container content (max-width: 36rem) {
      .step {
        padding-inline-start: 0;

        &::before {
          display: none;
        }

        > h2 .step-no {
          position: static;
        }
      }
    }
  `,
})
export class SetupSteps {
  /** The snippets of the client setup (R-API-030), every tab's. */
  readonly snippets = input.required<readonly ClientSnippet[]>();
  /** The reader has no credential: a snippet that only makes sense with one is left out (R-UI-123). */
  readonly anonymous = input(false);
  /** Publish is a step, also without a snippet, where the page offers it (R-UI-123); what it says then is projected. */
  readonly publish = input(false);
  /** A second sentence under a snippet, for a target that needs one (a Bastion chosen at the main installation, R-BAS-007). */
  readonly note = input<(sn: ClientSnippet) => string | null>(() => null);

  /** What the page says about a snippet (Q-UI-804): its sentence, the fold of a second way, whether it needs a credential. */
  protected textOf(sn: ClientSnippet): SnippetText | null {
    return snippetText(sn.id, { anonymous: this.anonymous() });
  }

  protected readonly byStep = computed<Record<Step, readonly ClientSnippet[]>>(() => {
    const shown = this.snippets().filter((s) => !(this.anonymous() && this.textOf(s)?.signedIn));
    return {
      configure: shown.filter((s) => s.tab === 'configure'),
      install: shown.filter((s) => s.tab === 'install'),
      publish: shown.filter((s) => s.tab === 'publish'),
      verify: shown.filter((s) => s.tab === 'verify'),
    };
  });
  protected readonly offerPublish = computed(() => this.publish() || this.byStep().publish.length > 0);
  /**
   * The number of the step Install: the first step where nothing is to configure (an OCI repository open to anonymous readers at a
   * Bastion, formats/oci.md Q-OCI-277), the second otherwise.
   */
  protected readonly installNumber = computed(() => (this.byStep().configure.length > 0 ? 2 : 1));
  /** The number of the step Verify: after Publish where the page offers it. */
  protected readonly verifyNumber = computed(() => this.installNumber() + (this.offerPublish() ? 2 : 1));
}
