import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { Page, expect, test } from '@playwright/test';
import { LayoutFinding, emptyScroll, findingLine, judge, layoutFindings } from './layout';
import { manifestDir, outDir } from './paths';
import { Kind } from './seed';
import { KEEP_OPEN, Screen, screens } from './screens';

/**
 * The renders of `make ui-renders` (rule 11 of docs/projektplan/promt.md; docs/developer/web-ui.md#renders-of-every-screen):
 * every screen, as each kind of user that reaches it, in both schemes and at three widths, every tab of the screen, the
 * whole length of the content. A screen fails when it shows an error notice, when a call of the app's own API answers 4xx
 * or 5xx, or when the console reports an error; the pictures are taken first, so a failure has its picture.
 */

/** The widths: wide, the narrowest window with the pinned sidebar, a phone. UI_RENDERS_WIDTHS=1440,1280,1024,390 adds one. */
export const widths: readonly number[] = (process.env['UI_RENDERS_WIDTHS'] ?? '1440,1024,390').split(',').map(Number);
export const schemes = ['light', 'dark'] as const;
const heightOf = (width: number): number => (width < 600 ? 844 : width < 1200 ? 768 : 900);

/** What one picture shows, beside the picture: the contact sheet and the audit read it. */
export interface Shot {
  readonly kind: Kind;
  readonly screen: string;
  readonly tab: string | null;
  readonly scheme: (typeof schemes)[number];
  readonly width: number;
  readonly file: string;
  readonly url: string;
  /** How far the content panel (or the page) scrolls sideways, in CSS px. */
  readonly overflowX: number;
  /**
   * The regions inside it that scroll sideways (a table's scroll region, a code block), with how far: in the grown window of the
   * picture, and in the window's first height, where a table taller than its region has a scrollbar beside its rows that takes
   * its width (the scrollbars are shown, as on a desktop with a mouse).
   */
  readonly regions: readonly { readonly what: string; readonly overflowX: number }[];
  /**
   * When the panel scrolls sideways: the innermost element that reaches furthest past its right edge, hidden ones included (a
   * closed section keeps its box), as a path of tags and classes, and by how far.
   */
  readonly offender: { readonly what: string; readonly overflowX: number } | null;
  /** The layout checks' defects in this picture (layout.ts, R-UI-170 to R-UI-174); measured in the light scheme only. */
  readonly findings: readonly LayoutFinding[];
  /** The findings that `allowed` of layout.ts names, with its reason. */
  readonly allowedFindings: readonly (LayoutFinding & { readonly why: string })[];
}

for (const screen of screens) {
  for (const kind of screen.kinds) {
    test(`${screen.id} as ${kind}`, { tag: `@${kind}` }, async ({ page }) => {
      forget(kind, screen);
      const failures = watch(page, screen);
      trackRequests(page);
      // Every width shows the screen as a first visit: the Details a walk opened at the width before are not reopened from the
      // browser's memory (Q-UI-967), unless the walk asks to keep them for its next load (KEEP_OPEN).
      await page.addInitScript((keep) => {
        if (sessionStorage.getItem(keep) === null) {
          for (const key of Object.keys(localStorage)) if (key.startsWith('quader.open.')) localStorage.removeItem(key);
        }
        sessionStorage.removeItem(keep);
      }, KEEP_OPEN);
      const shots: Shot[] = [];
      // A walk or a tab that throws stops the screen: what it took so far is recorded with the reason, so failures.txt names it.
      let width = 0;
      try {
        for (width of [...widths, ...(screen.widths ?? [])]) {
          await page.setViewportSize({ width, height: heightOf(width) });
          await page.emulateMedia({ colorScheme: 'light' });
          const t0 = Date.now();
          await page.goto(screen.path);
          // A phone's scrollbars lie over the content and take no room: so they do in the phone's pictures. A page that is not
          // the app (the dev server's own answer) refuses the style and keeps its scrollbars.
          if (width < 600 && (await page.locator('quader-root').count()) > 0) {
            await page.addStyleTag({ content: '* { scrollbar-width: none; }' });
          }
          if (process.env['UI_RENDERS_DEBUG']) console.log(`goto: ${Date.now() - t0} ms`);
          await settle(page);
          if (screen.walk) {
            // A flow: one picture of each state it names; an empty name is the screen as it opens.
            for await (const state of screen.walk(page)) {
              await settle(page);
              for (const scheme of schemes) {
                await page.emulateMedia({ colorScheme: scheme });
                shots.push(await shoot(page, kind, screen, state || null, scheme, width));
              }
              await page.emulateMedia({ colorScheme: 'light' });
              failures.notices.push(...(await errorNotices(page, screen, width, state || null)));
            }
            continue;
          }
          // The screen as it opens is the picture without a tab in its name; then every other tab.
          // allTextContents does not wait: most screens have no tab.
          const opened = (await page.locator('main [role=tab][aria-selected=true]').allTextContents())[0]?.replace(/\s+/g, ' ').trim();
          const tabs = (await tabNames(page)).filter((t) => t !== opened);
          for (const tab of [null, ...tabs]) {
            if (tab !== null) {
              await page.locator('main [role=tab]').filter({ hasText: tab }).first().click();
              await settle(page);
            }
            for (const scheme of schemes) {
              await page.emulateMedia({ colorScheme: scheme });
              shots.push(await shoot(page, kind, screen, tab, scheme, width));
            }
            await page.emulateMedia({ colorScheme: 'light' });
            failures.notices.push(...(await errorNotices(page, screen, width, tab)));
          }
        }
      } catch (e) {
        // The first line names the call; the lines after it say what it waited for (the element, what covered it, that it kept
        // moving). Without them a click that timed out on a runner cannot be explained from its report.
        const told = String(e instanceof Error ? e.message : e).split('\n').map((l) => l.trim()).filter((l) => l !== '');
        failures.notices.push(`stopped at ${width} px: ${told.slice(0, 12).join(' | ').slice(0, 1200)}`);
      }
      for (const shot of shots) {
        const where = `${shot.width} px, ${shot.scheme}${shot.tab ? `, ${shot.tab}` : ''}`;
        failures.notices.push(...shot.findings.map((f) => findingLine(f, where)));
      }
      record(kind, screen, shots, failures.list());
      expect(failures.list(), `${screen.id} as ${kind}: ${screen.path}`).toEqual([]);
    });
  }
}

/** The labels of the tabs of the screen (`role=tab` inside `main`), the selected one first left out by the caller. */
async function tabNames(page: Page): Promise<string[]> {
  const names = await page.locator('main [role=tab]').allTextContents();
  return names.map((n) => n.replace(/\s+/g, ' ').trim()).filter((n, i, a) => n !== '' && a.indexOf(n) === i);
}

/** The calls of the app's API that are open, per page: `settle` waits until there are none. */
const open = new WeakMap<Page, Set<unknown>>();

function trackRequests(page: Page): void {
  const pending = new Set<unknown>();
  open.set(page, pending);
  const api = (url: string): boolean => /^\/(api|auth)\//.test(new URL(url).pathname);
  page.on('request', (r) => {
    // A new document: the calls of the one before it are no longer the screen's. Chromium cancels them, but a call it cancels
    // while the server still works on it got no "failed" here, and kept `settle` waiting its 15 s on every later state of a walk
    // that passed through a slow page.
    if (r.isNavigationRequest() && r.frame() === page.mainFrame()) pending.clear();
    if (api(r.url())) pending.add(r);
  });
  page.on('requestfinished', (r) => pending.delete(r));
  page.on('requestfailed', (r) => pending.delete(r));
  // A redirected request gets its answer and no "finished" of its own.
  page.on('response', (r) => {
    if (r.status() >= 300 && r.status() < 400) pending.delete(r.request());
  });
}

/**
 * Waits until the screen stands still: the document loaded, no call of the app's API open for 400 ms (at most 15 s: a
 * screen that polls never gets there and is taken as it is), the fonts loaded and every finite animation finished.
 */
async function settle(page: Page): Promise<void> {
  await page.waitForLoadState('load');
  const pending = open.get(page);
  const deadline = Date.now() + 15_000;
  let quietSince = Date.now();
  while (pending && Date.now() < deadline) {
    await page.waitForTimeout(100);
    if (pending.size > 0) quietSince = Date.now();
    else if (Date.now() - quietSince >= 400) break;
  }
  if (pending && pending.size > 0 && Date.now() >= deadline) {
    console.log(`settle: on ${new URL(page.url()).pathname}, still open after 15 s: ${[...pending].map((r) => (r as { url(): string }).url()).join(', ')}`);
  }
  const t0 = Date.now();
  await page.evaluate(async () => {
    await document.fonts.ready;
    await Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
        .map((a) => a.finished.catch(() => undefined)),
    );
  });
  if (process.env['UI_RENDERS_DEBUG']) console.log(`settle: network ${t0 - deadline + 15_000} ms, animations ${Date.now() - t0} ms`);
  await page.mouse.move(0, 0);
}

/**
 * The whole content: the panel `main` scrolls inside the shell and a list's region scrolls inside the panel, each with a
 * height that follows the window, so the window grows by what they hide (a few rounds, at most 16,000 px), then shrinks back.
 */
async function shoot(page: Page, kind: Kind, screen: Screen, tab: string | null, scheme: Shot['scheme'], width: number): Promise<Shot> {
  const view = page.viewportSize() ?? { width, height: 900 };
  // Before the window grows to the page's length: a table that scrolls inside its region has its scrollbar now.
  const first = view.height <= heightOf(width) ? await page.evaluate(sidewaysRegions) : [];
  let height = view.height;
  // In the app's shell the panel scrolls and the window does not; neither scrolls past the content (R-UI-173). Measured in the
  // window's first height, as the user has it.
  const empty = view.height <= heightOf(width) ? await page.evaluate(emptyScroll) : { window: 0, panel: 0 };
  for (let round = 0; round < 4; round++) {
    const { hidden, by } = await page.evaluate(() => {
      const vertical = (el: Element | null): number => (el ? Math.max(0, el.scrollHeight - el.clientHeight) : 0);
      const main = document.querySelector('main');
      let inner = 0;
      let by = '';
      for (const el of Array.from(document.querySelectorAll<HTMLElement>('main *'))) {
        if (!['auto', 'scroll'].includes(getComputedStyle(el).overflowY)) continue;
        const v = vertical(el);
        if (v > inner) {
          inner = v;
          by = `${el.tagName.toLowerCase()}.${Array.from(el.classList).join('.')} ${v}`;
        }
      }
      // The shell's panel holds the content; the window's own overflow is a defect, reported, and not a part of the picture.
      const outer = main?.classList.contains('content') ? vertical(main) : Math.max(vertical(main), vertical(document.scrollingElement));
      return { hidden: outer + inner, by: `main ${vertical(main)}, inner ${by}` };
    });
    if (process.env['UI_RENDERS_DEBUG']) console.log(`grow: ${hidden} px (${by})`);
    if (hidden <= 1 || height >= 16_000) break;
    height = Math.min(height + hidden, 16_000);
    await page.setViewportSize({ width: view.width, height });
    await settle(page);
  }
  const measured = await page.evaluate(() => {
    const main = document.querySelector('main');
    const doc = document.scrollingElement;
    const overflowX = Math.max(main ? main.scrollWidth - main.clientWidth : 0, doc ? doc.scrollWidth - doc.clientWidth : 0);
    let offender: { what: string; overflowX: number } | null = null;
    if (overflowX > 1 && main) {
      const edge = main.getBoundingClientRect().left + main.clientWidth;
      let best: { el: Element; over: number; depth: number } | null = null;
      for (const el of Array.from(main.querySelectorAll('*'))) {
        const over = Math.round(el.getBoundingClientRect().right - edge);
        if (over <= 1) continue;
        let depth = 0;
        for (let e: Element | null = el; e && e !== main; e = e.parentElement) depth++;
        if (!best || over > best.over || (over === best.over && depth > best.depth)) best = { el, over, depth };
      }
      if (best) {
        const parts: string[] = [];
        for (let e: Element | null = best.el; e && e !== main && parts.length < 4; e = e.parentElement) {
          parts.unshift(e.tagName.toLowerCase() + Array.from(e.classList).slice(0, 2).map((c) => '.' + c).join(''));
        }
        offender = { what: parts.join(' > '), overflowX: best.over };
      }
    }
    return { overflowX, offender };
  });
  const regions = await page.evaluate(sidewaysRegions);
  for (const r of first) {
    const same = regions.find((g) => g.what === r.what);
    if (!same) regions.push({ ...r, what: `${r.what} (window's first height)` });
    else if (r.overflowX > same.overflowX) regions.splice(regions.indexOf(same), 1, r);
  }
  // The layout checks, in the grown window the picture shows. The two schemes have one layout, so the light one is measured.
  let findings: LayoutFinding[] = [];
  if (scheme === 'light') {
    findings = await page.evaluate(layoutFindings);
    // The page scrolls sideways at any width; a region inside it from 1024 px (at a phone's width a code block or a table that
    // cannot stack scrolls in its region by design).
    if (measured.overflowX > 1) {
      findings.push({
        check: 'sideways',
        what: measured.offender?.what ?? 'main',
        text: '',
        detail: `the content is ${measured.overflowX} px wider than the panel${measured.offender ? `; its widest element reaches ${measured.offender.overflowX} px past the edge` : ''}`,
        count: 1,
      });
    }
    if (empty.window > 1) {
      findings.push({ check: 'empty-scroll', what: 'html', text: '', detail: `the window scrolls ${empty.window} px past the app, into an empty page`, count: 1 });
    }
    if (empty.panel > 24) {
      findings.push({ check: 'empty-scroll', what: 'main.content', text: '', detail: `the panel scrolls ${empty.panel} px past its last content`, count: 1 });
    }
    if (width >= 1024) {
      for (const r of regions) findings.push({ check: 'sideways', what: r.what, text: '', detail: `the region scrolls ${r.overflowX} px sideways`, count: 1 });
    }
    // The lists the screen holds to the form of a table at 1,024 px and wider (Q-UI-969).
    if (width >= 1024 && screen.tables) {
      const stacked = await page.locator(`main :is(${screen.tables}).stacked`).count();
      if (stacked > 0) {
        findings.push({ check: 'stacked', what: screen.tables, text: '', detail: `${stacked} list(s) stand as blocks at ${width} px`, count: stacked });
      }
    }
  }
  const judged = judge(screen.id, findings);
  const name = `${screen.id}${tab ? `-${slug(tab)}` : ''}-${scheme}-${width}.png`;
  const file = join(kind, name);
  mkdirSync(join(outDir, kind), { recursive: true });
  const t0 = Date.now();
  await page.screenshot({ path: join(outDir, file), animations: 'disabled', caret: 'hide' });
  if (process.env['UI_RENDERS_DEBUG']) console.log(`shoot: ${Date.now() - t0} ms`);
  if (height !== view.height) {
    await page.setViewportSize(view);
    await settle(page);
  }
  return {
    kind,
    screen: screen.id,
    tab,
    scheme,
    width,
    file,
    url: new URL(page.url()).pathname + new URL(page.url()).search,
    regions,
    ...measured,
    findings: judged.defects,
    allowedFindings: judged.allowed,
  };
}

/** The regions inside the content panel that scroll sideways, with how far (run in the page). */
function sidewaysRegions(): { what: string; overflowX: number }[] {
  const regions: { what: string; overflowX: number }[] = [];
  for (const el of Array.from(document.querySelectorAll<HTMLElement>('main *'))) {
    const style = getComputedStyle(el);
    if (!['auto', 'scroll'].includes(style.overflowX) || el.clientWidth === 0) continue;
    const over = el.scrollWidth - el.clientWidth;
    if (over > 1) {
      const label = el.getAttribute('aria-label') ?? el.getAttribute('aria-labelledby') ?? '';
      regions.push({ what: `${el.tagName.toLowerCase()}.${Array.from(el.classList).join('.')}${label ? ` [${label}]` : ''}`, overflowX: over });
    }
  }
  return regions;
}

function slug(text: string): string {
  return text
    .toLowerCase()
    .replace(/\(\d+\)|\d+$/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

/** The failures of a screen: the console's errors, uncaught errors, and the app's API calls answered 4xx or 5xx. */
function watch(page: Page, screen: Screen): { notices: string[]; list(): string[] } {
  const seen: string[] = [];
  const notices: string[] = [];
  page.on('console', (m) => {
    if (m.type() !== 'error') return;
    // The browser's own line for a failed fetch repeats the response below.
    if (/^Failed to load resource: the server responded with a status of/.test(m.text())) return;
    seen.push(`console error: ${m.text().slice(0, 300)}`);
  });
  page.on('pageerror', (e) => seen.push(`uncaught: ${e.message.slice(0, 300)}`));
  page.on('response', (r) => {
    const url = new URL(r.url());
    if (!/^\/(api|auth)\//.test(url.pathname) || r.status() < 400) return;
    const line = `${r.request().method()} ${url.pathname}${url.search} answered ${r.status()}`;
    if (screen.expected?.some((e) => e.match.test(line))) return;
    seen.push(line);
  });
  return { notices, list: () => [...new Set([...seen, ...notices])] };
}

/**
 * An error notice on the screen is a failure, whatever caused it, unless the screen's walk answered the failure in the browser to
 * render the error state and names the notice in `notices`.
 */
async function errorNotices(page: Page, screen: Screen, width: number, tab: string | null): Promise<string[]> {
  const texts = await page.locator('main quader-error-notice').allTextContents();
  const where = `${tab ? `tab ${tab}, ` : ''}${width} px`;
  return texts
    .map((t) => t.replace(/\s+/g, ' ').trim())
    .filter((t) => !screen.notices?.some((n) => n.match.test(t)))
    .map((t) => `error notice (${where}): ${t.slice(0, 200)}`);
}

function record(kind: Kind, screen: Screen, shots: readonly Shot[], failures: readonly string[]): void {
  mkdirSync(manifestDir, { recursive: true });
  writeFileSync(join(manifestDir, `${kind}--${screen.id}.json`), JSON.stringify({ kind, screen: screen.id, path: screen.path, shots, failures }, null, 1));
}

/** Removes the pictures of the screen's last run as this kind of user: its tabs may have changed since. */
function forget(kind: Kind, screen: Screen): void {
  const manifest = join(manifestDir, `${kind}--${screen.id}.json`);
  if (!existsSync(manifest)) return;
  const last = JSON.parse(readFileSync(manifest, 'utf8')) as { shots: readonly Shot[] };
  for (const shot of last.shots) rmSync(join(outDir, shot.file), { force: true });
  rmSync(manifest, { force: true });
}
