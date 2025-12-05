# Changelog

All notable changes to FilaBridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.2.3.1] - 2025-12-05

### Changed

- update Dockerfile to use --no-scripts flag for apk to address Alpine 3.23 trigger script issues

## [v0.2.3] - 2025-12-05

### Changed

- update Dockerfile to include apk update before installing dependencies

## [v0.2.2] - 2025-12-05

### Added

- add URL copy functionality and properly encode NFC URLs.

### Fixed

- Update location management to reflect API limitations

### Changed

- migrate location management from FilaBridge to Spoolman, removing legacy location functions and updating related API endpoints

## [v0.2.1] - 2025-12-03

### Added

- accept hostnames or IP addresses for Spoolman and Printers.

## [v0.2] - 2025-12-03

### Added

- enhance settings UI with sub-tabs for better organization and add functionality for automatic spool assignment with location selection
- implement auto-assignment of previous spools with configuration options and API endpoints
- add toolhead name management with custom display names and API endpoints for retrieval and updates

### Fixed

- add HTML escaping for toolhead names to prevent XSS vulnerabilities
- handle null values for remaining weight in spool display across dropdowns and NFC tags
- identify and skip virtual printer toolhead locations in location management
- round remaining weight in spool tag details for improved display

### Changed

- improve event listener management for auto-assign previous spool checkbox

## [v0.1.5] - 2025-11-18

### Added

- embed static files into the binary and update routing to serve them

### Changed

- refactor CHANGELOG generation in release workflow to use printf for header and new entry creation

## [v0.1.3] - 2025-11-02

### Fixed

- implement error ID sanitization for URL safety in print error handling

## [v0.1.2] - 2025-10-21

### Fixed

- add copying of static files in Dockerfile to streamline asset deployment

## [v0.1.1] - 2025-10-21

### Added

- add static files directory to Dockerfile for improved asset management

### Changed

- update CHANGELOG and enhance README with additional screenshots

## [v0.1.0] - 2025-10-21

### Added

- implement NFC management features including QR code generation and location tracking

## [v0.0.15] - 2025-10-20

### Added

- add edit button for spools
- filter out spools with 0g remaining weight in GetAllSpools method

### Changed

- enhance changelog generation to categorize commits by type

## [v0.0.14] - 2025-10-15

### Added

- fix: properly encode error ID in fetch request for acknowledging print errors
- feat: add local time conversion for error timestamps in print processing notifications
- chore(release): update CHANGELOG for v0.0.13, removing outdated v0.0.11 entry
- fix: enhance print processing logic in FilamentBridge to prevent duplicate handling and improve state management
- chore(release): update changelog for v0.0.13


### Added

- bug: streamline print completion handling in monitorPrusaLink, removing files/jobs being processed duplicate times.
- fix: reduce Spoolman timeout from 30 seconds to 10 seconds for improved performance
- chore(release): update changelog for v0.0.12

## [v0.0.12] - 2025-10-14

### Added

- bug: fix not being able to dismiss error messages
- docs: Update README to use direct link for dashboard screenshot, improving accessibility
- chore(release): enhance CHANGELOG generation by categorizing commits and improving file handling
- chore(release): update changelog for v0.0.11

### Added

- feat: Add advanced timeout settings for PrusaLink and Spoolman API, enhancing configuration flexibility in the UI
- chore(release): update changelog for v0.0.10
