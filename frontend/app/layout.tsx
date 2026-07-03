import type { Metadata } from "next";
import { Mona_Sans, Geist_Mono } from "next/font/google";
import "./globals.css";
import { AuthProvider } from "@/lib/auth-context";

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
    <html lang="en" className={`${monaSans.variable} ${geistMono.variable} h-full antialiased`}>
      <body className="min-h-full flex flex-col">
        <AuthProvider>{children}</AuthProvider>
      </body>
    </html>
  );
}
