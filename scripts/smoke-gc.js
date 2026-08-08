#!/usr/bin/env node
// Post-v1 backlog #10/#11 verification: chunk garbage collection and trash
// auto-purge, end-to-end against the real Compose stack. Compose-only (like
// smoke-thumbnails.js): asserts DB state via `docker exec ... psql` and
// physical MinIO object presence via `docker exec ... test -d`.
//
// Requires nimbus-worker to be running with SHORT GC windows, e.g. a
// throwaway compose override setting NIMBUS_GC_INTERVAL=10s and
// NIMBUS_GC_GRACE=20s — the defaults (10m/1h) would make this test take
// hours. Pass the same values via GC_INTERVAL_S / GC_GRACE_S if overridden
// differently.
'use strict';
const crypto = require('crypto');
const { execFileSync } = require('child_process');

const BASE = process.env.NIMBUS_BASE_URL || 'http://localhost:8080';
const CHUNK_SIZE = 8 * 1024 * 1024;
const GC_INTERVAL_S = Number(process.env.GC_INTERVAL_S || 10);
const GC_GRACE_S = Number(process.env.GC_GRACE_S || 20);
// One full "phase" = grace elapses + the next tick notices; padded generously.
const PHASE_TIMEOUT_MS = (GC_GRACE_S + 3 * GC_INTERVAL_S + 10) * 1000;

let passed = 0;
function ok(name) { passed++; console.log(`  ok ${passed}: ${name}`); }
function fail(msg) { throw new Error(msg); }

async function jsonFetch(url, opts = {}) {
  const res = await fetch(url, { ...opts, headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) } });
  const text = await res.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; } catch { body = text; }
  if (!res.ok) throw new Error(`${opts.method || 'GET'} ${url} -> ${res.status}: ${JSON.stringify(body)}`);
  return body;
}

function sha256(buf) { return crypto.createHash('sha256').update(buf).digest('hex'); }

function psql(sql) {
  return execFileSync('docker', ['exec', 'nimbus-postgres-1', 'psql', '-U', 'nimbus', '-d', 'nimbus', '-tA', '-c', sql],
    { encoding: 'utf8' }).trim();
}

function objectExists(node, hash) {
  // MinIO stores each object as a directory <bucket>/<key>/ holding xl.meta.
  const out = execFileSync('docker',
    ['exec', `nimbus-minio-${node}-1`, 'sh', '-c', `test -d /data/nimbus-chunks/${hash} && echo yes || echo no`],
    { encoding: 'utf8' }).trim();
  return out === 'yes';
}

function compose(...args) {
  execFileSync('docker', ['compose', '-f', 'deploy/docker-compose.yml', ...args], { encoding: 'utf8', stdio: 'pipe' });
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitFor(desc, fn, timeoutMs = PHASE_TIMEOUT_MS) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    if (await fn()) return;
    if (Date.now() > deadline) fail(`timed out after ${timeoutMs}ms waiting for: ${desc}`);
    await sleep(2000);
  }
}

async function uploadFile(access, folderId, name, content) {
  const chunks = [];
  for (let i = 0; i < content.length; i += CHUNK_SIZE) chunks.push(content.subarray(i, i + CHUNK_SIZE));
  const hashes = chunks.map(sha256);
  const auth = { Authorization: `Bearer ${access}` };

  const { upload_id } = await jsonFetch(`${BASE}/v1/uploads`, {
    method: 'POST', headers: auth,
    body: JSON.stringify({ folder_id: folderId, name, size_bytes: content.length, mime_type: 'application/octet-stream' }),
  });
  // Upload-scoped, not global (audit §05: proof-of-possession) — needs
  // upload_id, so this runs after init now.
  const { missing } = await jsonFetch(`${BASE}/v1/uploads/${upload_id}/chunks/check`, {
    method: 'POST', headers: auth, body: JSON.stringify({ hashes }),
  });

  const missingSet = new Set(missing);
  for (let i = 0; i < chunks.length; i++) {
    if (!missingSet.has(hashes[i])) continue;
    const { targets } = await jsonFetch(`${BASE}/v1/uploads/${upload_id}/chunks/${hashes[i]}/init`, { method: 'POST', headers: auth });
    const etags = {};
    for (const t of targets) {
      const putRes = await fetch(t.put_url, { method: 'PUT', body: chunks[i] });
      if (!putRes.ok) fail(`PUT to ${t.node_id} failed: ${putRes.status}`);
      etags[t.node_id] = putRes.headers.get('etag');
    }
    await jsonFetch(`${BASE}/v1/uploads/${upload_id}/chunks/${hashes[i]}/commit`, {
      method: 'POST', headers: auth, body: JSON.stringify({ size_bytes: chunks[i].length, etags }),
    });
  }

  const { file_id, version_id } = await jsonFetch(`${BASE}/v1/uploads/${upload_id}/complete`, {
    method: 'POST', headers: { ...auth, 'Idempotency-Key': `gc-${upload_id}` },
    body: JSON.stringify({ chunk_order: hashes, size_bytes: content.length, checksum_sha256: sha256(content) }),
  });
  return { file_id, version_id, hashes, deduped: missing.length === 0 };
}

async function trashAndPurge(access, fileId) {
  const auth = { Authorization: `Bearer ${access}` };
  await jsonFetch(`${BASE}/v1/files/${fileId}`, { method: 'DELETE', headers: auth });
  await jsonFetch(`${BASE}/v1/files/${fileId}/purge`, { method: 'DELETE', headers: auth });
}

function gcStates(hashes) {
  const rows = psql(`SELECT hash, gc_state FROM chunks WHERE hash IN (${hashes.map((h) => `'${h}'`).join(',')})`);
  const map = {};
  for (const line of rows.split('\n').filter(Boolean)) {
    const [h, s] = line.split('|');
    map[h] = s;
  }
  return map;
}

function locations(hash) {
  return psql(`SELECT node_id FROM chunk_locations WHERE chunk_hash = '${hash}'`).split('\n').filter(Boolean);
}

async function main() {
  console.log(`gc smoke: interval=${GC_INTERVAL_S}s grace=${GC_GRACE_S}s phase timeout=${PHASE_TIMEOUT_MS / 1000}s`);
  const email = `gc-smoke-${Date.now()}@nimbus.dev`;
  const password = 'correct-horse-battery-staple';
  await jsonFetch(`${BASE}/v1/auth/register`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const { access_token: access } = await jsonFetch(`${BASE}/v1/auth/login`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const auth = { Authorization: `Bearer ${access}` };
  const { id: orgId } = await jsonFetch(`${BASE}/v1/orgs`, { method: 'POST', headers: auth, body: JSON.stringify({ name: 'GC Smoke Org' }) });
  const { id: folderId } = await jsonFetch(`${BASE}/v1/orgs/${orgId}/folders`, { method: 'POST', headers: auth, body: JSON.stringify({ name: 'GC' }) });

  // ---- 1. dedup protection: a purged file's chunks survive while a second file references them
  console.log('== dedup protection: purge one of two files sharing content ==');
  const content = crypto.randomBytes(9 * 1024 * 1024); // 2 chunks: 8 MiB + 1 MiB
  const a = await uploadFile(access, folderId, 'gc-a.bin', content);
  const b = await uploadFile(access, folderId, 'gc-b.bin', content);
  if (!b.deduped) fail('second upload of identical content was not fully deduped');
  await trashAndPurge(access, a.file_id);
  await sleep((GC_GRACE_S + 2 * GC_INTERVAL_S) * 1000); // enough time to be wrongly doomed, if the refcount were broken
  let states = gcStates(a.hashes);
  for (const h of a.hashes) if (states[h] !== 'live') fail(`chunk ${h} is '${states[h]}', expected live (still referenced by file B)`);
  ok('chunks stayed live while still referenced by the surviving file');

  // ---- 2. doom: purge the last reference, chunks get marked
  console.log('== mark phase: purge the last reference ==');
  await trashAndPurge(access, b.file_id);
  await waitFor('all chunks doomed', () => {
    const s = gcStates(a.hashes);
    return a.hashes.every((h) => s[h] === 'doomed');
  });
  ok('unreferenced chunks doomed after the grace window');

  // ---- 3. doomed chunks are invisible to dedup
  // A throwaway upload session just to scope the check (audit §05: dedup
  // check is per-upload/org now, not a bare global lookup).
  const { upload_id: checkUploadId } = await jsonFetch(`${BASE}/v1/uploads`, {
    method: 'POST', headers: auth,
    body: JSON.stringify({ folder_id: folderId, name: 'gc-doom-check.bin', size_bytes: content.length, mime_type: 'application/octet-stream' }),
  });
  const { missing } = await jsonFetch(`${BASE}/v1/uploads/${checkUploadId}/chunks/check`, { method: 'POST', headers: auth, body: JSON.stringify({ hashes: a.hashes }) });
  if (missing.length !== a.hashes.length) fail(`dedup check reported ${a.hashes.length - missing.length} doomed chunks as present`);
  ok('doomed chunks read as missing to the dedup check');

  // ---- 4. resurrection: re-uploading the content flips doomed back to live
  console.log('== resurrection: re-upload the same content while doomed ==');
  const c = await uploadFile(access, folderId, 'gc-c.bin', content);
  states = gcStates(c.hashes);
  for (const h of c.hashes) if (states[h] !== 'live') fail(`chunk ${h} is '${states[h]}' after re-upload, expected live`);
  const doomedAt = psql(`SELECT count(*) FROM chunks WHERE hash IN (${c.hashes.map((h) => `'${h}'`).join(',')}) AND doomed_at IS NOT NULL`);
  if (doomedAt !== '0') fail('resurrected chunks still carry doomed_at');
  ok('commit resurrected doomed chunks (live, doomed_at cleared)');

  // download must actually work after resurrection — fetch chunk 0 and re-hash it
  const plan = await jsonFetch(`${BASE}/v1/files/${c.file_id}/versions/${c.version_id}/download-plan`, { headers: auth });
  const target = plan.chunks[0].targets[0];
  const got = Buffer.from(await (await fetch(target)).arrayBuffer());
  if (sha256(got) !== c.hashes[0]) fail('downloaded chunk bytes do not match content hash after resurrection');
  ok('resurrected chunk downloads and passes checksum');

  // ---- 5. sweep: purge the resurrected file, chunks physically deleted
  console.log('== sweep phase: purge the last reference and wait for physical deletion ==');
  const locs = Object.fromEntries(c.hashes.map((h) => [h, locations(h)]));
  await trashAndPurge(access, c.file_id);
  await waitFor('chunks rows deleted', () => psql(`SELECT count(*) FROM chunks WHERE hash IN (${c.hashes.map((h) => `'${h}'`).join(',')})`) === '0',
    2 * PHASE_TIMEOUT_MS); // doom window + sweep window
  ok('chunks rows deleted by the sweep');
  for (const h of c.hashes) {
    for (const node of locs[h]) {
      if (objectExists(node, h)) fail(`MinIO object ${h} still present on ${node} after sweep`);
    }
  }
  ok('MinIO objects deleted from every recorded replica');

  // ---- 6. trash auto-purge (backlog #11): a file trashed past retention is purged without user action
  console.log('== trash auto-purge: backdate a trashed file past the retention window ==');
  const d = await uploadFile(access, folderId, 'gc-d.bin', crypto.randomBytes(1024 * 1024));
  await jsonFetch(`${BASE}/v1/files/${d.file_id}`, { method: 'DELETE', headers: auth });
  psql(`UPDATE files SET deleted_at = now() - interval '31 days' WHERE id = '${d.file_id}'`);
  await waitFor('expired trashed file auto-purged', () => psql(`SELECT count(*) FROM files WHERE id = '${d.file_id}'`) === '0');
  ok('file trashed past retention was auto-purged by the worker');
  await waitFor('auto-purged file’s chunks doomed', () => {
    const s = gcStates(d.hashes);
    return d.hashes.every((h) => s[h] === 'doomed' || s[h] === undefined);
  });
  ok('auto-purge fed the chunk GC (chunks doomed after references vanished)');

  // ---- 7. folder auto-purge with the live-content guard
  console.log('== folder auto-purge: guard against cascading over restored content ==');
  const { id: gFolder } = await jsonFetch(`${BASE}/v1/orgs/${orgId}/folders`, { method: 'POST', headers: auth, body: JSON.stringify({ name: 'GC-Doomed-Folder' }) });
  const e = await uploadFile(access, gFolder, 'gc-e.bin', crypto.randomBytes(1024 * 1024));
  await jsonFetch(`${BASE}/v1/folders/${gFolder}`, { method: 'DELETE', headers: auth }); // trashes folder + file
  psql(`UPDATE folders SET deleted_at = now() - interval '31 days' WHERE id = '${gFolder}'`);
  // restore just the file: the folder is expired but its subtree now holds live content
  await jsonFetch(`${BASE}/v1/files/${e.file_id}/restore`, { method: 'POST', headers: auth });
  await sleep(3 * GC_INTERVAL_S * 1000);
  if (psql(`SELECT count(*) FROM folders WHERE id = '${gFolder}'`) !== '1') fail('folder with a restored (live) file inside was purged — guard failed');
  ok('expired folder kept while its subtree contains a live file');
  await jsonFetch(`${BASE}/v1/files/${e.file_id}`, { method: 'DELETE', headers: auth });
  psql(`UPDATE files SET deleted_at = now() - interval '31 days' WHERE id = '${e.file_id}'`);
  await waitFor('expired folder purged once subtree is all-trashed', () => psql(`SELECT count(*) FROM folders WHERE id = '${gFolder}'`) === '0');
  if (psql(`SELECT count(*) FROM files WHERE id = '${e.file_id}'`) !== '0') fail('folder cascade left the trashed file row behind');
  ok('expired folder purged (cascading its trashed file) once nothing live remained');

  // ---- 8. at-least-once sweep: a down replica node blocks deletion, retried after recovery
  console.log('== down-node resilience: sweep must not half-delete, and must retry ==');
  const f = await uploadFile(access, folderId, 'gc-f.bin', crypto.randomBytes(1024 * 1024));
  const fLocs = locations(f.hashes[0]);
  const victim = fLocs[0];
  await trashAndPurge(access, f.file_id);
  await waitFor('chunk doomed before node kill', () => gcStates(f.hashes)[f.hashes[0]] === 'doomed');
  console.log(`  stopping ${victim} (holds a replica) ...`);
  compose('stop', `minio-${victim}`); // chunk_locations stores "node-N"; the compose service is "minio-node-N"
  try {
    await sleep((GC_GRACE_S + 3 * GC_INTERVAL_S) * 1000); // several sweep attempts, all of which must fail
    if (psql(`SELECT count(*) FROM chunks WHERE hash = '${f.hashes[0]}'`) !== '1') fail('chunk row deleted while a replica node was down');
    ok('sweep held the doomed row while a replica node was unreachable');
  } finally {
    console.log(`  restarting ${victim} ...`);
    compose('start', `minio-${victim}`);
  }
  await waitFor('chunk reaped after node recovery', () => psql(`SELECT count(*) FROM chunks WHERE hash = '${f.hashes[0]}'`) === '0',
    2 * PHASE_TIMEOUT_MS);
  for (const node of fLocs) {
    if (objectExists(node, f.hashes[0])) fail(`object still on ${node} after post-recovery sweep`);
  }
  ok('sweep retried after node recovery and deleted every replica');

  console.log(`\nALL ${passed} GC SMOKE CHECKS PASSED`);
}

main().catch((err) => { console.error('FAIL:', err.message); process.exit(1); });
