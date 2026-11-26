{{if not .Anonymous}}### Replacing a file

Upload the new bytes to the same path. The upload answers `201` like a new file and needs the overwrite permission besides deploy; without it Quader answers `403` and keeps the stored file.

### Deleting

`Invoke-WebRequest -Method Delete -Uri "{{.RegistryURL}}<path>" -Headers $QuaderHeaders` deletes a file; Quader answers `204`. The address of a folder deletes the folder with everything below it, which needs the delete permission on every item in it. A folder of more than 10,000 items is deleted by a background job: Quader answers `202`, and the files disappear as the job runs. A delete is final: Quader has no trash can yet.
{{end}}
