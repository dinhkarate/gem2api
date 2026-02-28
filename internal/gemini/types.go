package gemini

// SessionData holds bootstrap tokens extracted from gemini.google.com/app.
type SessionData struct {
	SNlM0e string // CSRF token (at= parameter)
	Cfb2h  string // Build label (bl= parameter)
	FdrFJe string // Session ID (f.sid= parameter)
}

// ModelInfo maps an OpenAI model name to Gemini web API header.
type ModelInfo struct {
	DisplayName string // Human-readable name
	HexID       string // Hex ID for x-goog-ext header (empty = default model)
	Advanced    bool   // Requires Gemini Advanced subscription
}

// Known models and their hex IDs (these change with Google updates).
// Last verified: Feb 28, 2026. See https://github.com/HanaokaYuzu/Gemini-API
//
// Google splits models into free (ai-free) and Advanced (ai-pro) tiers
// with different hex IDs. The "3.1" label only applies to Pro.
var KnownModels = map[string]ModelInfo{
	// === Default routing (no header needed) ===
	"gemini-2.0-flash": {
		DisplayName: "Gemini 2.0 Flash",
		HexID:       "",
	},
	"gemini-2.5-flash": {
		DisplayName: "Gemini 2.5 Flash",
		HexID:       "",
	},
	"gemini-2.5-pro": {
		DisplayName: "Gemini 2.5 Pro",
		HexID:       "",
	},

	// === Gemini 3 Free Tier ===
	"gemini-3-flash": {
		DisplayName: "Gemini 3 Flash",
		HexID:       "fbb127bbb056c959",
	},
	"gemini-3-flash-thinking": {
		DisplayName: "Gemini 3 Flash Thinking",
		HexID:       "5bf011840784117a",
	},
	"gemini-3-pro": {
		DisplayName: "Gemini 3 Pro",
		HexID:       "9d8ca3786ebdfbea",
	},

	// === Gemini 3 Advanced Tier (requires Gemini Advanced subscription) ===
	"gemini-3-flash-advanced": {
		DisplayName: "Gemini 3 Flash (Advanced)",
		HexID:       "56fdd199312815e2",
		Advanced:    true,
	},
	"gemini-3-flash-thinking-advanced": {
		DisplayName: "Gemini 3 Flash Thinking (Advanced)",
		HexID:       "e051ce1aa80aa576",
		Advanced:    true,
	},
	"gemini-3.1-pro": {
		DisplayName: "Gemini 3.1 Pro",
		HexID:       "e6fa609c3fa255c0",
		Advanced:    true,
	},
}

// ParsedFrame represents a single parsed response frame from Gemini web.
type ParsedFrame struct {
	Text           string // Response text (cumulative in snapshot streaming)
	Thoughts       string // Thinking model thoughts
	ConversationID string // cid for conversation continuity
	ResponseID     string // rid
	ChoiceID       string // rcid
	IsFinal        bool   // Whether this is the final frame
	ErrorCode      int    // Error code (0 = no error)
}
