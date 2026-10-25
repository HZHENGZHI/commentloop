import i18n from "./i18n";
import type { Theme } from "./types";

type MermaidApi = typeof import("mermaid").default;

let mermaidPromise: Promise<MermaidApi> | null = null;
let activeModalCleanup: (() => void) | null = null;

function loadMermaid(): Promise<MermaidApi> {
  mermaidPromise ??= import("mermaid").then((mod) => mod.default);
  return mermaidPromise;
}

function mermaidTextColor(fill: string, theme: Theme): string {
  const fallback = theme === "dark" ? "#e6edf3" : "#1f2328";
  const value = fill.trim().toLowerCase();
  if (!value || value === "none" || value === "transparent") return fallback;
  const match = value.match(
    /rgba?\(\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)/,
  );
  if (!match) return fallback;
  const [, r, g, b] = match.map(Number);
  const luminance = (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
  return luminance > 0.56 ? "#111827" : "#e6edf3";
}

function mermaidBackground(el: Element, container: HTMLElement): string {
  let ancestor: Element | null = el.parentElement;
  while (ancestor && ancestor !== container) {
    const shape = ancestor.querySelector(
      ":scope > rect, :scope > polygon, :scope > circle, :scope > ellipse, :scope > path",
    );
    if (shape) {
      const rect = shape as SVGRectElement;
      if (
        shape.tagName.toLowerCase() === "rect" &&
        (!rect.getAttribute("width") ||
          !rect.getAttribute("height") ||
          Number(rect.getAttribute("width")) <= 0 ||
          Number(rect.getAttribute("height")) <= 0)
      ) {
        ancestor = ancestor.parentElement;
        continue;
      }
      const candidate = getComputedStyle(shape).fill;
      if (
        candidate &&
        candidate !== "none" &&
        candidate !== "transparent" &&
        !/rgba\(\s*0\s*,\s*0\s*,\s*0\s*,\s*0\s*\)/.test(candidate)
      ) {
        return candidate;
      }
    }
    ancestor = ancestor.parentElement;
  }
  return "";
}

function fixMermaidContrast(container: HTMLElement, theme: Theme) {
  container.querySelectorAll<SVGTextElement>("text").forEach((text) => {
    const color = mermaidTextColor(mermaidBackground(text, container), theme);
    text.style.setProperty("fill", color, "important");
    text.querySelectorAll("tspan").forEach((tspan) => {
      (tspan as SVGElement).style.setProperty("fill", color, "important");
    });
  });
  // Mermaid uses HTML labels (foreignObject) for some diagram types. Its
  // generated CSS sets `color` on span elements, so SVG text overrides do not
  // reach them.
  container.querySelectorAll<SVGForeignObjectElement>("foreignObject").forEach((foreign) => {
    const color = mermaidTextColor(mermaidBackground(foreign, container), theme);
    foreign.style.setProperty("color", color, "important");
    foreign.querySelectorAll("*").forEach((child) => {
      (child as HTMLElement).style.setProperty("color", color, "important");
    });
  });
}

function mermaidConfig(theme: Theme) {
  return {
    startOnLoad: false,
    securityLevel: "strict" as const,
    theme: "base" as const,
    fontFamily:
      '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    themeVariables:
      theme === "dark"
        ? {
            background: "#12161b",
            primaryColor: "#1d2838",
            primaryTextColor: "#e6edf3",
            primaryBorderColor: "#64748b",
            secondaryColor: "#20262e",
            secondaryTextColor: "#e6edf3",
            secondaryBorderColor: "#475569",
            tertiaryColor: "#171b21",
            tertiaryTextColor: "#e6edf3",
            lineColor: "#94a3b8",
            textColor: "#e6edf3",
            labelTextColor: "#e6edf3",
            loopTextColor: "#e6edf3",
            noteTextColor: "#e6edf3",
            noteBkgColor: "#1d2838",
            noteBorderColor: "#64748b",
            activationBkgColor: "#20262e",
            activationBorderColor: "#64748b",
            sequenceNumberColor: "#e6edf3",
            classText: "#e6edf3",
            stateTextColor: "#e6edf3",
            stateLabelColor: "#e6edf3",
            transitionLabelColor: "#cbd5e1",
            mainBkg: "#1d2838",
            nodeBorder: "#64748b",
            clusterBkg: "#111418",
            clusterBorder: "#334155",
            edgeLabelBackground: "transparent",
            titleColor: "#e6edf3",
            actorBkg: "#1d2838",
            actorBorder: "#64748b",
            actorTextColor: "#e6edf3",
            signalColor: "#94a3b8",
            signalTextColor: "#e6edf3",
          }
        : {
            background: "#ffffff",
            primaryColor: "#f3f2ff",
            primaryTextColor: "#1f2328",
            primaryBorderColor: "#b9b7f4",
            secondaryColor: "#f8f8fc",
            secondaryTextColor: "#1f2328",
            secondaryBorderColor: "#d5d3fa",
            tertiaryColor: "#fbfbfd",
            tertiaryTextColor: "#1f2328",
            lineColor: "#6b7280",
            textColor: "#1f2328",
            labelTextColor: "#1f2328",
            loopTextColor: "#1f2328",
            noteTextColor: "#1f2328",
            noteBkgColor: "#fafaff",
            noteBorderColor: "#d5d3fa",
            activationBkgColor: "#f8f8fc",
            activationBorderColor: "#b9b7f4",
            sequenceNumberColor: "#1f2328",
            classText: "#1f2328",
            stateTextColor: "#1f2328",
            stateLabelColor: "#1f2328",
            transitionLabelColor: "#374151",
            mainBkg: "#f3f2ff",
            nodeBorder: "#b9b7f4",
            clusterBkg: "#fafaff",
            clusterBorder: "#d5d3fa",
            edgeLabelBackground: "transparent",
            titleColor: "#1f2328",
            actorBkg: "#f3f2ff",
            actorBorder: "#b9b7f4",
            actorTextColor: "#1f2328",
            signalColor: "#6b7280",
            signalTextColor: "#1f2328",
          },
    flowchart: {
      htmlLabels: false,
      curve: "basis" as const,
      padding: 14,
      nodeSpacing: 34,
      rankSpacing: 46,
    },
  };
}

function updateToolbar(block: HTMLElement) {
  const open = block.querySelector<HTMLButtonElement>("[data-mermaid-open]");
  const expand = block.querySelector<HTMLButtonElement>("[data-mermaid-expand]");
  const source = block.querySelector<HTMLButtonElement>("[data-mermaid-source]");
  const pre = block.querySelector<HTMLPreElement>(".mermaid-source");
  if (open) open.textContent = i18n.t("查看大图");
  if (expand) {
    expand.textContent = block.classList.contains("is-expanded")
      ? i18n.t("收起")
      : i18n.t("展开");
  }
  if (source && pre) source.textContent = pre.hidden ? i18n.t("源码") : i18n.t("图表");
}

function createBlock(source: string): HTMLElement {
  const block = document.createElement("div");
  block.className = "mermaid-block";
  block.dataset.source = source;

  const toolbar = document.createElement("div");
  toolbar.className = "mermaid-toolbar";
  const label = document.createElement("span");
  label.className = "mermaid-label";
  label.textContent = "Mermaid";
  const actions = document.createElement("span");
  actions.className = "mermaid-actions";
  const open = document.createElement("button");
  open.type = "button";
  open.className = "mermaid-action";
  open.dataset.mermaidOpen = "1";
  open.textContent = i18n.t("查看大图");
  const expand = document.createElement("button");
  expand.type = "button";
  expand.className = "mermaid-action";
  expand.dataset.mermaidExpand = "1";
  expand.textContent = i18n.t("展开");
  expand.addEventListener("click", () => {
    const expanded = block.classList.toggle("is-expanded");
    expand.textContent = expanded ? i18n.t("收起") : i18n.t("展开");
    expand.setAttribute("aria-expanded", String(expanded));
    if (expanded) block.classList.remove("at-bottom");
  });
  const sourceButton = document.createElement("button");
  sourceButton.type = "button";
  sourceButton.className = "mermaid-action";
  sourceButton.dataset.mermaidSource = "1";
  sourceButton.textContent = i18n.t("源码");
  actions.append(open, expand, sourceButton);
  toolbar.append(label, actions);

  const viewport = document.createElement("div");
  viewport.className = "mermaid-viewport";
  const diagram = document.createElement("div");
  diagram.className = "mermaid-diagram";
  viewport.appendChild(diagram);
  viewport.addEventListener("scroll", () => {
    block.classList.toggle(
      "at-bottom",
      viewport.scrollTop + viewport.clientHeight >= viewport.scrollHeight - 2,
    );
  });

  const sourcePre = document.createElement("pre");
  sourcePre.className = "mermaid-source";
  sourcePre.textContent = source;
  sourcePre.hidden = true;

  block.append(toolbar, viewport, sourcePre);
  open.addEventListener("click", () => openMermaidModal(block));
  sourceButton.addEventListener("click", () => {
    sourcePre.hidden = !sourcePre.hidden;
    sourceButton.textContent = sourcePre.hidden ? i18n.t("源码") : i18n.t("图表");
    block.classList.toggle("is-source", !sourcePre.hidden);
  });
  return block;
}

function nativeSize(svg: SVGSVGElement): { width: number; height: number } {
  const box = svg.viewBox?.baseVal;
  if (box && box.width > 0 && box.height > 0) {
    return { width: box.width, height: box.height };
  }
  const rect = svg.getBoundingClientRect();
  return { width: Math.max(1, rect.width), height: Math.max(1, rect.height) };
}

function openMermaidModal(block: HTMLElement) {
  activeModalCleanup?.();
  const sourceSvg = block.querySelector<SVGSVGElement>(".mermaid-diagram svg");
  if (!sourceSvg) return;
  const source = block.dataset.source ?? "";

  const backdrop = document.createElement("div");
  backdrop.className = "mermaid-modal-backdrop";
  const modal = document.createElement("div");
  modal.className = "mermaid-modal";
  modal.setAttribute("role", "dialog");
  modal.setAttribute("aria-modal", "true");
  modal.setAttribute("aria-label", i18n.t("查看大图"));

  const header = document.createElement("div");
  header.className = "mermaid-modal-head";
  const title = document.createElement("div");
  title.className = "mermaid-modal-title";
  title.textContent = "Mermaid";
  const controls = document.createElement("div");
  controls.className = "mermaid-modal-controls";

  const button = (text: string, onClick: () => void) => {
    const item = document.createElement("button");
    item.type = "button";
    item.className = "mermaid-action";
    item.textContent = text;
    item.addEventListener("click", onClick);
    return item;
  };

  const sourcePre = document.createElement("pre");
  sourcePre.className = "mermaid-modal-source";
  sourcePre.textContent = source;
  sourcePre.hidden = true;
  const sourceButton = button(i18n.t("源码"), () => {
    sourcePre.hidden = !sourcePre.hidden;
    sourceButton.textContent = sourcePre.hidden ? i18n.t("源码") : i18n.t("图表");
  });
  const closeButton = button(i18n.t("关闭"), () => cleanup());

  controls.append(
    button(i18n.t("适应宽度"), () => fit("width")),
    button(i18n.t("适应页面"), () => fit("page")),
    button("100%", () => setScale(1)),
    button("−", () => zoomAt(0.8)),
    button("+", () => zoomAt(1.25)),
    button(i18n.t("重置视图"), () => fit("width")),
    sourceButton,
    closeButton,
  );
  header.append(title, controls);

  const viewport = document.createElement("div");
  viewport.className = "mermaid-modal-viewport";
  const content = document.createElement("div");
  content.className = "mermaid-modal-content";
  const clone = sourceSvg.cloneNode(true) as SVGSVGElement;
  const { width, height } = nativeSize(sourceSvg);
  clone.setAttribute("width", String(width));
  clone.setAttribute("height", String(height));
  clone.style.maxWidth = "none";
  clone.style.width = `${width}px`;
  clone.style.height = `${height}px`;
  content.appendChild(clone);
  viewport.appendChild(content);

  modal.append(header, viewport, sourcePre);
  backdrop.appendChild(modal);
  document.body.appendChild(backdrop);
  document.body.classList.add("mermaid-modal-open");

  let scale = 1;
  let tx = 0;
  let ty = 0;
  let fitMode: "width" | "page" | "custom" = "width";

  const apply = () => {
    content.style.transform = `translate(${tx}px, ${ty}px) scale(${scale})`;
  };
  const fit = (mode: "width" | "page") => {
    const vw = viewport.clientWidth;
    const vh = viewport.clientHeight;
    const pad = 40;
    const availableW = Math.max(1, vw - pad * 2);
    const availableH = Math.max(1, vh - pad * 2);
    if (mode === "width") scale = Math.max(0.15, availableW / width);
    else scale = Math.max(0.15, Math.min(availableW / width, availableH / height));
    tx = (vw - width * scale) / 2;
    ty = mode === "page" ? (vh - height * scale) / 2 : pad;
    fitMode = mode;
    apply();
  };
  const setScale = (next: number) => {
    const vw = viewport.clientWidth;
    const vh = viewport.clientHeight;
    const nextScale = Math.max(0.15, Math.min(5, next));
    const ratio = nextScale / scale;
    tx = vw / 2 - (vw / 2 - tx) * ratio;
    ty = vh / 2 - (vh / 2 - ty) * ratio;
    scale = nextScale;
    fitMode = "custom";
    apply();
  };
  const zoomAt = (factor: number) => setScale(scale * factor);

  viewport.addEventListener(
    "wheel",
    (event) => {
      event.preventDefault();
      const rect = viewport.getBoundingClientRect();
      const mx = event.clientX - rect.left;
      const my = event.clientY - rect.top;
      const next = Math.max(0.15, Math.min(5, scale * (event.deltaY < 0 ? 1.12 : 0.89)));
      const ratio = next / scale;
      tx = mx - (mx - tx) * ratio;
      ty = my - (my - ty) * ratio;
      scale = next;
      fitMode = "custom";
      apply();
    },
    { passive: false },
  );

  let dragging = false;
  let dragX = 0;
  let dragY = 0;
  viewport.addEventListener("pointerdown", (event) => {
    if ((event.target as HTMLElement).closest("button")) return;
    dragging = true;
    dragX = event.clientX - tx;
    dragY = event.clientY - ty;
    viewport.setPointerCapture(event.pointerId);
    viewport.classList.add("dragging");
  });
  viewport.addEventListener("pointermove", (event) => {
    if (!dragging) return;
    tx = event.clientX - dragX;
    ty = event.clientY - dragY;
    fitMode = "custom";
    apply();
  });
  const stopDrag = () => {
    dragging = false;
    viewport.classList.remove("dragging");
  };
  viewport.addEventListener("pointerup", stopDrag);
  viewport.addEventListener("pointercancel", stopDrag);

  const onResize = () => {
    if (fitMode !== "custom") fit(fitMode);
  };
  window.addEventListener("resize", onResize);
  const onKey = (event: KeyboardEvent) => {
    if (event.key === "Escape") cleanup();
  };
  window.addEventListener("keydown", onKey);

  const cleanup = () => {
    activeModalCleanup = null;
    window.removeEventListener("resize", onResize);
    window.removeEventListener("keydown", onKey);
    document.body.classList.remove("mermaid-modal-open");
    backdrop.remove();
  };
  activeModalCleanup = cleanup;
  backdrop.addEventListener("mousedown", (event) => {
    if (event.target === backdrop) cleanup();
  });
  requestAnimationFrame(() => {
    // Tall diagrams would become unreadably large with fit-width. Open them
    // with the whole graph visible, then let the user zoom into a region.
    fit(height > width ? "page" : "width");
    closeButton.focus();
  });
}

export async function enhanceMermaidDiagrams(
  article: HTMLElement,
  theme: Theme,
): Promise<() => void> {
  let cancelled = false;
  const blocks = new Set<HTMLElement>();
  article.querySelectorAll<HTMLPreElement>("pre").forEach((pre) => {
    const code = pre.querySelector<HTMLElement>("code.language-mermaid");
    if (!code) return;
    const block = createBlock(code.textContent ?? "");
    pre.replaceWith(block);
    blocks.add(block);
  });
  article.querySelectorAll<HTMLElement>(".mermaid-block").forEach((block) => {
    updateToolbar(block);
    if (block.dataset.renderedTheme !== theme) blocks.add(block);
  });
  if (blocks.size === 0) return () => {};

  try {
    const mermaid = await loadMermaid();
    if (cancelled) return () => {};
    mermaid.initialize(mermaidConfig(theme));
    let index = 0;
    for (const block of blocks) {
      if (cancelled) break;
      const source = block.dataset.source ?? "";
      const diagram = block.querySelector<HTMLElement>(".mermaid-diagram");
      if (!diagram) continue;
      block.dataset.renderedTheme = theme;
      try {
        const { svg } = await mermaid.render(
          `commentloop-mermaid-${Date.now()}-${index++}`,
          source,
        );
        if (cancelled) break;
        diagram.innerHTML = svg;
        fixMermaidContrast(diagram, theme);
        block.querySelector(".mermaid-error")?.remove();
        requestAnimationFrame(() => {
          const viewport = block.querySelector<HTMLElement>(".mermaid-viewport");
          if (!viewport) return;
          block.classList.toggle(
            "is-clipped",
            diagram.scrollHeight > viewport.clientHeight + 1,
          );
        });
      } catch (error) {
        if (cancelled) break;
        diagram.textContent = "";
        const failure = document.createElement("div");
        failure.className = "mermaid-error";
        failure.textContent =
          error instanceof Error ? error.message : i18n.t("Mermaid 渲染失败");
        const sourcePre = document.createElement("pre");
        sourcePre.className = "mermaid-error-source";
        sourcePre.textContent = source;
        diagram.append(failure, sourcePre);
        block.classList.remove("is-clipped");
      }
    }
  } catch (error) {
    console.error("commentloop: failed to load Mermaid", error);
  }

  return () => {
    cancelled = true;
    activeModalCleanup?.();
  };
}
