import { describe, expect, it } from "vitest";
import { modsPageHref } from "./modsPage";

describe("modsPageHref", () => {
  it("opens through the panel on Pelican", () => {
    expect(
      modsPageHref({
        host: "pelican",
        publicUrl: "https://panel.example.com/easy-pz/abcd1234",
        modsPagePath: "/mods/tok/",
      }),
    ).toBe("https://panel.example.com/easy-pz/abcd1234/mods/tok/");
  });

  it("stays on this origin in Docker mode", () => {
    expect(
      modsPageHref({ host: "docker", publicUrl: "https://pz.example.com", modsPagePath: "/mods/tok/" }),
    ).toBe("/mods/tok/");
  });

  it("is empty when the page is off", () => {
    expect(modsPageHref({ host: "pelican", publicUrl: "https://p", modsPagePath: "" })).toBe("");
  });
});
