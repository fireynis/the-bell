import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
// Self-hosted rather than fetched from Google Fonts: a municipal notice board
// should not wait on a third-party stylesheet before it can render its own
// text. Both are the variable cuts, so one file covers every weight in use.
import "@fontsource-variable/newsreader";
import "@fontsource-variable/plus-jakarta-sans";
import "./index.css";
import App from "./App";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
