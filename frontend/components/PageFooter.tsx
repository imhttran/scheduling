import type { ReactNode } from "react";
import { Logo } from "./Logo";

export const VERSION = "0.0.1";

// Reusable page footer: the optional meta (e.g. role · email verified) renders
// on the left, and the brand mark + app version sit on the right.
export function PageFooter({ meta }: { meta?: ReactNode }) {
  return (
    <footer className="page-footer">
      {meta}
      <span className="footer-version">
        <Logo size={14} /> v{VERSION}
      </span>
    </footer>
  );
}
