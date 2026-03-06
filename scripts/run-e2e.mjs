import { spawn } from "node:child_process";
import { createWriteStream } from "node:fs";
import { mkdir } from "node:fs/promises";
import net from "node:net";
import { finished } from "node:stream/promises";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const RUN_TIMEOUT_MS = 60_000;
const SHUTDOWN_TIMEOUT_MS = 5_000;

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, "..");

function parseArgs(argv) {
  let mode = "stable";
  let db = String(process.env.E2E_DB_DRIVER ?? "sqlite")
    .trim()
    .toLowerCase();
  const passthrough = [];
  let forcePassthrough = false;

  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--") {
      forcePassthrough = true;
      continue;
    }
    if (forcePassthrough) {
      passthrough.push(arg);
      continue;
    }
    if (arg.startsWith("--mode=")) {
      mode = String(arg.slice("--mode=".length) || "").trim().toLowerCase();
      continue;
    }
    if (arg.startsWith("--db=")) {
      db = String(arg.slice("--db=".length) || "").trim().toLowerCase();
      continue;
    }
    passthrough.push(arg);
  }

  return { mode, db, passthrough };
}

function isValidMode(mode) {
  return mode === "stable" || mode === "ci" || mode === "fast";
}

function isValidDB(db) {
  return db === "sqlite" || db === "postgres";
}

function goBinary() {
  return process.platform === "win32" ? "go.exe" : "go";
}

function npxBinary() {
  return process.platform === "win32" ? "cmd.exe" : "npx";
}

function dockerBinary() {
  return process.platform === "win32" ? "docker.exe" : "docker";
}

function npxSpawnArgs(baseArgs) {
  if (process.platform === "win32") {
    return ["/d", "/s", "/c", "npx", ...baseArgs];
  }
  return baseArgs;
}

function createRunID() {
  return new Date().toISOString().replace(/[:.]/g, "-");
}

function delay(ms) {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

function hasWorkersArg(args) {
  for (const arg of args) {
    if (arg === "--workers" || arg.startsWith("--workers=")) {
      return true;
    }
  }
  return false;
}

function onceExit(child) {
  return new Promise((resolve) => {
    child.once("exit", (code, signal) => {
      resolve({ code, signal });
    });
  });
}

async function stopChild(child) {
  if (!child || child.killed || child.exitCode !== null) {
    return;
  }

  if (process.platform === "win32") {
    await new Promise((resolve) => {
      const killer = spawn("taskkill", ["/pid", String(child.pid), "/t", "/f"], {
        cwd: repoRoot,
        stdio: "ignore",
      });
      killer.once("exit", () => resolve());
      killer.once("error", () => resolve());
    });
    return;
  }

  child.kill("SIGTERM");
  const exited = await Promise.race([
    onceExit(child).then(() => true),
    delay(SHUTDOWN_TIMEOUT_MS).then(() => false),
  ]);
  if (!exited) {
    child.kill("SIGKILL");
    await onceExit(child);
  }
}

async function waitForServer(url, child, timeoutMs) {
  const startedAt = Date.now();

  while (Date.now() - startedAt < timeoutMs) {
    if (child.exitCode !== null) {
      throw new Error(`App exited before readiness check (exit ${child.exitCode})`);
    }

    try {
      const response = await fetch(url, { redirect: "manual" });
      if (response.status >= 200 && response.status < 500) {
        return;
      }
    } catch {
      // Server is still booting.
    }

    await delay(500);
  }

  throw new Error(`App did not become ready within ${timeoutMs} ms`);
}

function spawnAndWait(command, args, options) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, options);
    child.once("error", (error) => reject(error));
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });
}

function printRunContext(context) {
  console.log(`[e2e] mode=${context.mode}`);
  console.log(`[e2e] base_url=${context.baseURL}`);
  console.log(`[e2e] db_driver=${context.dbDriver}`);
  console.log(`[e2e] log_file=${context.appLogPath}`);
  if (context.dbDriver === "sqlite") {
    console.log(`[e2e] db_path=${context.dbPath}`);
  } else {
    console.log("[e2e] db_runtime=temporary-docker-postgres");
  }
  if (context.workerOverride !== null) {
    console.log(`[e2e] workers=${context.workerOverride}`);
  } else {
    console.log("[e2e] workers=playwright-default");
  }
}

function createPostgresDatabaseName(runID) {
  const normalized = runID.toLowerCase().replace(/[^a-z0-9]+/g, "_");
  return `ovumcy_e2e_${normalized}`.slice(0, 63);
}

function onceConnect(host, port) {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection({ host, port });
    const onError = (error) => {
      socket.destroy();
      reject(error);
    };
    socket.once("error", onError);
    socket.once("connect", () => {
      socket.end();
      resolve();
    });
  });
}

function spawnAndCapture(command, args, options) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, options);
    let stdout = "";
    let stderr = "";

    child.once("error", (error) => reject(error));
    child.stdout?.on("data", (chunk) => {
      stdout += String(chunk);
    });
    child.stderr?.on("data", (chunk) => {
      stderr += String(chunk);
    });
    child.once("exit", (code, signal) => {
      if (code === 0) {
        resolve({ code, signal, stdout: stdout.trim(), stderr: stderr.trim() });
        return;
      }
      reject(
        new Error(
          `${command} ${args.join(" ")} failed with exit ${code ?? "unknown"}${stderr ? `: ${stderr.trim()}` : ""}`,
        ),
      );
    });
  });
}

async function runDockerCapture(args) {
  return spawnAndCapture(dockerBinary(), args, {
    cwd: repoRoot,
    stdio: ["ignore", "pipe", "pipe"],
  });
}

async function waitForDockerPostgres(containerID, user, databaseName) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < RUN_TIMEOUT_MS) {
    try {
      await runDockerCapture(["exec", containerID, "pg_isready", "-U", user, "-d", databaseName]);
      return;
    } catch {
      await delay(500);
    }
  }
  throw new Error(`Postgres container ${containerID} did not become ready in time`);
}

async function loadDockerPort(containerID) {
  const result = await runDockerCapture(["port", containerID, "5432/tcp"]);
  const firstLine = String(result.stdout || "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find(Boolean);
  if (!firstLine) {
    throw new Error(`Docker did not publish a port for Postgres container ${containerID}`);
  }
  const lastColon = firstLine.lastIndexOf(":");
  if (lastColon < 0 || lastColon === firstLine.length - 1) {
    throw new Error(`Unexpected docker port output: ${firstLine}`);
  }
  return Number.parseInt(firstLine.slice(lastColon + 1), 10);
}

async function waitForHostPort(host, port) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < RUN_TIMEOUT_MS) {
    try {
      await onceConnect(host, port);
      return;
    } catch {
      await delay(500);
    }
  }
  throw new Error(`Postgres host port ${host}:${port} did not become reachable in time`);
}

async function startPostgresRuntime(runID) {
  const databaseName = createPostgresDatabaseName(runID);
  const user = process.env.E2E_POSTGRES_USER ?? "ovumcy";
  const password = process.env.E2E_POSTGRES_PASSWORD ?? "ovumcy";
  const image = process.env.E2E_POSTGRES_IMAGE ?? "postgres:17-alpine";

  const result = await runDockerCapture([
    "run",
    "-d",
    "--rm",
    "-P",
    "-e",
    `POSTGRES_USER=${user}`,
    "-e",
    `POSTGRES_PASSWORD=${password}`,
    "-e",
    `POSTGRES_DB=${databaseName}`,
    image,
  ]);
  const containerID = result.stdout.trim();
  if (!containerID) {
    throw new Error("Docker did not return a Postgres container ID");
  }

  try {
    await waitForDockerPostgres(containerID, user, databaseName);
    const port = await loadDockerPort(containerID);
    await waitForHostPort("127.0.0.1", port);

    return {
      containerID,
      dsn: `postgres://${encodeURIComponent(user)}:${encodeURIComponent(password)}@127.0.0.1:${port}/${databaseName}?sslmode=disable`,
    };
  } catch (error) {
    await runDockerCapture(["rm", "-f", containerID]).catch(() => {});
    throw error;
  }
}

async function main() {
  const { mode, db, passthrough } = parseArgs(process.argv.slice(2));
  if (!isValidMode(mode)) {
    throw new Error(`Unsupported mode "${mode}". Expected one of: stable, ci, fast`);
  }
  if (!isValidDB(db)) {
    throw new Error(`Unsupported db "${db}". Expected one of: sqlite, postgres`);
  }

  const runID = createRunID();
  const tmpDir = path.join(repoRoot, ".tmp", "e2e");
  await mkdir(tmpDir, { recursive: true });

  const appPort = Number.parseInt(process.env.E2E_APP_PORT ?? "18080", 10);
  if (!Number.isInteger(appPort) || appPort < 1 || appPort > 65535) {
    throw new Error(`Invalid E2E_APP_PORT: ${process.env.E2E_APP_PORT ?? ""}`);
  }

  const dbPath = path.join(tmpDir, `run-${runID}.db`);
  const appLogPath = path.join(tmpDir, `app-${runID}.log`);
  const appLogStream = createWriteStream(appLogPath, { flags: "a" });

  const baseURL = process.env.PLAYWRIGHT_BASE_URL ?? `http://127.0.0.1:${appPort}`;
  const workerOverrideFromEnv = Number.parseInt(process.env.E2E_PLAYWRIGHT_WORKERS ?? "", 10);
  const workerOverride =
    Number.isInteger(workerOverrideFromEnv) && workerOverrideFromEnv > 0
      ? workerOverrideFromEnv
      : mode === "fast"
        ? null
        : 1;

  const runContext = {
    mode,
    baseURL,
    appLogPath,
    dbPath,
    dbDriver: db,
    workerOverride,
  };
  printRunContext(runContext);

  const postgresRuntime = db === "postgres" ? await startPostgresRuntime(runID) : null;

  const appEnv = {
    ...process.env,
    SECRET_KEY: process.env.SECRET_KEY ?? "0123456789abcdef0123456789abcdef",
    DB_DRIVER: db,
    DB_PATH: dbPath,
    DATABASE_URL: postgresRuntime?.dsn ?? "",
    PORT: String(appPort),
    TZ: process.env.TZ ?? "UTC",
    DEFAULT_LANGUAGE: process.env.DEFAULT_LANGUAGE ?? "en",
    COOKIE_SECURE: process.env.COOKIE_SECURE ?? "false",
    RATE_LIMIT_LOGIN_MAX: process.env.RATE_LIMIT_LOGIN_MAX ?? "500",
    RATE_LIMIT_FORGOT_PASSWORD_MAX: process.env.RATE_LIMIT_FORGOT_PASSWORD_MAX ?? "500",
    RATE_LIMIT_API_MAX: process.env.RATE_LIMIT_API_MAX ?? "5000",
  };

  const appArgs = ["run", "./cmd/ovumcy"];
  const appProcess = spawn(goBinary(), appArgs, {
    cwd: repoRoot,
    env: appEnv,
    stdio: ["ignore", "pipe", "pipe"],
  });
  appProcess.stdout.pipe(appLogStream);
  appProcess.stderr.pipe(appLogStream);

  try {
    await waitForServer(`${baseURL}/login`, appProcess, RUN_TIMEOUT_MS);

    const playwrightArgs = ["playwright", "test"];
    if (workerOverride !== null && !hasWorkersArg(passthrough)) {
      playwrightArgs.push(`--workers=${workerOverride}`);
    }
    playwrightArgs.push(...passthrough);

    const result = await spawnAndWait(npxBinary(), npxSpawnArgs(playwrightArgs), {
      cwd: repoRoot,
      env: {
        ...process.env,
        PLAYWRIGHT_BASE_URL: baseURL,
      },
      stdio: "inherit",
    });

    if (result.code !== 0) {
      throw new Error(`Playwright failed with exit code ${result.code ?? "unknown"}`);
    }
  } finally {
    await stopChild(appProcess);
    appProcess.stdout.unpipe(appLogStream);
    appProcess.stderr.unpipe(appLogStream);
    appLogStream.end();
    await finished(appLogStream);
    if (postgresRuntime?.containerID) {
      await runDockerCapture(["rm", "-f", postgresRuntime.containerID]).catch(() => {});
    }
  }

  console.log("[e2e] completed successfully");
}

main()
  .then(() => {
    // Explicit exit avoids CI hangs when some runtime handles stay alive unexpectedly.
    process.exit(0);
  })
  .catch((error) => {
    const message = error instanceof Error ? error.message : String(error);
    console.error(`[e2e] failed: ${message}`);
    process.exit(1);
  });
