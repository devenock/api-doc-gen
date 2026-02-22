package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/models"
)

// CustomGenerator generates a custom Docusaurus site
type CustomGenerator struct {
	config *config.Config
}

// NewCustomGenerator creates a new custom generator
func NewCustomGenerator(cfg *config.Config) *CustomGenerator {
	return &CustomGenerator{config: cfg}
}

// Generate creates a custom Docusaurus documentation site
func (g *CustomGenerator) Generate(spec *models.APISpec) error {
	docusaurusPath := g.config.Output

	// Check if Docusaurus is already initialized
	if _, err := os.Stat(filepath.Join(docusaurusPath, "package.json")); os.IsNotExist(err) {
		fmt.Println("   📦 Initializing Docusaurus site...")
		if err := g.initDocusaurus(docusaurusPath); err != nil {
			return fmt.Errorf("failed to initialize Docusaurus: %w", err)
		}
	} else {
		fmt.Println("   📦 Using existing Docusaurus site...")
	}

	// Generate API documentation pages
	fmt.Println("   📝 Generating API documentation pages...")
	if err := g.generateAPIPages(spec, docusaurusPath); err != nil {
		return fmt.Errorf("failed to generate API pages: %w", err)
	}

	// Update sidebars
	if err := g.updateSidebars(spec, docusaurusPath); err != nil {
		return fmt.Errorf("failed to update sidebars: %w", err)
	}

	// Update docusaurus config
	if err := g.updateDocusaurusConfig(spec, docusaurusPath); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}

	fmt.Printf("   🌐 Docusaurus site ready at: %s\n", docusaurusPath)
	fmt.Println("   💡 Run 'npm start' in the output directory to view the site")
	fmt.Println("   💡 Run 'npm run build' to create a production build")

	return nil
}

// initDocusaurus initializes a new Docusaurus site
func (g *CustomGenerator) initDocusaurus(path string) error {
	// Create directory
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}

	// Check if npx is available
	if _, err := exec.LookPath("npx"); err != nil {
		return fmt.Errorf("npx not found. Please install Node.js and npm")
	}

	// Initialize Docusaurus using create-docusaurus
	cmd := exec.Command("npx", "create-docusaurus@latest", path, "classic", "--skip-install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// If npx fails, create a minimal structure manually
		return g.createMinimalDocusaurus(path)
	}

	return nil
}

// createMinimalDocusaurus creates a minimal Docusaurus structure manually
func (g *CustomGenerator) createMinimalDocusaurus(path string) error {
	// Create necessary directories
	dirs := []string{
		filepath.Join(path, "docs"),
		filepath.Join(path, "blog"),
		filepath.Join(path, "src", "pages"),
		filepath.Join(path, "src", "css"),
		filepath.Join(path, "static", "img"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	// Create package.json
	packageJSON := `{
  "name": "` + strings.ToLower(strings.ReplaceAll(g.config.Title, " ", "-")) + `",
  "version": "` + g.config.Version + `",
  "private": true,
  "scripts": {
    "docusaurus": "docusaurus",
    "start": "docusaurus start",
    "build": "docusaurus build",
    "swizzle": "docusaurus swizzle",
    "deploy": "docusaurus deploy",
    "clear": "docusaurus clear",
    "serve": "docusaurus serve",
    "write-translations": "docusaurus write-translations",
    "write-heading-ids": "docusaurus write-heading-ids"
  },
  "dependencies": {
    "@docusaurus/core": "^3.0.0",
    "@docusaurus/preset-classic": "^3.0.0",
    "@mdx-js/react": "^3.0.0",
    "clsx": "^2.0.0",
    "prism-react-renderer": "^2.1.0",
    "react": "^18.2.0",
    "react-dom": "^18.2.0"
  },
  "devDependencies": {
    "@docusaurus/module-type-aliases": "^3.0.0",
    "@docusaurus/types": "^3.0.0"
  },
  "browserslist": {
    "production": [
      ">0.5%",
      "not dead",
      "not op_mini all"
    ],
    "development": [
      "last 1 chrome version",
      "last 1 firefox version",
      "last 1 safari version"
    ]
  }
}`

	if err := os.WriteFile(filepath.Join(path, "package.json"), []byte(packageJSON), 0644); err != nil {
		return err
	}

	// Create docusaurus.config.js
	docusaurusConfig := `// @ts-check
// Note: type annotations allow type checking and IDEs autocompletion

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: '` + g.config.Title + `',
  tagline: '` + g.config.Description + `',
  url: 'https://your-docusaurus-test-site.com',
  baseUrl: '/',
  onBrokenLinks: 'warn',
  onBrokenMarkdownLinks: 'warn',
  favicon: 'img/favicon.ico',

  organizationName: 'your-org',
  projectName: 'api-docs',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          sidebarPath: require.resolve('./sidebars.js'),
          routeBasePath: '/',
        },
        blog: false,
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      navbar: {
        title: '` + g.config.Title + `',
        items: [
          {
            type: 'doc',
            docId: 'intro',
            position: 'left',
            label: 'API Documentation',
          },
        ],
      },
      footer: {
        style: 'dark',
        copyright: 'Built with ❤️ using apidoc-gen',
      },
      prism: {
        theme: require('prism-react-renderer').themes.github,
        darkTheme: require('prism-react-renderer').themes.dracula,
      },
    }),
};

module.exports = config;
`

	if err := os.WriteFile(filepath.Join(path, "docusaurus.config.js"), []byte(docusaurusConfig), 0644); err != nil {
		return err
	}

	// Create custom.css
	customCSS := `/**
 * Any CSS included here will be global. The classic template
 * bundles Infima by default. Infima is a CSS framework designed to
 * work well for content-centric websites.
 */

/* You can override the default Infima variables here. */
:root {
  --ifm-color-primary: #2e8555;
  --ifm-color-primary-dark: #29784c;
  --ifm-color-primary-darker: #277148;
  --ifm-color-primary-darkest: #205d3b;
  --ifm-color-primary-light: #33925d;
  --ifm-color-primary-lighter: #359962;
  --ifm-color-primary-lightest: #3cad6e;
  --ifm-code-font-size: 95%;
  --docusaurus-highlighted-code-line-bg: rgba(0, 0, 0, 0.1);
}

/* For readability concerns, you should choose a lighter palette in dark mode. */
[data-theme='dark'] {
  --ifm-color-primary: #25c2a0;
  --ifm-color-primary-dark: #21af90;
  --ifm-color-primary-darker: #1fa588;
  --ifm-color-primary-darkest: #1a8870;
  --ifm-color-primary-light: #29d5b0;
  --ifm-color-primary-lighter: #32d8b4;
  --ifm-color-primary-lightest: #4fddbf;
  --docusaurus-highlighted-code-line-bg: rgba(0, 0, 0, 0.3);
}
`

	if err := os.WriteFile(filepath.Join(path, "src", "css", "custom.css"), []byte(customCSS), 0644); err != nil {
		return err
	}

	return nil
}

// generateAPIPages generates markdown pages for each endpoint
func (g *CustomGenerator) generateAPIPages(spec *models.APISpec, docusaurusPath string) error {
	docsPath := filepath.Join(docusaurusPath, "docs")

	// Create intro page
	intro := `---
sidebar_position: 1
---

# Introduction

Welcome to the ` + spec.Title + ` documentation!

**Version:** ` + spec.Version + `

` + spec.Description + `

## Base URLs

`
	for _, server := range spec.Servers {
		intro += fmt.Sprintf("- **%s**: %s\n", server.Description, server.URL)
	}

	intro += `

## Available Endpoints

This documentation provides detailed information about all available API endpoints.

Navigate through the sidebar to explore different API endpoints and their usage.
`

	if err := os.WriteFile(filepath.Join(docsPath, "intro.md"), []byte(intro), 0644); err != nil {
		return err
	}

	// Group endpoints by path prefix or tags
	endpointsByGroup := g.groupEndpoints(spec.Endpoints)

	// Generate pages for each group
	for group, endpoints := range endpointsByGroup {
		groupDir := filepath.Join(docsPath, strings.ToLower(strings.ReplaceAll(group, " ", "-")))
		if err := os.MkdirAll(groupDir, 0755); err != nil {
			return err
		}

		// Create group index
		groupIndex := fmt.Sprintf(`---
sidebar_position: 2
---

# %s

API endpoints for %s operations.

`, group, strings.ToLower(group))

		if err := os.WriteFile(filepath.Join(groupDir, "index.md"), []byte(groupIndex), 0644); err != nil {
			return err
		}

		// Create page for each endpoint
		for i, endpoint := range endpoints {
			page := g.generateEndpointPage(endpoint, i+1)
			filename := fmt.Sprintf("%s-%s.md",
				strings.ToLower(endpoint.Method),
				strings.ToLower(strings.ReplaceAll(strings.Trim(endpoint.Path, "/"), "/", "-")))

			if err := os.WriteFile(filepath.Join(groupDir, filename), []byte(page), 0644); err != nil {
				return err
			}
		}
	}

	return nil
}

// groupEndpoints groups endpoints by a common prefix or tag
func (g *CustomGenerator) groupEndpoints(endpoints []models.Endpoint) map[string][]models.Endpoint {
	grouped := make(map[string][]models.Endpoint)

	for _, endpoint := range endpoints {
		group := "General"

		// Use tags if available
		if len(endpoint.Tags) > 0 {
			group = endpoint.Tags[0]
		} else {
			// Extract from path
			parts := strings.Split(strings.Trim(endpoint.Path, "/"), "/")
			if len(parts) > 0 && parts[0] != "" {
				group = strings.Title(parts[0])
			}
		}

		grouped[group] = append(grouped[group], endpoint)
	}

	return grouped
}

// generateEndpointPage generates a markdown page for an endpoint
func (g *CustomGenerator) generateEndpointPage(endpoint models.Endpoint, position int) string {
	method := strings.ToUpper(endpoint.Method)

	page := fmt.Sprintf(`---
sidebar_position: %d
---

# %s %s

%s

`, position, method, endpoint.Path, endpoint.Description)

	// Method badge
	page += fmt.Sprintf("**Method:** <span style=\"background-color: #%s; color: white; padding: 2px 8px; border-radius: 3px; font-weight: bold;\">%s</span>\n\n",
		g.getMethodColor(method), method)

	page += fmt.Sprintf("**Endpoint:** `%s`\n\n", endpoint.Path)

	// Parameters
	if len(endpoint.Parameters) > 0 {
		page += "## Parameters\n\n"

		// Group by type
		pathParams := []models.Parameter{}
		queryParams := []models.Parameter{}
		headerParams := []models.Parameter{}

		for _, param := range endpoint.Parameters {
			switch param.In {
			case "path":
				pathParams = append(pathParams, param)
			case "query":
				queryParams = append(queryParams, param)
			case "header":
				headerParams = append(headerParams, param)
			}
		}

		if len(pathParams) > 0 {
			page += "### Path Parameters\n\n"
			page += "| Name | Type | Required | Description |\n"
			page += "|------|------|----------|-------------|\n"
			for _, param := range pathParams {
				required := "Yes"
				if !param.Required {
					required = "No"
				}
				page += fmt.Sprintf("| `%s` | %s | %s | %s |\n",
					param.Name, param.Schema.Type, required, param.Description)
			}
			page += "\n"
		}

		if len(queryParams) > 0 {
			page += "### Query Parameters\n\n"
			page += "| Name | Type | Required | Description |\n"
			page += "|------|------|----------|-------------|\n"
			for _, param := range queryParams {
				required := "Yes"
				if !param.Required {
					required = "No"
				}
				page += fmt.Sprintf("| `%s` | %s | %s | %s |\n",
					param.Name, param.Schema.Type, required, param.Description)
			}
			page += "\n"
		}

		if len(headerParams) > 0 {
			page += "### Header Parameters\n\n"
			page += "| Name | Type | Required | Description |\n"
			page += "|------|------|----------|-------------|\n"
			for _, param := range headerParams {
				required := "Yes"
				if !param.Required {
					required = "No"
				}
				page += fmt.Sprintf("| `%s` | %s | %s | %s |\n",
					param.Name, param.Schema.Type, required, param.Description)
			}
			page += "\n"
		}
	}

	// Request Body
	if endpoint.RequestBody != nil {
		page += "## Request Body\n\n"
		page += endpoint.RequestBody.Description + "\n\n"

		if jsonContent, ok := endpoint.RequestBody.Content["application/json"]; ok {
			page += "```json\n"
			page += g.schemaToJSON(jsonContent.Schema, 0)
			page += "\n```\n\n"
		}
	}

	// Responses
	page += "## Responses\n\n"
	for code, response := range endpoint.Responses {
		page += fmt.Sprintf("### %d - %s\n\n", code, response.Description)

		if len(response.Content) > 0 {
			if jsonContent, ok := response.Content["application/json"]; ok {
				page += "```json\n"
				page += g.schemaToJSON(jsonContent.Schema, 0)
				page += "\n```\n\n"
			}
		}
	}

	// Example Request
	page += "## Example Request\n\n"
	page += fmt.Sprintf("```bash\ncurl -X %s \\\n", method)
	page += fmt.Sprintf("  '%s%s' \\\n", "{{baseUrl}}", endpoint.Path)
	page += "  -H 'Content-Type: application/json'\n"
	page += "```\n"

	return page
}

// getMethodColor returns a color code for HTTP methods
func (g *CustomGenerator) getMethodColor(method string) string {
	colors := map[string]string{
		"GET":    "61affe",
		"POST":   "49cc90",
		"PUT":    "fca130",
		"DELETE": "f93e3e",
		"PATCH":  "50e3c2",
	}

	if color, ok := colors[method]; ok {
		return color
	}
	return "999999"
}

// schemaToJSON converts a schema to JSON representation
func (g *CustomGenerator) schemaToJSON(schema models.Schema, indent int) string {
	indentStr := strings.Repeat("  ", indent)

	if schema.Type == "object" {
		result := "{\n"
		i := 0
		for name, prop := range schema.Properties {
			if i > 0 {
				result += ",\n"
			}
			result += fmt.Sprintf("%s  \"%s\": %s", indentStr, name, g.schemaToJSON(prop, indent+1))
			i++
		}
		result += fmt.Sprintf("\n%s}", indentStr)
		return result
	} else if schema.Type == "array" {
		if schema.Items != nil {
			return fmt.Sprintf("[\n%s  %s\n%s]", indentStr, g.schemaToJSON(*schema.Items, indent+1), indentStr)
		}
		return "[]"
	}

	// Primitive types
	switch schema.Type {
	case "string":
		return "\"string\""
	case "integer", "number":
		return "0"
	case "boolean":
		return "false"
	default:
		return "null"
	}
}

// updateSidebars updates the sidebars configuration
func (g *CustomGenerator) updateSidebars(spec *models.APISpec, docusaurusPath string) error {
	sidebarsJS := `// @ts-check

/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  apiSidebar: [
    'intro',
    {
      type: 'category',
      label: 'API Endpoints',
      items: [
        {
          type: 'autogenerated',
          dirName: '.',
        },
      ],
    },
  ],
};

module.exports = sidebars;
`

	return os.WriteFile(filepath.Join(docusaurusPath, "sidebars.js"), []byte(sidebarsJS), 0644)
}

// updateDocusaurusConfig updates the Docusaurus configuration
func (g *CustomGenerator) updateDocusaurusConfig(spec *models.APISpec, docusaurusPath string) error {
	// The config is already created in createMinimalDocusaurus
	// This function can be used to make additional updates if needed
	return nil
}
