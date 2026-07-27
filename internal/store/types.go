package store

// The types below mirror web/src/domain/types.ts exactly. JSON field names
// are the frontend's camelCase names, and optional TS fields are pointers
// with omitempty so they are omitted when absent, matching TS optionals.
//
// CreatedAt/UpdatedAt on Project and ActivityEntry are server-side columns
// that do not exist in the frontend types; they are excluded from JSON.

// Roles a User can hold. Exactly one admin exists per instance in
// practice: first-run setup creates the owner as admin (and the roles
// migration backfills the first credentialed user on upgraded
// databases); everyone who joins through an invite code is a member.
const (
	// RoleAdmin may manage invites in addition to everything members do.
	RoleAdmin = "admin"
	// RoleMember is the default role for every non-owner account.
	RoleMember = "member"
)

// User is a row in core.db's users table.
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	// Role is RoleAdmin for the instance owner, RoleMember otherwise.
	Role string `json:"role"`
	// PasswordHash is empty until phase 2 introduces authentication.
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"createdAt"`
}

// Space is a row in core.db's spaces registry. The space's content lives
// in its own database file named after ID.
type Space struct {
	ID         string  `json:"id"`
	UserID     int64   `json:"userId"`
	Name       string  `json:"name"`
	Color      string  `json:"color"`
	Position   int     `json:"position"`
	ArchivedAt *string `json:"archivedAt,omitempty"`
	CreatedAt  string  `json:"createdAt"`
}

// Project mirrors the frontend Project type.
type Project struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Color          string   `json:"color"`
	Purpose        string   `json:"purpose"`
	Outcome        string   `json:"outcome"`
	CurrentFocus   string   `json:"currentFocus"`
	NextAction     string   `json:"nextAction"`
	AltNextActions []string `json:"altNextActions"`
	Status         string   `json:"status"`
	ResumeContext  string   `json:"resumeContext"`
	WaitingOn      *string  `json:"waitingOn,omitempty"`
	Tags           []string `json:"tags"`
	// Server-side timestamps; not part of the frontend type.
	CreatedAt string `json:"-"`
	UpdatedAt string `json:"-"`
}

// ActivityLink mirrors the frontend ActivityLink type.
type ActivityLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// ActivityEntry mirrors the frontend ActivityEntry type.
type ActivityEntry struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"projectId"`
	Date        string         `json:"date"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Details     string         `json:"details"`
	EffortHours *float64       `json:"effortHours,omitempty"`
	Source      string         `json:"source"`
	Tags        []string       `json:"tags"`
	Links       []ActivityLink `json:"links"`
	NextAction  *string        `json:"nextAction,omitempty"`
	Planned     *bool          `json:"planned,omitempty"`
	// Server-side timestamps; not part of the frontend type.
	CreatedAt string `json:"-"`
	UpdatedAt string `json:"-"`
}

// TaskItem mirrors the frontend TaskItem type.
type TaskItem struct {
	ID        string  `json:"id"`
	ProjectID *string `json:"projectId,omitempty"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`
	Due       *string `json:"due,omitempty"`
	WaitingOn *string `json:"waitingOn,omitempty"`
	CreatedAt string  `json:"createdAt"`
}

// NoteItem mirrors the frontend NoteItem type.
type NoteItem struct {
	ID        string  `json:"id"`
	ProjectID *string `json:"projectId,omitempty"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	CreatedAt string  `json:"createdAt"`
}

// Reminder mirrors the frontend Reminder type.
type Reminder struct {
	ID        string  `json:"id"`
	Text      string  `json:"text"`
	RemindAt  string  `json:"remindAt"`
	ProjectID *string `json:"projectId,omitempty"`
	Done      *bool   `json:"done,omitempty"`
}

// InboxItem mirrors the frontend InboxItem type.
type InboxItem struct {
	ID                 string  `json:"id"`
	Raw                string  `json:"raw"`
	CapturedAt         string  `json:"capturedAt"`
	SuggestedKind      string  `json:"suggestedKind"`
	SuggestedProjectID *string `json:"suggestedProjectId,omitempty"`
	Status             string  `json:"status"`
}

// SpaceState is the complete content of one space, as served by
// GET /api/spaces/{id}/state. Slices are always non-nil so empty
// collections marshal as [] rather than null.
type SpaceState struct {
	Projects   []Project       `json:"projects"`
	Activities []ActivityEntry `json:"activities"`
	Tasks      []TaskItem      `json:"tasks"`
	Notes      []NoteItem      `json:"notes"`
	Reminders  []Reminder      `json:"reminders"`
	Inbox      []InboxItem     `json:"inbox"`
}
