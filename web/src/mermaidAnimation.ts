import i18n from "./i18n";

interface FlowEdge {
  from: string;
  to: string;
  id: string;
}

interface FlowStep {
  nodes: string[];
  edges: FlowEdge[];
}

interface ResolvedStep {
  nodes: SVGGElement[];
  edges: Array<{ path: SVGPathElement; label: Element | null }>;
}

interface EdgeOriginal {
  dasharray: string;
  dashoffset: string;
}

export interface MermaidAnimationController {
  readonly total: number;
  getStep: () => number;
  isActive: () => boolean;
  isPlaying: () => boolean;
  activate: () => void;
  deactivate: () => void;
  previous: () => void;
  next: () => void;
  toggle: () => void;
}

export const MERMAID_STEP_EVENT = "commentloop-mermaid-step";

const controllers = new WeakMap<HTMLElement, MermaidAnimationController>();

export function getMermaidAnimationController(
  block: HTMLElement,
): MermaidAnimationController | null {
  return controllers.get(block) ?? null;
}

const EDGE_OPERATOR = /(<-->|-->|---|-\.->|-\.-|==>|===|--x|--o)/g;
const MAX_STEPS = 160;

function normalizeStatement(line: string): string {
  return line
    .replace(/\|[^|]*\|/g, " ")
    .replace(/\s+--\s+[^\n-]+?\s+-->/g, " --> ")
    .replace(/\s*-\.\s+[^\n.]+?\s+\.->/g, " -.-> ")
    .replace(/\s*==\s+[^\n=]+?\s*==>/g, " ==> ")
    .replace(/:::[\w-]+/g, "");
}

function extractNodeIDs(segment: string): string[] {
  return segment
    .split("&")
    .map((part) => part.trim().match(/^([\w-]+)/)?.[1] ?? "")
    .filter(Boolean);
}

/**
 * Parse the subset of Mermaid flowchart syntax we can animate. This is a
 * presentation parser, not a Mermaid replacement: it only needs node IDs and
 * edge order so each source statement can reveal existing Mermaid SVG parts.
 */
export function parseFlowSteps(source: string): FlowStep[] {
  const raw = source.replace(/\r\n?/g, "\n").split(/\n|;/);
  let started = false;
  let isFlowchart = false;
  let steps: FlowStep[] = [];
  const seenNodes = new Set<string>();
  const declaredNodes: string[][] = [];
  const edgeCounts = new Map<string, number>();

  const addStep = (nodes: string[], edges: FlowEdge[]) => {
    const newNodes = nodes.filter((id) => {
      if (!id || seenNodes.has(id)) return false;
      seenNodes.add(id);
      return true;
    });
    if (!newNodes.length && !edges.length) return;
    steps.push({ nodes: newNodes, edges });
    if (steps.length > MAX_STEPS) steps = steps.slice(0, MAX_STEPS);
  };

  for (const rawLine of raw) {
    const line = rawLine.trim();
    if (!line || line.startsWith("%%")) continue;

    if (!started) {
      if (/^(?:flowchart|graph)(?:-elk)?\s+(?:TB|TD|BT|LR|RL)\b/i.test(line)) {
        started = true;
        isFlowchart = true;
      }
      continue;
    }
    if (!isFlowchart) break;
    if (/^(?:subgraph|end|direction|classDef|class|style|linkStyle|click|accTitle|accDescr)\b/i.test(line)) {
      continue;
    }

    const statement = normalizeStatement(line);
    if (!statement) continue;

    const parts = statement.split(EDGE_OPERATOR);
    if (parts.length === 1) {
      // A standalone node declaration. Defer it: many diagrams declare all
      // nodes first and connect them later. Revealing those boxes up front
      // makes the whole graph appear before any line starts, so nodes that
      // are used by an edge should enter together with that edge.
      const ids = extractNodeIDs(statement);
      if (ids.length) declaredNodes.push(ids);
      continue;
    }

    for (let i = 1; i < parts.length; i += 2) {
      const left = extractNodeIDs(parts[i - 1] ?? "");
      const right = extractNodeIDs(parts[i + 1] ?? "");
      if (!left.length || !right.length) continue;
      const edges: FlowEdge[] = [];
      for (const from of left) {
        for (const to of right) {
          const key = `${from}\u0000${to}`;
          const ordinal = edgeCounts.get(key) ?? 0;
          edgeCounts.set(key, ordinal + 1);
          edges.push({ from, to, id: `L_${from}_${to}_${ordinal}` });
        }
      }
      addStep([...left, ...right], edges);
    }
  }

  // Nodes declared but never used by an edge still need a step, otherwise
  // they would stay hidden for the whole playback.
  for (const ids of declaredNodes) {
    addStep(ids.filter((id) => !seenNodes.has(id)), []);
  }

  return steps;
}

function indexDiagram(diagram: HTMLElement) {
  const nodes = new Map<string, SVGGElement>();
  const edges = new Map<string, SVGPathElement>();
  const labels = new Map<string, Element>();

  diagram.querySelectorAll<SVGGElement>("g.node").forEach((node) => {
    const match = node.id.match(/-flowchart-(.+)-(\d+)$/);
    if (match) nodes.set(match[1], node);
  });
  diagram.querySelectorAll<SVGPathElement>("path[data-edge]").forEach((path) => {
    const id = path.dataset.id;
    if (id) edges.set(id, path);
  });
  diagram.querySelectorAll<Element>(".edgeLabel .label[data-id]").forEach((label) => {
    const id = label.getAttribute("data-id");
    if (id) labels.set(id, label.closest(".edgeLabel") ?? label);
  });

  return { nodes, edges, labels };
}

function edgeElement(path: SVGPathElement): Element {
  return path.closest("g.edgePath") ?? path;
}

function prepareEdgeOriginals(diagram: HTMLElement): WeakMap<SVGPathElement, EdgeOriginal> {
  const originals = new WeakMap<SVGPathElement, EdgeOriginal>();
  diagram.querySelectorAll<SVGPathElement>("path[data-edge]").forEach((path) => {
    const original = {
      dasharray: path.style.strokeDasharray,
      dashoffset: path.style.strokeDashoffset,
    };
    originals.set(path, original);
    path.dataset.mermaidOriginalDasharray = original.dasharray;
    path.dataset.mermaidOriginalDashoffset = original.dashoffset;
  });
  return originals;
}

function clearEdgeStyles(path: SVGPathElement, originals: WeakMap<SVGPathElement, EdgeOriginal>) {
  const original = originals.get(path);
  if (!original) return;
  path.style.strokeDasharray = original.dasharray;
  path.style.strokeDashoffset = original.dashoffset;
}

function createButton(className: string, text: string, title?: string): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.className = className;
  button.textContent = text;
  if (title) {
    button.title = title;
    button.setAttribute("aria-label", title);
  }
  return button;
}

export function setupMermaidAnimation(
  block: HTMLElement,
  diagram: HTMLElement,
  source: string,
): () => void {
  const animateButton = block.querySelector<HTMLButtonElement>("[data-mermaid-animate]");
  const sourcePre = block.querySelector<HTMLPreElement>(".mermaid-source");
  const viewport = block.querySelector<HTMLElement>(".mermaid-viewport");
  if (!animateButton || !sourcePre || !viewport) return () => {};

  const parsed = parseFlowSteps(source);
  if (parsed.length === 0) return () => {};

  const index = indexDiagram(diagram);
  const originals = prepareEdgeOriginals(diagram);
  const steps: ResolvedStep[] = [];
  for (const step of parsed) {
    const nodes = step.nodes.map((id) => index.nodes.get(id)).filter(Boolean) as SVGGElement[];
    const edges = step.edges
      .map((edge) => {
        const path = index.edges.get(edge.id);
        return path ? { path, label: index.labels.get(edge.id) ?? null } : null;
      })
      .filter(Boolean) as Array<{ path: SVGPathElement; label: Element | null }>;
    if (nodes.length || edges.length) steps.push({ nodes, edges });
  }
  if (steps.length === 0) return () => {};

  const controls = document.createElement("div");
  controls.className = "mermaid-animation-controls";
  controls.hidden = true;
  const prev = createButton("mermaid-step-button", "‹", i18n.t("上一步"));
  const toggle = createButton("mermaid-step-toggle", i18n.t("播放"));
  const next = createButton("mermaid-step-button", "›", i18n.t("下一步"));
  const stepLabel = document.createElement("span");
  stepLabel.className = "mermaid-step-label";
  const progress = document.createElement("span");
  progress.className = "mermaid-step-progress";
  const progressFill = document.createElement("i");
  progress.appendChild(progressFill);
  controls.append(prev, toggle, next, stepLabel, progress);
  block.insertBefore(controls, sourcePre);

  let active = false;
  let playing = false;
  let step = 0;
  let timer: number | undefined;
  const running = new Set<Animation>();

  const currentNodes = new Set<SVGGElement>();
  const currentEdges = new Set<Element>();

  const clearCurrent = () => {
    for (const node of currentNodes) node.classList.remove("mermaid-step-current");
    for (const edge of currentEdges) edge.classList.remove("mermaid-step-current");
    currentNodes.clear();
    currentEdges.clear();
  };

  const cancelDraws = () => {
    for (const animation of running) {
      try {
        animation.cancel();
      } catch {
        // The browser may have already finished it.
      }
    }
    running.clear();
    diagram.querySelectorAll<SVGPathElement>("path[data-edge]").forEach((path) => {
      clearEdgeStyles(path, originals);
    });
  };

  const revealUpTo = (target: number, animateNewEdges: boolean) => {
    cancelDraws();
    for (let i = 0; i < steps.length; i++) {
      const item = steps[i];
      const visible = i < target;
      for (const node of item.nodes) {
        node.classList.toggle("mermaid-step-hidden", !visible);
      }
      for (const edge of item.edges) {
        const element = edgeElement(edge.path);
        element.classList.toggle("mermaid-step-hidden", !visible);
        if (edge.label) edge.label.classList.toggle("mermaid-step-hidden", !visible);
      }
    }

    clearCurrent();
    if (target > 0) {
      const current = steps[target - 1];
      for (const node of current.nodes) {
        node.classList.add("mermaid-step-current");
        currentNodes.add(node);
      }
      for (const edge of current.edges) {
        const element = edgeElement(edge.path);
        element.classList.add("mermaid-step-current");
        currentEdges.add(element);
        if (animateNewEdges) drawEdge(edge.path);
      }
      const focus = current.nodes[0] ?? current.edges[0]?.path;
      if (focus) ensureVisible(focus);
    }
  };

  const drawEdge = (path: SVGPathElement) => {
    const length = path.getTotalLength();
    if (!length || typeof path.animate !== "function") return;
    const original = originals.get(path);
    if (!original) return;
    path.style.strokeDasharray = `${length}`;
    path.style.strokeDashoffset = `${length}`;
    let animation: Animation;
    try {
      animation = path.animate(
        [{ strokeDashoffset: `${length}` }, { strokeDashoffset: "0" }],
        { duration: 620, easing: "ease-out" },
      );
    } catch {
      clearEdgeStyles(path, originals);
      return;
    }
    running.add(animation);
    const finish = () => {
      running.delete(animation);
      clearEdgeStyles(path, originals);
    };
    animation.addEventListener("finish", finish, { once: true });
    animation.addEventListener("cancel", finish, { once: true });
  };

  const ensureVisible = (element: Element) => {
    if (block.classList.contains("is-expanded")) return;
    if (viewport.scrollHeight <= viewport.clientHeight + 2) return;
    const nodeRect = element.getBoundingClientRect();
    const viewRect = viewport.getBoundingClientRect();
    if (nodeRect.top >= viewRect.top + 8 && nodeRect.bottom <= viewRect.bottom - 8) {
      return;
    }
    const target =
      viewport.scrollTop +
      (nodeRect.top - viewRect.top) -
      Math.max(8, (viewport.clientHeight - nodeRect.height) / 2);
    viewport.scrollTo({ top: Math.max(0, target), behavior: "smooth" });
  };

  const updateControls = () => {
    const total = steps.length;
    toggle.textContent = playing
      ? i18n.t("暂停")
      : step >= total
        ? i18n.t("重播")
        : i18n.t("播放");
    stepLabel.textContent = `${i18n.t("步骤")} ${step} / ${total}`;
    progressFill.style.width = `${total ? (step / total) * 100 : 0}%`;
    prev.disabled = step <= 1;
    next.disabled = step >= total;
    block.dispatchEvent(
      new CustomEvent(MERMAID_STEP_EVENT, {
        detail: { active, playing, step, total },
      }),
    );
  };

  const stopTimer = () => {
    if (timer !== undefined) {
      window.clearTimeout(timer);
      timer = undefined;
    }
  };

  const schedule = () => {
    stopTimer();
    if (!playing) return;
    timer = window.setTimeout(() => {
      timer = undefined;
      if (step < steps.length) {
        step += 1;
        revealUpTo(step, true);
        updateControls();
        if (step >= steps.length) {
          playing = false;
          // The final step is fully revealed; keeping its accent color makes
          // the finished diagram look like it is still waiting on an action.
          clearCurrent();
          updateControls();
          return;
        }
        schedule();
      } else {
        playing = false;
        updateControls();
      }
    }, 1300);
  };

  const activate = () => {
    if (active) return;
    active = true;
    playing = false;
    step = 0;
    cancelDraws();
    block.classList.add("is-animating");
    controls.hidden = false;
    animateButton.textContent = i18n.t("退出");
    animateButton.setAttribute("aria-pressed", "true");
    for (const node of diagram.querySelectorAll<SVGGElement>("g.node")) {
      node.classList.add("mermaid-step-hidden");
    }
    for (const element of diagram.querySelectorAll<Element>(".edgePath, .edgeLabel")) {
      element.classList.add("mermaid-step-hidden");
    }
    step = 1;
    revealUpTo(step, true);
    playing = true;
    updateControls();
    schedule();
  };

  const deactivate = () => {
    active = false;
    playing = false;
    stopTimer();
    cancelDraws();
    step = 0;
    clearCurrent();
    for (const element of diagram.querySelectorAll<Element>(".mermaid-step-hidden")) {
      element.classList.remove("mermaid-step-hidden");
    }
    block.classList.remove("is-animating");
    controls.hidden = true;
    animateButton.textContent = i18n.t("动画");
    animateButton.setAttribute("aria-pressed", "false");
    updateControls();
  };

  const previous = () => {
    if (!active) return;
    playing = false;
    stopTimer();
    if (step > 1) {
      step -= 1;
      revealUpTo(step, false);
    }
    updateControls();
  };

  const forward = () => {
    if (!active) activate();
    playing = false;
    stopTimer();
    if (step < steps.length) {
      step += 1;
      revealUpTo(step, true);
      if (step >= steps.length) clearCurrent();
    }
    updateControls();
  };

  const togglePlayback = () => {
    if (!active) {
      activate();
      return;
    }
    if (playing) {
      playing = false;
      stopTimer();
    } else {
      if (step >= steps.length) {
        step = 1;
        revealUpTo(step, true);
      }
      playing = true;
      schedule();
    }
    updateControls();
  };

  const controller: MermaidAnimationController = {
    get total() {
      return steps.length;
    },
    getStep: () => step,
    isActive: () => active,
    isPlaying: () => playing,
    activate,
    deactivate,
    previous,
    next: forward,
    toggle: togglePlayback,
  };
  controllers.set(block, controller);

  const onAnimate = () => {
    if (active) deactivate();
    else activate();
  };
  const onToggle = () => togglePlayback();
  const onPrev = () => previous();
  const onNext = () => forward();

  animateButton.hidden = false;
  animateButton.setAttribute("aria-pressed", "false");
  animateButton.addEventListener("click", onAnimate);
  toggle.addEventListener("click", onToggle);
  prev.addEventListener("click", onPrev);
  next.addEventListener("click", onNext);
  updateControls();

  return () => {
    active = false;
    playing = false;
    stopTimer();
    cancelDraws();
    clearCurrent();
    for (const element of diagram.querySelectorAll<Element>(
      ".mermaid-step-hidden, .mermaid-step-current",
    )) {
      element.classList.remove("mermaid-step-hidden", "mermaid-step-current");
    }
    block.classList.remove("is-animating");
    animateButton.removeEventListener("click", onAnimate);
    toggle.removeEventListener("click", onToggle);
    prev.removeEventListener("click", onPrev);
    next.removeEventListener("click", onNext);
    animateButton.hidden = true;
    animateButton.textContent = i18n.t("动画");
    animateButton.setAttribute("aria-pressed", "false");
    controls.remove();
    controllers.delete(block);
  };
}
