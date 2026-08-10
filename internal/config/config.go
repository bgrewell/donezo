// Package config resolves donezod runtime configuration. Values come from
// CLI flags (with environment-variable fallbacks handled by the CLI layer)
// over the defaults defined here.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/llm"
	"github.com/bgrewell/donezo/internal/notify"
)

// DefaultPort is the default HTTP listen port.
const DefaultPort = 8787

// Environment variable names honored as flag fallbacks by the CLI.
const (
	// EnvPort overrides --port.
	EnvPort = "DONEZOD_PORT"
	// EnvDataDir overrides --data-dir.
	EnvDataDir = "DONEZOD_DATA_DIR"
	// EnvSeed overrides --seed.
	EnvSeed = "DONEZOD_SEED"
	// EnvTrustProxy overrides --trust-proxy.
	EnvTrustProxy = "DONEZOD_TRUST_PROXY"
	// EnvHideVersion overrides --hide-version.
	EnvHideVersion = "DONEZOD_HIDE_VERSION"
	// EnvTimezone overrides --timezone.
	EnvTimezone = "DONEZOD_TIMEZONE"
	// EnvTrashRetentionDays overrides --trash-retention-days.
	EnvTrashRetentionDays = "DONEZOD_TRASH_RETENTION_DAYS"
	// EnvDevAutoLogin overrides --dev-auto-login.
	EnvDevAutoLogin = "DONEZOD_DEV_AUTO_LOGIN"
	// EnvLLMProvider overrides --llm-provider.
	EnvLLMProvider = "DONEZOD_LLM_PROVIDER"
	// EnvLLMBaseURL overrides --llm-base-url.
	EnvLLMBaseURL = "DONEZOD_LLM_BASE_URL"
	// EnvLLMModel overrides --llm-model.
	EnvLLMModel = "DONEZOD_LLM_MODEL"
	// EnvLLMAPIKey supplies the model API key. It is environment-only and
	// has no flag: a key passed as an argument is visible in the process
	// list to every user on the host.
	EnvLLMAPIKey = "DONEZOD_LLM_API_KEY"

	// EnvSMTPHost overrides --smtp-host.
	EnvSMTPHost = "DONEZOD_SMTP_HOST"
	// EnvSMTPPort overrides --smtp-port.
	EnvSMTPPort = "DONEZOD_SMTP_PORT"
	// EnvSMTPUsername overrides --smtp-username.
	EnvSMTPUsername = "DONEZOD_SMTP_USERNAME"
	// EnvSMTPPassword supplies the relay password. Environment-only, for the
	// same reason as EnvLLMAPIKey.
	EnvSMTPPassword = "DONEZOD_SMTP_PASSWORD"
	// EnvSMTPFrom overrides --smtp-from.
	EnvSMTPFrom = "DONEZOD_SMTP_FROM"
	// EnvSMTPFromName overrides --smtp-from-name.
	EnvSMTPFromName = "DONEZOD_SMTP_FROM_NAME"
	// EnvSMTPSecurity overrides --smtp-security.
	EnvSMTPSecurity = "DONEZOD_SMTP_SECURITY"

	// EnvTwilioAccountSID overrides --twilio-account-sid.
	EnvTwilioAccountSID = "DONEZOD_TWILIO_ACCOUNT_SID"
	// EnvTwilioAuthToken supplies the Twilio auth token. Environment-only.
	EnvTwilioAuthToken = "DONEZOD_TWILIO_AUTH_TOKEN"
	// EnvTwilioFrom overrides --twilio-from.
	EnvTwilioFrom = "DONEZOD_TWILIO_FROM"

	// EnvPublicURL overrides --public-url.
	EnvPublicURL = "DONEZOD_PUBLIC_URL"
	// EnvReminderMaxLatenessHours overrides --reminder-max-lateness-hours.
	EnvReminderMaxLatenessHours = "DONEZOD_REMINDER_MAX_LATENESS_HOURS"
)

// EnvDevAutoLoginConsent must be set to exactly "1" for --dev-auto-login
// to be accepted with a data dir outside /tmp. It is a deliberate
// speed bump: dev auto-login disables authentication entirely.
const EnvDevAutoLoginConsent = "DONEZOD_I_KNOW_WHAT_IM_DOING"

// Config is the resolved donezod runtime configuration.
type Config struct {
	// Port is the HTTP listen port.
	Port int
	// DataDir holds core.db and the spaces/ directory.
	DataDir string
	// SeedPath, when non-empty, is a seed.json to import before serving.
	SeedPath string
	// TrustProxy declares a trusted reverse proxy directly in front of
	// donezod: rate limiting keys on the last X-Forwarded-For hop (the
	// one that proxy appended) instead of the socket address, and
	// X-Forwarded-Proto: https marks session cookies Secure. Leave it
	// off when clients can reach donezod directly — both headers are
	// then attacker-controlled and are ignored.
	TrustProxy bool

	// HideVersion stops the running version being reported to the web UI,
	// which otherwise shows it in the nav rail. Useful once an instance is
	// stable and public: knowing the exact build is of more use to somebody
	// probing it than to the people using it.
	HideVersion bool

	// Timezone is the IANA zone name in which calendar days are resolved for
	// a user who has no timezone of their own — someone who has only ever
	// connected over MCP, and so has never had a browser report one.
	//
	// Empty means the host's own zone, which is right for the usual case of
	// one person running donezod where they are. Set it when the host runs
	// somewhere else, as a container almost always does: a UTC container
	// would otherwise put an evening's work on tomorrow.
	Timezone string

	// TrashRetentionDays is how long a deleted item stays restorable before
	// it is purged for good. 0 disables the sweep, leaving the trash to be
	// emptied by hand — the escape hatch for anyone who would rather nothing
	// ever disappeared on a timer.
	TrashRetentionDays int
	// DevAutoLogin disables authentication and attributes every request
	// to the seeded dev user. It exists purely for frontend development
	// and tests; Validate refuses it unless DataDir is under /tmp or
	// EnvDevAutoLoginConsent is set to "1".
	DevAutoLogin bool
	// LLMProvider selects the optional language-model provider
	// ("anthropic" or "openai-compatible"). Empty leaves model features
	// switched off, which is the default and a fully supported state.
	LLMProvider string
	// LLMBaseURL is the model endpoint. Required for openai-compatible —
	// that is how a local runtime is reached; optional for anthropic,
	// where it points at a gateway instead of the default API.
	LLMBaseURL string
	// LLMModel names the model to call.
	LLMModel string
	// LLMAPIKey authenticates upstream. Supplied only through the
	// environment (see EnvLLMAPIKey) and never persisted: donezo has no
	// storage for a recoverable secret, and this way it needs none.
	LLMAPIKey string

	// SMTP* configure email delivery for reminders. SMTPHost empty leaves
	// email switched off, which is the default and fully supported.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	// SMTPPassword is supplied only through the environment
	// (see EnvSMTPPassword), like every other credential here.
	SMTPPassword string
	SMTPFrom     string
	SMTPFromName string
	// SMTPSecurity is "starttls" (default), "tls" or "none".
	SMTPSecurity string

	// Twilio* configure SMS delivery. TwilioAccountSID empty leaves SMS
	// switched off.
	TwilioAccountSID string
	// TwilioAuthToken is supplied only through the environment
	// (see EnvTwilioAuthToken).
	TwilioAuthToken string
	TwilioFrom      string

	// PublicURL is where this instance is reachable, used to link back from
	// a delivered reminder. Empty means the notification carries no link,
	// which is correct but less useful — a reminder that cannot be opened
	// has to be re-found by hand.
	PublicURL string

	// ReminderMaxLatenessHours bounds how overdue a reminder may be and
	// still be delivered. It matters after downtime: without it, an
	// instance that was off for a week comes back and sends every reminder
	// it missed at once, at whatever hour it happened to start. 0 disables
	// the bound, delivering everything however late.
	ReminderMaxLatenessHours int
}

// DefaultReminderMaxLatenessHours is how overdue a reminder may be and still
// be sent. A day is the useful line: a reminder from this morning is still
// worth having when the server comes back this evening, and one from last
// week is noise that arrives with no context.
const DefaultReminderMaxLatenessHours = 24

// DefaultDataDir returns the XDG data directory for donezo:
// $XDG_DATA_HOME/donezo when set, otherwise ~/.local/share/donezo.
func DefaultDataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "donezo"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "donezo"), nil
}

// Validate checks the configuration for values that can never work, and
// refuses the dangerous ones. DevAutoLogin is only accepted for
// throwaway data dirs under /tmp, unless EnvDevAutoLoginConsent=1 is
// set in the environment (read here, at validation time).
func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: port %d out of range 1-65535", c.Port)
	}
	if c.DataDir == "" {
		return errors.New("config: data dir is required")
	}
	if _, err := c.Location(); err != nil {
		return err
	}
	if c.TrashRetentionDays < 0 {
		return fmt.Errorf("config: trash retention %d days is negative; use 0 to disable the sweep (%s)",
			c.TrashRetentionDays, EnvTrashRetentionDays)
	}
	if err := c.validateLLM(); err != nil {
		return err
	}
	if err := c.validateNotify(); err != nil {
		return err
	}
	if c.DevAutoLogin && !underTmp(c.DataDir) && os.Getenv(EnvDevAutoLoginConsent) != "1" {
		return fmt.Errorf(
			"config: --dev-auto-login disables authentication and is refused for data dir %s; use a --data-dir under /tmp, or set %s=1 if you really mean it",
			c.DataDir, EnvDevAutoLoginConsent)
	}
	return nil
}

// Location resolves Timezone, defaulting to the host's zone when it is empty.
//
// An unusable name fails at startup rather than being discovered later: the
// symptom of falling back silently would be dates that are quietly a day out,
// which nobody reads as a configuration error.
func (c Config) Location() (*time.Location, error) {
	if c.Timezone == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, fmt.Errorf(
			"config: unknown timezone %q — use an IANA name such as America/Los_Angeles (%s)",
			c.Timezone, EnvTimezone)
	}
	return loc, nil
}

// TrashRetention is TrashRetentionDays as a duration. Zero disables the sweep.
func (c Config) TrashRetention() time.Duration {
	return time.Duration(c.TrashRetentionDays) * 24 * time.Hour
}

// validateLLM checks the optional model configuration. No provider is a
// valid, fully supported configuration; a provider that cannot possibly
// work is refused at startup rather than on every request.
func (c Config) validateLLM() error {
	if c.LLMProvider == "" {
		// Nothing configured. Naming a model or endpoint without a
		// provider is a typo worth catching — silently ignoring it would
		// leave the operator believing the feature is on.
		if c.LLMBaseURL != "" || c.LLMModel != "" || c.LLMAPIKey != "" {
			return fmt.Errorf("config: %s is required when any other %s* value is set",
				EnvLLMProvider, "DONEZOD_LLM_")
		}
		return nil
	}
	switch c.LLMProvider {
	case llm.ProviderAnthropic:
		if c.LLMAPIKey == "" {
			return fmt.Errorf("config: the %s provider needs %s", llm.ProviderAnthropic, EnvLLMAPIKey)
		}
	case llm.ProviderOpenAICompatible:
		if c.LLMBaseURL == "" {
			return fmt.Errorf("config: the %s provider needs %s (for example http://localhost:11434/v1)",
				llm.ProviderOpenAICompatible, EnvLLMBaseURL)
		}
		if c.LLMModel == "" {
			return fmt.Errorf("config: the %s provider needs %s", llm.ProviderOpenAICompatible, EnvLLMModel)
		}
	default:
		return fmt.Errorf("config: unknown LLM provider %q (want one of %s)",
			c.LLMProvider, strings.Join(llm.Providers, ", "))
	}
	return nil
}

// validateNotify checks the optional delivery channels. Nothing configured
// is valid and is the default; a channel that is half-configured is refused
// at startup, because the alternative is an operator who believes reminders
// are being delivered and finds out otherwise the first time one matters.
func (c Config) validateNotify() error {
	if err := c.validateSMTP(); err != nil {
		return err
	}
	if err := c.validateTwilio(); err != nil {
		return err
	}
	if c.ReminderMaxLatenessHours < 0 {
		return fmt.Errorf("config: reminder max lateness %d hours is negative; use 0 to deliver however late (%s)",
			c.ReminderMaxLatenessHours, EnvReminderMaxLatenessHours)
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("config: public URL %q must be absolute, like https://donezo.example.com (%s)",
				c.PublicURL, EnvPublicURL)
		}
	}
	return nil
}

// validateSMTP refuses an email configuration that cannot deliver.
func (c Config) validateSMTP() error {
	set := c.SMTPPort != 0 || c.SMTPUsername != "" || c.SMTPPassword != "" ||
		c.SMTPFrom != "" || c.SMTPFromName != "" || c.SMTPSecurity != ""
	if c.SMTPHost == "" {
		if set {
			return fmt.Errorf("config: %s is required when any other DONEZOD_SMTP_* value is set", EnvSMTPHost)
		}
		return nil
	}
	if c.SMTPPort < 1 || c.SMTPPort > 65535 {
		return fmt.Errorf("config: smtp port %d out of range 1-65535 (%s)", c.SMTPPort, EnvSMTPPort)
	}
	if c.SMTPFrom == "" {
		return fmt.Errorf("config: email delivery needs a sender address (%s)", EnvSMTPFrom)
	}
	if err := notify.ValidateAddress(notify.ChannelEmail, c.SMTPFrom); err != nil {
		return fmt.Errorf("config: %s: %w", EnvSMTPFrom, err)
	}
	if c.SMTPUsername != "" && c.SMTPPassword == "" {
		return fmt.Errorf("config: %s is set without %s", EnvSMTPUsername, EnvSMTPPassword)
	}
	switch notify.SMTPSecurity(c.SMTPSecurity) {
	case "", notify.SMTPStartTLS, notify.SMTPImplicitTLS, notify.SMTPNone:
	default:
		return fmt.Errorf("config: unknown smtp security %q (want starttls, tls or none) (%s)",
			c.SMTPSecurity, EnvSMTPSecurity)
	}
	return nil
}

// validateTwilio refuses an SMS configuration that cannot deliver.
func (c Config) validateTwilio() error {
	if c.TwilioAccountSID == "" {
		if c.TwilioAuthToken != "" || c.TwilioFrom != "" {
			return fmt.Errorf("config: %s is required when any other DONEZOD_TWILIO_* value is set",
				EnvTwilioAccountSID)
		}
		return nil
	}
	if c.TwilioAuthToken == "" {
		return fmt.Errorf("config: SMS delivery needs %s", EnvTwilioAuthToken)
	}
	if c.TwilioFrom == "" {
		return fmt.Errorf("config: SMS delivery needs a sending number (%s)", EnvTwilioFrom)
	}
	if _, err := notify.NewTwilioSender(notify.TwilioConfig{
		AccountSID: c.TwilioAccountSID,
		AuthToken:  c.TwilioAuthToken,
		From:       c.TwilioFrom,
	}); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// Senders builds the configured delivery channels. An unconfigured channel
// is simply absent from the registry; a misconfigured one is an error,
// which Validate has already reported at startup.
func (c Config) Senders() (*notify.Registry, error) {
	var senders []notify.Sender
	if c.SMTPHost != "" {
		s, err := notify.NewSMTPSender(notify.SMTPConfig{
			Host:     c.SMTPHost,
			Port:     c.SMTPPort,
			Username: c.SMTPUsername,
			Password: c.SMTPPassword,
			From:     c.SMTPFrom,
			FromName: c.SMTPFromName,
			Security: notify.SMTPSecurity(c.SMTPSecurity),
		})
		if err != nil {
			return nil, err
		}
		senders = append(senders, s)
	}
	if c.TwilioAccountSID != "" {
		s, err := notify.NewTwilioSender(notify.TwilioConfig{
			AccountSID: c.TwilioAccountSID,
			AuthToken:  c.TwilioAuthToken,
			From:       c.TwilioFrom,
		})
		if err != nil {
			return nil, err
		}
		senders = append(senders, s)
	}
	return notify.NewRegistry(senders...), nil
}

// ReminderMaxLateness is ReminderMaxLatenessHours as a duration. Zero means
// no bound.
func (c Config) ReminderMaxLateness() time.Duration {
	return time.Duration(c.ReminderMaxLatenessHours) * time.Hour
}

// underTmp reports whether dir resolves to /tmp or below.
func underTmp(dir string) bool {
	clean := filepath.Clean(dir)
	return clean == "/tmp" || strings.HasPrefix(clean, "/tmp"+string(filepath.Separator))
}
