ALTER TABLE uploads
    DROP CONSTRAINT uploads_file_id_fkey,
    ADD CONSTRAINT uploads_file_id_fkey FOREIGN KEY (file_id) REFERENCES files(id),
    DROP CONSTRAINT uploads_version_id_fkey,
    ADD CONSTRAINT uploads_version_id_fkey FOREIGN KEY (version_id) REFERENCES file_versions(id),
    DROP CONSTRAINT uploads_target_file_id_fkey,
    ADD CONSTRAINT uploads_target_file_id_fkey FOREIGN KEY (target_file_id) REFERENCES files(id);
