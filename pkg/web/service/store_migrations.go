package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/chainreactors/cyber/aop"
	scanpb "github.com/chainreactors/cyber/pkg/web/scan"
	"github.com/uptrace/bun"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var legacySchemaColumns = map[string][]string{
	"chat_aop_events": {"id", "session_id", "event_id", "cursor", "turn_id", "emitter", "sequence", "event_json", "created_at"},
	"scans":           {"id", "target", "mode", "verify", "sniper", "status", "progress", "error", "scan_json", "created_at", "updated_at"},
}

// migrateSchema recognizes legacy storage by its columns. Validation and all
// conversions share one transaction so failed startup leaves the original
// schema and records intact. Current databases need no conversion.
func migrateSchema(ctx context.Context, orm *bun.DB, schema SchemaModule) error {
	return orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		tables, err := schemaTables(tx)
		if err != nil {
			return err
		}
		if !slices.Equal(tables, schema.tableNames()) {
			return validateSchema(tx, schema)
		}
		var pending []string
		for _, table := range tables {
			columns, err := schemaColumns(tx, table)
			if err != nil {
				return err
			}
			if slices.Equal(columns, schema.Tables[table]) {
				continue
			}
			if !slices.Equal(columns, legacySchemaColumns[table]) {
				return fmt.Errorf("unsupported sqlite schema: %s columns %v, want %v", table, columns, schema.Tables[table])
			}
			pending = append(pending, table)
		}
		for _, table := range pending {
			var err error
			switch table {
			case "chat_aop_events":
				err = migrateEvents(ctx, tx)
			case "scans":
				err = migrateScans(ctx, tx)
			}
			if err != nil {
				return fmt.Errorf("migrate %s: %w", table, err)
			}
		}
		return validateSchema(tx, schema)
	})
}

func checkMigrationTriggers(ctx context.Context, tx bun.Tx, table string) error {
	var triggers int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE tbl_name = ? AND type = 'trigger'`, table).Scan(&triggers); err != nil {
		return err
	}
	if triggers != 0 {
		return fmt.Errorf("custom %s triggers prevent automatic migration", table)
	}
	return nil
}

func migrateEvents(ctx context.Context, tx bun.Tx) error {
	if err := checkMigrationTriggers(ctx, tx, "chat_aop_events"); err != nil {
		return err
	}
	// Rebuild the event table with the current model, preserving explicit indexes
	// as well as each row's identity, cursor, sequence and timestamp.
	var indexes []string
	if err := tx.NewRaw(`SELECT sql FROM sqlite_master WHERE tbl_name = 'chat_aop_events' AND type = 'index' AND sql IS NOT NULL`).Scan(ctx, &indexes); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE chat_aop_events RENAME TO chat_aop_events_legacy_json`); err != nil {
		return err
	}
	if _, err := tx.NewCreateTable().Model((*aopEventModel)(nil)).WithForeignKeys().Exec(ctx); err != nil {
		return err
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO chat_aop_events (id, session_id, event_id, cursor, turn_id, emitter, sequence, event_proto, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	rows, err := tx.QueryContext(ctx, `SELECT id, session_id, event_id, cursor, turn_id, emitter, sequence, event_json, created_at FROM chat_aop_events_legacy_json ORDER BY session_id, cursor`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, sessionID, eventID, turnID, emitter, raw, createdAt string
		var cursor, sequence int64
		if err := rows.Scan(&id, &sessionID, &eventID, &cursor, &turnID, &emitter, &sequence, &raw, &createdAt); err != nil {
			return err
		}
		event := new(aop.Event)
		if err := protojson.Unmarshal([]byte(raw), event); err != nil {
			return fmt.Errorf("decode event %q: %w", eventID, err)
		}
		binary, err := (proto.MarshalOptions{Deterministic: true}).Marshal(event)
		if err != nil {
			return fmt.Errorf("encode event %q: %w", eventID, err)
		}
		if _, err := insert.ExecContext(ctx, id, sessionID, eventID, cursor, turnID, emitter, sequence, binary, createdAt); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE chat_aop_events_legacy_json`); err != nil {
		return err
	}
	for _, statement := range indexes {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func migrateScans(ctx context.Context, tx bun.Tx) error {
	if err := checkMigrationTriggers(ctx, tx, "scans"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE scans ADD COLUMN has_options BOOLEAN NOT NULL DEFAULT false`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, scan_json FROM scans ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		scan := new(scanpb.Scan)
		if err := protojson.Unmarshal([]byte(raw), scan); err != nil {
			return fmt.Errorf("decode scan %q: %w", id, err)
		}
		if scan.Id != id {
			return fmt.Errorf("scan %q has mismatched JSON identity %q", id, scan.Id)
		}
		// JSON was the legacy read path. Restore its fields, including option
		// presence and timestamps, rather than retaining stale projections.
		model, err := scanToModel(scan)
		if err != nil {
			return fmt.Errorf("convert scan %q: %w", id, err)
		}
		if _, err := tx.NewUpdate().Model(model).
			Column("target", "mode", "verify", "sniper", "status", "progress", "error", "created_at", "updated_at", "has_options").
			WherePK().Exec(ctx); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	// Dropping only the obsolete column keeps indexes and session foreign keys.
	// SQLite rejects dependencies on scan_json; the transaction then rolls back.
	_, err = tx.ExecContext(ctx, `ALTER TABLE scans DROP COLUMN scan_json`)
	return err
}
