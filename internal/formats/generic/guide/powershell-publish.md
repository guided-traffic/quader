{{if not .Anonymous}}{{if or .PublishTarget (not .Repository)}}`Invoke-WebRequest -Method Put -InFile <file> -Uri "{{.RegistryURL}}<path>" -Headers $QuaderHeaders` uploads a file to the path with `PUT`. Quader answers `201` with the description of the stored file, its digests among them, and `200` when the path holds these bytes already. The folders on the way arise with the file. The page of a folder in Quader offers the command with the folder's address filled in, beside **Upload files**.

Without `-ContentType`, Quader stores the type the name of the file says for Markdown, plain text and logs, JSON, YAML, TOML and CSV, and serves any other file as `application/octet-stream`; the type of a form, which PowerShell may send for a body it was given no type for, is never stored for a file. To store a type, add `-ContentType <type>`.

With `-Headers ($QuaderHeaders + @{ 'If-None-Match' = '*' })` an upload to a path that exists answers `412`: a release is uploaded once, and a second run of the job does not replace it.

A file holds at most 10 GiB, unless an administrator set another limit; a larger upload answers `413` `payload_too_large`.
{{else}}You cannot upload to `{{.Repository}}`: you do not hold the deploy permission on it. Ask whoever manages the repository for the permission, or upload to another repository.
{{end}}{{end}}
