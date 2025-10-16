package unit

import (
	"os"
	"testing"

	"github.com/apimgr/gitignore/src/database"
)

func setupTestDB(t *testing.T) (*database.Database, func()) {
	dbPath := "/tmp/gitignore_test.db"

	// Remove existing test database
	os.Remove(dbPath)

	db, err := database.New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(dbPath)
	}

	return db, cleanup
}

func TestDatabase_New(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	if db == nil {
		t.Error("Database is nil")
	}
}

func TestDatabase_CreateAdminCredentials(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	username := "testadmin"
	password := "testpass123"
	token := "testtokenabc123"

	err := db.CreateAdminCredentials(username, password, token)
	if err != nil {
		t.Fatalf("Failed to create admin credentials: %v", err)
	}

	// Verify credentials exist
	exists, err := db.AdminCredentialsExist()
	if err != nil {
		t.Fatalf("Failed to check credentials: %v", err)
	}
	if !exists {
		t.Error("Credentials do not exist after creation")
	}
}

func TestDatabase_VerifyPassword(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	username := "testadmin"
	password := "testpass123"
	token := "testtokenabc123"

	err := db.CreateAdminCredentials(username, password, token)
	if err != nil {
		t.Fatalf("Failed to create credentials: %v", err)
	}

	tests := []struct {
		name     string
		username string
		password string
		want     bool
	}{
		{"Valid credentials", username, password, true},
		{"Invalid password", username, "wrongpass", false},
		{"Invalid username", "wronguser", password, false},
		{"Empty password", username, "", false},
		{"Empty username", "", password, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := db.VerifyPassword(tt.username, tt.password)
			if err != nil && tt.want {
				t.Errorf("Unexpected error: %v", err)
			}
			if valid != tt.want {
				t.Errorf("VerifyPassword() = %v, want %v", valid, tt.want)
			}
		})
	}
}

func TestDatabase_VerifyToken(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	username := "testadmin"
	password := "testpass123"
	token := "testtokenabc123"

	err := db.CreateAdminCredentials(username, password, token)
	if err != nil {
		t.Fatalf("Failed to create credentials: %v", err)
	}

	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{"Valid token", token, true},
		{"Invalid token", "wrongtoken", false},
		{"Empty token", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := db.VerifyToken(tt.token)
			if err != nil && tt.want {
				t.Errorf("Unexpected error: %v", err)
			}
			if valid != tt.want {
				t.Errorf("VerifyToken() = %v, want %v", valid, tt.want)
			}
		})
	}
}

func TestDatabase_Settings(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	key := "test.setting"
	value := "test value"

	// Set setting
	err := db.SetSetting(key, value)
	if err != nil {
		t.Fatalf("Failed to set setting: %v", err)
	}

	// Get setting
	retrieved, err := db.GetSetting(key)
	if err != nil {
		t.Fatalf("Failed to get setting: %v", err)
	}
	if retrieved != value {
		t.Errorf("GetSetting() = %v, want %v", retrieved, value)
	}

	// Update setting
	newValue := "updated value"
	err = db.SetSetting(key, newValue)
	if err != nil {
		t.Fatalf("Failed to update setting: %v", err)
	}

	retrieved, err = db.GetSetting(key)
	if err != nil {
		t.Fatalf("Failed to get updated setting: %v", err)
	}
	if retrieved != newValue {
		t.Errorf("GetSetting() after update = %v, want %v", retrieved, newValue)
	}
}

func TestDatabase_GetAllSettings(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Set multiple settings
	settings := map[string]string{
		"server.port":  "8080",
		"server.title": "Test Server",
		"log.level":    "debug",
	}

	for key, value := range settings {
		err := db.SetSetting(key, value)
		if err != nil {
			t.Fatalf("Failed to set setting %s: %v", key, err)
		}
	}

	// Get all settings
	all, err := db.GetAllSettings()
	if err != nil {
		t.Fatalf("Failed to get all settings: %v", err)
	}

	// Verify all settings exist
	for key, want := range settings {
		got, exists := all[key]
		if !exists {
			t.Errorf("Setting %s not found in GetAllSettings()", key)
			continue
		}
		if got != want {
			t.Errorf("GetAllSettings()[%s] = %v, want %v", key, got, want)
		}
	}
}

func TestDatabase_GetSettingNonExistent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.GetSetting("nonexistent.key")
	if err == nil {
		t.Error("Expected error for non-existent setting")
	}
}

func TestDatabase_AdminCredentialsExist(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Check before creation
	exists, err := db.AdminCredentialsExist()
	if err != nil {
		t.Fatalf("Failed to check credentials: %v", err)
	}
	if exists {
		t.Error("Credentials should not exist before creation")
	}

	// Create credentials
	err = db.CreateAdminCredentials("admin", "pass", "token")
	if err != nil {
		t.Fatalf("Failed to create credentials: %v", err)
	}

	// Check after creation
	exists, err = db.AdminCredentialsExist()
	if err != nil {
		t.Fatalf("Failed to check credentials: %v", err)
	}
	if !exists {
		t.Error("Credentials should exist after creation")
	}
}

func TestDatabase_Ping(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	err := db.GetDB().Ping()
	if err != nil {
		t.Errorf("Database ping failed: %v", err)
	}
}

func BenchmarkDatabase_SetSetting(b *testing.B) {
	db, err := database.New("sqlite", "/tmp/gitignore_bench.db")
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer func() {
		db.Close()
		os.Remove("/tmp/gitignore_bench.db")
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.SetSetting("test.key", "test value")
	}
}

func BenchmarkDatabase_GetSetting(b *testing.B) {
	db, err := database.New("sqlite", "/tmp/gitignore_bench.db")
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer func() {
		db.Close()
		os.Remove("/tmp/gitignore_bench.db")
	}()

	db.SetSetting("test.key", "test value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.GetSetting("test.key")
	}
}

func BenchmarkDatabase_VerifyPassword(b *testing.B) {
	db, err := database.New("sqlite", "/tmp/gitignore_bench.db")
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer func() {
		db.Close()
		os.Remove("/tmp/gitignore_bench.db")
	}()

	db.CreateAdminCredentials("admin", "password", "token")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.VerifyPassword("admin", "password")
	}
}
