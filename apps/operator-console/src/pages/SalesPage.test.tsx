import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as client from "../api/client";
import { SalesPage } from "./SalesPage";

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    api: {
      listOfferings: vi.fn(),
      getOffering: vi.fn(),
      listSessions: vi.fn(),
      listTicketTypes: vi.fn(),
      createOffering: vi.fn(),
      createSession: vi.fn(),
      createTicketType: vi.fn(),
      publishOffering: vi.fn(),
    },
  };
});

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SalesPage />
    </QueryClientProvider>,
  );
}

describe("SalesPage", () => {
  beforeEach(() => {
    vi.mocked(client.api.listOfferings).mockReset();
    vi.mocked(client.api.createOffering).mockReset();
    vi.mocked(client.api.publishOffering).mockReset();
    vi.mocked(client.api.getOffering).mockReset();
    vi.mocked(client.api.listSessions).mockReset();
    vi.mocked(client.api.listTicketTypes).mockReset();
  });

  it("lists offerings and creates a draft", async () => {
    const user = userEvent.setup();
    vi.mocked(client.api.listOfferings).mockResolvedValue({ items: [] });
    vi.mocked(client.api.createOffering).mockResolvedValue({
      id: "off_1",
      name: "Show",
      category: "concert",
      status: "DRAFT",
    });
    vi.mocked(client.api.getOffering).mockResolvedValue({
      id: "off_1",
      name: "Show",
      category: "concert",
      status: "DRAFT",
    });
    vi.mocked(client.api.listSessions).mockResolvedValue({ items: [] });
    vi.mocked(client.api.listTicketTypes).mockResolvedValue({ items: [] });
    // after create, list refetch
    vi.mocked(client.api.listOfferings)
      .mockResolvedValueOnce({ items: [] })
      .mockResolvedValue({
        items: [{ id: "off_1", name: "Show", category: "concert", status: "DRAFT" }],
      });

    renderPage();
    await waitFor(() => expect(screen.getByText(/No offerings yet/i)).toBeInTheDocument());
    await user.type(screen.getByLabelText(/^Name/), "Show");
    await user.click(screen.getByRole("button", { name: /Create draft/i }));
    await waitFor(() => expect(client.api.createOffering).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByText(/Created offering off_1/i)).toBeInTheDocument());
  });

  it("shows 403 on list failure", async () => {
    vi.mocked(client.api.listOfferings).mockRejectedValue(
      new client.HttpError({ status: 403, message: "forbidden" }),
    );
    renderPage();
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/403/));
  });
});
