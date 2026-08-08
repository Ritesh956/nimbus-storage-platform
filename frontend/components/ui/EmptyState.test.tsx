import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { EmptyState } from "./EmptyState";

// Audit §14/§10: zero frontend component tests existed. EmptyState is the
// shared component roadmap #16 introduced across four first-run/zero-result
// screens (search, trash, org-root, folder) — a small, high-reuse surface
// worth pinning down.

describe("EmptyState", () => {
  it("renders the title and icon, with description/action omitted by default", () => {
    render(<EmptyState icon={<svg data-testid="icon" />} title="No files yet" />);

    expect(screen.getByText("No files yet")).toBeInTheDocument();
    expect(screen.getByTestId("icon")).toBeInTheDocument();
    expect(screen.queryByText(/description/i)).not.toBeInTheDocument();
  });

  it("renders an optional description when provided", () => {
    render(<EmptyState icon={<svg />} title="No results" description="Try a different search term." />);
    expect(screen.getByText("Try a different search term.")).toBeInTheDocument();
  });

  it("does not render a description paragraph when omitted", () => {
    const { container } = render(<EmptyState icon={<svg />} title="No results" />);
    // Only the title <p> should exist, not a second one for description.
    expect(container.querySelectorAll("p")).toHaveLength(1);
  });

  it("renders an optional action", () => {
    render(<EmptyState icon={<svg />} title="No folders" action={<button>Create folder</button>} />);
    expect(screen.getByRole("button", { name: "Create folder" })).toBeInTheDocument();
  });
});
