# Changelog

All notable changes to this project will be documented in this file.

## v0.6.1 - 2026-03-18

### Added

- Added a shared SQLite storage backend for runtime config, plugin registry/private storage, and channel memory.
- Added sqlite-vec powered retrieval persistence so embeddings survive restarts instead of living only in memory.
- Added tests covering SQLite-backed memory persistence and registry migration.
- Added `Dockerfile` and `docker-compose.yml` for containerized deployment with persistent `/data` storage.

### Changed

- Removed the external plugin market index integration from the bot host and the `/plugin` panel.
- Updated the `/plugin` panel so admins can open the install modal directly, while upgrade/enable/disable/remove remain super-admin operations.
- Cleaned `.env.example` and both README files to remove the retired plugin market configuration.
- Added dedicated Chinese and English plugin authoring guides, and linked them from the README navigation.
- Reworked `/setup` into an embed panel with context-aware toggle buttons for the current server, channel, or thread.
- Runtime state now persists in `BOT_SQLITE_PATH` (default `bot.db`), while `BOT_CONFIG_FILE` remains as the legacy/bootstrap admin config source.
- Startup now auto-migrates legacy `bot_config.json` and `plugins/registry.json` data into SQLite when needed.

## v0.6.0 - 2026-03-18

### Added

- Added `PLUGIN_MARKET_INDEX_URL` so the host can read a static plugin market index from GitHub Pages or any other JSON endpoint.
- Added plugin market preview and external link buttons to the `/plugin` management panel.
- Added tests covering market link rendering in the plugin panel.

### Changed

- Bumped the host version to `v0.6.0`.
- Updated the README files and `.env.example` to document the official plugin market and its index URL.

### Fixed

- Fixed plugin market caching so an empty but valid market index is still cached instead of being re-fetched on every panel refresh.

## v0.5.0 - 2026-03-18

### Added

- Added the official Kizuna plugin monorepo `discord-bot-plugins`, containing the first-party `persona`, `proactive`, and `emoji` plugins.
- Added new host/plugin capabilities for core-powered replies, guild emoji listing, worldbook read/write, speech allowlist checks, richer interaction components, and admin role visibility in plugin context.

### Changed

- Migrated `/persona`, `/proactive`, and `/emoji` from built-in bot features into installable official plugins.
- Updated the main README files to document the new plugin-based installation flow and link to the official plugin repository.
- Bumped the host version to `v0.5.0`.

### Fixed

- Fixed a migration conflict where the core bot could still inject a legacy persona prompt after the official persona plugin was installed.
- Fixed a migration conflict where the core bot could still run legacy proactive-reply logic after the official proactive plugin was installed.

## v0.4.0 - 2026-03-18

### Added

- Added an external plugin host based on `JSON-RPC 2.0 over stdio`, with a shared `pkg/pluginapi` protocol package and Go SDK helpers for plugin authors.
- Added plugin registry persistence under `plugins/registry.json`, per-plugin private storage, capability manifests, dynamic slash command registration, and runtime plugin process management.
- Added `/plugin list|install|upgrade|remove|enable|disable|allow_here|deny_here|permissions` for plugin lifecycle management.
- Added prompt-build and response-postprocess plugin hooks, plus message event dispatch for installed plugins.
- Added an official example plugin at `examples/plugins/style-note` that demonstrates Git-installable prompt injection backed by plugin private storage.

### Changed

- Version bumped to `v0.4.0`.
- Discord command registration now merges core commands with enabled plugin commands and refreshes after plugin install, upgrade, remove, or global enable/disable.

### Fixed

- Fixed plugin command and component prefix conflict detection so installed plugins cannot shadow core management routes.

## v0.3.0 - 2026-03-18

### Added

- Added `/setup show|server|channel|thread|clear` to manage allowed speaking scopes directly from the current guild, channel, or thread, with persistent storage in `bot_config.json`.
- Added a dedicated `/proactive` management panel for enabling proactive replies and editing reply probability.
- Added tests covering the new setup flow, proactive reply behavior, allowlist persistence, and prompt rendering.

### Changed

- Default speaking behavior is now deny-by-default. The bot only speaks in locations explicitly allowed through `/setup`.
- Removed the old `/speech` panel flow and replaced it with the new `/setup` slash command flow.
- Startup logs now include the tracked application version `v0.3.0`.
- Updated both Chinese and English README files to document the current setup and release note entry point.

### Fixed

- Fixed proactive reply permission checks so they still obey the current allowed speaking scope.
- Fixed reply sending fallback logic when Discord rejects message references, retrying without the reference and logging the failure.
- Fixed context formatting leakage where assistant history could be sent back to the model with `时间(UTC+8)` / `发送者` / `内容` metadata, which sometimes caused the model to imitate that header format in replies.
