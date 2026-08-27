import { describe, expect, it } from "vitest";
import { exportConfig, getBackup, getConfig, listConfigs, listTrash, restoreBackup, restoreTrash, reviewConfig, saveConfig } from "./api";

describe("listConfigs", () => {
  it("returns the configuration list from the versioned API", async () => {
    const fetcher = async () => new Response(JSON.stringify({ configs: [{ name: "alpha.yml" }] }));

    await expect(listConfigs(fetcher)).resolves.toEqual([{ name: "alpha.yml" }]);
  });

  it("requests the versioned configuration endpoint", async () => {
    let requested = "";
    const fetcher = async (input: RequestInfo | URL) => {
      requested = String(input);
      return new Response(JSON.stringify({ configs: [] }));
    };

    await listConfigs(fetcher);

    expect(requested).toBe("/api/v1/configs");
  });
});

describe("getConfig", () => {
  it("opens a named configuration through the versioned API", async () => {
    let requested = "";
    const fetcher = async (input: RequestInfo | URL) => {
      requested = String(input);
      return new Response(JSON.stringify({ version: "v1", documents: [] }));
    };

    await expect(getConfig("alpha.yml", fetcher)).resolves.toEqual({ version: "v1", documents: [] });
    expect(requested).toBe("/api/v1/configs/alpha.yml");
  });
});

describe("exportConfig", () => {
  it("downloads the original named configuration through the export endpoint", async () => {
    let requested = "";
    const fetcher = async (input: RequestInfo | URL) => {
      requested = String(input);
      return new Response("cron: '@daily'\r\n");
    };

    await expect(exportConfig("alpha.yml", fetcher)).resolves.toBe("cron: '@daily'\r\n");
    expect(requested).toBe("/api/v1/configs/alpha.yml/export");
  });
});

describe("reviewConfig", () => {
  it("submits a draft for validation and diff review", async () => {
    let requested = "";
    let init: RequestInit | undefined;
    const fetcher = async (input: RequestInfo | URL, options?: RequestInit) => {
      requested = String(input);
      init = options;
      return new Response(JSON.stringify({ valid: true, diff: "diff" }));
    };

    await expect(reviewConfig("alpha.yml", { version: "v1", operations: [] }, fetcher)).resolves.toEqual({ valid: true, diff: "diff" });
    expect(requested).toBe("/api/v1/configs/alpha.yml/review");
    expect(init?.method).toBe("POST");
  });
});

describe("saveConfig", () => {
  it("submits a confirmed draft to the versioned configuration endpoint", async () => {
    let requested = "";
    let init: RequestInit | undefined;
    const fetcher = async (input: RequestInfo | URL, options?: RequestInit) => {
      requested = String(input);
      init = options;
      return new Response(JSON.stringify({ version: "v2" }));
    };

    await expect(saveConfig("alpha.yml", { version: "v1", confirmed: true, operations: [] }, fetcher)).resolves.toEqual({ version: "v2" });
    expect(requested).toBe("/api/v1/configs/alpha.yml");
    expect(init?.method).toBe("PATCH");
  });
});

describe("recovery APIs", () => {
  it("lists trash entries", async () => {
    let requested = "";
    const fetcher = async (input: RequestInfo | URL) => {
      requested = String(input);
      return new Response(JSON.stringify({ trash: [{ id: "entry", original: "alpha.yml" }] }));
    };

    await expect(listTrash(fetcher)).resolves.toEqual([{ id: "entry", original: "alpha.yml" }]);
    expect(requested).toBe("/api/v1/trash");
  });

  it("restores a trash entry", async () => {
    let requested = "";
    let init: RequestInit | undefined;
    const fetcher = async (input: RequestInfo | URL, options?: RequestInit) => {
      requested = String(input);
      init = options;
      return new Response(JSON.stringify({ restored: true }));
    };

    await restoreTrash("entry / one", fetcher);
    expect(requested).toBe("/api/v1/trash/entry%20%2F%20one/restore");
    expect(init?.method).toBe("POST");
  });

  it("loads a backup diff and restores it", async () => {
    const requests: Array<{ url: string; method?: string }> = [];
    const fetcher = async (input: RequestInfo | URL, options?: RequestInit) => {
      requests.push({ url: String(input), method: options?.method });
      return new Response(JSON.stringify({ diff: "diff" }));
    };

    await expect(getBackup("alpha.yml", fetcher)).resolves.toEqual({ diff: "diff" });
    await restoreBackup("alpha.yml", fetcher);
    expect(requests).toEqual([
      { url: "/api/v1/configs/alpha.yml/backup", method: undefined },
      { url: "/api/v1/configs/alpha.yml/backup/restore", method: "POST" },
    ]);
  });
});
