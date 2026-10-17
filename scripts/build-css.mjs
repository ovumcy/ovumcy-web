import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const buildResult = spawnSync(
  process.execPath,
  [
    "./node_modules/@tailwindcss/cli/dist/index.mjs",
    "-i",
    "./web/src/css/input.css",
    "-o",
    "./web/static/css/tailwind.css",
    "--minify"
  ],
  {
    // The paths above, and the sources Tailwind scans, belong to this script's
    // own checkout, whatever directory it was started from.
    cwd: fileURLToPath(new URL("..", import.meta.url)),
    stdio: "inherit"
  }
);

if (buildResult.error) {
  throw buildResult.error;
}

if (typeof buildResult.status === "number" && buildResult.status !== 0) {
  process.exit(buildResult.status);
}
