package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	conn.SetMaxOpenConns(1)
	return &DB{conn: conn}, nil
}

func (db *DB) Migrate() error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}
	driver, err := sqlite.WithInstance(db.conn, &sqlite.Config{})
	if err != nil {
		return fmt.Errorf("migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("migration init: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration up: %w", err)
	}
	return nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Ping(ctx context.Context) error {
	return db.conn.PingContext(ctx)
}

func parseTime(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// --- Mailboxes ---

func (db *DB) ListMailboxes(ctx context.Context, sortBy, sortOrder string) ([]MailboxWithStats, error) {
	allowed := map[string]string{
		"address":         "mb.address",
		"name":            "mb.name",
		"message_count":   "message_count",
		"last_message_at": "last_message_at",
	}
	col, ok := allowed[sortBy]
	if !ok {
		col = "last_message_at"
	}
	dir := "DESC"
	if strings.EqualFold(sortOrder, "asc") {
		dir = "ASC"
	}

	query := fmt.Sprintf(`
		SELECT mb.id, mb.address, mb.name, mb.created_at,
			COUNT(mm.id) AS message_count,
			COALESCE(MAX(m.created_at), mb.created_at) AS last_message_at
		FROM mailboxes mb
		LEFT JOIN mailbox_messages mm ON mm.mailbox_id = mb.id
		LEFT JOIN messages m ON m.id = mm.message_id
		GROUP BY mb.id
		ORDER BY %s %s`, col, dir)

	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list mailboxes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []MailboxWithStats
	for rows.Next() {
		var mb MailboxWithStats
		var createdAt, lastMsg string
		if err := rows.Scan(&mb.ID, &mb.Address, &mb.Name, &createdAt, &mb.MessageCount, &lastMsg); err != nil {
			return nil, fmt.Errorf("scan mailbox: %w", err)
		}
		mb.CreatedAt = parseTime(createdAt)
		mb.LastMessageAt = parseTime(lastMsg)
		result = append(result, mb)
	}
	return result, rows.Err()
}

func (db *DB) GetMailbox(ctx context.Context, id int64) (*MailboxWithStats, error) {
	var mb MailboxWithStats
	var createdAt, lastMsg string
	err := db.conn.QueryRowContext(ctx, `
		SELECT mb.id, mb.address, mb.name, mb.created_at,
			COUNT(mm.id) AS message_count,
			COALESCE(MAX(m.created_at), mb.created_at) AS last_message_at
		FROM mailboxes mb
		LEFT JOIN mailbox_messages mm ON mm.mailbox_id = mb.id
		LEFT JOIN messages m ON m.id = mm.message_id
		WHERE mb.id = ?
		GROUP BY mb.id`, id).Scan(&mb.ID, &mb.Address, &mb.Name, &createdAt, &mb.MessageCount, &lastMsg)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get mailbox: %w", err)
	}
	mb.CreatedAt = parseTime(createdAt)
	mb.LastMessageAt = parseTime(lastMsg)
	return &mb, nil
}

func (db *DB) GetOrCreateMailbox(ctx context.Context, address, name string) (*Mailbox, error) {
	var mb Mailbox
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, address, name, created_at FROM mailboxes WHERE address = ?`, address,
	).Scan(&mb.ID, &mb.Address, &mb.Name, &mb.CreatedAt)
	if err == nil {
		return &mb, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("lookup mailbox: %w", err)
	}

	res, err := db.conn.ExecContext(ctx,
		`INSERT INTO mailboxes (address, name) VALUES (?, ?)`, address, name)
	if err != nil {
		return nil, fmt.Errorf("create mailbox: %w", err)
	}
	mb.ID, _ = res.LastInsertId()
	mb.Address = address
	mb.Name = name
	mb.CreatedAt = time.Now().UTC()
	return &mb, nil
}

func (db *DB) DeleteMailbox(ctx context.Context, id int64) error {
	// Delete messages that only belong to this mailbox
	_, err := db.conn.ExecContext(ctx, `
		DELETE FROM messages WHERE id IN (
			SELECT mm.message_id FROM mailbox_messages mm
			WHERE mm.mailbox_id = ?
			AND mm.message_id NOT IN (
				SELECT message_id FROM mailbox_messages WHERE mailbox_id != ?
			)
		)`, id, id)
	if err != nil {
		return fmt.Errorf("delete mailbox messages: %w", err)
	}
	// Delete mailbox_messages associations
	if _, err := db.conn.ExecContext(ctx, `DELETE FROM mailbox_messages WHERE mailbox_id = ?`, id); err != nil {
		return fmt.Errorf("delete mailbox associations: %w", err)
	}
	// Delete the mailbox
	if _, err := db.conn.ExecContext(ctx, `DELETE FROM mailboxes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete mailbox: %w", err)
	}
	return nil
}

func (db *DB) DeleteAllMailboxes(ctx context.Context) error {
	if _, err := db.conn.ExecContext(ctx, `DELETE FROM attachments`); err != nil {
		return fmt.Errorf("delete all attachments: %w", err)
	}
	if _, err := db.conn.ExecContext(ctx, `DELETE FROM mailbox_messages`); err != nil {
		return fmt.Errorf("delete all mailbox_messages: %w", err)
	}
	if _, err := db.conn.ExecContext(ctx, `DELETE FROM messages`); err != nil {
		return fmt.Errorf("delete all messages: %w", err)
	}
	if _, err := db.conn.ExecContext(ctx, `DELETE FROM mailboxes`); err != nil {
		return fmt.Errorf("delete all mailboxes: %w", err)
	}
	return nil
}

// --- Messages ---

type StoreMessageInput struct {
	MessageID   string
	FromAddress string
	FromName    string
	Subject     string
	BodyText    string
	BodyHTML    *string
	Raw         []byte
	Size        int
	Recipients  []Recipient
	Attachments []StoreAttachment
}

type Recipient struct {
	Address string
	Name    string
	Type    string // "to", "cc", "bcc"
}

type StoreAttachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

func (db *DB) StoreMessage(ctx context.Context, input StoreMessageInput) (*Message, []int64, error) {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO messages (message_id, from_address, from_name, subject, body_text, body_html, raw, size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		input.MessageID, input.FromAddress, input.FromName, input.Subject,
		input.BodyText, input.BodyHTML, input.Raw, input.Size)
	if err != nil {
		return nil, nil, fmt.Errorf("insert message: %w", err)
	}
	msgID, _ := res.LastInsertId()

	var mailboxIDs []int64
	for _, r := range input.Recipients {
		var mbID int64
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM mailboxes WHERE address = ?`, r.Address).Scan(&mbID)
		if err == sql.ErrNoRows {
			res, err := tx.ExecContext(ctx,
				`INSERT INTO mailboxes (address, name) VALUES (?, ?)`, r.Address, r.Name)
			if err != nil {
				return nil, nil, fmt.Errorf("create mailbox: %w", err)
			}
			mbID, _ = res.LastInsertId()
		} else if err != nil {
			return nil, nil, fmt.Errorf("lookup mailbox: %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO mailbox_messages (mailbox_id, message_id, recipient_type) VALUES (?, ?, ?)`,
			mbID, msgID, r.Type); err != nil {
			return nil, nil, fmt.Errorf("insert mailbox_message: %w", err)
		}
		mailboxIDs = append(mailboxIDs, mbID)
	}

	for _, a := range input.Attachments {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO attachments (message_id, filename, content_type, size, content)
			VALUES (?, ?, ?, ?, ?)`,
			msgID, a.Filename, a.ContentType, len(a.Content), a.Content); err != nil {
			return nil, nil, fmt.Errorf("insert attachment: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}

	msg := &Message{
		ID:          msgID,
		MessageID:   input.MessageID,
		FromAddress: input.FromAddress,
		FromName:    input.FromName,
		Subject:     input.Subject,
		BodyText:    input.BodyText,
		BodyHTML:    input.BodyHTML,
		Size:        input.Size,
		CreatedAt:   time.Now().UTC(),
	}
	return msg, mailboxIDs, nil
}

func (db *DB) ListMessages(ctx context.Context, mailboxID int64, cursor int64, limit int) ([]MessageSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT m.id, m.from_address, m.from_name, m.subject, m.size, m.created_at,
			EXISTS(SELECT 1 FROM attachments a WHERE a.message_id = m.id) AS has_attachments
		FROM messages m
		JOIN mailbox_messages mm ON mm.message_id = m.id
		WHERE mm.mailbox_id = ?`
	args := []any{mailboxID}

	if cursor > 0 {
		query += ` AND m.id < ?`
		args = append(args, cursor)
	}
	query += ` ORDER BY m.created_at DESC, m.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []MessageSummary
	for rows.Next() {
		var ms MessageSummary
		if err := rows.Scan(&ms.ID, &ms.FromAddress, &ms.FromName, &ms.Subject, &ms.Size, &ms.CreatedAt, &ms.HasAttachments); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		result = append(result, ms)
	}
	return result, rows.Err()
}

func (db *DB) GetMessage(ctx context.Context, id int64) (*Message, error) {
	var msg Message
	err := db.conn.QueryRowContext(ctx, `
		SELECT id, message_id, from_address, from_name, subject, body_text, body_html, size, created_at
		FROM messages WHERE id = ?`, id).
		Scan(&msg.ID, &msg.MessageID, &msg.FromAddress, &msg.FromName, &msg.Subject,
			&msg.BodyText, &msg.BodyHTML, &msg.Size, &msg.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	return &msg, nil
}

func (db *DB) GetMessageRaw(ctx context.Context, id int64) ([]byte, error) {
	var raw []byte
	err := db.conn.QueryRowContext(ctx, `SELECT raw FROM messages WHERE id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get message raw: %w", err)
	}
	return raw, nil
}

func (db *DB) DeleteMessage(ctx context.Context, id int64) error {
	if _, err := db.conn.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	return db.cleanupEmptyMailboxes(ctx)
}

func (db *DB) DeleteMessages(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	if _, err := db.conn.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM messages WHERE id IN (%s)`, placeholders), args...); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	return db.cleanupEmptyMailboxes(ctx)
}

func (db *DB) cleanupEmptyMailboxes(ctx context.Context) error {
	_, err := db.conn.ExecContext(ctx, `
		DELETE FROM mailboxes WHERE id NOT IN (
			SELECT DISTINCT mailbox_id FROM mailbox_messages
		)`)
	if err != nil {
		return fmt.Errorf("cleanup empty mailboxes: %w", err)
	}
	return nil
}

// --- Attachments ---

func (db *DB) ListAttachments(ctx context.Context, messageID int64) ([]AttachmentMeta, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT id, message_id, filename, content_type, size
		FROM attachments WHERE message_id = ?`, messageID)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []AttachmentMeta
	for rows.Next() {
		var a AttachmentMeta
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.ContentType, &a.Size); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (db *DB) GetAttachment(ctx context.Context, id int64) (*Attachment, error) {
	var a Attachment
	err := db.conn.QueryRowContext(ctx, `
		SELECT id, message_id, filename, content_type, size, content
		FROM attachments WHERE id = ?`, id).
		Scan(&a.ID, &a.MessageID, &a.Filename, &a.ContentType, &a.Size, &a.Content)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return &a, nil
}

// --- Search ---

func (db *DB) SearchMessages(ctx context.Context, query string, cursor int64, limit int) ([]MessageSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	pattern := "%" + query + "%"

	sql := `
		SELECT DISTINCT m.id, m.from_address, m.from_name, m.subject, m.size, m.created_at,
			EXISTS(SELECT 1 FROM attachments a WHERE a.message_id = m.id) AS has_attachments
		FROM messages m
		WHERE (m.subject LIKE ? OR m.from_address LIKE ? OR m.from_name LIKE ? OR m.body_text LIKE ?)`
	args := []any{pattern, pattern, pattern, pattern}

	if cursor > 0 {
		sql += ` AND m.id < ?`
		args = append(args, cursor)
	}
	sql += ` ORDER BY m.created_at DESC, m.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.conn.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []MessageSummary
	for rows.Next() {
		var ms MessageSummary
		if err := rows.Scan(&ms.ID, &ms.FromAddress, &ms.FromName, &ms.Subject, &ms.Size, &ms.CreatedAt, &ms.HasAttachments); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		result = append(result, ms)
	}
	return result, rows.Err()
}

// --- Retention ---

func (db *DB) DeleteMessagesOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := db.conn.ExecContext(ctx, `DELETE FROM messages WHERE created_at < ?`, before)
	if err != nil {
		return 0, fmt.Errorf("delete old messages: %w", err)
	}
	if err := db.cleanupEmptyMailboxes(ctx); err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- List All Messages (across all mailboxes) ---

func (db *DB) ListAllMessages(ctx context.Context, cursor int64, limit int) ([]MessageSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT m.id, m.from_address, m.from_name, m.subject, m.size, m.created_at,
			EXISTS(SELECT 1 FROM attachments a WHERE a.message_id = m.id) AS has_attachments
		FROM messages m`
	var args []any

	if cursor > 0 {
		query += ` WHERE m.id < ?`
		args = append(args, cursor)
	}
	query += ` ORDER BY m.created_at DESC, m.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list all messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []MessageSummary
	for rows.Next() {
		var ms MessageSummary
		if err := rows.Scan(&ms.ID, &ms.FromAddress, &ms.FromName, &ms.Subject, &ms.Size, &ms.CreatedAt, &ms.HasAttachments); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		result = append(result, ms)
	}
	return result, rows.Err()
}

// --- External messages (all messages with recipients) ---

func (db *DB) ListAllMessagesExternal(ctx context.Context) ([]ExternalMessage, error) {
	// Get all messages
	rows, err := db.conn.QueryContext(ctx, `
		SELECT m.id, m.from_address, m.subject, m.size, m.created_at
		FROM messages m
		ORDER BY m.created_at DESC, m.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list external messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []ExternalMessage
	messageIndex := make(map[int64]int)
	for rows.Next() {
		var m ExternalMessage
		var createdAt string
		if err := rows.Scan(&m.ID, &m.FromAddress, &m.Subject, &m.Size, &createdAt); err != nil {
			return nil, fmt.Errorf("scan external message: %w", err)
		}
		m.CreatedAt = parseTime(createdAt)
		messageIndex[m.ID] = len(messages)
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(messages) == 0 {
		return messages, nil
	}

	// Get all recipients in one query
	recRows, err := db.conn.QueryContext(ctx, `
		SELECT mm.message_id, mb.address
		FROM mailbox_messages mm
		JOIN mailboxes mb ON mb.id = mm.mailbox_id
		ORDER BY mm.message_id`)
	if err != nil {
		return nil, fmt.Errorf("list external recipients: %w", err)
	}
	defer func() { _ = recRows.Close() }()

	for recRows.Next() {
		var msgID int64
		var addr string
		if err := recRows.Scan(&msgID, &addr); err != nil {
			return nil, fmt.Errorf("scan external recipient: %w", err)
		}
		if idx, ok := messageIndex[msgID]; ok {
			messages[idx].Recipients = append(messages[idx].Recipients, addr)
		}
	}
	return messages, recRows.Err()
}

// --- Message recipients ---

func (db *DB) GetMessageRecipients(ctx context.Context, messageID int64) ([]MessageRecipient, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT mb.address, mb.name, mm.recipient_type
		FROM mailbox_messages mm
		JOIN mailboxes mb ON mb.id = mm.mailbox_id
		WHERE mm.message_id = ?
		ORDER BY mm.recipient_type, mb.address`, messageID)
	if err != nil {
		return nil, fmt.Errorf("get message recipients: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []MessageRecipient
	for rows.Next() {
		var r MessageRecipient
		if err := rows.Scan(&r.Address, &r.Name, &r.RecipientType); err != nil {
			return nil, fmt.Errorf("scan recipient: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// --- Message mailbox IDs (for WebSocket notifications) ---

func (db *DB) GetMessageMailboxIDs(ctx context.Context, messageID int64) ([]int64, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT mailbox_id FROM mailbox_messages WHERE message_id = ?`, messageID)
	if err != nil {
		return nil, fmt.Errorf("get message mailbox IDs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan mailbox ID: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
