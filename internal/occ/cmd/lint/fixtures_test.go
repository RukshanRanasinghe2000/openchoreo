// Copyright 2025 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lint_test

// Fixtures for the CLI contract test, inherited verbatim from the toolkit so
// the recorded behavior is comparable. The components are deliberately small:
// the point is to exercise the dispatch, formatting and exit-code surface, not
// to re-test the rule engine, which has its own tests in tools/lint.

// validComponent passes validation, and is the baseline for scenarios whose
// expected exit code is 0.
const validComponent = `apiVersion: openchoreo.dev/v1alpha1
kind: Component
metadata:
  name: my-app
  namespace: default
spec:
  componentType:
    kind: ClusterComponentType
    name: deployment/service
  owner:
    projectName: default
`

// invalidComponent violates the metadata.name pattern, so validation reports
// findings and exits 1.
const invalidComponent = `apiVersion: openchoreo.dev/v1alpha1
kind: Component
metadata:
  name: "INVALID_NAME_UPPERCASE"
  namespace: default
spec:
  componentType:
    kind: ClusterComponentType
    name: deployment/service
  owner:
    projectName: default
`

// typoComponent misspells componentTyp, so it carries a fixable unknown-field
// finding. It backs the -fix and -dry-run scenarios; -dry-run alone is a usage
// error, which is the point of usage-dry-run-without-fix.
const typoComponent = `apiVersion: openchoreo.dev/v1alpha1
kind: Component
metadata:
  name: my-app
  namespace: default
spec:
  componentTyp:
    kind: ClusterComponentType
    name: deployment/service
  owner:
    projectName: default
`
