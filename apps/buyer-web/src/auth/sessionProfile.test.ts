import { describe, expect, it } from "vitest";
import { emailToBuyerId, isValidEmail } from "./sessionProfile";

describe("emailToBuyerId", () => {
  it("maps gmail to stable buyer id", () => {
    const a = emailToBuyerId("Alex.Smith@gmail.com");
    const b = emailToBuyerId("alex.smith@gmail.com");
    expect(a).toBe(b);
    expect(a.startsWith("g_")).toBe(true);
    expect(a.length).toBeGreaterThan(8);
  });

  it("rejects bad email", () => {
    expect(emailToBuyerId("not-an-email")).toBe("");
    expect(isValidEmail("you@gmail.com")).toBe(true);
    expect(isValidEmail("nope")).toBe(false);
  });
});
