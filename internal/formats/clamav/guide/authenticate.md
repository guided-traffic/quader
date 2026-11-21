At the level **Public**, `freshclam` needs no credential: the line of the snippet is all.

At a lower level a ClamAV installation authenticates in one of two ways.

### With a token

`freshclam` sends the user name and the password of its address as a Basic credential. Use a token of a service account made for this purpose, with no grant beyond reading this repository, as the password, and any user name; the snippet above writes it so for a repository that is not public. For example:
{{if not .Anonymous}}
```ini
PrivateMirror https://clamav:${QUADER_TOKEN}@quader.example.com/api/clamav/upstream/clamav
```
{{end}}
`freshclam` writes the whole address, the token with it, into its log at the level `INFO`, and ClamAV does not document this way.

### With a client certificate

`freshclam` 1.2 and later presents a client certificate on every request when two environment variables name it; `freshclam.conf` has no option for it. Quader admits a certificate only on an endpoint your operator sets up for client certificates, with the certificate authorities whose certificates it admits, and only at the levels **Internal** and **Public**: at **Private** a certificate reads nothing. Nothing of the certificate is written into `freshclam`'s log.
{{with .CertificateURLs}}
Point `freshclam` at that endpoint and give it the certificate, its key and the authority of the endpoint's own certificate in its environment, for the systemd unit in a drop-in:

```ini
# /etc/clamav/freshclam.conf (example)
PrivateMirror {{index . 0}}

# /etc/systemd/system/clamav-freshclam.service.d/quader.conf (the unit's name is the distribution's; example)
[Service]
Environment=FRESHCLAM_CLIENT_CERT=/etc/clamav/tls/client.crt
Environment=FRESHCLAM_CLIENT_KEY=/etc/clamav/tls/client.key
Environment=CURL_CA_BUNDLE=/etc/pki/quader-ca.pem
```

Then `systemctl daemon-reload` and `systemctl restart clamav-freshclam`.
{{else}}
No endpoint of this installation admits client certificates for this repository now; your operator sets one up (the manual's page **Endpoints**).
{{end}}
The certificate must be issued by an authority the endpoint admits; your operator names it. The key file is readable by the user `freshclam` runs as and by nobody else. An encrypted key needs `FRESHCLAM_CLIENT_KEY_PASSWD` in the same environment, where it is as readable as the unit.
