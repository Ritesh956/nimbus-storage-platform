import type { Metadata } from "next";
import { Mona_Sans, Geist_Mono } from "next/font/google";
import Script from "next/script";
import "./globals.css";
import { AuthProvider } from "@/lib/auth-context";
import { ToastProvider } from "@/components/Toast";
import { themeInitScript } from "@/lib/theme";

// Mona Sans is the Dashdark X reference design's typeface (variable font,
// weights come from the file's Text Single ramp: 400/500/600).
const monaSans = Mona_Sans({
  variable: "--font-mona-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Nimbus Storage",
  description: "A distributed cloud storage platform",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    // suppressHydrationWarning: the beforeInteractive script below sets
    // data-theme on this element before React hydrates, which would
    // otherwise be a real (expected, harmless) client/server markup
    // mismatch — same pattern next-themes and other theme-toggle
    // implementations use.
    <html
      lang="en"
      suppressHydrationWarning
      className={`${monaSans.variable} ${geistMono.variable} h-full antialiased`}
    >
      <head>
        {/* Applies a stored light/dark override (lib/theme.ts) before first
            paint — audit §11's light theme is worthless if a user who
            picked "Light" sees one dark frame flash on every load.
            beforeInteractive per next/script's own guidance for anything
            that must run ahead of hydration. */}
        <Script id="theme-init" strategy="beforeInteractive">
          {themeInitScript}
        </Script>
      </head>
      <body className="min-h-full flex flex-col">
        <ToastProvider>
          <AuthProvider>{children}</AuthProvider>
        </ToastProvider>
      </body>
    </html>
  );
}
