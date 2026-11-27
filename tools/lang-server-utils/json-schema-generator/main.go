// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

// Command json-schema-generator regenerates the minimal JSON schema files used
// by the rule engine from the OpenChoreo API Go types. It reads
// json-schema-generator.config.json beside the binary, or the --config path.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/openchoreo/openchoreo/tools/lint/templatever"
)

// changelogVersionRE matches a release heading like "## v1.2.0-rc.1".
var changelogVersionRE = regexp.MustCompile(`^##\s+v(\d+)\.(\d+)\.(\d+)`)

// explicitVersionRE accepts the version strings allowed in the config file.
var explicitVersionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// defaultRoot is the fallback OpenChoreo checkout when no sources are listed.
const defaultRoot = "."

// versionFileName is the manifest the generator updates and the rule engine reads.
const versionFileName = "template-version.json"

// job is one generation target resolved from the config file.
type job struct {
	root          string
	apiDir        string
	version       string
	fromChangelog bool
}

func main() {
	cfgFile := flag.String("config", defaultConfigPath(),
		"Path to the generator configuration; relative paths inside it use the working directory")
	versionOverride := flag.String("template-version", "", "Explicit vX.Y.Z template version")
	noManifest := flag.Bool("no-manifest", false, "Leave the shared manifest to the release coordinator")
	strict := flag.Bool("strict", false, "Fail if any registered kind cannot be generated")
	listFiles := flag.Bool("list-files", false, "Print expected schema filenames as JSON")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr,
			"json-schema-generator takes no arguments; edit the config file to change paths. Unknown: %v\n", flag.Args())
		os.Exit(2)
	}

	if *listFiles {
		files := make([]string, 0, len(kinds()))
		for _, k := range kinds() {
			files = append(files, k.fileName())
		}
		if err := json.NewEncoder(os.Stdout).Encode(files); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *versionOverride != "" && !explicitVersionRE.MatchString(*versionOverride) {
		fmt.Fprintln(os.Stderr, "invalid --template-version: want vX.Y.Z")
		os.Exit(2)
	}
	jobs, templatesDir, err := resolveJobs(*cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to prepare jobs: %v\n", err)
		os.Exit(1)
	}

	var written, skipped int
	var generated []string
	for _, j := range jobs {
		if *versionOverride != "" {
			j.version = *versionOverride
			j.fromChangelog = false
		}
		if err := resolveJobVersion(&j); err != nil {
			fmt.Fprintf(os.Stderr, "failed to resolve version for %s: %v\n", j.root, err)
			os.Exit(1)
		}
		if j.fromChangelog {
			fmt.Printf("OpenChoreo version from %s: %s\n", filepath.Join(j.root, "CHANGELOG.md"), j.version)
		} else {
			fmt.Printf("Using explicit version: %s\n", j.version)
		}
		targetDir := filepath.Join(templatesDir, versionDir(j.version))
		fmt.Printf("Writing schemas to: %s\n", targetDir)

		if _, err := os.Stat(j.apiDir); err != nil {
			fmt.Fprintf(os.Stderr, "cannot access api package %q: %v\n", j.apiDir, err)
			os.Exit(1)
		}

		pkg, files, typeErrs, err := parseAPI(j.apiDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse api package: %v\n", err)
			os.Exit(1)
		}
		for _, e := range typeErrs {
			fmt.Fprintf(os.Stderr, "warning: %v\n", e)
		}
		markers := collectMarkers(files)
		gen := newGenerator(pkg, markers)

		for _, ks := range kinds() {
			outPath := ks.outputPath(targetDir)

			rootSchema := gen.generateKind(ks)
			if rootSchema == nil {
				fmt.Fprintf(os.Stderr, "skip %s: Go type %q not found\n", ks.Dir, ks.GoTypeName)
				if *strict {
					os.Exit(1)
				}
				skipped++
				continue
			}

			if err := writeSchema(rootSchema, outPath); err != nil {
				fmt.Fprintf(os.Stderr, "skip %s: %v\n", ks.Dir, err)
				if *strict {
					os.Exit(1)
				}
				skipped++
				continue
			}
			fmt.Printf("wrote %s (%s)\n", outPath, ks.GoTypeName)
			written++
		}
		generated = append(generated, j.version)
	}

	fmt.Printf("\ndone: %d written, %d skipped\n", written, skipped)

	if *noManifest {
		return
	}
	latest, err := updateVersionFile(templatesDir, generated)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update %s: %v\n", filepath.Join(templatesDir, versionFileName), err)
		os.Exit(1)
	}
	fmt.Printf("updated %s (latest %s)\n", filepath.Join(templatesDir, versionFileName), latest)
}

// resolveJobs builds the generation jobs and the template directory from the
// config file; a config with no sources falls back to the default root.
func resolveJobs(configPath string) ([]job, string, error) {
	mkJob := func(root, apiOverride, versionOverride string) (job, error) {
		apiDir := apiOverride
		if apiDir == "" {
			apiDir = filepath.Join(root, "api", "v1alpha1")
		}
		if versionOverride != "" {
			if !explicitVersionRE.MatchString(versionOverride) {
				return job{}, fmt.Errorf("invalid version %q (want vX.Y.Z)", versionOverride)
			}
			return job{root: root, apiDir: apiDir, version: versionOverride}, nil
		}
		return job{root: root, apiDir: apiDir}, nil
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, "", err
	}
	templates := cfg.TemplatesDirOrDefault()

	if len(cfg.Sources) == 0 {
		j, err := mkJob(defaultRoot, "", "")
		if err != nil {
			return nil, "", err
		}
		return []job{j}, templates, nil
	}

	jobs := make([]job, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		j, err := mkJob(s.Root, s.API, s.Version)
		if err != nil {
			return nil, "", err
		}
		jobs = append(jobs, j)
	}
	return jobs, templates, nil
}

// resolveJobVersion fills the job's version from CHANGELOG.md unless it was
// pinned in the config file.
func resolveJobVersion(j *job) error {
	if j.version != "" {
		return nil
	}
	v, err := latestVersionFromChangelog(filepath.Join(j.root, "CHANGELOG.md"))
	if err != nil {
		return err
	}
	j.version = v
	j.fromChangelog = true
	return nil
}

// versionDir returns the folder name for a version, e.g. "template-v1.3.0".
func versionDir(version string) string {
	return "template-" + version
}

// latestVersionFromChangelog returns the first "## vX.Y.Z" heading, with any
// pre-release suffix stripped (e.g. "v1.2.0-rc.1" -> "v1.2.0").
func latestVersionFromChangelog(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := changelogVersionRE.FindStringSubmatch(sc.Text()); m != nil {
			return fmt.Sprintf("v%s.%s.%s", m[1], m[2], m[3]), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no '## vX.Y.Z' heading found in %s", path)
}

// updateVersionFile writes the manifest listing every template-v* folder plus
// the versions generated in this run, sorted ascending, with latest pointing at
// the greatest version. Regenerating an older tag never demotes latest.
func updateVersionFile(templatesDir string, generated []string) (string, error) {
	manifestPath := filepath.Join(templatesDir, versionFileName)

	versions := make(map[string]bool, len(generated))
	for _, v := range generated {
		if explicitVersionRE.MatchString(v) {
			versions[v] = true
		}
	}
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "template-v") {
			continue
		}
		versions[strings.TrimPrefix(e.Name(), "template-")] = true
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no template versions found in %s", templatesDir)
	}

	names := make([]string, 0, len(versions))
	for v := range versions {
		names = append(names, v)
	}
	sort.Slice(names, func(i, j int) bool {
		return templatever.Compare(names[i], names[j]) < 0
	})
	latest := names[len(names)-1]

	data, err := templatever.Marshal(templatever.File{Latest: latest, Versions: names})
	if err != nil {
		return "", err
	}
	return latest, os.WriteFile(manifestPath, data, 0o600)
}

func writeSchema(root *Field, path string) error {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
