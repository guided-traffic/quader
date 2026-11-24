# Quader manual

The manual for the people who use Quader and the people who run it. The binary embeds these pages and the UI shows
them under **Documentation** in the top bar, so the manual of an installation always matches the version it runs.
Readers on GitHub can browse the same files here.

| Section | Directory | For |
|---|---|---|
| User guide | [user/](user/) | Everyone who reads, installs or publishes packages and files, and the auditors of the installation |
| Administrator guide | [admin/](admin/) | Everyone who installs, configures and operates Quader |

## User guide

1. [Getting started](user/getting-started.md)
2. [Search](user/search.md)
3. [Repositories](user/repositories.md)
4. [Curating a remote repository](user/remote-curation.md)
5. [Packages](user/packages.md)
6. [Vulnerabilities](user/vulnerabilities.md)
7. [Security dashboard](user/security-dashboard.md)
8. [Signatures](user/signatures.md)
9. [Browsing and transferring files](user/files.md)
10. [Connecting a package client](user/client-setup.md)
11. [Container images with Docker and Podman](user/container-images.md)
12. [Debian packages with APT](user/apt-packages.md)
13. [Ansible collections and roles with ansible-galaxy](user/ansible-collections.md)
14. [Personal access tokens](user/access-tokens.md)
15. [Your usage and download limits](user/usage-and-limits.md)
16. [GitLab CI pipelines](user/gitlab-ci-pipelines.md)
17. [Errors and troubleshooting](user/troubleshooting.md)

## Administrator guide

1. [How Quader runs](admin/overview.md)
2. [Installation](admin/installation.md)
3. [Configuration](admin/configuration.md)
4. [TLS and certificates](admin/tls-and-certificates.md)
5. [Endpoints and exposure rules](admin/endpoints.md)
6. [The Quader Bastion](admin/bastion.md)
7. [Sign-in and identities](admin/sign-in-and-identity.md)
8. [Permissions](admin/permissions.md)
9. [Projects and delegated administration](admin/projects.md)
10. [Managing tokens](admin/tokens.md)
11. [Download limits and usage](admin/download-limits.md)
12. [Download links](admin/download-links.md)
13. [GitLab CI federation](admin/gitlab-ci.md)
14. [Managing repositories](admin/repositories.md)
15. [Upstream registries](admin/upstream-registries.md)
16. [Container image repositories](admin/container-repositories.md)
17. [Debian (APT) repositories](admin/apt-repositories.md)
18. [Ansible Galaxy repositories](admin/ansible-repositories.md)
19. [Importing from another registry](admin/import.md)
20. [Advisory sources](admin/vulnerability-sources.md)
21. [Signatures](admin/signatures.md)
22. [Admin dashboard](admin/dashboard.md)
23. [Monitoring and operations](admin/operations.md)
24. [Backup and restore](admin/backup-and-restore.md)
25. [Runbook](admin/runbook.md)
26. [Upgrades](admin/upgrades.md)
27. [Known limitations](admin/limitations.md)

The order of these two lists is the order of the manual in the UI; a test in
[internal/manual](../../internal/manual/) fails when this page and the embedded contents disagree.

## Writing a page

The pages are rendered on the server by the README renderer (goldmark with GitHub-flavoured Markdown, then the
sanitizer allowlist) and shown in the UI. That sets a few rules:

- **Title and summary.** A page starts with one `# Title` line, then one paragraph of plain text (no links, no code)
  that says what the page is for. The UI shows the title as the page heading and in the contents, and the summary on
  the overview. Below that, use `##` and `###` headings only.
- **Links between pages** are relative Markdown links to the `.md` file, with an optional fragment in GitHub's
  heading-slug form: `[tokens](access-tokens.md#create-a-token)`, `[endpoints](../admin/endpoints.md)`. The server
  turns them into links of the help area; a link to a page or heading that does not exist fails the manual's test.
- **No links into the rest of the repository** (specification, developer pages), because the UI does not serve them.
  The exceptions are the exhaustive configuration and Helm reference in the root README and the release notes, linked
  by their absolute GitHub addresses. Links to upstream documentation (npm, Helm, PostgreSQL, RFCs) are absolute.
- **No images and no raw HTML** other than `<details>` and `<summary>` for long reference blocks.
- **English**, present tense, second person for instructions. UI labels in bold as the English UI shows them
  (**Admin area › Endpoints**). Values in configuration snippets are marked `# default` or `# example`.
- **Only what is built.** Every statement is checked against the code of this version. A feature that does not exist
  yet is not described as if it did; [Known limitations](admin/limitations.md) lists what is missing.
