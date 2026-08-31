import type { ReactNode } from "react";

export const VERSION = "0.0.1";

// Reusable page footer: the optional meta (e.g. role · email verified) renders
// on the left, and the app version sits on the right.
export function PageFooter({ meta }: { meta?: ReactNode }) {
  return (
    <footer className="page-footer">
      {meta}
      <span className="footer-version">v{VERSION}</span>
    </footer>
  );
}
