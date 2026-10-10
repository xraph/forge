import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Workbench } from "./Workbench";
import "./styles.css";
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Workbench />
  </StrictMode>,
);
