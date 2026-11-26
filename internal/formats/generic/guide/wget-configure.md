Paste the snippet into the shell, or into the CI script, that runs wget; the snippets are written for a POSIX shell such as sh, bash or zsh. It defines the shell function `quader_wget`, which runs wget with the options and the address you give it{{if not .Anonymous}} and adds the header `Authorization: Bearer` with the token from the environment variable `{{.TokenEnvVar}}`.

The function reads the variable each time it runs and hands the header to wget as its configuration file, read from standard input (`--config=/dev/stdin`). The token therefore appears neither on wget's command line, where other users of the machine could read it in the process list, nor in a shell trace (`set -x`), and no file holds it. The configuration replaces your own `.wgetrc` for these calls{{end}}.
{{if .Anonymous}}
You are not signed in, so the function sends no credential. It works for repositories that are open to anonymous readers.
{{end}}
### Addresses

The address of {{if .Repository}}`{{.Repository}}`{{else}}the repository{{end}} is `{{.RegistryURL}}`; append the path of a file to it, with a space written as `%20`. The page of a file in Quader offers the command with the address filled in, behind **Download**.
