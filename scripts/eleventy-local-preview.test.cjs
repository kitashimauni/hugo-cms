"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const childProcess = require("node:child_process");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  createLoopbackServer,
  configureProjectDirectories,
  findOutputFile,
  listen,
  parseArguments,
} = require("./eleventy-local-preview.cjs");

test("keeps Eleventy directories project-root relative", () => {
  const handlers = {};
  const directories = {
    input: "/production/src",
    data: "/production/_data",
    includes: "/production/_includes",
    layouts: "/production/_layouts",
    output: "/production/public",
    setInput(value) { this.input = value; },
    setOutput(value) { this.output = value; },
  };
  configureProjectDirectories(
    { directories, userConfig: { on(name, callback) { handlers[name] = callback; } } },
    { input: "src", output: "/preview/public", json: true },
    () => {},
  );
  handlers["eleventy.beforeConfig"]();
  assert.equal(directories.input, "src");
  assert.equal(directories.data, "/production/_data");
  assert.equal(directories.includes, "/production/_includes");
  assert.equal(directories.layouts, "/production/_layouts");
  assert.equal(directories.output, "/preview/public");
});

test("serves output index paths without allowing traversal", () => {
  const output = fs.mkdtempSync(path.join(os.tmpdir(), "homecms-eleventy-output-"));
  fs.mkdirSync(path.join(output, "posts"));
  fs.writeFileSync(path.join(output, "posts", "index.html"), "ok");
  try {
    assert.equal(findOutputFile(output, "/posts/").filePath, path.join(output, "posts", "index.html"));
    assert.equal(findOutputFile(output, "/posts").redirect, "/posts/");
    assert.equal(findOutputFile(output, "/../secret").error, 400);
  } finally {
    fs.rmSync(output, { recursive: true, force: true });
  }
});

test("binds the preview server to loopback", async () => {
  const output = fs.mkdtempSync(path.join(os.tmpdir(), "homecms-eleventy-output-"));
  const server = createLoopbackServer(output);
  try {
    await listen(server, 0, "127.0.0.1");
    assert.equal(server.address().address, "127.0.0.1");
  } finally {
    await new Promise((resolve) => {
      server.closeAll();
      server.close(() => resolve());
    });
    fs.rmSync(output, { recursive: true, force: true });
  }
});

test("requires an input directory", () => {
  assert.throws(
    () => parseArguments(["--serve", "--output", "/tmp/output", "--port", "14123"]),
    /--input is required/,
  );
});

test("uses the project-root overlay in JSON mode", () => {
  const project = fs.mkdtempSync(path.join(os.tmpdir(), "homecms-eleventy-fixture-"));
  const packageDir = path.join(project, "node_modules", "@11ty", "eleventy");
  const input = path.join(project, "src");
  const output = path.join(project, "public");
  fs.mkdirSync(packageDir, { recursive: true });
  fs.mkdirSync(path.join(input, "posts"), { recursive: true });
  fs.mkdirSync(path.join(project, "_data"), { recursive: true });
  fs.mkdirSync(path.join(project, "_includes"), { recursive: true });
  fs.mkdirSync(path.join(project, "_layouts"), { recursive: true });
  fs.writeFileSync(path.join(project, "package.json"), "{}");
  fs.writeFileSync(path.join(packageDir, "package.json"), '{"main":"index.js"}');
  fs.writeFileSync(path.join(packageDir, "index.js"), `
    const path = require("node:path");
    module.exports = class FakeEleventy {
      constructor(input, output, options) {
        const handlers = {};
        const root = process.cwd();
        const directories = {
          input: path.resolve(root, input),
          data: path.join(root, "_data"),
          includes: path.join(root, "_includes"),
          layouts: path.join(root, "_layouts"),
          output,
          setInput(value) { this.input = path.resolve(root, value); },
          setOutput(value) { this.output = value; },
        };
        options.config({
          directories,
          userConfig: { on(name, callback) { handlers[name] = callback; } },
        });
        handlers["eleventy.beforeConfig"]();
        this.directories = directories;
      }
      async toJSON() {
        return [{
          inputPath: path.join(this.directories.input, "posts/one.md"),
          outputPath: this.directories.output,
          data: {
            directories: {
              data: this.directories.data,
              includes: this.directories.includes,
              layouts: this.directories.layouts,
            },
          },
        }];
      }
    };
  `);

  try {
    const script = path.resolve(__dirname, "eleventy-local-preview.cjs");
    const stdout = childProcess.execFileSync(process.execPath, [
      script,
      "--json",
      "--input", "src",
      "--output", output,
    ], { cwd: project, encoding: "utf8" });
    const [entry] = JSON.parse(stdout);
    assert.equal(entry.inputPath, path.join(input, "posts/one.md"));
    assert.equal(entry.data.directories.data, path.join(project, "_data"));
    assert.equal(entry.data.directories.includes, path.join(project, "_includes"));
    assert.equal(entry.data.directories.layouts, path.join(project, "_layouts"));
    assert.equal(entry.outputPath, output);
  } finally {
    fs.rmSync(project, { recursive: true, force: true });
  }
});
