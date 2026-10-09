// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package celvalidator

import (
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
)

// check compiles and type-checks expr against env, returning the first issue.
func check(t *testing.T, env *cel.Env, expr string) error {
	t.Helper()
	ast, iss := env.Parse(expr)
	if iss != nil && iss.Err() != nil {
		return iss.Err()
	}
	_, iss = env.Check(ast)
	if iss != nil && iss.Err() != nil {
		return iss.Err()
	}
	return nil
}

func TestNewComponentEnvCompilesReceiverMacro(t *testing.T) {
	env, _, err := NewComponentEnv(SchemaOptions{})
	if err != nil {
		t.Fatalf("NewComponentEnv: %v", err)
	}
	if err := check(t, env, `configurations.toConfigFileList()`); err != nil {
		t.Fatalf("toConfigFileList should type-check: %v", err)
	}
	if err := check(t, env, `workload.container.image`); err != nil {
		t.Fatalf("workload.container.image should type-check: %v", err)
	}
}

func TestNewResourceEnvApplied(t *testing.T) {
	env, err := NewResourceEnv(SchemaOptions{})
	if err != nil {
		t.Fatalf("NewResourceEnv: %v", err)
	}
	env, err = WithApplied(env)
	if err != nil {
		t.Fatalf("WithApplied: %v", err)
	}

	valid := `applied.deployment.status.readyReplicas == applied.deployment.status.replicas && applied.deployment.status.replicas > 0`
	if err := check(t, env, valid); err != nil {
		t.Fatalf("applied expression should type-check: %v", err)
	}

	// status is Dyn-typed, so field access beneath it must compile.
	if err := check(t, env, `applied.deployment.status.readyReplicas + 1`); err != nil {
		t.Fatalf("status is dyn and must accept arithmetic: %v", err)
	}
}

func TestNewResourceEnvRejectsUnknownField(t *testing.T) {
	env, err := NewResourceEnv(SchemaOptions{})
	if err != nil {
		t.Fatalf("NewResourceEnv: %v", err)
	}
	env, err = WithApplied(env)
	if err != nil {
		t.Fatalf("WithApplied: %v", err)
	}

	err = check(t, env, `applied.deployment.statu.readyReplicas`)
	if err == nil {
		t.Fatal("unknown field statu should fail type-check")
	}
	if !strings.Contains(err.Error(), "statu") {
		t.Fatalf("error should name the unknown field, got: %v", err)
	}
}

func TestCreateResourceBaseEnvOmitsComponentMacros(t *testing.T) {
	env, err := createResourceBaseEnv()
	if err != nil {
		t.Fatalf("createResourceBaseEnv: %v", err)
	}
	if err := check(t, env, `configurations.toConfigFileList()`); err == nil {
		t.Fatal("component receiver macro must NOT resolve without the context extensions")
	}
}
