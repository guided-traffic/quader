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
   * Which check found it: `icon-apart`, `broken`, `row-offset`, `row-actions`, `stacked-offset`, `stacked-place`, `label-apart`,
   * `empty-cell`, `cell-overflow`, `clipped`, `cramped`, `stacked-fits`, `sideways`, `empty-scroll`, `tab-scrollbar`, `english`,
   * `inner-scroll`.
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

  // The text of a finding is what the element shows: its text and its elements' text, without an icon's glyph name and without a
  // comment, whose `textContent` is the comment itself (the anchor Angular leaves for a block, `<!--container-->`, read
  // "For a remote repository without upstream keys container" as a fold's label, Q-UI-810).
  const textOf = (el: Element): string =>
    Array.from(el.childNodes)
      .map((n) => {
        if (n.nodeType === Node.TEXT_NODE) return n.textContent ?? '';
        if (n.nodeType === Node.ELEMENT_NODE && !inIcon(n as Element)) return n.textContent ?? '';
        return '';
      })
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
  // In a block of a stacked list an identifier longer than its field's column of values cannot stand on one line, and breaks there
  // (Q-UI-1050): an image's media type of 45 characters at 390 px. Measured, not named: one that would fit and broke is a finding.
  const widerThanItsField = (el: Element): boolean => {
    if (!el.matches('td') || !stackedTable(el)) return false;
    const before = getComputedStyle(el, '::before');
    const name = before.content !== 'none' && before.display !== 'inline' ? parseFloat(before.width) || 0 : 0;
    const cs = getComputedStyle(el);
    const room = el.getBoundingClientRect().width - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight) - name;
    return textRects(el, true).reduce((sum, r) => sum + r.width, 0) > room;
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
      if (kind === 'identifier' && (el.querySelector('.icon-name, .wrap-name') || (el.matches('th') && stackedTable(el)) || widerThanItsField(el))) continue;
      if (kind === 'header' && stackedTable(el)) continue;
      const lines = lineCount(textRects(el, true));
      if (lines > 1) add('broken', el, textOf(el), `a ${kind} in ${lines} lines, ${Math.round(el.getBoundingClientRect().width)} px wide`);
    }
  }

  // ---- broken-word: a short word of a list's cell or of a card's fact broken inside itself (R-UI-171) ----------------------
  // A column narrower than a word of ten letters ("Platfor" / "m enginee" / "ring", a project in the audit log). A long token (an
  // identifier, a path, a hash) may break where it must when no column holds it (Q-UI-425): the run of characters without a space
  // around the break is such a token when it is longer than 16 characters, and so is the text of `code`. A fact's value (`dl > dd`)
  // is measured too (G4-10): there a long token broken is a defect as well where the term's column took more room than the value's,
  // so that the value was squeezed beside a term that did not need the room ("3rdpartylicenses.txt" beside "Licences of the bundled
  // software" on the System page at 390 px).
  // The term of a fact's value: the `dt` before it, where the list lays them out side by side; null where it stands above.
  const termOf = (dd: Element): Element | null => {
    let el = dd.previousElementSibling;
    while (el && el.tagName !== 'DT') el = el.previousElementSibling;
    if (!el) return null;
    const a = el.getBoundingClientRect();
    const b = dd.getBoundingClientRect();
    return a.right <= b.left + 1 && a.bottom > b.top ? el : null;
  };
  for (const cell of Array.from(document.querySelectorAll('table.data > tbody > tr > :is(td, th), dl > dd'))) {
    if (!shown(cell) || (cell.matches('td, th') && stackedTable(cell)) || lineCount(textRects(cell)) < 2) continue;
    const term = cell.matches('dd') ? termOf(cell) : null;
    const squeezed = term !== null && term.getBoundingClientRect().width > cell.getBoundingClientRect().width + 1;
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
          if (before.length + after.length <= 16 || squeezed) broken = `${before}|${after}`;
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
        const units = Array.from(cell.querySelectorAll(UNIT)).filter(
          (unit) => outermost(unit) && shown(unit) && !positioned(unit) && unit.closest('table') === table,
        );
        // A control on a line of its own above the cell's text (a state's badge with the rule that decided it or a link under it,
        // Q-CUR-157) is the cell's first line, and the text under it its second: its middle is above the text's top. A control beside
        // the text shares the text's line and is measured against it (`off-line`).
        const first = units.map((unit) => ({ unit, rect: boxOf(unit) })).sort((a, b) => a.rect.top - b.rect.top)[0];
        if (text && !(first && middle(first.rect) < text.top)) {
          anchors.push({ cell, rect: text, what: '' });
          continue;
        }
        if (text && first) {
          anchors.push({ cell, rect: first.rect, what: nameOf(first.unit) });
          continue;
        }
        // A cell without text: its controls, each once (one measured against a text of its row already is named there).
        for (const unit of units) if (!unitsSeen.has(unit)) anchors.push({ cell, rect: boxOf(unit), what: nameOf(unit) });
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
  // Where a value starts: the left of its first text, or of its first box that holds no text of its own and is seen (a control, an
  // icon, a meter's bar), in reading order. A box that holds text is looked into: its text is where the reader's eye lands.
  function firstLeft(node: Node): number | null {
    for (let n = node.firstChild; n; n = n.nextSibling) {
      if (n.nodeType === Node.TEXT_NODE) {
        if (!/\S/.test(n.nodeValue ?? '')) continue;
        range.selectNodeContents(n);
        const r = Array.from(range.getClientRects()).find((x) => x.width > 0.5 && x.height > 0);
        if (r) return r.left;
        continue;
      }
      if (n.nodeType !== Node.ELEMENT_NODE) continue;
      const e = n as Element;
      if (getComputedStyle(e).display === 'none' || positioned(e) || e.closest('.visually-hidden, .cdk-visually-hidden')) continue;
      const r = e.getBoundingClientRect();
      if (e.matches(UNIT) || e.matches('img, svg, input, select, textarea, button') || (!/\S/.test(e.textContent ?? '') && r.width > 0.5 && r.height > 0.5)) {
        if (r.width > 0.5) return r.left;
        continue;
      }
      const inner = firstLeft(e);
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
  // The span takes the pseudo-element's box as well as its text: in the shared table's blocks the name is a cell of its own, as wide as
  // the list's column of names (Q-UI-1050), and the value stands beside that column, not beside the name's last letter.
  const NAME_BOX = ['display', 'inline-size', 'box-sizing', 'padding-inline-end', 'vertical-align', 'flex', 'overflow-wrap', 'color'] as const;
  // The name's text is measured as it stands, letter by letter: a name wraps between its words where its column is narrower than it,
  // never inside a word (Q-UI-1068). "Veröffentlicht" stood as "Veröffentli" / "cht" in the column of names of the list around it
  // (a Debian repository's versions opened in its packages at 390 px in German, 2026-11-24): `broken` is the word and where it broke.
  function nameBox(cell: Element): { text: DOMRect; column: DOMRect; broken: string | null } | null {
    const label = cell.getAttribute('data-label') ?? '';
    if (label === '') return null;
    const before = getComputedStyle(cell, '::before');
    const span = document.createElement('span');
    span.textContent = `${label} `;
    for (const p of NAME_BOX) {
      // An inline name has no width of its own: its computed width is `auto`, which the span keeps.
      if (p === 'inline-size' && before.display === 'inline') continue;
      span.style.setProperty(p, before.getPropertyValue(p));
    }
    cell.setAttribute(MEASURED, '');
    cell.prepend(span);
    const node = span.firstChild!;
    range.setStart(node, 0);
    range.setEnd(node, label.length);
    const text = range.getBoundingClientRect();
    const column = span.getBoundingClientRect();
    let broken: string | null = null;
    let top: number | null = null;
    for (let i = 0; i < label.length && !broken; i++) {
      range.setStart(node, i);
      range.setEnd(node, i + 1);
      const r = range.getClientRects()[0];
      if (!r) continue;
      if (top !== null && r.top > top + 2 && /[\p{L}\p{N}]/u.test(label[i - 1] ?? '') && /[\p{L}\p{N}]/u.test(label[i] ?? '')) {
        broken = `${/\S*$/u.exec(label.slice(0, i))?.[0] ?? ''}|${/^\S*/u.exec(label.slice(i))?.[0] ?? ''}`;
      }
      top = r.top;
    }
    span.remove();
    cell.removeAttribute(MEASURED);
    return { text, column, broken };
  }
  // ---- stacked-place: a field of a stacked list at the same place in every block (R-UI-174, Q-UI-1050) -------------------------
  // The fields of a stacked row stand a line each, the names in one column and the values in the next, so a reader finds a field at
  // the same place in every block of the list: every name of a list starts at one x and every value at another, within LINE_TOLERANCE
  // px, and no two fields of a block share a line. Side by side, as many to a line as fit, "Format" stood beside "Project" in one
  // block and beside "Class" in the next (T-25's renders, 2026-11-23).
  const places = new Map<Element, { name: number; value: number; label: string }>();
  for (const row of Array.from(document.querySelectorAll<HTMLTableRowElement>('.quader-data-table.stacked table.data > tbody > tr'))) {
    if (!shown(row)) continue;
    const facts: { cell: Element; box: DOMRect; value: DOMRect | null; name: DOMRect; piece: number | null; room: number }[] = [];
    for (const cell of Array.from(row.cells)) {
      if (!cell.matches('td[data-label]:not(.actions)') || !shown(cell) || getComputedStyle(cell, '::before').content === 'none') continue;
      const parts = [lines(cell)[0]?.reduce((a, b) => (a.top <= b.top ? a : b)) ?? null];
      for (const unit of Array.from(cell.querySelectorAll(UNIT))) if (outermost(unit) && shown(unit) && !positioned(unit)) parts.push(boxOf(unit));
      const value = parts.filter((r): r is DOMRect => r !== null).reduce<DOMRect | null>((a, b) => (a && a.top <= b.top ? a : b), null);
      const piece = firstPiece(cell);
      const measured = nameBox(cell);
      if (!measured) continue;
      if (measured.broken) {
        const [head, tail] = measured.broken.split('|');
        add(
          'broken',
          cell,
          cell.getAttribute('data-label') ?? '',
          `the column's name "${head}${tail}" broken inside its word ("${head}" / "${tail}"), the column of names ${Math.round(measured.column.width)} px wide`,
        );
      }
      const name = measured.text;
      // The room beside the name: a cell of a block may grow to the block's width, so the row's width less the cell's padding and the
      // name's box (its column, in the shared table's blocks).
      const cs = getComputedStyle(cell);
      const rs = getComputedStyle(row);
      const rowWidth = row.getBoundingClientRect().width - parseFloat(rs.paddingLeft) - parseFloat(rs.paddingRight);
      const nameWidth = measured.column.right - (cell.getBoundingClientRect().left + parseFloat(cs.paddingLeft) + parseFloat(cs.borderLeftWidth));
      const room = rowWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight) - nameWidth;
      facts.push({ cell, box: cell.getBoundingClientRect(), value, name, piece, room });
      // Where the value starts: the left of its first piece in reading order.
      const start = firstLeft(cell);
      if (start !== null) {
        const table = cell.closest('.quader-data-table');
        if (table) {
          const place = { name: name.left, value: start, label: cell.getAttribute('data-label') ?? '' };
          const known = places.get(table);
          if (!known) places.set(table, place);
          else if (Math.abs(place.name - known.name) > LINE_TOLERANCE || Math.abs(place.value - known.value) > LINE_TOLERANCE) {
            add(
              'stacked-place',
              cell,
              cell.textContent ?? '',
              `"${place.label}" has its name at ${Math.round(place.name - known.name)} px and its value at ${Math.round(place.value - known.value)} px from where "${known.label}" of the same list has them`,
            );
          }
        }
      }
    }
    for (const f of facts) {
      // A value whose first piece (its first word, or its first box that does not break: a link of the house, a badge) is wider
      // than the room beside the name could not stand there: on a line of its own is its right place.
      // "Under the name" is the value's middle below the name's bottom, not its top against that bottom: the box of a line of text is
      // its font's content area, which outgrows a line of 1rem by a pixel or two and by a different amount in each engine (Firefox
      // measured the name 18 px high where Chromium measured 17, so a value on the next line started 2 px above the name's bottom).
      if (f.value && (f.value.top + f.value.bottom) / 2 >= f.name.bottom && (f.piece === null || f.piece <= f.room)) {
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
        // A field beside another on one line of the block has its place by the widths of the values before it (Q-UI-1050).
        if (f.cell.closest('.quader-data-table')) {
          add('stacked-place', f.cell, f.cell.textContent ?? '', `"${f.cell.getAttribute('data-label') ?? ''}" stands beside "${first.cell.getAttribute('data-label') ?? ''}" on one line of the block`);
        }
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

  // ---- cell-overflow: a cell's text or icon reaching past its cell into the next (R-UI-173) --------------------------------------
  // A table lays its columns out by their least widths; content that claims a least width of 0 (a grid column of minmax(0, 1fr), a
  // flex item of min-inline-size 0) lets the table make its column narrower than its longest word, and the word runs under the
  // value of the next column (the System page's replicas at 1,280 px, 2026-11-23). A table that stands as blocks and content
  // that a box inside the cell clips or scrolls are left out: what is cut is the check clipped's.
  for (const cell of Array.from(document.querySelectorAll<HTMLElement>('main table td, main table th, [role="dialog"] table td, [role="dialog"] table th'))) {
    if (cell.closest('.quader-data-table.stacked') || !shown(cell)) continue;
    const edge = cell.getBoundingClientRect().right + 1;
    const clips = (el: Element | null): boolean => {
      for (let e = el; e && e !== cell; e = e.parentElement) {
        if (getComputedStyle(e).overflowX !== 'visible' || getComputedStyle(e).position === 'absolute' || getComputedStyle(e).position === 'fixed') return true;
      }
      return false;
    };
    const walker = document.createTreeWalker(cell, NodeFilter.SHOW_TEXT);
    let worst: { el: Element; past: number; text: string } | null = null;
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      const text = n.textContent ?? '';
      const el = n.parentElement;
      if (!text.trim() || !el || !shown(el) || clips(el)) continue;
      const range = document.createRange();
      range.selectNodeContents(n);
      const past = Math.max(...Array.from(range.getClientRects()).map((r) => r.right)) - edge;
      if (past > 0 && (!worst || past > worst.past)) worst = { el: inIcon(el) ? el.closest(ICON)! : el, past, text };
    }
    if (worst) add('cell-overflow', worst.el, worst.text, `${Math.round(worst.past + 1)} px past the right edge of its cell`);
  }

  // ---- clipped: a text cut although there is room beside it (R-UI-173) ----------------------------------------------------
  // An element cut with an ellipsis, or a read-only field's value cut (an address, a command), where the box that holds it has the
  // room the whole text needs to the element's right, up to the next element on its line (a copy button); a field with its joined
  // button is measured as one. A field the user types
  // into is as wide as its expected input (R-UI-085) and scrolls a longer value.
  const room = (el: Element): number => {
    const box = el.parentElement?.closest('td, th, li, p, div, dd, label, section, form');
    if (!box) return 0;
    const s = getComputedStyle(box);
    let right = box.getBoundingClientRect().right - parseFloat(s.paddingRight) - parseFloat(s.borderRightWidth);
    // A control after it on its line (a copy button beside a cut digest) holds its room: the room ends where it starts.
    const own = el.getBoundingClientRect();
    for (let next = el.nextElementSibling; next; next = next.nextElementSibling) {
      const r = next.getBoundingClientRect();
      if (r.width > 0 && r.top < own.bottom && r.bottom > own.top && r.left >= own.right - 1) {
        right = Math.min(right, r.left);
        break;
      }
    }
    return right - own.right;
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

  // ---- empty-cell: a list's cell without a value that shows nothing (R-UI-146, Q-UI-561) ------------------------------------
  // A cell without a value is the en dash (`<quader-no-value>`), heard as "None": an empty cell reads as not loaded. A cell of a column
  // whose header is for assistive technology only (an action, a check box, a download) may stay empty, as a row without that action
  // has nothing there; so may a cell across the row (an opened row's region). The projects' **Your role** was empty for an
  // administrator who is no member (2026-11-23).
  for (const table of Array.from(document.querySelectorAll<HTMLTableElement>('table.data'))) {
    if (!shown(table) || table.closest('.readme')) continue;
    const heads = Array.from(table.tHead?.rows[0]?.cells ?? []);
    for (const row of Array.from(table.tBodies).flatMap((b) => Array.from(b.rows))) {
      if (row.closest('table') !== table) continue;
      Array.from(row.cells).forEach((cell, i) => {
        if (cell.tagName !== 'TD' || cell.colSpan > 1 || cell.matches('.actions')) return;
        const head = heads[i];
        if (!head || head.querySelector(':scope > .th-inner > .visually-hidden, :scope > .th-inner > .th-sort > .visually-hidden')) return;
        if ((cell.textContent ?? '').trim() !== '' || cell.querySelector('*:not(:empty), input, img, svg')) return;
        add('empty-cell', cell, head.textContent ?? '', `the cell of "${(head.textContent ?? '').trim()}" shows nothing; a cell without a value is the dash`);
      });
    }
  }

  // ---- cramped: a short text wrapped beside a column that has room to spare (R-UI-173, Q-UI-1060) -------------------------
  // In a table that stands as a table, a short text (a summary, a count with its unit: at most CRAMPED_CHARS characters of its first
  // line's text, a `.secondary` line left out) in two or more lines, while the other columns together have the room its one line needs
  // and use none of it: a column is as wide as its widest content in every row, its head included, only where something set its width
  // (a fixed column), and the rest of its width is room that no row uses. W-5 saw "3 findings ·" / "0 fixable" in a column of the
  // versions at 1,024 px beside such a column (2026-11-20). A column that has a wrapped text of its own has no room to give. Measured as
  // `stacked-fits` measures: the cell is set on one line for the measurement (`white-space: nowrap`), and set back in the same task; a
  // cell whose lines stay under it has them by its layout (a mark in a block under a name, a line break), not by a wrap.
  const CRAMPED_CHARS = 40;
  const CRAMPED = 'data-quader-layout-cramped';
  const crampedStyle = document.createElement('style');
  crampedStyle.textContent = `[${CRAMPED}], [${CRAMPED}] * { white-space: nowrap !important; }`;
  /** The content box's left and width of a cell. */
  const contentBox = (cell: Element): { left: number; width: number } => {
    const r = cell.getBoundingClientRect();
    const cs = getComputedStyle(cell);
    const left = r.left + parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft);
    return { left, width: r.right - parseFloat(cs.borderRightWidth) - parseFloat(cs.paddingRight) - left };
  };
  /** The visible text of a cell, without an icon's glyph name, a `.secondary` line and text for assistive technology only. */
  const primaryText = (cell: Element): string => {
    let text = '';
    const walker = document.createTreeWalker(cell, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      const parent = n.parentElement;
      if (!parent || inIcon(parent) || !shown(parent) || parent.closest('.secondary')) continue;
      text += n.nodeValue ?? '';
    }
    return text.replace(/\s+/g, ' ').trim();
  };
  /**
   * How far a cell's content reaches from its content box's left: its text, its units (a badge with its padding, an icon, a control)
   * and the boxes that hold neither an element nor text (a meter's bar, a picture). A block that holds only text is as wide as the cell
   * whatever its text, so its text is measured, not its box.
   */
  const reach = (cell: Element, left: number): number => {
    let right = left;
    for (const r of textRects(cell)) right = Math.max(right, r.right);
    for (const el of Array.from(cell.querySelectorAll('*'))) {
      if (!el.matches(UNIT) && (el.children.length > 0 || /\S/.test(el.textContent ?? ''))) continue;
      if (!shown(el) || positioned(el)) continue;
      right = Math.max(right, el.getBoundingClientRect().right);
    }
    return right - left;
  };
  for (const table of Array.from(document.querySelectorAll<HTMLTableElement>('main table, [role="dialog"] table'))) {
    if (!shown(table) || table.closest('.readme') || getComputedStyle(table).display !== 'table') continue;
    const rows = Array.from(table.rows).filter((row) => row.closest('table') === table && shown(row) && Array.from(row.cells).every((c) => c.colSpan === 1));
    const columns = new Map<number, { width: number; used: number; wraps: boolean; long: boolean; name: string }>();
    const candidates: { cell: Element; column: number; text: string }[] = [];
    for (const row of rows) {
      Array.from(row.cells).forEach((cell, i) => {
        if (!shown(cell)) return;
        const box = contentBox(cell);
        const col = columns.get(i) ?? { width: box.width, used: 0, wraps: false, long: false, name: '' };
        col.width = Math.max(col.width, box.width);
        col.used = Math.max(col.used, reach(cell, box.left));
        // A column of relative times keeps the width of the widest one of the language (Q-UI-1069): what its rows' ages leave of it is
        // held for the ages of another day, not room to give.
        if (cell.matches('th.time-column')) col.used = Math.max(col.used, parseFloat(getComputedStyle(cell).minInlineSize) || 0);
        if (row.parentElement?.tagName === 'THEAD') col.name ||= primaryText(cell);
        const lines = lineCount(textRects(cell, true));
        if (lines > 1) {
          col.wraps = true;
          const short = primaryText(cell);
          // A column of long values that wrap (an owner's path, a description) is as wide as the list gives it, and a short value of it
          // wraps at that width as the long ones do: nothing of it is cramped.
          if (short.length > CRAMPED_CHARS) col.long = true;
          else if (row.parentElement?.tagName === 'TBODY' && short.length > 0) candidates.push({ cell, column: i, text: short });
        }
        columns.set(i, col);
      });
    }
    if (candidates.length === 0) continue;
    // The room the other columns have and use in no row, in a column without a wrapped text of its own.
    const givers = (except: number): { name: string; px: number }[] =>
      [...columns]
        .filter(([i, c]) => i !== except && !c.wraps && c.width - c.used >= 1)
        .map(([i, c]) => ({ name: c.name || `column ${i + 1}`, px: c.width - c.used }));
    // One cell per column: the one that needs the most room for its one line.
    const measured = new Map<number, { cell: Element; text: string; need: number }>();
    document.head.append(crampedStyle);
    for (const c of candidates) {
      const before = contentBox(c.cell).width;
      c.cell.setAttribute(CRAMPED, '');
      const box = contentBox(c.cell);
      // Lines that stay under `nowrap` are laid out so (a block under the text, a line break): nothing wrapped, nothing to give.
      const need = lineCount(textRects(c.cell, true)) > 1 ? 0 : reach(c.cell, box.left) - before;
      c.cell.removeAttribute(CRAMPED);
      const known = measured.get(c.column);
      if (need > 0 && !columns.get(c.column)?.long && (!known || need > known.need)) measured.set(c.column, { cell: c.cell, text: c.text, need });
    }
    crampedStyle.remove();
    for (const [column, m] of measured) {
      const from = givers(column);
      const room = from.reduce((sum, g) => sum + g.px, 0);
      if (room >= m.need) {
        const where = from.map((g) => `"${g.name}" ${Math.round(g.px)}`).join(', ');
        add('cramped', m.cell, m.text, `a short text in ${lineCount(textRects(m.cell, true))} lines needs ${Math.round(m.need)} px more for one line, and the other columns have ${Math.round(room)} px that no row uses (${where})`);
      }
    }
  }

  // ---- tab-scrollbar: a tab strip that shows a scrollbar (R-UI-173, Q-UI-1074) ---------------------------------------------
  // A strip wider than its place scrolls without one, with a fade at the edge; a scrollbar stood under the last tab of a version's
  // strip at 390 px in German. Measured as the room a scrollbar takes below the strip's content, so an overlay scrollbar, which
  // takes none, is not one.
  for (const strip of Array.from(document.querySelectorAll<HTMLElement>('.tab-nav, [role="tablist"]'))) {
    if (!shown(strip)) continue;
    const style = getComputedStyle(strip);
    const bar = strip.offsetHeight - strip.clientHeight - parseFloat(style.borderTopWidth) - parseFloat(style.borderBottomWidth);
    if (bar >= 1) add('tab-scrollbar', strip, strip.getAttribute('aria-label') ?? '', `a scrollbar of ${Math.round(bar)} px under the tabs`);
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

  // An English sentence in a German page (R-UI-214): three English server sentences stood in German pages on one day (the
  // upstream search's hint, a failed bulk's detail, the vulnerability trend's basis). In a page whose language is German, a shown
  // text of at least six words of which three are English function words is a finding, unless it stands in code, in a text marked
  // as another language (`lang="en"`: a technical detail, a format's English texts, the manual) or as not to be translated
  // (`translate="no"`: names, upstream texts, what a person typed). A heuristic: a German sentence holds none of these words, a
  // short English phrase is not seen, and a server's sentence wrongly marked `translate="no"` is the lint rule's to find.
  if ((document.documentElement.lang || '').toLowerCase().startsWith('de')) {
    const ENGLISH = new Set([
      'the', 'and', 'of', 'is', 'are', 'to', 'with', 'which', 'its', 'until', 'that', 'this', 'be', 'has', 'have', 'for', 'when',
      'from', 'your', 'you', 'not', 'been', 'does', 'there', 'their', 'than', 'would', 'should', 'could', 'it',
    ]);
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      const parent = n.parentElement;
      if (!parent || !shown(parent)) continue;
      if (parent.closest('code, pre, kbd, samp, script, style, textarea, [translate="no"]')) continue;
      if (!(parent.closest('[lang]')?.getAttribute('lang') ?? '').toLowerCase().startsWith('de')) continue;
      const words = (n.nodeValue ?? '').toLowerCase().match(/[a-zäöüß']+/g) ?? [];
      if (words.length < 6) continue;
      const english = words.filter((w) => ENGLISH.has(w)).length;
      if (english >= 3) add('english', parent, n.nodeValue ?? '', `an English sentence in a German page: ${english} of its ${words.length} words are English function words`);
    }
  }

  return [...found.values()];
}

/**
 * How far the window and the shell's panel scroll past their last content (run in the page, in the window's first height, before
 * the render grows it): something positioned (a text for assistive technology only) whose containing block is not the box it
 * belongs to stands where nothing else does and makes the panel, or the window, longer than the page, and the user scrolls into an
 * empty page (R-UI-173).
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
 * One scrollbar per page (R-UI-215, Q-UI-1113; run in the page, in the window's first height, before the render grows it, where a
 * region whose height follows the window still scrolls): inside the page (`main.content`) nothing scrolls up and down by itself,
 * but a boxed list of at most 24 rem with a frame above and below that no other scrolling box holds, a navigation column that sticks
 * beside the page, a popup, and a field that takes typed text. Inside an overlay (a dialog, a drawer, a menu, a popup) one element
 * may scroll; another that scrolls inside it is the finding. An element scrolls when its overflow upwards and downwards is `auto` or
 * `scroll` and its content is taller than its box, or a `max-block-size` caps it (a region built to scroll, which the seed's data
 * may not fill). The side bar, the window (`empty-scroll`) and a sideways scroll (`sideways`) are other checks'.
 */
export function innerScroll(): LayoutFinding[] {
  function pathOf(el: Element): string {
    const parts: string[] = [];
    for (let e: Element | null = el; e && e !== document.body && parts.length < 4; e = e.parentElement) {
      parts.unshift(e.tagName.toLowerCase() + Array.from(e.classList).filter((c) => !c.startsWith('ng-')).slice(0, 2).map((c) => '.' + c).join(''));
    }
    return parts.join(' > ');
  }
  function shown(el: Element): boolean {
    const r = el.getBoundingClientRect();
    if (r.width <= 1 || r.height <= 1) return false;
    if (el.closest('.visually-hidden, .cdk-visually-hidden, [hidden]')) return false;
    return getComputedStyle(el).visibility !== 'hidden';
  }
  /** How far it scrolls up and down, or null where it does not; a box built to scroll (capped) counts with 0 px. */
  function scrolls(el: Element): number | null {
    if (el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement || el instanceof HTMLInputElement) return null;
    const style = getComputedStyle(el);
    if (style.overflowY !== 'auto' && style.overflowY !== 'scroll') return null;
    const over = el.scrollHeight - el.clientHeight;
    if (over > 1) return over;
    return style.maxHeight !== 'none' ? 0 : null;
  }
  const px = (n: number): string => Math.round(n).toLocaleString('en');
  const box = 24 * parseFloat(getComputedStyle(document.documentElement).fontSize);
  const found = new Map<string, LayoutFinding & { count: number }>();
  const add = (el: Element, detail: string): void => {
    const what = pathOf(el);
    const known = found.get(what);
    if (known) known.count++;
    else found.set(what, { check: 'inner-scroll', what, text: (el.getAttribute('aria-label') ?? '').slice(0, 80), detail, count: 1 });
  };
  const main = document.querySelector('main.content');
  const overlays = Array.from(document.querySelectorAll('.cdk-overlay-container'));
  const elements = [...(main ? Array.from(main.querySelectorAll('*')) : []), ...overlays.flatMap((o) => Array.from(o.querySelectorAll('*')))];
  for (const el of elements) {
    const over = scrolls(el);
    if (over === null || !shown(el)) continue;
    const overlay = el.closest('.cdk-overlay-container');
    // The ancestors up to the page or the overlay: one that scrolls too makes this one the finding, wherever it stands.
    let popup = false;
    let sticky = false;
    let outer: Element | null = null;
    for (let e: Element | null = el; e && e !== main && e !== overlay; e = e.parentElement) {
      const style = getComputedStyle(e);
      if (style.position === 'absolute' || style.position === 'fixed') popup = true;
      if (style.position === 'sticky') sticky = true;
      if (e !== el && outer === null && scrolls(e) !== null) outer = e;
    }
    if (outer) {
      add(el, `scrolls ${px(over)} px inside ${pathOf(outer).split(' > ').at(-1)}, which scrolls too`);
      continue;
    }
    // An overlay's, or a popup's, one scroll.
    if (overlay || popup) continue;
    // A navigation column that sticks beside the page.
    if (sticky && (el.matches('nav, [role="navigation"], [role="tree"]') || el.querySelector('nav, [role="navigation"], [role="tree"]'))) continue;
    const style = getComputedStyle(el);
    const framed = parseFloat(style.borderTopWidth) >= 1 && parseFloat(style.borderBottomWidth) >= 1;
    if (framed && el.clientHeight <= box) continue;
    const why = framed ? `framed, above the ${px(box)} px of a boxed list` : 'no frame';
    add(el, `scrolls ${px(over)} px of its own in the page (${px(el.clientHeight)} px high, ${why}); the page scrolls instead`);
  }
  return [...found.values()];
}

/**
 * How far a code block may scroll sideways and still be allowed (Q-UI-995): about three windows of 1,440 px, room for a long command
 * or a URL with its query; a line longer than that is a value the page should have shortened.
 */
export const CODE_BLOCK_SCROLL = 4_000;

/**
 * Findings that are right as they are, each with its reason. A finding matches when its check is the same and every pattern
 * given matches (`screen` the screen's id, `what` the element's path, `text` its text, `detail` what was measured). Keep this
 * list short and exact: an entry that allows a whole check, or a whole screen, hides the next defect of its kind. An entry names
 * the element by its place (`what`), not by the words it shows: the German pass renders the same screens in another language.
 */
export const allowed: readonly {
  readonly check: string;
  readonly screen?: RegExp;
  readonly what?: RegExp;
  readonly text?: RegExp;
  readonly detail?: RegExp;
  /**
   * The most px the finding may measure (the first number of `detail`, "the region scrolls 2,400 px sideways") to be allowed: a
   * code block that scrolls a long line is right, one wider than a few screens is not (a Debian remote's stored document carried a
   * keyring of 77,000 characters on one line, a block of about 602,000 px, which the exemption hid for three days, Q-UI-995).
   */
  readonly atMost?: number;
  readonly why: string;
}[] = [
  {
    check: 'row-actions',
    screen: /^all-repositories/,
    what: /td\.actions/,
    detail: /^the action cells of one table are built 2 ways: (menu\+word \(\d+\), menu \(\d+\)|menu \(\d+\), menu\+word \(\d+\))$/,
    why: 'on the list of every project a system repository of upstream answers no client, so its row keeps the menu alone beside the Set up client and menu of every other row (Q-UI-896, Q-UI-1042)',
  },
  {
    check: 'row-actions',
    screen: /^repositories-upstream/,
    what: /td\.actions/,
    detail: /^the action cells of one table are built 2 ways: (menu\+word \(1\), menu \(1\)|menu \(1\), menu\+word \(1\))$/,
    why: 'in the section of the system repositories, upstream/clamav has Set up client while ClamAV definition updates is on, and scanner-trivy, which answers no client, has the menu alone (Q-UI-908)',
  },
  {
    // Matched by its place, never by its words: the label is translated ("Add selector", "Selektor hinzufügen"), and a rule that
    // matched the English words reported the German pass's button as a defect.
    check: 'broken',
    screen: /policy-new$/,
    what: /div\.selector-buttons > button\.btn\.btn-default$/,
    why: "a button that appends a repository's key names the key, and a key longer than a phone's line breaks inside the button rather than push the page sideways (policy-editor.scss)",
  },
  {
    check: 'sideways',
    what: /^pre\.code-block/,
    atMost: CODE_BLOCK_SCROLL,
    why: 'a code block keeps its lines as they are copied (a request, a command, a configuration) and scrolls a long one in its own region, up to a few screens',
  },
  {
    check: 'sideways',
    what: /^div\.readme-table/,
    why: "a document's table (the manual, a README) keeps its columns and scrolls in its own region where the page is narrower",
  },
];

/** The first number of a finding's measurement, without its thousands' separators. */
function measuredPx(detail: string): number {
  const m = /(\d[\d,]*)/.exec(detail);
  return m ? Number(m[1]!.replace(/,/g, '')) : 0;
}

/** Splits a picture's findings into the defects and the allowed ones. */
export function judge(screen: string, findings: readonly LayoutFinding[]): { defects: LayoutFinding[]; allowed: (LayoutFinding & { why: string })[] } {
  const defects: LayoutFinding[] = [];
  const kept: (LayoutFinding & { why: string })[] = [];
  for (const f of findings) {
    const rule = allowed.find(
      (a) =>
        a.check === f.check &&
        (!a.screen || a.screen.test(screen)) &&
        (!a.what || a.what.test(f.what)) &&
        (!a.text || a.text.test(f.text)) &&
        (!a.detail || a.detail.test(f.detail)) &&
        (a.atMost === undefined || measuredPx(f.detail) <= a.atMost),
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
