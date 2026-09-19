package profileui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

func (m *Model) View(width, height int) string {
	if m.ModalActive() {
		return m.formView(width, height)
	}
	frame := m.listFrame(width, height)
	if len(m.list.Items) == 0 {
		body := "\n  No saved profiles.\n\n  Log in on the host. Press s to save Default.\n  Press h to choose a harness."
		return frame.View(body)
	}
	// Shared viewports clamp their offsets while rendering. Render a copy so
	// View remains pure, including when called with a different terminal size.
	list := m.list
	list.Width, list.Height = width, max(1, frame.BodyHeight()-1)
	list.ClampScroll()
	columns := "  HARNESS     PROFILE          STATE               ACCOUNT"
	if width < 75 {
		columns = "  HARNESS    PROFILE            STATE"
	}
	body := theme.DimStyle.Render(columns) + "\n" + list.View(renderProfile)
	return frame.View(body)
}

func (m *Model) listFrame(width, height int) components.FixedFrame {
	heading := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorAmber).Render("Profiles")
	header := heading + "  ·  Host save harness: " + m.harness
	if height >= 12 {
		header += "\nAccount state for Docker, VM, and the host. Live profiles change as you work."
	}
	foot := "s Save  ·  h Harness  ·  n New/load  ·  Enter Load  ·  d Delete  ·  i Details  ·  r Refresh"
	if width < 90 {
		foot = "s Save  ·  h Harness  ·  n New/load  ·  Enter Load\nd Delete  ·  i Details  ·  r Refresh"
	}
	if m.message != "" {
		style := lipgloss.NewStyle().Foreground(theme.ColorAmber)
		if m.failed {
			style = style.Foreground(theme.ColorDanger)
		}
		foot = style.Render(ansi.Truncate(m.message, width, "…")) + "\n" + foot
	}
	if m.busy {
		header += "  ·  Working…"
	}
	return components.FixedFrame{Header: header, Footer: foot, Width: width, Height: height}
}

func renderProfile(item components.ListItem, selected bool, width int) string {
	profile := item.Data.(profiles.Summary)
	state := "Saved"
	if profile.Managed {
		state = "Live"
	}
	if profile.Loaded {
		state = "Host"
		if profile.Managed {
			state = "Host · Live"
		}
	}
	if profile.Mixed {
		state = "Shared roots differ"
	}
	if profile.Mismatch {
		state = "Account mismatch"
	}
	if profile.Pending {
		state = "Login needed"
	}
	if profile.InUse {
		state += " · In use"
	}
	var row string
	if width < 75 {
		row = fmt.Sprintf("  %-10s %-18s %s", profile.Harness, ansi.Truncate(profile.Name, 18, "…"), state)
	} else {
		row = fmt.Sprintf("  %-11s %-16s %-19s %s", profile.Harness, ansi.Truncate(profile.Name, 16, "…"), ansi.Truncate(state, 19, "…"), profile.Account)
	}
	style := lipgloss.NewStyle().Foreground(theme.ColorTextPrimary)
	if selected {
		row = "› " + strings.TrimPrefix(row, "  ")
		style = style.Foreground(theme.ColorAmber).Background(theme.ColorOakLight).Bold(true)
	}
	return style.Width(width).Render(ansi.Truncate(row, width, "…"))
}

func (m *Model) formView(width, height int) string {
	if m.form.Kind == formDetails {
		return m.detailsView(width, height)
	}
	title, body, confirm := "Load or create profile", "Harness: "+m.harness+"\n\nProfile name: "+m.form.Name+"_\n\nA new name starts with empty state.\nThe current account is saved first.", "Load"
	switch m.form.Kind {
	case formName:
		title, confirm = "Name the current account", "Save"
		body = "This account has no saved profile.\nUse a new name. Existing names are protected.\n\nProfile name: " + m.form.Name + "_"
	case formDelete:
		title, confirm = "Delete saved profile", "Delete"
		body = "Delete " + m.form.Pending.Load.Harness + "/" + m.form.Pending.Load.Name + "?\n\nThis removes unused state. Required recovery must be kept."
	case formConflict:
		title, confirm = "Both copies changed", "h: Host / s: Saved"
		body = "The host and saved profile have different changes.\nBoth copies are preserved.\n\nPress h to keep host changes as the saved profile.\nPress s to keep the saved profile changes.\nEsc cancels."
	}
	if m.form.Error != "" {
		body += "\n\n" + m.form.Error
	}
	if m.form.Kind == formConflict {
		confirm = "h Keep host  ·  s Keep saved"
	} else {
		confirm = "Enter " + confirm
	}
	frame := components.FixedFrame{Header: title, Footer: confirm + "  ·  Esc Cancel", Width: width, Height: height}
	return frame.View(lipgloss.NewStyle().Width(max(1, width-4)).Padding(0, 2).Render(body))
}

func (m *Model) detailsView(width, height int) string {
	frame := m.detailsFrame(width, height)
	content := m.form.Content
	content.SetContent(ansi.Wrap(m.detailsText(), max(1, width-1), ""))
	return frame.View(content.View(width, frame.BodyHeight()))
}

func (m *Model) detailsText() string {
	body := m.message
	if selected := m.list.Selected(); selected != nil {
		profile := selected.Data.(profiles.Summary)
		body = fmt.Sprintf("%s / %s\nAccount: %s\nLast saved: %s\n\n%s", profile.Harness, profile.Name, profile.Account, profile.Saved.Format("2006-01-02 15:04 MST"), body)
		if profile.Managed {
			if profile.Mixed {
				body += "\nShared host roots differ. Reload this profile before save."
			}
			if profile.Mismatch {
				body += "\nAccount mismatch. Restore the original login or an independent backup."
			}
			if profile.Pending {
				body += "\nLogin needed on the host, then save this profile."
			}
			if profile.InUse {
				body += "\nState is in use. Stop its writers before changing profiles."
			}
			body += "\n\nLive profile: writes are saved as you work. Save checks the account.\nUse cooper profiles backup for an independent copy."
			for _, root := range profile.HostRoots {
				owner := root.Harness + "/" + root.Profile
				if root.Harness == "" {
					owner = "unmanaged"
				}
				body += "\nHost " + root.Path + " uses " + owner
			}
		} else {
			body += "\n\nCopy mode. Preview conversion with cooper profiles migrate --dry-run."
		}
	}
	return body
}

func (m *Model) detailsFrame(width, height int) components.FixedFrame {
	return components.FixedFrame{Header: "Profile details and last result", Footer: "↑/↓ Scroll  ·  Enter/Esc Close", Width: width, Height: height}
}

func (m *Model) layout() {
	m.list.Width = m.width
	m.list.Height = max(1, m.listFrame(m.width, m.height).BodyHeight()-1)
	m.list.ClampScroll()
	if m.form.Kind == formDetails {
		m.form.Content.SetContent(ansi.Wrap(m.detailsText(), max(1, m.width-1), ""))
	}
}
