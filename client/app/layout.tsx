import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "SmartLock Admin",
  description: "Quản lý thẻ và camera SmartLock",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="vi"
      className="h-full antialiased"
      suppressHydrationWarning
    >
      <body className="min-h-full flex flex-col">{children}</body>
    </html>
  );
}
