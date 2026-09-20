package buildcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCIWorkflowParsesAsYAML(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", ".github", "workflows", "ci.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var workflow yaml.Node
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func TestCIReleaseRequiresEveryQualityAndSecurityJob(t *testing.T) {
	data, workflow := readCIWorkflow(t)
	_ = data
	jobs := mappingValue(t, workflow.Content[0], "jobs")
	release := mappingValue(t, jobs, "release")
	needs := mappingValue(t, release, "needs")
	actual := make(map[string]bool)
	switch needs.Kind {
	case yaml.ScalarNode:
		actual[needs.Value] = true
	case yaml.SequenceNode:
		for _, item := range needs.Content {
			actual[item.Value] = true
		}
	default:
		t.Fatalf("release.needs has YAML kind %d", needs.Kind)
	}
	for _, required := range []string{
		"lint-source-code", "build", "collaboration-resilience", "hosted-collaboration-gates",
	} {
		if !actual[required] {
			t.Errorf("release.needs does not contain %q", required)
		}
	}
}

func TestCIExternalActionsUseReviewedImmutableRevisions(t *testing.T) {
	data, _ := readCIWorkflow(t)
	usesPattern := regexp.MustCompile(`(?m)^\s*uses:\s*([^\s#]+)(?:\s+#\s*(.+))?\s*$`)
	immutablePattern := regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	matches := usesPattern.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("CI workflow contains no actions")
	}
	for _, match := range matches {
		action := match[1]
		if strings.HasPrefix(action, "./") {
			continue
		}
		if !immutablePattern.MatchString(action) {
			t.Errorf("action %q is not pinned to a 40-character commit SHA", action)
		}
		if strings.TrimSpace(match[2]) == "" {
			t.Errorf("action %q does not retain its reviewed release tag in a comment", action)
		}
	}
}

func readCIWorkflow(t *testing.T) ([]byte, *yaml.Node) {
	t.Helper()
	path := filepath.Join("..", "..", "..", ".github", "workflows", "ci.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var workflow yaml.Node
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return data, &workflow
}

func mappingValue(t *testing.T, mapping *yaml.Node, key string) *yaml.Node {
	t.Helper()
	if mapping.Kind != yaml.MappingNode {
		t.Fatalf("%q parent has YAML kind %d", key, mapping.Kind)
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	t.Fatalf("YAML mapping does not contain %q", key)
	return nil
}
