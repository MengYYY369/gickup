import { describe, expect, it } from "vitest";
import React from "react";

// RED: the real application must expose the workspace and recovery controls,
// not only operation-builder helpers.
import { renderToStaticMarkup } from "react-dom/server";

describe("WebUI editor", () => {
  it("renders lossless multi-document and ordered-collection controls", async () => {
    const { EditorControls } = await import("./main");
    const html = renderToStaticMarkup(<EditorControls documents={[{ source: { github: [{}] }, destination: { local: [{}] } }]} activeDocument={0} warnings={[]} />);

    expect(html).toContain("Configuration 1");
    expect(html).toContain("Source: github");
    expect(html).toContain("Destination: local");
    expect(html).toContain("Add document");
    expect(html).toContain("Copy document");
    expect(html).toContain("Delete document");
    expect(html).toContain("Move document up");
    expect(html).toContain("Move document down");
    expect(html).toContain("Provider");
    expect(html).toContain("Advanced fields");
  });

  it("renders unknown-field warnings with document and field locations", async () => {
    const { EditorControls } = await import("./main");
    const html = renderToStaticMarkup(<EditorControls documents={[{}]} activeDocument={0} warnings={[{ document: 0, path: "source.future_hoster", message: "Unknown field" }]} />);

    expect(html).toContain("Configuration 1");
    expect(html).toContain("source.future_hoster");
    expect(html).toContain("Unknown field");
  });
  it("renders file workspace and recovery actions in the real App component", async () => {
    const { App } = await import("./main");
    const html = renderToStaticMarkup(<App />);

    expect(html).toContain("New configuration");
    expect(html).toContain("Import configuration");
    expect(html).toContain("Trash");
    expect(html).toContain("Rename");
    expect(html).toContain("Copy");
    expect(html).toContain("Export");
    expect(html).toContain("Delete");
  });
  it("exposes the application component for component-level tests", async () => {
    const module = await import("./main");
    expect(module).toHaveProperty("App");
    expect(module).toHaveProperty("exportDownload");
  });

  it("exports testable document operations for add, copy, move, and delete", async () => {
    const module = await import("./main");
    expect(module.addDocumentOperation({ cron: "@hourly" })).toEqual({
      op: "add-document",
      value: { cron: "@hourly" },
    });
    expect(module.deleteDocumentOperation(1)).toEqual({ op: "delete-document", index: 1 });
  });

  it("builds explicit document collection operations", async () => {
    const module = await import("./main");
    expect(module.addDocumentOperation({ cron: "@daily" })).toEqual({ op: "add-document", value: { cron: "@daily" } });
    expect(module.copyDocumentOperation(1)).toEqual({ op: "copy-document", index: 1 });
    expect(module.moveDocumentOperation(2, 0)).toEqual({ op: "move-document", from: 2, to: 0 });
    expect(module.deleteDocumentOperation(1)).toEqual({ op: "delete-document", index: 1 });
  });

  it("applies add, copy, delete, move-up, and move-down document behavior", async () => {
    const { applyDocumentOperation } = await import("./main");
    const documents = [{ name: "first" }, { name: "second" }, { name: "third" }];

    expect(applyDocumentOperation(documents, { op: "add-document", value: { name: "fourth" } })).toEqual([
      { name: "first" }, { name: "second" }, { name: "third" }, { name: "fourth" },
    ]);
    expect(applyDocumentOperation(documents, { op: "copy-document", index: 1 })).toEqual([
      { name: "first" }, { name: "second" }, { name: "second" }, { name: "third" },
    ]);
    expect(applyDocumentOperation(documents, { op: "delete-document", index: 1 })).toEqual([
      { name: "first" }, { name: "third" },
    ]);
    expect(applyDocumentOperation(documents, { op: "move-document", from: 1, to: 0 })).toEqual([
      { name: "second" }, { name: "first" }, { name: "third" },
    ]);
    expect(applyDocumentOperation(documents, { op: "move-document", from: 1, to: 2 })).toEqual([
      { name: "first" }, { name: "third" }, { name: "second" },
    ]);
    expect(documents).toEqual([{ name: "first" }, { name: "second" }, { name: "third" }]);
  });

  it("integrates document controls with the real App", async () => {
    const { App } = await import("./main");
    const html = renderToStaticMarkup(<App />);

    expect(html).toContain("editor-controls");
  });

  it("builds ordered array operations", async () => {
    const module = await import("./main");
    expect(module.addArrayOperation(0, "source.any", { url: "https://example.test" })).toEqual({ op: "add-array", document: 0, path: "source.any", value: { url: "https://example.test" } });
    expect(module.copyArrayOperation(0, "source.any", 1)).toEqual({ op: "copy-array", document: 0, path: "source.any", index: 1 });
    expect(module.moveArrayOperation(0, "source.any", 2, 0)).toEqual({ op: "move-array", document: 0, path: "source.any", from: 2, to: 0 });
    expect(module.deleteArrayOperation(0, "source.any", 1)).toEqual({ op: "delete-array", document: 0, path: "source.any", index: 1 });
  });

  it("renders actionable ordered provider arrays as collapsed cards", async () => {
    const { EditorControls } = await import("./main");
    const html = renderToStaticMarkup(<EditorControls documents={[{ source: { github: [{ token: "secret" }, { token: "second" }] } }]} activeDocument={0} warnings={[]} />);

    expect(html).toContain("Add provider item");
    expect(html).toContain("Delete provider item");
    expect(html).toContain("Move provider item up");
    expect(html).toContain("Move provider item down");
    expect(html).toContain("draggable=\"true\"");
    expect(html).toContain("<details");
    expect(html).not.toContain("<details open=\"\"");
  });

  it("wires HTML5 drag events to reorder provider items", async () => {
    const { EditorControls } = await import("./main");
    const operations: Record<string, unknown>[] = [];
    const tree = EditorControls({
      documents: [{ source: { github: [{ name: "first" }, { name: "second" }] } }],
      activeDocument: 0,
      warnings: [],
      onOperation: operation => operations.push(operation),
    });
    const articles: React.ReactElement[] = [];
    function visit(node: React.ReactNode) {
      React.Children.forEach(node, child => {
        if (!React.isValidElement(child)) return;
        if (child.type === "article" && child.props.draggable) articles.push(child);
        visit(child.props.children);
      });
    }
    visit(tree);

    const dataTransfer = { setData: () => undefined, getData: () => "0", effectAllowed: "none", dropEffect: "none" };
    articles[0].props.onDragStart({ dataTransfer });
    let prevented = false;
    articles[1].props.onDragOver({ preventDefault: () => { prevented = true; }, dataTransfer });
    articles[1].props.onDrop({ preventDefault: () => undefined, dataTransfer });

    expect(prevented).toBe(true);
    expect(operations).toContainEqual({ op: "move-array", document: 0, path: "source.github", from: 0, to: 1 });
  });

  it("deletes YAML keys when optional fields are cleared", async () => {
    const { documentOperations } = await import("./main");
    expect(documentOperations(
      { cron: "@daily", source: { github: [{ token: "secret" }] } },
      { cron: "", source: { github: [{ token: "" }] } },
      0,
    )).toEqual([
      { op: "delete-field", document: 0, path: "cron" },
      { op: "delete-field", document: 0, path: "source.github.0.token" },
    ]);
  });

  it("builds file workspace API clients", async () => {
    const api = await import("./api");
    expect(api.createConfig).toBeTypeOf("function");
    expect(api.renameConfig).toBeTypeOf("function");
    expect(api.copyConfig).toBeTypeOf("function");
    expect(api.importConfig).toBeTypeOf("function");
    expect(api.deleteConfig).toBeTypeOf("function");
  });

  it("exposes real file workspace actions for testable interaction flows", async () => {
    const module = await import("./main");
    expect(module).toHaveProperty("createWorkspaceConfig");
    expect(module).toHaveProperty("renameWorkspaceConfig");
    expect(module).toHaveProperty("copyWorkspaceConfig");
    expect(module).toHaveProperty("importWorkspaceConfig");
    expect(module).toHaveProperty("deleteWorkspaceConfig");
  });

  it("writes changed scalar values as set-field operations", async () => {
    const { documentOperations } = await import("./main");
    expect(documentOperations(
      { cron: "@daily", source: { github: [{ token: "********", url: "https://one.test" }] } },
      { cron: "@hourly", source: { github: [{ token: "********", url: "https://two.test" }] } },
      1,
    )).toEqual([
      { op: "set-field", document: 1, path: "cron", value: "@hourly" },
      { op: "set-field", document: 1, path: "source.github.0.url", value: "https://two.test" },
    ]);
  });

  it("creates newly added keys and array items", async () => {
    const { documentOperations } = await import("./main");
    expect(documentOperations(
      { source: { github: [{ url: "https://one.test" }] } },
      { source: { github: [{ url: "https://one.test" }, { url: "https://two.test" }] }, log: { level: "debug" } },
      0,
    )).toEqual([
      { op: "add-array", document: 0, path: "source.github", value: { url: "https://two.test" } },
      { op: "set-field", document: 0, path: "log", value: { level: "debug" } },
    ]);
  });

  it("removes trailing array items through delete-array operations", async () => {
    const { documentOperations } = await import("./main");
    expect(documentOperations(
      { source: { any: [{ url: "one" }, { url: "two" }, { url: "three" }] } },
      { source: { any: [{ url: "one" }] } },
      0,
    )).toEqual([
      { op: "delete-array", document: 0, path: "source.any", index: 2 },
      { op: "delete-array", document: 0, path: "source.any", index: 1 },
    ]);
  });

  it("leaves masked sensitive values untouched unless they change", async () => {
    const { documentOperations } = await import("./main");
    expect(documentOperations(
      { source: { github: [{ token: "********" }] } },
      { source: { github: [{ token: "********" }] } },
      0,
    )).toEqual([]);
    expect(documentOperations(
      { source: { github: [{ token: "********" }] } },
      { source: { github: [{ token: "new-token" }] } },
      0,
    )).toEqual([{ op: "set-field", document: 0, path: "source.github.0.token", value: "new-token" }]);
    expect(documentOperations(
      { source: { github: [{ token: "********" }] } },
      { source: { github: [{ token: "" }] } },
      0,
    )).toEqual([{ op: "delete-field", document: 0, path: "source.github.0.token" }]);
  });

  it("stages pending document operations before field diffs", async () => {
    const { pendingOperations } = await import("./main");
    const base = [{ cron: "@daily" }, { cron: "@weekly" }];
    expect(pendingOperations(base, [{ op: "copy-document", index: 0 }], [
      { cron: "@daily" }, { cron: "@monthly" }, { cron: "@weekly" },
    ])).toEqual([
      { op: "copy-document", index: 0 },
      { op: "set-field", document: 1, path: "cron", value: "@monthly" },
    ]);
    expect(base).toEqual([{ cron: "@daily" }, { cron: "@weekly" }]);
  });

  it("applies ordered array operations to a document copy", async () => {
    const { applyArrayOperation } = await import("./main");
    const documents = [{ source: { any: [{ url: "one" }, { url: "two" }] } }];
    const moved = applyArrayOperation(documents, { op: "move-array", document: 0, path: "source.any", from: 0, to: 1 });
    expect(moved).toEqual([{ source: { any: [{ url: "two" }, { url: "one" }] } }]);
    const added = applyArrayOperation(moved, { op: "add-array", document: 0, path: "source.any", value: { url: "three" } });
    expect(added).toEqual([{ source: { any: [{ url: "two" }, { url: "one" }, { url: "three" }] } }]);
    const removed = applyArrayOperation(added, { op: "delete-array", document: 0, path: "source.any", index: 0 });
    expect(removed).toEqual([{ source: { any: [{ url: "one" }, { url: "three" }] } }]);
    expect(documents).toEqual([{ source: { any: [{ url: "one" }, { url: "two" }] } }]);
  });

  it("validates configuration file names", async () => {
    const { configNameError } = await import("./main");
    expect(configNameError("conf.yml")).toBeNull();
    expect(configNameError("nested.yaml")).toBeNull();
    expect(configNameError("")).toMatch(/file name/i);
    expect(configNameError("   ")).toMatch(/file name/i);
    expect(configNameError(".hidden.yml")).toMatch(/dot/i);
    expect(configNameError("sub/conf.yml")).toMatch(/slash/i);
    expect(configNameError("sub\\conf.yml")).toMatch(/slash/i);
    expect(configNameError("conf.txt")).toMatch(/\.ya?ml/i);
  });

  it("offers blank, example, and import entries in the sidebar", async () => {
    const { App } = await import("./main");
    const html = renderToStaticMarkup(<App />);
    expect(html).toContain("New configuration");
    expect(html).toContain("From example");
    expect(html).toContain("Import configuration");
    expect(html).toContain("Trash");
  });

  it("builds the editor ui schema with the cron widget and advanced sections", async () => {
    const { buildUiSchema } = await import("./editor");
    const uiSchema = buildUiSchema({ type: "object", properties: { destination: {}, log: {}, cron: {}, source: {}, metrics: {}, webhook: {} } });

    expect(uiSchema["ui:order"]).toEqual(["cron", "source", "destination", "metrics", "webhook", "log"]);
    expect(uiSchema.cron).toEqual({ "ui:widget": "cron" });
    expect(uiSchema.metrics).toEqual({ "ui:field": "advanced" });
    expect(uiSchema.webhook).toEqual({ "ui:field": "advanced" });
    expect(uiSchema.log).toEqual({ "ui:field": "advanced" });
  });

  it("renders the cron widget with presets and guidance", async () => {
    const { CronWidget } = await import("./editor");
    const Widget = CronWidget as unknown as (props: Record<string, unknown>) => React.ReactElement;
    const html = renderToStaticMarkup(<Widget id="root_cron" value="@daily" onChange={() => undefined} />);

    expect(html).toContain("@daily");
    expect(html).toContain("@hourly");
    expect(html).toContain("cron expression");
  });
});
