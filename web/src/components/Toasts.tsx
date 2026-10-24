import type { ToastItem } from "../types";
import { IconAlert, IconCheck, IconWifi, IconX } from "./Icons";

interface ToastsProps {
  items: ToastItem[];
  onDismiss: (id: number) => void;
}

export function Toasts({ items, onDismiss }: ToastsProps) {
  return (
    <div className="toasts" aria-live="polite">
      {items.map((toast) => (
        <div key={toast.id} className={`toast ${toast.kind}`}>
          <span className="toast-icon">
            {toast.kind === "success" ? (
              <IconCheck size={15} />
            ) : toast.kind === "error" || toast.kind === "warn" ? (
              <IconAlert size={15} />
            ) : (
              <IconWifi size={15} />
            )}
          </span>
          <span className="toast-text">{toast.message}</span>
          <button
            type="button"
            className="toast-close"
            onClick={() => onDismiss(toast.id)}
            aria-label="关闭提示"
          >
            <IconX size={13} />
          </button>
        </div>
      ))}
    </div>
  );
}
