# Contributing to FilaBridge

Thank you for considering contributing to FilaBridge! This document provides guidelines and information for contributors.

## How to Contribute

### Reporting Bugs

Before creating a bug report:
1. Check the existing issues to avoid duplicates
2. Gather relevant information (OS, Go version, printer model, error messages)
3. Create a detailed issue with steps to reproduce

Include in your bug report:
- **Description**: Clear description of the bug
- **Steps to reproduce**: Numbered list of steps
- **Expected behavior**: What should happen
- **Actual behavior**: What actually happens
- **Environment**: OS, Go version, printer model
- **Logs**: Relevant log output (sanitize any API keys!)

### Suggesting Features

Feature requests are welcome! Please:
1. Check if the feature has already been requested
2. Explain the use case and benefit
3. Provide examples of how it would work
4. Consider whether it fits the project scope

### Submitting Pull Requests

1. **Fork the repository** and create a branch from `main`
2. **Make your changes** with clear, descriptive commits
3. **Test thoroughly** - ensure existing functionality still works
4. **Update documentation** if needed (README, code comments)
5. **Submit a PR** with a clear description of changes

#### PR Guidelines

- **One feature per PR**: Keep changes focused
- **Follow Go conventions**: Run `go fmt` and `go vet`
- **Write clear commits**: Use descriptive commit messages
- **Update tests**: Add tests for new features
- **Document changes**: Update README if user-facing

## Development Setup

### Prerequisites

- Go 1.23 or higher
- Docker (for testing with Spoolman)
- A PrusaLink-compatible printer (or mock for testing)

### Local Development

1. **Clone your fork**:
   ```bash
   git clone https://github.com/needo37/filabridge.git
   cd filabridge
   ```

2. **Install dependencies**:
   ```bash
   go mod download
   ```

3. **Run Spoolman** (for testing):
   ```bash
   docker run -d --name spoolman -p 8000:8000 ghcr.io/donkie/spoolman:latest
   ```

4. **Build and run**:
   ```bash
   go build -o filabridge .
   ./filabridge
   ```

5. **Run tests**:
   ```bash
   go test ./...
   ```

### Code Style

- Follow standard Go conventions (run `go fmt`)
- Use meaningful variable and function names
- Add comments for complex logic
- Keep functions focused and reasonably sized
- Handle errors appropriately

### Project Structure

- `main.go` - Application entry point and CLI flags
- `config.go` - Configuration management and database schema
- `bridge.go` - Core monitoring and tracking logic
- `prusalink.go` - PrusaLink API client
- `spoolman.go` - Spoolman API client
- `web.go` - HTTP server and web interface

## Testing

### Manual Testing

1. Test with real printers when possible
2. Verify multi-toolhead support
3. Check G-code parsing accuracy
4. Test error handling (network failures, etc.)

### Automated Testing

- Write unit tests for new functions
- Test edge cases and error conditions
- Ensure tests are repeatable

## Areas for Contribution

Looking for ideas? Here are some areas that need help:

### High Priority
- Support for additional printer APIs (OctoPrint, Klipper/Moonraker)
- Improved error handling and logging
- Unit tests and integration tests
- Documentation improvements

### Medium Priority
- Mobile-responsive UI improvements
- Print statistics and analytics
- Email/webhook notifications
- Configuration import/export

### Low Priority
- Additional database backends
- Internationalization (i18n)
- Dark mode for web UI
- REST API documentation

## Communication

- **Issues**: For bugs and feature requests
- **Discussions**: For questions and general discussion
- **Pull Requests**: For code contributions

## Code of Conduct

### Our Standards

- Be respectful and inclusive
- Welcome newcomers and help them learn
- Focus on constructive feedback
- Assume good intentions

### Unacceptable Behavior

- Harassment or discrimination
- Trolling or inflammatory comments
- Publishing others' private information
- Any unprofessional conduct

## License

By contributing to FilaBridge, you agree that your contributions will be licensed under the GNU General Public License v3.0.

## Questions?

If you have questions about contributing:
1. Check existing issues and discussions
2. Open a new discussion
3. Reach out to the maintainers

Thank you for contributing to FilaBridge! 🎉

