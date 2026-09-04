"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");

const RELEASE_BASE_URL = "https://github.com/xkumiyu/agentstats/releases/download";

const GO_OS_BY_NODE_PLATFORM = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const GO_ARCH_BY_NODE_ARCH = {
  arm64: "arm64",
  x64: "amd64",
};

function targetFor(platform, arch) {
  const goOS = GO_OS_BY_NODE_PLATFORM[platform];
  const goArch = GO_ARCH_BY_NODE_ARCH[arch];

  if (!goOS || !goArch) {
    throw new Error(`Unsupported platform or architecture: ${platform}/${arch}`);
  }

  return { os: goOS, arch: goArch };
}

function assetName(version, target) {
  const name = `agentstats_${version}_${target.os}_${target.arch}`;
  return target.os === "windows" ? `${name}.exe` : name;
}

function releaseURL(version, asset) {
  return `${RELEASE_BASE_URL}/v${version}/${asset}`;
}

function checksumFor(contents, asset) {
  for (const line of contents.split(/\r?\n/)) {
    const fields = line.trim().split(/\s+/);
    if (fields.length < 2 || (fields[1] !== asset && fields[1] !== `*${asset}`)) {
      continue;
    }

    if (!/^[0-9a-f]{64}$/i.test(fields[0])) {
      throw new Error(`Invalid SHA-256 checksum for ${asset}`);
    }
    return fields[0].toLowerCase();
  }

  throw new Error(`Checksum not found for ${asset}`);
}

async function download(url) {
  if (typeof fetch !== "function") {
    throw new Error("Node.js 18 or newer is required to install agentstats");
  }

  const response = await fetch(url, {
    headers: {
      "User-Agent": "agentstats-npm-installer",
    },
  });

  if (!response.ok) {
    throw new Error(`Download failed (${response.status}) for ${url}`);
  }

  return Buffer.from(await response.arrayBuffer());
}

function nativeBinaryPath(target) {
  const filename = target.os === "windows" ? "agentstats-bin.exe" : "agentstats-bin";
  return path.join(__dirname, "..", "bin", filename);
}

async function install() {
  const packageJSONPath = path.join(__dirname, "..", "package.json");
  const packageJSON = JSON.parse(fs.readFileSync(packageJSONPath, "utf8"));
  const target = targetFor(process.platform, process.arch);
  const asset = assetName(packageJSON.version, target);
  const checksumsURL = releaseURL(packageJSON.version, "checksums.txt");
  const assetURL = releaseURL(packageJSON.version, asset);

  const checksums = (await download(checksumsURL)).toString("utf8");
  const expectedChecksum = checksumFor(checksums, asset);
  const binary = await download(assetURL);
  const actualChecksum = crypto.createHash("sha256").update(binary).digest("hex");

  if (actualChecksum !== expectedChecksum) {
    throw new Error(
      `Checksum mismatch for ${asset}: expected ${expectedChecksum}, got ${actualChecksum}`,
    );
  }

  const destination = nativeBinaryPath(target);
  const temporaryDestination = `${destination}.tmp-${process.pid}`;

  try {
    fs.writeFileSync(temporaryDestination, binary, { mode: 0o755 });
    if (process.platform !== "win32") {
      fs.chmodSync(temporaryDestination, 0o755);
    }
    if (process.platform === "win32") {
      fs.rmSync(destination, { force: true });
    }
    fs.renameSync(temporaryDestination, destination);
  } finally {
    fs.rmSync(temporaryDestination, { force: true });
  }

  console.log(`Installed agentstats ${packageJSON.version} for ${target.os}/${target.arch}`);
}

if (require.main === module) {
  install().catch((error) => {
    console.error(`[agentstats] ${error.message}`);
    process.exitCode = 1;
  });
}

module.exports = {
  assetName,
  checksumFor,
  install,
  releaseURL,
  targetFor,
};
