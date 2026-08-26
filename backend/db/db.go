package db

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init() error {
	path := os.Getenv("EKA_DB_PATH")
	if path == "" {
		// shared with the Next.js frontend
		path = filepath.Join("..", "data", "patients.sqlite")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var err error
	DB, err = sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	if _, err = DB.Exec(`CREATE TABLE IF NOT EXISTS care_context_links (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		care_context_id TEXT UNIQUE NOT NULL,
		abha_address    TEXT NOT NULL,
		hi_type         TEXT NOT NULL,
		display         TEXT NOT NULL,
		status          TEXT NOT NULL DEFAULT 'PENDING',
		linked_at       TEXT,
		error           TEXT,
		created_at      TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return err
	}
	// ponytail: content stored as BLOB in SQLite — fine for docs <10 MB; move to object storage if larger
	if _, err = DB.Exec(`CREATE TABLE IF NOT EXISTS documents (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		care_context_id TEXT UNIQUE NOT NULL,
		abha_address    TEXT NOT NULL,
		oid             TEXT NOT NULL,
		file_name       TEXT NOT NULL,
		mime_type       TEXT NOT NULL,
		content         BLOB NOT NULL,
		created_at      TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return err
	}
	if _, err = DB.Exec(`CREATE TABLE IF NOT EXISTS consents (
		consent_init_id TEXT PRIMARY KEY,
		consent_id      TEXT UNIQUE,
		patient_abha    TEXT NOT NULL,
		oid             TEXT NOT NULL,
		purpose         TEXT NOT NULL,
		hi_types        TEXT NOT NULL,
		date_from       TEXT NOT NULL,
		date_to         TEXT NOT NULL,
		expiry          TEXT NOT NULL,
		status          TEXT NOT NULL DEFAULT 'REQUESTED',
		created_at      TEXT NOT NULL DEFAULT (datetime('now')),
		granted_at      TEXT
	)`); err != nil {
		return err
	}
	_, err = DB.Exec(`CREATE TABLE IF NOT EXISTS consent_records (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		consent_id      TEXT NOT NULL,
		care_context_id TEXT NOT NULL UNIQUE,
		fhir_json       TEXT NOT NULL,
		received_at     TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	return err
}
