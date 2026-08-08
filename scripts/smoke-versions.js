#!/usr/bin/env node
// Day 6 deliverable (docs/09-roadmap.md): upload v1, download it, re-upload
// as v2 of the same file, restore to v1, verify via checksum throughout.
'use strict';
const crypto = require('crypto');

const BASE = process.env.NIMBUS_BASE_URL || 'http://localhost:8080';
const CHUNK_SIZE = 8 * 1024 * 1024;

async function jsonFetch(url, opts = {}) {
  const res = await fetch(url, { ...opts, headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) } });
  const text = await res.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; } catch { body = text; }
  if (!res.ok) throw new Error(`${opts.method || 'GET'} ${url} -> ${res.status}: ${JSON.stringify(body)}`);
  return body;
}

const sha256 = (buf) => crypto.createHash('sha256').update(buf).digest('hex');

async function upload(access, { folderId, fileId, name }, content) {
  const chunks = [];
  for (let i = 0; i < content.length; i += CHUNK_SIZE) chunks.push(content.subarray(i, i + CHUNK_SIZE));
  const hashes = chunks.map(sha256);
  const auth = { Authorization: `Bearer ${access}` };

  const initBody = fileId ? { file_id: fileId, size_bytes: content.length, mime_type: 'application/octet-stream' }
                          : { folder_id: folderId, name, size_bytes: content.length, mime_type: 'application/octet-stream' };
  const { upload_id } = await jsonFetch(`${BASE}/v1/uploads`, { method: 'POST', headers: auth, body: JSON.stringify(initBody) });

  // Upload-scoped, not global (audit §05: proof-of-possession) — needs
  // upload_id, so this runs after init now.
  const { missing } = await jsonFetch(`${BASE}/v1/uploads/${upload_id}/chunks/check`, { method: 'POST', headers: auth, body: JSON.stringify({ hashes }) });
  const missingSet = new Set(missing);

  for (let i = 0; i < chunks.length; i++) {
    const hash = hashes[i];
    if (!missingSet.has(hash)) continue;
    const { targets } = await jsonFetch(`${BASE}/v1/uploads/${upload_id}/chunks/${hash}/init`, { method: 'POST', headers: auth });
    const etags = {};
    for (const t of targets) {
      const putRes = await fetch(t.put_url, { method: 'PUT', body: chunks[i] });
      if (!putRes.ok) throw new Error(`PUT to ${t.node_id} failed: ${putRes.status}`);
      etags[t.node_id] = putRes.headers.get('etag');
    }
    await jsonFetch(`${BASE}/v1/uploads/${upload_id}/chunks/${hash}/commit`, { method: 'POST', headers: auth, body: JSON.stringify({ size_bytes: chunks[i].length, etags }) });
  }

  return jsonFetch(`${BASE}/v1/uploads/${upload_id}/complete`, {
    method: 'POST', headers: { ...auth, 'Idempotency-Key': `test-${upload_id}` },
    body: JSON.stringify({ chunk_order: hashes, size_bytes: content.length, checksum_sha256: sha256(content) }),
  });
}

async function download(access, fileId, versionId) {
  const auth = { Authorization: `Bearer ${access}` };
  const { chunks } = await jsonFetch(`${BASE}/v1/files/${fileId}/versions/${versionId}/download-plan`, { headers: auth });
  const parts = [];
  for (const c of chunks.sort((a, b) => a.sequence - b.sequence)) {
    let got = false;
    for (const url of c.targets) {
      const res = await fetch(url);
      if (res.ok) { parts.push(Buffer.from(await res.arrayBuffer())); got = true; break; }
    }
    if (!got) throw new Error(`could not download chunk ${c.sequence} from any of ${c.targets.length} targets`);
  }
  return Buffer.concat(parts);
}

async function main() {
  const email = `versions-smoke-${Date.now()}@nimbus.dev`;
  const password = 'correct-horse-battery-staple';
  await jsonFetch(`${BASE}/v1/auth/register`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const { access_token: access } = await jsonFetch(`${BASE}/v1/auth/login`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const auth = { Authorization: `Bearer ${access}` };
  const { id: orgId } = await jsonFetch(`${BASE}/v1/orgs`, { method: 'POST', headers: auth, body: JSON.stringify({ name: 'Versions Smoke Org' }) });
  const { id: folderId } = await jsonFetch(`${BASE}/v1/orgs/${orgId}/folders`, { method: 'POST', headers: auth, body: JSON.stringify({ name: 'Docs' }) });

  console.log('== uploading v1 (12MB) ==');
  const v1Content = crypto.randomBytes(12 * 1024 * 1024);
  const v1 = await upload(access, { folderId, name: 'report.bin' }, v1Content);
  console.log(`  file_id=${v1.file_id} version_id=${v1.version_id}`);

  console.log('== downloading v1 and verifying checksum ==');
  const downloaded1 = await download(access, v1.file_id, v1.version_id);
  if (sha256(downloaded1) !== sha256(v1Content)) throw new Error('v1 download checksum mismatch!');
  console.log(`  OK: ${downloaded1.length} bytes, checksum matches`);

  console.log('== re-uploading as v2 (18MB, different content) ==');
  const v2Content = crypto.randomBytes(18 * 1024 * 1024);
  const v2 = await upload(access, { fileId: v1.file_id }, v2Content);
  console.log(`  file_id=${v2.file_id} version_id=${v2.version_id}`);
  if (v2.file_id !== v1.file_id) throw new Error('expected re-upload to target the same file_id');

  console.log('== listing versions (expect 2) ==');
  const versions = await jsonFetch(`${BASE}/v1/files/${v1.file_id}/versions`, { headers: auth });
  if (versions.length !== 2) throw new Error(`expected 2 versions, got ${versions.length}`);
  console.log(`  OK: ${versions.length} versions`);

  console.log('== downloading v2 (should be the file\'s current content) and verifying ==');
  const downloaded2 = await download(access, v1.file_id, v2.version_id);
  if (sha256(downloaded2) !== sha256(v2Content)) throw new Error('v2 download checksum mismatch!');
  console.log(`  OK: ${downloaded2.length} bytes, checksum matches`);

  console.log('== restoring to v1 ==');
  const restored = await jsonFetch(`${BASE}/v1/files/${v1.file_id}/versions/${v1.version_id}/restore`, { method: 'POST', headers: auth });
  if (restored.latest_version_id !== v1.version_id) throw new Error('restore did not repoint latest_version_id to v1');
  console.log('  OK: latest_version_id repointed to v1');

  console.log('== downloading current (post-restore) content and verifying it is v1 again ==');
  const finalPlan = await jsonFetch(`${BASE}/v1/files/${v1.file_id}/versions/${restored.latest_version_id}/download-plan`, { headers: auth });
  const finalParts = [];
  for (const c of finalPlan.chunks.sort((a, b) => a.sequence - b.sequence)) {
    const res = await fetch(c.targets[0]);
    finalParts.push(Buffer.from(await res.arrayBuffer()));
  }
  const finalContent = Buffer.concat(finalParts);
  if (sha256(finalContent) !== sha256(v1Content)) throw new Error('post-restore content is not v1!');
  console.log('  OK: post-restore content matches v1 exactly');

  console.log('\nALL VERSION/DOWNLOAD SMOKE CHECKS PASSED');
}

main().catch((err) => { console.error('FAIL:', err.message); process.exit(1); });
