#!/bin/sh
# The part of make-fixtures.sh that runs inside the Debian image: a package tree with a changelog, a copyright file in the
# machine-readable format, a symbolic link and a manual page, built three times by dpkg-deb.
set -eu
root=/tmp/pkg
mkdir -p $root/DEBIAN $root/usr/bin $root/usr/share/doc/quader-docs $root/usr/share/man/man1
cat > $root/DEBIAN/control <<'CONTROL'
Package: quader-docs
Version: 1.2.0-1
Architecture: all
Maintainer: Quader test archive <debstub@quader.invalid>
Installed-Size: 8
Depends: libc6 (>= 2.34), python3:any | python3-minimal
Recommends: quader-hello
Provides: quader-doc-reader (= 1.2.0)
Section: doc
Priority: optional
Multi-Arch: foreign
Homepage: https://quader.invalid/docs
Description: reads what a package tells about itself
 A package file of the reader test of Quader.
 .
 It holds a changelog, a copyright file in the machine-readable
 format and a link.
CONTROL
printf '#!/bin/sh\necho quader-docs\n' > $root/usr/bin/quader-docs
chmod 755 $root/usr/bin/quader-docs
ln -s quader-docs $root/usr/bin/qd
printf '.TH QUADER-DOCS 1\n' | gzip -9n > $root/usr/share/man/man1/quader-docs.1.gz
cat > $root/usr/share/doc/quader-docs/copyright <<'COPYRIGHT'
Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: quader-docs

Files: *
Copyright: 2026 Quader test archive
License: Apache-2.0

Files: debian/*
Copyright: 2026 Quader test archive
License: GPL-2+

License: Apache-2.0
 The text of the licence.

License: GPL-2+
 The text of the licence.
COPYRIGHT
for v in 1.2.0-1 1.1.0-2 1.1.0-1; do
  printf 'quader-docs (%s) unstable; urgency=medium\n\n  * Release %s.\n\n -- Quader test archive <debstub@quader.invalid>  Thu, 01 Jan 2026 00:00:00 +0000\n\n' "$v" "$v"
done | gzip -9n > $root/usr/share/doc/quader-docs/changelog.Debian.gz
find $root -exec touch -h -d '2026-01-01 00:00:00' {} +
for z in xz zstd gzip; do
  SOURCE_DATE_EPOCH=1767225600 dpkg-deb --root-owner-group -Z$z --build $root /out/quader-docs_1.2.0-1_all.$z.deb >/dev/null
done
