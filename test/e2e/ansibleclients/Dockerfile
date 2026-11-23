# One ansible-galaxy client of the Ansible e2e set (R-ANS-032, Q-ANS-23, Q-ANS-49): the official Python image at its pinned digest, the exact
# ansible-core release under test with every dependency at the exact version of its constraints file, plus what a test pod needs and the
# client under test does not provide: curl (the upload through /files/), and the forwarder that lets the client use the repository's base
# URL unchanged. It is a test image, never pushed, built by test/e2e/ansibleclients/build.sh and loaded into kind by deploy/dev/e2e.sh.
# Not baked in: the dev CA (the pod mounts it; the pod's environment points the client's TLS at it) and any credential.
#
# The Python is the one each release supports: 2.17 runs on 3.10 to 3.12 (3.11 here), 2.21 on 3.12 to 3.14 (3.13 here).
#
#   BASE_IMAGE            one of the E2E_ANSIBLECLIENTS_*_IMAGE pins of tools/versions.env
#   ANSIBLE_CORE_VERSION  the E2E_ANSIBLECLIENTS_*_VERSION pin of the same client
#   ANSIBLE_SERIES        the series of the release (2.17, 2.21): the constraints file of the image is constraints-<series>.txt
#   --build-context fwd=<dir>   a directory with the static forwarder fwd-linux-<arch> (build.sh builds it)
#   --build-context ci=<dir>    .github/scripts/apt-install.sh and fetch.sh (build.sh copies them): every fetch of the build runs
#                         through one of them, bounded per attempt and retried, so a stalled mirror fails the build within minutes
#                         and names the fetch (docs/developer/e2e.md#the-images-of-a-ci-leg); mounted for the RUN alone, in no layer
ARG BASE_IMAGE
FROM ${BASE_IMAGE}
ARG TARGETARCH
ARG ANSIBLE_CORE_VERSION
ARG ANSIBLE_SERIES
ENV PYTHONDONTWRITEBYTECODE=1 ANSIBLE_NOCOLOR=1 PIP_DISABLE_PIP_VERSION_CHECK=1
COPY constraints-${ANSIBLE_SERIES}.txt /tmp/constraints.txt
# pip's --timeout ends a silent connection at 30 s, fetch.sh's bound an attempt that trickles.
RUN --mount=type=bind,from=ci,target=/ci set -eu; \
    bash /ci/fetch.sh "pip install of ansible-core ${ANSIBLE_CORE_VERSION} (pypi.org)" 180 3 \
      pip install --no-cache-dir --timeout 30 --constraint /tmp/constraints.txt "ansible-core==${ANSIBLE_CORE_VERSION}"; \
    rm /tmp/constraints.txt; \
    QUADER_APT_OPTIONS="-o APT::Install-Recommends=false" bash /ci/apt-install.sh curl; \
    rm -rf /var/lib/apt/lists/*
COPY --from=fwd fwd-linux-${TARGETARCH} /usr/local/bin/fwd
COPY clean.sh /usr/local/bin/quader-clean
COPY entrypoint.sh /usr/local/bin/quader-entrypoint.sh
ENTRYPOINT ["/usr/local/bin/quader-entrypoint.sh"]
