# Changelog

All notable changes to murmelmoney are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/). Add entries under **Unreleased** as you go and move
them into a version section when tagging a release.

## [Unreleased]

### Changed
- CI: bumped GitHub Actions (checkout v5, setup-node v5, setup-go v6, docker/* actions,
  action-gh-release v3) to their Node 24 releases, clearing the Node 20 deprecation warnings.

## [1.0.1] - 2026-08-17

### Fixed
- Docker: build the UI and Go binary on the host platform and cross-compile for the target
  architecture. Running `npm ci` under QEMU for arm64 crashed with SIGILL and hung the
  multi-arch image build.

## [1.0.0] - 2026-08-17

First release.

### Added
- Incomes & expenses split into *business* / *rental* / *private*, with one category and any
  number of tags per transaction (created on the fly).
- File attachments (receipts, invoices) and notes per transaction; attachments are only fetchable
  by their owner.
- Read-only transaction detail view (notes, attachment previews) separate from the edit form.
- Yearly overview by area, category and tag.
- Rough tax estimate driven by a user-editable JavaScript rule (ships with an Austrian
  freelancer + landlord example).
- Multi-user with per-user data; registration auto-closes after the first user
  (`MURMEL_REGISTRATION` overrides).
- Single binary + single SQLite file (Go + PocketBase + Svelte); default listen address
  `127.0.0.1:8070`.
- Mobile layout with bottom tab bar, installable as a PWA.
- Light/dark theme derived from the logo colours.
- CI workflow (svelte-check, go vet, build) and release workflow publishing binaries for
  Linux/macOS/Windows plus a multi-arch Docker image on ghcr.io.

[Unreleased]: https://github.com/floholz/murmelmoney/compare/v1.0.1...HEAD
[1.0.1]: https://github.com/floholz/murmelmoney/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/floholz/murmelmoney/releases/tag/v1.0.0
