#!/usr/bin/env bash
# The UI development profile's Bastion (docs/developer/web-ui.md#the-profiles-bastion): one replica of the role bastion beside the
# instance's quader, registered at it as ui-dev with its web interface and its documentation switched on, serving the instance's public
# catalogue at https://localhost:<UI_BASTION_PORT> (19091 in instance 1, plus (n - 1) * 100 in instance n). Needs a running backend
# (make ui-dev-backend, or what make ui-renders starts); runs in the foreground and ends with Ctrl-C or a TERM.
#
# Profile-side only: the script builds the Bastion's application into the worktree's embed tree (npm run build:embed's
# bastion-gui part) and a binary of its own at <state>/bin/quader-bastion; nothing of it reaches an image, a chart or the
# configuration of a release.
#
# The registration goes through the management API as the break-glass account at every start: the registration ui-dev (kept when it
# exists), the switches gui and docs on, a token when <state>/bastion/token is missing. Once the replica's handshake has reported its
# base URL, the public catalogue's builder is started once (POST /api/v1/system/tasks/bastion-catalogue/run); its triggers and the
# profile's schedule (config.sh) follow every change after that. A Bastion serves only what it distributes (authorization.md R-AUTHZ-043):
# the script allows no project and chooses no repository; the renders' setup does after its seed (seedDistribution of
# web/e2e/renders/bastions.ts), and until then the Bastion's web interface shows an empty start.
#
#   UI_BASTION_BUILD=0   keeps the application and the binary of the last start (a start in seconds)
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root"
state="$UI_STATE"
dir="$state/bastion"
name=ui-dev
base="https://localhost:$UI_BASTION_PORT"
backend="https://127.0.0.1:$UI_BACKEND_PORT"
origin="https://localhost:$UI_WEB_PORT"
cacert="$state/certs/ca.crt"

if (exec 3<>"/dev/tcp/127.0.0.1/$UI_BASTION_PORT") 2>/dev/null; then
  echo "bastion: the Bastion of instance $UI_INSTANCE answers on port $UI_BASTION_PORT already; stop it first" >&2
  exit 1
fi
if ! curl -sf --cacert "$cacert" -o /dev/null "https://127.0.0.1:$UI_OPS_PORT/readyz"; then
  echo "bastion: quader of instance $UI_INSTANCE is not ready on https://127.0.0.1:$UI_OPS_PORT; start it first (make ui-dev-backend)" >&2
  exit 1
fi
mkdir -p "$dir/cache" "$state/bin"
bin="$state/bin/quader-bastion"

if [ "${UI_BASTION_BUILD:-1}" != 0 ] || [ ! -x "$bin" ]; then
  # The Bastion's bundle into internal/bastion/gui/dist/<locale> (git ignores it), then the binary that embeds it.
  (cd web && node scripts/build-embed.mjs bastion-gui)
  CGO_ENABLED=0 go build -trimpath -o "$bin" ./cmd/quader
fi

# The management API as the break-glass account: a session in a cookie jar of this run, its CSRF token from the jar.
jar="$(mktemp)"
trap 'rm -f "$jar"' EXIT
api() { # api <method> <path> [<json body> [<content type>]]: prints the body, fails on a status of 400 or more but 409
  local out code
  out="$(mktemp)"
  # If-Match: * overwrites the switches deliberately (a PATCH requires the header, R-API-012).
  code="$(curl -s --cacert "$cacert" -b "$jar" -c "$jar" -o "$out" -w '%{http_code}' -X "$1" \
    -H "Origin: $origin" -H "X-CSRF-Token: $(awk '$6 == "__Host-qdr_csrf" { print $7 }' "$jar")" -H 'If-Match: *' \
    ${3:+-H "Content-Type: ${4:-application/json}" --data "$3"} "$backend$2")"
  if [ "$code" -ge 400 ] && [ "$code" != 409 ]; then
    echo "bastion: $1 $2 answered $code: $(head -c 400 "$out")" >&2
    rm -f "$out"
    return 1
  fi
  cat "$out"
  rm -f "$out"
}
password="$(cat "$state/secrets/break-glass-password")"
api POST /auth/local/login "{\"username\":\"admin\",\"password\":\"$password\"}" > /dev/null
api POST /api/v1/bastions "{\"name\":\"$name\",\"description\":\"The UI development profile's Bastion (deploy/ui-dev/bastion.sh)\"}" > /dev/null
# The class gui of the flood protection at the read class of the profile's quader (config.sh: 200 a second, burst 800) instead of the
# preset's 20 and 100: make ui-renders opens three pages at once from 127.0.0.1, and the preset answered 429 to the state, the legal
# pages and a script of the bundle (2026-11-26). One person in one browser stays far below the preset; the renders are not one person.
api PATCH "/api/v1/bastions/$name" \
  '{"switches":{"gui":true,"docs":true},"limits":{"flood":{"gui":{"requestsPerSecond":200,"burst":800}}}}' application/merge-patch+json > /dev/null
if [ ! -s "$dir/token" ]; then
  token="$(api POST "/api/v1/bastions/$name/tokens" '{"description":"deploy/ui-dev/bastion.sh"}' | sed -n 's/.*"token":"\(qdr_bst_[^"]*\)".*/\1/p')"
  [ -n "$token" ] || { echo "bastion: the token of $name was not in the answer" >&2; exit 1; }
  (umask 077 && printf '%s\n' "$token" > "$dir/token")
fi

# The replica's configuration: the instance's certificate on both listeners (localhost, 127.0.0.1), the main installation's listener
# with the profile's CA, a volume cache of 1 GiB with a catalogue share of 128 MiB (the dev profile's Bastions'), quick polls.
cat > "$dir/quader.yaml" <<YAML
# Written by deploy/ui-dev/bastion.sh at every start, so edit the script, not this file.
server:
  listen: 127.0.0.1:$UI_BASTION_PORT
  ops_listen: 127.0.0.1:$UI_BASTION_OPS_PORT
  base_url: $base
  tls:
    certificates:
      - cert_file: "$state/certs/server.crt"
        key_file: "$state/certs/server.key"
bastion:
  main:
    url: $backend
    token_file: "$dir/token"
    ca_file: "$cacert"
    handshake_interval: 10s
  cache:
    max_bytes: 1GiB
    max_object_bytes: 256MiB
    documents_max_bytes: 128MiB
    spool_max_bytes: 256MiB
    volume:
      dir: "$dir/cache"
  gui:
    poll_interval: 2s
    max_bytes: 128MiB
ui:
  # The Bastion's own imprint and privacy notice (R-BAS-100), shorter than the main installation's: the imprint in English only and the
  # privacy notice in German only, so each language's pages show one page in its words and one with the note and the other's text.
  legal:
    imprint:
      en: |
        Example Operations GmbH\\
        Musterstraße 1\\
        12345 Berlin, Germany

        Contact: <legal@example.com>, +49 30 1234567
    privacy:
      de: |
        ## Verantwortlicher

        Example Operations GmbH, Musterstraße 1, 12345 Berlin, <datenschutz@example.com>

        ## Welche Daten dieser Dienst verarbeitet

        Die Adresse jeder Anfrage und ihre Zeit, 7 Tage lang, um Missbrauch abzuwehren. Dieser Dienst setzt kein Cookie.
YAML

"$bin" serve --role bastion --config "$dir/quader.yaml" &
pid=$!
trap 'kill -TERM "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; rm -f "$jar"' EXIT INT TERM

# The builder starts once the handshake has reported the base URL (R-BAS-104): until then it stops for the registration.
for _ in $(seq 1 60); do
  if api GET "/api/v1/bastions/$name" | grep -q "\"baseUrl\":\"$base\""; then
    api POST /api/v1/system/tasks/bastion-catalogue/run '{}' > /dev/null
    break
  fi
  kill -0 "$pid" 2>/dev/null || { echo "bastion: the replica ended; its log is above" >&2; exit 1; }
  sleep 1
done

cat <<EOF

  Bastion:   $base/ui/   (registration $name of instance $UI_INSTANCE; the certificate is signed by $cacert)
  ops:       https://127.0.0.1:$UI_BASTION_OPS_PORT (readyz, metrics)

EOF
wait "$pid"
