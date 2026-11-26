import { Component } from '@angular/core';
import { ActivatedRouteSnapshot, Route, convertToParamMap } from '@angular/router';
import { DownloadLink } from '../app/shared/download-link';
import { renderScreen } from '../app/testing/screen';
import detailHtml from '../app/views/packages/package-detail-view.html' with { loader: 'text' };
import versionHtml from '../app/views/packages/package-version-view.html' with { loader: 'text' };
import signaturesHtml from '../app/views/signatures/signature-report.html' with { loader: 'text' };
import { BASTION_ROUTES } from './app.routes';
import indexHtml from './index.html' with { loader: 'text' };

// What a search engine reads of the Bastion's application (api-and-ui.md R-UI-231, bastion.md R-BAS-121): the titles name the page as
// the server's head does, the grade and the open findings are no snippet, a download is nofollow, and the page holds the two slots the
// server fills. Templates read as text and one link rendered; the file runs in milliseconds.

/** The resolved title of the route the path's routes reach, with the matcher's parameters. */
function titleOf(route: Route | undefined, params: Record<string, string>): string {
  const title = route?.title;
  if (typeof title !== 'function') return String(title);
  const snapshot = { paramMap: convertToParamMap(params) } as ActivatedRouteSnapshot;
  return (title as (r: ActivatedRouteSnapshot) => string)(snapshot);
}

@Component({
  selector: 'quader-test-download',
  imports: [DownloadLink],
  template: `<a href="/downloads/ui-1.0.0.tgz" download quaderDownload>ui-1.0.0.tgz</a>`,
})
class DownloadHost {}

describe('The Bastion for search engines (R-UI-231)', () => {
  it("R-UI-231 a project's list, a package, a version and the versions are titled by what they show, in the server's words", () => {
    const children = BASTION_ROUTES.find((r) => r.path === 'projects/:project/packages')?.children ?? [];
    const [list, versions, version, docsPackage, pkg] = children;
    expect(titleOf(list, { project: 'acme' })).toBe('Packages in acme');
    expect(titleOf(versions, { name: '@acme/ui' })).toBe('Versions of @acme/ui');
    expect(titleOf(version, { name: '@acme/ui', version: '1.0.0+build.1' })).toBe('@acme/ui 1.0.0+build.1');
    expect(titleOf(docsPackage, { name: 'acme.web' })).toBe('acme.web');
    expect(titleOf(pkg, { name: 'portal/web' })).toBe('portal/web');
  });

  it('R-UI-231 the grade and the open findings are no snippet, and a download is nofollow', async () => {
    expect(versionHtml).toMatch(/<span data-nosnippet><quader-grade /);
    expect(versionHtml).toMatch(/<div data-nosnippet>\s*<quader-package-vulnerabilities /);
    expect(detailHtml).toMatch(/<span data-nosnippet><quader-grade /);
    expect(signaturesHtml.match(/ download /g)?.length).toBe(signaturesHtml.match(/ download rel="nofollow" /g)?.length);
    const { root } = await renderScreen(DownloadHost);
    expect(root.querySelector('a')?.getAttribute('rel')).toBe('nofollow');
  });

  it("R-BAS-121 the page holds the server's slots: the head's, and the text's inside the root element after the spinner", () => {
    expect(indexHtml).toMatch(/<head>[\s\S]*<!--QUADER_HEAD-->[\s\S]*<\/head>/);
    expect(indexHtml).toMatch(/<quader-root [^>]*>[\s\S]*class="quader-boot[\s\S]*<!--QUADER_PAGE-->\s*<\/quader-root>/);
    expect(indexHtml).not.toContain('<title>');
  });
});
