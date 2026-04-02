package db

import (
	"fmt"
	"strings"
	"testing"
)

func TestGetDatabaseConfig(t *testing.T) {
	tests := []struct {
		dbType string
		found  bool
		image  string
	}{
		{"mysql", true, "mysql:8"},
		{"postgresql", true, "postgres:16"},
		{"mariadb", true, "mariadb:11"},
		{"mongodb", true, "mongo:7"},
		{"redis", true, "redis:7-alpine"},
		{"unknown", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.dbType, func(t *testing.T) {
			cfg, ok := GetDatabaseConfig(tt.dbType)
			if ok != tt.found {
				t.Errorf("found = %v, want %v", ok, tt.found)
			}
			if ok {
				if cfg.Image != tt.image {
					t.Errorf("Image = %q, want %q", cfg.Image, tt.image)
				}
			}
		})
	}
}

func TestProvisionDatabases_MySQL(t *testing.T) {
	envVars, _, creds, err := ProvisionDatabases("testapp", []string{"mysql"})
	if err != nil {
		t.Fatalf("ProvisionDatabases failed: %v", err)
	}

	if _, ok := envVars["DATABASE_URL"]; !ok {
		t.Error("Should set DATABASE_URL")
	}
	if _, ok := creds["mysql"]; !ok {
		t.Error("Should have mysql credentials")
	}
	if creds["mysql"] == "" {
		t.Error("Password should not be empty")
	}
}

func TestProvisionDatabases_Redis(t *testing.T) {
	envVars, _, creds, err := ProvisionDatabases("testapp", []string{"redis"})
	if err != nil {
		t.Fatalf("ProvisionDatabases failed: %v", err)
	}

	if _, ok := envVars["REDIS_URL"]; !ok {
		t.Error("Should set REDIS_URL")
	}
	if !strings.Contains(envVars["REDIS_URL"], "qd-testapp-redis") {
		t.Errorf("REDIS_URL = %q, should contain 'qd-testapp-redis'", envVars["REDIS_URL"])
	}
	// Redis doesn't need a password
	if creds["redis"] != "" {
		t.Error("Redis password should be empty")
	}
}

func TestProvisionDatabases_Multiple(t *testing.T) {
	envVars, volumes, _, err := ProvisionDatabases("myapp", []string{"mysql", "redis"})
	if err != nil {
		t.Fatalf("ProvisionDatabases failed: %v", err)
	}

	if _, ok := envVars["DATABASE_URL"]; !ok {
		t.Error("Should have DATABASE_URL")
	}
	if _, ok := envVars["REDIS_URL"]; !ok {
		t.Error("Should have REDIS_URL")
	}
	if len(volumes) != 2 {
		t.Errorf("Volumes = %d, want 2", len(volumes))
	}
}

func TestProvisionDatabases_Empty(t *testing.T) {
	envVars, volumes, _, err := ProvisionDatabases("app", []string{})
	if err != nil {
		t.Fatalf("ProvisionDatabases failed: %v", err)
	}
	if len(envVars) != 0 {
		t.Error("Should have no env vars for empty DB list")
	}
	if len(volumes) != 0 {
		t.Error("Should have no volumes for empty DB list")
	}
}

func TestProvisionDatabases_Unsupported(t *testing.T) {
	_, _, _, err := ProvisionDatabases("app", []string{"couchdb"})
	if err == nil {
		t.Error("Should fail for unsupported DB type")
	}
}

func TestProvisionDatabases_GeneratePasswordError(t *testing.T) {
	old := generatePassword
	generatePassword = func(length int) (string, error) {
		return "", fmt.Errorf("password error")
	}
	defer func() { generatePassword = old }()

	_, _, _, err := ProvisionDatabases("app", []string{"mysql"})
	if err == nil {
		t.Error("Should fail when GeneratePassword fails")
	}
}

func TestAvailableDatabases(t *testing.T) {
	dbs := AvailableDatabases()
	expected := []string{"mysql", "postgresql", "mariadb", "mongodb", "redis"}
	if len(dbs) != len(expected) {
		t.Errorf("AvailableDatabases count = %d, want %d", len(dbs), len(expected))
	}
	for _, db := range expected {
		if !containsStr(dbs, db) {
			t.Errorf("Missing %s from available databases", db)
		}
	}
}

func containsStr(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func TestProvisionDatabases_PostgreSQL(t *testing.T) {
	envVars, volumes, creds, err := ProvisionDatabases("pgapp", []string{"postgresql"})
	if err != nil {
		t.Fatalf("ProvisionDatabases failed: %v", err)
	}
	if _, ok := envVars["DATABASE_URL"]; !ok {
		t.Error("Should set DATABASE_URL for PostgreSQL")
	}
	if !strings.Contains(envVars["DATABASE_URL"], "qd-pgapp-postgresql") {
		t.Errorf("DATABASE_URL should contain container name, got %q", envVars["DATABASE_URL"])
	}
	if _, ok := creds["postgresql"]; !ok {
		t.Error("Should have postgresql credentials")
	}
	if len(volumes) != 1 {
		t.Errorf("Volumes count = %d, want 1", len(volumes))
	}
}

func TestProvisionDatabases_MongoDB(t *testing.T) {
	envVars, _, creds, err := ProvisionDatabases("mongoapp", []string{"mongodb"})
	if err != nil {
		t.Fatalf("ProvisionDatabases failed: %v", err)
	}
	if _, ok := envVars["MONGODB_URI"]; !ok {
		t.Error("Should set MONGODB_URI for MongoDB")
	}
	if _, ok := creds["mongodb"]; !ok {
		t.Error("Should have mongodb credentials")
	}
}

func TestProvisionDatabases_MariaDB(t *testing.T) {
	envVars, _, creds, err := ProvisionDatabases("mariaapp", []string{"mariadb"})
	if err != nil {
		t.Fatalf("ProvisionDatabases failed: %v", err)
	}
	if _, ok := envVars["DATABASE_URL"]; !ok {
		t.Error("Should set DATABASE_URL for MariaDB")
	}
	if _, ok := creds["mariadb"]; !ok {
		t.Error("Should have mariadb credentials")
	}
}

func TestProvisionDatabases_AllTypes(t *testing.T) {
	allDBs := []string{"mysql", "postgresql", "mariadb", "mongodb", "redis"}
	envVars, volumes, _, err := ProvisionDatabases("fullapp", allDBs)
	if err != nil {
		t.Fatalf("ProvisionDatabases failed: %v", err)
	}
	if len(volumes) != 5 {
		t.Errorf("Volumes count = %d, want 5", len(volumes))
	}
	// Should have at least DATABASE_URL and REDIS_URL and MONGODB_URI
	expectedVars := []string{"DATABASE_URL", "REDIS_URL", "MONGODB_URI", "MYSQL_URL", "POSTGRESQL_URL", "MARIADB_URL"}
	for _, key := range expectedVars {
		if _, ok := envVars[key]; !ok {
			t.Errorf("Missing env var %q", key)
		}
	}
}

func TestProvisionDatabases_MultipleSQL_FirstGetsDatabaseURL(t *testing.T) {
	envVars, _, _, err := ProvisionDatabases("app", []string{"mysql", "postgresql", "mariadb"})
	if err != nil {
		t.Fatalf("ProvisionDatabases failed: %v", err)
	}
	// DATABASE_URL should be the mysql one (first SQL DB)
	if !strings.Contains(envVars["DATABASE_URL"], "qd-app-mysql") {
		t.Errorf("DATABASE_URL should contain mysql host, got %q", envVars["DATABASE_URL"])
	}
	// All three should have their own specific keys
	for _, key := range []string{"MYSQL_URL", "POSTGRESQL_URL", "MARIADB_URL"} {
		if _, ok := envVars[key]; !ok {
			t.Errorf("Missing env var %q", key)
		}
	}
	if !strings.Contains(envVars["MYSQL_URL"], "qd-app-mysql") {
		t.Errorf("MYSQL_URL should point to mysql host, got %q", envVars["MYSQL_URL"])
	}
	if !strings.Contains(envVars["POSTGRESQL_URL"], "qd-app-postgresql") {
		t.Errorf("POSTGRESQL_URL should point to postgresql host, got %q", envVars["POSTGRESQL_URL"])
	}
	if !strings.Contains(envVars["MARIADB_URL"], "qd-app-mariadb") {
		t.Errorf("MARIADB_URL should point to mariadb host, got %q", envVars["MARIADB_URL"])
	}
}
