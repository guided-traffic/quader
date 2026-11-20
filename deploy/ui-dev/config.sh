#!/usr/bin/env bash
# Writes quader.yaml of the UI development profile from the state directory (default .dev/ui-dev) that deploy/ui-dev/up.sh
# created: it reads only the paths of the certificates, the secret files and the spool, and creates no certificate, secret or
# container. up.sh and serve.sh both run it, so a change of the profile's configuration reaches an existing state directory at
# the next backend start, without make ui-dev-up. A key that needs a new file of up.sh still needs up.sh once; this script then
# names the missing file.
# docs/developer/web-ui.md#local-ui-development
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
state="$(cd "${1:-$root/.dev/ui-dev}" 2>/dev/null && pwd)" || { echo "${1:-$root/.dev/ui-dev} is missing: run 'make ui-dev-up' first" >&2; exit 1; }
certs="$state/certs"
secrets="$state/secrets"

for f in "$certs/ca.crt" "$certs/server.crt" "$certs/server.key" "$secrets/db-user" "$secrets/db-pass" "$secrets/valkey-pass" \
  "$secrets/s3-id" "$secrets/s3-secret" "$secrets/keys/dev.key" "$secrets/break-glass-password" "$secrets/idp-client-secret"; do
  [ -f "$f" ] || { echo "$f is missing: run 'make ui-dev-up' first" >&2; exit 1; }
done
mkdir -p "$state/spool"

cat > "$state/quader.yaml.tmp" <<YAML
# Written by deploy/ui-dev/config.sh at every make ui-dev-up and make ui-dev-backend, so edit the script, not this file.
server:
  # The Angular dev server (make ui-dev-web) is the edge: the browser sees https://localhost:4200 only,
  # and the dev server's proxy sends everything outside /ui/ here.
  listen: 127.0.0.1:19080
  ops_listen: 127.0.0.1:19081
  base_url: https://localhost:4200
  trusted_proxies: ["127.0.0.1/32", "::1/128"]
  tls:
    certificates:
      - cert_file: "$certs/server.crt"
        key_file: "$certs/server.key"
  # The links of the image scanning (R-ARCH-038): the role all's worker calls its own scanner and the scanner pulls from its own
  # scan listener, both on a loopback port, each end presenting server.crt and verifying the other against the profile's CA under
  # 127.0.0.1, which server.crt lists. No names of the kinds of pod: in the role all the CA alone admits (Q-ARCH-553).
  internal_tls:
    ca_file: "$certs/ca.crt"
database:
  host: 127.0.0.1
  port: 25432
  name: quader
  username_file: "$secrets/db-user"
  password_file: "$secrets/db-pass"
  ssl_mode: verify-full
  ssl_root_cert_file: "$certs/ca.crt"
  preflight:
    allow_superuser: true
valkey:
  mode: standalone
  address: 127.0.0.1:26390
  password_file: "$secrets/valkey-pass"
  tls:
    ca_file: "$certs/ca.crt"
storage:
  s3:
    bucket: quader-blobs
    endpoint: https://127.0.0.1:28333
    region: us-east-1
    force_path_style: true
    ca_file: "$certs/ca.crt"
    credentials:
      source: static
      access_key_id_file: "$secrets/s3-id"
      secret_access_key_file: "$secrets/s3-secret"
  spool:
    dir: "$state/spool"
secrets:
  encryption_key_files: ["$secrets/keys/dev.key"]
  active_key_id: dev
auth:
  # The profile's sign-in provider (test/devidp, https://localhost:19090, started by serve.sh): it signs in sam, priya,
  # ada or noor without a password, the kinds of user of the renders (docs/developer/web-ui.md#renders-of-every-screen).
  oidc:
    providers:
      ui-dev:
        display_name: UI development users
        issuer: https://localhost:19090
        client_id: quader-ui-dev
        client_secret_file: "$secrets/idp-client-secret"
  # The sign-in screen offers the provider and the break-glass form instead of leaving for the provider at once.
  login_page:
    auto_redirect: false
  break_glass:
    password_file: "$secrets/break-glass-password"
    show_on_login_page: true
    # Longer than the defaults (15m idle, 1h absolute), so a working session is not cut off every hour.
    idle_timeout: 8h
    absolute_timeout: 24h
proxy:
  outbound:
    # The provider is on this machine: its certificate is signed by the profile's CA, and the loopback addresses, which
    # the egress policy refuses by default (R-REPO-038), are allowed. This also lets a remote repository of this profile
    # reach a service on this machine.
    tls:
      ca_files: ["$certs/ca.crt"]
    address_policy:
      allow_cidrs: ["127.0.0.0/8", "::1/128"]
vulnerabilities:
  sources:
    # The advisory sources default to the public archives; a UI session does not download them. The npm advisories come from
    # the archive the render seed uploads into the mirror repository platform/advisory-mirror (web/e2e/renders/advisories.ts,
    # R-VULN-005), so that the screens show every grade of R-VULN-038; until it exists, a sync run fails and says so.
    osv:
      mode: repository
      repository: platform/advisory-mirror
      ecosystems: [npm]
    kev:
      mode: "off"
maintenance:
  tasks:
    # Nor does it fetch the image scanning's database on a schedule: about 142 MB from mirror.gcr.io through upstream/scanner-trivy every
    # sixth hour (R-VULN-044). "Fetch the image database now" on Admin area > Advisory sources runs the task once.
    vuln-sync-trivy-db: {enabled: false, schedule: "23 */6 * * *"}
api:
  rate_limits:
    # make ui-renders opens three pages of one user at a time (UI_RENDERS_WORKERS): a full run reached 197 reads of one
    # user in 4 s against the default read budget of 200 per 4 s (50 a second, burst 200), and an earlier run got 429s.
    # One person in one browser stays far below the default; the renders are not one person.
    read:
      requests_per_second: 200
      burst: 800
    # The seed of make ui-renders writes a whole instance as one administrator in a few seconds (projects, repositories,
    # members, policies, the download limits' rules); on a new database it passed the default write budget (10 a second,
    # burst 50) and its rule of the download limits was answered 429. No person writes that fast; the seed does.
    write:
      requests_per_second: 100
      burst: 400
    # Every anonymous request of the renders comes from 127.0.0.1, one client address for the visitors of all three workers;
    # the default (5 a second, burst 20) answered 429 to whoami, the banner and the projects when three anonymous screens opened
    # at once. Here an address gets the default budget of one signed-in reader (Q-UI-686).
    anonymous:
      requests_per_second: 50
      burst: 200
YAML
mv "$state/quader.yaml.tmp" "$state/quader.yaml"
