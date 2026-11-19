package api

import (
	"context"

	"github.com/TaliaMarine/mailcatcher/internal/database"
	ws "github.com/TaliaMarine/mailcatcher/internal/websocket"
	"github.com/danielgtaylor/huma/v2"
)

// -- List Mailboxes --

type ListMailboxesInput struct {
	SortBy    string `query:"sort_by" enum:"address,name,message_count,last_message_at" default:"last_message_at" doc:"Sort field"`
	SortOrder string `query:"sort_order" enum:"asc,desc" default:"desc" doc:"Sort direction"`
}

type ListMailboxesOutput struct {
	Body []database.MailboxWithStats
}

func (a *API) listMailboxes(ctx context.Context, input *ListMailboxesInput) (*ListMailboxesOutput, error) {
	mailboxes, err := a.db.ListMailboxes(ctx, input.SortBy, input.SortOrder)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list mailboxes", err)
	}
	if mailboxes == nil {
		mailboxes = []database.MailboxWithStats{}
	}
	return &ListMailboxesOutput{Body: mailboxes}, nil
}

// -- Get Mailbox --

type GetMailboxInput struct {
	ID int64 `path:"id" doc:"Mailbox ID"`
}

type GetMailboxOutput struct {
	Body *database.MailboxWithStats
}

func (a *API) getMailbox(ctx context.Context, input *GetMailboxInput) (*GetMailboxOutput, error) {
	mb, err := a.db.GetMailbox(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get mailbox", err)
	}
	if mb == nil {
		return nil, huma.Error404NotFound("mailbox not found")
	}
	return &GetMailboxOutput{Body: mb}, nil
}

// -- Delete Mailbox --

type DeleteMailboxInput struct {
	ID int64 `path:"id" doc:"Mailbox ID"`
}

func (a *API) deleteMailbox(ctx context.Context, input *DeleteMailboxInput) (*struct{}, error) {
	mb, err := a.db.GetMailbox(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get mailbox", err)
	}
	if mb == nil {
		return nil, huma.Error404NotFound("mailbox not found")
	}

	if err := a.db.DeleteMailbox(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError("failed to delete mailbox", err)
	}

	a.hub.Broadcast(ws.Event{
		Type: "mailbox:deleted",
		Data: map[string]any{"mailbox_id": input.ID},
	})

	return nil, nil
}

// -- Delete All Mailboxes --

func (a *API) deleteAllMailboxes(ctx context.Context, input *struct{}) (*struct{}, error) {
	if err := a.db.DeleteAllMailboxes(ctx); err != nil {
		return nil, huma.Error500InternalServerError("failed to delete all mailboxes", err)
	}

	a.hub.Broadcast(ws.Event{
		Type: "mailbox:deleted",
		Data: map[string]any{"mailbox_id": "*"},
	})

	return nil, nil
}
