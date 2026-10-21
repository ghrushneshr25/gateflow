import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as client from "../api/client";
import { BudgetsPage } from "./BudgetsPage";

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    api: {
      getCoordination: vi.fn(),
      getBudget: vi.fn(),
      putBudget: vi.fn(),
    },
  };
});

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <BudgetsPage />
    </QueryClientProvider>,
  );
}

describe("BudgetsPage", () => {
  beforeEach(() => {
    vi.mocked(client.api.getCoordination).mockReset();
    vi.mocked(client.api.getBudget).mockReset();
    vi.mocked(client.api.putBudget).mockReset();
    vi.mocked(client.api.getCoordination).mockResolvedValue({
      id: "admission",
      epoch: 3,
      mode: "ACTIVE",
      updated_at: "2026-01-01T00:00:00Z",
    });
    vi.mocked(client.api.getBudget).mockResolvedValue({
      scope_key: "global",
      max_grants: 100,
      grants_issued: 10,
      max_active_checkouts: 50,
      active_checkouts: 2,
      coordination_epoch: 3,
    });
  });

  it("loads global budget and confirms update", async () => {
    const user = userEvent.setup();
    vi.mocked(client.api.putBudget).mockResolvedValue({
      scope_key: "global",
      max_grants: 200,
      grants_issued: 10,
      max_active_checkouts: 50,
      active_checkouts: 2,
      coordination_epoch: 3,
    });
    renderPage();
    await waitFor(() => expect(screen.getByLabelText(/Max grants/i)).toBeInTheDocument());
    const grants = screen.getByLabelText(/Max grants/i);
    await user.clear(grants);
    await user.type(grants, "200");
    await user.click(screen.getByRole("button", { name: /Review update/i }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Confirm update/i }));
    await waitFor(() =>
      expect(client.api.putBudget).toHaveBeenCalledWith("global", {
        max_grants: 200,
        max_active_checkouts: 50,
      }),
    );
  });

  it("allows reviewing budget update while fenced", async () => {
    const user = userEvent.setup();
    vi.mocked(client.api.getCoordination).mockResolvedValue({
      id: "admission",
      epoch: 4,
      mode: "FENCED",
      updated_at: "2026-01-01T00:00:00Z",
    });
    renderPage();
    await waitFor(() => expect(screen.getByLabelText(/Max grants/i)).toBeInTheDocument());
    const grants = screen.getByLabelText(/Max grants/i);
    await user.clear(grants);
    await user.type(grants, "150");
    await user.click(screen.getByRole("button", { name: /Review update/i }));
    // Confirm dialog explains fence does not reset counters; caps still updatable
    expect(screen.getByRole("dialog")).toHaveTextContent(/FENCED/i);
    expect(screen.getByRole("dialog")).toHaveTextContent(/Caps still update/i);
  });
});
