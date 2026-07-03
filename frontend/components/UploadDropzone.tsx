"use client";

import { useCallback, useRef, useState } from "react";
import { uploadFile, UploadProgress } from "@/lib/upload";
import { formatBytes } from "@/lib/format";

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
        className={`glow-ring cursor-pointer rounded-xl border-2 border-dashed p-6 text-center text-sm transition-colors ${
          dragging ? "border-accent bg-accent-soft" : "border-border text-muted hover:border-border-strong"
        }`}
      >
        Drag & drop files here, or click to browse
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
            <li key={i} className="flex items-center gap-3 rounded-lg bg-surface-2 px-3 py-2 text-xs">
              <span className="flex-1 truncate">{it.fileName}</span>
              {it.status === "error" ? (
                <span className="text-danger">{it.error ?? "failed"}</span>
              ) : it.status === "done" ? (
                <span className="text-success">done</span>
              ) : (
                <>
                  <span className="text-muted">
                    {it.status} · {formatBytes(it.loadedBytes)}/{formatBytes(it.totalBytes)}
                  </span>
                  <div className="h-1.5 w-24 overflow-hidden rounded-full bg-surface">
                    <div
                      className="h-full bg-accent-strong transition-all"
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
