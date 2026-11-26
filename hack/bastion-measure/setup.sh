#!/usr/bin/env bash
# Prepares the measurement of a client's command through a Bastion (W34-E4): on the stores of the worktree's own instance of the
# UI development profile (make ui-dev-up), it writes the certificates, the main installation's and the Bastion's configuration,
# builds quader, migrates, registers the Bastion and writes an administrator's service account token. Nothing here enters the
# product; the state lives in <ui-dev state>/e4. Run it again after make ui-dev-down PURGE=1.
#
#   hack/bastion-measure/setup.sh
#
# Then hack/bastion-measure/run.sh main|bastion starts the processes (README.md).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"
# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root"
# shellcheck source=hack/bastion-measure/env.sh
. hack/bastion-measure/env.sh

[ -f "$UI_STATE/certs/ca.key" ] || { echo "run make ui-dev-up first" >&2; exit 1; }
mkdir -p "$E4" "$E4/certs" "$E4/logs" "$E4/cache"

# One certificate for both processes and the stub, signed by the profile's CA: the Bastion's host, which the client containers
# reach through the host gateway, and localhost, where the Bastion reaches the main installation.
if [ ! -f "$E4/certs/e4.crt" ]; then
  openssl req -new -newkey rsa:2048 -nodes -keyout "$E4/certs/e4.key" -subj "/CN=$E4_HOST" -out "$E4/certs/e4.csr" 2>/dev/null
  printf 'subjectAltName=DNS:%s,DNS:%s,DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' "$E4_HOST" "$E4_STUB_HOST" > "$E4/certs/ext.cnf"
  openssl x509 -req -in "$E4/certs/e4.csr" -CA "$UI_STATE/certs/ca.crt" -CAkey "$UI_STATE/certs/ca.key" -CAcreateserial \
    -days 30 -sha256 -extfile "$E4/certs/ext.cnf" -out "$E4/certs/e4.crt" 2>/dev/null
fi
cp "$UI_STATE/certs/ca.crt" "$E4/certs/ca.crt"

s="$UI_STATE/secrets"
c="$UI_STATE/certs"
cat > "$E4/main.yaml" <<YAML
# Written by hack/bastion-measure/setup.sh: the main installation of the measurement, role all, on the profile's stores.
server:
  listen: 127.0.0.1:$E4_MAIN_PORT
  ops_listen: 127.0.0.1:$E4_MAIN_OPS_PORT
  base_url: https://localhost:$E4_MAIN_PORT
  additional_base_urls: ["https://$E4_HOST:$E4_BASTION_PORT"]
  tls:
    certificates:
      - cert_file: "$E4/certs/e4.crt"
        key_file: "$E4/certs/e4.key"
  internal_tls:
    ca_file: "$c/ca.crt"
database:
  host: 127.0.0.1
  port: $UI_PG_PORT
  name: quader
  username_file: "$s/db-user"
  password_file: "$s/db-pass"
  ssl_mode: verify-full
  ssl_root_cert_file: "$c/ca.crt"
  preflight:
    allow_superuser: true
valkey:
  mode: standalone
  address: 127.0.0.1:$UI_VALKEY_PORT
  password_file: "$s/valkey-pass"
  tls:
    ca_file: "$c/ca.crt"
storage:
  connections:
    - bucket: quader-blobs
      endpoint: https://127.0.0.1:$UI_S3_PORT
      region: us-east-1
      force_path_style: true
      ca_file: "$c/ca.crt"
      credentials_source: static
      access_key_id_file: "$s/s3-id"
      secret_access_key_file: "$s/s3-secret"
  spool:
    dir: "$E4/spool"
secrets:
  encryption_key_files: ["$s/keys/dev.key"]
  active_key_id: dev
auth:
  break_glass:
    password_file: "$s/break-glass-password"
api:
  rate_limits:
    # The seed writes thousands of packages as one administrator; the measured commands go through the Bastion.
    read: {requests_per_second: 2000, burst: 8000}
    write: {requests_per_second: 1000, burst: 4000}
vulnerabilities:
  sources:
    osv: {mode: "off"}
    kev: {mode: "off"}
signatures:
  trust_root:
    refresh: false
maintenance:
  tasks:
    vuln-sync-trivy-db: {enabled: false, schedule: "23 */6 * * *"}
YAML

CGO_ENABLED=0 go build -trimpath -o "$E4/quader" ./cmd/quader
CGO_ENABLED=0 go build -trimpath -o "$E4/e4" ./tools/bastionmeasure
"$E4/quader" migrate up --config "$E4/main.yaml"

if [ ! -s "$E4/bastion-token" ]; then
  "$E4/quader" admin bastion create "$E4_REG" --description "W34-E4 measurement" --config "$E4/main.yaml" || true
  (umask 077 && "$E4/quader" admin bastion token create "$E4_REG" --ttl 720h --config "$E4/main.yaml" > "$E4/bastion-token")
fi
if [ ! -s "$E4/admin-token" ]; then
  "$E4/quader" admin service-account create e4-admin --admin --description "W34-E4 measurement" --config "$E4/main.yaml" || true
  (umask 077 && echo '{"inherit":true,"admin":true}' |
    "$E4/quader" admin token create --service-account e4-admin --replace --ttl 720h --scope-file /dev/stdin --config "$E4/main.yaml" > "$E4/admin-token")
fi
echo "setup done: $E4"
