-- Share links grow two new scopes (post-Tier-3 session): a folder share
-- (public link to a folder's contents, navigable into subfolders) and a
-- multi-file bundle (one link for a hand-picked set of files, so sharing
-- five files doesn't mean sending five links). A link's scope is exactly
-- one of: file_id (the original single-file share), folder_id, or >=1
-- share_link_files rows — the two columns are CHECK-ed mutually exclusive
-- here; "bundle has rows, others don't" is enforced in sharing.Service
-- (CHECK can't span tables).
--
-- org_id is added (and backfilled from the shared file) so revoke
-- authorization is a single membership check against the link itself,
-- instead of re-deriving the org through whichever scope the link has.

ALTER TABLE share_links
    ADD COLUMN org_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    ADD COLUMN folder_id uuid REFERENCES folders(id) ON DELETE CASCADE,
    ALTER COLUMN file_id DROP NOT NULL,
    ADD CONSTRAINT ck_share_links_single_scope CHECK (file_id IS NULL OR folder_id IS NULL);

UPDATE share_links sl SET org_id = f.org_id FROM files f WHERE sl.file_id = f.id;
ALTER TABLE share_links ALTER COLUMN org_id SET NOT NULL;

CREATE TABLE share_link_files (
    share_id uuid NOT NULL REFERENCES share_links(id) ON DELETE CASCADE,
    file_id  uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    PRIMARY KEY (share_id, file_id)
);
