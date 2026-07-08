import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readdirSync, readFileSync, statSync, lstatSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import YAML from "yaml";

const websiteRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(websiteRoot, "..");
const catalogRoot = path.join(repoRoot, "catalog");
const generatedJSON = path.join(websiteRoot, "src", "data", "catalog.generated.json");
const registryIndex = path.join(websiteRoot, "public", "registry", "index.yaml");
const excludedDirs = new Set([".git", ".jeju-dev", "runs", "cache"]);

const localSources = process.env.JEJU_CATALOG_LOCAL_SOURCES === "1";
const allowDirty = process.env.JEJU_CATALOG_ALLOW_DIRTY === "1";
const sourceBase = process.env.JEJU_CATALOG_SOURCE_BASE || "github:cosmtrek/jeju";
const baseIndexPath = process.env.JEJU_CATALOG_BASE_INDEX || "";
const commit = process.env.GITHUB_SHA || git(["rev-parse", "HEAD"]);
const baseEntries = readBaseEntries(baseIndexPath);

if (!localSources && !allowDirty) {
  const dirty = git(["status", "--porcelain", "--", "catalog"], { optional: true });
  if (dirty.trim() !== "") {
    throw new Error(
      "catalog has uncommitted changes; refusing to generate pinned registry sources. " +
        "Use JEJU_CATALOG_LOCAL_SOURCES=1 for local testing or JEJU_CATALOG_ALLOW_DIRTY=1 for an explicit draft build.",
    );
  }
}

const packageRoots = discoverPackageRoots(catalogRoot);
const packages = packageRoots.map((packageRoot) => readPackage(packageRoot));
packages.sort((a, b) => a.id.localeCompare(b.id));

const registry = buildRegistry(packages, baseEntries);

mkdirSync(path.dirname(generatedJSON), { recursive: true });
mkdirSync(path.dirname(registryIndex), { recursive: true });
writeFileSync(generatedJSON, `${JSON.stringify(packages, null, 2)}\n`);
writeFileSync(registryIndex, YAML.stringify(registry));

console.log(`generated ${path.relative(repoRoot, registryIndex)} with ${packages.length} packages`);
console.log(`generated ${path.relative(repoRoot, generatedJSON)}`);

function discoverPackageRoots(root) {
  const roots = [];
  for (const namespace of readdirSync(root, { withFileTypes: true })) {
    if (!namespace.isDirectory()) continue;
    const namespacePath = path.join(root, namespace.name);
    for (const pkg of readdirSync(namespacePath, { withFileTypes: true })) {
      if (!pkg.isDirectory()) continue;
      const packageRoot = path.join(namespacePath, pkg.name);
      statSync(path.join(packageRoot, "jeju.package.yaml"));
      roots.push(packageRoot);
    }
  }
  return roots;
}

function readPackage(packageRoot) {
  const relPath = toPosix(path.relative(repoRoot, packageRoot));
  const manifest = YAML.parse(readFileSync(path.join(packageRoot, "jeju.package.yaml"), "utf8"));
  const agentRel = manifest.agent?.manifest;
  const agentPath = path.join(packageRoot, agentRel);
  const agent = YAML.parse(readFileSync(agentPath, "utf8"));
  const digest = digestDir(packageRoot);
  const candidateSource = localSources
    ? packageRoot
    : `${sourceBase}//${relPath}?ref=${commit}`;
  const base = baseEntries.get(entryKey(manifest.metadata.id, manifest.metadata.version));
  if (base && base.digest && base.digest !== digest) {
    throw new Error(
      `published registry entry ${manifest.metadata.id}@${manifest.metadata.version} has digest ${base.digest}, ` +
        `but current catalog digest is ${digest}; bump metadata.version before changing package content`,
    );
  }
  const source = base?.source || candidateSource;
  return {
    id: manifest.metadata.id,
    name: manifest.metadata.name || manifest.metadata.id,
    version: manifest.metadata.version,
    description: manifest.metadata.description,
    path: relPath,
    source,
    digest,
    model: agent.models?.providers?.primary?.model || "",
    providerPreset: agent.models?.providers?.primary?.preset || "",
    access: agent.permissions?.access || "",
    approval: agent.permissions?.approval || "",
    tools: normalizeTools(agent.tools),
    output: agent.output?.name || "",
  };
}

function buildRegistry(packages, baseEntries) {
  const byPackage = new Map();
  for (const entry of baseEntries.values()) {
    addRegistryEntry(byPackage, entry.id, entry.version, entry.source, entry.digest);
  }
  for (const pkg of packages) {
    addRegistryEntry(byPackage, pkg.id, pkg.version, pkg.source, pkg.digest);
  }
  return {
    packages: Object.fromEntries(
      [...byPackage.entries()]
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([id, versions]) => [
          id,
          {
            versions: Object.fromEntries(
              [...versions.entries()]
                .sort(([a], [b]) => a.localeCompare(b))
                .map(([version, entry]) => [version, { source: entry.source, digest: entry.digest }]),
            ),
          },
        ]),
    ),
  };
}

function addRegistryEntry(byPackage, id, version, source, digest) {
  if (!byPackage.has(id)) byPackage.set(id, new Map());
  byPackage.get(id).set(version, { source, digest });
}

function readBaseEntries(file) {
  const entries = new Map();
  if (!file) return entries;
  if (!existsSync(file)) {
    console.warn(`warning: base registry index ${file} does not exist; generating registry without published baseline`);
    return entries;
  }
  const index = YAML.parse(readFileSync(file, "utf8")) || {};
  for (const entry of index.entries || []) {
    if (!entry.id || !entry.version) continue;
    entries.set(entryKey(entry.id, entry.version), {
      id: entry.id,
      version: String(entry.version),
      source: entry.source,
      digest: entry.digest || "",
    });
  }
  if (Array.isArray(index.packages)) {
    for (const entry of index.packages) {
      addBasePackageListEntry(entries, entry);
    }
    return entries;
  }
  if (!index.packages) return entries;
  if (typeof index.packages !== "object") {
    throw new Error(`base registry ${file} has invalid packages field`);
  }
  for (const [id, pkg] of Object.entries(index.packages)) {
    if (!pkg || typeof pkg !== "object") {
      throw new Error(`base registry package ${id} is not an object`);
    }
    for (const [version, item] of Object.entries(pkg.versions || {})) {
      if (!item || typeof item !== "object") {
        throw new Error(`base registry entry ${id}@${version} is not an object`);
      }
      entries.set(entryKey(id, version), {
        id,
        version,
        source: item.source,
        digest: item.digest || "",
      });
    }
  }
  return entries;
}

function addBasePackageListEntry(entries, entry) {
  if (!entry?.id) return;
  if (entry.version) {
    entries.set(entryKey(entry.id, entry.version), {
      id: entry.id,
      version: String(entry.version),
      source: entry.source,
      digest: entry.digest || "",
    });
    return;
  }
  for (const [version, item] of Object.entries(entry.versions || {})) {
    if (!item || typeof item !== "object") {
      throw new Error(`base registry entry ${entry.id}@${version} is not an object`);
    }
    entries.set(entryKey(entry.id, version), {
      id: entry.id,
      version,
      source: item.source,
      digest: item.digest || "",
    });
  }
}

function entryKey(id, version) {
  return `${id}@${version}`;
}

function normalizeTools(tools) {
  if (!Array.isArray(tools)) return [];
  return tools.map((tool) => (typeof tool === "string" ? tool : tool.name || tool.uses)).filter(Boolean);
}

function digestDir(root) {
  const hash = createHash("sha256");
  const files = collectFiles(root).sort();
  for (const rel of files) {
    const file = path.join(root, rel);
    const stat = statSync(file);
    const mode = (stat.mode & 0o777) || 0o644;
    hash.update(toPosix(rel));
    hash.update(Buffer.from([0]));
    hash.update(mode.toString(8).padStart(4, "0"));
    hash.update(Buffer.from([0]));
    hash.update(readFileSync(file));
    hash.update(Buffer.from([0]));
  }
  return `sha256:${hash.digest("hex")}`;
}

function collectFiles(root, dir = root) {
  const files = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    const lstat = lstatSync(full);
    if (lstat.isSymbolicLink()) {
      throw new Error(`symlink ${full} is not allowed in package content`);
    }
    if (entry.isDirectory()) {
      if (excludedDirs.has(entry.name)) continue;
      files.push(...collectFiles(root, full));
      continue;
    }
    if (!entry.isFile()) continue;
    files.push(path.relative(root, full));
  }
  return files;
}

function git(args, options = {}) {
  try {
    return execFileSync("git", args, {
      cwd: repoRoot,
      encoding: "utf8",
      stdio: ["ignore", "pipe", options.optional ? "ignore" : "pipe"],
    }).trim();
  } catch (error) {
    if (options.optional) return "";
    throw error;
  }
}

function toPosix(value) {
  return value.split(path.sep).join("/");
}
