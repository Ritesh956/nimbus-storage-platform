-- Supports re-uploading a new version of an existing file. Not in the
-- original docs/06-api-design.md contract (POST /v1/uploads only described
-- creating a brand-new file) — the Day 6 roadmap deliverable explicitly
-- requires "upload v1, re-upload as v2", which needs this. Flagged rather
-- than silently patched, per this project's own rule.
ALTER TABLE uploads ADD COLUMN target_file_id uuid REFERENCES files(id);
