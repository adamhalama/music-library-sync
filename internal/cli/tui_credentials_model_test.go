package cli

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/auth"
)

func TestCredentialsSecretEditReplacesExistingValue(t *testing.T) {
	model := tuiCredentialsModel{}
	model.edit = &tuiCredentialsEditState{
		Kind:          auth.CredentialKindDeemixARL,
		Field:         "deemix_arl",
		Title:         "Deezer ARL",
		ExistingValue: "old-secret",
		MaskInput:     true,
	}

	next, _ := model.updateEdit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new-secret")})
	if next.edit.Buffer != "new-secret" {
		t.Fatalf("expected pasted secret to replace the empty edit buffer, got %q", next.edit.Buffer)
	}
}

func TestCredentialsARLEditStartsEmpty(t *testing.T) {
	t.Setenv("UDL_DEEMIX_ARL", "existing-secret")
	model := tuiCredentialsModel{}
	model.startEditForCard(tuiCredentialsCard{Kind: auth.CredentialKindDeemixARL})

	if model.edit == nil {
		t.Fatal("expected credential edit state")
	}
	if model.edit.Buffer != "" {
		t.Fatalf("expected secret edit buffer to start empty, got %q", model.edit.Buffer)
	}
	if model.edit.Cursor != 0 {
		t.Fatalf("expected cursor at start, got %d", model.edit.Cursor)
	}
}

func TestOnboardingSecretEditKeepsExistingValueSeparate(t *testing.T) {
	model := tuiOnboardingModel{}
	model.startEdit("deemix_arl", "Deezer ARL", "old-secret", nil)

	if model.edit.Buffer != "" {
		t.Fatalf("expected secret edit buffer to start empty, got %q", model.edit.Buffer)
	}
	if model.edit.ExistingValue != "old-secret" {
		t.Fatalf("expected existing secret to be preserved separately, got %q", model.edit.ExistingValue)
	}

	model.applyEdit()
	if model.deemixARL != "old-secret" {
		t.Fatalf("expected blank submit to preserve existing secret, got %q", model.deemixARL)
	}
}
