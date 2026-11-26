{{if not .Anonymous}}{{if or .PublishTarget (not .Repository)}}`quader_wget --method PUT --body-file <file> --output-document - "{{.RegistryURL}}<path>"` uploads a file to the path with `PUT` and prints Quader's answer, the description of the stored file with its digests; without `--output-document -` wget would save that answer as a file. Quader answers `201`, and `200` when the path holds these bytes already. The folders on the way arise with the file. The page of a folder in Quader offers the command with the folder's address filled in, beside **Upload files**.

wget sends the body of a `PUT` as `application/x-www-form-urlencoded` unless it is told otherwise. Quader does not store that type for a file: it stores the type the name says for Markdown, plain text and logs, JSON, YAML, TOML and CSV, and serves any other file as `application/octet-stream`. To store a type, add `--header "Content-Type: <type>"`.

With `--header "If-None-Match: *"` an upload to a path that exists answers `412`: a release is uploaded once, and a second run of the job does not replace it.

A file holds at most 10 GiB, unless an administrator set another limit; a larger upload answers `413` `payload_too_large`.
{{else}}You cannot upload to `{{.Repository}}`: you do not hold the deploy permission on it. Ask whoever manages the repository for the permission, or upload to another repository.
{{end}}{{end}}
