| What `freshclam` reports | Why | What to do |
|---|---|---|
| `404` for every database | **ClamAV definition updates** is switched off, or the level of the repository hides it from this client (a client certificate at **Private**) | Ask an administrator to switch it on, or send a token (see "Sign freshclam in") |
| `401` | The repository is not public and the address carries no token, or one that is not valid | Put a valid token into the address |
| `503` with a time to wait | A request figure of the download limits refused the request | Nothing: `freshclam` tries again at its next check |
| A curl error such as `SSL certificate problem` or `alert certificate required` on the endpoint of the client certificates | No certificate, an expired one, or one of an authority the endpoint does not admit: the endpoint refuses the connection before any request | Check `FRESHCLAM_CLIENT_CERT` and `FRESHCLAM_CLIENT_KEY` and the certificate's dates, and ask your operator which authority the endpoint admits; `freshclam` tries again at its next check |
| `Can't verify database integrity` | A file changed on its way | Compare the SHA-256 of the file with the one on the repository's page |
| It waits without asking | `freshclam.dat` holds a cool-down after a refusal of another server; Quader never answers `403` or `429`, which cause one | Wait until the cool-down ends, or remove `freshclam.dat` |
