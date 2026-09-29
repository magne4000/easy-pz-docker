import { describe, expect, it } from "vitest";
import { posixCommand, powershellCommand } from "./ModsPage";

const d = { modsLine: "ModA;Mod B;Brita_2", mapLine: "RavenCreek;Muldraugh, KY", iniName: "Guyverotte.ini" };

describe("config one-liners", () => {
  it("POSIX sed keeps a .bak and rewrites the three keys", () => {
    const cmd = posixCommand(d);
    expect(cmd).toContain("sed -i.bak -E");
    expect(cmd).toContain("s|^Mods=.*|Mods=ModA;Mod B;Brita_2|");
    expect(cmd).toContain("s|^WorkshopItems=.*|WorkshopItems=|");
    expect(cmd).toContain("'Server/Guyverotte.ini'");
  });

  it("escapes sed metacharacters and quotes", () => {
    const cmd = posixCommand({ ...d, modsLine: "A|B&C'D" });
    expect(cmd).toContain("Mods=A\\|B\\&C'\\''D");
  });

  it("PowerShell backs up, then rewrites", () => {
    const cmd = powershellCommand(d);
    expect(cmd.startsWith("Copy-Item 'Server\\Guyverotte.ini' 'Server\\Guyverotte.ini.bak';")).toBe(true);
    expect(cmd).toContain("-replace '^Mods=.*','Mods=ModA;Mod B;Brita_2'");
  });

  it("escapes $ so PowerShell -replace does not treat it as a group", () => {
    expect(powershellCommand({ ...d, modsLine: "A$1" })).toContain("Mods=A$$1");
  });
});
