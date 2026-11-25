import { ApplicationRef, EnvironmentProviders, Provider, Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { expect, inject as provided } from 'vitest';
import { page } from 'vitest/browser';
import { findingLine, innerScroll, layoutFindings } from '../../../e2e/renders/layout';
import { SessionApi } from '../api/session-api';
import { FakeSessionApi, provideFakeApis } from '../api/testing/fakes';
import { SessionStore } from '../core/session-store';
import { DialogOptions, DialogRef, QuaderDialog } from '../shared/ui/dialog';
import { ApiError } from '../api/problem';
import { RenderOptions, RenderedScreen, renderScreen, settle, typeInto } from './screen';

declare module 'vitest' {
  export interface ProvidedContext {
    /** Where `UI_STATES_PICTURES=1` writes the pictures (vitest.states.config.ts); empty: no pictures. */
    picturesDir: string;
  }
}

/**
 * The windows every state is measured in (docs/developer/web-ui.md#dialog-states): a phone's and a wide screen's. An overlay is
 * at most 52 rem wide, so the widths between show nothing these two do not.
 */
export const WINDOWS: readonly { readonly width: number; readonly height: number }[] = [
  { width: 390, height: 844 },
  { width: 1440, height: 900 },
];

/** A state of the web application: opened afresh in each window by `open`, which configures the test module itself. */
export type Open = () => Promise<unknown>;

/** One frame painted. */
const frame = (): Promise<void> => new Promise((resolve) => requestAnimationFrame(() => resolve()));

/**
 * Waits until the state stands still: the fonts are loaded (the app's Inter and its icon font, through the global stylesheet),
 * every animation has finished, and frames are painted.
 */
export async function quiet(): Promise<void> {
  await document.fonts.ready;
  for (let i = 0; i < 3; i++) {
    // A spinner turns for as long as its load runs (R-UI-217): its animation never finishes, and is not waited for.
    await Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
        .map((a) => a.finished.catch(() => undefined)),
    );
    await frame();
  }
  await frame();
}

/**
 * The page behind an open dialog is hidden for the measurement and the picture: a dialog's layout does not depend on it, and a
 * component without the application's shell around it is not the page a person sees.
 */
function hideBehind(): void {
  if (!document.querySelector('.cdk-overlay-container [role="dialog"]')) return;
  for (const el of Array.from(document.body.children)) {
    if (!el.matches('.cdk-overlay-container, .cdk-live-announcer-element, .cdk-visually-hidden, script, style')) {
      (el as HTMLElement).style.visibility = 'hidden';
    }
  }
}

/**
 * Opens a state in every window of `WINDOWS`, measures it with the render set's own `layoutFindings` and `innerScroll` (layout.ts:
 * an overlay has one scroll of its own at most, R-UI-215) and fails with every finding, named as failures.txt names it. With
 * `UI_STATES_PICTURES=1` a picture of each window is written
 * to `.out/ui-states/<name>-<width>.png`.
 */
export async function expectState(name: string, open: Open): Promise<void> {
  const lines: string[] = [];
  const dir = provided('picturesDir');
  for (const { width, height } of WINDOWS) {
    await page.viewport(width, height);
    TestBed.resetTestingModule();
    clearPage();
    await open();
    await quiet();
    hideBehind();
    // The window is the state's first and only height, so a scroll inside a dialog's one scroll is measured as the renders do.
    for (const f of [...layoutFindings(), ...innerScroll()]) lines.push(findingLine(f, `${name} at ${width}`));
    if (dir) await page.screenshot({ path: `${dir}/${name}-${width}.png` });
  }
  TestBed.resetTestingModule();
  clearPage();
  expect(lines).toEqual([]);
}

/** The providers of every state: a router that finds every path, and the in-memory fakes of the API. */
function baseProviders(extra: readonly Provider[] = []): (Provider | EnvironmentProviders)[] {
  return [provideRouter([{ path: '**', children: [] }]), ...provideFakeApis(), ...extra];
}

/** A service of the test injector: a fake bound to an abstract service class, or a store. */
export type Get = <A>(token: abstract new (...args: never[]) => A) => A;
const get: Get = <A>(token: abstract new (...args: never[]) => A): A => TestBed.inject(token as never) as A;

export interface AppOptions {
  /** Capabilities of `authz/me`; the default is the administrator's. */
  readonly capabilities?: readonly string[];
  readonly providers?: readonly Provider[];
  /** Runs after the providers exist and before the state opens (fills a fake, makes its next call fail). */
  readonly setup?: (get: Get) => unknown;
}

/** Configures the test module as the application runs without a screen: the router, the fakes, a signed-in session. */
export async function app(options: AppOptions = {}): Promise<Get> {
  await TestBed.configureTestingModule({ providers: baseProviders(options.providers) }).compileComponents();
  if (options.capabilities) (TestBed.inject(SessionApi) as FakeSessionApi).meResult = { capabilities: [...options.capabilities] };
  await options.setup?.(get);
  await TestBed.inject(SessionStore).load();
  return get;
}

export interface DialogState<R, T> {
  readonly ref: DialogRef<R, T>;
  /** The dialog's panel (`[role="dialog"]`). */
  readonly panel: HTMLElement;
  readonly get: Get;
}

/**
 * Opens a dialog through the application's dialog service (`QuaderDialog`), with the options its screen opens it with (the
 * call site is named beside each state), against the in-memory fakes, and lets it settle.
 */
export async function openDialog<T, D = unknown, R = unknown>(
  component: Type<T>,
  options: DialogOptions<D> & AppOptions = {},
): Promise<DialogState<R, T>> {
  await app(options);
  const ref = TestBed.inject(QuaderDialog).open<T, D, R>(component, options);
  return { ref, panel: await dialogPanel(component.name), get };
}

/** The open dialog's panel, once it has settled. */
export async function dialogPanel(what = 'the dialog'): Promise<HTMLElement> {
  await stable();
  const panel = document.querySelector<HTMLElement>('.cdk-overlay-container [role="dialog"]');
  if (!panel) throw new Error(`${what} did not open`);
  return panel;
}

/** Renders a screen (the host of a dialog or of a tooltip) against the fakes: `renderScreen` of the component tier. */
export function openHost<T>(component: Type<T>, options: RenderOptions = {}): Promise<RenderedScreen<T>> {
  return renderScreen(component, options);
}

/**
 * Renders a component as the content of a page: inside the shell's root and its panel (`quader-root > main.content`), whose
 * global styles (_shell.scss) give it the room and the padding a screen has. For a state that stands in a page (a token's
 * secret, an element whose tooltip opens at the window's edge).
 */
export async function openInPage<T>(component: Type<T>, inputs: Readonly<Record<string, unknown>> = {}, options: AppOptions = {}): Promise<ComponentFixture<T>> {
  await app(options);
  const fixture = TestBed.createComponent(component);
  for (const [name, value] of Object.entries(inputs)) fixture.componentRef.setInput(name, value);
  const root = document.createElement('quader-root');
  root.setAttribute(PAGE, '');
  root.style.cssText = 'display: flex; flex-direction: column; block-size: 100%;';
  const main = document.createElement('main');
  main.className = 'content';
  root.append(main);
  document.body.append(root);
  main.append(fixture.nativeElement as HTMLElement);
  await stable(fixture);
  return fixture;
}

const PAGE = 'data-quader-states-page';

/** Removes what a state left in the document beside the test module's own elements: the page of `openInPage`. */
function clearPage(): void {
  for (const el of Array.from(document.querySelectorAll(`[${PAGE}]`))) el.remove();
}

/** Lets change detection, promises and the application's effects run until nothing more changes. */
export async function stable(fixture?: ComponentFixture<unknown>): Promise<void> {
  for (let i = 0; i < 4; i++) {
    if (fixture) await settle(fixture, 2);
    else await TestBed.inject(ApplicationRef).whenStable();
    TestBed.tick();
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
}

/** Clicks the button of `root` whose text is `label` (or matches it) and lets the state settle. */
export async function click(root: ParentNode, label: string | RegExp): Promise<void> {
  const found = Array.from(root.querySelectorAll<HTMLButtonElement>('button')).find((b) => {
    const t = (b.textContent ?? '').replace(/\s+/g, ' ').trim();
    return typeof label === 'string' ? t === label : label.test(t);
  });
  if (!found) throw new Error(`no button "${String(label)}"`);
  found.click();
  await stable();
}

/** Types `value` into the field of `root` labelled `label` (its label starts so) and lets the state settle. */
export async function fill(root: ParentNode, label: string, value: string): Promise<void> {
  typeInto(root, label, value);
  await stable();
}

/** Submits the open dialog's form with its submit button, as a person does, and lets the state settle. */
export async function submit(panel: HTMLElement): Promise<void> {
  const button = panel.querySelector<HTMLButtonElement>('.dialog-actions button[type="submit"]');
  if (!button) throw new Error('the dialog has no submit button');
  button.click();
  await stable();
}

/** A request ID as the server writes one: 32 characters, one token (error-notice.ts). */
export const REQUEST_ID = '7f3c9a2e5b8d4f1a6c0e9b3d7a5f2c8e';

/**
 * A failure the screen has no sentence for (R-UI-042, R-UI-216): the shared error notice says its status's sentence and shows the
 * status, the time, the request ID with its Copy button, the code and the server's English text, a long shape of the notice.
 */
export function serverFailure(
  detail = 'The object store did not accept the write within 30 seconds: the connection to storage-eu-central-1.internal was reset by its peer.',
): ApiError {
  return new ApiError(500, { code: 'object_store_unavailable', status: 500, detail, requestId: REQUEST_ID }, REQUEST_ID);
}

/** A refusal of a write's fields (422): each pointer with the server's English sentence, under its field. */
export function fieldRefusal(errors: Readonly<Record<string, string>>): ApiError {
  return new ApiError(
    422,
    {
      code: 'validation_failed',
      status: 422,
      detail: 'The request has invalid fields.',
      requestId: REQUEST_ID,
      errors: Object.entries(errors).map(([pointer, detail]) => ({ pointer, code: 'invalid_value', detail })),
    },
    REQUEST_ID,
  );
}

type AnyFn = (...args: unknown[]) => unknown;

/** Makes the next call of a fake's method fail with `error`; the calls after it are the fake's own again. */
export function failNext<A extends object>(api: A, method: keyof A, error: ApiError): void {
  const target = api as Record<keyof A, unknown>;
  const original = target[method] as AnyFn;
  target[method] = (): Promise<never> => {
    target[method] = original;
    return Promise.reject(error);
  };
}

/** Makes every call of a fake's method fail with `error` (a list or a detail the dialog reads when it opens). */
export function failAlways<A extends object>(api: A, method: keyof A, error: ApiError): void {
  (api as Record<keyof A, unknown>)[method] = (): Promise<never> => Promise.reject(error);
}

/** Waits until `ready` holds (an answer the state waits for), settling between looks; `what` names it in the error. */
export async function until(ready: () => boolean, what: string, within = 5_000): Promise<void> {
  const end = Date.now() + within;
  while (!ready()) {
    if (Date.now() > end) throw new Error(`waited ${within} ms for ${what}`);
    await new Promise((resolve) => setTimeout(resolve, 20));
    await stable();
  }
}

/**
 * A text of German length where the text is data (a description, a display name, a reason a person typed): long words without a
 * break, as a German or a technical text has them.
 */
export const LONG_TEXT =
  'Bereitstellungsverantwortlichkeitsübertragung for the platform-engineering-release-candidates pipeline of the Kubernetes-Infrastrukturbetriebsteam';
