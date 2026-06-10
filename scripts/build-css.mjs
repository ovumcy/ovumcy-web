import { spawnSync } from "node:child_process";

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
    stdio: "inherit"
  }
);

if (buildResult.error) {
  throw buildResult.error;
}

if (typeof buildResult.status === "number" && buildResult.status !== 0) {
  process.exit(buildResult.status);
}
