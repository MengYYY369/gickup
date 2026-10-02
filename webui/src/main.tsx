import React, { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import Form from "@rjsf/core";
import validator from "@rjsf/validator-ajv8";
import { copyConfig, createConfig, deleteConfig, exportConfig, getBackup, getConfig, importConfig, listConfigs, listTrash, renameConfig, restoreBackup, restoreTrash, reviewConfig, saveConfig } from "./api";
import "./style.css";

type ConfigInfo = { name: string; documents: number; modified: string; valid: boolean };
type OpenConfig = {
  documents: Record<string, unknown>[];
  schema: Record<string, unknown>;
  uiSchema: Record<string, unknown>;
  yaml: { source: string };
  version: string;
  warnings: { document: number; path: string; message: string }[];
};

type EditorControlsProps = {
  documents: Record<string, unknown>[];
  activeDocument: number;
  warnings: { document: number; path: string; message: string }[];
  onOperation?: (operation: Record<string, unknown>) => void;
};

function documentSummary(document: Record<string, unknown>, section: "source" | "destination") {
  const value = document[section];
  if (!value || typeof value !== "object" || Array.isArray(value)) return "none";
  const providers = Object.keys(value as Record<string, unknown>);
  return providers.length > 0 ? providers.join(", ") : "none";
}

export function EditorControls({ documents, activeDocument, warnings, onOperation }: EditorControlsProps) {
  const document = documents[activeDocument] ?? {};
  const providers = ["source", "destination"]
    .flatMap(section => {
      const value = document[section];
      return value && typeof value === "object" && !Array.isArray(value)
        ? Object.entries(value as Record<string, unknown>).map(([provider, items]) => ({ section, provider, items }))
        : [];
    });

  return <section className="editor-controls">
    <nav aria-label="YAML documents">
      {documents.map((item, index) => <article key={index}>
        <strong>Configuration {index + 1}</strong>
        <small>Source: {documentSummary(item, "source")}</small>
        <small>Destination: {documentSummary(item, "destination")}</small>
      </article>)}
    </nav>
    <div className="document-actions">
      <button type="button" onClick={() => onOperation?.(addDocumentOperation({}))}>Add document</button>
      <button type="button" disabled={documents.length === 0} onClick={() => onOperation?.(copyDocumentOperation(activeDocument))}>Copy document</button>
      <button type="button" disabled={documents.length === 0} onClick={() => onOperation?.(deleteDocumentOperation(activeDocument))}>Delete document</button>
      <button type="button" disabled={activeDocument <= 0} onClick={() => onOperation?.(moveDocumentOperation(activeDocument, activeDocument - 1))}>Move document up</button>
      <button type="button" disabled={activeDocument < 0 || activeDocument >= documents.length - 1} onClick={() => onOperation?.(moveDocumentOperation(activeDocument, activeDocument + 1))}>Move document down</button>
    </div>
    {providers.map(({ section, provider, items }) => <details key={`${section}-${provider}`}>
      <summary>Provider: {provider}</summary>
      {Array.isArray(items) && <div className="provider-items">
        {items.map((_, index) => <article
          key={index}
          draggable
          onDragStart={event => {
            event.dataTransfer.effectAllowed = "move";
            event.dataTransfer.setData("text/plain", String(index));
          }}
          onDragOver={event => {
            event.preventDefault();
            event.dataTransfer.dropEffect = "move";
          }}
          onDrop={event => {
            event.preventDefault();
            const from = Number(event.dataTransfer.getData("text/plain"));
            if (Number.isInteger(from) && from !== index) {
              onOperation?.(moveArrayOperation(activeDocument, `${section}.${provider}`, from, index));
            }
          }}
        >
          <strong>Item {index + 1}</strong>
          <button type="button" disabled={index === 0} onClick={() => onOperation?.(moveArrayOperation(activeDocument, `${section}.${provider}`, index, index - 1))}>Move provider item up</button>
          <button type="button" disabled={index === items.length - 1} onClick={() => onOperation?.(moveArrayOperation(activeDocument, `${section}.${provider}`, index, index + 1))}>Move provider item down</button>
          <button type="button" onClick={() => onOperation?.(copyArrayOperation(activeDocument, `${section}.${provider}`, index))}>Copy provider item</button>
          <button type="button" onClick={() => onOperation?.(deleteArrayOperation(activeDocument, `${section}.${provider}`, index))}>Delete provider item</button>
        </article>)}
        <button type="button" onClick={() => onOperation?.(addArrayOperation(activeDocument, `${section}.${provider}`, {}))}>Add provider item</button>
      </div>}
    </details>)}
    <details>
      <summary>Advanced fields</summary>
    </details>
    {warnings.map((warning, index) => <p className="warning" key={index}>
      Configuration {warning.document + 1}, {warning.path}: {warning.message}
    </p>)}
  </section>;
}

export function documentOperations(original: Record<string, unknown>, draft: Record<string, unknown>, document: number) {
  const operations: Record<string, unknown>[] = [];
  diffDocumentValue(original, draft, "", document, operations);
  return operations;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function diffDocumentValue(base: unknown, draft: unknown, path: string, document: number, operations: Record<string, unknown>[]) {
  const childPath = (segment: string | number) => (path ? `${path}.${segment}` : String(segment));

  if (base === undefined || base === null) {
    if (draft === undefined || draft === null || draft === "") return;
    operations.push({ op: "set-field", document, path, value: draft });
    return;
  }
  if (draft === undefined || draft === null || draft === "") {
    if (base === "") return;
    operations.push({ op: "delete-field", document, path });
    return;
  }
  if (Array.isArray(base) && Array.isArray(draft)) {
    const shared = Math.min(base.length, draft.length);
    for (let index = 0; index < shared; index++) {
      diffDocumentValue(base[index], draft[index], childPath(index), document, operations);
    }
    for (let index = shared; index < draft.length; index++) {
      operations.push({ op: "add-array", document, path, value: draft[index] });
    }
    for (let index = base.length - 1; index >= draft.length; index--) {
      operations.push({ op: "delete-array", document, path, index });
    }
    return;
  }
  if (isPlainObject(base) && isPlainObject(draft)) {
    for (const key of Object.keys(base)) {
      diffDocumentValue(base[key], draft[key], childPath(key), document, operations);
    }
    for (const key of Object.keys(draft)) {
      if (!(key in base)) operations.push({ op: "set-field", document, path: childPath(key), value: draft[key] });
    }
    return;
  }
  if (base !== draft) {
    operations.push({ op: "set-field", document, path, value: draft });
  }
}

export function addDocumentOperation(value: Record<string, unknown>) {
  return { op: "add-document", value };
}

export function copyDocumentOperation(index: number) {
  return { op: "copy-document", index };
}

export function moveDocumentOperation(from: number, to: number) {
  return { op: "move-document", from, to };
}

export function deleteDocumentOperation(index: number) {
  return { op: "delete-document", index };
}

type DocumentOperation =
  | { op: "add-document"; value: Record<string, unknown> }
  | { op: "copy-document"; index: number }
  | { op: "delete-document"; index: number }
  | { op: "move-document"; from: number; to: number };

export function applyDocumentOperation(documents: Record<string, unknown>[], operation: DocumentOperation) {
  const next = documents.map(document => structuredClone(document));
  switch (operation.op) {
    case "add-document":
      next.push(structuredClone(operation.value));
      break;
    case "copy-document":
      if (operation.index >= 0 && operation.index < next.length) {
        next.splice(operation.index + 1, 0, structuredClone(next[operation.index]));
      }
      break;
    case "delete-document":
      if (operation.index >= 0 && operation.index < next.length) next.splice(operation.index, 1);
      break;
    case "move-document": {
      if (operation.from < 0 || operation.from >= next.length || operation.to < 0 || operation.to >= next.length) break;
      const [moved] = next.splice(operation.from, 1);
      next.splice(operation.to, 0, moved);
      break;
    }
  }
  return next;
}

export function applyArrayOperation(documents: Record<string, unknown>[], operation: Record<string, unknown>) {
  const next = documents.map(item => structuredClone(item));
  const target = next[Number(operation.document ?? 0)];
  if (!target || typeof operation.path !== "string" || operation.path === "") return next;
  let container: unknown = target;
  for (const segment of operation.path.split(".")) {
    if (Array.isArray(container)) container = container[Number(segment)];
    else if (isPlainObject(container)) container = container[segment];
    else return next;
  }
  if (!Array.isArray(container)) return next;
  switch (String(operation.op)) {
    case "add-array":
      container.push(structuredClone(operation.value));
      break;
    case "copy-array": {
      const index = Number(operation.index);
      if (index >= 0 && index < container.length) container.splice(index + 1, 0, structuredClone(container[index]));
      break;
    }
    case "delete-array": {
      const index = Number(operation.index);
      if (index >= 0 && index < container.length) container.splice(index, 1);
      break;
    }
    case "move-array": {
      const from = Number(operation.from);
      const to = Number(operation.to);
      if (from >= 0 && from < container.length && to >= 0 && to < container.length) {
        const [item] = container.splice(from, 1);
        container.splice(to, 0, item);
      }
      break;
    }
  }
  return next;
}

export function pendingOperations(baseDocuments: Record<string, unknown>[], documentOperationsList: Record<string, unknown>[], workingDocuments: Record<string, unknown>[]) {
  const staged = documentOperationsList.reduce<Record<string, unknown>[]>(
    (documents, operation) => applyDocumentOperation(documents, operation as DocumentOperation),
    baseDocuments ?? [],
  );
  const operations: Record<string, unknown>[] = [...documentOperationsList];
  workingDocuments.forEach((document, index) => {
    if (index < staged.length) operations.push(...documentOperations(staged[index] ?? {}, document ?? {}, index));
  });
  return operations;
}

export function addArrayOperation(document: number, path: string, value: unknown) {
  return { op: "add-array", document, path, value };
}

export function copyArrayOperation(document: number, path: string, index: number) {
  return { op: "copy-array", document, path, index };
}

export function moveArrayOperation(document: number, path: string, from: number, to: number) {
  return { op: "move-array", document, path, from, to };
}

export function deleteArrayOperation(document: number, path: string, index: number) {
  return { op: "delete-array", document, path, index };
}

export async function exportDownload(name: string) {
  const content = await exportConfig(name);
  const url = URL.createObjectURL(new Blob([content], { type: "application/yaml;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  URL.revokeObjectURL(url);
}

export async function createWorkspaceConfig(name: string, template: "blank" | "example") {
  await createConfig(name, template);
  return listConfigs();
}

export async function renameWorkspaceConfig(name: string, next: string) {
  await renameConfig(name, next);
  return listConfigs();
}

export async function copyWorkspaceConfig(name: string, next: string) {
  await copyConfig(name, next);
  return listConfigs();
}

export async function importWorkspaceConfig(name: string, content: string) {
  await importConfig(name, content);
  return listConfigs();
}

export async function deleteWorkspaceConfig(name: string) {
  await deleteConfig(name);
  return listConfigs();
}

export function App() {
  const [configs, setConfigs] = useState<ConfigInfo[]>([]);
  const [name, setName] = useState("");
  const [opened, setOpened] = useState<OpenConfig | null>(null);
  const [working, setWorking] = useState<Record<string, unknown>[]>([]);
  const [pendingDocumentOperations, setPendingDocumentOperations] = useState<Record<string, unknown>[]>([]);
  const [document, setDocument] = useState(0);
  const [dirty, setDirty] = useState(false);
  const [review, setReview] = useState<{ diff: string; yaml: string; valid: boolean; errors: string[] } | null>(null);
  const [backupDiff, setBackupDiff] = useState("");
  const [trash, setTrash] = useState<{ id: string; original: string }[]>([]);
  const [message, setMessage] = useState("");

  useEffect(() => { listConfigs().then(setConfigs).catch(error => setMessage(String(error))); }, []);
  useEffect(() => {
    const guard = (event: BeforeUnloadEvent) => { if (dirty) event.preventDefault(); };
    addEventListener("beforeunload", guard);
    return () => removeEventListener("beforeunload", guard);
  }, [dirty]);

  const draft = working[document] ?? {};

  async function load(next: string) {
    const value = await getConfig(next) as OpenConfig;
    setName(next); setOpened(value);
    setWorking(value.documents.map(item => structuredClone(item)));
    setPendingDocumentOperations([]);
    setDocument(0); setDirty(false); setReview(null);
  }

  async function open(next: string) {
    if (dirty && !confirm("Discard unsaved changes?")) return;
    await load(next);
    setMessage("");
  }

  function operations() {
    return pendingOperations(opened?.documents ?? [], pendingDocumentOperations, working);
  }

  function handleEditorOperation(operation: Record<string, unknown>) {
    if (!opened) return;
    const kind = String(operation.op ?? "");
    if (kind.endsWith("-document")) {
      const next = applyDocumentOperation(working, operation as DocumentOperation);
      setPendingDocumentOperations([...pendingDocumentOperations, operation]);
      setWorking(next);
      setDocument(current => Math.min(current, Math.max(0, next.length - 1)));
    } else if (kind.endsWith("-array")) {
      setWorking(applyArrayOperation(working, operation));
    }
    setDirty(true);
    setReview(null);
  }

  async function prepareSave() {
    if (!opened) return;
    const result = await reviewConfig(name, { version: opened.version, operations: operations() });
    setReview(result); setMessage(result.valid ? "Review changes before saving." : "Fix validation errors before saving.");
  }

  async function save() {
    if (!opened || !review?.valid || !confirm("Save these reviewed changes?")) return;
    try {
      await saveConfig(name, { version: opened.version, confirmed: true, operations: operations() });
      await load(name); setMessage("Saved.");
    } catch (error) {
      setMessage(String(error).includes("409") ? "The file changed on disk. Reload before saving." : String(error));
    }
  }

  async function showBackup() {
    if (!name) return;
    const backup = await getBackup(name);
    setBackupDiff(backup.diff);
  }

  async function restoreOpenedBackup() {
    if (!name) return;
    await restoreBackup(name);
    await open(name);
    setBackupDiff("");
    setMessage("Backup restored.");
  }

  async function showTrash() {
    setTrash(await listTrash());
  }

  async function restoreTrashEntry(id: string) {
    await restoreTrash(id);
    setTrash(await listTrash());
    setConfigs(await listConfigs());
    setMessage("Configuration restored from trash.");
  }

  return <main className="app">
    <aside><h1>Gickup</h1><p>Configuration editor</p>
      <div className="workspace-actions"><button onClick={() => createConfig("new.yml", "blank")}>New configuration</button><button onClick={() => importConfig("import.yml", "")}>Import configuration</button><button onClick={showTrash}>Trash</button></div>
      <div className="file-actions"><button disabled={!opened} onClick={() => name && renameConfig(name, name)}>Rename</button><button disabled={!opened} onClick={() => name && copyConfig(name, `copy-${name}`)}>Copy</button><button disabled={!opened} onClick={() => name && exportDownload(name)}>Export</button><button disabled={!opened} onClick={showBackup}>Backup</button><button disabled={!opened} onClick={() => name && deleteConfig(name)}>Delete</button></div>
      {trash.length > 0 && <section aria-label="Trash entries">{trash.map(entry => <div key={entry.id}><span>{entry.original}</span><button onClick={() => restoreTrashEntry(entry.id)}>Restore {entry.original}</button></div>)}</section>}
      <ul>{configs.map(config => <li key={config.name}><button onClick={() => open(config.name)}>{config.name}</button><small>{config.documents} docs · {config.valid ? "valid" : "invalid"}</small></li>)}</ul>
      {opened && <nav>{working.map((_, index) => <button key={index} onClick={() => { if (!dirty || confirm("Discard unsaved changes?")) { setDocument(index); setDirty(false); } }}>Configuration {index + 1}</button>)}</nav>}
    </aside>
    <section>
      {message && <p role="status">{message}</p>}
      <EditorControls
        documents={working}
        activeDocument={document}
        warnings={opened?.warnings ?? []}
        onOperation={handleEditorOperation}
      />
      {!opened ? <h2>Select a YAML configuration</h2> : <>
        <header><h2>{name}</h2><button disabled={!dirty} onClick={prepareSave}>Review changes</button></header>
        {opened.warnings.map((warning, index) => <p className="warning" key={index}>{warning.path}: {warning.message}</p>)}
        <Form schema={opened.schema} uiSchema={opened.uiSchema} formData={draft} validator={validator} liveValidate={false} onChange={event => { const next = event.formData ?? {}; setWorking(current => current.map((item, index) => index === document ? next : item)); setDirty(true); setReview(null); }} onSubmit={prepareSave}><button type="submit">Review changes</button></Form>
        <details><summary>Read-only YAML</summary><pre>{opened.yaml.source}</pre></details>
        {backupDiff && <section className="backup"><h3>Backup review</h3><pre>{backupDiff}</pre><button onClick={restoreOpenedBackup}>Restore backup</button></section>}
        {review && <section className="review"><h3>Save review</h3>{review.errors.map(error => <p className="error" key={error}>{error}</p>)}<pre>{review.diff}</pre><button disabled={!review.valid} onClick={save}>Confirm save</button></section>}
      </>}
    </section>
  </main>;
}

if (typeof document !== "undefined") {
  const root = document.getElementById("root");
  if (root) createRoot(root).render(<App />);
}
