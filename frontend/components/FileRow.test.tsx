import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FileRow } from "./FileRow";
import { ToastProvider } from "./Toast";
import type { FileSummary } from "@/lib/types";

// Audit §10's other named highest-value gap: the file browser (this
// component drives rename/move/trash for every file row) had no tests at
// all. Covers the optimistic-mutation contract item 38/§12 introduced —
// FileRow calls the parent-supplied onOptimistic* props, not api.files.*
// directly — plus the failure-surfaces-via-toast-not-inline-alert
// decision documented in FileRow.tsx's own trash() comment.

const versionsMock = vi.fn();
const restoreMock = vi.fn();

vi.mock("@/lib/api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api")>("@/lib/api");
  return {
    ApiError: actual.ApiError,
    api: {
      files: {
        versions: (...args: unknown[]) => versionsMock(...args),
        restore: (...args: unknown[]) => restoreMock(...args),
      },
    },
  };
});

const file: FileSummary = {
  id: "file-1",
  name: "report.pdf",
  size_bytes: 2048,
  mime_type: "application/pdf",
  has_thumbnail: false,
};

function renderRow(overrides: Partial<React.ComponentProps<typeof FileRow>> = {}) {
  const props = {
    file,
    orgId: "org-1",
    folderId: "folder-1",
    onChanged: vi.fn(),
    onOptimisticTrash: vi.fn().mockResolvedValue(undefined),
    onOptimisticRename: vi.fn().mockResolvedValue(undefined),
    onOptimisticMove: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  render(
    <ToastProvider>
      <FileRow {...props} />
    </ToastProvider>,
  );
  return props;
}

beforeEach(() => {
  versionsMock.mockReset().mockResolvedValue([]);
  restoreMock.mockReset().mockResolvedValue(undefined);
});

describe("FileRow", () => {
  it("renders the file name and size, collapsed by default", () => {
    renderRow();
    expect(screen.getByText("report.pdf")).toBeInTheDocument();
    expect(screen.getByText("2.0 KB")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Move to trash/ })).not.toBeInTheDocument();
  });

  it("expands on click and loads versions exactly once", async () => {
    const user = userEvent.setup();
    renderRow();

    await user.click(screen.getByText("report.pdf"));

    expect(await screen.findByRole("button", { name: /Move to trash/ })).toBeInTheDocument();
    expect(versionsMock).toHaveBeenCalledWith("file-1");
    expect(versionsMock).toHaveBeenCalledTimes(1);
  });

  it("rename calls onOptimisticRename with the new name, not api.files.rename directly", async () => {
    const user = userEvent.setup();
    const props = renderRow();

    await user.click(screen.getByText("report.pdf"));
    await user.click(await screen.findByRole("button", { name: /Rename/ }));
    const input = screen.getByLabelText("File name");
    await user.clear(input);
    await user.type(input, "renamed.pdf");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    expect(props.onOptimisticRename).toHaveBeenCalledWith("file-1", "renamed.pdf");
  });

  it("a failed rename shows the row's own inline alert (the row survives a rename failure)", async () => {
    const user = userEvent.setup();
    const { ApiError } = await import("@/lib/api");
    const onOptimisticRename = vi.fn().mockRejectedValue(new ApiError(409, "conflict", "name already exists"));
    renderRow({ onOptimisticRename });

    await user.click(screen.getByText("report.pdf"));
    await user.click(await screen.findByRole("button", { name: /Rename/ }));
    await user.click(screen.getByRole("button", { name: "Save name" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("name already exists");
  });

  it("trash calls onOptimisticTrash and shows an undo toast on success", async () => {
    const user = userEvent.setup();
    const props = renderRow();

    await user.click(screen.getByText("report.pdf"));
    await user.click(await screen.findByRole("button", { name: /Move to trash/ }));

    expect(props.onOptimisticTrash).toHaveBeenCalledWith("file-1");
    const toast = await screen.findByRole("status");
    expect(toast).toHaveTextContent('"report.pdf" moved to trash');
    expect(within(toast).getByRole("button", { name: "Undo" })).toBeInTheDocument();
  });

  it("clicking Undo restores the file and calls onChanged", async () => {
    const user = userEvent.setup();
    const props = renderRow();

    await user.click(screen.getByText("report.pdf"));
    await user.click(await screen.findByRole("button", { name: /Move to trash/ }));
    const toast = await screen.findByRole("status");
    await user.click(within(toast).getByRole("button", { name: "Undo" }));

    expect(restoreMock).toHaveBeenCalledWith("file-1");
    expect(props.onChanged).toHaveBeenCalled();
  });

  it("a failed trash surfaces via toast, not the row's inline alert (the row may already be gone from the list)", async () => {
    const user = userEvent.setup();
    const { ApiError } = await import("@/lib/api");
    const onOptimisticTrash = vi.fn().mockRejectedValue(new ApiError(500, "internal", "chunk delete failed"));
    renderRow({ onOptimisticTrash });

    await user.click(screen.getByText("report.pdf"));
    await user.click(await screen.findByRole("button", { name: /Move to trash/ }));

    const toast = await screen.findByRole("status");
    expect(toast).toHaveTextContent("chunk delete failed");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
