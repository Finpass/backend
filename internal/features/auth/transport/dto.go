package transport

import (
	"time"

	"shenzen/backend/internal/features/auth/application"
	"shenzen/backend/internal/features/auth/domain"
)

type registerRequest struct {
	Name     string `json:"name" validate:"required,max=80"`
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

type emailRequest struct {
	Email string `json:"email" validate:"required,email,max=254"`
}

type verifyRequest struct {
	Email string `json:"email" validate:"required,email,max=254"`
	Code  string `json:"code" validate:"required,len=6,numeric"`
}

type loginRequest struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,max=72"`
	Remember bool   `json:"remember"`
}

type googleRequest struct {
	AccessToken string `json:"accessToken" validate:"required,max=4096"`
	Remember    bool   `json:"remember"`
}

type resetRequest struct {
	Email       string `json:"email" validate:"required,email,max=254"`
	Code        string `json:"code" validate:"required,len=6,numeric"`
	NewPassword string `json:"newPassword" validate:"required,min=8,max=72"`
}

type userResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type sessionResponse struct {
	User            userResponse `json:"user"`
	AccessToken     string       `json:"accessToken"`
	AccessExpiresAt time.Time    `json:"accessExpiresAt"`
}

type pendingResponse struct {
	Email string `json:"email"`
}

func toUser(u domain.User) userResponse { return userResponse{ID: u.ID, Email: u.Email, Name: u.Name} }

func toSession(r application.Result) sessionResponse {
	return sessionResponse{User: toUser(r.User), AccessToken: r.Tokens.Access, AccessExpiresAt: r.Tokens.AccessExpiresAt}
}

type deleteRequest struct {
	Password string `json:"password" validate:"max=72"` // required by the service when the account has a password
	Confirm  string `json:"confirm" validate:"required,eq=DELETE"`
}
