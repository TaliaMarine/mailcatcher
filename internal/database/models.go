package database

import "time"

type Mailbox struct {
	ID        int64     `json:"id"`
	Address   string    `json:"address"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type MailboxWithStats struct {
	Mailbox
	MessageCount  int       `json:"message_count"`
	LastMessageAt time.Time `json:"last_message_at"`
}

type Message struct {
	ID          int64     `json:"id"`
	MessageID   string    `json:"message_id"`
	FromAddress string    `json:"from_address"`
	FromName    string    `json:"from_name"`
	Subject     string    `json:"subject"`
	BodyText    string    `json:"body_text"`
	BodyHTML    *string   `json:"body_html"`
	Raw         []byte    `json:"-"`
	Size        int       `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
}

type MessageSummary struct {
	ID             int64     `json:"id"`
	FromAddress    string    `json:"from_address"`
	FromName       string    `json:"from_name"`
	Subject        string    `json:"subject"`
	Size           int       `json:"size"`
	HasAttachments bool      `json:"has_attachments"`
	CreatedAt      time.Time `json:"created_at"`
}

type ExternalMessage struct {
	ID          int64     `json:"id"`
	FromAddress string    `json:"-"`
	Subject     string    `json:"subject"`
	Size        int       `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	Recipients  []string  `json:"-"`
}

type MailboxMessage struct {
	ID            int64  `json:"id"`
	MailboxID     int64  `json:"mailbox_id"`
	MessageID     int64  `json:"message_id"`
	RecipientType string `json:"recipient_type"`
}

type MessageRecipient struct {
	Address       string `json:"address"`
	Name          string `json:"name"`
	RecipientType string `json:"recipient_type"`
}

type Attachment struct {
	ID          int64  `json:"id"`
	MessageID   int64  `json:"message_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	Content     []byte `json:"-"`
}

type AttachmentMeta struct {
	ID          int64  `json:"id"`
	MessageID   int64  `json:"message_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}
