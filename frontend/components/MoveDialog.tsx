"use client";

import { useEffect, useState } from "react";
import useSWR from "swr";
import { api, ApiError } from "@/lib/api";
import { Button } from "./ui/Button";
import { ArrowLeftIcon, FolderIcon } from "./ui/Icons";

interface Props {
  orgId: string;
  item: { kind: "file"; id: string; name: string; currentFolderId: string } | { kind: "folder"; id: string; name: string };
  onClose: () => void;
  onMoved: () => void;
}

// MoveDialog is a drill-down folder picker over the same listing endpoints
// the browser pages use (root folders + folder children), driving the
// PATCH move support that until now only had a rename UI. The folder being
// moved is hidden from the listing — you can't reach yourself or your own
// descendants without going through yourself, so that alone keeps the
// picker cycle-free (the backend still enforces it server-side).
export function MoveDialog({ orgId, item, onClose, onMoved }: Props) {
  // Path from root down to where the picker currently is; empty = root.
  const [path, setPath] = useState<{ id: string; name: string }[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const current = path[path.length - 1];
  const { data, isLoading } = useSWR(
    current ? ["move-children", current.id] : ["move-root", orgId],
    () => (current ? api.folders.children(current.id).then((d) => d.folders) : api.orgs.rootFolders(orgId)),
  );
  const folders = data?.filter((f) => f.id !== item.id);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  // Files must live in a folder; folders may also move to root.
  const canMoveHere =
    item.kind === "file" ? current != null && current.id !== item.currentFolderId : true;

  async function moveHere() {
    setBusy(true);
    setError(null);
    try {
      if (item.kind === "file") {
        await api.files.move(item.id, current!.id);
      } else {
        await api.folders.move(item.id, current?.id ?? null);
      }
      onMoved();
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "move failed");
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/70 p-4 backdrop-blur-sm" onClick={onClose}>
      <div
        className="panel flex w-full max-w-md flex-col overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="border-b border-border/60 px-5 py-3.5">
          <div className="text-sm font-medium">Move &ldquo;{item.name}&rdquo;</div>
          <div className="mt-1 flex items-center gap-1.5 text-xs text-muted-2">
            {path.length > 0 && (
              <button
                onClick={() => setPath((p) => p.slice(0, -1))}
                className="glow-ring rounded p-0.5 transition-colors hover:text-foreground"
                title="Up one level"
              >
                <ArrowLeftIcon size={13} />
              </button>
            )}
            <span className="truncate">
              {["All folders", ...path.map((p) => p.name)].join(" / ")}
            </span>
          </div>
        </div>

        <ul className="max-h-64 overflow-y-auto">
          {isLoading && <li className="px-5 py-3 text-xs text-muted-2">Loading…</li>}
          {folders?.map((f) => (
            <li key={f.id} className="border-t border-border/40 first:border-t-0">
              <button
                onClick={() => setPath((p) => [...p, { id: f.id, name: f.name }])}
                className="glow-ring flex w-full items-center gap-3 px-5 py-2.5 text-left text-sm transition-colors hover:bg-surface-2/60"
              >
                <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent-2">
                  <FolderIcon size={14} />
                </span>
                <span className="truncate">{f.name}</span>
              </button>
            </li>
          ))}
          {folders && folders.length === 0 && !isLoading && (
            <li className="px-5 py-3 text-xs text-muted-2">No subfolders here.</li>
          )}
        </ul>

        <div className="flex items-center justify-between gap-3 border-t border-border/60 px-5 py-3.5">
          {error ? <p className="min-w-0 truncate text-xs text-danger">{error}</p> : <span />}
          <div className="flex shrink-0 gap-2">
            <Button variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button disabled={!canMoveHere || busy} onClick={moveHere}>
              Move here
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
