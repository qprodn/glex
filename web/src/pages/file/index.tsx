import type { StoredFile } from "@/services/file/v1";
import { type ServiceError, services } from "@/services";
import { getToken } from "@/utils/token";
import { buildFilter } from "@/utils/aipFilter";
import { createTokenPagedRequest } from "@/utils/pager";
import { InboxOutlined, UploadOutlined } from "@ant-design/icons";
import {
  type ActionType,
  PageContainer,
  type ProColumns,
  ProTable,
} from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Button, Input, Modal, Typography, Upload } from "antd";
import React, { useRef, useState } from "react";

/**
 * 文件管理: Stored Files list. The two transfer routes are hand-written and
 * multipart/streaming (ADR 0008), so the generated client covers only the
 * metadata calls — upload goes through a raw fetch with the bearer token,
 * download proxies the bytes through the service and saves via a blob URL.
 */

/** Read the kratos structured error body out of a non-2xx fetch Response. */
async function serviceErrorFrom(res: Response): Promise<ServiceError> {
  try {
    return (await res.json()) as ServiceError;
  } catch {
    return { code: res.status, message: res.statusText };
  }
}

function authHeaders(json: boolean): Record<string, string> {
  const headers: Record<string, string> = json
    ? { Accept: "application/json" }
    : {};
  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  return headers;
}

/** uploadMultipart relays one file (plus optional business path) to POST /file/files. */
async function uploadMultipart(file: File, bizPath: string): Promise<StoredFile> {
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
async function downloadBytes(
  fileId: string,
  disposition: "inline" | "attachment",
  originalName: string,
): Promise<void> {
  const res = await fetch(
    `/file/files/${encodeURIComponent(fileId)}/content?disposition=${disposition}`,
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

function formatSize(bytes: number | undefined): string {
  if (!bytes || bytes <= 0) {
    return "-";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 100 || unit === 0 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

/** previewable reports whether a browser tab can render the type inline. */
function previewable(mimeType: string | undefined): boolean {
  return (
    !!mimeType &&
    (mimeType.startsWith("image/") ||
      mimeType === "application/pdf" ||
      mimeType.startsWith("text/"))
  );
}

const FileList: React.FC = () => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  const [uploadOpen, setUploadOpen] = useState(false);
  const [bizPath, setBizPath] = useState("");

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<StoredFile>(async (pageSize, pageToken, params) => {
        const set = await services.file.ListFiles({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "original_name", value: String(params.originalName ?? "") || undefined },
            { field: "suffix", value: String(params.suffix ?? "") || undefined },
            { field: "mime_type", value: String(params.mimeType ?? "") || undefined },
          ]),
          orderBy: undefined,
        });
        return { items: set.files ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const onWithdraw = (record: StoredFile) => {
    Modal.confirm({
      title: intl.formatMessage(
        { id: "pages.file.withdraw.confirm", defaultMessage: "确定撤回文件「{name}」吗？" },
        { name: record.originalName },
      ),
      content: intl.formatMessage({
        id: "pages.file.withdraw.hint",
        defaultMessage: "撤回立即删除存储字节且不可恢复；记录将作为审计历史保留（ADR 0009）。",
      }),
      onOk: async () => {
        await services.file.WithdrawFile({ fileId: record.fileId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      },
    });
  };

  const onDownload = async (record: StoredFile, disposition: "inline" | "attachment") => {
    try {
      await downloadBytes(String(record.fileId ?? ""), disposition, record.originalName ?? "");
    } catch (error) {
      const serviceError = error as ServiceError;
      message.error(serviceError?.message ?? String(error));
    }
  };

  const columns: ProColumns<StoredFile>[] = [
    { title: "ID", dataIndex: "fileId", width: 90, search: false },
    {
      title: intl.formatMessage({ id: "pages.file.originalName", defaultMessage: "文件名" }),
      dataIndex: "originalName",
      ellipsis: true,
    },
    {
      title: intl.formatMessage({ id: "pages.file.suffix", defaultMessage: "扩展名" }),
      dataIndex: "suffix",
      width: 90,
      render: (_, record) => <Typography.Text code>{record.suffix || "-"}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.file.mimeType", defaultMessage: "类型" }),
      dataIndex: "mimeType",
      width: 170,
      ellipsis: true,
    },
    {
      title: intl.formatMessage({ id: "pages.file.size", defaultMessage: "大小" }),
      dataIndex: "sizeBytes",
      width: 100,
      search: false,
      render: (_, record) => formatSize(record.sizeBytes),
    },
    {
      title: intl.formatMessage({ id: "pages.file.objKey", defaultMessage: "存储键" }),
      dataIndex: "objKey",
      search: false,
      ellipsis: true,
      render: (_, record) => <Typography.Text code>{record.objKey}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.file.createBy", defaultMessage: "上传人" }),
      dataIndex: "createBy",
      width: 110,
      search: false,
    },
    {
      title: intl.formatMessage({ id: "common.createdAt", defaultMessage: "上传时间" }),
      dataIndex: "createdAt",
      width: 160,
      search: false,
      valueType: "dateTime",
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 200,
      render: (_, record) => [
        previewable(record.mimeType) ? (
          <Access key="preview" accessible={access.perm("system:file:download")}>
            <a onClick={() => onDownload(record, "inline")}>
              {intl.formatMessage({ id: "pages.file.preview", defaultMessage: "预览" })}
            </a>
          </Access>
        ) : null,
        <Access key="download" accessible={access.perm("system:file:download")}>
          <a onClick={() => onDownload(record, "attachment")}>
            {intl.formatMessage({ id: "pages.file.download", defaultMessage: "下载" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("system:file:remove")}>
          <Typography.Link type="danger" onClick={() => onWithdraw(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<StoredFile>
        headerTitle={intl.formatMessage({ id: "menu.system.file", defaultMessage: "文件管理" })}
        rowKey="fileId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
        toolBarRender={() => [
          <Access key="upload" accessible={access.perm("system:file:upload")}>
            <Button
              type="primary"
              icon={<UploadOutlined />}
              onClick={() => {
                setBizPath("");
                setUploadOpen(true);
              }}
            >
              {intl.formatMessage({ id: "pages.file.upload", defaultMessage: "上传" })}
            </Button>
          </Access>,
        ]}
      />
      <Modal
        title={intl.formatMessage({ id: "pages.file.upload.title", defaultMessage: "上传文件" })}
        open={uploadOpen}
        footer={null}
        onCancel={() => setUploadOpen(false)}
        destroyOnHidden
      >
        <Input
          allowClear
          placeholder={intl.formatMessage({
            id: "pages.file.bizPath.placeholder",
            defaultMessage: "业务目录（可选，如 avatar；小写字母、数字、连字符、斜杠）",
          })}
          value={bizPath}
          onChange={(e) => setBizPath(e.target.value)}
          style={{ marginBottom: 16 }}
        />
        <Upload.Dragger
          maxCount={1}
          showUploadList={false}
          customRequest={async ({ file, onSuccess, onError }) => {
            try {
              const created = await uploadMultipart(file as unknown as File, bizPath.trim());
              message.success(
                intl.formatMessage(
                  { id: "pages.file.upload.success", defaultMessage: "已上传：{name}" },
                  { name: created.originalName },
                ),
              );
              onSuccess?.(created);
              setUploadOpen(false);
              actionRef.current?.reload();
            } catch (error) {
              const serviceError = error as ServiceError;
              message.error(serviceError?.message ?? String(error));
              onError?.(error as Error);
            }
          }}
        >
          <p className="ant-upload-drag-icon">
            <InboxOutlined />
          </p>
          <p className="ant-upload-text">
            {intl.formatMessage({
              id: "pages.file.upload.drag",
              defaultMessage: "点击或拖拽文件到此处上传",
            })}
          </p>
          <p className="ant-upload-hint">
            {intl.formatMessage({
              id: "pages.file.upload.hint",
              defaultMessage: "类型与大小上限由服务端配置决定（按内容嗅探判定）",
            })}
          </p>
        </Upload.Dragger>
      </Modal>
    </PageContainer>
  );
};

export default FileList;
