import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

test.use({ permissions: ["clipboard-read", "clipboard-write"] });

const file = (name: string) => path.resolve(".", name);
const remove = (name: string) => {
  for (const suffix of ["", ".bak"]) fs.rmSync(file(name + suffix), { force: true });
};
const write = (name: string, content: string) => fs.writeFileSync(file(name), content);

test.beforeEach(() => {
  fs.rmSync(path.resolve(".", ".gickup-trash"), { recursive: true, force: true });
  for (const name of fs.readdirSync(".")) {
    if (name.startsWith("smoke-") && (name.endsWith(".yml") || name.endsWith(".yaml") || name.endsWith(".bak"))) {
      fs.rmSync(path.resolve(".", name), { force: true });
    }
  }
});

test("edits and saves a real configuration created from the example", async ({ page }) => {
  const name = "smoke-main.yml";
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name: "From example" }).click();
  await page.getByLabel("File name").fill(name);
  await page.getByRole("button", { name: "Confirm file action" }).click();
  await expect(page.getByRole("status")).toHaveText("Configuration created.");

  await page.getByLabel("Cron").fill("0 3 * * *");
  await page.getByRole("button", { name: "Review changes" }).first().click();
  await expect(page.locator(".review pre")).toContainText("0 3 * * *");
  await page.getByRole("button", { name: "Confirm save" }).click();
  await expect(page.getByRole("status")).toHaveText("Saved.");

  const saved = fs.readFileSync(file(name), "utf8");
  expect(saved).toContain("cron: 0 3 * * *");
  expect(saved).toContain("# optional - when cron is not provided");

  await page.getByRole("button", { name }).click();
  await expect(page.getByLabel("Cron")).toHaveValue("0 3 * * *");
});

test("keeps masked secrets on disk when saving", async ({ page }) => {
  const name = "smoke-secret-save.yml";
  write(name, "cron: '@daily'\nsource:\n  any:\n    - url: https://one.test\n      token: supersecret\n");
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name }).click();
  await page.getByLabel("Cron").fill("@hourly");
  await page.getByRole("button", { name: "Review changes" }).first().click();
  await page.getByRole("button", { name: "Confirm save" }).click();
  await expect(page.getByRole("status")).toHaveText("Saved.");

  const saved = fs.readFileSync(file(name), "utf8");
  expect(saved).toContain("cron: \"@hourly\"");
  expect(saved).toContain("token: supersecret");
  expect(saved).not.toContain("********");
});

test("edits multiple documents and adds provider items", async ({ page }) => {
  const name = "smoke-multi.yml";
  write(name, "cron: '@daily'\nsource:\n  any:\n    - url: https://one.test\n---\ncron: '@weekly'\nsource:\n  any:\n    - url: https://two.test\n");
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name }).click();

  await page.getByRole("button", { name: "Configuration 2" }).click();
  await page.getByLabel("Cron").fill("@monthly");
  await page.getByText("Provider: any").click();
  await page.getByRole("button", { name: "Add provider item" }).first().click();
  await page.locator("#root_source_any_1_url").fill("https://three.test");

  await page.getByRole("button", { name: "Review changes" }).first().click();
  await expect(page.locator(".review pre")).toContainText("@monthly");
  await page.getByRole("button", { name: "Confirm save" }).click();
  await expect(page.getByRole("status")).toHaveText("Saved.");

  const saved = fs.readFileSync(file(name), "utf8");
  expect(saved).toContain("cron: '@daily'");
  expect(saved).toContain('cron: "@monthly"');
  expect(saved).toContain("https://one.test");
  expect(saved).toContain("https://two.test");
  expect(saved).toContain("https://three.test");
  expect(saved).not.toContain("@weekly");
});

test("adds a third YAML document through the editor", async ({ page }) => {
  const name = "smoke-docs.yml";
  write(name, "cron: '@daily'\n---\ncron: '@weekly'\n");
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name }).click();
  await page.getByRole("button", { name: "Add document" }).click();
  await page.getByRole("button", { name: "Configuration 3" }).click();
  await page.getByLabel("Cron").fill("@hourly");

  await page.getByRole("button", { name: "Review changes" }).first().click();
  await page.getByRole("button", { name: "Confirm save" }).click();
  await expect(page.getByRole("status")).toHaveText("Saved.");

  const saved = fs.readFileSync(file(name), "utf8");
  const documents = saved.split(/^---$/m).map(part => part.trim()).filter(Boolean);
  expect(documents).toHaveLength(3);
  expect(documents[2]).toContain('cron: "@hourly"');
});

test("runs the file workspace end to end", async ({ page }) => {
  const base = "smoke-ws.yml";
  const copy = "smoke-ws-copy.yml";
  const renamed = "smoke-ws-renamed.yml";
  const imported = "smoke-import.yml";
  write(base, "cron: '@daily'\n");
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name: base }).click();

  await page.getByRole("button", { name: "Copy", exact: true }).click();
  await page.getByLabel("File name").fill(copy);
  await page.getByRole("button", { name: "Confirm file action" }).click();
  await expect(page.getByRole("status")).toHaveText("Configuration copied.");
  expect(fs.existsSync(file(copy))).toBe(true);

  await page.getByRole("button", { name: "Rename", exact: true }).click();
  await page.getByLabel("File name").fill(renamed);
  await page.getByRole("button", { name: "Confirm file action" }).click();
  await expect(page.getByRole("status")).toHaveText("Configuration renamed.");
  expect(fs.existsSync(file(renamed))).toBe(true);
  expect(fs.existsSync(file(copy))).toBe(false);

  await page.getByRole("button", { name: "Import configuration" }).click();
  await page.getByLabel("File name").fill(imported);
  await page.getByLabel("Configuration content").fill("cron: '@hourly'\n");
  await page.getByRole("button", { name: "Confirm file action" }).click();
  await expect(page.getByRole("status")).toHaveText("Configuration imported.");
  expect(fs.readFileSync(file(imported), "utf8")).toContain("@hourly");

  await page.getByRole("button", { name: imported }).click();
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Moved to trash.");
  expect(fs.existsSync(file(imported))).toBe(false);

  await page.getByRole("button", { name: "Trash", exact: true }).click();
  await page.getByRole("button", { name: `Restore ${imported}` }).click();
  await expect(page.getByRole("status")).toHaveText("Configuration restored from trash.");
  expect(fs.existsSync(file(imported))).toBe(true);

  await page.getByRole("button", { name: imported }).click();
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Moved to trash.");
  await page.getByRole("button", { name: "Trash", exact: true }).click();
  await page.getByRole("button", { name: "Delete forever" }).first().click();
  await expect(page.getByRole("status")).toHaveText("Deleted permanently.");
});

test("reviews and restores a real backup", async ({ page }) => {
  const name = "smoke-backup.yml";
  write(name, "# keep\ncron: '@daily'\n");
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name }).click();
  await page.getByLabel("Cron").fill("@hourly");
  await page.getByRole("button", { name: "Review changes" }).first().click();
  await page.getByRole("button", { name: "Confirm save" }).click();
  await expect(page.getByRole("status")).toHaveText("Saved.");

  await page.getByRole("button", { name: "Backup", exact: true }).click();
  await expect(page.locator(".backup pre")).toContainText("@daily");
  await page.getByRole("button", { name: "Restore backup" }).click();
  await expect(page.getByRole("status")).toHaveText("Backup restored.");
  expect(fs.readFileSync(file(name), "utf8")).toContain("@daily");
  expect(fs.readFileSync(file(name), "utf8")).toContain("# keep");
});

test("detects an external change while editing and reloads", async ({ page }) => {
  const name = "smoke-external.yml";
  write(name, "cron: '@daily'\n");
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name }).click();
  await page.getByLabel("Cron").fill("@hourly");

  write(name, "cron: '@weekly'\n");
  await expect(page.getByRole("alert")).toContainText("changed on disk", { timeout: 15000 });
  await page.getByRole("button", { name: "View differences" }).click();
  await expect(page.getByText("Current disk version")).toBeVisible();
  await page.getByRole("button", { name: "Reload from disk" }).click();
  await expect(page.getByRole("status")).toHaveText("Reloaded from disk.");
  await expect(page.getByLabel("Cron")).toHaveValue("@weekly");
});

test("reveals real secrets on demand", async ({ page }) => {
  const name = "smoke-secret.yml";
  write(name, "source:\n  any:\n    - url: https://one.test\n      token: supersecret\n");

  await page.goto("/");
  await page.getByRole("button", { name }).click();
  await page.getByText("Read-only YAML").click();
  const preview = page.locator("details", { hasText: "Read-only YAML" }).locator("pre");
  await expect(preview).toContainText("********");
  await expect(preview).not.toContainText("supersecret");
  await page.getByRole("button", { name: "Reveal secrets" }).first().click();
  await expect(preview).toContainText("supersecret");
  await page.getByRole("button", { name: "Hide secrets" }).first().click();
  await expect(preview).not.toContainText("supersecret");
});

test("blocks invalid drafts and lets you take them away", async ({ page }) => {
  const name = "smoke-invalid.yml";
  write(name, "cron: '@daily'\n");
  page.on("dialog", dialog => dialog.accept());

  await page.goto("/");
  await page.getByRole("button", { name }).click();
  await page.getByLabel("Cron").fill("not a cron");
  await page.getByRole("button", { name: "Review changes" }).first().click();
  await expect(page.getByText(/invalid cron/i)).toBeVisible();
  await expect(page.getByRole("button", { name: "Confirm save" })).toBeDisabled();

  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download draft" }).click();
  expect((await download).suggestedFilename()).toBe(`${name}.draft.yml`);

  await page.getByRole("button", { name: "Copy draft" }).click();
  await expect(page.getByRole("status")).toHaveText("Draft copied to clipboard.");
  const clipboard = await page.evaluate(() => navigator.clipboard.readText());
  expect(clipboard).toContain("not a cron");
  expect(fs.readFileSync(file(name), "utf8")).toBe("cron: '@daily'\n");
});

test("shows cron guidance and collapsible advanced sections with the real schema", async ({ page }) => {
  const name = "smoke-sections.yml";
  write(name, "cron: '@daily'\nmetrics:\n  prometheus:\n    listen_addr: ':6178'\n");
  await page.goto("/");
  await page.getByRole("button", { name }).click();

  await expect(page.getByText("Five-field cron expression", { exact: false })).toBeVisible();
  const advanced = page.locator("details.advanced-field");
  await expect(advanced.filter({ hasText: "metrics" }).first()).not.toHaveAttribute("open", "");
  await advanced.filter({ hasText: "metrics" }).first().locator("summary").click();
  await expect(advanced.filter({ hasText: "metrics" }).first()).toHaveAttribute("open", "");
});
