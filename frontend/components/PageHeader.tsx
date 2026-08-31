import type { ReactNode } from "react";

// Reusable page header. The title and optional subtitle render on the left,
// and any actions (children, e.g. a logout link) sit on the right.
export function PageHeader({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <header className="page-header">
      <div>
        <h1>{title}</h1>
        {subtitle ? <p>{subtitle}</p> : null}
      </div>
      {children}
    </header>
  );
}
