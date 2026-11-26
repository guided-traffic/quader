{{if .Anonymous}}You are reading this guide without signing in. {{if .Repository}}`{{.Repository}}` is open to anonymous readers, so wget needs no credential to download from it.{{else}}A repository that is open to anonymous readers needs no credential to download from.{{end}} Uploading and deleting always need a token: sign in to Quader to see how to create one and how wget sends it.
{{else}}wget signs in with a Quader token, never with your password.

- A **personal access token** is yours: create it under Profile, Personal tokens. Limit it to the repositories and actions it needs and give it an expiry: downloading needs read, uploading a new file deploy, replacing a file with other bytes overwrite as well, and deleting delete.
- A **service-account token** belongs to a service account, for CI jobs and other automation. An administrator creates the account and its tokens.

`quader_wget` sends the token as a bearer token. `--header`, `--user` and `--password` would put it on wget's command line, and a `.netrc` would keep it in a file, so the snippets use neither.

### Keep the token in {{.TokenEnvVar}}

Set `{{.TokenEnvVar}}` in the environment wget runs in and keep the token out of scripts and command lines: a shell keeps the lines you type in its history. In CI, store a service-account token with only the permissions the job needs as a masked, protected CI/CD variable named `{{.TokenEnvVar}}`, and paste the function into the job's script.

### Checking the sign-in

`quader_wget --spider "{{.RegistryURL}}"` succeeds when the token may read {{if .Repository}}`{{.Repository}}`{{else}}the repository{{end}}. An unknown, malformed, expired or revoked token answers `401`, and so does an empty `{{.TokenEnvVar}}`. A valid token that may not read the repository gets `404`, or `403` where an administrator chose not to hide the existence of repositories.
{{end}}
