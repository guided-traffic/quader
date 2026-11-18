/**
 * The layout checks of `make ui-renders` (R-UI-170 to R-UI-174, docs/developer/web-ui.md#renders-of-every-screen): what the
 * owner and the development agent kept finding by eye in the pictures, measured in the page once per picture. Each finding
 * fails the screen, unless `allowed` below names it with its reason.
 *
 * `layoutFindings` runs in the page (`page.evaluate`), so it carries its helpers inside and reads nothing from this module.
 */

/** One defect of the layout, as the render report and failures.txt name it. */
export interface LayoutFinding {
  /** Which check found it: `icon-apart`, `broken`, `row-offset`, `row-actions`, `clipped`, `stacked-fits`, `sideways`, `empty-scroll`. */
  readonly check: string;
  /** The element, as a path of up to four tags with their first two classes. */
  readonly what: string;
  /** Its text, shortened. */
  readonly text: string;
  /** What was measured. */
  readonly detail: string;
  /** How many elements of the same path showed it in this picture; the first one is described. */
  readonly count: number;
}

/**
 * The findings of the picture's present state (run in the page). Cheap: one walk over the elements of each kind, no waiting,
 * no change of the page but the measurement of a stacked table, which is undone in the same task (no frame is painted between).
 */
export function layoutFindings(): LayoutFinding[] {
  const found = new Map<string, { check: string; what: string; text: string; detail: string; count: number }>();
  const add = (check: string, el: Element, text: string, detail: string): void => {
    const what = pathOf(el);
    const key = `${check} ${what}`;
    const known = found.get(key);
    if (known) known.count++;
    else found.set(key, { check, what, text: text.replace(/\s+/g, ' ').trim().slice(0, 80), detail, count: 1 });
  };

  function pathOf(el: Element): string {
    const parts: string[] = [];
    for (let e: Element | null = el; e && e !== document.body && parts.length < 4; e = e.parentElement) {
      parts.unshift(e.tagName.toLowerCase() + Array.from(e.classList).filter((c) => !c.startsWith('ng-')).slice(0, 2).map((c) => '.' + c).join(''));
    }
    return parts.join(' > ');
  }

  const ICON = '.material-icons, quader-format-icon, quader-visibility-icon, quader-project-mark, quader-avatar';
  /**
   * The element that is one icon: a component's host, or the glyph itself. A copy button is the icon of the value before it (a
   * hash and its button went to two lines at 1,024 px).
   */
  const iconUnit = (el: Element): Element =>
    el.closest('quader-format-icon, quader-visibility-icon, quader-project-mark, quader-avatar, quader-copy-button') ?? el;
  const inIcon = (el: Element | null): boolean => !!el?.closest(ICON);

  /** Shown: has a box, is not hidden, and is not text for assistive technology only. */
  function shown(el: Element): boolean {
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) return false;
    if (el.closest('.visually-hidden, .cdk-visually-hidden, [hidden]')) return false;
    return getComputedStyle(el).visibility !== 'hidden';
  }

  /**
   * The boxes of the visible text inside an element, line by line in document order, without the glyphs of icons; with
   * `primary`, without a second line the element has by design (`.secondary`, the description under a name).
   */
  function textRects(el: Element, primary = false): DOMRect[] {
    const rects: DOMRect[] = [];
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    const range = document.createRange();
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      if (!/\S/.test(n.nodeValue ?? '')) continue;
      const parent = n.parentElement;
      if (!parent || inIcon(parent) || !shown(parent)) continue;
      if (primary && parent.closest('.secondary')) continue;
      range.selectNodeContents(n);
      for (const r of Array.from(range.getClientRects())) if (r.width > 0.5 && r.height > 0) rects.push(r);
    }
    return rects;
  }

  /** The first visible text node of an element, without the glyphs of icons. */
  function firstText(el: Element): { rect: DOMRect; font: string } | null {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    const range = document.createRange();
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      if (!/\S/.test(n.nodeValue ?? '')) continue;
      const parent = n.parentElement;
      if (!parent || inIcon(parent) || !shown(parent)) continue;
      range.selectNodeContents(n);
      const rect = Array.from(range.getClientRects()).find((r) => r.width > 0.5 && r.height > 0);
      if (!rect) continue;
      const s = getComputedStyle(parent);
      return { rect, font: `${s.fontSize} ${s.fontFamily}` };
    }
    return null;
  }

  /** How many lines boxes hold: two boxes share a line when they overlap by half the lower one's height. */
  function lineCount(rects: readonly DOMRect[]): number {
    const lines: { top: number; bottom: number }[] = [];
    for (const r of [...rects].sort((a, b) => a.top - b.top)) {
      const line = lines.find((l) => Math.min(l.bottom, r.bottom) - Math.max(l.top, r.top) >= Math.min(l.bottom - l.top, r.height) / 2);
      if (line) {
        line.top = Math.min(line.top, r.top);
        line.bottom = Math.max(line.bottom, r.bottom);
      } else lines.push({ top: r.top, bottom: r.bottom });
    }
    return lines.length;
  }

  const textOf = (el: Element): string =>
    Array.from(el.childNodes)
      .map((n) => (n.nodeType === Node.ELEMENT_NODE && inIcon(n as Element) ? '' : (n.textContent ?? '')))
      .join(' ');

  // ---- icon-apart: an icon on another line than its label (R-UI-170) ------------------------------------------------------
  // The label is the icon's next sibling with visible text, else its previous one (a sort arrow, a "new window" mark after a
  // link). An icon its parent lays out above its text (a column, a block of its own, a grid of one column) is meant so.
  const units = new Set<Element>();
  for (const el of Array.from(document.querySelectorAll(ICON))) units.add(iconUnit(el));
  for (const unit of units) {
    if (!shown(unit)) continue;
    const parent = unit.parentElement;
    if (!parent) continue;
    const ps = getComputedStyle(parent);
    const us = getComputedStyle(unit);
    if (/flex/.test(ps.display) && ps.flexDirection.startsWith('column')) continue;
    if (/^(block|flex|grid|table|list-item|flow-root)$/.test(us.display) && /^(block|flow-root|list-item|table-cell)$/.test(ps.display)) continue;
    if (/grid/.test(ps.display) && ps.gridTemplateColumns.trim().split(/\s+/).length < 2) continue;
    const label = (nodes: ChildNode[], last: boolean): { rect: DOMRect; node: Node } | null => {
      for (const n of nodes) {
        let rects: DOMRect[] = [];
        if (n.nodeType === Node.TEXT_NODE) {
          if (!/\S/.test(n.nodeValue ?? '')) continue;
          const range = document.createRange();
          range.selectNodeContents(n);
          rects = Array.from(range.getClientRects()).filter((r) => r.width > 0.5 && r.height > 0);
        } else if (n.nodeType === Node.ELEMENT_NODE) {
          const e = n as Element;
          if (units.has(e) || inIcon(e) || !shown(e)) continue;
          rects = textRects(e);
        }
        if (rects.length === 0) continue;
        // Beside its label in a row of a flex or grid box, the icon stands on the line of the label's block (a copy button beside a
        // hash of three lines); in running text, on the label's first line, or its last for an icon after it.
        if (/flex|grid/.test(ps.display) && !(/flex/.test(ps.display) && ps.flexWrap === 'wrap')) {
          const top = Math.min(...rects.map((r) => r.top));
          const bottom = Math.max(...rects.map((r) => r.bottom));
          return { rect: new DOMRect(rects[0]?.left ?? 0, top, 1, bottom - top), node: n };
        }
        const rect = last ? rects[rects.length - 1] : rects[0];
        if (rect) return { rect, node: n };
      }
      return null;
    };
    const siblings = Array.from(parent.childNodes);
    const at = siblings.indexOf(unit as ChildNode);
    const found = label(siblings.slice(at + 1), false) ?? label(siblings.slice(0, at).reverse(), true);
    if (!found) continue;
    const ir = unit.getBoundingClientRect();
    const overlap = Math.min(ir.bottom, found.rect.bottom) - Math.max(ir.top, found.rect.top);
    if (overlap <= 0) {
      add('icon-apart', parent, found.node.textContent ?? '', `the icon "${(unit.textContent ?? '').trim() || unit.tagName.toLowerCase()}" stands at y ${Math.round(ir.top)}, its label's line at y ${Math.round(found.rect.top)}`);
    }
  }

  // ---- broken: a short thing broken across lines (R-UI-171) ---------------------------------------------------------------
  // What the style guide keeps on one line: a status or a badge, a chip, a button's label (not a button that is a row's name in
  // the look of a link, `button.link`, which wraps as a name does), a tab, a list's header, a date or a time (`time`; the relative
  // time and its absolute one in brackets may stand on two lines, Q-UI-431), a key or an identifier in a cell (`translate="no"`;
  // not a name that may break beside its icon, Q-UI-425, nor a key a list lets wrap, Q-UI-303, nor its `.secondary` line), a
  // number with its unit, a format, a class and a level. A notice or a verdict in a paragraph (`p.warn`) is a sentence and wraps.
  // A table of a document (the manual, a guide, a README) is prose, not a list, and is not measured.
  const stackedTable = (el: Element): boolean => {
    const t = el.closest('table');
    return !!t && getComputedStyle(t).display !== 'table';
  };
  const oneLine: readonly [string, string][] = [
    ['status', '.badge, span.ok, span.warn, span.bad, .chip'],
    ['button', 'button:not(.link), a.btn, [role="tab"]'],
    ['header', 'table.data thead th'],
    ['time', 'time'],
    ['identifier', 'td[translate="no"]:not(.wrap), th[scope="row"][translate="no"]:not(.wrap-name)'],
    ['number', 'td.num, th.num'],
    ['format', '.format-name, .class-name, .type-cell, td.level, td.level-cell'],
  ];
  for (const [kind, selector] of oneLine) {
    for (const el of Array.from(document.querySelectorAll(selector))) {
      if (!shown(el)) continue;
      if (kind === 'identifier' && (el.querySelector('.icon-name, .wrap-name') || (el.matches('th') && stackedTable(el)))) continue;
      if (kind === 'header' && stackedTable(el)) continue;
      const lines = lineCount(textRects(el, true));
      if (lines > 1) add('broken', el, textOf(el), `a ${kind} in ${lines} lines, ${Math.round(el.getBoundingClientRect().width)} px wide`);
    }
  }

  // ---- broken-word: a short word of a list's cell broken inside itself (R-UI-171) -----------------------------------------
  // A column narrower than a word of ten letters ("Platfor" / "m enginee" / "ring", a project in the audit log). A long token (an
  // identifier, a path, a hash) may break where it must when no column holds it (Q-UI-425): the run of characters without a space
  // around the break is such a token when it is longer than 16 characters, and so is the text of `code`.
  for (const cell of Array.from(document.querySelectorAll('table.data > tbody > tr > :is(td, th)'))) {
    if (!shown(cell) || stackedTable(cell) || lineCount(textRects(cell)) < 2) continue;
    const walker = document.createTreeWalker(cell, NodeFilter.SHOW_TEXT);
    const range = document.createRange();
    let broken: string | null = null;
    for (let n = walker.nextNode(); n && !broken; n = walker.nextNode()) {
      const text = n.nodeValue ?? '';
      const parent = n.parentElement;
      if (!/\S/.test(text) || !parent || inIcon(parent) || !shown(parent) || parent.closest('code')) continue;
      let top: number | null = null;
      for (let i = 0; i < text.length && !broken; i++) {
        range.setStart(n, i);
        range.setEnd(n, i + 1);
        const r = range.getClientRects()[0];
        if (!r) continue;
        if (top !== null && r.top > top + 2 && /[\p{L}\p{N}]/u.test(text[i - 1] ?? '') && /[\p{L}\p{N}]/u.test(text[i] ?? '')) {
          const before = /\S*$/u.exec(text.slice(0, i))?.[0] ?? '';
          const after = /^\S*/u.exec(text.slice(i))?.[0] ?? '';
          if (before.length + after.length <= 16) broken = `${before}|${after}`;
        }
        top = r.top;
      }
    }
    if (broken) {
      const [head, tail] = broken.split('|');
      add('broken', cell, textOf(cell), `the word "${head}${tail}" broken inside itself ("${head}" / "${tail}"), the cell ${Math.round(cell.getBoundingClientRect().width)} px wide`);
    }
  }

  // ---- row-offset and row-actions: a row laid out differently from its neighbours (R-UI-172) -------------------------------
  // The first line of every cell of a list's row stands on one line with the row's name: the same baseline within a pixel where
  // the fonts are the same; else the same middle within 1.5 px, because a smaller text centred in a badge lands on a half pixel
  // (the walk's defects were 2 and 5 px). A cell without text is measured by its first icon. Then the action cells of one table:
  // the same construction in every row that has one (a menu, a word, an icon), as R-UI-163 asks. A list is `table.data`.
  for (const table of Array.from(document.querySelectorAll<HTMLTableElement>('table.data'))) {
    if (!shown(table) || getComputedStyle(table).display !== 'table') continue;
    const signatures = new Map<string, number>();
    let example: Element | null = null;
    for (const body of Array.from(table.tBodies)) {
      for (const row of Array.from(body.rows)) {
        if (getComputedStyle(row).display !== 'table-row') continue;
        const anchors: { cell: Element; rect: DOMRect; font: string | null }[] = [];
        for (const cell of Array.from(row.cells)) {
          if (!shown(cell)) continue;
          const text = firstText(cell);
          if (text) anchors.push({ cell, rect: text.rect, font: text.font });
          else {
            const icon = Array.from(cell.querySelectorAll(ICON)).map(iconUnit).find(shown);
            if (icon) anchors.push({ cell, rect: icon.getBoundingClientRect(), font: null });
          }
        }
        const ref = anchors.find((a) => a.font !== null);
        if (ref && anchors.length > 1) {
          for (const a of anchors) {
            if (a === ref) continue;
            const off = a.font === ref.font ? a.rect.bottom - ref.rect.bottom : (a.rect.top + a.rect.bottom) / 2 - (ref.rect.top + ref.rect.bottom) / 2;
            if (Math.abs(off) > (a.font === ref.font ? 1 : 1.5)) {
              add('row-offset', a.cell, a.cell.textContent ?? '', `${Math.abs(off).toFixed(1)} px ${off > 0 ? 'lower' : 'higher'} than "${(ref.cell.textContent ?? '').replace(/\s+/g, ' ').trim().slice(0, 30)}" of its row`);
            }
          }
        }
        const actions = row.querySelector(':scope > td.actions');
        if (!actions) continue;
        const kinds = Array.from(actions.querySelectorAll('button, a.btn'))
          .filter(shown)
          .map((b) => {
            if (b.getAttribute('aria-haspopup')) return 'menu';
            return textRects(b).length > 0 ? 'word' : 'icon';
          })
          .sort()
          .join('+');
        if (!kinds) continue;
        signatures.set(kinds, (signatures.get(kinds) ?? 0) + 1);
        example ??= actions;
      }
    }
    if (signatures.size > 1 && example) {
      add('row-actions', example, '', `the action cells of one table are built ${signatures.size} ways: ${[...signatures].map(([k, n]) => `${k} (${n})`).join(', ')}`);
    }
  }

  // ---- clipped: a text cut although there is room beside it (R-UI-173) ----------------------------------------------------
  // An element cut with an ellipsis, or a read-only field's value cut (an address, a command), where the box that holds it has the
  // room the whole text needs to the element's right; a field with its joined button is measured as one. A field the user types
  // into is as wide as its expected input (R-UI-085) and scrolls a longer value.
  const room = (el: Element): number => {
    const box = el.parentElement?.closest('td, th, li, p, div, dd, label, section, form');
    if (!box) return 0;
    const s = getComputedStyle(box);
    const right = box.getBoundingClientRect().right - parseFloat(s.paddingRight) - parseFloat(s.borderRightWidth);
    return right - el.getBoundingClientRect().right;
  };
  for (const el of Array.from(document.querySelectorAll<HTMLElement>('main *, [role="dialog"] *'))) {
    const isField = el instanceof HTMLInputElement && el.readOnly && /^(text|url|search|email|)$/.test(el.type) && el.value !== '';
    if (!isField && getComputedStyle(el).textOverflow !== 'ellipsis') continue;
    const cut = el.scrollWidth - el.clientWidth;
    if (cut <= 1 || !shown(el)) continue;
    const free = room(el.closest('quader-address-field') ?? el);
    if (free >= cut) add('clipped', el, isField ? (el as HTMLInputElement).value : (el.textContent ?? ''), `${cut} px of it cut, ${Math.round(free)} px free beside it`);
  }

  // ---- stacked-fits: a table whose rows stand as blocks although its columns fit (R-UI-174) -------------------------------
  // Measured as the data table measures it: its columns at their least widths against the room of its scroll region.
  for (const host of Array.from(document.querySelectorAll<HTMLElement>('.quader-data-table.stacked'))) {
    const table = host.querySelector<HTMLElement>('table.data');
    const region = host.querySelector<HTMLElement>('.table-scroll');
    if (!table || !region || !shown(host)) continue;
    host.classList.remove('stacked');
    const width = table.style.inlineSize;
    table.style.inlineSize = '0px';
    const need = table.getBoundingClientRect().width;
    table.style.inlineSize = width;
    const has = region.clientWidth;
    host.classList.add('stacked');
    if (need <= has) add('stacked-fits', host, host.querySelector('caption')?.textContent ?? '', `stacked, but its columns need ${Math.round(need)} px and have ${has}`);
  }

  return [...found.values()];
}

/**
 * How far the window and the shell's panel scroll past their last content (run in the page, in the window's first height, before
 * the render grows it): something positioned (a text for assistive technology only) that stands at its place in rows a region
 * hides makes the panel, or the window, as long as those rows, and the user scrolls into an empty page (R-UI-173).
 */
export function emptyScroll(): { window: number; panel: number } {
  const main = document.querySelector('main.content');
  const doc = document.scrollingElement;
  if (!main || !doc) return { window: 0, panel: 0 };
  const top = main.getBoundingClientRect().top - main.scrollTop;
  let bottom = 0;
  for (const el of Array.from(main.querySelectorAll('*'))) {
    const r = el.getBoundingClientRect();
    if (r.height < 1 || r.width < 2 || el.closest('.visually-hidden, .cdk-visually-hidden')) continue;
    bottom = Math.max(bottom, r.bottom - top);
  }
  const panel = main.scrollHeight > main.clientHeight + 1 ? main.scrollHeight - bottom - parseFloat(getComputedStyle(main).paddingBottom) : 0;
  return { window: Math.max(0, doc.scrollHeight - doc.clientHeight), panel: Math.max(0, Math.round(panel)) };
}

/**
 * Findings that are right as they are, each with its reason. A finding matches when its check is the same and every pattern
 * given matches (`screen` the screen's id, `what` the element's path, `text` its text). Keep this list short and exact: an
 * entry that allows a whole check, or a whole screen, hides the next defect of its kind.
 */
export const allowed: readonly {
  readonly check: string;
  readonly screen?: RegExp;
  readonly what?: RegExp;
  readonly text?: RegExp;
  readonly why: string;
}[] = [
  {
    check: 'broken',
    screen: /policy-new$/,
    what: /div\.selector-buttons > button/,
    text: /^Add selector /,
    why: "a button that appends a repository's key names the key, and a key longer than a phone's line breaks inside the button rather than push the page sideways (policy-editor.scss)",
  },
  {
    check: 'sideways',
    what: /^pre\.code-block/,
    why: 'a code block keeps its lines as they are copied (a request, a command, a configuration) and scrolls a long one in its own region',
  },
  {
    check: 'sideways',
    what: /^div\.readme-table/,
    why: "a document's table (the manual, a README) keeps its columns and scrolls in its own region where the page is narrower",
  },
];

/** Splits a picture's findings into the defects and the allowed ones. */
export function judge(screen: string, findings: readonly LayoutFinding[]): { defects: LayoutFinding[]; allowed: (LayoutFinding & { why: string })[] } {
  const defects: LayoutFinding[] = [];
  const kept: (LayoutFinding & { why: string })[] = [];
  for (const f of findings) {
    const rule = allowed.find(
      (a) => a.check === f.check && (!a.screen || a.screen.test(screen)) && (!a.what || a.what.test(f.what)) && (!a.text || a.text.test(f.text)),
    );
    if (rule) kept.push({ ...f, why: rule.why });
    else defects.push(f);
  }
  return { defects, allowed: kept };
}

/** One line of failures.txt for a finding. */
export function findingLine(f: LayoutFinding, where: string): string {
  return `layout ${f.check} (${where}): ${f.what}${f.text ? ` "${f.text}"` : ''}: ${f.detail}${f.count > 1 ? ` (and ${f.count - 1} more like it)` : ''}`;
}
