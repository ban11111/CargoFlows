package database

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrationOwnershipBelongsToOneShotCommand(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file")
	}
	apiRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	repoRoot := filepath.Dir(apiRoot)

	for _, command := range []string{"server", "worker"} {
		body, err := os.ReadFile(filepath.Join(apiRoot, "cmd", command, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "database.Migrate(") {
			t.Fatalf("cmd/%s must not own schema migration", command)
		}
	}

	migrator, err := os.ReadFile(filepath.Join(apiRoot, "cmd", "migrate", "main.go"))
	if err != nil {
		t.Fatalf("read one-shot migrator: %v", err)
	}
	if !strings.Contains(string(migrator), "database.Migrate(") {
		t.Fatal("one-shot migrator does not call database.Migrate")
	}

	compose, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(compose)
	if !strings.Contains(contents, "\n  migrate:") || strings.Count(contents, "condition: service_completed_successfully") < 2 {
		t.Fatal("Compose must gate both API and worker on the one-shot migrator")
	}
}
