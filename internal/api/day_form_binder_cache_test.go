package api

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestHandlerDerivesTheDayFormBindingKeyOnce(t *testing.T) {
	t.Parallel()

	handler := &Handler{secretKey: []byte("day-form-binder-cache-secret-0123456789")}
	if handler.dayFormBinder() != handler.dayFormBinder() {
		t.Fatal("a handler must reuse one binder, not derive the key on every call")
	}

	user := &models.User{ID: 9}
	binding, err := handler.dayFormAccountBinding(user)
	if err != nil {
		t.Fatalf("bind account: %v", err)
	}
	if dayFormBindingRefused(handler.dayFormBinder(), user, [][]byte{[]byte(binding)}, true) {
		t.Fatal("the account's own binding must not be refused")
	}
	other := &models.User{ID: 10}
	if !dayFormBindingRefused(handler.dayFormBinder(), other, [][]byte{[]byte(binding)}, true) {
		t.Fatal("another account's binding must be refused")
	}
}
