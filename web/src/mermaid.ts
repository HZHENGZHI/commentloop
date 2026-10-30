import i18n from "./i18n";
import {
  getMermaidAnimationController,
  MERMAID_STEP_EVENT,
  setupMermaidAnimation,
} from "./mermaidAnimation";
import type { Theme } from "./types";

type MermaidApi = typeof import("mermaid").default;

let mermaidPromise: Promise<MermaidApi> | null = null;

// Rendered (and contrast-fixed) SVG markup keyed by theme + source. Document
// updates replace the preview HTML, so without this every agent edit would
// re-run Mermaid layout for every diagram on the page.
const SVG_CACHE_LIMIT = 64;
const svgCache = new Map<string, string>();

function cachedSvg(key: string): string | undefined {
  const svg = svgCache.get(key);
  if (svg !== undefined) {
    svgCache.delete(key);
    svgCache.set(key, svg);
  }
  return svg;
}

function storeSvg(key: string, svg: string) {
  svgCache.set(key, svg);
  while (svgCache.size > SVG_CACHE_LIMIT) {
    const oldest = svgCache.keys().next().value;
    if (oldest === undefined) break;
    svgCache.delete(oldest);
  }
}
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

function stripEdgeLabelBackgrounds(container: Element) {
  container.querySelectorAll<HTMLElement>(".edgeLabel, .edgeLabel *, rect.background, .labelBkg").forEach((el) => {
    el.style.setProperty("background", "transparent", "important");
    el.style.setProperty("background-color", "transparent", "important");
    if (el.tagName.toLowerCase() === "rect") {
      el.style.setProperty("fill", "transparent", "important");
      el.style.setProperty("stroke", "none", "important");
    }
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
            primaryColor: "#eef3f8",
            primaryTextColor: "#1f2328",
            primaryBorderColor: "#9fb4ca",
            secondaryColor: "#f6f7f8",
            secondaryTextColor: "#1f2328",
            secondaryBorderColor: "#cfd8e2",
            tertiaryColor: "#fbfbfd",
            tertiaryTextColor: "#1f2328",
            lineColor: "#6b7280",
            textColor: "#1f2328",
            labelTextColor: "#1f2328",
            loopTextColor: "#1f2328",
            noteTextColor: "#1f2328",
            noteBkgColor: "#f7f9fb",
            noteBorderColor: "#cfd8e2",
            activationBkgColor: "#f6f7f8",
            activationBorderColor: "#9fb4ca",
            sequenceNumberColor: "#1f2328",
            classText: "#1f2328",
            stateTextColor: "#1f2328",
            stateLabelColor: "#1f2328",
            transitionLabelColor: "#374151",
            mainBkg: "#eef3f8",
            nodeBorder: "#9fb4ca",
            clusterBkg: "#f7f9fb",
            clusterBorder: "#cfd8e2",
            edgeLabelBackground: "transparent",
            titleColor: "#1f2328",
            actorBkg: "#eef3f8",
            actorBorder: "#9fb4ca",
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
  const animate = document.createElement("button");
  animate.type = "button";
  animate.className = "mermaid-action";
  animate.dataset.mermaidAnimate = "1";
  animate.textContent = i18n.t("动画");
  animate.hidden = true;
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
  actions.append(animate, open, expand, sourceButton);
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
  const animation = getMermaidAnimationController(block);

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
  const { width, height } = nativeSize(sourceSvg);
  const makeClone = () => {
    const next = sourceSvg.cloneNode(true) as SVGSVGElement;
    next.setAttribute("width", String(width));
    next.setAttribute("height", String(height));
    next.style.maxWidth = "none";
    next.style.width = `${width}px`;
    next.style.height = `${height}px`;
    stripEdgeLabelBackgrounds(next);
    return next;
  };
  const clone = makeClone();
  content.appendChild(clone);
  viewport.appendChild(content);

  const sourceNodes = Array.from(sourceSvg.querySelectorAll<SVGGElement>("g.node"));
  const cloneNodes = Array.from(clone.querySelectorAll<SVGGElement>("g.node"));
  const sourceEdges = Array.from(sourceSvg.querySelectorAll<SVGPathElement>("path[data-edge]"));
  const cloneEdges = Array.from(clone.querySelectorAll<SVGPathElement>("path[data-edge]"));
  const sourceLabels = Array.from(sourceSvg.querySelectorAll<Element>(".edgeLabel"));
  const cloneLabels = Array.from(clone.querySelectorAll<Element>(".edgeLabel"));
  const stepClasses = ["mermaid-step-hidden", "mermaid-step-current"];
  const syncStepClasses = (from: Element, to: Element) => {
    for (const name of stepClasses) {
      to.classList.toggle(name, from.classList.contains(name));
    }
  };
  let cloneStep = -1;
  const cloneDraws = new Set<Animation>();
  const restoreCloneEdge = (path: SVGPathElement) => {
    path.style.strokeDasharray = path.dataset.mermaidOriginalDasharray ?? "";
    path.style.strokeDashoffset = path.dataset.mermaidOriginalDashoffset ?? "";
    path.setAttribute("marker-end", path.dataset.mermaidOriginalMarkerEnd ?? "");
  };
  const cancelCloneDraws = () => {
    for (const animation of cloneDraws) {
      try {
        animation.cancel();
      } catch {
        // Already finished.
      }
    }
    cloneDraws.clear();
    for (const path of cloneEdges) restoreCloneEdge(path);
  };
  const drawCloneEdge = (path: SVGPathElement) => {
    const length = path.getTotalLength();
    if (!length || typeof path.animate !== "function") {
      restoreCloneEdge(path);
      return;
    }
    path.style.strokeDasharray = `${length}`;
    path.style.strokeDashoffset = `${length}`;
    path.setAttribute("marker-end", "");
    const animation = path.animate(
      [{ strokeDashoffset: `${length}` }, { strokeDashoffset: "0" }],
      { duration: 520, easing: "ease-out" },
    );
    cloneDraws.add(animation);
    const finish = () => {
      cloneDraws.delete(animation);
      restoreCloneEdge(path);
    };
    animation.addEventListener("finish", finish, { once: true });
    animation.addEventListener("cancel", finish, { once: true });
  };
  const syncClone = (animateEdges: boolean) => {
    const current = animation?.isActive() ? animation.getStep() : 0;
    const changed = current !== cloneStep;
    if (changed) cancelCloneDraws();

    for (let i = 0; i < sourceNodes.length; i++) {
      const target = cloneNodes[i];
      if (target) syncStepClasses(sourceNodes[i], target);
    }
    const hasCurrentEdges = sourceEdges.some((path) =>
      path.classList.contains("mermaid-step-current"),
    );
    for (let i = 0; i < sourceEdges.length; i++) {
      const target = cloneEdges[i];
      if (!target) continue;
      const source = sourceEdges[i];
      syncStepClasses(source, target);
      if (changed && animateEdges && source.classList.contains("mermaid-step-current")) {
        drawCloneEdge(target);
      }
    }
    for (let i = 0; i < sourceLabels.length; i++) {
      const target = cloneLabels[i];
      if (target) syncStepClasses(sourceLabels[i], target);
    }
    if (!hasCurrentEdges) cancelCloneDraws();
    cloneStep = current;
  };

  const playback = document.createElement("div");
  playback.className = "mermaid-animation-controls mermaid-modal-playback";
  playback.hidden = !animation;
  const playbackPrev = document.createElement("button");
  playbackPrev.type = "button";
  playbackPrev.className = "mermaid-step-button";
  playbackPrev.textContent = "‹";
  playbackPrev.title = i18n.t("上一步");
  playbackPrev.setAttribute("aria-label", i18n.t("上一步"));
  const playbackToggle = document.createElement("button");
  playbackToggle.type = "button";
  playbackToggle.className = "mermaid-step-toggle";
  playbackToggle.textContent = i18n.t("播放");
  const playbackNext = document.createElement("button");
  playbackNext.type = "button";
  playbackNext.className = "mermaid-step-button";
  playbackNext.textContent = "›";
  playbackNext.title = i18n.t("下一步");
  playbackNext.setAttribute("aria-label", i18n.t("下一步"));
  const playbackStep = document.createElement("span");
  playbackStep.className = "mermaid-step-label";
  const playbackProgress = document.createElement("span");
  playbackProgress.className = "mermaid-step-progress";
  const playbackFill = document.createElement("i");
  playbackProgress.appendChild(playbackFill);
  playback.append(playbackPrev, playbackToggle, playbackNext, playbackStep, playbackProgress);

  const syncPlayback = (animateEdges = false) => {
    if (!animation) return;
    const total = animation.total;
    const current = animation.getStep();
    const active = animation.isActive();
    playbackToggle.textContent = !active
      ? i18n.t("播放")
      : animation.isPlaying()
        ? i18n.t("暂停")
        : current >= total
          ? i18n.t("重播")
          : i18n.t("播放");
    playbackStep.textContent = `${i18n.t("步骤")} ${current} / ${total}`;
    playbackFill.style.width = `${total ? (current / total) * 100 : 0}%`;
    playbackPrev.disabled = !active || current <= 1;
    playbackNext.disabled = !active || current >= total;
    syncClone(animateEdges);
  };

  const onStep = (event: Event) => {
    const detail = (event as CustomEvent<{ animateEdges?: boolean }>).detail;
    syncPlayback(detail?.animateEdges === true);
  };
  if (animation) {
    block.addEventListener(MERMAID_STEP_EVENT, onStep);
    playbackPrev.addEventListener("click", () => animation.previous());
    playbackNext.addEventListener("click", () => animation.next());
    playbackToggle.addEventListener("click", () => {
      if (!animation.isActive()) animation.activate();
      else animation.toggle();
    });
    syncPlayback();
  }

  modal.append(header, playback, viewport, sourcePre);
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
    block.removeEventListener(MERMAID_STEP_EVENT, onStep);
    cancelCloneDraws();
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
  isCancelled: () => boolean = () => false,
): Promise<() => void> {
  let cancelled = false;
  const stopped = () => cancelled || isCancelled() || !article.isConnected;
  const blocks = new Set<HTMLElement>();
  const animationDisposers: Array<() => void> = [];
  article.querySelectorAll<HTMLPreElement>("pre").forEach((pre) => {
    const code = pre.querySelector<HTMLElement>("code.language-mermaid");
    if (!code) return;
    const block = createBlock(code.textContent ?? "");
    pre.replaceWith(block);
    blocks.add(block);
  });
  const bindAnimation = (block: HTMLElement) => {
    const diagram = block.querySelector<HTMLElement>(".mermaid-diagram");
    if (!diagram?.querySelector("svg")) return;
    animationDisposers.push(
      setupMermaidAnimation(block, diagram, block.dataset.source ?? ""),
    );
  };
  article.querySelectorAll<HTMLElement>(".mermaid-block").forEach((block) => {
    updateToolbar(block);
    if (block.dataset.renderedTheme !== theme) blocks.add(block);
    else bindAnimation(block);
  });
  if (blocks.size === 0) {
    return () => {
      for (const dispose of animationDisposers) dispose();
    };
  }

  const markClipped = (block: HTMLElement, diagram: HTMLElement) => {
    requestAnimationFrame(() => {
      const viewport = block.querySelector<HTMLElement>(".mermaid-viewport");
      if (!viewport) return;
      block.classList.toggle(
        "is-clipped",
        diagram.scrollHeight > viewport.clientHeight + 1,
      );
    });
  };

  try {
    let mermaid: MermaidApi | null = null;
    let index = 0;
    for (const block of blocks) {
      if (stopped()) break;
      const source = block.dataset.source ?? "";
      const diagram = block.querySelector<HTMLElement>(".mermaid-diagram");
      if (!diagram) continue;
      const cacheKey = `${theme}\u0000${source}`;
      const cached = cachedSvg(cacheKey);
      if (cached !== undefined) {
        block.dataset.renderedTheme = theme;
        diagram.innerHTML = cached;
        animationDisposers.push(setupMermaidAnimation(block, diagram, source));
        block.querySelector(".mermaid-error")?.remove();
        markClipped(block, diagram);
        continue;
      }
      if (!mermaid) {
        mermaid = await loadMermaid();
        if (stopped()) break;
        mermaid.initialize(mermaidConfig(theme));
      }
      try {
        const { svg } = await mermaid.render(
          `commentloop-mermaid-${Date.now()}-${index++}`,
          source,
        );
        if (stopped()) break;
        block.dataset.renderedTheme = theme;
        diagram.innerHTML = svg;
        fixMermaidContrast(diagram, theme);
        stripEdgeLabelBackgrounds(diagram);
        storeSvg(cacheKey, diagram.innerHTML);
        animationDisposers.push(setupMermaidAnimation(block, diagram, source));
        block.querySelector(".mermaid-error")?.remove();
        markClipped(block, diagram);
      } catch (error) {
        if (stopped()) break;
        block.dataset.renderedTheme = theme;
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
    for (const dispose of animationDisposers) dispose();
  };
}
