import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as client from "../api/client";
import { BookingPage } from "./BookingPage";

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    getToken: vi.fn(() => "tok"),
    getOrCreateIdempotencyKey: vi.fn(() => "idem-fixed"),
    clearIdempotencyKey: vi.fn(),
    getOrderHint: vi.fn(() => null),
    setOrderHint: vi.fn(),
    api: {
      queueMe: vi.fn(),
      joinQueue: vi.fn(),
      requestGrant: vi.fn(),
      checkout: vi.fn(),
      getOrder: vi.fn(),
      reconcileOrder: vi.fn(),
      listOrders: vi.fn(),
    },
  };
});

function renderBook() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/book/off_1?session=ses_1&tt=tt_1&pool=pool_1&price=1000"]}>
        <Routes>
          <Route path="/book/:offeringId" element={<BookingPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("BookingPage", () => {
  beforeEach(() => {
    vi.mocked(client.api.queueMe).mockReset();
    vi.mocked(client.api.joinQueue).mockReset();
    vi.mocked(client.api.requestGrant).mockReset();
    vi.mocked(client.api.checkout).mockReset();
    vi.mocked(client.api.queueMe).mockRejectedValue(
      new client.HttpError({ status: 404, message: "not in queue" }),
    );
  });

  it("joins, grants, and checkouts with stable idempotency key", async () => {
    const user = userEvent.setup();
    vi.mocked(client.api.joinQueue).mockResolvedValue({
      id: "q1",
      session_id: "ses_1",
      buyer_id: "b1",
      sequence_no: 1,
      status: "JOINED",
    });
    vi.mocked(client.api.requestGrant).mockResolvedValue({
      queue_entry: {
        id: "q1",
        session_id: "ses_1",
        buyer_id: "b1",
        sequence_no: 1,
        status: "GRANTED",
        entitlement_id: "ent_1",
      },
      entitlement: {
        id: "ent_1",
        session_id: "ses_1",
        buyer_id: "b1",
        status: "ISSUED",
      },
    });
    vi.mocked(client.api.checkout).mockResolvedValue({
      id: "ord_1",
      session_id: "ses_1",
      status: "CONFIRMED",
      quantity: 1,
      amount_cents: 1000,
    });

    renderBook();
    await user.click(screen.getByRole("button", { name: /Join queue/i }));
    await waitFor(() => expect(client.api.joinQueue).toHaveBeenCalled());
    await user.click(screen.getByRole("button", { name: /Request grant/i }));
    await waitFor(() => expect(client.api.requestGrant).toHaveBeenCalled());
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /^Checkout$/i })).not.toBeDisabled(),
    );
    await user.click(screen.getByRole("button", { name: /^Checkout$/i }));
    await waitFor(() =>
      expect(client.api.checkout).toHaveBeenCalledWith(
        expect.objectContaining({
          entitlement_id: "ent_1",
          idempotency_key: "idem-fixed",
        }),
      ),
    );
  });

  it("keeps idempotency key messaging on timeout", async () => {
    const user = userEvent.setup();
    vi.mocked(client.api.requestGrant).mockResolvedValue({
      queue_entry: {
        id: "q1",
        session_id: "ses_1",
        buyer_id: "b1",
        sequence_no: 1,
        status: "GRANTED",
        entitlement_id: "ent_1",
      },
      entitlement: { id: "ent_1", session_id: "ses_1", buyer_id: "b1", status: "ISSUED" },
    });
    vi.mocked(client.api.checkout).mockRejectedValue(
      new client.HttpError({ status: 0, message: "failed to fetch" }),
    );
    renderBook();
    await user.click(screen.getByRole("button", { name: /Request grant/i }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /^Checkout$/i })).not.toBeDisabled(),
    );
    await user.click(screen.getByRole("button", { name: /^Checkout$/i }));
    await waitFor(() =>
      expect(screen.getByText(/outcome uncertain/i)).toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: /Find prior order/i })).toBeInTheDocument();
  });

  it("surfaces fenced grant clearly", async () => {
    const user = userEvent.setup();
    vi.mocked(client.api.requestGrant).mockRejectedValue(
      new client.HttpError({
        status: 503,
        code: "ADM_COORDINATION_FENCED",
        message: "fenced",
      }),
    );
    renderBook();
    await user.click(screen.getByRole("button", { name: /Request grant/i }));
    await waitFor(() => expect(screen.getByText(/fenced/i)).toBeInTheDocument());
  });
});
