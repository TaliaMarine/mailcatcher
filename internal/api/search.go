package api

import (
	"context"

	"github.com/TaliaMarine/mailcatcher/internal/database"
	"github.com/danielgtaylor/huma/v2"
)

// -- Search Messages --

type SearchMessagesInput struct {
	Q      string `query:"q" required:"true" minLength:"1" doc:"Search query"`
	Cursor int64  `query:"cursor" doc:"Cursor for pagination (message ID)" default:"0"`
	Limit  int    `query:"limit" doc:"Number of results to return" default:"50" minimum:"1" maximum:"100"`
}

type SearchMessagesOutput struct {
	Body []database.MessageSummary
}

func (a *API) searchMessages(ctx context.Context, input *SearchMessagesInput) (*SearchMessagesOutput, error) {
	messages, err := a.db.SearchMessages(ctx, input.Q, input.Cursor, input.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to search messages", err)
	}
	if messages == nil {
		messages = []database.MessageSummary{}
	}
	return &SearchMessagesOutput{Body: messages}, nil
}
