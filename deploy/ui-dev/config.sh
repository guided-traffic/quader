#!/usr/bin/env bash
# Writes quader.yaml of the UI development profile from the state directory (default .dev/ui-dev) that deploy/ui-dev/up.sh
# created: it reads only the paths of the certificates, the secret files and the spool, and creates no certificate, secret or
# container. up.sh and serve.sh both run it, so a change of the profile's configuration reaches an existing state directory at
# the next backend start, without make ui-dev-up. A key that needs a new file of up.sh still needs up.sh once; this script then
# names the missing file.
# docs/developer/web-ui.md#local-ui-development
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# The ports of the instance (deploy/ui-dev/instance.sh, UI_INSTANCE); its state directory is the default.
# shellcheck source=deploy/ui-dev/instance.sh
. "$root/deploy/ui-dev/instance.sh"
ui_instance "$root"
state="$(cd "${1:-$UI_STATE}" 2>/dev/null && pwd)" || { echo "${1:-$UI_STATE} is missing: run 'make ui-dev-up UI_INSTANCE=$UI_INSTANCE' first" >&2; exit 1; }
certs="$state/certs"
secrets="$state/secrets"

for f in "$certs/ca.crt" "$certs/server.crt" "$certs/server.key" "$secrets/db-user" "$secrets/db-pass" "$secrets/valkey-pass" \
  "$secrets/s3-id" "$secrets/s3-secret" "$secrets/keys/dev.key" "$secrets/break-glass-password" "$secrets/idp-client-secret"; do
  [ -f "$f" ] || { echo "$f is missing: run 'make ui-dev-up' first" >&2; exit 1; }
done
mkdir -p "$state/spool"

# The image scanner of the profile (deploy/ui-dev/scanners.sh, Q-ARCH-1064), unless UI_SCANNERS=0: quader opens its scan listener,
# calls vuln-scanner-trivy, records that scanner's report of itself by its name, and fetches the analyser's database on the task's
# schedule. Without it the profile names no scanner and fetches no database.
if [ "${UI_SCANNERS:-1}" != 0 ]; then
  scan_listen="  scan_listen: 127.0.0.1:$UI_SCAN_PORT"
  images="  images:
    scanner:
      url: https://127.0.0.1:$UI_TRIVY_PORT
      server_name: vuln-scanner-trivy.quader.test
    registry:
      client_name: vuln-scanner-trivy.quader.test"
  trivy_db="vuln-sync-trivy-db: {enabled: true, schedule: \"23 */6 * * *\"}"
else
  scan_listen="" images=""
  trivy_db="vuln-sync-trivy-db: {enabled: false, schedule: \"23 */6 * * *\"}"
fi
# The malware scanning of the profile (deploy/ui-dev/malware.sh, Q-ARCH-1065), unless UI_SCANNERS=0 or UI_MALWARE=0: quader calls
# malware-scanner-clamav, whose container Docker publishes on 127.0.0.1, admits it and the updater malware-updater-freshclam on the scan
# listener's malware routes by their names, and asks the updater for ClamAV's databases. Without it the section names no scanner and no
# updater, and the profile's files read "No scanner" (R-MAL-024).
if [ "${UI_SCANNERS:-1}" != 0 ] && [ "${UI_MALWARE:-1}" != 0 ]; then
  malware="malware:
  scanner:
    url: https://127.0.0.1:$UI_CLAMAV_PORT
    server_name: malware-scanner-clamav.quader.test
  scan_listener:
    client_name: malware-scanner-clamav.quader.test
    updater_client_name: malware-updater-freshclam.quader.test
  updater:
    url: https://127.0.0.1:$UI_FRESHCLAM_PORT
    server_name: malware-updater-freshclam.quader.test"
else
  malware=""
fi

cat > "$state/quader.yaml.tmp" <<YAML
# Written by deploy/ui-dev/config.sh at every make ui-dev-up and make ui-dev-backend, so edit the script, not this file.
server:
  # The Angular dev server (make ui-dev-web) is the edge: the browser sees https://localhost:4200 only,
  # and the dev server's proxy sends everything outside /ui/ here.
  listen: 127.0.0.1:$UI_BACKEND_PORT
  ops_listen: 127.0.0.1:$UI_OPS_PORT
  base_url: https://localhost:$UI_WEB_PORT
  # The base URL the render seed's Bastion dmz-berlin reported (web/e2e/renders/bastions.ts): Set up client answers a Bastion's
  # snippets only for a base URL listed here (R-BAS-007). No request reaches it; the profile runs no Bastion.
  additional_base_urls: ["https://packages.dmz-berlin.example.com"]
  trusted_proxies: ["127.0.0.1/32", "::1/128"]
  tls:
    certificates:
      - cert_file: "$certs/server.crt"
        key_file: "$certs/server.key"
  # The trust anchor of the links between quader's pods (R-ARCH-038). The role all builds no scanner of any kind (Q-ARCH-563); the
  # profile runs vuln-scanner-trivy beside it in containers (deploy/ui-dev/scanners.sh, Q-ARCH-1064) and malware-scanner-clamav as the
  # chart's pod on Docker (deploy/ui-dev/malware.sh, Q-ARCH-1065), which this process calls and which read from the scan listener below;
  # quader presents server.crt (localhost), and in the role all the CA alone admits a scanner (Q-ARCH-553). With UI_SCANNERS=0 no link of the scanning runs, and the render seed's scanner replicas are rows it writes
  # (web/e2e/renders/scanners.ts).
  internal_tls:
    ca_file: "$certs/ca.crt"
$scan_listen
database:
  host: 127.0.0.1
  port: $UI_PG_PORT
  name: quader
  username_file: "$secrets/db-user"
  password_file: "$secrets/db-pass"
  ssl_mode: verify-full
  ssl_root_cert_file: "$certs/ca.crt"
  preflight:
    allow_superuser: true
valkey:
  mode: standalone
  address: 127.0.0.1:$UI_VALKEY_PORT
  password_file: "$secrets/valkey-pass"
  tls:
    ca_file: "$certs/ca.crt"
storage:
  connections:
    - bucket: quader-blobs
      endpoint: https://127.0.0.1:$UI_S3_PORT
      region: us-east-1
      force_path_style: true
      ca_file: "$certs/ca.crt"
      credentials_source: static
      access_key_id_file: "$secrets/s3-id"
      secret_access_key_file: "$secrets/s3-secret"
  spool:
    dir: "$state/spool"
secrets:
  encryption_key_files: ["$secrets/keys/dev.key"]
  active_key_id: dev
auth:
  # The profile's sign-in provider (test/devidp, https://localhost:<UI_IDP_PORT>, 19090 in instance 1, started by serve.sh): it signs in sam, priya,
  # ada or noor without a password, the kinds of user of the renders (docs/developer/web-ui.md#renders-of-every-screen).
  oidc:
    providers:
      ui-dev:
        display_name: UI development users
        issuer: https://localhost:$UI_IDP_PORT
        client_id: quader-ui-dev
        client_secret_file: "$secrets/idp-client-secret"
  # The sign-in screen offers the provider and the break-glass form instead of leaving for the provider at once.
  login_page:
    auto_redirect: false
  # A session through the provider lasts 8 h idle and 24 h absolute instead of 1 h and 12 h. make ui-renders signs its kinds of
  # user in once, in its setup, and the auditor's screens come last: in a run longer than an hour its session had ended unused, and
  # every auditor screen answered 401 (Test and Release 45378983191 and 45383627038, 2026-11-24).
  sessions:
    idle_timeout: 8h
    absolute_timeout: 24h
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
$images
$malware
signatures:
  trust_root:
    # Nor does it refresh the public Sigstore root from tuf-repo-cdn.sigstore.dev (R-SIG-017), which nine requests at every start did:
    # the profile's root is the snapshot the release embeds (internal/signatures/shipped/data). The section on Settings > Signatures
    # shows it as the snapshot of the release; the walk answers a refreshed root, with and without a failure, in the browser (Q-UI-1047).
    refresh: false
maintenance:
  tasks:
    # The image scanning's database: about 142 MB from mirror.gcr.io through upstream/scanner-trivy the first time, then a check of
    # the tag's document every sixth hour (R-VULN-044); scanners.sh starts it once at every start. Off with UI_SCANNERS=0, where
    # "Fetch the image database now" on Admin area > Advisory sources runs it once.
    $trivy_db
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
