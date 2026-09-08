import type { StoredFile } from "@/services/file/v1";
import { type ServiceError } from "@/services";
import { DownloadOutlined, PaperClipOutlined } from "@ant-design/icons";
import { useIntl } from "@umijs/max";
import { App, Button, Upload } from "antd";
import React from "react";

import {
  batchResolveFiles,
  downloadBytes,
  uploadMultipart,
} from "@/utils/fileTransfer";

/**
 * FileUpload is the attachments form control: the value is a list of Stored
 * File ids. Uploads relay through the service (utils/fileTransfer); each
 * item keeps its metadata for name/size display and re-resolves on mount —
 * including a fresh presigned display URL (ADR 0016). Saves-as downloads go
 * through the proxied content route (ADR 0008), not the display URL.
 */
export interface FileUploadProps {
  /** Current Stored File ids. */
  value?: number[];
  onChange?: (fileIds: number[]) => void;
  /** Business directory prefix for the Stored Key. */
  bizPath?: string;
  maxCount?: number;
  disabled?: boolean;
}

type UploadItem = {
  uid: string;
  fileId: number;
  meta: StoredFile;
};

const FileUpload: React.FC<FileUploadProps> = ({
  value,
  onChange,
  bizPath = "",
  maxCount,
  disabled = false,
}) => {
  const { message } = App.useApp();
  const intl = useIntl();
  const [items, setItems] = React.useState<UploadItem[]>([]);

  // Re-resolve metadata (and fresh display URLs) for ids that arrive from
  // outside this control — initial form values or programmatic sets.
  React.useEffect(() => {
    let stale = false;
    batchResolveFiles(value ?? []).then((files) => {
      if (stale) {
        return;
      }
      const resolved = files.map((meta) => ({
        uid: `file-${meta.fileId}`,
        fileId: Number(meta.fileId ?? 0),
        meta,
      }));
      // Keep freshly uploaded entries the batch ask has not seen yet.
      const known = new Set(resolved.map((item) => item.fileId));
      setItems((current) => [
        ...resolved,
        ...current.filter((item) => !known.has(item.fileId)),
      ]);
    });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(value ?? [])]);

  const emit = (next: UploadItem[]) => {
    setItems(next);
    onChange?.(next.map((item) => item.fileId));
  };

  return (
    <>
      <Upload
        multiple
        maxCount={maxCount}
        disabled={disabled}
        showUploadList={false}
        customRequest={async ({ file: raw, onSuccess, onError }) => {
          try {
            const created = await uploadMultipart(raw as unknown as File, bizPath);
            const item: UploadItem = {
              uid: `file-${created.fileId}`,
              fileId: Number(created.fileId ?? 0),
              meta: created,
            };
            emit([...items, item]);
            onSuccess?.(created);
          } catch (error) {
            const serviceError = error as ServiceError;
            message.error(serviceError?.message ?? String(error));
            onError?.(error as Error);
          }
        }}
      >
        <Button icon={<PaperClipOutlined />} disabled={disabled}>
          {intl.formatMessage({
            id: "component.fileUpload.select",
            defaultMessage: "选择文件",
          })}
        </Button>
      </Upload>
      <ul style={{ listStyle: "none", padding: 0, margin: "8px 0 0" }}>
        {items.map((item) => (
          <li
            key={item.uid}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              padding: "4px 0",
            }}
          >
            <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis" }}>
              {item.meta.originalName}
              <span style={{ color: "rgba(0,0,0,0.45)", marginLeft: 8 }}>
                {item.meta.sizeBytes ? `${Math.ceil(Number(item.meta.sizeBytes) / 1024)} KB` : ""}
              </span>
            </span>
            <a
              onClick={() =>
                downloadBytes(item.fileId, "attachment", item.meta.originalName ?? "")
              }
            >
              <DownloadOutlined />
            </a>
            {!disabled && (
              <a
                onClick={() => emit(items.filter((entry) => entry.fileId !== item.fileId))}
                style={{ color: "rgba(0,0,0,0.45)" }}
              >
                ✕
              </a>
            )}
          </li>
        ))}
      </ul>
    </>
  );
};

export default FileUpload;
