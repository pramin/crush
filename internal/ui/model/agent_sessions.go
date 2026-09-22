package model

import (
	"context"
	"fmt"
	"image"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
)

var activeAgentViewCheck func() time.Time = func() func() time.Time {
	now := time.Now
	return func() time.Time {
		return now().Add(-30 * time.Minute)
	}
}()

// agentSessionsInfo renders the agent sessions section showing the root
// session and active sub-agents spawned from the current session.
func (m *UI) agentSessionsInfo(width int, isSection bool) string {
	t := m.com.Styles

	title := t.Resource.Heading.Render("Agent Sessions")
	if isSection {
		title = common.Section(t, title, width)
	}

	views := m.getAgentViews()
	if len(views) == 0 {
		list := t.Resource.AdditionalText.Render("None")
		return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, list))
	}

	// Deduplicate by session ID and filter to only active sessions.
	seen := map[string]bool{}
	var active []session.AgentView
	for _, v := range views {
		if seen[v.SessionID] {
			continue
		}
		seen[v.SessionID] = true

		// Skip reported/completed sessions.
		if v.IsReported {
			continue
		}

		// Keep only recently active sessions.
		if time.Unix(v.UpdatedAt, 0).After(activeAgentViewCheck()) {
			active = append(active, v)
		}
	}

	// Sort: root first, then by most recent update.
	if len(active) > 0 {
		slices.SortFunc(active, func(a, b session.AgentView) int {
			if a.IsRoot && !b.IsRoot {
				return -1
			}
			if !a.IsRoot && b.IsRoot {
				return 1
			}
			return int(b.UpdatedAt - a.UpdatedAt)
		})
	}

	if len(active) == 0 {
		return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, t.Resource.AdditionalText.Render("None")))
	}

	var lines []string
	for _, v := range active {
		var icon string
		switch v.Status {
		case "error":
			icon = t.Resource.ErrorIcon.Render("●")
		case "success":
			icon = t.Resource.OnlineIcon.Render("●")
		default:
			if v.IsRunning {
				icon = t.Resource.OnlineIcon.Render("●")
			} else {
				icon = t.Resource.AdditionalText.Render("●")
			}
		}
		lines = append(lines, fmt.Sprintf("%s %s", icon, v.Title))
	}

	list := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, list))
}

// getAgentViews returns the cached agent sessions view list.
func (m *UI) getAgentViews() []session.AgentView {
	if m.session == nil {
		return nil
	}
	return m.agentViews
}

// dispatchAgentViewsRefresh schedules an off-thread refresh of the
// agent sessions list when the current session has sub-agents.
func (m *UI) dispatchAgentViewsRefresh() tea.Cmd {
	if m.session == nil || m.session.ParentSessionID != "" {
		return nil
	}
	// Already in flight; nothing to do.
	if m.agentViewsInFlight {
		return nil
	}
	m.agentViewsInFlight = true
	m.agentViewsGen++
	gen := m.agentViewsGen
	return func() tea.Msg {
		views, err := m.com.Workspace.ListAgentSessions(context.Background(), m.session.ID)
		if err != nil {
			return agentViewsMsg{err: err}
		}
		slices.SortFunc(views, func(a, b session.AgentView) int {
			// Active sessions first.
			if a.IsRunning != b.IsRunning {
				if a.IsRunning {
					return -1
				}
				return 1
			}
			// Then by most recent update.
			switch {
			case a.UpdatedAt > b.UpdatedAt:
				return -1
			case a.UpdatedAt < b.UpdatedAt:
				return 1
			default:
				return 0
			}
		})
		// Enrich with run completion status from in-memory tracking.
		for i := range views {
			if status, ok := m.runCompletionStatuses[views[i].SessionID]; ok {
				if views[i].IsReported {
					// Session is reported; preserve completion status.
					views[i].Status = status
				} else {
					// Session is not yet reported; only set status if
					// it was previously recorded (e.g., session was
					// reported but completion arrived late).
					views[i].Status = ""
				}
			}
		}
		m.agentViews = views
		m.agentViewsChecked = time.Now()
		m.agentViewsInFlight = false
		return agentViewsMsg{views: views, err: nil, gen: gen}
	}
}

// invalidateAgentViews clears the cached agent sessions so they are
// refreshed on the next sidebar render.
func (m *UI) invalidateAgentViews() {
	m.agentViews = nil
	m.agentViewsChecked = time.Time{}
}

// agentViewsMsg is a message indicating that agent sessions have
// been refreshed.
type agentViewsMsg struct {
	views []session.AgentView
	err   error
	gen   uint64 // generation to discard stale results
}

// handleSidebarSessionClick handles mouse clicks on agent session items
// in the sidebar, switching to the clicked session.
func (m *UI) handleSidebarSessionClick(msg tea.MouseClickMsg) tea.Cmd {
	if !image.Pt(msg.X, msg.Y).In(m.layout.sidebar) {
		return nil
	}

	views := m.getAgentViews()
	if len(views) == 0 {
		return nil
	}

	// Compute the y-offset within the sidebar content (after logo).
	logoHeight := lipgloss.Height(m.sidebarDrawLogo)
	contentY := msg.Y - m.layout.sidebar.Min.Y - logoHeight
	if contentY < 0 || contentY >= m.layout.sidebar.Dy() {
		return nil
	}

	// Parse the sidebar content to find agent session items.
	lines := strings.Split(m.sidebarContent, "\n")

	// Find which agent session was clicked by matching lines against views.
	clickedIdx := -1
	for i, line := range lines {
		// Check if this line contains a visible dot and is within our content area.
		if strings.Contains(line, "●") && int(contentY) >= i && int(contentY) < i+1 {
			// This line is within the clicked row range.
			// Check if it's an agent session line (contains session title).
			for idx := range views {
				if strings.Contains(line, views[idx].Title) {
					clickedIdx = idx
					break
				}
			}
		}
	}

	if clickedIdx < 0 || clickedIdx >= len(views) {
		return nil
	}

	// Switch to the clicked session.
	sessionID := views[clickedIdx].SessionID
	m.loadSession(sessionID)
	return nil
}

// cycleAgentSessions returns a tea.Cmd that cycles to the next agent session
// in the active agent sessions list. It returns nil if there are no agent
// sessions to cycle through.
func (m *UI) cycleAgentSessions() tea.Cmd {
	if !m.hasSession() {
		return nil
	}
	views := m.getAgentViews()
	if len(views) == 0 {
		return nil
	}
	// Deduplicate by session ID and filter to only active sessions.
	seen := map[string]bool{}
	var active []session.AgentView
	for _, v := range views {
		if seen[v.SessionID] {
			continue
		}
		seen[v.SessionID] = true
		if v.IsRoot {
			continue
		}
		if v.IsReported {
			continue
		}
		if time.Unix(v.UpdatedAt, 0).After(activeAgentViewCheck()) {
			active = append(active, v)
		}
	}
	// Also include root for cycling.
	var allSessions []string
	for _, v := range views {
		if v.IsRoot {
			allSessions = append(allSessions, v.SessionID)
		}
	}
	for _, v := range active {
		allSessions = append(allSessions, v.SessionID)
	}
	if len(allSessions) <= 1 {
		return nil
	}
	// Find current session index and cycle.
	currentIdx := -1
	for i, sid := range allSessions {
		if sid == m.session.ID {
			currentIdx = i
			break
		}
	}
	nextIdx := (currentIdx + 1) % len(allSessions)
	if nextIdx < 0 {
		nextIdx = 0
	}
	return m.loadSession(allSessions[nextIdx])
}


