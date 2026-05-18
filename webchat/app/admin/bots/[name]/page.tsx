import { BotDetailView } from './bot-detail-view';

// Placeholder path so Next.js generates the HTML shell for this dynamic route.
// The Go SPA server falls back to index.html for all paths, so the client-side
// router handles actual bot names at runtime.
export function generateStaticParams() {
  return [{ name: '_bot' }];
}

export default function BotDetailPage() {
  return <BotDetailView />;
}
