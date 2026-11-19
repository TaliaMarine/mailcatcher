package api

import (
	"context"
	"fmt"
	"html"

	"github.com/TaliaMarine/mailcatcher/internal/database"
	ws "github.com/TaliaMarine/mailcatcher/internal/websocket"
	"github.com/danielgtaylor/huma/v2"
)

// -- List External Messages (app-to-app) --

type ExternalMessageResponse struct {
	ID         int64    `json:"id"`
	Sender     string   `json:"sender"`
	Recipients []string `json:"recipients"`
	Subject    string   `json:"subject"`
	Size       string   `json:"size"`
	CreatedAt  string   `json:"created_at"`
}

type ListExternalMessagesOutput struct {
	Body []ExternalMessageResponse
}

func (a *API) listExternalMessages(ctx context.Context, _ *struct{}) (*ListExternalMessagesOutput, error) {
	messages, err := a.db.ListAllMessagesExternal(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list messages", err)
	}

	result := make([]ExternalMessageResponse, 0, len(messages))
	for _, m := range messages {
		recipients := make([]string, 0, len(m.Recipients))
		for _, r := range m.Recipients {
			recipients = append(recipients, fmt.Sprintf("<%s>", r))
		}
		result = append(result, ExternalMessageResponse{
			ID:         m.ID,
			Sender:     fmt.Sprintf("<%s> size=%d", m.FromAddress, m.Size),
			Recipients: recipients,
			Subject:    m.Subject,
			Size:       fmt.Sprintf("%d", m.Size),
			CreatedAt:  m.CreatedAt.Format("2006-01-02T15:04:05+00:00"),
		})
	}

	return &ListExternalMessagesOutput{Body: result}, nil
}

// -- List Messages --

type ListMessagesInput struct {
	ID     int64 `path:"id" doc:"Mailbox ID"`
	Cursor int64 `query:"cursor" doc:"Cursor for pagination (message ID)" default:"0"`
	Limit  int   `query:"limit" doc:"Number of messages to return" default:"50" minimum:"1" maximum:"100"`
}

type ListMessagesOutput struct {
	Body []database.MessageSummary
}

func (a *API) listMessages(ctx context.Context, input *ListMessagesInput) (*ListMessagesOutput, error) {
	messages, err := a.db.ListMessages(ctx, input.ID, input.Cursor, input.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list messages", err)
	}
	if messages == nil {
		messages = []database.MessageSummary{}
	}
	return &ListMessagesOutput{Body: messages}, nil
}

// -- List All Messages --

type ListAllMessagesInput struct {
	Cursor int64 `query:"cursor" doc:"Cursor for pagination (message ID)" default:"0"`
	Limit  int   `query:"limit" doc:"Number of messages to return" default:"50" minimum:"1" maximum:"100"`
}

type ListAllMessagesOutput struct {
	Body []database.MessageSummary
}

func (a *API) listAllMessages(ctx context.Context, input *ListAllMessagesInput) (*ListAllMessagesOutput, error) {
	messages, err := a.db.ListAllMessages(ctx, input.Cursor, input.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list messages", err)
	}
	if messages == nil {
		messages = []database.MessageSummary{}
	}
	return &ListAllMessagesOutput{Body: messages}, nil
}

// -- Get Message --

type GetMessageInput struct {
	ID int64 `path:"id" doc:"Message ID"`
}

type MessageDetail struct {
	database.Message
	Attachments []database.AttachmentMeta   `json:"attachments"`
	Recipients  []database.MessageRecipient `json:"recipients"`
}

type GetMessageOutput struct {
	Body *MessageDetail
}

func (a *API) getMessage(ctx context.Context, input *GetMessageInput) (*GetMessageOutput, error) {
	msg, err := a.db.GetMessage(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get message", err)
	}
	if msg == nil {
		return nil, huma.Error404NotFound("message not found")
	}

	attachments, err := a.db.ListAttachments(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list attachments", err)
	}
	if attachments == nil {
		attachments = []database.AttachmentMeta{}
	}

	recipients, err := a.db.GetMessageRecipients(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get recipients", err)
	}
	if recipients == nil {
		recipients = []database.MessageRecipient{}
	}

	return &GetMessageOutput{
		Body: &MessageDetail{
			Message:     *msg,
			Attachments: attachments,
			Recipients:  recipients,
		},
	}, nil
}

// -- Get Message Raw --

type GetMessageRawInput struct {
	ID int64 `path:"id" doc:"Message ID"`
}

func (a *API) getMessageRaw(ctx context.Context, input *GetMessageRawInput) (*huma.StreamResponse, error) {
	raw, err := a.db.GetMessageRaw(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get raw message", err)
	}
	if raw == nil {
		return nil, huma.Error404NotFound("message not found")
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "message/rfc822")
			ctx.SetHeader("Content-Disposition", fmt.Sprintf(`attachment; filename="message-%d.eml"`, input.ID))
			ctx.SetStatus(200)
			writer := ctx.BodyWriter()
			if _, err := writer.Write(raw); err != nil {
				a.logger.Error("failed to write raw message content", "error", err)
			}
		},
	}, nil
}

// -- Get Message HTML --

// htmlCSP confines a rendered email: no scripts, no forms, no framing of other
// content, no navigation of the parent; images, styles and fonts may load.
const htmlCSP = "sandbox allow-popups allow-popups-to-escape-sandbox; default-src 'none'; " +
	"img-src * data:; style-src * 'unsafe-inline'; font-src *; base-uri 'none'; form-action 'none'"

func (a *API) getMessageHTML(ctx context.Context, input *GetMessageRawInput) (*huma.StreamResponse, error) {
	msg, err := a.db.GetMessage(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get message", err)
	}
	if msg == nil {
		return nil, huma.Error404NotFound("message not found")
	}

	body := msg.BodyText
	if msg.BodyHTML != nil {
		body = *msg.BodyHTML
	} else {
		body = "<!DOCTYPE html><meta charset=utf-8><style>body{font-family:ui-monospace,monospace;white-space:pre-wrap;padding:16px;margin:0}</style>" +
			html.EscapeString(body)
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
			ctx.SetHeader("Content-Security-Policy", htmlCSP)
			ctx.SetHeader("Referrer-Policy", "no-referrer")
			ctx.SetStatus(200)
			if _, err := ctx.BodyWriter().Write([]byte(body)); err != nil {
				a.logger.Error("failed to write message html", "error", err)
			}
		},
	}, nil
}

// -- Delete Message --

type DeleteMessageInput struct {
	ID int64 `path:"id" doc:"Message ID"`
}

func (a *API) deleteMessage(ctx context.Context, input *DeleteMessageInput) (*struct{}, error) {
	msg, err := a.db.GetMessage(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get message", err)
	}
	if msg == nil {
		return nil, huma.Error404NotFound("message not found")
	}

	mailboxIDs, err := a.db.GetMessageMailboxIDs(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get mailbox IDs", err)
	}

	if err := a.db.DeleteMessage(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError("failed to delete message", err)
	}

	a.hub.Broadcast(ws.Event{
		Type: "message:deleted",
		Data: map[string]any{
			"message_ids": []int64{input.ID},
			"mailbox_ids": mailboxIDs,
		},
	})

	return nil, nil
}

// -- Bulk Delete Messages --

type DeleteMessagesInput struct {
	Body struct {
		IDs []int64 `json:"ids" minItems:"1" doc:"Message IDs to delete"`
	}
}

func (a *API) deleteMessages(ctx context.Context, input *DeleteMessagesInput) (*struct{}, error) {
	// Collect all affected mailbox IDs before deleting
	mailboxIDSet := make(map[int64]bool)
	for _, msgID := range input.Body.IDs {
		ids, err := a.db.GetMessageMailboxIDs(ctx, msgID)
		if err != nil {
			continue
		}
		for _, id := range ids {
			mailboxIDSet[id] = true
		}
	}

	if err := a.db.DeleteMessages(ctx, input.Body.IDs); err != nil {
		return nil, huma.Error500InternalServerError("failed to delete messages", err)
	}

	var mailboxIDs []int64
	for id := range mailboxIDSet {
		mailboxIDs = append(mailboxIDs, id)
	}

	a.hub.Broadcast(ws.Event{
		Type: "message:deleted",
		Data: map[string]any{
			"message_ids": input.Body.IDs,
			"mailbox_ids": mailboxIDs,
		},
	})

	return nil, nil
}
