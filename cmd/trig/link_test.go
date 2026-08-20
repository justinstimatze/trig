package main

import "testing"

func TestParseTicketFlagArgs(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantTicket  string
		wantFlagKey string
		wantDryRun  bool
	}{
		{
			name:        "ticket and flag key",
			args:        []string{"CUR-515", "agent-mode"},
			wantTicket:  "CUR-515",
			wantFlagKey: "agent-mode",
		},
		{
			name:        "trailing dry-run flag",
			args:        []string{"CUR-515", "agent-mode", "--dry-run"},
			wantTicket:  "CUR-515",
			wantFlagKey: "agent-mode",
			wantDryRun:  true,
		},
		{
			name:        "leading flag before positionals",
			args:        []string{"--dry-run", "CUR-515", "agent-mode"},
			wantTicket:  "CUR-515",
			wantFlagKey: "agent-mode",
			wantDryRun:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ticketID, flagKey, dryRun := parseTicketFlagArgs(tc.args, "usage: trig link TICKET-ID FLAG-KEY [--dry-run]\n")
			if ticketID != tc.wantTicket || flagKey != tc.wantFlagKey || dryRun != tc.wantDryRun {
				t.Errorf("parseTicketFlagArgs(%v) = (%q, %q, %v), want (%q, %q, %v)",
					tc.args, ticketID, flagKey, dryRun, tc.wantTicket, tc.wantFlagKey, tc.wantDryRun)
			}
		})
	}
}
