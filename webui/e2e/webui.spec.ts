import { expect, test } from "@playwright/test";

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
  await page.getByRole("button", { name: "Trash" }).click();
  await expect(page.getByText("deleted.yml", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Restore deleted.yml" }).click();
  await expect(page.getByRole("status")).toContainText("restored");
});
