import { useEffect, useRef, useState } from "react";
import { Empty, Input, Spin } from "antd";
import { useTranslation } from "react-i18next";
import { searchDocs } from "../api";
import type { SearchResult } from "../types";
import { IconSearch } from "./Icons";
import { AppModal } from "./AppModal";

interface SearchModalProps {
  onClose: () => void;
  onOpen: (path: string) => void;
}

function Highlight({ text, query }: { text: string; query: string }) {
  const needle = query.trim();
  if (!needle) return <>{text}</>;
  const lower = text.toLowerCase();
  const target = needle.toLowerCase();
  const parts: Array<{ text: string; hit: boolean }> = [];
  let offset = 0;
  while (offset < text.length) {
    const index = lower.indexOf(target, offset);
    if (index < 0) {
      parts.push({ text: text.slice(offset), hit: false });
      break;
    }
    if (index > offset) parts.push({ text: text.slice(offset, index), hit: false });
    parts.push({ text: text.slice(index, index + needle.length), hit: true });
    offset = index + needle.length;
  }
  return (
    <>
      {parts.map((part, index) =>
        part.hit ? <mark key={index}>{part.text}</mark> : <span key={index}>{part.text}</span>,
      )}
    </>
  );
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function SearchModal({ onClose, onOpen }: SearchModalProps) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const requestRef = useRef(0);

  useEffect(() => {
    const q = query.trim();
    const request = ++requestRef.current;
    if (!q) {
      setResults([]);
      setLoading(false);
      setError(null);
      return;
    }
    setLoading(true);
    const timer = window.setTimeout(() => {
      searchDocs(q)
        .then((response) => {
          if (requestRef.current === request) setResults(response.results);
        })
        .catch((err: unknown) => {
          if (requestRef.current === request) setError(errorText(err));
        })
        .finally(() => {
          if (requestRef.current === request) setLoading(false);
        });
    }, 220);
    return () => window.clearTimeout(timer);
  }, [query]);

  return (
    <AppModal
      title={
        <span className="app-modal-title">
          <IconSearch size={17} />
          {t("全文搜索")}
        </span>
      }
      width={680}
      onClose={onClose}
      className="search-modal"
    >
      <Input
        autoFocus
        size="large"
        value={query}
        onChange={(event) => {
          setQuery(event.target.value);
          setError(null);
        }}
        prefix={<IconSearch size={15} />}
        placeholder={t("搜索文档内容")}
        allowClear
      />
      <p className="search-scope-hint">{t("仅搜索本地目录")}</p>
      {error ? <p className="modal-error">{t("搜索失败：{{error}}", { error })}</p> : null}
      {loading ? (
        <div className="search-loading">
          <Spin />
        </div>
      ) : query.trim() && results.length === 0 && !error ? (
        <Empty description={t("没有匹配结果")} />
      ) : (
        <div className="search-results">
          {results.map((result) => (
            <button
              key={`${result.path}:${result.line}`}
              type="button"
              className="search-result"
              onClick={() => onOpen(result.path)}
            >
              <div className="search-result-head">
                <strong>{result.name}</strong>
                <span>{result.rootName}/{result.relPath}</span>
                <em>{result.line > 0 ? `L${result.line}` : t("路径匹配")}</em>
              </div>
              <div className="search-result-snippet">
                <Highlight text={result.snippet} query={query} />
              </div>
            </button>
          ))}
        </div>
      )}
    </AppModal>
  );
}
