import { Component, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { ClientSetupApi } from '../../api/packages-api';
import { AddressField } from '../../shared/address-field';
import { CodeWindow } from '../../shared/code/code-window';
import { ErrorNotice } from '../../shared/error-notice';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { Loading } from '../../shared/ui/loading';

/** A command longer than this many lines is collapsed behind an expander (R-UI-009). */
export const COLLAPSE_AFTER_LINES = 3;

/**
 * The install command of one version in one repository for one client (R-UI-009): the install snippet of the client setup answer
 * (R-API-030) for that client, with the package and the version filled in and quoted by the server. It asks with
 * `credentials=none`, so it never holds a token. A command of one line is one copyable field (Q-UI-320), a longer one the snippet's
 * window (Q-UI-801), collapsed after three lines. The version page's install panel shows it for the client chosen in its picker
 * (`<quader-install-command>`); the menu of a version in a repository's list shows it once per client of the format
 * (`<quader-client-command-menu>`, Q-UI-1066).
 */
@Component({
  selector: 'quader-client-command',
  imports: [Loading, AddressField, CodeWindow, ErrorNotice],
  template: `
    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="failure.retry()" />
    } @else if (answer.isLoading()) {
      <quader-loading class="command-loading" i18n="@@pkg.install.loading">Loading the command.</quader-loading>
    } @else if (command()) {
      @if (oneLine()) {
        <!-- A one-line command is one copyable value, as an address is: the field with its joined copy button (Q-UI-320). -->
        <quader-address-field
          class="command-field"
          [style.--command-chars]="command().length"
          [value]="command()"
          [label]="commandLabel()"
          [copyLabel]="copyLabel()"
        />
      } @else {
        <!-- A command of several lines is the window of Set up client, one look for a snippet everywhere (Q-UI-801). -->
        @if (snippet(); as sn) {
          <quader-code-window
            [content]="command()"
            [language]="sn.language"
            [fileName]="sn.fileName"
            [marks]="sn.marks"
            [copyLabel]="copyLabel()"
            [downloadable]="false"
            [shownLines]="shownLines()"
          />
        }
      }
      @if (collapsible()) {
        <button
          type="button"
          class="btn btn-tertiary expander"
          [attr.aria-expanded]="expanded()"
          (click)="expanded.set(!expanded())"
        >
          @if (expanded()) {
            <ng-container i18n="@@pkg.install.collapse">Show less</ng-container>
          } @else {
            <ng-container i18n="@@pkg.install.expand">Show the whole command</ng-container>
          }
        </button>
      }
    }
  `,
  styleUrl: './install-panel.scss',
  host: { class: 'client-command' },
})
export class ClientCommand {
  private readonly setup = inject(ClientSetupApi);

  readonly format = input.required<string>();
  readonly name = input.required<string>();
  /** The version string; null while none is selected. */
  readonly version = input.required<string | null>();
  /** The qualified name of the repository the command installs through; null while none is chosen. */
  readonly repository = input.required<string | null>();
  /** The client of R-API-030 (`npm`, `podman`, …); null while none is chosen. */
  readonly client = input.required<string | null>();
  /** The accessible name of the one-line field; the menu names the client in it. */
  readonly commandLabel = input($localize`:@@pkg.install.command:Install command`);
  /** The accessible name and title of the copy button. */
  readonly copyLabel = input($localize`:@@pkg.install.copy:Copy install command`);

  protected readonly expanded = signal(false);

  protected readonly answer = resource({
    params: () => {
      const repository = this.repository();
      const client = this.client();
      const version = this.version();
      return repository && client && version !== null ? { repository, client, name: this.name(), version } : undefined;
    },
    loader: ({ params }) =>
      this.setup.get(params.repository, {
        client: params.client,
        credentials: 'none',
        package: params.name,
        version: params.version,
      }),
  });

  protected readonly snippet = computed(() => valueOf(this.answer)?.snippets.find((s) => s.tab === 'install') ?? null);
  protected readonly command = computed(() => this.snippet()?.content.trimEnd() ?? '');
  protected readonly lines = computed(() => this.command().split('\n'));
  protected readonly collapsible = computed(() => this.lines().length > COLLAPSE_AFTER_LINES);
  /**
   * How many lines the command shows folded; null for all of them, at whatever height they wrap to, so a long line never scrolls
   * inside the window (R-UI-215). The height follows it with the animation of the style guide.
   */
  protected readonly shownLines = computed(() => (this.collapsible() && !this.expanded() ? COLLAPSE_AFTER_LINES : null));
  /** The command's failure, kept while its notice reads it again (R-UI-216). */
  protected readonly failure = failureOf(this.answer);
  /** A command of one line is shown as one copyable field (Q-UI-320); a longer one is the snippet's window (Q-UI-801). */
  protected readonly oneLine = computed(() => !this.command().includes('\n'));

  constructor() {
    // Another version or another package starts collapsed again.
    effect(() => {
      this.version();
      untracked(() => this.expanded.set(false));
    });
  }
}
