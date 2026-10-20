import { beforeEach, describe, expect, it, vi } from "vitest";

// handoff keeps the attempt in module state: load a fresh copy per test.
const load = () => import("./handoff");

beforeEach(() => {
  vi.resetModules();
  window.history.replaceState({ idx: 3 }, "", "/");
});

describe("takeHandoff", () => {
  it("reads #t= and clears the fragment, keeping path, query and history state", async () => {
    const { takeHandoff } = await load();
    window.history.replaceState({ idx: 3 }, "", "/auth/pelican?x=1#t=abc.def-ghi");
    expect(takeHandoff()).toEqual({ token: "abc.def-ghi", to: null });
    expect(window.location.pathname).toBe("/auth/pelican");
    expect(window.location.search).toBe("?x=1");
    expect(window.location.hash).toBe("");
    expect(window.history.state).toEqual({ idx: 3 });
  });

  it("returns empty without a token, still clearing any fragment", async () => {
    const { takeHandoff } = await load();
    window.history.replaceState(null, "", "/auth/pelican");
    expect(takeHandoff().token).toBe("");
    window.history.replaceState(null, "", "/auth/pelican#other=1");
    expect(takeHandoff().token).toBe("");
    expect(window.location.hash).toBe("");
    window.history.replaceState(null, "", "/auth/pelican#t=");
    expect(takeHandoff().token).toBe("");
  });

  it("decodes the token like a query value", async () => {
    const { takeHandoff } = await load();
    window.history.replaceState(null, "", "/auth/pelican#t=a%2Bb");
    expect(takeHandoff().token).toBe("a+b");
  });

  it("keeps `to` only when it is a path on this origin", async () => {
    const { takeHandoff } = await load();
    const to = (raw: string) => {
      window.history.replaceState(null, "", `/auth/pelican#t=x&to=${raw}`);
      return takeHandoff().to;
    };
    expect(to(encodeURIComponent("/mods?tab=2"))).toBe("/mods?tab=2");
    expect(to("%2Fbackups")).toBe("/backups");
    for (const bad of [
      "//evil.example/x",
      "https://evil.example/",
      "/\\evil.example",
      "javascript:alert(1)",
      "mods",
      "/a%0Ab",
    ]) {
      expect(to(encodeURIComponent(decodeURIComponent(bad))), bad).toBeNull();
    }
  });
});

describe("handoffOnce", () => {
  it("posts the token exactly once across repeated calls (StrictMode)", async () => {
    const { handoffOnce } = await load();
    window.history.replaceState(null, "", "/auth/pelican#t=tok&to=%2Fmods");
    const post = vi.fn(async (token: string) => ({ token }));
    const first = handoffOnce(post);
    const second = handoffOnce(post);
    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith("tok");
    expect(second).toBe(first);
    expect(first?.to).toBe("/mods");
    await expect(first?.result).resolves.toEqual({ token: "tok" });
    expect(window.location.hash).toBe("");
  });

  it("shares a failed attempt instead of retrying the spent token", async () => {
    const { handoffOnce } = await load();
    window.history.replaceState(null, "", "/auth/pelican#t=tok");
    const post = vi.fn(() => Promise.reject(new Error("token already used")));
    const first = handoffOnce(post);
    await expect(first?.result).rejects.toThrow("token already used");
    expect(handoffOnce(post)).toBe(first);
    expect(post).toHaveBeenCalledTimes(1);
  });

  it("returns null without a token, and posts a new token when the fragment changes", async () => {
    const { handoffOnce } = await load();
    window.history.replaceState(null, "", "/auth/pelican");
    const post = vi.fn(async (token: string) => token);
    expect(handoffOnce(post)).toBeNull();
    expect(post).not.toHaveBeenCalled();
    window.history.replaceState(null, "", "/auth/pelican#t=one");
    await expect(handoffOnce(post)?.result).resolves.toBe("one");
    window.history.replaceState(null, "", "/auth/pelican#t=two");
    await expect(handoffOnce(post)?.result).resolves.toBe("two");
    expect(post).toHaveBeenCalledTimes(2);
  });
});
