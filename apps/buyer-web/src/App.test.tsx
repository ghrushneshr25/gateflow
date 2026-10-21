import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { App } from "./App";

describe("App", () => {
  it("renders shell with buyer auth and journey nav", () => {
    const client = new QueryClient();
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <App />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Gateflow");
    expect(screen.getByLabelText(/Buyer authentication/i)).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Primary" })).toHaveTextContent("Offerings");
  });
});
