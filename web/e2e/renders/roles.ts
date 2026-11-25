import { Api, expectStatus, lit, otherProject, patient, psql } from './seed';

/**
 * Roles an Ansible Galaxy remote read (formats/ansible.md R-ANS-056). The render profile reaches no Galaxy, so the seed writes what the
 * remote's fills would have written: the package `role/<id>` and its versions in the remote `galaxy` of the other project, and the
 * role's facts with the upstream's README in `ansible_roles`. The README is HTML as the Galaxy renders one, with headings, a table, code
 * blocks, an image and a badge; the server sanitizes it when the page asks for it (R-ANS-039).
 *
 * - geerlingguy.pip (role/16): twenty versions, many tags, the README;
 * - acme.rolex (role/17): blocked by a rule of the remote, so its page says it is not delivered;
 * - role/18: a role the remote holds without facts, as one cached before the remote kept them (Q-ANS-113).
 */
export const pipRole = { format: 'ansible', name: 'role/16', display: 'geerlingguy.pip', version: '3.9.0' } as const;
export const blockedRole = { format: 'ansible', name: 'role/17', display: 'acme.rolex', version: '1.0.0' } as const;
export const bareRole = { format: 'ansible', name: 'role/18', version: '1.2.0' } as const;

const pipVersions = [
  '1.0.0', '1.1.0', '1.2.0', '1.3.0', '2.0.0', '2.0.1', '2.1.0', '2.2.0', '2.3.0', '2.4.0',
  '3.0.0', '3.1.0', '3.2.0', '3.3.0', '3.4.0', '3.5.0', '3.6.0', '3.7.0', '3.8.0', '3.9.0',
];

const pipTags = [
  'python', 'pip', 'development', 'packaging', 'system', 'web', 'django', 'flask', 'virtualenv', 'tools', 'linux', 'debian',
  'ubuntu', 'rhel', 'rocky', 'alma', 'fedora', 'centos', 'server', 'automation',
];

const pipReadme = `<h1>Ansible Role: Pip (for Python)</h1>
<p><a href="https://github.com/geerlingguy/ansible-role-pip/actions/workflows/ci.yml" rel="noopener noreferrer"><img alt="CI" src="https://github.com/geerlingguy/ansible-role-pip/actions/workflows/ci.yml/badge.svg"></a></p>
<p>An Ansible Role that installs <a href="https://pip.pypa.io/en/stable/" rel="noopener noreferrer">Pip</a> on Linux, and the Python packages you list.</p>
<h2>Requirements</h2>
<p>On RedHat/CentOS, you may need to have EPEL installed before running this role. You can use the <code>geerlingguy.repo-epel</code> role if you need a simple way to ensure it's installed.</p>
<h2>Role Variables</h2>
<p>Available variables are listed below, along with default values (see <code>defaults/main.yml</code>):</p>
<table>
<thead><tr><th>Variable</th><th>Default</th><th>What it does</th></tr></thead>
<tbody>
<tr><td><code>pip_package</code></td><td><code>python3-pip</code></td><td>The name of the package to install to get <code>pip</code> on the system. For older systems that don't have Python 3 available, you can set this to <code>python-pip</code>.</td></tr>
<tr><td><code>pip_executable</code></td><td><code>pip3</code></td><td>The role will try to autodetect the pip executable based on the <code>pip_package</code> (e.g. <code>pip</code> for Python 2 and <code>pip3</code> for Python 3).</td></tr>
<tr><td><code>pip_install_packages</code></td><td><code>[]</code></td><td>A list of packages to install with pip. Examples below.</td></tr>
</tbody>
</table>
<pre><code class="language-yaml">pip_install_packages:
  # Specify a name and version.
  - name: docker
    version: "1.2.3"
  - name: awscli
    version: "1.11.91"
  # Or specify bare packages to get the latest release.
  - docker
  - awscli
  # Or uninstall a package.
  - name: docker
    state: absent
  # Or update a package to the latest version.
  - name: docker
    state: latest
</code></pre>
<h2>Dependencies</h2>
<p>None.</p>
<h2>Example Playbook</h2>
<pre><code class="language-yaml">- hosts: all

  vars:
    pip_install_packages:
      - name: docker
      - name: awscli

  roles:
    - geerlingguy.pip
</code></pre>
<p><img alt="A diagram of the role" src="https://raw.githubusercontent.com/geerlingguy/ansible-role-pip/master/docs/diagram.png"></p>
<h2>License</h2>
<p>MIT / BSD</p>
<h2>Author Information</h2>
<p>This role was created in 2017 by <a href="https://www.jeffgeerling.com/" rel="noopener noreferrer">Jeff Geerling</a>, author of <em>Ansible for DevOps</em>.</p>
<script>alert("the sanitizer removes this")</script>
`;

/** The roles of the remote `galaxy` and their facts; the rule that blocks acme.rolex, through the API so that every replica reads it. */
export async function seedRoles(admin: Api): Promise<void> {
  const repo = `(SELECT r.id FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = ${lit(otherProject)} AND r.key = 'galaxy')`;
  const pkg = (name: string, display: string): string =>
    `INSERT INTO packages (repo_id, name, display_name) SELECT ${repo}, ${lit(name)}, ${lit(display)} ON CONFLICT (repo_id, name) DO NOTHING;`;
  const versions = (name: string, list: readonly string[]): string =>
    list
      .map(
        (v, i) =>
          // Without a sort key: the background step of the keys gives every version its format's (00116), as for a row of before.
          `INSERT INTO package_versions (package_id, version, display_version, created_at)
             SELECT id, ${lit(v)}, ${lit(v)}, now() - interval '1 day' * ${list.length - i} FROM packages WHERE repo_id = ${repo} AND name = ${lit(name)}
             ON CONFLICT DO NOTHING;`,
      )
      .join('\n');
  const tags = (list: readonly string[]): string => lit(JSON.stringify(list));
  psql(`BEGIN;
${pkg(pipRole.name, pipRole.display)}
${versions(pipRole.name, pipVersions)}
${pkg(blockedRole.name, blockedRole.display)}
${versions(blockedRole.name, ['0.9.0', '1.0.0'])}
${pkg(bareRole.name, bareRole.name)}
${versions(bareRole.name, ['1.0.0', '1.1.0', '1.2.0'])}
INSERT INTO ansible_roles (repo_id, role_id, owner, name, description, github_user, github_repo, github_branch, tags, dependencies, downloads,
                           commit_sha, imported_at, readme_state, readme_file, readme_html, readme_commit)
VALUES (${repo}, 16, 'geerlingguy', 'pip', 'Pip (Python package manager) for Linux.', 'geerlingguy', 'ansible-role-pip', 'master',
        ${tags(pipTags)}::jsonb, ${tags(['geerlingguy.repo-epel'])}::jsonb, 4182733, ${lit('5daab495728fb2b6fa35613039bd26b4034cf414')},
        now() - interval '3 days', 'available', 'README.md', ${lit(pipReadme)}, ${lit('5daab495728fb2b6fa35613039bd26b4034cf414')}),
       (${repo}, 17, 'acme', 'rolex', 'Sets up the Rolex time service of the Acme data centres.', 'acme', 'ansible-role-rolex', 'main',
        ${tags(['time', 'ntp'])}::jsonb, '[]'::jsonb, 312, '', now() - interval '20 days', 'absent', '', '', '')
ON CONFLICT (repo_id, role_id) DO NOTHING;
COMMIT;
`);
  const rules = `/api/v1/projects/${otherProject}/repositories/galaxy/delivery-rules`;
  const res = await patient(() => admin.post(rules, { kind: 'package', name: blockedRole.name, effect: 'block' }));
  await expectStatus(res, [200, 201, 409], `block ${blockedRole.name} in ${otherProject}/galaxy`);
}
