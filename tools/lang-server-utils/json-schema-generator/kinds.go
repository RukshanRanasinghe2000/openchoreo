// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"strings"
)

// kindSpec describes one resource kind whose minimal JSON schema should be
// generated from its Go type in the v1alpha1 API package.
type kindSpec struct {
	// GoTypeName is the top-level resource struct, e.g. "Environment".
	GoTypeName string

	// Dir is a label used in log messages, e.g. "cluster-data-plane".
	Dir string

	// File is the output JSON filename; when empty it defaults to
	// <snake_case GoTypeName>_minimal_defi.json.
	File string

	// MetadataCategory selects the metadata overlay to use.
	MetadataCategory metadataCategory
}

type metadataCategory int

const (
	metaNamespacedUser metadataCategory = iota
	metaNamespacedWorkflow
	metaClusterDefault
	metaClusterProjectType
	metaClusterWorkflow
)

// kinds returns the full registry of kinds that the rule engine validates.
func kinds() []kindSpec {
	return []kindSpec{
		// Namespaced config kinds.
		{GoTypeName: "Project", Dir: "project"},
		{GoTypeName: "Component", Dir: "component"},
		{GoTypeName: "ComponentType", Dir: "component-type"},
		{GoTypeName: "ClusterComponentType", Dir: "cluster-component-type", MetadataCategory: metaClusterDefault},
		{GoTypeName: "Environment", Dir: "environment", File: "environment_minimal_def.json"},
		{GoTypeName: "DeploymentPipeline", Dir: "deployment-pipeline", File: "deploymentpipelines_minimal_defi.json"},
		{GoTypeName: "ProjectReleaseBinding", Dir: "project-release-binding"},
		{GoTypeName: "ReleaseBinding", Dir: "release_binding"},
		{GoTypeName: "Workflow", Dir: "workflow", MetadataCategory: metaNamespacedWorkflow},
		{
			GoTypeName: "ClusterWorkflow", Dir: "cluster-workflows",
			File: "clusterworkflows_minimal_defi.json", MetadataCategory: metaClusterWorkflow,
		},
		{GoTypeName: "WorkflowRun", Dir: "workflow-run"},
		{GoTypeName: "Workload", Dir: "workload"},
		{GoTypeName: "ClusterProjectType", Dir: "cluster-project-type", MetadataCategory: metaClusterProjectType},
		{GoTypeName: "ClusterResourceType", Dir: "cluster-resource-type", MetadataCategory: metaClusterDefault},
		{GoTypeName: "ResourceType", Dir: "resource-type"},
		{GoTypeName: "ClusterDataPlane", Dir: "cluster-data-plane", MetadataCategory: metaClusterDefault},
		{
			GoTypeName: "ClusterObservabilityPlane", Dir: "cluster-observability-planes",
			File: "clusterobservabilityplanes_minimal_defi.json", MetadataCategory: metaClusterDefault,
		},
		{GoTypeName: "Trait", Dir: "trait"},
		{
			GoTypeName: "ClusterTrait", Dir: "cluster-trait",
			File: "clustertrait_minimal_defi.json", MetadataCategory: metaClusterDefault,
		},

		// Namespaced controller-managed release artifacts.
		{GoTypeName: "AuthzRole", Dir: "authz-role"},
		{GoTypeName: "AuthzRoleBinding", Dir: "authz-role-binding"},
		{GoTypeName: "DataPlane", Dir: "data-plane"},
		{GoTypeName: "ObservabilityAlertRule", Dir: "observability-alert-rule"},
		{GoTypeName: "ObservabilityAlertsNotificationChannel", Dir: "observability-alerts-notification-channel"},
		{GoTypeName: "ObservabilityPlane", Dir: "observability-plane"},
		{GoTypeName: "ProjectType", Dir: "project-type"},
		{GoTypeName: "Resource", Dir: "resource"},
		{GoTypeName: "ResourceReleaseBinding", Dir: "resource-release-binding"},
		{GoTypeName: "SecretReference", Dir: "secret-reference"},
		{GoTypeName: "WorkflowPlane", Dir: "workflow-plane"},

		// Cluster-scoped kinds.
		{GoTypeName: "ComponentRelease", Dir: "component-release"},
		{GoTypeName: "ProjectRelease", Dir: "project-release"},
		{GoTypeName: "RenderedRelease", Dir: "rendered-release"},
		{GoTypeName: "ResourceRelease", Dir: "resource-release"},

		{GoTypeName: "ClusterAuthzRole", Dir: "cluster-authz-role", MetadataCategory: metaClusterDefault},
		{GoTypeName: "ClusterAuthzRoleBinding", Dir: "cluster-authz-role-binding", MetadataCategory: metaClusterDefault},
		{GoTypeName: "ClusterWorkflowPlane", Dir: "cluster-workflow-plane", MetadataCategory: metaClusterDefault},
	}
}

func (k kindSpec) defaultFile() string {
	var sb strings.Builder
	for i, r := range k.GoTypeName {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				sb.WriteByte('_')
			}
			sb.WriteRune(r + ('a' - 'A'))
		} else {
			sb.WriteRune(r)
		}
	}
	sb.WriteString("_minimal_defi.json")
	return sb.String()
}

func (k kindSpec) fileName() string {
	if k.File != "" {
		return k.File
	}
	return k.defaultFile()
}

func (k kindSpec) outputPath(templateRoot string) string {
	return filepath.Join(templateRoot, k.fileName())
}

func (k kindSpec) metadataBuilder() *Field {
	switch k.MetadataCategory {
	case metaNamespacedWorkflow:
		return metadataNamespacedWorkflow()
	case metaClusterDefault:
		return metadataClusterDefault()
	case metaClusterProjectType:
		return metadataClusterProjectType()
	case metaClusterWorkflow:
		return metadataClusterWorkflow()
	default:
		return metadataNamespacedUser()
	}
}
