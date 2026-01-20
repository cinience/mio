package types

import (
	"context"
	"log/slog"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/domain/meetingminutes"
	ttsdomain "backend-server/internal/domain/tts"

	"github.com/cloudwego/eino/schema"
)

// Connection represents the connection context passed to function tools
// This interface abstracts the connection object from the Python implementation
type Connection interface {
	// GetRemoteAddr returns the remote address ip:port
	GetRemoteAddr() string

	GetDeviceID() string

	GetMetadata() map[string]string

	// GetAudioFormat returns the preferred downstream audio format (sample rate, frame duration, codec)
	GetAudioFormat() (sampleRate int, frameDuration int, format string)

	// GetUserAudio returns recent user audio payload with its content type.
	// Empty payload means no available audio sample.
	GetUserAudio() ([]byte, string)

	// StreamAudio pushes an audio stream to the connected device through the TTS channel.
	// It returns a channel that will receive a terminal error (nil on success) once streaming completes.
	StreamAudio(ctx context.Context, description string, audioChan chan []byte) (<-chan error, error)

	// GetLoop returns the event loop (for async operations)
	//GetLoop() interface{}

	// GetLogger returns the logger instance
	GetLogger() *slog.Logger

	// GetDialogue returns the dialogue context
	//GetDialogue() Dialogue

	// GetPrompt returns the current prompt
	//GetPrompt() string

	// SetPrompt sets the current prompt
	//SetPrompt(prompt string)

	// ChangeSystemPrompt changes the system prompt
	ChangeSystemPrompt(prompt string) error

	TemporaryChangeVoice(context.Context, *ttsdomain.ChangeVoiceOptions) error

	// SetCloseAfterChat sets whether to close after chat
	SetCloseAfterChat(close bool, text string)

	// GetLastNewsLink returns the last news link (for news functions)
	//GetLastNewsLink() map[string]interface{}

	// SetLastNewsLink sets the last news link
	SetLastNewsLink(link map[string]interface{})

	// GetManagerAPIService exposes the manager-api integration for tools that need it.
	GetManagerAPIService() manager_api.ManagerAPIService

	// StartMeetingMinutes enables meeting minutes mode for the current session.
	StartMeetingMinutes() error
	// StopMeetingMinutes ends meeting minutes mode and returns the buffered snapshot.
	StopMeetingMinutes() (*meetingminutes.Snapshot, error)
}

// Dialogue represents the dialogue context
type Dialogue interface {
	UpdateSystemMessage(message string) error
}

// FunctionTool represents a function tool that can be called by LLM
type FunctionTool interface {
	// GetInfo returns the tool information for LLM (eino schema)
	GetInfo() *schema.ToolInfo

	// Execute runs the function with given arguments
	Execute(ctx context.Context, conn Connection, args map[string]interface{}) (*ActionResponse, error)

	// GetType returns the tool type
	GetType() ToolType

	// GetName returns the function name
	GetName() string

	// GetDescription returns the function description
	GetDescription() interface{}
}

// FunctionItem represents a registered function item (equivalent to Python's FunctionItem)
type FunctionItem struct {
	Name        string       `json:"name"`
	Description interface{}  `json:"description"`
	Function    FunctionTool `json:"-"` // Don't serialize the function itself
	Type        ToolType     `json:"type"`
}

// NewFunctionItem creates a new FunctionItem
func NewFunctionItem(name string, description interface{}, function FunctionTool, toolType ToolType) *FunctionItem {
	return &FunctionItem{
		Name:        name,
		Description: description,
		Function:    function,
		Type:        toolType,
	}
}

// GetToolInfo returns the eino ToolInfo for this function
func (fi *FunctionItem) GetToolInfo() *schema.ToolInfo {
	if fi.Function != nil {
		return fi.Function.GetInfo()
	}
	return nil
}
