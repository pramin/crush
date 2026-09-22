package tools

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/session"
)

//go:embed spawn.md
var spawnDescription string

type SpawnParams struct {
	Prompt   string `json:"prompt" description:"The prompt to run in the new session"`
	Title    string `json:"title" description:"Optional title for the new session"`
	ParentID string `json:"parent_id" description:"The parent session ID"`
}

const SpawnToolName = "spawn"

func NewSpawnTool(
	sessions session.Service,
	messages message.Service,
	permissions permission.Service,
	workingDir string,
) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SpawnToolName,
		spawnDescription,
		func(ctx context.Context, params SpawnParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Prompt == "" {
				return fantasy.NewTextErrorResponse("prompt is required"), nil
			}

			parentID := GetSessionFromContext(ctx)
			if parentID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session_id is required for spawning a sub-agent")
			}

			if params.ParentID != "" {
				parentID = params.ParentID
			}

			// Generate a unique session ID for the sub-agent
			sessionID := call.ID
			title := params.Title
			if title == "" {
				title = params.Prompt
				if len(title) > 80 {
					title = title[:80] + "..."
				}
			}

			// Create the sub-agent session
			_, err := sessions.CreateTaskSession(ctx, sessionID, parentID, title)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to create sub-agent session: %w", err)
			}

			// Add the initial user message
			textParts := []message.ContentPart{
				message.TextContent{Text: params.Prompt},
			}
			msg, err := messages.Create(ctx, sessionID, message.CreateMessageParams{
				Role:   message.User,
				Parts:  textParts,
			})
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to create initial message: %w", err)
			}

			// Flush the message to ensure it's persisted before returning
			if err := messages.Flush(ctx, msg.ID); err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to flush message: %w", err)
			}

			// Auto-approve permissions for the sub-agent (inherits yolo from parent)
			permissions.AutoApproveSession(sessionID)

			// Update parent session's updated_at
			parentSession, err := sessions.Get(ctx, parentID)
			if err == nil {
				parentSession.UpdatedAt = time.Now().Unix()
				if _, err := sessions.Save(ctx, parentSession); err != nil {
					// Non-fatal: log but don't fail the spawn
					_ = err
				}
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("Sub-agent session spawned: %s\nSession ID: %s", title, sessionID)),
				map[string]any{
					"session_id": sessionID,
					"title":      title,
				},
			), nil
		},
	)
}
