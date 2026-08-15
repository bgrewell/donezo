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

// The regression guard for the v0.9.0 startup failure.
//
// A plain `donezod` with nothing configured refused to start, because the
// CLI's own flag defaults — port 587, starttls, a from-name — looked to
// validateSMTP like a half-configured email setup. Every existing test built
// a zero-valued Config, which is not the configuration the binary produces,
// so all of them passed while the shipped binary would not boot.
func TestCLIDefaultsValidate(t *testing.T) {
	if err := CLIDefaults("/tmp/donezo-test").Validate(); err != nil {
		t.Fatalf("a plain donezod run does not validate: %v", err)
	}
}

// And with nothing configured, no channel is built — email being off is the
// default, not an error.
func TestCLIDefaultsConfigureNoChannels(t *testing.T) {
	reg, err := CLIDefaults("/tmp/donezo-test").Senders()
	if err != nil {
		t.Fatalf("Senders: %v", err)
	}
	if reg.Any() {
		t.Fatal("a default run configured a delivery channel")
	}
}

// The check still earns its place: a password with no host is the mistake it
// was written for, and must still be refused.
func TestSMTPHalfConfiguredIsStillRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "password without host", mutate: func(c *Config) { c.SMTPPassword = "hunter2" }},
		{name: "from without host", mutate: func(c *Config) { c.SMTPFrom = "donezo@example.com" }},
		{name: "username without host", mutate: func(c *Config) { c.SMTPUsername = "ben" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CLIDefaults("/tmp/donezo-test")
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("half-configured email was accepted")
			}
			if !strings.Contains(err.Error(), EnvSMTPHost) {
				t.Fatalf("Validate = %v, want it to name %s", err, EnvSMTPHost)
			}
		})
	}
}

// ListenAddr must bind loopback under --trust-proxy (so an exposed port
// cannot bypass the proxy) and all interfaces otherwise, with an explicit
// --bind winning either way. This is finding #1's belt half.
func TestListenAddr(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "no proxy binds all interfaces", mutate: func(c *Config) {}, want: ":8787"},
		{name: "trust-proxy binds loopback", mutate: func(c *Config) { c.TrustProxy = true }, want: "127.0.0.1:8787"},
		{name: "explicit bind wins without proxy", mutate: func(c *Config) { c.BindAddress = "0.0.0.0" }, want: "0.0.0.0:8787"},
		{name: "explicit bind wins under proxy", mutate: func(c *Config) { c.TrustProxy = true; c.BindAddress = "10.0.0.5" }, want: "10.0.0.5:8787"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			tt.mutate(&cfg)
			if got := cfg.ListenAddr(); got != tt.want {
				t.Fatalf("ListenAddr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTrustedProxyPrefixes(t *testing.T) {
	cfg := baseConfig()
	cfg.TrustedProxyCIDRs = []string{"10.0.0.0/8", " 192.168.1.0/24 ", ""}
	got, err := cfg.TrustedProxyPrefixes()
	if err != nil {
		t.Fatalf("TrustedProxyPrefixes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d prefixes, want 2 (empty entry skipped)", len(got))
	}

	cfg.TrustedProxyCIDRs = []string{"not-a-cidr"}
	if _, err := cfg.TrustedProxyPrefixes(); err == nil {
		t.Fatal("TrustedProxyPrefixes accepted a non-CIDR")
	}
	// And Validate surfaces the same error at startup.
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted a bad trusted-proxy CIDR")
	}
}
