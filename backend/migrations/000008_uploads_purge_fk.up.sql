-- Bug found by the GC smoke suite (backlog #10 session): uploads.file_id /
-- version_id / target_file_id referenced files/file_versions with no ON
-- DELETE action, so purging any file that came from an upload (i.e. every
-- file — CreateWithVersion is the only creation path) failed with an FK
-- violation. The completed-upload row is session bookkeeping (idempotency
-- replay, audit), not a reference that should pin a purged file's row
-- forever — purge nulls it out instead.
ALTER TABLE uploads
    DROP CONSTRAINT uploads_file_id_fkey,
    ADD CONSTRAINT uploads_file_id_fkey FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE SET NULL,
    DROP CONSTRAINT uploads_version_id_fkey,
    ADD CONSTRAINT uploads_version_id_fkey FOREIGN KEY (version_id) REFERENCES file_versions(id) ON DELETE SET NULL,
    DROP CONSTRAINT uploads_target_file_id_fkey,
    ADD CONSTRAINT uploads_target_file_id_fkey FOREIGN KEY (target_file_id) REFERENCES files(id) ON DELETE SET NULL;
