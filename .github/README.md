# FilaBridge

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.23-00ADD8?logo=go)](https://golang.org/)
[![GitHub release](https://img.shields.io/github/v/release/needo37/filabridge)](https://github.com/needo37/filabridge/releases)

A high-performance Go microservice that bridges PrusaLink-compatible printers and Spoolman for (mostly) automatic filament inventory management. Originally designed for Prusa printers (CORE One, XL, MK4, etc.) but works with any printer that supports the PrusaLink API.

### The Problem

I run multiple 3D printers and use Spoolman to track my filament inventory. The issue? I had to manually update filament usage after every print. With multi-material prints on my Prusa XL, this was getting tedious and error-prone.

## Features

- 🔗 **PrusaLink Compatibility**: Works with any PrusaLink-compatible printer (Prusa CORE One, XL, MK4, Mini, and more)
- 📊 **Real-time Dashboard**: Web interface with live updates via WebSocket connections
- 🎯 **Multi-Toolhead Support**: Seamlessly handles single and multi-toolhead printers (tested with 5-toolhead Prusa XL)
- 📈 **Smart Usage Tracking**: Automatically parses G-code files to accurately track filament consumption per toolhead
- 💾 **Persistent Storage**: SQLite database stores toolhead mappings and complete print history
- ⚡ **High Performance**: Single lightweight binary, minimal resource usage, fast execution
- 🔧 **Web-based Config**: No config files needed - manage everything through the web UI
- 🔍 **Smart Spool Search**: Search and filter spools by ID, material, brand, or name with real-time filtering
- ⚠️ **Error Handling**: Print error detection with acknowledgment system for failed filament tracking
- 🔄 **Auto-mapping**: Automatic spool assignment when selecting from dropdown menus
- 🌐 **Live Updates**: Real-time status updates without page refreshes using WebSocket technology

## Why FilaBridge?

Managing filament inventory across multiple 3D printers is tedious. FilaBridge automates this by:
- Monitoring your printers in real-time with live WebSocket updates
- Tracking which spools are loaded on which toolheads
- Automatically updating your Spoolman inventory when prints complete
- Providing accurate filament usage by parsing G-code files
- Handling errors gracefully with clear notifications and acknowledgment system

No more manual updates or guesswork about remaining filament!

## Screenshot

![FilaBridge Dashboard](https://github.com/needo37/filabridge/blob/main/screenshots/dashboard.png?raw=true)
*FilaBridge web interface showing printer status and filament mappings*

## Prerequisites

- A PrusaLink-compatible 3D printer (Prusa or any printer with PrusaLink API)
- PrusaLink enabled on your printer(s) for local network access
- Spoolman
- **For building from source**: Go 1.23 or higher

## Installation

### Option 1: Docker (Easiest)

1. **Run Spoolman** (if not already running):
   ```bash
   docker run -d --name spoolman -p 8000:8000 -v spoolman-data:/home/spoolman/data ghcr.io/donkie/spoolman:latest
   ```

2. **Run FilaBridge**:
   ```bash
   docker run -d --name filabridge -p 5000:5000 \
     -v .:/app/data \
     ghcr.io/needo37/filabridge:latest
   ```

3. **Configure**: Open `http://localhost:5000` and click "⚙️ Configuration"

**Using docker-compose (recommended for full stack):**
```bash
git clone https://github.com/needo37/filabridge.git
cd filabridge
docker-compose up -d
```

The docker-compose.yml automatically sets the `FILABRIDGE_DB_PATH` environment variable to `/app/data` to ensure the database persists in the mounted volume.

### Option 2: Pre-built Binary

1. **Download the latest release** for your platform from the [Releases page](https://github.com/needo37/filabridge/releases)
   - Linux (amd64, arm64)
   - macOS (amd64/Intel, arm64/Apple Silicon)
   - Windows (amd64)

2. **Make it executable** (Linux/macOS):
   ```bash
   chmod +x filabridge
   ```

3. **Run Spoolman** (if not already running):
   ```bash
   docker run -d --name spoolman -p 8000:8000 -v spoolman-data:/home/spoolman/data ghcr.io/donkie/spoolman:latest
   ```

4. **Start FilaBridge**:
   ```bash
   ./filabridge
   ```

5. **Configure**: Open `http://localhost:5000` and click "⚙️ Configuration"

### Option 3: Build from Source

1. **Clone and build**:
   ```bash
   git clone https://github.com/needo37/filabridge.git
   cd filabridge
   go mod download
   go build -o filabridge .
   ```

2. **Run Spoolman** (if not already running):
   ```bash
   docker run -d --name spoolman -p 8000:8000 -v spoolman-data:/home/spoolman/data ghcr.io/donkie/spoolman:latest
   ```

3. **Start FilaBridge**:
   ```bash
   ./filabridge
   ```

## Configuration

The system stores all configuration in the SQLite database. For Docker deployments, you can optionally set the `FILABRIDGE_DB_PATH` environment variable to specify where the database should be stored (defaults to `/app/data` in Docker).

### First Run

1. Start the application
2. Open the web interface at `http://localhost:5000`
3. Click "Start Configuration" button
4. Enter a name for your Printer.
5. Enter your PrusaLink IP Address and API key
6. Choose the number of toolheads your printer has.
7. Click "Save Configuration"
8. The service will automatically restart with new settings

## Usage

### Running the Service

```bash
# Run both bridge service and web interface (recommended)
./filabridge

# Custom host and port
./filabridge --host 0.0.0.0 --port 8080
```

### Web Interface

The web interface provides:

- **Printer Status**: Real-time view of printer states and current jobs with live WebSocket updates
- **Toolhead Mapping**: Assign filament spools to specific toolheads with smart search functionality
- **Progress Monitoring**: Visual progress bars for active prints
- **Live Updates**: Real-time status updates without page refreshes
- **Spool Search**: Search and filter spools by ID, material, brand, or name
- **Error Management**: View and acknowledge print processing errors
- **Auto-mapping**: Automatic spool assignment when selecting from dropdowns

### Filament Management

1. **Add spools to Spoolman**: Use Spoolman's web interface to add your filament spools
2. **Map spools to toolheads**: Use the FilaBridge web interface to assign spools with smart search
3. **Monitor usage**: The system automatically tracks and updates filament usage
4. **Handle errors**: Acknowledge any print processing errors that require manual intervention

## API Endpoints

The web interface also provides REST API endpoints:

- `GET /api/status` - Get current printer status and mappings
- `GET /api/spools` - Get all spools from Spoolman
- `POST /api/map_toolhead` - Map a spool to a toolhead
- `POST /api/unmap_toolhead` - Unmap a spool from a toolhead
- `GET /api/print-errors` - Get all unacknowledged print errors
- `POST /api/print-errors/{id}/acknowledge` - Acknowledge a print error
- `WS /ws/status` - WebSocket endpoint for real-time status updates

## Project Structure

```
filabridge/
├── main.go                 # Application entry point
├── config.go              # Configuration management
├── prusalink.go           # PrusaLink API client
├── spoolman.go            # Spoolman API client
├── bridge.go              # Core monitoring and tracking logic
├── web.go                 # HTTP server and web interface
├── templates/             # HTML templates
├── go.mod                 # Go module definition
└── README.md              # Documentation
```

## Troubleshooting

### Common Issues

1. **Printers not accessible**:
   - Check IP addresses in the web interface configuration
   - Ensure PrusaLink is enabled on both printers
   - Verify network connectivity

2. **Spoolman connection failed**:
   - Make sure Spoolman is running
   - Check the Spoolman URL in the web interface configuration
   - Verify Spoolman is accessible at the specified URL

3. **Filament usage not tracked**:
   - Ensure spools are mapped to toolheads
   - Check that prints are completing (not just pausing)
   - Verify PrusaLink API is returning filament usage data

4. **WebSocket connection issues**:
   - Check browser console for WebSocket connection errors
   - Ensure no firewall is blocking WebSocket connections
   - The interface will fall back to periodic polling if WebSocket fails

5. **Print processing errors**:
   - Check the error notifications in the web interface
   - Acknowledge errors after manually updating Spoolman
   - Review logs for detailed error information

### Logs

The service logs important events to the console. Look for:
- Printer status updates
- Filament usage calculations
- Spoolman update confirmations
- WebSocket connection status
- Print processing errors
- Error messages

## Development

### Building from Source

```bash
# Download dependencies
go mod download

# Build the application
go build -o filabridge .

# Run tests
go test ./...

# Run with race detection
go run -race .
```

## Contributing

Contributions are welcome! Here's how you can help:

- 🐛 **Report bugs**: Open an issue with details about the problem
- 💡 **Suggest features**: Share your ideas for improvements
- 🔧 **Submit PRs**: Fix bugs or add features (please open an issue first for major changes)
- 📖 **Improve docs**: Help make the documentation clearer
- ⭐ **Star the repo**: Show your support!

See [CONTRIBUTING.md](CONTRIBUTING.md) for detailed guidelines.

## Roadmap

- [ ] Support for additional printer APIs
- [x] Provide a Docker Image
- [x] Real-time WebSocket updates
- [x] Enhanced spool search functionality
- [x] Print error handling and acknowledgment
- [ ] NFC Support
- [ ] Mobile-responsive UI improvements

## Support the Project

If you find FilaBridge useful:
- ⭐ Star the repository
- 🐛 Report bugs and suggest features
- 📢 Share it with the 3D printing community
- 🤝 Contribute code or documentation

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.

## Support

For issues specific to:
- **PrusaLink**: Check Prusa's documentation
- **Spoolman**: Visit the [Spoolman GitHub repository](https://github.com/pdrd/spoolman)
- **This bridge**: Open an issue in this repository
