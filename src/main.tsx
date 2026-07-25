import React from "react";
import ReactDOM from "react-dom/client";

import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "@fontsource/ibm-plex-mono/600.css";

import "@grewelltech/console/tokens.css";
import "@grewelltech/console/base.css";
import "./styles/themes.css";
import "./styles/app.css";

import { ThemeProvider } from "./state/ThemeProvider";
import { AppProvider } from "./state/AppStore";
import { OnboardingProvider } from "./components/onboarding/OnboardingProvider";
import App from "./App";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ThemeProvider>
      <AppProvider>
        <OnboardingProvider>
          <App />
        </OnboardingProvider>
      </AppProvider>
    </ThemeProvider>
  </React.StrictMode>
);
