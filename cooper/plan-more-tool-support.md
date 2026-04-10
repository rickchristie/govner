# Plan: More Tool Support

## LSP Support

This section documents the built-in LSP coverage in OpenCode so Cooper can reason about which language-server dependencies are worth bundling or validating.

Verified against upstream OpenCode source:

- `packages/opencode/src/lsp/server.ts`
- `packages/opencode/src/lsp/index.ts`

Important runtime behavior:

- OpenCode does not eagerly start every built-in LSP.
- A server is only considered when a file is read or touched and that file matches the server's extension list.
- If root detection succeeds, OpenCode spawns that server for that root and reuses it.
- The sidebar shows active connected server IDs, not the full built-in support matrix.

| Server ID | Launch command | File extensions | Notes |
| --- | --- | --- | --- |
| `deno` | `deno lsp` | `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs` | Only activates when `deno.json` or `deno.jsonc` exists. Requires `deno` on `PATH`. |
| `typescript` | `typescript-language-server --stdio` | `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.mts`, `.cts` | Skipped when Deno markers exist. Requires project-resolvable `typescript/lib/tsserver.js` plus a `typescript-language-server` binary resolved from the environment or npm. |
| `vue` | `vue-language-server --stdio` | `.vue` | Uses `PATH` first, then npm-resolved `@vue/language-server` unless LSP downloads are disabled. |
| `eslint` | `node <Global.Path.bin>/vscode-eslint/server/out/eslintServer.js --stdio` | `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.mts`, `.cts`, `.vue` | Requires an `eslint` module in the project. OpenCode can download and build the VS Code ESLint server if needed. |
| `oxlint` | `oxlint --lsp` or `oxc_language_server` | `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.mts`, `.cts`, `.vue`, `.astro`, `.svelte` | Prefers local or global `oxlint` when it supports `--lsp`, otherwise falls back to `oxc_language_server`. |
| `biome` | `biome lsp-proxy --stdio` | `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.mts`, `.cts`, `.json`, `.jsonc`, `.vue`, `.astro`, `.svelte`, `.css`, `.graphql`, `.gql`, `.html` | Prefers local `node_modules/.bin/biome`, then `PATH`, then npm resolution. |
| `gopls` | `gopls` | `.go` | Prefers `PATH`. If missing and `go` exists, OpenCode can install `golang.org/x/tools/gopls@latest`. |
| `ruby-lsp` | `rubocop --lsp` | `.rb`, `.rake`, `.gemspec`, `.ru` | The server ID is `ruby-lsp`, but the executable is `rubocop`. OpenCode can install it with `gem install`. |
| `ty` | `ty server` | `.py`, `.pyi` | Experimental. Enabled only with `OPENCODE_EXPERIMENTAL_LSP_TY`; when enabled, `pyright` is removed. Also passes discovered `pythonPath` initialization when available. |
| `pyright` | `pyright-langserver --stdio` | `.py`, `.pyi` | Disabled when experimental `ty` is enabled. Prefers `PATH`, then npm resolution. Also passes discovered `pythonPath` initialization when available. |
| `elixir-ls` | `elixir-ls` or generated `language_server.sh` / `language_server.bat` | `.ex`, `.exs` | Prefers `PATH`; otherwise OpenCode can download ElixirLS source and build its release. |
| `zls` | `zls` | `.zig`, `.zon` | Prefers `PATH`; otherwise OpenCode can download a matching `zls` release. Requires Zig to be installed. |
| `csharp` | `csharp-ls` | `.cs` | Prefers `PATH`; otherwise OpenCode can install it with `dotnet tool install csharp-ls`. |
| `fsharp` | `fsautocomplete` | `.fs`, `.fsi`, `.fsx`, `.fsscript` | Prefers `PATH`; otherwise OpenCode can install it with `dotnet tool install fsautocomplete`. |
| `sourcekit-lsp` | `sourcekit-lsp` or `xcrun --find sourcekit-lsp` | `.swift`, `.objc`, `objcpp` | Uses `PATH` first. On Apple toolchains it can fall back to locating `sourcekit-lsp` via `xcrun`. |
| `rust` | `rust-analyzer` | `.rs` | Requires `rust-analyzer` on `PATH`. Root detection promotes Cargo workspace roots. |
| `clangd` | `clangd --background-index --clang-tidy` | `.c`, `.cpp`, `.cc`, `.cxx`, `.c++`, `.h`, `.hpp`, `.hh`, `.hxx`, `.h++` | Prefers `PATH`, then OpenCode-managed binaries. OpenCode can download a matching `clangd` release if needed. |
| `svelte` | `svelteserver --stdio` | `.svelte` | Uses `PATH` first, then npm-resolved `svelte-language-server`. |
| `astro` | `astro-ls --stdio` | `.astro` | Requires a resolvable local TypeScript server. Uses `PATH` first, then npm-resolved `@astrojs/language-server`, and passes `typescript.tsdk` initialization. |
| `jdtls` | `java -jar <org.eclipse.equinox.launcher_*.jar> ...` | `.java` | Requires Java 21+. OpenCode can download and unpack JDTLS, then launches it with platform-specific config and a temp data dir. |
| `kotlin-ls` | `<Global.Path.bin>/kotlin-ls/kotlin-lsp.(sh|cmd) --stdio` | `.kt`, `.kts` | Uses an OpenCode-managed Kotlin LSP package under `Global.Path.bin`. |
| `yaml-ls` | `yaml-language-server --stdio` | `.yaml`, `.yml` | Uses `PATH` first, then npm-resolved `yaml-language-server`. |
| `lua-ls` | `lua-language-server` | `.lua` | Uses `PATH` first; otherwise OpenCode can download and extract LuaLS. |
| `php intelephense` | `intelephense --stdio` | `.php` | Uses `PATH` first, then npm-resolved `intelephense`. Telemetry is explicitly disabled in initialization. |
| `prisma` | `prisma language-server` | `.prisma` | Requires `prisma` on `PATH`. Root detection looks for Prisma schema locations. |
| `dart` | `dart language-server --lsp` | `.dart` | Requires `dart` on `PATH`. |
| `ocaml-lsp` | `ocamllsp` | `.ml`, `.mli` | Requires `ocamllsp` on `PATH`. |
| `bash` | `bash-language-server start` | `.sh`, `.bash`, `.zsh`, `.ksh` | Uses `PATH` first, then npm-resolved `bash-language-server`. Root is always the current OpenCode instance directory. |
| `terraform` | `terraform-ls serve` | `.tf`, `.tfvars` | Prefers `PATH`; otherwise OpenCode can download a matching `terraform-ls` release. Also enables Terraform-specific initialization options. |
| `texlab` | `texlab` | `.tex`, `.bib` | Prefers `PATH`; otherwise OpenCode can download a matching `texlab` release. |
| `dockerfile` | `docker-langserver --stdio` | `.dockerfile`, `Dockerfile` | Uses `PATH` first, then npm-resolved `dockerfile-language-server-nodejs`. Root is always the current OpenCode instance directory. |
| `gleam` | `gleam lsp` | `.gleam` | Requires `gleam` on `PATH`. |
| `clojure-lsp` | `clojure-lsp listen` | `.clj`, `.cljs`, `.cljc`, `.edn` | Requires `clojure-lsp` on `PATH`. |
| `nixd` | `nixd` | `.nix` | Requires `nixd` on `PATH`. Root prefers `flake.nix`, then git worktree root, then instance directory. |
| `tinymist` | `tinymist` | `.typ`, `.typc` | Prefers `PATH`; otherwise OpenCode can download a matching `tinymist` release. |
| `haskell-language-server` | `haskell-language-server-wrapper --lsp` | `.hs`, `.lhs` | Requires `haskell-language-server-wrapper` on `PATH`. |
| `julials` | `julia --startup-file=no --history-file=no -e "using LanguageServer; runserver()"` | `.jl` | Requires `julia` on `PATH` and a usable `LanguageServer` package in that Julia environment. |
