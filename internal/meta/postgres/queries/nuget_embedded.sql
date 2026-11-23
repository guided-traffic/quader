-- The embedded files of a NuGet version (formats/nuget.md R-NUGET-024, Q-NUGET-135): the statement behind
-- internal/formatdata/nuget/embedded.go. The files are read out of the stored .nupkg when a client asks for them; what the
-- .nuspec names is in nuget_version.metadata, so a version that embeds none is answered from this statement alone.

-- name: NuGetEmbeddedNames :many
-- The package paths the .nuspec of a live version names for its icon, its license file and its README, '' for each it names
-- none: the icon element, the license element of type file, the readme element. No row: the repository holds no live
-- version of the number.
SELECT COALESCE(btrim(nv.metadata ->> 'icon'), '')::text AS icon,
       (CASE WHEN nv.metadata #>> '{license,type}' = 'file'
             THEN COALESCE(btrim(nv.metadata #>> '{license,value}'), '')
             ELSE '' END)::text AS license,
       COALESCE(btrim(nv.metadata ->> 'readme'), '')::text AS readme
  FROM packages p
  JOIN package_versions pv ON pv.package_id = p.id
  JOIN nuget_version nv ON nv.version_id = pv.id
 WHERE p.repo_id = sqlc.arg(repository_id)::bigint
   AND p.name = sqlc.arg(lower_id)::text
   AND pv.version = sqlc.arg(lower_version)::text
   AND pv.trashed_at IS NULL;
