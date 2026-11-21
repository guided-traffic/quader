// Checks the pins of the OCI clients images (R-OCI-035) for `make versions-check`:
//   node test/e2e/ociclients/check-pins.mjs <E2E_OCICLIENTS_DOCKER_IMAGE> <E2E_OCICLIENTS_PODMAN_IMAGE> <E2E_OCICLIENTS_COSIGN_IMAGE>
// 1. The Docker image is <image>:<x.y.z>-dind@sha256:<digest> (the dind variant carries the CLI, Buildx and dockerd), the Podman
//    image is <image>:v<x.y.z>@sha256:<digest> (an exact release, not a moving minor tag).
//    The cosign image is <image>:v<x.y.z>@sha256:<digest>.
// 2. The three Dockerfiles take their image as a build argument without a default and have no literal pin of their own (the cosign
//    image's second stage is FROM scratch); build.sh passes the three pins.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = dirname(fileURLToPath(import.meta.url));
const [docker, podman, cosign] = process.argv.slice(2);
const problems = [];

if (!/^[^:@]+:\d+\.\d+\.\d+-dind@sha256:[0-9a-f]{64}$/.test(docker ?? '')) problems.push(`E2E_OCICLIENTS_DOCKER_IMAGE '${docker}' is not <image>:<x.y.z>-dind@sha256:<digest>`);
if (!/^[^:@]+:v\d+\.\d+\.\d+@sha256:[0-9a-f]{64}$/.test(podman ?? '')) problems.push(`E2E_OCICLIENTS_PODMAN_IMAGE '${podman}' is not <image>:v<x.y.z>@sha256:<digest>`);
if (!/^[^:@]+:v\d+\.\d+\.\d+@sha256:[0-9a-f]{64}$/.test(cosign ?? '')) problems.push(`E2E_OCICLIENTS_COSIGN_IMAGE '${cosign}' is not <image>:v<x.y.z>@sha256:<digest>`);

for (const [file, args] of [['Dockerfile.docker', ['DOCKER_IMAGE']], ['Dockerfile.podman', ['PODMAN_IMAGE']], ['Dockerfile.cosign', ['COSIGN_IMAGE']]]) {
  const text = readFileSync(join(dir, file), 'utf8');
  for (const arg of args) {
    if (!new RegExp(`^ARG ${arg}\\s*$`, 'm').test(text)) problems.push(`${file}: ARG ${arg} is missing or has a default`);
  }
  const froms = [...text.matchAll(/^FROM\s+(\S+)/gm)].map((x) => x[1]);
  for (const f of froms) if (f !== 'scratch' && !args.some((arg) => f === `\${${arg}}`)) problems.push(`${file}: FROM ${f} is not one of the pinned \${${args.join('}, ${')}}`);
}

const build = readFileSync(join(dir, 'build.sh'), 'utf8');
for (const v of ['E2E_OCICLIENTS_DOCKER_IMAGE', 'E2E_OCICLIENTS_PODMAN_IMAGE', 'E2E_OCICLIENTS_COSIGN_IMAGE']) {
  if (!build.includes(`$${v}`)) problems.push(`build.sh does not pass ${v}`);
}

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
