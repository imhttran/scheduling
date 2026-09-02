import type { ReactNode } from "react";
import { Logo } from "./Logo";

// Reusable page header. The brand mark, title and optional subtitle render on
// the left, and any actions (children, e.g. a logout link) sit on the right.
// Pass the welcome greeting via `right` when it should be right-aligned.
export function PageHeader({
  title,
  subtitle,
  right,
  children,
}: {
  title: string;
  subtitle?: ReactNode;
  right?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <header className="page-header">
      <div className="page-header-title">
        <Logo size={28} />
        <div>
          <h1>{title}</h1>
          {subtitle ? <p>{subtitle}</p> : null}
        </div>
      </div>
      <div className="page-header-right">
        {right}
        {children}
      </div>
    </header>
  );
}
