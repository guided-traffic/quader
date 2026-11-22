-- quader:phase expand
--
-- The delivery by signed links of the object store (storage.md R-STOR-014, R-STOR-070 to R-STOR-074; ADR-242): the installation's
-- switch, off by default, and its details, one row as the malware scan's switch (Q-STOR-280). Every replica reads it from a snapshot
-- that the change area download_link_settings refreshes, so a download costs no query for it (R-ARCH-027). The store
-- internal/downloadlinks is the one door to the table.

CREATE TABLE download_link_settings (
    id                 boolean     PRIMARY KEY DEFAULT true CHECK (id),
    enabled            boolean     NOT NULL DEFAULT false,
    min_size           bigint      NOT NULL DEFAULT 1048576 CHECK (min_size BETWEEN 0 AND 1099511627776),
    link_life_seconds  integer     NOT NULL DEFAULT 60 CHECK (link_life_seconds BETWEEN 10 AND 3600),
    -- connection id -> https origin; a connection that is absent links to its own endpoint (Q-STOR-281)
    public_addresses   jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(public_addresses) = 'object'),
    version            bigint      NOT NULL DEFAULT 1,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    updated_by         uuid        NULL REFERENCES principal (id) ON DELETE SET NULL
);
INSERT INTO download_link_settings (id) VALUES (true);

INSERT INTO cluster_epoch (area) VALUES ('download_link_settings');
