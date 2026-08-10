import { SectionLabel, cn } from "@grewelltech/console";

import { useAppDispatch, useAppState } from "@/state/AppStore";
import { useSession } from "@/components/auth/session";
import { AppearanceSection } from "@/components/settings/AppearanceSection";
import { McpTokensSection } from "@/components/settings/McpTokensSection";
import { PolishPromptSection } from "@/components/settings/PolishPromptSection";
import { ReminderDeliverySection } from "@/components/settings/ReminderDeliverySection";
import { InvitesSection } from "@/components/admin/InvitesSection";
import { UsageSection } from "@/components/admin/UsageSection";
import { AboutSection } from "@/components/settings/AboutSection";

/** One entry in the settings rail.
 *
 *  `admin` is a rendering decision only. Every admin section's data comes
 *  from an endpoint that checks the role itself, so a member who reaches one
 *  of these another way gets a 403 from the server rather than a screen full
 *  of somebody else's figures. */
interface SettingsSection {
  id: string;
  label: string;
  blurb: string;
  admin?: boolean;
  Body: () => JSX.Element;
}

/** The sections, in the order they appear. Adding a setting means adding an
 *  entry here and a component — not a new dialog and a new entry point,
 *  which is how the account menu grew five of them. */
export const SETTINGS_SECTIONS: SettingsSection[] = [
  {
    id: "appearance",
    label: "Appearance",
    blurb: "Theme, typeface and text size. Stored with your account, so they follow you between machines.",
    Body: AppearanceSection,
  },
  {
    id: "reminders",
    label: "Reminders",
    blurb: "Where reminders reach you when you are not looking at donezo.",
    Body: ReminderDeliverySection,
  },
  {
    id: "ai",
    label: "AI",
    blurb: "How the model rewrites a quick capture, and the tokens that let one act as you.",
    Body: PolishPromptSection,
  },
  {
    id: "tokens",
    label: "MCP tokens",
    blurb: "Connect Claude or another MCP client. Tokens act as you — treat them like passwords.",
    Body: McpTokensSection,
  },
  {
    id: "invites",
    label: "Invites",
    blurb: "Who may create an account on this instance.",
    admin: true,
    Body: InvitesSection,
  },
  {
    id: "usage",
    label: "Usage",
    blurb: "What gets used and what does not — counts only, never anyone's content.",
    admin: true,
    Body: UsageSection,
  },
  {
    id: "about",
    label: "About",
    blurb: "What this instance is running, and what its operator has switched on.",
    Body: AboutSection,
  },
];

/** The default section when the route names none. */
const DEFAULT_SECTION = SETTINGS_SECTIONS[0].id;

/**
 * Settings: one home for everything that used to hang off the account menu
 * as its own dialog.
 *
 * Deliberately not in the nav rail — settings is not a place you work, it is
 * a place you visit from your account. It is still a real route
 * (`#/settings/<section>`), so a section can be linked to directly, which is
 * what makes it worth being a page rather than another dialog.
 */
export default function SettingsView() {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const { user } = useSession();
  const isAdmin = user?.role === "admin";

  const visible = SETTINGS_SECTIONS.filter((s) => !s.admin || isAdmin);
  const requested = state.settingsSection ?? DEFAULT_SECTION;
  // A member who lands on an admin section's URL (a bookmark from before a
  // role change, a shared link) gets the first section rather than an empty
  // screen. The server refuses the data either way.
  const active = visible.find((s) => s.id === requested) ?? visible[0];

  const select = (id: string) => dispatch({ type: "SET_SETTINGS_SECTION", section: id });

  return (
    <div className="mx-auto max-w-[1000px] px-4 py-6 sm:px-6 lg:px-8">
      <SectionLabel className="mb-1 mt-0">Settings</SectionLabel>
      <p className="mb-5 max-w-[70ch] font-sans text-[0.85rem] text-gtc-muted">
        Yours, and — if you run this instance — everyone's.
      </p>

      <div className="flex flex-col gap-6 md:flex-row md:gap-8">
        <nav aria-label="Settings sections" className="md:w-[180px] md:shrink-0">
          <ul className="m-0 flex list-none flex-wrap gap-1 p-0 md:flex-col md:flex-nowrap">
            {visible.map((section) => {
              const selected = section.id === active?.id;
              return (
                <li key={section.id}>
                  <button
                    type="button"
                    onClick={() => select(section.id)}
                    aria-current={selected ? "page" : undefined}
                    className={cn(
                      "w-full rounded-gtc px-2.5 py-1.5 text-left font-mono text-[0.68rem] uppercase tracking-label",
                      "outline-none transition-colors focus-visible:shadow-gtc-focus",
                      selected
                        ? "bg-gtc-tint-accent text-gtc-accent"
                        : "text-gtc-muted hover:text-gtc-text"
                    )}
                  >
                    {section.label}
                    {section.admin && (
                      <span className="ml-1.5 text-[0.9em] text-gtc-muted/70" title="Admin only">
                        ◆
                      </span>
                    )}
                  </button>
                </li>
              );
            })}
          </ul>
        </nav>

        <div className="min-w-0 flex-1">
          {active && (
            <>
              <h2 className="m-0 font-sans text-[1.05rem] font-semibold leading-none text-gtc-text">
                {active.label}
              </h2>
              <p className="mb-4 mt-1.5 max-w-[70ch] font-sans text-[0.85rem] text-gtc-muted">
                {active.blurb}
              </p>
              <active.Body />
            </>
          )}
        </div>
      </div>
    </div>
  );
}
