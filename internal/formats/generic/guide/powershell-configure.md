Paste the snippet into the PowerShell session, or the script, that runs `Invoke-WebRequest`; it works in PowerShell 7 and in Windows PowerShell 5.1. It fills the variable `$QuaderHeaders`, which every other snippet passes as `-Headers`{{if not .Anonymous}}, with the header `Authorization: Bearer` and the token from the environment variable `{{.TokenEnvVar}}`.

PowerShell reads the variable inside its own process, so the token is no argument of any program: other users of the machine do not see it in the process list. The header lives in the session; a new session or a script runs the snippet again{{end}}.
{{if .Anonymous}}
You are not signed in, so the variable holds no header and the commands send no credential. They work for repositories that are open to anonymous readers.
{{end}}
### Addresses

The address of {{if .Repository}}`{{.Repository}}`{{else}}the repository{{end}} is `{{.RegistryURL}}`; append the path of a file to it, with a space written as `%20`. The page of a file in Quader offers the command with the address and the name filled in, behind **Download**.

In Windows PowerShell 5.1, `curl` and `wget` are other names of `Invoke-WebRequest`, not the programs of the same name: the snippets of those clients need `curl.exe` or `wget.exe` there.
