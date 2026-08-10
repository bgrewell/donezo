package config

import (
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/notify"
)

// baseConfig is a configuration that validates, so each test can change one
// thing and see only that thing's effect.
func baseConfig() Config {
	return Config{Port: DefaultPort, DataDir: "/tmp/donezo-test"}
}

func TestValidateNotify(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "nothing configured is valid", mutate: func(*Config) {}},

		{
			name: "smtp complete",
			mutate: func(c *Config) {
				c.SMTPHost, c.SMTPPort, c.SMTPFrom = "smtp.example.com", 587, "donezo@example.com"
			},
		},
		{
			name: "smtp without a host but with other values",
			// The failure this catches is an operator who set the password
			// and the from address and believes email is on.
			mutate:  func(c *Config) { c.SMTPFrom = "donezo@example.com"; c.SMTPPort = 587 },
			wantErr: "DONEZOD_SMTP_HOST is required",
		},
		{
			name:    "smtp without a from address",
			mutate:  func(c *Config) { c.SMTPHost, c.SMTPPort = "smtp.example.com", 587 },
			wantErr: "needs a sender address",
		},
		{
			name: "smtp with a malformed from address",
			mutate: func(c *Config) {
				c.SMTPHost, c.SMTPPort, c.SMTPFrom = "smtp.example.com", 587, "donezo"
			},
			wantErr: "plain email address",
		},
		{
			name: "smtp port out of range",
			mutate: func(c *Config) {
				c.SMTPHost, c.SMTPPort, c.SMTPFrom = "smtp.example.com", 70000, "donezo@example.com"
			},
			wantErr: "out of range",
		},
		{
			name: "smtp username without a password",
			mutate: func(c *Config) {
				c.SMTPHost, c.SMTPPort, c.SMTPFrom = "smtp.example.com", 587, "donezo@example.com"
				c.SMTPUsername = "ben"
			},
			wantErr: "without DONEZOD_SMTP_PASSWORD",
		},
		{
			name: "unknown smtp security",
			mutate: func(c *Config) {
				c.SMTPHost, c.SMTPPort, c.SMTPFrom = "smtp.example.com", 587, "donezo@example.com"
				c.SMTPSecurity = "ssl"
			},
			wantErr: "unknown smtp security",
		},

		{
			name: "twilio complete",
			mutate: func(c *Config) {
				c.TwilioAccountSID = "AC00000000000000000000000000000001"
				c.TwilioAuthToken, c.TwilioFrom = "token", "+15550001111"
			},
		},
		{
			name:    "twilio token without a sid",
			mutate:  func(c *Config) { c.TwilioAuthToken = "token" },
			wantErr: "DONEZOD_TWILIO_ACCOUNT_SID is required",
		},
		{
			name:    "twilio sid without a token",
			mutate:  func(c *Config) { c.TwilioAccountSID = "AC0000000000000000000000000000001" },
			wantErr: "needs DONEZOD_TWILIO_AUTH_TOKEN",
		},
		{
			name: "twilio without a from number",
			mutate: func(c *Config) {
				c.TwilioAccountSID, c.TwilioAuthToken = "AC0000000000000000000000000000001", "token"
			},
			wantErr: "needs a sending number",
		},
		{
			name: "twilio from number not in E.164",
			mutate: func(c *Config) {
				c.TwilioAccountSID, c.TwilioAuthToken = "AC0000000000000000000000000000001", "token"
				c.TwilioFrom = "5550001111"
			},
			wantErr: "international format",
		},

		{name: "negative lateness", mutate: func(c *Config) { c.ReminderMaxLatenessHours = -1 }, wantErr: "negative"},
		{name: "zero lateness is allowed", mutate: func(c *Config) { c.ReminderMaxLatenessHours = 0 }},
		{name: "public url absolute", mutate: func(c *Config) { c.PublicURL = "https://donezo.example.com" }},
		{name: "public url relative", mutate: func(c *Config) { c.PublicURL = "donezo.example.com" }, wantErr: "must be absolute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSendersBuildsConfiguredChannelsOnly(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   map[notify.Channel]bool
	}{
		{
			name:   "nothing configured",
			mutate: func(*Config) {},
			want:   map[notify.Channel]bool{notify.ChannelEmail: false, notify.ChannelSMS: false},
		},
		{
			name: "email only",
			mutate: func(c *Config) {
				c.SMTPHost, c.SMTPPort, c.SMTPFrom = "smtp.example.com", 587, "donezo@example.com"
			},
			want: map[notify.Channel]bool{notify.ChannelEmail: true, notify.ChannelSMS: false},
		},
		{
			name: "both",
			mutate: func(c *Config) {
				c.SMTPHost, c.SMTPPort, c.SMTPFrom = "smtp.example.com", 587, "donezo@example.com"
				c.TwilioAccountSID = "AC00000000000000000000000000000001"
				c.TwilioAuthToken, c.TwilioFrom = "token", "+15550001111"
			},
			want: map[notify.Channel]bool{notify.ChannelEmail: true, notify.ChannelSMS: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			tt.mutate(&cfg)
			reg, err := cfg.Senders()
			if err != nil {
				t.Fatalf("Senders: %v", err)
			}
			for channel, want := range tt.want {
				if got := reg.Configured(channel); got != want {
					t.Fatalf("Configured(%s) = %v, want %v", channel, got, want)
				}
			}
		})
	}
}

// The credential convention: secrets arrive through the environment, so
// there must be no flag-shaped way to pass them. This asserts the constant
// names still say so, since the CLI reads them by name.
func TestSecretsAreEnvironmentOnly(t *testing.T) {
	for _, env := range []string{EnvSMTPPassword, EnvTwilioAuthToken, EnvLLMAPIKey} {
		if !strings.HasPrefix(env, "DONEZOD_") {
			t.Fatalf("%q is not a DONEZOD_ environment variable", env)
		}
	}
}
