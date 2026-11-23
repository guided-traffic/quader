# The package-manager clients of the npm acceptance matrix (R-NPM-045): one exact version per gate row,
# installed from package-lock.json, and the forwarder that lets them use the registry's base URL
# unchanged. It is a test image: never pushed, built by test/e2e/clients/build.sh, loaded into kind by
# deploy/dev/e2e.sh. Not baked in: the dev CA (the pod mounts it, NODE_EXTRA_CA_CERTS names it) and any
# credential.
#
#   NODE_IMAGE  E2E_CLIENTS_NODE_IMAGE of tools/versions.env (the tag is web/.nvmrc; make versions-check)
#   CURL_VERSION, JQ_VERSION   E2E_CLIENTS_CURL_VERSION, E2E_CLIENTS_JQ_VERSION: the exact apt versions of the two tools the
#               exchange step of the reference pipeline (R-CI-028) runs
#   --build-context fwd=<dir>   a directory with the static forwarder fwd-linux-<arch> (build.sh builds it)
#   --build-context ci=<dir>    .github/scripts/apt-install.sh and fetch.sh (build.sh copies them): every fetch of the build runs
#               through one of them, bounded per attempt and retried, so a stalled mirror fails the build within minutes and
#               names the fetch (docs/developer/e2e.md#the-images-of-a-ci-leg); mounted for the RUN alone, in no layer
ARG NODE_IMAGE

FROM ${NODE_IMAGE} AS install
WORKDIR /clients
COPY package.json package-lock.json ./
# --ignore-scripts keeps every package's scripts from running, then pnpm 12 alone is rebuilt: its install
# script replaces the Node launcher with the native binary of its platform package. Without that step the
# launcher may download a binary when it first runs. The fetch is `npm ci`, bounded per attempt by fetch.sh (npm's own
# fetch-timeout ends a silent request at 60 s, the bound a trickling one); the rebuild fetches nothing.
RUN --mount=type=bind,from=ci,target=/ci \
    bash /ci/fetch.sh "npm ci of the npm clients (registry.npmjs.org)" 180 3 \
      npm ci --ignore-scripts --no-audit --no-fund --fetch-timeout=60000 --fetch-retries=1 \
 && npm rebuild pnpm12 --no-audit --no-fund

FROM ${NODE_IMAGE}
ARG TARGETARCH
ARG CURL_VERSION
ARG JQ_VERSION
# curl and jq: the exchange step of the reference pipeline (R-CI-028) is `printf ... | curl ... | jq`, as a GitLab job runs it.
RUN --mount=type=bind,from=ci,target=/ci \
    QUADER_APT_OPTIONS="-o APT::Install-Recommends=false" bash /ci/apt-install.sh "curl=${CURL_VERSION}" "jq=${JQ_VERSION}" \
 && rm -rf /var/lib/apt/lists/*
# The npm of the Node image is a second npm 11; the clients are the rows of package.json and nothing
# else, so the image has no `npm`, `npx` or `corepack` on its PATH.
RUN rm -rf /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/corepack \
      /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/corepack \
 && mkdir /work && chown 1000:1000 /work
COPY --from=install /clients/node_modules /clients/node_modules
COPY bin/ /usr/local/bin/
# The one `npm` there is, on pnpm 10's PATH only (bin/pnpm10): pnpm 10 shells out to npm for whoami, view and publish.
COPY shim/ /clients/shim/
COPY --from=fwd fwd-linux-${TARGETARCH} /usr/local/bin/fwd
USER 1000:1000
WORKDIR /work
ENV HOME=/work
ENTRYPOINT ["/usr/local/bin/fwd"]
