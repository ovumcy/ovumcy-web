import { spawn } from "node:child_process";
import { createWriteStream, existsSync } from "node:fs";
import { mkdir } from "node:fs/promises";
import net from "node:net";
import { createRequire } from "node:module";
import { finished } from "node:stream/promises";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { startLocalHTTPSProxy, startLocalOIDCProvider } from "./e2e-runtime.mjs";

const RUN_TIMEOUT_MS = 60_000;
const SHUTDOWN_TIMEOUT_MS = 5_000;
const REDACTED = "[REDACTED]";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, "..");
const require = createRequire(import.meta.url);
const playwrightCLIPath = require.resolve("@playwright/test/cli");

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
  if (process.platform !== "win32") {
    return "go";
  }

  const configured = String(process.env.GO_BINARY ?? "").trim();
  if (configured) {
    return configured;
  }

  const candidates = [
    path.join(process.env.ProgramFiles ?? "C:\\Program Files", "Go", "bin", "go.exe"),
  ];
  if (process.env.GOROOT) {
    candidates.unshift(path.join(process.env.GOROOT, "bin", "go.exe"));
  }

  for (const candidate of candidates) {
    if (candidate && existsSync(candidate)) {
      return candidate;
    }
  }

  return "go.exe";
}

function dockerBinary() {
  return process.platform === "win32" ? "docker.exe" : "docker";
}

function createRunID() {
  return new Date().toISOString().replace(/[:.]/g, "-");
}

function delay(ms) {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

function parsePortValue(value, label) {
  const trimmed = String(value ?? "").trim();
  if (!trimmed) {
    return null;
  }

  const port = Number.parseInt(trimmed, 10);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`Invalid ${label}: ${trimmed}`);
  }

  return port;
}

function reservePort(port = 0) {
  return new Promise((resolve, reject) => {
    const server = net.createServer();

    server.unref();
    server.once("error", (error) => reject(error));

    server.once("listening", () => {
      const address = server.address();
      const reservedPort = address && typeof address === "object" ? address.port : 0;
      server.close((closeError) => {
        if (closeError) {
          reject(closeError);
          return;
        }
        if (!Number.isInteger(reservedPort) || reservedPort < 1 || reservedPort > 65535) {
          reject(new Error("Failed to reserve a valid app port"));
          return;
        }
        resolve(reservedPort);
      });
    });

    server.listen(port, "127.0.0.1");
  });
}

async function resolveAppPort() {
  const explicitPort = parsePortValue(process.env.E2E_APP_PORT ?? "", "E2E_APP_PORT");
  if (explicitPort !== null) {
    return explicitPort;
  }

  return reservePort(0);
}

function redactSensitiveText(value) {
  let text = String(value ?? "");
  const secrets = [
    String(process.env.E2E_POSTGRES_PASSWORD ?? "").trim(),
    String(process.env.DATABASE_URL ?? "").trim(),
  ]
    .filter(Boolean)
    .sort((left, right) => right.length - left.length);

  for (const secret of secrets) {
    text = text.split(secret).join(REDACTED);
  }

  text = text.replace(/(POSTGRES_PASSWORD=)([^\s]+)/gi, `$1${REDACTED}`);
  text = text.replace(/(password=)([^\s]+)/gi, `$1${REDACTED}`);
  text = text.replace(/(postgres(?:ql)?:\/\/[^:\s]+:)([^@/\s]+)(@)/gi, `$1${REDACTED}$3`);

  return text;
}

function formatFailedCommand(command, args) {
  return [command, ...args].map((arg) => redactSensitiveText(arg)).join(" ");
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
  if (context.appURL !== context.baseURL) {
    console.log(`[e2e] app_url=${context.appURL}`);
  }
  console.log(`[e2e] db_driver=${context.dbDriver}`);
  console.log(`[e2e] log_file=${context.appLogPath}`);
  if (context.dbDriver === "sqlite") {
    console.log(`[e2e] db_path=${context.dbPath}`);
  } else {
    console.log("[e2e] db_runtime=temporary-docker-postgres");
  }
  if (context.localOIDCIssuer) {
    console.log(`[e2e] oidc_issuer=${context.localOIDCIssuer}`);
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
      const failedCommand = formatFailedCommand(command, args);
      const safeStderr = redactSensitiveText(stderr.trim());
      reject(
        new Error(
          `${failedCommand} failed with exit ${code ?? "unknown"}${safeStderr ? `: ${safeStderr}` : ""}`,
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

async function generateLocalTLSFixture(tmpDir, runID) {
  const certPath = path.join(tmpDir, `localhost-${runID}.cert.pem`);
  const keyPath = path.join(tmpDir, `localhost-${runID}.key.pem`);

  await spawnAndCapture(
    goBinary(),
    ["run", "./scripts/e2e-tls-cert", "--cert", certPath, "--key", keyPath],
    {
      cwd: repoRoot,
      stdio: ["ignore", "pipe", "pipe"],
    },
  );

  return { certPath, keyPath };
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

  const appPort = await resolveAppPort();
  const localOIDCProviderEnabled =
    String(process.env.E2E_OIDC_PROVIDER ?? "").trim().toLowerCase() === "local";
  const cookieSecureEnabled = String(process.env.COOKIE_SECURE ?? "false").trim().toLowerCase() === "true";
  const useHTTPSProxy =
    String(process.env.E2E_USE_HTTPS_PROXY ?? "").trim().toLowerCase() === "true" ||
    cookieSecureEnabled ||
    localOIDCProviderEnabled;
  const tlsFixture = useHTTPSProxy || localOIDCProviderEnabled ? await generateLocalTLSFixture(tmpDir, runID) : null;
  const publicPort = useHTTPSProxy ? await reservePort(0) : appPort;
  const appURL = `http://127.0.0.1:${appPort}`;
  const baseURL =
    process.env.PLAYWRIGHT_BASE_URL ??
    (useHTTPSProxy ? `https://127.0.0.1:${publicPort}` : appURL);
  const localOIDCPort = localOIDCProviderEnabled ? await reservePort(0) : null;
  const localOIDCIssuer = localOIDCPort ? `https://127.0.0.1:${localOIDCPort}` : "";

  const dbPath = path.join(tmpDir, `run-${runID}.db`);
  const appLogPath = path.join(tmpDir, `app-${runID}.log`);
  const appLogStream = createWriteStream(appLogPath, { flags: "a" });

  const workerOverrideFromEnv = Number.parseInt(process.env.E2E_PLAYWRIGHT_WORKERS ?? "", 10);
  const fastModeNeedsWin32Cap = mode === "fast" && process.platform === "win32";
  if (fastModeNeedsWin32Cap && !(Number.isInteger(workerOverrideFromEnv) && workerOverrideFromEnv > 0)) {
    console.log("[e2e] win32 detected in fast mode: capping to 1 worker (SQLite-backed scenarios need serial execution here)");
  }
  const workerOverride =
    Number.isInteger(workerOverrideFromEnv) && workerOverrideFromEnv > 0
      ? workerOverrideFromEnv
      : mode === "fast"
        ? fastModeNeedsWin32Cap
          ? 1
          : null
        : 1;

  const runContext = {
    mode,
    baseURL,
    appURL,
    appLogPath,
    dbPath,
    dbDriver: db,
    localOIDCIssuer,
    workerOverride,
  };
  printRunContext(runContext);

  const postgresRuntime = db === "postgres" ? await startPostgresRuntime(runID) : null;
  const httpsProxy =
    useHTTPSProxy
      ? await startLocalHTTPSProxy({
          certPath: tlsFixture.certPath,
          keyPath: tlsFixture.keyPath,
          listenPort: publicPort,
          targetPort: appPort,
        })
      : null;
  const localOIDCProvider =
    localOIDCProviderEnabled
      ? await startLocalOIDCProvider({
          certPath: tlsFixture.certPath,
          keyPath: tlsFixture.keyPath,
          listenPort: localOIDCPort,
          clientID: process.env.OIDC_CLIENT_ID ?? "ovumcy-e2e",
          clientSecret: process.env.OIDC_CLIENT_SECRET ?? "ovumcy-e2e-secret",
          redirectURL: process.env.OIDC_REDIRECT_URL ?? `${baseURL}/auth/oidc/callback`,
          issuerURL: localOIDCIssuer,
          testEmail: process.env.OIDC_TEST_PROVIDER_EMAIL ?? "oidc-browser@example.com",
          testSubject: process.env.OIDC_TEST_PROVIDER_SUB ?? "oidc-browser-user",
          testName: process.env.OIDC_TEST_PROVIDER_NAME ?? "OIDC Browser User",
          emailVerified:
            String(process.env.OIDC_TEST_PROVIDER_EMAIL_VERIFIED ?? "true").trim().toLowerCase() !==
            "false",
        })
      : null;

  const appEnv = {
    ...process.env,
    SECRET_KEY: process.env.SECRET_KEY ?? "0123456789abcdef0123456789abcdef",
    DB_DRIVER: db,
    DB_PATH: dbPath,
    DATABASE_URL: postgresRuntime?.dsn ?? "",
    PORT: String(appPort),
    TZ: process.env.TZ ?? "UTC",
    DEFAULT_LANGUAGE: process.env.DEFAULT_LANGUAGE ?? "en",
    COOKIE_SECURE: process.env.COOKIE_SECURE ?? (useHTTPSProxy ? "true" : "false"),
    RATE_LIMIT_LOGIN_MAX: process.env.RATE_LIMIT_LOGIN_MAX ?? "500",
    RATE_LIMIT_FORGOT_PASSWORD_MAX: process.env.RATE_LIMIT_FORGOT_PASSWORD_MAX ?? "500",
    RATE_LIMIT_REGISTER_MAX: process.env.RATE_LIMIT_REGISTER_MAX ?? "500",
    RATE_LIMIT_API_MAX: process.env.RATE_LIMIT_API_MAX ?? "5000",
    OIDC_ENABLED: process.env.OIDC_ENABLED ?? (localOIDCProviderEnabled ? "true" : "false"),
    OIDC_ISSUER_URL: process.env.OIDC_ISSUER_URL ?? localOIDCIssuer,
    OIDC_CLIENT_ID: process.env.OIDC_CLIENT_ID ?? (localOIDCProviderEnabled ? "ovumcy-e2e" : ""),
    OIDC_CLIENT_SECRET:
      process.env.OIDC_CLIENT_SECRET ?? (localOIDCProviderEnabled ? "ovumcy-e2e-secret" : ""),
    OIDC_REDIRECT_URL:
      process.env.OIDC_REDIRECT_URL ??
      (localOIDCProviderEnabled ? `${baseURL}/auth/oidc/callback` : ""),
    OIDC_CA_FILE: process.env.OIDC_CA_FILE ?? (localOIDCProviderEnabled ? tlsFixture.certPath : ""),
    OIDC_POST_LOGOUT_REDIRECT_URL:
      process.env.OIDC_POST_LOGOUT_REDIRECT_URL ??
      (localOIDCProviderEnabled ? `${baseURL}/login` : ""),
    SSL_CERT_FILE: process.env.SSL_CERT_FILE ?? (localOIDCProviderEnabled ? tlsFixture.certPath : ""),
  };
  const playwrightEnv = {
    ...process.env,
    PLAYWRIGHT_BASE_URL: baseURL,
    PLAYWRIGHT_IGNORE_HTTPS_ERRORS: useHTTPSProxy || localOIDCProviderEnabled ? "true" : "false",
    COOKIE_SECURE: appEnv.COOKIE_SECURE,
    OIDC_ENABLED: appEnv.OIDC_ENABLED,
    OIDC_ISSUER_URL: appEnv.OIDC_ISSUER_URL,
    OIDC_CLIENT_ID: appEnv.OIDC_CLIENT_ID,
    OIDC_REDIRECT_URL: appEnv.OIDC_REDIRECT_URL,
    OIDC_CA_FILE: appEnv.OIDC_CA_FILE,
    OIDC_LOGIN_MODE: process.env.OIDC_LOGIN_MODE ?? "hybrid",
    OIDC_AUTO_PROVISION: process.env.OIDC_AUTO_PROVISION ?? "false",
    OIDC_POST_LOGOUT_REDIRECT_URL: appEnv.OIDC_POST_LOGOUT_REDIRECT_URL,
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
    await waitForServer(`${appURL}/login`, appProcess, RUN_TIMEOUT_MS);

    const playwrightArgs = [playwrightCLIPath, "test"];
    if (workerOverride !== null && !hasWorkersArg(passthrough)) {
      playwrightArgs.push(`--workers=${workerOverride}`);
    }
    playwrightArgs.push(...passthrough);

    const result = await spawnAndWait(process.execPath, playwrightArgs, {
      cwd: repoRoot,
      env: playwrightEnv,
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
    if (localOIDCProvider) {
      await localOIDCProvider.close().catch(() => {});
    }
    if (httpsProxy) {
      await httpsProxy.close().catch(() => {});
    }
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
    const message = redactSensitiveText(error instanceof Error ? error.message : String(error));
    console.error(`[e2e] failed: ${message}`);
    process.exit(1);
  });
