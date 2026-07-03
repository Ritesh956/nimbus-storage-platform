// Liveness/readiness target for the container/k8s probes (Day 13) — the
// backend has /healthz and /readyz already; this is the frontend's
// equivalent so probes don't have to hit "/" and pay for a full page render.
export async function GET() {
  return Response.json({ status: "ok" });
}
