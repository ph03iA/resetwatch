package store

import "context"

// SaveCodexBinary updates the connection without overwriting concurrent email changes.
func (d *DB) SaveCodexBinary(ctx context.Context, binary string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	config, err := loadConfig(ctx, tx)
	if err != nil {
		return err
	}
	config.CodexBin = binary
	if err := saveConfig(ctx, tx, config); err != nil {
		return err
	}
	return tx.Commit()
}
