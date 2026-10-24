import { useEffect, useRef } from "react";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { basicSetup } from "codemirror";
import { LanguageDescription } from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { oneDark } from "@codemirror/theme-one-dark";
import type { Theme } from "../types";

interface CodeViewerProps {
  filename: string;
  content: string;
  theme: Theme;
  target?: { line: number; character: number; nonce: number } | null;
  onDefinition: (line: number, character: number) => void;
  onCursorChange?: (line: number, character: number) => void;
}

const codeTheme = EditorView.theme(
  {
    "&": {
      height: "100%",
      backgroundColor: "transparent",
      color: "var(--text-primary)",
    },
    ".cm-scroller": {
      fontFamily: "var(--font-mono)",
      lineHeight: "1.65",
      fontSize: "12.5px",
    },
    ".cm-content": {
      padding: "12px 16px 80px 6px",
    },
    ".cm-line": {
      padding: "0 2px",
    },
    ".cm-gutters": {
      backgroundColor: "transparent",
      color: "var(--text-faint)",
      border: "none",
      paddingLeft: "6px",
      minWidth: "38px",
    },
    ".cm-activeLine": {
      backgroundColor: "var(--editor-active-line)",
    },
    ".cm-activeLineGutter": {
      backgroundColor: "transparent",
      color: "var(--accent)",
    },
    ".cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection": {
      backgroundColor: "var(--selection) !important",
    },
    "&.cm-focused": { outline: "none" },
    ".cm-cursor, .cm-dropCursor": {
      borderLeftColor: "var(--accent)",
    },
  },
  { dark: false },
);

const darkTheme = EditorView.theme(
  {
    "&": {
      height: "100%",
      backgroundColor: "transparent",
      color: "var(--text-primary)",
    },
    ".cm-scroller": {
      fontFamily: "var(--font-mono)",
      lineHeight: "1.65",
      fontSize: "12.5px",
    },
    ".cm-content": {
      padding: "12px 16px 80px 6px",
    },
    ".cm-line": {
      padding: "0 2px",
    },
    ".cm-gutters": {
      backgroundColor: "transparent",
      color: "var(--text-faint)",
      border: "none",
      paddingLeft: "6px",
      minWidth: "38px",
    },
    ".cm-activeLine": {
      backgroundColor: "var(--editor-active-line)",
    },
    ".cm-activeLineGutter": {
      backgroundColor: "transparent",
      color: "var(--accent)",
    },
    ".cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection": {
      backgroundColor: "var(--selection) !important",
    },
    "&.cm-focused": { outline: "none" },
    ".cm-cursor, .cm-dropCursor": {
      borderLeftColor: "var(--accent)",
    },
  },
  { dark: true },
);

export function CodeViewer({
  filename,
  content,
  theme,
  target,
  onDefinition,
  onCursorChange,
}: CodeViewerProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const languageCompartment = useRef(new Compartment());
  const definitionRef = useRef(onDefinition);
  definitionRef.current = onDefinition;
  const cursorRef = useRef(onCursorChange);
  cursorRef.current = onCursorChange;

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const language = languageCompartment.current;
    const state = EditorState.create({
      doc: content,
      extensions: [
        basicSetup,
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
        theme === "dark" ? oneDark : codeTheme,
        theme === "dark" ? darkTheme : codeTheme,
        language.of([]),
        EditorView.domEventHandlers({
          mousedown: (event, view) => {
            if (
              event.button !== 0 ||
              (!event.metaKey && !event.ctrlKey)
            ) {
              return false;
            }
            const pos = view.posAtCoords({
              x: event.clientX,
              y: event.clientY,
            });
            if (pos == null) return false;
            const line = view.state.doc.lineAt(pos);
            definitionRef.current(line.number, pos - line.from + 1);
            event.preventDefault();
            return true;
          },
        }),
        EditorView.updateListener.of((update) => {
          if (!update.selectionSet && !update.docChanged) return;
          const pos = update.state.selection.main.head;
          const line = update.state.doc.lineAt(pos);
          cursorRef.current?.(line.number, pos - line.from + 1);
        }),
      ],
    });
    const view = new EditorView({ state, parent: host });
    viewRef.current = view;
    const initialPos = view.state.selection.main.head;
    const initialLine = view.state.doc.lineAt(initialPos);
    cursorRef.current?.(initialLine.number, initialPos - initialLine.from + 1);

    let active = true;
    const description = LanguageDescription.matchFilename(
      languages,
      filename,
    );
    if (description) {
      void description.load().then((support) => {
        if (!active || viewRef.current !== view) return;
        view.dispatch({
          effects: language.reconfigure(support),
        });
      });
    }

    return () => {
      active = false;
      viewRef.current = null;
      view.destroy();
    };
  }, [content, filename, theme]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view || !target) return;
    const lineNumber = Math.max(
      1,
      Math.min(target.line, view.state.doc.lines),
    );
    const line = view.state.doc.line(lineNumber);
    const pos = Math.min(
      line.to,
      line.from + Math.max(0, target.character - 1),
    );
    view.dispatch({
      selection: { anchor: pos },
      effects: EditorView.scrollIntoView(pos, { y: "center" }),
    });
    view.focus();
  }, [target, content, filename]);

  return <div className="code-viewer" ref={hostRef} />;
}
