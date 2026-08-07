"use client";

import { useCallback, useRef, useState } from "react";
import { uploadFile, UploadProgress } from "@/lib/upload";
import { formatBytes } from "@/lib/format";
import { UploadCloudIcon } from "./ui/Icons";

interface Props {
  folderId: string;
  onUploaded: () => void;
}

export function UploadDropzone({ folderId, onUploaded }: Props) {
  const [dragging, setDragging] = useState(false);
  const [items, setItems] = useState<UploadProgress[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);

  const startUploads = useCallback(
    (files: FileList | File[]) => {
      Array.from(files).forEach((file) => {
        const index = items.length; // append position, updated per-file below via functional state
        setItems((prev) => [...prev, { fileName: file.name, loadedBytes: 0, totalBytes: file.size, status: "hashing" }]);
        void uploadFile(file, { folderId }, (p) => {
          setItems((prev) => {
            const next = [...prev];
            const i = next.findIndex((it, idx) => it.fileName === p.fileName && idx >= index);
            if (i !== -1) next[i] = p;
            return next;
          });
        })
          .then(() => onUploaded())
          .catch(() => {
            /* progress already reflects the error via status: "error" */
          });
      });
    },
    [items.length, folderId, onUploaded],
  );

  return (
    <div>
      <div
        role="button"
        tabIndex={0}
        aria-label="Upload files — drag and drop, or activate to browse"
        onDragOver={(e) => {
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          if (e.dataTransfer.files.length) startUploads(e.dataTransfer.files);
        }}
        onClick={() => inputRef.current?.click()}
        onKeyDown={(e) => {
          if (e.key !== "Enter" && e.key !== " ") return;
          e.preventDefault();
          inputRef.current?.click();
        }}
        className={`glow-ring flex cursor-pointer flex-col items-center gap-2 rounded-xl border border-dashed px-6 py-7 text-center transition-colors ${
          dragging
            ? "border-accent bg-accent-soft"
            : "border-border bg-surface/40 hover:border-border-strong hover:bg-surface/70"
        }`}
      >
        <span className="gradient-primary grid size-10 place-items-center rounded-full text-white shadow-[0_0_18px_rgba(203,60,255,0.35)]">
          <UploadCloudIcon size={19} />
        </span>
        <div className="text-sm font-medium">Drag &amp; drop files here, or click to browse</div>
        <div className="text-[11px] text-muted-2">Chunked · resumable · deduplicated · replicated ×2</div>
        <input
          ref={inputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            if (e.target.files?.length) startUploads(e.target.files);
            e.target.value = "";
          }}
        />
      </div>

      {items.length > 0 && (
        <ul className="mt-3 flex flex-col gap-2">
          {items.map((it, i) => (
            <li key={i} className="panel flex items-center gap-3 px-4 py-2.5 text-xs">
              <span className="flex-1 truncate font-medium">{it.fileName}</span>
              {it.status === "error" ? (
                <span className="text-danger">{it.error ?? "failed"}</span>
              ) : it.status === "done" ? (
                <span className="rounded border border-success/25 bg-success/10 px-1.5 py-0.5 text-[10px] font-semibold text-success">
                  done
                </span>
              ) : (
                <>
                  <span className="text-muted-2">
                    {it.status} · {formatBytes(it.loadedBytes)}/{formatBytes(it.totalBytes)}
                  </span>
                  <div className="h-1.5 w-28 overflow-hidden rounded-full bg-surface-deep">
                    <div
                      className="h-full rounded-full bg-gradient-to-r from-accent-2 to-accent transition-all"
                      style={{ width: `${it.totalBytes ? Math.round((it.loadedBytes / it.totalBytes) * 100) : 0}%` }}
                    />
                  </div>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
