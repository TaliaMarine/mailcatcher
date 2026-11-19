package api

import (
	"context"
	"mime"

	"github.com/TaliaMarine/mailcatcher/internal/database"
	"github.com/danielgtaylor/huma/v2"
)

// -- List Attachments --

type ListAttachmentsInput struct {
	ID int64 `path:"id" doc:"Message ID"`
}

type ListAttachmentsOutput struct {
	Body []database.AttachmentMeta
}

func (a *API) listAttachments(ctx context.Context, input *ListAttachmentsInput) (*ListAttachmentsOutput, error) {
	attachments, err := a.db.ListAttachments(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list attachments", err)
	}
	if attachments == nil {
		attachments = []database.AttachmentMeta{}
	}
	return &ListAttachmentsOutput{Body: attachments}, nil
}

// -- Get Attachment (download) --

type GetAttachmentInput struct {
	ID int64 `path:"id" doc:"Attachment ID"`
}

func (a *API) getAttachment(ctx context.Context, input *GetAttachmentInput) (*huma.StreamResponse, error) {
	att, err := a.db.GetAttachment(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get attachment", err)
	}
	if att == nil {
		return nil, huma.Error404NotFound("attachment not found")
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", att.ContentType)
			ctx.SetHeader("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": att.Filename}))
			ctx.SetHeader("X-Content-Type-Options", "nosniff")
			ctx.SetStatus(200)
			writer := ctx.BodyWriter()
			if _, err := writer.Write(att.Content); err != nil {
				a.logger.Error("failed to write attachment content", "error", err)
			}
		},
	}, nil
}
