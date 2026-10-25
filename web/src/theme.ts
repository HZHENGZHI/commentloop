import githubLight from "github-markdown-css/github-markdown-light.css?url";
import githubDark from "github-markdown-css/github-markdown-dark.css?url";
import type { Theme } from "./types";

const THEME_KEY = "commentloop:theme";

export function currentTheme(): Theme {
  return document.documentElement.dataset.theme === "dark" ? "dark" : "light";
}

function githubLink(theme: Theme): HTMLLinkElement {
  let link = document.getElementById("commentloop-github") as HTMLLinkElement | null;
  if (!link) {
    link = document.createElement("link");
    link.id = "commentloop-github";
    link.rel = "stylesheet";
    document.head.appendChild(link);
  }
  link.href = theme === "dark" ? githubDark : githubLight;
  return link;
}

export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
  try {
    localStorage.setItem(THEME_KEY, theme);
  } catch {
    // Ignore storage restrictions.
  }
  githubLink(theme);
  const highlight = document.getElementById(
    "commentloop-highlight",
  ) as HTMLLinkElement | null;
  if (highlight) {
    highlight.href = `/api/highlight.css?theme=${theme}`;
  }
}

// Apply the GitHub Markdown stylesheet as early as possible.
githubLink(currentTheme());
