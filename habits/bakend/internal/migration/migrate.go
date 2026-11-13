package migration

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
)

//go:embed postgres/*.sql
var embeddedMigrations embed.FS

type Migration struct {
	Version string
	UpSQL   string
	DownSQL string
}

func loadEmbeddedMigrations() ([]Migration, error) {
	entries, err := embeddedMigrations.ReadDir("postgres")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded migrations: %v", err)
	}

	migs := map[string]*Migration{}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}

		parts := strings.Split(name, ".")
		if len(parts) != 3 {
			continue
		}

		version := parts[0]
		direction := parts[1]

		content, err := embeddedMigrations.ReadFile("postgres/" + name)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %v", name, err)
		}

		mig, ok := migs[version]
		if !ok {
			mig = &Migration{Version: version}
			migs[version] = mig
		}
		if direction == "up" {
			mig.UpSQL = string(content)
		} else if direction == "down" {
			mig.DownSQL = string(content)
		}
	}

	var migrations []Migration
	for _, m := range migs {
		migrations = append(migrations, *m)
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

func ApplyMigrations(db *sql.DB) error {
	// Создаём таблицу для трекинга версий
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY);`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations: %v", err)
	}

	migs, err := loadEmbeddedMigrations()
	if err != nil {
		return fmt.Errorf("failed to loadEmbeddedMigrations: %v", err)
	}

	for _, m := range migs {
		var exists string
		err := db.QueryRow("SELECT version FROM schema_migrations WHERE version = $1", m.Version).Scan(&exists)
		if err == nil {
			continue // уже применена
		}

		log.Printf("⬆️ Applying migration %s...", m.Version)
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.UpSQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s failed: %v", m.Version, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", m.Version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		log.Printf("✅ Migration %s applied", m.Version)
	}

	return nil
}

func RollbackLast(db *sql.DB) error {
	migs, err := loadEmbeddedMigrations()
	if err != nil {
		return err
	}

	var lastVersion string
	err = db.QueryRow("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&lastVersion)
	if err == sql.ErrNoRows {
		log.Println("No migrations to rollback")
		return nil
	} else if err != nil {
		return err
	}

	for _, m := range migs {
		if m.Version == lastVersion {
			log.Printf("⬇️ Rolling back migration %s...", lastVersion)
			tx, err := db.Begin()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(m.DownSQL); err != nil {
				tx.Rollback()
				return err
			}
			if _, err := tx.Exec("DELETE FROM schema_migrations WHERE version = $1", lastVersion); err != nil {
				tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			log.Printf("✅ Rolled back %s", lastVersion)
			return nil
		}
	}
	return nil
}
