# Browsing and transferring files

The files of a repository are on the repository's own page: open a folder, open a file, download it, and, where your permissions allow, upload files, create folders and delete. Every file is also reachable over plain HTTP below the files path, which this page shows with curl.

## Where the files are

Open a project, select **Repositories** in its side bar and select a repository.

- For a **generic** repository the page lists its files: the folders and files of its root, with **Name**, **Size** and **Last change**.
- For a repository of a **package format** (npm, NuGet, container images, Debian, Ansible) the page lists its packages or images, which is what you usually need ([Repositories](repositories.md#the-content-of-a-repository)). Its raw files, as Quader stores them, are behind **More actions** › **Files**. That page says what the files are and leads back to the list with the link before the sentence. A virtual repository stores only what it generates itself (the index of a Debian or Ansible virtual repository); the files of its members are on their pages.

Select a folder's name to open it. Above the list, the path shows where you are: **Files**, then each folder, the last one the folder you are in; select one to go up. The copy button after the last folder copies the path. On a narrow screen the folders between **Files** and the last one are behind **Show the folders in between**. The breadcrumb at the top of the page ends at the repository.

Every folder and every file has its own address, for example `https://quader.example.com/ui/en/projects/web/repositories/generic-local/files/releases/app-1.2.0.tar.gz`, so you can bookmark it or send the link. The browser's back and forward buttons walk through the folders you opened.

A folder loads its entries 200 at a time; **Load more** at the end of the list loads the next 200. **Filter files** keeps the entries whose name contains what you type, among the entries loaded so far; while the folder has more, a note under the field says so, and **Load more** lets the filter see them too. Files and folders you may not read are not listed.

What a repository holds depends on its class: a local repository shows what was uploaded or published to it, a remote repository the files it has cached so far. See [Repositories](repositories.md#repository-classes).

## The page of a file

Select a file's name to open its page. It shows:

- the file's path, its name last, with **Download** and, if you may delete it, **Delete** at the end of the row;
- **Download address**, the file's address below the files path, with a copy button: the address for curl and other tools (see [Transfer files with curl](#transfer-files-with-curl));
- **Size**, **Media type**, **Created** and **Last changed** (each with who did it, where known), and **SHA-256** with a copy button. If the stored content is missing or damaged, **Content** says so; tell an administrator.
- **Malware scan**, while the installation scans for malware: what the scan said of the file's bytes (see [the malware scan's states](#the-malware-scans-states)).

### The malware scan's states

Quader compares the bytes of every file a client can download with ClamAV's signatures of known malware. The fact **Malware scan**
says what that found; a file Quader writes itself (a Debian index) has none.

| State | What it means | Downloads |
|---|---|---|
| **No malware found**, with the date of the ClamAV database | No signature matched. A signature scan does not find malware that no signature describes yet | Served |
| **Queued** | The file is not checked yet and waits for its scan, since the time named; it is checked in turn | Served, or, where only checked artifacts can be downloaded, the download waits for the scan for up to 25 seconds |
| **No scanner**, **No signatures** | The file cannot be checked now: no malware scanner is connected, or no ClamAV signature database has been imported yet. It is checked once an administrator provides it | As **Queued** |
| **Malware found**, with the signature and, inside an archive, the file it was found in | A signature matched | Refused |
| **Could not be checked**, with the reason: too large, a scan limit was reached inside it, encrypted, or every scan failed | Nothing could check the bytes | Served, or refused where only checked artifacts can be downloaded |
| **Released**, by whom and when | An instance auditor released these bytes after looking at them | Served |

**Downloads are refused.** after the state says that the file cannot be downloaded from this repository now; the page then offers no
**Download**, and a version's **Files** tab none in the file's row. The download address stays: a client that asks gets the refusal and
its reason. What to do then is in
[Errors and troubleshooting](troubleshooting.md#a-download-is-refused-for-malware). The same bytes have the same state in every
repository that holds them.

The files of the two system repositories of the project Upstream, `scanner-trivy` and `clamav`, which an administrator can
open, cannot be downloaded at all: their pages offer no **Download**, no **Download address** and no copy button in a folder's rows,
and say in one sentence which of Quader's work reads them ([Upstream registries](../admin/upstream-registries.md#where-the-system-repositories-show)).

**Details** at the end of the page opens the rest: every checksum (**SHA-256**, **SHA-512**, **SHA-1** and **MD5**), each with a copy button; the downloads (**Downloads**, **Last download** and **Last downloader**), which reach the statistics within a minute of a download; and the file as the management API describes it, with the matching curl requests. They send the token from the environment variable `QUADER_TOKEN`: the header is in double quotes so that your shell fills in the value, and the address is in single quotes. Set the variable first, without typing the token into a shell that records its history.

## What you can do

The page offers only what you may do where you are. Someone who may only read sees the list, the folders, the files, **Download** and the copy buttons.

| Action | Where | Permission |
|---|---|---|
| Download a file | The file's page | Read |
| Copy a file's download address | The copy button in the file's row, or the file's page | Read |
| Upload files, create a folder | **Upload files** and **New folder** above the list of a folder, in a generic local repository | Deploy on the folder |
| Delete a folder | **More actions for this folder** › **Delete folder** above the list of the folder (not the root) | Delete on every file in it |
| Delete a file | **Delete** on the file's page | Delete on the file |

Copying and moving files and a trash can are not available yet; see [Known limitations](../admin/limitations.md).

## Download a file

1. Open the file's page.
2. Select **Download**. The browser saves the file under its name.

The button downloads through the web application, with your sign-in: it needs no token and counts like any other download. The address under **Download address** is the one for tools, and it needs a token unless the repository is open to anonymous readers.

## Upload files

You need the deploy permission on the target folder, and the repository must be a generic local repository. npm packages, NuGet packages, container images, Debian packages and Ansible collections are published with their clients, not uploaded here: their repositories offer no upload, and remote and virtual repositories take no uploads at all. **Set up client** shows how to publish with a client.

1. Open the folder to upload into (the repository's page is its root).
2. Select **Upload files** above the list. It appears only in a generic local repository, and only where you hold the deploy permission.
3. Check **Target folder**. It is relative to the root of the repository and prefilled with the folder you are in; missing folders are created, and an empty field is the root.
4. Drop files onto the dialog, or select **Choose files**.
5. To replace files that already exist at the target, tick **Replace files that already exist**. The option appears only if you hold the overwrite permission there.
6. Select **Upload**.

The files are sent one after another. **Status** shows **waiting**, **computing checksum**, a progress bar, **uploaded**, **cancelled**, or the reason a file failed. **Cancel upload** stops the file that is being sent; **Close** closes the dialog, and the list shows the new files.

- The browser uploads files of up to 100 MiB (the administrator's limit `ui.deploy.max_upload_bytes`: the dialog refuses a larger file before it starts, and the server answers it with **413** before it reads the body). For a larger file the dialog says to use a native client or curl, which take files up to the installation's limit (10 GiB by default).
- The browser computes the SHA-256 of each file before sending it, and Quader checks it on arrival, so a damaged transfer is not stored.
- Without the replace option, a file that exists already is kept, and the dialog says **A file already exists at this path.** Uploading the same bytes again with the replace option changes nothing.
- The repository's include and exclude patterns can refuse a path; ask whoever manages the repository.

## Create a folder

1. Open the folder to create the new folder in.
2. Select **New folder**.
3. Enter the **Folder name**. A name with slashes, such as `2026/q4`, creates the folders in between.
4. Select **Create folder**.

Folders also arise on their own when you upload a file to a path that contains them.

## Delete files and folders

You need the delete permission on the file, or on every file in a folder; if one is missing, nothing is deleted. The root of a repository cannot be deleted here; whoever manages the repository deletes the repository instead.

1. Open the file's page and select **Delete**, or open the folder and select **More actions for this folder** › **Delete folder**.
2. Read the confirmation: it says how many items a folder holds. For a folder of more than 100 items, type the folder name into **Type the name to confirm**.
3. Select **Delete**.

The page then shows the folder above. Deleting is final: Quader has no trash can yet. A very large folder (by default more than 10,000 items) is deleted by a background job; the folder above shows **Deleting** with its progress, and its list is read again when the job has ended.

## Transfer files with curl

Every file of a repository has an address below the files path, with the key of the repository's project and the repository's own key:

```text
https://quader.example.com/files/<project-key>/<repository-key>/<path>
```

| Method | On a file | On a folder (address ends with `/`) |
|---|---|---|
| `GET` | Downloads the file | Lists the entries you may read, as JSON, or as a page with links in a browser |
| `HEAD` | Returns size, checksums and dates without the content | The same for the listing |
| `PUT` | Uploads the file (generic local repositories) | Creates an empty folder, without a body |
| `DELETE` | Deletes the file | Deletes the folder with everything below it |

### Authenticate

curl signs in with a Quader token, never with your password: a personal access token for yourself, or a service-account token for a CI job. Create one under **Profile and tokens**, limited to the repositories and actions it needs; see [Personal access tokens](access-tokens.md). Quader reads the token from the `Authorization: Bearer` header. It also takes the token as the password of HTTP Basic authentication with any user name, unless an administrator switched that off.

Keep the token out of command lines and scripts, where the process list and the shell history would show it. The client setup snippet for curl defines a shell function that reads the token from the environment variable `QUADER_TOKEN` each time it runs and hands it to curl on standard input:

```shell
quader_curl() {
  curl --silent --show-error --config - "$@" <<EOF
header = "Authorization: Bearer ${QUADER_TOKEN}"
EOF
}
```

Set `QUADER_TOKEN` in the environment curl runs in, for example from a masked CI variable, then paste the function into the shell or the CI script. Because standard input carries the header, `quader_curl` cannot upload from standard input; upload a file instead. The variable name can differ on your installation; [Connecting a package client](client-setup.md) shows the snippet as your installation writes it. For a repository open to anonymous readers, plain `curl` without a token downloads as well.

### Download and check

The examples keep the address of one file in a shell variable:

```shell
FILE="https://quader.example.com/files/web/generic-local/releases/app-1.2.0.tar.gz"
quader_curl --fail --remote-name "$FILE"
quader_curl --fail --head "$FILE"
quader_curl --fail --continue-at - --output app-1.2.0.tar.gz "$FILE"
```

The second line downloads the file into the current directory under its name. The third shows its size, its `ETag` (the SHA-256 in hexadecimal) and its `Repr-Digest` without downloading it. The last one resumes a broken download where the local copy ends.

`--fail` makes curl exit with an error instead of saving an error answer as the file. Quader checks a file against its checksum while it serves it whole; a resumed copy joins two transfers, so compare it with the `ETag` (`sha256sum` prints the same hexadecimal value). Add `?stats=false` to the address when a download should not count in the statistics.

### Upload

```shell
quader_curl --fail --upload-file app-1.2.0.tar.gz "$FILE"
quader_curl --fail --upload-file app-1.2.0.tar.gz --header "If-None-Match: *" "$FILE"
quader_curl --fail --upload-file app-1.2.0.tar.gz \
  --header "Repr-Digest: sha-256=:$(openssl dgst -sha256 -binary app-1.2.0.tar.gz | base64):" \
  --header "Content-Type: application/gzip" \
  "$FILE"
```

The first command uploads the file; the folders on the way are created. The second uploads only if the path is free and never replaces a file. The third has Quader check the SHA-256 you computed, refusing the upload with `409` if the bytes differ, and stores a media type.

Quader answers `201` for a new or replaced file and `200` when the path already holds these bytes. Replacing a file with other bytes needs the overwrite permission besides deploy. Without `Content-Type`, the file is served as `application/octet-stream`.

### List and delete

```shell
quader_curl --fail "https://quader.example.com/files/web/generic-local/releases/?limit=100"
quader_curl --fail --request DELETE "$FILE"
```

The first command lists a folder as JSON: `items` holds the entries you may read, and `nextCursor` leads to the next page; pass it as `cursor`. A page holds 1,000 entries unless `limit` asks for another number (at most 10,000). Without the trailing `/`, a folder address redirects to the one with it. The second command deletes a file and answers `204`; a folder of more than 10,000 items answers `202` and is deleted by a background job.

### Common answers

| Status | Code | Meaning |
|---|---|---|
| 401 | `invalid_token` | The token is unknown, expired or revoked, or the variable is empty |
| 403 | `forbidden` | The token lacks the permission: deploy, overwrite or delete |
| 403 | `not_admitted` | The repository's include and exclude patterns refuse this path |
| 404 | `not_found` | The file, the repository or the project does not exist, or you may not read it |
| 405 | `method_not_allowed` | An upload to a remote or virtual repository |
| 409 | `conflict` | An upload to an npm repository, or bytes that differ from the `Repr-Digest` sent |
| 409 | `path_conflict` | A file where a folder is, or the other way round |
| 412 | `precondition_failed` | `If-None-Match: *` and the path exists |
| 413 | `payload_too_large` | The file is larger than the limit of the installation or the repository |

The full list, with what to do for each code, is in [Errors and troubleshooting](troubleshooting.md).

### On an endpoint for customers or partners

An installation can serve repositories on further addresses with exposure rules, for example for customers. There, a file outside the exposed paths answers `404` like a missing one, a listing leaves it out, and uploads and deletes answer `403` with `endpoint_read_only`. Use the token issued for that address, and upload through the main address of the installation.
