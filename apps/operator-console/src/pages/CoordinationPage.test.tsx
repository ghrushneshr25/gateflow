import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as client from "../api/client";
import { CoordinationPage } from "./CoordinationPage";

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    api: {
      getCoordination: vi.fn(),
      fence: vi.fn(),
      unfence: vi.fn(),
    },
  };
});

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CoordinationPage />
    </QueryClientProvider>,
  );
}

describe("CoordinationPage", () => {
  beforeEach(() => {
    vi.mocked(client.api.getCoordination).mockReset();
    vi.mocked(client.api.fence).mockReset();
    vi.mocked(client.api.unfence).mockReset();
  });

  it("shows ACTIVE and confirms fence", async () => {
    const user = userEvent.setup();
    vi.mocked(client.api.getCoordination).mockResolvedValue({
      id: "admission",
      epoch: 1,
      mode: "ACTIVE",
      updated_at: "2026-01-01T00:00:00Z",
    });
    vi.mocked(client.api.fence).mockResolvedValue({
      id: "admission",
      epoch: 2,
      mode: "FENCED",
      fence_reason: "test",
      updated_at: "2026-01-01T00:00:01Z",
    });
    renderPage();
    await waitFor(() => expect(screen.getByText("ACTIVE")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Fence…" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm fence" }));
    await waitFor(() => expect(screen.getByText(/Fenced · epoch 2/i)).toBeInTheDocument());
  });
});
