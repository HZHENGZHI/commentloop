import type { ReactNode } from "react";
import { Modal } from "antd";

interface AppModalProps {
  title: ReactNode;
  width?: number | string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  className?: string;
}

export function AppModal({
  title,
  width = 520,
  onClose,
  children,
  footer,
  className,
}: AppModalProps) {
  return (
    <Modal
      open
      onCancel={onClose}
      title={title}
      width={width}
      footer={footer ?? null}
      centered
      className={`app-modal${className ? ` ${className}` : ""}`}
    >
      {children}
    </Modal>
  );
}
