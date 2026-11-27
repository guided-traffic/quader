# Browsing a Bastion

A Bastion is a read-only copy of an installation's public content at an address of its own, for people outside the company. When its administrator switches its web interface on, anyone can browse there the public projects, packages, images and files, see each version's vulnerabilities, signatures and malware scan, read the documentation and copy the commands to install, without signing in. This page says what the pages show and what they leave out.

## The start and a project

Open the Bastion's address in a browser, for example `https://packages.example.com/` *(example)*. It opens in English or German by your browser's language; the link beside the theme button in the top bar switches to the other language, and the theme button chooses light, dark or the system's colours.

- **Projects** lists the projects that publish something here, each by its name with its description. With a single project the start opens that project at once. "No package is published here yet." means there is nothing public yet.
- **A project** lists its packages and images, with the columns of the main installation's list: the package with its description, its format, its latest version, the malware scan of that version and when it changed. **Search packages** finds a package by its name, its description or a keyword; the column headers sort, and **Load more** loads the next 50. Above the list, **Files** leads to the project's repositories of plain files. While the Bastion shows documentation, a search also lists the matching entries of the documentation below the packages.

## A package and a version

An npm or NuGet package and an Ansible collection open on their newest version, a container image and a Debian package on the table of their versions, as on the main installation ([Packages](packages.md)). The table of versions has the column **Vulnerabilities** with each version's grade and open findings.

A version's page shows its facts under its name: the grade (point at it for the counts), **Malware scan** with one word (**Checked**, **Malware**, **Not checkable**, **Unchecked**; open the word for the details, such as the date of the signatures it was checked with), when it was published and its size. Its tabs:

- **README** or **Documentation**, and **Changelog** for a Debian package, while the Bastion shows documentation. A link to an entry of an Ansible collection's documentation (`?doc=module/…`) opens that entry, and the search of the tab searches the version's documentation.
- **Vulnerabilities**: per repository the grade card and the open findings, the worst first: severity and score, the identifier with its aliases, the summary, for an image the affected packages inside it, and the fixed versions ([Without signing in](vulnerabilities.md#without-signing-in) describes the same list).
- **Files**: each file, or each manifest and layer of an image, with its size and SHA-256, its malware scan and its signature; a file's name downloads it from the Bastion.
- **Signatures**, where the version has any: each signature with its kind, signer, issuer, time proof, log and whether it is trusted ([Signatures](signatures.md)).
- **Details**: the metadata of the package type.

The button **Install**, **Pull** or **Download** of the version opens its commands for each client, on the Bastion's address.

## Files

**Files** of a project opens a repository of plain files: its folders and files, each file with its size, SHA-256 and malware scan, **Download** and the commands of each client. A file's signature files (`.sig`, `.asc`, `.sigstore.json`) are listed beside it; the page shows only whether the file's signature is verified, not the signer.

## Set up client

Every panel of commands links **Set up client**: the page of the repository with the steps **Configure** and **Install**, and **Verify** where the installation signs, for the client you choose under **Client**. Everything is public, so the snippets need no token and no sign-in line, and there is no step to publish: a Bastion only serves downloads. Run **Configure** once; the commands of the version pages and of the files rely on it. An image repository has nothing to configure: a public image pulls without signing in, so its page starts at **Install**.

## How current the pages are

Each page ends with **As of** and how long ago this Bastion last brought its copy up to date (point at it for the date and time), and with the links **Imprint** and **Privacy notice** where its operator wrote them. When the copy is more than an hour old *(default)*, a band above the page says "This information is from <date and time>. Newer information is not available right now." The page reads newer information by itself; there is nothing to reload. "Not available right now" means the Bastion has no copy to show yet; the page tries again by itself.

## What a Bastion's pages do not show

Only what is public is on a Bastion. A Bastion's pages never show who published or triaged a package, download counts, dismissed or resolved findings, an advisory's own page, the packages that use a package, or anything of a repository that is not public. Nothing can be changed, published or signed in to there; to publish, use the installation itself ([Install through a Bastion](client-setup.md#install-through-a-bastion)).
