/** The SMS consent disclosure shown at the point a person enters their mobile
 *  number.
 *
 *  This must stay verbatim identical to the Go constant `optInDisclosure`
 *  (internal/api/legal.go) — the public /sms-opt-in page and the campaign's
 *  Twilio message_flow both quote it, and a carrier reviewer compares what the
 *  opt-in screen actually shows against that quote. A paraphrase here is a
 *  vetting failure, not a style choice. */
export const SMS_OPT_IN_DISCLOSURE =
  "By entering your number you agree to receive a one-time verification code " +
  "and the reminder texts you schedule from donezo Reminders. Message " +
  "frequency varies. Message and data rates may apply. Reply STOP to cancel, " +
  "HELP for help. We do not share, sell, or provide your mobile phone number " +
  "or messaging consent data to third parties or affiliates for marketing or " +
  "promotional purposes.";
