package db

import (
	"fmt"
	"strings"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

var generatePassword = state.GeneratePassword

type DatabaseConfig struct {
	Type         string
	Image        string
	Env          map[string]string
	Volume       string
	HealthCheck  map[string]interface{}
	ConnTemplate string
}

var databaseDefs = map[string]DatabaseConfig{
	"mysql": {
		Type:  "mysql",
		Image: "mysql:8",
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": "", // generated
			"MYSQL_DATABASE":      "", // app name
		},
		Volume: "/var/lib/mysql",
		HealthCheck: map[string]interface{}{
			"test":     []string{"CMD", "mysqladmin", "ping", "-h", "localhost"},
			"interval": "10s",
			"timeout":  "5s",
			"retries":  5,
		},
		ConnTemplate: "mysql://root:%s@qd-%s-mysql:3306/%s",
	},
	"postgresql": {
		Type:  "postgresql",
		Image: "postgres:16",
		Env: map[string]string{
			"POSTGRES_PASSWORD": "", // generated
			"POSTGRES_DB":       "", // app name
		},
		Volume: "/var/lib/postgresql/data",
		HealthCheck: map[string]interface{}{
			"test":     []string{"CMD-SHELL", "pg_isready -U postgres"},
			"interval": "10s",
			"timeout":  "5s",
			"retries":  5,
		},
		ConnTemplate: "postgresql://postgres:%s@qd-%s-postgresql:5432/%s",
	},
	"mariadb": {
		Type:  "mariadb",
		Image: "mariadb:11",
		Env: map[string]string{
			"MARIADB_ROOT_PASSWORD": "",
			"MARIADB_DATABASE":      "",
		},
		Volume: "/var/lib/mysql",
		HealthCheck: map[string]interface{}{
			"test":     []string{"CMD", "healthcheck.sh", "--connect"},
			"interval": "10s",
			"timeout":  "5s",
			"retries":  5,
		},
		ConnTemplate: "mysql://root:%s@qd-%s-mariadb:3306/%s",
	},
	"mongodb": {
		Type:  "mongodb",
		Image: "mongo:7",
		Env: map[string]string{
			"MONGO_INITDB_ROOT_PASSWORD": "",
		},
		Volume: "/data/db",
		HealthCheck: map[string]interface{}{
			"test":     []string{"CMD", "mongosh", "--eval", "db.adminCommand('ping')"},
			"interval": "10s",
			"timeout":  "5s",
			"retries":  5,
		},
		ConnTemplate: "mongodb://root:%s@qd-%s-mongodb:27017/%s",
	},
	"redis": {
		Type:         "redis",
		Image:        "redis:7-alpine",
		Env:          map[string]string{},
		Volume:       "/data",
		HealthCheck:  nil,
		ConnTemplate: "redis://qd-%s-redis:6379",
	},
}

func GetDatabaseConfig(dbType string) (*DatabaseConfig, bool) {
	cfg, ok := databaseDefs[dbType]
	if !ok {
		return nil, false
	}
	return &cfg, true
}

func ProvisionDatabases(appName string, selectedDBs []string) (map[string]string, []string, map[string]string, error) {
	envVars := make(map[string]string)
	volumes := []string{}
	credentials := make(map[string]string)

	for _, dbType := range selectedDBs {
		dbType = strings.TrimSpace(dbType)
		cfg, ok := databaseDefs[dbType]
		if !ok {
			return nil, nil, nil, fmt.Errorf("unsupported database type: %s", dbType)
		}

		password := ""
		if dbType != "redis" {
			var err error
			password, err = generatePassword(20)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to generate password for %s: %w", dbType, err)
			}
		}

		credentials[dbType] = password

		volumeName := fmt.Sprintf("qd-%s-%s-data", appName, dbType)
		volumes = append(volumes, volumeName)

		if dbType == "redis" {
			envVars["REDIS_URL"] = fmt.Sprintf("redis://qd-%s-redis:6379", appName)
		} else {
			connStr := fmt.Sprintf(cfg.ConnTemplate, password, appName, appName)
			// Set per-type URL key (e.g., MYSQL_URL, POSTGRESQL_URL)
			specificKey := strings.ToUpper(dbType) + "_URL"
			if dbType == "mongodb" {
				specificKey = "MONGODB_URI"
			}
			envVars[specificKey] = connStr
			// Set DATABASE_URL for the first SQL-type database only
			if _, exists := envVars["DATABASE_URL"]; !exists {
				envVars["DATABASE_URL"] = connStr
			}
		}
	}

	return envVars, volumes, credentials, nil
}

func AvailableDatabases() []string {
	return []string{"mysql", "postgresql", "mariadb", "mongodb", "redis"}
}
