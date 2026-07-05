import { defineConfig } from "vitest/config";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.dirname(fileURLToPath(import.meta.url));

// Vitest config for pure-logic unit tests (no React/jsdom needed yet).
// Path alias mirrors tsconfig.json `@/*` → `./*`.
export default defineConfig({
    resolve: {
        alias: {
            "@": root,
        },
    },
    test: {
        environment: "node",
        include: ["lib/**/*.test.ts", "lib/**/*.spec.ts"],
        // Exclude:
        //  - e2e (playwright) and build artifacts
        //  - session-select.test.ts: uses node:test native runner, run via
        //    `node --test --experimental-strip-types` (see file header), not vitest
        exclude: [
            "node_modules/**",
            ".next/**",
            "e2e/**",
            "lib/session-select.test.ts",
        ],
    },
});
