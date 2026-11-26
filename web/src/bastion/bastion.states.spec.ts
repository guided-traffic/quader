import { Injectable, computed, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, it } from 'vitest';
import { LegalApi } from '../app/api/legal-api';
import { ApiError } from '../app/api/problem';
import { FakeLegalApi } from '../app/api/testing/fakes';
import { PackagesApi } from '../app/api/packages-api';
import { FakePackagesApi, ansibleDocsFixture, ansibleFixture, ociFixture } from '../app/api/testing/fakes-packages';
import { FormatCatalog } from '../app/core/format-names';
import { SIGNED_IN } from '../app/core/signed-in';
import { SystemClients } from '../app/core/system-clients';
import { app, expectState, openInPage, stable } from '../app/testing/states';
import { LegalStore } from '../app/core/legal-store';
import { BastionApp } from './app';
import { BastionState, BastionStateStore } from './data/state';
import { BastionFormatCatalog, BastionSystemClients } from './data/reads';
import { PackageListPage } from './pages/package-list-page';
import { PackagePage, VersionPage } from './pages/package-pages';

/**
 * The Bastion's pages in a browser (api-and-ui.md R-UI-227 to R-UI-229, rule 11 of promt.md): the shell with an old state and with no
 * catalogue to show, a project's packages, a package, an npm version with its README, an image and an Ansible collection's documentation, each against
 * the main installation's in-memory fakes (the same shapes the catalogue holds) and the Bastion's own bindings, measured by the render
 * set's layout checks at 390 and 1,440 px (`make ui-states`); `UI_STATES_PICTURES=1` writes the pictures. The Bastion's reads of
 * `/ui/data/v1/` are R-UI-228's spec; here the views read the fakes, as the main installation's states do.
 */

/** The state store as a page reads it, held at one answer. */
@Injectable()
class HeldState {
  readonly answer = signal<BastionState | null>(null);
  readonly failureValue = signal<ApiError | null>(null);
  readonly state = this.answer.asReadonly();
  readonly failure = this.failureValue.asReadonly();
  readonly notReady = computed(() => this.failureValue()?.problem?.code === 'not_ready');
  readonly off = computed(() => this.failureValue()?.status === 404);
  readonly settled = computed(() => this.answer() !== null || this.failureValue() !== null);
  refresh(): Promise<void> {
    return Promise.resolve();
  }
}

/** The image of the fakes (fakes-packages.ts): two platforms, layers, findings, signatures. */
const oci = (get: <A>(token: abstract new (...args: never[]) => A) => A): void => ociFixture(get(PackagesApi) as FakePackagesApi);

const providers = [
  { provide: FormatCatalog, useClass: BastionFormatCatalog },
  { provide: SystemClients, useClass: BastionSystemClients },
  { provide: SIGNED_IN, useValue: signal(false).asReadonly() },
  { provide: BastionStateStore, useClass: HeldState },
];

async function shell(state: BastionState | null, failure: ApiError | null): Promise<void> {
  await app({
    providers,
    setup: (get) => {
      (get(LegalApi) as FakeLegalApi).pagesResult = {
        imprint: { html: '<p>Example Operations GmbH</p>', lang: 'de' },
        privacy: { html: '<p>Privacy</p>', lang: 'en' },
      };
    },
  });
  const held = TestBed.inject(BastionStateStore) as unknown as HeldState;
  held.answer.set(state);
  held.failureValue.set(failure);
  const fixture = TestBed.createComponent(BastionApp);
  // The shell's global styles are scoped to the root element (_shell.scss): the test's host stands inside one, as the page's does,
  // marked as the states' page so that the next state removes it.
  const root = document.createElement('quader-root');
  root.setAttribute('data-quader-states-page', '');
  root.style.cssText = 'display: block; block-size: 100%;';
  document.body.append(root);
  root.append(fixture.nativeElement as HTMLElement);
  await stable(fixture);
  await TestBed.inject(LegalStore).refresh();
  await stable(fixture);
}

describe("the Bastion's pages in a browser (R-UI-227, R-UI-229)", () => {
  it('the shell with an old state: the band, the footer with the legal links and As of', async () => {
    await expectState('bastion-shell-stale', () =>
      shell({ confirmedAt: '2026-11-26T07:15:00Z', staleAfterSeconds: 3600, stale: true, docs: true }, null),
    );
  });

  it('the shell of a Bastion that has no catalogue to show yet', async () => {
    const notReady = new ApiError(503, { type: 'about:blank', title: 'Service Unavailable', status: 503, code: 'not_ready' }, null, 30);
    await expectState('bastion-not-ready', () => shell(null, notReady));
  });

  it("a project's packages", async () => {
    await expectState('bastion-package-list', () => openInPage(PackageListPage, { project: 'web' }, { providers }));
  });

  it("a package's versions", async () => {
    await expectState('bastion-package-oci', () =>
      openInPage(PackagePage, { project: 'web', format: 'oci', name: 'team/app' }, { providers, setup: oci }),
    );
  });

  // The fakes' npm version holds no vulnerability report, so its tab Vulnerabilities is not drawn here; the findings' state is open
  // (R-UI-230).
  it('an npm version with its README', async () => {
    await expectState('bastion-version-npm', () =>
      openInPage(VersionPage, { project: 'web', format: 'npm', name: '@team-a/ui', version: '1.4.0' }, { providers }),
    );
  });

  it('an image', async () => {
    await expectState('bastion-version-image', () =>
      openInPage(VersionPage, { project: 'web', format: 'oci', name: 'team/app', version: '1.2.0' }, { providers, setup: oci }),
    );
  });

  it("an Ansible collection's documentation", async () => {
    await expectState('bastion-version-ansible', () =>
      openInPage(VersionPage, { project: 'web', format: 'ansible', name: 'acme.tools' }, {
        providers,
        setup: (get) => {
          const fake = get(PackagesApi) as FakePackagesApi;
          ansibleFixture(fake);
          ansibleDocsFixture(fake);
        },
      }),
    );
  });
});
