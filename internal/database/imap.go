package database

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// IMAPMessage is the part of a message the IMAP server serves.
type IMAPMessage struct {
	ID        int64
	Size      int
	CreatedAt time.Time
	Raw       []byte // nil unless requested
}

// ListMessageIDs returns the ids of the messages in a mailbox in ascending
// order, or of every message when mailboxID is 0.
func (db *DB) ListMessageIDs(ctx context.Context, mailboxID int64) ([]int64, error) {
	query := `SELECT id FROM messages ORDER BY id`
	var args []any
	if mailboxID != 0 {
		query = `SELECT DISTINCT m.id FROM messages m
			JOIN mailbox_messages mm ON mm.message_id = m.id
			WHERE mm.mailbox_id = ? ORDER BY m.id`
		args = append(args, mailboxID)
	}
	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list message ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan message id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetIMAPMessage loads a message for IMAP. The raw bytes, which can be large,
// are only read when withRaw is set. Returns nil if the message is gone.
func (db *DB) GetIMAPMessage(ctx context.Context, id int64, withRaw bool) (*IMAPMessage, error) {
	query := `SELECT id, size, created_at FROM messages WHERE id = ?`
	if withRaw {
		query = `SELECT id, size, created_at, raw FROM messages WHERE id = ?`
	}
	var m IMAPMessage
	dest := []any{&m.ID, &m.Size, &m.CreatedAt}
	if withRaw {
		dest = append(dest, &m.Raw)
	}
	err := db.conn.QueryRowContext(ctx, query, id).Scan(dest...)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get imap message: %w", err)
	}
	return &m, nil
}

// FindMailboxByAddress looks a mailbox up by its (case-insensitive) address.
func (db *DB) FindMailboxByAddress(ctx context.Context, address string) (*Mailbox, error) {
	var mb Mailbox
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, address, name, created_at FROM mailboxes WHERE address = ?`,
		strings.ToLower(address)).Scan(&mb.ID, &mb.Address, &mb.Name, &mb.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find mailbox: %w", err)
	}
	return &mb, nil
}

// UIDValidity returns the IMAP UIDVALIDITY of this database.
func (db *DB) UIDValidity(ctx context.Context) (uint32, error) {
	var v string
	if err := db.conn.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'uid_validity'`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read uid validity: %w", err)
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil || n == 0 {
		return 1, nil
	}
	return uint32(n), nil
}

// NextMessageID returns the id the next stored message will get. Message ids
// are never reused (AUTOINCREMENT), which makes them stable IMAP UIDs.
func (db *DB) NextMessageID(ctx context.Context) (int64, error) {
	var seq int64
	err := db.conn.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name = 'messages'`).Scan(&seq)
	if err == sql.ErrNoRows {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read message sequence: %w", err)
	}
	return seq + 1, nil
}
