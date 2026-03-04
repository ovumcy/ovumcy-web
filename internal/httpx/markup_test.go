package httpx

import "testing"

func TestStatusErrorMarkupEscapesHTML(t *testing.T) {
	got := StatusErrorMarkup(`<script>alert("x")</script>`)
	want := `<div class="status-error">&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;</div>`
	if got != want {
		t.Fatalf("unexpected markup: got %q want %q", got, want)
	}
}

func TestDismissibleStatusOKMarkupEscapesHTML(t *testing.T) {
	got := DismissibleStatusOKMarkup(`<b>Saved</b>`, `<script>alert("x")</script>`)
	want := `<div class="status-ok"><div class="toast-body"><span class="toast-message-wrap"><span class="toast-icon" aria-hidden="true">✓</span><span class="toast-message">&lt;b&gt;Saved&lt;/b&gt;</span></span><button type="button" class="toast-close" data-dismiss-status aria-label="&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;">×</button></div></div>`
	if got != want {
		t.Fatalf("unexpected markup: got %q want %q", got, want)
	}
}
