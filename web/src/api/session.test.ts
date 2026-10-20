import { afterEach, describe, expect, it, vi } from "vitest";
import { renewURL, sessionToken, setSessionToken, startRenewal } from "./session";

afterEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  vi.restoreAllMocks();
});

describe("session token", () => {
  it("lives in localStorage (per origin, so per port)", () => {
    expect(sessionToken()).toBeNull();
    setSessionToken("abc");
    expect(localStorage.getItem("pzman.session")).toBe("abc");
    expect(sessionToken()).toBe("abc");
    setSessionToken(null);
    expect(sessionToken()).toBeNull();
  });

  it("degrades when storage is unavailable", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("denied", "SecurityError");
    });
    expect(sessionToken()).toBeNull();
  });
});

describe("renewal", () => {
  const loc = (path: string) => ({ pathname: path, search: "?a=1", assign: vi.fn() }) as unknown as Location;

  it("goes to the panel's Open route with the current page as `to`", () => {
    expect(renewURL("https://panel.example.com/easy-pz/abcd1234/open", "/mods?a=1")).toBe(
      "https://panel.example.com/easy-pz/abcd1234/open?to=%2Fmods%3Fa%3D1",
    );
    setSessionToken("ended");
    const l = loc("/mods");
    expect(startRenewal("https://p/easy-pz/abcd1234/open", l)).toBe(true);
    expect(l.assign).toHaveBeenCalledWith("https://p/easy-pz/abcd1234/open?to=%2Fmods%3Fa%3D1");
    expect(sessionToken(), "the ended session is dropped").toBeNull();
  });

  it("does not loop: a second renewal right after the first gives up", () => {
    expect(startRenewal("https://p/open", loc("/"))).toBe(true);
    const l = loc("/");
    expect(startRenewal("https://p/open", l)).toBe(false);
    expect(l.assign).not.toHaveBeenCalled();
  });
});
