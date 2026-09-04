"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  assetName,
  checksumFor,
  releaseURL,
  targetFor,
} = require("../scripts/install.js");

test("maps Node platform and architecture to a GoReleaser target", () => {
  assert.deepEqual(targetFor("darwin", "arm64"), {
    os: "darwin",
    arch: "arm64",
  });
  assert.deepEqual(targetFor("win32", "x64"), {
    os: "windows",
    arch: "amd64",
  });
});

test("rejects unsupported targets", () => {
  assert.throws(
    () => targetFor("aix", "x64"),
    /Unsupported platform or architecture: aix\/x64/,
  );
  assert.throws(
    () => targetFor("linux", "ppc64"),
    /Unsupported platform or architecture: linux\/ppc64/,
  );
});

test("builds raw release asset names", () => {
  assert.equal(
    assetName("1.2.3", { os: "linux", arch: "amd64" }),
    "agentstats_1.2.3_linux_amd64",
  );
  assert.equal(
    assetName("1.2.3", { os: "windows", arch: "amd64" }),
    "agentstats_1.2.3_windows_amd64.exe",
  );
});

test("builds a release asset URL", () => {
  assert.equal(
    releaseURL("1.2.3", "agentstats_1.2.3_linux_amd64"),
    "https://github.com/xkumiyu/agentstats/releases/download/v1.2.3/agentstats_1.2.3_linux_amd64",
  );
});

test("selects the exact SHA-256 checksum entry", () => {
  const asset = "agentstats_1.2.3_linux_amd64";
  const checksums = [
    `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  ${asset}`,
    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  agentstats_1.2.3_windows_amd64.exe",
  ].join("\n");

  assert.equal(
    checksumFor(checksums, asset),
    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  );
});

test("rejects a missing or malformed checksum entry", () => {
  assert.throws(
    () => checksumFor("aaaaaaaa  other-file", "agentstats_1.2.3_linux_amd64"),
    /Checksum not found for agentstats_1.2.3_linux_amd64/,
  );
  assert.throws(
    () => checksumFor("not-a-sha  agentstats_1.2.3_linux_amd64", "agentstats_1.2.3_linux_amd64"),
    /Invalid SHA-256 checksum for agentstats_1.2.3_linux_amd64/,
  );
});
