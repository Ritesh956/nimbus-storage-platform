// Live server-push over SSE (backlog #12): subscribes to
// GET /v1/orgs/{orgId}/events and surfaces the two event kinds the backend
// relays (org activity, storage-node health transitions). Implemented with
// fetch + a stream reader rather than EventSource because EventSource can't
// send an Authorization header, and putting the bearer token in the query
// string would leak it into server request logs.
//
// Events carry no full payloads on purpose — consumers treat them as
// revalidation signals for SWR caches they already own (mutate the right
// key), not as a second source of truth.
import { useEffect, useRef } from "react";
import { getAccessToken } from "./tokens";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type LiveActivity = { verb: string; target_type: string; target_id: string };
export type LiveNodeHealth = { node: string; status: string };

type Handlers = {
  onActivity?: (e: LiveActivity) => void;
  onNodeHealth?: (e: LiveNodeHealth) => void;
};

export function useLiveEvents(orgId: string | undefined, handlers: Handlers) {
  // Handlers live in a ref so a re-render with fresh closures doesn't tear
  // down and re-open the stream.
  const handlersRef = useRef(handlers);
  handlersRef.current = handlers;

  useEffect(() => {
    if (!orgId) return;
    const ctrl = new AbortController();
    let stopped = false;

    async function connect(attempt: number) {
      try {
        const token = getAccessToken();
        if (!token) throw new Error("not authenticated");
        const res = await fetch(`${BASE_URL}/v1/orgs/${orgId}/events`, {
          headers: { Authorization: `Bearer ${token}`, Accept: "text/event-stream" },
          signal: ctrl.signal,
        });
        if (!res.ok || !res.body) throw new Error(`stream failed: ${res.status}`);
        attempt = 0; // reset backoff once genuinely connected

        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buf = "";
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          let sep;
          while ((sep = buf.indexOf("\n\n")) >= 0) {
            const frame = buf.slice(0, sep);
            buf = buf.slice(sep + 2);
            let event = "message";
            let data = "";
            for (const line of frame.split("\n")) {
              if (line.startsWith("event:")) event = line.slice(6).trim();
              else if (line.startsWith("data:")) data += line.slice(5).trim();
            }
            if (!data || data === "{}") continue;
            try {
              if (event === "activity") handlersRef.current.onActivity?.(JSON.parse(data));
              else if (event === "node_health") handlersRef.current.onNodeHealth?.(JSON.parse(data));
            } catch {
              // malformed frame — skip it rather than kill the stream
            }
          }
        }
      } catch {
        // fall through to the reconnect below (network error, 401 after
        // token expiry — the next attempt picks up whatever token the API
        // client's refresh flow has since written)
      }
      if (!stopped) {
        const delay = Math.min(1000 * 2 ** attempt, 15000);
        setTimeout(() => {
          if (!stopped) void connect(attempt + 1);
        }, delay);
      }
    }

    void connect(0);
    return () => {
      stopped = true;
      ctrl.abort();
    };
  }, [orgId]);
}
