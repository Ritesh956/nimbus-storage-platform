DROP TABLE share_link_files;
-- Folder/bundle shares can't be represented in the old single-file schema.
DELETE FROM share_links WHERE file_id IS NULL;
ALTER TABLE share_links
    DROP CONSTRAINT ck_share_links_single_scope,
    ALTER COLUMN file_id SET NOT NULL,
    DROP COLUMN folder_id,
    DROP COLUMN org_id;
