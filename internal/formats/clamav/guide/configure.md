Put the line of the snippet into `freshclam.conf` (`/etc/clamav/freshclam.conf` with the packages of most distributions). `PrivateMirror` replaces ClamAV's own network, its DNS record and its patch files; `DatabaseMirror` lines may stay, `PrivateMirror` wins. `ScriptedUpdates`, `DNSDatabaseInfo` and `Checks` need no change. Quader serves no patch files, so name Quader in `PrivateMirror` only, never in `DatabaseMirror`.

Write the address as the snippet does: without a trailing slash, which `freshclam` adds itself before a file name, and with its scheme, since `freshclam` reads an address without one as `http://`.

### The certificate

Where the certificate of Quader is issued by an internal certificate authority, `freshclam` must trust it; `freshclam.conf` has no option for that. On Linux, set the environment variable `CURL_CA_BUNDLE` to the file of the authority for `freshclam` (for the systemd unit, a drop-in with `Environment=CURL_CA_BUNDLE=/etc/pki/quader-ca.pem`), or add the authority to the system store. On macOS and Windows, add it to the system store.

### Without freshclam

An installation that runs no `freshclam` (a `clamd` fed by a script, a host that takes a copy) downloads the three databases from fixed addresses, which always serve the version Quader's scanners use, into its database directory and reloads `clamd`:
{{if .RequiresAuth}}
```sh
# example; the directory is the distribution's. curl reads the header from standard input, so no log and no process list shows it.
cd /var/lib/clamav
for db in main daily bytecode; do
  curl -fsS --config - -o "$db.cvd.part" "{{.RegistryURL}}$db.cvd" <<EOF && mv "$db.cvd.part" "$db.cvd"
header = "Authorization: Bearer ${QUADER_TOKEN}"
EOF
done
clamdscan --reload
```
{{else}}
```sh
# example; the directory is the distribution's
cd /var/lib/clamav
for db in main daily bytecode; do
  curl -fsS -o "$db.cvd.part" "{{.RegistryURL}}$db.cvd" && mv "$db.cvd.part" "$db.cvd"
done
clamdscan --reload
```
{{end}}{{with .CertificateURLs}}
With a client certificate instead (see "Sign freshclam in"), on the endpoint that admits it:

```sh
# example; the files are the installation's
cd /var/lib/clamav
for db in main daily bytecode; do
  curl -fsS --cert /etc/clamav/tls/client.crt --key /etc/clamav/tls/client.key --cacert /etc/pki/quader-ca.pem \
    -o "$db.cvd.part" "{{index . 0}}/$db.cvd" && mv "$db.cvd.part" "$db.cvd"
done
clamdscan --reload
```
{{end}}
Each run fetches the whole files, about 110 MiB; `clamd` verifies each signed `.cvd` when it loads it. A ClamAV that enforces FIPS limits also needs each `<db>-<version>.cvd.sign` beside it, whose name carries the version the repository's page shows; the plain script leaves it out.
