# The Ansible Galaxy role answers (R-ANS-056)

Answers the public Galaxy gave on 2026-11-25, for the unit tests of a role's facts, its README and the role search
([roles_test.go](../../roles_test.go)). Each is cut to what a test reads, and nothing else is changed:

| File | Request | Cut |
|---|---|---|
| `lookup.json` | `GET https://galaxy.ansible.com/api/v1/roles/?owner__username=geerlingguy&name=docker` | `summary_fields.versions` to its first two entries (eleven came) |
| `content.json` | `GET https://galaxy.ansible.com/api/v1/roles/10923/content/` | `readme_html` to the part before its first `<h2` (8,526 bytes came) |
| `search.json` | `GET https://galaxy.ansible.com/api/v1/search/roles/?autocomplete=docker&page_size=3` | `results` to its first two entries |

What the answers showed, and the spec records (formats/ansible.md, the table of client behaviour): a role document has no minimum Ansible
version, no platforms and no licence (the role's `meta/main.yml` holds them, which the API does not answer); the content document holds
the README only as `readme_html`, the HTML the Galaxy rendered; a search hit has the shape of a lookup's role and names no
`download_url`; the search's `next` link is an `http` address of the Galaxy, which a remote never passes on.
