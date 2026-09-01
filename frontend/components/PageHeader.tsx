import type { ReactNode } from "react";

// Reusable page header. The title and optional subtitle render on the left,
// and any actions (children, e.g. a logout link) sit on the right. Pass the
// welcome greeting via `right` when it should be right-aligned.
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
      <div>
        <h1>{title}</h1>
        {subtitle ? <p>{subtitle}</p> : null}
      </div>
      <div className="page-header-right">
        {right}
        {children}
      </div>
    </header>
  );
}
