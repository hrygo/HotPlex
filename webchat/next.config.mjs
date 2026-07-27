import fs from "node:fs";
import path from "node:path";

const PREFIX = "HOTPLEX_WEBCHAT_";

// Pre-load .env.local and .env into process.env so next.config.mjs
// captures variables defined in webchat/.env.local (e.g. HOTPLEX_WEBCHAT_WS_URL).
function loadLocalEnv() {
  const envFiles = [".env", ".env.local"];
  for (const file of envFiles) {
    const filePath = path.join(process.cwd(), file);
    if (fs.existsSync(filePath)) {
      const content = fs.readFileSync(filePath, "utf8");
      for (const line of content.split("\n")) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith("#")) continue;
        const idx = trimmed.indexOf("=");
        if (idx > 0) {
          const key = trimmed.slice(0, idx).trim();
          const value = trimmed.slice(idx + 1).trim();
          if (!(key in process.env)) {
            process.env[key] = value;
          }
        }
      }
    }
  }
}

loadLocalEnv();

// Auto-forward all HOTPLEX_WEBCHAT_* env vars to the client bundle.
// To add a new config: just set it in .env and read from lib/config.ts.
const env = Object.fromEntries(
  Object.entries(process.env)
    .filter(([k]) => k.startsWith(PREFIX))
    .map(([k, v]) => [k, v ?? ""]),
);

const nextConfig = { reactStrictMode: false, output: "export", env };

export default nextConfig;
