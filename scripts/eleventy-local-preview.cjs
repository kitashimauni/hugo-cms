#!/usr/bin/env node

"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");
const { createRequire } = require("node:module");

const RELOAD_SCRIPT_PATH = "/__hugo_cms_reload.js";
const RELOAD_SOCKET_PATH = "/__hugo_cms_live_reload";
const RELOAD_SCRIPT = `(() => {
  const protocol = location.protocol === "https:" ? "wss:" : "ws:";
  const socket = new WebSocket(protocol + "//" + location.host + "${RELOAD_SOCKET_PATH}");
  socket.addEventListener("message", (event) => {
    try {
      const message = JSON.parse(event.data);
      if (message.type === "eleventy.reload") location.reload();
    } catch (_) {
      // Ignore malformed development notifications.
    }
  });
})();`;

function parseArguments(argv) {
  const options = { json: false };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--json") {
      options.json = true;
      continue;
    }
    if (argument === "--serve") {
      options.serve = true;
      continue;
    }
    if (argument.startsWith("--") && index + 1 < argv.length) {
      const key = argument.slice(2).replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
      options[key] = argv[++index];
      continue;
    }
    throw new Error(`Unknown Eleventy local preview argument: ${argument}`);
  }
  if (!options.input || !options.productionInput) {
    throw new Error("--input and --production-input are required");
  }
  if (!options.json && (!options.output || !options.port || !options.host)) {
    throw new Error("--output, --port and --host are required for local preview serving");
  }
  return options;
}

function relativeDirectory(from, target) {
  const relative = path.relative(path.resolve(from), path.resolve(target));
  return relative || ".";
}

function configureProjectDirectories(eleventyConfig, options, notify) {
  eleventyConfig.userConfig.on("eleventy.beforeConfig", () => {
    const directories = eleventyConfig.directories;
    const productionInput = directories.input;
    const productionData = directories.data;
    const productionIncludes = directories.includes;
    const productionLayouts = directories.layouts;

    // Read the user's configuration against the production repository first,
    // then replace only the input/output roots. Rebased directory values keep
    // ../_includes, global data and layout paths anchored to the repository.
    directories.setInput(options.input);
    directories.setData(relativeDirectory(options.input, productionData));
    directories.setIncludes(relativeDirectory(options.input, productionIncludes));
    if (productionLayouts) {
      directories.setLayouts(relativeDirectory(options.input, productionLayouts));
    }
    directories.setOutput(options.output);

  });

  if (!options.json) {
    eleventyConfig.userConfig.on("eleventy.after", notify);
  }
}

function getEleventyClass() {
  const projectRequire = createRequire(path.join(process.cwd(), "package.json"));
  const loaded = projectRequire("@11ty/eleventy");
  return loaded.Eleventy || loaded.default || loaded;
}

function contentType(filePath) {
  const extension = path.extname(filePath).toLowerCase();
  const types = {
    ".css": "text/css; charset=utf-8",
    ".gif": "image/gif",
    ".html": "text/html; charset=utf-8",
    ".ico": "image/x-icon",
    ".jpeg": "image/jpeg",
    ".jpg": "image/jpeg",
    ".js": "text/javascript; charset=utf-8",
    ".json": "application/json; charset=utf-8",
    ".png": "image/png",
    ".svg": "image/svg+xml",
    ".txt": "text/plain; charset=utf-8",
    ".webp": "image/webp",
    ".woff": "font/woff",
    ".woff2": "font/woff2",
  };
  return types[extension] || "application/octet-stream";
}

function findOutputFile(outputRoot, pathname) {
  let decodedPath;
  try {
    decodedPath = decodeURIComponent(pathname);
  } catch (_) {
    return { error: 400 };
  }
  const root = path.resolve(outputRoot);
  const relativePath = decodedPath.replace(/^[/\\]+/, "");
  const candidate = path.resolve(root, relativePath);
  if (candidate !== root && !candidate.startsWith(`${root}${path.sep}`)) {
    return { error: 400 };
  }

  try {
    const info = fs.statSync(candidate);
    if (info.isFile()) return { filePath: candidate, redirect: null };
    if (info.isDirectory()) {
      if (!pathname.endsWith("/")) return { redirect: `${pathname}/` };
      const indexPath = path.join(candidate, "index.html");
      if (fs.existsSync(indexPath)) return { filePath: indexPath, redirect: null };
    }
  } catch (_) {
    // Continue with Eleventy’s extension and trailing-slash conventions.
  }

  const htmlPath = `${candidate}.html`;
  if (fs.existsSync(htmlPath)) return { filePath: htmlPath, redirect: null };
  const indexPath = path.join(candidate, "index.html");
  if (fs.existsSync(indexPath)) {
    return pathname.endsWith("/")
      ? { filePath: indexPath, redirect: null }
      : { redirect: `${pathname}/` };
  }
  return { error: 404 };
}

function injectReloadScript(body) {
  const tag = `<script src="${RELOAD_SCRIPT_PATH}"></script>`;
  if (body.includes("</head>")) return body.replace("</head>", `${tag}</head>`);
  if (body.includes("</body>")) return body.replace("</body>", `${tag}</body>`);
  return `${body}${tag}`;
}

function websocketFrame(message) {
  const payload = Buffer.from(JSON.stringify(message));
  if (payload.length < 126) {
    return Buffer.concat([Buffer.from([0x81, payload.length]), payload]);
  }
  if (payload.length < 65536) {
    const header = Buffer.alloc(4);
    header[0] = 0x81;
    header[1] = 126;
    header.writeUInt16BE(payload.length, 2);
    return Buffer.concat([header, payload]);
  }
  throw new Error("Eleventy local preview reload message is too large");
}

function createLoopbackServer(outputRoot) {
  const clients = new Set();
  const server = http.createServer((request, response) => {
    const requestURL = new URL(request.url || "/", "http://127.0.0.1/");
    if (requestURL.pathname === RELOAD_SCRIPT_PATH) {
      const body = Buffer.from(RELOAD_SCRIPT);
      response.writeHead(200, { "Content-Type": "text/javascript; charset=utf-8", "Content-Length": body.length });
      if (request.method !== "HEAD") response.end(body);
      else response.end();
      return;
    }

    const match = findOutputFile(outputRoot, requestURL.pathname);
    if (match.error) {
      response.writeHead(match.error, { "Content-Type": "text/plain; charset=utf-8" });
      response.end(match.error === 404 ? "Not found" : "Invalid path");
      return;
    }
    if (match.redirect) {
      response.writeHead(301, { Location: match.redirect });
      response.end();
      return;
    }

    let body = fs.readFileSync(match.filePath);
    const type = contentType(match.filePath);
    if (type.startsWith("text/html")) {
      body = Buffer.from(injectReloadScript(body.toString("utf8")));
    }
    response.writeHead(200, {
      "Cache-Control": "no-store",
      "Content-Length": body.length,
      "Content-Type": type,
    });
    if (request.method !== "HEAD") response.end(body);
    else response.end();
  });

  server.on("upgrade", (request, socket) => {
    const requestURL = new URL(request.url || "/", "http://127.0.0.1/");
    const key = request.headers["sec-websocket-key"];
    if (requestURL.pathname !== RELOAD_SOCKET_PATH || typeof key !== "string") {
      socket.destroy();
      return;
    }
    const accept = crypto.createHash("sha1").update(`${key}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`).digest("base64");
    socket.write([
      "HTTP/1.1 101 Switching Protocols",
      "Upgrade: websocket",
      "Connection: Upgrade",
      `Sec-WebSocket-Accept: ${accept}`,
      "\r\n",
    ].join("\r\n"));
    clients.add(socket);
    socket.on("close", () => clients.delete(socket));
    socket.on("error", () => clients.delete(socket));
  });

  server.broadcast = (message) => {
    const frame = websocketFrame(message);
    for (const client of clients) {
      if (!client.destroyed) client.write(frame);
    }
  };

  server.closeAll = () => {
    for (const client of clients) client.destroy();
    clients.clear();
  };
  return server;
}

function listen(server, port, host) {
  return new Promise((resolve, reject) => {
    const onError = (error) => {
      server.off("listening", onListening);
      reject(error);
    };
    const onListening = () => {
      server.off("error", onError);
      resolve();
    };
    server.once("error", onError);
    server.once("listening", onListening);
    server.listen({ host, port: Number(port) });
  });
}

async function closeServer(server) {
  if (!server) return;
  server.closeAll();
  if (!server.listening) return;
  await new Promise((resolve) => server.close(() => resolve()));
}

async function main(argv = process.argv.slice(2)) {
  const options = parseArguments(argv);
  const Eleventy = getEleventyClass();
  let server;
  let stopping = false;
  const notify = () => server?.broadcast({ type: "eleventy.reload" });
  const eleventy = new Eleventy(options.productionInput, options.output, {
    source: "script",
    runMode: options.json ? "build" : "serve",
    quietMode: options.json,
    config: (eleventyConfig) => configureProjectDirectories(eleventyConfig, options, notify),
  });

  if (options.json) {
    const result = await eleventy.toJSON();
    process.stdout.write(JSON.stringify(result));
    return;
  }

  await eleventy.init();
  await eleventy.watch();
  server = createLoopbackServer(options.output);
  await listen(server, options.port, options.host);

  const shutdown = async () => {
    if (stopping) return;
    stopping = true;
    await eleventy.stopWatch();
    await closeServer(server);
    process.exit(0);
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

if (require.main === module) {
  main().catch((error) => {
    console.error(error?.stack || error);
    process.exitCode = 1;
  });
}

module.exports = {
  configureProjectDirectories,
  createLoopbackServer,
  findOutputFile,
  listen,
  parseArguments,
  relativeDirectory,
};
