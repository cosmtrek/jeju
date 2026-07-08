import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import YAML from "yaml";

const websiteRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(websiteRoot, "..");
const catalogRoot = path.join(repoRoot, "catalog");

for (const packageRoot of discoverPackageRoots(catalogRoot)) {
  const manifest = YAML.parse(readFileSync(path.join(packageRoot, "jeju.package.yaml"), "utf8"));
  console.log(`jeju:${manifest.metadata.id}@${manifest.metadata.version}`);
}

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
  return roots.sort();
}
