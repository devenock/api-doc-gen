# Example APIs

Three complete REST APIs for testing and demonstrating `api-doc-gen` across different Go router libraries.

| Example | Framework | Module |
|---------|-----------|--------|
| [`ecommerce-gin/`](ecommerce-gin/) | Gin | Users, Products, Analytics |
| [`blog-chi/`](blog-chi/) | Chi | Authors, Posts, Analytics |
| [`crm-gorilla/`](crm-gorilla/) | Gorilla Mux | Users, Contacts, Deals, Analytics |

---

## Quick start

From the **repo root**, run the generator against any example:

```bash
# Swagger UI (default)
go run . generate examples/ecommerce-gin --no-interactive --type swagger -o /tmp/ecommerce-docs

# Postman collection
go run . generate examples/blog-chi --no-interactive --type postman -o /tmp/blog-pm

# Docusaurus site
go run . generate examples/crm-gorilla --no-interactive --type custom -o /tmp/crm-site
```

Or use the interactive wizard (omit `--no-interactive`):

```bash
go run . generate examples/ecommerce-gin
```

---

## Running the APIs locally

Each example is a self-contained Go module. Install dependencies and start the server:

```bash
cd examples/ecommerce-gin
go mod tidy
go run .           # starts on :8080

cd examples/blog-chi
go mod tidy
go run .           # starts on :8081

cd examples/crm-gorilla
go mod tidy
go run .           # starts on :8082
```

---

## Incremental documentation workflow

Developers rarely implement all modules at once. `api-doc-gen` reads whatever routes exist in your code — no config changes required.

**Pattern:** register one module, generate, ship docs, repeat.

### Example: adding modules one at a time to the Gin e-commerce API

**Step 1 — Auth and users only**

```go
// main.go
v1 := r.Group("/api/v1")

auth := v1.Group("/auth")
auth.POST("/register", handlers.Register)
auth.POST("/login", handlers.Login)

users := v1.Group("/users", JWTAuth(), AdminOnly())
users.GET("", handlers.ListUsers)
users.GET("/:id", handlers.GetUser)
```

```bash
go run . generate . --no-interactive --type swagger -o ./docs
# → 5 endpoints documented
```

**Step 2 — Add the products module**

```go
// main.go (append below the existing groups)
products := v1.Group("/products")
products.GET("", handlers.ListProducts)
products.GET("/:id", handlers.GetProduct)
products.POST("", handlers.CreateProduct)
```

```bash
go run . generate . --no-interactive --type swagger -o ./docs
# → 8 endpoints documented — users + products
```

**Step 3 — Add analytics**

```go
analytics := v1.Group("/analytics", JWTAuth(), AdminOnly())
analytics.GET("/sales", handlers.GetSalesReport)
analytics.GET("/revenue", handlers.GetRevenueReport)
```

```bash
go run . generate . --no-interactive --type swagger -o ./docs
# → 10 endpoints documented — all three modules
```

Each `generate` run overwrites the previous output and reflects the current state of your code exactly. No configuration file to maintain.

---

## What each example demonstrates

### `ecommerce-gin` — Gin (26 endpoints)

- **Group-based routing** with `r.Group("/api/v1")`
- **Middleware stacking** — `JWTAuth()` and `AdminOnly()` on separate groups
- **Mixed visibility** — public product listing + admin-only mutations
- **Request body binding** — `c.ShouldBindJSON(&req)` mapped to OpenAPI requestBody
- **Query parameters** — pagination, filtering, sorting extracted automatically
- **Path params** — `:id` normalized to `{id}` in the generated spec

### `blog-chi` — Chi (28 endpoints)

- **Nested route groups** — `r.Route("/posts", ...)` inside `r.Route("/api/v1", ...)`
- **Slug-based paths** — `/posts/{slug}` alongside `/posts/{slug}/comments`
- **Auth inheritance** — `r.Group("/", JWTAuth())` inside a public parent route block
- **`{param:regex}` normalization** — patterns stripped from path in the spec

### `crm-gorilla` — Gorilla Mux (25 endpoints)

- **Subrouter chaining** — two-level `PathPrefix().Subrouter()` for clean separation
- **`http.Method*` constants** — `Methods(http.MethodPost)` instead of string literals
- **Per-subrouter middleware** — `contacts.Use(JWTMiddleware)` scoped to one group
- **PATCH method** — `UpdateDealStage` on `/{id}/stage`
