package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Migration struct {
	Version     string    `json:"version"`
	Description string    `json:"description"`
	SQL         string    `json:"sql"`
	CreatedAt   time.Time `json:"created_at"`
}

type VerifyRequest struct {
	Version string   `json:"version"`
}

type VerifyResponse struct {
	Status             string      `json:"status"`
	CurrentVersion     string      `json:"currentVersion"`
	ClientVersion      string      `json:"clientVersion,omitempty"`
	RequiredMigrations []Migration `json:"requiredMigrations,omitempty"`
}

type VersionResponse struct {
	CurrentVersion string `json:"currentVersion"`
}

type MigrationsResponse struct {
	RequiredMigrations []Migration `json:"requiredMigrations"`
}

type SchemaResponse struct {
	CurrentVersion string      `json:"currentVersion"`
	Migrations     []Migration `json:"migrations"`
}

type MigrationResponse struct {
	Status         string `json:"status"`
	Version        string `json:"version"`
	CurrentVersion string `json:"currentVersion"`
}

type SchemaRegistry struct {
	db *sql.DB
}

func NewSchemaRegistry(dataPath string) (*SchemaRegistry, error) {
	os.MkdirAll(dataPath, 0755)

	dbPath := filepath.Join(dataPath, "registry.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			version TEXT PRIMARY KEY,
			description TEXT NOT NULL,
			sql_script TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create migrations table: %w", err)
	}

	return &SchemaRegistry{db: db}, nil
}

func (sr *SchemaRegistry) Close() error {
	return sr.db.Close()
}

func (sr *SchemaRegistry) GetCurrentVersion() (string, error) {
	var version string
	err := sr.db.QueryRow(`
		SELECT version FROM migrations
		ORDER BY version DESC LIMIT 1
	`).Scan(&version)

	if err == sql.ErrNoRows {
		return "0.0.0", nil // default
	}

	if err != nil {
		return "", fmt.Errorf("failed to get current version: %w", err)
	}

	return version, nil
}

func (sr *SchemaRegistry) GetAllMigrations() ([]Migration, error) {
	rows, err := sr.db.Query(`
		SELECT version, description, sql_script, created_at
		FROM migrations
		ORDER BY version ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query migrations: %w", err)
	}
	defer rows.Close()

	var migrations []Migration
	for rows.Next() {
		var m Migration
		if err := rows.Scan(&m.Version, &m.Description, &m.SQL, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan migration: %w", err)
		}
		migrations = append(migrations, m)
	}

	return migrations, nil
}

func (sr *SchemaRegistry) GetMigrations(fromVersion, toVersion string) ([]Migration, error) {
	rows, err := sr.db.Query(`
		SELECT version, description, sql_script, created_at
		FROM migrations
		WHERE version > ? AND version <= ?
		ORDER BY version ASC
	`, fromVersion, toVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to query migrations: %w", err)
	}
	defer rows.Close()

	var migrations []Migration
	for rows.Next() {
		var m Migration
		if err := rows.Scan(&m.Version, &m.Description, &m.SQL, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan migration: %w", err)
		}
		migrations = append(migrations, m)
	}

	return migrations, nil
}

func (sr *SchemaRegistry) RegisterMigration(m Migration) error {
	var count int
	err := sr.db.QueryRow("SELECT COUNT(*) FROM migrations WHERE version = ?", m.Version).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check existing version: %w", err)
	}

	if count > 0 {
		return fmt.Errorf("version %s already exists", m.Version)
	}

	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}

	_, err = sr.db.Exec(
		"INSERT INTO migrations (version, description, sql_script, created_at) VALUES (?, ?, ?, ?)",
		m.Version, m.Description, m.SQL, m.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert migration: %w", err)
	}

	return nil
}

func (sr *SchemaRegistry) handleRequests() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/schema", sr.handleSchema)
	mux.HandleFunc("/version", sr.handleVersion)
	mux.HandleFunc("/migrations", sr.handleMigrations)
	mux.HandleFunc("/verify", sr.handleVerify)

	return mux
}

func (sr *SchemaRegistry) handleSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	version, err := sr.GetCurrentVersion()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	migrations, err := sr.GetAllMigrations()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := SchemaResponse{
		CurrentVersion: version,
		Migrations:     migrations,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleVersion handles the /version endpoint (GET)
func (sr *SchemaRegistry) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	version, err := sr.GetCurrentVersion()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := VersionResponse{CurrentVersion: version}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (sr *SchemaRegistry) handleMigrations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sr.handleGetMigrations(w, r)
	case http.MethodPost:
		sr.handleRegisterMigration(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (sr *SchemaRegistry) handleGetMigrations(w http.ResponseWriter, r *http.Request) {
	fromVersion := r.URL.Query().Get("from")

	if fromVersion == "" {
		http.Error(w, "'from' query parameter is required", http.StatusBadRequest)
		return
	}

	toVersion, err := sr.GetCurrentVersion()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	migrations, err := sr.GetMigrations(fromVersion, toVersion)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := MigrationsResponse{RequiredMigrations: migrations}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (sr *SchemaRegistry) handleRegisterMigration(w http.ResponseWriter, r *http.Request) {
	var migration Migration
	if err := json.NewDecoder(r.Body).Decode(&migration); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if migration.SQL == "" {
		http.Error(w, "SQL is a required field", http.StatusBadRequest)
		return
	}

	if migration.Version == "" {
		currentVersion, err := sr.GetCurrentVersion()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var major, minor, patch int
		_, err = fmt.Sscanf(currentVersion, "%d.%d.%d", &major, &minor, &patch)
		if err != nil {

			patch++
		} else {

			patch++
		}

		migration.Version = fmt.Sprintf("%d.%d.%d", major, minor, patch)
	}

	if err := sr.RegisterMigration(migration); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	currentVersion, _ := sr.GetCurrentVersion()

	response := MigrationResponse{
		Status:         "migration-registered",
		Version:        migration.Version,
		CurrentVersion: currentVersion,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (sr *SchemaRegistry) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Version == "" {
		http.Error(w, "Version is a required field", http.StatusBadRequest)
		return
	}

	currentVersion, err := sr.GetCurrentVersion()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Version == currentVersion {
		response := VerifyResponse{
			Status:         "up-to-date",
			CurrentVersion: currentVersion,
			ClientVersion:  req.Version,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	migrations, err := sr.GetMigrations(req.Version, currentVersion)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := VerifyResponse{
		Status:             "update-required",
		CurrentVersion:     currentVersion,
		ClientVersion:      req.Version,
		RequiredMigrations: migrations,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	dataPath := getEnv("DATA_PATH", "./data")
	port := getEnv("PORT", "8080")

	registry, err := NewSchemaRegistry(dataPath)
	if err != nil {
		log.Fatalf("Failed to initialize schema registry: %v", err)
	}
	defer registry.Close()

	log.Printf("Schema registry server starting on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, registry.handleRequests()))
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
