#!/usr/bin/env bash
set -euo pipefail

cd workspace/app
rm -rf repo
mkdir -p repo
cd repo
git init -q -b master
git config user.email benchmark@example.com
git config user.name "Benchmark"
printf '%s\n' '<h1>Original site</h1>' > index.html
git add index.html
git commit -q -m 'initial site'
git checkout -q -b lost-work
printf '%s\n' '<h1>Jeju benchmark recovered change</h1>' > index.html
git add index.html
git commit -q -m 'update site'
git checkout -q master
