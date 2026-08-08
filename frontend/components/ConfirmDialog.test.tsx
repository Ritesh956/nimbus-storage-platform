import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ConfirmDialog } from "./ConfirmDialog";

// Audit §14/§10/§13: zero frontend component tests existed, and this
// dialog's a11y behavior (useModal's focus trap/Escape-close, added in the
// accessibility-hardening session) was only ever manually verified. This
// pins down the observable contract: Escape/backdrop/Cancel all close
// without confirming, Confirm calls onConfirm and surfaces a thrown error
// via role="alert" instead of silently swallowing it.

const noop = async () => {};

describe("ConfirmDialog", () => {
  it("renders the title, body, and confirm label", () => {
    render(<ConfirmDialog title="Delete file?" body="This cannot be undone." confirmLabel="Delete" onCancel={vi.fn()} onConfirm={noop} />);
    expect(screen.getByRole("heading", { name: "Delete file?" })).toBeInTheDocument();
    expect(screen.getByText("This cannot be undone.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Delete/ })).toBeInTheDocument();
  });

  it("calls onCancel when the Cancel button is clicked", async () => {
    const onCancel = vi.fn();
    const user = userEvent.setup();
    render(<ConfirmDialog title="t" body="b" confirmLabel="Delete" onCancel={onCancel} onConfirm={noop} />);

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("calls onCancel when Escape is pressed (useModal's keyboard contract)", async () => {
    const onCancel = vi.fn();
    const user = userEvent.setup();
    render(<ConfirmDialog title="t" body="b" confirmLabel="Delete" onCancel={onCancel} onConfirm={noop} />);

    await user.keyboard("{Escape}");
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("calls onCancel when clicking the backdrop, but not when clicking inside the panel", async () => {
    const onCancel = vi.fn();
    const user = userEvent.setup();
    render(<ConfirmDialog title="Delete file?" body="b" confirmLabel="Delete" onCancel={onCancel} onConfirm={noop} />);

    // Clicking the dialog's own heading (inside the panel) must not close it.
    await user.click(screen.getByRole("heading", { name: "Delete file?" }));
    expect(onCancel).not.toHaveBeenCalled();

    // The backdrop is the outer fixed-inset div — role="dialog"'s parent.
    const dialog = screen.getByRole("dialog");
    const backdrop = dialog.parentElement!;
    await user.click(backdrop);
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("calls onConfirm and shows a busy state while the async action is pending", async () => {
    let resolveConfirm: () => void = () => {};
    const onConfirm = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveConfirm = resolve;
        }),
    );
    const user = userEvent.setup();
    render(<ConfirmDialog title="t" body="b" confirmLabel="Delete forever" onCancel={vi.fn()} onConfirm={onConfirm} />);

    await user.click(screen.getByRole("button", { name: "Delete forever" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: /Deleting…/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Deleting…/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();

    // On success the component does NOT clear its own busy state (no
    // finally/setBusy(false) on the success path in confirm()) — by design,
    // per the component's doc comment: "the dialog... closes itself only
    // after the action resolves", meaning the *caller* unmounts it once
    // onConfirm resolves, rather than this component un-busying itself to
    // sit there confirmable again. Confirming that contract rather than
    // asserting a self-clearing busy state the component was never meant
    // to have.
    resolveConfirm();
    await waitFor(() => expect(onConfirm).toHaveResolved());
    expect(screen.getByRole("button", { name: /Deleting…/ })).toBeInTheDocument();
  });

  it("surfaces a rejected onConfirm's error via role=alert instead of closing silently", async () => {
    const onConfirm = vi.fn().mockRejectedValue(new Error("purge failed: file still referenced"));
    const onCancel = vi.fn();
    const user = userEvent.setup();
    render(<ConfirmDialog title="t" body="b" confirmLabel="Delete" onCancel={onCancel} onConfirm={onConfirm} />);

    await user.click(screen.getByRole("button", { name: "Delete" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("purge failed: file still referenced");
    // A failed confirm must not have auto-closed the dialog.
    expect(onCancel).not.toHaveBeenCalled();
    // And it must have recovered from the busy state, not left the button stuck.
    expect(screen.getByRole("button", { name: "Delete" })).not.toBeDisabled();
  });
});
