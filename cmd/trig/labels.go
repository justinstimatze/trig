package main

import "github.com/justinstimatze/trig/internal/linear"

// ensureLabelApplied makes sure issue has a label named name, creating the
// workspace label first if it doesn't exist yet. No-op if already applied.
func ensureLabelApplied(c *linear.Client, issue *linear.Issue, name string) error {
	for _, l := range issue.Labels {
		if l.Name == name {
			return nil
		}
	}
	label, err := c.GetLabelByName(name)
	if err != nil {
		label, err = c.CreateLabel(name)
		if err != nil {
			return err
		}
	}
	return c.AddLabel(issue.ID, label.ID)
}

// ensureLabelRemoved makes sure issue does NOT have a label named name.
// No-op if it isn't present (including if the label doesn't exist at all).
func ensureLabelRemoved(c *linear.Client, issue *linear.Issue, name string) error {
	for _, l := range issue.Labels {
		if l.Name == name {
			return c.RemoveLabel(issue.ID, l.ID)
		}
	}
	return nil
}
