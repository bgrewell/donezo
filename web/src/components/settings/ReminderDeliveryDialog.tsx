import * as React from "react";
import { Button, Dialog, Input, Select, cn } from "@grewelltech/console";
import { Check, Mail, MessageSquare } from "lucide-react";

import {
  ApiError,
  createNotifyContact,
  deleteNotifyContact,
  fetchNotifyContacts,
  fetchNotifyStatus,
  sendNotifyContactCode,
  verifyNotifyContact,
  type NotifyChannel,
  type NotifyChannelStatus,
  type NotifyContact,
} from "@/api/client";
import { useSession } from "@/components/auth/session";
import { relativeFromInstant } from "@/lib/time";

/** How each channel introduces itself in the form. */
const CHANNELS: {
  value: NotifyChannel;
  label: string;
  placeholder: string;
  hint: string;
}[] = [
  {
    value: "email",
    label: "Email",
    placeholder: "you@example.com",
    hint: "The address reminders are sent to.",
  },
  {
    value: "sms",
    label: "Text message",
    placeholder: "+15551234567",
    hint: "International format — a + and the country code, no spaces or dashes.",
  },
];

/** Channel glyph for a destination row. */
function ChannelIcon({ channel }: { channel: NotifyChannel }) {
  const Icon = channel === "email" ? Mail : MessageSquare;
  return <Icon className="h-3.5 w-3.5 shrink-0 text-gtc-muted" aria-hidden />;
}

/**
 * Where this user's reminders are delivered.
 *
 * The shape of the screen follows the one rule that makes the feature safe:
 * a destination is added unverified and stays undeliverable until a code
 * sent to it comes back. So every row is either "waiting for its code" or
 * "verified", and there is no third state where something might be sent.
 *
 * Which channels exist at all is the operator's decision (environment
 * variables, not a settings screen), so an unconfigured channel says so
 * rather than letting somebody add a number that would never be texted.
 */
export function ReminderDeliveryDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { sessionExpired } = useSession();

  const [status, setStatus] = React.useState<NotifyChannelStatus[] | null>(null);
  const [contacts, setContacts] = React.useState<NotifyContact[] | null>(null);
  const [listError, setListError] = React.useState<string | null>(null);

  const [channel, setChannel] = React.useState<NotifyChannel>("email");
  const [address, setAddress] = React.useState("");
  const [label, setLabel] = React.useState("");
  const [adding, setAdding] = React.useState(false);
  const [addError, setAddError] = React.useState<string | null>(null);
  const [notice, setNotice] = React.useState<string | null>(null);

  // Code entry is per row: two destinations can be waiting at once.
  const [codes, setCodes] = React.useState<Record<string, string>>({});
  const [rowError, setRowError] = React.useState<Record<string, string>>({});
  const [busyId, setBusyId] = React.useState<string | null>(null);
  const [confirmRemoveId, setConfirmRemoveId] = React.useState<string | null>(null);

  const errorText = (err: unknown) => {
    if (err instanceof ApiError && err.status === 401) sessionExpired();
    if (err instanceof ApiError && err.status === 0) {
      return "can't reach the server — try again in a moment";
    }
    return err instanceof Error ? err.message : String(err);
  };

  const refresh = React.useCallback(async () => {
    try {
      const [channels, list] = await Promise.all([fetchNotifyStatus(), fetchNotifyContacts()]);
      setStatus(channels);
      setContacts(list);
      setListError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) sessionExpired();
      setListError(err instanceof Error ? err.message : String(err));
    }
  }, [sessionExpired]);

  // Fresh state per open, like every other settings dialog.
  React.useEffect(() => {
    if (open) {
      void refresh();
      return;
    }
    setStatus(null);
    setContacts(null);
    setListError(null);
    setAddress("");
    setLabel("");
    setAddError(null);
    setNotice(null);
    setCodes({});
    setRowError({});
    setBusyId(null);
    setConfirmRemoveId(null);
    setAdding(false);
  }, [open, refresh]);

  const configured = (c: NotifyChannel) => status?.find((s) => s.channel === c)?.configured ?? false;
  const anyConfigured = status?.some((s) => s.configured) ?? false;
  const current = CHANNELS.find((c) => c.value === channel) ?? CHANNELS[0];

  const add = async () => {
    if (adding || !address.trim()) return;
    setAdding(true);
    setAddError(null);
    setNotice(null);
    try {
      const created = await createNotifyContact(channel, address.trim(), label.trim());
      setAddress("");
      setLabel("");
      setNotice(
        created.warning ??
          `Code sent to ${created.contact.address}. Enter it below to start delivering here.`
      );
      await refresh();
    } catch (err) {
      setAddError(errorText(err));
    } finally {
      setAdding(false);
    }
  };

  const verify = async (contact: NotifyContact) => {
    const code = (codes[contact.id] ?? "").trim();
    if (!code || busyId) return;
    setBusyId(contact.id);
    setRowError((prev) => ({ ...prev, [contact.id]: "" }));
    try {
      await verifyNotifyContact(contact.id, code);
      setCodes((prev) => ({ ...prev, [contact.id]: "" }));
      setNotice(`${contact.address} is confirmed — reminders will be delivered there.`);
      await refresh();
    } catch (err) {
      setRowError((prev) => ({ ...prev, [contact.id]: errorText(err) }));
    } finally {
      setBusyId(null);
    }
  };

  const resend = async (contact: NotifyContact) => {
    if (busyId) return;
    setBusyId(contact.id);
    setRowError((prev) => ({ ...prev, [contact.id]: "" }));
    try {
      await sendNotifyContactCode(contact.id);
      setNotice(`New code sent to ${contact.address}.`);
      await refresh();
    } catch (err) {
      setRowError((prev) => ({ ...prev, [contact.id]: errorText(err) }));
    } finally {
      setBusyId(null);
    }
  };

  const remove = async (contact: NotifyContact) => {
    setBusyId(contact.id);
    setRowError((prev) => ({ ...prev, [contact.id]: "" }));
    try {
      await deleteNotifyContact(contact.id);
      setConfirmRemoveId(null);
      await refresh();
    } catch (err) {
      setRowError((prev) => ({ ...prev, [contact.id]: errorText(err) }));
    } finally {
      setBusyId(null);
    }
  };

  const contactRow = (contact: NotifyContact) => {
    const verified = Boolean(contact.verifiedAt);
    const confirming = confirmRemoveId === contact.id;
    const error = rowError[contact.id];
    return (
      <li key={contact.id} className="space-y-1.5 rounded-gtc px-2 py-1.5">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <ChannelIcon channel={contact.channel} />
          <span className="font-sans text-[0.82rem] text-gtc-text">{contact.address}</span>
          {contact.label && (
            <span className="rounded-gtc border border-gtc-line px-1.5 py-[2px] font-mono text-[0.6rem] uppercase tracking-label text-gtc-muted">
              {contact.label}
            </span>
          )}
          {verified ? (
            <span className="flex items-center gap-1 font-mono text-[0.62rem] uppercase tracking-label text-gtc-success">
              <Check className="h-3 w-3" aria-hidden />
              confirmed {relativeFromInstant(contact.verifiedAt!)}
            </span>
          ) : (
            <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-warn">
              waiting for its code
            </span>
          )}
          <span className="flex-1" />
          {confirming ? (
            <span className="flex items-center gap-1.5">
              <Button
                variant="danger"
                size="sm"
                noGlyph
                disabled={busyId === contact.id}
                onClick={() => void remove(contact)}
              >
                Remove
              </Button>
              <Button variant="ghost" size="sm" noGlyph onClick={() => setConfirmRemoveId(null)}>
                Keep
              </Button>
            </span>
          ) : (
            <Button
              variant="ghost"
              size="sm"
              noGlyph
              onClick={() => setConfirmRemoveId(contact.id)}
              aria-label={`Remove ${contact.address}`}
            >
              Remove
            </Button>
          )}
        </div>

        {!verified && (
          <div className="flex flex-wrap items-center gap-2 pl-6">
            <Input
              value={codes[contact.id] ?? ""}
              onChange={(e) => setCodes((prev) => ({ ...prev, [contact.id]: e.target.value }))}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  void verify(contact);
                }
              }}
              aria-label={`Verification code for ${contact.address}`}
              placeholder="6-digit code"
              inputMode="numeric"
              className="w-[140px] !py-1 !text-[0.78rem]"
            />
            <Button
              size="sm"
              variant="primary"
              noGlyph
              disabled={busyId === contact.id || !(codes[contact.id] ?? "").trim()}
              onClick={() => void verify(contact)}
            >
              Confirm
            </Button>
            <Button
              size="sm"
              variant="ghost"
              noGlyph
              disabled={busyId === contact.id}
              onClick={() => void resend(contact)}
            >
              Resend
            </Button>
          </div>
        )}
        {error && (
          <p className="m-0 pl-6 font-mono text-[0.66rem] text-gtc-danger" role="alert">
            ▸ {error}
          </p>
        )}
      </li>
    );
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Where reminders reach you"
      maxWidthClassName="max-w-xl"
    >
      <div className="space-y-4">
        <p className="m-0 text-[0.85rem] text-gtc-muted">
          Reminders show up in donezo. Add an address or number and they will find you when you
          are not looking at it.
        </p>

        {status !== null && !anyConfigured && (
          <p
            className="m-0 rounded-gtc border border-gtc-line bg-gtc-inset px-2.5 py-2 text-[0.8rem] text-gtc-muted"
            role="status"
          >
            This instance has no delivery set up yet, so reminders stay in the app. Email and SMS
            are configured by whoever runs donezod — see{" "}
            <a
              href="https://github.com/bgrewell/donezo/blob/main/docs/install.md#reminder-delivery"
              target="_blank"
              rel="noreferrer"
              className="font-mono text-gtc-accent underline-offset-2 hover:underline focus-visible:underline focus-visible:outline-none"
            >
              docs/install.md
            </a>
            .
          </p>
        )}

        {anyConfigured && (
          <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="w-[150px]">
                <Select
                  value={channel}
                  onChange={(e) => {
                    setChannel(e.target.value as NotifyChannel);
                    setAddError(null);
                  }}
                  aria-label="Delivery channel"
                  className="!py-1 !text-[0.7rem] uppercase tracking-label"
                >
                  {CHANNELS.map((c) => (
                    <option key={c.value} value={c.value} disabled={!configured(c.value)}>
                      {c.label}
                      {configured(c.value) ? "" : " (not set up)"}
                    </option>
                  ))}
                </Select>
              </span>
              <Input
                value={address}
                onChange={(e) => setAddress(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    void add();
                  }
                }}
                aria-label="Address or number"
                placeholder={current.placeholder}
                className="w-[240px] !py-1 !font-sans !text-[0.82rem] normal-case"
              />
              <Input
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    void add();
                  }
                }}
                aria-label="Label (optional)"
                placeholder="label"
                className="w-[110px] !py-1 !font-sans !text-[0.82rem] normal-case"
              />
              <Button
                size="sm"
                variant="primary"
                noGlyph
                disabled={adding || !address.trim() || !configured(channel)}
                onClick={() => void add()}
              >
                {adding ? "Adding…" : "Add"}
              </Button>
            </div>
            <p className="m-0 text-[0.72rem] text-gtc-muted">{current.hint}</p>
            {addError && (
              <p className="m-0 font-mono text-[0.66rem] text-gtc-danger" role="alert">
                ▸ {addError}
              </p>
            )}
            {notice && (
              <p className="m-0 font-mono text-[0.66rem] text-gtc-accent" role="status">
                ▸ {notice}
              </p>
            )}
          </div>
        )}

        <div>
          <div className="pb-1.5 font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
            Your destinations
          </div>
          {listError ? (
            <p className="m-0 font-mono text-[0.66rem] text-gtc-danger" role="alert">
              ▸ {listError}
            </p>
          ) : contacts === null ? (
            <p className="m-0 font-mono text-[0.72rem] text-gtc-muted">loading…</p>
          ) : contacts.length === 0 ? (
            <p className="m-0 text-[0.85rem] text-gtc-muted">
              Nothing yet. Reminders stay in donezo until you add one.
            </p>
          ) : (
            <ul className={cn("m-0 max-h-72 list-none space-y-1 overflow-y-auto p-0")}>
              {contacts.map(contactRow)}
            </ul>
          )}
        </div>
      </div>
    </Dialog>
  );
}
