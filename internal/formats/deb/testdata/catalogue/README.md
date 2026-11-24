# The Debian catalogue's recorded index (kit check 22, the parse's test)

`Packages` is eight paragraphs, kept as they came, of `https://download.docker.com/linux/debian/dists/trixie/stable/binary-amd64/Packages`
as it was on 2026-11-24 (two of cagent, two of containerd.io with its Provides, one of docker-compose-plugin, three of
docker-secrets-engine). The catalogue's twin of kit check 22 ([catalogue_fixture_test.go](../../catalogue_fixture_test.go)) and the parse's
test in internal/formatdata/deb read it (remote-curation.md R-CUR-007, formats/debian.md R-DEB-036).

`trixie-InRelease` is `https://download.docker.com/linux/debian/dists/trixie/InRelease`, kept as it came on 2026-11-24, 08:23 local
time (Components `stable edge test nightly`, Architectures `amd64 arm64 armhf s390x ppc64el`); `dists/trixy/InRelease` and
`dists/trixy/Release` answered 404 in the same minute. The fixture's unknown levels read them (Q-CUR-165).
