import { describe, expect, it } from "vitest";
import { HttpError } from "../api/client";
import { explainApiError, isTerminalOrderStatus, orderStatusCopy } from "./statusCopy";

describe("statusCopy", () => {
  it("never treats payment success as confirmed", () => {
    const c = orderStatusCopy("PAYMENT_SUCCEEDED");
    expect(c.label.toLowerCase()).not.toContain("confirmed booking");
    expect(c.next.toLowerCase()).toMatch(/not confirmation|until confirmed/);
  });

  it("explains fence and capacity", () => {
    expect(
      explainApiError(new HttpError({ status: 503, code: "ADM_COORDINATION_FENCED", message: "fenced" }))
        .title,
    ).toMatch(/fenced/i);
    expect(
      explainApiError(new HttpError({ status: 409, code: "INV_CAPACITY_EXCEEDED", message: "full" }))
        .title,
    ).toMatch(/sold out|capacity/i);
  });

  it("timeout is uncertain not failed", () => {
    const e = explainApiError(new HttpError({ status: 0, message: "failed to fetch" }));
    expect(e.action.toLowerCase()).toMatch(/uncertain|may already|idempotency/);
    expect(e.retryable).toBe(true);
  });

  it("terminal statuses", () => {
    expect(isTerminalOrderStatus("CONFIRMED")).toBe(true);
    expect(isTerminalOrderStatus("FAILED")).toBe(true);
    expect(isTerminalOrderStatus("PAYMENT_PENDING")).toBe(false);
  });
});
