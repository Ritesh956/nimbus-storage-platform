#!/usr/bin/env node
// Day 12 deliverable (docs/09-roadmap.md, docs/07-distributed-architecture.md §5):
// proves an in-flight, multi-chunk upload survives a storage node dying
// *while the upload is happening* — not just at rest, which is all
// scripts/smoke-storage.sh (Day 4) proves.
//
// Named chaos-node-kill.js, not the .sh the design docs originally sketched:
// this needs real binary chunk PUTs plus precise control over exactly when
// `docker stop` fires relative to which chunk just committed — the same
// reason Days 5-9's chunked-upload/thumbnail scripts are Node, not bash
// (see smoke-upload.js's header comment). Docs updated to match.
'use strict';
const crypto = require('crypto');
const path = require('path');
const { execSync } = require('child_process');

const BASE = process.env.NIMBUS_BASE_URL || 'http://localhost:8080';
const CHUNK_SIZE = 8 * 1024 * 1024; // matches NIMBUS_CHUNK_SIZE_BYTES default
const COMPOSE_DIR = path.join(__dirname, '..', 'deploy');
const TARGET_NODE = process.argv[2] || 'node-2';
const TARGET_CONTAINER = `minio-${TARGET_NODE}`;
const KILL_AFTER_CHUNK = 2; // 0-indexed: kill right after this many chunks have committed

const results = [];
function record(label, ok, detail) {
  results.push({ label, ok, detail });
  console.log(`  ${ok ? 'OK' : 'FAIL'}: ${label}${detail ? ` (${detail})` : ''}`);
  return ok;
}

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)); }
function sha256(buf) { return crypto.createHash('sha256').update(buf).digest('hex'); }

async function jsonFetch(url, opts = {}) {
  const res = await fetch(url, { ...opts, headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) } });
  const text = await res.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; } catch { body = text; }
  if (!res.ok) throw new Error(`${opts.method || 'GET'} ${url} -> ${res.status}: ${JSON.stringify(body)}`);
  return body;
}

async function nodeStatus(auth, nodeId) {
  const nodes = await jsonFetch(`${BASE}/v1/admin/nodes`, { headers: auth });
  const n = nodes.find((x) => x.id === nodeId);
  return n ? n.status : 'missing';
}

// waitForStatus polls /v1/admin/nodes the same way scripts/smoke-storage.sh
// does — used here to deterministically confirm detection before continuing
// the upload, rather than racing the ~6s (3 x 2s probe) breaker window.
async function waitForStatus(auth, nodeId, want, timeoutSec) {
  const start = Date.now();
  for (;;) {
    const status = await nodeStatus(auth, nodeId);
    const elapsed = (Date.now() - start) / 1000;
    if (status === want) return elapsed;
    if (elapsed > timeoutSec) throw new Error(`timed out after ${timeoutSec}s waiting for ${nodeId} to become ${want} (last status: ${status})`);
    await sleep(1000);
  }
}

function dockerCompose(args) {
  execSync(`docker compose ${args}`, { cwd: COMPOSE_DIR, stdio: 'ignore' });
}

async function main() {
  const email = `chaos-smoke-${Date.now()}@nimbus.dev`;
  const password = 'correct-horse-battery-staple';
  await jsonFetch(`${BASE}/v1/auth/register`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const { access_token } = await jsonFetch(`${BASE}/v1/auth/login`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const auth = { Authorization: `Bearer ${access_token}` };
  const { id: orgId } = await jsonFetch(`${BASE}/v1/orgs`, { method: 'POST', headers: auth, body: JSON.stringify({ name: 'Chaos Smoke Org' }) });
  const { id: folderId } = await jsonFetch(`${BASE}/v1/orgs/${orgId}/folders`, { method: 'POST', headers: auth, body: JSON.stringify({ name: 'Chaos' }) });

  console.log(`== baseline: ${TARGET_NODE} should be healthy ==`);
  record('baseline node healthy', (await nodeStatus(auth, TARGET_NODE)) === 'healthy');

  console.log('== generating a ~52MB file (7 chunks: 6x8MB + 1x4MB) ==');
  const content = crypto.randomBytes(6 * CHUNK_SIZE + 4 * 1024 * 1024);
  const originalChecksum = sha256(content);
  const chunks = [];
  for (let i = 0; i < content.length; i += CHUNK_SIZE) chunks.push(content.subarray(i, i + CHUNK_SIZE));
  const hashes = chunks.map(sha256);
  console.log(`  ${chunks.length} chunks, checksum ${originalChecksum.slice(0, 12)}...`);

  const { upload_id } = await jsonFetch(`${BASE}/v1/uploads`, {
    method: 'POST', headers: auth,
    body: JSON.stringify({ folder_id: folderId, name: 'chaos-test-file.bin', size_bytes: content.length, mime_type: 'application/octet-stream' }),
  });
  console.log(`  upload session: ${upload_id}`);

  let killed = false;
  for (let i = 0; i < chunks.length; i++) {
    const hash = hashes[i];
    const { targets } = await jsonFetch(`${BASE}/v1/uploads/${upload_id}/chunks/${hash}/init`, { method: 'POST', headers: auth });

    if (killed) {
      record(`chunk ${i} placement avoids dead node ${TARGET_NODE}`, !targets.some((t) => t.node_id === TARGET_NODE));
    }

    const etags = {};
    for (const t of targets) {
      const putRes = await fetch(t.put_url, { method: 'PUT', body: chunks[i] });
      if (!putRes.ok) throw new Error(`PUT to ${t.node_id} failed: ${putRes.status}`);
      etags[t.node_id] = putRes.headers.get('etag');
    }
    console.log(`  chunk ${i} (${hash.slice(0, 12)}...) -> ${targets.map((t) => t.node_id).join(', ')}`);

    await jsonFetch(`${BASE}/v1/uploads/${upload_id}/chunks/${hash}/commit`, {
      method: 'POST', headers: auth, body: JSON.stringify({ size_bytes: chunks[i].length, etags }),
    });

    if (i === KILL_AFTER_CHUNK - 1 && !killed) {
      console.log(`== mid-upload: stopping ${TARGET_CONTAINER} after ${KILL_AFTER_CHUNK} of ${chunks.length} chunks committed ==`);
      dockerCompose(`stop ${TARGET_CONTAINER}`);
      killed = true;
      try {
        const elapsed = await waitForStatus(auth, TARGET_NODE, 'down', 15);
        record('down-detection (must be <=10s per NFR-3)', elapsed <= 10, `${elapsed.toFixed(1)}s`);
      } catch (err) {
        record('down-detection (must be <=10s per NFR-3)', false, err.message);
      }
    }
  }

  console.log('== completing upload (remaining chunks placed while a node was down) ==');
  const { file_id, version_id } = await jsonFetch(`${BASE}/v1/uploads/${upload_id}/complete`, {
    method: 'POST', headers: { ...auth, 'Idempotency-Key': `chaos-${upload_id}` },
    body: JSON.stringify({ chunk_order: hashes, size_bytes: content.length, checksum_sha256: originalChecksum }),
  });
  record('upload completes despite mid-flight node failure', Boolean(file_id && version_id));

  console.log('== downloading the completed file and verifying it byte-for-byte matches ==');
  const { chunks: plan } = await jsonFetch(`${BASE}/v1/files/${file_id}/versions/${version_id}/download-plan`, { headers: auth });
  plan.sort((a, b) => a.sequence - b.sequence);
  const parts = [];
  for (const c of plan) {
    let fetched = null;
    for (const url of c.targets) {
      const res = await fetch(url);
      if (res.ok) { fetched = Buffer.from(await res.arrayBuffer()); break; }
    }
    if (!fetched) throw new Error(`could not fetch chunk seq=${c.sequence} from any of its ${c.targets.length} replicas`);
    parts.push(fetched);
  }
  const downloaded = Buffer.concat(parts);
  record('downloaded content matches original checksum', sha256(downloaded) === originalChecksum, `${downloaded.length} bytes`);

  console.log(`== restarting ${TARGET_CONTAINER} ==`);
  dockerCompose(`start ${TARGET_CONTAINER}`);
  try {
    const elapsed = await waitForStatus(auth, TARGET_NODE, 'healthy', 15);
    record('node recovers after restart', true, `${elapsed.toFixed(1)}s`);
  } catch (err) {
    record('node recovers after restart', false, err.message);
  }

  console.log('\n== summary ==');
  for (const r of results) console.log(`  [${r.ok ? 'PASS' : 'FAIL'}] ${r.label}${r.detail ? ` (${r.detail})` : ''}`);
  const failed = results.filter((r) => !r.ok);
  if (failed.length > 0) {
    console.log(`\n${failed.length}/${results.length} ASSERTIONS FAILED`);
    process.exit(1);
  }
  console.log(`\nALL ${results.length} CHAOS ASSERTIONS PASSED`);
}

main().catch((err) => {
  console.error('FAIL:', err.message);
  // Best-effort: don't leave the target node stopped if something earlier
  // threw before the restart step ran.
  try { dockerCompose(`start ${TARGET_CONTAINER}`); } catch { /* already up, or docker unavailable */ }
  process.exit(1);
});
