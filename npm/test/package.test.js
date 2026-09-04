"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");

const packageJSON = require("../package.json");

test("does not depend on lifecycle scripts to install the CLI", () => {
  assert.equal(packageJSON.scripts?.postinstall, undefined);
});
