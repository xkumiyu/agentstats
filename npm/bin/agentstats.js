#!/usr/bin/env node

"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const nativeFilename = process.platform === "win32" ? "agentstats-bin.exe" : "agentstats-bin";
const nativePath = path.join(__dirname, nativeFilename);

if (!fs.existsSync(nativePath)) {
  console.error(
    "agentstats: native binary is missing; reinstall without --ignore-scripts: npm install --global @xkumiyu/agentstats",
  );
  process.exitCode = 1;
} else {
  const result = spawnSync(nativePath, process.argv.slice(2), {
    stdio: "inherit",
  });

  if (result.error) {
    console.error(`[agentstats] ${result.error.message}`);
    process.exitCode = 1;
  } else {
    process.exitCode = result.status ?? 1;
  }
}
