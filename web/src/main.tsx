import React from "react";
import ReactDOM from "react-dom/client";

import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "@fontsource/ibm-plex-mono/600.css";

// Alternate font set ("inter" in src/lib/themes.ts). @font-face only — the
// browser fetches the files lazily when the families are actually used.
import "@fontsource/inter/400.css";
import "@fontsource/inter/500.css";
import "@fontsource/inter/600.css";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@fontsource/jetbrains-mono/600.css";

import "@grewelltech/console/tokens.css";
import "@grewelltech/console/base.css";
import "./styles/themes.css";
import "./styles/app.css";

import { ThemeProvider } from "./state/ThemeProvider";
import { AppProvider } from "./state/AppStore";
import { AuthGate } from "./components/auth/AuthGate";
import { OnboardingProvider } from "./components/onboarding/OnboardingProvider";
import App from "./App";

// The gate lives outside AppProvider so the store mounts fresh per space:
// keying on the space id tears the whole app down on switch and boots it
// from that space's server data.
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ThemeProvider>
      <AuthGate>
        {(spaceId, data) => (
          <AppProvider key={spaceId} spaceId={spaceId} initialData={data}>
            <OnboardingProvider>
              <App />
            </OnboardingProvider>
          </AppProvider>
        )}
      </AuthGate>
    </ThemeProvider>
  </React.StrictMode>
);
