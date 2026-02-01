import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@xyflow/react/dist/style.css";
import "./index.css";
import App from "./App.tsx";
import store from "./store.ts";
import { Provider } from "react-redux";

// Apply saved theme on initial load
const savedTheme = localStorage.getItem("theme") || "saas";
document.documentElement.setAttribute("data-theme", savedTheme);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Provider store={store}>
      <App />
    </Provider>
  </StrictMode>,
);
