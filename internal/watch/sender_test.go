package watch

import "testing"

func TestComposeUsesCanonicalSenderFormat(t *testing.T) {
	now := watchTime()
	m := compose(composeAlert(now), nil, now)
	if !m.CanonicalSender {
		t.Fatal("new reminders must use the corrected sender format before their first send is frozen")
	}
}
