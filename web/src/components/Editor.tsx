import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
} from "react";
import { Compartment, EditorState, Prec } from "@codemirror/state";
import { EditorView, keymap, placeholder } from "@codemirror/view";
import { basicSetup } from "codemirror";
import { markdown, markdownLanguage } from "@codemirror/lang-markdown";
import { languages } from "@codemirror/language-data";
import { oneDark } from "@codemirror/theme-one-dark";
import type { Theme } from "../types";

export interface EditorDocument {
  content: string;
  version: number;
}

export interface EditorHandle {
  focus: () => void;
  scrollToRatio: (ratio: number) => void;
}

interface EditorProps {
  document: EditorDocument;
  theme: Theme;
  onChange: (value: string) => void;
  onSave: () => void;
  onScrollRatio: (ratio: number) => void;
}

const lightTheme = EditorView.theme(
  {
    "&": {
      height: "100%",
      backgroundColor: "transparent",
      color: "var(--text-primary)",
    },
    ".cm-scroller": {
      fontFamily: "var(--font-mono)",
      lineHeight: "1.72",
    },
    ".cm-content": {
      padding: "22px 18px 160px 6px",
      fontSize: "14.5px",
      caretColor: "var(--accent)",
    },
    ".cm-line": {
      padding: "0 2px",
    },
    ".cm-gutters": {
      backgroundColor: "transparent",
      color: "var(--text-faint)",
      border: "none",
      paddingLeft: "6px",
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
    ".cm-cursor, .cm-dropCursor": {
      borderLeftColor: "var(--accent)",
    },
    ".cm-panels": {
      backgroundColor: "var(--surface-2)",
      color: "var(--text-primary)",
    },
    ".cm-tooltip": {
      backgroundColor: "var(--surface-2)",
      border: "1px solid var(--border-strong)",
      borderRadius: "8px",
      color: "var(--text-primary)",
    },
    "&.cm-focused": { outline: "none" },
    ".cm-searchMatch": {
      backgroundColor: "var(--search-match)",
      outline: "1px solid var(--accent-soft-border)",
    },
    ".cm-searchMatch.cm-searchMatch-selected": {
      backgroundColor: "var(--search-match-selected)",
    },
    ".cm-placeholder": {
      color: "var(--text-faint)",
    },
  },
  { dark: false },
);

// oneDark provides the syntax colors; these overrides make the chrome blend
// into mdlive's own dark surface.
const darkOverrides = EditorView.theme(
  {
    "&": {
      height: "100%",
      backgroundColor: "transparent",
      color: "var(--text-primary)",
    },
    ".cm-scroller": {
      fontFamily: "var(--font-mono)",
      lineHeight: "1.72",
    },
    ".cm-content": {
      padding: "22px 18px 160px 6px",
      fontSize: "14.5px",
      caretColor: "var(--accent)",
    },
    ".cm-line": {
      padding: "0 2px",
    },
    ".cm-gutters": {
      backgroundColor: "transparent",
      color: "var(--text-faint)",
      border: "none",
      paddingLeft: "6px",
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
    ".cm-cursor, .cm-dropCursor": {
      borderLeftColor: "var(--accent)",
    },
    ".cm-panels": {
      backgroundColor: "var(--surface-2)",
      color: "var(--text-primary)",
    },
    ".cm-tooltip": {
      backgroundColor: "var(--surface-2)",
      border: "1px solid var(--border-strong)",
      borderRadius: "8px",
      color: "var(--text-primary)",
    },
    "&.cm-focused": { outline: "none" },
    ".cm-searchMatch": {
      backgroundColor: "var(--search-match)",
      outline: "1px solid var(--accent-soft-border)",
    },
    ".cm-searchMatch.cm-searchMatch-selected": {
      backgroundColor: "var(--search-match-selected)",
    },
    ".cm-placeholder": {
      color: "var(--text-faint)",
    },
  },
  { dark: true },
);

function themeExtensions(theme: Theme) {
  return theme === "dark" ? [oneDark, darkOverrides] : lightTheme;
}

export const Editor = forwardRef<EditorHandle, EditorProps>(function Editor(
  { document: doc, theme, onChange, onSave, onScrollRatio },
  ref,
) {
  const hostRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const themeCompartment = useRef(new Compartment());
  const applyingExternal = useRef(false);
  const versionRef = useRef(doc.version);

  const onChangeRef = useRef(onChange);
  const onSaveRef = useRef(onSave);
  const onScrollRef = useRef(onScrollRatio);
  const themeRef = useRef(theme);
  onChangeRef.current = onChange;
  onSaveRef.current = onSave;
  onScrollRef.current = onScrollRatio;
  themeRef.current = theme;

  useImperativeHandle(
    ref,
    () => ({
      focus() {
        viewRef.current?.focus();
      },
      scrollToRatio(ratio: number) {
        const view = viewRef.current;
        if (!view) return;
        const scroller = view.scrollDOM;
        scroller.scrollTop =
          ratio * Math.max(0, scroller.scrollHeight - scroller.clientHeight);
      },
    }),
    [],
  );

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    const view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: doc.content,
        extensions: [
          basicSetup,
          markdown({
            base: markdownLanguage,
            codeLanguages: languages,
          }),
          EditorView.lineWrapping,
          placeholder("开始写点什么，右侧会实时预览…"),
          themeCompartment.current.of(themeExtensions(themeRef.current)),
          Prec.highest(
            keymap.of([
              {
                key: "Mod-s",
                preventDefault: true,
                run: () => {
                  onSaveRef.current();
                  return true;
                },
              },
            ]),
          ),
          EditorView.contentAttributes.of({
            "aria-label": "Markdown 编辑区",
            spellcheck: "false",
          }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged && !applyingExternal.current) {
              onChangeRef.current(update.state.doc.toString());
            }
          }),
        ],
      }),
    });
    viewRef.current = view;

    const scroller = view.scrollDOM;
    const handleScroll = () => {
      const max = scroller.scrollHeight - scroller.clientHeight;
      onScrollRef.current(max <= 0 ? 0 : scroller.scrollTop / max);
    };
    scroller.addEventListener("scroll", handleScroll, { passive: true });

    return () => {
      scroller.removeEventListener("scroll", handleScroll);
      view.destroy();
      viewRef.current = null;
    };
    // The editor is created once; document/theme changes are applied below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    viewRef.current?.dispatch({
      effects: themeCompartment.current.reconfigure(themeExtensions(theme)),
    });
  }, [theme]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view || versionRef.current === doc.version) return;
    versionRef.current = doc.version;
    const current = view.state.doc.toString();
    if (current === doc.content) return;

    const scrollTop = view.scrollDOM.scrollTop;
    applyingExternal.current = true;
    try {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: doc.content },
      });
      view.scrollDOM.scrollTop = scrollTop;
    } finally {
      applyingExternal.current = false;
    }
  }, [doc]);

  return <div ref={hostRef} className="editor-host" />;
});
