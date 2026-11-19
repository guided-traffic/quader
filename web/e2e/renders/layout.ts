/**
 * The layout checks of `make ui-renders` (R-UI-170 to R-UI-174, docs/developer/web-ui.md#renders-of-every-screen): what the
 * owner and the development agent kept finding by eye in the pictures, measured in the page once per picture. Each finding
 * fails the screen, unless `allowed` below names it with its reason.
 *
 * `layoutFindings` runs in the page (`page.evaluate`), so it carries its helpers inside and reads nothing from this module.
 */

/** One defect of the layout, as the render report and failures.txt name it. */
export interface LayoutFinding {
  /**
   * Which check found it: `icon-apart`, `broken`, `row-offset`, `row-actions`, `stacked-offset`, `label-apart`, `clipped`,
   * `stacked-fits`, `sideways`, `empty-scroll`.
   */
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
 * no change of the page but the measurement of a stacked table and of a stacked row's column names, which is undone in the same
 * task (no frame is painted between).
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
  const inIcon = (el: Element | null): boolean => !!el?.closest(ICON);

  /** Shown: has a box, is not hidden, and is not text for assistive technology only. */
  function shown(el: Element): boolean {
    const r = el.getBoundingClientRect();
    // A box of a pixel is clipped to nothing (a stacked list's header that sorts nothing, kept for assistive technology).
    if (r.width <= 1 || r.height <= 1) return false;
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

  // ---- icon-apart and off-line: a control or an icon off the line of its text (R-UI-170, R-UI-175) ---------------------------
  // A unit is a control or an icon: what a person sees as one thing beside a text (UNIT below). A unit inside a unit is a part of
  // the outer one: the glyph of a button is the button, so the button's box is measured, not its glyph. Its text is the nearest
  // text in its row: the next sibling with visible text outside every unit, else the previous one, else the same among the
  // siblings of its parent, and so on up to the row's box (Q-UI-721). How a box lays its children out decides what a row is:
  //   - a flex row: one row, unless a wrapping row put them on two of its lines;
  //   - a flex column, a grid's other row, a block above or below: the unit is laid out above or below its text on purpose, and
  //     the search stops there;
  //   - running text (inline boxes in a block): one row, unless the line broke between them;
  //   - a table's cell bounds the search: the row of a table is the check `row-offset`'s.
  // Its middle must be the middle of the text's first line within LINE_TOLERANCE px; a unit after its text may instead stand on
  // the text's last line (it continues the text: a "new window" mark after a link of two lines); a picture (an avatar, a project's
  // mark, a grade's shield) at least 1.5 lines high beside a text of two lines or more is centred on the first two lines or on the
  // whole text (a card's figure); a control beside a framed box (a code block, a list of members) stands on the box, not on the
  // text inside it, and is not measured (Q-UI-724). A unit that shares no height with the line is `icon-apart`; one that shares some but stands off its middle is
  // `off-line`. A control that a wrapping row moved to its next line (a toolbar at 390 px) is not a finding; an icon, a mark, a
  // status or a copy button moved away from its text is.
  const LINE_TOLERANCE = 1;
  const UNIT = [
    'button:not(.link)',
    'a.btn',
    '[role="button"]:not(.link)',
    '[role="switch"]',
    '[role="checkbox"]',
    '[role="radio"]',
    'input[type="checkbox"]',
    'input[type="radio"]',
    'select',
    'input.text-input',
    '.badge',
    'span:is(.ok, .warn, .bad):has(> .material-icons)',
    '.chip',
    'quader-avatar',
    'quader-project-mark',
    'quader-format-icon',
    'quader-visibility-icon',
    'quader-copy-button',
    'quader-grade-shield',
    '.picture',
    '.material-icons',
  ].join(', ');
  /**
   * The units that belong to their text: moved to another line than it, they are a defect wherever they stand. A unit with a word
   * of its own (a badge, a status, a chip) says what it is wherever it stands, and may wrap to a line of its own like a word.
   */
  const ATTACHED =
    '.material-icons, quader-format-icon, quader-visibility-icon, quader-project-mark, quader-avatar, quader-copy-button, quader-grade-shield, .picture, input[type="checkbox"], input[type="radio"]';
  const PICTURE = 'quader-avatar, quader-project-mark, quader-grade-shield, .picture';
  const range = document.createRange();

  /** Shown for the search of a text: a box of `display: contents` has no box of its own, but its children do. */
  const visibleBox = (el: Element): boolean => {
    const s = getComputedStyle(el);
    if (s.display === 'contents') return s.visibility !== 'hidden' && !el.closest('.visually-hidden, .cdk-visually-hidden, [hidden]');
    return shown(el);
  };
  const positioned = (el: Element): boolean => /^(absolute|fixed)$/.test(getComputedStyle(el).position);
  /** The box of a unit: a host of `display: inline` around a box (a copy button's host) is measured by what it holds. */
  const boxOf = (unit: Element): DOMRect => {
    if (getComputedStyle(unit).display !== 'inline' || unit.children.length === 0) return unit.getBoundingClientRect();
    const rects = Array.from(unit.children).filter(shown).map((c) => c.getBoundingClientRect());
    if (rects.length === 0) return unit.getBoundingClientRect();
    const top = Math.min(...rects.map((r) => r.top));
    const bottom = Math.max(...rects.map((r) => r.bottom));
    const left = Math.min(...rects.map((r) => r.left));
    const right = Math.max(...rects.map((r) => r.right));
    return new DOMRect(left, top, right - left, bottom - top);
  };
  /** The visible text of a node outside every unit and every positioned box, as its lines (top to bottom), each a list of boxes. */
  function lines(node: Node): DOMRect[][] {
    const rects: DOMRect[] = [];
    const take = (t: Node): void => {
      if (!/\S/.test(t.nodeValue ?? '')) return;
      const p = t.parentElement;
      if (!p || p.closest(UNIT) || !shown(p)) return;
      for (let e: Element | null = p; e && e !== node; e = e.parentElement) if (positioned(e)) return;
      range.selectNodeContents(t);
      for (const r of Array.from(range.getClientRects())) if (r.width > 0.5 && r.height > 0) rects.push(r);
    };
    if (node.nodeType === Node.TEXT_NODE) take(node);
    else if (node.nodeType === Node.ELEMENT_NODE) {
      const e = node as Element;
      if (e.matches(UNIT) || !visibleBox(e) || positioned(e)) return [];
      const walker = document.createTreeWalker(e, NodeFilter.SHOW_TEXT);
      for (let n = walker.nextNode(); n; n = walker.nextNode()) take(n);
    }
    const out: { top: number; bottom: number; rects: DOMRect[] }[] = [];
    for (const r of rects) {
      const line = out.find((l) => Math.min(l.bottom, r.bottom) - Math.max(l.top, r.top) >= Math.min(l.bottom - l.top, r.height) / 2);
      if (line) {
        line.top = Math.min(line.top, r.top);
        line.bottom = Math.max(line.bottom, r.bottom);
        line.rects.push(r);
      } else out.push({ top: r.top, bottom: r.bottom, rects: [r] });
    }
    return out.sort((a, b) => a.top - b.top).map((l) => l.rects);
  }
  const middle = (r: DOMRect): number => (r.top + r.bottom) / 2;
  const overlaps = (a: DOMRect, b: DOMRect): boolean => Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 0;
  /** On one line: they share at least half the height of the lower of the two (a badge that wrapped touches the line above). */
  const sameLine = (a: DOMRect, b: DOMRect): boolean => Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) >= Math.min(a.height, b.height) / 2;
  /**
   * A framed box: a block with a border or a filled background of its own and padding (a code block, a list of members, a field).
   * A control beside one stands on the box, as two boxes side by side do, not on the first line of the text inside it.
   */
  const framed = (el: Element): boolean => {
    const s = getComputedStyle(el);
    if (/^inline/.test(s.display) || s.display === 'contents') return false;
    const fill = s.backgroundColor !== 'rgba(0, 0, 0, 0)' && s.backgroundColor !== 'transparent' && parseFloat(s.paddingTop) >= 4;
    return parseFloat(s.borderTopWidth) > 0 || fill;
  };
  /**
   * Whether the unit stands beside a framed box of the sibling: the sibling is one, or holds one at the unit's height (a list of
   * members under its heading, with Add and Remove beside the list). The first 200 boxes of the sibling are looked at.
   */
  const inFrame = (n: Node, box: DOMRect): boolean => {
    if (n.nodeType !== Node.ELEMENT_NODE) return false;
    const walker = document.createTreeWalker(n, NodeFilter.SHOW_ELEMENT);
    let seen = 0;
    for (let e: Node | null = n; e && seen < 200; e = walker.nextNode(), seen++) {
      const el = e as Element;
      if (!el.matches(UNIT) && framed(el) && overlaps(el.getBoundingClientRect(), box)) return true;
    }
    return false;
  };
  const inline = (n: Node): boolean =>
    n.nodeType === Node.TEXT_NODE || (n.nodeType === Node.ELEMENT_NODE && /^inline/.test(getComputedStyle(n as Element).display));
  /** The box that lays out a box's children: a box of `display: contents` hands them to its parent. */
  const layoutBox = (el: Element): Element => {
    let e: Element = el;
    while (e.parentElement && getComputedStyle(e).display === 'contents') e = e.parentElement;
    return e;
  };
  const nodeBox = (n: Node): DOMRect | null => {
    if (n.nodeType === Node.ELEMENT_NODE) return (n as Element).getBoundingClientRect();
    range.selectNodeContents(n);
    return range.getBoundingClientRect();
  };

  /** Where a unit's text is, or why there is none to measure it against. */
  type Text = { node: Node; side: 'after' | 'before'; lines: DOMRect[][]; wrapped: boolean; framed: boolean };
  function textOfUnit(unit: Element, box: DOMRect): Text | 'stacked' | null {
    let branch: Element = unit;
    for (let depth = 0; depth < 8; depth++) {
      const parent = branch.parentElement;
      if (!parent || parent === document.body || parent.matches('main, [role="dialog"], tr, tbody, thead, table')) return null;
      const lay = layoutBox(parent);
      const ls = getComputedStyle(lay);
      const kids = Array.from(parent.childNodes);
      const at = kids.indexOf(branch);
      const order: [ChildNode, 'after' | 'before'][] = [
        ...kids.slice(at + 1).map((n) => [n, 'after'] as [ChildNode, 'after']),
        ...kids.slice(0, at).reverse().map((n) => [n, 'before'] as [ChildNode, 'before']),
      ];
      let stacked = false;
      for (const [n, side] of order) {
        // Whether the sibling holds text at all, cheaply (no layout): a sibling with text decides where the search ends.
        if (!/\S/.test(n.textContent ?? '')) continue;
        // How the box lays the unit's branch and the sibling out, decided before the sibling's text is measured: a block of a
        // whole page beside a header's button is not walked.
        let arrangement: 'row' | 'text' | 'stack';
        if (/flex/.test(ls.display)) arrangement = ls.flexDirection.startsWith('column') ? 'stack' : 'row';
        else if (/grid/.test(ls.display)) {
          const nb = nodeBox(n);
          arrangement = ls.gridTemplateColumns.trim().split(/\s+/).length < 2 || !nb || !overlaps(nb, box) ? 'stack' : 'row';
        } else arrangement = inline(branch) && inline(n) ? 'text' : 'stack';
        if (arrangement === 'stack') {
          // A block above or below the unit, a column, a grid's other row: laid out so on purpose.
          if (n.nodeType === Node.TEXT_NODE || (n.nodeType === Node.ELEMENT_NODE && !(n as Element).matches(UNIT) && visibleBox(n as Element))) stacked = true;
          continue;
        }
        const ls2 = lines(n);
        if (ls2.length === 0) continue;
        const lineBox = (side === 'after' ? ls2[0] : ls2[ls2.length - 1])?.[0];
        if (!lineBox) continue;
        // Two lines of a wrapping row (a toolbar that wrapped, or an icon that left its text), or a line of running text that
        // broke between the unit and its text.
        const wrapped =
          arrangement === 'row'
            ? /flex/.test(ls.display) && ls.flexWrap !== 'nowrap' && !ls2.some((l) => l.some((r) => overlaps(r, box)))
            : !sameLine(lineBox, box);
        return { node: n, side, lines: ls2, wrapped, framed: arrangement === 'row' && inFrame(n, box) };
      }
      if (stacked) return 'stacked';
      // A column of controls without a text (Add, Remove and Move between two lists): the controls stand above and below each other
      // beside the boxes they act on, as a toolbar of their own, and are not paired with a text further away.
      if (depth === 0 && /flex/.test(ls.display) && ls.flexDirection.startsWith('column') && Array.from(parent.children).filter((c) => c.matches(UNIT)).length > 1) {
        return 'stacked';
      }
      // A table's cell is the end of the search: the row is the check row-offset's.
      if (parent.matches('td, th')) return null;
      branch = parent;
    }
    return null;
  }

  const outermost = (el: Element): boolean => !el.parentElement?.closest(UNIT);
  const unitsSeen = new Set<Element>();
  const nameOf = (unit: Element): string =>
    unit.getAttribute('aria-label') ?? ((unit.textContent ?? '').replace(/\s+/g, ' ').trim().slice(0, 30) || unit.tagName.toLowerCase());
  for (const unit of Array.from(document.querySelectorAll(UNIT))) {
    if (!outermost(unit) || !shown(unit) || positioned(unit)) continue;
    const box = boxOf(unit);
    const found = textOfUnit(unit, box);
    if (found === null || found === 'stacked') continue;
    unitsSeen.add(unit);
    const textName = (found.node.textContent ?? '').replace(/\s+/g, ' ').trim();
    const first = found.lines[0] ?? [];
    const last = found.lines[found.lines.length - 1] ?? [];
    const firstBox = first[0];
    if (!firstBox) continue;
    if (found.framed) continue;
    if (found.wrapped) {
      if (unit.matches(ATTACHED)) {
        const near = found.side === 'after' ? firstBox : (last[last.length - 1] ?? firstBox);
        add('icon-apart', unit, textName, `"${nameOf(unit)}" stands at y ${Math.round(box.top)}, its text's line at y ${Math.round(near.top)}`);
      }
      continue;
    }
    const m = middle(box);
    const targets = [middle(firstBox)];
    if (found.side === 'before') {
      const lastBox = last[last.length - 1];
      if (lastBox) targets.push(middle(lastBox));
    }
    // A picture of two lines and more: centred on the first two lines (a name and its key), or on its whole text (a card's figure).
    // Its text is every text beside it on that side: a name and its key may be two boxes of one grid (the project switcher).
    if (unit.matches(PICTURE) && box.height >= 1.5 * firstBox.height) {
      const kids = Array.from(found.node.parentNode?.childNodes ?? []);
      const at = kids.indexOf(found.node as ChildNode);
      const beside = found.side === 'after' ? kids.slice(at) : kids.slice(0, at + 1);
      const all = beside.flatMap((n) => lines(n)).sort((a, b) => a[0]!.top - b[0]!.top);
      const line2 = all.find((l) => l[0]!.top >= firstBox.bottom - 1)?.[0];
      if (line2) targets.push((firstBox.top + line2.bottom) / 2);
      // The whole text is the boxes beside it, its buttons included (a profile's picture beside a sentence and its buttons).
      const boxes = beside.map(nodeBox).filter((r): r is DOMRect => !!r && r.height > 0);
      if (boxes.length > 0 && (line2 || boxes.some((r) => r.height > 1.5 * firstBox.height))) {
        targets.push((Math.min(...boxes.map((r) => r.top)) + Math.max(...boxes.map((r) => r.bottom))) / 2);
      }
    }
    const off = targets.map((t) => m - t).reduce((a, b) => (Math.abs(b) < Math.abs(a) ? b : a));
    if (Math.abs(off) <= LINE_TOLERANCE) continue;
    if (!overlaps(box, firstBox) && !(found.side === 'before' && last[0] && overlaps(box, last[0]))) {
      add('icon-apart', unit, textName, `"${nameOf(unit)}" stands at y ${Math.round(box.top)}, its text's line at y ${Math.round(firstBox.top)}`);
    } else {
      add('off-line', unit, textName, `the middle of "${nameOf(unit)}" (${Math.round(box.height)} px high) is ${Math.abs(off).toFixed(1)} px ${off > 0 ? 'below' : 'above'} the middle of its text's first line`);
    }
  }

  // ---- broken: a short thing broken across lines (R-UI-171) ---------------------------------------------------------------
  // What the style guide keeps on one line: a status or a badge, a chip, a button's label (not a button that is a row's name in
  // the look of a link, `button.link`, which wraps as a name does, nor the toggle that is a section's heading), a tab, a list's header, a date or a time (`time`; the relative
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
    // Not the toggle of a section's heading (`h2 > button.toggle`): its label is the heading and wraps as a heading does (a
    // guide's "Remove a version, import tasks and tombstones" at 390 px, in two lines on the runner and in one on a Mac).
    ['button', 'button:not(.link):not(:is(h1, h2, h3, h4) > .toggle), a.btn, [role="tab"]'],
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

  // ---- row-offset and row-actions: a row laid out differently from its neighbours (R-UI-172, R-UI-176) ---------------------
  // In a table's row the first line of every cell's text and every control of a cell without text (an action cell, a check box's
  // cell) have one middle, the row's name's, within LINE_TOLERANCE px: the baseline is not compared, because a smaller text centred
  // in a badge has the same middle and another baseline. A control in a cell with text is measured against that text above
  // (`off-line`), so a defect is named once. A row that stands as a block (R-UI-174) is measured on its first line: the name and the
  // actions. Every table is measured but a document's (the manual, a guide, a README: `.readme`); its head as well as its body.
  // Then the action cells of one list (`table.data`): the same construction in every row that has one (a menu, a word, an icon),
  // as R-UI-163 asks.
  for (const table of Array.from(document.querySelectorAll<HTMLTableElement>('table'))) {
    if (!shown(table) || table.closest('.readme')) continue;
    const signatures = new Map<string, number>();
    let example: Element | null = null;
    for (const row of Array.from(table.rows)) {
      if (row.closest('table') !== table || !shown(row)) continue;
      const anchors: { cell: Element; rect: DOMRect; what: string }[] = [];
      for (const cell of Array.from(row.cells)) {
        if (!shown(cell)) continue;
        const text = lines(cell)[0]?.[0];
        if (text) {
          anchors.push({ cell, rect: text, what: '' });
          continue;
        }
        for (const unit of Array.from(cell.querySelectorAll(UNIT))) {
          if (!outermost(unit) || unitsSeen.has(unit) || !shown(unit) || positioned(unit) || unit.closest('table') !== table) continue;
          anchors.push({ cell, rect: boxOf(unit), what: nameOf(unit) });
        }
      }
      const ref = anchors.find((a) => a.what === '' && a.cell.matches('th[scope="row"]')) ?? anchors.find((a) => a.what === '') ?? anchors[0];
      if (ref && anchors.length > 1) {
        const refText = (ref.cell.textContent ?? '').replace(/\s+/g, ' ').trim().slice(0, 30);
        const stackedRow = getComputedStyle(row).display !== 'table-row';
        for (const a of anchors) {
          // A stacked row's later lines: each cell there is a fact with its column's name, not a part of the first line.
          if (a === ref || (stackedRow && a.rect.top >= ref.rect.bottom)) continue;
          const off = middle(a.rect) - middle(ref.rect);
          if (Math.abs(off) > LINE_TOLERANCE) {
            add('row-offset', a.cell, a.what || (a.cell.textContent ?? ''), `${a.what ? `the middle of "${a.what}"` : "the middle of the cell's first line"} is ${Math.abs(off).toFixed(1)} px ${off > 0 ? 'below' : 'above'} that of "${refText}" of its row`);
          }
        }
      }
      if (!table.matches('table.data') || row.parentElement?.tagName !== 'TBODY') continue;
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
    if (signatures.size > 1 && example) {
      add('row-actions', example, '', `the action cells of one table are built ${signatures.size} ways: ${[...signatures].map(([k, n]) => `${k} (${n})`).join(', ')}`);
    }
  }

  // ---- stacked-offset and label-apart: the facts of a stacked row (R-UI-174, R-UI-175, Q-UI-762) ---------------------------
  // After its first line a row that stands as a block is a run of facts, each its column's name (`td[data-label]::before`) and
  // its value. The value's first line stands on the name's line (`label-apart`: a meter's box under its "Now"), and the facts side
  // by side on one line of the block have their names on one middle within LINE_TOLERANCE px (`stacked-offset`: an ended token's
  // "Ended", centred with its two lines against "Limit" and "Last used", a line above them); a value of two lines grows downwards.
  // The name is a pseudo-element, measured by a span of its text in its place (`nameBox`). A value whose first piece is wider than the room
  // beside the name (a long key that does not break, a link of the house, which is a box of its own) starts on the next line by
  // right, and is no finding: measured, not named by a selector.
  // The first piece of a value, in the order it is read: the first word of its first text, or its first box that does not break
  // inside (an inline-block, an inline flex or grid box, a picture, a control). A block, a flex or grid box and a list are looked
  // into: they put the value on a line of its own, and the question is whether what starts them would fit beside the name.
  const ATOMIC = /^inline-(block|flex|grid|table)$/;
  function firstPiece(node: Node): number | null {
    for (let n = node.firstChild; n; n = n.nextSibling) {
      if (n.nodeType === Node.TEXT_NODE) {
        const text = n.nodeValue ?? '';
        const start = text.search(/\S/);
        if (start < 0) continue;
        const end = text.slice(start).search(/\s/);
        range.setStart(n, start);
        range.setEnd(n, end < 0 ? text.length : start + end);
        return range.getBoundingClientRect().width;
      }
      if (n.nodeType !== Node.ELEMENT_NODE) continue;
      const e = n as Element;
      const d = getComputedStyle(e).display;
      if (d === 'none' || positioned(e) || e.closest('.visually-hidden, .cdk-visually-hidden')) continue;
      if (ATOMIC.test(d) || e.matches('img, svg, input, select, textarea') || e.matches(UNIT)) {
        const r = e.getBoundingClientRect();
        if (r.width > 0) return r.width;
        continue;
      }
      const inner = firstPiece(e);
      if (inner !== null) return inner;
    }
    return null;
  }
  // The column's name of a cell, as it stands: the pseudo-element has no box to measure, so for the measurement it is replaced by
  // a span of the same text in the same place (an inline box of a block cell, an item of a flex or grid cell) and put back in
  // the same task. A mark of no width beside the name wrapped to its own line in a flex cell and measured a line too low.
  const MEASURED = 'data-quader-layout-name';
  const nameStyle = document.createElement('style');
  nameStyle.textContent = `[${MEASURED}]::before { content: none !important; }`;
  document.head.append(nameStyle);
  function nameBox(cell: Element): DOMRect | null {
    const label = cell.getAttribute('data-label') ?? '';
    if (label === '') return null;
    const span = document.createElement('span');
    span.textContent = `${label} `;
    span.style.color = getComputedStyle(cell, '::before').color;
    cell.setAttribute(MEASURED, '');
    cell.prepend(span);
    range.setStart(span.firstChild!, 0);
    range.setEnd(span.firstChild!, label.length);
    const box = range.getBoundingClientRect();
    span.remove();
    cell.removeAttribute(MEASURED);
    return box;
  }
  for (const row of Array.from(document.querySelectorAll<HTMLTableRowElement>('.quader-data-table.stacked table.data > tbody > tr'))) {
    if (!shown(row)) continue;
    const facts: { cell: Element; box: DOMRect; value: DOMRect | null; name: DOMRect; piece: number | null; room: number }[] = [];
    for (const cell of Array.from(row.cells)) {
      if (!cell.matches('td[data-label]:not(.actions)') || !shown(cell) || getComputedStyle(cell, '::before').content === 'none') continue;
      const parts = [lines(cell)[0]?.reduce((a, b) => (a.top <= b.top ? a : b)) ?? null];
      for (const unit of Array.from(cell.querySelectorAll(UNIT))) if (outermost(unit) && shown(unit) && !positioned(unit)) parts.push(boxOf(unit));
      const value = parts.filter((r): r is DOMRect => r !== null).reduce<DOMRect | null>((a, b) => (a && a.top <= b.top ? a : b), null);
      const piece = firstPiece(cell);
      const name = nameBox(cell);
      if (!name) continue;
      // The room beside the name: a cell of a block grows to the block's width, so the row's width less the cell's padding and its name.
      const cs = getComputedStyle(cell);
      const rs = getComputedStyle(row);
      const rowWidth = row.getBoundingClientRect().width - parseFloat(rs.paddingLeft) - parseFloat(rs.paddingRight);
      const nameWidth = name.right - (cell.getBoundingClientRect().left + parseFloat(cs.paddingLeft) + parseFloat(cs.borderLeftWidth));
      const room = rowWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight) - nameWidth;
      facts.push({ cell, box: cell.getBoundingClientRect(), value, name, piece, room });
    }
    for (const f of facts) {
      // A value whose first piece (its first word, or its first box that does not break: a link of the house, a badge) is wider
      // than the room beside the name could not stand there: on a line of its own is its right place.
      if (f.value && f.value.top >= f.name.bottom - LINE_TOLERANCE && (f.piece === null || f.piece <= f.room)) {
        add(
          'label-apart',
          f.cell,
          f.cell.textContent ?? '',
          `the value starts ${Math.round(f.value.top - f.name.top)} px below the column's name, on a line of its own, though its first piece (${Math.round(f.piece ?? 0)} px) fits the ${Math.round(f.room)} px beside the name`,
        );
      }
    }
    // The facts of one line of the block: their boxes share height, as the boxes of two lines do not.
    const byTop = [...facts].sort((a, b) => a.box.top - b.box.top);
    let line: typeof facts = [];
    let bottom = 0;
    const measureLine = (): void => {
      const [first, ...rest] = line;
      if (!first) return;
      for (const f of rest) {
        const off = middle(f.name) - middle(first.name);
        if (Math.abs(off) > LINE_TOLERANCE) {
          const firstName = first.cell.getAttribute('data-label') ?? '';
          add('stacked-offset', f.cell, f.cell.textContent ?? '', `the column's name "${f.cell.getAttribute('data-label') ?? ''}" is ${Math.abs(off).toFixed(1)} px ${off > 0 ? 'below' : 'above'} "${firstName}" on its line of the block`);
        }
      }
    };
    for (const f of byTop) {
      if (line.length > 0 && f.box.top >= bottom - LINE_TOLERANCE) {
        measureLine();
        line = [];
      }
      bottom = line.length === 0 ? f.box.bottom : Math.max(bottom, f.box.bottom);
      line.push(f);
    }
    measureLine();
  }
  nameStyle.remove();

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

  // ---- joined: a button joined to a field is as high as the field (R-UI-175) ---------------------------------------------------
  // A field with its copy button is one box: the button's edges are the input's. The line rule gave the joined button of a
  // command in an opened row its margins, and the row's stretch made it 48 px beside an input of 32 (2026-11-19).
  for (const host of Array.from(document.querySelectorAll('quader-address-field'))) {
    const field = host.querySelector('input');
    const button = host.querySelector('button');
    if (!field || !button || !shown(field) || !shown(button)) continue;
    const f = field.getBoundingClientRect();
    const b = button.getBoundingClientRect();
    if (Math.abs(f.top - b.top) > 1 || Math.abs(f.bottom - b.bottom) > 1) {
      add('joined', host, field.value, `the joined button is ${Math.round(b.height)} px high (y ${Math.round(b.top)} to ${Math.round(b.bottom)}), its field ${Math.round(f.height)} px (y ${Math.round(f.top)} to ${Math.round(f.bottom)})`);
    }
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
