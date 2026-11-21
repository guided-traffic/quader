At the level **Public**, `freshclam` needs no credential: the line of the snippet is all.

At a lower level, `freshclam` sends the user name and the password of its address as a Basic credential. Use a token of a service account made for this purpose, with no grant beyond reading this repository, as the password, and any user name; the snippet above writes it so for a repository that is not public. For example:
{{if not .Anonymous}}
```ini
PrivateMirror https://clamav:${QUADER_TOKEN}@quader.example.com/api/clamav/upstream/clamav
```
{{end}}
`freshclam` writes the whole address, the token with it, into its log at the level `INFO`, and ClamAV does not document this way. A client certificate, which `freshclam` 1.2 and later can present, authenticates nothing here: Quader maps no client certificate to a principal on this address.
