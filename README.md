# API Documentation Generator (apidoc-gen)

A powerful Go CLI tool that automatically generates API documentation by scanning your codebase. Supports multiple output formats including Swagger/OpenAPI, Postman collections, and custom Docusaurus websites.

## Features

- 🔍 **Automatic Code Analysis** - Scans your Go codebase to detect API endpoints
- 🎯 **Framework Detection** - Supports Gin, Echo, Fiber, Gorilla Mux, Chi, and more
- 📚 **Multiple Output Formats**:
  - **Swagger/OpenAPI 3.0** - Industry-standard API documentation with Swagger UI
  - **Postman Collection** - Import directly into Postman
  - **Custom Docusaurus Site** - Beautiful, searchable documentation website
- ⚙️ **Configuration Management** - Use Viper for flexible configuration
- 🎨 **Interactive CLI** - Built with Cobra for a great user experience
- 🚀 **Fast and Efficient** - Written in Go for maximum performance

## Installation

### Prerequisites

- Go 1.22 or higher
- Node.js and npm (for custom Docusaurus output)

### Build from Source

```bash
git clone https://github.com/yourusername/apidoc-gen.git
cd apidoc-gen
go build -o apidoc-gen
```

Or install directly:

```bash
go install github.com/yourusername/apidoc-gen@latest
```

## Quick Start

### 1. Initialize Configuration

```bash
apidoc-gen init
```

This creates a `.apidoc-gen.yaml` file in your current directory.

### 2. Generate Documentation

Run in interactive mode:

```bash
apidoc-gen generate
```

Or specify all options via flags:

```bash
apidoc-gen generate \
  --type swagger \
  --output ./docs \
  --framework gin \
  --title "My API" \
  --version "1.0.0"
```

### 3. View Your Documentation

**For Swagger:**
```bash
cd docs
# Open index.html in your browser
```

**For Postman:**
```bash
# Import docs/collection.json into Postman
```

**For Custom Docusaurus:**
```bash
cd docs
npm install
npm start
```

## Usage

### Commands

#### `generate`

Generate API documentation from your codebase.

```bash
apidoc-gen generate [path] [flags]
```

**Flags:**
- `-o, --output` - Output directory (default: "./docs")
- `-t, --type` - Documentation type: swagger, postman, or custom
- `-f, --framework` - Backend framework: gin, echo, fiber, gorilla, chi (auto-detected if not specified)
- `--title` - API title (default: "API Documentation")
- `--version` - API version (default: "1.0.0")
- `--base-path` - Base path for API endpoints (e.g., /api/v1)
- `--exclude` - Directories to exclude (comma-separated)
- `--interactive` - Use interactive mode (default: true)
- `-v, --verbose` - Verbose output

**Examples:**

```bash
# Interactive mode (recommended for first time)
apidoc-gen generate

# Generate Swagger docs for current directory
apidoc-gen generate --type swagger

# Generate Postman collection for a specific project
apidoc-gen generate /path/to/project --type postman --output ./api-docs

# Generate custom Docusaurus site
apidoc-gen generate --type custom --framework gin

# Generate with full configuration
apidoc-gen generate \
  --type swagger \
  --framework echo \
  --title "My Awesome API" \
  --version "2.0.0" \
  --base-path "/api/v2" \
  --exclude "vendor,test,tmp"
```

#### `init`

Create a configuration file in the current directory.

```bash
apidoc-gen init
```

Creates `.apidoc-gen.yaml` with default settings.

### Configuration File

The `.apidoc-gen.yaml` file allows you to set default values:

```yaml
# Output directory for generated documentation
output: ./docs

# Documentation type: swagger, postman, or custom
type: swagger

# Backend framework (optional): gin, echo, fiber, gorilla, chi
# Leave empty for auto-detection
framework: ""

# Directories to exclude from scanning
exclude:
  - vendor
  - node_modules
  - .git
  - test
  - tests

# API base path (e.g., /api/v1)
base_path: ""

# API metadata
title: "API Documentation"
version: "1.0.0"
description: "Auto-generated API documentation"

# Server configuration
servers:
  - url: "http://localhost:8080"
    description: "Development server"
  - url: "https://api.example.com"
    description: "Production server"

# Verbose output
verbose: false
```

### Environment Variables

Configuration can also be set via environment variables (prefixed with `APIDOC_`):

```bash
export APIDOC_TYPE=swagger
export APIDOC_OUTPUT=./docs
export APIDOC_FRAMEWORK=gin
apidoc-gen generate
```

## Supported Frameworks

- **Gin** - `github.com/gin-gonic/gin`
- **Echo** - `github.com/labstack/echo`
- **Fiber** - `github.com/gofiber/fiber`
- **Gorilla Mux** - `github.com/gorilla/mux`
- **Chi** - `github.com/go-chi/chi`
- Generic route detection for other frameworks

## Output Formats

### Swagger/OpenAPI

Generates:
- `openapi.json` - OpenAPI 3.0 specification (JSON)
- `openapi.yaml` - OpenAPI 3.0 specification (YAML)
- `index.html` - Swagger UI for interactive documentation

Features:
- Full OpenAPI 3.0 compliance
- Request/response schemas
- Parameter documentation
- Authentication specifications

### Postman Collection

Generates:
- `collection.json` - Postman Collection v2.1

Features:
- Organized by tags/endpoints
- Pre-filled example requests
- Environment variables
- Query parameters and headers

### Custom Docusaurus Site

Generates a complete Docusaurus documentation website:

Features:
- Searchable documentation
- Dark mode support
- Responsive design
- Markdown-based pages
- Easy customization
- Static site generation

## Best Practices

### 1. Document Your Code

Add **plain Go comments** to your handler functions. The tool uses them for summary and description:

```go
// GetUser retrieves a user by ID.
// It returns 404 if the user is not found.
func GetUser(c *gin.Context) {
    // Implementation
}
```

> **Note:** Swag-style annotations (`@Summary`, `@Param`, `@Success`, `@Router`, etc.) are not parsed yet. Only the handler’s doc comment is used for summary and description. Path parameters are inferred from route patterns (e.g. `/users/:id`).

### 2. Use Configuration Files

Create `.apidoc-gen.yaml` in your project root for consistent documentation generation across your team.

### 3. Exclude Unnecessary Directories

Always exclude vendor, test, and node_modules directories to speed up analysis.

### 4. Version Your API

Use semantic versioning and update the version in your config when releasing new API versions.

### 5. Automate Documentation

Add documentation generation to your CI/CD pipeline:

```bash
#!/bin/bash
# .github/workflows/docs.yml
apidoc-gen generate --type swagger --output ./docs
# Deploy docs to GitHub Pages or hosting service
```

## Project Structure

```
apidoc-gen/
├── cmd/
│   └── root.go            # CLI commands
├── pkg/
│   ├── analyzer/           # Code analysis
│   │   └── analyzer.go
│   ├── generator/          # Documentation generators
│   │   ├── generator.go
│   │   ├── swagger.go
│   │   ├── postman.go
│   │   └── custom.go
│   ├── models/             # Data models
│   │   └── models.go
│   └── config/             # Configuration
│       └── config.go
├── internal/
│   └── prompt/             # Interactive prompts
│       └── prompt.go
├── go.mod
├── go.sum
├── main.go
├── Makefile
└── README.md
```

## Development

### Building

```bash
make build
```

### Testing

```bash
make test
```

### Running

```bash
make run
```

### Installing Locally

```bash
make install
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## Troubleshooting

### "No endpoints found"

- Ensure your framework is correctly detected or specified
- Check that your route definitions follow standard patterns
- Use verbose mode (`-v`) to see detailed analysis

### "npx not found" (Custom Docusaurus)

- Install Node.js and npm from https://nodejs.org
- Ensure npm is in your PATH

### Configuration not being read

- Ensure `.apidoc-gen.yaml` is in the current directory or specify with `--config`
- Check YAML syntax

## License

MIT License - see LICENSE file for details

## Acknowledgments

- Built with [Cobra](https://github.com/spf13/cobra) for CLI
- Configuration powered by [Viper](https://github.com/spf13/viper)
- Interactive prompts using [promptui](https://github.com/manifoldco/promptui)
- Documentation powered by [Docusaurus](https://docusaurus.io/)

## Support

- 📖 [Documentation](https://github.com/yourusername/apidoc-gen/wiki)
- 🐛 [Issue Tracker](https://github.com/yourusername/apidoc-gen/issues)
- 💬 [Discussions](https://github.com/yourusername/apidoc-gen/discussions)

---

**Made with ❤️ by the API Documentation Generator Team**
