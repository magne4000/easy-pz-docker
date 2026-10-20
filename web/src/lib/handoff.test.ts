import { beforeEach, describe, expect, it, vi } from "vitest";

// handoff keeps the attempt in module state: load a fresh copy per test.
const load = () => import("./handoff");

beforeEach(() => {
  vi.resetModules();
  window.history.replaceState({ idx: 3 }, "", "/");
});

describe("takeHandoffToken", () => {
  it("reads #t= and clears the fragment, keeping path, query and history state", async () => {
    const { takeHandoffToken } = await load();
    window.history.replaceState({ idx: 3 }, "", "/auth/pelican?x=1#t=abc.def-ghi");
    expect(takeHandoffToken()).toBe("abc.def-ghi");
    expect(window.location.pathname).toBe("/auth/pelican");
    expect(window.location.search).toBe("?x=1");
    expect(window.location.hash).toBe("");
    expect(window.history.state).toEqual({ idx: 3 });
  });

  it("returns empty without a token, still clearing any fragment", async () => {
    const { takeHandoffToken } = await load();
    window.history.replaceState(null, "", "/auth/pelican");
    expect(takeHandoffToken()).toBe("");
    window.history.replaceState(null, "", "/auth/pelican#other=1");
    expect(takeHandoffToken()).toBe("");
    expect(window.location.hash).toBe("");
    window.history.replaceState(null, "", "/auth/pelican#t=");
    expect(takeHandoffToken()).toBe("");
  });

  it("decodes the token like a query value", async () => {
    const { takeHandoffToken } = await load();
    window.history.replaceState(null, "", "/auth/pelican#t=a%2Bb");
    expect(takeHandoffToken()).toBe("a+b");
  });
});

describe("handoffOnce", () => {
  it("posts the token exactly once across repeated calls (StrictMode)", async () => {
    const { handoffOnce } = await load();
    window.history.replaceState(null, "", "/auth/pelican#t=tok");
    const post = vi.fn(async (token: string) => ({ token }));
    const first = handoffOnce(post);
    const second = handoffOnce(post);
    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith("tok");
    expect(second).toBe(first);
    await expect(first).resolves.toEqual({ token: "tok" });
    expect(window.location.hash).toBe("");
  });

  it("shares a failed attempt instead of retrying the spent token", async () => {
    const { handoffOnce } = await load();
    window.history.replaceState(null, "", "/auth/pelican#t=tok");
    const post = vi.fn(() => Promise.reject(new Error("token already used")));
    const first = handoffOnce(post);
    await expect(first).rejects.toThrow("token already used");
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
    await expect(handoffOnce(post)).resolves.toBe("one");
    window.history.replaceState(null, "", "/auth/pelican#t=two");
    await expect(handoffOnce(post)).resolves.toBe("two");
    expect(post).toHaveBeenCalledTimes(2);
  });
});
