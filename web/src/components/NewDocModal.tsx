import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Button, Input } from "antd";
import { IconFilePlus } from "./Icons";
import { AppModal } from "./AppModal";
import { withMarkdownExtension } from "../utils";

interface NewDocModalProps {
  defaultDir: string;
  displayDir: string;
  exists: (path: string) => boolean;
  onClose: () => void;
  onCreate: (path: string) => Promise<void>;
}

export function NewDocModal({
  defaultDir,
  displayDir,
  exists,
  onClose,
  onCreate,
}: NewDocModalProps) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const preview = name.trim()
    ? [displayDir, withMarkdownExtension(name)].filter(Boolean).join("/")
    : null;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      setError(t("请输入文件名"));
      return;
    }
    const full = withMarkdownExtension(
      [defaultDir, trimmed].filter(Boolean).join("/"),
    );
    if (
      full
        .split("/")
        .some(
          (segment) =>
            segment === ".." || segment === "." || segment.startsWith("."),
        )
    ) {
      setError(t("文件名不能以 . 开头或包含 .."));
      return;
    }
    if (exists(full)) {
      setError(t("同名文件已存在"));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await onCreate(full);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("创建失败"));
      setBusy(false);
    }
  };

  return (
    <AppModal
      title={
        <span className="app-modal-title">
          <IconFilePlus size={17} />
          {t("新建 Markdown 文档")}
        </span>
      }
      width={480}
      onClose={onClose}
      className="new-doc-modal"
    >
      <form onSubmit={submit}>
        <label className="modal-label" htmlFor="new-doc-name">
          {t("文件路径")}
        </label>
        <Input
          id="new-doc-name"
          autoFocus
          addonBefore={displayDir ? `${displayDir}/` : undefined}
          value={name}
          onChange={(event) => {
            setName(event.target.value);
            setError(null);
          }}
          placeholder={t("未命名.md")}
          spellCheck={false}
          autoComplete="off"
        />
        <p className="modal-hint">
          {preview ? (
            <>
              {t("将创建")} <code>{preview}</code>
            </>
          ) : (
            t("可以输入子目录，如 notes/想法.md")
          )}
        </p>
        {error ? <p className="modal-error">{error}</p> : null}
        <div className="modal-actions">
          <Button onClick={onClose}>{t("取消")}</Button>
          <Button type="primary" htmlType="submit" loading={busy}>
            {busy ? t("创建中…") : t("创建")}
          </Button>
        </div>
      </form>
    </AppModal>
  );
}
