import { describe, expect, it } from 'vitest';
import { liveGroups } from './search-model';

describe('The typed list of the search field', () => {
  it('R-ANS-056 R-UI-097 a package suggestion is titled by its name and leads to the address of its key', () => {
    const groups = liveGroups({
      q: 'geerlingguy',
      filter: 'all',
      answer: {
        packages: {
          items: [
            { id: 'pkg_role', format: 'ansible', name: 'geerlingguy.pip', key: 'role/16', project: 'web', repository: 'galaxy' },
            { id: 'pkg_npm', format: 'npm', name: '@web/ui-kit', project: 'web', repository: 'npm-local' },
          ],
          more: false,
        },
      },
      instanceAdmin: false,
      pages: [],
      adminPages: [],
    });
    const entries = groups.flatMap((g) => g.entries).filter((e) => e.kind === 'package');
    expect(entries.map((e) => [e.label, e.commands.join('/')])).toEqual([
      ['geerlingguy.pip', '/projects/web/packages/ansible/role/16'],
      ['@web/ui-kit', '/projects/web/packages/npm/@web/ui-kit'],
    ]);
  });
});
