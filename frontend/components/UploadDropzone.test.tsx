import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { UploadDropzone } from "./UploadDropzone";
import type { UploadProgress } from "@/lib/upload";

// Audit §10's other named highest-value gap (alongside FileRow): the
// upload entry point had no component tests. lib/upload.ts's own resume/
// fingerprint state machine is already covered (lib/upload.test.ts) — this
// tests UploadDropzone's own job, which is purely UI: turning a
// drag/drop/file-picker event into an uploadFile() call and rendering
// whatever progress callback it gets back.

const uploadFileMock = vi.fn();

vi.mock("@/lib/upload", () => ({
  uploadFile: (...args: unknown[]) => uploadFileMock(...args),
}));

function makeFile(name = "photo.png") {
  return new File([new Uint8Array(10)], name, { type: "image/png" });
}

// The hidden <input type="file"> has no accessible name of its own (the
// aria-label lives on the surrounding role="button" div instead, so
// getByRole/getByLabelText both resolve to that div, not the input) —
// grabbed directly by tag/type instead.
function fileInput(container: HTMLElement): HTMLInputElement {
  return container.querySelector('input[type="file"]')!;
}

beforeEach(() => {
  uploadFileMock.mockReset();
});

describe("UploadDropzone", () => {
  it("renders the drop target with instructions and no progress list initially", () => {
    render(<UploadDropzone folderId="folder-1" onUploaded={vi.fn()} />);
    expect(screen.getByText(/Drag & drop files here/)).toBeInTheDocument();
    expect(screen.queryByRole("listitem")).not.toBeInTheDocument();
  });

  it("selecting a file via the hidden input starts an upload against the given folder", async () => {
    uploadFileMock.mockImplementation(
      (_file: File, _opts: unknown, onProgress: (p: UploadProgress) => void) => {
        onProgress({ fileName: "photo.png", loadedBytes: 5, totalBytes: 10, status: "uploading" });
        return Promise.resolve();
      },
    );
    const onUploaded = vi.fn();
    const { container } = render(<UploadDropzone folderId="folder-1" onUploaded={onUploaded} />);

    await userEvent.upload(fileInput(container), makeFile());

    expect(uploadFileMock).toHaveBeenCalledTimes(1);
    const [file, opts] = uploadFileMock.mock.calls[0];
    expect(file.name).toBe("photo.png");
    expect(opts).toEqual({ folderId: "folder-1" });
    expect(await screen.findByText("photo.png")).toBeInTheDocument();
    expect(screen.getByText(/uploading/)).toBeInTheDocument();
    await waitFor(() => expect(onUploaded).toHaveBeenCalledTimes(1));
  });

  it("dropping a file starts an upload the same way as the file picker", async () => {
    uploadFileMock.mockResolvedValue(undefined);
    render(<UploadDropzone folderId="folder-1" onUploaded={vi.fn()} />);

    const dropzone = screen.getByRole("button", { name: /Upload files/ });
    fireEvent.drop(dropzone, { dataTransfer: { files: [makeFile("dropped.txt")] } });

    await waitFor(() => expect(uploadFileMock).toHaveBeenCalledTimes(1));
    expect(uploadFileMock.mock.calls[0][0].name).toBe("dropped.txt");
  });

  it("shows the done badge once a progress callback reports status: done", async () => {
    uploadFileMock.mockImplementation(
      (_file: File, _opts: unknown, onProgress: (p: UploadProgress) => void) => {
        onProgress({ fileName: "photo.png", loadedBytes: 10, totalBytes: 10, status: "done" });
        return Promise.resolve();
      },
    );
    const { container } = render(<UploadDropzone folderId="folder-1" onUploaded={vi.fn()} />);

    await userEvent.upload(fileInput(container), makeFile());

    expect(await screen.findByText("done")).toBeInTheDocument();
  });

  it("shows the error message from a failed upload's progress callback and doesn't call onUploaded", async () => {
    uploadFileMock.mockImplementation(
      (_file: File, _opts: unknown, onProgress: (p: UploadProgress) => void) => {
        onProgress({
          fileName: "photo.png",
          loadedBytes: 0,
          totalBytes: 10,
          status: "error",
          error: "network failure",
        });
        return Promise.reject(new Error("network failure"));
      },
    );
    const onUploaded = vi.fn();
    const { container } = render(<UploadDropzone folderId="folder-1" onUploaded={onUploaded} />);

    await userEvent.upload(fileInput(container), makeFile());

    expect(await screen.findByText("network failure")).toBeInTheDocument();
    expect(onUploaded).not.toHaveBeenCalled();
  });

  it("is keyboard-operable: Enter on the dropzone opens the file picker", async () => {
    const { container } = render(<UploadDropzone folderId="folder-1" onUploaded={vi.fn()} />);
    const dropzone = screen.getByRole("button", { name: /Upload files/ });
    const clickSpy = vi.spyOn(fileInput(container), "click");

    dropzone.focus();
    await userEvent.keyboard("{Enter}");

    expect(clickSpy).toHaveBeenCalled();
  });
});
