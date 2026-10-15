package store

import (
	"context"
	"encoding/json"

	"resetwatch/internal/notify"
)

// FreezeMessage preserves Resend's requirement that retries with one key have identical bodies.
func (d *DB) FreezeMessage(ctx context.Context, alert Alert, message notify.Message) (notify.Message, error) {
	data, err := json.Marshal(message)
	if err != nil {
		return notify.Message{}, err
	}
	var payload string
	err = d.db.QueryRowContext(ctx, `UPDATE alerts SET payload=CASE WHEN payload='' THEN ? ELSE payload END
 WHERE id=? AND state='sending' AND attempts=? RETURNING payload`, string(data), alert.ID, alert.Attempts).Scan(&payload)
	if err != nil {
		return notify.Message{}, err
	}
	var frozen notify.Message
	err = json.Unmarshal([]byte(payload), &frozen)
	return frozen, err
}
