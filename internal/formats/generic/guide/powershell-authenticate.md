{{if .Anonymous}}You are reading this guide without signing in. {{if .Repository}}`{{.Repository}}` is open to anonymous readers, so PowerShell needs no credential to download from it.{{else}}A repository that is open to anonymous readers needs no credential to download from.{{end}} Uploading and deleting always need a token: sign in to Quader to see how to create one and how PowerShell sends it.
{{else}}PowerShell signs in with a Quader token, never with your password.

- A **personal access token** is yours: create it under Profile, Personal tokens. Limit it to the repositories and actions it needs and give it an expiry: downloading needs read, uploading a new file deploy, replacing a file with other bytes overwrite as well, and deleting delete.
- A **service-account token** belongs to a service account, for CI jobs and other automation. An administrator creates the account and its tokens.

The snippets send the token as a bearer token from `$QuaderHeaders`, never as an argument of a program.

### Keep the token in {{.TokenEnvVar}}

Set `$env:{{.TokenEnvVar}}` in the session before the configure snippet, and keep the token out of scripts: PowerShell keeps the lines you type in its history file. In CI, store a service-account token with only the permissions the job needs as a masked, protected CI/CD variable named `{{.TokenEnvVar}}`, and run the configure snippet in the job's script.

### Checking the sign-in

`Invoke-WebRequest -Method Head -Uri "{{.RegistryURL}}" -Headers $QuaderHeaders` succeeds when the token may read {{if .Repository}}`{{.Repository}}`{{else}}the repository{{end}}. An unknown, malformed, expired or revoked token answers `401`, and so does an empty `$env:{{.TokenEnvVar}}`. A valid token that may not read the repository gets `404`, or `403` where an administrator chose not to hide the existence of repositories.
{{end}}
