import { afterEach, describe, expect, it } from 'vitest';

/**
 * The link of a page in the overlays (R-UI-044, Q-UI-128's links): a drawer, a dialog and a popover stand in the CDK's overlay container,
 * outside the shell whose rule colours a page's links, so the global stylesheet gives the container's links the page's look. Read from
 * the computed style of the app's own stylesheet in the states' browser: milliseconds, no stack.
 */
describe('links in the overlays (R-UI-044)', () => {
  let container: HTMLElement | null = null;
  afterEach(() => container?.remove());

  /** The colour a custom property of the theme resolves to, as the browser computes a colour. */
  function tokenColour(name: string): string {
    const probe = document.createElement('span');
    probe.style.color = `var(${name})`;
    document.body.append(probe);
    const colour = getComputedStyle(probe).color;
    probe.remove();
    return colour;
  }

  it('R-UI-044 a plain link in a drawer takes the link colour of the page; a menu item and a button keep their own', () => {
    container = document.createElement('div');
    container.className = 'cdk-overlay-container';
    container.innerHTML =
      '<div class="quader-drawer"><div class="drawer"><p>Read <a id="plain" href="#a">the guide</a>.</p>' +
      '<a id="item" class="menu-item" href="#b">Settings</a><a id="button" class="btn btn-default" href="#c">Open</a></div></div>';
    document.body.append(container);
    const link = tokenColour('--quader-link');
    const colour = (id: string): string => getComputedStyle(container!.querySelector(`#${id}`)!).color;
    expect(colour('plain')).toBe(link);
    expect(getComputedStyle(container.querySelector('#plain')!).textUnderlineOffset).not.toBe('auto');
    expect(colour('item')).not.toBe(link);
    expect(colour('button')).not.toBe(link);
  });
});
