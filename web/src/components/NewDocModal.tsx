import { useEffect, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { IconFilePlus, IconX } from "./Icons";
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
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

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
    <div
      className="modal-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label={t("新建文档")}>
        <div className="modal-head">
          <div className="modal-title">
            <IconFilePlus size={17} />
            {t("新建 Markdown 文档")}
          </div>
          <button
            type="button"
            className="icon-btn small"
            onClick={onClose}
            aria-label={t("关闭")}
          >
            <IconX size={15} />
          </button>
        </div>
        <form onSubmit={submit}>
          <label className="modal-label" htmlFor="new-doc-name">
            {t("文件路径")}
          </label>
          <div className="modal-input-wrap">
            {displayDir ? <span className="modal-prefix">{displayDir}/</span> : null}
            <input
              id="new-doc-name"
              ref={inputRef}
              value={name}
              onChange={(event) => {
                setName(event.target.value);
                setError(null);
              }}
              placeholder={t("未命名.md")}
              spellCheck={false}
              autoComplete="off"
            />
          </div>
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
            <button type="button" className="btn ghost" onClick={onClose}>
              {t("取消")}
            </button>
            <button type="submit" className="btn primary" disabled={busy}>
              {busy ? t("创建中…") : t("创建")}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
