import { afterEach, describe, expect, it, vi } from "vitest";
import { getOAuthProviders } from "@/lib/api/auth";

function mockFetch(body: unknown, ok = true) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok,
      status: ok ? 200 : 500,
      json: async () => body,
    })),
  );
}

describe("getOAuthProviders", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("parses the stable providers envelope", async () => {
    mockFetch({
      providers: [{ name: "keycloak", display_name: "Enterprise SSO" }],
    });

    await expect(getOAuthProviders()).resolves.toEqual([
      { name: "keycloak", display_name: "Enterprise SSO" },
    ]);
  });

  it("keeps compatibility with the legacy array response", async () => {
    mockFetch([{ name: "okta", display_name: "Okta" }]);

    await expect(getOAuthProviders()).resolves.toEqual([
      { name: "okta", display_name: "Okta" },
    ]);
  });

  it("degrades to an empty list for failed discovery", async () => {
    mockFetch({ error: "not ready" }, false);

    await expect(getOAuthProviders()).resolves.toEqual([]);
  });
});
