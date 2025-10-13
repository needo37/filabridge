# Changelog

All notable changes to FilaBridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.0.8] - 2025-10-10

### Added
- Enhanced spool display by including IDs in the UI and improve search functionality for numeric IDs
- Print error handling with acknowledgment feature and enhance UI for displaying print errors

### Fixed
- Standardize error messages in print processing for consistency and clarity

### Changed
- Refactor toolhead mapping logic to handle unmapping and improve dropdown functionality for spool selection

[Full Changelog](https://github.com/needo37/filabridge/compare/v0.0.7...v0.0.8)

## [v0.0.8] - 2025-10-10

[Full Changelog](https://github.com/needo37/filabridge/compare/v0.0.7...v0.0.8)

## [v0.0.7] - 2025-10-10

### Added
- Update default Spoolman URL and enhance logging for printer info retrieval and model detection

[Full Changelog](https://github.com/needo37/filabridge/compare/v0.0.6...v0.0.7)

## [v0.0.6] - 2025-10-10

### Added
- Implement mutex locking for printer configuration operations and enhance config loading process
- Add search functionality to dropdown in index.html with styling and no-results handling
- Integrate WebSocket support for real-time status updates in the web interface, including client connection management and dashboard updates
- Add spool assignment conflict checking and available spools retrieval in the web interface, enhancing toolhead mapping functionality
- Enhance toolhead mapping functionality by adding auto-mapping for selected spools

### Changed
- Introduce constants for configuration keys and default values, streamline printer state handling, and enhance filament usage processing
- Optimize Dockerfile and GitHub Actions for improved build caching and layer management

### Documentation
- Update README.md to clarify database path configuration for Docker deployments

[Full Changelog](https://github.com/needo37/filabridge/compare/v0.0.5...v0.0.6)

## [v0.0.5] - 2025-10-10

### Added
- Add Docker support with Dockerfile, docker-compose, and CI workflow

### Changed
- Remove README.md and update .github/README.md with Docker installation instructions
- Update docker-compose to use pre-built image for filabridge
- Update Go base image in Dockerfile from 1.21-alpine to 1.23-alpine

### Fixed
- Enhance database configuration to support environment variable for database path

[Full Changelog](https://github.com/needo37/filabridge/compare/v0.0.4...v0.0.5)

## [v0.0.4] - 2025-10-10

### Added
- Add configuration for conventional commits and changelog generation

### Changed
- Refactor IsFirstRun logic to check printer_configs instead of configuration table
- Remove CHANGELOG.md and update changelog generation process in release workflow
- Improve changelog generation and update release workflow for better handling of detached HEAD state

[Full Changelog](https://github.com/needo37/filabridge/compare/v0.0.3...v0.0.4)

## [v0.0.3] - 2025-10-09

### Changed
- Refactor template loading to use embedded filesystem for HTML templates
- Remove duplicate connection error display from index.html template

[Full Changelog](https://github.com/needo37/filabridge/compare/v0.0.2...v0.0.3)

## [v0.0.2] - 2025-10-09

[Full Changelog](https://github.com/needo37/filabridge/compare/v0.0.1...v0.0.2)

## [v0.0.1] - 2025-10-09

[Full Changelog](https://github.com/needo37/filabridge/compare/Release-v0.0.1...v0.0.1)

## [Release-v0.0.1] - 2025-10-09

[Full Changelog](https://github.com/needo37/filabridge/compare/Release-v0.0.1...Release-v0.0.1)