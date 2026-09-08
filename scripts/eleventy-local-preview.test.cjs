"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const childProcess = require("node:child_process");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  createLoopbackServer,
  findOutputFile,
  listen,
  parseArguments,
  relativeDirectory,
} = require("./eleventy-local-preview.cjs");

test("rebases production Eleventy directories from a shadow input", () => {
  const shadowInput = path.join(os.tmpdir(), "homecms-shadow", "content");
  const productionInput = path.join(os.tmpdir(), "daily-blog", "content");
  assert.equal(
    relativeDirectory(shadowInput, path.join(productionInput, "../_includes")),
    path.relative(shadowInput, path.join(productionInput, "../_includes")),
  );
  assert.equal(
    path.resolve(shadowInput, relativeDirectory(shadowInput, path.join(productionInput, "_data"))),
    path.join(productionInput, "_data"),
  );
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

test("requires production input for the shadow command", () => {
  assert.throws(
    () => parseArguments(["--serve", "--input", "/tmp/shadow", "--output", "/tmp/output", "--port", "14123"]),
    /--input and --production-input are required/,
  );
});

test("uses the same production-relative directories in JSON mode", () => {
  const project = fs.mkdtempSync(path.join(os.tmpdir(), "homecms-eleventy-fixture-"));
  const packageDir = path.join(project, "node_modules", "@11ty", "eleventy");
  const productionInput = path.join(project, "content");
  const shadowInput = path.join(project, "shadow", "content");
  const output = path.join(project, "preview-output");
  fs.mkdirSync(packageDir, { recursive: true });
  fs.mkdirSync(productionInput, { recursive: true });
  fs.mkdirSync(shadowInput, { recursive: true });
  fs.writeFileSync(path.join(project, "package.json"), "{}");
  fs.writeFileSync(path.join(packageDir, "package.json"), '{"main":"index.js"}');
  fs.writeFileSync(path.join(packageDir, "index.js"), `
    const path = require("node:path");
    module.exports = class FakeEleventy {
      constructor(input, output, options) {
        const handlers = {};
        const directories = {
          input,
          data: path.join(input, "_data"),
          includes: path.join(input, "../_includes"),
          layouts: path.join(input, "../_layouts"),
          output,
          setInput(value) { this.input = value; },
          setData(value) { this.data = path.resolve(this.input, value); },
          setIncludes(value) { this.includes = path.resolve(this.input, value); },
          setLayouts(value) { this.layouts = path.resolve(this.input, value); },
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
      "--input", shadowInput,
      "--production-input", productionInput,
      "--output", output,
    ], { cwd: project, encoding: "utf8" });
    const [entry] = JSON.parse(stdout);
    assert.equal(entry.inputPath, path.join(shadowInput, "posts/one.md"));
    assert.equal(entry.data.directories.data, path.join(productionInput, "_data"));
    assert.equal(entry.data.directories.includes, path.join(project, "_includes"));
    assert.equal(entry.data.directories.layouts, path.join(project, "_layouts"));
    assert.equal(entry.outputPath, output);
  } finally {
    fs.rmSync(project, { recursive: true, force: true });
  }
});
