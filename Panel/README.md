# CorvusMiner Control Panel

A modern web-based control panel for managing mining operations, built with Go and SQLite.

## Features

- **Dashboard**: Real-time overview of mining operations and statistics
- **Miner Management**: Add, remove, and monitor mining devices
- **Configuration**: Centralized settings for pool connection and mining parameters
- **Responsive Design**: Works seamlessly on desktop and mobile devices
- **RESTful API**: JSON endpoints for programmatic access
- **Real-time Updates**: Dynamic data refresh without page reloads

## Project Structure

```
Panel/
├── main.go                 # Application entry point
├── go.mod                  # Go module dependencies
├── handlers/
│   └── handlers.go        # HTTP request handlers
├── models/
│   └── models.go          # Data models (Miner, Config)
├── database/
│   └── db.go              # SQLite database operations
├── templates/
│   ├── layout.html        # Base template
│   ├── dashboard.html     # Dashboard page
│   ├── miners.html        # Miner management page
│   └── config.html        # Configuration page
├── static/
│   ├── css/
│   │   └── style.css      # Stylesheet
│   └── js/
│       └── main.js        # Frontend JavaScript
└── corvusminer.db           # SQLite database (auto-created)
```

## Prerequisites

- Go 1.21 or later
- Git

## Installation

1. Clone the repository or navigate to the project directory:
```bash
cd Panel
```

2. Download Go dependencies:
```bash
go mod download
```

3. Build the project:
```bash
go build -o corvusminer-panel
```

## Running the Application

```bash
./corvusminer-panel
```

The application will start on `http://localhost:8080`

### For Development (with auto-reload)

Install air for hot-reloading:
```bash
go install github.com/cosmtrek/air@latest
air
```

## Usage

### Dashboard
Access the main dashboard at `http://localhost:8080/` to view:
- Total number of miners
- Pool configuration
- Recent miner statistics

### Miner Management
Navigate to `/miners` to:
- View all configured miners
- Add new miners (name, IP, port)
- Delete miners
- Monitor real-time status

### Configuration
Navigate to `/config` to:
- Set pool URL (e.g., `stratum+tcp://pool.example.com:3333`)
- Configure worker name
- Set difficulty target
- Enable/disable SSL/TLS
- Adjust update intervals

## API Endpoints

### Miners
- `GET /api/miners` - Get all miners
- `POST /api/miners/add` - Add new miner
- `POST /api/miners/delete` - Delete miner

### Configuration
- `GET /api/config/get` - Get current configuration
- `POST /api/config/update` - Update configuration

### Pages
- `GET /` - Dashboard
- `GET /miners` - Miner list page
- `GET /config` - Configuration page

## Database Schema

### Miners Table
```sql
CREATE TABLE miners (
    id INTEGER PRIMARY KEY,
    name TEXT UNIQUE,
    ip TEXT,
    port INTEGER,
    status TEXT,
    hashrate TEXT,
    shares INTEGER,
    created_at TIMESTAMP
);
```

### Config Table
```sql
CREATE TABLE config (
    id INTEGER PRIMARY KEY,
    pool_url TEXT,
    worker_name TEXT,
    difficulty_target TEXT,
    enable_ssl INTEGER,
    update_interval INTEGER,
    updated_at TIMESTAMP
);
```

## Extending the Project

### Adding New Handlers
Add new handler functions in `handlers/handlers.go`:
```go
func (h *Handler) NewPage(w http.ResponseWriter, r *http.Request) {
    // Implementation
}
```

### Adding New Database Operations
Extend `database/db.go` with additional query methods:
```go
func (db *DB) NewOperation() error {
    // Implementation
}
```

### Adding New Templates
Create new HTML templates in `templates/` directory and register them in handlers.

### Styling
Modify `static/css/style.css` or add new CSS files and link them in `templates/layout.html`.

## Development Notes

- The application uses Go's `html/template` package for templating
- SQLite database is automatically created on first run as `corvusminer.db`
- Static files are served with proper MIME types
- All API responses use JSON format
- CORS is not configured (for internal use)

## Troubleshooting

### Database Issues
If you encounter database lock errors, ensure the application isn't running multiple times:
```bash
pkill corvusminer-panel
```

### Port Already in Use
If port 8080 is in use, modify `main.go` to use a different port:
```go
log.Fatal(http.ListenAndServe(":YOUR_PORT", nil))
```

### Static Files Not Loading
Ensure you run the application from the project directory where the `static/` folder exists.

## License

Proprietary - CorvusMiner 2025

## Support

For issues or feature requests, please contact the development team.
