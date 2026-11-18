import {
  ApplicationRef,
  Component,
  ComponentRef,
  DestroyRef,
  ElementRef,
  EnvironmentInjector,
  afterRenderEffect,
  computed,
  createComponent,
  inject,
  input,
  ViewEncapsulation,
} from '@angular/core';
import { GuideSection } from '../../api/guides-api';
import { PackageReadme } from '../../api/packages-api';
import { CopyButton } from '../../shared/copy-button';
import { Disclosure } from '../../shared/ui/disclosure';
import { PackageReadmeView } from '../packages/package-readme';

/** The topics whose sections stand open below the snippets: what the repository is, and what this endpoint changes. */
const OPEN_TOPICS: readonly string[] = ['what', 'endpoint'];

/**
 * The explanation of a guide below its snippets (R-UI-121, R-UI-125, Q-UI-485): the section that says what a repository of the class
 * is, and the endpoint's note where there is one, stand open with their heading; every other section (configuring, signing in,
 * installing, publishing, tags, audit, lockfiles, troubleshooting) is a closed disclosure titled with its heading, so the titles are
 * the contents and nothing is boxed above them. The HTML is the server's, bound like a README (R-UI-008, the same component and
 * finishing pass); the prose keeps a measure, and every code block of it gets a copy button. The text is the format's and English
 * (Q-UI-86), so it carries `lang="en"`; the frame is translated.
 */
@Component({
  selector: 'quader-guide-view',
  imports: [Disclosure, PackageReadmeView],
  template: `
    @for (s of opened(); track s.id) {
      <section class="guide-open" [attr.aria-labelledby]="anchor(s)" [attr.data-section]="s.id">
        <h2 [id]="anchor(s)" lang="en">{{ s.title }}</h2>
        <quader-package-readme class="guide-prose" lang="en" [readme]="introOf(s)" />
        @if (restOf(s); as rest) {
          <quader-disclosure class="guide-rest" [heading]="moreHeading">
            <quader-package-readme class="guide-prose" lang="en" [readme]="rest" />
          </quader-disclosure>
        }
      </section>
    }
    @for (s of folded(); track s.id) {
      <quader-disclosure class="guide-more" [heading]="s.title" lang="en" [attr.data-section]="s.id">
        <quader-package-readme class="guide-prose" [readme]="readmeOf(s)" />
      </quader-disclosure>
    }
  `,
  styleUrl: './guide-view.scss',
  encapsulation: ViewEncapsulation.None,
})
export class GuideView {
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly appRef = inject(ApplicationRef);
  private readonly environment = inject(EnvironmentInjector);

  readonly sections = input.required<readonly GuideSection[]>();
  /** Keeps the anchors of two guides on one page apart. */
  readonly prefix = input('guide');

  protected readonly opened = computed(() => this.sections().filter((s) => OPEN_TOPICS.includes(s.topic)));
  protected readonly folded = computed(() => this.sections().filter((s) => !OPEN_TOPICS.includes(s.topic)));
  private readonly readmes = computed(
    () =>
      new Map(
        this.sections().map((s) => [
          s.id,
          { contentType: 'text/markdown', html: s.html, truncated: false, externalImagesBlocked: 0 } satisfies PackageReadme,
        ]),
      ),
  );
  private readonly copyLabel = $localize`:@@guide.copy-code:Copy code`;
  protected readonly moreHeading = $localize`:@@guide.more:More about this`;
  /**
   * An open section stands open with its introduction only, the text before its first subheading; the rest folds under **More
   * about this** (Q-UI-485): what a virtual repository is fits in three lines, how its members answer does not.
   */
  private readonly split = computed(
    () =>
      new Map(
        this.opened().map((s) => {
          const at = s.html.search(/<h[3-6][\s>]/);
          return [s.id, at > 0 ? [s.html.slice(0, at), s.html.slice(at)] : [s.html, '']] as const;
        }),
      ),
  );
  private readonly buttons: ComponentRef<CopyButton>[] = [];

  constructor() {
    // A command of the prose is copied like a snippet (R-UI-125): each code block gets the copy button of the snippets.
    afterRenderEffect(() => {
      this.sections();
      for (const pre of this.host.nativeElement.querySelectorAll<HTMLPreElement>('.readme pre')) {
        if (pre.parentElement?.classList.contains('prose-code')) continue;
        const wrap = document.createElement('div');
        wrap.className = 'prose-code';
        pre.replaceWith(wrap);
        wrap.append(pre);
        const slot = document.createElement('quader-copy-button');
        wrap.append(slot);
        const ref = createComponent(CopyButton, { environmentInjector: this.environment, hostElement: slot });
        ref.setInput('value', (pre.textContent ?? '').replace(/\n$/, ''));
        ref.setInput('label', this.copyLabel);
        this.appRef.attachView(ref.hostView);
        ref.changeDetectorRef.detectChanges();
        this.buttons.push(ref);
      }
    });
    inject(DestroyRef).onDestroy(() => {
      for (const ref of this.buttons) ref.destroy();
    });
  }

  protected anchor(s: GuideSection): string {
    return `${this.prefix()}-${s.id.replaceAll('.', '-')}`;
  }

  protected introOf(s: GuideSection): PackageReadme {
    return readme(this.split().get(s.id)?.[0] ?? s.html);
  }

  protected restOf(s: GuideSection): PackageReadme | null {
    const rest = this.split().get(s.id)?.[1] ?? '';
    return rest.trim() === '' ? null : readme(rest);
  }

  protected readmeOf(s: GuideSection): PackageReadme | null {
    return this.readmes().get(s.id) ?? null;
  }
}

function readme(html: string): PackageReadme {
  return { contentType: 'text/markdown', html, truncated: false, externalImagesBlocked: 0 };
}
