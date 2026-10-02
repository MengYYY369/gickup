import { expect, test } from "@playwright/test";

test.use({ permissions: ["clipboard-read", "clipboard-write"] });

const schema = {
  type: "object",
  properties: {
    cron: { type: "string", title: "Cron" },
  },
};

function opened(version = "v1") {
  return {
    documents: [{ cron: "@daily" }],
    schema,
    uiSchema: {},
    yaml: { source: "cron: '@daily'\n" },
    version,
    warnings: [],
  };
}

test.beforeEach(async ({ page }) => {
  await page.route("**/api/v1/configs", async route => {
    await route.fulfill({ json: { configs: [{ name: "alpha.yml", documents: 1, modified: "", valid: true }] } });
  });
  await page.route("**/api/v1/configs/alpha.yml", async route => {
    if (route.request().method() === "GET") {
      await route.fulfill({ json: opened() });
      return;
    }
    await route.fulfill({ json: { version: "v2" } });
  });
});

test("opens, edits, reviews the diff, and confirms save", async ({ page }) => {
  await page.route("**/api/v1/configs/alpha.yml/review", async route => {
    await route.fulfill({ json: { diff: "-cron: '@daily'\n+cron: '@hourly'", yaml: "cron: '@hourly'\n", valid: true, errors: [] } });
  });
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await page.getByLabel("Cron").fill("@hourly");
  await page.getByRole("button", { name: "Review changes" }).first().click();
  await expect(page.getByText("Save review")).toBeVisible();
  await expect(page.locator(".review pre")).toContainText("+cron: '@hourly'");
  await page.getByRole("button", { name: "Confirm save" }).click();
  await expect(page.getByRole("status")).toHaveText("Saved.");
});

test("blocks an invalid reviewed configuration", async ({ page }) => {
  await page.route("**/api/v1/configs/alpha.yml/review", async route => {
    await route.fulfill({ json: { diff: "diff", yaml: "", valid: false, errors: ["cron is invalid"] } });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await page.getByLabel("Cron").fill("invalid");
  await page.getByRole("button", { name: "Review changes" }).first().click();
  await expect(page.getByText("cron is invalid")).toBeVisible();
  await expect(page.getByRole("button", { name: "Confirm save" })).toBeDisabled();
});

test("reports an external edit conflict", async ({ page }) => {
  await page.route("**/api/v1/configs/alpha.yml/review", async route => {
    await route.fulfill({ json: { diff: "diff", yaml: "cron: '@hourly'\n", valid: true, errors: [] } });
  });
  await page.route("**/api/v1/configs/alpha.yml", async route => {
    if (route.request().method() === "PATCH") {
      await route.fulfill({ status: 409, body: "configuration changed" });
      return;
    }
    await route.fulfill({ json: opened() });
  });
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await page.getByLabel("Cron").fill("@hourly");
  await page.getByRole("button", { name: "Review changes" }).first().click();
  await page.getByRole("button", { name: "Confirm save" }).click();
  await expect(page.getByRole("status")).toContainText("changed on disk");
});

test("reviews and restores a backup", async ({ page }) => {
  await page.route("**/api/v1/configs/alpha.yml/backup", async route => {
    await route.fulfill({ json: { current: "cron: '@hourly'\n", backup: "cron: '@daily'\n", diff: "-@daily\n+@hourly" } });
  });
  await page.route("**/api/v1/configs/alpha.yml/backup/restore", async route => {
    await route.fulfill({ json: { restored: true } });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await page.getByRole("button", { name: "Backup" }).click();
  await expect(page.getByText("-@daily")).toBeVisible();
  await page.getByRole("button", { name: "Restore backup" }).click();
  await expect(page.getByRole("status")).toContainText("restored");
});

test("lists and restores a trashed configuration", async ({ page }) => {
  await page.route("**/api/v1/trash", async route => {
    await route.fulfill({ json: { trash: [{ id: "trash-1", original: "deleted.yml" }] } });
  });
  await page.route("**/api/v1/trash/trash-1/restore", async route => {
    await route.fulfill({ json: { restored: true } });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "Trash", exact: true }).click();
  await expect(page.getByText("deleted.yml", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Restore deleted.yml" }).click();
  await expect(page.getByRole("status")).toContainText("restored");
});

test("creates a configuration from the example template", async ({ page }) => {
  let created: Record<string, unknown> = {};
  await page.route("**/api/v1/configs", async route => {
    if (route.request().method() === "POST") {
      created = route.request().postDataJSON();
      await route.fulfill({ status: 201, body: "" });
      return;
    }
    await route.fulfill({ json: { configs: [{ name: "alpha.yml", documents: 1, modified: "", valid: true }] } });
  });
  await page.route("**/api/v1/configs/beta.yml", async route => {
    await route.fulfill({ json: opened("v2") });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "From example" }).click();
  await page.getByLabel("File name").fill("beta.yml");
  await page.getByRole("button", { name: "Confirm file action" }).click();

  await expect(page.getByRole("status")).toHaveText("Configuration created.");
  expect(created).toEqual({ name: "beta.yml", template: "example" });
});

test("rejects an invalid configuration file name", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "New configuration" }).click();
  await page.getByLabel("File name").fill("broken.txt");
  await page.getByRole("button", { name: "Confirm file action" }).click();

  await expect(page.getByRole("status")).toContainText(".yml");
});

test("renames a configuration through the form", async ({ page }) => {
  let renamed: Record<string, unknown> = {};
  await page.route("**/api/v1/configs/alpha.yml/rename", async route => {
    renamed = route.request().postDataJSON();
    await route.fulfill({ status: 200, body: "" });
  });
  await page.route("**/api/v1/configs/beta.yml", async route => {
    await route.fulfill({ json: opened("v2") });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await page.getByRole("button", { name: "Rename", exact: true }).click();
  await page.getByLabel("File name").fill("beta.yml");
  await page.getByRole("button", { name: "Confirm file action" }).click();

  await expect(page.getByRole("status")).toHaveText("Configuration renamed.");
  expect(renamed).toEqual({ name: "beta.yml" });
});

test("copies a configuration through the form", async ({ page }) => {
  let copied: Record<string, unknown> = {};
  await page.route("**/api/v1/configs/alpha.yml/copy", async route => {
    copied = route.request().postDataJSON();
    await route.fulfill({ status: 201, body: "" });
  });
  await page.route("**/api/v1/configs/copy-alpha.yml", async route => {
    await route.fulfill({ json: opened("v3") });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await page.getByRole("button", { name: "Copy", exact: true }).click();
  await page.getByRole("button", { name: "Confirm file action" }).click();

  await expect(page.getByRole("status")).toHaveText("Configuration copied.");
  expect(copied).toEqual({ name: "copy-alpha.yml" });
});

test("imports configuration content", async ({ page }) => {
  let imported: Record<string, unknown> = {};
  await page.route("**/api/v1/configs/import", async route => {
    imported = route.request().postDataJSON();
    await route.fulfill({ status: 201, body: "" });
  });
  await page.route("**/api/v1/configs/gamma.yml", async route => {
    await route.fulfill({ json: opened("v3") });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "Import configuration" }).click();
  await page.getByLabel("File name").fill("gamma.yml");
  await page.getByLabel("Configuration content").fill("cron: '@hourly'\n");
  await page.getByRole("button", { name: "Confirm file action" }).click();

  await expect(page.getByRole("status")).toHaveText("Configuration imported.");
  expect(imported).toEqual({ name: "gamma.yml", content: "cron: '@hourly'\n" });
});

test("moves a configuration to trash and deletes it forever", async ({ page }) => {
  const requests: string[] = [];
  await page.route("**/api/v1/configs/alpha.yml", async route => {
    if (route.request().method() === "DELETE") {
      requests.push("delete-config");
      await route.fulfill({ status: 200, body: "" });
      return;
    }
    await route.fulfill({ json: opened() });
  });
  await page.route("**/api/v1/trash", async route => {
    await route.fulfill({ json: { trash: [{ id: "trash-1", original: "deleted.yml" }] } });
  });
  await page.route("**/api/v1/trash/trash-1", async route => {
    requests.push(`${String(route.request().method()).toLowerCase()}-trash`);
    await route.fulfill({ status: 200, body: "" });
  });
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Moved to trash.");

  await page.getByRole("button", { name: "Trash", exact: true }).click();
  await page.getByRole("button", { name: "Delete forever" }).click();
  await expect(page.getByRole("status")).toHaveText("Deleted permanently.");
  expect(requests).toEqual(["delete-config", "delete-trash"]);
});

test("warns when the file changes on disk while editing", async ({ page }) => {
  let version = "v1";
  await page.route("**/api/v1/configs/alpha.yml", async route => {
    if (route.request().method() === "GET") {
      await route.fulfill({
        json: {
          ...opened(version),
          yaml: { source: version === "v1" ? "cron: '@daily'\n" : "cron: '@weekly'\n" },
        },
      });
      return;
    }
    await route.fulfill({ json: {} });
  });
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await page.getByLabel("Cron").fill("@hourly");
  version = "v2";

  await expect(page.getByRole("alert")).toContainText("changed on disk", { timeout: 10000 });
  await page.getByRole("button", { name: "View differences" }).click();
  await expect(page.getByText("Current disk version")).toBeVisible();
  await page.getByRole("button", { name: "Reload from disk" }).click();
  await expect(page.getByRole("status")).toHaveText("Reloaded from disk.");
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("shows cron guidance and collapses advanced sections", async ({ page }) => {
  const schemaWithAdvanced = {
    type: "object",
    properties: {
      cron: { type: "string", title: "Cron" },
      metrics: { type: "object", title: "Metrics", properties: { listen_addr: { type: "string", title: "Listen address" } } },
    },
  };
  await page.route("**/api/v1/configs/alpha.yml", async route => {
    await route.fulfill({ json: { ...opened(), schema: schemaWithAdvanced } });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await expect(page.getByText("Five-field cron expression", { exact: false })).toBeVisible();

  const advanced = page.locator("details.advanced-field");
  await expect(advanced.locator("input#root_metrics_listen_addr")).not.toBeVisible();
  await advanced.locator("summary").click();
  await expect(advanced.locator("input#root_metrics_listen_addr")).toBeVisible();
});

test("reveals secrets in the read-only YAML on demand", async ({ page }) => {
  const masked = { ...opened(), yaml: { source: "source:\n  github:\n    - token: ********\n" } };
  await page.route("**/api/v1/configs/alpha.yml", async route => {
    if (route.request().method() === "GET") {
      await route.fulfill({ json: masked });
      return;
    }
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/configs/alpha.yml/raw", async route => {
    await route.fulfill({ json: { yaml: "source:\n  github:\n    - token: supersecret\n" } });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await page.getByText("Read-only YAML").click();
  const preview = page.locator("details", { hasText: "Read-only YAML" }).locator("pre");
  await expect(preview).toContainText("********");

  await page.getByRole("button", { name: "Reveal secrets" }).first().click();
  await expect(preview).toContainText("supersecret");

  await page.getByRole("button", { name: "Hide secrets" }).first().click();
  await expect(preview).toContainText("********");
});

test("offers draft download and copy when validation fails", async ({ page }) => {
  await page.route("**/api/v1/configs/alpha.yml/review", async route => {
    await route.fulfill({ json: { diff: "-cron: '@daily'\n+cron: invalid", yaml: "cron: invalid\n", valid: false, errors: ["cron is invalid"] } });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "alpha.yml" }).click();
  await page.getByLabel("Cron").fill("invalid");
  await page.getByRole("button", { name: "Review changes" }).first().click();
  await expect(page.getByText("cron is invalid")).toBeVisible();

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download draft" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("alpha.yml.draft.yml");

  await page.getByRole("button", { name: "Copy draft" }).click();
  await expect(page.getByRole("status")).toHaveText("Draft copied to clipboard.");
  const clipboard = await page.evaluate(() => navigator.clipboard.readText());
  expect(clipboard).toContain("cron: invalid");
});
