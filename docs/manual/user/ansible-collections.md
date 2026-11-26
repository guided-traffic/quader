# Ansible collections and roles with ansible-galaxy

How to point ansible-galaxy at a Quader repository, give it a token, install collections and roles, publish a collection, and what the messages of the client mean. The page Set up client of a repository gives the same commands with the addresses of your installation, and the guide that explains them.

## The address and what ansible-galaxy reads

Quader serves the Galaxy API from repositories of the format **Ansible Galaxy**. The client is `ansible-galaxy` of ansible-core; the setup lists 2.17 and 2.21. The address of a repository is the server URL you give the client:

| Part | Example | Rule |
|---|---|---|
| Address | `https://quader.example.com/api/ansible/web/ansible-local/` | the base URL, `/api/ansible/`, the project key and the repository key, with the trailing slash |

Use the address as it is. There is no separate path for the API: the address itself answers the discovery document the client asks first.

| Class | Collections | Roles |
|---|---|---|
| Local | the collections you publish | not served: `ansible-galaxy role install` against a local repository alone fails with the client's message that the v1 API is not available; a role is published inside a collection ([Publish a role](#publish-a-role-put-it-into-a-collection)) |
| Remote | a cache of a Galaxy, the public one by default | served by translation of the Galaxy's role API, the archive fetched from the hosts the administrator allowed; `role search` and `role info` ask the Galaxy ([Roles from a Galaxy](#roles-from-a-galaxy)) |
| Virtual | the union of its members | answered by the first remote member that knows the role; `role search` by the first remote member that answers it |

A collection name is `namespace.name` in lower case; a version is a semantic version such as `1.2.3` or `1.0.0-rc.1`.

## Configure ansible-galaxy

The page **Set up client** of an Ansible Galaxy repository renders this for that repository:

```ini
# ansible.cfg, next to your playbooks or as ~/.ansible.cfg
[galaxy]
server_list = quader

[galaxy_server.quader]
url=https://quader.example.com/api/ansible/web/ansible-local/
```

```shell
# a repository whose artifacts are not public also needs the token, from the environment
export ANSIBLE_GALAXY_SERVER_QUADER_TOKEN="${QUADER_TOKEN}"
```

`ansible.cfg` does not expand variables, so the token is not written into it: a reference there would be sent as the token itself. `ansible-galaxy` reads the token of a server named `quader` from `ANSIBLE_GALAXY_SERVER_QUADER_TOKEN`, so keep the server named `quader`, or rename the variable after the server (`ANSIBLE_GALAXY_SERVER_<NAME>_TOKEN`, upper case). The page also renders the forms for PowerShell and cmd.

With several servers, `ansible-galaxy` asks them in the order of `server_list` and takes the first that has the collection. Put your own repository first, or give the client one virtual repository.

## Which credential ansible-galaxy sends

| How you configure it | What the client sends |
|---|---|
| `ANSIBLE_GALAXY_SERVER_QUADER_TOKEN`, a `token=` line in `ansible.cfg`, or `--token` | `Authorization: Token <token>` |
| `username=` and `password=` in `ansible.cfg` | HTTP Basic with the token as the password; the user name is ignored |
| a `~/.netrc` line for the host | HTTP Basic for that host |

Prefer the environment variable: it keeps the token out of every file. A wrong token is refused with `401` and never falls back to anonymous access.

**Roles need the `~/.netrc` line.** The client downloads a role archive without its token header and falls back to `~/.netrc`. The setup page renders the line for your host:

```shell
touch ~/.netrc && chmod 600 ~/.netrc
printf 'machine %s login quader password %s\n' quader.example.com "${QUADER_TOKEN}" >>~/.netrc
```

The command appends the line, because netrc expands no variable. The client uses the line for every request to the host and then sends Basic in place of its token header, so keep the token in it valid.
Without the line the role lookup works, because it carries the token, and the archive download fails with the client's `401`. One line serves every repository on the host, so a token that is scoped to one repository cannot fetch a role from another.

## Install

```shell
ansible-galaxy collection install acme.tools            # the newest stable version
ansible-galaxy collection install acme.tools:1.2.0      # an exact version
ansible-galaxy collection install -r requirements.yml   # several, with versions and ranges
```

```yaml
# requirements.yml
collections:
  - name: acme.tools
    version: '>=1.0.0'
    source: quader
roles:
  - name: geerlingguy.pip
```

The dependencies of a collection are resolved by the client through the same servers, so each of them passes the same rules of the repository. Roles are installed with `ansible-galaxy role install -r requirements.yml`; the `roles:` entries need a remote or virtual repository over a Galaxy and the netrc line above. A role with a Git or URL source is fetched by the client on its own and never passes through Quader.

`ansible-galaxy` caches its answers in `~/.ansible/galaxy_cache`. After a version was removed on the server, the cached list can still show it; `--clear-response-cache` forces a new request.

## Publish a collection

```shell
ansible-galaxy collection build                         # writes acme-tools-1.0.0.tar.gz
ANSIBLE_GALAXY_SERVER_QUADER_TOKEN="${QUADER_TOKEN}" \
  ansible-galaxy collection publish acme-tools-1.0.0.tar.gz --server quader
```

The command publishes to the server `quader` of your `ansible.cfg` (see [Configure ansible-galaxy](#configure-ansible-galaxy)) and hands ansible-galaxy the token in that server's variable, so it is no argument other users of the machine could read in the process list.

The collection's `galaxy.yml` names the namespace, name and version, so the command needs no coordinates; the namespace and name have to be lower case. The command prints the import messages of the server and exits with an error when the import failed, and then the repository holds no version. You need the permission **Deploy** on the repository. On a virtual repository the publish goes to its deployment target, which has to be a local repository.

- Publishing the same file again succeeds and changes nothing.
- Different bytes under an existing version are refused (`version_exists`). Publish a new version number.
- A deleted version leaves a tombstone, and its number cannot be published again (`version_tombstoned`) until an administrator clears the tombstone.
- A first publish of a name that a remote member of a virtual repository already serves is refused unless you may manage the repository.

### On the repository's page

Without a client, a collection is published with **Upload collection** in the toolbar of a local or virtual repository's page, which you see
when you may publish there; the page of a namespace offers it too, with the repositories you may publish into. Choose the archive that `ansible-galaxy collection build` made, or drop it, and press **Upload**. Quader reads the
namespace, the name and the version from the archive, never from the file's name, and runs every check of the command above. The dialog then
shows the import log: the state, and the messages with their warnings. **Open version** leads to the version's page. A refusal (no permission,
a version that exists, a namespace that belongs to other publishers, an archive that is too large) shows the reason with its details, and the file
stays chosen.

On a repository that serves only signed versions, the version is stored and readers are refused it until its signature is added; post the
signature as [Sign a collection](#sign-a-collection) describes.

The import messages include warnings that never stop a publish, as Galaxy's import log has them: *No changelog found* when the collection has
none of `changelogs/changelog.yaml`, `CHANGELOG.md` or `CHANGELOG.rst` at its root or in `docs/`, and a warning for a missing README, a
missing `requires_ansible` in `meta/runtime.yml` and a licence that is not named. Quader runs no ansible-lint and no ansible-doc on what you
publish.

The web interface shows the import tasks of a local repository on its **Import tasks** tab, and the version with the Ansible versions it needs, its dependencies, licenses and tags on the page of the version.

## Publish a role: put it into a collection

Quader takes no role of its own: `ansible-galaxy role import` asks a Galaxy to fetch a role from GitHub, which Quader does not do. Put the
role into the `roles/` directory of a collection and publish the collection, as Ansible recommends since 2.10:

```shell
ansible-galaxy collection init acme.platform              # a collection skeleton with a roles/ directory
cp -r ./my-role acme/platform/roles/agent                 # the role, under the name the collection gives it
ansible-galaxy collection build acme/platform && ANSIBLE_GALAXY_SERVER_QUADER_TOKEN="${QUADER_TOKEN}" ansible-galaxy collection publish acme-platform-1.0.0.tar.gz --server quader
```

A playbook then uses the role as `acme.platform.agent`, and the collection's documentation lists it under **Roles**. The page of a role offers
**Upload collection** when you may publish into an Ansible Galaxy repository of the project.

## Roles from a Galaxy

A remote repository of a Galaxy shows every role a client installed through it by its owner and name, `geerlingguy.pip`, with what the
Galaxy says of it: the description, the tags, the repository on GitHub, the downloads at the Galaxy, the dependencies and the README, which
is the first thing on the role's page. The README is fetched from the Galaxy once a client installed the role and again when the Galaxy
names a new commit of it; until it is there the page says so. The install command of the page is `ansible-galaxy role install
geerlingguy.pip,<version>`.

Delivery rules, include and exclude patterns and permissions see a role by the Galaxy's number, `role/16`, which the role's page names: a
rule for `geerlingguy.pip` would name a collection. The tab **Upstream** of the repository finds a role by that name (the refusal of a rule
links there) with every version the Galaxy has and what the rules say of each.

`ansible-galaxy role search <words>` asks the Galaxy through the repository and lists the roles you could install from it: a role the rules
of the repository refuse is not listed. `ansible-galaxy role info geerlingguy.pip` shows what the Galaxy says of the role.

## Sign a collection

`ansible-galaxy` checks the signatures of a version with `--keyring` against `MANIFEST.json`, but has no command to upload one. Sign the
`MANIFEST.json` of the archive you published and post the signature to the version, for example from your CI job:

```shell
tar -xzf acme-tools-1.0.0.tar.gz MANIFEST.json
gpg --detach-sign --armor --output MANIFEST.json.asc MANIFEST.json
jq -n --rawfile s MANIFEST.json.asc '{signature: $s}' | curl -fsS -X POST -H "Authorization: Bearer ${QUADER_TOKEN}" \
  -H 'Content-Type: application/json' --data @- \
  https://quader.example.com/api/v1/projects/web/repositories/ansible-local/ansible/collections/acme.tools/versions/1.0.0/signatures
```

You need the permission **Deploy** on the repository. Quader checks that the signature is one OpenPGP signature that names its key's
fingerprint and that it was made over this version's `MANIFEST.json` (otherwise `422 signature_invalid`), and serves it in `signatures[]` of
the version, where `ansible-galaxy collection install --keyring` and `collection verify` find it. To refuse a version without a valid
signature, give the count with a plus: `--required-valid-signature-count +1`; with `1` alone `ansible-galaxy` installs a version that has no
signature at all and says nothing. A version takes up to 16 signatures; a new
signature of the same key replaces the old one; `DELETE …/signatures/<fingerprint>` removes one.

## Read the documentation of a collection

The page of a collection version has the tabs **Documentation** and **Contents** when Quader holds documentation for it:

- **Documentation** has two panes. The left pane is a tree: **Documentation** (the README and the other Markdown files of the collection), **Modules**, **Plugins** (by type: filter, lookup, module_utils and so on) and **Roles**, each with its count. A row of an entry that has no documentation in its source is greyed and labelled *no documentation*. The right pane is the open page; it scrolls with the page, and the tree stays beside it with a scroll of its own. The search field at the top of the tree finds an entry by its name or by the words of its description, its parameters and its return values, from two characters; the groups keep their counts and the line under the field says how many entries are left. On a narrow screen one pane shows at a time, with a button back to the tree, which shows the entry you read where its group is open.
- A page of a module or plugin shows what `ansible-doc` prints, in the same order: the short description, the collection and version that added it (*added in acme.tools 1.2.0*), a *Deprecated* badge, a line of links to the sections of the page, then Synopsis, Parameters (a table; the suboptions of a parameter are indented and fold with the arrow in front of the name), Notes, Requirements, Attributes, See also, Examples, Return values and Authors. A lookup plugin shows its **Terms** apart from its **Keyword parameters**, a filter or a test its **Input** and **Positional parameters**, as the documentation site does. A section with nothing in it is left out. The examples are coloured as YAML. The page of a role is its README. At the bottom a line says where the documentation comes from: *Generated by Quader from the archive* with the time, or *Taken from the upstream*.
- Under the description of a plugin's option, **Configuration** says how else it is set: an entry of the ini file (*INI entry `[callback_timer] sort_order`*), an environment variable, a variable or a play keyword.
- A name of a module, a plugin, an option or a return value in the text is a link when this version documents it: it opens that page or scrolls to that row in place. Each row of a table has a link button in front of its name (shown when you point at the row) that copies the address of the row; the address ends in `#parameter-<name>` (`#parameter-retry/count` for a suboption) or `#return-<name>`, as on the documentation site, and opens the page at that row, marked.
- **Contents** lists everything the version contains with its type and short description, sorted by type and name, with a filter by type. The number in the tab is the number of entries. A name with documentation opens its page in the Documentation tab; a name without is plain text.
- The address of a page is a link: `…?tab=documentation&doc=module/check_host_reachable&q=host` opens the same page with the same search. Changing the version starts again at the README.

For a local repository Quader writes the documentation when the collection is published, from the text of the archive: it reads the documentation block of each module and plugin and does not run any of the collection's code. A remote repository shows the documentation it fetched from the upstream; until it has, the tab says *The documentation of this version is being fetched*, with a **Refresh** button. If the upstream sent none, or it is too large or unreadable, the tab says so. A version that was published with problems in its documentation blocks lists them on the **Import log** tab, which a local repository's versions have: the import task of the version, newest first, with its state and its messages.

The documentation is shown to those who may read the repository's wiki. Someone who may download the collection and may not read its wiki sees no Documentation and no Contents tab, and no message about it.

The page of a version also has a **Dependencies** section on the **Details** tab: the collections this version needs with their version ranges, each a link to its page when this Quader holds the collection. The link to the collection's own site is labelled **Docs site**.

## Errors and what to do

| What you see | Why, and what to do |
|---|---|
| `HTTP Error 401` at the first request, naming the server | The repository needs a credential and the client sent none or a wrong one. Check that the variable is set in the shell that runs the command and that the server is named `quader`. |
| `403` or `permission_denied` on a publish | The token may read but not deploy to the repository, or the namespace belongs to another publisher. |
| `Mismatch artifact hash` | The archive does not match the SHA-256 in the version document. Quader refuses an archive that does not match what the upstream announced, so you see this when a document was changed. Report it to the administrator. |
| `Galaxy import process failed` with a description | The server refused the archive: not a collection archive, a name or version that breaks the rules, or a manifest that disagrees with the file name. |
| `The v1 API is not available`, or a role that is not found | A role install reached a repository that serves no roles. Put a virtual repository with a remote Galaxy member into `server_list`. |
| A role downloads its metadata and then fails with `401` | The netrc line is missing for this host. |
| `503 upstream_unavailable` | A remote member could not be asked and the merged answer would have been incomplete. Try again. |
