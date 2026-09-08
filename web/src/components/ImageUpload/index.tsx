import type { StoredFile } from "@/services/file/v1";
import { type ServiceError } from "@/services";
import { PlusOutlined } from "@ant-design/icons";
import { useIntl } from "@umijs/max";
import { App, Upload } from "antd";
import React from "react";

import { fetchFile, uploadMultipart } from "@/utils/fileTransfer";

/**
 * ImageUpload is the single-image form control (avatar, cover): the value is
 * a Stored File id, uploads relay through the service, and the preview rides
 * the short-TTL presigned Display URL (ADR 0016) — refreshed from GetFile
 * whenever the id changes, since the URL lapses.
 */
export interface ImageUploadProps {
  /** Current Stored File id; 0/undefined shows the empty badge. */
  value?: number;
  onChange?: (fileId: number) => void;
  /** Business directory prefix for the Stored Key (default "avatar"). */
  bizPath?: string;
  accept?: string;
  disabled?: boolean;
}

const ImageUpload: React.FC<ImageUploadProps> = ({
  value,
  onChange,
  bizPath = "avatar",
  accept = "image/*",
  disabled = false,
}) => {
  const { message } = App.useApp();
  const intl = useIntl();
  const [file, setFile] = React.useState<StoredFile | undefined>(undefined);

  React.useEffect(() => {
    let stale = false;
    if (!value) {
      setFile(undefined);
      return;
    }
    fetchFile(value).then((f) => {
      if (!stale) {
        setFile(f);
      }
    });
    return () => {
      stale = true;
    };
  }, [value]);

  const url = file?.url;
  return (
    <Upload
      listType="picture-card"
      accept={accept}
      maxCount={1}
      showUploadList={false}
      disabled={disabled}
      customRequest={async ({ file: raw, onSuccess, onError }) => {
        try {
          const created = await uploadMultipart(raw as unknown as File, bizPath);
          setFile(created);
          onChange?.(Number(created.fileId ?? 0));
          onSuccess?.(created);
        } catch (error) {
          const serviceError = error as ServiceError;
          message.error(serviceError?.message ?? String(error));
          onError?.(error as Error);
        }
      }}
      onRemove={() => {
        setFile(undefined);
        onChange?.(0);
      }}
    >
      {url ? (
        <img src={url} alt={file?.originalName ?? ""} style={{ width: "100%" }} />
      ) : (
        <div>
          <PlusOutlined />
          <div style={{ marginTop: 8 }}>
            {intl.formatMessage({
              id: "component.imageUpload.upload",
              defaultMessage: "上传",
            })}
          </div>
        </div>
      )}
    </Upload>
  );
};

export default ImageUpload;
