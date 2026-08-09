import { notFound } from "next/navigation";
import { KitchenSink } from "./KitchenSink";

// Dev-only (audit §11's own suggested fix: "even a minimal Storybook-less
// 'kitchen sink' route would pay for itself fast"). Guarded at the page
// level rather than left out of the build entirely — the standard Next.js
// pattern for a route nobody should reach in production: the segment
// still compiles (so a local `npm run build` catches it breaking), but any
// request against a real deployment 404s exactly like a route that never
// existed. No link to this page anywhere in the app's own nav.
export default function KitchenSinkPage() {
  if (process.env.NODE_ENV !== "development") notFound();
  return <KitchenSink />;
}
