package api

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/bgrewell/donezo/internal/llm"
	"github.com/bgrewell/donezo/internal/notify"
)

// The published privacy policy and terms (/privacy, /terms).
//
// These exist because a carrier will not approve an SMS program without
// them: Twilio's A2P registration asks for a direct link to each, and checks
// the terms for the program name, the rates and frequency wording, a support
// contact, and the HELP and STOP instructions.
//
// Everything party-specific is runtime configuration, not baked in. Whoever
// hosts donezo makes these promises, not whoever wrote it — so the operator's
// name and support address come from the environment, and with neither set
// the pages are not served at all. Publishing a policy that names the wrong
// party, or nobody, is worse than publishing none.
//
// The prose is deliberately narrow because donezo's messaging is narrow: the
// only messages it ever sends are ones the recipient scheduled for
// themselves, plus the one-time code that proves the destination is theirs.
// There is no marketing, no list, and nothing to sell — which is what makes
// the strong wording carriers look for honest here rather than aspirational.

// policyRevision is the date the wording below last changed.
//
// A constant rather than time.Now: a policy whose "last updated" moves every
// time somebody loads it tells the reader nothing, and a reviewer reads that
// date as the date the terms were set.
const policyRevision = "10 August 2026"

// programName is the messaging program as registered with the carrier. It
// must match what the operator files, so it is not configurable per instance.
const programName = "donezo Reminders"

// legalPage is everything the templates need.
type legalPage struct {
	Title        string
	OperatorName string
	SupportEmail string
	ProgramName  string
	Revision     string
	SiteURL      string
	// Integrations are the third-party services this instance actually
	// sends data to. Rendered from live configuration rather than assumed,
	// so the disclosure describes this deployment instead of the software's
	// full set of possibilities.
	Integrations []integration
	// OtherPolicyPath links the two documents to each other.
	OtherPolicyPath  string
	OtherPolicyTitle string
}

// integration is one third-party processor, named with what reaches it.
type integration struct {
	Name string
	What string
}

// integrations lists the processors this instance is configured to use.
func (s *Server) integrations() []integration {
	var out []integration
	if sender, ok := s.notifiers.Sender(notify.ChannelSMS); ok {
		_ = sender
		out = append(out, integration{
			Name: "Twilio",
			What: "your mobile number and the text of the reminder, so the message can be delivered to your phone",
		})
	}
	if _, ok := s.notifiers.Sender(notify.ChannelEmail); ok {
		out = append(out, integration{
			Name: "an email relay",
			What: "your email address and the text of the reminder, so the message can be delivered to your inbox",
		})
	}
	if _, off := s.llm.(llm.Disabled); !off {
		out = append(out, integration{
			Name: s.llm.Provider() + " (language model)",
			What: "the text of a note you explicitly ask to tidy up, at the moment you ask — never automatically, and never anything else",
		})
	}
	return out
}

// handlePrivacy serves the privacy policy.
func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	s.renderPolicy(w, privacyTemplate, legalPage{
		Title:            "Privacy Policy",
		OtherPolicyPath:  "/terms",
		OtherPolicyTitle: "Terms and Conditions",
	})
}

// handleTerms serves the terms and conditions.
func (s *Server) handleTerms(w http.ResponseWriter, r *http.Request) {
	s.renderPolicy(w, termsTemplate, legalPage{
		Title:            "Terms and Conditions",
		OtherPolicyPath:  "/privacy",
		OtherPolicyTitle: "Privacy Policy",
	})
}

// renderPolicy fills in the runtime values and writes the page.
//
// Both pages are public: a policy nobody can read without an account is not
// published, and the carrier reviewing it has no account.
func (s *Server) renderPolicy(w http.ResponseWriter, tmpl *template.Template, page legalPage) {
	if s.operatorName == "" || s.supportEmail == "" {
		// Not configured, so this instance publishes nothing. 404 rather
		// than an empty page: there is genuinely no document here.
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}
	page.OperatorName = s.operatorName
	page.SupportEmail = s.supportEmail
	page.ProgramName = programName
	page.Revision = policyRevision
	page.SiteURL = strings.TrimSuffix(s.publicURL, "/")
	page.Integrations = s.integrations()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// A policy is public and static; letting it be cached briefly keeps a
	// reviewer's repeat loads off the database entirely.
	w.Header().Set("Cache-Control", "public, max-age=300")
	if err := tmpl.Execute(w, page); err != nil {
		s.logger.Printf("render %s: %v", page.Title, err)
	}
}

// policyStyle is the shared chrome. Inline because these pages must render
// for somebody with no account, no JavaScript, and no interest in the app —
// including an automated reviewer.
const policyStyle = `
:root { color-scheme: light dark; }
body {
  margin: 0 auto; padding: 2.5rem 1.25rem 4rem; max-width: 46rem;
  font: 16px/1.65 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  color: #16191d; background: #fff;
}
@media (prefers-color-scheme: dark) { body { color: #dde3ea; background: #0f1216; } }
h1 { font-size: 1.6rem; margin: 0 0 .25rem; }
h2 { font-size: 1.1rem; margin: 2rem 0 .5rem; }
.meta { color: #6b7580; font-size: .85rem; margin: 0 0 2rem; }
a { color: #2f6feb; }
@media (prefers-color-scheme: dark) { a { color: #6ea8ff; } }
ul { padding-left: 1.25rem; }
li { margin: .35rem 0; }
strong { font-weight: 700; }
.box {
  border: 1px solid rgba(128,140,155,.35); border-radius: 6px;
  padding: .85rem 1rem; margin: 1.25rem 0;
}
footer { margin-top: 3rem; font-size: .85rem; color: #6b7580; }
`

// header is the shared page opening.
const policyHeader = `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} — {{.ProgramName}}</title>
<style>` + policyStyle + `</style>
</head><body>
<h1>{{.Title}}</h1>
<p class="meta">{{.ProgramName}}, operated by {{.OperatorName}}. Last updated {{.Revision}}.</p>
`

// footer closes both pages, cross-linking them.
const policyFooter = `
<footer>
  <p><a href="{{.OtherPolicyPath}}">{{.OtherPolicyTitle}}</a>
  {{if .SiteURL}}· <a href="{{.SiteURL}}">{{.SiteURL}}</a>{{end}}</p>
  <p>Questions about this document: <a href="mailto:{{.SupportEmail}}">{{.SupportEmail}}</a>.</p>
</footer>
</body></html>`

var privacyTemplate = template.Must(template.New("privacy").Parse(policyHeader + `
<p>{{.OperatorName}} operates this donezo instance. donezo is a personal
work-tracking tool: the notes, tasks, projects and reminders you write are
yours, and this policy explains what is stored, what it is used for, and who
else ever sees it.</p>

<h2>What is collected</h2>
<ul>
  <li><strong>Your account</strong> — the username and display name you choose,
  and your password, which is stored only as an irreversible hash.</li>
  <li><strong>What you write</strong> — the projects, activities, tasks, notes,
  reminders and captures you enter, and the timezone your browser reports so
  that dates land on the day you meant.</li>
  <li><strong>Where you asked to be reached</strong> — if you choose to have
  reminders delivered, the email address or mobile number you add, and a
  record that you confirmed it.</li>
  <li><strong>Technical records needed to run the service</strong> — a session
  cookie so you stay signed in, any API tokens you create, and server logs of
  requests. Logs record the request path, not what you wrote.</li>
</ul>

<h2>What it is used for</h2>
<p>Only to provide the service to you: to show you your own data, to sign you
in, and — if you set one up — to deliver a reminder you scheduled. It is not
used to build a profile, and it is not used for advertising.</p>

<div class="box">
  <p><strong>Your information is not sold, rented, or traded. No mobile
  information is shared with third parties or affiliates for marketing or
  promotional purposes.</strong> Text messaging originator opt-in data and
  consent are never shared with any third party for any purpose.</p>
</div>

<h2>Who else processes it</h2>
{{if .Integrations}}
<p>Delivering messages and running the service requires a small number of
service providers, which act only on {{.OperatorName}}'s instructions and only
for the purpose named:</p>
<ul>
  {{range .Integrations}}<li><strong>{{.Name}}</strong> receives {{.What}}.</li>
  {{end}}
</ul>
<p>These providers are processors, not recipients of a data sale. Nothing is
shared with them for marketing, and nothing else is shared with anyone.</p>
{{else}}
<p>This instance sends your data to no third-party service at all. It is
stored on the server that runs it and goes nowhere else.</p>
{{end}}

<h2>How long it is kept</h2>
<p>Your data stays until you delete it. Deleting moves an item to the trash,
where you can restore it; emptying the trash, or leaving it past the retention
window the operator has configured, removes it permanently. Closing your
account removes the account and everything in it.</p>

<h2>Your choices</h2>
<ul>
  <li>Delivery is opt-in. Nothing is sent to an address or number until you
  add it and confirm it with the code sent there.</li>
  <li>You can remove a delivery destination at any time under
  <em>Settings → Reminders</em>, which stops all messages to it.</li>
  <li>For a mobile number you can also reply <strong>STOP</strong> to any
  message. See the <a href="{{.OtherPolicyPath}}">Terms and Conditions</a>.</li>
  <li>To ask what is held about you, or to have your account and its contents
  deleted, email <a href="mailto:{{.SupportEmail}}">{{.SupportEmail}}</a>.</li>
</ul>

<h2>Security</h2>
<p>Passwords are stored as salted argon2id hashes and never in a recoverable
form. Session tokens and API tokens are stored only as hashes. Traffic to this
site is served over HTTPS. No system is perfect, and no promise of absolute
security is made here.</p>

<h2>Children</h2>
<p>This service is not directed to children under 13, and accounts are created
only by invitation.</p>

<h2>Changes</h2>
<p>If this policy changes, the date at the top changes with it. Material
changes affecting message delivery will be sent to the destinations you have
confirmed.</p>
` + policyFooter))

var termsTemplate = template.Must(template.New("terms").Parse(policyHeader + `
<p>These terms cover your use of this donezo instance, operated by
{{.OperatorName}}, and in particular its reminder messaging program.</p>

<h2>The messaging program</h2>
<ul>
  <li><strong>Program name:</strong> {{.ProgramName}}.</li>
  <li><strong>What it sends:</strong> reminders that you scheduled for
  yourself inside donezo, delivered by email or text message at the time you
  chose, plus a one-time verification code when you first add a destination.
  It sends nothing else — no marketing, no newsletters, no promotions.</li>
  <li><strong>How you join:</strong> you add your own email address or mobile
  number in <em>Settings → Reminders</em> and confirm it with the code sent
  there. That confirmation is your consent, and nothing is delivered to a
  destination that has not been confirmed.</li>
  <li><strong>Message frequency:</strong> varies — you receive a message only
  when a reminder you created comes due, so the number is entirely determined
  by you. Many users receive none in a given week.</li>
  <li><strong>Cost:</strong> donezo does not charge for messages.
  <strong>Message and data rates may apply</strong> from your mobile carrier.</li>
  <li><strong>Carriers:</strong> mobile carriers are not liable for delayed or
  undelivered messages.</li>
</ul>

<div class="box">
  <p>Text <strong>STOP</strong> to any message to cancel. You will receive a
  final confirmation, and no further text messages will be sent to that
  number. You can also remove the number under <em>Settings → Reminders</em>,
  which has the same effect.</p>
  <p>Text <strong>HELP</strong> for help, or email
  <a href="mailto:{{.SupportEmail}}">{{.SupportEmail}}</a>. Support is
  available by email at that address.</p>
</div>

<h2>Your account</h2>
<p>Accounts are created by invitation. Keep your password and any API tokens
you create private — a token acts as you. You are responsible for what is done
through your account, and for the lawfulness of what you store in it.</p>

<h2>Acceptable use</h2>
<p>Do not use this service to break the law, to store or send content you have
no right to, to attempt to reach another person's data, or to disrupt the
service for others. Do not use the reminder feature to send messages to a
number or address that is not yours.</p>

<h2>Your content</h2>
<p>What you write stays yours. {{.OperatorName}} claims no ownership of it and
uses it only to run the service for you, as described in the
<a href="{{.OtherPolicyPath}}">Privacy Policy</a>.</p>

<h2>Availability and liability</h2>
<p>The service is provided as is, without warranty of any kind. It may be
unavailable, and a reminder may be delayed or fail to arrive — do not rely on
it as the only safeguard for anything critical. To the extent the law allows,
{{.OperatorName}} is not liable for any indirect or consequential loss arising
from use of the service.</p>

<h2>Ending it</h2>
<p>You may stop using the service and have your account deleted at any time by
emailing <a href="mailto:{{.SupportEmail}}">{{.SupportEmail}}</a>.
{{.OperatorName}} may suspend an account that breaches these terms.</p>

<h2>Changes</h2>
<p>If these terms change, the date at the top changes with them.</p>
` + policyFooter))
