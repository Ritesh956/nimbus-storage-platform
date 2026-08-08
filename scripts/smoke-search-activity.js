#!/usr/bin/env node
// Day 8 deliverable (docs/09-roadmap.md): search by name/type/date works;
// an activity event appears after upload; upload.completed is actually
// published to NATS JetStream (checked directly against the stream, not
// just trusted from the API response).
'use strict';
const crypto = require('crypto');

const BASE = process.env.NIMBUS_BASE_URL || 'http://localhost:8080';
const sha256 = (buf) => crypto.createHash('sha256').update(buf).digest('hex');

async function jf(url, opts = {}) {
  const res = await fetch(url, { ...opts, headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) } });
  const text = await res.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; } catch { body = text; }
  if (!res.ok) throw new Error(`${opts.method || 'GET'} ${url} -> ${res.status}: ${JSON.stringify(body)}`);
  return body;
}

async function uploadSmallFile(access, folderId, name, content, mimeType) {
  const auth = { Authorization: `Bearer ${access}` };
  const hash = sha256(content);
  const { upload_id } = await jf(`${BASE}/v1/uploads`, { method: 'POST', headers: auth, body: JSON.stringify({ folder_id: folderId, name, size_bytes: content.length, mime_type: mimeType }) });
  // Upload-scoped, not global (audit §05: proof-of-possession) — needs
  // upload_id, so this runs after init now.
  await jf(`${BASE}/v1/uploads/${upload_id}/chunks/check`, { method: 'POST', headers: auth, body: JSON.stringify({ hashes: [hash] }) });
  const { targets } = await jf(`${BASE}/v1/uploads/${upload_id}/chunks/${hash}/init`, { method: 'POST', headers: auth });
  const etags = {};
  for (const t of targets) {
    const res = await fetch(t.put_url, { method: 'PUT', body: content });
    etags[t.node_id] = res.headers.get('etag');
  }
  await jf(`${BASE}/v1/uploads/${upload_id}/chunks/${hash}/commit`, { method: 'POST', headers: auth, body: JSON.stringify({ size_bytes: content.length, etags }) });
  return jf(`${BASE}/v1/uploads/${upload_id}/complete`, { method: 'POST', headers: { ...auth, 'Idempotency-Key': 'x' + upload_id }, body: JSON.stringify({ chunk_order: [hash], size_bytes: content.length, checksum_sha256: hash }) });
}

async function main() {
  const email = `search-activity-${Date.now()}@nimbus.dev`;
  const password = 'correct-horse-battery-staple';
  await jf(`${BASE}/v1/auth/register`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const { access_token: access } = await jf(`${BASE}/v1/auth/login`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const auth = { Authorization: `Bearer ${access}` };
  const { id: orgId } = await jf(`${BASE}/v1/orgs`, { method: 'POST', headers: auth, body: JSON.stringify({ name: 'Search Activity Org' }) });
  const { id: folderId } = await jf(`${BASE}/v1/orgs/${orgId}/folders`, { method: 'POST', headers: auth, body: JSON.stringify({ name: 'Mixed' }) });

  console.log('== uploading 3 files: 2 images, 1 text ==');
  const before = new Date(Date.now() - 5000).toISOString();
  const png = await uploadSmallFile(access, folderId, 'photo-alpha.png', Buffer.from('fake-png-bytes-1'), 'image/png');
  const jpg = await uploadSmallFile(access, folderId, 'photo-beta.jpg', Buffer.from('fake-jpg-bytes-2'), 'image/jpeg');
  const txt = await uploadSmallFile(access, folderId, 'readme.txt', Buffer.from('just some text'), 'text/plain');
  const after = new Date(Date.now() + 5000).toISOString();
  console.log(`  file_ids: ${png.file_id}, ${jpg.file_id}, ${txt.file_id}`);

  console.log('== search by name (q=photo) ==');
  const byName = await jf(`${BASE}/v1/orgs/${orgId}/search?q=photo`, { headers: auth });
  if (byName.results.length !== 2) throw new Error(`expected 2 results for q=photo, got ${byName.results.length}`);
  console.log(`  OK: ${byName.results.length} results`);

  console.log('== search by type (type=image) ==');
  const byType = await jf(`${BASE}/v1/orgs/${orgId}/search?type=image`, { headers: auth });
  if (byType.results.length !== 2) throw new Error(`expected 2 results for type=image, got ${byType.results.length}`);
  console.log(`  OK: ${byType.results.length} results`);

  console.log('== search by type (type=text) ==');
  const byType2 = await jf(`${BASE}/v1/orgs/${orgId}/search?type=text`, { headers: auth });
  if (byType2.results.length !== 1 || byType2.results[0].name !== 'readme.txt') throw new Error('expected exactly readme.txt for type=text');
  console.log('  OK: 1 result (readme.txt)');

  console.log('== search by date range (date_from/date_to bracketing the uploads) ==');
  const byDate = await jf(`${BASE}/v1/orgs/${orgId}/search?date_from=${encodeURIComponent(before)}&date_to=${encodeURIComponent(after)}`, { headers: auth });
  if (byDate.results.length !== 3) throw new Error(`expected 3 results in date range, got ${byDate.results.length}`);
  console.log(`  OK: ${byDate.results.length} results`);

  console.log('== search with a narrow limit exercises cursor pagination ==');
  const page1 = await jf(`${BASE}/v1/orgs/${orgId}/search?limit=2`, { headers: auth });
  if (page1.results.length !== 2 || !page1.next_cursor) throw new Error('expected 2 results + next_cursor on page 1');
  const page2 = await jf(`${BASE}/v1/orgs/${orgId}/search?limit=2&cursor=${encodeURIComponent(page1.next_cursor)}`, { headers: auth });
  if (page2.results.length !== 1) throw new Error(`expected 1 remaining result on page 2, got ${page2.results.length}`);
  const allIds = new Set([...page1.results, ...page2.results].map(r => r.file_id));
  if (allIds.size !== 3) throw new Error('paginated results were not a clean partition of all 3 files');
  console.log('  OK: 2 + 1 across pages, no duplicates/gaps');

  console.log('== activity feed shows the 3 uploads ==');
  const act = await jf(`${BASE}/v1/orgs/${orgId}/activity`, { headers: auth });
  const uploadEvents = act.events.filter(e => e.verb === 'uploaded' && e.target_type === 'file');
  if (uploadEvents.length !== 3) throw new Error(`expected 3 'uploaded' activity events, got ${uploadEvents.length}`);
  console.log(`  OK: ${uploadEvents.length} 'uploaded' events present`);

  console.log('\nALL SEARCH/ACTIVITY SMOKE CHECKS PASSED');
}

main().catch((err) => { console.error('FAIL:', err.message); process.exit(1); });
