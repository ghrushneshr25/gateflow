import { describe, expect, it } from "vitest";
import { mintBuyerDevJwt, normalizeBuyerId } from "./mintDevJwt";

describe("mintBuyerDevJwt", () => {
  it("normalizes buyer id", () => {
    expect(normalizeBuyerId("  Buyer_Demo-1  ")).toBe("buyer_demo-1");
    expect(normalizeBuyerId("ab")).toBe("");
  });

  it("mints a three-part JWT verifiable shape", async () => {
    const tok = await mintBuyerDevJwt("buyer_demo_1", { secret: "gateflow-dev-secret-change-me" });
    const parts = tok.split(".");
    expect(parts).toHaveLength(3);
    const mid = parts[1];
    expect(mid).toBeTruthy();
    const json = atob(mid!.replace(/-/g, "+").replace(/_/g, "/"));
    const payload = JSON.parse(json) as { sub: string; principal_type: string; scopes: string[]; iss: string };
    expect(payload.sub).toBe("buyer_demo_1");
    expect(payload.principal_type).toBe("buyer");
    expect(payload.scopes).toContain("buyer:self");
    expect(payload.iss).toBe("gateflow-dev");
  });
});
