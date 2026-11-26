import type { CatalogueDocTree } from '../../views/packages/docs-model';

/*
 * The fixture of the search inside a version's documentation at a Bastion (bastion.md R-BAS-124, R-BAS-125): a collection's tree as the
 * Bastion's catalogue holds it (the main installation's answer with each entry's searchWords), and what the main installation's search
 * answered for a list of texts. The integration test TestR_BAS_124_TheBrowsersSearchOfAVersionIsTheMainInstallations
 * (test/integration/ansiblelocal/catalogue_docs_test.go) writes it from a real installation (QUADER_IT_WRITE_DOCS_FIXTURE=1) and compares
 * it on every run; the web unit test of docs-model.ts narrows the tree with the browser's rule and finds the same answers. The JSON
 * between the markers is read by the Go test: keep it plain JSON.
 */

/** What the main installation answered for a text: the entries left (`type/name`), the files, whether the changelog stays, the count. */
export interface DocsSearchCase {
  readonly q: string;
  readonly entries: readonly string[];
  readonly files: readonly string[];
  readonly changelog: boolean;
  readonly matchCount: number;
}

export interface DocsSearchFixture {
  readonly tree: CatalogueDocTree;
  readonly cases: readonly DocsSearchCase[];
}

// BEGIN FIXTURE
export const DOCS_SEARCH_FIXTURE: DocsSearchFixture = {
  "cases": [
    {
      "q": "host",
      "entries": [
        "module/copy_file",
        "module/host_check"
      ],
      "files": [],
      "changelog": false,
      "matchCount": 2
    },
    {
      "q": "HOST answers",
      "entries": [
        "module/host_check"
      ],
      "files": [],
      "changelog": false,
      "matchCount": 1
    },
    {
      "q": "fstype",
      "entries": [
        "module/mount"
      ],
      "files": [],
      "changelog": false,
      "matchCount": 1
    },
    {
      "q": "file",
      "entries": [
        "module/copy_file",
        "module/mount"
      ],
      "files": [],
      "changelog": false,
      "matchCount": 2
    },
    {
      "q": "1.2",
      "entries": [],
      "files": [],
      "changelog": false,
      "matchCount": 0
    },
    {
      "q": "v1",
      "entries": [
        "module/copy_file"
      ],
      "files": [],
      "changelog": false,
      "matchCount": 1
    },
    {
      "q": "_check",
      "entries": [
        "module/host_check"
      ],
      "files": [],
      "changelog": false,
      "matchCount": 1
    },
    {
      "q": "tim",
      "entries": [
        "module/host_check",
        "callback/timer"
      ],
      "files": [],
      "changelog": false,
      "matchCount": 2
    },
    {
      "q": "install",
      "entries": [],
      "files": [
        "install.md"
      ],
      "changelog": false,
      "matchCount": 0
    },
    {
      "q": "acme",
      "entries": [],
      "files": [
        "README.md"
      ],
      "changelog": false,
      "matchCount": 0
    },
    {
      "q": "ext4 xfs",
      "entries": [
        "module/mount"
      ],
      "files": [],
      "changelog": false,
      "matchCount": 1
    },
    {
      "q": "copy-file",
      "entries": [],
      "files": [],
      "changelog": false,
      "matchCount": 0
    },
    {
      "q": "Sort_Order",
      "entries": [],
      "files": [],
      "changelog": false,
      "matchCount": 0
    },
    {
      "q": "zz",
      "entries": [],
      "files": [],
      "changelog": false,
      "matchCount": 0
    }
  ],
  "tree": {
    "files": [
      {
        "name": "README.md",
        "title": "acme.tools 1.0.0"
      },
      {
        "name": "install.md",
        "title": "Install"
      }
    ],
    "groups": [
      {
        "category": "module",
        "count": 3,
        "label": "Modules",
        "types": [
          {
            "count": 3,
            "entries": [
              {
                "hasDocs": true,
                "name": "copy_file",
                "searchWords": "as backup copies copy dest did file host keep old one the to v1.2.3 where",
                "shortDescription": "Copies a file to a host, as v1.2.3 did",
                "type": "module"
              },
              {
                "hasDocs": true,
                "name": "host_check",
                "searchWords": "address answered answers checks host how long name or probe reachable seconds sends the timeout to wait whether within",
                "shortDescription": "Checks whether a host answers",
                "type": "module"
              },
              {
                "hasDocs": true,
                "name": "mount",
                "searchWords": "a device ext4 file fstype mounts options opts src system the type xfs",
                "shortDescription": "Mounts a file system (ext4, xfs)",
                "type": "module"
              }
            ],
            "type": "module"
          }
        ]
      },
      {
        "category": "plugin",
        "count": 1,
        "label": "Plugins",
        "types": [
          {
            "count": 1,
            "entries": [
              {
                "hasDocs": true,
                "name": "timer",
                "searchWords": "order sort tasks the timer times",
                "shortDescription": "Times tasks",
                "type": "callback"
              }
            ],
            "type": "callback"
          }
        ]
      },
      {
        "category": "role",
        "count": 0,
        "label": "Roles",
        "types": []
      }
    ],
    "repository": "it/ansible-local",
    "source": "generated",
    "state": "available",
    "truncated": false
  }
};
// END FIXTURE
