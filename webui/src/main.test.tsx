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
});
