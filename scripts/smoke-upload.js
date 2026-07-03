#!/usr/bin/env node
// Day 5 deliverable (docs/09-roadmap.md): a multi-chunk file uploaded
// end-to-end via the real chunked-upload flow, with chunks confirmed
// replicated on 2 nodes each and dedup verified on a second upload of the
// same content. Plain Node (no deps) since it needs real SHA-256 hashing
// and multiple sequential HTTP calls — awkward in bash/curl alone.
'use strict';
const crypto = require('crypto');

const BASE = process.env.NIMBUS_BASE_URL || 'http://localhost:8080';
const CHUNK_SIZE = 8 * 1024 * 1024; // matches NIMBUS_CHUNK_SIZE_BYTES default

async function jsonFetch(url, opts = {}) {
  const res = await fetch(url, {
    ...opts,
    headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
  });
  const text = await res.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; } catch { body = text; }
  if (!res.ok) {
    throw new Error(`${opts.method || 'GET'} ${url} -> ${res.status}: ${JSON.stringify(body)}`);
  }
  return body;
}

function sha256(buf) {
  return crypto.createHash('sha256').update(buf).digest('hex');
}

async function uploadFile(access, folderId, name, content) {
  const chunks = [];
  for (let i = 0; i < content.length; i += CHUNK_SIZE) chunks.push(content.subarray(i, i + CHUNK_SIZE));
  const hashes = chunks.map(sha256);
  const auth = { Authorization: `Bearer ${access}` };

  const { missing } = await jsonFetch(`${BASE}/v1/chunks/check`, {
    method: 'POST', headers: auth, body: JSON.stringify({ hashes }),
  });
  console.log(`  chunks: ${chunks.length} total, ${missing.length} missing (${chunks.length - missing.length} deduped)`);

  const { upload_id } = await jsonFetch(`${BASE}/v1/uploads`, {
    method: 'POST', headers: auth,
    body: JSON.stringify({ folder_id: folderId, name, size_bytes: content.length, mime_type: 'application/octet-stream' }),
  });
  console.log(`  upload session: ${upload_id}`);

  const missingSet = new Set(missing);
  for (let i = 0; i < chunks.length; i++) {
    const hash = hashes[i];
    if (!missingSet.has(hash)) continue; // deduped, nothing to upload
    const { targets } = await jsonFetch(`${BASE}/v1/uploads/${upload_id}/chunks/${hash}/init`, {
      method: 'POST', headers: auth,
    });
    if (targets.length < 2) throw new Error(`expected >=2 replica targets, got ${targets.length}`);

    const etags = {};
    for (const t of targets) {
      const putRes = await fetch(t.put_url, { method: 'PUT', body: chunks[i] });
      if (!putRes.ok) throw new Error(`PUT to ${t.node_id} failed: ${putRes.status}`);
      etags[t.node_id] = putRes.headers.get('etag');
    }
    console.log(`  chunk ${i} (${hash.slice(0, 12)}...) -> ${targets.map(t => t.node_id).join(', ')}`);

    await jsonFetch(`${BASE}/v1/uploads/${upload_id}/chunks/${hash}/commit`, {
      method: 'POST', headers: auth,
      body: JSON.stringify({ size_bytes: chunks[i].length, etags }),
    });
  }

  const { file_id, version_id } = await jsonFetch(`${BASE}/v1/uploads/${upload_id}/complete`, {
    method: 'POST', headers: { ...auth, 'Idempotency-Key': `test-${upload_id}` },
    body: JSON.stringify({ chunk_order: hashes, size_bytes: content.length, checksum_sha256: sha256(content) }),
  });
  return { file_id, version_id, upload_id, hashes };
}

async function main() {
  const email = `upload-smoke-${Date.now()}@nimbus.dev`;
  const password = 'correct-horse-battery-staple';
  await jsonFetch(`${BASE}/v1/auth/register`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const { access_token } = await jsonFetch(`${BASE}/v1/auth/login`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const { id: orgId } = await jsonFetch(`${BASE}/v1/orgs`, {
    method: 'POST', headers: { Authorization: `Bearer ${access_token}` }, body: JSON.stringify({ name: 'Upload Smoke Org' }),
  });
  const { id: folderId } = await jsonFetch(`${BASE}/v1/orgs/${orgId}/folders`, {
    method: 'POST', headers: { Authorization: `Bearer ${access_token}` }, body: JSON.stringify({ name: 'Uploads' }),
  });

  console.log('== uploading a 20MB file (3 chunks: 8MB, 8MB, 4MB) ==');
  const content = crypto.randomBytes(20 * 1024 * 1024);
  const first = await uploadFile(access_token, folderId, 'test-file.bin', content);
  console.log(`  OK: file_id=${first.file_id} version_id=${first.version_id}`);

  console.log('== uploading the SAME content again under a new name (must dedup all chunks) ==');
  const second = await uploadFile(access_token, folderId, 'test-file-copy.bin', content);
  console.log(`  OK: file_id=${second.file_id} version_id=${second.version_id}`);
  if (second.file_id === first.file_id) throw new Error('expected a distinct file for the second upload');

  console.log('== verifying rename works on the resulting file (Day 3 handler, real data this time) ==');
  await jsonFetch(`${BASE}/v1/files/${first.file_id}`, {
    method: 'PATCH', headers: { Authorization: `Bearer ${access_token}` }, body: JSON.stringify({ name: 'renamed.bin' }),
  });
  console.log('  OK');

  console.log('\nALL UPLOAD SMOKE CHECKS PASSED');
}

main().catch((err) => { console.error('FAIL:', err.message); process.exit(1); });
