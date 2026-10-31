import { Alert } from "antd";
import { useTranslation } from "react-i18next";
import type { ToastItem } from "../types";

interface ToastsProps {
  items: ToastItem[];
  onDismiss: (id: number) => void;
}

export function Toasts({ items, onDismiss }: ToastsProps) {
  const { t } = useTranslation();
  const kindToType = (kind: ToastItem["kind"]) => {
    if (kind === "success") return "success" as const;
    if (kind === "error") return "error" as const;
    if (kind === "warn") return "warning" as const;
    return "info" as const;
  };
  return (
    <div className="toasts" aria-live="polite">
      {items.map((toast) => (
        <Alert
          key={toast.id}
          type={kindToType(toast.kind)}
          message={toast.message}
          showIcon
          closable
          onClose={() => onDismiss(toast.id)}
          aria-label={t("关闭提示")}
        />
      ))}
    </div>
  );
}
