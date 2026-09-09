import type { ImportRowError } from "@/services/user/v1";
import { services } from "@/services";
import { downloadWorkbook, uploadMultipart } from "@/utils/fileTransfer";
import { DownloadOutlined, InboxOutlined } from "@ant-design/icons";
import { App, Alert, Button, Modal, Table, Typography, Upload } from "antd";
import { useIntl } from "@umijs/max";
import React, { useState } from "react";

/**
 * ImportUsersModal drives the ADR 0017 import flow end to end: download the
 * template, upload the filled workbook through the file module (business
 * prefix import/user — the Import File stays as the audit record), then
 * hand the Stored File id to ImportUsers. Row errors render as a table;
 * a non-empty list means nothing was imported (all-or-nothing).
 */
const ImportUsersModal: React.FC<{
  open: boolean;
  onClose: (changed: boolean) => void;
}> = ({ open, onClose }) => {
  const { message } = App.useApp();
  const intl = useIntl();
  const [fileId, setFileId] = useState<number | undefined>();
  const [fileName, setFileName] = useState<string>("");
  const [uploading, setUploading] = useState(false);
  const [importing, setImporting] = useState(false);
  const [rowErrors, setRowErrors] = useState<ImportRowError[] | undefined>();

  const reset = () => {
    setFileId(undefined);
    setFileName("");
    setRowErrors(undefined);
  };

  const close = (changed: boolean) => {
    reset();
    onClose(changed);
  };

  const onImport = async () => {
    if (!fileId) {
      return;
    }
    setImporting(true);
    try {
      const out = await services.user.ImportUsers({ fileId });
      if (out.rowErrors?.length) {
        setRowErrors(out.rowErrors);
        message.warning(
          intl.formatMessage(
            { id: "pages.user.import.failed", defaultMessage: "共 {count} 处错误，未导入任何数据" },
            { count: out.rowErrors.length },
          ),
        );
      } else {
        message.success(
          intl.formatMessage(
            { id: "pages.user.import.done", defaultMessage: "导入成功，共 {count} 条" },
            { count: out.importedCount ?? 0 },
          ),
        );
        close(true);
      }
    } finally {
      setImporting(false);
    }
  };

  return (
    <Modal
      open={open}
      title={intl.formatMessage({ id: "pages.user.import.title", defaultMessage: "导入用户" })}
      width={640}
      onCancel={() => close(false)}
      footer={[
        <Button
          key="template"
          icon={<DownloadOutlined />}
          onClick={() => downloadWorkbook("/iam/user/-/importTemplate", {}, "用户导入模板.xlsx")}
        >
          {intl.formatMessage({ id: "pages.user.import.template", defaultMessage: "下载模板" })}
        </Button>,
        <Button key="cancel" onClick={() => close(false)}>
          {intl.formatMessage({ id: "common.cancel", defaultMessage: "取消" })}
        </Button>,
        <Button key="import" type="primary" loading={importing} disabled={!fileId} onClick={onImport}>
          {intl.formatMessage({ id: "common.import", defaultMessage: "导入" })}
        </Button>,
      ]}
    >
      {rowErrors?.length ? (
        <Alert
          type="error"
          showIcon
          message={intl.formatMessage({
            id: "pages.user.import.errors",
            defaultMessage: "以下行校验未通过，全部数据均未导入（修正后请重新上传）",
          })}
          style={{ marginBottom: 16 }}
        />
      ) : null}
      <Upload.Dragger
        accept=".xlsx"
        maxCount={1}
        disabled={uploading || importing}
        customRequest={async ({ file, onSuccess, onError }) => {
          setUploading(true);
          try {
            const stored = await uploadMultipart(file as File, "import/user");
            setFileId(stored.fileId);
            setFileName(stored.originalName ?? (file as File).name);
            onSuccess?.(stored);
          } catch (err) {
            onError?.(err as Error);
          } finally {
            setUploading(false);
          }
        }}
        onRemove={() => {
          setFileId(undefined);
          setFileName("");
        }}
      >
        <p className="ant-upload-drag-icon">
          <InboxOutlined />
        </p>
        <p className="ant-upload-text">
          {intl.formatMessage({ id: "pages.user.import.drag", defaultMessage: "点击或拖拽 xlsx 文件到这里上传" })}
        </p>
        <p className="ant-upload-hint">
          {intl.formatMessage({
            id: "pages.user.import.hint",
            defaultMessage: "先下载模板填写；登录账号、用户昵称为必填，部门填完整路径，性别/状态填字典文本",
          })}
        </p>
      </Upload.Dragger>
      {fileId ? (
        <Typography.Text type="secondary" style={{ display: "block", marginTop: 8 }}>
          {intl.formatMessage(
            { id: "pages.user.import.uploaded", defaultMessage: "已上传：{name}（Stored File #{id}，导入后保留为审计记录）" },
            { name: fileName, id: fileId },
          )}
        </Typography.Text>
      ) : null}
      {rowErrors?.length ? (
        <Table<ImportRowError>
          size="small"
          rowKey={(e) => `${e.row}-${e.column}`}
          pagination={false}
          scroll={{ y: 240 }}
          columns={[
            { title: intl.formatMessage({ id: "pages.user.import.row", defaultMessage: "行号" }), dataIndex: "row", width: 80 },
            { title: intl.formatMessage({ id: "pages.user.import.column", defaultMessage: "列" }), dataIndex: "column", width: 120 },
            { title: intl.formatMessage({ id: "pages.user.import.reason", defaultMessage: "原因" }), dataIndex: "reason" },
          ]}
          dataSource={rowErrors}
        />
      ) : null}
    </Modal>
  );
};

export default ImportUsersModal;
