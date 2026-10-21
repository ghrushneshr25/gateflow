import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "./App";

describe("App", () => {
  it("renders shell and recovery nav", () => {
    const client = new QueryClient();
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <App />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Gateflow Ops");
    const nav = screen.getByRole("navigation", { name: "Primary" });
    expect(nav).toBeInTheDocument();
    expect(nav).toHaveTextContent("Sales");
    expect(nav).toHaveTextContent("Budgets");
    expect(nav).toHaveTextContent("Reconciliation");
    expect(nav).toHaveTextContent("Outbox");
    expect(nav).toHaveTextContent("Coordination");
    expect(screen.getByLabelText(/Operator authentication/i)).toBeInTheDocument();
  });
});
