// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"strings"
	"testing"

	"github.com/openchoreo/openchoreo/tools/lint/templatever"
)

// TestLoadError verifies the registry loads cleanly and reports no error.
func TestLoadError(t *testing.T) {
	if err := LoadError(); err != nil {
		t.Fatalf("expected no load error, got: %v", err)
	}
}

// TestVersionFileParsed verifies LatestVersion and AvailableVersions are
// populated from template-version.json and stay consistent.
func TestVersionFileParsed(t *testing.T) {
	versions := AvailableVersions()
	if len(versions) == 0 {
		t.Fatal("expected at least one template version")
	}
	if LatestVersion == "" {
		t.Fatal("expected LatestVersion to be populated")
	}
	contains := func(v string) bool {
		for _, x := range versions {
			if x == v {
				return true
			}
		}
		return false
	}
	if !contains(LatestVersion) {
		t.Errorf("LatestVersion %q not listed in AvailableVersions %v", LatestVersion, versions)
	}
	for i := 1; i < len(versions); i++ {
		if templatever.Compare(versions[i-1], versions[i]) >= 0 {
			t.Errorf("versions not ascending at %d: %v", i, versions)
		}
	}
	// LatestVersion must be the greatest available version.
	if templatever.Compare(versions[len(versions)-1], LatestVersion) != 0 {
		t.Errorf("expected %q to be the greatest version, got list %v", LatestVersion, versions)
	}
}

// TestAllKindsPopulated verifies AllKinds triggers the lazy load and returns
// every registered kind name.
func TestAllKindsPopulated(t *testing.T) {
	kinds := AllKinds()
	if len(kinds) == 0 {
		t.Fatal("expected AllKinds to return at least one kind")
	}
	seen := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate kind %q", k)
		}
		seen[k] = true
		if SchemaFor(k) == nil {
			t.Errorf("SchemaFor(%q) returned nil", k)
		}
	}
}

// TestSetVersionUnknown verifies an unknown version is rejected with an error.
func TestSetVersionUnknown(t *testing.T) {
	if err := SetVersion("v9.9.9"); err == nil {
		t.Fatal("expected error for unknown version")
	}
	if err := SetVersion("garbage"); err == nil {
		t.Fatal("expected error for a non-version string")
	}
	if err := SetVersion("latest"); err != nil {
		t.Fatalf("SetVersion(latest): %v", err)
	}
}

// TestSetVersionSwitchRoundTrip verifies versions can be selected and the
// default is the latest template version.
func TestSetVersionSwitchRoundTrip(t *testing.T) {
	versions := AvailableVersions()
	if len(versions) < 2 {
		t.Skip("need at least 2 template versions")
	}
	prev := CurrentVersion()
	defer func() { _ = SetVersion(prev) }()

	if CurrentVersion() != LatestVersion {
		t.Fatalf("expected default %s, got %s", LatestVersion, CurrentVersion())
	}
	oldest := versions[0]
	if err := SetVersion(oldest); err != nil {
		t.Fatalf("SetVersion(%s): %v", oldest, err)
	}
	if CurrentVersion() != oldest {
		t.Fatalf("expected %s, got %s", oldest, CurrentVersion())
	}
	if err := SetVersion("latest"); err != nil {
		t.Fatalf("SetVersion(latest): %v", err)
	}
	if CurrentVersion() != LatestVersion {
		t.Fatalf("expected %s after latest, got %s", LatestVersion, CurrentVersion())
	}
}

// TestAbbreviatedErrorList verifies the unknown-version error lists every
// available version so users know what to pick.
func TestAbbreviatedErrorList(t *testing.T) {
	err := SetVersion("v0.0.1")
	if err == nil {
		t.Fatal("expected error for unknown version")
	}
	msg := err.Error()
	for _, v := range AvailableVersions() {
		if !strings.Contains(msg, v) {
			t.Errorf("error %q missing available version %q", msg, v)
		}
	}
}

// TestSchemaDiffersAcrossVersions verifies the same kind resolves to different
// schemas across template versions: ReleaseBinding gained the deliverable
// status.delivery field between the oldest and the latest shipped version.
func TestSchemaDiffersAcrossVersions(t *testing.T) {
	versions := AvailableVersions()
	if len(versions) < 2 {
		t.Skip("need at least 2 template versions")
	}
	prev := CurrentVersion()
	defer func() { _ = SetVersion(prev) }()

	oldest, newest := versions[0], LatestVersion
	if err := SetVersion(oldest); err != nil {
		t.Fatalf("SetVersion(%s): %v", oldest, err)
	}
	oldSchema := SchemaFor("ReleaseBinding")
	if oldSchema == nil {
		t.Fatalf("SchemaFor(ReleaseBinding) nil in %s", oldest)
	}
	if err := SetVersion(newest); err != nil {
		t.Fatalf("SetVersion(%s): %v", newest, err)
	}
	newSchema := SchemaFor("ReleaseBinding")
	if newSchema == nil {
		t.Fatalf("SchemaFor(ReleaseBinding) nil in %s", newest)
	}
	_, haveDelivery := newSchema.Properties["status"].Properties["delivery"]
	if !haveDelivery {
		t.Fatalf("expected %s ReleaseBinding status to include delivery", newest)
	}
	statusOld, ok := oldSchema.Properties["status"]
	if !ok {
		t.Fatalf("expected %s ReleaseBinding status property", oldest)
	}
	_, hadDelivery := statusOld.Properties["delivery"]
	if hadDelivery {
		t.Fatalf("expected %s ReleaseBinding status to not include delivery", oldest)
	}
}
