# Rich API Documentation — Design & Roadmap

This document describes how to make the CLI produce **complete** documentation in one flow: `init` → `generate` → full Swagger with descriptions, request/response schemas, per-module organization, and JWT/auth on protected routes.

---

## 1. What “Everything” Means

| Deliverable | Description |
|-------------|-------------|
| **All endpoints** | Every route from every module, with correct full path (including groups). |
| **Descriptions** | Summary and description for each operation (from code or generated). |
| **Request bodies** | Per-endpoint request schema (e.g. JSON body) with fields and types. |
| **Response schemas** | Response body structure, not just `type: object`. |
| **Per-module organization** | Endpoints grouped by tag (e.g. `products`, `users`) in Swagger UI. |
| **JWT / auth** | Protected routes marked with `security: [BearerAuth]`; `securitySchemes` defined. |
| **Handler “annotations”** | Rich OpenAPI metadata for each handler, whether or not the code has swag comments. |

The CLI does **not** write swag annotations (e.g. `// @Summary`) into handler source files. It generates the OpenAPI spec from code; the **output** (openapi.yaml, openapi.json, index.html) is the single source of truth. An optional “write annotations back to source” feature could be added later.

---

## 2. Two Ways to Get Rich Docs

### Option A: Static analysis only (no AI)

- **How:** Parse the code: handler signatures, comments, middleware chains, route paths.
- **Pros:** No API keys, fast, deterministic, works offline, no cost.
- **Cons:** Descriptions are only as good as existing comments; no “understanding” of intent.

**What we can do today:**

1. **Descriptions** — Use the comment above the handler (we already do this in `extractHandlerComments`); extend to all frameworks and improve fallback (e.g. summary from handler name).
2. **Request/response schemas** — Resolve handler’s first argument (e.g. `func(c *gin.Context, req *CreateProductRequest)`) and return type; use Go types to build OpenAPI schemas (we need a type resolver or simple struct walking).
3. **Tags / modules** — Derive from path segment (e.g. `/api/v1/products` → tag `products`) or from file path (e.g. `internal/modules/products/routes/` → tag `products`).
4. **JWT / auth** — Detect middleware attached to routes (e.g. `group.Use(AuthRequired())` or `router.Use(JWT())`); mark those routes with `security: [BearerAuth]` and add `components.securitySchemes` in OpenAPI.

### Option B: AI-assisted enrichment (optional)

- **How:** Send handler code (and maybe route + framework) to an LLM; get back summary, description, and optionally request/response description.
- **Pros:** Rich, human-like text even when comments are missing; can infer auth from context.
- **Cons:** Needs an API key (OpenAI, Anthropic, etc.), cost and latency, non-deterministic.

**Practical approach:**

- **Optional feature** behind a flag (e.g. `--enrich-with-ai`) and env var for the API key (e.g. `APIDOC_OPENAI_API_KEY`).
- Use AI only where static analysis leaves gaps: e.g. generate summary/description when the handler has no or minimal comment.
- For **large projects:** batch handlers, cache results, and/or only run AI for “missing” descriptions to keep cost and time bounded.

---

## 3. Recommended Path: Hybrid

1. **Phase 1 — Static analysis (no AI)**  
   Implement everything we can from code:
   - Tags from path or directory.
   - JWT/auth from middleware detection.
   - Request body schema from handler’s first “body” type (e.g. struct pointer).
   - Response schema from named return type or common patterns.
   - Stronger use of existing comments (summary + description).

   Result: one `generate` run gives correct paths, modules, auth, and basic schemas; descriptions come from comments where present.

2. **Phase 2 — Optional AI enrichment**  
   Add:
   - Config/flag: `enrich_with_ai: true` or `--enrich-with-ai`.
   - Env: `APIDOC_OPENAI_API_KEY` (or similar).
   - For each endpoint with empty or very short description, call the LLM with handler code and route; write back summary + description (and optionally improve schema description).
   - Batching and optional caching for large codebases.

   Result: same flow, but docs are “filled in” where the code doesn’t explain itself.

---

## 4. Implementation Roadmap

### Phase 1 — Static analysis (order of implementation) ✅ Done

| Step | What | Where |
|------|------|--------|
| 1.1 | **Tags from path** — First path segment after base (e.g. `products`, `users`) → OpenAPI tags; ensure Swagger generator emits tags. | `pkg/analyzer`, `pkg/generator/swagger.go` |
| 1.2 | **JWT / security** — Detect middleware names (e.g. `Auth`, `JWT`, `AuthRequired`) on groups or routes; set `Endpoint.Security` and add `securitySchemes` in spec. | `pkg/analyzer` (middleware tracking), `pkg/generator/swagger.go` |
| 1.3 | **Request body from handler** — Resolve handler func; if first param (after context) is a struct, resolve its fields and build `RequestBody` schema. | `pkg/analyzer` (type resolution or simple struct scan) |
| 1.4 | **Response schema** — From handler’s return type or common response wrapper; at least improve beyond generic `object`. | `pkg/analyzer`, `pkg/models` |
| 1.5 | **Description fallbacks** — If no comment, use handler name (e.g. `CreateProduct`) or method+path as summary. | `pkg/analyzer` (already partially there; extend) |

### Phase 2 — Optional AI

| Step | What | Where |
|------|------|--------|
| 2.1 | **Config and flag** — `enrich_with_ai`, `openai_api_key` (or env-only); `--enrich-with-ai`, `--no-enrich`. | `pkg/config`, `cmd/root.go` |
| 2.2 | **Enrichment client** — Small client that calls OpenAI (or configurable provider) with a prompt: “Given this Go handler and route, return OpenAPI summary and description.” | New package, e.g. `pkg/enrich` or `internal/llm` |
| 2.3 | **Batch and cache** — Process endpoints in batches; optional file cache (e.g. `.apidoc-gen.cache`) keyed by handler path + content hash to avoid re-calling for unchanged code. | Same package, `cmd` |
| 2.4 | **Merge** — Only overwrite empty summary/description (or short ones); merge AI output into `Endpoint` before generating Swagger. | `cmd/root.go` (run after analyze, before generate) |

---

## 5. Handling Large Projects

- **Static analysis:** Already file-by-file; exclude dirs from config; no change needed for scale.
- **AI:**
  - Only enrich endpoints with missing/short descriptions.
  - Process in batches (e.g. 10–20 handlers per request) to reduce round-trips.
  - Cache results by (file path, handler name, content hash); skip if cache hit.
  - Make AI optional and off by default so large projects can run without key or cost.

---

## 6. Summary

- **Goal:** One flow (`init` → `generate`) produces full Swagger: all endpoints, descriptions, request/response schemas, per-module tags, JWT on protected routes.
- **Approach:** Static analysis covers structure, schemas, tags, and auth; optional AI fills in descriptions where code doesn’t.
- **No requirement** for developers to add annotations; the generated OpenAPI is the documentation. Optionally, a future step could write swag comments back to source.
- **Large backends:** Static part scales by design; AI is optional, batched, and cacheable.

Implement Phase 1 first so the CLI “handles everything” without AI; add Phase 2 when you want the best possible descriptions regardless of comment quality.
