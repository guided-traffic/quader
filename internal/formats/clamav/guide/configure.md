Put the line of the snippet into `freshclam.conf` (`/etc/clamav/freshclam.conf` with the packages of most distributions). `PrivateMirror` replaces ClamAV's own network, its DNS record and its patch files; `DatabaseMirror` lines may stay, `PrivateMirror` wins. `ScriptedUpdates`, `DNSDatabaseInfo` and `Checks` need no change. Quader serves no patch files, so name Quader in `PrivateMirror` only, never in `DatabaseMirror`.

The address is `{{.RegistryURL}}` without its trailing slash, with the scheme written out: `freshclam` reads an address without a scheme as `http://`.

### The certificate

Where the certificate of Quader is issued by an internal certificate authority, `freshclam` must trust it; `freshclam.conf` has no option for that. On Linux, set the environment variable `CURL_CA_BUNDLE` to the file of the authority for `freshclam` (for the systemd unit, a drop-in with `Environment=CURL_CA_BUNDLE=/etc/pki/quader-ca.pem`), or add the authority to the system store. On macOS and Windows, add it to the system store.
