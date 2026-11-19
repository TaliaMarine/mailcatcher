package api

import (
	"context"

	"github.com/TaliaMarine/mailcatcher/internal/auth"
	"github.com/danielgtaylor/huma/v2"
)

type UserInfo struct {
	Email             string `json:"email" doc:"User email address"`
	Name              string `json:"name" doc:"User display name"`
	PreferredUsername string `json:"preferred_username" doc:"User preferred username"`
	CanSignOut        bool   `json:"can_sign_out" doc:"Whether the session can be ended from the UI (OIDC sign-in)"`
}

type GetMeOutput struct {
	Body UserInfo
}

func (a *API) getMe(ctx context.Context, _ *struct{}) (*GetMeOutput, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("authentication required")
	}

	return &GetMeOutput{
		Body: UserInfo{
			Email:             u.Email,
			Name:              u.Name,
			PreferredUsername: u.PreferredUsername,
			CanSignOut:        !a.cfg.AuthDisabled && a.cfg.OIDCIssuer != "",
		},
	}, nil
}
