package subscriptions

import "testing"

func TestTransitions(t *testing.T) {
	legal := [][2]Status{
		{PendingPayment, Active}, {PendingPayment, Expired},
		{Active, Paused}, {Active, PauseScheduled}, {Active, PastDue}, {Active, CancelScheduled}, {Active, Cancelled},
		{PauseScheduled, Paused}, {Paused, Active}, {PastDue, Active}, {CancelScheduled, Active}, {CancelScheduled, Cancelled},
	}
	for _, c := range legal {
		if !CanTransition(c[0], c[1]) {
			t.Errorf("%s -> %s should be allowed", c[0], c[1])
		}
	}
	illegal := [][2]Status{
		{PendingPayment, Paused}, {PendingPayment, Cancelled}, {PendingPayment, PastDue},
		{Cancelled, Active}, {Expired, Active}, {Cancelled, Expired},
		{Paused, PastDue}, {PastDue, Paused}, {CancelScheduled, Paused}, {Active, Expired}, {Active, PendingPayment},
		{Active, Active},
	}
	for _, c := range illegal {
		if CanTransition(c[0], c[1]) {
			t.Errorf("%s -> %s should be refused", c[0], c[1])
		}
	}
	// Every status has an entry, and terminal ones lead nowhere.
	for _, s := range []Status{PendingPayment, Active, PauseScheduled, Paused, PastDue, CancelScheduled, Cancelled, Expired} {
		if _, ok := transitions[s]; !ok {
			t.Errorf("%s has no transition entry", s)
		}
		if s.Label() == string(s) {
			t.Errorf("%s has no label", s)
		}
	}
	if len(transitions[Cancelled]) != 0 || len(transitions[Expired]) != 0 {
		t.Error("terminal statuses must have no transitions")
	}
}
