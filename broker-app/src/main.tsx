import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { DeskProvider } from "./hooks/useDesk";
import "./index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <DeskProvider>
      <App />
    </DeskProvider>
  </StrictMode>,
);
