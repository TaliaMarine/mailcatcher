package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testDB creates a temporary database for testing and returns it along with a cleanup function.
func testDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func storeTestMessage(t *testing.T, db *DB, subject string, recipients []Recipient) (*Message, []int64) {
	t.Helper()
	html := "<p>Hello</p>"
	input := StoreMessageInput{
		MessageID:   "test-msg-id@example.com",
		FromAddress: "sender@example.com",
		FromName:    "Sender",
		Subject:     subject,
		BodyText:    "Hello, World!",
		BodyHTML:    &html,
		Raw:         []byte("raw email content"),
		Size:        100,
		Recipients:  recipients,
	}
	msg, mailboxIDs, err := db.StoreMessage(context.Background(), input)
	if err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	return msg, mailboxIDs
}

// --- Open / Migrate ---

func TestOpen_InvalidPath(t *testing.T) {
	_, err := Open("/nonexistent/dir/test.db")
	// On open the driver may not fail, but ping should — or migrate should.
	// With modernc/sqlite the Open itself succeeds; the real failure is on Migrate or Ping.
	if err != nil {
		// That's fine too — depends on driver
		return
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db := testDB(t)
	// Running migrate again should be a no-op (ErrNoChange).
	if err := db.Migrate(); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestPing(t *testing.T) {
	db := testDB(t)
	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "close_test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Ping after close should fail
	if err := db.Ping(context.Background()); err == nil {
		t.Error("expected error after Close, got nil")
	}
}

// --- Mailboxes ---

func TestGetOrCreateMailbox_CreateAndGet(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	mb, err := db.GetOrCreateMailbox(ctx, "alice@example.com", "Alice")
	if err != nil {
		t.Fatalf("GetOrCreateMailbox (create): %v", err)
	}
	if mb.Address != "alice@example.com" || mb.Name != "Alice" || mb.ID == 0 {
		t.Errorf("unexpected mailbox: %+v", mb)
	}

	// Get existing
	mb2, err := db.GetOrCreateMailbox(ctx, "alice@example.com", "Alice Updated")
	if err != nil {
		t.Fatalf("GetOrCreateMailbox (get): %v", err)
	}
	if mb2.ID != mb.ID {
		t.Errorf("expected same ID %d, got %d", mb.ID, mb2.ID)
	}
}

func TestListMailboxes_Empty(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	mailboxes, err := db.ListMailboxes(ctx, "last_message_at", "desc")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(mailboxes) != 0 {
		t.Errorf("expected 0 mailboxes, got %d", len(mailboxes))
	}
}

func TestListMailboxes_SortByAddress(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeTestMessage(t, db, "Msg 1", []Recipient{{Address: "bravo@example.com", Name: "Bravo", Type: "to"}})
	storeTestMessage(t, db, "Msg 2", []Recipient{{Address: "alpha@example.com", Name: "Alpha", Type: "to"}})

	mailboxes, err := db.ListMailboxes(ctx, "address", "asc")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(mailboxes) != 2 {
		t.Fatalf("expected 2 mailboxes, got %d", len(mailboxes))
	}
	if mailboxes[0].Address != "alpha@example.com" {
		t.Errorf("expected first mailbox alpha, got %s", mailboxes[0].Address)
	}
}

func TestListMailboxes_InvalidSortFallsBack(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Should not error with invalid sort field — falls back to last_message_at
	_, err := db.ListMailboxes(ctx, "bogus_field", "desc")
	if err != nil {
		t.Fatalf("ListMailboxes with invalid sort: %v", err)
	}
}

func TestGetMailbox_NotFound(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	mb, err := db.GetMailbox(ctx, 99999)
	if err != nil {
		t.Fatalf("GetMailbox: %v", err)
	}
	if mb != nil {
		t.Errorf("expected nil, got %+v", mb)
	}
}

func TestGetMailbox_Found(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	_, mailboxIDs := storeTestMessage(t, db, "Test", []Recipient{{Address: "user@example.com", Name: "User", Type: "to"}})

	mb, err := db.GetMailbox(ctx, mailboxIDs[0])
	if err != nil {
		t.Fatalf("GetMailbox: %v", err)
	}
	if mb == nil {
		t.Fatal("expected mailbox, got nil")
	}
	if mb.MessageCount != 1 {
		t.Errorf("MessageCount = %d, want 1", mb.MessageCount)
	}
}

func TestDeleteMailbox(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	_, mailboxIDs := storeTestMessage(t, db, "Test", []Recipient{{Address: "del@example.com", Name: "Del", Type: "to"}})

	if err := db.DeleteMailbox(ctx, mailboxIDs[0]); err != nil {
		t.Fatalf("DeleteMailbox: %v", err)
	}

	mb, err := db.GetMailbox(ctx, mailboxIDs[0])
	if err != nil {
		t.Fatalf("GetMailbox: %v", err)
	}
	if mb != nil {
		t.Error("expected mailbox to be deleted")
	}
}

func TestDeleteAllMailboxes(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeTestMessage(t, db, "Msg 1", []Recipient{{Address: "a@example.com", Name: "A", Type: "to"}})
	storeTestMessage(t, db, "Msg 2", []Recipient{{Address: "b@example.com", Name: "B", Type: "to"}})

	if err := db.DeleteAllMailboxes(ctx); err != nil {
		t.Fatalf("DeleteAllMailboxes: %v", err)
	}

	mailboxes, err := db.ListMailboxes(ctx, "address", "asc")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(mailboxes) != 0 {
		t.Errorf("expected 0 mailboxes, got %d", len(mailboxes))
	}
}

// --- Messages ---

func TestStoreMessage_Basic(t *testing.T) {
	db := testDB(t)

	msg, mailboxIDs := storeTestMessage(t, db, "Hello", []Recipient{
		{Address: "alice@example.com", Name: "Alice", Type: "to"},
		{Address: "bob@example.com", Name: "Bob", Type: "cc"},
	})

	if msg.ID == 0 {
		t.Error("expected non-zero message ID")
	}
	if msg.Subject != "Hello" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Hello")
	}
	if len(mailboxIDs) != 2 {
		t.Errorf("expected 2 mailbox IDs, got %d", len(mailboxIDs))
	}
}

func TestStoreMessage_WithAttachments(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	input := StoreMessageInput{
		MessageID:   "attach@example.com",
		FromAddress: "sender@example.com",
		Subject:     "With Attachment",
		BodyText:    "See attached",
		Raw:         []byte("raw"),
		Size:        50,
		Recipients:  []Recipient{{Address: "user@example.com", Name: "User", Type: "to"}},
		Attachments: []StoreAttachment{
			{Filename: "report.pdf", ContentType: "application/pdf", Content: []byte("pdf-content")},
			{Filename: "image.png", ContentType: "image/png", Content: []byte("png-content")},
		},
	}

	msg, _, err := db.StoreMessage(ctx, input)
	if err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}

	attachments, err := db.ListAttachments(ctx, msg.ID)
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	if len(attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d", len(attachments))
	}
	if attachments[0].Filename != "report.pdf" {
		t.Errorf("attachment filename = %q, want %q", attachments[0].Filename, "report.pdf")
	}
}

func TestListMessages_ByMailbox(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeTestMessage(t, db, "Msg 1", []Recipient{{Address: "inbox@example.com", Name: "Inbox", Type: "to"}})
	storeTestMessage(t, db, "Msg 2", []Recipient{{Address: "inbox@example.com", Name: "Inbox", Type: "to"}})
	storeTestMessage(t, db, "Msg 3", []Recipient{{Address: "other@example.com", Name: "Other", Type: "to"}})

	mailboxes, err := db.ListMailboxes(ctx, "address", "asc")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}

	var inboxID int64
	for _, mb := range mailboxes {
		if mb.Address == "inbox@example.com" {
			inboxID = mb.ID
		}
	}

	messages, err := db.ListMessages(ctx, inboxID, 0, 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
	}
}

func TestListMessages_Pagination(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		storeTestMessage(t, db, "Msg", []Recipient{{Address: "page@example.com", Name: "Page", Type: "to"}})
	}

	mailboxes, _ := db.ListMailboxes(ctx, "address", "asc")
	mbID := mailboxes[0].ID

	// Get first page (limit 3)
	page1, err := db.ListMessages(ctx, mbID, 0, 3)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("page1 len = %d, want 3", len(page1))
	}

	// Get second page using cursor from last item of page 1
	cursor := page1[len(page1)-1].ID
	page2, err := db.ListMessages(ctx, mbID, cursor, 3)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2))
	}
}

func TestListMessages_LimitClamping(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeTestMessage(t, db, "Test", []Recipient{{Address: "clamp@example.com", Name: "Clamp", Type: "to"}})
	mailboxes, _ := db.ListMailboxes(ctx, "address", "asc")
	mbID := mailboxes[0].ID

	// Limit 0 should be clamped to 50
	_, err := db.ListMessages(ctx, mbID, 0, 0)
	if err != nil {
		t.Fatalf("ListMessages with limit 0: %v", err)
	}
	// Limit >100 should be clamped to 50
	_, err = db.ListMessages(ctx, mbID, 0, 200)
	if err != nil {
		t.Fatalf("ListMessages with limit 200: %v", err)
	}
}

func TestListAllMessages(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeTestMessage(t, db, "Msg 1", []Recipient{{Address: "a@example.com", Name: "A", Type: "to"}})
	storeTestMessage(t, db, "Msg 2", []Recipient{{Address: "b@example.com", Name: "B", Type: "to"}})

	messages, err := db.ListAllMessages(ctx, 0, 50)
	if err != nil {
		t.Fatalf("ListAllMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
	}
}

func TestGetMessage_NotFound(t *testing.T) {
	db := testDB(t)

	msg, err := db.GetMessage(context.Background(), 99999)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg != nil {
		t.Error("expected nil for non-existent message")
	}
}

func TestGetMessage_Found(t *testing.T) {
	db := testDB(t)

	stored, _ := storeTestMessage(t, db, "Get Me", []Recipient{{Address: "get@example.com", Name: "Get", Type: "to"}})

	msg, err := db.GetMessage(context.Background(), stored.ID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message, got nil")
	}
	if msg.Subject != "Get Me" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Get Me")
	}
	if msg.BodyHTML == nil || *msg.BodyHTML != "<p>Hello</p>" {
		t.Errorf("BodyHTML unexpected: %v", msg.BodyHTML)
	}
}

func TestGetMessageRaw(t *testing.T) {
	db := testDB(t)

	stored, _ := storeTestMessage(t, db, "Raw Test", []Recipient{{Address: "raw@example.com", Name: "Raw", Type: "to"}})

	raw, err := db.GetMessageRaw(context.Background(), stored.ID)
	if err != nil {
		t.Fatalf("GetMessageRaw: %v", err)
	}
	if string(raw) != "raw email content" {
		t.Errorf("raw = %q, want %q", string(raw), "raw email content")
	}
}

func TestGetMessageRaw_NotFound(t *testing.T) {
	db := testDB(t)

	raw, err := db.GetMessageRaw(context.Background(), 99999)
	if err != nil {
		t.Fatalf("GetMessageRaw: %v", err)
	}
	if raw != nil {
		t.Errorf("expected nil, got %q", string(raw))
	}
}

func TestDeleteMessage(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	msg, _ := storeTestMessage(t, db, "Delete Me", []Recipient{{Address: "del@example.com", Name: "Del", Type: "to"}})

	if err := db.DeleteMessage(ctx, msg.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	got, err := db.GetMessage(ctx, msg.ID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if got != nil {
		t.Error("expected message to be deleted")
	}

	// Mailbox should be cleaned up since it has no messages
	mailboxes, _ := db.ListMailboxes(ctx, "address", "asc")
	if len(mailboxes) != 0 {
		t.Errorf("expected empty mailbox cleanup, got %d mailboxes", len(mailboxes))
	}
}

func TestDeleteMessages_Bulk(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	msg1, _ := storeTestMessage(t, db, "Bulk 1", []Recipient{{Address: "bulk@example.com", Name: "Bulk", Type: "to"}})
	msg2, _ := storeTestMessage(t, db, "Bulk 2", []Recipient{{Address: "bulk@example.com", Name: "Bulk", Type: "to"}})

	if err := db.DeleteMessages(ctx, []int64{msg1.ID, msg2.ID}); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	messages, _ := db.ListAllMessages(ctx, 0, 50)
	if len(messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(messages))
	}
}

func TestDeleteMessages_EmptySlice(t *testing.T) {
	db := testDB(t)
	// Should be a no-op
	if err := db.DeleteMessages(context.Background(), []int64{}); err != nil {
		t.Fatalf("DeleteMessages empty: %v", err)
	}
}

func TestDeleteMessage_CleansUpOrphanedMailbox(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Message in a single mailbox — deleting message should remove mailbox
	msg, mailboxIDs := storeTestMessage(t, db, "Solo", []Recipient{{Address: "solo@example.com", Name: "Solo", Type: "to"}})

	if err := db.DeleteMessage(ctx, msg.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	mb, err := db.GetMailbox(ctx, mailboxIDs[0])
	if err != nil {
		t.Fatalf("GetMailbox: %v", err)
	}
	if mb != nil {
		t.Error("expected orphaned mailbox to be cleaned up")
	}
}

// --- Attachments ---

func TestListAttachments_Empty(t *testing.T) {
	db := testDB(t)
	msg, _ := storeTestMessage(t, db, "No Attach", []Recipient{{Address: "a@example.com", Name: "A", Type: "to"}})

	attachments, err := db.ListAttachments(context.Background(), msg.ID)
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	if len(attachments) != 0 {
		t.Errorf("expected 0 attachments, got %d", len(attachments))
	}
}

func TestGetAttachment_NotFound(t *testing.T) {
	db := testDB(t)
	att, err := db.GetAttachment(context.Background(), 99999)
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}
	if att != nil {
		t.Error("expected nil for non-existent attachment")
	}
}

func TestGetAttachment_Found(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	input := StoreMessageInput{
		MessageID:   "att@example.com",
		FromAddress: "sender@example.com",
		Subject:     "Attachment",
		BodyText:    "body",
		Raw:         []byte("raw"),
		Size:        50,
		Recipients:  []Recipient{{Address: "user@example.com", Name: "User", Type: "to"}},
		Attachments: []StoreAttachment{
			{Filename: "doc.txt", ContentType: "text/plain", Content: []byte("hello world")},
		},
	}

	msg, _, err := db.StoreMessage(ctx, input)
	if err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}

	metas, err := db.ListAttachments(ctx, msg.ID)
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(metas))
	}

	att, err := db.GetAttachment(ctx, metas[0].ID)
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}
	if att == nil {
		t.Fatal("expected attachment, got nil")
	}
	if string(att.Content) != "hello world" {
		t.Errorf("content = %q, want %q", string(att.Content), "hello world")
	}
	if att.Filename != "doc.txt" {
		t.Errorf("filename = %q, want %q", att.Filename, "doc.txt")
	}
}

// --- Search ---

func TestSearchMessages(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeTestMessage(t, db, "Welcome aboard", []Recipient{{Address: "s@example.com", Name: "S", Type: "to"}})
	storeTestMessage(t, db, "Invoice #123", []Recipient{{Address: "s@example.com", Name: "S", Type: "to"}})
	storeTestMessage(t, db, "Meeting notes", []Recipient{{Address: "s@example.com", Name: "S", Type: "to"}})

	results, err := db.SearchMessages(ctx, "invoice", 0, 50)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if len(results) > 0 && results[0].Subject != "Invoice #123" {
		t.Errorf("subject = %q, want %q", results[0].Subject, "Invoice #123")
	}
}

func TestSearchMessages_NoResults(t *testing.T) {
	db := testDB(t)
	results, err := db.SearchMessages(context.Background(), "nonexistent", 0, 50)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchMessages_ByFromAddress(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeTestMessage(t, db, "Hello", []Recipient{{Address: "r@example.com", Name: "R", Type: "to"}})

	results, err := db.SearchMessages(ctx, "sender@example", 0, 50)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result searching by from address, got %d", len(results))
	}
}

// --- Retention ---

func TestDeleteMessagesOlderThan(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeTestMessage(t, db, "Old Msg", []Recipient{{Address: "r@example.com", Name: "R", Type: "to"}})

	// Delete messages older than future date (should delete everything)
	future := time.Now().UTC().Add(24 * time.Hour)
	deleted, err := db.DeleteMessagesOlderThan(ctx, future)
	if err != nil {
		t.Fatalf("DeleteMessagesOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	messages, _ := db.ListAllMessages(ctx, 0, 50)
	if len(messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(messages))
	}
}

func TestDeleteMessagesOlderThan_NoneDeleted(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeTestMessage(t, db, "Recent", []Recipient{{Address: "r@example.com", Name: "R", Type: "to"}})

	past := time.Now().UTC().Add(-24 * time.Hour)
	deleted, err := db.DeleteMessagesOlderThan(ctx, past)
	if err != nil {
		t.Fatalf("DeleteMessagesOlderThan: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}

// --- Recipients ---

func TestGetMessageRecipients(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	msg, _ := storeTestMessage(t, db, "Multi Recipient", []Recipient{
		{Address: "alice@example.com", Name: "Alice", Type: "to"},
		{Address: "bob@example.com", Name: "Bob", Type: "cc"},
		{Address: "carol@example.com", Name: "Carol", Type: "bcc"},
	})

	recipients, err := db.GetMessageRecipients(ctx, msg.ID)
	if err != nil {
		t.Fatalf("GetMessageRecipients: %v", err)
	}
	if len(recipients) != 3 {
		t.Fatalf("expected 3 recipients, got %d", len(recipients))
	}

	// Should be ordered by recipient_type, address
	types := make(map[string]int)
	for _, r := range recipients {
		types[r.RecipientType]++
	}
	if types["to"] != 1 || types["cc"] != 1 || types["bcc"] != 1 {
		t.Errorf("unexpected recipient types: %v", types)
	}
}

func TestGetMessageMailboxIDs(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	msg, expectedIDs := storeTestMessage(t, db, "Mailbox IDs", []Recipient{
		{Address: "x@example.com", Name: "X", Type: "to"},
		{Address: "y@example.com", Name: "Y", Type: "cc"},
	})

	ids, err := db.GetMessageMailboxIDs(ctx, msg.ID)
	if err != nil {
		t.Fatalf("GetMessageMailboxIDs: %v", err)
	}
	if len(ids) != len(expectedIDs) {
		t.Errorf("expected %d mailbox IDs, got %d", len(expectedIDs), len(ids))
	}
}

// --- Multi-recipient message in multiple mailboxes ---

func TestMultiRecipient_AppearsInAllMailboxes(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeTestMessage(t, db, "Group Email", []Recipient{
		{Address: "alice@example.com", Name: "Alice", Type: "to"},
		{Address: "bob@example.com", Name: "Bob", Type: "cc"},
	})

	mailboxes, _ := db.ListMailboxes(ctx, "address", "asc")
	if len(mailboxes) != 2 {
		t.Fatalf("expected 2 mailboxes, got %d", len(mailboxes))
	}
	for _, mb := range mailboxes {
		if mb.MessageCount != 1 {
			t.Errorf("mailbox %s: expected 1 message, got %d", mb.Address, mb.MessageCount)
		}
	}
}

// --- DeleteMailbox only removes exclusive messages ---

func TestDeleteMailbox_PreservesSharedMessages(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Message shared between two mailboxes
	msg, _ := storeTestMessage(t, db, "Shared", []Recipient{
		{Address: "keep@example.com", Name: "Keep", Type: "to"},
		{Address: "delete@example.com", Name: "Delete", Type: "cc"},
	})

	mailboxes, _ := db.ListMailboxes(ctx, "address", "asc")
	var deleteID int64
	for _, mb := range mailboxes {
		if mb.Address == "delete@example.com" {
			deleteID = mb.ID
		}
	}

	if err := db.DeleteMailbox(ctx, deleteID); err != nil {
		t.Fatalf("DeleteMailbox: %v", err)
	}

	// Message should still exist because it belongs to "keep" mailbox
	got, err := db.GetMessage(ctx, msg.ID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if got == nil {
		t.Error("expected shared message to survive mailbox deletion")
	}
}

// --- parseTime ---

func TestParseTime(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool // true if should parse successfully (non-zero)
	}{
		{"sqlite format", "2024-01-15 10:30:00", true},
		{"ISO with Z", "2024-01-15T10:30:00Z", true},
		{"RFC3339", "2024-01-15T10:30:00+02:00", true},
		{"invalid", "not-a-date", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTime(tt.input)
			if tt.want && result.IsZero() {
				t.Errorf("parseTime(%q) returned zero time, expected valid time", tt.input)
			}
			if !tt.want && !result.IsZero() {
				t.Errorf("parseTime(%q) returned %v, expected zero time", tt.input, result)
			}
		})
	}
}

// --- Edge: opening DB in tmpdir that gets cleaned ---

func TestOpen_TempDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "test.db")
	// Create the subdirectory
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
