package db

import (
	"strings"
	"testing"
)

// TestMongoDB_RequiresBothRootCredentials is the regression test for a MongoDB
// deploy that could never start. The mongo entrypoint aborts unless BOTH
// MONGO_INITDB_ROOT_USERNAME and MONGO_INITDB_ROOT_PASSWORD are provided; the
// config only declared the password, so the container exited on boot with
// "MONGO_INITDB_ROOT_USERNAME and MONGO_INITDB_ROOT_PASSWORD must both be
// provided" and the app's depends_on never satisfied.
func TestMongoDB_RequiresBothRootCredentials(t *testing.T) {
	cfg, ok := GetDatabaseConfig("mongodb")
	if !ok {
		t.Fatal("mongodb config should exist")
	}

	user, hasUser := cfg.Env["MONGO_INITDB_ROOT_USERNAME"]
	if !hasUser {
		t.Fatal("MONGO_INITDB_ROOT_USERNAME must be declared or the container refuses to start")
	}
	if user == "" {
		t.Error("MONGO_INITDB_ROOT_USERNAME must have a concrete value, not a generated-secret placeholder")
	}
	if user != MongoRootUser {
		t.Errorf("username = %q, want %q to match the connection string", user, MongoRootUser)
	}
	if _, hasPass := cfg.Env["MONGO_INITDB_ROOT_PASSWORD"]; !hasPass {
		t.Error("MONGO_INITDB_ROOT_PASSWORD must be declared")
	}
}

// TestMongoDB_ConnStringUsesAuthSource covers the second half of the same bug:
// the root user is created in the `admin` database, not in the per-app database
// named in the URI path, so authentication fails without authSource=admin.
func TestMongoDB_ConnStringUsesAuthSource(t *testing.T) {
	envVars, _, creds, err := ProvisionDatabases("mongoapp", []string{"mongodb"})
	if err != nil {
		t.Fatalf("ProvisionDatabases failed: %v", err)
	}

	uri := envVars["MONGODB_URI"]
	if uri == "" {
		t.Fatal("MONGODB_URI should be set")
	}
	if !strings.Contains(uri, "authSource=admin") {
		t.Errorf("MONGODB_URI must set authSource=admin, got %q", uri)
	}
	if !strings.HasPrefix(uri, "mongodb://"+MongoRootUser+":") {
		t.Errorf("MONGODB_URI should authenticate as %q, got %q", MongoRootUser, uri)
	}
	if creds["mongodb"] == "" {
		t.Error("mongodb should get a generated password")
	}
	if !strings.Contains(uri, creds["mongodb"]) {
		t.Error("MONGODB_URI should embed the generated password")
	}
}

// TestStaticEnvValuesAreConcrete documents the contract buildComposeData relies
// on: an empty value in a database's Env map marks a placeholder for a
// generated secret, and a non-empty one is a literal to pass through verbatim.
func TestStaticEnvValuesAreConcrete(t *testing.T) {
	for _, name := range AvailableDatabases() {
		cfg, ok := GetDatabaseConfig(name)
		if !ok {
			t.Fatalf("%s should have a config", name)
		}
		for key, val := range cfg.Env {
			isPlaceholder := strings.Contains(key, "PASSWORD") ||
				strings.Contains(key, "DATABASE") ||
				key == "POSTGRES_DB"
			if !isPlaceholder && val == "" {
				t.Errorf("%s: %s is neither a known generated secret nor a literal value", name, key)
			}
		}
	}
}
