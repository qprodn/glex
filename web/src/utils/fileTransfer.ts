import type { StoredFile } from "@/services/file/v1";
import { type ServiceError, services } from "@/services";
import { getToken } from "@/utils/token";

/**
 * File transfer plumbing shared by the file page and the upload components.
 * The two transfer routes are hand-written and multipart/streaming (ADR
 * 0008), so the generated client covers only the metadata calls — upload
 * goes through a raw fetch with the bearer token, download proxies the
 * bytes through the service and saves via a blob URL, and display rides the
 * short-TTL presigned url on every Stored File response (ADR 0016).
 */

/** Read the kratos structured error body out of a non-2xx fetch Response. */
export async function serviceErrorFrom(res: Response): Promise<ServiceError> {
  try {
    return (await res.json()) as ServiceError;
  } catch {
    return { code: res.status, message: res.statusText };
  }
}

function authHeaders(json: boolean): Record<string, string> {
  const headers: Record<string, string> = json ? { Accept: "application/json" } : {};
  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  return headers;
}

/** uploadMultipart relays one file (plus optional business path) to POST /file/files. */
export async function uploadMultipart(file: File, bizPath: string): Promise<StoredFile> {
  const form = new FormData();
  form.append("file", file);
  if (bizPath) {
    form.append("path", bizPath);
  }
  const res = await fetch("/file/files", {
    method: "POST",
    headers: authHeaders(true),
    body: form,
  });
  if (!res.ok) {
    throw await serviceErrorFrom(res);
  }
  return (await res.json()) as StoredFile;
}

/**
 * downloadBytes proxies the stored bytes (disposition inline|attachment).
 * Inline opens a blob URL in a new tab (browser-rendered preview for
 * images/PDF); attachment saves under the original name.
 */
export async function downloadBytes(
  fileId: string | number,
  disposition: "inline" | "attachment",
  originalName: string,
): Promise<void> {
  const res = await fetch(
    `/file/files/${encodeURIComponent(String(fileId))}/content?disposition=${disposition}`,
    { headers: authHeaders(false) },
  );
  if (!res.ok) {
    throw await serviceErrorFrom(res);
  }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  if (disposition === "inline") {
    window.open(url, "_blank", "noopener");
    // The opened tab pins the blob; revoke lazily so it can finish loading.
    window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
    return;
  }
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = originalName;
  anchor.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/**
 * fetchFile re-reads one Stored File through the generated metadata client —
 * the way to mint a fresh Display URL after the previous one lapsed (the
 * presigned url carries a short TTL, ADR 0016). Unknown or withdrawn files
 * resolve to undefined.
 */
export async function fetchFile(fileId: string | number): Promise<StoredFile | undefined> {
  try {
    return await services.file.GetFile({ fileId: Number(fileId) });
  } catch {
    // Withdrawn or missing: display callers render their default.
    return undefined;
  }
}

/**
 * batchResolveFiles swaps a batch of Stored File ids for metadata with fresh
 * display URLs (RuoYi's listByIds counterpart). Unknown or withdrawn ids
 * are absent from the result; a failed ask degrades to what resolved.
 */
export async function batchResolveFiles(fileIds: (number | string)[]): Promise<StoredFile[]> {
  const ids = fileIds.map((id) => Number(id)).filter((id) => Number.isFinite(id) && id !== 0);
  if (!ids.length) {
    return [];
  }
  try {
    const set = await services.file.BatchGetFiles({ fileIds: ids });
    return set.files ?? [];
  } catch {
    return [];
  }
}
