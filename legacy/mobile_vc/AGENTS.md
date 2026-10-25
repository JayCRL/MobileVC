# MobileVC Agent Notes

When working in this project, use the `mobilevc-release` skill for any task involving:

- npm version bumps
- `npm run build:binaries`
- publishing `@justprove/mobilevc`
- publishing `@justprove/mobilevc-server-*`
- release verification
- syncing npm release artifacts back to GitHub

For release work, follow this release order:

1. `npm whoami`
2. `npm view @justprove/mobilevc version`
3. `npm version patch --no-git-tag-version`
4. `npm run build:binaries`
5. `npm pack --dry-run`
6. Publish all `packages/server-*`
7. Publish root package
8. Verify npm version
9. Commit release artifacts and push if requested

Do not stage unrelated local files such as `.claude`, `node_modules`, logs, or unrelated platform changes during release tasks.
