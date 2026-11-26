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
 * window (Q-UI-801), collapsed after three lines. With a `digest` (an image's, formats/oci.md R-OCI-041) it is the pull by digest: the
 * command of the image without its tag, and `@<digest>` after the reference, which ends the command's line (the digest is `sha256:` and
 * hex digits, nothing a shell reads). The panel of a version's commands (`<quader-client-command-menu>`, Q-UI-1066, Q-UI-1240), in a
 * repository's list and in the head of a version's page, shows it once per client of the format, a tab each, and an image's pull by
 * digest as the second line of the tab. With a `path` (a generic repository's file or folder, formats/generic.md R-GENERIC-017) it asks
 * for that path instead of a package: the file's download, the check of it by `snippetId`, or the upload into a folder by `tab`.
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
  styleUrl: './client-command.scss',
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
  /** An image's digest: the command pulls the image by it instead of by the tag. */
  readonly digest = input<string | null>(null);
  /**
   * A file's path, or a folder's with a trailing slash, of a generic repository (formats/generic.md R-GENERIC-017): the command is the
   * server's for that path instead of a package's, the address and the name filled in and quoted by it.
   */
  readonly path = input<string | null>(null);
  /** The file's SHA-256, which the server writes into the check of a download. */
  readonly sha256 = input<string | null>(null);
  /** The tab of the client setup whose first snippet is the command: `install`, or `publish` for an upload into a folder. */
  readonly tab = input<'install' | 'publish'>('install');
  /** A snippet by its id instead of the tab's first: the check of a download (`curl.install.checksum`). */
  readonly snippetId = input<string | null>(null);
  /** The accessible name of the one-line field; the menu names the client in it. */
  readonly commandLabel = input($localize`:@@pkg.install.command:Install command`);
  /** The accessible name and title of the copy button. */
  readonly copyLabel = input($localize`:@@pkg.install.copy:Copy install command`);

  protected readonly expanded = signal(false);

  protected readonly answer = resource({
    params: () => {
      const repository = this.repository();
      const client = this.client();
      if (!repository || !client) return undefined;
      const path = this.path();
      if (path !== null) return { repository, client, name: '', version: '', path, sha256: this.sha256() };
      // A pull by digest asks for the image without a version: the digest follows the reference.
      const version = this.digest() ? '' : this.version();
      return version !== null ? { repository, client, name: this.name(), version, path: null, sha256: null } : undefined;
    },
    loader: ({ params }) =>
      this.setup.get(
        params.repository,
        params.path !== null
          ? { client: params.client, credentials: 'none', path: params.path, ...(params.sha256 ? { sha256: params.sha256 } : {}) }
          : {
              client: params.client,
              credentials: 'none',
              package: params.name,
              ...(params.version !== '' ? { version: params.version } : {}),
            },
      ),
  });

  protected readonly snippet = computed(() => {
    const snippets = valueOf(this.answer)?.snippets ?? [];
    const id = this.snippetId();
    return (id ? snippets.find((s) => s.id === id) : snippets.find((s) => s.tab === this.tab())) ?? null;
  });
  protected readonly command = computed(() => {
    const text = this.snippet()?.content.trimEnd() ?? '';
    const digest = this.digest();
    return digest && text !== '' && !text.includes('\n') ? `${text}@${digest}` : text;
  });
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
