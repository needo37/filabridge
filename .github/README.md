# GitHub Actions Workflows

This directory contains automated workflows for the FilaBridge project.

## release.yml

Automatically builds cross-platform binaries and creates a GitHub release when you push a version tag.

### How to Trigger a Release

1. **Ensure all changes are committed** to the main branch

2. **Create a version tag**:
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   ```

3. **Push the tag to GitHub**:
   ```bash
   git push origin v1.0.0
   ```

4. **Monitor the build**:
   - Go to the "Actions" tab in your GitHub repository
   - Watch the "Release" workflow run
   - It will take a few minutes to build all platforms

5. **Check the release**:
   - Go to the "Releases" section
   - Your new release will be created automatically
   - All binaries will be attached

### Platforms Built

The workflow builds FilaBridge for:
- **Linux AMD64** - Most Linux servers and desktops
- **Linux ARM64** - Raspberry Pi, ARM servers
- **Windows AMD64** - Windows 10+ (64-bit)
- **macOS AMD64** - Intel Macs
- **macOS ARM64** - Apple Silicon Macs (M1, M2, M3, etc.)

### Build Details

- **CGO Enabled**: Required for SQLite support
- **Cross-compilation**: Uses platform-specific compilers
- **Checksums**: SHA256 checksums generated for verification
- **Release Notes**: Auto-generated from workflow template

### Troubleshooting

**If the build fails:**

1. Check the Actions log for errors
2. Common issues:
   - CGO compilation errors (check compiler installation)
   - Go version mismatch
   - Missing dependencies

**To rebuild:**

1. Delete the failed release (if created)
2. Delete the tag: `git tag -d v1.0.0 && git push origin :refs/tags/v1.0.0`
3. Fix the issue and create the tag again

### Manual Release (if needed)

If you need to create a release manually:

```bash
# Build for each platform
GOOS=linux GOARCH=amd64 go build -o filabridge-linux-amd64 .
GOOS=linux GOARCH=arm64 go build -o filabridge-linux-arm64 .
GOOS=windows GOARCH=amd64 go build -o filabridge-windows-amd64.exe .
GOOS=darwin GOARCH=amd64 go build -o filabridge-darwin-amd64 .
GOOS=darwin GOARCH=arm64 go build -o filabridge-darwin-arm64 .

# Create checksums
sha256sum filabridge-* > checksums.txt
```

Then create a release manually through the GitHub web interface.

### Future Workflows

Consider adding:
- **CI tests** - Run tests on every push/PR
- **Docker image builds** - Build and push Docker images
- **Code quality checks** - Linting, formatting validation
- **Security scans** - Dependency vulnerability scanning

