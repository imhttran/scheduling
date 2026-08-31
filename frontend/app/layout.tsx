import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  description: "Auth, dashboard, and profile template frontend.",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
