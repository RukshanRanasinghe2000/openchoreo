# JSON schema generator

Generate the minimal schema format consumed by the existing LSP and linter
from OpenChoreo API Go types and kubebuilder validation markers. This is a
developer tool, separate from the distributed `occ` executable.

Run from the OpenChoreo repository root:

```sh
go run ./tools/lang-server-utils/json-schema-generator --config tools/lang-server-utils/json-schema-generator/json-schema-generator.config.json
```

Or use the equivalent Make target:

```sh
make lint-schemas
```

The example config reads this checkout's `api/v1alpha1`, derives the version
from the first release heading in `CHANGELOG.md` (stripping prerelease suffixes),
and writes `tools/templates/template-vX.Y.Z/*.json`. It also updates
`tools/templates/template-version.json`; generating an older version does not
demote a newer version that still has a template directory.

Generation overwrites matching output files. Review the resulting diff before
committing. This tool emits the existing minimal format, not a general-purpose
JSON Schema document, and does not generate CEL environment data.

## Other versions and output directories

Use a separate config to generate from matching historical source checkouts:

```json
{
  "templatesDir": "/path/to/scratch/templates",
  "sources": [
    { "root": "/path/to/openchoreo-v1.2.0", "version": "v1.2.0" },
    { "root": "/path/to/openchoreo-v1.3.0", "version": "v1.3.0" }
  ]
}
```

Each source requires `root`; optional `api` overrides `<root>/api/v1alpha1`.
An explicit `version` must have the form `vX.Y.Z` and bypasses changelog lookup.
Use the corresponding API source for each historical version; setting an old
version label on current API types does not reconstruct that release's schemas.

All relative paths, including paths inside the config, resolve from the process
working directory, not the config directory. Unknown config fields are rejected.
Without `templatesDir`, output defaults to `tools/templates`; without sources,
the source defaults to the working directory.

```sh
make lint-schemas JSON_SCHEMA_CONFIG=/path/to/custom-config.json
go build -o /path/to/bin/json-schema-generator ./tools/lang-server-utils/json-schema-generator
```

A compiled executable accepts the same `--config` option. When it is omitted,
the tool looks for `json-schema-generator.config.json` beside that executable;
use an explicit config when running with `go run`.

```sh
go test ./tools/lang-server-utils/json-schema-generator/... -count=1
```

## Release pipeline

Release builds use the shared coordinator with `VERSION`, `--template-version`,
`--no-manifest`, and `--strict`. The coordinator writes the final manifest only
once both JSON and CEL generation succeed. Use `make go.build.release.occ` to
build a latest-only executable without modifying checked-in historical templates.
See [release instructions](../../../docs/contributors/occ-template-releases.md).
