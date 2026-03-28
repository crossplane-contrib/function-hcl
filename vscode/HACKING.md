## Building and running locally

```sh
npm run setup
```

This installs dependencies, downloads the language server binary for your platform into `bin/`,
and builds the extension.
To produce a `.vsix` package for local installation:

```sh
npm run package
```

The `bin/function-hcl-ls` binary is cached — the download script is a no-op if it already exists.
To force a re-download, delete the `bin/` directory first.

## Local testing

To test the extension in a development instance of VS Code:

1. Open this directory in VS Code:
   ```sh
   code .
   ```

2. Ensure the language server binary is present:
   ```sh
   node build/downloadServer.mjs
   ```

3. Press `F5` or go to **Run > Start Debugging**.

This launches a second VS Code window (the Extension Development Host) with the
extension loaded from source. The setup that makes this work is in `.vscode/`:

- **`launch.json`** — defines a launch configuration of type `extensionHost` that
  starts VS Code with `--extensionDevelopmentPath` pointing to this directory.
  It sets a pre-launch task that runs `npm run watch`.
- **`tasks.json`** — defines the `watch` task (`tsc -b -w`) as a background build
  that recompiles TypeScript on every save, so changes are picked up automatically
  in the debug session.

To pick up changes, save the file and reload the Extension Development Host
window (`Ctrl+Shift+P` / `Cmd+Shift+P` → "Developer: Reload Window").

## Publishing to the VS Code Marketplace

The extension is published automatically by the release workflow (`.github/workflows/release.yaml`)
when a `v*` tag is pushed. The workflow builds platform-specific `.vsix` packages for
`darwin-arm64`, `darwin-x64`, `linux-arm64`, and `linux-x64`, each bundling the
corresponding language server binary. These are published to the Marketplace and
attached to the GitHub release.

### Dependencies for publishing

- **`VSCE_PAT` secret** — a Personal Access Token from Azure DevOps with the
  Marketplace > Manage scope, stored as a GitHub Actions secret in the repository.
- **`publisher` in `package.json`** — currently set to `function-hcl-authors`.
  This must match the publisher ID registered at https://marketplace.visualstudio.com/manage.
- To create a PAT you need to go to this URL (which cannot be found from the Azure portal, as far as I could tell),
  https://dev.azure.com/, go to User Settings and create one.

## npm scripts

| Script                    | Command                                                                                                                                               |
|---------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------|
| `npm run compile`         | Bundles the extension into `dist/extension.js` using esbuild                                                                                          |
| `npm run watch`           | Runs esbuild in watch mode, rebuilding `dist/extension.js` on file changes. Used by the debug launch configuration                                    |
| `npm run package`         | Produces a `.vsix` package for local installation                                                                                                     |
| `npm run download:server` | Downloads the language server binary for your platform into `bin/`. Accepts `--target` and `--local-tarball` flags                                    |
| `npm run clean`           | Removes all generated directories: `bin/`, `dist/`, `out/`, and `node_modules/`                                                                       |
| `npm run setup`           | First-time setup: installs dependencies, downloads the language server binary, and builds the extension                                                |
| `vscode:prepublish`       | Runs automatically during `vsce package` — downloads the language server and builds the extension in production mode. Not intended to be run directly |

## File reference

| File                          | Purpose                                                                                                                                              |
|-------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------|
| `src/extension.ts`            | Extension entry point — activates the language client, resolves the server binary path                                                               |
| `src/languageServer.ts`       | Returns the path to the bundled language server binary                                                                                               |
| `src/test/extension.test.ts`  | Extension test suite                                                                                                                                 |
| `build/downloadServer.mjs`    | Build-time script that downloads the language server binary from GitHub releases into `bin/`. Supports `--target` and `--local-tarball` flags for CI |
| `package.json`                | Extension manifest — metadata, dependencies, scripts, VS Code contribution points                                                                    |
| `esbuild.js`                  | Bundles the TypeScript source into a single `dist/extension.js` using esbuild                                                                        |
| `tsconfig.json`               | TypeScript compiler configuration                                                                                                                    |
| `eslint.config.mjs`           | ESLint configuration                                                                                                                                 |
| `language-configuration.json` | HCL language behavior in the editor — comments, brackets, folding regions                                                                            |
| `syntaxes/hcl.tmGrammar.json` | TextMate grammar for HCL syntax highlighting                                                                                                         |
| `.vscodeignore`               | Files excluded from the `.vsix` package                                                                                                              |
| `.vscode/launch.json`         | Debug launch configuration for the Extension Development Host                                                                                        |
| `.vscode/tasks.json`          | Build tasks (compile and watch)                                                                                                                      |
| `.vscode/settings.json`       | Workspace settings for this directory                                                                                                                |
| `.vscode/extensions.json`     | Recommended extensions for contributors                                                                                                              |
| `.vscode-test.mjs`            | Test runner configuration for `@vscode/test-cli`                                                                                                     |
