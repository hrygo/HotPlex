import type { Metadata } from 'next';
import { NuqsAdapter } from 'nuqs/adapters/next/app';
import localFont from 'next/font/local';
import './globals.css';

const inter = localFont({
  src: [
    { path: '../node_modules/@fontsource/inter/files/inter-latin-100-normal.woff2', weight: '100' },
    { path: '../node_modules/@fontsource/inter/files/inter-latin-200-normal.woff2', weight: '200' },
    { path: '../node_modules/@fontsource/inter/files/inter-latin-300-normal.woff2', weight: '300' },
    { path: '../node_modules/@fontsource/inter/files/inter-latin-400-normal.woff2', weight: '400' },
    { path: '../node_modules/@fontsource/inter/files/inter-latin-500-normal.woff2', weight: '500' },
    { path: '../node_modules/@fontsource/inter/files/inter-latin-600-normal.woff2', weight: '600' },
    { path: '../node_modules/@fontsource/inter/files/inter-latin-700-normal.woff2', weight: '700' },
    { path: '../node_modules/@fontsource/inter/files/inter-latin-800-normal.woff2', weight: '800' },
    { path: '../node_modules/@fontsource/inter/files/inter-latin-900-normal.woff2', weight: '900' },
  ],
  display: 'swap',
  variable: '--font-inter',
});

const outfit = localFont({
  src: [
    { path: '../node_modules/@fontsource/outfit/files/outfit-latin-100-normal.woff2', weight: '100' },
    { path: '../node_modules/@fontsource/outfit/files/outfit-latin-200-normal.woff2', weight: '200' },
    { path: '../node_modules/@fontsource/outfit/files/outfit-latin-300-normal.woff2', weight: '300' },
    { path: '../node_modules/@fontsource/outfit/files/outfit-latin-400-normal.woff2', weight: '400' },
    { path: '../node_modules/@fontsource/outfit/files/outfit-latin-500-normal.woff2', weight: '500' },
    { path: '../node_modules/@fontsource/outfit/files/outfit-latin-600-normal.woff2', weight: '600' },
    { path: '../node_modules/@fontsource/outfit/files/outfit-latin-700-normal.woff2', weight: '700' },
    { path: '../node_modules/@fontsource/outfit/files/outfit-latin-800-normal.woff2', weight: '800' },
    { path: '../node_modules/@fontsource/outfit/files/outfit-latin-900-normal.woff2', weight: '900' },
  ],
  display: 'swap',
  variable: '--font-outfit',
});

const jetbrainsMono = localFont({
  src: [
    { path: '../node_modules/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-100-normal.woff2', weight: '100' },
    { path: '../node_modules/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-200-normal.woff2', weight: '200' },
    { path: '../node_modules/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-300-normal.woff2', weight: '300' },
    { path: '../node_modules/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-400-normal.woff2', weight: '400' },
    { path: '../node_modules/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-500-normal.woff2', weight: '500' },
    { path: '../node_modules/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-600-normal.woff2', weight: '600' },
    { path: '../node_modules/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-700-normal.woff2', weight: '700' },
    { path: '../node_modules/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-800-normal.woff2', weight: '800' },
  ],
  display: 'swap',
  variable: '--font-jetbrains',
});

export const metadata: Metadata = {
  title: 'HotPlex AI',
  description: 'AI-powered coding agent — HotPlex Worker Gateway',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN" className={`dark ${inter.variable} ${outfit.variable} ${jetbrainsMono.variable}`}>
      <head />
      <body>
        <NuqsAdapter>{children}</NuqsAdapter>
      </body>
    </html>
  );
}
