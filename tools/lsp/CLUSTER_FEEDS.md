# Cluster completion feeds

The stdio language server accepts these optional JSON-RPC notifications:

- `openchoreo/updateResources`: `{ "Kind": ["name"], "ComponentRelease:componentName": ["releaseName"] }`
- `openchoreo/updateResourceSchemas`: `{ "Kind": { "name": { "parameters": { "type": "object", "properties": {} }, "environmentConfigs": { "type": "object", "properties": {} } } } }`

Each message replaces its corresponding session snapshot. Send `{}` to clear it. Invalid messages retain the previous snapshot and are logged. The two snapshots are independent; clients should clear both on logout or context changes and resend data after reconnecting.

Cluster reference names supplement local suggestions. Matching local definitions take priority over received schemas. These two feeds supplement completion; they do not create filesystem entries. Navigation and missing-reference checks use the separate location snapshot below. The existing authentication check remains required before the server starts. The client fetches cluster data; the language server does not contact the cluster.


## Navigation and reference validation

`openchoreo/updateResourceLocations` replaces navigation targets atomically:

```json
{
  "contextId": "active-context-id",
  "namespace": "default",
  "generation": 1,
  "completeKinds": ["ComponentType", "Trait"],
  "resources": [
    {
      "kind": "ComponentType",
      "name": "service",
      "namespace": "default",
      "workloadType": "deployment",
      "uri": "openchoreo://active-context-id/namespaces/default/component-type/service.yaml"
    }
  ]
}
```

Clients that fetch cluster resources should set initialization option
`clusterReferenceFeeds: true` so missing-reference validation waits for feeds
from the start of the session.

`completeKinds` is optional and backward compatible. Include a kind only after
successfully fetching its entire list for this context and namespace. An empty
successful list still counts as complete; failed or unavailable lists do not.
Snapshots without this field support navigation but do not prove references
missing from the cluster. The names and schemas notification formats are unchanged.

Before refreshing or switching context/namespace, send the new generation with
`resources: []` and omit `completeKinds`. On logout, also set `contextId` and
`namespace` to null. Older generations and malformed snapshots are rejected
without replacing valid state. Resend the complete snapshot after server restart.

Missing references produce `unresolved-resource-reference` error diagnostics on
the name value. Matching local definitions take priority. Checks respect resource
kind, namespace, workload type, and component ownership for releases. Workspace-only
clients keep local validation; cluster-connected sessions suppress missing-reference
errors until the relevant list is complete. Updates revalidate open documents.

Quick fixes offer up to three scoped, deduplicated names with case-insensitive
Levenshtein similarity of at least 0.6, ranked by similarity, matching path prefix,
and lexical order. Only a uniquely best score is preferred. Edits preserve YAML
quotes and carry the document version, allowing clients to reject stale edits.
No reference name is replaced automatically and no cluster writes are performed.
