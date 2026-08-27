export async function listConfigs(fetcher: typeof fetch = fetch) {
  const response = await fetcher("/api/v1/configs");
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  const body = await response.json();
  return body.configs;
}

export async function getConfig(name: string, fetcher: typeof fetch = fetch) {
  const response = await fetcher(`/api/v1/configs/${encodeURIComponent(name)}`);
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}

export async function exportConfig(name: string, fetcher: typeof fetch = fetch) {
  const response = await fetcher(`/api/v1/configs/${encodeURIComponent(name)}/export`);
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.text();
}

export async function reviewConfig(name: string, draft: unknown, fetcher: typeof fetch = fetch) {
  const response = await fetcher(`/api/v1/configs/${encodeURIComponent(name)}/review`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(draft),
  });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}

export async function saveConfig(name: string, draft: unknown, fetcher: typeof fetch = fetch) {
  const response = await fetcher(`/api/v1/configs/${encodeURIComponent(name)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(draft),
  });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}

export async function listTrash(fetcher: typeof fetch = fetch) {
  const response = await fetcher("/api/v1/trash");
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  const body = await response.json();
  return body.trash;
}

export async function restoreTrash(id: string, fetcher: typeof fetch = fetch) {
  const response = await fetcher(`/api/v1/trash/${encodeURIComponent(id)}/restore`, { method: "POST" });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}

export async function getBackup(name: string, fetcher: typeof fetch = fetch) {
  const response = await fetcher(`/api/v1/configs/${encodeURIComponent(name)}/backup`);
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}

export async function restoreBackup(name: string, fetcher: typeof fetch = fetch) {
  const response = await fetcher(`/api/v1/configs/${encodeURIComponent(name)}/backup/restore`, { method: "POST" });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}

export async function createConfig(name: string, template: "blank" | "example", fetcher: typeof fetch = fetch) {
  const response = await fetcher("/api/v1/configs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, template }),
  });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}

export async function renameConfig(name: string, next: string, fetcher: typeof fetch = fetch) {
  const response = await fetcher(`/api/v1/configs/${encodeURIComponent(name)}/rename`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: next }),
  });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}

export async function copyConfig(name: string, next: string, fetcher: typeof fetch = fetch) {
  const response = await fetcher(`/api/v1/configs/${encodeURIComponent(name)}/copy`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: next }),
  });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}

export async function importConfig(name: string, content: string, fetcher: typeof fetch = fetch) {
  const response = await fetcher("/api/v1/configs/import", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, content }),
  });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}

export async function deleteConfig(name: string, fetcher: typeof fetch = fetch) {
  const response = await fetcher(`/api/v1/configs/${encodeURIComponent(name)}`, { method: "DELETE" });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json();
}
