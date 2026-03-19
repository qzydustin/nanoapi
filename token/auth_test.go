package token

import (
	"testing"

	"github.com/qzydustin/nanoapi/config"
)

func testService() *Service {
	return NewService([]config.TokenConfig{
		{ID: "tok_1", Name: "default", Key: "nk_test123"},
		{ID: "tok_2", Name: "other", Key: "nk_other456"},
	})
}

func TestAuthenticateConfiguredToken(t *testing.T) {
	svc := testService()

	ctx, err := svc.Authenticate("nk_test123")
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if ctx.TokenID != "tok_1" {
		t.Errorf("token_id = %q", ctx.TokenID)
	}
	if ctx.TokenName != "default" {
		t.Errorf("token_name = %q", ctx.TokenName)
	}
}

func TestAuthenticateInvalid(t *testing.T) {
	svc := testService()

	_, err := svc.Authenticate("nk_invalid_token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}
