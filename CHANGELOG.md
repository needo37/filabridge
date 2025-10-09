# Changelog

All notable changes to FilaBridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2025-10-09

### Added
- Initial release of FilaBridge
- PrusaLink API integration for printer monitoring
- Spoolman API integration for inventory management
- Web-based dashboard with real-time printer status
- Multi-toolhead support (tested with 5-toolhead Prusa XL)
- G-code file parsing for accurate filament usage tracking
- SQLite database for persistent storage
- Toolhead to spool mapping functionality
- Print history tracking
- Web-based configuration interface
- Docker support with included Dockerfile
- Docker Compose setup including Spoolman
- Cross-platform binary builds (Linux, macOS, Windows)
- Comprehensive documentation and README
- Contributing guidelines
- GPL-3.0 license

### Technical Details
- Built with Go 1.23
- Uses Gin web framework
- SQLite3 for data persistence
- Support for both local network (PrusaLink) and remote (PrusaConnect) access
- REST API for programmatic access
- Automatic print completion detection
- Per-toolhead filament tracking

[1.0.0]: https://github.com/needo37/filabridge/releases/tag/v1.0.0

