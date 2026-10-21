import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as client from "../api/client";
import { ReconciliationPage } from "./ReconciliationPage";

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    api: {
      getReconciliation: vi.fn(),
      reconcileOrder: vi.fn(),
    },
  };
});

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ReconciliationPage />
    </QueryClientProvider>,
  );
}

describe("ReconciliationPage", () => {
  beforeEach(() => {
    vi.mocked(client.api.getReconciliation).mockReset();
    vi.mocked(client.api.reconcileOrder).mockReset();
  });

  it("shows empty state", async () => {
    vi.mocked(client.api.getReconciliation).mockResolvedValue({
      reconciliation_required: [],
      payment_pending: [],
    });
    renderPage();
    await waitFor(() =>
      expect(screen.getByText(/No reconciliation-required/i)).toBeInTheDocument(),
    );
  });

  it("lists orders and reconciles with server result", async () => {
    const user = userEvent.setup();
    vi.mocked(client.api.getReconciliation).mockResolvedValue({
      reconciliation_required: [
        {
          id: "ord_1",
          buyer_id: "b1",
          session_id: "s1",
          status: "RECONCILIATION_REQUIRED",
          quantity: 1,
          amount_cents: 1000,
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      payment_pending: [],
    });
    vi.mocked(client.api.reconcileOrder).mockResolvedValue({
      order: {
        id: "ord_1",
        buyer_id: "b1",
        session_id: "s1",
        status: "CONFIRMED",
        quantity: 1,
        amount_cents: 1000,
      },
      result: "resolved_confirmed",
    });
    renderPage();
    await waitFor(() => expect(screen.getByText("ord_1")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Reconcile" }));
    await waitFor(() =>
      expect(screen.getByText(/resolved_confirmed/i)).toBeInTheDocument(),
    );
    expect(client.api.reconcileOrder).toHaveBeenCalledWith("ord_1");
  });

  it("surfaces 403 distinctly", async () => {
    vi.mocked(client.api.getReconciliation).mockRejectedValue(
      new client.HttpError({ status: 403, message: "forbidden" }),
    );
    renderPage();
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/403/));
  });
});
