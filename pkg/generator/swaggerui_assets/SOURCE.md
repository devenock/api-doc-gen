Vendored from `swagger-ui-dist@5.32.15` (https://www.npmjs.com/package/swagger-ui-dist),
fetched via `https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.32.15/<file>` and verified
against jsdelivr's own published SHA-256 hashes before being committed:

| File | SHA-256 (base64) |
|------|-------------------|
| `swagger-ui.css` | `1/OfdkqhjHtH3QW5r1YT43PkrA81V8JpPVLQq8JGTXY=` |
| `swagger-ui-bundle.js` | `p+NE8ncLLwdSfOgo4JUWJpg7jy3Nt6gmaJwCMgI/mVs=` |
| `swagger-ui-standalone-preset.js` | `GtL/16I23KTlcM4rou895yHXVT1OdEnbWN9myylDEek=` |

Embedded into the binary (see `swagger.go`) and written alongside the generated
`index.html` so the Swagger UI works offline with no runtime CDN dependency and
no third party able to alter what gets served, unlike loading these from a CDN
at view-time. To upgrade: pick a new pinned version, re-download the same three
files, re-verify their hashes via
`https://data.jsdelivr.com/v1/packages/npm/swagger-ui-dist@<version>?structure=flat`,
and update this table.
