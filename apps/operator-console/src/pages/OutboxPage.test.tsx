import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as client from "../api/client";
import { OutboxPage } from "./OutboxPage";

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    api: {
      listQuarantined: vi.fn(),
      replayEvent: vi.fn(),
    },
  };
});

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OutboxPage />
    </QueryClientProvider>,
  );
}

describe("OutboxPage", () => {
  beforeEach(() => {
    vi.mocked(client.api.listQuarantined).mockReset();
    vi.mocked(client.api.replayEvent).mockReset();
  });

  it("confirms before replay and shows result", async () => {
    const user = userEvent.setup();
    vi.mocked(client.api.listQuarantined).mockResolvedValue({
      items: [
        {
          id: 1,
          event_id: "evt_1",
          event_type: "order.order.confirmed",
          aggregate_id: "ord_1",
          attempt_count: 25,
          replay_count: 0,
          last_error: "boom",
          created_at: "2026-01-01T00:00:00Z",
          correlation_id: "c1",
        },
      ],
    });
    vi.mocked(client.api.replayEvent).mockResolvedValue({
      event: {
        id: 1,
        event_id: "evt_1",
        event_type: "order.order.confirmed",
        aggregate_id: "ord_1",
        attempt_count: 25,
        replay_count: 1,
        created_at: "2026-01-01T00:00:00Z",
        correlation_id: "c1",
      },
      result: "queued_for_publish",
    });
    renderPage();
    await waitFor(() => expect(screen.getByText("evt_1")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Replay…" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm replay" }));
    await waitFor(() => expect(screen.getByText(/queued_for_publish/i)).toBeInTheDocument());
    expect(client.api.replayEvent).toHaveBeenCalledTimes(1);
  });
});
